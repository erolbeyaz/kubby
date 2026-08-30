package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/store"
)

// A forward lives as long as someone is using it, and no longer. Nothing about a tunnel
// into a cluster should outlive the reason it was opened.
const forwardIdleLimit = 30 * time.Minute

// forwardSession is one open route from Kubby to a port inside a pod.
//
// The tunnel is the server's, not the reader's: their browser talks to Kubby over the
// session they already hold, and Kubby talks to the pod. That is what makes this work in
// a browser at all — nothing can bind a local port there.
type forwardSession struct {
	ID        string    `json:"id"`
	ClusterID string    `json:"clusterId"`
	TypeKey   string    `json:"type"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Pod       string    `json:"pod"`
	Port      int       `json:"port"`
	URL       string    `json:"url"`
	StartedAt time.Time `json:"startedAt"`
	// Mode is how the browser reaches it: "port" for a real local port, "proxy" for the
	// authenticated route through Kubby. The two behave differently enough that the
	// screen has to say which one this is.
	Mode string `json:"mode"`
	// LocalPort is the port that was opened, when one was.
	LocalPort int `json:"localPort,omitempty"`
	// Note explains a fallback, so a reader who expected a port and got a proxy learns
	// why here rather than by the page misbehaving.
	Note string `json:"note,omitempty"`

	owner   uuid.UUID
	proxy   *httputil.ReverseProxy
	local   *cluster.LocalForward
	touched time.Time
	stop    context.CancelFunc
}

const (
	forwardModePort  = "port"
	forwardModeProxy = "proxy"
)

// close ends everything the session holds. A local forward keeps a listening socket
// that the proxy's context knows nothing about, so stopping one is not stopping both.
func (session *forwardSession) close() {
	session.stop()
	session.local.Close()
}

type forwardRegistry struct {
	mu       sync.Mutex
	sessions map[string]*forwardSession
}

func newForwardRegistry() *forwardRegistry {
	return &forwardRegistry{sessions: make(map[string]*forwardSession)}
}

func (reg *forwardRegistry) add(session *forwardSession) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.expireLocked()
	reg.sessions[session.ID] = session
}

// get returns a session belonging to this reader. A forward is personal: it was opened
// under one person's permissions, so it may not be borrowed by another's session.
func (reg *forwardRegistry) get(id string, owner uuid.UUID) *forwardSession {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.expireLocked()

	session, ok := reg.sessions[id]
	if !ok || session.owner != owner {
		return nil
	}
	session.touched = time.Now().UTC()
	return session
}

func (reg *forwardRegistry) list(clusterID string, owner uuid.UUID) []forwardSession {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.expireLocked()

	out := make([]forwardSession, 0, len(reg.sessions))
	for _, session := range reg.sessions {
		if session.owner == owner && session.ClusterID == clusterID {
			out = append(out, *session)
		}
	}
	return out
}

// touch marks a session as in use. A local forward carries traffic Kubby never sees as
// an HTTP request, so without this the idle sweep would close a tunnel someone is using.
func (reg *forwardRegistry) touch(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	if session, ok := reg.sessions[id]; ok {
		session.touched = time.Now().UTC()
	}
}

func (reg *forwardRegistry) remove(id string, owner uuid.UUID) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	session, ok := reg.sessions[id]
	if !ok || session.owner != owner {
		return false
	}
	delete(reg.sessions, id)
	session.close()
	return true
}

func (reg *forwardRegistry) expireLocked() {
	cutoff := time.Now().UTC().Add(-forwardIdleLimit)
	for id, session := range reg.sessions {
		if session.touched.Before(cutoff) {
			delete(reg.sessions, id)
			session.close()
		}
	}
}

// ---------------------------------------------------------------- ports

type portOption struct {
	Name      string `json:"name,omitempty"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Container string `json:"container,omitempty"`
}

