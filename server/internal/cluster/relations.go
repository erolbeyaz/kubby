package cluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/erolbeyaz/kubby/internal/store"
)

// Relation is one object connected to another, and how.
type Relation struct {
	// Direction is "owner" for what created this, "owned" for what it created, and
	// "serves" for a connection that is not ownership at all.
	Direction string `json:"direction"`
	Kind      string `json:"kind"`
	TypeKey   string `json:"typeKey"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	// Detail says what the connection is, where the direction alone does not.
	Detail   string `json:"detail,omitempty"`
	Severity string `json:"severity,omitempty"`
}

// Relations answers the first question asked of a misbehaving object: what is it part of.
//
// Kubernetes stores this as ownerReferences pointing upward and label selectors pointing
// down, so following it in either direction means two different lookups. Doing that by
// hand — read the pod, find the ReplicaSet, find the Deployment — is the round trip this
// removes.
func (s *Service) Relations(ctx context.Context, cluster *store.Cluster, resourceType ResourceType, namespace, name string, impersonate *ImpersonationConfig) ([]Relation, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	object, err := client.Resource(resourceType.GVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, resourceType)
	}

	var out []Relation
	out = append(out, ownerChain(ctx, s, cluster, object, namespace, impersonate)...)

	switch resourceType.Kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
		out = append(out, selectedPods(ctx, s, cluster, object, namespace, impersonate)...)
	case "Service":
		out = append(out, servedPods(ctx, s, cluster, object, namespace, impersonate)...)
	}
	return out, nil
}

// ownerChain walks upward until nothing owns the object, so a pod reports its ReplicaSet
// and the Deployment behind it rather than only the first step.
func ownerChain(ctx context.Context, s *Service, cluster *store.Cluster, object *unstructured.Unstructured, namespace string, impersonate *ImpersonationConfig) []Relation {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil
	}

	var out []Relation
	current := object

	// Bounded: an ownership cycle is invalid but a tool should not hang on one.
	for depth := 0; depth < 6; depth++ {
		name, kind := ownerOf(current)
		if name == "" {
			return out
		}

		typeKey := typeKeyForKind(kind)
		out = append(out, Relation{
			Direction: "owner", Kind: kind, TypeKey: typeKey, Namespace: namespace, Name: name,
		})
		if typeKey == "" {
			return out
		}

		ownerType, err := LookupType(typeKey)
		if err != nil {
			return out
		}
		next, err := client.Resource(ownerType.GVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return out
		}
		current = next
	}
	return out
}

func selectedPods(ctx context.Context, s *Service, cluster *store.Cluster, object *unstructured.Unstructured, namespace string, impersonate *ImpersonationConfig) []Relation {
	selector, found, err := unstructured.NestedStringMap(object.Object, "spec", "selector", "matchLabels")
	if err != nil || !found || len(selector) == 0 {
		return nil
	}
	return podsMatching(ctx, s, cluster, namespace, selector, "owned", impersonate)
}

// servedPods follows a Service to what answers it. A Service with a selector that matches
// nothing is a common and quiet outage, and this is where it becomes visible.
func servedPods(ctx context.Context, s *Service, cluster *store.Cluster, object *unstructured.Unstructured, namespace string, impersonate *ImpersonationConfig) []Relation {
	selector, found, err := unstructured.NestedStringMap(object.Object, "spec", "selector")
	if err != nil || !found || len(selector) == 0 {
		return nil
	}

	pods := podsMatching(ctx, s, cluster, namespace, selector, "serves", impersonate)
	if len(pods) == 0 {
		return []Relation{{
			Direction: "serves",
			Kind:      "Pod",
			TypeKey:   "pods",
			Namespace: namespace,
			Name:      "no pods match this selector",
			Detail:    fmt.Sprintf("selector %s matches nothing", labels.Set(selector).String()),
			Severity:  SeverityError,
		}}
	}
	return pods
}

func podsMatching(ctx context.Context, s *Service, cluster *store.Cluster, namespace string, selector map[string]string, direction string, impersonate *ImpersonationConfig) []Relation {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil
	}

	podType, err := LookupType("pods")
	if err != nil {
		return nil
	}

	pods, err := client.Resource(podType.GVR()).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(selector).String(),
	})
	if err != nil {
		return nil
	}

	out := make([]Relation, 0, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		relation := Relation{
			Direction: direction, Kind: "Pod", TypeKey: "pods",
			Namespace: pod.GetNamespace(), Name: pod.GetName(),
		}

		fields, severity := projectPod(pod)
		relation.Severity = severity
		relation.Detail = fields["status"]
		out = append(out, relation)
	}
	return out
}

// typeKeyForKind maps a kind onto the registry key its URL uses. An owner Kubby does not
// know is still reported, just not linkable.
func typeKeyForKind(kind string) string {
	for _, t := range builtinTypes {
		if t.Kind == kind {
			return t.Key()
		}
	}
	return ""
}
