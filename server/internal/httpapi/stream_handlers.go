package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/erolbeyaz/kubby/internal/cluster"
)

// heartbeat keeps an idle stream from being closed by a proxy that sees silence as death.
const heartbeat = 20 * time.Second

// streamResources sends changes to a kind as server-sent events.
//
// SSE rather than a WebSocket (ADR-004): this is one-way, and SSE reconnects on its own,
// survives HTTP/2 multiplexing and needs no framing of its own. The interactive streams —
// logs, exec — are WebSockets because they are conversations.
func (h *resourceHandlers) streamResources(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	resourceType, err := cluster.LookupType(strings.Trim(chi.URLParam(r, "*"), "/"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "streaming is not supported here")
		return
	}

	namespaces := namespacesFrom(r.URL.Query().Get("namespace"))
	namespace := ""
	if len(namespaces) == 1 {
		namespace = namespaces[0]
	}

	changes, err := h.svc.Watch(r.Context(), c,
		cluster.WatchRequest{Type: resourceType, Namespace: namespace}, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Nothing between here and the browser may buffer this; a proxy that collects a
	// stream into a response turns live updates into a very slow list.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-ticker.C:
			// A comment line: legal SSE, ignored by the client, and enough traffic to
			// keep an idle connection from being reaped.
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()

		case change, open := <-changes:
			if !open {
				return
			}
			// Several namespaces are filtered here rather than at the API server, which
			// watches one namespace or all of them and nothing in between.
			if !matchesNamespaces(namespaces, change) {
				continue
			}

			payload, err := json.Marshal(change)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func matchesNamespaces(namespaces []string, change cluster.Change) bool {
	if len(namespaces) <= 1 || change.Type == cluster.ChangeReset {
		return true
	}
	for _, namespace := range namespaces {
		if namespace == change.Namespace {
			return true
		}
	}
	return false
}
