CREATE TABLE user_settings (
    user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    settings jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(settings) = 'object' AND octet_length(settings::text) <= 262144),
    updated_at timestamptz NOT NULL DEFAULT now()
);
