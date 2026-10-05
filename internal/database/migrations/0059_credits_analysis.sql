-- Credits reuse immutable media-analysis admission and the existing task
-- manager. Their evidence never overwrites intro or administrator marker state.
ALTER TABLE libraries ALTER COLUMN options SET DEFAULT
 '{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false,"EnablePreviewGeneration":false,"EnableBackgroundPreviewGeneration":false,"EnableAudioWaveformGeneration":false,"EnableCreditsDetection":false}'::jsonb;
ALTER TABLE libraries DROP CONSTRAINT libraries_options_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_options_check CHECK (
 jsonb_typeof(options)='object'
 AND options ?& ARRAY['EnableLocalMetadata','EnableLocalImages','EnableEmbeddedArtwork']
 AND options-'EnableLocalMetadata'-'EnableLocalImages'-'EnableEmbeddedArtwork'-'EnableIntroDetection'-'EnablePreviewGeneration'-'EnableBackgroundPreviewGeneration'-'EnableAudioWaveformGeneration'-'EnableCreditsDetection'='{}'::jsonb
 AND jsonb_typeof(options->'EnableLocalMetadata')='boolean'
 AND jsonb_typeof(options->'EnableLocalImages')='boolean'
 AND jsonb_typeof(options->'EnableEmbeddedArtwork')='boolean'
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
);

ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check CHECK
 (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check CHECK
 (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested'));
INSERT INTO task_system_events(name) VALUES ('CreditsAnalysisRequested');

ALTER TABLE task_runs DROP CONSTRAINT task_runs_analysis_input_check;
ALTER TABLE task_runs ADD CONSTRAINT task_runs_analysis_input_check CHECK(
 (task_key NOT IN ('media.intro_analysis','media.preview_generation','media.credits_analysis') AND analysis_input IS NULL AND analysis_config_fingerprint='') OR
 (task_key IN ('media.intro_analysis','media.preview_generation','media.credits_analysis') AND analysis_input IS NOT NULL AND jsonb_typeof(analysis_input)='object'
 AND octet_length(analysis_input::text)<=65536 AND analysis_input-'LibraryIds'-'ItemIds'-'Force'='{}'::jsonb
 AND (NOT analysis_input ? 'LibraryIds' OR jsonb_typeof(analysis_input->'LibraryIds')='array')
 AND (NOT analysis_input ? 'ItemIds' OR jsonb_typeof(analysis_input->'ItemIds')='array')
 AND (NOT analysis_input ? 'Force' OR jsonb_typeof(analysis_input->'Force')='boolean')
 AND analysis_config_fingerprint ~ '^[0-9a-f]{64}$'));
ALTER TABLE task_runs DROP CONSTRAINT task_runs_analysis_authority_check;
ALTER TABLE task_runs ADD CONSTRAINT task_runs_analysis_authority_check CHECK(
 task_key NOT IN ('media.intro_analysis','media.preview_generation','media.credits_analysis') OR
 (source='manual' AND actor_kind='admin' AND actor_user_id<>'' AND actor_session_id<>'' AND actor_application_key_id=0 AND actor_client_session_id='') OR
 (source='compatibility' AND actor_session_id<>'' AND
  ((actor_kind='emby' AND actor_user_id<>'' AND actor_application_key_id=0 AND actor_client_session_id='') OR
   (actor_kind='application_key' AND actor_user_id='' AND actor_application_key_id>0 AND actor_client_session_id<>''))) OR
 (source IN ('schedule','startup','system_event') AND actor_kind='system' AND actor_user_id='' AND actor_session_id=''
  AND actor_application_key_id=0 AND actor_client_session_id='' AND actor_peer_ip=''));
ALTER TABLE analysis_work DROP CONSTRAINT analysis_work_task_key_check;
ALTER TABLE analysis_work ADD CONSTRAINT analysis_work_task_key_check
 CHECK(task_key IN ('media.intro_analysis','media.preview_generation','media.credits_analysis'));
CREATE OR REPLACE FUNCTION enforce_task_analysis_scope_key() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE analysis boolean;
BEGIN
 SELECT task_key IN ('media.intro_analysis','media.preview_generation','media.credits_analysis') INTO STRICT analysis FROM task_runs WHERE id=NEW.run_id;
 IF analysis <> (NEW.analysis_scope_key<>'') THEN RAISE EXCEPTION 'task analysis scope key mismatch'; END IF;
 RETURN NEW;
END $$;

CREATE TABLE analysis_credits_detections (
 item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0), source_revision text NOT NULL,
 profile_fingerprint text NOT NULL CHECK(profile_fingerprint ~ '^[0-9a-f]{64}$'),
 profile_revision bigint NOT NULL CHECK(profile_revision>0), publication_epoch bigint NOT NULL CHECK(publication_epoch>0),
 child_id text NOT NULL, cohort_revision text NOT NULL CHECK(cohort_revision ~ '^[0-9a-f]{64}$'),
 status text NOT NULL CHECK(status IN ('qualified','no_result')),
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=131072),
 start_ticks bigint, end_ticks bigint, auto_published boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((start_ticks IS NULL)=(end_ticks IS NULL)),
 CHECK((status='qualified')=(start_ticks IS NOT NULL)),
 CHECK(start_ticks IS NULL OR (start_ticks>=0 AND end_ticks>start_ticks AND end_ticks<=432000000000)),
 CHECK(NOT auto_published OR status='qualified')
);
CREATE TABLE analysis_credits_detection_sources (
 item_id text NOT NULL REFERENCES analysis_credits_detections(item_id) ON DELETE CASCADE,
 source_item_id text NOT NULL, library_id text NOT NULL, root_id text NOT NULL,
 source_revision text NOT NULL, hierarchy_revision text NOT NULL, episode_key text NOT NULL,
 content_sha256 text NOT NULL DEFAULT '' CHECK(content_sha256='' OR content_sha256 ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(item_id,source_item_id)
);
