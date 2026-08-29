package logsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubStore answers the three requests a probe makes, and records how it was asked.
type stubStore struct {
	authorization string
	searchBody    map[string]any
	sample        map[string]any
	indices       int
	status        int
	errorBody     string
}

func (s *stubStore) server(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.authorization = r.Header.Get("Authorization")

		if s.status != 0 {
			w.WriteHeader(s.status)
			_, _ = w.Write([]byte(s.errorBody))
			return
		}

		switch {
		case r.URL.Path == "/":
			_, _ = w.Write([]byte(`{"cluster_name":"logging-prod","version":{"number":"9.3.2"}}`))

		case strings.HasPrefix(r.URL.Path, "/_resolve/index/"):
			streams := make([]any, s.indices)
			for i := range streams {
				streams[i] = map[string]any{"name": "logs-x"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data_streams": streams})

		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			_, _ = w.Write([]byte(`{"idx":{"mappings":{"properties":{"log":{"type":"text"}}}}}`))

		case strings.HasSuffix(r.URL.Path, "/_search"):
			_ = json.NewDecoder(r.Body).Decode(&s.searchBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": map[string]any{
					"total": map[string]any{"value": 456672},
					"hits":  []any{map[string]any{"_source": s.sample}},
				},
			})

		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProbeReportsWhatAnsweredAndWhatItHolds(t *testing.T) {
	store := &stubStore{
		indices: 3,
		sample: map[string]any{
			"@timestamp": "2026-08-29T20:54:03.541Z",
			"log":        "Cannot open database \"NetTrexCommon\" requested by the login.",
			"kubernetes": map[string]any{"pod_name": "nx-fxrateengineapi-58d8b9d945-74tc4"},
		},
	}
	server := store.server(t)

	client, err := New(Config{URL: server.URL, Index: "logs-hybprod-app-*"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	probe, err := client.Probe(context.Background(), 15*time.Minute)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	// Which store answered, not just that one did: an operator who typed the wrong
	// address needs to see it rather than a green tick.
	if probe.Cluster != "logging-prod" || probe.Version != "9.3.2" {
		t.Errorf("probe identified %q %q", probe.Cluster, probe.Version)
	}
	if probe.Indices != 3 {
		t.Errorf("indices = %d, want 3", probe.Indices)
	}
	if probe.Documents != 456672 {
		t.Errorf("documents = %d, want 456672", probe.Documents)
	}
	if probe.WindowMinutes != 15 {
		t.Errorf("window = %d minutes, want 15", probe.WindowMinutes)
	}
	if probe.SampleAt != "2026-08-29T20:54:03.541Z" {
		t.Errorf("sampleAt = %q", probe.SampleAt)
	}
	// The whole document, because the operator has to read the field names off it.
	if _, ok := probe.Sample["kubernetes"]; !ok {
		t.Errorf("sample lost its fields: %+v", probe.Sample)
	}
}

// A pattern matching nothing is not an error to Elasticsearch — a search over it
// succeeds and finds none — so the resolve step has to be the thing that reports it, or
// a typo reads as a quiet cluster.
func TestProbeReportsAPatternThatMatchesNothing(t *testing.T) {
	store := &stubStore{indices: 0}
	client, err := New(Config{URL: store.server(t).URL, Index: "logs-typo-*"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	probe, err := client.Probe(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Indices != 0 {
		t.Errorf("indices = %d, want 0", probe.Indices)
	}
}

// These are somebody else's application logs and Kubby did not write them. A password or
// a token in one is ordinary, and this document is about to be rendered in a browser.
func TestProbeRedactsTheSampleItShows(t *testing.T) {
	// Assembled rather than written out: a literal token in the tree is a finding for
	// the secret scanner, and a test fixture is not worth teaching it to ignore.
	secret := "eyJ" + strings.Repeat("h", 16) + "." + strings.Repeat("p", 16) + "." + strings.Repeat("s", 16)
	store := &stubStore{
		indices: 1,
		sample: map[string]any{
			"log":      "connecting with token: " + secret,
			"password": "hunter2",
			"nested":   map[string]any{"authorization": "Bearer abcdefghijklmnop"},
		},
	}
	client, _ := New(Config{URL: store.server(t).URL, Index: "logs-*"})

	probe, err := client.Probe(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	rendered, _ := json.Marshal(probe.Sample)
	for _, forbidden := range []string{secret, "hunter2", "abcdefghijklmnop"} {
		if strings.Contains(string(rendered), forbidden) {
			t.Errorf("the sample still carries %q: %s", forbidden, rendered)
		}
	}
}

func TestProbeTruncatesAWholeStackTrace(t *testing.T) {
	store := &stubStore{indices: 1, sample: map[string]any{"log": strings.Repeat("at com.example.Thing.run(Thing.java:42) ", 200)}}
	client, _ := New(Config{URL: store.server(t).URL, Index: "logs-*"})

	probe, err := client.Probe(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if line, _ := probe.Sample["log"].(string); len(line) > maxSampleField+4 {
		t.Errorf("sample line is %d bytes; it was not truncated", len(line))
	}
}

func TestAuthenticationMatchesTheScheme(t *testing.T) {
	cases := []struct {
		name   string
		cfg    Config
		header string
	}{
		{"bearer", Config{Scheme: "bearer", Secret: "tok"}, "Bearer tok"},
		{"api key", Config{Scheme: "apikey", Secret: "key"}, "ApiKey key"},
		{"basic", Config{Username: "kubby", Secret: "pw"}, "Basic a3ViYnk6cHc="},
		{"none", Config{}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubStore{indices: 1}
			cfg := tc.cfg
			cfg.URL, cfg.Index = store.server(t).URL, "logs-*"

			client, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := client.Probe(context.Background(), time.Minute); err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if store.authorization != tc.header {
				t.Errorf("Authorization = %q, want %q", store.authorization, tc.header)
			}
		})
	}
}

// A refused credential and a pattern that matches nothing are different problems with
// the same red tick; saying which one sends the operator to the right place.
func TestRejectionsSayWhichProblemThisIs(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "rejected the credential"},
		{http.StatusForbidden, "may not read this index"},
		{http.StatusNotFound, "nothing matches the index pattern"},
		{http.StatusInternalServerError, "answered 500"},
	}

	for _, tc := range cases {
		store := &stubStore{status: tc.status, errorBody: `{"error":{"reason":"detail from the store"}}`}
		client, _ := New(Config{URL: store.server(t).URL, Index: "logs-*"})

		_, err := client.Probe(context.Background(), time.Minute)
		if err == nil {
			t.Fatalf("status %d produced no error", tc.status)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d said %q, want it to mention %q", tc.status, err, tc.want)
		}
		if !strings.Contains(err.Error(), "detail from the store") {
			t.Errorf("status %d dropped the store's own reason: %v", tc.status, err)
		}
	}
}

// The pattern becomes a path segment in every request built here, so it may not carry
// one of its own.
func TestIndexPatternMayNotSmuggleAPath(t *testing.T) {
	for _, index := range []string{"../_cluster/settings", "logs/*", "logs?x=1", "logs#f"} {
		if _, err := New(Config{URL: "http://es:9200", Index: index}); err == nil {
			t.Errorf("index %q was accepted", index)
		}
	}
}

func TestAnUnconfiguredSourceIsNotAnError(t *testing.T) {
	if _, err := New(Config{}); err != ErrNotConfigured {
		t.Errorf("New with nothing set returned %v, want ErrNotConfigured", err)
	}
	if (Config{URL: "http://es:9200"}).Configured() {
		t.Error("a config with no index reports itself configured")
	}
}

// The failure mode this reports is invisible otherwise: a keyword field cannot be
// searched for a substring, and a length limit on one drops the long lines silently.
func TestMessageMappingIsReadAndReported(t *testing.T) {
	cases := []struct {
		name     string
		mapping  string
		analyzed bool
		limit    int
	}{
		{"text", `{"type":"text"}`, true, 0},
		{"match_only_text", `{"type":"match_only_text"}`, true, 0},
		{"keyword", `{"type":"keyword","ignore_above":1024}`, false, 1024},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/":
					_, _ = w.Write([]byte(`{"cluster_name":"c","version":{"number":"9"}}`))
				case strings.HasPrefix(r.URL.Path, "/_resolve/index/"):
					_, _ = w.Write([]byte(`{"data_streams":[{"name":"logs-x"}]}`))
				case strings.HasSuffix(r.URL.Path, "/_mapping"):
					_, _ = w.Write([]byte(`{"idx":{"mappings":{"properties":{"log":` + tc.mapping + `}}}}`))
				default:
					_, _ = w.Write([]byte(`{"hits":{"total":{"value":1},"hits":[]}}`))
				}
			}))
			t.Cleanup(server.Close)

			client, _ := New(Config{URL: server.URL, Index: "logs-*"})
			probe, err := client.Probe(context.Background(), time.Minute)
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}

			if probe.Message.Analyzed != tc.analyzed {
				t.Errorf("%s reported analyzed=%v", tc.name, probe.Message.Analyzed)
			}
			if probe.Message.IgnoreAbove != tc.limit {
				t.Errorf("%s reported ignoreAbove=%d, want %d", tc.name, probe.Message.IgnoreAbove, tc.limit)
			}
			if probe.MessageField != "log" {
				t.Errorf("message field = %q", probe.MessageField)
			}
		})
	}
}

