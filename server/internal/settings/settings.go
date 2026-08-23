// Package settings holds the deployment-wide options an admin edits at runtime.
package settings

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Keys under which each group is stored.
const (
	KeyNodeShell = "node_shell"
	KeyMetrics   = "metrics"
	KeyAuditSink = "audit_sink"
)

// Keys for the secrets those groups carry.
const (
	SecretMetricsPassword = "metrics.password"
	SecretAuditToken      = "audit_sink.token"
)

// NodeShell is where a node shell's pod comes from.
//
// The image is a full reference rather than a registry prefix: a node permitted to pull
// only from its own registry needs the whole path rewritten, and one field covers a
// proxy cache, a mirror and a self-built image without assuming the shape of any of them
// (ADR-065).
type NodeShell struct {
	Image      string `json:"image"`
	Namespace  string `json:"namespace"`
	PullSecret string `json:"pullSecret,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// DefaultNodeShell is off, because a node shell is root on the machine.
func DefaultNodeShell() NodeShell {
	return NodeShell{Image: "docker.io/library/alpine:3.20", Namespace: "kube-system"}
}

// Metrics is where historical measurements are read from.
//
// metrics-server keeps no history, so a chart over time needs something that does. This
// is the connection to it; what is asked of it arrives in phase 9.
type Metrics struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	// Username is half of basic auth; the password is sealed separately.
	Username string `json:"username,omitempty"`
	// HasPassword tells the browser one is stored without sending it.
	HasPassword        bool   `json:"hasPassword"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	Organization       string `json:"organization,omitempty"`
}

// AuditSink is where the audit stream is copied to.
//
// Kubby's own audit trail is never disabled (ADR-002); this is an additional destination,
// because a trail that lives only in the tool being audited is not much of a trail.
type AuditSink struct {
	Enabled bool `json:"enabled"`
	// Kind is elasticsearch, loki, syslog or http.
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Index    string `json:"index,omitempty"`
	Username string `json:"username,omitempty"`
	// HasToken tells the browser a credential is stored without sending it.
	HasToken           bool `json:"hasToken"`
	InsecureSkipVerify bool `json:"insecureSkipVerify"`
}

var sinkKinds = map[string]bool{"elasticsearch": true, "loki": true, "syslog": true, "http": true}

// Service reads and writes settings, sealing the secrets they carry.
type Service struct {
	repo    *store.SettingsRepo
	keyring *crypto.Keyring
}

func New(repo *store.SettingsRepo, keyring *crypto.Keyring) *Service {
	return &Service{repo: repo, keyring: keyring}
}

// All is everything an admin screen shows. Secrets are reported as present, never sent.
type All struct {
	NodeShell NodeShell `json:"nodeShell"`
	Metrics   Metrics   `json:"metrics"`
	AuditSink AuditSink `json:"auditSink"`
}

func (s *Service) All(ctx context.Context) (*All, error) {
	out := &All{NodeShell: DefaultNodeShell()}

	if _, err := s.repo.Get(ctx, KeyNodeShell, &out.NodeShell); err != nil {
		return nil, err
	}
	if _, err := s.repo.Get(ctx, KeyMetrics, &out.Metrics); err != nil {
		return nil, err
	}
	if _, err := s.repo.Get(ctx, KeyAuditSink, &out.AuditSink); err != nil {
		return nil, err
	}

	// Whether a credential exists is a fact about configuration; the credential itself
	// never leaves this process except towards the system it belongs to.
	_, _, hasPassword, err := s.repo.GetSecret(ctx, SecretMetricsPassword)
	if err != nil {
		return nil, err
	}
	out.Metrics.HasPassword = hasPassword

	_, _, hasToken, err := s.repo.GetSecret(ctx, SecretAuditToken)
	if err != nil {
		return nil, err
	}
	out.AuditSink.HasToken = hasToken

	return out, nil
}

// SaveNodeShell validates and stores the node shell settings.
func (s *Service) SaveNodeShell(ctx context.Context, value NodeShell, by uuid.UUID) error {
	value.Image = strings.TrimSpace(value.Image)
	value.Namespace = strings.TrimSpace(value.Namespace)

	if value.Image == "" {
		return fmt.Errorf("an image reference is required")
	}
	if strings.Contains(value.Image, " ") {
		return fmt.Errorf("an image reference cannot contain spaces")
	}
	if value.Namespace == "" {
		return fmt.Errorf("a namespace is required")
	}
	return s.repo.Put(ctx, KeyNodeShell, value, by)
}

// SaveMetrics stores the metrics connection, sealing the password when one is given.
//
// An empty password means "leave what is stored", not "clear it": a form that wipes a
// credential because the field was not retyped is a form that loses credentials.
func (s *Service) SaveMetrics(ctx context.Context, value Metrics, password string, clearPassword bool, by uuid.UUID) error {
	value.URL = strings.TrimSpace(value.URL)
	if value.Enabled {
		if err := validateURL(value.URL); err != nil {
			return err
		}
	}

	value.HasPassword = false
	if err := s.repo.Put(ctx, KeyMetrics, value, by); err != nil {
		return err
	}
	return s.storeSecret(ctx, SecretMetricsPassword, password, clearPassword, by)
}

// SaveAuditSink stores the audit destination.
func (s *Service) SaveAuditSink(ctx context.Context, value AuditSink, token string, clearToken bool, by uuid.UUID) error {
	value.URL = strings.TrimSpace(value.URL)
	value.Kind = strings.TrimSpace(value.Kind)

	if value.Enabled {
		if !sinkKinds[value.Kind] {
			return fmt.Errorf("unknown sink type %q", value.Kind)
		}
		if err := validateURL(value.URL); err != nil {
			return err
		}
	}

	value.HasToken = false
	if err := s.repo.Put(ctx, KeyAuditSink, value, by); err != nil {
		return err
	}
	return s.storeSecret(ctx, SecretAuditToken, token, clearToken, by)
}

func (s *Service) storeSecret(ctx context.Context, key, value string, clear bool, by uuid.UUID) error {
	if clear {
		return s.repo.DeleteSecret(ctx, key)
	}
	if value == "" {
		return nil
	}

	// Bound to its own key, so a row moved to another setting's name will not open.
	sealed, err := s.keyring.Seal([]byte(value), []byte(key))
	if err != nil {
		return fmt.Errorf("seal %s: %w", key, err)
	}
	return s.repo.PutSecret(ctx, key, sealed, s.keyring.ActiveVersion(), by)
}

// Secret opens one stored credential, for the code that talks to the system it belongs to.
func (s *Service) Secret(ctx context.Context, key string) (string, bool, error) {
	sealed, _, found, err := s.repo.GetSecret(ctx, key)
	if err != nil || !found {
		return "", false, err
	}

	plain, err := s.keyring.Open(sealed, []byte(key))
	if err != nil {
		return "", false, fmt.Errorf("open %s: %w", key, err)
	}
	return string(plain), true, nil
}

// validateURL refuses what cannot be dialled, before something else discovers it at
// three in the morning.
func validateURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("a URL is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("that is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("the URL must start with http:// or https://")
	}
	if parsed.Host == "" {
		return fmt.Errorf("the URL has no host")
	}
	return nil
}
