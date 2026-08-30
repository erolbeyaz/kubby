package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/promql"
)

// clusterMetrics answers what Prometheus knows and the Kubernetes API does not: what the
// cluster was doing before anyone looked at it.
//
// A cluster with no Prometheus is a normal cluster, so that is reported as a state rather
// than as an error — the dashboard says what is missing and how to add it instead of
// showing a failure the reader did nothing to cause.
func (h *resourceHandlers) clusterMetrics(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	window := parseWindow(r.URL.Query().Get("window"))

	health, source, err := h.svc.ClusterHealthMetrics(r.Context(), c, window)
	if errors.Is(err, promql.ErrNotConfigured) {
		// Nothing typed, and nothing found in the cluster either. Reported as a state so
		// the panel can step aside quietly instead of showing a failure the reader did
		// not cause.
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	if err != nil {
		// Reachable but unhappy is worth saying out loud: a wrong address or a refused
		// credential is a thing the reader can fix, and a silent empty panel is not.
		body := map[string]any{"configured": true, "error": err.Error()}
		describeSource(body, source)
		writeJSON(w, http.StatusOK, body)
		return
	}

	body := map[string]any{"configured": true, "health": health}
	describeSource(body, source)
	writeJSON(w, http.StatusOK, body)
}

// parseWindow keeps the choice to a few sensible spans. An arbitrary duration from the
// query string would let one request ask Prometheus for a year at fifteen-second steps.
func parseWindow(raw string) time.Duration {
	switch raw {
	case "15m":
		return 15 * time.Minute
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

// describeSource adds where the numbers came from, and says nothing when there is nothing
// to say.
//
// An empty string would be worse than an absent field: the client validates the source
// against the three it knows, so sending "" would fail the whole response over a detail,
// and the panel would show zeros as though the cluster were empty.
func describeSource(body map[string]any, source cluster.MetricsSource) {
	if source.Kind == "" {
		return
	}
	body["source"] = source.Kind
	if source.Where != "" {
		body["endpoint"] = source.Where
	}
}

// podMetrics reads one pod's own usage over time.
//
// On demand rather than in the cluster payload: a fleet has thousands of pods and a
// reader looks at one, so this is two queries when a panel opens instead of thousands of
// series on every refresh.
func (h *resourceHandlers) podMetrics(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	window := parseWindow(r.URL.Query().Get("window"))

	usage, source, err := h.svc.PodUsageMetrics(r.Context(), c, namespace, name, window)
	if errors.Is(err, promql.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	if err != nil {
		body := map[string]any{"configured": true, "error": err.Error()}
		describeSource(body, source)
		writeJSON(w, http.StatusOK, body)
		return
	}

	body := map[string]any{"configured": true, "usage": usage}
	describeSource(body, source)
	writeJSON(w, http.StatusOK, body)
}
