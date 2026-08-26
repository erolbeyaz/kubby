package promql

import (
	"context"
	"os"
	"testing"
	"time"
)

// The node panel joins two exporters on a name and drops anything that is not a machine.
// Neither claim can be checked against a fixture, so this runs against a real Prometheus:
//
//	KUBBY_PROBE_URL=http://prometheus:9090 go test -run TestNodePanel ./internal/promql
func TestNodePanelDescribesRealMachines(t *testing.T) {
	url := os.Getenv("KUBBY_PROBE_URL")
	if url == "" {
		t.Skip("KUBBY_PROBE_URL is not set; skipping the live node panel test")
	}

	client, err := New(Config{URL: url, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	health := ReadClusterHealth(ctx, client, time.Hour)
	if len(health.NodeDetails) == 0 {
		t.Fatal("no nodes")
	}

	for _, node := range health.NodeDetails {
		t.Logf("%-28s %-14s ready=%-5v %.0f cores  %.0f/%.0f pods  cpu=%.0f%% mem=%.0f%% committed=%.0f%%  %s",
			node.Name, node.Role, node.Ready, node.Cores, node.Pods, node.PodCapacity,
			node.CPUPercent, node.MemoryPercent, node.CPUCommittedPercent, node.KubeletVersion)

		// A card has to describe a machine. The "unknown" node kube-state-metrics reports
		// for pods that were never scheduled is not one, and it arrived with no cores.
		if node.Cores == 0 {
			t.Errorf("node %q has no cores; it is probably not a machine", node.Name)
		}
		if node.Role != "control-plane" && node.Role != "worker" {
			t.Errorf("node %q has role %q", node.Name, node.Role)
		}
	}

	if health.Capacity.Nodes != len(health.NodeDetails) {
		t.Errorf("capacity counts %d nodes, the panel shows %d",
			health.Capacity.Nodes, len(health.NodeDetails))
	}
	// Requests that were never scheduled must not reach this number; counting them read
	// 4706% on a cluster with two deliberately unschedulable pods.
	if health.Capacity.CPUCommittedPercent > 500 {
		t.Errorf("CPU committed is %.0f%%, which means unscheduled requests are being counted",
			health.Capacity.CPUCommittedPercent)
	}
	t.Logf("capacity: %+v", health.Capacity)
	t.Logf("stuck=%d died=%d", len(health.Stuck), len(health.Died))
}
