package promql

import (
	"context"
	"sort"
	"strings"
)

// NodeDetail is one machine, as both a computer and a Kubernetes object.
//
// The two views disagree in useful ways and the card shows both: `CPUPercent` is what the
// machine is actually doing, `CPUCommittedPercent` is what the pods on it reserved. A node
// can be 8% busy and 95% committed — nothing more will schedule onto it while it sits
// nearly idle — and that is invisible if only one of the two is shown.
type NodeDetail struct {
	Name string `json:"name"`
	// Role is "control-plane" or "worker". Only control-plane nodes are labelled as
	// anything, so a worker is what is left.
	Role  string `json:"role"`
	Ready bool   `json:"ready"`

	CPUPercent    float64 `json:"cpuPercent"`
	MemoryPercent float64 `json:"memoryPercent"`
	DiskPercent   float64 `json:"diskPercent"`

	Cores            float64 `json:"cores"`
	MemoryTotalBytes float64 `json:"memoryTotalBytes"`

	CPUCommittedPercent    float64 `json:"cpuCommittedPercent"`
	MemoryCommittedPercent float64 `json:"memoryCommittedPercent"`

	Pods        float64 `json:"pods"`
	PodCapacity float64 `json:"podCapacity"`

	NetworkRxBytes float64 `json:"networkRxBytes"`
	NetworkTxBytes float64 `json:"networkTxBytes"`
	LoadPerCore    float64 `json:"loadPerCore"`
	UptimeSeconds  float64 `json:"uptimeSeconds"`

	// Conditions Kubernetes raises about the machine. Each is true only when it is a
	// problem, so an empty list is a healthy node.
	MemoryPressure     bool `json:"memoryPressure"`
	DiskPressure       bool `json:"diskPressure"`
	PIDPressure        bool `json:"pidPressure"`
	NetworkUnavailable bool `json:"networkUnavailable"`
	Unschedulable      bool `json:"unschedulable"`

	// SwapPercent, InodePercent and the I/O figures are the ways a node fails that CPU
	// and memory percentages do not show. A node with free memory and 100% inodes is
	// out of disk in the only way that matters.
	SwapPercent    float64 `json:"swapPercent"`
	SwapTotal      float64 `json:"swapTotalBytes"`
	InodePercent   float64 `json:"inodePercent"`
	DiskReadBytes  float64 `json:"diskReadBytes"`
	DiskWriteBytes float64 `json:"diskWriteBytes"`
	// DiskBusyPercent is how much of the time the disk had a request in flight — the
	// number that explains a slow node when CPU and memory both look fine.
	DiskBusyPercent float64 `json:"diskBusyPercent"`
	IOWaitPercent   float64 `json:"ioWaitPercent"`

	NetworkRxErrors float64 `json:"networkRxErrors"`
	NetworkTxErrors float64 `json:"networkTxErrors"`
	NetworkDrops    float64 `json:"networkDrops"`

	// ClockSkewSeconds matters more than it looks: certificates and log ordering both
	// depend on it, and a drifting clock fails them quietly.
	ClockSkewSeconds float64 `json:"clockSkewSeconds"`
	// BootTime is when the machine last started, so an unannounced reboot is visible.
	BootTimeUnix float64 `json:"bootTimeUnix"`

	// Exporters says whether the two sources this row depends on are answering. Without
	// it a node whose exporter died looks like a node doing nothing.
	NodeExporterUp bool `json:"nodeExporterUp"`
	KubeletUp      bool `json:"kubeletUp"`

	CPULimitPercent    float64 `json:"cpuLimitPercent"`
	MemoryLimitPercent float64 `json:"memoryLimitPercent"`
	CPUAllocatable     float64 `json:"cpuAllocatable"`
	MemoryAllocatable  float64 `json:"memoryAllocatable"`

	KubeletVersion string `json:"kubeletVersion,omitempty"`
	OSImage        string `json:"osImage,omitempty"`
	Kernel         string `json:"kernel,omitempty"`
	Architecture   string `json:"architecture,omitempty"`
}

// Capacity is the cluster added up, for the row of tiles above the cards.
type Capacity struct {
	Nodes                  int     `json:"nodes"`
	Cores                  float64 `json:"cores"`
	MemoryBytes            float64 `json:"memoryBytes"`
	Pods                   float64 `json:"pods"`
	PodCapacity            float64 `json:"podCapacity"`
	CPUCommittedPercent    float64 `json:"cpuCommittedPercent"`
	MemoryCommittedPercent float64 `json:"memoryCommittedPercent"`
}

// ContainerIssue is one container and why it is not running, named well enough to open it.
type ContainerIssue struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Reason    string `json:"reason"`
}

