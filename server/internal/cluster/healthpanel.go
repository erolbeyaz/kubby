package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/erolbeyaz/kubby/internal/health"
	"github.com/erolbeyaz/kubby/internal/k8s"
	"github.com/erolbeyaz/kubby/internal/store"
)

// dynamicReader adapts the dynamic client to what detectors need, preferring the
// informer cache so a health sweep does not re-list what is already in memory.
type dynamicReader struct {
	client    dynamic.Interface
	pool      *InformerPool
	clusterID uuid.UUID
}

func (r dynamicReader) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	if items, ok := r.cached(gvr, namespace); ok {
		return items, nil
	}

	list, err := r.client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}
	return list.Items, nil
}

// cached serves the sweep from the informer cache where one is already running. A health
// sweep touches pods and events, which are the two the pool is most likely to hold, and
// re-listing them on every refresh is load the API server does not need.
func (r dynamicReader) cached(gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, bool) {
	if r.pool == nil {
		return nil, false
	}
	objects, ok := r.pool.Cached(r.clusterID, gvr, namespace)
	if !ok {
		return nil, false
	}

	items := make([]unstructured.Unstructured, 0, len(objects))
	for _, object := range objects {
		typed, ok := object.(*unstructured.Unstructured)
		if !ok {
			return nil, false
		}
		items = append(items, *typed)
	}
	return items, true
}

// HealthOptions narrows a sweep.
type HealthOptions struct {
	Namespaces []string
	Sidecars   []string
	// EventWindow is how far back warning events are read.
	EventWindow time.Duration
}

// Health sweeps a cluster for everything that is wrong.
func (s *Service) Health(ctx context.Context, cluster *store.Cluster, opts HealthOptions, impersonate *ImpersonationConfig) (*health.Report, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	classifier := k8s.NewClassifier(opts.Sidecars)
	collector := &health.Collector{
		Reader: dynamicReader{client: client, pool: s.pool, clusterID: cluster.ID},
		Detectors: []health.Detector{
			&health.WorkloadDetector{Containers: classifier, Namespaces: opts.Namespaces},
			&health.NodeDetector{},
			&health.BatchDetector{Namespaces: opts.Namespaces},
			&health.StorageDetector{Namespaces: opts.Namespaces},
			&health.EventDetector{Namespaces: opts.Namespaces, Window: opts.EventWindow},
			&health.CertificateDetector{Namespaces: opts.Namespaces},
		},
	}
	return collector.Collect(ctx), nil
}
