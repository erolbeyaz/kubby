// Package health finds what is wrong in a cluster without being asked where to look.
package health

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Severity orders findings by how much they demand attention.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Category groups findings so the panel reads as a short list of themes rather than a
// long list of objects.
const (
	CategoryWorkload = "workload"
	CategorySidecar  = "sidecar"
	CategoryNode     = "node"
	CategoryBatch    = "batch"
	CategoryStorage  = "storage"
	CategoryEvent    = "event"
	CategoryCert     = "certificate"
)

// Reader is the slice of the cluster a detector needs. Keeping it this narrow is what
// lets every detector be tested against a fake instead of an API server.
type Reader interface {
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error)
}

// Finding is one thing that is wrong, and enough context to act on it.
type Finding struct {
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	// Reason is the machine word for the problem: CrashLoopBackOff, OOMKilled, Unschedulable.
	Reason string `json:"reason"`
	// Detail is the sentence a human reads instead of opening the object.
	Detail string `json:"detail"`
	// Container names the container at fault, where one is.
	Container string `json:"container,omitempty"`
	// Count collapses repetition: the same problem seen many times is one row and a number.
	Count int `json:"count,omitempty"`
	// Age is when the problem was last observed, RFC 3339 UTC. Conversion to local time
	// happens in the browser (ADR-026).
	LastSeen string `json:"lastSeen,omitempty"`
	// TypeKey deep-links the finding to the object's own view.
	TypeKey string `json:"typeKey,omitempty"`
}

// Detector examines one aspect of a cluster.
type Detector interface {
	Name() string
	Detect(ctx context.Context, r Reader) ([]Finding, error)
}

// Report is what the panel renders.
type Report struct {
	Findings []Finding `json:"findings"`
	// Failed names detectors that could not run. A detector that fails must not hide the
	// ones that succeeded: a partial answer is still the difference between looking in
	// the right place and hunting.
	Failed map[string]string `json:"failed,omitempty"`
	// Counts is the per-severity tally the sidebar badge reads.
	Counts map[string]int `json:"counts"`
}

var severityRank = map[string]int{SeverityCritical: 0, SeverityWarning: 1, SeverityInfo: 2}

// Sort orders findings the way they should be read: worst first, then by kind and name so
// the list is stable between refreshes and the eye can keep its place.
func Sort(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] < severityRank[b.Severity]
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
}