// readNodes fills one card per node.
//
// Each query contributes what it knows and nothing insists on being present: a cluster
// without node-exporter still gets cards with the Kubernetes half filled in, which is more
// useful than no panel at all.
func readNodes(ctx context.Context, client *Client) []NodeDetail {
	nodes := map[string]*NodeDetail{}

	// Only a query that is *about nodes* may create a card. Everything else fills one in.
	//
	// Otherwise a label that merely happens to be called `node` invents machines: pods
	// that were never scheduled are reported by kube-state-metrics as living on a node
	// named "unknown", and this cluster grew a fourth node with no cores and two pods on
	// it. A card has to correspond to a machine somebody could ssh into.
	seed := func(expr string, nameLabels ...string) {
		samples, err := client.Query(ctx, expr)
		if err != nil {
			return
		}
		for _, sample := range samples {
			name := strings.TrimSpace(label(sample.Labels, nameLabels...))
			if name == "" {
				continue
			}
			if _, ok := nodes[name]; !ok {
				nodes[name] = &NodeDetail{Name: name}
			}
		}
	}

	// Both, because either can be the one that is missing: kube-state-metrics knows the
	// Kubernetes object, node-exporter knows the machine, and a cluster can be running
	// without one of them.
	seed(queryNodeInfo, "node")
	seed(queryNodeArch, "nodename")

	at := func(name string) *NodeDetail {
		return nodes[strings.TrimSpace(name)]
	}

	// gauge runs one query and assigns its value, ignoring a query that fails: a missing
	// exporter should cost one field, not the panel.
	gauge := func(expr string, assign func(*NodeDetail, float64)) {
		samples, err := client.Query(ctx, expr)
		if err != nil {
			return
		}
		for _, sample := range samples {
			if node := at(label(sample.Labels, "node", "nodename", "instance")); node != nil {
				assign(node, sample.Value)
			}
		}
	}

	gauge(queryCPUByNode, func(n *NodeDetail, v float64) { n.CPUPercent = v })
	gauge(queryMemoryByNode, func(n *NodeDetail, v float64) { n.MemoryPercent = v })
	gauge(queryNodeDisk, func(n *NodeDetail, v float64) { n.DiskPercent = v })
	gauge(queryNodeCores, func(n *NodeDetail, v float64) { n.Cores = v })
	gauge(queryNodeMemory, func(n *NodeDetail, v float64) { n.MemoryTotalBytes = v })
	gauge(queryNodeCPUCommitted, func(n *NodeDetail, v float64) { n.CPUCommittedPercent = v })
	gauge(queryNodeMemoryCommitted, func(n *NodeDetail, v float64) { n.MemoryCommittedPercent = v })
	gauge(queryNodePods, func(n *NodeDetail, v float64) { n.Pods = v })
	gauge(queryNodePodCapacity, func(n *NodeDetail, v float64) { n.PodCapacity = v })
	gauge(queryNodeNetworkRx, func(n *NodeDetail, v float64) { n.NetworkRxBytes = v })
	gauge(queryNodeNetworkTx, func(n *NodeDetail, v float64) { n.NetworkTxBytes = v })
	gauge(queryNodeLoad, func(n *NodeDetail, v float64) { n.LoadPerCore = v })
	gauge(queryNodeUptime, func(n *NodeDetail, v float64) { n.UptimeSeconds = v })
	gauge(queryNodeReady, func(n *NodeDetail, _ float64) { n.Ready = true })
	gauge(queryNodeSwap, func(n *NodeDetail, v float64) { n.SwapPercent = v })
	gauge(queryNodeSwapTotal, func(n *NodeDetail, v float64) { n.SwapTotal = v })
	gauge(queryNodeInodes, func(n *NodeDetail, v float64) { n.InodePercent = v })
	gauge(queryNodeDiskRead, func(n *NodeDetail, v float64) { n.DiskReadBytes = v })
	gauge(queryNodeDiskWrite, func(n *NodeDetail, v float64) { n.DiskWriteBytes = v })
	gauge(queryNodeDiskBusy, func(n *NodeDetail, v float64) { n.DiskBusyPercent = v })
	gauge(queryNodeIOWait, func(n *NodeDetail, v float64) { n.IOWaitPercent = v })
	gauge(queryNodeRxErrors, func(n *NodeDetail, v float64) { n.NetworkRxErrors = v })
	gauge(queryNodeTxErrors, func(n *NodeDetail, v float64) { n.NetworkTxErrors = v })
	gauge(queryNodeDrops, func(n *NodeDetail, v float64) { n.NetworkDrops = v })
	gauge(queryNodeClockSkew, func(n *NodeDetail, v float64) { n.ClockSkewSeconds = v })
	gauge(queryNodeBootTime, func(n *NodeDetail, v float64) { n.BootTimeUnix = v })
	gauge(queryNodeExporterUp, func(n *NodeDetail, _ float64) { n.NodeExporterUp = true })
	gauge(queryKubeletUp, func(n *NodeDetail, _ float64) { n.KubeletUp = true })
	gauge(queryNodeCPULimits, func(n *NodeDetail, v float64) { n.CPULimitPercent = v })
	gauge(queryNodeMemoryLimits, func(n *NodeDetail, v float64) { n.MemoryLimitPercent = v })
	gauge(queryNodeCPUAllocatable, func(n *NodeDetail, v float64) { n.CPUAllocatable = v })
	gauge(queryNodeMemoryAllocatable, func(n *NodeDetail, v float64) { n.MemoryAllocatable = v })
	gauge(`kube_node_spec_unschedulable == 1`, func(n *NodeDetail, _ float64) { n.Unschedulable = true })

	for condition, assign := range map[string]func(*NodeDetail){
		"MemoryPressure":     func(n *NodeDetail) { n.MemoryPressure = true },
		"DiskPressure":       func(n *NodeDetail) { n.DiskPressure = true },
		"PIDPressure":        func(n *NodeDetail) { n.PIDPressure = true },
		"NetworkUnavailable": func(n *NodeDetail) { n.NetworkUnavailable = true },
	} {
		set := assign
		gauge(`kube_node_status_condition{condition="`+condition+`",status="true"} == 1`,
			func(n *NodeDetail, _ float64) { set(n) })
	}

	if samples, err := client.Query(ctx, queryNodeInfo); err == nil {
		for _, sample := range samples {
			if node := at(sample.Labels["node"]); node != nil {
				node.KubeletVersion = sample.Labels["kubelet_version"]
				node.OSImage = sample.Labels["os_image"]
				node.Kernel = sample.Labels["kernel_version"]
			}
		}
	}
	if samples, err := client.Query(ctx, queryNodeArch); err == nil {
		for _, sample := range samples {
			if node := at(sample.Labels["nodename"]); node != nil {
				node.Architecture = sample.Labels["machine"]
			}
		}
	}
	if samples, err := client.Query(ctx, queryNodeRole); err == nil {
		for _, sample := range samples {
			if node := at(sample.Labels["node"]); node != nil {
				node.Role = sample.Labels["role"]
			}
		}
	}

	out := make([]NodeDetail, 0, len(nodes))
	for _, node := range nodes {
		if node.Role == "" {
			node.Role = "worker"
		}
		out = append(out, *node)
	}

	// Control-plane first, then by name. Stable, because a list of machines that
	// reorders itself every thirty seconds cannot be read — and the control plane is
	// the one whose trouble matters most.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Role == "control-plane") != (out[j].Role == "control-plane") {
			return out[i].Role == "control-plane"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// capacityOf adds the cards up rather than asking Prometheus again.
//
// The committed percentages are recomputed from the totals instead of averaging the
// per-node ones: the mean of three percentages is not the percentage of the whole unless
// the nodes are identical, and they usually are not.
func capacityOf(nodes []NodeDetail) Capacity {
	var out Capacity
	var cpuRequested, memRequested, cpuAllocatable, memAllocatable float64

	for _, node := range nodes {
		out.Nodes++
		out.Cores += node.Cores
		out.MemoryBytes += node.MemoryTotalBytes
		out.Pods += node.Pods
		out.PodCapacity += node.PodCapacity

		// Against allocatable, not capacity: the per-node percentages are a share of what
		// the scheduler may hand out, and reading them against what the machine
		// physically has mixes two bases. They are equal on a cluster that reserves
		// nothing, and quietly are not on one that does.
		cpu := node.CPUAllocatable
		if cpu == 0 {
			cpu = node.Cores
		}
		mem := node.MemoryAllocatable
		if mem == 0 {
			mem = node.MemoryTotalBytes
		}

		cpuRequested += node.CPUCommittedPercent / 100 * cpu
		cpuAllocatable += cpu
		memRequested += node.MemoryCommittedPercent / 100 * mem
		memAllocatable += mem
	}

	if cpuAllocatable > 0 {
		out.CPUCommittedPercent = cpuRequested / cpuAllocatable * 100
	}
	if memAllocatable > 0 {
		out.MemoryCommittedPercent = memRequested / memAllocatable * 100
	}
	return out
}

// containerIssues names the containers behind a reason, so a ring can be clicked through
// to the pod rather than only counted.
func containerIssues(ctx context.Context, client *Client, expr string) []ContainerIssue {
	samples, err := client.Query(ctx, expr)
	if err != nil {
		return nil
	}

	out := make([]ContainerIssue, 0, len(samples))
	for _, sample := range samples {
		issue := ContainerIssue{
			Namespace: sample.Labels["namespace"],
			Pod:       sample.Labels["pod"],
			Container: sample.Labels["container"],
			Reason:    sample.Labels["reason"],
		}
		if issue.Pod == "" || issue.Reason == "" {
			continue
		}
		out = append(out, issue)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Reason != out[j].Reason {
			return out[i].Reason < out[j].Reason
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Pod < out[j].Pod
	})
	return out
}
