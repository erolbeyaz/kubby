package promql

import (
	"strings"
	"testing"
)

// The verdict is derived from conditions, not scored, and every branch is one somebody
// can argue with. These pin the branches that matter most.
func TestStatusIsDerivedFromConditions(t *testing.T) {
	base := ClusterSummary{NodesReady: known(3), PodsReady: known(20)}

	t.Run("a not-ready node is critical", func(t *testing.T) {
		s := base
		s.NodesNotReady = known(1)

		status, reasons := deriveStatus(s)
		if status != StatusCritical {
			t.Errorf("status is %s", status)
		}
		if !strings.Contains(strings.Join(reasons, " "), "not ready") {
			t.Errorf("reasons do not say why: %v", reasons)
		}
	})

	t.Run("a workload short of replicas is degraded", func(t *testing.T) {
		s := base
		s.Unavailable = known(2)

		if status, _ := deriveStatus(s); status != StatusDegraded {
			t.Errorf("status is %s", status)
		}
	})

	// Its own reason rather than a share of "pending". Kubernetes leaves a pod in phase
	// Pending while its image will not pull, so counting it as pending put a number on
	// the overview that no row underneath accounted for.
	t.Run("a container that will not start is degraded and says so", func(t *testing.T) {
		s := base
		s.ContainersNotStarting = known(3)

		status, reasons := deriveStatus(s)
		if status != StatusDegraded {
			t.Errorf("status is %s", status)
		}
		if !strings.Contains(strings.Join(reasons, " "), "will not start") {
			t.Errorf("reasons do not say why: %v", reasons)
		}
	})

	t.Run("everything quiet is healthy", func(t *testing.T) {
		if status, reasons := deriveStatus(base); status != StatusHealthy || len(reasons) != 0 {
			t.Errorf("status is %s with %v", status, reasons)
		}
	})
}

// The failure this guards against: a metric nobody collects reads as zero, zero reads as
// nothing wrong, and a cluster with no monitoring at all reports itself healthy.
func TestAnUnreadableClusterIsNotHealthy(t *testing.T) {
	status, reasons := deriveStatus(ClusterSummary{})

	if status != StatusUnknown {
		t.Fatalf("a cluster with no readable metrics reported %s", status)
	}
	if len(reasons) == 0 {
		t.Error("Unknown was reported without saying why")
	}
}

// An unknown reading is not evidence of anything, in either direction.
func TestUnknownReadingsDoNotRaiseTheVerdict(t *testing.T) {
	s := ClusterSummary{NodesReady: known(3), PodsReady: known(20)}
	// Not collected: value zero, known false. It must not be read as "zero alerts", and
	// it must not be read as a problem either.
	s.AlertsCritical = Unknown
	s.Evicted = Unknown
	s.NodesNotReady = Unknown

	if status, reasons := deriveStatus(s); status != StatusHealthy {
		t.Errorf("unknown readings changed the verdict to %s: %v", status, reasons)
	}
}

// A pod pending for ten seconds is a scheduler working; one pending for hours is not.
func TestOnlyALongPendingPodCounts(t *testing.T) {
	s := ClusterSummary{NodesReady: known(3), PodsReady: known(20)}

	s.LongestPendingSeconds = known(30)
	if status, _ := deriveStatus(s); status != StatusHealthy {
		t.Errorf("a pod pending for 30 seconds made the cluster %s", status)
	}

	s.LongestPendingSeconds = known(3600)
	if status, _ := deriveStatus(s); status != StatusDegraded {
		t.Errorf("a pod pending for an hour left the cluster %s", status)
	}
}
