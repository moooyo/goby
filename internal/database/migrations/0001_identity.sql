CREATE TABLE server_settings (
    key text PRIMARY KEY,
    value text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id text PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    normalized_name text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    has_password boolean NOT NULL DEFAULT true,
    is_administrator boolean NOT NULL DEFAULT false,
    is_disabled boolean NOT NULL DEFAULT false,
    policy jsonb NOT NULL DEFAULT '{}'::jsonb,
    configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    kind text NOT NULL CHECK (kind IN ('admin', 'emby')),
    client_name text NOT NULL DEFAULT '',
    device_id text NOT NULL DEFAULT '',
    device_name text NOT NULL DEFAULT '',
    client_version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_active_expiry_idx ON sessions(expires_at) WHERE revoked_at IS NULL;
