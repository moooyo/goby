-- Bitmap sidecars have independently indexed language tracks and immutable
-- component snapshots. They do not enter text delivery or single-file deletion.
CREATE TABLE item_bitmap_subtitles (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
    stream_index integer NOT NULL CHECK (stream_index BETWEEN 0 AND 2147483647),
    active boolean NOT NULL DEFAULT true,
    relative_path text NOT NULL CHECK (
        relative_path <> '' AND relative_path NOT LIKE '/%'
        AND position(chr(92) IN relative_path) = 0
        AND relative_path !~ '(^|/)[.][.](/|$)'
    ),
    source_stream_index integer NOT NULL CHECK (source_stream_index BETWEEN 0 AND 31),
    format text NOT NULL CHECK (format IN ('sup', 'vobsub')),
    codec text NOT NULL CHECK (codec IN ('hdmv_pgs_subtitle', 'dvd_subtitle')),
    source_hash text NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
    language text NOT NULL DEFAULT '' CHECK (octet_length(language) <= 32),
    title text NOT NULL DEFAULT '' CHECK (octet_length(title) <= 512),
    is_default boolean NOT NULL DEFAULT false,
    is_forced boolean NOT NULL DEFAULT false,
    is_hearing_impaired boolean NOT NULL DEFAULT false,
    components jsonb NOT NULL CHECK (jsonb_typeof(components) = 'array'),
    PRIMARY KEY (item_id, stream_index),
    CHECK ((format = 'sup' AND codec = 'hdmv_pgs_subtitle' AND source_stream_index = 0
            AND jsonb_array_length(components) = 1) OR
        (format = 'vobsub' AND codec = 'dvd_subtitle' AND jsonb_array_length(components) = 2))
);

-- The item lock serializes allocation across file, owned, and bitmap tracks.
-- Retired public indexes remain reserved, including every language of a pair.
CREATE UNIQUE INDEX item_bitmap_subtitles_active_source_idx
    ON item_bitmap_subtitles(item_id, relative_path, source_stream_index) WHERE active;
CREATE INDEX item_bitmap_subtitles_root_idx ON item_bitmap_subtitles(root_id);
