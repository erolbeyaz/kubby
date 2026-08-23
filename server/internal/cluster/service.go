package cluster

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"k8s.io/client-go/rest"

	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/store"
)

var (
	ErrResourceNotFound   = errors.New("resource not found")
	ErrClusterForbidden   = errors.New("the cluster credential is not permitted")
	ErrCredentialRejected = errors.New("the cluster credential was rejected")
	ErrKindUnavailable    = errors.New("this kind is not available on this cluster")

	ErrReadOnlyCluster = errors.New("this cluster is locked read-only")
	ErrNoCredential    = errors.New("cluster has no stored credential")
	ErrNotPermitted    = errors.New("you do not have access to this cluster")
)

// Settings tune how the service connects to clusters.
type Settings struct {
	DefaultQPS     float32
	DefaultBurst   int
	Timeout        time.Duration
	ProxyURL       string
	ExtraCAs       *x509.CertPool
	AllowLoopback  bool
	AllowInCluster bool
}

// Service owns the cluster lifecycle: validating pasted kubeconfigs, storing them
// encrypted, and producing clients.
type Service struct {
	clusters  *store.ClusterRepo
	keyring   *crypto.Keyring
	settings  Settings
	pool      *InformerPool
	discovery *discoveryCache
}

func NewService(db *store.DB, keyring *crypto.Keyring, settings Settings) *Service {
	return &Service{
		clusters:  db.Clusters(),
		keyring:   keyring,
		settings:  settings,
		discovery: newDiscoveryCache(),
	}
}

// WithInformerPool attaches a cache. Without one the service still works, listing
// everything on demand — which is what tests and one-shot commands want.
func (s *Service) WithInformerPool(pool *InformerPool) *Service {
	s.pool = pool
	return s
}

// AddressPolicy is the SSRF policy the service applies to pasted kubeconfigs.
func (s *Service) AddressPolicy() AddressPolicy {
	return AddressPolicy{AllowLoopback: s.settings.AllowLoopback}
}

// ValidationResult is the preview shown before anything is written down (ADR-018).
type ValidationResult struct {
	Contexts       []ContextInfo
	CurrentContext string
	// Probe is the outcome of actually connecting with the selected context, so the
	// user sees whether the credential works before committing to it.
	Probe *Health
}

// Validate parses a pasted kubeconfig and, when the selected context is usable, probes
// it. Nothing is stored: this is what makes "check before you save" possible.
func (s *Service) Validate(ctx context.Context, raw []byte, contextName string) (*ValidationResult, error) {
	parsed, err := ParseKubeconfig(raw, s.AddressPolicy())
	if err != nil {
		return nil, err
	}

	result := &ValidationResult{
		Contexts:       parsed.Contexts,
		CurrentContext: parsed.CurrentContext,
	}

	selected, err := parsed.Context(contextName)
	if err != nil {
		// The kubeconfig is structurally fine; the chosen context just is not usable.
		// Return the list anyway so the user can pick a different one.
		return result, nil
	}

	cfg, err := RESTConfig(raw, s.connectionOptions(selected.Name, 0, nil))
	if err != nil {
		return nil, err
	}

	health := Probe(ctx, cfg, s.settings.Timeout)
	result.Probe = &health
	return result, nil
}

// CreateInput is a validated cluster registration.
type CreateInput struct {
	Name             string
	Environment      string
	EnvironmentLabel string
	Color            string
	Kubeconfig       []byte
	ContextName      string
	ProxyURL         *string
	CreatedBy        uuid.UUID
}

// Create stores a cluster with its kubeconfig encrypted, then records what the probe
// found. The credential never touches the database in plaintext.
func (s *Service) Create(ctx context.Context, in CreateInput) (*store.Cluster, error) {
	parsed, err := ParseKubeconfig(in.Kubeconfig, s.AddressPolicy())
	if err != nil {
		return nil, err
	}
	selected, err := parsed.Context(in.ContextName)
	if err != nil {
		return nil, err
	}

	// A fresh identifier is needed before encrypting: it binds the ciphertext to this
	// row so a blob copied onto another cluster fails to decrypt.
	clusterID := uuid.New()
	sealed, err := s.keyring.Seal(in.Kubeconfig, []byte(clusterID.String()))
	if err != nil {
		return nil, fmt.Errorf("encrypt kubeconfig: %w", err)
	}

	created, err := s.clusters.CreateWithID(ctx, clusterID, store.NewCluster{
		Name:                  strings.TrimSpace(in.Name),
		Environment:           in.Environment,
		EnvironmentLabel:      in.EnvironmentLabel,
		Color:                 in.Color,
		AuthSource:            "kubeconfig",
		APIServerURL:          selected.Server,
		InsecureSkipTLSVerify: selected.InsecureSkipTLSVerify,
		ProxyURL:              in.ProxyURL,
		ContextName:           selected.Name,
		KubeconfigEnc:         sealed,
		CreatedBy:             in.CreatedBy,
	})
	if err != nil {
		return nil, err
	}

	s.refreshHealth(ctx, created)
	return s.clusters.ByID(ctx, created.ID)
}

