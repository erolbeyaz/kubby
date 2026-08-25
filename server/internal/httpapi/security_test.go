package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The security tests, kept together.
//
// Scattered through the feature files these are easy to lose track of, and the question
// "is authorisation actually enforced everywhere" is one somebody should be able to answer
// by reading one file.

// A manifest that parses, so an authorisation test reaches the authorisation check.
const validManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: authz-probe
  namespace: payments
data:
  probe: "1"
`

// ---------------------------------------------------------------- path traversal

// The SPA handler builds a path from the request and looks it up. Semgrep flags the
// `path.Clean` there, correctly in general: Clean is not a sanitiser. Here the lookup is
// against an embedded fs.FS rather than the OS, and both fs.Stat and http.FileServer
// enforce fs.ValidPath, which rejects `..` outright. This proves it rather than asserting
// it, so the day someone swaps the embedded FS for os.DirFS the test fails.
func TestTheStaticHandlerServesNothingOutsideItsAssets(t *testing.T) {
	h := newHarness(t)

	attempts := []string{
		"/../../../../etc/passwd",
		"/..%2f..%2f..%2fetc%2fpasswd",
		"/assets/../../../etc/passwd",
		"/./../../etc/shadow",
		"//etc/passwd",
		"/%2e%2e/%2e%2e/etc/passwd",
	}

	for _, attempt := range attempts {
		resp := h.do(http.MethodGet, attempt, nil)
		body := readBody(resp)
		_ = resp.Body.Close()

		// Anything not found falls through to index.html, which is how a single-page app
		// has to behave. What must never happen is a file from outside the assets.
		if strings.Contains(body, "root:") || strings.Contains(body, "/bin/bash") {
			t.Fatalf("%s served a file from the host: %.120s", attempt, body)
		}
		if resp.StatusCode >= 500 {
			t.Errorf("%s answered %d; a bad path should not be an error", attempt, resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------- authorisation

// Every write endpoint, with a role that holds no write permission at all.
func TestEveryWriteEndpointRefusesAReadOnlyRole(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "authz-writes")

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "viewer@example.com", "displayName": "Viewer",
		"password": testPassword, "role": "readonly",
	})
	memberBody := decode[map[string]any](t, member)
	_ = member.Body.Close()

	// Granted write on the cluster, so a refusal proves the role check specifically:
	// the grant is as permissive as it gets and the role still has to stop it.
	grant := h.do(http.MethodPut, "/api/v1/clusters/"+id+"/grants", map[string]string{
		"userId": toString(memberBody["id"]), "accessLevel": "write",
	})
	_ = grant.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("viewer@example.com", testPassword)
	_ = login.Body.Close()

	writes := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/v1/clusters/" + id + "/apply", map[string]any{"manifest": validManifest}},
		{http.MethodDelete, "/api/v1/clusters/" + id + "/object/pods?namespace=payments&name=x", nil},
		{http.MethodPost, "/api/v1/clusters/" + id + "/scale", map[string]any{"typeKey": "apps/deployments", "namespace": "payments", "name": "x", "replicas": 3}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/restart", map[string]any{"typeKey": "apps/deployments", "namespace": "payments", "name": "x"}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/evict", map[string]any{"namespace": "payments", "name": "x"}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/node/cordon", map[string]any{"name": "x"}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/node/drain", map[string]any{"name": "x"}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/cronjob/trigger", map[string]any{"namespace": "payments", "name": "x"}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/rollback", map[string]any{"typeKey": "apps/deployments", "namespace": "payments", "name": "x", "revision": 1}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/forwards", map[string]any{"namespace": "payments", "name": "x", "port": 80}},
	}

	for _, write := range writes {
		resp := h.do(write.method, write.path, write.body)
		status := resp.StatusCode
		_ = resp.Body.Close()

		if status != http.StatusForbidden {
			t.Errorf("%s %s answered %d for a reader; want 403", write.method, write.path, status)
		}
	}
}

// The other half: a role that may write, on a cluster they were granted read.
//
// The grant level existed in the access screen and in the cluster listing, and nowhere in
// the write path — so choosing "read" for somebody on a production cluster stopped
// nothing at all.
func TestAReadGrantStopsWritesFromARoleThatCouldOtherwiseWrite(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "authz-grant-level")

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "operator@example.com", "displayName": "Operator",
		"password": testPassword, "role": "user",
	})
	memberBody := decode[map[string]any](t, member)
	_ = member.Body.Close()

	grant := h.do(http.MethodPut, "/api/v1/clusters/"+id+"/grants", map[string]string{
		"userId": toString(memberBody["id"]), "accessLevel": "read",
	})
	_ = grant.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("operator@example.com", testPassword)
	_ = login.Body.Close()

	// Reading is what the grant is for.
	read := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/namespaces", nil)
	readStatus := read.StatusCode
	_ = read.Body.Close()
	if readStatus != http.StatusOK {
		t.Fatalf("a read grant could not read: %d", readStatus)
	}

	// Writing is not.
	for _, write := range []struct {
		method string
		path   string
		body   any
	}{
		// apply works out what to authorise from the manifest, so it parses first; an
		// invalid one is refused before the check is reached and proves nothing.
		{http.MethodPost, "/api/v1/clusters/" + id + "/apply", map[string]any{"manifest": validManifest}},
		{http.MethodPost, "/api/v1/clusters/" + id + "/scale", map[string]any{"typeKey": "apps/deployments", "namespace": "payments", "name": "x", "replicas": 1}},
		{http.MethodDelete, "/api/v1/clusters/" + id + "/object/pods?namespace=payments&name=x", nil},
		{http.MethodPost, "/api/v1/clusters/" + id + "/forwards", map[string]any{"namespace": "payments", "name": "x", "port": 80}},
	} {
		resp := h.do(write.method, write.path, write.body)
		status := resp.StatusCode
		_ = resp.Body.Close()

		if status != http.StatusForbidden {
			t.Errorf("%s %s answered %d with only a read grant; want 403", write.method, write.path, status)
		}
	}
}

// Administrative endpoints, with an ordinary user.
func TestAdministrativeEndpointsRefuseAnOrdinaryUser(t *testing.T) {
	h := signedInAdmin(t)

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "plain@example.com", "displayName": "Plain",
		"password": testPassword, "role": "user",
	})
	_ = member.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("plain@example.com", testPassword)
	_ = login.Body.Close()

	forbidden := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/users", nil},
		{http.MethodPost, "/api/v1/users", map[string]string{"email": "x@example.com", "displayName": "X", "password": testPassword, "role": "admin"}},
		{http.MethodGet, "/api/v1/audit", nil},
		{http.MethodGet, "/api/v1/settings", nil},
		{http.MethodPut, "/api/v1/settings/node-shell", map[string]any{"enabled": true, "image": "x", "namespace": "y"}},
		{http.MethodGet, "/metrics", nil},
		{http.MethodPost, "/api/v1/clusters", map[string]any{"name": "sneaky", "environment": "test", "kubeconfig": "x"}},
	}

	for _, attempt := range forbidden {
		resp := h.do(attempt.method, attempt.path, attempt.body)
		status := resp.StatusCode
		_ = resp.Body.Close()

		if status != http.StatusForbidden {
			t.Errorf("%s %s answered %d for an ordinary user; want 403", attempt.method, attempt.path, status)
		}
	}
}

// A cluster somebody was never granted must not be reachable through any of its paths,
// and must report as missing rather than forbidden: "forbidden" confirms it exists.
func TestAnUngrantedClusterIsInvisible(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "authz-invisible")

	member := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "outside@example.com", "displayName": "Outside",
		"password": testPassword, "role": "user",
	})
	_ = member.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()
	login := h.login("outside@example.com", testPassword)
	_ = login.Body.Close()

	paths := []string{
		"/api/v1/clusters/" + id,
		"/api/v1/clusters/" + id + "/overview",
		"/api/v1/clusters/" + id + "/namespaces",
		"/api/v1/clusters/" + id + "/resources/pods",
		"/api/v1/clusters/" + id + "/health",
		"/api/v1/clusters/" + id + "/metrics",
		"/api/v1/clusters/" + id + "/helm-releases",
		"/api/v1/clusters/" + id + "/workloads-overview",
	}

	for _, path := range paths {
		resp := h.do(http.MethodGet, path, nil)
		status := resp.StatusCode
		body := readBody(resp)
		_ = resp.Body.Close()

		if status != http.StatusNotFound {
			t.Errorf("%s answered %d for an ungranted cluster; want 404", path, status)
		}
		if strings.Contains(body, "authz-invisible") {
			t.Errorf("%s named the cluster in its answer, confirming it exists", path)
		}
	}
}

// ---------------------------------------------------------------- sessions

// A session identifier issued before signing in must not still be valid after. Otherwise
// anyone who can set a cookie in a victim's browser — a shared machine, a subdomain —
// waits for them to sign in and then holds their session.
func TestSigningInReplacesTheSessionIdentifier(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("fixation@example.com")

	// Setup creates the account; signing in is what issues a session.
	first := h.login("fixation@example.com", testPassword)
	_ = first.Body.Close()

	before := h.sessionCookie()
	if before == "" {
		t.Fatal("signing in issued no session cookie")
	}

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	second := h.login("fixation@example.com", testPassword)
	_ = second.Body.Close()

	after := h.sessionCookie()
	if after == "" {
		t.Fatal("signing in again issued no session cookie")
	}
	if before == after {
		t.Fatal("the session identifier survived a sign-out and sign-in")
	}
}

// Signing out must end the session on the server, not only clear the cookie: a token
// somebody copied before signing out would otherwise still work.
func TestSigningOutEndsTheSessionOnTheServer(t *testing.T) {
	h := signedInAdmin(t)

	me := h.do(http.MethodGet, "/api/v1/me", nil)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("signed in but /me answered %d", me.StatusCode)
	}
	_ = me.Body.Close()

	stolen := h.sessionCookie()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	if stolen == "" {
		t.Skip("this harness does not expose the session cookie")
	}

	replayed := h.doWithCookie(http.MethodGet, "/api/v1/me", stolen)
	status := replayed.StatusCode
	_ = replayed.Body.Close()

	if status == http.StatusOK {
		t.Fatal("a session token still worked after signing out")
	}
}

// ---------------------------------------------------------------- injection

// Every one of these reaches a query, a label selector or an argument list somewhere.
// None of them is concatenated into a shell or a query language by hand, and this is the
// test that says so: what comes back is an ordinary answer or an ordinary refusal, never
// a five hundred.
func TestHostileInputIsDataEverywhere(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "injection")

	hostile := []string{
		"'; DROP TABLE clusters; --",
		"$(whoami)",
		"`id`",
		"a\" or \"1\"=\"1",
		"../../etc/passwd",
		"{{.}}",
		"%00",
		"\x00truncated",
		strings.Repeat("A", 4096),
	}

	for _, value := range hostile {
		for _, path := range []string{
			"/api/v1/search?q=",
			"/api/v1/clusters/" + id + "/resources/pods?search=",
			"/api/v1/clusters/" + id + "/resources/pods?namespace=",
			"/api/v1/clusters/" + id + "/ports/default/",
		} {
			resp := h.do(http.MethodGet, path+urlEscape(value), nil)
			status := resp.StatusCode
			_ = resp.Body.Close()

			if status >= 500 {
				t.Errorf("%s with %.30q answered %d; hostile input should be refused, not crash",
					path, value, status)
			}
		}
	}
}

// ---------------------------------------------------------------- kill switch

// The deployment-wide lock is absolute: it stops the first administrator being created,
// which is the strongest statement it can make about itself. Anything that got past it
// would be a path that skipped the gate entirely.
func TestTheReadOnlyKillSwitchStopsEvenSetup(t *testing.T) {
	h := newHarnessReadOnly(t)

	resp := h.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"email": "admin@example.com", "displayName": "Admin", "password": testPassword,
	})
	status := resp.StatusCode
	body := readBody(resp)
	_ = resp.Body.Close()

	if status == http.StatusOK || status == http.StatusCreated {
		t.Fatal("the first administrator was created with the kill switch on")
	}
	if !strings.Contains(strings.ToLower(body), "read-only") {
		t.Errorf("the refusal did not name the kill switch: %s", body)
	}
}

// ---------------------------------------------------------------- helpers

// doWithCookie replays a session token the way somebody who copied one would, rather
// than through the harness's own jar.
func (h *harness) doWithCookie(method, path, session string) *http.Response {
	h.t.Helper()

	req, err := http.NewRequest(method, h.server.URL+path, nil) //nolint:noctx // a test
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "kubby_session", Value: session})

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func urlEscape(value string) string {
	return url.QueryEscape(value)
}
