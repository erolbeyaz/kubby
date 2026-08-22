package httpapi

import (
	"context"
	"net/http"
	"time"
)

// Pinger is the readiness dependency contract. store.DB satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
	Detail string            `json:"detail,omitempty"`
}

// handleLive answers the liveness probe. It must not touch dependencies: a database
// outage should not cause the orchestrator to restart an otherwise healthy process.
func handleLive() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	}
}

// handleReady answers the readiness probe and reports 503 while a dependency is down.
func handleReady(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, healthResponse{
				Status: "unavailable",
				Checks: map[string]string{"database": "unreachable"},
				Detail: "database is not reachable",
			})
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{
			Status: "ok",
			Checks: map[string]string{"database": "ok"},
		})
	}
}