// A phrase query finds nothing on a keyword field and a wildcard finds nothing on a
// text one, so the shape has to follow the mapping rather than a preference.
func TestQueryShapeFollowsTheMapping(t *testing.T) {
	rule := Rule{Name: "r", Match: []string{"Cannot open database"}}

	analyzed, _ := json.Marshal(rule.query("log", MessageMapping{Type: "text", Analyzed: true}))
	if !strings.Contains(string(analyzed), "match_phrase") || strings.Contains(string(analyzed), "wildcard") {
		t.Errorf("an analyzed field got %s", analyzed)
	}

	keyword, _ := json.Marshal(rule.query("log", MessageMapping{Type: "keyword"}))
	if !strings.Contains(string(keyword), "wildcard") || strings.Contains(string(keyword), "match_phrase") {
		t.Errorf("a keyword field got %s", keyword)
	}
	if !strings.Contains(string(keyword), `"case_insensitive":true`) {
		t.Errorf("the wildcard is case-sensitive: %s", keyword)
	}

	// Mapping unknown: both clauses, because exactly one can match and guessing wrong
	// finds nothing without erroring.
	unknown, _ := json.Marshal(rule.query("log", MessageMapping{}))
	if !strings.Contains(string(unknown), "match_phrase") || !strings.Contains(string(unknown), "wildcard") {
		t.Errorf("an unknown mapping got %s", unknown)
	}
}

func TestAPhrasesOwnWildcardsAreNotPatternSyntax(t *testing.T) {
	rule := Rule{Name: "r", Match: []string{"panic: *"}}

	encoded, _ := json.Marshal(rule.query("log", MessageMapping{Type: "keyword"}))
	if !strings.Contains(string(encoded), `panic: \\*`) {
		t.Errorf("the phrase's own star was left as syntax: %s", encoded)
	}
}
