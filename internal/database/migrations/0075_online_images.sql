-- Existing libraries remain opted out without rewriting their revision or xmin.
ALTER TABLE libraries ALTER COLUMN options SET DEFAULT
 '{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableOnlineImages":false,"EnableIntroDetection":false,"EnablePreviewGeneration":false,"EnableBackgroundPreviewGeneration":false,"EnableAudioWaveformGeneration":false,"EnableCreditsDetection":false,"EnableSubtitleTimelineGeneration":false}'::jsonb;
ALTER TABLE libraries DROP CONSTRAINT libraries_options_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_options_check CHECK (
 jsonb_typeof(options)='object'
 AND options ?& ARRAY['EnableLocalMetadata','EnableLocalImages','EnableEmbeddedArtwork']
 AND options-'EnableLocalMetadata'-'EnableLocalImages'-'EnableEmbeddedArtwork'-'EnableOnlineImages'-'EnableIntroDetection'-'EnablePreviewGeneration'-'EnableBackgroundPreviewGeneration'-'EnableAudioWaveformGeneration'-'EnableCreditsDetection'-'EnableSubtitleTimelineGeneration'='{}'::jsonb
 AND jsonb_typeof(options->'EnableLocalMetadata')='boolean'
 AND jsonb_typeof(options->'EnableLocalImages')='boolean'
 AND jsonb_typeof(options->'EnableEmbeddedArtwork')='boolean'
 AND (NOT (options ? 'EnableOnlineImages') OR jsonb_typeof(options->'EnableOnlineImages')='boolean')
 AND (options->'EnableOnlineImages' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
 AND (NOT (options ? 'EnableIntroDetection') OR jsonb_typeof(options->'EnableIntroDetection')='boolean')
 AND (options->'EnableIntroDetection' IS DISTINCT FROM 'true'::jsonb OR collection_type='tvshows')
 AND (NOT (options ? 'EnablePreviewGeneration') OR jsonb_typeof(options->'EnablePreviewGeneration')='boolean')
 AND (options->'EnablePreviewGeneration' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
 AND (NOT (options ? 'EnableBackgroundPreviewGeneration') OR jsonb_typeof(options->'EnableBackgroundPreviewGeneration')='boolean')
 AND (options->'EnableBackgroundPreviewGeneration' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
 AND (NOT (options ? 'EnableAudioWaveformGeneration') OR jsonb_typeof(options->'EnableAudioWaveformGeneration')='boolean')
 AND (options->'EnableAudioWaveformGeneration' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
 AND (NOT (options ? 'EnableCreditsDetection') OR jsonb_typeof(options->'EnableCreditsDetection')='boolean')
 AND (options->'EnableCreditsDetection' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
 AND (NOT (options ? 'EnableSubtitleTimelineGeneration') OR jsonb_typeof(options->'EnableSubtitleTimelineGeneration')='boolean')
 AND (options->'EnableSubtitleTimelineGeneration' IS DISTINCT FROM 'true'::jsonb OR collection_type IN ('movies','tvshows','mixed'))
);

-- This signal follows committed opt-ins and completed scans. Downloaded images
-- never emit it, so task publications cannot create a recursive scan/download loop.
ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check CHECK
 (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested','SubtitleTimelineGenerationRequested','OnlineImagesRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check CHECK
 (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested','SubtitleTimelineGenerationRequested','OnlineImagesRequested'));
INSERT INTO task_system_events(name) VALUES ('OnlineImagesRequested');
