package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/google/uuid"
)

// resyncPeriod is how often informers re-list. Watches deliver changes; the resync is
// a safety net against a missed event, not the primary path.
const resyncPeriod = 10 * time.Minute

// stripServerFields removes what Kubby never renders but Kubernetes always sends.
//
// managedFields and the last-applied-configuration annotation are typically 30-50% of an
// object's size (ADR-019). Dropping them before the cache stores anything is the single
// largest saving available, and it costs nothing because no screen reads them.
func stripServerFields(obj any) (any, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		// Tombstones and unexpected shapes pass through untouched rather than failing
		// the whole informer.
		return obj, nil
	}

	accessor.SetManagedFields(nil)
	if annotations := accessor.GetAnnotations(); annotations != nil {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		accessor.SetAnnotations(annotations)
	}
	return obj, nil
}

// clusterCache holds the informers for one cluster.
type clusterCache struct {
	factory   dynamicinformer.DynamicSharedInformerFactory
	informers map[schema.GroupVersionResource]cache.SharedIndexInformer
	stop      chan struct{}
	lastUsed  time.Time
	warming   bool
}

// InformerPool keeps a warm cache per cluster and lets idle ones go.
//
// Caching every cluster forever would put the memory of every connected cluster in one
// process; caching nothing would hammer the API server on every screen. The pool keeps
// hot kinds for clusters in active use and releases them after an idle period.
type InformerPool struct {
	mu       sync.Mutex
	caches   map[uuid.UUID]*clusterCache
	idleTTL  time.Duration
	logger   *slog.Logger
	stopOnce sync.Once
	stop     chan struct{}
}

func NewInformerPool(idleTTL time.Duration, logger *slog.Logger) *InformerPool {
	pool := &InformerPool{
		caches:  make(map[uuid.UUID]*clusterCache),
		idleTTL: idleTTL,
		logger:  logger.With(slog.String("component", "informer-pool")),
		stop:    make(chan struct{}),
	}
	go pool.reapIdle()
	return pool
}

// Warm ensures a cluster's hot informers are running and synced.
//
// It reports whether the cache was already warm, so a caller can tell the user that a
// first load is populating rather than simply slow.
func (p *InformerPool) Warm(ctx context.Context, clusterID uuid.UUID, cfg *rest.Config) (alreadyWarm bool, err error) {
	p.mu.Lock()
	if existing, ok := p.caches[clusterID]; ok {
		existing.lastUsed = time.Now()
		warm := !existing.warming
		p.mu.Unlock()
		return warm, nil
	}

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		p.mu.Unlock()
		return false, fmt.Errorf("create dynamic client: %w", err)
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		client, resyncPeriod, metav1.NamespaceAll, nil)

	entry := &clusterCache{
		factory:   factory,
		informers: make(map[schema.GroupVersionResource]cache.SharedIndexInformer),
		stop:      make(chan struct{}),
		lastUsed:  time.Now(),
		warming:   true,
	}
	p.caches[clusterID] = entry
	p.mu.Unlock()

	for _, resourceType := range HotTypes() {
		gvr := resourceType.GVR()
		informer := factory.ForResource(gvr).Informer()

		if err := informer.SetTransform(stripServerFields); err != nil {
			p.logger.WarnContext(ctx, "could not install cache transform",
				slog.String("resource", gvr.String()), slog.String("error", err.Error()))
		}
		entry.informers[gvr] = informer
	}

	factory.Start(entry.stop)

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	synced := factory.WaitForCacheSync(syncCtx.Done())

	p.mu.Lock()
	entry.warming = false
	p.mu.Unlock()

	for gvr, ok := range synced {
		if !ok {
			// A kind the cluster does not serve is normal (Gateway API, for example);
			// it simply falls back to on-demand listing.
			p.logger.DebugContext(ctx, "informer did not sync",
				slog.String("resource", gvr.String()))
		}
	}
	p.logger.InfoContext(ctx, "cluster cache warmed", slog.String("cluster", clusterID.String()))
	return false, nil
}

// Cached returns objects for a type from the cache, or false when this type is not
// cached for this cluster.
func (p *InformerPool) Cached(clusterID uuid.UUID, gvr schema.GroupVersionResource, namespace string) ([]runtime.Object, bool) {
	p.mu.Lock()
	entry, ok := p.caches[clusterID]
	if ok {
		entry.lastUsed = time.Now()
	}
	p.mu.Unlock()

	if !ok || entry.warming {
		return nil, false
	}

	informer, ok := entry.informers[gvr]
	if !ok || !informer.HasSynced() {
		return nil, false
	}

	items := informer.GetStore().List()

	out := make([]runtime.Object, 0, len(items))
	for _, item := range items {
		obj, ok := item.(runtime.Object)
		if !ok {
			continue
		}
		if namespace != "" {
			accessor, err := meta.Accessor(obj)
			if err != nil || accessor.GetNamespace() != namespace {
				continue
			}
		}
		out = append(out, obj)
	}
	return out, true
}

// Release drops a cluster's cache, used when a cluster is removed or its credential
// replaced — the old client must not keep watching with a credential that is gone.
func (p *InformerPool) Release(clusterID uuid.UUID) {
	p.mu.Lock()
	entry, ok := p.caches[clusterID]
	if ok {
		delete(p.caches, clusterID)
	}
	p.mu.Unlock()

	if ok {
		close(entry.stop)
		p.logger.Info("cluster cache released", slog.String("cluster", clusterID.String()))
	}
}

// Stats reports what the pool currently holds, for /metrics and for diagnosing a
// cluster whose memory use looks wrong.
type CacheStats struct {
	ClusterID uuid.UUID
	Kinds     int
	Objects   int
	LastUsed  time.Time
	Warming   bool
}

func (p *InformerPool) Stats() []CacheStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]CacheStats, 0, len(p.caches))
	for id, entry := range p.caches {
		stat := CacheStats{
			ClusterID: id,
			Kinds:     len(entry.informers),
			LastUsed:  entry.lastUsed,
			Warming:   entry.warming,
		}
		for _, informer := range entry.informers {
			if informer.HasSynced() {
				stat.Objects += len(informer.GetStore().List())
			}
		}
		out = append(out, stat)
	}
	return out
}

// reapIdle releases caches for clusters nobody has looked at recently.
func (p *InformerPool) reapIdle() {
	if p.idleTTL <= 0 {
		return
	}
	ticker := time.NewTicker(p.idleTTL / 4)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-p.idleTTL)

			p.mu.Lock()
			var expired []uuid.UUID
			for id, entry := range p.caches {
				if entry.lastUsed.Before(cutoff) {
					expired = append(expired, id)
				}
			}
			p.mu.Unlock()

			for _, id := range expired {
				p.Release(id)
			}
		}
	}
}

// Close stops the reaper and releases every cache.
func (p *InformerPool) Close() {
	p.stopOnce.Do(func() { close(p.stop) })

	p.mu.Lock()
	ids := make([]uuid.UUID, 0, len(p.caches))
	for id := range p.caches {
		ids = append(ids, id)
	}
	p.mu.Unlock()

	for _, id := range ids {
		p.Release(id)
	}
}
