package health

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func node(name string, spec, status map[string]any) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
		"status":     status,
	}}
}

func detectNodes(t *testing.T, nodes ...unstructured.Unstructured) []Finding {
	t.Helper()

	findings, err := (&NodeDetector{}).Detect(context.Background(), fakeReader{
		objects: map[schema.GroupVersionResource][]unstructured.Unstructured{nodesGVR: nodes},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return findings
}

// A kubelet that stopped reporting is a different failure from one reporting ill health,
// and the detail has to say which.
func TestUnknownReadyStatusReadsAsUnreachable(t *testing.T) {
	findings := detectNodes(t, node("node-a", nil, map[string]any{
		"conditions": []any{map[string]any{"type": "Ready", "status": "Unknown"}},
	}))

	if len(findings) != 1 || findings[0].Reason != "NotReady" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Detail != "The kubelet has stopped reporting; the node may be unreachable." {
		t.Fatalf("detail = %q", findings[0].Detail)
	}
}

func TestDetectsPressureConditions(t *testing.T) {
	findings := detectNodes(t, node("node-a", nil, map[string]any{
		"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": "DiskPressure", "status": "True"},
		},
	}))

	if len(findings) != 1 || findings[0].Reason != "DiskPressure" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Severity != SeverityWarning {
		t.Fatalf("severity = %q", findings[0].Severity)
	}
}

// A node left cordoned after maintenance is a quiet cause of Pending pods, so it is
// reported — but as information, because cordoning is deliberate.
func TestCordonedNodeIsInformation(t *testing.T) {
	findings := detectNodes(t, node("node-a", map[string]any{"unschedulable": true}, map[string]any{
		"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
	}))

	if len(findings) != 1 || findings[0].Reason != "Cordoned" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Severity != SeverityInfo {
		t.Fatalf("severity = %q, want info", findings[0].Severity)
	}
}

func TestHealthyNodeProducesNothing(t *testing.T) {
	findings := detectNodes(t, node("node-a", nil, map[string]any{
		"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": "MemoryPressure", "status": "False"},
		},
	}))

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestKubeletSkewOnlyReportsBeyondTheSupportedWindow(t *testing.T) {
	nodes := []unstructured.Unstructured{
		node("ok", nil, map[string]any{"nodeInfo": map[string]any{"kubeletVersion": "v1.32.1"}}),
		node("old", nil, map[string]any{"nodeInfo": map[string]any{"kubeletVersion": "v1.30.4"}}),
	}

	findings := KubeletSkew(nodes, 34)

	if len(findings) != 1 || findings[0].Name != "old" {
		t.Fatalf("findings = %+v, want only the node beyond two minor versions", findings)
	}
}
