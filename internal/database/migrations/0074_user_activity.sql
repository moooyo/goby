CREATE TABLE user_activity (
    user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    last_login_at timestamptz,
    last_activity_at timestamptz,
    CHECK (last_login_at IS NULL OR
        (last_login_at >= '0001-01-01T00:00:00Z'::timestamptz AND last_login_at < '10000-01-01T00:00:00Z'::timestamptz)),
    CHECK (last_activity_at IS NULL OR
        (last_activity_at >= '0001-01-01T00:00:00Z'::timestamptz AND last_activity_at < '10000-01-01T00:00:00Z'::timestamptz)),
    CHECK (last_login_at IS NULL OR (last_activity_at IS NOT NULL AND last_activity_at >= last_login_at))
);

-- Retained logins are evidence for their actual issuance and activity times.
-- An account with no retained login remains unknown rather than using its
-- creation time. Future login and activity writes maintain this independent
-- summary, so revocation and session retention cannot erase it.
INSERT INTO user_activity (user_id, last_login_at, last_activity_at)
SELECT u.id, history.last_login_at,
    GREATEST(history.last_login_at, history.last_activity_at)
FROM users u LEFT JOIN (
    SELECT user_id,
        max(created_at) FILTER (WHERE created_at >= '0001-01-01T00:00:00Z'::timestamptz
            AND created_at < '10000-01-01T00:00:00Z'::timestamptz) AS last_login_at,
        max(last_seen_at) FILTER (WHERE last_seen_at >= '0001-01-01T00:00:00Z'::timestamptz
            AND last_seen_at < '10000-01-01T00:00:00Z'::timestamptz) AS last_activity_at
    FROM sessions WHERE kind IN ('admin', 'emby') AND user_id IS NOT NULL
    GROUP BY user_id
) history ON history.user_id = u.id;
