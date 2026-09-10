-- Application keys share a hidden server-device generation. Ordinary logins
-- keep their independent registry and never attach to this table.
CREATE TABLE application_key_devices (
    id bigint PRIMARY KEY DEFAULT nextval('devices_id_seq'),
    reported_device_id text NOT NULL CHECK (octet_length(reported_device_id) <= 256),
    reported_name text NOT NULL DEFAULT '' CHECK (octet_length(reported_name) <= 256),
    custom_name text CHECK (custom_name IS NULL OR octet_length(custom_name) BETWEEN 1 AND 256),
    app_name text NOT NULL DEFAULT '' CHECK (octet_length(app_name) <= 256),
    app_version text NOT NULL DEFAULT '' CHECK (octet_length(app_version) <= 256),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ip_address text NOT NULL DEFAULT '' CHECK (octet_length(ip_address) <= 256),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    deleted_at timestamptz,
    CHECK (id > 0)
);

CREATE UNIQUE INDEX application_key_devices_current_reported_id_idx
    ON application_key_devices(reported_device_id) WHERE deleted_at IS NULL;

-- Every schema-16 key, including revoked history, keeps numeric identity 1.
-- Backfill only the new projection; no existing credential or key field changes.
INSERT INTO application_key_devices
    (id, reported_device_id, reported_name, app_name, app_version, created_at, last_seen_at, ip_address)
SELECT 1,
    (SELECT a.device_id FROM application_keys k JOIN sessions a ON a.id = k.credential_id ORDER BY k.id LIMIT 1),
    a.device_name, a.client_name, a.client_version,
    (SELECT min(created_at) FROM sessions WHERE id IN (SELECT credential_id FROM application_keys)),
    GREATEST(a.last_seen_at, COALESCE(k.last_used_at, a.created_at)), k.ip_address
FROM application_keys k JOIN sessions a ON a.id = k.credential_id
ORDER BY GREATEST(a.last_seen_at, COALESCE(k.last_used_at, a.created_at)) DESC, k.id DESC LIMIT 1;

ALTER TABLE application_keys
    DROP CONSTRAINT application_keys_reported_device_numeric_id_check,
    ADD CONSTRAINT application_keys_device_generation_fkey
        FOREIGN KEY (reported_device_numeric_id) REFERENCES application_key_devices(id);

CREATE INDEX application_keys_device_generation_idx
    ON application_keys(reported_device_numeric_id, credential_id);
