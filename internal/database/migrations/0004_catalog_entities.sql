CREATE TABLE catalog_entities (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('Genre', 'Tag', 'Studio', 'Person')),
    name text NOT NULL CHECK (btrim(name) <> ''),
    normalized_name text GENERATED ALWAYS AS (lower(btrim(name))) STORED,
    normalized_hash bytea NOT NULL CHECK (octet_length(normalized_hash) = 32)
        CHECK (normalized_hash = sha256(convert_to(lower(btrim(name)), 'UTF8'))),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, normalized_hash)
);

-- NFO fields may exceed the B-tree key size. Keep the complete normalized name
-- for exact matching and index its fixed-size SHA-256 digest instead. Hashing is
-- done by PostgreSQL in both migrations and live writes, without an extension.
CREATE FUNCTION set_catalog_entity_name_hash()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.normalized_hash := sha256(convert_to(lower(btrim(NEW.name)), 'UTF8'));
    RETURN NEW;
END;
$$;

CREATE TRIGGER catalog_entities_name_hash BEFORE INSERT OR UPDATE OF name ON catalog_entities
FOR EACH ROW EXECUTE FUNCTION set_catalog_entity_name_hash();

CREATE TABLE item_entities (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    entity_id bigint NOT NULL REFERENCES catalog_entities(id),
    position integer NOT NULL CHECK (position > 0),
    display_name text NOT NULL,
    role text NOT NULL DEFAULT '',
    credit_type text NOT NULL DEFAULT '',
    sort_order integer CHECK (sort_order >= 0),
    PRIMARY KEY (item_id, entity_id, position)
);

CREATE INDEX item_entities_entity_item_idx ON item_entities(entity_id, item_id);

-- This function is invoked inside the same owned transaction that persists the
-- item's NFO metadata. It is also the migration backfill path, so normalization
-- and duplicate handling are identical for new and previously scanned items.
CREATE FUNCTION sync_catalog_item_entities(p_item_id text, p_metadata jsonb)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM item_entities WHERE item_id = p_item_id;

    WITH raw_values AS (
        SELECT facets.kind, entry.value #>> '{}' AS name, entry.ordinality::integer AS position,
            ''::text AS role, ''::text AS credit_type, NULL::integer AS sort_order
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
                ELSE NULL END
        FROM jsonb_array_elements(
            CASE WHEN jsonb_typeof(p_metadata -> 'People') = 'array'
                THEN p_metadata -> 'People' ELSE '[]'::jsonb END
        ) WITH ORDINALITY AS entry(value, ordinality)
        WHERE jsonb_typeof(entry.value) = 'object' AND jsonb_typeof(entry.value -> 'Name') = 'string'
    ), valid_values AS (
        SELECT kind, btrim(name) AS display_name, lower(btrim(name)) AS normalized_name,
            position, role, credit_type, sort_order
        FROM raw_values WHERE btrim(name) <> '' AND octet_length(name) <= 65536
    ), retained_values AS (
        SELECT DISTINCT ON (kind, normalized_name) * FROM valid_values
        WHERE kind <> 'Person' ORDER BY kind, normalized_name, position
    ), associations AS (
        SELECT * FROM retained_values
        UNION ALL
        SELECT * FROM valid_values WHERE kind = 'Person'
    ), saved_entities AS (
        INSERT INTO catalog_entities (kind, name)
        SELECT DISTINCT ON (kind, normalized_name) kind, display_name FROM associations
        ORDER BY kind, normalized_name, position
        -- A digest collision must fail rather than merge distinct identities.
        ON CONFLICT (kind, normalized_hash) DO UPDATE SET name = CASE
            WHEN catalog_entities.normalized_name = EXCLUDED.normalized_name THEN catalog_entities.name
            ELSE NULL END
        RETURNING id, kind, normalized_name
    )
    INSERT INTO item_entities (item_id, entity_id, position, display_name, role, credit_type, sort_order)
    SELECT p_item_id, entity.id, source.position, source.display_name, source.role, source.credit_type, source.sort_order
    FROM associations source JOIN saved_entities entity
        ON entity.kind = source.kind AND entity.normalized_name = source.normalized_name;
END;
$$;

-- Existing NFO metadata becomes navigable immediately after upgrading from
-- migration 0003. No filesystem scan or user action is required for this step.
SELECT sync_catalog_item_entities(id, local_metadata) FROM items
WHERE local_metadata IS NOT NULL ORDER BY created_at, id;
