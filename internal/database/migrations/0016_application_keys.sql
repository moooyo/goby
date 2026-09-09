-- The shared credential table retains every existing login and foreign key.
ALTER TABLE sessions
    DROP CONSTRAINT sessions_kind_check,
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN expires_at DROP NOT NULL,
    ADD CONSTRAINT sessions_kind_check CHECK (kind IN ('admin', 'emby', 'application_key')),
    ADD CONSTRAINT sessions_credential_scope_check CHECK (
        (kind IN ('admin', 'emby') AND user_id IS NOT NULL AND expires_at IS NOT NULL)
        OR (kind = 'application_key' AND user_id IS NULL AND expires_at IS NULL)
    );

CREATE TABLE application_keys (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    credential_id text NOT NULL UNIQUE REFERENCES sessions(id) ON DELETE CASCADE,
    secret_ciphertext bytea NOT NULL,
    created_by text REFERENCES users(id) ON DELETE SET NULL,
    last_used_at timestamptz,
    ip_address text NOT NULL DEFAULT '',
    reported_device_numeric_id bigint NOT NULL DEFAULT 1 CHECK (reported_device_numeric_id = 1)
);

CREATE INDEX sessions_application_key_presence_idx ON sessions(last_seen_at DESC, id)
    WHERE kind = 'application_key' AND revoked_at IS NULL;

-- Client sessions are contexts of one credential, never additional logins.
CREATE TABLE application_key_clients (
    id text PRIMARY KEY,
    credential_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_name text NOT NULL CHECK (octet_length(client_name) <= 256),
    device_id text NOT NULL CHECK (octet_length(device_id) <= 256),
    device_name text NOT NULL CHECK (octet_length(device_name) <= 256),
    client_version text NOT NULL CHECK (octet_length(client_version) <= 256),
    client_capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (credential_id, client_name, device_id),
    CHECK (id <> credential_id),
    CHECK (jsonb_typeof(client_capabilities) = 'object'),
    CHECK (octet_length(client_capabilities::text) <= 131072)
);

CREATE INDEX application_key_clients_presence_idx ON application_key_clients(last_seen_at DESC, id);

-- Userless playback keeps credential ownership and all existing cascades.
ALTER TABLE play_sessions
    ALTER COLUMN user_id DROP NOT NULL,
    ADD COLUMN application_client_id text REFERENCES application_key_clients(id) ON DELETE CASCADE;
ALTER TABLE encoding_jobs
    ALTER COLUMN user_id DROP NOT NULL,
    ADD COLUMN application_client_id text REFERENCES application_key_clients(id) ON DELETE CASCADE;
ALTER TABLE client_playback_references DROP CONSTRAINT client_playback_references_pkey;
ALTER TABLE client_playback_references
    ALTER COLUMN user_id DROP NOT NULL,
    ADD COLUMN application_client_id text REFERENCES application_key_clients(id) ON DELETE CASCADE;
ALTER TABLE client_playback_references
    ADD CONSTRAINT client_playback_references_owner_key
    UNIQUE NULLS NOT DISTINCT (user_id, auth_session_id, application_client_id, device_id, client_nonce);

DROP INDEX play_sessions_current_source_idx;
CREATE UNIQUE INDEX play_sessions_current_source_idx
    ON play_sessions(user_id, auth_session_id, application_client_id, device_id, item_id, media_source_id)
    NULLS NOT DISTINCT
    WHERE NOT client_correlated AND state IN ('Prepared', 'Playing', 'Paused');

CREATE INDEX play_sessions_application_client_idx ON play_sessions(application_client_id);
CREATE INDEX encoding_jobs_application_client_idx ON encoding_jobs(application_client_id);
CREATE INDEX client_playback_references_application_client_idx ON client_playback_references(application_client_id);
