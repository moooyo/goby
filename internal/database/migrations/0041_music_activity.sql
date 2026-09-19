-- Admit only the new music field names for metadata activity. Retain the
-- existing check name and every unrelated action/field rule; do not rewrite or
-- backfill historical activity values or earlier published migrations.
DO $$
DECLARE constraint_name text;
        constraint_definition text;
        marker text := '''People''::text';
        marker_position integer;
BEGIN
    SELECT conname, pg_get_constraintdef(oid) INTO STRICT constraint_name, constraint_definition
    FROM pg_constraint WHERE conrelid = 'activity_entries'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%changed_fields%'
      AND pg_get_constraintdef(oid) LIKE '%metadata.updated%'
      AND pg_get_constraintdef(oid) LIKE '%Overrides%';
    marker_position := strpos(constraint_definition, marker);
    IF marker_position = 0
        OR strpos(substr(constraint_definition, marker_position + length(marker)), marker) <> 0
        OR constraint_definition LIKE '%AlbumArtists%' THEN
        RAISE EXCEPTION 'music activity constraint cannot be extended';
    END IF;
    constraint_definition := replace(constraint_definition, marker,
        marker || ', ''Album''::text, ''Artists''::text, ''AlbumArtists''::text');
    EXECUTE format('ALTER TABLE activity_entries DROP CONSTRAINT %I', constraint_name);
    EXECUTE format('ALTER TABLE activity_entries ADD CONSTRAINT %I %s', constraint_name, constraint_definition);
END $$;
