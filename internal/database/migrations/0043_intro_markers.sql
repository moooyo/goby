CREATE TABLE item_intro_state (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    source_revision text NOT NULL DEFAULT '',
    start_ticks bigint,
    end_ticks bigint,
    provenance text NOT NULL DEFAULT '',
    last_edited_by text REFERENCES users(id) ON DELETE SET NULL,
    last_edited_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((start_ticks IS NULL AND end_ticks IS NULL AND source_revision = '' AND provenance = '')
        OR (start_ticks IS NOT NULL AND end_ticks IS NOT NULL AND start_ticks >= 0
            AND end_ticks > start_ticks AND source_revision <> '' AND provenance IN ('Manual', 'Import')))
);