// listPorts answers what there is to forward, so the reader picks from the object's own
// ports instead of remembering a number.
func (h *resourceHandlers) listPorts(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	typeKey := r.URL.Query().Get("type")
	if typeKey == "" {
		typeKey = "pods"
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	ports, err := h.svc.ForwardablePorts(r.Context(), c, typeKey, namespace, name, impersonationFor(r, c))
	if err != nil {
		writeResourceError(w, r, err)
		return
	}

	out := make([]portOption, 0, len(ports))
	for _, port := range ports {
		out = append(out, portOption{
			Name: port.Name, Port: port.Port, Protocol: port.Protocol, Container: port.Container,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ports": out})
}

// ---------------------------------------------------------------- sessions

type startForwardBody struct {
	Type      string `json:"type"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Port      int    `json:"port"`
	// LocalPort is which port to open on Kubby's host. Zero means any free one, which is
	// what "Random" in the dialog sends.
	LocalPort int `json:"localPort,omitempty"`
	// Proxy asks for the authenticated route instead of a port of its own.
	Proxy bool `json:"proxy,omitempty"`
}

func (h *resourceHandlers) startForward(w http.ResponseWriter, r *http.Request) {
	var body startForwardBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Type == "" {
		body.Type = "pods"
	}
	if body.Port <= 0 || body.Port > 65535 {
		writeError(w, r, http.StatusBadRequest, "a port between 1 and 65535 is required")
		return
	}

	// Reaching a port inside a pod is `pods/portforward`, a create — so it goes through
	// the same gate as any other write: the kill switch, the role, the cluster's lock,
	// and the cluster's own answer.
	podType, err := cluster.LookupType("pods")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "pods are not registered")
		return
	}
	ctx, ok := h.authoriseWrite(w, r, podType, body.Namespace, body.Name, cluster.VerbCreate)
	if !ok {
		return
	}

	target, err := h.svc.ResolveForward(r.Context(), ctx.cluster, body.Type, body.Namespace, body.Name,
		body.Port, impersonationFor(r, ctx.cluster))
	if err != nil {
		h.recordWrite(r, ctx, audit.ActionPortForwarded, body.Namespace, body.Name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	session := h.newForwardSession(r, ctx.cluster, body, *target)
	h.forwards.add(session)
	h.recordWrite(r, ctx, audit.ActionPortForwarded, body.Namespace, body.Name, audit.ResultSuccess, nil)

	writeJSON(w, http.StatusOK, session)
}

func (h *resourceHandlers) newForwardSession(r *http.Request, c *store.Cluster, body startForwardBody, target cluster.ForwardTarget) *forwardSession {
	_, user := principal(r)
	id := uuid.NewString()

	// Detached from the request that opened it: the tunnel outlives this response and is
	// ended by the reader, by idleness, or by the process shutting down.
	tunnelCtx, stop := context.WithCancel(context.WithoutCancel(r.Context()))
	impersonate := impersonationFor(r, c)

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return h.svc.DialForward(ctx, c, target, impersonate)
		},
		// One connection per request keeps a dead pod from poisoning a pool the reader
		// cannot see or flush.
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	go func() {
		<-tunnelCtx.Done()
		transport.CloseIdleConnections()
	}()

	prefix := "/api/v1/forward/" + id
	session := &forwardSession{
		ID:        id,
		ClusterID: c.ID.String(),
		TypeKey:   body.Type,
		Namespace: body.Namespace,
		Name:      body.Name,
		Pod:       target.Pod,
		Port:      target.Port,
		URL:       prefix + "/",
		Mode:      forwardModeProxy,
		StartedAt: time.Now().UTC(),
		owner:     user.ID,
		touched:   time.Now().UTC(),
		stop:      stop,
	}

	// A port of its own where that is possible, because it is the only shape in which an
	// arbitrary web application works: its own origin, at its own root, with its own
	// cookies. The proxy below stays as the answer for a Kubby the browser can only
	// reach over HTTP — in a cluster, say, where a port on the pod is not a port anyone
	// outside it can dial.
	if !body.Proxy {
		local, err := h.svc.ListenForward(r.Context(), c, target, impersonate,
			h.forwardCfg.Bind, body.LocalPort, cluster.PortRange(h.forwardCfg.Ports),
			func() { h.forwards.touch(id) },
			func(err error) {
				h.logger.Warn("port-forward tunnel failed",
					slog.String("cluster", c.Name), slog.String("error", err.Error()))
			})
		if err == nil {
			session.local = local
			session.LocalPort = local.Port
			session.Mode = forwardModePort
			session.URL = fmt.Sprintf("http://%s:%d/", h.forwardHost(r), local.Port)
		} else {
			session.Note = "No local port could be opened, so this is proxied through Kubby: " + err.Error()
		}
	}

	session.proxy = &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			// The pod does not know it is being reached through a tunnel, and a Host of
			// Kubby's own name would confuse virtual-hosted apps more than this does.
			pr.Out.URL.Host = fmt.Sprintf("%s:%d", target.Pod, target.Port)
			pr.Out.Host = pr.Out.URL.Host
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.Header.Del("Cookie")
		},
		ModifyResponse: func(resp *http.Response) error {
			rewriteRedirect(resp, prefix)
			isolate(resp)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			writeError(w, req, http.StatusBadGateway,
				fmt.Sprintf("could not reach %s:%d — %v", target.Pod, target.Port, err))
		},
	}
	return session
}

// isolate cuts the proxied page off from Kubby itself.
//
// This matters more than it looks. The page is served from Kubby's own origin, so without
// this a forwarded workload could read the CSRF cookie, call Kubby's API with the
// reader's session, and reach every cluster they can — forwarding a port would be handing
// that workload the reader's account. A CSP sandbox *without* allow-same-origin puts the
// page in an opaque origin of its own, where it can still run its own scripts but cannot
// see Kubby's cookies or storage.
//
// The cost is that the page cannot use cookies at all, so a forwarded app that needs a
// login of its own will not keep a session. That is the right trade: a dashboard behind a
// port-forward is worth reaching, an account takeover is not worth risking.
func isolate(resp *http.Response) {
	// Its cookies would be stored against Kubby's origin, where they could overwrite
	// Kubby's own.
	resp.Header.Del("Set-Cookie")
	// Ours replaces them; the page is already sandboxed and framing it is intended.
	resp.Header.Del("X-Frame-Options")
	resp.Header.Del("Content-Security-Policy")
	resp.Header.Del("Content-Security-Policy-Report-Only")
	resp.Header.Set("Content-Security-Policy", sandboxPolicy)
}

// sandboxPolicy deliberately omits allow-same-origin. With it the page would be back on
// Kubby's origin and the whole exercise would be pointless.
const sandboxPolicy = "sandbox allow-scripts allow-forms allow-popups allow-modals"

// rewriteRedirect keeps a relocation inside the tunnel. An app that answers "/" with a
// redirect to "/login" would otherwise send the browser to Kubby's own login.
func rewriteRedirect(resp *http.Response, prefix string) {
	location := resp.Header.Get("Location")
	if location == "" {
		return
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.IsAbs() || !strings.HasPrefix(parsed.Path, "/") {
		return
	}
	parsed.Path = prefix + parsed.Path
	resp.Header.Set("Location", parsed.String())
}

func (h *resourceHandlers) listForwards(w http.ResponseWriter, r *http.Request) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	_, user := principal(r)
	writeJSON(w, http.StatusOK, map[string]any{"forwards": h.forwards.list(c.ID.String(), user.ID)})
}

func (h *resourceHandlers) stopForward(w http.ResponseWriter, r *http.Request) {
	_, user := principal(r)
	if !h.forwards.remove(chi.URLParam(r, "forwardId"), user.ID) {
		writeError(w, r, http.StatusNotFound, "no such forward")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveForward is the tunnel itself: everything under the session's prefix goes to the
// pod. It carries the reader's own session cookie, so an unauthenticated tab reaches
// nothing.
func (h *resourceHandlers) serveForward(w http.ResponseWriter, r *http.Request) {
	_, user := principal(r)

	session := h.forwards.get(chi.URLParam(r, "forwardId"), user.ID)
	if session == nil {
		writeError(w, r, http.StatusNotFound, "this forward is closed")
		return
	}

	// Kubby's own baseline headers are for Kubby's own pages. Left in place they would be
	// added alongside the sandbox rather than replaced by it, and a `default-src 'self'`
	// read from an opaque origin resolves to nothing — the forwarded page would load
	// none of its own assets.
	//
	// The sandbox is written here rather than only on the way back, so it is on the
	// response whatever happens next: a tunnel that fails answers from this path too, and
	// it was going out with no policy at all.
	w.Header().Del("X-Frame-Options")
	w.Header().Set("Content-Security-Policy", sandboxPolicy)

	session.proxy.ServeHTTP(w, r)
}
