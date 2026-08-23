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
	{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Category: CategoryWorkload, Hot: true},
	{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Category: CategoryWorkload, Hot: true},
	{Group: "apps", Version: "v1", Resource: "statefulsets", Kind: "StatefulSet", Namespaced: true, Category: CategoryWorkload, Hot: true},
	{Group: "apps", Version: "v1", Resource: "daemonsets", Kind: "DaemonSet", Namespaced: true, Category: CategoryWorkload, Hot: true},
	{Group: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true, Category: CategoryWorkload, Hot: true},
	{Group: "batch", Version: "v1", Resource: "jobs", Kind: "Job", Namespaced: true, Category: CategoryWorkload},
	{Group: "batch", Version: "v1", Resource: "cronjobs", Kind: "CronJob", Namespaced: true, Category: CategoryWorkload},

	// Config
	{Group: "", Version: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespaced: true, Category: CategoryConfig},
	{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true, Category: CategoryConfig},
	{Group: "", Version: "v1", Resource: "resourcequotas", Kind: "ResourceQuota", Namespaced: true, Category: CategoryConfig},
	{Group: "", Version: "v1", Resource: "limitranges", Kind: "LimitRange", Namespaced: true, Category: CategoryConfig},

	// Network
	{Group: "", Version: "v1", Resource: "services", Kind: "Service", Namespaced: true, Category: CategoryNetwork, Hot: true},
	{Group: "", Version: "v1", Resource: "endpoints", Kind: "Endpoints", Namespaced: true, Category: CategoryNetwork},
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespaced: true, Category: CategoryNetwork},
	{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies", Kind: "NetworkPolicy", Namespaced: true, Category: CategoryNetwork},
	{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes", Kind: "HTTPRoute", Namespaced: true, Category: CategoryNetwork},

	// Storage
	{Group: "", Version: "v1", Resource: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true, Category: CategoryStorage, Hot: true},
	{Group: "", Version: "v1", Resource: "persistentvolumes", Kind: "PersistentVolume", Category: CategoryStorage},
	{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses", Kind: "StorageClass", Category: CategoryStorage},

	// Access control
	{Group: "", Version: "v1", Resource: "serviceaccounts", Kind: "ServiceAccount", Namespaced: true, Category: CategoryAccessControl},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles", Kind: "Role", Namespaced: true, Category: CategoryAccessControl},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings", Kind: "RoleBinding", Namespaced: true, Category: CategoryAccessControl},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles", Kind: "ClusterRole", Category: CategoryAccessControl},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings", Kind: "ClusterRoleBinding", Category: CategoryAccessControl},

	// Cluster
	{Group: "", Version: "v1", Resource: "nodes", Kind: "Node", Category: CategoryCluster, Hot: true},
	{Group: "", Version: "v1", Resource: "namespaces", Kind: "Namespace", Category: CategoryCluster, Hot: true},
	{Group: "", Version: "v1", Resource: "events", Kind: "Event", Namespaced: true, Category: CategoryCluster, Hot: true},
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
		return out[i].Kind < out[j].Kind
	})
	return out
}

// LookupType resolves a URL key to a known type.
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
