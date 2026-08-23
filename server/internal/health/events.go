package health

import (
	"context"
	"fmt"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var eventsGVR = schema.GroupVersionResource{Version: "v1", Resource: "events"}

// DefaultEventWindow is how far back warning events are read. An hour is long enough to
// still be looking at the incident and short enough that yesterday's noise is gone.
const DefaultEventWindow = time.Hour

// EventDetector reports recent warnings, one row per object and reason.
//
// The same reason repeated forty times is one problem, not forty. Listing each occurrence
// buries everything else in the panel.
type EventDetector struct {
	Namespaces []string
	Window     time.Duration
	// Now is injectable so the window is testable without waiting for time to pass.
	Now func() time.Time
}

func (d *EventDetector) Name() string { return "event" }

func (d *EventDetector) Detect(ctx context.Context, r Reader) ([]Finding, error) {
	cutoff := d.now().Add(-d.window())
	grouped := map[string]*Finding{}

	for _, namespace := range namespacesOr(d.Namespaces) {
		events, err := r.List(ctx, eventsGVR, namespace)
		if err != nil {
			return nil, err
		}
		for i := range events {
			d.collect(&events[i], cutoff, grouped)
		}
	}

	findings := make([]Finding, 0, len(grouped))
	for _, finding := range grouped {
		findings = append(findings, *finding)
	}
	// Map iteration is random; without this the panel reshuffles on every refresh.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Count != findings[j].Count {
			return findings[i].Count > findings[j].Count
		}
		return findings[i].Name < findings[j].Name
	})
	return findings, nil
}

func (d *EventDetector) collect(event *unstructured.Unstructured, cutoff time.Time, into map[string]*Finding) {
	if eventType, _, _ := unstructured.NestedString(event.Object, "type"); eventType != "Warning" {
		return
	}

	last := lastTimestamp(event)
	if last.IsZero() || last.Before(cutoff) {
		return
	}

	reason, _, _ := unstructured.NestedString(event.Object, "reason")
	message, _, _ := unstructured.NestedString(event.Object, "message")
	kind, _, _ := unstructured.NestedString(event.Object, "involvedObject", "kind")
	name, _, _ := unstructured.NestedString(event.Object, "involvedObject", "name")
	namespace, _, _ := unstructured.NestedString(event.Object, "involvedObject", "namespace")
	count, _, _ := unstructured.NestedInt64(event.Object, "count")
	if count == 0 {
		count = 1
	}

	key := fmt.Sprintf("%s/%s/%s/%s", namespace, kind, name, reason)
	existing, found := into[key]
	if !found {
		into[key] = &Finding{
			Category:  CategoryEvent,
			Severity:  SeverityWarning,
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Reason:    reason,
			Detail:    message,
			Count:     int(count),
			LastSeen:  last.UTC().Format(time.RFC3339),
		}
		return
	}

	existing.Count += int(count)
	if last.UTC().Format(time.RFC3339) > existing.LastSeen {
		existing.LastSeen = last.UTC().Format(time.RFC3339)
		existing.Detail = message
	}
}

// lastTimestamp reads whichever of the three time fields the API server filled in. Events
// written through the events.k8s.io API leave lastTimestamp empty.
func lastTimestamp(event *unstructured.Unstructured) time.Time {
	for _, field := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		value, _, _ := unstructured.NestedString(event.Object, field)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (d *EventDetector) window() time.Duration {
	if d.Window <= 0 {
		return DefaultEventWindow
	}
	return d.Window
}

func (d *EventDetector) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}
