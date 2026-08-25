// Package backup exports and restores Kubby's own configuration.
//
// Not the audit trail. That is shipped somewhere it outlives this installation (ADR-101),
// and a backup tool able to rewrite it would be the wrong kind of tool to hand anyone.
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Archive is what an export contains.
//
// The kubeconfigs travel in the clear *inside* the archive and the archive itself is
// encrypted. Re-sealing them with the instance key would make the file unopenable by the
// installation being restored into, which is the only situation this is for.
type Archive struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	// Kubby is the version that wrote it, so a restore into an older one can say why it
	// refuses rather than failing on a column that does not exist yet.
	Kubby string `json:"kubby"`

	Clusters []ArchivedCluster `json:"clusters"`
	Users    []ArchivedUser    `json:"users"`
	Grants   []ArchivedGrant   `json:"grants"`
	Settings []ArchivedSetting `json:"settings"`
}

type ArchivedCluster struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Environment           string `json:"environment"`
	EnvironmentLabel      string `json:"environmentLabel,omitempty"`
	Color                 string `json:"color,omitempty"`
	AuthSource            string `json:"authSource"`
	APIServerURL          string `json:"apiServerUrl"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTlsVerify"`
	ProxyURL              string `json:"proxyUrl,omitempty"`
	ImpersonationEnabled  bool   `json:"impersonationEnabled"`
	QPSLimit              int    `json:"qpsLimit"`
	ReadOnly              bool   `json:"readOnly"`
	MetricsURL            string `json:"metricsUrl,omitempty"`
	MetricsUsername       string `json:"metricsUsername,omitempty"`

	ContextName string `json:"contextName"`
	// Kubeconfig is the credential itself. This is why the archive is encrypted.
	Kubeconfig string `json:"kubeconfig"`
}

type ArchivedUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
	// PasswordHash travels as the hash, never as a password: an argon2id hash is not
	// reversible and restoring one keeps people signed in with what they already know.
	PasswordHash string `json:"passwordHash"`
	// TOTP secrets are deliberately absent. Restoring them would move a second factor
	// between installations on the strength of one passphrase, and re-enrolling is a
	// minute's work.
}

type ArchivedGrant struct {
	UserID      string `json:"userId"`
	ClusterID   string `json:"clusterId"`
	AccessLevel string `json:"accessLevel"`
}

type ArchivedSetting struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// Summary is what happened, for the operator to read.
type Summary struct {
	Clusters int
	Users    int
	Grants   int
	Settings int
	Skipped  int
}

// Service reads and writes archives.
type Service struct {
	db      *store.DB
	keyring *crypto.Keyring
}

func New(db *store.DB, keyring *crypto.Keyring) *Service {
	return &Service{db: db, keyring: keyring}
}

// Export writes an encrypted archive.
func (s *Service) Export(ctx context.Context, path, passphrase string) (Summary, error) {
	archive := Archive{Version: archiveVersion, ExportedAt: time.Now().UTC(), Kubby: kubbyVersion}

	clusters, err := s.db.Clusters().List(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("read clusters: %w", err)
	}

	for _, c := range clusters {
		contextName, sealed, err := s.db.Clusters().Credential(ctx, c.ID)
		if err != nil {
			return Summary{}, fmt.Errorf("read the credential for %s: %w", c.Name, err)
		}

		// Opened here and re-sealed by the archive's own key, because the installation
		// being restored into has a different instance key by definition.
		plain, err := s.keyring.Open(sealed, []byte("cluster:"+c.ID.String()))
		if err != nil {
			return Summary{}, fmt.Errorf("decrypt the credential for %s: %w", c.Name, err)
		}

		entry := ArchivedCluster{
			ID: c.ID.String(), Name: c.Name, Environment: c.Environment,
			EnvironmentLabel: c.EnvironmentLabel, Color: c.Color,
			AuthSource: c.AuthSource, APIServerURL: c.APIServerURL,
			InsecureSkipTLSVerify: c.InsecureSkipTLSVerify,
			ImpersonationEnabled:  c.ImpersonationEnabled,
			QPSLimit:              c.QPSLimit, ReadOnly: c.ReadOnly,
			MetricsURL: c.MetricsURL, MetricsUsername: c.MetricsUsername,
			ContextName: contextName, Kubeconfig: string(plain),
		}
		if c.ProxyURL != nil {
			entry.ProxyURL = *c.ProxyURL
		}
		archive.Clusters = append(archive.Clusters, entry)

		grants, err := s.db.Clusters().ListGrants(ctx, c.ID)
		if err != nil {
			return Summary{}, fmt.Errorf("read grants for %s: %w", c.Name, err)
		}
		for _, g := range grants {
			archive.Grants = append(archive.Grants, ArchivedGrant{
				UserID: g.UserID.String(), ClusterID: c.ID.String(), AccessLevel: g.AccessLevel,
			})
		}
	}

	users, err := s.db.Users().List(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("read users: %w", err)
	}
	for _, u := range users {
		archive.Users = append(archive.Users, ArchivedUser{
			ID: u.ID.String(), Email: u.Email, DisplayName: u.DisplayName,
			Role: string(u.Role), Active: u.IsActive, PasswordHash: u.PasswordHash,
		})
	}

	// Each group by name rather than a table dump: a new setting added later should not
	// silently ride along in an archive written by an older build.
	for _, key := range settingKeys {
		var value json.RawMessage
		found, err := s.db.Settings().Get(ctx, key, &value)
		if err != nil {
			return Summary{}, fmt.Errorf("read setting %s: %w", key, err)
		}
		if found {
			archive.Settings = append(archive.Settings, ArchivedSetting{Key: key, Value: value})
		}
	}

	body, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return Summary{}, fmt.Errorf("encode the archive: %w", err)
	}

	sealed, err := seal(body, passphrase)
	if err != nil {
		return Summary{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Summary{}, fmt.Errorf("create the archive directory: %w", err)
	}
	// 0600: this file holds every cluster credential Kubby has.
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return Summary{}, fmt.Errorf("write the archive: %w", err)
	}

	return Summary{
		Clusters: len(archive.Clusters), Users: len(archive.Users),
		Grants: len(archive.Grants), Settings: len(archive.Settings),
	}, nil
}

// The settings groups an archive carries. Their sealed secrets are deliberately absent:
// they are sealed with the instance key, and an archive that carried them would either be
// unopenable after a restore or would move credentials between installations on the
// strength of one passphrase.
var settingKeys = []string{"node_shell", "pod_debug", "metrics", "audit_sink"}

const (
	archiveVersion = 1
	kubbyVersion   = "0.9"
)
