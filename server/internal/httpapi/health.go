package httpapi

import (
	"context"
	"net/http"
	"strconv"
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
// SchemaReader reports the applied schema version, so readiness can tell a reachable
// database from a usable one.
type SchemaReader interface {
	SchemaVersion(ctx context.Context) (int64, error)
}

func handleReady(db Pinger, schema SchemaReader) http.HandlerFunc {
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

		// Reachable is not the same as usable. An empty database answers a ping perfectly
		// well, and a readiness probe that says yes to one is worse than no probe at all:
		// it tells the orchestrator to send traffic to a server that cannot answer a
		// single request.
		if schema != nil {
			version, err := schema.SchemaVersion(ctx)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, healthResponse{
					Status: "unavailable",
					Checks: map[string]string{"database": "ok", "schema": "missing"},
					Detail: "the database has no schema; migrations have not been applied",
				})
				return
			}
			writeJSON(w, http.StatusOK, healthResponse{
				Status: "ok",
				Checks: map[string]string{
					"database": "ok",
					"schema":   "v" + strconv.FormatInt(version, 10),
				},
			})
			return
		}

		writeJSON(w, http.StatusOK, healthResponse{
			Status: "ok",
			Checks: map[string]string{"database": "ok"},
		})
	}
}
