package logsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Severity says how hard to press the point.
const (
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Finding is one pod that keeps saying something is wrong.
//
// Never the lines themselves. A single pod wrote 456,672 matching lines in a quarter of
// an hour in the deployment this was built for; what a reader needs from that is the
// count, how long it has been going on, and one line that says what it is.
type Finding struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	// Rule and Class name what matched and what kind of problem it is.
	Rule  string `json:"rule"`
	Class string `json:"class"`
	Count int64  `json:"count"`
	// Summary is the identity of the thing that failed, pulled out of the message:
	// which database, which user, which address.
	Summary string `json:"summary,omitempty"`
	// Sample is one matched line, redacted and truncated.
	Sample    string    `json:"sample"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Severity  string    `json:"severity"`
	// Pods is how many pods a rolled-up finding covers. Absent on a pod's own finding.
	Pods int `json:"pods,omitempty"`
}

// sliceCount is how many pieces the window is cut into to tell a pod that is failing
// continuously from one that stumbled once. Three is enough to make the distinction and
// few enough that a short window still has usable pieces.
const sliceCount = 3

// maxPods caps the terms aggregation. A deployment where more pods than this are
// complaining has one incident, not two hundred, and the tail adds nothing.
const maxPods = 200

// minCount is how many matching lines a pod must have written before it is worth
// saying anything.
//
// A retry that succeeded on the second attempt logged one failure, and reporting that
// as a problem is how a list fills with marks nobody reads. Something genuinely broken
// says so repeatedly — the failure this was built for repeated every fifteen seconds
// for twenty-two hours.
const minCount = 3

// maxSampleLine is what a tooltip can hold. A Java stack trace arrives as one document
// with thirty lines in it, and twenty-nine of them are frames.
const maxSampleLine = 400

// Sweep asks the log store one question about the whole cluster.
//
// One request, whatever the pod count: the aggregation does the grouping where the data
// already is. Reading logs pod by pod would be thousands of requests against the API
// server of the cluster being watched, to learn something the log store can answer in a
// hundred milliseconds.
func (c *Client) Sweep(ctx context.Context, rules []Rule, window time.Duration) ([]Finding, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	fields := c.cfg.Fields.withDefaults()

	// How the message field is indexed decides what kind of query can find a substring
	// in it, and getting that wrong finds nothing without erroring.
	mapping := c.messageMapping(ctx, fields.Message)

	query := sweepQuery(rules, fields, window, mapping)
	if query == nil {
		return nil, nil
	}

	payload, err := c.do(ctx, "POST", "/"+c.index+"/_search", query)
	if err != nil {
		return nil, err
	}

	var response sweepResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("could not read the search result: %w", err)
	}
	return response.findings(rules, window), nil
}

func sweepQuery(rules []Rule, fields Fields, window time.Duration, mapping MessageMapping) map[string]any {
	should := make([]any, 0, len(rules))
	perRule := make(map[string]any, len(rules))
	for _, rule := range rules {
		query := rule.query(fields.Message, mapping)
		if query == nil {
			continue
		}
		should = append(should, query)
		perRule[rule.Name] = query
	}
	if len(should) == 0 {
		return nil
	}

	minutes := int(window / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	sliceMinutes := minutes / sliceCount
	if sliceMinutes < 1 {
		sliceMinutes = 1
	}

	return map[string]any{
		"size": 0,
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []any{
					map[string]any{"range": map[string]any{
						fields.Timestamp: map[string]any{"gte": fmt.Sprintf("now-%dm", minutes)},
					}},
				},
				"must": []any{
					map[string]any{"bool": map[string]any{"should": should, "minimum_should_match": 1}},
				},
			},
		},
		"aggs": map[string]any{
			"pods": map[string]any{
				"terms": map[string]any{
					"field": fields.Pod,
					"size":  maxPods,
					"order": map[string]any{"_count": "desc"},
				},
				"aggs": map[string]any{
					"namespace":  map[string]any{"terms": map[string]any{"field": fields.Namespace, "size": 1}},
					"container":  map[string]any{"terms": map[string]any{"field": fields.Container, "size": 1}},
					"first_seen": map[string]any{"min": map[string]any{"field": fields.Timestamp}},
					"last_seen":  map[string]any{"max": map[string]any{"field": fields.Timestamp}},
					// Which slices of the window it was failing in, which is what tells
					// a continuing outage from a single stumble.
					"slices": map[string]any{"date_histogram": map[string]any{
						"field":          fields.Timestamp,
						"fixed_interval": fmt.Sprintf("%dm", sliceMinutes),
						"min_doc_count":  1,
					}},
					"rules": map[string]any{"filters": map[string]any{"filters": perRule}},
					"sample": map[string]any{"top_hits": map[string]any{
						"size":    1,
						"sort":    []any{map[string]any{fields.Timestamp: "desc"}},
						"_source": []string{fields.Message},
					}},
				},
			},
		},
	}
}

type sweepResponse struct {
	Aggregations struct {
		Pods struct {
			Buckets []struct {
				Key      string `json:"key"`
				DocCount int64  `json:"doc_count"`

				Namespace termsAgg `json:"namespace"`
				Container termsAgg `json:"container"`
				FirstSeen valueAgg `json:"first_seen"`
				LastSeen  valueAgg `json:"last_seen"`

				Slices struct {
					Buckets []struct {
						DocCount int64 `json:"doc_count"`
					} `json:"buckets"`
				} `json:"slices"`

				Rules struct {
					Buckets map[string]struct {
						DocCount int64 `json:"doc_count"`
					} `json:"buckets"`
				} `json:"rules"`

				Sample struct {
					Hits struct {
						Hits []struct {
							Source map[string]any `json:"_source"`
						} `json:"hits"`
					} `json:"hits"`
				} `json:"sample"`
			} `json:"buckets"`
		} `json:"pods"`
	} `json:"aggregations"`
}

type termsAgg struct {
	Buckets []struct {
		Key string `json:"key"`
	} `json:"buckets"`
}

func (t termsAgg) first() string {
	if len(t.Buckets) == 0 {
		return ""
	}
	return t.Buckets[0].Key
}

type valueAgg struct {
	Value float64 `json:"value"`
}

func (v valueAgg) time() time.Time {
	if v.Value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(v.Value)).UTC()
}

func (r sweepResponse) findings(rules []Rule, _ time.Duration) []Finding {
	byName := make(map[string]Rule, len(rules))
	for _, rule := range rules {
		byName[rule.Name] = rule
	}

	findings := make([]Finding, 0, len(r.Aggregations.Pods.Buckets))
	for _, bucket := range r.Aggregations.Pods.Buckets {
		if bucket.DocCount < minCount {
			continue
		}
		rule, ok := byName[winningRule(bucket.Rules.Buckets, rules)]
		if !ok {
			continue
		}

		message := ""
		if hits := bucket.Sample.Hits.Hits; len(hits) > 0 {
			message = messageOf(hits[0].Source)
		}

		// The lines belong to somebody else's application and Kubby did not write them;
		// a connection string in one is ordinary and this is about to be rendered in a
		// browser (ADR-010).
		sample := redactLine(message)

		findings = append(findings, Finding{
			Namespace: bucket.Namespace.first(),
			Pod:       bucket.Key,
			Container: bucket.Container.first(),
			Rule:      rule.Name,
			Class:     rule.Class,
			Count:     bucket.DocCount,
			Summary:   rule.Summarise(message),
			Sample:    sample,
			FirstSeen: bucket.FirstSeen.time(),
			LastSeen:  bucket.LastSeen.time(),
			Severity:  severityOf(len(bucket.Slices.Buckets)),
		})
	}

	// Loudest first, and stable so a list does not reshuffle between sweeps.
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Count != findings[j].Count {
			return findings[i].Count > findings[j].Count
		}
		return findings[i].Pod < findings[j].Pod
	})
	return findings
}

// winningRule picks the rule that explains the pod.
//
// Rules are tried in the order they are declared rather than by how many lines each
// matched: the generic net at the end of the list matches everything the specific ones
// do, and "Application error" is a worse answer than "SQL Server" for the same lines.
func winningRule(matched map[string]struct {
	DocCount int64 `json:"doc_count"`
}, rules []Rule) string {
	for _, rule := range rules {
		if matched[rule.Name].DocCount > 0 {
			return rule.Name
		}
	}
	return ""
}

// severityOf reads how much of the window the pod was failing in.
//
// Failing in every slice is an outage that is still going; failing in one is something
// that happened. Both are worth showing, and pretending they are the same thing is how
// a mark stops meaning anything.
func severityOf(slicesSeen int) string {
	if slicesSeen >= sliceCount {
		return SeverityError
	}
	return SeverityWarning
}

func messageOf(source map[string]any) string {
	// Fluent Bit writes a flat `log`; an ECS pipeline nests. Both are read the same way.
	if text, ok := source["log"].(string); ok {
		return text
	}
	if text, ok := source["message"].(string); ok {
		return text
	}
	for _, value := range source {
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

// redactLine prepares a matched line for the screen: secrets out, first line only,
// length capped. The frames of a stack trace say where, not what.
func redactLine(message string) string {
	line := strings.TrimSpace(redactString(message))
	if head, _, found := strings.Cut(line, "\n"); found {
		line = strings.TrimSpace(head)
	}
	return truncateTo(line, maxSampleLine)
}
