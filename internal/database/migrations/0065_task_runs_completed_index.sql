-- Task polling selects the latest completed result independently of creation
-- order. Keep the history index for the separate created-time history API.
CREATE INDEX task_runs_completed_idx ON task_runs(task_id, finished_at DESC, id DESC)
    WHERE finished_at IS NOT NULL;
