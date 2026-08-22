-- +goose Up
-- +goose StatementBegin

CREATE TABLE clusters (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                     TEXT        NOT NULL,

    -- environment drives behaviour (prod demands typed confirmation); environment_label
    -- is free text so a team can call the same environment whatever it calls it.
    environment              TEXT        NOT NULL DEFAULT 'test',
    environment_label        TEXT        NOT NULL DEFAULT '',
    color                    TEXT        NOT NULL DEFAULT '',

    auth_source              TEXT        NOT NULL DEFAULT 'kubeconfig',
    api_server_url           TEXT        NOT NULL DEFAULT '',
    insecure_skip_tls_verify BOOLEAN     NOT NULL DEFAULT FALSE,
    proxy_url                TEXT,

    -- Populated by the health probe, not by the user.
    credential_status        TEXT        NOT NULL DEFAULT 'unknown',
    status_detail            TEXT        NOT NULL DEFAULT '',
    k8s_version              TEXT        NOT NULL DEFAULT '',
    node_count               INTEGER,
    metrics_available        BOOLEAN     NOT NULL DEFAULT FALSE,
    last_validated_at        TIMESTAMPTZ,

    impersonation_enabled    BOOLEAN     NOT NULL DEFAULT FALSE,
    qps_limit                INTEGER     NOT NULL DEFAULT 50,

    -- Independent of any role: when set, nobody writes to this cluster, admins included.
    read_only                BOOLEAN     NOT NULL DEFAULT FALSE,

    created_by               UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT clusters_environment_valid CHECK (environment IN ('prod', 'preprod', 'test', 'dr')),
    CONSTRAINT clusters_auth_source_valid CHECK (auth_source IN ('kubeconfig', 'in-cluster')),
    CONSTRAINT clusters_credential_status_valid
        CHECK (credential_status IN ('valid', 'invalid', 'unreachable', 'unknown'))
);

CREATE UNIQUE INDEX clusters_name_key ON clusters (lower(name));

COMMENT ON COLUMN clusters.read_only IS 'Cluster-wide write lock. Independent of user roles; administrators are not exempt.';

-- The kubeconfig lives here and only here, always encrypted. One row per cluster: a
-- credential update replaces it rather than accumulating copies of a secret.
CREATE TABLE cluster_credentials (
    cluster_id      UUID PRIMARY KEY REFERENCES clusters (id) ON DELETE CASCADE,
    context_name    TEXT        NOT NULL,
    -- Self-describing envelope blob: format version, key version, wrapped data key and
    -- ciphertext (see internal/crypto). No plaintext column exists by design.
    kubeconfig_enc  BYTEA       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ
);

COMMENT ON TABLE cluster_credentials IS 'Encrypted kubeconfigs. Never store or return plaintext.';

CREATE TABLE user_cluster_grants (
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    cluster_id   UUID NOT NULL REFERENCES clusters (id) ON DELETE CASCADE,
    access_level TEXT NOT NULL,
    granted_by   UUID REFERENCES users (id) ON DELETE SET NULL,
    granted_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, cluster_id),
    CONSTRAINT user_cluster_grants_level_valid CHECK (access_level IN ('read', 'write'))
);

CREATE INDEX user_cluster_grants_cluster_idx ON user_cluster_grants (cluster_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_cluster_grants;
DROP TABLE IF EXISTS cluster_credentials;
DROP TABLE IF EXISTS clusters;
-- +goose StatementEnd
