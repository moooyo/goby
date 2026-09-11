-- Theme owner numbers belong only to this namespace. They are not catalog
-- entity IDs, item aliases, or a global numeric routing namespace.
-- Hold the item write boundary through trigger installation and backfill.
LOCK TABLE items IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE theme_owner_ids (
    id bigint GENERATED ALWAYS AS IDENTITY (START WITH 1 MINVALUE 1 NO CYCLE)
        PRIMARY KEY CONSTRAINT theme_owner_ids_id_check CHECK (id > 0),
    item_id text UNIQUE REFERENCES items(id) ON DELETE CASCADE,
    virtual_root boolean NOT NULL DEFAULT false,
    CONSTRAINT theme_owner_ids_kind_check CHECK (
        (virtual_root AND item_id IS NULL) OR (NOT virtual_root AND item_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX theme_owner_ids_virtual_root_idx
    ON theme_owner_ids (virtual_root) WHERE virtual_root;

CREATE FUNCTION assign_theme_owner_id()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Bind to the inserting item's schema rather than a caller search path.
    EXECUTE pg_catalog.format('INSERT INTO %I.theme_owner_ids (item_id) VALUES ($1)', TG_TABLE_SCHEMA)
        USING NEW.id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER items_assign_theme_owner_id AFTER INSERT ON items
    FOR EACH ROW EXECUTE FUNCTION assign_theme_owner_id();

-- Only this transaction's newly created identity sequence is consumed. No old
-- item, entity, sequence, or migration-history row is rewritten by the backfill.
INSERT INTO theme_owner_ids (virtual_root) VALUES (true);
INSERT INTO theme_owner_ids (item_id) SELECT id FROM items ORDER BY id;

-- FK/UNIQUE checks prevent orphaned or duplicate owners, but cannot require
-- every item and the virtual root to be present after trigger-disabled restore.
-- Restore semantic validation must separately require complete item coverage
-- and exactly one virtual root; a read must never repair missing mappings.

-- Reservations outlive a failed scan or an inactive resource. Directory
-- markers cover the exact directory and every descendant within this root.
CREATE TABLE theme_reserved_paths (
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
    relative_path text COLLATE "C" NOT NULL,
    is_directory boolean NOT NULL,
    PRIMARY KEY (root_id, relative_path),
    CONSTRAINT theme_reserved_paths_canonical_check CHECK (
        relative_path <> '' AND position(chr(92) in relative_path) = 0
        AND relative_path !~ '^[A-Za-z]:'
        AND NOT (string_to_array(relative_path, '/') && ARRAY['', '.', '..'])
    )
);

-- Cross-row eligibility belongs to the shared visibility/backup validator.
-- Inactive history may retain an owner that moved roots or became reserved.
CREATE TABLE item_theme_resources (
    resource_item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    owner_item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('song', 'video')),
    active boolean NOT NULL,
    CONSTRAINT item_theme_resources_distinct_owner_check CHECK (resource_item_id <> owner_item_id)
);
CREATE INDEX item_theme_resources_owner_idx ON item_theme_resources (owner_item_id, kind, resource_item_id);

-- Reserve only known canonical layouts already present in the old catalog.
-- A descendant proves its ancestor component is a directory even if no old
-- folder item was indexed. ASCII translate deliberately avoids Unicode folds.
-- This migration does not infer an attachment owner, reparent any item, or
-- claim that an old file was successfully probed as a theme resource.
WITH canonical_items AS (
    SELECT root_id, relative_path, is_folder, string_to_array(relative_path, '/') AS components
    FROM items WHERE root_id IS NOT NULL AND relative_path <> ''
        AND position(chr(92) in relative_path) = 0 AND relative_path !~ '^[A-Za-z]:'
        AND NOT (string_to_array(relative_path, '/') && ARRAY['', '.', '..'])
), directories AS (
    SELECT item.root_id, array_to_string(item.components[1:component.position], '/') AS relative_path,
        true AS is_directory
    FROM canonical_items item CROSS JOIN LATERAL generate_subscripts(item.components, 1) AS component(position)
    WHERE (component.position < array_length(item.components, 1) OR item.is_folder)
        AND translate(item.components[component.position], 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz')
            IN ('theme-music', 'backdrops')
), files AS (
    SELECT root_id, relative_path, false AS is_directory FROM canonical_items
    WHERE NOT is_folder AND translate(components[array_length(components, 1)],
        'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz')
        IN ('theme.mp3', 'theme.flac', 'theme.m4a', 'theme.aac', 'theme.ogg', 'theme.opus', 'theme.wav', 'theme.wma',
            'theme.aiff', 'theme.aif', 'theme.alac', 'theme.ape', 'theme.mka')
), markers AS (
    SELECT * FROM directories UNION ALL SELECT * FROM files
)
INSERT INTO theme_reserved_paths (root_id, relative_path, is_directory)
SELECT root_id, relative_path, bool_or(is_directory) FROM markers GROUP BY root_id, relative_path;
