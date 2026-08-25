package cluster

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/erolbeyaz/kubby/internal/store"
)

// SearchHit is one object found, with everything needed to open it.
type SearchHit struct {
	ClusterID   string `json:"clusterId"`
	ClusterName string `json:"clusterName"`
	Environment string `json:"environment"`
	TypeKey     string `json:"typeKey"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	// Status is the row's own summary, so a hit shows whether the thing found is healthy
	// without opening it.
	Status string `json:"status,omitempty"`
	// Severity marks a hit that is in trouble, so a broken object sorts above a healthy
	// one with a slightly better name match.
	Severity string `json:"severity,omitempty"`
	Age      string `json:"age,omitempty"`

	// score orders the results and is not sent: it is an implementation detail of the
	// ranking, and a number on screen invites arguments about why one hit beat another.
	score int
}

// SearchResult is what one query found, and what it could not reach.
type SearchResult struct {
	Hits []SearchHit `json:"hits"`
	// Unreachable names the clusters that did not answer. A search that quietly returns
	// fewer results because a cluster is down is a search that lies: the reader concludes
	// the object does not exist.
	Unreachable []SearchProblem `json:"unreachable,omitempty"`
	Truncated   bool            `json:"truncated"`
}

type SearchProblem struct {
	ClusterID   string `json:"clusterId"`
	ClusterName string `json:"clusterName"`
	Reason      string `json:"reason"`
}

// SearchRequest is one query across a fleet.
type SearchRequest struct {
	Query string
	// Clusters are the ones the reader may see. Searching anything else would leak the
	// existence of objects in a cluster they were never granted.
	Clusters []*store.Cluster
	Limit    int
}

// searchTypes are the kinds a global search covers.
//
// The hot ones only. A search that asked every cluster for every kind would be dozens of
// list calls per keystroke against every API server in the fleet — and the kinds people
// search for by name are these. Anything else is found by opening its own list.
var searchTypes = []string{
	"pods", "apps/deployments", "apps/statefulsets", "apps/daemonsets",
	"services", "namespaces", "nodes", "configmaps", "secrets", "networking.k8s.io/ingresses",
}

// Search looks for a name across every cluster the reader can see.
//
// Every cluster is asked at once and each one is given its own deadline, so one slow or
// unreachable cluster costs the whole search that deadline rather than blocking it. What
// could not be reached is reported rather than silently dropped.
func (s *Service) Search(ctx context.Context, req SearchRequest, impersonate func(*store.Cluster) *ImpersonationConfig) *SearchResult {
	needle := strings.ToLower(strings.TrimSpace(req.Query))
	if needle == "" {
		return &SearchResult{}
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	out := &SearchResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, cluster := range req.Clusters {
		// A cluster Kubby already knows it cannot reach is skipped rather than waited on:
		// the monitor has established that, and spending the deadline to rediscover it
		// makes every search slow while one cluster is down.
		if cluster.CredentialStatus == store.CredentialInvalid {
			mu.Lock()
			out.Unreachable = append(out.Unreachable, SearchProblem{
				ClusterID: cluster.ID.String(), ClusterName: cluster.Name,
				Reason: "the stored credential is not usable",
			})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(c *store.Cluster) {
			defer wg.Done()

			clusterCtx, cancel := context.WithTimeout(ctx, searchTimeout)
			defer cancel()

			hits, err := s.searchCluster(clusterCtx, c, needle, impersonate(c))

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Unreachable = append(out.Unreachable, SearchProblem{
					ClusterID: c.ID.String(), ClusterName: c.Name, Reason: err.Error(),
				})
				return
			}
			out.Hits = append(out.Hits, hits...)
		}(cluster)
	}

	wg.Wait()

	sort.SliceStable(out.Hits, func(i, j int) bool {
		if out.Hits[i].score != out.Hits[j].score {
			return out.Hits[i].score > out.Hits[j].score
		}
		return out.Hits[i].Name < out.Hits[j].Name
	})

	if len(out.Hits) > req.Limit {
		out.Hits = out.Hits[:req.Limit]
		out.Truncated = true
	}
	sort.Slice(out.Unreachable, func(i, j int) bool {
		return out.Unreachable[i].ClusterName < out.Unreachable[j].ClusterName
	})
	return out
}

func (s *Service) searchCluster(ctx context.Context, cluster *store.Cluster, needle string, impersonate *ImpersonationConfig) ([]SearchHit, error) {
	var (
		mu   sync.Mutex
		hits []SearchHit
		wg   sync.WaitGroup
		// firstErr is the cluster's own failure — a bad credential, an unreachable API
		// server — as opposed to one kind being unavailable, which is normal.
		firstErr error
	)

	for _, key := range searchTypes {
		resourceType, err := LookupType(key)
		if err != nil {
			continue
		}

		wg.Add(1)
		go func(rt ResourceType) {
			defer wg.Done()

			result, err := s.List(ctx, cluster, ListRequest{
				Type: rt,
				// Asked of the API server rather than filtered here, so a cluster with
				// ten thousand pods does not send all of them for one keystroke.
				Search: needle,
				Limit:  perKindSearchLimit,
			}, impersonate)
			if err != nil {
				mu.Lock()
				// A kind the cluster does not serve, or one this credential may not
				// read, is not a failed search — it is a normal answer of "none".
				if isClusterLevelFailure(err) && firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, row := range result.Rows {
				hits = append(hits, SearchHit{
					ClusterID:   cluster.ID.String(),
					ClusterName: cluster.Name,
					Environment: cluster.Environment,
					TypeKey:     rt.Key(),
					Kind:        rt.Kind,
					Namespace:   row.Namespace,
					Name:        row.Name,
					Status:      row.Fields["status"],
					Severity:    row.Severity,
					Age:         row.Age,
					score:       scoreHit(row.Name, needle, row.Severity),
				})
			}
		}(resourceType)
	}

	wg.Wait()

	if len(hits) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return hits, nil
}

// scoreHit ranks a match.
//
// An exact name beats a prefix, a prefix beats a substring, and something broken beats
// something healthy. The last rule is the one worth having: a global search is nearly
// always someone looking for the thing that is wrong.
func scoreHit(name, needle, severity string) int {
	lower := strings.ToLower(name)

	score := 0
	switch {
	case lower == needle:
		score = 100
	case strings.HasPrefix(lower, needle):
		score = 60
	case strings.Contains(lower, needle):
		score = 30
	}

	// A shorter name containing the needle is a closer match than a long one that merely
	// includes it.
	if extra := len(lower) - len(needle); extra < 20 {
		score += 20 - extra
	}

	switch severity {
	case "error":
		score += 25
	case "warn":
		score += 12
	}
	return score
}

// isClusterLevelFailure separates "this cluster is not answering" from "this kind is not
// available here", which is the difference between reporting a cluster as unreachable and
// quietly returning nothing for one resource type.
func isClusterLevelFailure(err error) bool {
	message := strings.ToLower(err.Error())
	for _, normal := range []string{"not found", "forbidden", "may not", "unknown resource"} {
		if strings.Contains(message, normal) {
			return false
		}
	}
	return true
}

const (
	// One keystroke fans out to every cluster, so the deadline is short: a search that
	// waits ten seconds on one unreachable cluster is a search nobody uses.
	searchTimeout = 4 * time.Second
	// Per kind per cluster. The point is to find something by name, not to page through
	// results, and a palette shows a screenful.
	perKindSearchLimit = 25
	defaultSearchLimit = 60
)
