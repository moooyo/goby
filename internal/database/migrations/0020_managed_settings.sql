-- Only explicit application settings are persisted here. Startup defaults and
-- deployment secrets remain outside this table; old server_settings is intact.
CREATE TABLE managed_settings (
    id smallint PRIMARY KEY CHECK (id = 1),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    server_name text CHECK (octet_length(server_name) BETWEEN 1 AND 128),
    max_bitrate bigint CHECK (max_bitrate BETWEEN 1 AND 1000000000),
    max_width integer CHECK (max_width BETWEEN 1 AND 8192),
    max_height integer CHECK (max_height BETWEEN 1 AND 8192),
    max_audio_channels integer CHECK (max_audio_channels BETWEEN 1 AND 8),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- NULL means use this process's validated startup default. Migration does not
-- copy environment values into overrides or rewrite any existing business row.
INSERT INTO managed_settings (id) VALUES (1);
