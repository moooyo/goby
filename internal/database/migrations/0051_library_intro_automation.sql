-- Missing intro policy in an existing three-key row means disabled. Preserve
-- those rows and their revision/xmin; new writes persist the explicit switch.
ALTER TABLE libraries DROP CONSTRAINT libraries_options_check;
ALTER TABLE libraries ALTER COLUMN options SET DEFAULT
    '{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false}'::jsonb;
ALTER TABLE libraries ADD CONSTRAINT libraries_options_check CHECK (
    jsonb_typeof(options)='object'
    AND options ?& ARRAY['EnableLocalMetadata','EnableLocalImages','EnableEmbeddedArtwork']
    AND options-'EnableLocalMetadata'-'EnableLocalImages'-'EnableEmbeddedArtwork'-'EnableIntroDetection'='{}'::jsonb
    AND jsonb_typeof(options->'EnableLocalMetadata')='boolean'
    AND jsonb_typeof(options->'EnableLocalImages')='boolean'
    AND jsonb_typeof(options->'EnableEmbeddedArtwork')='boolean'
    AND (NOT (options ? 'EnableIntroDetection') OR jsonb_typeof(options->'EnableIntroDetection')='boolean')
    AND (options->'EnableIntroDetection' IS DISTINCT FROM 'true'::jsonb OR collection_type='tvshows')
);

-- A dedicated request survives both direct and task-owned scan completion.
ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check
    CHECK (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check
    CHECK (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested'));
INSERT INTO task_system_events(name) VALUES ('IntroAnalysisRequested');

-- Automatic publication now follows the library switch. Retire the legacy
-- global opt-out without reinterpreting results captured under its old profile.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM analysis_settings WHERE id=1 AND NOT auto_publish_intros) THEN
        UPDATE analysis_settings SET auto_publish_intros=true,revision=revision+1,
            updated_at=clock_timestamp() WHERE id=1;
        UPDATE analysis_detections SET auto_published=false WHERE auto_published;
        DELETE FROM analysis_previews;
        DELETE FROM analysis_feature_cache;
    END IF;
END $$;
