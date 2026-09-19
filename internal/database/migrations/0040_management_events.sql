-- Three coalescing signals survive process restart without retaining source
-- payloads. Task receipts retain the exact consumed sequence ranges.
CREATE TABLE task_system_events (
    name text PRIMARY KEY CHECK (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged')),
    sequence bigint NOT NULL DEFAULT 0 CHECK (sequence >= 0),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lifecycle_key text NOT NULL DEFAULT '' CHECK (octet_length(lifecycle_key) <= 128),
    CHECK (name = 'ServerStarted' OR lifecycle_key = '')
);
INSERT INTO task_system_events(name) VALUES ('ServerStarted'),('LibraryChanged'),('ConfigurationChanged');

ALTER TABLE task_triggers ADD COLUMN system_event text
    CHECK (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged'));
ALTER TABLE task_triggers ADD COLUMN last_event_sequence bigint NOT NULL DEFAULT 0 CHECK (last_event_sequence >= 0);
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_kind_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_kind_check
    CHECK (kind IN ('interval','daily','weekly','startup','system_event'));

DO $$
DECLARE constraint_name text;
BEGIN
    SELECT conname INTO STRICT constraint_name FROM pg_constraint
    WHERE conrelid = 'task_triggers'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%kind%'
      AND pg_get_constraintdef(oid) LIKE '%anchor_at IS NOT NULL%';
    EXECUTE format('ALTER TABLE task_triggers DROP CONSTRAINT %I', constraint_name);
END $$;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_shape_check CHECK (
    (kind='interval' AND interval_ticks IS NOT NULL AND anchor_at IS NOT NULL
        AND time_of_day_ticks IS NULL AND day_of_week IS NULL AND timezone IS NULL AND system_event IS NULL)
    OR (kind='daily' AND time_of_day_ticks IS NOT NULL AND timezone IS NOT NULL
        AND day_of_week IS NULL AND interval_ticks IS NULL AND anchor_at IS NULL AND system_event IS NULL)
    OR (kind='weekly' AND time_of_day_ticks IS NOT NULL AND timezone IS NOT NULL
        AND day_of_week IS NOT NULL AND interval_ticks IS NULL AND anchor_at IS NULL AND system_event IS NULL)
    OR (kind='startup' AND interval_ticks IS NULL AND anchor_at IS NULL
        AND time_of_day_ticks IS NULL AND day_of_week IS NULL AND timezone IS NULL AND system_event IS NULL)
    OR (kind='system_event' AND system_event IS NOT NULL AND next_fire_at IS NULL
        AND interval_ticks IS NULL AND anchor_at IS NULL AND time_of_day_ticks IS NULL
        AND day_of_week IS NULL AND timezone IS NULL)
);

ALTER TABLE task_runs DROP CONSTRAINT task_runs_source_check;
ALTER TABLE task_runs ADD CONSTRAINT task_runs_source_check
    CHECK (source IN ('manual','compatibility','schedule','startup','system_event'));
ALTER TABLE task_runs DROP CONSTRAINT task_runs_trigger_source_check;
ALTER TABLE task_runs ADD CONSTRAINT task_runs_trigger_source_check CHECK (
    (source IN ('manual','compatibility') AND trigger_id IS NULL AND trigger_revision IS NULL AND scheduled_for IS NULL)
    OR (source IN ('schedule','startup','system_event') AND trigger_id IS NOT NULL AND trigger_revision IS NOT NULL AND scheduled_for IS NOT NULL)
);

CREATE TABLE task_system_event_receipts (
    trigger_id text NOT NULL,
    task_id text NOT NULL,
    schedule_revision bigint NOT NULL CHECK (schedule_revision > 0),
    system_event text NOT NULL REFERENCES task_system_events(name) ON DELETE RESTRICT,
    first_sequence bigint NOT NULL CHECK (first_sequence > 0),
    last_sequence bigint NOT NULL CHECK (last_sequence >= first_sequence),
    occurred_at timestamptz NOT NULL,
    run_id text NOT NULL,
    disposition text NOT NULL CHECK (disposition IN ('admitted','overlap')),
    PRIMARY KEY (trigger_id, schedule_revision, last_sequence),
    FOREIGN KEY (trigger_id,task_id,schedule_revision)
        REFERENCES task_triggers(id,task_id,schedule_revision) ON DELETE RESTRICT,
    FOREIGN KEY (run_id,task_id) REFERENCES task_runs(id,task_id) ON DELETE RESTRICT
);
