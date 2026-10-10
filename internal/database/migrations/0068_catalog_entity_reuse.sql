-- Resolve shared catalog dimensions without consuming identity values or
-- rewriting unchanged rows. Missing identities retain the conflict path so a
-- concurrent insert outside the statement snapshot still returns its exact ID.
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
    ), desired_entities AS MATERIALIZED (
        SELECT DISTINCT ON (kind, normalized_name) kind, display_name, normalized_name,
            sha256(convert_to(normalized_name, 'UTF8')) AS normalized_hash
        FROM associations
        ORDER BY kind, normalized_name, source_priority, position, credit_type
    ), existing_entities AS MATERIALIZED (
        -- Protect both the matched identity and its spelling until this owner
        -- commits. SHARE also conflicts with updates that do not change a key.
        SELECT entity.id, entity.kind, entity.normalized_name
        FROM catalog_entities entity JOIN desired_entities desired
            ON entity.kind = desired.kind AND entity.normalized_hash = desired.normalized_hash
            AND entity.normalized_name = desired.normalized_name
        ORDER BY entity.kind, entity.normalized_name
        FOR SHARE OF entity
    ), inserted_entities AS (
        INSERT INTO catalog_entities (kind, name)
        SELECT desired.kind, desired.display_name FROM desired_entities desired
        WHERE NOT EXISTS (
            SELECT 1 FROM existing_entities entity
            WHERE entity.kind = desired.kind AND entity.normalized_name = desired.normalized_name
        )
        ORDER BY desired.kind, desired.normalized_name
        -- A digest collision still fails instead of merging distinct names.
        -- Artist spelling wins a first-insert tie with an album-artist credit.
        ON CONFLICT (kind, normalized_hash) DO UPDATE SET name = CASE
            WHEN catalog_entities.normalized_name = EXCLUDED.normalized_name THEN catalog_entities.name
            ELSE NULL END
        RETURNING id, kind, normalized_name
    ), saved_entities AS (
        SELECT id, kind, normalized_name FROM existing_entities
        UNION ALL
        SELECT id, kind, normalized_name FROM inserted_entities
    )
    INSERT INTO item_entities (item_id, entity_id, position, display_name, role, credit_type, sort_order, credit_group)
    SELECT p_item_id, entity.id, source.position, source.display_name, source.role, source.credit_type, source.sort_order,
        source.source_priority::smallint
    FROM associations source JOIN saved_entities entity
        ON entity.kind = source.kind AND entity.normalized_name = source.normalized_name;
END;
$$;
