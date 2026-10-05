-- Audio waveform bundles are permanent source-side files. The database stores
-- only opt-in policy, durable work and receipts; no waveform bytes or LRU state.
ALTER TABLE libraries ALTER COLUMN options SET DEFAULT
 '{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnableIntroDetection":false,"EnablePreviewGeneration":false,"EnableBackgroundPreviewGeneration":false,"EnableAudioWaveformGeneration":false}'::jsonb;
ALTER TABLE libraries DROP CONSTRAINT libraries_options_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_options_check CHECK (
 jsonb_typeof(options)='object'
 AND options ?& ARRAY['EnableLocalMetadata','EnableLocalImages','EnableEmbeddedArtwork']
 AND options-'EnableLocalMetadata'-'EnableLocalImages'-'EnableEmbeddedArtwork'-'EnableIntroDetection'-'EnablePreviewGeneration'-'EnableBackgroundPreviewGeneration'-'EnableAudioWaveformGeneration'='{}'::jsonb
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
);

ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check CHECK
 (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check CHECK
 (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested'));
INSERT INTO task_system_events(name) VALUES ('AudioWaveformGenerationRequested');

CREATE TABLE audio_waveform_queue (
 item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
 operation_id text NOT NULL DEFAULT md5(random()::text || clock_timestamp()::text) CHECK(operation_id ~ '^[0-9a-f]{32}$'),
 requested_revision bigint NOT NULL DEFAULT 1 CHECK(requested_revision>0),
 completed_revision bigint NOT NULL DEFAULT 0 CHECK(completed_revision>=0 AND completed_revision<=requested_revision),
 claimed_revision bigint NOT NULL DEFAULT 0 CHECK(claimed_revision>=0 AND claimed_revision<=requested_revision),
 force boolean NOT NULL DEFAULT false, manual boolean NOT NULL DEFAULT false,
 actor_user_id text NOT NULL DEFAULT '' CHECK(octet_length(actor_user_id)<=128),
 actor_session_id text NOT NULL DEFAULT '' CHECK(octet_length(actor_session_id)<=128),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','ready','failed','cancelled')),
 run_id text NOT NULL DEFAULT '', child_id text NOT NULL DEFAULT '',
 source_revision text NOT NULL DEFAULT '' CHECK(octet_length(source_revision)<=256),
 reused boolean NOT NULL DEFAULT false,
 error_code text NOT NULL DEFAULT '' CHECK(octet_length(error_code)<=128),
 requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 started_at timestamptz, finished_at timestamptz,
 CHECK(state<>'running' OR (run_id<>'' AND child_id<>'' AND claimed_revision>0)),
 CHECK((manual AND actor_user_id<>'' AND actor_session_id<>'') OR (NOT manual AND actor_user_id='' AND actor_session_id='')),
 CHECK(NOT force OR manual)
);
CREATE INDEX audio_waveform_queue_pending_idx ON audio_waveform_queue(requested_at,item_id)
 WHERE requested_revision>completed_revision;

CREATE TABLE audio_waveform_requests (
 request_id text PRIMARY KEY CHECK(octet_length(request_id) BETWEEN 1 AND 128),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 queued bigint NOT NULL CHECK(queued>=0), created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
