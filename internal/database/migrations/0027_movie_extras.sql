-- The migration runner holds the schema-26 write boundary and validates its
-- original semantic state before executing this additive transition.
CREATE TABLE extra_reserved_paths (
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
    relative_path text COLLATE "C" NOT NULL,
    is_directory boolean NOT NULL,
    PRIMARY KEY (root_id, relative_path),
    CONSTRAINT extra_reserved_paths_canonical_check CHECK (
        relative_path <> '' AND position(chr(92) in relative_path) = 0
        AND relative_path !~ '^[A-Za-z]:'
        AND NOT (string_to_array(relative_path, '/') && ARRAY['', '.', '..'])
    )
);

-- Both resource tables retain role history. Publication serializes on item
-- rows; current-schema semantic validation rejects cross-role membership even
-- when one or both associations are inactive.
CREATE TABLE item_extra_resources (
    resource_item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    owner_item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('clip', 'deleted_scene', 'trailer')),
    active boolean NOT NULL,
    CONSTRAINT item_extra_resources_distinct_owner_check CHECK (resource_item_id <> owner_item_id)
);
CREATE INDEX item_extra_resources_owner_idx ON item_extra_resources (owner_item_id, kind, resource_item_id);

-- A stored descendant proves that its ancestor is a directory. Only canonical
-- paths in Movies libraries participate. The first auxiliary directory is the
-- boundary: an outer theme directory blocks inner extras, and an outer extra
-- directory reserves all descendants, including nested and nonvideo entries.
-- ASCII translation deliberately excludes Unicode case-fold lookalikes.
WITH canonical_items AS (
    SELECT item.root_id, item.relative_path, item.is_folder,
        string_to_array(item.relative_path, '/') AS components
    FROM items item JOIN library_roots root ON root.id = item.root_id
        AND root.library_id = item.library_id
        JOIN libraries library ON library.id = root.library_id
    WHERE library.collection_type = 'movies' AND item.relative_path <> ''
        AND position(chr(92) in item.relative_path) = 0 AND item.relative_path !~ '^[A-Za-z]:'
        AND NOT (string_to_array(item.relative_path, '/') && ARRAY['', '.', '..'])
), boundaries AS (
    SELECT item.root_id, array_to_string(item.components[1:boundary.position], '/') AS relative_path,
        boundary.layout
    FROM canonical_items item CROSS JOIN LATERAL (
        SELECT component.position,
            translate(item.components[component.position], 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz') AS layout
        FROM generate_subscripts(item.components, 1) AS component(position)
        WHERE (component.position < array_length(item.components, 1) OR item.is_folder)
            AND translate(item.components[component.position], 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz')
                IN ('theme-music', 'backdrops', 'featurettes', 'deleted scenes', 'trailers')
        ORDER BY component.position LIMIT 1
    ) boundary
), markers AS MATERIALIZED (
    SELECT DISTINCT root_id, relative_path FROM boundaries
    WHERE layout IN ('featurettes', 'deleted scenes', 'trailers')
), affected_links AS MATERIALIZED (
    SELECT link.resource_item_id, link.owner_item_id
    FROM item_theme_resources link JOIN items owner ON owner.id = link.owner_item_id
    WHERE link.active AND EXISTS (SELECT 1 FROM markers marker
        WHERE marker.root_id = owner.root_id AND (owner.relative_path COLLATE "C" = marker.relative_path
            OR left(owner.relative_path, length(marker.relative_path) + 1) COLLATE "C" = marker.relative_path || '/'))
), locked_items AS MATERIALIZED (
    SELECT item.id FROM items item
    WHERE item.id IN (SELECT resource_item_id FROM affected_links UNION SELECT owner_item_id FROM affected_links)
    ORDER BY item.id COLLATE "C" FOR UPDATE
)
INSERT INTO extra_reserved_paths (root_id, relative_path, is_directory)
SELECT marker.root_id, marker.relative_path, true FROM markers marker
CROSS JOIN (SELECT count(*) FROM locked_items) lock_boundary;

-- Only active Theme relationships whose previously ordinary owners are now
-- covered by the newly created reservations may change. No item, metadata,
-- identity, sequence, unrelated relationship or inactive history is rewritten.
UPDATE item_theme_resources link SET active = false
FROM items owner WHERE owner.id = link.owner_item_id AND link.active
    AND EXISTS (SELECT 1 FROM extra_reserved_paths marker
        WHERE marker.root_id = owner.root_id AND (owner.relative_path COLLATE "C" = marker.relative_path
            OR (marker.is_directory AND left(owner.relative_path, length(marker.relative_path) + 1) COLLATE "C"
                = marker.relative_path || '/')));

-- The runner validates strict combined schema-27 semantics in this same
-- transaction before recording successful migration or permitting commit.
