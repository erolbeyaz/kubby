package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The wire formats Kubby can ship to.
const (
	KindElasticsearch = "elasticsearch"
	KindLoki          = "loki"
	KindHTTP          = "http"
)

// DefaultAuditIndex is where records go when no name is given. Also the default data
// stream name, and the pattern the bootstrapped template matches.
const DefaultAuditIndex = "kubby-audit"

// Long enough for a busy cluster to answer, short enough that saving the setting does not
// appear to hang when the address is wrong.
const templateTimeout = 15 * time.Second

// NewSink builds the sink a configuration asks for.
func NewSink(cfg SinkConfig) (Sink, error) {
	switch cfg.Kind {
	case KindElasticsearch:
		sink, err := newHTTPSink(KindElasticsearch, "/_bulk", cfg, encodeBulk)
		if err != nil {
			return nil, err
		}
		if cfg.DataStream {
			// Put the template before the first document rather than lazily: a `create`
			// into a name with no matching template makes an ordinary index, and by the
			// time anyone notices the data is already in the wrong shape.
			ctx, cancel := context.WithTimeout(context.Background(), templateTimeout)
			defer cancel()
			if err := sink.ensureDataStream(ctx); err != nil {
				return nil, err
			}
		}
		return sink, nil
	case KindLoki:
		return newHTTPSink(KindLoki, "/loki/api/v1/push", cfg, encodeLokiPush)
	case KindHTTP:
		// A plain collector: newline-delimited JSON, which is what most HTTP receivers
		// — QRadar's included — accept without a format of their own.
		return newHTTPSink(KindHTTP, "", cfg, encodeNDJSON)
	default:
		return nil, fmt.Errorf("unknown sink type %q", cfg.Kind)
	}
}

// ---------------------------------------------------------------- elasticsearch

// encodeBulk writes the action/document pairs the _bulk API expects.
//
// No document id is sent: letting Elasticsearch assign one means a retried batch appends
// duplicates rather than silently overwriting, and a duplicated audit record is far less
// dangerous than a replaced one.
func encodeBulk(events []Shipped, cfg SinkConfig) ([]byte, string, error) {
	index := strings.TrimSpace(cfg.Index)
	if index == "" {
		index = DefaultAuditIndex
	}

	// A data stream is append-only and accepts `create`; `index` is rejected outright.
	// The distinction is the whole difference between the two modes on the wire.
	verb := "index"
	if cfg.DataStream {
		verb = "create"
	}

	var body strings.Builder
	action, err := json.Marshal(map[string]any{verb: map[string]any{"_index": index}})
	if err != nil {
		return nil, "", fmt.Errorf("encode the bulk header: %w", err)
	}

	for _, event := range events {
		document, err := json.Marshal(withLabels(event, cfg))
		if err != nil {
			return nil, "", fmt.Errorf("encode an audit event: %w", err)
		}
		body.Write(action)
		body.WriteByte('\n')
		body.Write(document)
		body.WriteByte('\n')
	}

	// The trailing newline is required; without it Elasticsearch rejects the last pair.
	return []byte(body.String()), "application/x-ndjson", nil
}

// elasticsearchBulkErrors reads the per-document outcome out of a 200 response.
func elasticsearchBulkErrors(body []byte) error {
	var answer struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		// A 200 that is not the shape we expect is not proof of success, but it is also
		// not proof of failure; the batch is accepted rather than retried forever.
		return nil
	}
	if !answer.Errors {
		return nil
	}

	failed, first := 0, ""
	for _, item := range answer.Items {
		for _, outcome := range item {
			if outcome.Status < 200 || outcome.Status >= 300 {
				failed++
				if first == "" {
					first = string(outcome.Error)
				}
			}
		}
	}
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("elasticsearch rejected %d of %d documents: %s",
		failed, len(answer.Items), trim([]byte(first)))
}

// ---------------------------------------------------------------- loki

// encodeLokiPush groups events into streams.
//
// Grouped by the labels rather than one stream per event: Loki indexes by label set, and
// a distinct set per event would create a stream per event — the single most effective
// way to make a Loki cluster fall over.
//
// Only low-cardinality fields become labels. The actor's email, the resource name and the
// request id stay inside the line, where they are searchable but not indexed.
func encodeLokiPush(events []Shipped, cfg SinkConfig) ([]byte, string, error) {
	type entry struct {
		at   time.Time
		line string
	}
	grouped := map[string][]entry{}
	labelSets := map[string]map[string]string{}

	for _, event := range events {
		labels := map[string]string{"job": "kubby", "action": event.Action, "result": event.Result}
		for key, value := range cfg.ExtraLabels {
			labels[key] = value
		}

		key := labelKey(labels)
		line, err := json.Marshal(withLabels(event, cfg))
		if err != nil {
			return nil, "", fmt.Errorf("encode an audit event: %w", err)
		}
		grouped[key] = append(grouped[key], entry{at: event.Timestamp, line: string(line)})
		labelSets[key] = labels
	}

	streams := make([]map[string]any, 0, len(grouped))
	for key, entries := range grouped {
		// Loki refuses a stream whose entries are not in ascending time order.
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })

		values := make([][]string, 0, len(entries))
		for _, item := range entries {
			values = append(values, []string{
				strconv.FormatInt(item.at.UnixNano(), 10),
				item.line,
			})
		}
		streams = append(streams, map[string]any{"stream": labelSets[key], "values": values})
	}

	body, err := json.Marshal(map[string]any{"streams": streams})
	if err != nil {
		return nil, "", fmt.Errorf("encode the push: %w", err)
	}
	return body, "application/json", nil
}

func labelKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out strings.Builder
	for _, key := range keys {
		out.WriteString(key)
		out.WriteByte('=')
		out.WriteString(labels[key])
		out.WriteByte(',')
	}
	return out.String()
}

// ---------------------------------------------------------------- plain http

func encodeNDJSON(events []Shipped, cfg SinkConfig) ([]byte, string, error) {
	var body strings.Builder
	for _, event := range events {
		line, err := json.Marshal(withLabels(event, cfg))
		if err != nil {
			return nil, "", fmt.Errorf("encode an audit event: %w", err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	return []byte(body.String()), "application/x-ndjson", nil
}

// withLabels folds the deployment's own labels into the document, so a fleet shipping
// into one index can still be told apart.
func withLabels(event Shipped, cfg SinkConfig) Shipped {
	if len(cfg.ExtraLabels) == 0 {
		return event
	}

	details := make(map[string]any, len(event.Details)+len(cfg.ExtraLabels))
	for key, value := range event.Details {
		details[key] = value
	}
	for key, value := range cfg.ExtraLabels {
		details[key] = value
	}
	event.Details = details
	return event
}
