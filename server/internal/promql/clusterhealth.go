package promql

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// ClusterHealth is what Prometheus can say that the Kubernetes API cannot.
//
// Everything here is about *time*. The cluster already tells Kubby what is true now — a
// pod's phase, a node's conditions, a restart count. What it cannot say is whether the
// restarts are speeding up, how long a workload has been short of replicas, or what the
// node was doing before anyone looked. That is the whole reason for this file.
type ClusterHealth struct {
	// CPU and Memory are what the nodes are actually doing, which is not what the pods
	// asked for. A cluster can be 90% requested and 10% used, or the reverse.
	CPU    []Point `json:"cpu"`
	Memory []Point `json:"memory"`
	// Disk is the fullest filesystem on each node. metrics-server reports no disk at all,
	// and a full disk is what takes a cluster down overnight.
	Disk []NodeGauge `json:"disk"`

	// Restarts is the count per hour across the cluster. The shape matters more than the
	// number: twelve in the last hour after none for a day is an incident.
	Restarts []Point `json:"restarts"`
	// Failing names the containers that keep dying and, crucially, why.
	Failing []FailingContainer `json:"failing"`
	// Degraded is a workload short of replicas, with how long it has been that way.
	Degraded []DegradedWorkload `json:"degraded"`
	// NodeIssues is a node condition that has been true long enough to matter.
	NodeIssues []NodeIssue `json:"nodeIssues"`

	// ---- the dashboard's own panels ----

	// Pods and Nodes are the headline numbers, big enough to read from across a desk.
	Pods  PodTotals  `json:"pods"`
	Nodes NodeTotals `json:"nodes"`
	// Restarts24h is one number for the day, separate from the shape above.
	Restarts24h float64 `json:"restarts24h"`

	// CPUByNode and MemoryByNode name every node, so a single hot machine is visible
	// rather than averaged away.
	CPUByNode    []NodeGauge `json:"cpuByNode"`
	MemoryByNode []NodeGauge `json:"memoryByNode"`

	// TopCPU and TopMemory are who is actually using the cluster.
	TopCPU    []NamedValue `json:"topCpu"`
	TopMemory []NamedValue `json:"topMemory"`

	// Reasons is why containers died; Waiting is why they are stuck now. Two different
	// questions, and a reader acts on them differently.
	Reasons []NamedValue `json:"reasons"`
	Waiting []NamedValue `json:"waiting"`

	// Summary is the top row of the Cluster Overview: the verdict and the ten numbers
	// behind it, each of which says whether it could be read at all.
	Summary ClusterSummary `json:"summary"`

	// Nodes is one card per machine, and Capacity is those cards added up.
	NodeDetails []NodeDetail `json:"nodeDetails"`
	Capacity    Capacity     `json:"capacity"`

	// Problems is every object that is wrong, in one list: the first question is "what is
	// broken", not "what kind of broken thing would I like to look at".
	Problems []Finding `json:"problems"`
	// PodProblems is the problem table: every pod that is wrong, with what it was using
	// against what it asked for.
	PodProblems []PodProblem `json:"podProblems"`
	// StorageProblems is the claims and volumes that are not doing their job.
	StorageProblems []StorageProblem `json:"storageProblems"`

	// Workloads is the replica tables — Deployment, StatefulSet, DaemonSet together.
	Workloads []WorkloadRow `json:"workloads"`
	// Alerts is what this cluster's own Prometheus is firing.
	Alerts []Alert `json:"alerts"`
	// ControlPlane is row 7, and is mostly N/A on clusters that do not scrape it.
	ControlPlane ControlPlane `json:"controlPlane"`
	// NamespaceUsage is who is actually consuming the cluster.
	NamespaceUsage []NamespaceUsage `json:"namespaceUsage"`
	// Spread is the same pods counted per node, so an uneven placement is visible.
	Spread []Spread `json:"spread"`
	// Trends carries the per-node lines that only mean something over time.
	Trends Trends `json:"trends"`
	// Extras is everything that did not fit the shapes above: throttling, exit codes,
	// autoscalers, services with nothing behind them, and the readings that only exist
	// when somebody instrumented for them.
	Extras Extras `json:"extras"`

	// Stuck and Died name the containers behind the two rings, so a reader can open one
	// instead of going to look for it.
	Stuck []ContainerIssue `json:"stuck"`
	Died  []ContainerIssue `json:"died"`

	// Window is the period the series cover.
	WindowMinutes int `json:"windowMinutes"`
	// Warnings names what could not be read, so a partial panel never passes as complete.
	Warnings []string `json:"warnings,omitempty"`
}

