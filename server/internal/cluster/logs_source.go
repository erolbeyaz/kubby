package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/erolbeyaz/kubby/internal/logsearch"
	"github.com/erolbeyaz/kubby/internal/store"
)

// LogsConfig assembles what this cluster's log store needs, decrypting the secret at the
// moment it is used rather than carrying it around on the cluster struct.
func (s *Service) LogsConfig(ctx context.Context, cluster *store.Cluster) (logsearch.Config, error) {
	secret, err := s.logsSecretFor(ctx, cluster)
	if err != nil {
		return logsearch.Config{}, err
	}

	return logsearch.Config{
		URL:                cluster.LogsURL,
		Index:              cluster.LogsIndex,
		Username:           cluster.LogsUsername,
		Secret:             secret,
		Scheme:             cluster.LogsAuthScheme,
		InsecureSkipVerify: cluster.LogsInsecureSkipVerify,
	}, nil
}

// LogsClient builds a reader for this cluster's logs, or reports that it has none.
//
// The field names come from the caller because they are configuration shared by the
// whole deployment, while the address and credential belong to this cluster.
func (s *Service) LogsClient(ctx context.Context, cluster *store.Cluster, fields logsearch.Fields) (*logsearch.Client, error) {
	cfg, err := s.LogsConfig(ctx, cluster)
	if err != nil {
		return nil, err
	}
	cfg.Fields = fields
	return logsearch.New(cfg)
}

// ProbeLogs runs a connection test.
//
// The override carries what an operator has typed but not yet saved: a test that only
// ever checked the stored values would mean saving a wrong address to find out it is
// wrong. An empty field in the override falls back to what is stored, so testing after
// a save works without retyping the credential.
func (s *Service) ProbeLogs(ctx context.Context, cluster *store.Cluster, override logsearch.Config, window time.Duration) (*logsearch.Probe, error) {
	stored, err := s.LogsConfig(ctx, cluster)
	if err != nil {
		return nil, err
	}

	cfg := logsearch.Config{
		URL:                orTrimmed(override.URL, stored.URL),
		Index:              orTrimmed(override.Index, stored.Index),
		Username:           orTrimmed(override.Username, stored.Username),
		Secret:             orTrimmed(override.Secret, stored.Secret),
		Scheme:             orTrimmed(override.Scheme, stored.Scheme),
		InsecureSkipVerify: override.InsecureSkipVerify,
	}

	client, err := logsearch.New(cfg)
	if err != nil {
		return nil, err
	}
	return client.Probe(ctx, window)
}

func orTrimmed(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func (s *Service) logsSecretFor(ctx context.Context, cluster *store.Cluster) (string, error) {
	sealed, err := s.clusters.LogsSecret(ctx, cluster.ID)
	if err != nil {
		return "", err
	}
	if len(sealed) == 0 {
		return "", nil
	}

	// Bound to the row exactly as the kubeconfig is, so a ciphertext moved between rows
	// does not decrypt.
	plain, err := s.keyring.Open(sealed, logsAAD(cluster.ID.String()))
	if err != nil {
		return "", fmt.Errorf("decrypt the log source secret: %w", err)
	}
	return string(plain), nil
}

// SealLogsSecret prepares one for storage.
func (s *Service) SealLogsSecret(clusterID, secret string) ([]byte, error) {
	if secret == "" {
		return nil, nil
	}
	return s.keyring.Seal([]byte(secret), logsAAD(clusterID))
}

func logsAAD(clusterID string) []byte {
	return []byte("cluster:" + clusterID + ":logs-secret")
}
