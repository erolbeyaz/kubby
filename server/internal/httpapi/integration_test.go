package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/auth"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/httpapi"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

const testPassword = "Tr0ubador&Horse!vault"

// harness is a running server plus a client that keeps cookies, so tests exercise the
// same flow a browser would.
type harness struct {
	t      *testing.T
	server *httptest.Server
	db     *store.DB
	client *http.Client
	schema string
}

// newHarness starts a server against an isolated schema so tests never see each
// other's users.
func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithMFA(t, false)
}

// newHarnessReadOnly builds one with the deployment-wide write lock on, so the kill
// switch can be tested as the thing it is rather than assumed.
func newHarnessReadOnly(t *testing.T) *harness {
	t.Helper()
	return buildHarness(t, false, true)
}

// newHarnessWithMFA controls whether administrators must hold a second factor.
func newHarnessWithMFA(t *testing.T, requireMFAForAdmin bool) *harness {
	t.Helper()
	return buildHarness(t, requireMFAForAdmin, false)
}

func buildHarness(t *testing.T, requireMFAForAdmin, readOnly bool) *harness {
	t.Helper()

	dsn := os.Getenv("KUBBY_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("KUBBY_TEST_DB_DSN is not set; skipping HTTP integration tests")
	}

	schema := "test_" + uuid.NewString()[:8]
	ctx := context.Background()

	db, err := store.OpenDSN(ctx, dsn+" search_path="+schema, 5)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		db.Close()
	})
	applySchema(t, db, schema)

	cfg := &config.Config{}
	cfg.HTTP.PublicURL, _ = url.Parse("http://localhost")
	alias, _ := url.Parse("https://kubby.alias.example.com")
	cfg.HTTP.AllowedOrigins = []*url.URL{alias}
	cfg.HTTP.ReadOnly = readOnly
	cfg.Auth = config.AuthConfig{
		SessionTTL: 900_000_000_000, RefreshTTL: 3_600_000_000_000,
		Argon2MemoryMiB: 16, LoginMaxAttempts: 3,
		LockoutDurations:   []time.Duration{5 * time.Minute, 10 * time.Minute},
		MaxLockouts:        3,
		RequireMFAForAdmin: requireMFAForAdmin,
		LoginRatePerMinute: 10_000, LoginRateBurst: 10_000,
		APIRatePerMinute: 100_000, APIRateBurst: 100_000,
	}

	keyring, err := crypto.NewKeyring(1, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	srv := httpapi.New(httpapi.Deps{
		Config: cfg,
		Logger: logger,
		DB:     db,
		Store:  db,
		Auth: auth.NewService(db, keyring, auth.Settings{
			SessionTTL: cfg.Auth.SessionTTL, RefreshTTL: cfg.Auth.RefreshTTL,
			LoginMaxAttempts: cfg.Auth.LoginMaxAttempts,
			LockoutDurations: cfg.Auth.LockoutDurations, MaxLockouts: cfg.Auth.MaxLockouts,
			RequireMFAForAdmin: requireMFAForAdmin,
			Argon2:             auth.DefaultArgon2Params(16), Issuer: "kubby-test",
		}),
		// Settings seal their credentials with the same key as cluster credentials, so
		// the harness has to hand the keyring over or every save panics.
		Keyring: keyring,
		Cluster: cluster.NewService(db, keyring, cluster.Settings{
			DefaultQPS: 20, DefaultBurst: 40, Timeout: 10 * time.Second, AllowLoopback: true,
		}),
		Audit: audit.New(db.Audit(), logger),
		WebFS: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}},
	})
	t.Cleanup(srv.Close)

	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	jar, _ := newJar()
	h := &harness{t: t, server: ts, db: db, schema: schema, client: &http.Client{Jar: jar}}

	// The SPA always loads before it posts anything; mirror that so the CSRF token is
	// bootstrapped the same way it is in a browser.
	bootstrap := h.do(http.MethodGet, "/api/v1/setup/status", nil)
	_ = bootstrap.Body.Close()
	return h
}

