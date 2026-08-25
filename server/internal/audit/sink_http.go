package audit

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SinkConfig is everything a shipping destination needs.
type SinkConfig struct {
	// Kind selects the wire format: elasticsearch, loki or http.
	Kind string
	URL  string
	// Index is Elasticsearch's index name, ignored elsewhere.
	Index string
	// Username with Token as the password is basic auth; Token alone is a bearer token
	// or an API key, depending on Scheme.
	Username string
	Token    string
	// Scheme picks how Token is presented: "bearer", "apikey", or empty for basic auth
	// alongside Username.
	Scheme             string
	InsecureSkipVerify bool
	// ExtraLabels are attached to every event, so a fleet shipping into one place can
	// still be told apart.
	ExtraLabels map[string]string
	// DataStream writes to an Elasticsearch data stream rather than a plain index.
	//
	// What Elastic recommends for append-only time series, which an audit trail is: it
	// handles rollover, retention and the backing indices itself. It also changes the
	// wire format — a data stream accepts `create` and rejects `index` — so this is not
	// a naming convention, it is a different request.
	DataStream bool
}

// httpSink is the shared transport. Elasticsearch and Loki differ only in the body they
// want and the path they want it at, so everything else lives here once.
type httpSink struct {
	name     string
	endpoint string
	cfg      SinkConfig
	client   *http.Client
	encode   func(events []Shipped, cfg SinkConfig) ([]byte, string, error)
}

func newHTTPSink(name, path string, cfg SinkConfig, encode func([]Shipped, SinkConfig) ([]byte, string, error)) (*httpSink, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil {
		return nil, fmt.Errorf("the sink address is not a URL: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("the sink address must be http or https, not %q", base.Scheme)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("the sink address has no host")
	}

	endpoint := *base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + path

	return &httpSink{
		name:     name,
		endpoint: endpoint.String(),
		cfg:      cfg,
		encode:   encode,
		client: &http.Client{
			Timeout: sendTimeout,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: sendTimeout,
				MaxIdleConnsPerHost:   2,
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					//nolint:gosec // an internal SIEM behind a private CA is the normal case
					InsecureSkipVerify: cfg.InsecureSkipVerify,
				},
			},
		},
	}, nil
}

func (s *httpSink) Name() string { return s.name }

func (s *httpSink) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

func (s *httpSink) Send(ctx context.Context, events []Shipped) error {
	body, contentType, err := s.encode(events, s.cfg)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build the request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	s.authorise(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", s.name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read regardless of status: the body is where these systems explain a rejection,
	// and a connection whose body is not drained cannot be reused.
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return s.checkBody(answer)
	}
	return fmt.Errorf("%s answered %d: %s", s.name, resp.StatusCode, trim(answer))
}

// checkBody exists for Elasticsearch, which answers 200 to a bulk request in which
// individual documents failed. A sink that reports success for a batch it partly
// discarded is worse than one that fails.
func (s *httpSink) checkBody(body []byte) error {
	if s.cfg.Kind != KindElasticsearch {
		return nil
	}
	return elasticsearchBulkErrors(body)
}

func (s *httpSink) authorise(req *http.Request) {
	switch {
	case s.cfg.Scheme == "bearer" && s.cfg.Token != "":
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	case s.cfg.Scheme == "apikey" && s.cfg.Token != "":
		// Elasticsearch's own scheme name, which is not "Bearer" and is not basic auth.
		req.Header.Set("Authorization", "ApiKey "+s.cfg.Token)
	case s.cfg.Username != "":
		req.SetBasicAuth(s.cfg.Username, s.cfg.Token)
	}
}

func trim(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		return text[:300] + "…"
	}
	if text == "" {
		return "no detail given"
	}
	return text
}

const maxErrorBytes = 16 << 10
