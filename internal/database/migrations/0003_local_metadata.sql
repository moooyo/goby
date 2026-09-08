ALTER TABLE items
    ADD COLUMN local_metadata jsonb,
    ADD COLUMN local_metadata_hash text NOT NULL DEFAULT '',
    ADD COLUMN local_metadata_path text NOT NULL DEFAULT '';

ALTER TABLE items ADD CONSTRAINT items_local_metadata_hash_format
    CHECK (local_metadata_hash = '' OR local_metadata_hash ~ '^[0-9a-f]{64}$');
