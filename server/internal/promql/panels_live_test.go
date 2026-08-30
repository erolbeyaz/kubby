package promql

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAllPanelsLive(t *testing.T) {
	url := os.Getenv("KUBBY_PROBE_URL")
	if url == "" {
		t.Skip("KUBBY_PROBE_URL is not set")
	}
	client, err := New(Config{URL: url, Timeout: 25 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	h := ReadClusterHealth(ctx, client, time.Hour)
	t.Logf("whole payload in %s", time.Since(start))

	t.Logf("problems=%d workloads=%d alerts=%d namespaces=%d nodes=%d",
		len(h.Problems), len(h.Workloads), len(h.Alerts), len(h.NamespaceUsage), len(h.NodeDetails))
	for i, p := range h.Problems {
		if i >= 6 {
			break
		}
		t.Logf("  [%s] %-22s %s/%s %s (%.0fm)", p.Severity, p.Kind, p.Namespace, p.Name, p.Reason, p.AgeSecs/60)
	}
	for i, w := range h.Workloads {
		if i >= 4 {
			break
		}
		t.Logf("  %-12s %s/%s ready=%.0f/%.0f healthy=%v", w.Kind, w.Namespace, w.Name, w.Ready, w.Desired, w.Healthy())
	}
	cp := h.ControlPlane
	show := func(name string, r Reading) {
		if r.Known {
			t.Logf("  cp %-22s %.3f", name, r.Value)
		} else {
			t.Logf("  cp %-22s N/A", name)
		}
	}
	show("apiServers", cp.APIServers)
	show("apiLatencyP99", cp.APILatencyP99)
	show("apiErrors5xx", cp.APIErrors5xx)
	show("etcdMembers", cp.EtcdMembers)
	show("corednsUp", cp.CoreDNSUp)
	show("corednsLatencyP99", cp.CoreDNSLatencyP99)
	show("scrapeTargets", cp.ScrapeTargets)
	show("ruleFailures", cp.RuleFailures)
	t.Logf("trends cpu=%d disk=%d netRx=%d", len(h.Trends.CPUByNodeOver), len(h.Trends.DiskByNode), len(h.Trends.NetworkRx))
	for i, n := range h.NamespaceUsage {
		if i >= 3 {
			break
		}
		t.Logf("  ns %-14s cpu=%.3f mem=%.0fMiB pods=%.0f", n.Namespace, n.CPUCores, n.MemoryBytes/1048576, n.Pods)
	}
	if len(h.Warnings) > 0 {
		t.Logf("warnings: %v", h.Warnings)
	}
	if len(h.Problems) == 0 {
		t.Error("this cluster has deliberately broken workloads; problems should not be empty")
	}
}

// The node figures have to agree with `kubectl top node`, because that is what the
// scheduler sees and what a pod's requests are compared against.
//
// They did not: node-exporter reports the host's own memory, which is right on bare
// metal and wrong wherever a node is a container. Every k3d node in one cluster reported
// the same 5.7GiB, because they share the machine's /proc.
func TestNodeUsageIsMeasuredTheWayKubernetesMeasuresIt(t *testing.T) {
	url := os.Getenv("KUBBY_PROBE_URL")
	if url == "" {
		t.Skip("KUBBY_PROBE_URL is not set")
	}
	client, err := New(Config{URL: url, Timeout: 25 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	health := ReadClusterHealth(ctx, client, time.Hour)

	if len(health.Trends.NodeCPUCores) == 0 || len(health.Trends.NodeMemoryBytes) == 0 {
		t.Fatal("no node usage series; is cAdvisor being scraped?")
	}

	// Distinct machines report distinct figures. The broken source did not.
	seen := map[float64]int{}
	for _, series := range health.Trends.NodeMemoryBytes {
		if len(series.Points) == 0 {
			continue
		}
		last := series.Points[len(series.Points)-1].Value
		t.Logf("%-22s %6.0f MiB", series.Name, last/(1024*1024))
		seen[last]++
	}
	for value, count := range seen {
		if count > 1 && len(seen) == 1 {
			t.Errorf("every node reports the same %0.f bytes; the wrong thing is being measured", value)
		}
	}

	for _, series := range health.Trends.NodeCPUCores {
		if len(series.Points) == 0 {
			continue
		}
		cores := series.Points[len(series.Points)-1].Value
		t.Logf("%-22s %6.3f cores", series.Name, cores)
		// A node using more than its own capacity is a measurement of something else.
		if cores > 128 {
			t.Errorf("%s reports %.1f cores, which is not this node", series.Name, cores)
		}
	}
}
