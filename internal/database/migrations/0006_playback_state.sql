CREATE TABLE user_item_data (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    playback_position_ticks bigint NOT NULL DEFAULT 0 CHECK (playback_position_ticks >= 0),
    play_count integer NOT NULL DEFAULT 0 CHECK (play_count >= 0),
    is_favorite boolean NOT NULL DEFAULT false,
    played boolean NOT NULL DEFAULT false,
    last_played_at timestamptz CHECK (last_played_at IS NULL OR
        (last_played_at >= '0001-01-01T00:00:00Z'::timestamptz AND last_played_at < '10000-01-01T00:00:00Z'::timestamptz)),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, item_id)
);

CREATE INDEX user_item_data_resume_idx ON user_item_data(user_id, last_played_at DESC, item_id)
    WHERE playback_position_ticks > 0 AND NOT played;

CREATE TABLE play_sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth_session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    device_id text NOT NULL,
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    media_source_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('Prepared', 'Playing', 'Paused', 'Stopped', 'Expired')),
    position_ticks bigint NOT NULL DEFAULT 0 CHECK (position_ticks >= 0),
    duration_ticks bigint NOT NULL CHECK (duration_ticks >= 0),
    counted boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    started_at timestamptz,
    stopped_at timestamptz,
    CHECK (position_ticks <= duration_ticks),
    CHECK (id <> auth_session_id)
);

CREATE UNIQUE INDEX play_sessions_current_source_idx
    ON play_sessions(user_id, auth_session_id, device_id, item_id, media_source_id)
    WHERE state IN ('Prepared', 'Playing', 'Paused');
CREATE INDEX play_sessions_owner_recent_idx ON play_sessions(auth_session_id, created_at DESC, id);
CREATE INDEX play_sessions_expiry_idx ON play_sessions(expires_at, id);
CREATE INDEX play_sessions_user_expiry_idx ON play_sessions(user_id, expires_at, id);

-- Planned sessions expire after inactivity and only a bounded recent terminal
-- history is retained per user by the playback controller.
