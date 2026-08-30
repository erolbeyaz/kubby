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

	// The pods inside that Pending bucket that are held back by a container which will
	// not start, so the ring can name them instead of filing them under "pending".
	queryPodsNotStarting = `count(
		kube_pod_status_phase{phase="Pending"} == 1
		and on (namespace, pod) (` + stuckContainers + `)
	) or vector(0)`

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

	// Node usage as Kubernetes itself measures it, read from the kubelet's cAdvisor.
	//
	// Not node-exporter. That reports the host's own memory and CPU, which is right on a
	// bare-metal node and wrong wherever a "node" is a container: every k3d node in one
	// cluster reported the same 5.7GiB, because they share the machine's /proc. These
	// two agree with `kubectl top node`, with the scheduler, and with what a pod's
	// requests are compared against.
	queryNodeCPUCores    = `sum by (instance) (rate(container_cpu_usage_seconds_total{id="/"}[5m]))`
	queryNodeMemoryBytes = `sum by (instance) (container_memory_working_set_bytes{id="/"})`

	queryMemoryByNode = `100 * (
		1 - sum by (instance) (node_memory_MemAvailable_bytes)
		      / sum by (instance) (node_memory_MemTotal_bytes)
	) * on (instance) group_left(nodename) (node_uname_info)`

	// Who is using the cluster, in cores — the unit every panel reading this prints, and
	// the one the cluster's own capacity is quoted in. Millicores here read as 61 cores
	// on a 12-core cluster. Sub-core values are rendered as millicores in the UI.
	queryTopNamespaceCPU = `topk(8, sum by (namespace) (
		rate(container_cpu_usage_seconds_total{container!=""}[5m])
	))`

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

// ---- the node panel ----
//
// One card per node, so the question "what is this cluster made of, and which machine is
// the one under strain" is answerable without leaving the page.
//
// Everything here joins node-exporter (which knows the machine) to kube-state-metrics
// (which knows the Kubernetes object) on the node's name. node-exporter labels its series
// by `instance`, an address, so `node_uname_info` is used to turn that into the hostname
// the rest of the cluster calls it. Reporting the same three machines as names in one
// panel and as `10.0.0.4:9100` in the next is the kind of thing that makes a reader think
// they are looking at six.
const (
	// Cores and memory the machine has, as Kubernetes counts them.
	queryNodeCores  = `kube_node_status_capacity{resource="cpu"}`
	queryNodeMemory = `kube_node_status_capacity{resource="memory"}`
	// Pods it will accept, against how many it is holding.
	queryNodePodCapacity = `kube_node_status_capacity{resource="pods"}`
	queryNodePods        = `sum by (node) (kube_pod_info)`

	// What the pods on this node have *reserved*, against what the node can give out.
	//
	// `node!=""` is not a tidiness filter, it is the whole correctness of the number: an
	// unschedulable pod asking for 500 cores is still a request, and counting it reported
	// this cluster at 4706% committed. Only what actually landed on a node is a claim on
	// that node.
	queryNodeCPUCommitted = `sum by (node) (kube_pod_container_resource_requests{resource="cpu",node!=""})
		/ on (node) kube_node_status_allocatable{resource="cpu"} * 100`
	queryNodeMemoryCommitted = `sum by (node) (kube_pod_container_resource_requests{resource="memory",node!=""})
		/ on (node) kube_node_status_allocatable{resource="memory"} * 100`

	// Network, excluding the interfaces that only carry the cluster's own plumbing back
	// and forth: counting veth and CNI traffic would report every node as busy.
	queryNodeNetworkRx = `sum by (nodename) (
		rate(node_network_receive_bytes_total{device!~"lo|veth.*|cni.*|flannel.*|docker.*|br.*|cali.*"}[5m])
		* on (instance) group_left(nodename) node_uname_info)`
	queryNodeNetworkTx = `sum by (nodename) (
		rate(node_network_transmit_bytes_total{device!~"lo|veth.*|cni.*|flannel.*|docker.*|br.*|cali.*"}[5m])
		* on (instance) group_left(nodename) node_uname_info)`

	// Load per core rather than raw load: 8 means nothing until you know whether the
	// machine has four cores or sixty-four.
	queryNodeLoad = `sum by (nodename) (
		node_load1 / on (instance) group_left count by (instance) (node_cpu_seconds_total{mode="idle"})
		* on (instance) group_left(nodename) node_uname_info)`

	queryNodeUptime = `sum by (nodename) (
		(time() - node_boot_time_seconds) * on (instance) group_left(nodename) node_uname_info)`

	// The fullest real filesystem, named the same way as everything else on the card.
	queryNodeDisk = `max by (nodename) (
		(1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|ramfs|squashfs"}
		   / node_filesystem_size_bytes) * 100
		* on (instance) group_left(nodename) node_uname_info)`

	// Identity: what it runs and whether Kubernetes considers it usable.
	queryNodeInfo  = `kube_node_info`
	queryNodeReady = `kube_node_status_condition{condition="Ready",status="true"} > 0`
	// Only control-plane nodes carry a role; the absence of one is what makes a worker.
	queryNodeRole = `kube_node_role`
	queryNodeArch = `node_uname_info`

	// ---- what is wrong, with the pods behind it ----
	//
	// The same two questions the rings already answer, but naming the pods so the reader
	// can go straight to one instead of going to look for it.
	queryStuckContainers = `kube_pod_container_status_waiting_reason > 0`
	queryDiedContainers  = `kube_pod_container_status_last_terminated_reason > 0`
)

