ALTER TABLE sessions
    ADD COLUMN client_capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT sessions_client_capabilities_object CHECK (jsonb_typeof(client_capabilities) = 'object'),
    -- Allow JSONB formatting overhead above the application's 64 KiB input limit.
    ADD CONSTRAINT sessions_client_capabilities_size CHECK (octet_length(client_capabilities::text) <= 131072);

CREATE INDEX sessions_emby_presence_idx ON sessions(last_seen_at DESC, id)
    WHERE kind = 'emby' AND revoked_at IS NULL;

CREATE INDEX sessions_emby_user_presence_idx ON sessions(user_id, last_seen_at DESC, id)
    WHERE kind = 'emby' AND revoked_at IS NULL;
