package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/erolbeyaz/kubby/internal/cluster"
)

// logReadLimit caps a single line. A container writing one enormous line should not be
// able to exhaust the server's memory through a log viewer.
const logReadLimit = 256 * 1024

// podContainers lists a pod's containers so a log view can offer them, with the
// application container first and injected ones grouped after (ADR-030).
func (h *resourceHandlers) podContainers(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name, ok := podTarget(w, r)
	if !ok {
		return
	}

	containers, err := h.svc.PodContainers(r.Context(), c, namespace, name, h.sidecars, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
}

// podRestarts explains why a pod's containers restarted.
func (h *resourceHandlers) podRestarts(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name, ok := podTarget(w, r)
	if !ok {
		return
	}

	summary, err := h.svc.PodRestarts(r.Context(), c, namespace, name, h.sidecars, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// describe renders the same text kubectl describe prints.
func (h *resourceHandlers) describe(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	resourceType, err := cluster.LookupType(strings.Trim(chi.URLParam(r, "*"), "/"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}

	text, err := h.svc.Describe(r.Context(), c, resourceType,
		r.URL.Query().Get("namespace"), r.URL.Query().Get("name"), impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}

// podLogs streams a pod's log over a WebSocket.
//
// A WebSocket rather than a plain response because a log view is a conversation: the
// reader switches container, asks for the previous instance, or stops following, and
// each of those would otherwise be a new connection and a new place in the log.
func (h *resourceHandlers) podLogs(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name, ok := podTarget(w, r)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The connection is same-origin: the browser sends the session cookie with it,
		// so accepting a cross-origin upgrade would hand any page a reader's session.
		OriginPatterns: h.allowedOrigins,
	})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	req := logRequestFrom(r, namespace, name)
	ctx := conn.CloseRead(r.Context())

	stream, container, err := h.svc.OpenLog(ctx, c, req, h.sidecars, impersonationFor(r, c))
	if err != nil {
		_ = writeWS(ctx, conn, map[string]any{"type": "error", "message": err.Error()})
		return
	}
	defer func() { _ = stream.Close() }()

	if writeWS(ctx, conn, map[string]any{"type": "open", "container": container}) != nil {
		return
	}

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), logReadLimit)

	for scanner.Scan() {
		if writeWS(ctx, conn, map[string]any{"type": "line", "line": scanner.Text()}) != nil {
			return
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		_ = writeWS(ctx, conn, map[string]any{"type": "error", "message": err.Error()})
		return
	}
	_ = writeWS(ctx, conn, map[string]any{"type": "end"})
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func writeWS(ctx context.Context, conn *websocket.Conn, payload map[string]any) error {
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func logRequestFrom(r *http.Request, namespace, name string) cluster.LogRequest {
	query := r.URL.Query()

	req := cluster.LogRequest{
		Namespace:  namespace,
		Pod:        name,
		Container:  query.Get("container"),
		Follow:     query.Get("follow") == "true",
		Previous:   query.Get("previous") == "true",
		Timestamps: query.Get("timestamps") == "true",
		TailLines:  cluster.DefaultLogTail,
	}
	if raw := query.Get("tail"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
			req.TailLines = value
		}
	}
	if raw := query.Get("since"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
			req.SinceSeconds = value
		}
	}
	return req
}

func podTarget(w http.ResponseWriter, r *http.Request) (namespace, name string, ok bool) {
	namespace = chi.URLParam(r, "namespace")
	name = chi.URLParam(r, "name")

	if namespace == "" || name == "" {
		writeError(w, r, http.StatusBadRequest, "namespace and name are required")
		return "", "", false
	}
	return namespace, name, true
}

// originHosts is the allowlist the log WebSocket accepts upgrades from: the public URL
// and whatever else is configured, and nothing else.
func originHosts(publicURL *url.URL, allowed []*url.URL) []string {
	hosts := make([]string, 0, len(allowed)+1)
	if publicURL != nil {
		hosts = append(hosts, publicURL.Host)
	}
	for _, origin := range allowed {
		hosts = append(hosts, origin.Host)
	}
	return hosts
}
