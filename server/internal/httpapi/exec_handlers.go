package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/store"
)

// The frames a terminal exchanges. Text in both directions, tagged, because a terminal
// carries three different things — keystrokes, output and window sizes — over one socket.
type execFrame struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	// Name carries an uploaded file's path, relative to the session's own directory.
	// Only the terminal reads these; a pod shell has nowhere to put a file.
	Name string `json:"name,omitempty"`
}

// podShell opens an interactive session inside a container.
func (h *resourceHandlers) podShell(w http.ResponseWriter, r *http.Request) {
	namespace, name, ok := podTarget(w, r)
	if !ok {
		return
	}

	// A shell is a write to the cluster in every sense that matters, so it goes through
	// the same gate as one: the kill switch, the role, the cluster's lock, and the
	// cluster's own answer for pods/exec.
	podType, err := cluster.LookupType("pods")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "pods are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, podType, namespace, name, cluster.VerbCreate)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.allowedOrigins})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	h.runShell(r, conn, ctx.cluster, cluster.ExecRequest{
		Namespace: namespace,
		Pod:       name,
		Container: r.URL.Query().Get("container"),
	}, audit.ActionPodShellOpened, name)
}

// debugShell attaches a container with a shell to a pod that has none, then opens a
// session in it.
//
// It is a separate route from the ordinary shell because it is a separate decision: an
// ephemeral container cannot be removed while the pod lives, so it is never started as a
// silent fallback.
func (h *resourceHandlers) debugShell(w http.ResponseWriter, r *http.Request) {
	namespace, name, ok := podTarget(w, r)
	if !ok {
		return
	}

	podType, err := cluster.LookupType("pods")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "pods are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, podType, namespace, name, cluster.VerbCreate)
	if !ok {
		return
	}

	config, err := h.settings.All(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read the debug image setting")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.allowedOrigins})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	target := r.URL.Query().Get("container")
	container, err := h.svc.StartDebugContainer(r.Context(), ctx.cluster, namespace, name, target,
		config.PodDebug.Image, impersonationFor(r, ctx.cluster))
	if err != nil {
		h.recordWrite(r, ctx, audit.ActionDebugContainerStarted, namespace, name, audit.ResultError, nil)
		_ = writeWS(r.Context(), conn, map[string]any{"type": "error", "data": err.Error()})
		return
	}
	h.recordWrite(r, ctx, audit.ActionDebugContainerStarted, namespace, name, audit.ResultSuccess, nil)

	h.runShell(r, conn, ctx.cluster, cluster.ExecRequest{
		Namespace: namespace,
		Pod:       name,
		Container: container,
	}, audit.ActionPodShellOpened, name)
}

// nodeShell starts a privileged pod on a node and opens a session in it.
func (h *resourceHandlers) nodeShell(w http.ResponseWriter, r *http.Request) {
	node := chi.URLParam(r, "name")

	nodeType, err := cluster.LookupType("nodes")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "nodes are not registered")
		return
	}
	// Root on the machine. Admin-only, and refused on a cluster locked read-only.
	ctx, ok := h.authoriseWrite(w, r, nodeType, "", node, cluster.VerbPatch)
	if !ok {
		return
	}

	config, err := h.settings.All(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read the node shell settings")
		return
	}
	if !config.NodeShell.Enabled {
		writeError(w, r, http.StatusForbidden,
			"node shells are turned off; an admin enables them in Kubby Settings")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.allowedOrigins})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	shellSettings := cluster.NodeShellSettings{
		Enabled:    config.NodeShell.Enabled,
		Image:      config.NodeShell.Image,
		Namespace:  config.NodeShell.Namespace,
		PullSecret: config.NodeShell.PullSecret,
	}

	namespace, pod, err := h.svc.StartNodeShell(r.Context(), ctx.cluster, node, shellSettings,
		impersonationFor(r, ctx.cluster))
	if err != nil {
		h.recordWrite(r, ctx, audit.ActionNodeShellOpened, "", node, audit.ResultError, nil)
		_ = writeWS(r.Context(), conn, map[string]any{"type": "error", "data": err.Error()})
		return
	}

	// Whatever happens next, the privileged pod goes. A shell left running in someone's
	// cluster is a hole this tool opened.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
		defer cancel()
		_ = h.svc.StopNodeShell(cleanup, ctx.cluster, namespace, pod, impersonationFor(r, ctx.cluster))
	}()

	h.runShell(r, conn, ctx.cluster, cluster.ExecRequest{
		Namespace: namespace,
		Pod:       pod,
	}, audit.ActionNodeShellOpened, node)
}

