CREATE TABLE item_subtitles (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
    stream_index integer NOT NULL CHECK (stream_index BETWEEN 0 AND 2147483647),
    active boolean NOT NULL DEFAULT true,
    relative_path text NOT NULL CHECK (
        relative_path <> '' AND relative_path NOT LIKE '/%'
        AND position(chr(92) IN relative_path) = 0
        AND relative_path !~ '(^|/)[.][.](/|$)'
    ),
    file_identity text NOT NULL,
    source_hash text NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
    file_size bigint NOT NULL CHECK (file_size BETWEEN 1 AND 8388608),
    modified_at timestamptz NOT NULL,
    change_time_ns bigint NOT NULL CHECK (change_time_ns >= 0),
    codec text NOT NULL CHECK (codec IN ('srt', 'vtt')),
    language text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    is_default boolean NOT NULL DEFAULT false,
    is_forced boolean NOT NULL DEFAULT false,
    is_hearing_impaired boolean NOT NULL DEFAULT false,
    mime_type text NOT NULL CHECK (mime_type IN ('application/x-subrip', 'text/vtt')),
    PRIMARY KEY (item_id, stream_index),
    CHECK ((codec = 'srt' AND mime_type = 'application/x-subrip') OR
        (codec = 'vtt' AND mime_type = 'text/vtt'))
);

-- Retired indexes remain reserved so an old URL cannot select a new track.
CREATE UNIQUE INDEX item_subtitles_active_path_idx ON item_subtitles(item_id, relative_path) WHERE active;
CREATE INDEX item_subtitles_root_idx ON item_subtitles(root_id);
