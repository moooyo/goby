-- Online source facts remain separate from local files and administrator locks.
ALTER TABLE item_metadata_state
    ADD COLUMN online_source jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(online_source) = 'object'),
    ADD COLUMN online_type text NOT NULL DEFAULT '' CHECK (length(online_type) <= 64),
    ADD COLUMN online_base jsonb CHECK (online_base IS NULL OR jsonb_typeof(online_base) = 'object');

CREATE TABLE item_provider_sources (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('tmdb', 'musicbrainz')),
    provider_id text NOT NULL CHECK (length(provider_id) BETWEEN 1 AND 256),
    source_url text NOT NULL CHECK (length(source_url) <= 2048),
    language text NOT NULL DEFAULT '' CHECK (length(language) <= 32),
    fields jsonb NOT NULL CHECK (jsonb_typeof(fields) = 'object'),
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, provider)
);

-- Selected artwork is bounded, server-owned data and never modifies media roots.
CREATE TABLE item_provider_images (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    image_type text NOT NULL CHECK (image_type IN ('Primary', 'Backdrop', 'Thumb', 'Banner', 'Logo', 'Art')),
    image_index integer NOT NULL CHECK (image_index BETWEEN 0 AND 31),
    provider text NOT NULL CHECK (provider = 'tmdb'),
    provider_id text NOT NULL CHECK (length(provider_id) BETWEEN 1 AND 256),
    image_id text NOT NULL CHECK (length(image_id) BETWEEN 1 AND 256),
    content bytea NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 20971520),
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/gif')),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 16384),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 16384),
    source_hash text NOT NULL CHECK (source_hash ~ '^[a-f0-9]{64}$'),
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, image_type, image_index),
    CHECK (width::bigint * height <= 26214400),
    CHECK (image_type = 'Backdrop' OR image_index = 0)
);
CREATE INDEX item_provider_images_fetched_idx ON item_provider_images(fetched_at);

CREATE TABLE item_subtitle_provider_sources (
    item_id text NOT NULL,
    stream_index integer NOT NULL,
    provider text NOT NULL CHECK (provider = 'opensubtitles'),
    provider_id text NOT NULL CHECK (length(provider_id) BETWEEN 1 AND 256),
    downloaded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, stream_index),
    FOREIGN KEY (item_id, stream_index) REFERENCES item_subtitles(item_id, stream_index) ON DELETE CASCADE
);