type NodeGauge struct {
	Node    string  `json:"node"`
	Percent float64 `json:"percent"`
}

// NamedValue is one slice of a ring or one bar in a row.
type NamedValue struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// PodTotals is the phase breakdown, which is the fastest read of whether anything is
// wrong: everything Running is a quiet cluster.
type PodTotals struct {
	Running float64 `json:"running"`
	// Pending is a pod waiting to be placed or still starting. A pod held back by a
	// container that will not start is in phase Pending too, and is counted in
	// NotStarting instead — under one name, on one row, so the tile and the table below
	// it cannot disagree about the same pod.
	Pending     float64 `json:"pending"`
	NotStarting float64 `json:"notStarting"`
	Failed      float64 `json:"failed"`
	Succeeded   float64 `json:"succeeded"`
	Unknown     float64 `json:"unknown"`
}

func (p PodTotals) Total() float64 {
	return p.Running + p.Pending + p.NotStarting + p.Failed + p.Succeeded + p.Unknown
}

type NodeTotals struct {
	Ready float64 `json:"ready"`
	Total float64 `json:"total"`
}

// FailingContainer is a container that has restarted recently, and the kubelet's own
// reason for the last one. "OOMKilled" and "Error" call for completely different work,
// and a restart count alone does not distinguish them.
type FailingContainer struct {
	Namespace string  `json:"namespace"`
	Pod       string  `json:"pod"`
	Container string  `json:"container"`
	Restarts  float64 `json:"restarts"`
	Reason    string  `json:"reason,omitempty"`
}

type DegradedWorkload struct {
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Missing   float64 `json:"missing"`
	// ForMinutes is how long it has been short. Thirty seconds is a rollout; forty
	// minutes is a problem nobody noticed.
	ForMinutes float64 `json:"forMinutes"`
}

type NodeIssue struct {
	Node      string  `json:"node"`
	Condition string  `json:"condition"`
	Minutes   float64 `json:"minutes"`
}