// do issues a request, attaching the CSRF header from the cookie the way the SPA does.
func (h *harness) do(method, path string, body any) *http.Response {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if u, _ := url.Parse(h.server.URL); u != nil {
		for _, c := range h.client.Jar.Cookies(u) {
			if c.Name == "kubby_csrf" {
				req.Header.Set("X-CSRF-Token", c.Value)
			}
		}
	}

	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// raw issues a request without cookies or CSRF — an attacker's view of the API.
func (h *harness) raw(method, path string, body any) *http.Response {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (h *harness) sessionCookie() string {
	u, _ := url.Parse(h.server.URL)
	for _, c := range h.client.Jar.Cookies(u) {
		if c.Name == "kubby_session" {
			return c.Value
		}
	}
	return ""
}

func (h *harness) completeSetup(email string) {
	h.t.Helper()
	resp := h.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"email": email, "displayName": "First Admin", "password": testPassword,
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		h.t.Fatalf("setup returned %d: %s", resp.StatusCode, readBody(resp))
	}
}

func (h *harness) login(email, password string) *http.Response {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	})
}

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

// ---------------------------------------------------------------- tests

func TestSetupWizardRunsExactlyOnce(t *testing.T) {
	h := newHarness(t)

	status := func() bool {
		resp := h.do(http.MethodGet, "/api/v1/setup/status", nil)
		defer func() { _ = resp.Body.Close() }()
		return decode[map[string]bool](t, resp)["setupRequired"]
	}

	if !status() {
		t.Fatal("setupRequired is false on an empty database")
	}
	h.completeSetup("admin@example.com")
	if status() {
		t.Error("setupRequired is still true after setup completed")
	}

	t.Run("a second attempt is refused", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
			"email": "attacker@example.com", "displayName": "Attacker", "password": testPassword,
		})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusGone {
			t.Fatalf("second setup returned %d, want 410: the wizard must not create a second admin", resp.StatusCode)
		}
	})
}

func TestSetupRejectsWeakPasswords(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/api/v1/setup/admin", map[string]string{
		"email": "admin@example.com", "displayName": "Admin", "password": "password123",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("weak password returned %d, want 422", resp.StatusCode)
	}
}

func TestLoginAndSessionLifecycle(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	t.Run("wrong password is rejected", func(t *testing.T) {
		resp := h.login("admin@example.com", "WrongPassword123!")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("unknown account is indistinguishable", func(t *testing.T) {
		resp := h.login("nobody@example.com", testPassword)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("correct password signs in", func(t *testing.T) {
		resp := h.login("admin@example.com", testPassword)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d, want 200: %s", resp.StatusCode, readBody(resp))
		}

		var sawSession, sawCSRF bool
		for _, c := range resp.Cookies() {
			switch c.Name {
			case "kubby_session":
				sawSession = true
				if !c.HttpOnly {
					t.Error("session cookie is not HttpOnly")
				}
				if c.SameSite != http.SameSiteStrictMode {
					t.Error("session cookie is not SameSite=Strict")
				}
			case "kubby_csrf":
				sawCSRF = true
				if c.HttpOnly {
					t.Error("CSRF cookie must be readable by the SPA")
				}
			}
		}
		if !sawSession || !sawCSRF {
			t.Fatal("login did not set both the session and CSRF cookies")
		}
	})

	t.Run("me reflects the signed-in admin", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/me", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d, want 200", resp.StatusCode)
		}

		body := decode[struct {
			User        map[string]any `json:"user"`
			Permissions []string       `json:"permissions"`
		}](t, resp)

		if body.User["role"] != "admin" {
			t.Errorf("role = %v, want admin", body.User["role"])
		}
		if len(body.Permissions) == 0 {
			t.Error("admin has no permissions")
		}
	})

	t.Run("logout revokes the session", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("logout returned %d, want 204", resp.StatusCode)
		}

		after := h.do(http.MethodGet, "/api/v1/me", nil)
		defer func() { _ = after.Body.Close() }()
		if after.StatusCode != http.StatusUnauthorized {
			t.Errorf("/me after logout returned %d, want 401", after.StatusCode)
		}
	})
}

