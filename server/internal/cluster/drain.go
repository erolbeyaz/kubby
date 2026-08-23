package cluster

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/erolbeyaz/kubby/internal/store"
)

// DrainPlan is what a drain would do, before it does any of it.
type DrainPlan struct {
	Node string `json:"node"`
	// Evict are the pods that will be asked to leave.
	Evict []DrainPod `json:"evict"`
	// Skip are the pods that stay, each with the reason it stays.
	Skip []DrainPod `json:"skip"`
}

// DrainPod is one pod a drain has an opinion about.
type DrainPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Owner     string `json:"owner,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// PlanDrain lists what a drain would move and what it would leave.
//
// A plan rather than a flag: draining a node is the most consequential thing in this
// phase, and the difference between "moves eleven pods" and "moves eleven pods and
// deletes the only copy of a database" is visible here and nowhere else.
func (s *Service) PlanDrain(ctx context.Context, cluster *store.Cluster, node string, impersonate *ImpersonationConfig) (*DrainPlan, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	podType, err := LookupType("pods")
	if err != nil {
		return nil, err
	}

	pods, err := client.Resource(podType.GVR()).Namespace("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node,
	})
	if err != nil {
		return nil, translateAPIError(err, ResourceType{Kind: "Pod", Resource: "pods"})
	}

	plan := &DrainPlan{Node: node}
	for i := range pods.Items {
		pod := &pods.Items[i]
		entry := DrainPod{Namespace: pod.GetNamespace(), Name: pod.GetName()}

		owner, ownerKind := ownerOf(pod)
		if owner != "" {
			entry.Owner = ownerKind + "/" + owner
		}

		if reason := whyItStays(pod, ownerKind); reason != "" {
			entry.Reason = reason
			plan.Skip = append(plan.Skip, entry)
			continue
		}

		// A pod with no controller is not coming back on its own, and saying so is the
		// difference between a drain and a deletion nobody meant.
		if owner == "" {
			entry.Reason = "Nothing will recreate it"
		}
		plan.Evict = append(plan.Evict, entry)
	}

	sort.Slice(plan.Evict, func(i, j int) bool { return plan.Evict[i].Name < plan.Evict[j].Name })
	sort.Slice(plan.Skip, func(i, j int) bool { return plan.Skip[i].Name < plan.Skip[j].Name })
	return plan, nil
}

// whyItStays reports why a pod is not evicted, or empty if it is.
func whyItStays(pod *unstructured.Unstructured, ownerKind string) string {
	// A DaemonSet immediately puts the pod back on the same node, so evicting it is a
	// restart dressed as a drain.
	if ownerKind == "DaemonSet" {
		return "Managed by a DaemonSet"
	}
	// A mirror pod is the kubelet's copy of a static manifest; the API server cannot
	// remove it and the kubelet would recreate it anyway.
	if _, mirror := pod.GetAnnotations()["kubernetes.io/config.mirror"]; mirror {
		return "Static pod, managed by the kubelet"
	}
	if phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase"); phase == "Succeeded" || phase == "Failed" {
		return "Already finished"
	}
	return ""
}

// Drain cordons the node and evicts what the plan said it would.
//
// Cordon first, always: evicting without cordoning lets the scheduler put the pods
// straight back where they came from.
func (s *Service) Drain(ctx context.Context, cluster *store.Cluster, node string, impersonate *ImpersonationConfig) ([]EvictResult, error) {
	if err := s.SetUnschedulable(ctx, cluster, node, true, impersonate); err != nil {
		return nil, fmt.Errorf("cordon %s: %w", node, err)
	}

	plan, err := s.PlanDrain(ctx, cluster, node, impersonate)
	if err != nil {
		return nil, err
	}

	results := make([]EvictResult, 0, len(plan.Evict))
	for _, pod := range plan.Evict {
		result := EvictResult{Namespace: pod.Namespace, Name: pod.Name}

		if err := s.Evict(ctx, cluster, pod.Namespace, pod.Name, impersonate); err != nil {
			// A budget that refuses is the system working. The drain reports it and
			// carries on, because stopping would leave the node half-drained with no
			// account of which half.
			result.Reason = err.Error()
		} else {
			result.Evicted = true
		}
		results = append(results, result)
	}
	return results, nil
}
