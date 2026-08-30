package promql

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PodUsage is one pod's own history, in the units its requests are written in.
//
// Read on demand rather than folded into the cluster-wide payload: a fleet has thousands
// of pods and nobody looks at more than one at a time, so this is two queries when a
// panel is opened instead of two thousand series on every refresh.
type PodUsage struct {
	CPUCores    []Point `json:"cpuCores"`
	MemoryBytes []Point `json:"memoryBytes"`
	// Containers carries the same two per container, for a pod that runs more than one.
	Containers map[string]ContainerUsage `json:"containers,omitempty"`
}

// ContainerUsage is one container inside the pod.
type ContainerUsage struct {
	CPUCores    []Point `json:"cpuCores"`
	MemoryBytes []Point `json:"memoryBytes"`
}

// PodUsageOver reads what a pod has been using.
//
// The `container!=""` filter drops the pause container's totals, which cAdvisor also
// publishes under the pod with an empty container label — counting both doubles every
// number.
func (c *Client) PodUsageOver(ctx context.Context, namespace, pod string, window time.Duration) (*PodUsage, error) {
	if err := labelSafe(namespace); err != nil {
		return nil, fmt.Errorf("namespace: %w", err)
	}
	if err := labelSafe(pod); err != nil {
		return nil, fmt.Errorf("pod: %w", err)
	}

	selector := fmt.Sprintf(`namespace=%q,pod=%q,container!=""`, namespace, pod)
	step := stepFor(window)

	usage := &PodUsage{Containers: map[string]ContainerUsage{}}

	if series, err := c.QueryRange(ctx,
		fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[5m]))`, selector), window, step); err == nil {
		usage.CPUCores = firstSeries(series)
	}
	if series, err := c.QueryRange(ctx,
		fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, selector), window, step); err == nil {
		usage.MemoryBytes = firstSeries(series)
	}

	// Per container as well, because a pod with a sidecar in it answers "which of them"
	// and the total never does.
	cpu, _ := c.QueryRange(ctx,
		fmt.Sprintf(`sum by (container) (rate(container_cpu_usage_seconds_total{%s}[5m]))`, selector), window, step)
	memory, _ := c.QueryRange(ctx,
		fmt.Sprintf(`sum by (container) (container_memory_working_set_bytes{%s})`, selector), window, step)

	for _, series := range cpu {
		name := series.Labels["container"]
		if name == "" {
			continue
		}
		entry := usage.Containers[name]
		entry.CPUCores = series.Points
		usage.Containers[name] = entry
	}
	for _, series := range memory {
		name := series.Labels["container"]
		if name == "" {
			continue
		}
		entry := usage.Containers[name]
		entry.MemoryBytes = series.Points
		usage.Containers[name] = entry
	}

	return usage, nil
}

// labelSafe refuses anything that would end the quoted matcher it is going into.
//
// Kubernetes names cannot contain these, so a value that does is not a name — it is
// somebody trying to write their own query.
func labelSafe(value string) error {
	if value == "" {
		return fmt.Errorf("is required")
	}
	if strings.ContainsAny(value, `"\`+"\n{}") {
		return fmt.Errorf("%q is not a Kubernetes name", value)
	}
	return nil
}

func firstSeries(series []Series) []Point {
	if len(series) == 0 {
		return nil
	}
	return series[0].Points
}