// Session fixation: the token held before authenticating must not become the
// authenticated session.
func TestLoginIssuesAFreshSessionToken(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	first := h.login("admin@example.com", testPassword)
	_ = first.Body.Close()
	tokenAfterFirst := h.sessionCookie()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	second := h.login("admin@example.com", testPassword)
	_ = second.Body.Close()
	tokenAfterSecond := h.sessionCookie()

	if tokenAfterFirst == "" || tokenAfterSecond == "" {
		t.Fatal("no session cookie was issued")
	}
	if tokenAfterFirst == tokenAfterSecond {
		t.Fatal("the same session token was reused across logins; session fixation is possible")
	}
}

func TestRepeatedFailuresLockTheAccount(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	for i := range 3 {
		resp := h.login("admin@example.com", "WrongPassword123!")
		_ = resp.Body.Close()
		if i < 2 && resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d returned %d, want 401", i+1, resp.StatusCode)
		}
	}

	locked := h.login("admin@example.com", testPassword)
	defer func() { _ = locked.Body.Close() }()
	if locked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after the lockout threshold, login with the CORRECT password returned %d, want 429",
			locked.StatusCode)
	}
}

// The central authorisation check must hold even when the caller bypasses the UI.
func TestReadOnlyUserCannotReachAdminEndpoints(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	adminLogin := h.login("admin@example.com", testPassword)
	_ = adminLogin.Body.Close()

	create := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "viewer@example.com", "displayName": "Viewer",
		"password": testPassword, "role": string(rbac.RoleReadOnly),
	})
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("creating the readonly user returned %d: %s", create.StatusCode, readBody(create))
	}
	_ = create.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	viewerLogin := h.login("viewer@example.com", testPassword)
	_ = viewerLogin.Body.Close()

	forbidden := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/users", nil},
		{http.MethodPost, "/api/v1/users", map[string]string{
			"email": "x@example.com", "displayName": "X", "password": testPassword, "role": "admin",
		}},
		{http.MethodGet, "/api/v1/audit", nil},
	}

	for _, c := range forbidden {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			resp := h.do(c.method, c.path, c.body)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("readonly user got %d for %s %s, want 403", resp.StatusCode, c.method, c.path)
			}
		})
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	for _, path := range []string{"/api/v1/me", "/api/v1/users", "/api/v1/audit", "/api/v1/me/sessions"} {
		resp := h.raw(http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without a session returned %d, want 401", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// A mutating request without the double-submit token must fail even with a valid
// session cookie.
func TestMutatingRequestsRequireCSRFToken(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	login := h.login("admin@example.com", testPassword)
	_ = login.Body.Close()

	body, _ := json.Marshal(map[string]string{
		"email": "nocsrf@example.com", "displayName": "No CSRF",
		"password": testPassword, "role": "user",
	})
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/users", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	u, _ := url.Parse(h.server.URL)
	for _, c := range h.client.Jar.Cookies(u) {
		req.AddCookie(c) // cookies included, CSRF header deliberately omitted
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("request without the CSRF header returned %d, want 403", resp.StatusCode)
	}
}

func TestCrossOriginMutationIsRejected(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	login := h.login("admin@example.com", testPassword)
	_ = login.Body.Close()

	body, _ := json.Marshal(map[string]string{"currentPassword": testPassword, "newPassword": testPassword})
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")

	u, _ := url.Parse(h.server.URL)
	for _, c := range h.client.Jar.Cookies(u) {
		req.AddCookie(c)
		if c.Name == "kubby_csrf" {
			req.Header.Set("X-CSRF-Token", c.Value)
		}
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin mutation returned %d, want 403", resp.StatusCode)
	}
}

func TestLastAdminCannotBeRemoved(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	login := h.login("admin@example.com", testPassword)
	_ = login.Body.Close()

	created := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "second@example.com", "displayName": "Second", "password": testPassword, "role": "admin",
	})
	second := decode[map[string]any](t, created)
	_ = created.Body.Close()

	t.Run("an admin cannot change their own role", func(t *testing.T) {
		list := h.do(http.MethodGet, "/api/v1/users", nil)
		users := decode[struct {
			Users []map[string]any `json:"users"`
		}](t, list)
		_ = list.Body.Close()

		var selfID string
		for _, u := range users.Users {
			if u["email"] == "admin@example.com" {
				selfID = fmt.Sprint(u["id"])
			}
		}
		resp := h.do(http.MethodPatch, "/api/v1/users/"+selfID, map[string]string{"role": "readonly"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("self-demotion returned %d, want 409", resp.StatusCode)
		}
	})

	t.Run("demoting another admin is allowed while one remains", func(t *testing.T) {
		resp := h.do(http.MethodPatch, "/api/v1/users/"+fmt.Sprint(second["id"]),
			map[string]string{"role": "readonly"})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("demoting a second admin returned %d, want 200: %s", resp.StatusCode, readBody(resp))
		}
	})
}

