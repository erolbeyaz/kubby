package cluster

import (
	"context"
	"sort"
	"sync"

	"github.com/erolbeyaz/kubby/internal/store"
)

// WorkloadCount is one kind and how much of it is healthy.
type WorkloadCount struct {
	Kind    string `json:"kind"`
	TypeKey string `json:"typeKey"`
	Total   int    `json:"total"`
	// Ready is how many are in the state their kind considers working. The bar is the
	// ratio: a solid bar means the namespace is doing what it was asked to.
	Ready int `json:"ready"`
}

// WorkloadOverview is the shape of what is running, and what just happened to it.
type WorkloadOverview struct {
	Counts []WorkloadCount `json:"counts"`
	Events *ListResult     `json:"events"`
}

// workloadKinds is the order the overview reads in: what runs, then what runs it.
var workloadKinds = []string{
	"pods", "apps/deployments", "apps/daemonsets", "apps/statefulsets",
	"apps/replicasets", "batch/jobs", "batch/cronjobs",
}

// WorkloadsOverview counts each workload kind and returns the recent events beside them.
//
// The counts and the events answer the same question at two distances: what is here, and
// what has been happening to it. Reading them apart is how a rollout in progress looks
// like a cluster that is simply wrong.
func (s *Service) WorkloadsOverview(ctx context.Context, cluster *store.Cluster, namespaces []string, impersonate *ImpersonationConfig) (*WorkloadOverview, error) {
	out := &WorkloadOverview{Counts: make([]WorkloadCount, 0, len(workloadKinds))}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, key := range workloadKinds {
		resourceType, err := LookupType(key)
		if err != nil {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			list, err := s.List(ctx, cluster, ListRequest{Type: resourceType, Namespaces: namespaces}, impersonate)
			if err != nil {
				// A kind the credential cannot list is left out rather than shown as
				// zero: "no deployments" and "not allowed to look" are different facts.
				return
			}

			count := WorkloadCount{Kind: resourceType.Kind, TypeKey: key, Total: len(list.Rows)}
			for _, row := range list.Rows {
				if row.Severity == "" {
					count.Ready++
				}
			}

			mu.Lock()
			defer mu.Unlock()
			out.Counts = append(out.Counts, count)
		}()
	}

	eventType, err := LookupType("events")
	if err == nil {
		wg.Add(1)
		go func() {
			defer wg.Done()

			events, err := s.List(ctx, cluster, ListRequest{
				Type: eventType, Namespaces: namespaces, Limit: eventLimit,
			}, impersonate)
			if err != nil {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			out.Events = events
		}()
	}
	wg.Wait()

	// Concurrency loses the order the kinds were asked in, and that order is the point.
	position := make(map[string]int, len(workloadKinds))
	for index, key := range workloadKinds {
		position[key] = index
	}
	sort.Slice(out.Counts, func(i, j int) bool {
		return position[out.Counts[i].TypeKey] < position[out.Counts[j].TypeKey]
	})
	return out, nil
}

// eventLimit caps the event list on the overview. Enough to see what is going on,
// bounded so a noisy cluster does not send a megabyte to draw a table nobody scrolls.
const eventLimit = 200
