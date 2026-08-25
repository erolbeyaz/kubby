package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// Restore reads an archive back into an installation.
//
// Additive, never destructive. Anything already present is left exactly as it is and
// counted as skipped: a restore run against a live installation by mistake should be a
// no-op, not the last thing that happens to it. Replacing a cluster means removing it
// first, deliberately.
//
// The credentials are re-sealed with *this* installation's key on the way in, which is
// the whole reason the archive carries them opened.
func (s *Service) Restore(ctx context.Context, path, passphrase string, dryRun bool) (Summary, error) {
	sealed, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, fmt.Errorf("read the archive: %w", err)
	}

	body, err := open(sealed, passphrase)
	if err != nil {
		return Summary{}, err
	}

	var archive Archive
	if err := json.Unmarshal(body, &archive); err != nil {
		// The passphrase was right — GCM already proved that — so this is a real
		// corruption or a format from a future build.
		return Summary{}, fmt.Errorf("the archive opened but does not parse: %w", err)
	}
	if archive.Version > archiveVersion {
		return Summary{}, fmt.Errorf("this archive is version %d and was written by Kubby %s; "+
			"this build reads version %d", archive.Version, archive.Kubby, archiveVersion)
	}

	var summary Summary

	// Users first: a grant refers to one, and restoring a grant for a user who is not
	// there yet would fail on a foreign key with an error nobody can act on.
	for _, u := range archive.Users {
		id, err := uuid.Parse(u.ID)
		if err != nil {
			return summary, fmt.Errorf("user %s has an unusable id: %w", u.Email, err)
		}
		role, err := rbac.ParseRole(u.Role)
		if err != nil {
			return summary, fmt.Errorf("user %s has an unknown role %q", u.Email, u.Role)
		}

		existing, err := s.db.Users().ByEmail(ctx, u.Email)
		if err == nil && existing != nil {
			summary.Skipped++
			continue
		}
		if dryRun {
			summary.Users++
			continue
		}
		if err := s.db.Users().RestoreUser(ctx, store.RestoredUser{
			ID: id, Email: u.Email, DisplayName: u.DisplayName,
			PasswordHash: u.PasswordHash, Role: role, IsActive: u.Active,
		}); err != nil {
			return summary, fmt.Errorf("restore user %s: %w", u.Email, err)
		}
		summary.Users++
	}

	for _, c := range archive.Clusters {
		id, err := uuid.Parse(c.ID)
		if err != nil {
			return summary, fmt.Errorf("cluster %s has an unusable id: %w", c.Name, err)
		}

		if existing, err := s.db.Clusters().ByID(ctx, id); err == nil && existing != nil {
			summary.Skipped++
			continue
		}
		if dryRun {
			summary.Clusters++
			continue
		}

		// Sealed with this installation's key and bound to this row, exactly as it would
		// be if the kubeconfig had been pasted in.
		resealed, err := s.keyring.Seal([]byte(c.Kubeconfig), []byte("cluster:"+c.ID))
		if err != nil {
			return summary, fmt.Errorf("seal the credential for %s: %w", c.Name, err)
		}

		in := store.NewCluster{
			Name: c.Name, Environment: c.Environment, EnvironmentLabel: c.EnvironmentLabel,
			Color: c.Color, AuthSource: c.AuthSource, APIServerURL: c.APIServerURL,
			InsecureSkipTLSVerify: c.InsecureSkipTLSVerify,
			ContextName:           c.ContextName, KubeconfigEnc: resealed,
		}
		if c.ProxyURL != "" {
			proxy := c.ProxyURL
			in.ProxyURL = &proxy
		}

		if _, err := s.db.Clusters().CreateWithID(ctx, id, in); err != nil {
			return summary, fmt.Errorf("restore cluster %s: %w", c.Name, err)
		}

		// The settings that are not part of registering it.
		readOnly, impersonation, qps := c.ReadOnly, c.ImpersonationEnabled, c.QPSLimit
		metricsURL, metricsUser, metricsInsecure := c.MetricsURL, c.MetricsUsername, false
		if err := s.db.Clusters().UpdateSettings(ctx, id, store.ClusterSettings{
			ReadOnly: &readOnly, ImpersonationEnabled: &impersonation, QPSLimit: &qps,
			MetricsURL: &metricsURL, MetricsUsername: &metricsUser,
			MetricsInsecureSkipVerify: &metricsInsecure,
		}); err != nil {
			return summary, fmt.Errorf("restore the settings for %s: %w", c.Name, err)
		}
		summary.Clusters++
	}

	for _, g := range archive.Grants {
		userID, err := uuid.Parse(g.UserID)
		if err != nil {
			continue
		}
		clusterID, err := uuid.Parse(g.ClusterID)
		if err != nil {
			continue
		}
		if dryRun {
			summary.Grants++
			continue
		}
		// Granted by the user themselves in the record, because the administrator who
		// made the original decision is not necessarily the one running the restore, and
		// inventing an actor would put a name on a decision they did not make.
		if err := s.db.Clusters().SetGrant(ctx, userID, clusterID, userID, g.AccessLevel); err != nil {
			return summary, fmt.Errorf("restore a grant: %w", err)
		}
		summary.Grants++
	}

	for _, setting := range archive.Settings {
		if dryRun {
			summary.Settings++
			continue
		}
		var value any
		if err := json.Unmarshal(setting.Value, &value); err != nil {
			return summary, fmt.Errorf("setting %s does not parse: %w", setting.Key, err)
		}
		if err := s.db.Settings().Put(ctx, setting.Key, value, uuid.Nil); err != nil {
			return summary, fmt.Errorf("restore setting %s: %w", setting.Key, err)
		}
		summary.Settings++
	}

	return summary, nil
}
