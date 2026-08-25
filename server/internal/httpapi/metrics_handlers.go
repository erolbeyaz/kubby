package httpapi

import (
	"errors"
	"net/http"
	"time"

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

	health, err := h.svc.ClusterHealthMetrics(r.Context(), c, window)
	if errors.Is(err, promql.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	if err != nil {
		// Reachable but unhappy is worth saying out loud: a wrong address or a refused
		// credential is a thing the reader can fix, and a silent empty panel is not.
		writeJSON(w, http.StatusOK, map[string]any{"configured": true, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "health": health})
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
