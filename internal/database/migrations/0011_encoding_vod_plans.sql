-- VOD producers retain bounded source-time cut points in their immutable plan.
-- Preserve the published schema history and replace only the JSON size limit.
ALTER TABLE encoding_jobs
    DROP CONSTRAINT encoding_jobs_plan_check,
    ADD CONSTRAINT encoding_jobs_plan_check
        CHECK (jsonb_typeof(plan) = 'object' AND octet_length(plan::text) <= 131072);
