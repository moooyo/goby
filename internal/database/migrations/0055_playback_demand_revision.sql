-- The row lock orders committed playback demand. Presence-only Ping reports
-- retain the existing revision and cannot resume or supersede production.
ALTER TABLE play_sessions ADD COLUMN playback_revision bigint NOT NULL DEFAULT 0
    CONSTRAINT play_sessions_playback_revision_nonnegative CHECK (playback_revision >= 0);
