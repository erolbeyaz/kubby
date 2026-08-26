package health

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func targets(names ...string) []FleetTarget {
	out := make([]FleetTarget, 0, len(names))
	for _, name := range names {
		out = append(out, FleetTarget{ID: name, Name: name, Status: "valid"})
	}
	return out
}

func reportWith(critical, warning int) *Report {
	report := &Report{Counts: map[string]int{SeverityCritical: critical, SeverityWarning: warning}}
	for i := 0; i < critical; i++ {
		report.Findings = append(report.Findings, Finding{Severity: SeverityCritical, Name: "pod"})
	}
	return report
}

// A cluster that cannot be reached comes back marked, not missing: a partial fleet view
// beats no fleet view.
func TestUnreachableClusterStillGetsACard(t *testing.T) {
	f := &Fleet{}
	gather := Gatherers{Sweep: func(_ context.Context, target FleetTarget) (*Report, error) {
		if target.ID == "broken" {
			return nil, errors.New("dial tcp: connection refused")
		}
		return reportWith(0, 1), nil
	}}

	cards := f.Cards(context.Background(), targets("healthy", "broken"), gather)

	if len(cards) != 2 {
		t.Fatalf("cards = %d, want one per cluster", len(cards))
	}
	// Unreachable sorts first: a cluster you cannot see is worse than one you can see is broken.
	if !cards[0].Unreachable || cards[0].ID != "broken" {
		t.Fatalf("first card = %+v", cards[0])
	}
	if cards[0].Error == "" {
		t.Fatal("an unreachable card must say why")
	}
}

// One slow cluster must not hold the page.
func TestSlowClusterIsBounded(t *testing.T) {
	f := &Fleet{Timeout: 20 * time.Millisecond}
	gather := Gatherers{Sweep: func(ctx context.Context, target FleetTarget) (*Report, error) {
		if target.ID == "slow" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return reportWith(1, 0), nil
	}}

	start := time.Now()
	cards := f.Cards(context.Background(), targets("slow", "fast"), gather)

	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("cards took %s; the per-cluster deadline did not apply", elapsed)
	}
	if len(cards) != 2 {
		t.Fatalf("cards = %d", len(cards))
	}
}

// Without a cache, a fleet of twenty clusters means twenty concurrent sweeps on every
// page load, and the tool becomes a cause of the outages it is meant to find.
func TestRepeatSweepsAreServedFromCache(t *testing.T) {
	var calls atomic.Int32
	now := time.Now()

	f := &Fleet{TTL: time.Minute, Now: func() time.Time { return now }}
	gather := Gatherers{Sweep: func(context.Context, FleetTarget) (*Report, error) {
		calls.Add(1)
		return reportWith(2, 0), nil
	}}

	f.Cards(context.Background(), targets("a", "b"), gather)
	second := f.Cards(context.Background(), targets("a", "b"), gather)

	if got := calls.Load(); got != 2 {
		t.Fatalf("sweeps = %d, want one per cluster", got)
	}
	if !second[0].Stale {
		t.Fatal("a cached card must say it is cached")
	}
}

func TestCacheExpires(t *testing.T) {
	var calls atomic.Int32
	now := time.Now()

	f := &Fleet{TTL: 30 * time.Second, Now: func() time.Time { return now }}
	gather := Gatherers{Sweep: func(context.Context, FleetTarget) (*Report, error) {
		calls.Add(1)
		return reportWith(0, 0), nil
	}}

	f.Cards(context.Background(), targets("a"), gather)
	now = now.Add(31 * time.Second)
	f.Cards(context.Background(), targets("a"), gather)

	if got := calls.Load(); got != 2 {
		t.Fatalf("sweeps = %d, want the cache to have expired", got)
	}
}

// Changing a cluster's credentials should be reflected now, not up to a TTL later.
func TestForgetDropsACachedCard(t *testing.T) {
	var calls atomic.Int32
	f := &Fleet{TTL: time.Minute}
	gather := Gatherers{Sweep: func(context.Context, FleetTarget) (*Report, error) {
		calls.Add(1)
		return reportWith(0, 0), nil
	}}

	f.Cards(context.Background(), targets("a"), gather)
	f.Forget("a")
	f.Cards(context.Background(), targets("a"), gather)

	if got := calls.Load(); got != 2 {
		t.Fatalf("sweeps = %d, want the forgotten cluster to be swept again", got)
	}
}

