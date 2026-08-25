package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrClusterNameInUse = errors.New("a cluster with that name already exists")

// Credential status values, set by the health probe rather than by the user.
const (
	CredentialValid       = "valid"
	CredentialInvalid     = "invalid"
	CredentialUnreachable = "unreachable"
	CredentialUnknown     = "unknown"
)

const (
	AccessRead  = "read"
	AccessWrite = "write"
)

const clusterColumns = `
	id, name, environment, environment_label, color, auth_source, api_server_url,
	insecure_skip_tls_verify, proxy_url, credential_status, status_detail, k8s_version,
	node_count, metrics_available, last_validated_at, impersonation_enabled, qps_limit,
	read_only, metrics_url, metrics_username, metrics_insecure_skip_verify,
	created_by, created_at, updated_at`

// Cluster is a registered Kubernetes cluster. The kubeconfig is deliberately absent:
// it lives encrypted in cluster_credentials and is never carried on this struct.
type Cluster struct {
	ID                    uuid.UUID
	Name                  string
	Environment           string
	EnvironmentLabel      string
	Color                 string
	AuthSource            string
	APIServerURL          string
	InsecureSkipTLSVerify bool
	ProxyURL              *string
	CredentialStatus      string
	StatusDetail          string
	K8sVersion            string
	NodeCount             *int
	MetricsAvailable      bool
	LastValidatedAt       *time.Time
	ImpersonationEnabled  bool
	QPSLimit              int
	ReadOnly              bool
	// MetricsURL is this cluster's Prometheus. Empty falls back to the deployment-wide
	// setting, which is what a central Prometheus or Thanos looks like.
	MetricsURL                string
	MetricsUsername           string
	MetricsInsecureSkipVerify bool
	CreatedBy                 *uuid.UUID
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

// DisplayEnvironment is what the UI shows: the free-text label when set, else the code.
func (c *Cluster) DisplayEnvironment() string {
	if c.EnvironmentLabel != "" {
		return c.EnvironmentLabel
	}
	return c.Environment
}

// NeedsAttention reports whether the cluster cannot currently be used.
func (c *Cluster) NeedsAttention() bool {
	return c.CredentialStatus == CredentialInvalid || c.CredentialStatus == CredentialUnreachable
}

type ClusterRepo struct{ db *DB }

func (db *DB) Clusters() *ClusterRepo { return &ClusterRepo{db: db} }

// NewCluster carries everything needed to register a cluster in one transaction.
type NewCluster struct {
	Name                  string
	Environment           string
	EnvironmentLabel      string
	Color                 string
	AuthSource            string
	APIServerURL          string
	InsecureSkipTLSVerify bool
	ProxyURL              *string
	ContextName           string
	KubeconfigEnc         []byte
	CreatedBy             uuid.UUID
}

// Create registers a cluster with a generated identifier.
func (r *ClusterRepo) Create(ctx context.Context, in NewCluster) (*Cluster, error) {
	return r.CreateWithID(ctx, uuid.New(), in)
}

// CreateWithID registers a cluster under a caller-supplied identifier and stores its
// encrypted credential atomically: a cluster row without a credential would be
// unusable, and a credential without a row orphaned.
//
// The caller supplies the id because the ciphertext is bound to it as additional
// authenticated data, which has to be known before the credential can be encrypted.
func (r *ClusterRepo) CreateWithID(ctx context.Context, id uuid.UUID, in NewCluster) (*Cluster, error) {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cluster creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The zero uuid means "no creator on record", which is a real state — a cluster
	// brought back from an archive has one. Sent as-is it is a valid uuid that matches no
	// user and the row is refused by the foreign key.
	var createdBy *uuid.UUID
	if in.CreatedBy != uuid.Nil {
		creator := in.CreatedBy
		createdBy = &creator
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO clusters
			(id, name, environment, environment_label, color, auth_source, api_server_url,
			 insecure_skip_tls_verify, proxy_url, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+clusterColumns,
		id, strings.TrimSpace(in.Name), in.Environment, in.EnvironmentLabel, in.Color,
		in.AuthSource, in.APIServerURL, in.InsecureSkipTLSVerify, in.ProxyURL, createdBy)

	cluster, err := scanCluster(row)
	if isUniqueViolation(err) {
		return nil, ErrClusterNameInUse
	}
	if err != nil {
		return nil, fmt.Errorf("insert cluster: %w", err)
	}

	if len(in.KubeconfigEnc) > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO cluster_credentials (cluster_id, context_name, kubeconfig_enc)
			VALUES ($1, $2, $3)`,
			cluster.ID, in.ContextName, in.KubeconfigEnc)
		if err != nil {
			return nil, fmt.Errorf("insert cluster credential: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cluster creation: %w", err)
	}
	return cluster, nil
}

func (r *ClusterRepo) ByID(ctx context.Context, id uuid.UUID) (*Cluster, error) {
	row := r.db.pool.QueryRow(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = $1`, id)

	cluster, err := scanCluster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find cluster: %w", err)
	}
	return cluster, nil
}

