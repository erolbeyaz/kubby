package cluster

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func deployment(replicas int64, extra map[string]any) *unstructured.Unstructured {
	metadata := map[string]any{
		"name":            "payments-api",
		"namespace":       "payments",
		"resourceVersion": "12345",
		"generation":      int64(7),
		"uid":             "abc-123",
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   metadata,
		"spec":       map[string]any{"replicas": replicas},
		"status":     map[string]any{"readyReplicas": replicas},
	}}
}

func set(t *testing.T, obj *unstructured.Unstructured, value any, path ...string) {
	t.Helper()

	if err := unstructured.SetNestedField(obj.Object, value, path...); err != nil {
		t.Fatalf("set %v: %v", path, err)
	}
}

func rendered(lines []DiffLine, kind string) []string {
	out := []string{}
	for _, line := range lines {
		if line.Kind == kind {
			out = append(out, strings.TrimSpace(line.Text))
		}
	}
	return out
}

func TestDiffShowsTheChangedField(t *testing.T) {
	lines := Diff(deployment(3, nil), deployment(5, nil))

	if got := rendered(lines, "removed"); len(got) != 1 || got[0] != "replicas: 3" {
		t.Fatalf("removed = %v", got)
	}
	if got := rendered(lines, "added"); len(got) != 1 || got[0] != "replicas: 5" {
		t.Fatalf("added = %v", got)
	}
}

// Showing resourceVersion and generation turns every diff into noise about fields nobody
// wrote, which is how people stop reading diffs at all.
func TestDiffIgnoresFieldsTheServerOwns(t *testing.T) {
	before := deployment(3, nil)
	after := deployment(3, nil)
	after.SetResourceVersion("99999")
	set(t, after, int64(9), "metadata", "generation")
	set(t, after, int64(1), "status", "readyReplicas")

	if lines := Diff(before, after); len(lines) != 0 {
		t.Fatalf("diff = %+v, want nothing: only server-owned fields changed", lines)
	}
}

// A creation has nothing to compare against, so every line is new.
func TestDiffAgainstNothingIsAllAdded(t *testing.T) {
	lines := Diff(nil, deployment(1, nil))

	if len(lines) == 0 {
		t.Fatal("creating an object produced no diff")
	}
	for _, line := range lines {
		if line.Kind != "added" {
			t.Fatalf("line %+v should be added", line)
		}
	}
}

func TestDiffKeepsSurroundingContext(t *testing.T) {
	lines := Diff(deployment(3, nil), deployment(5, nil))

	if len(rendered(lines, "context")) == 0 {
		t.Fatal("a diff with no context lines cannot be read")
	}
}

// The last-applied annotation is a copy of the manifest; diffing it doubles every change.
func TestDiffIgnoresTheLastAppliedAnnotation(t *testing.T) {
	before := deployment(3, nil)
	after := deployment(3, nil)
	after.SetAnnotations(map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": `{"spec":{"replicas":3}}`,
	})

	if lines := Diff(before, after); len(lines) != 0 {
		t.Fatalf("diff = %+v, want nothing", lines)
	}
}
