package promql

import (
	"context"
	"fmt"
	"math"
	"sync"
)

// Reading is a number that might not exist, and says which.
//
// Prometheus cannot tell "nothing matched" from "that metric was never collected" —
// both come back empty. The usual fix, `or vector(0)`, turns both into zero, and zero is
// the most reassuring thing this screen can say. On a cluster whose kube-state-metrics
// does not publish pod reasons at all, that reports "0 evicted pods" to somebody deciding
// whether to go back to bed.
//
// So presence is probed separately: if the bare metric exists, a filtered count of zero is
// a real zero and Known is true. If it does not, the panel shows N/A and says why.
type Reading struct {
	Value float64 `json:"value"`
	Known bool    `json:"known"`
}

// known marks a value as read. A value that is not a real number is not a reading:
// NaN means the quantile had nothing to work with, which is "no data", not zero.
func known(v float64) Reading {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Unknown
	}
	return Reading{Value: v, Known: true}
}

// counted is a reading of something discrete. increase() extrapolates to the edges of
// its window, so a counter that stepped 29 times comes back as 29.26 — and "29.3
// restarts in the last hour" is a third of an event that never happened.
func counted(v float64) Reading {
	r := known(v)
	if r.Known {
		r.Value = math.Round(r.Value)
	}
	return r
}

// Unknown is the zero value, and is what a missing metric produces.
var Unknown = Reading{}

