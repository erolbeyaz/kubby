package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"

	"github.com/erolbeyaz/kubby/internal/store"
)

// ListRequest narrows a resource listing.
type ListRequest struct {
	Type ResourceType
	// Namespaces narrows the listing. Empty means every namespace; more than one is
	// common when a service spans a few of them and watching only one hides half the
	// picture.
	Namespaces []string
	Search     string
	SortBy     string
	Descending bool
	Limit      int
	Continue   string
}

// singleNamespace reports the one namespace to ask the API server for, or "" when the
// request spans several and has to be narrowed after listing.
func (r ListRequest) singleNamespace() string {
	if len(r.Namespaces) == 1 {
		return r.Namespaces[0]
	}
	return ""
}

func (r ListRequest) matchesNamespace(namespace string) bool {
	if len(r.Namespaces) == 0 {
		return true
	}
	for _, candidate := range r.Namespaces {
		if candidate == namespace {
			return true
		}
	}
	return false
}

// ListResult is a page of projected rows.
type ListResult struct {
	Columns []Column `json:"columns"`
	Rows    []Row    `json:"rows"`
	Total   int      `json:"total"`
	// FromCache tells the client whether this came from a warm informer, which is also
	// what distinguishes "empty" from "not loaded yet".
	FromCache bool   `json:"fromCache"`
	Warming   bool   `json:"warming,omitempty"`
	Continue  string `json:"continue,omitempty"`
	// HideName drops the name column for kinds whose own name says nothing.
	HideName bool `json:"hideName,omitempty"`
}

// List returns projected rows for a resource type.
//
// Hot kinds come from the informer cache; everything else is listed from the API server
// on demand. Either way the client receives projections, never raw objects.
func (s *Service) List(ctx context.Context, cluster *store.Cluster, req ListRequest, impersonate *ImpersonationConfig) (*ListResult, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	result := &ListResult{Columns: ColumnsFor(req.Type.Kind)}

	// Usage is fetched alongside the objects rather than per row: one call for the
	// whole list instead of one per pod.
	var usage map[string]Usage
	if supportsUsage(req.Type.Kind) && cluster.MetricsAvailable {
		usage = fetchUsage(ctx, cfg, metricsGVRFor(req.Type.Kind), req.singleNamespace())
		if usage != nil {
			result.Columns = append(result.Columns, MetricsColumns...)
		}
	}

	if req.Type.Hot && s.pool != nil {
		warm, warmErr := s.pool.Warm(ctx, cluster.ID, cfg)
		if warmErr != nil {
			return nil, warmErr
		}
		if objects, ok := s.pool.Cached(cluster.ID, req.Type.GVR(), req.singleNamespace()); ok {
			result.FromCache = true
			result.Rows = projectAll(req.Type.Kind, objects, now)
			applyUsage(result.Rows, usage)
			finishList(result, req)
			return result, nil
		}
		// The cache is still filling: fall through to a direct list so the first view
		// shows data rather than an empty table.
		result.Warming = !warm
	}

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	options := metav1.ListOptions{Continue: req.Continue}
	if req.Limit > 0 {
		options.Limit = int64(req.Limit)
	}

	var api dynamic.ResourceInterface = client.Resource(req.Type.GVR())
	if namespace := req.singleNamespace(); req.Type.Namespaced && namespace != "" {
		api = client.Resource(req.Type.GVR()).Namespace(namespace)
	}

	list, err := api.List(ctx, options)
	if err != nil {
		return nil, translateAPIError(err, req.Type)
	}

	objects := make([]runtime.Object, 0, len(list.Items))
	for i := range list.Items {
		objects = append(objects, &list.Items[i])
	}
	result.Rows = projectAll(req.Type.Kind, objects, now)
	applyUsage(result.Rows, usage)
	result.Continue = list.GetContinue()
	finishList(result, req)
	return result, nil
}

// Get returns one object as YAML-ready JSON.
//
// managedFields and the last-applied annotation are stripped here too: they are noise
// in a YAML view and are what makes an object unreadable at a glance.
func (s *Service) Get(ctx context.Context, cluster *store.Cluster, resourceType ResourceType, namespace, name string, impersonate *ImpersonationConfig) (*unstructured.Unstructured, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	var api dynamic.ResourceInterface = client.Resource(resourceType.GVR())
	if resourceType.Namespaced && namespace != "" {
		api = client.Resource(resourceType.GVR()).Namespace(namespace)
	}

	obj, err := api.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, resourceType)
	}

	obj.SetManagedFields(nil)
	if annotations := obj.GetAnnotations(); annotations != nil {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		obj.SetAnnotations(annotations)
	}
	return obj, nil
}

