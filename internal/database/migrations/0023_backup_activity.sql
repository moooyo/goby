-- Extend the existing activity history without changing or backfilling rows.
-- Migration 22 used PostgreSQL-generated CHECK names. Resolve only the four
-- uniquely identified constraints by their referenced column sets, and fail
-- closed if the catalog has an unexpected or ambiguous definition.
DO $$
DECLARE
    activity_table regclass := 'activity_entries'::regclass;
    target_columns text[];
    matching_names text[];
BEGIN
    FOR target_columns IN
        SELECT columns FROM (VALUES
            (ARRAY['action']::text[]),
            (ARRAY['resource_kind']::text[]),
            (ARRAY['action','state']::text[]),
            (ARRAY['action','resource_kind']::text[])
        ) AS targets(columns)
    LOOP
        SELECT array_agg(constraint_entry.conname::text ORDER BY constraint_entry.conname)
        INTO matching_names
        FROM pg_catalog.pg_constraint constraint_entry
        WHERE constraint_entry.conrelid = activity_table
            AND constraint_entry.contype = 'c'
            AND ARRAY(
                SELECT attribute_entry.attname::text
                FROM pg_catalog.pg_attribute attribute_entry
                WHERE attribute_entry.attrelid = constraint_entry.conrelid
                    AND attribute_entry.attnum = ANY(constraint_entry.conkey)
                ORDER BY attribute_entry.attname::text COLLATE "C"
            ) = target_columns;
        IF cardinality(matching_names) IS DISTINCT FROM 1 THEN
            RAISE EXCEPTION 'Activity constraint discovery found an unexpected catalog';
        END IF;
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', activity_table, matching_names[1]);
    END LOOP;
END
$$;

ALTER TABLE activity_entries
    ADD CONSTRAINT activity_entries_action_check CHECK (action IN (
        'user.created','user.updated','user.password_reset','session.login','session.revoked',
        'application_key.created','application_key.revealed','application_key.revoked',
        'device.updated','device.removed','library.created','library.removed',
        'scan.requested','scan.cancel_requested','scan.finished','metadata.updated','settings.updated',
        'task.admitted','task.cancel_requested','task.finished','task.schedule_updated',
        'backup.requested','backup.cancel_requested','backup.finished','backup.imported',
        'backup.delete_requested','backup.deleted','backup.downloaded',
        'restore.requested','restore.planned','restore.apply_requested','restore.applied',
        'restore.rollback_requested','restore.cancel_requested','restore.failed')),
    ADD CONSTRAINT activity_entries_resource_kind_check CHECK (resource_kind IN (
        'user','session','application_key','device','library','scan','item','settings','task','task_run',
        'backup','restore')),
    ADD CONSTRAINT activity_entries_terminal_state_check CHECK (
        (action IN ('scan.finished','task.finished','backup.finished')
            AND state IN ('completed','failed','cancelled','interrupted'))
        OR (action = 'restore.applied' AND state = 'completed')
        OR (action = 'restore.failed' AND state = 'failed')
        OR (action NOT IN ('scan.finished','task.finished','backup.finished','restore.applied','restore.failed')
            AND state = '')
    ),
    ADD CONSTRAINT activity_entries_action_resource_check CHECK (
        (action IN ('user.created','user.updated','user.password_reset') AND resource_kind = 'user')
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
    );

-- Durable outbox replay may acknowledge each phase only once. Repeated
-- authorized download preparations are separate facts and remain repeatable.
CREATE UNIQUE INDEX activity_entries_backup_restore_phase_idx
    ON activity_entries(action,resource_id)
    WHERE action IN (
        'backup.requested','backup.cancel_requested','backup.finished','backup.imported','backup.delete_requested','backup.deleted',
        'restore.requested','restore.planned','restore.apply_requested','restore.applied',
        'restore.rollback_requested','restore.cancel_requested','restore.failed');
