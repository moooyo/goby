ALTER TABLE play_sessions
    ADD COLUMN player_state jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT play_sessions_player_state_shape CHECK (
        jsonb_typeof(player_state) = 'object'
        AND octet_length(player_state::text) <= 2048
        AND player_state - ARRAY[
            'CanSeek', 'IsMuted', 'VolumeLevel', 'AudioStreamIndex',
            'SubtitleStreamIndex', 'PlayMethod', 'RepeatMode', 'PlaybackRate',
            'Shuffle', 'SubtitleOffset'
        ]::text[] = '{}'::jsonb
    ),
    ADD CONSTRAINT play_sessions_player_state_booleans CHECK (
        (NOT player_state ? 'CanSeek' OR jsonb_typeof(player_state -> 'CanSeek') = 'boolean')
        AND (NOT player_state ? 'IsMuted' OR jsonb_typeof(player_state -> 'IsMuted') = 'boolean')
        AND (NOT player_state ? 'Shuffle' OR jsonb_typeof(player_state -> 'Shuffle') = 'boolean')
    ),
    ADD CONSTRAINT play_sessions_player_state_integers CHECK (
        (NOT player_state ? 'VolumeLevel' OR CASE
            WHEN jsonb_typeof(player_state -> 'VolumeLevel') = 'number'
                AND player_state ->> 'VolumeLevel' ~ '^-?[0-9]+$'
            THEN (player_state ->> 'VolumeLevel')::numeric BETWEEN 0 AND 100
            ELSE false END)
        AND (NOT player_state ? 'AudioStreamIndex' OR CASE
            WHEN jsonb_typeof(player_state -> 'AudioStreamIndex') = 'number'
                AND player_state ->> 'AudioStreamIndex' ~ '^-?[0-9]+$'
            THEN (player_state ->> 'AudioStreamIndex')::numeric BETWEEN -1 AND 2147483647
            ELSE false END)
        AND (NOT player_state ? 'SubtitleStreamIndex' OR CASE
            WHEN jsonb_typeof(player_state -> 'SubtitleStreamIndex') = 'number'
                AND player_state ->> 'SubtitleStreamIndex' ~ '^-?[0-9]+$'
            THEN (player_state ->> 'SubtitleStreamIndex')::numeric BETWEEN -1 AND 2147483647
            ELSE false END)
        AND (NOT player_state ? 'SubtitleOffset' OR CASE
            WHEN jsonb_typeof(player_state -> 'SubtitleOffset') = 'number'
                AND player_state ->> 'SubtitleOffset' ~ '^-?[0-9]+$'
            THEN (player_state ->> 'SubtitleOffset')::numeric BETWEEN -2147483648 AND 2147483647
            ELSE false END)
    ),
    ADD CONSTRAINT play_sessions_player_state_enums CHECK (
        (NOT player_state ? 'PlayMethod' OR (
            jsonb_typeof(player_state -> 'PlayMethod') = 'string'
            AND player_state ->> 'PlayMethod' IN ('DirectPlay', 'DirectStream', 'Transcode')))
        AND (NOT player_state ? 'RepeatMode' OR (
            jsonb_typeof(player_state -> 'RepeatMode') = 'string'
            AND player_state ->> 'RepeatMode' IN ('RepeatNone', 'RepeatAll', 'RepeatOne')))
    ),
    ADD CONSTRAINT play_sessions_player_state_rate CHECK (
        NOT player_state ? 'PlaybackRate' OR CASE
            WHEN jsonb_typeof(player_state -> 'PlaybackRate') = 'number'
            THEN (player_state ->> 'PlaybackRate')::numeric > 0
                AND (player_state ->> 'PlaybackRate')::numeric <= 10
            ELSE false END
    );

-- This object is display metadata supplied by a client. Server-owned playback
-- position, pause state, owner, item, and source remain authoritative columns.
