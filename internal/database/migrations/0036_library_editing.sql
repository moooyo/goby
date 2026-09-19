ALTER TABLE libraries
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN options jsonb NOT NULL DEFAULT '{"EnableLocalMetadata":true,"EnableLocalImages":true}'::jsonb,
    ADD CONSTRAINT libraries_options_check CHECK (
        jsonb_typeof(options) = 'object'
        AND options ?& ARRAY['EnableLocalMetadata','EnableLocalImages']
        AND options - 'EnableLocalMetadata' - 'EnableLocalImages' = '{}'::jsonb
        AND jsonb_typeof(options->'EnableLocalMetadata') = 'boolean'
        AND jsonb_typeof(options->'EnableLocalImages') = 'boolean'
    );

-- Root approvals and mappings share the edit revision. A root change therefore
-- invalidates an editor opened before the independent binding operation.
CREATE FUNCTION advance_library_edit_revision() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE libraries SET revision = revision + 1 WHERE id = OLD.library_id;
        RETURN OLD;
    END IF;
    UPDATE libraries SET revision = revision + 1 WHERE id = NEW.library_id;
    IF TG_OP = 'UPDATE' AND OLD.library_id <> NEW.library_id THEN
        UPDATE libraries SET revision = revision + 1 WHERE id = OLD.library_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER library_roots_edit_revision
AFTER INSERT OR DELETE OR UPDATE OF library_id, path, allowed_path, relative_path, binding_revision, storage_binding ON library_roots
FOR EACH ROW EXECUTE FUNCTION advance_library_edit_revision();

ALTER TABLE activity_entries
    DROP CONSTRAINT activity_entries_action_check,
    DROP CONSTRAINT activity_entries_action_resource_check;

ALTER TABLE activity_entries
    ADD CONSTRAINT activity_entries_action_check CHECK (action IN (
        'user.created','user.updated','user.deleted','user.password_reset','session.login','session.revoked',
        'application_key.created','application_key.revealed','application_key.revoked',
        'device.updated','device.removed','library.created','library.updated','library.removed',
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
        OR (action IN ('library.created','library.updated','library.removed') AND resource_kind = 'library')
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