// A deactivated user must lose access at once, not when their token expires.
func TestDeactivationRevokesActiveSessions(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	adminLogin := h.login("admin@example.com", testPassword)
	_ = adminLogin.Body.Close()

	created := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "victim@example.com", "displayName": "Victim", "password": testPassword, "role": "user",
	})
	victim := decode[map[string]any](t, created)
	_ = created.Body.Close()

	// A second client so the admin session stays intact.
	jar, _ := newJar()
	victimClient := &harness{t: t, server: h.server, db: h.db, client: &http.Client{Jar: jar}}
	bootstrap := victimClient.do(http.MethodGet, "/api/v1/setup/status", nil)
	_ = bootstrap.Body.Close()

	login := victimClient.login("victim@example.com", testPassword)
	_ = login.Body.Close()

	check := victimClient.do(http.MethodGet, "/api/v1/me", nil)
	if check.StatusCode != http.StatusOK {
		t.Fatalf("victim could not sign in: %d", check.StatusCode)
	}
	_ = check.Body.Close()

	deactivate := h.do(http.MethodPatch, "/api/v1/users/"+fmt.Sprint(victim["id"]),
		map[string]bool{"isActive": false})
	if deactivate.StatusCode != http.StatusOK {
		t.Fatalf("deactivation returned %d: %s", deactivate.StatusCode, readBody(deactivate))
	}
	_ = deactivate.Body.Close()

	after := victimClient.do(http.MethodGet, "/api/v1/me", nil)
	defer func() { _ = after.Body.Close() }()
	if after.StatusCode == http.StatusOK {
		t.Fatal("a deactivated user still has a working session")
	}
}

func TestAuditTrailRecordsAuthenticationEvents(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	failed := h.login("admin@example.com", "WrongPassword123!")
	_ = failed.Body.Close()

	ok := h.login("admin@example.com", testPassword)
	_ = ok.Body.Close()

	resp := h.do(http.MethodGet, "/api/v1/audit", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit listing returned %d", resp.StatusCode)
	}

	events := decode[struct {
		Events []struct {
			Action string `json:"action"`
			Result string `json:"result"`
		} `json:"events"`
	}](t, resp)

	seen := map[string]string{}
	for _, e := range events.Events {
		seen[e.Action] = e.Result
	}

	for action, wantResult := range map[string]string{
		audit.ActionSetupCompleted: audit.ResultSuccess,
		audit.ActionLoginFailed:    audit.ResultDenied,
		audit.ActionLoginSucceeded: audit.ResultSuccess,
	} {
		got, ok := seen[action]
		if !ok {
			t.Errorf("audit trail is missing %q", action)
			continue
		}
		if got != wantResult {
			t.Errorf("%s recorded result %q, want %q", action, got, wantResult)
		}
	}
}