// The rest of what a machine can be wrong about.
//
// CPU and memory percentages miss most of these: a node with free memory and no inodes
// left is out of disk, a node whose exporter died looks idle, and a node with a drifting
// clock fails certificates quietly. They are cheap to ask for and expensive to be without.
const (
	queryNodeSwap = `sum by (nodename) (
		(1 - node_memory_SwapFree_bytes / (node_memory_SwapTotal_bytes > 0)) * 100
		* on (instance) group_left(nodename) node_uname_info)`
	queryNodeSwapTotal = `sum by (nodename) (
		node_memory_SwapTotal_bytes * on (instance) group_left(nodename) node_uname_info)`

	// Inodes, not bytes: a filesystem can be 20% full and completely unwritable.
	queryNodeInodes = `max by (nodename) (
		(1 - node_filesystem_files_free{fstype!~"tmpfs|overlay|ramfs|squashfs"}
		   / node_filesystem_files) * 100
		* on (instance) group_left(nodename) node_uname_info)`

	queryNodeDiskRead = `sum by (nodename) (
		rate(node_disk_read_bytes_total[5m]) * on (instance) group_left(nodename) node_uname_info)`
	queryNodeDiskWrite = `sum by (nodename) (
		rate(node_disk_written_bytes_total[5m]) * on (instance) group_left(nodename) node_uname_info)`
	// Time the disk spent with a request in flight: the number that explains a slow node
	// when CPU and memory both look fine.
	queryNodeDiskBusy = `max by (nodename) (
		rate(node_disk_io_time_seconds_total[5m]) * 100
		* on (instance) group_left(nodename) node_uname_info)`
	queryNodeIOWait = `sum by (nodename) (
		rate(node_cpu_seconds_total{mode="iowait"}[5m]) * 100
		* on (instance) group_left(nodename) node_uname_info)`

	queryNodeRxErrors = `sum by (nodename) (
		rate(node_network_receive_errs_total[5m])
		* on (instance) group_left(nodename) node_uname_info)`
	queryNodeTxErrors = `sum by (nodename) (
		rate(node_network_transmit_errs_total[5m])
		* on (instance) group_left(nodename) node_uname_info)`
	queryNodeDrops = `sum by (nodename) (
		(rate(node_network_receive_drop_total[5m]) + rate(node_network_transmit_drop_total[5m]))
		* on (instance) group_left(nodename) node_uname_info)`

	queryNodeClockSkew = `sum by (nodename) (
		node_timex_offset_seconds * on (instance) group_left(nodename) node_uname_info)`
	queryNodeBootTime = `sum by (nodename) (
		node_boot_time_seconds * on (instance) group_left(nodename) node_uname_info)`

	// Whether the two sources this row depends on are answering at all.
	//
	// Matched by target rather than by job name: every Prometheus install names its jobs
	// differently — this cluster calls node-exporter `kubernetes-service-endpoints` and
	// the kubelet `kubernetes-nodes` — and a regex over job names is a guess that fails
	// silently, reporting a healthy exporter as down.
	//
	// node-exporter is whatever `up` series shares an instance with node_uname_info; the
	// kubelet is whatever `up` series is labelled with the node's own name.
	queryNodeExporterUp = `(up * on (instance) group_left(nodename) node_uname_info) == 1`
	queryKubeletUp      = `up == 1`

	// Limits, alongside requests: a node can be under its requests and still have every
	// container on it throttled by its own limit.
	queryNodeCPULimits = `sum by (node) (kube_pod_container_resource_limits{resource="cpu",node!=""})
		/ on (node) kube_node_status_allocatable{resource="cpu"} * 100`
	queryNodeMemoryLimits = `sum by (node) (kube_pod_container_resource_limits{resource="memory",node!=""})
		/ on (node) kube_node_status_allocatable{resource="memory"} * 100`

	queryNodeCPUAllocatable    = `kube_node_status_allocatable{resource="cpu"}`
	queryNodeMemoryAllocatable = `kube_node_status_allocatable{resource="memory"}`
)
