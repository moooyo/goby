-- Automatic backups use the existing task scheduler. The policy owns only
-- encrypted configuration; task triggers remain the scheduling authority.
CREATE TABLE backup_policy (
    id integer PRIMARY KEY CHECK (id = 1),
    keep_count integer NOT NULL DEFAULT 7 CHECK (keep_count BETWEEN 1 AND 30),
    include_images boolean NOT NULL DEFAULT true,
    configured_kind text NOT NULL DEFAULT 'daily' CHECK (configured_kind IN ('daily','weekly')),
    configured_day_of_week smallint CHECK (configured_day_of_week BETWEEN 0 AND 6),
    configured_time_of_day_ticks bigint NOT NULL DEFAULT 108000000000 CHECK (configured_time_of_day_ticks >= 0 AND configured_time_of_day_ticks < 864000000000 AND configured_time_of_day_ticks % 600000000 = 0),
    configured_timezone text NOT NULL DEFAULT 'UTC' CHECK (octet_length(configured_timezone) BETWEEN 1 AND 128),
    secret_generation bigint NOT NULL DEFAULT 1 CHECK (secret_generation > 0),
    passphrase_ciphertext bytea CHECK (octet_length(passphrase_ciphertext) BETWEEN 44 AND 1056),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((configured_kind='weekly') = (configured_day_of_week IS NOT NULL))
);
INSERT INTO backup_policy(id) VALUES(1);

-- Filesystem objects are identified by their exact generated id and digest.
-- Imported and manually generated backups never enter this retention scope.
CREATE TABLE backup_policy_runs (
    run_id text PRIMARY KEY CHECK (run_id ~ '^[0-9a-f]{32}$'),
    backup_id text NOT NULL UNIQUE CHECK (backup_id ~ '^[0-9a-f]{32}$'),
    deployment_id text NOT NULL CHECK (octet_length(deployment_id) BETWEEN 1 AND 128),
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    include_images boolean NOT NULL,
    state text NOT NULL CHECK (state IN ('running','publishing','completed','failed','interrupted','deleting','deleted','restored')),
    sha256 text NOT NULL DEFAULT '' CHECK (sha256 = '' OR sha256 ~ '^[0-9a-f]{64}$'),
    error_code text NOT NULL DEFAULT '' CHECK (octet_length(error_code) <= 64),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (state NOT IN ('publishing','completed','deleting','deleted') OR sha256 <> '')
);
CREATE INDEX backup_policy_completed_idx ON backup_policy_runs(deployment_id, created_at DESC, run_id DESC) WHERE state='completed';

-- backup.finished remains the pre-publication authorization receipt. Only the
-- new published fact may drive a successful-completion notification. Preserve
-- every existing historical check and row while extending the known checks.
DO $$
DECLARE
    logical_name text;
    constraint_name text;
    matching_names text[];
    definition text;
    addition text;
BEGIN
    FOREACH logical_name IN ARRAY ARRAY[
        'activity_entries_action_check',
        'activity_entries_terminal_state_check',
        'activity_entries_action_resource_check',
        'activity_entries_root_binding_fact_check'
    ] LOOP
        constraint_name := logical_name;
        IF logical_name = 'activity_entries_terminal_state_check' THEN
            -- Schema 28 preserves the historical terminal CHECK and its name.
            -- Its exact column set excludes the later root-binding fact CHECK.
            SELECT array_agg(constraint_entry.conname::text ORDER BY constraint_entry.conname)
            INTO matching_names
            FROM pg_catalog.pg_constraint constraint_entry
            WHERE constraint_entry.conrelid = 'activity_entries'::regclass
                AND constraint_entry.contype = 'c'
                AND ARRAY(
                    SELECT attribute_entry.attname::text
                    FROM pg_catalog.pg_attribute attribute_entry
                    WHERE attribute_entry.attrelid = constraint_entry.conrelid
                        AND attribute_entry.attnum = ANY(constraint_entry.conkey)
                    ORDER BY attribute_entry.attname::text COLLATE "C"
                ) = ARRAY['action','state']::text[];
            IF cardinality(matching_names) IS DISTINCT FROM 1 THEN
                RAISE EXCEPTION 'Backup publication activity constraint discovery found an unexpected catalog';
            END IF;
            constraint_name := matching_names[1];
        END IF;
        SELECT pg_get_constraintdef(oid) INTO STRICT definition
        FROM pg_constraint
        WHERE conrelid='activity_entries'::regclass AND contype='c' AND conname=constraint_name;
        IF left(definition,6) <> 'CHECK ' OR position('backup.published' IN definition)>0 THEN
            RAISE EXCEPTION 'Unexpected activity publication constraint';
        END IF;
        addition := CASE logical_name
            WHEN 'activity_entries_terminal_state_check' THEN 'action=''backup.published'' AND state=''completed'''
            WHEN 'activity_entries_action_resource_check' THEN 'action=''backup.published'' AND resource_kind=''backup'''
            ELSE 'action=''backup.published'''
        END;
        EXECUTE format('ALTER TABLE activity_entries DROP CONSTRAINT %I',constraint_name);
        EXECUTE format('ALTER TABLE activity_entries ADD CONSTRAINT %I CHECK ((%s) OR (%s))',
            constraint_name,substring(definition FROM 7),addition);
    END LOOP;
END $$;

ALTER TABLE activity_entries ADD CONSTRAINT activity_entries_backup_publication_fact_check CHECK (
    action<>'backup.published'
    OR (source='system' AND actor_kind='system' AND actor_id='' AND actor_credential_id=''
        AND resource_kind='backup' AND state='completed' AND revision=0 AND previous_revision=0
        AND affected_count=0 AND cardinality(changed_fields)=0
        AND observation_fingerprint COLLATE "C" ~ '^[0-9a-f]{64}$')
);
CREATE UNIQUE INDEX activity_entries_backup_published_idx ON activity_entries(resource_id) WHERE action='backup.published';

-- Activity retention may remove the display row. Keep its exact publication
-- identity separately so a later recovery reconciliation cannot notify twice.
CREATE TABLE backup_publication_receipts (
    backup_id text PRIMARY KEY CHECK (backup_id ~ '^[0-9a-f]{32}$'),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    observation_fingerprint text NOT NULL CHECK (observation_fingerprint ~ '^[0-9a-f]{64}$'),
    activity_id bigint NOT NULL UNIQUE CHECK (activity_id > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
