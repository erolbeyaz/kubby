package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/health"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// listLimit caps a single page. Large clusters are paged; the informer path returns the
// whole cached set, which is already bounded by what the cluster holds.
const defaultListLimit = 1000

type resourceHandlers struct {
	svc      *cluster.Service
	clusters *store.ClusterRepo
	audit    *audit.Emitter
	// fleet caches per-cluster sweeps so a fleet of twenty clusters does not mean twenty
	// concurrent sweeps on every page load.
	fleet       *health.Fleet
	sidecars    []string
	eventWindow time.Duration
	// allowedOrigins gates the log WebSocket. The browser sends the session cookie with
	// the upgrade, so accepting a cross-origin one would hand any page a reader's session.
	allowedOrigins []string
	// readOnly is the deployment-wide kill switch: when it is on, nothing writes.
	readOnly bool
}

// resolveCluster loads the cluster in the URL and enforces read access. Sharing this
// with the cluster handlers would couple two unrelated route groups, so the rule is
// repeated deliberately and kept identical: an ungranted cluster reports as missing.
func (h *resourceHandlers) resolveCluster(w http.ResponseWriter, r *http.Request) (*store.Cluster, bool) {
	_, user := principal(r)

	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}

	c, err := h.clusters.ByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "cluster not found")
		return nil, false
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not load the cluster")
		return nil, false
	}

	if !rbac.Role(user.Role).Can(rbac.PermClusterManage) {
		level, err := h.clusters.AccessLevel(r.Context(), user.ID, c.ID)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not check cluster access")
			return nil, false
		}
		if level == "" {
			writeError(w, r, http.StatusNotFound, "cluster not found")
			return nil, false
		}
	}
	return c, true
}

// impersonationFor maps the signed-in user onto a Kubernetes identity when the cluster
// is configured for it, so the cluster's own audit log names a person rather than a
// service account (ADR-005).
func impersonationFor(r *http.Request, c *store.Cluster) *cluster.ImpersonationConfig {
	if !c.ImpersonationEnabled {
		return nil
	}
	_, user := principal(r)
	if user == nil {
		return nil
	}
	return &cluster.ImpersonationConfig{Username: user.Email}
}

// ---------------------------------------------------------------- catalogue

type resourceTypeResponse struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Group      string `json:"group,omitempty"`
	Namespaced bool   `json:"namespaced"`
	Category   string `json:"category"`
	Cached     bool   `json:"cached"`
}

// namespacesFrom splits the comma-separated namespace parameter. Empty entries are
// dropped so a trailing comma does not become a request for the "" namespace.
func namespacesFrom(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (h *resourceHandlers) listTypes(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	// Only what this cluster actually serves: offering Gateway API on a cluster without
	// those CRDs turns a missing feature into a confusing error.
	types := h.svc.AvailableTypes(r.Context(), c, impersonationFor(r, c))
	out := make([]resourceTypeResponse, 0, len(types))
	for _, t := range types {
		out = append(out, resourceTypeResponse{
			Key: t.Key(), Kind: t.Kind, Group: t.Group,
			Namespaced: t.Namespaced, Category: string(t.Category), Cached: t.Hot,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"types": out})
}

func (h *resourceHandlers) listNamespaces(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	names, err := h.svc.Namespaces(r.Context(), c, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": names})
}

func (h *resourceHandlers) overview(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	overview, err := h.svc.ClusterOverview(r.Context(), c, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// ---------------------------------------------------------------- listing

func (h *resourceHandlers) list(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	// The type key contains a slash for grouped resources ("apps/deployments"), so it
	// arrives as a wildcard rather than a path parameter.
	key := strings.Trim(chi.URLParam(r, "*"), "/")
	resourceType, err := cluster.LookupType(key)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}

	query := r.URL.Query()
	limit := atoiOr(query.Get("limit"), defaultListLimit)
	if limit <= 0 || limit > 5000 {
		limit = defaultListLimit
	}

	result, err := h.svc.List(r.Context(), c, cluster.ListRequest{
		Type:       resourceType,
		Namespaces: namespacesFrom(query.Get("namespace")),
		Search:     query.Get("search"),
		SortBy:     query.Get("sort"),
		Descending: query.Get("desc") == "true",
		Limit:      limit,
		Continue:   query.Get("continue"),
	}, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------- detail

func (h *resourceHandlers) get(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	key := strings.Trim(chi.URLParam(r, "*"), "/")
	resourceType, err := cluster.LookupType(key)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}

	query := r.URL.Query()
	name := query.Get("name")
	if name == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}

	obj, err := h.svc.Get(r.Context(), c, resourceType, query.Get("namespace"), name, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, obj.Object)
}

// ---------------------------------------------------------------- errors

// writeResourceError keeps the distinction the cluster layer drew: what the credential
// may not do, what the cluster does not serve, and what simply is not there.
func writeResourceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, cluster.ErrResourceNotFound):
		writeError(w, r, http.StatusNotFound, err.Error())
	case errors.Is(err, cluster.ErrKindUnavailable):
		writeError(w, r, http.StatusNotFound, err.Error())
	case errors.Is(err, cluster.ErrClusterForbidden):
		writeError(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, cluster.ErrCredentialRejected):
		// Not the user's problem: the stored credential stopped working, and the
		// cluster list will already be showing it as invalid.
		writeError(w, r, http.StatusBadGateway, err.Error())
	case errors.Is(err, cluster.ErrNoCredential):
		writeError(w, r, http.StatusBadGateway, "this cluster has no stored credential")
	default:
		writeError(w, r, http.StatusBadGateway, "could not read from the cluster: "+err.Error())
	}
}
