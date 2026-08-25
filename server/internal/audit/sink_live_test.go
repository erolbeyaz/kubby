package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

// These run against real receivers, which the wire formats need: a hand-written NDJSON
// bulk body and a Loki push are exactly the kind of thing that passes a unit test and is
// then rejected by the system it was written for.
//
//	KUBBY_TEST_ELASTICSEARCH=http://localhost:9200 \
//	KUBBY_TEST_LOKI=http://localhost:3100 \
//	go test ./internal/audit/ -run Live
//
// Both are started by `docker compose --profile observability up -d`.

func TestLiveElasticsearchAcceptsAndStoresTheBulk(t *testing.T) {
	endpoint := os.Getenv("KUBBY_TEST_ELASTICSEARCH")
	if endpoint == "" {
		t.Skip("set KUBBY_TEST_ELASTICSEARCH to run against a real Elasticsearch")
	}

	index := "kubby-audit-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sink, err := NewSink(SinkConfig{Kind: KindElasticsearch, URL: endpoint, Index: index})
	if err != nil {
		t.Fatalf("build sink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	marker := "actor-" + index
	events := []Shipped{
		{Timestamp: time.Now().UTC(), Action: "pod.deleted", Result: "success", ActorEmail: marker},
		{Timestamp: time.Now().UTC(), Action: "cluster.locked", Result: "denied", ActorEmail: marker,
			Details: map[string]any{"reason": "go-live"}},
	}

	if err := sink.Send(context.Background(), events); err != nil {
		t.Fatalf("Elasticsearch refused the bulk: %v", err)
	}

	// Accepted is not the same as stored: the whole point of reading the bulk response is
	// that a 200 can still hide rejected documents.
	body := getJSON(t, endpoint+"/"+index+"/_refresh")
	_ = body

	var found int
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		search := getJSON(t, endpoint+"/"+index+"/_search?q=actor:%22"+url.QueryEscape(marker)+"%22")
		found = int(digFloat(search, "hits", "total", "value"))
		if found >= 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if found < 2 {
		t.Fatalf("Elasticsearch accepted the bulk but stored %d of 2 documents", found)
	}

	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, endpoint+"/"+index, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	})
}

// A data stream is what Elastic recommends for an append-only trail, and it is not a
// naming convention: it needs `create` rather than `index`, and it only forms at all if a
// matching index template exists. Without the template a `create` quietly makes an
// ordinary index — the shipping works, nothing errors, and the result is exactly the thing
// that was asked against.
func TestLiveElasticsearchFormsARealDataStream(t *testing.T) {
	endpoint := os.Getenv("KUBBY_TEST_ELASTICSEARCH")
	if endpoint == "" {
		t.Skip("set KUBBY_TEST_ELASTICSEARCH to run against a real Elasticsearch")
	}

	name := "kubby-audit-ds-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sink, err := NewSink(SinkConfig{
		Kind: KindElasticsearch, URL: endpoint, Index: name, DataStream: true,
	})
	if err != nil {
		t.Fatalf("build sink (this is where the template is created): %v", err)
	}
	defer func() { _ = sink.Close() }()

	t.Cleanup(func() {
		deleteAt(t, endpoint+"/_data_stream/"+name)
		deleteAt(t, endpoint+"/_index_template/kubby-audit-"+name)
	})

	marker := "actor-" + name
	events := []Shipped{
		{Timestamp: time.Now().UTC(), Action: "pod.deleted", Result: "success", ActorEmail: marker},
		{Timestamp: time.Now().UTC(), Action: "cluster.locked", Result: "denied", ActorEmail: marker},
	}
	if err := sink.Send(context.Background(), events); err != nil {
		t.Fatalf("Elasticsearch refused the bulk: %v", err)
	}

	// The assertion that matters: a data stream exists under that name, with a backing
	// index. A plain index would answer _search perfectly well and prove nothing.
	answer := getJSON(t, endpoint+"/_data_stream/"+name)
	streams, _ := dig(answer, "data_streams").([]any)
	if len(streams) == 0 {
		t.Fatalf("no data stream named %s was created; the documents went into a plain index", name)
	}

	first, _ := streams[0].(map[string]any)
	indices, _ := dig(first, "indices").([]any)
	if len(indices) == 0 {
		t.Error("the data stream has no backing index")
	}
	if template, _ := dig(first, "template").(string); template == "" {
		t.Error("the data stream is not governed by a template")
	}
}

func TestLiveLokiAcceptsAndReturnsThePush(t *testing.T) {
	endpoint := os.Getenv("KUBBY_TEST_LOKI")
	if endpoint == "" {
		t.Skip("set KUBBY_TEST_LOKI to run against a real Loki")
	}

	sink, err := NewSink(SinkConfig{Kind: KindLoki, URL: endpoint})
	if err != nil {
		t.Fatalf("build sink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	now := time.Now().UTC()
	// Deliberately out of order: Loki refuses a stream whose entries do not ascend, so
	// this is the case the encoder sorts for.
	events := []Shipped{
		{Timestamp: now.Add(2 * time.Second), Action: "pod.deleted", Result: "success", ActorEmail: "a@example.com"},
		{Timestamp: now, Action: "pod.deleted", Result: "success", ActorEmail: "b@example.com"},
		{Timestamp: now.Add(time.Second), Action: "cluster.locked", Result: "denied", ActorEmail: "c@example.com"},
	}

	if err := sink.Send(context.Background(), events); err != nil {
		t.Fatalf("Loki refused the push: %v", err)
	}

	query := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&start=%d&end=%d",
		endpoint, url.QueryEscape(`{job="kubby"}`),
		now.Add(-time.Minute).UnixNano(), now.Add(time.Minute).UnixNano())

	var streams int
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		answer := getJSON(t, query)
		result, _ := dig(answer, "data", "result").([]any)
		streams = len(result)
		if streams >= 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Two distinct action/result pairs among three events, which is the grouping that
	// keeps Loki from creating a stream per event.
	if streams < 2 {
		t.Fatalf("Loki returned %d streams for 2 distinct label sets", streams)
	}
}

func deleteAt(t *testing.T, endpoint string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, endpoint, nil) //nolint:noctx // a test cleanup
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func getJSON(t *testing.T, endpoint string) map[string]any {
	t.Helper()

	resp, err := http.Get(endpoint) //nolint:gosec,noctx // a test against a local receiver
	if err != nil {
		t.Fatalf("GET %s: %v", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(body, &out)
	return out
}

func dig(value any, path ...string) any {
	for _, key := range path {
		asMap, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = asMap[key]
	}
	return value
}

func digFloat(value any, path ...string) float64 {
	number, _ := dig(value, path...).(float64)
	return number
}