// Namespaces lists namespace names, which the picker needs before anything else.
func (s *Service) Namespaces(ctx context.Context, cluster *store.Cluster, impersonate *ImpersonationConfig) ([]string, error) {
	nsType, err := LookupType("namespaces")
	if err != nil {
		return nil, err
	}

	result, err := s.List(ctx, cluster, ListRequest{Type: nsType}, impersonate)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		names = append(names, row.Name)
	}
	sort.Strings(names)
	return names, nil
}

// ReleaseCache drops a cluster's informers, used when its credential changes or it is
// removed: a cache built with a credential that no longer applies must not linger.
func (s *Service) ReleaseCache(clusterID uuid.UUID) {
	if s.pool != nil {
		s.pool.Release(clusterID)
	}
	s.discovery.forget(clusterID)
}

// CacheStats exposes what the informer pool holds.
func (s *Service) CacheStats() []CacheStats {
	if s.pool == nil {
		return nil
	}
	return s.pool.Stats()
}

// applyUsage merges measurements into rows. Rows with no measurement keep an em dash
// rather than a zero, which would claim the pod uses nothing.
func applyUsage(rows []Row, usage map[string]Usage) {
	if usage == nil {
		return
	}
	for i := range rows {
		measured, ok := usage[usageKey(rows[i].Namespace, rows[i].Name)]
		if !ok {
			rows[i].Fields["cpu"] = "—"
			rows[i].Fields["memory"] = "—"
			continue
		}
		rows[i].Fields["cpu"] = measured.FormatCPU()
		rows[i].Fields["memory"] = measured.FormatMemory()
	}
}

func projectAll(kind string, objects []runtime.Object, now time.Time) []Row {
	rows := make([]Row, 0, len(objects))
	for _, obj := range objects {
		typed, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		rows = append(rows, Project(kind, typed, now))
	}
	return rows
}

// finishList applies namespace narrowing, search and sorting, all server-side so the
// client never has to hold the full set to filter it.
func finishList(result *ListResult, req ListRequest) {
	if len(req.Namespaces) > 1 {
		kept := result.Rows[:0]
		for _, row := range result.Rows {
			if req.matchesNamespace(row.Namespace) {
				kept = append(kept, row)
			}
		}
		result.Rows = kept
	}

	if search := strings.ToLower(strings.TrimSpace(req.Search)); search != "" {
		filtered := result.Rows[:0]
		for _, row := range result.Rows {
			if matchesSearch(row, search) {
				filtered = append(filtered, row)
			}
		}
		result.Rows = filtered
	}

	sortBy, descending := req.SortBy, req.Descending
	if sortBy == "" && req.Type.DefaultSort != "" {
		// An event list read oldest-first is a list nobody wants: what just happened is
		// the reason the screen was opened.
		sortBy, descending = req.Type.DefaultSort, req.Type.DefaultSortDescending
	}

	sortRows(result.Rows, sortBy, descending)
	result.HideName = HidesName(req.Type.Kind)
	result.Total = len(result.Rows)
}

func matchesSearch(row Row, search string) bool {
	if strings.Contains(strings.ToLower(row.Name), search) ||
		strings.Contains(strings.ToLower(row.Namespace), search) {
		return true
	}
	for _, value := range row.Fields {
		if strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}

func sortRows(rows []Row, sortBy string, descending bool) {
	less := func(i, j int) bool {
		switch sortBy {
		case "", "name":
			if rows[i].Namespace != rows[j].Namespace {
				return rows[i].Namespace < rows[j].Namespace
			}
			return rows[i].Name < rows[j].Name
		case "lastSeen":
			return rows[i].Fields["lastSeen"] < rows[j].Fields["lastSeen"]
		case "age":
			// Newest first means the most recently created, so compare timestamps
			// rather than the rendered age string.
			return rows[i].CreatedAt > rows[j].CreatedAt
		default:
			left, right := rows[i].Fields[sortBy], rows[j].Fields[sortBy]
			if left == right {
				return rows[i].Name < rows[j].Name
			}
			return left < right
		}
	}

	if descending {
		sort.SliceStable(rows, func(i, j int) bool { return less(j, i) })
		return
	}
	sort.SliceStable(rows, less)
}

// translateAPIError turns a Kubernetes error into something a user can act on.
func translateAPIError(err error, resourceType ResourceType) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%w: %s", ErrResourceNotFound, resourceType.Kind)
	case apierrors.IsForbidden(err):
		return fmt.Errorf("%w: the cluster credential may not list %s", ErrClusterForbidden, resourceType.Resource)
	case apierrors.IsUnauthorized(err):
		return fmt.Errorf("%w: the cluster credential was rejected", ErrCredentialRejected)
	case meta_IsNoMatchError(err):
		// A kind the cluster does not serve at all, such as Gateway API on a cluster
		// without those CRDs. Not an error the user caused.
		return fmt.Errorf("%w: %s is not served by this cluster", ErrKindUnavailable, resourceType.Kind)
	default:
		return err
	}
}

func meta_IsNoMatchError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "could not find the requested resource")
}
