package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/erolbeyaz/kubby/internal/config"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func newTestRouter(t *testing.T, db Pinger) http.Handler {
	t.Helper()

	publicURL, err := url.Parse("https://kubby.example.com")
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	cfg := &config.Config{}
	cfg.HTTP.PublicURL = publicURL

	cfg.Auth.LoginRatePerMinute = 1000
	cfg.Auth.LoginRateBurst = 1000
	cfg.Auth.APIRatePerMinute = 10000
	cfg.Auth.APIRateBurst = 10000

	srv := New(Deps{
		Config: cfg,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		DB:     db,
		Store:  nil,
		Auth:   nil,
		Audit:  nil,
		WebFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>Kubby</title>")}},
	})
	t.Cleanup(srv.Close)
	return srv.Handler
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthzIsUnaffectedByDatabaseOutage(t *testing.T) {
	h := newTestRouter(t, stubPinger{err: errors.New("connection refused")})

	rec := do(t, h, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200: liveness must not restart a healthy process", rec.Code)
	}
}

func TestReadyzReportsDatabaseState(t *testing.T) {
	t.Run("database up", func(t *testing.T) {
		rec := do(t, newTestRouter(t, stubPinger{}), http.MethodGet, "/readyz")
		if rec.Code != http.StatusOK {
			t.Fatalf("/readyz = %d, want 200", rec.Code)
		}
	})

	t.Run("database down", func(t *testing.T) {
		rec := do(t, newTestRouter(t, stubPinger{err: errors.New("connection refused")}), http.MethodGet, "/readyz")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("/readyz = %d, want 503", rec.Code)
		}
	})
}

// Compliance requirement: a running deployment must identify its own build.
func TestVersionReportsBuildMetadata(t *testing.T) {
	rec := do(t, newTestRouter(t, stubPinger{}), http.MethodGet, "/version")
	if rec.Code != http.StatusOK {
		t.Fatalf("/version = %d, want 200", rec.Code)
	}

	var body versionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /version: %v", err)
	}
	if body.Version == "" || body.CommitSHA == "" || body.BuildDate == "" || body.GoVersion == "" {
		t.Errorf("/version has empty fields: %+v", body)
	}
}

func TestSecurityHeadersArePresent(t *testing.T) {
	rec := do(t, newTestRouter(t, stubPinger{}), http.MethodGet, "/healthz")

	want := map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	}
	for header, substring := range want {
		got := rec.Header().Get(header)
		if got == "" {
			t.Errorf("%s header is missing", header)
			continue
		}
		if header == "Content-Security-Policy" && !contains(got, substring) {
			t.Errorf("CSP %q does not contain %q", got, substring)
		}
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header is missing; users cannot quote an id when reporting failures")
	}
}

func TestUnknownAPIPathReturnsJSONNotHTML(t *testing.T) {
	rec := do(t, newTestRouter(t, stubPinger{}), http.MethodGet, "/api/v1/does-not-exist")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("API 404 must be JSON, got %q", rec.Body.String())
	}
	if body.RequestID == "" {
		t.Error("error response is missing requestId")
	}
}

func TestUnknownUIPathFallsBackToIndex(t *testing.T) {
	rec := do(t, newTestRouter(t, stubPinger{}), http.MethodGet, "/clusters/prod/pods")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: client-side routes must survive a reload", rec.Code)
	}
	if !contains(rec.Body.String(), "Kubby") {
		t.Errorf("expected index.html, got %q", rec.Body.String())
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
