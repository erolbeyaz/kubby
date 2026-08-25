package promql

import (
	"fmt"
	"time"
)

// The queries behind the cluster health panel.
//
// Kept together and named, rather than inline, because each one is a claim about what a
// number means and that claim is the part worth reviewing. They target what a plain
// `prometheus-community/prometheus` install provides — node-exporter, kube-state-metrics
// and the kubelet's own cAdvisor — so nothing here assumes an operator or a custom
// recording rule.

const (
	// CPU actually in use across the cluster, as a percentage of what the nodes have.
	//
	// Derived from idle time rather than from a "usage" metric, because that is what
	// node-exporter publishes: everything not idle is in use, including iowait, which a
	// sum of user+system would quietly drop.
	queryCPUPercent = `100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])))`

	// Memory in use, as node-exporter sees it.
	//
	// MemAvailable rather than MemFree: the kernel's page cache is free for the taking,
	// and counting it as used would show every healthy cluster at 95%.
	queryMemoryPercent = `100 * (1 - sum(node_memory_MemAvailable_bytes) / sum(node_memory_MemTotal_bytes))`

	// The fullest real filesystem on each node.
	//
	// tmpfs and overlay are excluded: they are memory and container layers, not the disk
	// that fills up at three in the morning.
	queryDiskPercent = `max by (instance) (
		100 * (1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|squashfs|ramfs"}
		         / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|squashfs|ramfs"})
	)`

	// Restarts per hour across the cluster.
	//
	// A rate rather than a total: the total only ever climbs, so it says nothing about
	// whether anything is wrong right now.
	queryRestartRate = `sum(rate(kube_pod_container_status_restarts_total[15m])) * 3600`

	// The kubelet's own reason for the last termination. This is the metric that turns
	// "restarting" into something actionable — OOMKilled is a limit, Error is the
	// application, ContainerCannotRun is the image.
	queryTerminationReason = `kube_pod_container_status_last_terminated_reason > 0`

	// Replicas a Deployment is short of, right now.
	queryUnavailable = `kube_deployment_status_replicas_unavailable > 0`

	// How long it has been short of them. A rollout is seconds; anything else is a
	// problem that has been sitting there.
	//
	// Measured as time since it was last *healthy*, not since it went unhealthy. The
	// obvious `timestamp(unavailable > 0)` is wrong and was: timestamp() reports the
	// sample's own evaluation time, so it always answers "just now" no matter how long
	// the workload has been degraded. The `and on(...)` keeps only what is broken right
	// now — without it every healthy deployment reports a duration too.
	// The `or` covers a deployment that has never been healthy at all — a bad image from
	// the first rollout has no earlier zero to measure from, and without the fallback the
	// most broken case of all would be the one that never appears. The outer parentheses
	// matter: `A or B and C` parses as `A or (B and C)`, which let every healthy
	// deployment through.
	queryUnavailableFor = `(
		(
			time() - max by (namespace, deployment) (
				last_over_time(timestamp(kube_deployment_status_replicas_unavailable == 0)[1d:1m])
			)
		)
		or
		(time() - max by (namespace, deployment) (kube_deployment_created))
	) and on (namespace, deployment) (
		max by (namespace, deployment) (kube_deployment_status_replicas_unavailable) > 0
	)`

	// A node condition that should be false and is not, and how long it has been.
	//
	// Ready is left out rather than inverted: a node that is not Ready is already the
	// loudest thing on the screen from the Kubernetes API itself, and the panel is here
	// for what that API cannot say. Same shape as above, and wrong the same way if
	// timestamp() is used directly.
	queryNodeConditions = `(
		(
			time() - max by (node, condition) (
				last_over_time(timestamp(kube_node_status_condition{condition!="Ready", status="true"} == 0)[1d:1m])
			)
		)
		or on (node, condition) (
			time() - max by (node, condition) (
				kube_node_status_condition{condition!="Ready", status="true"} * 0
				  + on (node) group_left() max by (node) (kube_node_created)
			)
		)
	) and on (node, condition) (
		max by (node, condition) (kube_node_status_condition{condition!="Ready", status="true"}) > 0
	)`

	// ---------------------------------------------------------------- dashboard

	// Pods by phase, for the ring that answers "is anything not Running" at a glance.
	queryPodPhases = `sum by (phase) (kube_pod_status_phase) > 0`

	// Nodes that report themselves Ready, against how many there are.
	queryNodesReady = `sum(kube_node_status_condition{condition="Ready", status="true"})`
	queryNodesTotal = `count(kube_node_info)`

	// Restarts over a day, as one number.
	queryRestarts24h = `sum(increase(kube_pod_container_status_restarts_total[24h]))`

	// Per node, named.
	//
	// node-exporter labels by `instance`, which is an address; the node's real name lives
	// in node_uname_info. Joining is the difference between a chart of IP addresses and a
	// chart someone can act on.
	queryCPUByNode = `100 * (1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])))
		* on (instance) group_left(nodename) (node_uname_info)`

	queryMemoryByNode = `100 * (
		1 - sum by (instance) (node_memory_MemAvailable_bytes)
		      / sum by (instance) (node_memory_MemTotal_bytes)
	) * on (instance) group_left(nodename) (node_uname_info)`

	// Who is using the cluster. In millicores rather than cores: a namespace using two
	// thousandths of a core reads as 2m, not as 0.00.
	queryTopNamespaceCPU = `topk(8, sum by (namespace) (
		rate(container_cpu_usage_seconds_total{container!=""}[5m])
	) * 1000)`

	queryTopNamespaceMemory = `topk(8, sum by (namespace) (
		container_memory_working_set_bytes{container!=""}
	))`

	// Why containers died, and why they are stuck. Two different questions: the first is
	// about something that already happened, the second about something still happening.
	queryReasonBreakdown  = `sum by (reason) (kube_pod_container_status_last_terminated_reason > 0)`
	queryWaitingBreakdown = `sum by (reason) (kube_pod_container_status_waiting_reason > 0)`
)

// queryRestartsIn counts restarts per container over the window the panel covers, rather
// than for all time: a container that crashed twice last week is not news.
func queryRestartsIn(window time.Duration) string {
	return fmt.Sprintf(
		`sum by (namespace, pod, container) (increase(kube_pod_container_status_restarts_total[%s]))`,
		promDuration(window),
	)
}

// promDuration writes a window the way PromQL wants it.
func promDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
