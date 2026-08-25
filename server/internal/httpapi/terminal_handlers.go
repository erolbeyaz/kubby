package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/kubectlsh"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// clusterTerminal opens a terminal scoped to kubectl and to one cluster.
//
// Deliberately not a shell on this machine: that shell would carry Kubby's own
// environment — the encryption key, the database, every stored kubeconfig — and would
// reduce the cluster grants, the read-only locks and the audit trail to decoration. What
// the reader gets instead is kubectl, already pointed at the cluster in front of them,
// with their own permissions and every command recorded.
func (h *resourceHandlers) clusterTerminal(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	_, user := principal(r)
	impersonate := impersonationFor(r, c)

	// The same gate as every other write, asked once: the answer decides whether this
	// terminal may run mutating commands, not whether it may open at all.
	//
	// It used to refuse outright on a locked cluster, on the grounds that kubectl needs
	// the cluster's credential and a terminal can read it back out. That is still true —
	// but the read-only lock exists to stop accidents, and extracting a credential to use
	// it elsewhere is not an accident. Refusing to open cost every reader a working
	// terminal on a locked cluster to defend against something the gate was never for
	// (ADR-107 supersedes ADR-096).
	verdict, err := h.svc.CheckWrite(r.Context(), c,
		cluster.WriteRequest{Type: terminalProbeType(), Namespace: "", Name: "", Verb: cluster.VerbPatch},
		cluster.Permission{
			GlobalReadOnly: h.readOnly,
			MayWrite:       rbac.Role(user.Role).Can(rbac.PermClusterWrite),
		},
		impersonate,
	)
	if err != nil {
		writeResourceError(w, r, err)
		return
	}
	kubeconfig, contextName, err := h.svc.RenderKubeconfig(r.Context(), c, impersonate)
	if err != nil {
		writeResourceError(w, r, err)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.allowedOrigins})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	out := &wsWriter{ctx: ctx, conn: conn}
	_ = writeWS(ctx, conn, map[string]any{"type": "open"})

	session, err := kubectlsh.Open(kubectlsh.Options{
		Kubeconfig:  kubeconfig,
		ContextName: contextName,
		ClusterName: c.Name,
		Permission: kubectlsh.Permission{
			MayWrite:     verdict.Allowed,
			DeniedReason: verdict.Reason,
		},
		Timeout: terminalCommandLimit,
		// Every command, refused ones included. A terminal against a cluster with no
		// record of what was typed in it is not auditable (ADR-013 #5).
		OnCommand: func(command string, allowed bool) {
			result := audit.ResultSuccess
			if !allowed {
				result = audit.ResultDenied
			}
			h.recordTerminalCommand(r, c, command, result)
		},
	}, out)
	if err != nil {
		_ = writeWS(ctx, conn, map[string]any{"type": "error", "data": err.Error()})
		return
	}
	defer func() { _ = session.Close() }()

	h.recordTerminalCommand(r, c, "", audit.ResultSuccess)

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
			// An arrow key arrives as a three-byte escape sequence rather than as a
			// character, and feeding it to the line editor would print its letters.
			if strings.HasPrefix(frame.Data, "\x1b[") {
				session.Escape(frame.Data)
				continue
			}
			session.Input(ctx, frame.Data)
		case "resize":
			session.Resize(frame.Cols, frame.Rows)

		case "upload":
			// The manifest on the reader's own machine, put where the command they are
			// about to type can reach it.
			contents, decodeErr := base64.StdEncoding.DecodeString(frame.Data)
			if decodeErr != nil {
				_ = writeWS(ctx, conn, map[string]any{
					"type": "stdout",
					"data": "\x1b[31mthat file could not be read\x1b[0m\r\n",
				})
				continue
			}
			if uploadErr := session.Upload(frame.Name, contents); uploadErr != nil {
				h.recordTerminalCommand(r, c, "upload "+frame.Name, audit.ResultError)
				_ = writeWS(ctx, conn, map[string]any{
					"type": "stdout",
					"data": "\x1b[31m" + uploadErr.Error() + "\x1b[0m\r\n",
				})
				continue
			}
			h.recordTerminalCommand(r, c, "upload "+frame.Name, audit.ResultSuccess)
		}
	}
}

func (h *resourceHandlers) recordTerminalCommand(r *http.Request, c *store.Cluster, command, result string) {
	if h.audit == nil {
		return
	}
	_, user := principal(r)

	action := audit.ActionTerminalOpened
	details := map[string]any{}
	if command != "" {
		action = audit.ActionTerminalCommand
		// The command itself, because that is the whole record: a terminal against a
		// cluster with no note of what was typed in it is not auditable.
		details["command"] = command
	}

	h.audit.Record(r.Context(), audit.Event{
		Action:     action,
		Result:     result,
		ActorID:    &user.ID,
		ActorEmail: user.Email,
		ClusterID:  &c.ID,
		Details:    details,
	})
}

// terminalProbeType is what the write gate is asked about. Nodes are the broadest thing
// a terminal could reach, so an answer of yes here is not narrower than what the session
// can then do.
func terminalProbeType() cluster.ResourceType {
	nodes, err := cluster.LookupType("nodes")
	if err != nil {
		return cluster.ResourceType{}
	}
	return nodes
}

// terminalCommandLimit bounds one command, not the session. It is generous because a
// `kubectl logs -f` is a legitimate thing to leave running; closing the tab ends it.
const terminalCommandLimit = 30 * time.Minute
