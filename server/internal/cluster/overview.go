package cluster

import (
	"context"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/erolbeyaz/kubby/internal/store"
)

var (
	schemaNodes      = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
	schemaNamespaces = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	schemaPods       = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
)

// Gauge is one measurement against what was reserved and what exists.
//
// All five numbers are reported because they answer different questions: usage says
// what is happening now, requests say what the scheduler committed, limits say where
// the ceiling is, and capacity says what the hardware has.
type Gauge struct {
	Usage       float64 `json:"usage"`
	Requests    float64 `json:"requests"`
	Limits      float64 `json:"limits"`
	Allocatable float64 `json:"allocatable"`
	Capacity    float64 `json:"capacity"`
	Unit        string  `json:"unit"`
}

// Overview is the cluster at a glance.
type Overview struct {
	Nodes            int    `json:"nodes"`
	NodesReady       int    `json:"nodesReady"`
	Namespaces       int    `json:"namespaces"`
	MetricsAvailable bool   `json:"metricsAvailable"`
	K8sVersion       string `json:"k8sVersion"`

	CPU    Gauge `json:"cpu"`
	Memory Gauge `json:"memory"`
	Pods   Gauge `json:"pods"`

	// Problems is what would otherwise have to be hunted for. The full health panel
	// arrives in phase 5; this is the count that says whether to go looking.
	Problems []Problem `json:"problems"`
}

// Problem is one thing wrong in the cluster.
type Problem struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	Severity  string `json:"severity"`
}

// ClusterOverview gathers capacity, usage and obvious problems in one pass.
func (s *Service) ClusterOverview(ctx context.Context, cluster *store.Cluster, impersonate *ImpersonationConfig) (*Overview, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	out := &Overview{
		MetricsAvailable: cluster.MetricsAvailable,
		K8sVersion:       cluster.K8sVersion,
		CPU:              Gauge{Unit: "cores"},
		Memory:           Gauge{Unit: "MiB"},
		Pods:             Gauge{Unit: "pods"},
	}

	nodes, err := client.Resource(schemaNodes).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, translateAPIError(err, ResourceType{Kind: "Node", Resource: "nodes"})
	}
	out.Nodes = len(nodes.Items)

	for i := range nodes.Items {
		node := &nodes.Items[i]
		if nodeReady(node) {
			out.NodesReady++
		} else {
			out.Problems = append(out.Problems, Problem{
				Kind: "Node", Name: node.GetName(), Reason: "NotReady", Severity: SeverityError,
			})
		}

		capacity, _, _ := unstructured.NestedStringMap(node.Object, "status", "capacity")
		allocatable, _, _ := unstructured.NestedStringMap(node.Object, "status", "allocatable")

		out.CPU.Capacity += cores(capacity["cpu"])
		out.CPU.Allocatable += cores(allocatable["cpu"])
		out.Memory.Capacity += mib(capacity["memory"])
		out.Memory.Allocatable += mib(allocatable["memory"])
		out.Pods.Capacity += number(capacity["pods"])
		out.Pods.Allocatable += number(allocatable["pods"])
	}

	namespaces, err := client.Resource(schemaNamespaces).List(ctx, metav1.ListOptions{})
	if err == nil {
		out.Namespaces = len(namespaces.Items)
	}

	pods, err := client.Resource(schemaPods).List(ctx, metav1.ListOptions{})
	if err == nil {
		out.Pods.Usage = float64(len(pods.Items))

		for i := range pods.Items {
			pod := &pods.Items[i]
			accumulateRequests(pod, out)

			if reason, severity := podProblem(pod); reason != "" {
				out.Problems = append(out.Problems, Problem{
					Kind: "Pod", Namespace: pod.GetNamespace(), Name: pod.GetName(),
					Reason: reason, Severity: severity,
				})
			}
		}
	}

	if cluster.MetricsAvailable {
		if usage := fetchUsage(ctx, cfg, nodeMetricsGVR, ""); usage != nil {
			for _, measured := range usage {
				out.CPU.Usage += float64(measured.CPUMilli) / 1000
				out.Memory.Usage += float64(measured.MemoryMiB)
			}
		}
	}
	return out, nil
}

// accumulateRequests adds a pod's declared requests and limits to the cluster totals.
func accumulateRequests(pod *unstructured.Unstructured, out *Overview) {
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")

	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resources, _, _ := unstructured.NestedMap(container, "resources")

		requests, _ := resources["requests"].(map[string]any)
		limits, _ := resources["limits"].(map[string]any)

		out.CPU.Requests += cores(stringOf(requests, "cpu"))
		out.CPU.Limits += cores(stringOf(limits, "cpu"))
		out.Memory.Requests += mib(stringOf(requests, "memory"))
		out.Memory.Limits += mib(stringOf(limits, "memory"))
	}
}

// podProblem reports what is wrong with a pod, if anything. This is a preview of the
// health panel, restricted to states visible without extra API calls.
func podProblem(pod *unstructured.Unstructured) (reason, severity string) {
	phase := nestedString(pod, "status", "phase")

	switch phase {
	case "Failed":
		return "Failed", SeverityError
	case "Pending":
		return "Pending", SeverityWarning
	}

	statuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	for _, raw := range statuses {
		status, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		waiting, _ := status["state"].(map[string]any)
		if waiting == nil {
			continue
		}
		if state, ok := waiting["waiting"].(map[string]any); ok {
			if value, _ := state["reason"].(string); value != "" && value != "ContainerCreating" {
				return value, SeverityError
			}
		}
		if terminated, ok := waiting["terminated"].(map[string]any); ok {
			if value, _ := terminated["reason"].(string); value == "OOMKilled" {
				return value, SeverityError
			}
		}
	}
	return "", ""
}

func nodeReady(node *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(node.Object, "status", "conditions")

	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if conditionType, _ := condition["type"].(string); conditionType == "Ready" {
			value, _ := condition["status"].(string)
			return value == "True"
		}
	}
	return false
}

func stringOf(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func cores(value string) float64 {
	if value == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return 0
	}
	return float64(quantity.MilliValue()) / 1000
}

func mib(value string) float64 {
	if value == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return 0
	}
	return float64(quantity.Value()) / (1024 * 1024)
}

func number(value string) float64 {
	if value == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return 0
	}
	return float64(quantity.Value())
}
