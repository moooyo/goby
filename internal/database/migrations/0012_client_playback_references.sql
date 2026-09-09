ALTER TABLE play_sessions
    ADD COLUMN client_correlated boolean NOT NULL DEFAULT false;

DROP INDEX play_sessions_current_source_idx;
CREATE UNIQUE INDEX play_sessions_current_source_idx
    ON play_sessions(user_id, auth_session_id, device_id, item_id, media_source_id)
    WHERE NOT client_correlated AND state IN ('Prepared', 'Playing', 'Paused');

-- A client reference belongs to one authentication scope and one internal play.
-- Deleting bounded playback history keeps a NULL target as a tombstone while
-- its authentication session remains valid, so old URLs cannot create new plays.
CREATE TABLE client_playback_references (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth_session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    device_id text NOT NULL CHECK (octet_length(device_id) <= 256),
    client_nonce text NOT NULL CHECK (
        octet_length(client_nonce) BETWEEN 1 AND 256
        AND btrim(client_nonce) <> ''
        AND client_nonce !~ '[[:cntrl:]]'
        AND left(client_nonce, 5) <> 'play_'
    ),
    play_session_id text UNIQUE REFERENCES play_sessions(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, auth_session_id, device_id, client_nonce)
);

CREATE INDEX client_playback_references_auth_idx ON client_playback_references(auth_session_id);
