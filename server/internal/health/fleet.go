package health

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Fleet defaults. The cache matters more than it looks: without it, a fleet of twenty
// clusters means every page load fans twenty concurrent sweeps at twenty API servers,
// and the tool becomes a cause of the outages it is meant to find.
const (
	DefaultFleetTTL     = 30 * time.Second
	DefaultFleetTimeout = 10 * time.Second
	// TopFindings is how many findings a card carries. The card says whether to look;
	// the cluster's own panel says what at.
	TopFindings = 3
)

// ClusterCard is one cluster's health, small enough to put many on a screen.
type ClusterCard struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment string `json:"environment,omitempty"`
	Colour      string `json:"colour,omitempty"`
	// Status mirrors the stored credential status: valid, invalid, unreachable.
	Status string `json:"status"`
	// Unreachable is set when this sweep could not reach the cluster, whatever the
	// stored status said. A partial fleet view beats no fleet view.
	Unreachable bool           `json:"unreachable"`
	Error       string         `json:"error,omitempty"`
	Counts      map[string]int `json:"counts"`
	Top         []Finding      `json:"top,omitempty"`
	// CheckedAt is when this card's data was gathered, RFC 3339 UTC.
	CheckedAt string `json:"checkedAt"`
	// Stale marks a card served from cache rather than swept just now.
	Stale bool `json:"stale"`
	// Capacity is what the cluster is made of. Absent when it could not be reached, or
	// when nothing was asked to gather it — a card without it still says what is wrong,
	// which is the question this screen exists for.
	Capacity *ClusterCapacity `json:"capacity,omitempty"`
}

// ClusterCapacity is the size of a cluster, for a card that has to say what is on the
// other end of the link before anyone clicks it.
type ClusterCapacity struct {
	Nodes      int     `json:"nodes"`
	NodesReady int     `json:"nodesReady"`
	Cores      float64 `json:"cores"`
	MemoryMiB  float64 `json:"memoryMiB"`
	Pods       int     `json:"pods"`
	K8sVersion string  `json:"k8sVersion,omitempty"`
}

// FleetTarget is a cluster to sweep.
type FleetTarget struct {
	ID          string
	Name        string
	Environment string
	Colour      string
	Status      string
}

// SweepFunc gathers one cluster's report.
type SweepFunc func(ctx context.Context, target FleetTarget) (*Report, error)

// CapacityFunc gathers what a cluster is made of. Optional; a nil func leaves the card
// without a capacity block rather than failing the sweep.
type CapacityFunc func(ctx context.Context, target FleetTarget) (*ClusterCapacity, error)

// Gatherers are how one request reads its clusters.
//
// Passed in per call rather than held on the Fleet, because they close over the caller's
// own state — which clusters this user may see, and their stored credentials. Assigning
// them to the shared struct meant two overlapping requests could each run the other's
// closure: at best a sweep against the wrong grant map, at worst a nil dereference on a
// cluster the other user cannot see.
type Gatherers struct {
	Sweep SweepFunc
	// Capacity is optional; nil leaves the cards without a capacity block.
	Capacity CapacityFunc
}

// Fleet sweeps many clusters and caches what it finds.
type Fleet struct {
	TTL     time.Duration
	Timeout time.Duration
	Now     func() time.Time

	mu     sync.Mutex
	cached map[string]cardEntry
}

type cardEntry struct {
	card ClusterCard
	at   time.Time
}

// Cards sweeps every target concurrently and returns a card for each.
//
// One slow or unreachable cluster must not hold the page: each sweep has its own deadline
// and a cluster that misses it comes back marked rather than missing.
func (f *Fleet) Cards(ctx context.Context, targets []FleetTarget, gather Gatherers) []ClusterCard {
	cards := make([]ClusterCard, len(targets))

	var wg sync.WaitGroup
	for i, target := range targets {
		if card, ok := f.fromCache(target); ok {
			cards[i] = card
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			cards[i] = f.sweep(ctx, target, gather)
		}()
	}
	wg.Wait()

	// Worst first, so the fleet view answers "where do I look" without scrolling.
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if a.Unreachable != b.Unreachable {
			return a.Unreachable
		}
		if a.Counts[SeverityCritical] != b.Counts[SeverityCritical] {
			return a.Counts[SeverityCritical] > b.Counts[SeverityCritical]
		}
		if a.Counts[SeverityWarning] != b.Counts[SeverityWarning] {
			return a.Counts[SeverityWarning] > b.Counts[SeverityWarning]
		}
		return a.Name < b.Name
	})
	return cards
}

func (f *Fleet) sweep(ctx context.Context, target FleetTarget, gather Gatherers) ClusterCard {
	card := ClusterCard{
		ID:          target.ID,
		Name:        target.Name,
		Environment: target.Environment,
		Colour:      target.Colour,
		Status:      target.Status,
		Counts:      map[string]int{},
		CheckedAt:   f.now().UTC().Format(time.RFC3339),
	}

	scoped, cancel := context.WithTimeout(ctx, f.timeout())
	defer cancel()

	// Both against the same deadline: the size of the cluster is worth having but never
	// worth holding the page for, so a capacity that does not answer costs that one
	// block and nothing else.
	var (
		capacity    *ClusterCapacity
		capacityErr error
		wg          sync.WaitGroup
	)
	if gather.Capacity != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			capacity, capacityErr = gather.Capacity(scoped, target)
		}()
	}

	report, err := gather.Sweep(scoped, target)
	wg.Wait()
	if err != nil {
		card.Unreachable = true
		card.Error = err.Error()
		f.store(target.ID, card)
		return card
	}
	if capacityErr == nil {
		card.Capacity = capacity
	}

	card.Counts = report.Counts
	if len(report.Findings) > TopFindings {
		card.Top = report.Findings[:TopFindings]
	} else {
		card.Top = report.Findings
	}
	f.store(target.ID, card)
	return card
}

func (f *Fleet) fromCache(target FleetTarget) (ClusterCard, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	entry, found := f.cached[target.ID]
	if !found || f.now().Sub(entry.at) > f.ttl() {
		return ClusterCard{}, false
	}
	entry.card.Stale = true
	return entry.card, true
}

func (f *Fleet) store(id string, card ClusterCard) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cached == nil {
		f.cached = map[string]cardEntry{}
	}
	f.cached[id] = cardEntry{card: card, at: f.now()}
}

// Forget drops a cluster's cached card, so a change to its credentials or a manual
// refresh is reflected immediately rather than up to a TTL later.
func (f *Fleet) Forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cached, id)
}

func (f *Fleet) ttl() time.Duration {
	if f.TTL <= 0 {
		return DefaultFleetTTL
	}
	return f.TTL
}

func (f *Fleet) timeout() time.Duration {
	if f.Timeout <= 0 {
		return DefaultFleetTimeout
	}
	return f.Timeout
}

func (f *Fleet) now() time.Time {
	if f.Now == nil {
		return time.Now()
	}
	return f.Now()
}
