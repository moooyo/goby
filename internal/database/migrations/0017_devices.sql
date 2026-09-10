-- Ordinary devices have registry generations independent of credentials.
-- Numeric 1 remains reserved for the existing application-key server identity.
CREATE TABLE devices (
    id bigint GENERATED ALWAYS AS IDENTITY (START WITH 2) PRIMARY KEY,
    reported_device_id text NOT NULL CHECK (octet_length(reported_device_id) BETWEEN 1 AND 256),
    reported_name text NOT NULL DEFAULT '',
    custom_name text CHECK (custom_name IS NULL OR octet_length(custom_name) BETWEEN 1 AND 256),
    app_name text NOT NULL DEFAULT '',
    app_version text NOT NULL DEFAULT '',
    last_user_id text REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ip_address text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    deleted_at timestamptz
);

CREATE UNIQUE INDEX devices_current_reported_id_idx ON devices(reported_device_id) WHERE deleted_at IS NULL;
CREATE INDEX devices_current_activity_idx ON devices(last_seen_at DESC, id DESC) WHERE deleted_at IS NULL;

ALTER TABLE sessions
    ADD COLUMN device_registry_id bigint REFERENCES devices(id),
    ADD CONSTRAINT sessions_device_registry_scope_check CHECK (device_registry_id IS NULL OR kind = 'emby');

-- Preserve revoked and expired login history, and keep empty reported IDs
-- unregistered. Raw client metadata and every credential remain unchanged.
INSERT INTO devices (reported_device_id, reported_name, app_name, app_version, last_user_id, created_at, last_seen_at)
SELECT device_id, device_name, client_name, client_version, user_id, first_created_at, last_activity_at
FROM (
    SELECT DISTINCT ON (device_id) device_id, device_name, client_name, client_version, user_id,
        min(created_at) OVER (PARTITION BY device_id) AS first_created_at,
        max(last_seen_at) OVER (PARTITION BY device_id) AS last_activity_at
    FROM sessions WHERE kind = 'emby' AND device_id <> ''
    ORDER BY device_id, last_seen_at DESC, created_at DESC, id DESC
) registered ORDER BY device_id;

UPDATE sessions authentication SET device_registry_id = device.id
FROM devices device
WHERE authentication.kind = 'emby' AND authentication.device_id = device.reported_device_id;

CREATE INDEX sessions_device_registry_idx ON sessions(device_registry_id, id)
    WHERE device_registry_id IS NOT NULL AND kind = 'emby';