// The fleet view answers "where do I look" without scrolling.
func TestCardsAreSortedByHowBadTheyAre(t *testing.T) {
	f := &Fleet{}
	gather := Gatherers{Sweep: func(_ context.Context, target FleetTarget) (*Report, error) {
		switch target.ID {
		case "quiet":
			return reportWith(0, 0), nil
		case "warning":
			return reportWith(0, 5), nil
		}
		return reportWith(3, 0), nil
	}}

	cards := f.Cards(context.Background(), targets("quiet", "warning", "critical"), gather)

	order := []string{cards[0].ID, cards[1].ID, cards[2].ID}
	want := []string{"critical", "warning", "quiet"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestCardCarriesOnlyTheWorstFewFindings(t *testing.T) {
	f := &Fleet{}
	gather := Gatherers{Sweep: func(context.Context, FleetTarget) (*Report, error) {
		return reportWith(10, 0), nil
	}}

	cards := f.Cards(context.Background(), targets("a"), gather)

	if len(cards[0].Top) != TopFindings {
		t.Fatalf("top = %d, want %d", len(cards[0].Top), TopFindings)
	}
	if cards[0].Counts[SeverityCritical] != 10 {
		t.Fatalf("counts must still report the full tally: %+v", cards[0].Counts)
	}
}

// A card has to say what is on the other end of the link before anyone clicks it: how
// many machines, how big they are, how many are answering.
func TestACardCarriesWhatTheClusterIsMadeOf(t *testing.T) {
	f := &Fleet{}
	gather := Gatherers{
		Sweep: func(context.Context, FleetTarget) (*Report, error) {
			return &Report{Counts: map[string]int{}}, nil
		},
		Capacity: func(context.Context, FleetTarget) (*ClusterCapacity, error) {
			return &ClusterCapacity{Nodes: 3, NodesReady: 3, Cores: 12, MemoryMiB: 47088}, nil
		},
	}

	cards := f.Cards(context.Background(), targets("c1"), gather)
	if len(cards) != 1 || cards[0].Capacity == nil {
		t.Fatalf("the card carries no capacity: %+v", cards)
	}
	if cards[0].Capacity.Cores != 12 || cards[0].Capacity.Nodes != 3 {
		t.Errorf("capacity came back wrong: %+v", cards[0].Capacity)
	}
}

// Capacity is worth having and never worth failing for. A cluster that answers the
// health sweep but not the node list is still a cluster worth showing.
func TestAFailedCapacityDoesNotFailTheCard(t *testing.T) {
	f := &Fleet{}
	gather := Gatherers{
		Sweep: func(context.Context, FleetTarget) (*Report, error) {
			return &Report{Counts: map[string]int{SeverityWarning: 2}}, nil
		},
		Capacity: func(context.Context, FleetTarget) (*ClusterCapacity, error) {
			return nil, errors.New("nodes is forbidden")
		},
	}

	cards := f.Cards(context.Background(), targets("c1"), gather)
	if cards[0].Unreachable {
		t.Error("a cluster that answered the sweep was reported unreachable")
	}
	if cards[0].Capacity != nil {
		t.Errorf("a failed capacity left something behind: %+v", cards[0].Capacity)
	}
	if cards[0].Counts[SeverityWarning] != 2 {
		t.Error("the findings were lost with it")
	}
}

// Nothing to gather it with is not an error either.
func TestNoCapacityGathererIsFine(t *testing.T) {
	f := &Fleet{}
	gather := Gatherers{Sweep: func(context.Context, FleetTarget) (*Report, error) {
		return &Report{Counts: map[string]int{}}, nil
	}}

	cards := f.Cards(context.Background(), targets("c1"), gather)
	if cards[0].Unreachable || cards[0].Capacity != nil {
		t.Errorf("card is wrong: %+v", cards[0])
	}
}

// The gatherers close over which clusters *this* user may see. Held on the shared Fleet
// they were overwritten by whichever request assigned them last, so two overlapping
// requests could each run the other's closure — a sweep against the wrong grant map, or
// a nil dereference on a cluster the other user cannot see.
func TestEachCallUsesItsOwnGatherers(t *testing.T) {
	f := &Fleet{}
	var wg sync.WaitGroup
	seen := make([]string, 2)

	for i, owner := range []string{"alice", "bob"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gather := Gatherers{Sweep: func(context.Context, FleetTarget) (*Report, error) {
				seen[i] = owner
				return &Report{Counts: map[string]int{}}, nil
			}}
			f.Cards(context.Background(), targets("cluster-"+owner), gather)
		}()
	}
	wg.Wait()

	if seen[0] != "alice" || seen[1] != "bob" {
		t.Fatalf("a call ran another call's gatherer: %v", seen)
	}
}