// ReadClusterHealth runs the panel's queries together.
//
// Concurrently and independently: one query failing — a metric a particular exporter does
// not publish, say — must leave the rest of the panel standing, because a panel that is
// all-or-nothing is a panel that is usually nothing.
func ReadClusterHealth(ctx context.Context, client *Client, window time.Duration) *ClusterHealth {
	step := stepFor(window)
	out := &ClusterHealth{WindowMinutes: int(window.Minutes())}

	var mu sync.Mutex
	warn := func(what string, err error) {
		mu.Lock()
		defer mu.Unlock()
		out.Warnings = append(out.Warnings, what+": "+err.Error())
	}

	var wg sync.WaitGroup
	run := func(what string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				warn(what, err)
			}
		}()
	}

	run("cpu", func() error {
		series, err := client.QueryRange(ctx, queryCPUPercent, window, step)
		if err != nil {
			return err
		}
		out.CPU = firstPoints(series)
		return nil
	})

	run("memory", func() error {
		series, err := client.QueryRange(ctx, queryMemoryPercent, window, step)
		if err != nil {
			return err
		}
		out.Memory = firstPoints(series)
		return nil
	})

	run("restarts", func() error {
		series, err := client.QueryRange(ctx, queryRestartRate, window, step)
		if err != nil {
			return err
		}
		out.Restarts = firstPoints(series)
		return nil
	})

	run("disk", func() error {
		samples, err := client.Query(ctx, queryDiskPercent)
		if err != nil {
			return err
		}
		for _, sample := range samples {
			out.Disk = append(out.Disk, NodeGauge{
				Node:    label(sample.Labels, "node", "instance"),
				Percent: sample.Value,
			})
		}
		sort.Slice(out.Disk, func(i, j int) bool { return out.Disk[i].Percent > out.Disk[j].Percent })
		return nil
	})

	run("failing containers", func() error {
		failing, err := readFailing(ctx, client, window)
		if err != nil {
			return err
		}
		out.Failing = failing
		return nil
	})

	run("degraded workloads", func() error {
		samples, err := client.Query(ctx, queryUnavailable)
		if err != nil {
			return err
		}
		durations, _ := client.Query(ctx, queryUnavailableFor)

		for _, sample := range samples {
			if sample.Value <= 0 {
				continue
			}
			namespace := sample.Labels["namespace"]
			name := sample.Labels["deployment"]
			out.Degraded = append(out.Degraded, DegradedWorkload{
				Namespace:  namespace,
				Name:       name,
				Kind:       "Deployment",
				Missing:    sample.Value,
				ForMinutes: matchDuration(durations, namespace, name),
			})
		}
		sort.Slice(out.Degraded, func(i, j int) bool {
			return out.Degraded[i].ForMinutes > out.Degraded[j].ForMinutes
		})
		return nil
	})

	run("node conditions", func() error {
		samples, err := client.Query(ctx, queryNodeConditions)
		if err != nil {
			return err
		}
		for _, sample := range samples {
			if sample.Value <= 0 {
				continue
			}
			out.NodeIssues = append(out.NodeIssues, NodeIssue{
				Node:      label(sample.Labels, "node", "instance"),
				Condition: sample.Labels["condition"],
				Minutes:   sample.Value / 60,
			})
		}
		return nil
	})

	run("pods", func() error {
		samples, err := client.Query(ctx, queryPodPhases)
		if err != nil {
			return err
		}
		// Split out of Pending rather than added to it: these pods were placed and then
		// stopped by their own containers, and the fix is an image or a config, not room
		// in the cluster.
		stuck, err := client.Query(ctx, queryPodsNotStarting)
		if err == nil {
			out.Pods.NotStarting = firstValue(stuck)
		}
		for _, sample := range samples {
			switch sample.Labels["phase"] {
			case "Running":
				out.Pods.Running = sample.Value
			case "Pending":
				out.Pods.Pending = sample.Value - out.Pods.NotStarting
				if out.Pods.Pending < 0 {
					out.Pods.Pending = 0
				}
			case "Failed":
				out.Pods.Failed = sample.Value
			case "Succeeded":
				out.Pods.Succeeded = sample.Value
			default:
				out.Pods.Unknown += sample.Value
			}
		}
		return nil
	})

	run("nodes", func() error {
		ready, err := client.Query(ctx, queryNodesReady)
		if err != nil {
			return err
		}
		total, err := client.Query(ctx, queryNodesTotal)
		if err != nil {
			return err
		}
		out.Nodes = NodeTotals{Ready: firstValue(ready), Total: firstValue(total)}
		return nil
	})

	run("restarts today", func() error {
		samples, err := client.Query(ctx, queryRestarts24h)
		if err != nil {
			return err
		}
		out.Restarts24h = math.Round(firstValue(samples))
		return nil
	})

	run("cpu by node", func() error {
		gauges, err := gaugesByNode(ctx, client, queryCPUByNode)
		if err != nil {
			return err
		}
		out.CPUByNode = gauges
		return nil
	})

	run("memory by node", func() error {
		gauges, err := gaugesByNode(ctx, client, queryMemoryByNode)
		if err != nil {
			return err
		}
		out.MemoryByNode = gauges
		return nil
	})

	run("top cpu", func() error {
		values, err := namedValues(ctx, client, queryTopNamespaceCPU, "namespace")
		if err != nil {
			return err
		}
		out.TopCPU = values
		return nil
	})

	run("top memory", func() error {
		values, err := namedValues(ctx, client, queryTopNamespaceMemory, "namespace")
		if err != nil {
			return err
		}
		out.TopMemory = values
		return nil
	})

	run("termination reasons", func() error {
		values, err := namedValues(ctx, client, queryReasonBreakdown, "reason")
		if err != nil {
			return err
		}
		out.Reasons = values
		return nil
	})

	run("problems", func() error {
		out.Problems = readProblems(ctx, client)
		return nil
	})

	run("pod problems", func() error {
		out.PodProblems = readPodProblems(ctx, client)
		return nil
	})

	run("storage problems", func() error {
		out.StorageProblems = readStorageProblems(ctx, client)
		return nil
	})

	run("workloads", func() error {
		out.Workloads = readWorkloads(ctx, client)
		return nil
	})

	run("alerts", func() error {
		out.Alerts = readAlerts(ctx, client)
		return nil
	})

	run("control plane", func() error {
		out.ControlPlane = readControlPlane(ctx, client)
		return nil
	})

	run("namespace usage", func() error {
		out.NamespaceUsage = readNamespaces(ctx, client)
		return nil
	})

	run("spread", func() error {
		out.Spread = readSpread(ctx, client)
		return nil
	})

	run("extras", func() error {
		out.Extras = readExtras(ctx, client)
		return nil
	})

	run("trends", func() error {
		out.Trends = readTrends(ctx, client, window, step)
		return nil
	})

	run("summary", func() error {
		out.Summary = readSummary(ctx, client)
		return nil
	})

	run("node detail", func() error {
		out.NodeDetails = readNodes(ctx, client)
		out.Capacity = capacityOf(out.NodeDetails)
		return nil
	})

	run("stuck containers", func() error {
		out.Stuck = containerIssues(ctx, client, queryStuckContainers)
		return nil
	})

	run("died containers", func() error {
		out.Died = containerIssues(ctx, client, queryDiedContainers)
		return nil
	})

	run("waiting reasons", func() error {
		values, err := namedValues(ctx, client, queryWaitingBreakdown, "reason")
		if err != nil {
			return err
		}
		out.Waiting = values
		return nil
	})

	wg.Wait()
	sort.Strings(out.Warnings)
	return out
}

