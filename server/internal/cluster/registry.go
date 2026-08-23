package cluster

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Category groups resource kinds the way the navigation presents them.
type Category string

const (
	CategoryWorkload      Category = "workload"
	CategoryConfig        Category = "config"
	CategoryNetwork       Category = "network"
	CategoryStorage       Category = "storage"
	CategoryAccessControl Category = "access"
	CategoryCluster       Category = "cluster"
	CategoryCustom        Category = "custom"
)

// ResourceType describes one kind Kubby can list.
type ResourceType struct {
	Group      string
	Version    string
	Resource   string // plural, as the API server names it
	Kind       string
	Namespaced bool
	Category   Category
	// DefaultSort is the column a list opens on when the reader has not chosen one.
	DefaultSort           string
	DefaultSortDescending bool
	// Order places the kind within its category. The navigation follows the shape of a
	// cluster — workloads before the config they read, config before the network in
	// front of it — rather than the alphabet, which puts CronJob above Pod.
	Order int
	// Hot marks kinds worth keeping in an informer cache. Everything else is listed on
	// demand, because caching every kind would cost far more memory than it saves
	// (ADR-006, ADR-019).
	Hot bool
}

func (r ResourceType) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
}

// Key identifies a type in a URL: "pods", "apps/deployments".
func (r ResourceType) Key() string {
	if r.Group == "" {
		return r.Resource
	}
	return r.Group + "/" + r.Resource
}

// builtinTypes is what Kubby knows without asking the cluster. Custom resources are
// discovered at runtime and handled generically.
var builtinTypes = []ResourceType{
	// Workloads
	{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Category: CategoryWorkload, Hot: true, Order: 1},
	{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Category: CategoryWorkload, Hot: true, Order: 2},
	{Group: "apps", Version: "v1", Resource: "daemonsets", Kind: "DaemonSet", Namespaced: true, Category: CategoryWorkload, Hot: true, Order: 3},
	{Group: "apps", Version: "v1", Resource: "statefulsets", Kind: "StatefulSet", Namespaced: true, Category: CategoryWorkload, Hot: true, Order: 4},
	{Group: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true, Category: CategoryWorkload, Hot: true, Order: 5},
	{Group: "", Version: "v1", Resource: "replicationcontrollers", Kind: "ReplicationController", Namespaced: true, Category: CategoryWorkload, Order: 6},
	{Group: "batch", Version: "v1", Resource: "jobs", Kind: "Job", Namespaced: true, Category: CategoryWorkload, Order: 7},
	{Group: "batch", Version: "v1", Resource: "cronjobs", Kind: "CronJob", Namespaced: true, Category: CategoryWorkload, Order: 8},

	// Config
	{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true, Category: CategoryConfig, Order: 1},
	{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true, Category: CategoryConfig, Order: 2},
	{Group: "", Version: "v1", Resource: "resourcequotas", Kind: "ResourceQuota", Namespaced: true, Category: CategoryConfig, Order: 3},
	{Group: "", Version: "v1", Resource: "limitranges", Kind: "LimitRange", Namespaced: true, Category: CategoryConfig, Order: 4},
	{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers", Kind: "HorizontalPodAutoscaler", Namespaced: true, Category: CategoryConfig, Order: 5},
	{Group: "policy", Version: "v1", Resource: "poddisruptionbudgets", Kind: "PodDisruptionBudget", Namespaced: true, Category: CategoryConfig, Order: 6},
	{Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses", Kind: "PriorityClass", Category: CategoryConfig, Order: 7},
	{Group: "node.k8s.io", Version: "v1", Resource: "runtimeclasses", Kind: "RuntimeClass", Category: CategoryConfig, Order: 8},
	{Group: "coordination.k8s.io", Version: "v1", Resource: "leases", Kind: "Lease", Namespaced: true, Category: CategoryConfig, Order: 9},
	{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations", Kind: "MutatingWebhookConfiguration", Category: CategoryConfig, Order: 10},
	{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations", Kind: "ValidatingWebhookConfiguration", Category: CategoryConfig, Order: 11},

	// Network
	{Group: "", Version: "v1", Resource: "services", Kind: "Service", Namespaced: true, Category: CategoryNetwork, Hot: true, Order: 1},
	{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices", Kind: "EndpointSlice", Namespaced: true, Category: CategoryNetwork, Order: 2},
	{Group: "", Version: "v1", Resource: "endpoints", Kind: "Endpoints", Namespaced: true, Category: CategoryNetwork, Order: 3},
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespaced: true, Category: CategoryNetwork, Order: 4},
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses", Kind: "IngressClass", Category: CategoryNetwork, Order: 5},
	{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies", Kind: "NetworkPolicy", Namespaced: true, Category: CategoryNetwork, Order: 6},
	{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes", Kind: "HTTPRoute", Namespaced: true, Category: CategoryNetwork, Order: 7},

	// Storage
	{Group: "", Version: "v1", Resource: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true, Category: CategoryStorage, Hot: true, Order: 1},
	{Group: "", Version: "v1", Resource: "persistentvolumes", Kind: "PersistentVolume", Category: CategoryStorage, Order: 2},
	{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses", Kind: "StorageClass", Category: CategoryStorage, Order: 3},

	// Access control
	{Group: "", Version: "v1", Resource: "serviceaccounts", Kind: "ServiceAccount", Namespaced: true, Category: CategoryAccessControl, Order: 1},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles", Kind: "Role", Namespaced: true, Category: CategoryAccessControl, Order: 2},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings", Kind: "RoleBinding", Namespaced: true, Category: CategoryAccessControl, Order: 3},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles", Kind: "ClusterRole", Category: CategoryAccessControl, Order: 4},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings", Kind: "ClusterRoleBinding", Category: CategoryAccessControl, Order: 5},

	// Cluster
	{Group: "", Version: "v1", Resource: "nodes", Kind: "Node", Category: CategoryCluster, Hot: true, Order: 1},
	{Group: "", Version: "v1", Resource: "namespaces", Kind: "Namespace", Category: CategoryCluster, Hot: true, Order: 2},
	{Group: "", Version: "v1", Resource: "events", Kind: "Event", Namespaced: true, Category: CategoryCluster, Hot: true, Order: 3,
		DefaultSort: "lastSeen", DefaultSortDescending: true},
}

var typesByKey = func() map[string]ResourceType {
	out := make(map[string]ResourceType, len(builtinTypes))
	for _, t := range builtinTypes {
		out[t.Key()] = t
	}
	return out
}()

// BuiltinTypes returns the known kinds in a stable order.
func BuiltinTypes() []ResourceType {
	out := append([]ResourceType(nil), builtinTypes...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// LookupType resolves a URL key to a known type.
// LookupKind finds a type by the apiVersion and kind a manifest declares, which is what
// a pasted document carries instead of a route key.
func LookupKind(gvk schema.GroupVersionKind) (ResourceType, error) {
	for _, t := range builtinTypes {
		if t.Kind == gvk.Kind && t.Group == gvk.Group && t.Version == gvk.Version {
			return t, nil
		}
	}
	return ResourceType{}, fmt.Errorf("unknown kind %s/%s", gvk.GroupVersion(), gvk.Kind)
}

func LookupType(key string) (ResourceType, error) {
	if t, ok := typesByKey[strings.TrimSpace(key)]; ok {
		return t, nil
	}
	return ResourceType{}, fmt.Errorf("unknown resource type %q", key)
}

// HotTypes are the kinds an informer cache is kept for.
func HotTypes() []ResourceType {
	var out []ResourceType
	for _, t := range builtinTypes {
		if t.Hot {
			out = append(out, t)
		}
	}
	return out
}
