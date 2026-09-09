-- Automatic source snapshots and administrator state are deliberately separate
-- from the sparse local NFO projection already stored on catalog items.
CREATE FUNCTION catalog_metadata_automatic_values(
    p_name text, p_sort_name text, p_overview text, p_type text,
    p_index integer, p_parent_index integer, p_source jsonb
) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT source || jsonb_build_object(
        'Name', p_name, 'SortName', p_sort_name, 'Overview', p_overview,
        'OriginalTitle', COALESCE(source->>'OriginalTitle', ''),
        'OfficialRating', COALESCE(source->>'OfficialRating', ''),
        'ProductionYear', source->'ProductionYear',
        'PremiereDate', source->'PremiereDate',
        'CommunityRating', source->'CommunityRating',
        'ProviderIDs', CASE WHEN jsonb_typeof(source->'ProviderIDs') = 'object' THEN source->'ProviderIDs' ELSE '{}'::jsonb END,
        'Genres', CASE WHEN jsonb_typeof(source->'Genres') = 'array' THEN source->'Genres' ELSE '[]'::jsonb END,
        'Tags', CASE WHEN jsonb_typeof(source->'Tags') = 'array' THEN source->'Tags' ELSE '[]'::jsonb END,
        'Studios', CASE WHEN jsonb_typeof(source->'Studios') = 'array' THEN source->'Studios' ELSE '[]'::jsonb END,
        'People', CASE WHEN jsonb_typeof(source->'People') = 'array' THEN source->'People' ELSE '[]'::jsonb END,
        'IndexNumber', CASE WHEN p_type IN ('Season', 'Episode') THEN p_index END,
        'ParentIndexNumber', CASE WHEN p_type = 'Episode' THEN p_parent_index END)
    FROM (SELECT CASE WHEN jsonb_typeof(p_source) = 'object' THEN p_source ELSE '{}'::jsonb END AS source) normalized;
$$;

CREATE FUNCTION catalog_metadata_source_key(p_item items) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_build_object('Hash', p_item.local_metadata_hash, 'NFOPath', p_item.local_metadata_path,
        'Type', p_item.type, 'IsFolder', p_item.is_folder, 'ParentId', p_item.parent_id,
        'Path', p_item.path, 'RootId', p_item.root_id, 'RelativePath', p_item.relative_path,
        'HasNFO', p_item.local_metadata IS NOT NULL);
$$;

CREATE TABLE item_metadata_state (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    automatic jsonb NOT NULL CHECK (jsonb_typeof(automatic) = 'object'),
    source_key jsonb NOT NULL CHECK (jsonb_typeof(source_key) = 'object'),
    overrides jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(overrides) = 'object'),
    locked_values jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(locked_values) = 'object'),
    effective jsonb,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    last_edited_by text REFERENCES users(id) ON DELETE SET NULL,
    last_edited_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Backfill never rewrites catalog values or any existing related table. The
-- effective projection retains all pre-upgrade omission and null semantics.
INSERT INTO item_metadata_state (item_id, automatic, source_key, effective)
SELECT i.id, catalog_metadata_automatic_values(i.name, i.sort_name, i.overview,
    i.type, i.index_number, i.parent_index_number, i.local_metadata),
    catalog_metadata_source_key(i), i.local_metadata
FROM items i;

CREATE FUNCTION initialize_catalog_metadata_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO item_metadata_state (item_id, automatic, source_key, effective)
    VALUES (NEW.id, catalog_metadata_automatic_values(NEW.name, NEW.sort_name, NEW.overview,
        NEW.type, NEW.index_number, NEW.parent_index_number, NEW.local_metadata),
        catalog_metadata_source_key(NEW), NEW.local_metadata)
    ON CONFLICT (item_id) DO NOTHING;
    RETURN NEW;
END;
$$;

CREATE TRIGGER items_initialize_metadata_state AFTER INSERT ON items
    FOR EACH ROW EXECUTE FUNCTION initialize_catalog_metadata_state();
