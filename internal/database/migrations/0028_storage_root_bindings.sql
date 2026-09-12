-- Existing roots remain explicitly unbound. This migration observes no media
-- path and never infers storage approval from a scan or a registered pathname.
ALTER TABLE library_roots
    ADD COLUMN binding_revision bigint NOT NULL DEFAULT 1,
    ADD COLUMN storage_binding jsonb,
    ADD COLUMN bound_at timestamptz,
    ADD COLUMN bound_by text,
    ADD CONSTRAINT library_roots_binding_revision_check CHECK (binding_revision > 0),
    ADD CONSTRAINT library_roots_storage_binding_check CHECK (
        (storage_binding IS NULL AND bound_at IS NULL AND bound_by IS NULL)
        OR (storage_binding IS NOT NULL AND jsonb_typeof(storage_binding) = 'object'
            AND octet_length(storage_binding::text) <= 4194304
            AND bound_at IS NOT NULL AND isfinite(bound_at)
            AND bound_by IS NOT NULL AND octet_length(bound_by) BETWEEN 1 AND 256
            AND bound_by COLLATE "C" ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$')
    );

-- Historical audit identifiers remain values, not cascading references. New
-- columns have neutral defaults for every already committed activity entry.
ALTER TABLE activity_entries
    ADD COLUMN previous_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN observation_fingerprint text NOT NULL DEFAULT '';

-- Resolve only the three schema-23 constraints whose whitelists are extended.
-- Names may have changed; an absent or ambiguous referenced-column set must
-- reject this entire migration. The terminal-state CHECK is not replaced.
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
            RAISE EXCEPTION 'Root binding activity constraint discovery found an unexpected catalog';
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
        'restore.rollback_requested','restore.cancel_requested','restore.failed',
        'library.root_binding.updated')),
    ADD CONSTRAINT activity_entries_resource_kind_check CHECK (resource_kind IN (
        'user','session','application_key','device','library','scan','item','settings','task','task_run',
        'backup','restore','library_root')),
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
        OR (action = 'library.root_binding.updated' AND resource_kind = 'library_root')
    ),
    ADD CONSTRAINT activity_entries_root_binding_fact_check CHECK (
        (action = 'library.root_binding.updated' AND source = 'native' AND actor_kind = 'user'
            AND previous_revision >= 1 AND previous_revision < 9223372036854775807
            AND revision::numeric = previous_revision::numeric + 1
            AND octet_length(observation_fingerprint) = 64
            AND observation_fingerprint COLLATE "C" ~ '^[0-9a-f]{64}$'
            AND affected_count = 0 AND state = '' AND cardinality(changed_fields) = 0)
        OR (action <> 'library.root_binding.updated' AND previous_revision = 0 AND observation_fingerprint = '')
    );