// A user whose policy requires MFA but who has not enrolled yet must still be able to
// reach enrolment. Without this the first administrator is locked out permanently the
// moment they complete the setup wizard.
func TestMandatoryMFAEnrolmentIsReachable(t *testing.T) {
	h := newHarnessWithMFA(t, true)
	h.completeSetup("admin@example.com")

	login := h.login("admin@example.com", testPassword)
	body := decode[struct {
		MFARequired          bool `json:"mfaRequired"`
		MFAEnrolled          bool `json:"mfaEnrolled"`
		MFAEnrolmentRequired bool `json:"mfaEnrolmentRequired"`
	}](t, login)
	_ = login.Body.Close()

	if !body.MFARequired || body.MFAEnrolled || !body.MFAEnrolmentRequired {
		t.Fatalf("login response = %+v, want MFA required and enrolment required", body)
	}

	t.Run("normal endpoints stay closed until enrolment completes", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/api/v1/me", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("/me before enrolment returned %d, want 403", resp.StatusCode)
		}
	})

	t.Run("verifying without an enrolled authenticator is refused clearly", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/api/v1/auth/mfa/verify", map[string]string{"code": "000000"})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("verify without enrolment returned %d, want 409", resp.StatusCode)
		}
	})

	var secret string
	t.Run("enrolment is reachable", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/api/v1/me/mfa/enroll", nil)
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("enrolment returned %d, want 200: the account would be locked out", resp.StatusCode)
		}
		secret = decode[struct {
			Secret string `json:"secret"`
		}](t, resp).Secret
	})

	t.Run("confirming enrolment signs the user in", func(t *testing.T) {
		code, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatalf("generate code: %v", err)
		}

		resp := h.do(http.MethodPost, "/api/v1/me/mfa/confirm", map[string]string{"code": code})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("confirm returned %d: %s", resp.StatusCode, readBody(resp))
		}

		codes := decode[struct {
			RecoveryCodes []string `json:"recoveryCodes"`
		}](t, resp)
		if len(codes.RecoveryCodes) == 0 {
			t.Error("no recovery codes were issued")
		}

		after := h.do(http.MethodGet, "/api/v1/me", nil)
		defer func() { _ = after.Body.Close() }()
		if after.StatusCode != http.StatusOK {
			t.Fatalf("/me after enrolment returned %d, want 200", after.StatusCode)
		}
	})
}

