package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ensureDataStream puts the index template a data stream needs.
//
// Without a matching template, a `create` into a name that does not exist quietly makes an
// ordinary index instead — the shipping works, nothing errors, and the reader ends up with
// exactly the thing they asked not to have. So the template is not optional and its
// absence is not something to discover later.
//
// Idempotent: Elasticsearch replaces a template of the same name, and the mapping below is
// the same every time.
func (s *httpSink) ensureDataStream(ctx context.Context) error {
	name := strings.TrimSpace(s.cfg.Index)
	if name == "" {
		name = DefaultAuditIndex
	}

	template := map[string]any{
		// Exactly this stream, not a wildcard over the cluster's namespace: a template
		// matching more than it should would silently turn somebody else's index into a
		// data stream.
		"index_patterns": []string{name},
		"data_stream":    map[string]any{},
		// Above the defaults, so a template someone wrote themselves wins. An operator who
		// has tuned retention or mappings should not have Kubby overwrite them on restart.
		"priority": 100,
		"template": map[string]any{
			"mappings": map[string]any{
				"properties": map[string]any{
					// The timestamp field a data stream is built around. Named @timestamp
					// because that is the only name Elasticsearch accepts by default.
					"@timestamp":   map[string]any{"type": "date"},
					"action":       keyword(),
					"result":       keyword(),
					"actor":        keyword(),
					"actorId":      keyword(),
					"clusterId":    keyword(),
					"namespace":    keyword(),
					"resourceKind": keyword(),
					"resourceName": keyword(),
					"clientIp":     map[string]any{"type": "ip"},
					"requestId":    keyword(),
					// Details carries whatever an action attached. Left unindexed rather
					// than mapped: one action adding a field of a new type would otherwise
					// break every document after it with a mapping conflict.
					"details": map[string]any{"type": "object", "enabled": false},
				},
			},
		},
		"_meta": map[string]any{
			"description": "Kubby audit trail. Created by Kubby; edit or replace with a higher-priority template.",
		},
	}

	body, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("build the index template: %w", err)
	}

	endpoint, err := s.templateURL(name)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build the template request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	s.authorise(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Elasticsearch to create the data stream template: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("these credentials may not create an index template, "+
			"which a data stream needs: %s", trim(answer))
	}
	return fmt.Errorf("the data stream template was refused (%d): %s",
		resp.StatusCode, trim(answer))
}

// templateURL points at _index_template on the same host the bulk endpoint uses, rather
// than at the bulk path itself.
func (s *httpSink) templateURL(name string) (string, error) {
	base, err := url.Parse(s.endpoint)
	if err != nil {
		return "", fmt.Errorf("the sink address is not a URL: %w", err)
	}
	base.Path = strings.TrimSuffix(strings.TrimSuffix(base.Path, "/_bulk"), "/") +
		"/_index_template/kubby-audit-" + name
	return base.String(), nil
}

func keyword() map[string]any {
	// keyword rather than text: every one of these is filtered and aggregated on, never
	// searched for a word inside. An analysed field would make `actor:"a@example.com"`
	// match anyone at that domain.
	return map[string]any{"type": "keyword"}
}
