// Package promql reads a Prometheus-compatible HTTP API.
//
// Only the query endpoints are used, and only for reading. Kubby does not scrape, store
// or alert — Prometheus already does all three, and duplicating it would mean a second
// set of numbers that disagrees with the one the team already trusts.
//
// What it is here for is the one thing the Kubernetes API cannot answer: what happened
// before now. metrics-server keeps no history, events expire within the hour, and a
// restart count says nothing about whether it is getting worse.
package promql

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is one endpoint and how to reach it.
type Config struct {
	URL      string
	Username string
	Password string
	// InsecureSkipVerify is offered because an internal Prometheus is often behind a
	// certificate from the same private CA the clusters use, and refusing to look at it
	// helps nobody.
	InsecureSkipVerify bool
	Timeout            time.Duration

	// HTTPClient replaces the transport this client would otherwise build.
	//
	// It exists so a Prometheus with no address of its own can be reached through its
	// cluster's API server, authenticated by the credential Kubby already holds. That
	// path needs client certificates and a bearer token this package has no business
	// knowing about, so the caller hands over a client instead of describing one.
	HTTPClient *http.Client
}

// Client queries one Prometheus.
type Client struct {
	base *url.URL
	auth Config
	http *http.Client
}

// New builds a client, refusing an address it should not dial.
func New(cfg Config) (*Client, error) {
	trimmed := strings.TrimSpace(cfg.URL)
	if trimmed == "" {
		return nil, ErrNotConfigured
	}

	base, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("the metrics address is not a URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("the metrics address must be http or https, not %q", base.Scheme)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("the metrics address has no host")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	if cfg.HTTPClient != nil {
		// Copied rather than mutated: it belongs to the caller, and a timeout set here
		// would follow it everywhere else it is used.
		injected := *cfg.HTTPClient
		if injected.Timeout <= 0 {
			injected.Timeout = timeout
		}
		return &Client{base: base, auth: cfg, http: &injected}, nil
	}

	return &Client{
		base: base,
		auth: cfg,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				// Prometheus is a dependency, not the point: a slow one must not hold a
				// page open, and a dead one must fail rather than hang.
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: timeout,
				MaxIdleConnsPerHost:   4,
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // deliberate, see Config
				},
			},
		},
	}, nil
}

// Sample is one instant value with the labels that identify it.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Series is one line on a chart.
type Series struct {
	Labels map[string]string
	Points []Point
}

// Point is a value at a moment. The time is always UTC (ADR-026).
//
// Tagged, because this one crosses the wire: every chart on the cluster dashboard is a
// list of these, and without tags they serialise as "At" and "Value" while the client
// validates for "at" and "value". The whole response failed to parse over it, and the
// panel showed zeros as though the cluster were empty.
type Point struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// Query asks for the value of an expression now.
func (c *Client) Query(ctx context.Context, expr string) ([]Sample, error) {
	form := url.Values{"query": {expr}}

	body, err := c.post(ctx, "/api/v1/query", form)
	if err != nil {
		return nil, err
	}

	var parsed instantResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("the metrics endpoint returned something that is not a Prometheus answer")
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus refused the query: %s", orUnknown(parsed.Error))
	}

	out := make([]Sample, 0, len(parsed.Data.Result))
	for _, item := range parsed.Data.Result {
		value, ok := readValue(item.Value)
		if !ok {
			continue
		}
		out = append(out, Sample{Labels: item.Metric, Value: value})
	}
	return out, nil
}

// QueryRange asks for an expression over a window, which is the whole reason Prometheus
// is here: a number now is already on the screen, a shape over an hour is not.
func (c *Client) QueryRange(ctx context.Context, expr string, window, step time.Duration) ([]Series, error) {
	end := time.Now().UTC()
	start := end.Add(-window)

	form := url.Values{
		"query": {expr},
		"start": {formatTime(start)},
		"end":   {formatTime(end)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}

	body, err := c.post(ctx, "/api/v1/query_range", form)
	if err != nil {
		return nil, err
	}

	var parsed rangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("the metrics endpoint returned something that is not a Prometheus answer")
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus refused the query: %s", orUnknown(parsed.Error))
	}

	out := make([]Series, 0, len(parsed.Data.Result))
	for _, item := range parsed.Data.Result {
		points := make([]Point, 0, len(item.Values))
		for _, raw := range item.Values {
			at, value, ok := readPoint(raw)
			if !ok {
				continue
			}
			points = append(points, Point{At: at, Value: value})
		}
		out = append(out, Series{Labels: item.Metric, Points: points})
	}
	return out, nil
}

// Ping reports whether the endpoint is a Prometheus that will answer, so the settings
// screen can say so before anyone relies on it.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Query(ctx, "vector(1)")
	return err
}

// post sends the expression in the body rather than the query string: a PromQL expression
// is easily longer than the URL length some proxies accept, and a truncated query fails
// in a way nobody can read.
func (c *Client) post(ctx context.Context, path string, form url.Values) ([]byte, error) {
	endpoint := *c.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(),
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build the metrics request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.auth.Username != "" {
		req.SetBasicAuth(c.auth.Username, c.auth.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the metrics endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded: a misconfigured address could be anything, and this must not be a way to
	// read an arbitrary large response into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("could not read the metrics response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusBadRequest, http.StatusUnprocessableEntity:
		// Prometheus reports a bad query with 400 and a JSON body worth showing.
		return body, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("the metrics endpoint refused these credentials")
	case http.StatusNotFound:
		return nil, fmt.Errorf("no Prometheus API at that address — the URL should be its root, not a path")
	}
	return nil, fmt.Errorf("the metrics endpoint answered %d", resp.StatusCode)
}

// ErrNotConfigured means no endpoint was given, which is not a failure: a cluster with no
// Prometheus is a normal cluster, and the panel simply says so.
var ErrNotConfigured = fmt.Errorf("no metrics endpoint is configured for this cluster")

type instantResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

type rangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// readValue pulls the number out of Prometheus's [timestamp, "value"] pair, where the
// value is a string so that NaN and Inf survive JSON.
func readValue(pair []any) (float64, bool) {
	if len(pair) != 2 {
		return 0, false
	}
	text, ok := pair[1].(string)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	// NaN and Inf are dropped rather than carried.
	//
	// Prometheus returns them routinely — histogram_quantile over an empty histogram is
	// NaN, and a rate divided by zero is Inf — and Go's JSON encoder refuses both. One
	// NaN from one quantile on one cluster would fail the encoding of the entire
	// response, and the dashboard would show nothing at all rather than the one panel
	// that had no data. Dropping the sample here makes it read as "no data", which is
	// what it means.
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func readPoint(pair []any) (time.Time, float64, bool) {
	value, ok := readValue(pair)
	if !ok {
		return time.Time{}, 0, false
	}
	seconds, ok := pair[0].(float64)
	if !ok {
		return time.Time{}, 0, false
	}
	return time.UnixMilli(int64(seconds * 1000)).UTC(), value, true
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', 3, 64)
}

func orUnknown(text string) string {
	if strings.TrimSpace(text) == "" {
		return "no reason given"
	}
	return text
}

const (
	defaultTimeout   = 15 * time.Second
	maxResponseBytes = 8 << 20
)