// runShell wires a socket to a session and records what was typed.
func (h *resourceHandlers) runShell(r *http.Request, conn *websocket.Conn, c *store.Cluster, req cluster.ExecRequest, action, subject string) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stdin, stdinWriter := io.Pipe()
	resize := make(chan remotecommand.TerminalSize, 4)

	// A tool that hands out cluster-wide shells without a record of what was typed is not
	// auditable (ADR-013 #5). The transcript is written to the audit stream, not stored
	// beside the object it is about.
	recorder := h.newSessionRecorder(r, c, action, subject)

	go func() {
		defer func() {
			_ = stdinWriter.Close()
			close(resize)
			cancel()
		}()

		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var frame execFrame
			if err := json.Unmarshal(data, &frame); err != nil {
				continue
			}

			switch frame.Type {
			case "stdin":
				recorder.typed(frame.Data)
				if _, err := stdinWriter.Write([]byte(frame.Data)); err != nil {
					return
				}
			case "resize":
				select {
				case resize <- remotecommand.TerminalSize{Width: frame.Cols, Height: frame.Rows}:
				default:
				}
			}
		}
	}()

	_ = writeWS(ctx, conn, map[string]any{"type": "open"})

	err := h.svc.Exec(ctx, c, req, h.sidecars, cluster.ExecStreams{
		Stdin:  stdin,
		Stdout: &wsWriter{ctx: ctx, conn: conn},
		Stderr: &wsWriter{ctx: ctx, conn: conn},
		Resize: resize,
	}, impersonationFor(r, c))

	recorder.close(err)

	if err != nil {
		_ = writeWS(ctx, conn, map[string]any{"type": "error", "data": err.Error()})
		return
	}
	_ = writeWS(ctx, conn, map[string]any{"type": "end"})
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// wsWriter turns container output into frames.
type wsWriter struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	if err := writeWS(w.ctx, w.conn, map[string]any{"type": "stdout", "data": string(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ---------------------------------------------------------------- port forward

func (h *resourceHandlers) portForward(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	typeKey := r.URL.Query().Get("type")
	if typeKey == "" {
		typeKey = "pods"
	}

	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil || port <= 0 || port > 65535 {
		writeError(w, r, http.StatusBadRequest, "a port between 1 and 65535 is required")
		return
	}

	target, err := h.svc.ResolveForward(r.Context(), c, typeKey, namespace, name, port, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.allowedOrigins})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	if err := h.svc.Forward(r.Context(), c, *target, &wsTunnel{ctx: r.Context(), conn: conn},
		impersonationFor(r, c)); err != nil {
		_ = writeWS(r.Context(), conn, map[string]any{"type": "error", "data": err.Error()})
		return
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// wsTunnel carries raw bytes both ways as binary frames. Unlike a terminal there is
// nothing to tag: whatever the port speaks is not ours to interpret.
type wsTunnel struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (t *wsTunnel) Read(p []byte) (int, error) {
	_, data, err := t.conn.Read(t.ctx)
	if err != nil {
		return 0, io.EOF
	}
	return copy(p, data), nil
}

func (t *wsTunnel) Write(p []byte) (int, error) {
	if err := t.conn.Write(t.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
