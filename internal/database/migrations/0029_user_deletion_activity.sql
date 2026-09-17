-- Deletion records retain their actor and target identifiers after the account
-- and its FK-owned rows disappear. No historical events are inferred or changed.
ALTER TABLE activity_entries
    DROP CONSTRAINT activity_entries_action_check,
    DROP CONSTRAINT activity_entries_action_resource_check;

ALTER TABLE activity_entries
    ADD CONSTRAINT activity_entries_action_check CHECK (action IN (
        'user.created','user.updated','user.deleted','user.password_reset','session.login','session.revoked',
        'application_key.created','application_key.revealed','application_key.revoked',
        'device.updated','device.removed','library.created','library.removed',
        'scan.requested','scan.cancel_requested','scan.finished','metadata.updated','settings.updated',
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
        OR (action = 'metadata.updated' AND resource_kind = 'item')
        OR (action = 'settings.updated' AND resource_kind = 'settings')
        OR (action IN ('task.admitted','task.cancel_requested','task.finished') AND resource_kind = 'task_run')
        OR (action = 'task.schedule_updated' AND resource_kind = 'task')
        OR (action IN ('backup.requested','backup.cancel_requested','backup.finished','backup.imported',
            'backup.delete_requested','backup.deleted','backup.downloaded') AND resource_kind = 'backup')
        OR (action IN ('restore.requested','restore.planned','restore.apply_requested','restore.applied',
            'restore.rollback_requested','restore.cancel_requested','restore.failed') AND resource_kind = 'restore')
        OR (action = 'library.root_binding.updated' AND resource_kind = 'library_root')
    );
