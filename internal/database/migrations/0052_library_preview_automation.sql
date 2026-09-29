-- Existing libraries keep their exact options and revision. An absent preview
-- generation switch means disabled; future library writes persist it explicitly.
ALTER TABLE libraries DROP CONSTRAINT libraries_options_check;
ALTER TABLE libraries ALTER COLUMN options SET DEFAULT
    '{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false,"EnablePreviewGeneration":false}'::jsonb;
ALTER TABLE libraries ADD CONSTRAINT libraries_options_check CHECK (
    jsonb_typeof(options)='object'
    AND options ?& ARRAY['EnableLocalMetadata','EnableLocalImages','EnableEmbeddedArtwork']
    AND options-'EnableLocalMetadata'-'EnableLocalImages'-'EnableEmbeddedArtwork'-'EnableIntroDetection'-'EnablePreviewGeneration'='{}'::jsonb
    AND jsonb_typeof(options->'EnableLocalMetadata')='boolean'
    AND jsonb_typeof(options->'EnableLocalImages')='boolean'
    AND jsonb_typeof(options->'EnableEmbeddedArtwork')='boolean'
    AND (NOT (options ? 'EnableIntroDetection') OR jsonb_typeof(options->'EnableIntroDetection')='boolean')
    AND (options->'EnableIntroDetection' IS DISTINCT FROM 'true'::jsonb OR collection_type='tvshows')
    AND (NOT (options ? 'EnablePreviewGeneration') OR jsonb_typeof(options->'EnablePreviewGeneration')='boolean')
    AND (options->'EnablePreviewGeneration' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
);

-- Preview requests do not share the intro counter or recursively reschedule scans.
ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check
    CHECK (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check
    CHECK (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested'));
INSERT INTO task_system_events(name) VALUES ('PreviewGenerationRequested');
