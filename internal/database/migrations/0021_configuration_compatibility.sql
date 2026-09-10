-- Preserve schema-20 values, revisions, and timestamps while making configured
-- names distinct from the public fallback selected by a process at startup.
ALTER TABLE managed_settings
    ADD COLUMN server_name_mode text,
    ADD COLUMN compatibility_max_width integer NOT NULL DEFAULT 0
        CHECK (compatibility_max_width BETWEEN 0 AND 8192);

UPDATE managed_settings SET server_name_mode =
    CASE WHEN server_name IS NULL THEN 'deployment' ELSE 'custom' END;

ALTER TABLE managed_settings
    ALTER COLUMN server_name_mode SET NOT NULL,
    ALTER COLUMN server_name_mode SET DEFAULT 'deployment',
    DROP CONSTRAINT managed_settings_server_name_check,
    ADD CONSTRAINT managed_settings_server_name_mode_check
        CHECK (server_name_mode IN ('deployment', 'custom', 'empty', 'unset')),
    ADD CONSTRAINT managed_settings_server_name_state_check CHECK (
        (server_name_mode IN ('deployment', 'unset') AND server_name IS NULL)
        OR (server_name_mode = 'empty' AND server_name IS NOT NULL AND server_name = '')
        OR (server_name_mode = 'custom' AND server_name IS NOT NULL
            AND octet_length(server_name) BETWEEN 1 AND 128)
    );