// List returns every cluster. Used by administrators.
func (r *ClusterRepo) List(ctx context.Context) ([]*Cluster, error) {
	rows, err := r.db.pool.Query(ctx, `SELECT `+clusterColumns+` FROM clusters ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	return collectClusters(rows)
}

// ListForUser returns only the clusters a user has been granted, so an unauthorised
// cluster never reaches the client in the first place.
func (r *ClusterRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]*Cluster, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT `+clusterColumns+`
		FROM clusters c
		JOIN user_cluster_grants g ON g.cluster_id = c.id AND g.user_id = $1
		ORDER BY c.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list clusters for user: %w", err)
	}
	return collectClusters(rows)
}

// AccessLevel reports what a user may do on a cluster, or "" when not granted.
func (r *ClusterRepo) AccessLevel(ctx context.Context, userID, clusterID uuid.UUID) (string, error) {
	var level string
	err := r.db.pool.QueryRow(ctx,
		`SELECT access_level FROM user_cluster_grants WHERE user_id = $1 AND cluster_id = $2`,
		userID, clusterID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read access level: %w", err)
	}
	return level, nil
}

func (r *ClusterRepo) SetGrant(ctx context.Context, userID, clusterID, grantedBy uuid.UUID, level string) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO user_cluster_grants (user_id, cluster_id, access_level, granted_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, cluster_id) DO UPDATE SET access_level = $3, granted_by = $4, granted_at = now()`,
		userID, clusterID, level, grantedBy)
	if err != nil {
		return fmt.Errorf("set cluster grant: %w", err)
	}
	return nil
}

func (r *ClusterRepo) RemoveGrant(ctx context.Context, userID, clusterID uuid.UUID) error {
	_, err := r.db.pool.Exec(ctx,
		`DELETE FROM user_cluster_grants WHERE user_id = $1 AND cluster_id = $2`, userID, clusterID)
	if err != nil {
		return fmt.Errorf("remove cluster grant: %w", err)
	}
	return nil
}

// Grant pairs a user with their access level on one cluster.
type Grant struct {
	UserID      uuid.UUID
	Email       string
	DisplayName string
	AccessLevel string
}

func (r *ClusterRepo) ListGrants(ctx context.Context, clusterID uuid.UUID) ([]Grant, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT u.id, u.email, u.display_name, g.access_level
		FROM user_cluster_grants g
		JOIN users u ON u.id = g.user_id
		WHERE g.cluster_id = $1
		ORDER BY u.email`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("list cluster grants: %w", err)
	}
	defer rows.Close()

	var grants []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.UserID, &g.Email, &g.DisplayName, &g.AccessLevel); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

// Credential returns the encrypted kubeconfig and the selected context.
func (r *ClusterRepo) Credential(ctx context.Context, clusterID uuid.UUID) (contextName string, enc []byte, err error) {
	err = r.db.pool.QueryRow(ctx,
		`SELECT context_name, kubeconfig_enc FROM cluster_credentials WHERE cluster_id = $1`,
		clusterID).Scan(&contextName, &enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("read cluster credential: %w", err)
	}
	return contextName, enc, nil
}

// ReplaceCredential swaps in a freshly pasted kubeconfig and clears the failed status,
// since the whole point of replacing it is that the old one stopped working.
func (r *ClusterRepo) ReplaceCredential(ctx context.Context, clusterID uuid.UUID, contextName string, enc []byte, apiServerURL string, insecure bool) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin credential replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO cluster_credentials (cluster_id, context_name, kubeconfig_enc, rotated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (cluster_id) DO UPDATE
		SET context_name = $2, kubeconfig_enc = $3, rotated_at = now()`,
		clusterID, contextName, enc)
	if err != nil {
		return fmt.Errorf("replace credential: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE clusters
		SET api_server_url = $2, insecure_skip_tls_verify = $3,
		    credential_status = 'unknown', status_detail = '', updated_at = now()
		WHERE id = $1`, clusterID, apiServerURL, insecure)
	if err != nil {
		return fmt.Errorf("reset cluster status: %w", err)
	}
	return tx.Commit(ctx)
}

// HealthUpdate is what a probe learned about a cluster.
type HealthUpdate struct {
	CredentialStatus string
	StatusDetail     string
	K8sVersion       string
	NodeCount        *int
	MetricsAvailable bool
}

func (r *ClusterRepo) RecordHealth(ctx context.Context, id uuid.UUID, h HealthUpdate) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE clusters
		SET credential_status = $2, status_detail = $3, k8s_version = $4,
		    node_count = $5, metrics_available = $6, last_validated_at = now(), updated_at = now()
		WHERE id = $1`,
		id, h.CredentialStatus, h.StatusDetail, h.K8sVersion, h.NodeCount, h.MetricsAvailable)
	if err != nil {
		return fmt.Errorf("record cluster health: %w", err)
	}
	return nil
}

