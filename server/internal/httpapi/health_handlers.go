package httpapi

import (
	"context"
	"fmt"
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

// clusterHealth sweeps one cluster for everything that is wrong.
func (h *resourceHandlers) clusterHealth(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	report, err := h.svc.Health(r.Context(), c, cluster.HealthOptions{
		Namespaces:  namespacesFrom(r.URL.Query().Get("namespace")),
		Sidecars:    h.sidecars,
		EventWindow: h.eventWindow,
	}, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// fleetHealth answers the question the user actually arrives with: not "is this cluster
// broken" but "which of my clusters is broken".
func (h *resourceHandlers) fleetHealth(w http.ResponseWriter, r *http.Request) {
	_, user := principal(r)

	// Only clusters the user has a grant on. Filtering here rather than in the browser
	// is the difference between a permission and a decoration.
	var (
		clusters []*store.Cluster
		err      error
	)
	if rbac.Role(user.Role) == rbac.RoleAdmin {
		clusters, err = h.clusters.List(r.Context())
	} else {
		clusters, err = h.clusters.ListForUser(r.Context(), user.ID)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list clusters")
		return
	}

	targets := make([]health.FleetTarget, 0, len(clusters))
	byID := map[string]*store.Cluster{}
	for _, c := range clusters {
		targets = append(targets, health.FleetTarget{
			ID:          c.ID.String(),
			Name:        c.Name,
			Environment: c.DisplayEnvironment(),
			Colour:      c.Color,
			Status:      c.CredentialStatus,
		})
		byID[c.ID.String()] = c
	}

	// A cluster whose stored credential is already known not to work has nothing to
	// sweep; attempting the connection would spend the whole per-cluster deadline to
	// learn what the row already says.
	h.fleet.Sweep = func(ctx context.Context, target health.FleetTarget) (*health.Report, error) {
		c := byID[target.ID]
		if c.CredentialStatus != cluster.StatusValid {
			return nil, fmt.Errorf("the stored credential is %s", c.CredentialStatus)
		}
		return h.svc.Health(ctx, c, cluster.HealthOptions{
			Sidecars:    h.sidecars,
			EventWindow: h.eventWindow,
		}, impersonationFor(r, c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clusters": h.fleet.Cards(r.Context(), targets)})
}

// workloadsOverview counts what is running and shows what has been happening to it.
func (h *resourceHandlers) workloadsOverview(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	overview, err := h.svc.WorkloadsOverview(r.Context(), c,
		namespacesFrom(r.URL.Query().Get("namespace")), impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// relations answers what an object is part of, and what is part of it.
func (h *resourceHandlers) relations(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	resourceType, err := cluster.LookupType(strings.Trim(chi.URLParam(r, "*"), "/"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}

	relations, err := h.svc.Relations(r.Context(), c, resourceType,
		r.URL.Query().Get("namespace"), name, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relations": relations})
}

// ---------------------------------------------------------------- secrets

// secretKeys lists a secret's keys and sizes. Values are never included here; disclosure
// is a separate, audited request (ADR-057).
func (h *resourceHandlers) secretKeys(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name, ok := secretTarget(w, r)
	if !ok {
		return
	}

	keys, err := h.svc.SecretKeys(r.Context(), c, namespace, name, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// revealSecret discloses one key's value.
//
// Every disclosure is written to the audit stream before the value leaves the process:
// the record says who read which key of which secret, and never what it said. A tool
// with cluster-wide credentials that let secrets be read without a trace would be worse
// than no tool.
func (h *resourceHandlers) revealSecret(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name, ok := secretTarget(w, r)
	if !ok {
		return
	}
	key := r.URL.Query().Get("key")

	value, err := h.svc.RevealSecret(r.Context(), c, namespace, name, key, impersonationFor(r, c))
	if err != nil {
		h.recordReveal(r, c, namespace, name, key, audit.ResultError)
		writeResourceError(w, r, err)
		return
	}

	h.recordReveal(r, c, namespace, name, key, audit.ResultSuccess)
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": value})
}

func (h *resourceHandlers) recordReveal(r *http.Request, c *store.Cluster, namespace, name, key, result string) {
	if h.audit == nil {
		return
	}
	_, user := principal(r)

	h.audit.Record(r.Context(), audit.Event{
		Action:       audit.ActionSecretRevealed,
		Result:       result,
		ActorID:      &user.ID,
		ActorEmail:   user.Email,
		ClusterID:    &c.ID,
		Namespace:    namespace,
		ResourceKind: "Secret",
		ResourceName: name,
		// The key, never the value: audit records access, not content.
		Details: map[string]any{"key": key},
	})
}

func secretTarget(w http.ResponseWriter, r *http.Request) (namespace, name string, ok bool) {
	namespace = chi.URLParam(r, "namespace")
	name = chi.URLParam(r, "name")

	if namespace == "" || name == "" {
		writeError(w, r, http.StatusBadRequest, "namespace and name are required")
		return "", "", false
	}
	return namespace, name, true
}

// defaultEventWindow is how far back the health panel reads warning events.
const defaultEventWindow = time.Hour
