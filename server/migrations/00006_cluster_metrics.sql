-- +goose Up
-- +goose StatementBegin

-- Where this cluster's history is read from.
--
-- Per cluster rather than one address for the deployment: Prometheus is normally deployed
-- into the cluster it observes, so two clusters mean two endpoints, and a single global
-- address would answer every cluster with one cluster's numbers. The deployment-wide
-- setting stays as the fallback, which is what a central Prometheus or Thanos looks like.
ALTER TABLE clusters
    ADD COLUMN metrics_url                  TEXT    NOT NULL DEFAULT '',
    ADD COLUMN metrics_username             TEXT    NOT NULL DEFAULT '',
    ADD COLUMN metrics_insecure_skip_verify BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN clusters.metrics_url IS
    'Prometheus-compatible HTTP API for this cluster. Empty falls back to the deployment setting.';
COMMENT ON COLUMN clusters.metrics_username IS
    'Basic auth user. The password is sealed in cluster_credentials, never here.';

-- The password is sealed with the same keyring and bound to the row, exactly as the
-- kubeconfig is: a credential in a plain column is a credential in every backup.
ALTER TABLE cluster_credentials
    ADD COLUMN metrics_password_enc BYTEA;

COMMENT ON COLUMN cluster_credentials.metrics_password_enc IS
    'Basic auth password for the metrics endpoint, encrypted at rest. Never logged.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cluster_credentials DROP COLUMN metrics_password_enc;
ALTER TABLE clusters
    DROP COLUMN metrics_insecure_skip_verify,
    DROP COLUMN metrics_username,
    DROP COLUMN metrics_url;
-- +goose StatementEnd