// The SPA is served from the API's own origin in production, but an installation may
// legitimately be reachable under more than one hostname. Configured origins are
// accepted; everything else is still refused.
func TestConfiguredOriginsAreAccepted(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	login := h.login("admin@example.com", testPassword)
	_ = login.Body.Close()

	post := func(origin string) int {
		body, _ := json.Marshal(map[string]string{
			"currentPassword": testPassword, "newPassword": testPassword,
		})
		req, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/me/password", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}

		u, _ := url.Parse(h.server.URL)
		for _, c := range h.client.Jar.Cookies(u) {
			req.AddCookie(c)
			if c.Name == "kubby_csrf" {
				req.Header.Set("X-CSRF-Token", c.Value)
			}
		}

		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	if got := post("https://kubby.alias.example.com"); got == http.StatusForbidden {
		t.Error("a configured additional origin was rejected")
	}
	if got := post("https://evil.example.com"); got != http.StatusForbidden {
		t.Errorf("an unlisted origin returned %d, want 403", got)
	}
}

// Lockouts escalate and eventually block the account, and the response tells the user
// how many attempts remain (ADR-035).
func TestLockoutsEscalateAndFinallyBlock(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	// A non-admin: administrators are deliberately never blocked.
	adminLogin := h.login("admin@example.com", testPassword)
	_ = adminLogin.Body.Close()

	created := h.do(http.MethodPost, "/api/v1/users", map[string]string{
		"email": "member@example.com", "displayName": "Member",
		"password": testPassword, "role": "user",
	})
	member := decode[map[string]any](t, created)
	_ = created.Body.Close()

	logout := h.do(http.MethodPost, "/api/v1/auth/logout", nil)
	_ = logout.Body.Close()

	type failure struct {
		Error             string `json:"error"`
		AttemptsRemaining int    `json:"attemptsRemaining"`
		LockedForSeconds  int    `json:"lockedForSeconds"`
		Blocked           bool   `json:"blocked"`
	}

	attempt := func() (int, failure) {
		resp := h.login("member@example.com", "WrongPassword123!")
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode, decode[failure](t, resp)
	}

	// maxAttempts is 3 in the harness; the ladder is 5m then 10m, blocking on the third.
	t.Run("remaining attempts are reported", func(t *testing.T) {
		status, body := attempt()
		if status != http.StatusUnauthorized {
			t.Fatalf("first failure returned %d, want 401", status)
		}
		if body.AttemptsRemaining != 2 {
			t.Errorf("attemptsRemaining = %d, want 2", body.AttemptsRemaining)
		}

		status, body = attempt()
		if status != http.StatusUnauthorized || body.AttemptsRemaining != 1 {
			t.Errorf("second failure = %d / %d remaining, want 401 / 1", status, body.AttemptsRemaining)
		}
	})

	t.Run("first lockout lasts five minutes", func(t *testing.T) {
		status, body := attempt()
		if status != http.StatusTooManyRequests {
			t.Fatalf("third failure returned %d, want 429", status)
		}
		if body.LockedForSeconds < 250 || body.LockedForSeconds > 300 {
			t.Errorf("lockedForSeconds = %d, want about 300", body.LockedForSeconds)
		}
	})

	clearLock := func() {
		t.Helper()
		_, err := h.db.Pool().Exec(context.Background(),
			`UPDATE users SET locked_until = NULL WHERE email = 'member@example.com'`)
		if err != nil {
			t.Fatalf("clear lock: %v", err)
		}
	}

	t.Run("second lockout lasts ten minutes", func(t *testing.T) {
		clearLock()
		attempt()
		attempt()
		status, body := attempt()

		if status != http.StatusTooManyRequests {
			t.Fatalf("second lockout returned %d, want 429", status)
		}
		if body.LockedForSeconds < 550 || body.LockedForSeconds > 600 {
			t.Errorf("lockedForSeconds = %d, want about 600", body.LockedForSeconds)
		}
	})

	t.Run("third lockout blocks the account", func(t *testing.T) {
		clearLock()
		attempt()
		attempt()
		status, body := attempt()

		if status != http.StatusForbidden {
			t.Fatalf("third lockout returned %d, want 403", status)
		}
		if !body.Blocked {
			t.Error("response does not report the account as blocked")
		}

		// A blocked account stays out even with the correct password.
		resp := h.login("member@example.com", testPassword)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("correct password on a blocked account returned %d, want 403", resp.StatusCode)
		}
	})

	t.Run("an administrator can unblock", func(t *testing.T) {
		login := h.login("admin@example.com", testPassword)
		_ = login.Body.Close()

		resp := h.do(http.MethodPatch, "/api/v1/users/"+fmt.Sprint(member["id"]),
			map[string]bool{"unblock": true})
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unblock returned %d: %s", resp.StatusCode, readBody(resp))
		}
		if decode[map[string]any](t, resp)["isBlocked"] != false {
			t.Error("user is still marked blocked after unblocking")
		}
	})
}

// An installation must never be able to lock itself out of its own administration.
func TestAdministratorsAreNeverBlocked(t *testing.T) {
	h := newHarness(t)
	h.completeSetup("admin@example.com")

	for range 12 {
		resp := h.login("admin@example.com", "WrongPassword123!")
		_ = resp.Body.Close()

		if _, err := h.db.Pool().Exec(context.Background(),
			`UPDATE users SET locked_until = NULL WHERE email = 'admin@example.com'`); err != nil {
			t.Fatalf("clear lock: %v", err)
		}
	}

	var blocked *time.Time
	err := h.db.Pool().QueryRow(context.Background(),
		`SELECT blocked_at FROM users WHERE email = 'admin@example.com'`).Scan(&blocked)
	if err != nil {
		t.Fatalf("read blocked_at: %v", err)
	}
	if blocked != nil {
		t.Fatal("an administrator was blocked; the installation would be unadministrable")
	}

	resp := h.login("admin@example.com", testPassword)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("administrator could not sign in with the correct password: %d", resp.StatusCode)
	}
}
