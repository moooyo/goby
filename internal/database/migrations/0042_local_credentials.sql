ALTER TABLE users
    ADD COLUMN local_password_hash text,
    ADD COLUMN profile_pin_ciphertext bytea,
    ADD COLUMN local_credentials_revision bigint NOT NULL DEFAULT 1 CHECK (local_credentials_revision > 0),
    ADD COLUMN local_password_failures integer NOT NULL DEFAULT 0 CHECK (local_password_failures BETWEEN 0 AND 5),
    ADD COLUMN local_password_blocked_until timestamptz,
    ADD CONSTRAINT users_local_password_hash_check CHECK (local_password_hash IS NULL OR local_password_hash ~ '^\$2[aby]\$10\$[./A-Za-z0-9]{53}$'),
    ADD CONSTRAINT users_profile_pin_ciphertext_check CHECK (profile_pin_ciphertext IS NULL OR octet_length(profile_pin_ciphertext) = 36);

-- Legacy configuration values were inert. Never promote a plaintext historical
-- PIN into authentication authority or preserve it in subsequent backups.
UPDATE users SET configuration = (configuration - 'ProfilePin') || '{"EnableLocalPassword":false}'::jsonb
    WHERE configuration ? 'ProfilePin' OR configuration @> '{"EnableLocalPassword":true}'::jsonb;

ALTER TABLE sessions ADD COLUMN local_auth boolean NOT NULL DEFAULT false;
ALTER TABLE sessions ADD CONSTRAINT sessions_local_auth_check CHECK (NOT local_auth OR kind = 'emby');