func gaugesByNode(ctx context.Context, client *Client, expr string) ([]NodeGauge, error) {
	samples, err := client.Query(ctx, expr)
	if err != nil {
		return nil, err
	}

	out := make([]NodeGauge, 0, len(samples))
	for _, sample := range samples {
		out = append(out, NodeGauge{
			Node:    label(sample.Labels, "nodename", "node", "instance"),
			Percent: sample.Value,
		})
	}
	// By name rather than by value: a bar chart of nodes that reorders itself every
	// thirty seconds cannot be read.
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out, nil
}

func namedValues(ctx context.Context, client *Client, expr, labelName string) ([]NamedValue, error) {
	samples, err := client.Query(ctx, expr)
	if err != nil {
		return nil, err
	}

	out := make([]NamedValue, 0, len(samples))
	for _, sample := range samples {
		if sample.Value <= 0 {
			continue
		}
		out = append(out, NamedValue{Name: label(sample.Labels, labelName), Value: sample.Value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	return out, nil
}

func firstValue(samples []Sample) float64 {
	if len(samples) == 0 {
		return 0
	}
	return samples[0].Value
}

// readFailing joins two questions the panel asks as one: which containers restarted, and
// what the kubelet said about the last time each died.
func readFailing(ctx context.Context, client *Client, window time.Duration) ([]FailingContainer, error) {
	samples, err := client.Query(ctx, queryRestartsIn(window))
	if err != nil {
		return nil, err
	}

	// Best effort: a cluster where nothing has terminated yet publishes no reasons at
	// all, and that is not a failure worth reporting.
	reasons, _ := client.Query(ctx, queryTerminationReason)

	out := make([]FailingContainer, 0, len(samples))
	for _, sample := range samples {
		if sample.Value <= 0 {
			continue
		}
		entry := FailingContainer{
			Namespace: sample.Labels["namespace"],
			Pod:       sample.Labels["pod"],
			Container: sample.Labels["container"],
			Restarts:  sample.Value,
		}
		entry.Reason = matchReason(reasons, entry.Namespace, entry.Pod, entry.Container)
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Restarts > out[j].Restarts })
	if len(out) > maxFailing {
		out = out[:maxFailing]
	}
	return out, nil
}

func matchReason(reasons []Sample, namespace, pod, container string) string {
	for _, reason := range reasons {
		if reason.Value <= 0 {
			continue
		}
		if reason.Labels["namespace"] == namespace &&
			reason.Labels["pod"] == pod &&
			reason.Labels["container"] == container {
			return reason.Labels["reason"]
		}
	}
	return ""
}

func matchDuration(durations []Sample, namespace, name string) float64 {
	for _, duration := range durations {
		if duration.Labels["namespace"] == namespace && duration.Labels["deployment"] == name {
			return duration.Value / 60
		}
	}
	return 0
}

func firstPoints(series []Series) []Point {
	if len(series) == 0 {
		return nil
	}
	return series[0].Points
}

// label returns the first of several names a metric might carry the node under, because
// node-exporter labels by instance and kube-state-metrics by node.
func label(labels map[string]string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(labels[name]); value != "" {
			return value
		}
	}
	return "unknown"
}

// stepFor keeps a series to a few hundred points however long the window is: more than
// that is invisible on a sparkline and only costs Prometheus work.
func stepFor(window time.Duration) time.Duration {
	step := window / 120
	if step < 15*time.Second {
		return 15 * time.Second
	}
	return step.Round(15 * time.Second)
}

const maxFailing = 8
