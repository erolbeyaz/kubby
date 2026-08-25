package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sampleEvents() []Shipped {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	return []Shipped{
		{Timestamp: base.Add(2 * time.Second), Action: "pod.deleted", Result: "success", ActorEmail: "a@example.com"},
		{Timestamp: base, Action: "pod.deleted", Result: "success", ActorEmail: "b@example.com"},
		{Timestamp: base.Add(time.Second), Action: "cluster.locked", Result: "denied", ActorEmail: "c@example.com"},
	}
}

// The _bulk API wants an action line before every document and a trailing newline after
// the last. Getting either wrong makes Elasticsearch reject the whole batch.
func TestElasticsearchBulkBodyIsWellFormed(t *testing.T) {
	body, contentType, err := encodeBulk(sampleEvents(), SinkConfig{Index: "audit-2026"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if contentType != "application/x-ndjson" {
		t.Errorf("content type was %q", contentType)
	}
	if !strings.HasSuffix(string(body), "\n") {
		t.Error("the body must end in a newline or the last pair is rejected")
	}

	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected an action and a document for each of 3 events, got %d lines", len(lines))
	}

	for i := 0; i < len(lines); i += 2 {
		var action struct {
			Index struct {
				Index string `json:"_index"`
				ID    string `json:"_id"`
			} `json:"index"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &action); err != nil {
			t.Fatalf("action line %d is not JSON: %v", i, err)
		}
		if action.Index.Index != "audit-2026" {
			t.Errorf("action line %d names index %q", i, action.Index.Index)
		}
		// No id: a retried batch should append a duplicate rather than overwrite an
		// existing record, because a replaced audit record is the dangerous outcome.
		if action.Index.ID != "" {
			t.Errorf("a document id was sent (%q); a retry would then overwrite", action.Index.ID)
		}
	}
}

// Elasticsearch answers 200 to a bulk request in which individual documents failed.
// Treating that as success would report an audit trail as shipped when it was not.
func TestElasticsearchPartialFailureIsNotSuccess(t *testing.T) {
	body := `{"errors":true,"items":[
		{"index":{"status":201}},
		{"index":{"status":400,"error":{"type":"mapper_parsing_exception"}}}
	]}`

	err := elasticsearchBulkErrors([]byte(body))
	if err == nil {
		t.Fatal("a partly rejected bulk was reported as success")
	}
	if !strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("the error should say how many were rejected, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mapper_parsing_exception") {
		t.Errorf("the error should carry Elasticsearch's own reason, got: %v", err)
	}

	if err := elasticsearchBulkErrors([]byte(`{"errors":false,"items":[{"index":{"status":201}}]}`)); err != nil {
		t.Errorf("a fully accepted bulk was reported as a failure: %v", err)
	}
}

// Loki indexes by label set. A distinct set per event would create a stream per event,
// which is the most effective way to bring a Loki cluster down.
func TestLokiGroupsEventsIntoStreams(t *testing.T) {
	body, _, err := encodeLokiPush(sampleEvents(), SinkConfig{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var push struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &push); err != nil {
		t.Fatalf("not JSON: %v", err)
	}

	// Two distinct action/result pairs among three events.
	if len(push.Streams) != 2 {
		t.Fatalf("expected 2 streams for 2 distinct label sets, got %d", len(push.Streams))
	}

	for _, stream := range push.Streams {
		// High-cardinality fields must stay out of the labels.
		for _, forbidden := range []string{"actor", "resourceName", "requestId", "clientIp"} {
			if _, present := stream.Stream[forbidden]; present {
				t.Errorf("%q is a label; it would explode Loki's index", forbidden)
			}
		}
		if stream.Stream["job"] != "kubby" {
			t.Errorf("stream has no job label: %v", stream.Stream)
		}

		// Loki rejects a stream whose entries are not in ascending time order.
		previous := int64(0)
		for _, value := range stream.Values {
			at, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				t.Fatalf("timestamp %q is not a nanosecond integer", value[0])
			}
			if at < previous {
				t.Error("entries are out of order; Loki refuses the push")
			}
			previous = at

			// The detail lives in the line, where it is searchable but not indexed.
			if !strings.Contains(value[1], "actor") {
				t.Errorf("the line carries no actor: %s", value[1])
			}
		}
	}
}

func TestExtraLabelsReachTheDocument(t *testing.T) {
	body, _, err := encodeNDJSON(sampleEvents()[:1], SinkConfig{
		ExtraLabels: map[string]string{"deployment": "kubby-prod"},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(body), "kubby-prod") {
		t.Error("a fleet shipping into one index could not be told apart")
	}
}

// Each scheme is a different header, and getting it wrong is a 401 nobody can debug from
// the sink's side.
func TestCredentialsArePresentedTheWayEachSystemExpects(t *testing.T) {
	cases := []struct {
		name string
		cfg  SinkConfig
		want string
	}{
		{"bearer", SinkConfig{Scheme: "bearer", Token: "abc"}, "Bearer abc"},
		{"elasticsearch api key", SinkConfig{Scheme: "apikey", Token: "abc"}, "ApiKey abc"},
		{"basic", SinkConfig{Username: "kubby", Token: "secret"}, "Basic "},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var seen string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = r.Header.Get("Authorization")
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			cfg := testCase.cfg
			cfg.Kind = KindHTTP
			cfg.URL = server.URL
			sink, err := NewSink(cfg)
			if err != nil {
				t.Fatalf("build sink: %v", err)
			}
			defer func() { _ = sink.Close() }()

			if err := sink.Send(context.Background(), sampleEvents()[:1]); err != nil {
				t.Fatalf("send: %v", err)
			}
			if !strings.HasPrefix(seen, testCase.want) {
				t.Errorf("sent %q, want it to start with %q", seen, testCase.want)
			}
		})
	}
}

func TestAnHTTPErrorCarriesTheSinksOwnExplanation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"index is read-only"}`))
	}))
	defer server.Close()

	sink, err := NewSink(SinkConfig{Kind: KindHTTP, URL: server.URL})
	if err != nil {
		t.Fatalf("build sink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	err = sink.Send(context.Background(), sampleEvents())
	if err == nil {
		t.Fatal("a 400 was reported as success")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("the sink's own explanation was dropped: %v", err)
	}
}

// The settings screen must not be able to save a sink that has no sender behind it.
func TestOnlySinksThatShipCanBeBuilt(t *testing.T) {
	for _, kind := range []string{KindElasticsearch, KindLoki, KindHTTP} {
		sink, err := NewSink(SinkConfig{Kind: kind, URL: "http://localhost:9200"})
		if err != nil {
			t.Errorf("%s should be buildable: %v", kind, err)
			continue
		}
		_ = sink.Close()
	}

	if _, err := NewSink(SinkConfig{Kind: "syslog", URL: "http://localhost"}); err == nil {
		t.Error("syslog has no sender; building it must fail rather than ship nothing")
	}
}

func TestASinkAddressMustBeAUsableURL(t *testing.T) {
	for _, bad := range []string{"", "ftp://host/x", "not a url", "http://"} {
		if _, err := NewSink(SinkConfig{Kind: KindHTTP, URL: bad}); err == nil {
			t.Errorf("%q was accepted as a sink address", bad)
		}
	}
}
