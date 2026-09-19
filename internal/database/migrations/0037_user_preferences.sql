ALTER TABLE users ADD COLUMN configuration_revision bigint NOT NULL DEFAULT 1
    CHECK (configuration_revision > 0);

CREATE TABLE display_preferences (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client text NOT NULL CHECK (octet_length(client) BETWEEN 1 AND 256),
    preferences_id text NOT NULL CHECK (octet_length(preferences_id) BETWEEN 1 AND 256),
    preferences jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(preferences) = 'object' AND octet_length(preferences::text) <= 131072),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, client, preferences_id)
);

ALTER TABLE user_item_data
    ADD COLUMN hide_from_resume boolean NOT NULL DEFAULT false,
    ADD COLUMN rating double precision CHECK (rating >= 0 AND rating <= 10),
    ADD COLUMN likes boolean,
    ADD COLUMN remembered_media_source_id text NOT NULL DEFAULT '' CHECK (octet_length(remembered_media_source_id) <= 256),
    ADD COLUMN remembered_media_stamp text NOT NULL DEFAULT '' CHECK (remembered_media_stamp = '' OR remembered_media_stamp ~ '^[0-9a-f]{32}$'),
    ADD COLUMN remembered_audio_stream_index integer CHECK (remembered_audio_stream_index >= 0),
    ADD COLUMN remembered_subtitle_stream_index integer CHECK (remembered_subtitle_stream_index >= -1);
