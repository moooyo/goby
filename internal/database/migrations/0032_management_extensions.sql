-- Generic task workers retain the existing child history and aggregate model.
-- A durable claim token replaces a scan reference only for non-scan children.
ALTER TABLE task_run_children ADD COLUMN executor_token text
    CHECK (executor_token IS NULL OR executor_token ~ '^[0-9a-f]{32}$');

DO $$
DECLARE constraint_name text;
BEGIN
    SELECT conname INTO STRICT constraint_name FROM pg_constraint
    WHERE conrelid = 'task_run_children'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%scan_job_id IS NOT NULL%'
      AND pg_get_constraintdef(oid) LIKE '%queued%';
    EXECUTE format('ALTER TABLE task_run_children DROP CONSTRAINT %I', constraint_name);
END $$;

ALTER TABLE task_run_children ADD CONSTRAINT task_children_execution_reference
    CHECK (state NOT IN ('queued', 'running') OR scan_job_id IS NOT NULL OR executor_token IS NOT NULL);
ALTER TABLE task_run_children ADD CONSTRAINT task_children_execution_kind
    CHECK (executor_token IS NULL OR (scan_job_id IS NULL AND state <> 'waiting'));

-- Settings remain an explicit closed typed object; no compatibility passthrough.
ALTER TABLE managed_settings ADD COLUMN management jsonb NOT NULL DEFAULT
    '{"Metadata":{"EnableInternetProviders":false,"PreferredMetadataLanguage":"en","MetadataCountryCode":"US"},"Subtitles":{"DownloadLanguages":["en"],"DownloadMovieSubtitles":true,"DownloadEpisodeSubtitles":true},"Tasks":{"MaxConcurrent":2,"CacheRetentionDays":30,"CacheMaxEntries":10000}}'::jsonb
    CHECK (jsonb_typeof(management) = 'object');

-- Extend the existing closed activity field allowlist without copying private
-- management values into the audit history.
DO $$
DECLARE constraint_name text;
        constraint_definition text;
BEGIN
    SELECT conname, pg_get_constraintdef(oid) INTO STRICT constraint_name, constraint_definition
    FROM pg_constraint WHERE conrelid = 'activity_entries'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%TranscodingMaxWidth%'
      AND pg_get_constraintdef(oid) LIKE '%changed_fields%';
    constraint_definition := replace(constraint_definition,
        '''TranscodingMaxWidth''::text', '''TranscodingMaxWidth''::text, ''Management''::text');
    IF constraint_definition NOT LIKE '%Management%' THEN
        RAISE EXCEPTION 'settings activity constraint cannot be extended';
    END IF;
    EXECUTE format('ALTER TABLE activity_entries DROP CONSTRAINT %I', constraint_name);
    EXECUTE format('ALTER TABLE activity_entries ADD CONSTRAINT %I %s', constraint_name, constraint_definition);
END $$;
