CREATE TABLE media_deletion_operations (
    id text PRIMARY KEY,
    item_id text NOT NULL,
    library_id text NOT NULL REFERENCES libraries(id) ON DELETE RESTRICT,
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE RESTRICT,
    actor_id text NOT NULL,
    credential_id text NOT NULL,
    origin_host text NOT NULL CHECK (origin_host ~ '^[0-9a-f]{64}$'),
    kind text NOT NULL CHECK (kind IN ('media', 'subtitle')),
    subtitle_index integer NOT NULL DEFAULT -1,
    state text NOT NULL CHECK (state IN ('prepared', 'catalog_removed')),
    source_snapshot jsonb NOT NULL CHECK (jsonb_typeof(source_snapshot) = 'object'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (item_id, kind, subtitle_index),
    CHECK ((kind = 'media' AND subtitle_index = -1) OR (kind = 'subtitle' AND subtitle_index >= 0))
);

CREATE INDEX media_deletion_operations_library_idx ON media_deletion_operations(library_id);

-- A prepared filesystem move is reversible and remains outside SQL locks.
-- Serialize scan admission against its durable journal using the library row.
CREATE FUNCTION prevent_scans_during_media_deletion() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('Queued', 'Running') THEN
        PERFORM id FROM libraries WHERE id = NEW.library_id FOR KEY SHARE;
        IF EXISTS (SELECT 1 FROM media_deletion_operations WHERE library_id = NEW.library_id) THEN
            RAISE EXCEPTION 'media deletion recovery is pending for this library' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER scan_jobs_pending_media_deletion
BEFORE INSERT OR UPDATE OF status ON scan_jobs
FOR EACH ROW EXECUTE FUNCTION prevent_scans_during_media_deletion();

CREATE FUNCTION prevent_root_changes_during_media_deletion() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM media_deletion_operations WHERE root_id = OLD.id) THEN
        RAISE EXCEPTION 'media deletion recovery is pending for this root' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER library_roots_pending_media_deletion
BEFORE UPDATE OF path, allowed_path, relative_path, binding_revision, storage_binding ON library_roots
FOR EACH ROW EXECUTE FUNCTION prevent_root_changes_during_media_deletion();


ALTER TABLE activity_entries
    DROP CONSTRAINT activity_entries_action_check,
    DROP CONSTRAINT activity_entries_action_resource_check;

ALTER TABLE activity_entries
    ADD CONSTRAINT activity_entries_action_check CHECK (action IN (
        'user.created','user.updated','user.deleted','user.password_reset','session.login','session.revoked',
        'application_key.created','application_key.revealed','application_key.revoked',
        'device.updated','device.removed','library.created','library.removed',
        'scan.requested','scan.cancel_requested','scan.finished','metadata.updated','item.deleted','subtitle.deleted','settings.updated',
        'task.admitted','task.cancel_requested','task.finished','task.schedule_updated',
        'backup.requested','backup.cancel_requested','backup.finished','backup.imported',
        'backup.delete_requested','backup.deleted','backup.downloaded',
        'restore.requested','restore.planned','restore.apply_requested','restore.applied',
        'restore.rollback_requested','restore.cancel_requested','restore.failed',
        'library.root_binding.updated')),
    ADD CONSTRAINT activity_entries_action_resource_check CHECK (
        (action IN ('user.created','user.updated','user.deleted','user.password_reset') AND resource_kind = 'user')
        OR (action IN ('session.login','session.revoked') AND resource_kind = 'session')
        OR (action IN ('application_key.created','application_key.revealed','application_key.revoked') AND resource_kind = 'application_key')
        OR (action IN ('device.updated','device.removed') AND resource_kind = 'device')
        OR (action IN ('library.created','library.removed') AND resource_kind = 'library')
        OR (action IN ('scan.requested','scan.cancel_requested','scan.finished') AND resource_kind = 'scan')
        OR (action IN ('metadata.updated','item.deleted','subtitle.deleted') AND resource_kind = 'item')
        OR (action = 'settings.updated' AND resource_kind = 'settings')
        OR (action IN ('task.admitted','task.cancel_requested','task.finished') AND resource_kind = 'task_run')
        OR (action = 'task.schedule_updated' AND resource_kind = 'task')
        OR (action IN ('backup.requested','backup.cancel_requested','backup.finished','backup.imported',
            'backup.delete_requested','backup.deleted','backup.downloaded') AND resource_kind = 'backup')
        OR (action IN ('restore.requested','restore.planned','restore.apply_requested','restore.applied',
            'restore.rollback_requested','restore.cancel_requested','restore.failed') AND resource_kind = 'restore')
        OR (action = 'library.root_binding.updated' AND resource_kind = 'library_root')
    );
