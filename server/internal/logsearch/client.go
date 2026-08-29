// Package logsearch reads application logs out of the system that already holds them.
//
// A shipper on every node is reading every container's log file as fast as it is written
// — that is its whole job. Asking Kubernetes for the same lines a second time, once per
// pod, would be the same work done worse and would put the cost on the cluster being
// observed. So Kubby never tails a pod for this: it asks the log store one question and
// gets one answer for the whole fleet.
//
// Only reading, and only aggregates plus a single sample line. A pod can write half a
// million error lines in a quarter of an hour (it has), and none of them belong in
// Kubby's memory.
package logsearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

// ErrNotConfigured is returned when a cluster has no log source, which is the state
// every cluster starts in and a perfectly ordinary one to be in.
var ErrNotConfigured = errors.New("no log source is configured")

// Config is one log store and how to reach it.
type Config struct {
	URL string
	// Index is the index or data stream pattern to search, such as
	// `logs-hybprod-app-*`. It is typed by an operator and never guessed: which indices
	// belong to which cluster is a local convention, and a wrong guess would attribute
	// one cluster's failures to another.
	Index string
	// Username with Secret as the password is basic auth; Secret alone is a bearer token
	// or an API key, depending on Scheme. The same shape the audit sinks use.
	Username string
	Secret   string
	Scheme   string
	// InsecureSkipVerify is offered because an internal Elasticsearch usually sits
	// behind a certificate from the same private CA the clusters use.
	InsecureSkipVerify bool
	Timeout            time.Duration
	// Fields names the parts of a document Kubby reads. Zero values fall back to
	// Fluent Bit's spelling.
	Fields Fields
}

// Configured reports whether there is anything to connect to.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.URL) != "" && strings.TrimSpace(c.Index) != ""
}

// Client queries one log store.
type Client struct {
	base  *url.URL
	cfg   Config
	http  *http.Client
	index string
}

// New builds a client, refusing an address it should not dial.
func New(cfg Config) (*Client, error) {
	address := strings.TrimSpace(cfg.URL)
	index := strings.TrimSpace(cfg.Index)
	if address == "" || index == "" {
		return nil, ErrNotConfigured
	}

	base, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("the log source address is not a URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("the log source address must be http or https, not %q", base.Scheme)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("the log source address has no host")
	}
	// A pattern is a path segment in every request built here, so it may not smuggle one
	// of its own — `../_cluster/settings` would otherwise be a valid "index".
	if strings.ContainsAny(index, "/?#") {
		return nil, fmt.Errorf("the index pattern may not contain /, ? or #")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Client{
		base:  base,
		cfg:   cfg,
		index: index,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				// The log store is a dependency, not the point: a slow one must not hold
				// a page open and a dead one must fail rather than hang.
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: timeout,
				MaxIdleConnsPerHost:   2,
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // deliberate, see Config
				},
			},
		},
	}, nil
}

// Index reports the pattern this client searches.
func (c *Client) Index() string { return c.index }

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	endpoint := *c.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + path

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode the query: %w", err)
		}
		reader = strings.NewReader(string(encoded))
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	c.authenticate(request)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("could not reach the log source: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	// Capped: an error body from a misconfigured endpoint can be a whole HTML page, and
	// none of it is worth holding.
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("could not read the log source's answer: %w", err)
	}

	if response.StatusCode >= 400 {
		return nil, statusError(response.StatusCode, payload)
	}
	return payload, nil
}

// authenticate never logs what it attaches, and the header is set on the request rather
// than carried in the URL so it cannot end up in an access log along the way.
func (c *Client) authenticate(request *http.Request) {
	secret := strings.TrimSpace(c.cfg.Secret)

	switch strings.ToLower(strings.TrimSpace(c.cfg.Scheme)) {
	case "bearer":
		if secret != "" {
			request.Header.Set("Authorization", "Bearer "+secret)
		}
	case "apikey":
		if secret != "" {
			request.Header.Set("Authorization", "ApiKey "+secret)
		}
	default:
		if c.cfg.Username != "" {
			request.SetBasicAuth(c.cfg.Username, secret)
		}
	}
}

// statusError turns a rejection into something an operator can act on. Elasticsearch
// says why in a structured body; the raw status alone sends people to the wrong place —
// a 401 is a credential, a 404 is a pattern that matches nothing.
func statusError(status int, payload []byte) error {
	var body struct {
		Error struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &body)

	detail := strings.TrimSpace(body.Error.Reason)
	if detail == "" {
		detail = strings.TrimSpace(string(payload))
	}
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}

	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("the log source rejected the credential (401): %s", detail)
	case http.StatusForbidden:
		return fmt.Errorf("the credential may not read this index (403): %s", detail)
	case http.StatusNotFound:
		return fmt.Errorf("nothing matches the index pattern (404): %s", detail)
	default:
		return fmt.Errorf("the log source answered %d: %s", status, detail)
	}
}
