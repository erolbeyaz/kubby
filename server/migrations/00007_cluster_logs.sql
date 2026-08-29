-- +goose Up
-- +goose StatementBegin

-- Where this cluster's application logs are read from.
--
-- Per cluster, and separate from the audit sink: the sink is where Kubby writes its own
-- trail, this is where a cluster's applications already write theirs. They are different
-- systems with different credentials often enough that sharing one setting would mean
-- pointing Kubby at the wrong Elasticsearch to fix the other one.
--
-- Kubby never asks the cluster for these logs. A shipper is already reading every line
-- on every node; asking it again through the API server would be the same work done
-- worse, and would put the load on the thing being observed.
ALTER TABLE clusters
    ADD COLUMN logs_url                  TEXT    NOT NULL DEFAULT '',
    ADD COLUMN logs_index                TEXT    NOT NULL DEFAULT '',
    ADD COLUMN logs_auth_scheme          TEXT    NOT NULL DEFAULT '',
    ADD COLUMN logs_username             TEXT    NOT NULL DEFAULT '',
    ADD COLUMN logs_insecure_skip_verify BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN clusters.logs_url IS
    'Elasticsearch HTTP API holding this cluster''s application logs. Empty disables the feature for this cluster.';
COMMENT ON COLUMN clusters.logs_index IS
    'Index or data stream pattern to search, e.g. logs-hybprod-app-*. Written by the operator; Kubby does not guess it.';
COMMENT ON COLUMN clusters.logs_auth_scheme IS
    'How the stored secret is presented: empty for basic auth beside logs_username, or bearer / apikey.';

-- Sealed with the same keyring and bound to the row, exactly as the kubeconfig and the
-- metrics password are: a credential in a plain column is a credential in every backup.
ALTER TABLE cluster_credentials
    ADD COLUMN logs_secret_enc BYTEA;

COMMENT ON COLUMN cluster_credentials.logs_secret_enc IS
    'Password, bearer token or API key for the log source, encrypted at rest. Never logged.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cluster_credentials DROP COLUMN logs_secret_enc;
ALTER TABLE clusters
    DROP COLUMN logs_insecure_skip_verify,
    DROP COLUMN logs_username,
    DROP COLUMN logs_auth_scheme,
    DROP COLUMN logs_index,
    DROP COLUMN logs_url;
-- +goose StatementEnd
