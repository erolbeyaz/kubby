package cluster

import (
	"context"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/erolbeyaz/kubby/internal/store"
)

// ChangeType is what happened to an object.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
	// ChangeReset means the watch restarted and what the client holds is no longer
	// trustworthy: it should list again rather than patch what it has.
	ChangeReset ChangeType = "reset"
)

// Change is one thing that happened, already projected into a row.
type Change struct {
	Type ChangeType `json:"type"`
	Row  *Row       `json:"row,omitempty"`
	// Namespace and Name identify a deleted object, whose row is not sent.
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	// Reason explains a reset.
	Reason string `json:"reason,omitempty"`
}

// WatchRequest is what to follow.
type WatchRequest struct {
	Type      ResourceType
	Namespace string
}

// Watch follows a kind and sends each change as a projected row.
//
// Rows rather than raw objects, exactly as the list does (ADR-004): a client that
// receives a projection on first load and a raw object on every update would need two
// readers for the same thing, and the second would drift.
//
// The channel closes when the context ends. A watch that the API server drops is
// restarted, and the client is told to list again first: a gap in a watch is invisible,
// and patching a list across one leaves rows that quietly no longer exist.
func (s *Service) Watch(ctx context.Context, cluster *store.Cluster, req WatchRequest, impersonate *ImpersonationConfig) (<-chan Change, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	// The same measurements the list attaches. Without them a watch event replaces a row
	// with one that has no CPU or memory, so the figures vanish the moment anything about
	// the pod changes and only return on the next full refetch.
	feed := s.startUsageFeed(ctx, cluster, req, impersonate)

	out := make(chan Change, watchBuffer)

	go func() {
		defer close(out)

		for {
			watcher, err := client.Resource(req.Type.GVR()).Namespace(req.Namespace).
				Watch(ctx, metav1.ListOptions{})
			if err != nil {
				send(ctx, out, Change{Type: ChangeReset, Reason: translateAPIError(err, req.Type).Error()})
				if !sleep(ctx, watchRetryDelay) {
					return
				}
				continue
			}

			if !drain(ctx, watcher, out, req.Type.Kind, feed) {
				watcher.Stop()
				return
			}
			watcher.Stop()

			// The API server ends watches routinely — a resourceVersion ages out, a
			// connection is recycled. Reconnecting is normal; pretending the client's
			// list survived it is not.
			send(ctx, out, Change{Type: ChangeReset, Reason: "the watch was restarted"})
			if !sleep(ctx, watchRetryDelay) {
				return
			}
		}
	}()

	return out, nil
}

// drain forwards events until the watch ends. It reports whether to reconnect.
func drain(ctx context.Context, watcher watch.Interface, out chan<- Change, kind string, feed *usageFeed) bool {
	now := time.Now()

	for {
		select {
		case <-ctx.Done():
			return false
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return true
			}

			object, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				// A watch error arrives as a Status object rather than the kind asked
				// for; there is nothing to project and the safe answer is to relist.
				send(ctx, out, Change{Type: ChangeReset, Reason: "the cluster interrupted the watch"})
				return true
			}

			change := Change{Namespace: object.GetNamespace(), Name: object.GetName()}
			switch event.Type {
			case watch.Added:
				change.Type = ChangeAdded
			case watch.Modified:
				change.Type = ChangeModified
			case watch.Deleted:
				change.Type = ChangeDeleted
			case watch.Bookmark:
				continue
			default:
				send(ctx, out, Change{Type: ChangeReset, Reason: "the cluster interrupted the watch"})
				return true
			}

			if change.Type != ChangeDeleted {
				row := Project(kind, object, now)
				feed.applyTo(&row)
				change.Row = &row
			}
			if !send(ctx, out, change) {
				return false
			}
		}
	}
}

func send(ctx context.Context, out chan<- Change, change Change) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- change:
		return true
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

const (
	// watchBuffer absorbs a burst — a rollout touches every pod at once — without making
	// the watch wait on a slow reader.
	watchBuffer = 256
	// watchRetryDelay keeps a cluster that refuses watches from being asked in a loop.
	watchRetryDelay = 2 * time.Second
)

// usageFeed keeps the latest measurements for a watched kind.
//
// metrics-server publishes on its own schedule, unrelated to when an object changes, so
// the two are followed separately and joined here. A nil feed simply means this kind has
// no measurements, or the cluster has no metrics-server.
type usageFeed struct {
	mu     sync.RWMutex
	values map[string]Usage
}

// startUsageFeed refreshes measurements for as long as the watch runs.
func (s *Service) startUsageFeed(ctx context.Context, cluster *store.Cluster, req WatchRequest, impersonate *ImpersonationConfig) *usageFeed {
	if !supportsUsage(req.Type.Kind) || !cluster.MetricsAvailable {
		return nil
	}

	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil
	}

	feed := &usageFeed{}
	gvr := metricsGVRFor(req.Type.Kind)

	// Once before the first event, so the very first change already carries figures.
	feed.set(fetchUsage(ctx, cfg, gvr, req.Namespace))

	go func() {
		ticker := time.NewTicker(usageRefresh)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				feed.set(fetchUsage(ctx, cfg, gvr, req.Namespace))
			}
		}
	}()

	return feed
}

func (f *usageFeed) set(values map[string]Usage) {
	if values == nil {
		// Metrics-server being briefly unreachable is not a reason to forget what was
		// measured a moment ago; the alternative is the figures blinking out again.
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values = values
}

// applyTo fills a projected row's usage fields, leaving an em dash where nothing was
// measured rather than a zero, which would claim the pod uses nothing.
func (f *usageFeed) applyTo(row *Row) {
	if f == nil {
		return
	}
	f.mu.RLock()
	measured, ok := f.values[usageKey(row.Namespace, row.Name)]
	known := f.values != nil
	f.mu.RUnlock()

	if !known {
		return
	}
	if !ok {
		row.Fields["cpu"] = "—"
		row.Fields["memory"] = "—"
		return
	}
	row.Fields["cpu"] = measured.FormatCPU()
	row.Fields["memory"] = measured.FormatMemory()
}

// usageRefresh follows metrics-server's own resolution: asking faster returns the same
// numbers and costs the cluster a request each time.
const usageRefresh = 15 * time.Second
