-- Colon characters name ordinary Linux path components. Keep every existing
-- root-relative canonical-path restriction without applying Windows drives.
-- Historical migrations and archive catalogs retain their original semantics.
ALTER TABLE theme_reserved_paths
    DROP CONSTRAINT theme_reserved_paths_canonical_check,
    ADD CONSTRAINT theme_reserved_paths_canonical_check CHECK (
        relative_path <> '' AND position(chr(92) in relative_path) = 0
        AND NOT (string_to_array(relative_path, '/') && ARRAY['', '.', '..'])
    );

ALTER TABLE extra_reserved_paths
    DROP CONSTRAINT extra_reserved_paths_canonical_check,
    ADD CONSTRAINT extra_reserved_paths_canonical_check CHECK (
        relative_path <> '' AND position(chr(92) in relative_path) = 0
        AND NOT (string_to_array(relative_path, '/') && ARRAY['', '.', '..'])
    );
