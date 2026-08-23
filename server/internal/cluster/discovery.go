package cluster

import (
	"context"
	"sync"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/store"
)

// discoveryTTL is how long a cluster's served-resource list is trusted. CRDs come and
// go, but not by the second.
const discoveryTTL = 5 * time.Minute

type discoveryEntry struct {
	served  map[string]bool
	fetched time.Time
}

// discoveryCache remembers which resources each cluster actually serves.
//
// Offering a kind the cluster does not have produces a confusing failure: the user
// clicks HTTPRoute and is told the resource was not found, which reads as "your cluster
// is broken" rather than "Gateway API is not installed here".
type discoveryCache struct {
	mu      sync.Mutex
	entries map[uuid.UUID]discoveryEntry
}

func newDiscoveryCache() *discoveryCache {
	return &discoveryCache{entries: make(map[uuid.UUID]discoveryEntry)}
}

// served returns the set of "group/resource" keys the cluster serves.
func (d *discoveryCache) served(ctx context.Context, clusterID uuid.UUID, cfg *rest.Config) map[string]bool {
	d.mu.Lock()
	entry, ok := d.entries[clusterID]
	d.mu.Unlock()

	if ok && time.Since(entry.fetched) < discoveryTTL {
		return entry.served
	}

	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil
	}

	// Partial discovery is normal: an aggregated API server that is momentarily down
	// makes the whole call return an error alongside usable results.
	_, lists, err := client.ServerGroupsAndResources()
	if lists == nil && err != nil {
		return nil
	}

	found := make(map[string]bool, 128)
	for _, list := range lists {
		group, _, _ := splitGroupVersion(list.GroupVersion)
		for _, resource := range list.APIResources {
			key := resource.Name
			if group != "" {
				key = group + "/" + resource.Name
			}
			found[key] = true
		}
	}

	d.mu.Lock()
	d.entries[clusterID] = discoveryEntry{served: found, fetched: time.Now()}
	d.mu.Unlock()
	return found
}

func (d *discoveryCache) forget(clusterID uuid.UUID) {
	d.mu.Lock()
	delete(d.entries, clusterID)
	d.mu.Unlock()
}

// splitGroupVersion divides "apps/v1" into its parts; a bare "v1" has no group.
func splitGroupVersion(groupVersion string) (group, version string, ok bool) {
	for i := range groupVersion {
		if groupVersion[i] == '/' {
			return groupVersion[:i], groupVersion[i+1:], true
		}
	}
	return "", groupVersion, true
}

// AvailableTypes returns the built-in kinds this cluster actually serves.
func (s *Service) AvailableTypes(ctx context.Context, cluster *store.Cluster, impersonate *ImpersonationConfig) []ResourceType {
	all := BuiltinTypes()

	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return all
	}

	served := s.discovery.served(ctx, cluster.ID, cfg)
	if served == nil {
		// Discovery failed; offering everything is better than offering nothing.
		return all
	}

	out := make([]ResourceType, 0, len(all))
	for _, resourceType := range all {
		if served[resourceType.Key()] {
			out = append(out, resourceType)
		}
	}
	return out
}
