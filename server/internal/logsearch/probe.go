package logsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/erolbeyaz/kubby/internal/logging"
)

// Probe is what a connection test found.
//
// Enough to tell the three ways this goes wrong apart without opening Kibana: the
// address is unreachable, the credential is refused, or the pattern matches nothing.
// The sample document is included because the last of those is the one that looks like
// success — a pattern that resolves to real indices holding somebody else's logs.
type Probe struct {
	// Cluster and Version identify the store that answered, so an operator who typed
	// the wrong address sees it rather than a green tick.
	Cluster string `json:"cluster,omitempty"`
	Version string `json:"version,omitempty"`
	// Indices is how many concrete indices and data streams the pattern resolves to.
	Indices int `json:"indices"`
	// Documents counts what the pattern holds over the window, whatever it says.
	Documents     int64 `json:"documents"`
	WindowMinutes int   `json:"windowMinutes"`
	// Sample is one whole document, so the operator can read the field names off it.
	// Kubby needs to know which field carries the message and which the pod name, and
	// showing the document is a better answer than asking someone to remember.
	Sample map[string]any `json:"sample,omitempty"`
	// SampleAt is when that document was written, which says whether the stream is live.
	SampleAt string `json:"sampleAt,omitempty"`
	// Message is how the store indexes the field the rules are matched against. A
	// keyword field cannot be searched for a substring by a phrase query, and a length
	// limit on one silently drops the long stack traces this feature exists to find —
	// neither shows up as an error, only as nothing ever being found.
	Message MessageMapping `json:"message"`
	// MessageField is which field that is, since it is configurable.
	MessageField string `json:"messageField,omitempty"`
}

// Probe reports whether the configured source can be reached and whether the pattern
// points at anything.
func (c *Client) Probe(ctx context.Context, window time.Duration) (*Probe, error) {
	if window <= 0 {
		window = 15 * time.Minute
	}
	result := &Probe{WindowMinutes: int(window / time.Minute)}

	root, err := c.do(ctx, "GET", "/", nil)
	if err != nil {
		return nil, err
	}
	var identity struct {
		ClusterName string `json:"cluster_name"`
		Version     struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := json.Unmarshal(root, &identity); err != nil {
		// Something answered on that address but it is not Elasticsearch. Saying so is
		// more useful than reporting a parse failure of an unnamed document.
		return nil, fmt.Errorf("the address answered but does not look like Elasticsearch")
	}
	result.Cluster, result.Version = identity.ClusterName, identity.Version.Number

	count, err := c.resolveIndices(ctx)
	if err != nil {
		return nil, err
	}
	result.Indices = count

	documents, sample, at, err := c.sample(ctx, window)
	if err != nil {
		return nil, err
	}
	result.Documents, result.Sample, result.SampleAt = documents, sample, at

	fields := c.cfg.Fields.withDefaults()
	result.MessageField = fields.Message
	result.Message = c.messageMapping(ctx, fields.Message)

	return result, nil
}

// resolveIndices counts what the pattern actually names. A pattern matching nothing is
// not an error to Elasticsearch — a search over it succeeds and finds none — so it has
// to be asked directly, or a typo reads as a quiet cluster.
func (c *Client) resolveIndices(ctx context.Context) (int, error) {
	payload, err := c.do(ctx, "GET", "/_resolve/index/"+c.index, nil)
	if err != nil {
		return 0, err
	}

	var resolved struct {
		Indices     []json.RawMessage `json:"indices"`
		Aliases     []json.RawMessage `json:"aliases"`
		DataStreams []json.RawMessage `json:"data_streams"`
	}
	if err := json.Unmarshal(payload, &resolved); err != nil {
		return 0, fmt.Errorf("could not read the index list: %w", err)
	}
	return len(resolved.Indices) + len(resolved.Aliases) + len(resolved.DataStreams), nil
}

func (c *Client) sample(ctx context.Context, window time.Duration) (int64, map[string]any, string, error) {
	query := map[string]any{
		"size":             1,
		"sort":             []any{map[string]any{"@timestamp": "desc"}},
		"track_total_hits": true,
		"query": map[string]any{
			"range": map[string]any{
				"@timestamp": map[string]any{"gte": fmt.Sprintf("now-%dm", int(window/time.Minute))},
			},
		},
	}

	payload, err := c.do(ctx, "POST", "/"+c.index+"/_search", query)
	if err != nil {
		return 0, nil, "", err
	}

	var response struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return 0, nil, "", fmt.Errorf("could not read the search result: %w", err)
	}

	if len(response.Hits.Hits) == 0 {
		return response.Hits.Total.Value, nil, "", nil
	}

	source := response.Hits.Hits[0].Source
	at, _ := source["@timestamp"].(string)

	redacted, _ := redactSample(source).(map[string]any)
	return response.Hits.Total.Value, redacted, at, nil
}

// maxSampleField caps one value of the sample document. A single log entry here is a
// whole Java stack trace often enough that showing it in full would bury the field names
// the sample is there to reveal.
const maxSampleField = 600

// redactSample walks a document the way the logger walks a record.
//
// These are somebody else's application logs and Kubby did not write them: a connection
// string, a bearer token or a password ends up in them regularly, and this document is
// about to be rendered in a browser. The redaction layer is not optional (ADR-010) and
// a value read out of a log store is exactly the kind it exists for.
func redactSample(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if logging.IsSensitiveKey(key) {
				out[key] = logging.Redacted
				continue
			}
			out[key] = redactSample(item)
		}
		return out

	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactSample(item)
		}
		return out

	case string:
		return truncate(redactString(typed))

	default:
		return value
	}
}

func redactString(s string) string { return logging.RedactString(s) }

func truncate(s string) string { return truncateTo(s, maxSampleField) }

func truncateTo(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// Cut on a rune boundary; a log line is not guaranteed to be ASCII and half a rune
	// renders as a replacement character.
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
