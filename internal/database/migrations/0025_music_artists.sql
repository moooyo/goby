-- Accepted embedded music facts are independent from raw NFO metadata and
-- administrator overrides. Existing rows have no inferred music provenance.
ALTER TABLE item_metadata_state
    ADD COLUMN music_source jsonb NOT NULL DEFAULT '{}'::jsonb
        CONSTRAINT item_metadata_state_music_source_check
        CHECK (jsonb_typeof(music_source) = 'object');

-- A music artist has its own durable identity, never a relabeled Person ID.
ALTER TABLE catalog_entities DROP CONSTRAINT catalog_entities_kind_check;
ALTER TABLE catalog_entities ADD CONSTRAINT catalog_entities_kind_check
    CHECK (kind IN ('Genre', 'Tag', 'Studio', 'Person', 'MusicArtist'));

-- The same artist may occupy the same position in both artist credit lists.
-- Legacy credit types are unbounded text, so keep them out of the index key.
-- Preserve every existing association and all original field values.
ALTER TABLE item_entities
    ADD COLUMN credit_group smallint NOT NULL DEFAULT 0
        CONSTRAINT item_entities_credit_group_check
        CHECK (credit_group IN (0, 1, 2));
ALTER TABLE item_entities DROP CONSTRAINT item_entities_pkey;
ALTER TABLE item_entities ADD CONSTRAINT item_entities_pkey
    PRIMARY KEY (item_id, entity_id, credit_group, position);

CREATE OR REPLACE FUNCTION sync_catalog_item_entities(p_item_id text, p_metadata jsonb)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM item_entities WHERE item_id = p_item_id;

    WITH raw_values AS (
        SELECT facets.kind, entry.value #>> '{}' AS name, entry.ordinality::integer AS position,
            ''::text AS role, ''::text AS credit_type, NULL::integer AS sort_order,
            0::integer AS source_priority
        FROM (VALUES ('Genre', 'Genres'), ('Tag', 'Tags'), ('Studio', 'Studios')) AS facets(kind, field)
        CROSS JOIN LATERAL jsonb_array_elements(
            CASE WHEN jsonb_typeof(p_metadata -> facets.field) = 'array'
                THEN p_metadata -> facets.field ELSE '[]'::jsonb END
        ) WITH ORDINALITY AS entry(value, ordinality)
        WHERE jsonb_typeof(entry.value) = 'string'
        UNION ALL
        SELECT 'Person', entry.value ->> 'Name', entry.ordinality::integer,
            CASE WHEN jsonb_typeof(entry.value -> 'Role') = 'string' THEN entry.value ->> 'Role' ELSE '' END,
            CASE WHEN jsonb_typeof(entry.value -> 'Type') = 'string' THEN entry.value ->> 'Type' ELSE '' END,
            CASE WHEN jsonb_typeof(entry.value -> 'SortOrder') = 'number'
                AND (entry.value ->> 'SortOrder') ~ '^[0-9]{1,10}$'
                THEN CASE WHEN (entry.value ->> 'SortOrder')::numeric <= 2147483647
                    THEN (entry.value ->> 'SortOrder')::integer ELSE NULL END
                ELSE NULL END,
            0::integer
        FROM jsonb_array_elements(
            CASE WHEN jsonb_typeof(p_metadata -> 'People') = 'array'
                THEN p_metadata -> 'People' ELSE '[]'::jsonb END
        ) WITH ORDINALITY AS entry(value, ordinality)
        WHERE jsonb_typeof(entry.value) = 'object' AND jsonb_typeof(entry.value -> 'Name') = 'string'
        UNION ALL
        SELECT 'MusicArtist', entry.value #>> '{}', entry.ordinality::integer,
            ''::text, credits.credit_type, NULL::integer, credits.source_priority
        FROM (VALUES ('Artist', 'Artists', 1), ('AlbumArtist', 'AlbumArtists', 2))
            AS credits(credit_type, field, source_priority)
        CROSS JOIN LATERAL jsonb_array_elements(
            CASE WHEN jsonb_typeof(p_metadata -> credits.field) = 'array'
                THEN p_metadata -> credits.field ELSE '[]'::jsonb END
        ) WITH ORDINALITY AS entry(value, ordinality)
        WHERE jsonb_typeof(entry.value) = 'string'
    ), valid_values AS (
        SELECT kind, btrim(name) AS display_name, lower(btrim(name)) AS normalized_name,
            position, role, credit_type, sort_order, source_priority
        FROM raw_values WHERE btrim(name) <> '' AND octet_length(name) <= 65536
    ), retained_values AS (
        SELECT DISTINCT ON (kind, normalized_name, credit_type) * FROM valid_values
        WHERE kind <> 'Person' ORDER BY kind, normalized_name, credit_type, position
    ), associations AS (
        SELECT * FROM retained_values
        UNION ALL
        SELECT * FROM valid_values WHERE kind = 'Person'
    ), saved_entities AS (
        INSERT INTO catalog_entities (kind, name)
        SELECT DISTINCT ON (kind, normalized_name) kind, display_name FROM associations
        ORDER BY kind, normalized_name, source_priority, position, credit_type
        -- A digest collision still fails instead of merging distinct names.
        -- Artist spelling wins a first-insert tie with an album-artist credit.
        ON CONFLICT (kind, normalized_hash) DO UPDATE SET name = CASE
            WHEN catalog_entities.normalized_name = EXCLUDED.normalized_name THEN catalog_entities.name
            ELSE NULL END
        RETURNING id, kind, normalized_name
    )
    INSERT INTO item_entities (item_id, entity_id, position, display_name, role, credit_type, sort_order, credit_group)
    SELECT p_item_id, entity.id, source.position, source.display_name, source.role, source.credit_type, source.sort_order,
        source.source_priority::smallint
    FROM associations source JOIN saved_entities entity
        ON entity.kind = source.kind AND entity.normalized_name = source.normalized_name;
END;
$$;
