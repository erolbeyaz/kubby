package cluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var (
	podMetricsGVR = schema.GroupVersionResource{
		Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods",
	}
	nodeMetricsGVR = schema.GroupVersionResource{
		Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes",
	}
)

// Usage is one object's measured resource consumption.
type Usage struct {
	CPUMilli  int64
	MemoryMiB int64
}

// FormatCPU renders CPU the way kubectl top does.
func (u Usage) FormatCPU() string { return fmt.Sprintf("%dm", u.CPUMilli) }

// FormatMemory renders memory in mebibytes.
func (u Usage) FormatMemory() string { return fmt.Sprintf("%dMi", u.MemoryMiB) }

// usageKey identifies a measurement: "namespace/name", or just "name" for nodes.
func usageKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// fetchUsage reads live measurements from metrics.k8s.io.
//
// It never fails a listing: metrics-server is optional and frequently absent or
// briefly unavailable, and a list of pods is still worth showing without usage
// figures (ADR-007). A nil map simply means the columns render as "—".
func fetchUsage(ctx context.Context, cfg *rest.Config, gvr schema.GroupVersionResource, namespace string) map[string]Usage {
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil
	}

	var api dynamic.ResourceInterface = client.Resource(gvr)
	if namespace != "" && gvr == podMetricsGVR {
		api = client.Resource(gvr).Namespace(namespace)
	}

	list, err := api.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	out := make(map[string]Usage, len(list.Items))
	for _, item := range list.Items {
		name := item.GetName()
		key := usageKey(item.GetNamespace(), name)

		if gvr == nodeMetricsGVR {
			usage, _, _ := unstructuredMap(item.Object, "usage")
			out[key] = parseUsage(usage)
			continue
		}

		// A pod's usage is the sum of its containers', which is what kubectl top
		// reports and what a list column should show.
		containers, _, _ := unstructuredSlice(item.Object, "containers")
		var total Usage
		for _, raw := range containers {
			container, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			usage, _, _ := unstructuredMap(container, "usage")
			measured := parseUsage(usage)
			total.CPUMilli += measured.CPUMilli
			total.MemoryMiB += measured.MemoryMiB
		}
		out[key] = total
	}
	return out
}

// parseUsage converts Kubernetes quantity strings into plain numbers.
func parseUsage(usage map[string]any) Usage {
	var out Usage

	if raw, ok := usage["cpu"].(string); ok {
		if quantity, err := resource.ParseQuantity(raw); err == nil {
			out.CPUMilli = quantity.MilliValue()
		}
	}
	if raw, ok := usage["memory"].(string); ok {
		if quantity, err := resource.ParseQuantity(raw); err == nil {
			out.MemoryMiB = quantity.Value() / (1024 * 1024)
		}
	}
	return out
}

// MetricsColumns are appended to kinds that carry usage, so the client renders them
// like any other column rather than special-casing metrics.
var MetricsColumns = []Column{
	{Key: "cpu", Label: "CPU", Mono: true},
	{Key: "memory", Label: "Memory", Mono: true},
}

// supportsUsage reports whether a kind has measurable usage.
func supportsUsage(kind string) bool {
	return kind == "Pod" || kind == "Node"
}

func metricsGVRFor(kind string) schema.GroupVersionResource {
	if kind == "Node" {
		return nodeMetricsGVR
	}
	return podMetricsGVR
}

func unstructuredMap(obj map[string]any, path ...string) (map[string]any, bool, error) {
	current := obj
	for i, key := range path {
		value, ok := current[key]
		if !ok {
			return nil, false, nil
		}
		if i == len(path)-1 {
			result, ok := value.(map[string]any)
			return result, ok, nil
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		current = next
	}
	return nil, false, nil
}

func unstructuredSlice(obj map[string]any, key string) ([]any, bool, error) {
	value, ok := obj[key]
	if !ok {
		return nil, false, nil
	}
	result, ok := value.([]any)
	return result, ok, nil
}
