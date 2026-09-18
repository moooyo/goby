-- Dynamic streams have their own elapsed clock. Their session reports must not
-- be clipped to, or mark completion of, the catalog item's local-file duration.
ALTER TABLE play_sessions ADD COLUMN is_dynamic boolean NOT NULL DEFAULT false;

DO $$
DECLARE constraint_name text;
BEGIN
    SELECT conname INTO STRICT constraint_name FROM pg_constraint
    WHERE conrelid = 'play_sessions'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%position_ticks <= duration_ticks%';
    EXECUTE format('ALTER TABLE play_sessions DROP CONSTRAINT %I', constraint_name);
END $$;

ALTER TABLE play_sessions ADD CONSTRAINT play_sessions_position_range
    CHECK (is_dynamic OR position_ticks <= duration_ticks);
