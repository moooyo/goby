-- Definitions have stable identities; existing scan rows remain independent
-- execution history and receive no fabricated task run during migration.
CREATE TABLE task_definitions (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    key text NOT NULL UNIQUE CHECK (octet_length(key) BETWEEN 1 AND 128),
    emby_key text NOT NULL DEFAULT '' CHECK (octet_length(emby_key) <= 128),
    name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 256),
    description text NOT NULL DEFAULT '' CHECK (octet_length(description) <= 2048),
    category text NOT NULL DEFAULT '' CHECK (octet_length(category) <= 128),
    is_hidden boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    schedule_timezone text NOT NULL DEFAULT 'UTC' CHECK (octet_length(schedule_timezone) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- These are native schedule kinds. This schema does not establish which
-- trigger write forms an Emby reference accepts or how its timer behaves.
CREATE TABLE task_triggers (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    task_id text NOT NULL REFERENCES task_definitions(id) ON DELETE RESTRICT,
    schedule_revision bigint NOT NULL CHECK (schedule_revision > 0),
    position integer NOT NULL CHECK (position BETWEEN 0 AND 31),
    kind text NOT NULL CHECK (kind IN ('interval', 'daily', 'weekly', 'startup')),
    interval_ticks bigint CHECK (interval_ticks > 0 AND interval_ticks <= 92233720368547758),
    anchor_at timestamptz,
    time_of_day_ticks bigint CHECK (time_of_day_ticks >= 0 AND time_of_day_ticks < 864000000000),
    day_of_week smallint CHECK (day_of_week BETWEEN 0 AND 6),
    timezone text CHECK (octet_length(timezone) BETWEEN 1 AND 128),
    max_runtime_ticks bigint CHECK (max_runtime_ticks > 0 AND max_runtime_ticks <= 92233720368547758),
    next_fire_at timestamptz,
    last_due_at timestamptz,
    calculation_error text NOT NULL DEFAULT '' CHECK (octet_length(calculation_error) <= 128),
    retired_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT task_triggers_identity_revision_key UNIQUE (id, task_id, schedule_revision),
    CHECK (
        (kind = 'interval' AND interval_ticks IS NOT NULL AND anchor_at IS NOT NULL
            AND time_of_day_ticks IS NULL AND day_of_week IS NULL AND timezone IS NULL)
        OR (kind = 'daily' AND time_of_day_ticks IS NOT NULL AND timezone IS NOT NULL
            AND day_of_week IS NULL AND interval_ticks IS NULL AND anchor_at IS NULL)
        OR (kind = 'weekly' AND time_of_day_ticks IS NOT NULL AND timezone IS NOT NULL
            AND day_of_week IS NOT NULL AND interval_ticks IS NULL AND anchor_at IS NULL)
        OR (kind = 'startup' AND interval_ticks IS NULL AND anchor_at IS NULL
            AND time_of_day_ticks IS NULL AND day_of_week IS NULL AND timezone IS NULL)
    )
);

CREATE UNIQUE INDEX task_triggers_active_position_idx
    ON task_triggers(task_id, position) WHERE retired_at IS NULL;
CREATE INDEX task_triggers_due_idx
    ON task_triggers(next_fire_at, id) WHERE retired_at IS NULL AND next_fire_at IS NOT NULL;

CREATE TABLE task_runs (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    task_id text NOT NULL REFERENCES task_definitions(id) ON DELETE RESTRICT,
    state text NOT NULL CHECK (state IN ('pending', 'running', 'stopping', 'completed', 'failed', 'cancelled', 'interrupted')),
    source text NOT NULL CHECK (source IN ('manual', 'compatibility', 'schedule', 'startup')),
    request_id text CHECK (octet_length(request_id) BETWEEN 1 AND 128),
    request_fingerprint bytea CHECK (octet_length(request_fingerprint) = 32),
    actor_user_id text NOT NULL DEFAULT '',
    actor_session_id text NOT NULL DEFAULT '',
    actor_kind text NOT NULL DEFAULT '',
    task_key text NOT NULL,
    task_emby_key text NOT NULL DEFAULT '',
    task_name text NOT NULL,
    trigger_id text,
    trigger_revision bigint CHECK (trigger_revision > 0),
    scheduled_for timestamptz,
    max_runtime_ticks bigint CHECK (max_runtime_ticks > 0 AND max_runtime_ticks <= 92233720368547758),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    deadline_at timestamptz,
    stop_requested_at timestamptz,
    stop_reason text NOT NULL DEFAULT '' CHECK (stop_reason IN ('', 'administrator', 'max_runtime', 'shutdown')),
    finished_at timestamptz,
    error_code text NOT NULL DEFAULT '' CHECK (octet_length(error_code) <= 128),
    error_message text NOT NULL DEFAULT '' CHECK (octet_length(error_message) <= 2048),
    total_children bigint NOT NULL DEFAULT 0 CHECK (total_children >= 0),
    terminal_children bigint NOT NULL DEFAULT 0 CHECK (terminal_children >= 0),
    completed_children bigint NOT NULL DEFAULT 0 CHECK (completed_children >= 0),
    failed_children bigint NOT NULL DEFAULT 0 CHECK (failed_children >= 0),
    cancelled_children bigint NOT NULL DEFAULT 0 CHECK (cancelled_children >= 0),
    interrupted_children bigint NOT NULL DEFAULT 0 CHECK (interrupted_children >= 0),
    unavailable_children bigint NOT NULL DEFAULT 0 CHECK (unavailable_children >= 0),
    scanned bigint NOT NULL DEFAULT 0 CHECK (scanned >= 0),
    added bigint NOT NULL DEFAULT 0 CHECK (added >= 0),
    updated bigint NOT NULL DEFAULT 0 CHECK (updated >= 0),
    CONSTRAINT task_runs_trigger_scope_fkey
        FOREIGN KEY (trigger_id, task_id, trigger_revision)
        REFERENCES task_triggers(id, task_id, schedule_revision) ON DELETE RESTRICT,
    CONSTRAINT task_runs_trigger_source_check CHECK (
        (source IN ('manual', 'compatibility') AND trigger_id IS NULL
            AND trigger_revision IS NULL AND scheduled_for IS NULL)
        OR (source IN ('schedule', 'startup') AND trigger_id IS NOT NULL
            AND trigger_revision IS NOT NULL AND scheduled_for IS NOT NULL)
    ),
    CHECK ((request_id IS NULL) = (request_fingerprint IS NULL)),
    CHECK ((state IN ('pending', 'running', 'stopping')) = (finished_at IS NULL)),
    CHECK (state <> 'running' OR started_at IS NOT NULL),
    CHECK (state <> 'stopping' OR stop_requested_at IS NOT NULL),
    CHECK ((stop_requested_at IS NULL) = (stop_reason = '')),
    CHECK (deadline_at IS NULL OR started_at IS NOT NULL),
    CHECK (terminal_children <= total_children),
    CHECK (terminal_children = completed_children + failed_children + cancelled_children + interrupted_children + unavailable_children),
    UNIQUE (id, task_id)
);

CREATE UNIQUE INDEX task_runs_one_active_idx
    ON task_runs(task_id) WHERE state IN ('pending', 'running', 'stopping');
CREATE UNIQUE INDEX task_runs_request_id_idx
    ON task_runs(task_id, request_id) WHERE request_id IS NOT NULL;
CREATE INDEX task_runs_history_idx ON task_runs(task_id, created_at DESC, id DESC);
CREATE INDEX task_runs_active_idx ON task_runs(created_at, id)
    WHERE state IN ('pending', 'running', 'stopping');

-- Different manual requests can coalesce into one active run. Every accepted
-- request keeps its own receipt so a retry after completion cannot start again.
CREATE TABLE task_run_requests (
    task_id text NOT NULL REFERENCES task_definitions(id) ON DELETE RESTRICT,
    request_id text NOT NULL CHECK (octet_length(request_id) BETWEEN 1 AND 128),
    run_id text NOT NULL,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (task_id, request_id),
    FOREIGN KEY (run_id, task_id) REFERENCES task_runs(id, task_id) ON DELETE CASCADE
);
CREATE INDEX task_run_requests_run_idx ON task_run_requests(run_id);

CREATE TABLE task_run_children (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    run_id text NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
    library_id text NOT NULL,
    library_name text NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    state text NOT NULL DEFAULT 'waiting'
        CHECK (state IN ('waiting', 'queued', 'running', 'completed', 'failed', 'cancelled', 'unavailable', 'interrupted')),
    scan_job_id text,
    scanned bigint NOT NULL DEFAULT 0 CHECK (scanned >= 0),
    added bigint NOT NULL DEFAULT 0 CHECK (added >= 0),
    updated bigint NOT NULL DEFAULT 0 CHECK (updated >= 0),
    error_code text NOT NULL DEFAULT '' CHECK (octet_length(error_code) <= 128),
    error_message text NOT NULL DEFAULT '' CHECK (octet_length(error_message) <= 2048),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    finished_at timestamptz,
    UNIQUE (run_id, library_id),
    UNIQUE (run_id, ordinal),
    CHECK ((state IN ('waiting', 'queued', 'running')) = (finished_at IS NULL)),
    CHECK (state NOT IN ('queued', 'running') OR scan_job_id IS NOT NULL),
    CHECK (state <> 'running' OR started_at IS NOT NULL),
    CHECK (state <> 'waiting' OR (scan_job_id IS NULL AND started_at IS NULL))
);

CREATE UNIQUE INDEX task_run_children_scan_job_idx
    ON task_run_children(scan_job_id) WHERE scan_job_id IS NOT NULL;
CREATE INDEX task_run_children_dispatch_idx ON task_run_children(run_id, ordinal, id)
    WHERE state IN ('waiting', 'queued', 'running');

CREATE TABLE task_occurrences (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    task_id text NOT NULL REFERENCES task_definitions(id) ON DELETE RESTRICT,
    trigger_id text NOT NULL,
    schedule_revision bigint NOT NULL CHECK (schedule_revision > 0),
    due_at timestamptz NOT NULL,
    last_due_at timestamptz,
    occurrence_count bigint NOT NULL DEFAULT 1 CHECK (occurrence_count > 0),
    disposition text NOT NULL CHECK (disposition IN ('admitted', 'overlap', 'missed')),
    run_id text,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT task_occurrences_trigger_scope_fkey
        FOREIGN KEY (trigger_id, task_id, schedule_revision)
        REFERENCES task_triggers(id, task_id, schedule_revision) ON DELETE RESTRICT,
    CONSTRAINT task_occurrences_run_scope_fkey
        FOREIGN KEY (run_id, task_id) REFERENCES task_runs(id, task_id) ON DELETE RESTRICT,
    UNIQUE (trigger_id, schedule_revision, due_at),
    CHECK (last_due_at IS NULL OR last_due_at >= due_at),
    CHECK (disposition = 'missed' OR (run_id IS NOT NULL AND occurrence_count = 1)),
    CHECK (disposition <> 'missed' OR run_id IS NULL)
);

ALTER TABLE scan_jobs ADD COLUMN task_child_id text;
ALTER TABLE scan_jobs ADD CONSTRAINT scan_jobs_task_child_id_fkey
    FOREIGN KEY (task_child_id) REFERENCES task_run_children(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX scan_jobs_task_child_id_idx ON scan_jobs(task_child_id)
    WHERE task_child_id IS NOT NULL;
