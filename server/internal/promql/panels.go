package promql

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// The panels behind rows 2 and 4 through 8.
//
// Everything here is shaped so the reader can act on it: a finding names the object it is
// about, precisely enough for the screen to open it. A count that cannot be clicked
// through is a count somebody has to go and look for.

// Finding is one object that is wrong, and why.
type Finding struct {
	// Kind is the Kubernetes kind, so the screen knows where to send the reader.
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
	// Reason is the short label — CrashLoopBackOff, Pending, Failed.
	Reason string `json:"reason"`
	// Detail is the sentence under it, when there is one worth reading.
	Detail string `json:"detail,omitempty"`
	// Severity is "error" or "warn". Errors sort first because they are what somebody
	// opened this screen to find.
	Severity string  `json:"severity"`
	Node     string  `json:"node,omitempty"`
	AgeSecs  float64 `json:"ageSeconds,omitempty"`

	// rank is how specific this finding is, and never leaves the server. It exists so
	// one broken pod cannot be counted three times — see collapsePodFindings.
	rank int
}

// How specific a pod finding is. A pod whose image will not pull is also Pending and
// also NotReady, and every one of those is true; listing all three counts one broken
// pod as three problems and puts a number above the table that nothing in it explains.
const (
	rankWaiting = iota
	rankTerminated
	rankFailed
	rankPending
	rankNotReady
)

// collapsePodFindings keeps the most specific thing known about each pod.
//
// A container that is failing is described by its own reason, and the pod-level Pending
// and NotReady that follow from it are dropped. A pod with no container-level finding
// keeps one pod-level finding — the most specific of them. Everything that is not a Pod
// passes through untouched: a Deployment and the pods under it are separate objects and
// a reader wants both.
func collapsePodFindings(findings []Finding) []Finding {
	type podKey struct{ namespace, name string }

	// A container-level finding for the container, and the best pod-level one for a pod
	// that has none.
	byContainer := map[podKey]map[string]Finding{}
	byPod := map[podKey]Finding{}
	var out []Finding

	for _, f := range findings {
		if f.Kind != "Pod" {
			out = append(out, f)
			continue
		}
		key := podKey{f.Namespace, f.Name}
		if f.Container != "" {
			if byContainer[key] == nil {
				byContainer[key] = map[string]Finding{}
			}
			if best, ok := byContainer[key][f.Container]; !ok || f.rank < best.rank {
				// The exit reason explains the backoff rather than competing with it, so
				// the one that loses is kept as the sentence under the one that wins.
				if ok && best.rank == rankTerminated {
					f.Detail = "last exit: " + best.Reason
				}
				byContainer[key][f.Container] = f
			} else if best.rank == rankWaiting && f.rank == rankTerminated {
				best.Detail = "last exit: " + f.Reason
				byContainer[key][f.Container] = best
			}
			continue
		}
		if best, ok := byPod[key]; !ok || f.rank < best.rank {
			byPod[key] = f
		}
	}

	for key, containers := range byContainer {
		for _, f := range containers {
			out = append(out, f)
		}
		delete(byPod, key)
	}
	for _, f := range byPod {
		out = append(out, f)
	}
	return out
}

// WorkloadRow is one controller and how far it is from what was asked for.
type WorkloadRow struct {
	Kind         string  `json:"kind"`
	Namespace    string  `json:"namespace"`
	Name         string  `json:"name"`
	Ready        float64 `json:"ready"`
	Desired      float64 `json:"desired"`
	Updated      float64 `json:"updated"`
	Available    float64 `json:"available"`
	Misscheduled float64 `json:"misscheduled"`
}

// Healthy is the question the row exists to answer, and it is not "is it Running".
func (w WorkloadRow) Healthy() bool { return w.Desired > 0 && w.Ready >= w.Desired }

