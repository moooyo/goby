-- Cache the existing v1 invalidation stamp with the root revision observed
-- when source facts are written. Readers still compare the current binding
-- and use the original expression on a miss; this cache grants no authority.
ALTER TABLE items
    ADD COLUMN media_operation_source_revision text,
    ADD COLUMN media_operation_source_binding_revision bigint;

CREATE FUNCTION refresh_item_media_operation_source_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    source_binding_revision bigint;
BEGIN
    IF NEW.media IS NULL OR NEW.root_id IS NULL THEN
        NEW.media_operation_source_revision := NULL;
        NEW.media_operation_source_binding_revision := NULL;
        RETURN NEW;
    END IF;
    SELECT root.binding_revision INTO source_binding_revision
    FROM library_roots root WHERE root.id = NEW.root_id;
    NEW.media_operation_source_binding_revision := source_binding_revision;
    NEW.media_operation_source_revision := 'media-operation-source-v1-' || md5(jsonb_build_array(
        NEW.root_id, NEW.relative_path, NEW.file_identity, NEW.file_size,
        extract(epoch FROM NEW.modified_at), NEW.media, source_binding_revision)::text);
    RETURN NEW;
END
$$;

CREATE TRIGGER items_media_operation_source_revision
BEFORE INSERT OR UPDATE OF root_id, relative_path, file_identity, file_size, modified_at, media ON items
FOR EACH ROW EXECUTE FUNCTION refresh_item_media_operation_source_revision();

-- One migration pass warms existing media without rewriting root bindings or
-- changing source fields and timestamps. Migration execution is transactional
-- and bounded by the migration runner's deadline; failure rolls back the pass.
UPDATE items item SET
    media_operation_source_binding_revision = root.binding_revision,
    media_operation_source_revision = 'media-operation-source-v1-' || md5(jsonb_build_array(
        item.root_id, item.relative_path, item.file_identity, item.file_size,
        extract(epoch FROM item.modified_at), item.media, root.binding_revision)::text)
FROM library_roots root
WHERE item.root_id = root.id AND item.media IS NOT NULL;