// ReplaceCredential swaps in a newly pasted kubeconfig. This is the only way to change
// a stored credential: it is never sent back to the client to be edited (ADR-018).
func (s *Service) ReplaceCredential(ctx context.Context, clusterID uuid.UUID, raw []byte, contextName string) error {
	parsed, err := ParseKubeconfig(raw, s.AddressPolicy())
	if err != nil {
		return err
	}
	selected, err := parsed.Context(contextName)
	if err != nil {
		return err
	}

	sealed, err := s.keyring.Seal(raw, []byte(clusterID.String()))
	if err != nil {
		return fmt.Errorf("encrypt kubeconfig: %w", err)
	}
	if err := s.clusters.ReplaceCredential(ctx, clusterID, selected.Name, sealed, selected.Server, selected.InsecureSkipTLSVerify); err != nil {
		return err
	}

	cluster, err := s.clusters.ByID(ctx, clusterID)
	if err != nil {
		return err
	}
	s.refreshHealth(ctx, cluster)
	return nil
}

// RESTConfigFor decrypts a cluster's credential and builds a client configuration.
func (s *Service) RESTConfigFor(ctx context.Context, cluster *store.Cluster, impersonate *ImpersonationConfig) (*rest.Config, error) {
	if cluster.AuthSource == "in-cluster" {
		if !s.settings.AllowInCluster || !RunningInCluster() {
			return nil, fmt.Errorf("in-cluster access is not enabled")
		}
		return InClusterRESTConfig(s.connectionOptions("", cluster.QPSLimit, impersonate))
	}

	contextName, sealed, err := s.clusters.Credential(ctx, cluster.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoCredential
	}
	if err != nil {
		return nil, err
	}

	raw, err := s.keyring.Open(sealed, []byte(cluster.ID.String()))
	if err != nil {
		return nil, fmt.Errorf("decrypt kubeconfig: %w", err)
	}

	opts := s.connectionOptions(contextName, cluster.QPSLimit, impersonate)
	if cluster.ProxyURL != nil {
		opts.ProxyURL = *cluster.ProxyURL
	}

	return RESTConfig(raw, opts)
}

// Refresh probes a cluster and records the outcome.
func (s *Service) Refresh(ctx context.Context, cluster *store.Cluster) (*store.Cluster, error) {
	s.refreshHealth(ctx, cluster)
	return s.clusters.ByID(ctx, cluster.ID)
}

func (s *Service) refreshHealth(ctx context.Context, cluster *store.Cluster) {
	health := Health{Status: StatusUnreachable, Detail: "no credential"}

	if cfg, err := s.RESTConfigFor(ctx, cluster, nil); err == nil {
		health = Probe(ctx, cfg, s.settings.Timeout)
	} else {
		health.Detail = err.Error()
	}

	nodeCount := health.NodeCount
	_ = s.clusters.RecordHealth(ctx, cluster.ID, store.HealthUpdate{
		CredentialStatus: health.Status,
		StatusDetail:     health.Detail,
		K8sVersion:       health.K8sVersion,
		NodeCount:        nodeCount,
		MetricsAvailable: health.MetricsAvailable,
	})
}

func (s *Service) connectionOptions(contextName string, qps int, impersonate *ImpersonationConfig) ConnectionOptions {
	opts := ConnectionOptions{
		ContextName: contextName,
		QPS:         s.settings.DefaultQPS,
		Burst:       s.settings.DefaultBurst,
		Timeout:     s.settings.Timeout,
		ProxyURL:    s.settings.ProxyURL,
		ExtraCAs:    s.settings.ExtraCAs,
		Impersonate: impersonate,
	}
	if qps > 0 {
		opts.QPS = float32(qps)
		opts.Burst = qps * 2
	}
	return opts
}