// ClusterSettings are the fields an administrator may edit directly.
type ClusterSettings struct {
	Name                 *string
	Environment          *string
	EnvironmentLabel     *string
	Color                *string
	ReadOnly             *bool
	ImpersonationEnabled *bool
	QPSLimit             *int
	ProxyURL             *string
	// MetricsURL and friends point this cluster at its own Prometheus.
	MetricsURL                *string
	MetricsUsername           *string
	MetricsInsecureSkipVerify *bool
}

func (r *ClusterRepo) UpdateSettings(ctx context.Context, id uuid.UUID, s ClusterSettings) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE clusters SET
			name = COALESCE($2, name),
			environment = COALESCE($3, environment),
			environment_label = COALESCE($4, environment_label),
			color = COALESCE($5, color),
			read_only = COALESCE($6, read_only),
			impersonation_enabled = COALESCE($7, impersonation_enabled),
			qps_limit = COALESCE($8, qps_limit),
			proxy_url = COALESCE($9, proxy_url),
			metrics_url = COALESCE($10, metrics_url),
			metrics_username = COALESCE($11, metrics_username),
			metrics_insecure_skip_verify = COALESCE($12, metrics_insecure_skip_verify),
			updated_at = now()
		WHERE id = $1`,
		id, s.Name, s.Environment, s.EnvironmentLabel, s.Color, s.ReadOnly,
		s.ImpersonationEnabled, s.QPSLimit, s.ProxyURL,
		s.MetricsURL, s.MetricsUsername, s.MetricsInsecureSkipVerify)
	if isUniqueViolation(err) {
		return ErrClusterNameInUse
	}
	if err != nil {
		return fmt.Errorf("update cluster settings: %w", err)
	}
	return nil
}

func (r *ClusterRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx, `DELETE FROM clusters WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func collectClusters(rows pgx.Rows) ([]*Cluster, error) {
	defer rows.Close()

	var clusters []*Cluster
	for rows.Next() {
		cluster, err := scanCluster(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func scanCluster(row scannable) (*Cluster, error) {
	var c Cluster
	err := row.Scan(
		&c.ID, &c.Name, &c.Environment, &c.EnvironmentLabel, &c.Color, &c.AuthSource,
		&c.APIServerURL, &c.InsecureSkipTLSVerify, &c.ProxyURL, &c.CredentialStatus,
		&c.StatusDetail, &c.K8sVersion, &c.NodeCount, &c.MetricsAvailable,
		&c.LastValidatedAt, &c.ImpersonationEnabled, &c.QPSLimit, &c.ReadOnly,
		&c.MetricsURL, &c.MetricsUsername, &c.MetricsInsecureSkipVerify,
		&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// MetricsPassword returns the sealed basic-auth password for this cluster's metrics
// endpoint. Nil means none is stored, which is the common case.
func (r *ClusterRepo) MetricsPassword(ctx context.Context, clusterID uuid.UUID) ([]byte, error) {
	var enc []byte
	err := r.db.pool.QueryRow(ctx,
		`SELECT metrics_password_enc FROM cluster_credentials WHERE cluster_id = $1`,
		clusterID).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read metrics password: %w", err)
	}
	return enc, nil
}

// SetMetricsPassword stores or clears it. The value arrives already sealed: this layer
// never sees a credential in the clear.
func (r *ClusterRepo) SetMetricsPassword(ctx context.Context, clusterID uuid.UUID, enc []byte) error {
	_, err := r.db.pool.Exec(ctx,
		`UPDATE cluster_credentials SET metrics_password_enc = $2 WHERE cluster_id = $1`,
		clusterID, enc)
	if err != nil {
		return fmt.Errorf("store metrics password: %w", err)
	}
	return nil
}