// Alert is one firing Prometheus alert.
type Alert struct {
	Name      string `json:"name"`
	Severity  string `json:"severity"`
	Namespace string `json:"namespace,omitempty"`
	Object    string `json:"object,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// ControlPlane is row 7. Every field is a Reading because on a managed or k3s-class
// cluster most of these endpoints are simply not scraped, and saying so is the honest
// answer — not zero.
type ControlPlane struct {
	APIServers    Reading `json:"apiServers"`
	APILatencyP50 Reading `json:"apiLatencyP50"`
	APILatencyP95 Reading `json:"apiLatencyP95"`
	APILatencyP99 Reading `json:"apiLatencyP99"`
	APIErrors4xx  Reading `json:"apiErrors4xx"`
	APIErrors5xx  Reading `json:"apiErrors5xx"`
	APIRequests   Reading `json:"apiRequests"`

	EtcdMembers       Reading `json:"etcdMembers"`
	EtcdHasLeader     Reading `json:"etcdHasLeader"`
	EtcdLeaderChanges Reading `json:"etcdLeaderChanges"`
	EtcdDBBytes       Reading `json:"etcdDbBytes"`
	EtcdFsyncP99      Reading `json:"etcdFsyncP99"`

	SchedulerAttempts      Reading `json:"schedulerAttempts"`
	SchedulerUnschedulable Reading `json:"schedulerUnschedulable"`

	CoreDNSUp         Reading `json:"corednsUp"`
	CoreDNSErrorRate  Reading `json:"corednsErrorRate"`
	CoreDNSLatencyP99 Reading `json:"corednsLatencyP99"`

	ScrapeTargets  Reading `json:"scrapeTargets"`
	ScrapeFailures Reading `json:"scrapeFailures"`
	RuleFailures   Reading `json:"ruleFailures"`

	// CertExpiryDays is the soonest certificate expiry, in days.
	CertExpiryDays Reading `json:"certExpiryDays"`

	ControllerQueueDepth Reading `json:"controllerQueueDepth"`
	ControllerRetries    Reading `json:"controllerRetries"`
	IngressRequests      Reading `json:"ingressRequests"`
	IngressErrorRate     Reading `json:"ingressErrorRate"`
	IngressLatencyP99    Reading `json:"ingressLatencyP99"`
	QuotaNearLimit       Reading `json:"quotaNearLimit"`
	VolumeCapacityBytes  Reading `json:"volumeCapacityBytes"`
	VolumesBound         Reading `json:"volumesBound"`
	VolumeRequestedBytes Reading `json:"volumeRequestedBytes"`
	VolumeUsedBytes      Reading `json:"volumeUsedBytes"`
}

// Spread is how many pods one namespace has on one node.
//
// The total per namespace cannot show this: a namespace whose pods all landed on one
// machine loses everything when that machine reboots, and reads as perfectly healthy
// until it does.
type Spread struct {
	Namespace string  `json:"namespace"`
	Node      string  `json:"node"`
	Pods      float64 `json:"pods"`
}

// NamespaceUsage is row 8: who is actually consuming the cluster.
type NamespaceUsage struct {
	Namespace      string  `json:"namespace"`
	CPUCores       float64 `json:"cpuCores"`
	CPURequests    float64 `json:"cpuRequests"`
	MemoryBytes    float64 `json:"memoryBytes"`
	MemoryRequests float64 `json:"memoryRequests"`
	Pods           float64 `json:"pods"`
}

// Trends is row 5: the shapes that only make sense over time.
type Trends struct {
	Disk          []Series      `json:"-"`
	DiskByNode    []NamedSeries `json:"diskByNode"`
	NetworkRx     []NamedSeries `json:"networkRx"`
	NetworkTx     []NamedSeries `json:"networkTx"`
	CPUByNodeOver []NamedSeries `json:"cpuByNodeOverTime"`
	MemoryByNode  []NamedSeries `json:"memoryByNodeOverTime"`
	// NodeCPUCores and NodeMemoryBytes are the same machines in absolute units, measured
	// the way Kubernetes measures them. A percentage says how full a node is; a pod asks
	// for millicores and mebibytes, and those are what the reader compares against.
	NodeCPUCores    []NamedSeries `json:"nodeCpuCoresOverTime"`
	NodeMemoryBytes []NamedSeries `json:"nodeMemoryBytesOverTime"`
	IOWaitByNode    []NamedSeries `json:"ioWaitByNode"`

	// Sparks are the little lines under the summary tiles. A count with no shape behind
	// it cannot tell a spike from a plateau, and those call for different actions.
	Sparks map[string][]Point `json:"sparks"`
}

// NamedSeries is one labelled line on a chart.
type NamedSeries struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

// readProblems collects every object that is wrong into one list.
//
// One list rather than nine panels because the first question is "what is broken", not
// "what kind of broken thing would I like to look at". The kind is a column.
func readProblems(ctx context.Context, client *Client) []Finding {
	var mu sync.Mutex
	var out []Finding
	var wg sync.WaitGroup

	// Pod ages, looked up once and joined in memory: a finding without "for how long"
	// cannot be triaged, and a rollout looks exactly like an outage for its first minute.
	ages := map[string]float64{}
	if samples, err := client.Query(ctx, `time() - kube_pod_start_time`); err == nil {
		for _, s := range samples {
			ages[s.Labels["namespace"]+"/"+s.Labels["pod"]] = s.Value
		}
	}

	collect := func(expr string, build func(Sample) (Finding, bool)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			samples, err := client.Query(ctx, expr)
			if err != nil {
				return
			}
			var found []Finding
			for _, s := range samples {
				if f, ok := build(s); ok {
					if f.Kind == "Pod" && f.AgeSecs == 0 {
						f.AgeSecs = ages[f.Namespace+"/"+f.Name]
					}
					found = append(found, f)
				}
			}
			mu.Lock()
			out = append(out, found...)
			mu.Unlock()
		}()
	}

	collect(`kube_pod_container_status_waiting_reason > 0`, func(s Sample) (Finding, bool) {
		reason := s.Labels["reason"]
		// ContainerCreating and PodInitializing are a pod starting normally, not a
		// problem. Listing them turns every deployment into an incident.
		if reason == "ContainerCreating" || reason == "PodInitializing" || reason == "" {
			return Finding{}, false
		}
		return Finding{
			Kind: "Pod", Namespace: s.Labels["namespace"], Name: s.Labels["pod"],
			Container: s.Labels["container"], Reason: reason,
			Detail: "container will not start", Severity: "error",
			rank: rankWaiting,
		}, true
	})

	collect(`kube_pod_container_status_last_terminated_reason > 0`, func(s Sample) (Finding, bool) {
		reason := s.Labels["reason"]
		if reason == "Completed" || reason == "" {
			return Finding{}, false
		}
		return Finding{
			Kind: "Pod", Namespace: s.Labels["namespace"], Name: s.Labels["pod"],
			Container: s.Labels["container"], Reason: reason,
			Detail: "last exit reason", Severity: "error",
			rank: rankTerminated,
		}, true
	})

	collect(`kube_pod_status_phase{phase="Pending"} == 1`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "Pod", Namespace: s.Labels["namespace"], Name: s.Labels["pod"],
			Reason: "Pending", Detail: "not scheduled or not started", Severity: "warn",
			rank: rankPending,
		}, true
	})

	collect(`kube_pod_status_phase{phase="Failed"} == 1`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "Pod", Namespace: s.Labels["namespace"], Name: s.Labels["pod"],
			Reason: "Failed", Detail: "gave up", Severity: "error",
			rank: rankFailed,
		}, true
	})

	// Running but not passing readiness: the case a phase count cannot show, and the one
	// that means traffic is not arriving.
	collect(`kube_pod_status_ready{condition="true"} == 0`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "Pod", Namespace: s.Labels["namespace"], Name: s.Labels["pod"],
			Reason: "NotReady", Detail: "running but failing readiness", Severity: "warn",
			rank: rankNotReady,
		}, true
	})

	collect(`kube_deployment_status_replicas_unavailable > 0`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "Deployment", Namespace: s.Labels["namespace"], Name: s.Labels["deployment"],
			Reason: "Unavailable", Detail: "below desired replicas", Severity: "error",
		}, true
	})

	collect(`kube_statefulset_status_replicas_ready < kube_statefulset_status_replicas`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "StatefulSet", Namespace: s.Labels["namespace"], Name: s.Labels["statefulset"],
			Reason: "Unavailable", Detail: "below desired replicas", Severity: "error",
		}, true
	})

	collect(`kube_daemonset_status_number_unavailable > 0`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "DaemonSet", Namespace: s.Labels["namespace"], Name: s.Labels["daemonset"],
			Reason: "Unavailable", Detail: "not running on every node", Severity: "warn",
		}, true
	})

	collect(`kube_job_status_failed > 0`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "Job", Namespace: s.Labels["namespace"], Name: s.Labels["job_name"],
			Reason: "Failed", Detail: "job did not complete", Severity: "error",
		}, true
	})

	collect(`kube_persistentvolumeclaim_status_phase{phase!="Bound"} == 1`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "PersistentVolumeClaim", Namespace: s.Labels["namespace"],
			Name: s.Labels["persistentvolumeclaim"], Reason: s.Labels["phase"],
			Detail: "not bound to a volume", Severity: "warn",
		}, true
	})

	collect(`kube_node_status_condition{condition="Ready",status="true"} == 0`, func(s Sample) (Finding, bool) {
		return Finding{
			Kind: "Node", Name: s.Labels["node"], Reason: "NotReady",
			Detail: "node is not accepting work", Severity: "error",
		}, true
	})

	collect(`kube_node_status_condition{condition=~"MemoryPressure|DiskPressure|PIDPressure",status="true"} == 1`,
		func(s Sample) (Finding, bool) {
			return Finding{
				Kind: "Node", Name: s.Labels["node"], Reason: s.Labels["condition"],
				Detail: "node is under pressure", Severity: "warn",
			}, true
		})

	wg.Wait()

	out = collapsePodFindings(out)

	// Errors first, then the longest-standing, then by name. Stable, because a list that
	// reorders itself under the cursor cannot be clicked.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Severity == "error") != (out[j].Severity == "error") {
			return out[i].Severity == "error"
		}
		if out[i].AgeSecs != out[j].AgeSecs {
			return out[i].AgeSecs > out[j].AgeSecs
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})

	// Bounded: a cluster with two thousand broken pods should not send two thousand rows
	// to a browser. The count above the table is the whole number; this is what is drawn.
	const most = 200
	if len(out) > most {
		out = out[:most]
	}
	return out
}

// readWorkloads fills the replica tables.
func readWorkloads(ctx context.Context, client *Client) []WorkloadRow {
	rows := map[string]*WorkloadRow{}
	var mu sync.Mutex

	at := func(kind, namespace, name string) *WorkloadRow {
		if name == "" {
			return nil
		}
		key := kind + "/" + namespace + "/" + name
		if _, ok := rows[key]; !ok {
			rows[key] = &WorkloadRow{Kind: kind, Namespace: namespace, Name: name}
		}
		return rows[key]
	}

	gather := func(expr, kind, nameLabel string, assign func(*WorkloadRow, float64)) {
		samples, err := client.Query(ctx, expr)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, s := range samples {
			if row := at(kind, s.Labels["namespace"], s.Labels[nameLabel]); row != nil {
				assign(row, s.Value)
			}
		}
	}

	gather(`kube_deployment_status_replicas_ready`, "Deployment", "deployment",
		func(w *WorkloadRow, v float64) { w.Ready = v })
	gather(`kube_deployment_spec_replicas`, "Deployment", "deployment",
		func(w *WorkloadRow, v float64) { w.Desired = v })
	gather(`kube_deployment_status_replicas_updated`, "Deployment", "deployment",
		func(w *WorkloadRow, v float64) { w.Updated = v })
	gather(`kube_deployment_status_replicas_available`, "Deployment", "deployment",
		func(w *WorkloadRow, v float64) { w.Available = v })

	gather(`kube_statefulset_status_replicas_ready`, "StatefulSet", "statefulset",
		func(w *WorkloadRow, v float64) { w.Ready = v })
	gather(`kube_statefulset_replicas`, "StatefulSet", "statefulset",
		func(w *WorkloadRow, v float64) { w.Desired = v })
	gather(`kube_statefulset_status_replicas_current`, "StatefulSet", "statefulset",
		func(w *WorkloadRow, v float64) { w.Available = v })
	gather(`kube_statefulset_status_replicas_updated`, "StatefulSet", "statefulset",
		func(w *WorkloadRow, v float64) { w.Updated = v })

	gather(`kube_daemonset_status_number_available`, "DaemonSet", "daemonset",
		func(w *WorkloadRow, v float64) { w.Available = v })
	gather(`kube_daemonset_status_desired_number_scheduled`, "DaemonSet", "daemonset",
		func(w *WorkloadRow, v float64) { w.Desired = v })
	gather(`kube_daemonset_status_number_ready`, "DaemonSet", "daemonset",
		func(w *WorkloadRow, v float64) { w.Ready = v })
	gather(`kube_daemonset_status_number_misscheduled`, "DaemonSet", "daemonset",
		func(w *WorkloadRow, v float64) { w.Misscheduled = v })

	out := make([]WorkloadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}

	// Unhealthy first: the table is read to find what is short, not to admire what is not.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Healthy() != out[j].Healthy() {
			return !out[i].Healthy()
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// readAlerts reads Prometheus's own firing alerts.
//
// From the ALERTS series rather than Alertmanager: it needs no second connection, and the
// alerts a cluster's own Prometheus is firing are the ones its operators wrote.
func readAlerts(ctx context.Context, client *Client) []Alert {
	samples, err := client.Query(ctx, `ALERTS{alertstate="firing"}`)
	if err != nil {
		return nil
	}

	out := make([]Alert, 0, len(samples))
	for _, s := range samples {
		alert := Alert{
			Name:      s.Labels["alertname"],
			Severity:  s.Labels["severity"],
			Namespace: s.Labels["namespace"],
			Summary:   s.Labels["summary"],
		}
		// Whichever object label the rule happened to carry, so the alert can be opened.
		for label, kind := range map[string]string{
			"pod": "Pod", "deployment": "Deployment", "statefulset": "StatefulSet",
			"daemonset": "DaemonSet", "node": "Node", "job_name": "Job",
			"persistentvolumeclaim": "PersistentVolumeClaim",
		} {
			if value := s.Labels[label]; value != "" {
				alert.Object, alert.Kind = value, kind
				break
			}
		}
		if alert.Name != "" {
			out = append(out, alert)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return severityRank(out[i].Severity) < severityRank(out[j].Severity)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

// readControlPlane fills row 7, saying N/A wherever the endpoint is not scraped.
func readControlPlane(ctx context.Context, client *Client) ControlPlane {
	var out ControlPlane
	var mu sync.Mutex
	var wg sync.WaitGroup

	probe := func(presence, value string, assign func(Reading)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !exists(ctx, client, presence) {
				return
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

	const api = "apiserver_request_total"
	// Control-plane nodes, not scrape targets. The same API server is often scraped
	// twice under two `instance` labels — on k3s the kubelet and the API server are one
	// process, so both jobs return the same series — and counting targets reported two
	// API servers on a cluster that has one. The instance count is the fallback for a
	// cluster whose kube-state-metrics does not publish node roles.
	probe(api, `count(kube_node_role{role=~"control-plane|master"})
		or count(count by (instance) (apiserver_request_total))
		or vector(0)`,
		func(r Reading) { out.APIServers = r })
	probe(api, `sum(rate(apiserver_request_total[5m])) or vector(0)`,
		func(r Reading) { out.APIRequests = r })
	probe(api, `100 * sum(rate(apiserver_request_total{code=~"4.."}[5m])) / sum(rate(apiserver_request_total[5m])) or vector(0)`,
		func(r Reading) { out.APIErrors4xx = r })
	probe(api, `100 * sum(rate(apiserver_request_total{code=~"5.."}[5m])) / sum(rate(apiserver_request_total[5m])) or vector(0)`,
		func(r Reading) { out.APIErrors5xx = r })

	const apiLatency = "apiserver_request_duration_seconds_bucket"
	for _, q := range []struct {
		quantile string
		assign   func(Reading)
	}{
		{"0.50", func(r Reading) { out.APILatencyP50 = r }},
		{"0.95", func(r Reading) { out.APILatencyP95 = r }},
		{"0.99", func(r Reading) { out.APILatencyP99 = r }},
	} {
		probe(apiLatency,
			`histogram_quantile(`+q.quantile+`, sum by (le) (rate(apiserver_request_duration_seconds_bucket{verb!~"WATCH|WATCHLIST|CONNECT"}[5m]))) or vector(0)`,
			q.assign)
	}

	probe("etcd_server_has_leader", `count(etcd_server_has_leader) or vector(0)`,
		func(r Reading) { out.EtcdMembers = r })
	probe("etcd_server_has_leader", `max(etcd_server_has_leader) or vector(0)`,
		func(r Reading) { out.EtcdHasLeader = r })
	probe("etcd_server_leader_changes_seen_total",
		`max(increase(etcd_server_leader_changes_seen_total[1h])) or vector(0)`,
		func(r Reading) { out.EtcdLeaderChanges = r })
	probe("etcd_mvcc_db_total_size_in_bytes", `max(etcd_mvcc_db_total_size_in_bytes) or vector(0)`,
		func(r Reading) { out.EtcdDBBytes = r })
	probe("etcd_disk_wal_fsync_duration_seconds_bucket",
		`histogram_quantile(0.99, sum by (le) (rate(etcd_disk_wal_fsync_duration_seconds_bucket[5m]))) or vector(0)`,
		func(r Reading) { out.EtcdFsyncP99 = r })

	probe("scheduler_schedule_attempts_total",
		`sum(rate(scheduler_schedule_attempts_total[5m])) or vector(0)`,
		func(r Reading) { out.SchedulerAttempts = r })
	probe("scheduler_pending_pods", `sum(scheduler_pending_pods) or vector(0)`,
		func(r Reading) { out.SchedulerUnschedulable = r })

	probe("coredns_dns_requests_total", `count(count by (instance) (coredns_dns_requests_total)) or vector(0)`,
		func(r Reading) { out.CoreDNSUp = r })
	probe("coredns_dns_responses_total",
		`100 * sum(rate(coredns_dns_responses_total{rcode=~"SERVFAIL|REFUSED"}[5m])) / sum(rate(coredns_dns_responses_total[5m])) or vector(0)`,
		func(r Reading) { out.CoreDNSErrorRate = r })
	probe("coredns_dns_request_duration_seconds_bucket",
		`histogram_quantile(0.99, sum by (le) (rate(coredns_dns_request_duration_seconds_bucket[5m]))) or vector(0)`,
		func(r Reading) { out.CoreDNSLatencyP99 = r })

	probe("up", `count(up) or vector(0)`, func(r Reading) { out.ScrapeTargets = r })
	probe("up", `count(up == 0) or vector(0)`, func(r Reading) { out.ScrapeFailures = r })
	probe("prometheus_rule_evaluation_failures_total",
		`sum(increase(prometheus_rule_evaluation_failures_total[1h])) or vector(0)`,
		func(r Reading) { out.RuleFailures = r })

	// Certificates: whichever exporter is publishing them. The soonest expiry is the only
	// one worth a number — the rest are a table.
	probe("apiserver_client_certificate_expiration_seconds_bucket",
		`min(apiserver_client_certificate_expiration_seconds_bucket) / 86400 or vector(0)`,
		func(r Reading) { out.CertExpiryDays = r })

	// Controller manager: a queue that is growing is reconciliation falling behind, which
	// shows up as objects that quietly never converge.
	probe("workqueue_depth", `sum(workqueue_depth) or vector(0)`,
		func(r Reading) { out.ControllerQueueDepth = r })
	probe("workqueue_retries_total", `sum(rate(workqueue_retries_total[5m])) or vector(0)`,
		func(r Reading) { out.ControllerRetries = r })

	probe("nginx_ingress_controller_requests",
		`sum(rate(nginx_ingress_controller_requests[5m])) or vector(0)`,
		func(r Reading) { out.IngressRequests = r })
	probe("nginx_ingress_controller_requests",
		`100 * sum(rate(nginx_ingress_controller_requests{status=~"5.."}[5m])) / sum(rate(nginx_ingress_controller_requests[5m])) or vector(0)`,
		func(r Reading) { out.IngressErrorRate = r })
	probe("nginx_ingress_controller_request_duration_seconds_bucket",
		`histogram_quantile(0.99, sum by (le) (rate(nginx_ingress_controller_request_duration_seconds_bucket[5m]))) or vector(0)`,
		func(r Reading) { out.IngressLatencyP99 = r })

	probe("kube_resourcequota",
		`count(kube_resourcequota{type="used"} / on (namespace, resource) kube_resourcequota{type="hard"} > 0.9) or vector(0)`,
		func(r Reading) { out.QuotaNearLimit = r })

	probe("kube_persistentvolume_capacity_bytes",
		`sum(kube_persistentvolume_capacity_bytes) or vector(0)`,
		func(r Reading) { out.VolumeCapacityBytes = r })
	probe("kube_persistentvolume_status_phase",
		`count(kube_persistentvolume_status_phase{phase="Bound"} == 1) or vector(0)`,
		func(r Reading) { out.VolumesBound = r })
	// What the claims asked for, against what the volumes provide. Actual usage needs
	// kubelet_volume_stats, which many installs do not scrape — so it is N/A rather than
	// a number that would look like usage and is not.
	probe("kube_persistentvolumeclaim_resource_requests_storage_bytes",
		`sum(kube_persistentvolumeclaim_resource_requests_storage_bytes) or vector(0)`,
		func(r Reading) { out.VolumeRequestedBytes = r })
	probe("kubelet_volume_stats_used_bytes",
		`sum(kubelet_volume_stats_used_bytes) or vector(0)`,
		func(r Reading) { out.VolumeUsedBytes = r })

	wg.Wait()
	return out
}

// readNamespaces fills row 8.
func readNamespaces(ctx context.Context, client *Client) []NamespaceUsage {
	rows := map[string]*NamespaceUsage{}

	at := func(namespace string) *NamespaceUsage {
		if namespace == "" {
			return nil
		}
		if _, ok := rows[namespace]; !ok {
			rows[namespace] = &NamespaceUsage{Namespace: namespace}
		}
		return rows[namespace]
	}

	gather := func(expr string, assign func(*NamespaceUsage, float64)) {
		samples, err := client.Query(ctx, expr)
		if err != nil {
			return
		}
		for _, s := range samples {
			if row := at(s.Labels["namespace"]); row != nil {
				assign(row, s.Value)
			}
		}
	}

	gather(`sum by (namespace) (rate(container_cpu_usage_seconds_total{container!=""}[5m]))`,
		func(n *NamespaceUsage, v float64) { n.CPUCores = v })
	gather(`sum by (namespace) (container_memory_working_set_bytes{container!=""})`,
		func(n *NamespaceUsage, v float64) { n.MemoryBytes = v })
	gather(`sum by (namespace) (kube_pod_container_resource_requests{resource="cpu",node!=""})`,
		func(n *NamespaceUsage, v float64) { n.CPURequests = v })
	gather(`sum by (namespace) (kube_pod_container_resource_requests{resource="memory",node!=""})`,
		func(n *NamespaceUsage, v float64) { n.MemoryRequests = v })
	gather(`count by (namespace) (kube_pod_info)`,
		func(n *NamespaceUsage, v float64) { n.Pods = v })

	out := make([]NamespaceUsage, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUCores > out[j].CPUCores })
	return out
}

// readSpread fills the namespace-by-node density view.
func readSpread(ctx context.Context, client *Client) []Spread {
	samples, err := client.Query(ctx, `count by (namespace, node) (kube_pod_info{node!=""})`)
	if err != nil {
		return nil
	}

	out := make([]Spread, 0, len(samples))
	for _, s := range samples {
		namespace, node := s.Labels["namespace"], s.Labels["node"]
		// "unknown" is what kube-state-metrics calls a pod that never landed anywhere.
		// It is not a machine, and a column for it would invent one.
		if namespace == "" || node == "" || node == "unknown" {
			continue
		}
		out = append(out, Spread{Namespace: namespace, Node: node, Pods: s.Value})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Node < out[j].Node
	})
	return out
}

// readTrends fills row 5 — the shapes that only mean something over time.
func readTrends(ctx context.Context, client *Client, window, step time.Duration) Trends {
	var out Trends
	var mu sync.Mutex
	var wg sync.WaitGroup

	named := func(expr, label string, assign func([]NamedSeries)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			series, err := client.QueryRange(ctx, expr, window, step)
			if err != nil {
				return
			}
			lines := make([]NamedSeries, 0, len(series))
			for _, s := range series {
				name := s.Labels[label]
				if name == "" {
					name = s.Labels["instance"]
				}
				lines = append(lines, NamedSeries{Name: name, Points: s.Points})
			}
			sort.Slice(lines, func(i, j int) bool { return lines[i].Name < lines[j].Name })
			mu.Lock()
			defer mu.Unlock()
			assign(lines)
		}()
	}

	named(queryCPUByNode, "nodename", func(s []NamedSeries) { out.CPUByNodeOver = s })
	named(queryMemoryByNode, "nodename", func(s []NamedSeries) { out.MemoryByNode = s })
	named(queryNodeCPUCores, "instance", func(s []NamedSeries) { out.NodeCPUCores = s })
	named(queryNodeMemoryBytes, "instance", func(s []NamedSeries) { out.NodeMemoryBytes = s })
	named(queryNodeIOWait, "nodename", func(s []NamedSeries) { out.IOWaitByNode = s })

	// One line per summary tile that has a shape worth seeing.
	sparks := map[string]string{
		"podsReady":            `count(kube_pod_status_ready{condition="true"} == 1)`,
		"podsPending":          `sum(kube_pod_status_phase{phase="Pending"})`,
		"nodesReady":           `count(kube_node_status_condition{condition="Ready",status="true"} == 1)`,
		"restarts1h":           `sum(increase(kube_pod_container_status_restarts_total[1h]))`,
		"apiErrorRate":         `100 * sum(rate(apiserver_request_total{code=~"5.."}[5m])) / sum(rate(apiserver_request_total[5m]))`,
		"targetsDown":          `count(up == 0) or vector(0)`,
		"oomKilled":            `count(kube_pod_container_status_last_terminated_reason{reason="OOMKilled"} == 1) or vector(0)`,
		"unavailableWorkloads": `count(kube_deployment_status_replicas_unavailable > 0) or vector(0)`,
	}
	for name, expr := range sparks {
		key, query := name, expr
		wg.Add(1)
		go func() {
			defer wg.Done()
			series, err := client.QueryRange(ctx, query, window, step)
			if err != nil || len(series) == 0 {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if out.Sparks == nil {
				out.Sparks = map[string][]Point{}
			}
			out.Sparks[key] = series[0].Points
		}()
	}
	named(queryNodeDisk, "nodename", func(s []NamedSeries) { out.DiskByNode = s })
	named(queryNodeNetworkRx, "nodename", func(s []NamedSeries) { out.NetworkRx = s })
	named(queryNodeNetworkTx, "nodename", func(s []NamedSeries) { out.NetworkTx = s })

	wg.Wait()
	return out
}

// ContainerRisk is a container that is not failing yet but is set up to.
//
// Separate from Finding because nothing is wrong right now: a container with no memory
// limit is running fine until the day it takes the node down with it, and mixing that
// into the problems table would bury the pods that are actually broken.
type ContainerRisk struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	// Kind is "throttled", "no-requests" or "no-limits".
	Kind string `json:"kind"`
	// Value carries the throttle percentage where there is one.
	Value float64 `json:"value,omitempty"`
}

// ExitCode is a container that stopped with something other than success.
type ExitCode struct {
	Namespace string  `json:"namespace"`
	Pod       string  `json:"pod"`
	Container string  `json:"container"`
	Code      float64 `json:"code"`
	Reason    string  `json:"reason,omitempty"`
}

// ScaleRow is an HPA and whether it has run out of room.
type ScaleRow struct {
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"`
	Current   float64 `json:"current"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	AtCeiling bool    `json:"atCeiling"`
}

// ServiceGap is a Service nothing answers on.
type ServiceGap struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ScrapeTarget is one Prometheus target that is not answering.
type ScrapeTarget struct {
	Job      string `json:"job"`
	Instance string `json:"instance"`
}

// Extras is everything the overview needs that does not belong to one of the other shapes.
type Extras struct {
	Risks         []ContainerRisk `json:"risks"`
	ExitCodes     []ExitCode      `json:"exitCodes"`
	Scalers       []ScaleRow      `json:"scalers"`
	ScalersKnown  bool            `json:"scalersKnown"`
	ServiceGaps   []ServiceGap    `json:"serviceGaps"`
	ServicesKnown bool            `json:"servicesKnown"`
	DownTargets   []ScrapeTarget  `json:"downTargets"`
	// ContainersReady and ContainersTotal answer "Running" separately from "working".
	ContainersReady Reading `json:"containersReady"`
	ContainersTotal Reading `json:"containersTotal"`
	Restarts15m     Reading `json:"restarts15m"`
	Restarts24h     Reading `json:"restarts24h"`
	// AppErrorRate is N/A unless something in the cluster publishes HTTP metrics. A
	// cluster with no instrumented application has no error rate — not a rate of zero.
	AppErrorRate Reading `json:"appErrorRate"`
	// LateCronJobs counts schedules that have not run when they should have.
	LateCronJobs Reading `json:"lateCronJobs"`
	// StalledRollouts is a Deployment that has been progressing for too long.
	StalledRollouts []Finding `json:"stalledRollouts"`
}

// readExtras fills the panels that did not fit the other shapes.
func readExtras(ctx context.Context, client *Client) Extras {
	var out Extras
	var mu sync.Mutex
	var wg sync.WaitGroup

	run := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}

	probe := func(presence, value string, assign func(Reading)) {
		run(func() {
			if !exists(ctx, client, presence) {
				return
			}
			samples, err := client.Query(ctx, value)
			if err != nil || len(samples) == 0 {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			assign(known(samples[0].Value))
		})
	}

	probe("kube_pod_container_status_ready",
		`count(kube_pod_container_status_ready == 1) or vector(0)`,
		func(r Reading) { out.ContainersReady = r })
	probe("kube_pod_container_status_ready",
		`count(kube_pod_container_status_ready) or vector(0)`,
		func(r Reading) { out.ContainersTotal = r })
	probe("kube_pod_container_status_restarts_total",
		`sum(increase(kube_pod_container_status_restarts_total[15m])) or vector(0)`,
		func(r Reading) { out.Restarts15m = counted(r.Value) })
	probe("kube_pod_container_status_restarts_total",
		`sum(increase(kube_pod_container_status_restarts_total[24h])) or vector(0)`,
		func(r Reading) { out.Restarts24h = counted(r.Value) })

	// Only meaningful if something is instrumented. Absent means "nobody is publishing an
	// error rate", which is a different sentence from "the error rate is zero".
	probe("http_requests_total",
		`100 * sum(rate(http_requests_total{code=~"5.."}[5m])) / sum(rate(http_requests_total[5m])) or vector(0)`,
		func(r Reading) { out.AppErrorRate = r })

	probe("kube_cronjob_status_last_schedule_time",
		`count((time() - kube_cronjob_status_last_schedule_time) > 3600) or vector(0)`,
		func(r Reading) { out.LateCronJobs = r })

	// Containers being throttled by their own CPU limit: the reason a service is slow
	// while every dashboard says the node is idle.
	run(func() {
		samples, err := client.Query(ctx,
			`100 * rate(container_cpu_cfs_throttled_periods_total{container!=""}[5m])
			   / rate(container_cpu_cfs_periods_total{container!=""}[5m]) > 1`)
		if err != nil {
			return
		}
		var risks []ContainerRisk
		for _, s := range samples {
			risks = append(risks, ContainerRisk{
				Namespace: s.Labels["namespace"], Pod: s.Labels["pod"],
				Container: s.Labels["container"], Kind: "throttled", Value: s.Value,
			})
		}
		mu.Lock()
		out.Risks = append(out.Risks, risks...)
		mu.Unlock()
	})

	// Containers with no request, and containers with no limit. The first makes the
	// scheduler guess; the second lets one pod take a node down.
	for _, want := range []struct {
		expr string
		kind string
	}{
		{`kube_pod_container_info unless on (namespace, pod, container) kube_pod_container_resource_requests{resource="cpu"}`, "no-cpu-requests"},
		{`kube_pod_container_info unless on (namespace, pod, container) kube_pod_container_resource_requests{resource="memory"}`, "no-requests"},
		{`kube_pod_container_info unless on (namespace, pod, container) kube_pod_container_resource_limits{resource="memory"}`, "no-limits"},
	} {
		expr, kind := want.expr, want.kind
		run(func() {
			samples, err := client.Query(ctx, expr)
			if err != nil {
				return
			}
			var risks []ContainerRisk
			for _, s := range samples {
				risks = append(risks, ContainerRisk{
					Namespace: s.Labels["namespace"], Pod: s.Labels["pod"],
					Container: s.Labels["container"], Kind: kind,
				})
			}
			mu.Lock()
			out.Risks = append(out.Risks, risks...)
			mu.Unlock()
		})
	}

	run(func() {
		samples, err := client.Query(ctx,
			`kube_pod_container_status_last_terminated_exitcode != 0`)
		if err != nil {
			return
		}
		var codes []ExitCode
		for _, s := range samples {
			codes = append(codes, ExitCode{
				Namespace: s.Labels["namespace"], Pod: s.Labels["pod"],
				Container: s.Labels["container"], Code: s.Value, Reason: s.Labels["reason"],
			})
		}
		sort.Slice(codes, func(i, j int) bool { return codes[i].Pod < codes[j].Pod })
		mu.Lock()
		out.ExitCodes = codes
		mu.Unlock()
	})

	run(func() {
		if !exists(ctx, client, "kube_horizontalpodautoscaler_spec_max_replicas") {
			return
		}
		mu.Lock()
		out.ScalersKnown = true
		mu.Unlock()

		rows := map[string]*ScaleRow{}
		gather := func(expr string, assign func(*ScaleRow, float64)) {
			samples, err := client.Query(ctx, expr)
			if err != nil {
				return
			}
			for _, s := range samples {
				name := s.Labels["horizontalpodautoscaler"]
				if name == "" {
					continue
				}
				key := s.Labels["namespace"] + "/" + name
				if _, ok := rows[key]; !ok {
					rows[key] = &ScaleRow{Namespace: s.Labels["namespace"], Name: name}
				}
				assign(rows[key], s.Value)
			}
		}
		gather(`kube_horizontalpodautoscaler_status_current_replicas`, func(r *ScaleRow, v float64) { r.Current = v })
		gather(`kube_horizontalpodautoscaler_spec_min_replicas`, func(r *ScaleRow, v float64) { r.Min = v })
		gather(`kube_horizontalpodautoscaler_spec_max_replicas`, func(r *ScaleRow, v float64) { r.Max = v })

		var list []ScaleRow
		for _, row := range rows {
			row.AtCeiling = row.Max > 0 && row.Current >= row.Max
			list = append(list, *row)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].AtCeiling != list[j].AtCeiling {
				return list[i].AtCeiling
			}
			return list[i].Name < list[j].Name
		})
		mu.Lock()
		out.Scalers = list
		mu.Unlock()
	})

	// A Service with no endpoints answers nothing, and nothing else reports it.
	run(func() {
		if !exists(ctx, client, "kube_endpointslice_endpoints") {
			return
		}
		mu.Lock()
		out.ServicesKnown = true
		mu.Unlock()

		samples, err := client.Query(ctx, `sum by (namespace, service) (kube_endpointslice_endpoints) == 0`)
		if err != nil {
			return
		}
		var gaps []ServiceGap
		for _, s := range samples {
			if name := s.Labels["service"]; name != "" {
				gaps = append(gaps, ServiceGap{Namespace: s.Labels["namespace"], Name: name})
			}
		}
		sort.Slice(gaps, func(i, j int) bool { return gaps[i].Namespace+gaps[i].Name < gaps[j].Namespace+gaps[j].Name })
		mu.Lock()
		out.ServiceGaps = gaps
		mu.Unlock()
	})

	run(func() {
		samples, err := client.Query(ctx, `up == 0`)
		if err != nil {
			return
		}
		var down []ScrapeTarget
		for _, s := range samples {
			down = append(down, ScrapeTarget{Job: s.Labels["job"], Instance: s.Labels["instance"]})
		}
		mu.Lock()
		out.DownTargets = down
		mu.Unlock()
	})

	// A rollout that has been "progressing" for more than a quarter of an hour is stuck,
	// whatever the controller still calls it.
	run(func() {
		samples, err := client.Query(ctx,
			`kube_deployment_status_condition{condition="Progressing",status="false"} == 1`)
		if err != nil {
			return
		}
		var stalled []Finding
		for _, s := range samples {
			stalled = append(stalled, Finding{
				Kind: "Deployment", Namespace: s.Labels["namespace"], Name: s.Labels["deployment"],
				Reason: "RolloutStalled", Detail: "not progressing", Severity: "warn",
			})
		}
		mu.Lock()
		out.StalledRollouts = stalled
		mu.Unlock()
	})

	wg.Wait()

	sort.Slice(out.Risks, func(i, j int) bool {
		if out.Risks[i].Kind != out.Risks[j].Kind {
			return out.Risks[i].Kind < out.Risks[j].Kind
		}
		if out.Risks[i].Value != out.Risks[j].Value {
			return out.Risks[i].Value > out.Risks[j].Value
		}
		return out.Risks[i].Pod < out.Risks[j].Pod
	})
	return out
}

// PodProblem is one pod that is wrong, with everything the table shows about it.
//
// Richer than Finding because the problem table answers a second question beside "what is
// broken": whether the pod was starved or over-limit. A CrashLoop at 620m used against a
// 500m request reads differently from the same CrashLoop at 24m — the first is a pod
// fighting for CPU, the second is an application falling over on its own.
type PodProblem struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	Node      string `json:"node,omitempty"`
	// Status is the short label: CrashLoopBackOff, Pending, OOMKilled, NotReady.
	Status string `json:"status"`
	// Reason is the sentence under it — the kubelet's own words where there are any.
	Reason   string  `json:"reason,omitempty"`
	Severity string  `json:"severity"`
	Restarts float64 `json:"restarts"`
	AgeSecs  float64 `json:"ageSeconds"`

	// Usage is absent for a pod that never started, which is why these are Readings: a
	// Pending pod has requests and limits but no usage, and zero would be a lie.
	CPUUsed       Reading `json:"cpuUsed"`
	CPURequest    Reading `json:"cpuRequest"`
	CPULimit      Reading `json:"cpuLimit"`
	MemoryUsed    Reading `json:"memoryUsed"`
	MemoryRequest Reading `json:"memoryRequest"`
	MemoryLimit   Reading `json:"memoryLimit"`
}

// StorageProblem is one claim or volume that is not doing its job.
type StorageProblem struct {
	Namespace     string  `json:"namespace"`
	Name          string  `json:"name"`
	Phase         string  `json:"phase"`
	StorageClass  string  `json:"storageClass,omitempty"`
	CapacityBytes Reading `json:"capacityBytes"`
	UsedBytes     Reading `json:"usedBytes"`
	Severity      string  `json:"severity"`
}

// readPodProblems builds the problem table.
func readPodProblems(ctx context.Context, client *Client) []PodProblem {
	type key struct{ namespace, pod string }

	var mu sync.Mutex
	found := map[key]*PodProblem{}

	at := func(namespace, pod string) *PodProblem {
		if namespace == "" || pod == "" {
			return nil
		}
		k := key{namespace, pod}
		if _, ok := found[k]; !ok {
			found[k] = &PodProblem{Namespace: namespace, Pod: pod}
		}
		return found[k]
	}

	// The problems themselves, most specific first: a pod that is CrashLooping is
	// described by that, not by the NotReady it also is.
	for _, source := range []struct {
		expr     string
		status   func(Sample) string
		reason   func(Sample) string
		severity string
		// exit marks the source that explains a row rather than competing with it: the
		// reason a container last died is why it is in backoff, so it is added to a row
		// that already exists instead of being dropped by first-writer-wins.
		exit bool
	}{
		{
			expr:     `kube_pod_container_status_waiting_reason > 0`,
			status:   func(s Sample) string { return s.Labels["reason"] },
			reason:   func(Sample) string { return "container will not start" },
			severity: "error",
		},
		{
			expr:     `kube_pod_container_status_last_terminated_reason > 0`,
			status:   func(s Sample) string { return s.Labels["reason"] },
			reason:   func(Sample) string { return "last termination reason" },
			severity: "error",
			exit:     true,
		},
		{
			expr:     `kube_pod_status_phase{phase="Failed"} == 1`,
			status:   func(Sample) string { return "Failed" },
			reason:   func(Sample) string { return "gave up" },
			severity: "error",
		},
		{
			expr:     `kube_pod_status_phase{phase="Pending"} == 1`,
			status:   func(Sample) string { return "Pending" },
			reason:   func(Sample) string { return "not scheduled or not started" },
			severity: "warn",
		},
		{
			expr:     `kube_pod_status_ready{condition="true"} == 0`,
			status:   func(Sample) string { return "NotReady" },
			reason:   func(Sample) string { return "running but failing readiness" },
			severity: "warn",
		},
	} {
		samples, err := client.Query(ctx, source.expr)
		if err != nil {
			continue
		}
		for _, sample := range samples {
			status := source.status(sample)
			if status == "" || status == "ContainerCreating" || status == "PodInitializing" ||
				status == "Completed" {
				continue
			}
			problem := at(sample.Labels["namespace"], sample.Labels["pod"])
			if problem == nil {
				continue
			}
			// First writer wins, and the list above is ordered by how much the label
			// tells the reader.
			if problem.Status == "" {
				problem.Status = status
				problem.Reason = source.reason(sample)
				problem.Severity = source.severity
				problem.Container = sample.Labels["container"]
				continue
			}
			// Why it died, on the row that says it will not start. This is the line
			// that saves opening the pod to find out.
			if source.exit && problem.Container == sample.Labels["container"] &&
				!strings.Contains(problem.Reason, "last exit") {
				problem.Reason += " · last exit: " + status
			}
		}
	}

	if len(found) == 0 {
		return nil
	}

	// Everything else is a lookup keyed by the same pod. Run together: each is one query
	// over the whole cluster, and filtering per pod would be one query per row.
	var wg sync.WaitGroup
	fill := func(expr string, assign func(*PodProblem, float64)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			samples, err := client.Query(ctx, expr)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, sample := range samples {
				if problem := found[key{sample.Labels["namespace"], sample.Labels["pod"]}]; problem != nil {
					assign(problem, sample.Value)
				}
			}
		}()
	}

	fill(`sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!=""}[5m]))`,
		func(p *PodProblem, v float64) { p.CPUUsed = known(v) })
	fill(`sum by (namespace, pod) (container_memory_working_set_bytes{container!=""})`,
		func(p *PodProblem, v float64) { p.MemoryUsed = known(v) })
	fill(`sum by (namespace, pod) (kube_pod_container_resource_requests{resource="cpu"})`,
		func(p *PodProblem, v float64) { p.CPURequest = known(v) })
	fill(`sum by (namespace, pod) (kube_pod_container_resource_limits{resource="cpu"})`,
		func(p *PodProblem, v float64) { p.CPULimit = known(v) })
	fill(`sum by (namespace, pod) (kube_pod_container_resource_requests{resource="memory"})`,
		func(p *PodProblem, v float64) { p.MemoryRequest = known(v) })
	fill(`sum by (namespace, pod) (kube_pod_container_resource_limits{resource="memory"})`,
		func(p *PodProblem, v float64) { p.MemoryLimit = known(v) })
	fill(`sum by (namespace, pod) (kube_pod_container_status_restarts_total)`,
		func(p *PodProblem, v float64) { p.Restarts = v })
	fill(`time() - kube_pod_start_time`,
		func(p *PodProblem, v float64) { p.AgeSecs = v })

	wg.Add(1)
	go func() {
		defer wg.Done()
		samples, err := client.Query(ctx, `kube_pod_info`)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, sample := range samples {
			if problem := found[key{sample.Labels["namespace"], sample.Labels["pod"]}]; problem != nil {
				node := sample.Labels["node"]
				// kube-state-metrics reports a pod that never landed as node="unknown";
				// "unscheduled" is what that actually means to a reader.
				if node == "" || node == "unknown" {
					node = "unscheduled"
				}
				problem.Node = node
			}
		}
	}()

	wg.Wait()

	out := make([]PodProblem, 0, len(found))
	for _, problem := range found {
		out = append(out, *problem)
	}

	// Most critical first, then the longest-standing: the top of this table is what
	// somebody acts on.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Severity == "error") != (out[j].Severity == "error") {
			return out[i].Severity == "error"
		}
		if out[i].Restarts != out[j].Restarts {
			return out[i].Restarts > out[j].Restarts
		}
		if out[i].AgeSecs != out[j].AgeSecs {
			return out[i].AgeSecs > out[j].AgeSecs
		}
		return out[i].Namespace+out[i].Pod < out[j].Namespace+out[j].Pod
	})
	return out
}

// readStorageProblems builds the storage table.
func readStorageProblems(ctx context.Context, client *Client) []StorageProblem {
	type key struct{ namespace, name string }
	found := map[key]*StorageProblem{}

	samples, err := client.Query(ctx, `kube_persistentvolumeclaim_status_phase == 1`)
	if err != nil {
		return nil
	}
	for _, sample := range samples {
		namespace, name := sample.Labels["namespace"], sample.Labels["persistentvolumeclaim"]
		if namespace == "" || name == "" {
			continue
		}
		phase := sample.Labels["phase"]
		if phase == "Bound" {
			continue
		}
		found[key{namespace, name}] = &StorageProblem{
			Namespace: namespace, Name: name, Phase: phase, Severity: "warn",
		}
	}
	if len(found) == 0 {
		return nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	fill := func(expr, nameLabel string, assign func(*StorageProblem, Sample)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := client.Query(ctx, expr)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, row := range rows {
				if problem := found[key{row.Labels["namespace"], row.Labels[nameLabel]}]; problem != nil {
					assign(problem, row)
				}
			}
		}()
	}

	fill(`kube_persistentvolumeclaim_resource_requests_storage_bytes`, "persistentvolumeclaim",
		func(p *StorageProblem, s Sample) { p.CapacityBytes = known(s.Value) })
	fill(`kube_persistentvolumeclaim_info`, "persistentvolumeclaim",
		func(p *StorageProblem, s Sample) { p.StorageClass = s.Labels["storageclass"] })
	fill(`kubelet_volume_stats_used_bytes`, "persistentvolumeclaim",
		func(p *StorageProblem, s Sample) { p.UsedBytes = known(s.Value) })

	wg.Wait()

	out := make([]StorageProblem, 0, len(found))
	for _, problem := range found {
		out = append(out, *problem)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Namespace+out[i].Name < out[j].Namespace+out[j].Name
	})
	return out
}