// ClusterSummary is the top row: the ten numbers somebody reads in the first two seconds.
type ClusterSummary struct {
	// Status is Healthy, Degraded, Critical or Unknown — derived from the conditions in
	// Reasons, never from a score. Unknown when the inputs cannot be read, because
	// falling back to Healthy is how a broken monitor reports a broken cluster as fine.
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`

	NodesReady         Reading `json:"nodesReady"`
	NodesTotal         Reading `json:"nodesTotal"`
	NodesNotReady      Reading `json:"nodesNotReady"`
	NodesUnschedulable Reading `json:"nodesUnschedulable"`
	NodesUnderPressure Reading `json:"nodesUnderPressure"`

	PodsReady Reading `json:"podsReady"`
	PodsTotal Reading `json:"podsTotal"`
	// PodsPending is pods waiting to start for a reason nobody has named yet — the
	// scheduler cannot place them, or they are still being created. A pod whose image
	// will not pull is also in phase Pending, but it is counted as a stuck container
	// instead: reporting it twice, under two names, is what made this number disagree
	// with the problem table underneath it.
	PodsPending Reading `json:"podsPending"`
	// ContainersNotStarting is the other half of that split: containers held in a
	// waiting state that already has a name — ImagePullBackOff, CrashLoopBackOff,
	// CreateContainerConfigError.
	ContainersNotStarting Reading `json:"containersNotStarting"`
	// LongestPendingSeconds is how long the oldest Pending pod has been waiting. A pod
	// pending for ten seconds is a scheduler doing its job; one pending for two days is
	// a cluster that cannot fit it.
	LongestPendingSeconds Reading `json:"longestPendingSeconds"`

	Restarts1h  Reading `json:"restarts1h"`
	OOMKilled   Reading `json:"oomKilled"`
	Evicted     Reading `json:"evicted"`
	Unavailable Reading `json:"unavailableWorkloads"`

	AlertsCritical Reading `json:"alertsCritical"`
	AlertsWarning  Reading `json:"alertsWarning"`

	APIErrorRate Reading `json:"apiErrorRate"`
	TargetsDown  Reading `json:"targetsDown"`
	TargetsTotal Reading `json:"targetsTotal"`
}

// stuckContainers is a container held in a waiting state that already says what is
// wrong. ContainerCreating and PodInitializing are excluded: they are a pod starting,
// not a pod failing, and every panel that uses this treats them as pending instead.
const stuckContainers = `kube_pod_container_status_waiting_reason{reason!~"ContainerCreating|PodInitializing"} > 0`

const (
	StatusHealthy  = "Healthy"
	StatusDegraded = "Degraded"
	StatusCritical = "Critical"
	StatusUnknown  = "Unknown"
)

// readSummary fills the top row.
func readSummary(ctx context.Context, client *Client) ClusterSummary {
	var out ClusterSummary
	var mu sync.Mutex
	var wg sync.WaitGroup

	// probe asks two questions at once: does this metric exist, and what does it say.
	probe := func(presence, value string, assign func(Reading)) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if !exists(ctx, client, presence) {
				return // leaves Unknown
			}
			samples, err := client.Query(ctx, value)
			if err != nil || len(samples) == 0 {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			assign(known(samples[0].Value))
		}()
	}

	const nodeConditions = "kube_node_status_condition"
	probe(nodeConditions, `count(kube_node_status_condition{condition="Ready",status="true"} == 1) or vector(0)`,
		func(r Reading) { out.NodesReady = r })
	probe(nodeConditions, `count(kube_node_status_condition{condition="Ready",status="true"} == 0) or vector(0)`,
		func(r Reading) { out.NodesNotReady = r })
	probe(nodeConditions, `count(kube_node_status_condition{condition=~"MemoryPressure|DiskPressure|PIDPressure",status="true"} == 1) or vector(0)`,
		func(r Reading) { out.NodesUnderPressure = r })
	probe("kube_node_info", `count(kube_node_info) or vector(0)`,
		func(r Reading) { out.NodesTotal = r })
	probe("kube_node_spec_unschedulable", `count(kube_node_spec_unschedulable == 1) or vector(0)`,
		func(r Reading) { out.NodesUnschedulable = r })

	probe("kube_pod_status_ready", `count(kube_pod_status_ready{condition="true"} == 1) or vector(0)`,
		func(r Reading) { out.PodsReady = r })
	probe("kube_pod_info", `count(kube_pod_info) or vector(0)`,
		func(r Reading) { out.PodsTotal = r })
	// Pending, minus the pods already reported by a louder name.
	//
	// Kubernetes leaves a pod in phase Pending while its image will not pull, so the
	// plain sum counted an ImagePullBackOff pod as pending — and the problem table
	// below labelled that same pod ImagePullBackOff, so the screen showed "1 pending"
	// with nothing pending on it. ContainerCreating and PodInitializing stay in: those
	// are genuinely a pod on its way up, and the problem table treats them the same way.
	probe("kube_pod_status_phase", `sum(
		kube_pod_status_phase{phase="Pending"} == 1
		unless on (namespace, pod) (`+stuckContainers+`)
	) or vector(0)`,
		func(r Reading) { out.PodsPending = r })
	probe("kube_pod_container_status_waiting_reason", `count(`+stuckContainers+`) or vector(0)`,
		func(r Reading) { out.ContainersNotStarting = r })
	probe("kube_pod_start_time", `max(
		time() - kube_pod_start_time
		and on (namespace, pod) (
			kube_pod_status_phase{phase="Pending"} == 1
			unless on (namespace, pod) (`+stuckContainers+`)
		)
	) or vector(0)`,
		func(r Reading) { out.LongestPendingSeconds = r })

	probe("kube_pod_container_status_restarts_total",
		`sum(increase(kube_pod_container_status_restarts_total[1h])) or vector(0)`,
		func(r Reading) { out.Restarts1h = counted(r.Value) })
	probe("kube_pod_container_status_last_terminated_reason",
		`count(kube_pod_container_status_last_terminated_reason{reason="OOMKilled"} == 1) or vector(0)`,
		func(r Reading) { out.OOMKilled = r })
	// Deliberately its own metric rather than folded into the one above: a cluster whose
	// kube-state-metrics does not publish pod reasons must say N/A here, not zero.
	probe("kube_pod_status_reason", `count(kube_pod_status_reason{reason="Evicted"} == 1) or vector(0)`,
		func(r Reading) { out.Evicted = r })

	probe("kube_deployment_status_replicas_unavailable",
		`count(kube_deployment_status_replicas_unavailable > 0) or vector(0)`,
		func(r Reading) { out.Unavailable = r })

	probe("ALERTS", `count(ALERTS{alertstate="firing",severity="critical"}) or vector(0)`,
		func(r Reading) { out.AlertsCritical = r })
	probe("ALERTS", `count(ALERTS{alertstate="firing",severity="warning"}) or vector(0)`,
		func(r Reading) { out.AlertsWarning = r })

	probe("apiserver_request_total",
		`100 * sum(rate(apiserver_request_total{code=~"5.."}[5m])) / sum(rate(apiserver_request_total[5m])) or vector(0)`,
		func(r Reading) { out.APIErrorRate = r })

	probe("up", `count(up == 0) or vector(0)`, func(r Reading) { out.TargetsDown = r })
	probe("up", `count(up) or vector(0)`, func(r Reading) { out.TargetsTotal = r })

	wg.Wait()

	out.Status, out.Reasons = deriveStatus(out)
	return out
}

// exists asks whether a metric is collected at all.
func exists(ctx context.Context, client *Client, metric string) bool {
	samples, err := client.Query(ctx, "count("+metric+")")
	return err == nil && len(samples) > 0 && samples[0].Value > 0
}

// deriveStatus turns the readings into one word and the reasons behind it.
//
// Every branch is an explicit condition somebody can argue with, and the reasons are
// returned so the card can say what tripped it. A number nobody could read never counts
// as evidence of health — it counts as not knowing.
func deriveStatus(s ClusterSummary) (string, []string) {
	var critical, degraded []string

	add := func(list *[]string, format string, args ...any) {
		*list = append(*list, fmt.Sprintf(format, args...))
	}

	if s.NodesNotReady.Known && s.NodesNotReady.Value > 0 {
		add(&critical, "%.0f node not ready", s.NodesNotReady.Value)
	}
	if s.APIErrorRate.Known && s.APIErrorRate.Value > 5 {
		add(&critical, "API server returning %.1f%% 5xx", s.APIErrorRate.Value)
	}
	if s.TargetsTotal.Known && s.TargetsDown.Known && s.TargetsTotal.Value > 0 &&
		s.TargetsDown.Value > s.TargetsTotal.Value/2 {
		add(&critical, "%.0f of %.0f scrape targets down", s.TargetsDown.Value, s.TargetsTotal.Value)
	}
	if s.AlertsCritical.Known && s.AlertsCritical.Value > 0 {
		add(&critical, "%.0f critical alert(s) firing", s.AlertsCritical.Value)
	}

	if s.NodesUnderPressure.Known && s.NodesUnderPressure.Value > 0 {
		add(&degraded, "%.0f node under pressure", s.NodesUnderPressure.Value)
	}
	if s.NodesUnschedulable.Known && s.NodesUnschedulable.Value > 0 {
		add(&degraded, "%.0f node cordoned", s.NodesUnschedulable.Value)
	}
	if s.Unavailable.Known && s.Unavailable.Value > 0 {
		add(&degraded, "%.0f workload(s) short of replicas", s.Unavailable.Value)
	}
	if s.LongestPendingSeconds.Known && s.LongestPendingSeconds.Value > 15*60 {
		add(&degraded, "a pod has been pending for %s", humanDuration(s.LongestPendingSeconds.Value))
	}
	// Its own reason rather than a share of "pending": these pods were scheduled and
	// then stopped, and the fix is an image or a config, not room in the cluster.
	if s.ContainersNotStarting.Known && s.ContainersNotStarting.Value > 0 {
		add(&degraded, "%.0f container(s) will not start", s.ContainersNotStarting.Value)
	}
	if s.OOMKilled.Known && s.OOMKilled.Value > 0 {
		add(&degraded, "%.0f container(s) OOMKilled", s.OOMKilled.Value)
	}
	if s.Evicted.Known && s.Evicted.Value > 0 {
		add(&degraded, "%.0f pod(s) evicted", s.Evicted.Value)
	}
	if s.APIErrorRate.Known && s.APIErrorRate.Value > 1 && s.APIErrorRate.Value <= 5 {
		add(&degraded, "API server returning %.1f%% 5xx", s.APIErrorRate.Value)
	}
	if s.TargetsDown.Known && s.TargetsDown.Value > 0 {
		add(&degraded, "%.0f scrape target(s) down", s.TargetsDown.Value)
	}
	if s.AlertsWarning.Known && s.AlertsWarning.Value > 0 {
		add(&degraded, "%.0f warning alert(s) firing", s.AlertsWarning.Value)
	}

	switch {
	case len(critical) > 0:
		return StatusCritical, append(critical, degraded...)
	case len(degraded) > 0:
		return StatusDegraded, degraded
	}

	// Nothing is wrong — but "nothing is wrong" is only worth saying if anything could
	// have been read. Without the node and pod metrics this screen knows nothing, and
	// saying Healthy would be a guess wearing a green badge.
	if !s.NodesReady.Known && !s.PodsReady.Known {
		return StatusUnknown, []string{"no cluster metrics are being collected"}
	}
	return StatusHealthy, nil
}

func humanDuration(seconds float64) string {
	switch {
	case seconds >= 86400:
		return fmt.Sprintf("%.0fd", seconds/86400)
	case seconds >= 3600:
		return fmt.Sprintf("%.0fh", seconds/3600)
	default:
		return fmt.Sprintf("%.0fm", seconds/60)
	}
}
