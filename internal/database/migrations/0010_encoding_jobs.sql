-- Encoding records contain correlation identifiers and bounded technical plans.
-- The opened source, access token, output path, and FFmpeg stderr are never stored.
CREATE TABLE encoding_jobs (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth_session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    device_id text NOT NULL CHECK (octet_length(device_id) <= 256),
    play_session_id text NOT NULL REFERENCES play_sessions(id) ON DELETE CASCADE,
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    media_source_id text NOT NULL CHECK (octet_length(media_source_id) BETWEEN 1 AND 256),
    source_stamp text NOT NULL CHECK (octet_length(source_stamp) BETWEEN 1 AND 256),
    plan jsonb NOT NULL CHECK (jsonb_typeof(plan) = 'object' AND octet_length(plan::text) <= 8192),
    state text NOT NULL CHECK (state IN ('queued', 'running', 'completed', 'failed', 'cancelled', 'interrupted')),
    output_bytes bigint NOT NULL DEFAULT 0 CHECK (output_bytes >= 0),
    error_code text NOT NULL DEFAULT '' CHECK (error_code = '' OR error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    created_at timestamptz NOT NULL CHECK (created_at >= '0001-01-01T00:00:00Z'::timestamptz
        AND created_at < '10000-01-01T00:00:00Z'::timestamptz),
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at
        AND updated_at < '10000-01-01T00:00:00Z'::timestamptz),
    last_access_at timestamptz NOT NULL CHECK (last_access_at >= created_at
        AND last_access_at < '10000-01-01T00:00:00Z'::timestamptz)
);

CREATE INDEX encoding_jobs_user_recent_idx ON encoding_jobs(user_id, created_at DESC, id);
CREATE INDEX encoding_jobs_auth_session_idx ON encoding_jobs(auth_session_id, id);
CREATE INDEX encoding_jobs_play_session_idx ON encoding_jobs(play_session_id, id);
CREATE INDEX encoding_jobs_item_idx ON encoding_jobs(item_id, id);
CREATE INDEX encoding_jobs_active_idx ON encoding_jobs(state, updated_at, id)
    WHERE state IN ('queued', 'running');

-- Authentication and playback ownership are checked under parent-row locks by
-- the repository before insertion. Status persistence remains available after
-- logout so cancellation can reach a terminal state. Parent deletion cascades
-- preserve the existing account, catalog, and bounded playback retention rules.
