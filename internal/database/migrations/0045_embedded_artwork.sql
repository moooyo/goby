CREATE TABLE item_embedded_artwork (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    source_revision text NOT NULL CHECK (source_revision ~ '^embedded-source-v1-[0-9a-f]{32}$'),
    probe_version integer NOT NULL CHECK (probe_version > 0),
    extraction_version integer NOT NULL CHECK (extraction_version > 0),
    status text NOT NULL CHECK (status IN ('ready', 'none', 'failed')),
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code IN ('', 'extraction_failed', 'cache_full')),
    stream_index integer,
    picture_type text,
    source_hash text,
    mime_type text,
    width integer,
    height integer,
    content bytea,
    inspected_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (
        (status = 'ready' AND failure_code = '' AND stream_index IS NOT NULL AND stream_index BETWEEN 0 AND 4095
            AND picture_type IS NOT NULL AND picture_type IN ('Front', 'Other', 'Back')
            AND source_hash IS NOT NULL AND source_hash ~ '^[0-9a-f]{64}$'
            AND mime_type IS NOT NULL AND mime_type IN ('image/jpeg', 'image/png', 'image/gif')
            AND width IS NOT NULL AND width BETWEEN 1 AND 16384
            AND height IS NOT NULL AND height BETWEEN 1 AND 16384 AND width::bigint * height <= 26214400
            AND content IS NOT NULL AND octet_length(content) BETWEEN 1 AND 20971520)
        OR (status IN ('none', 'failed') AND stream_index IS NULL AND picture_type IS NULL
            AND source_hash IS NULL AND mime_type IS NULL AND width IS NULL AND height IS NULL AND content IS NULL
            AND ((status = 'none' AND failure_code = '') OR (status = 'failed' AND failure_code <> '')))
    )
);
