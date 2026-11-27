-- Full-stream evidence is separate from the probe document and its source
-- revision. NULL evidence is pending or deferred; a terminal result has no retry.
CREATE TABLE dolby_vision_verifications (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    stream_index integer NOT NULL CHECK (stream_index >= 0),
    scan_job_id text NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
    source_revision text NOT NULL CHECK (octet_length(source_revision) BETWEEN 1 AND 128),
    verifier_version integer NOT NULL CHECK (verifier_version > 0),
    tool_identity text NOT NULL CHECK (octet_length(tool_identity) <= 512),
    evidence jsonb,
    attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    retry_at timestamptz NOT NULL DEFAULT '-infinity',
    verified_at timestamptz,
    PRIMARY KEY (item_id,stream_index),
    CONSTRAINT dolby_vision_verifications_evidence_check CHECK (CASE
        WHEN evidence IS NULL THEN true
        WHEN jsonb_typeof(evidence) <> 'object' THEN false
        ELSE octet_length(evidence::text) <= 2048
            AND evidence - ARRAY['StreamIndex','Verified','Profile','ResidualMixed','ResidualDisabled',
                'FrameCount','InBandParameterSets','Reason'] = '{}'::jsonb
            AND (jsonb_typeof(evidence->'StreamIndex') = 'number'
                AND evidence->>'StreamIndex' = stream_index::text) IS TRUE
            AND (NOT evidence ? 'Verified' OR jsonb_typeof(evidence->'Verified') = 'boolean')
            AND (NOT evidence ? 'Profile' OR (jsonb_typeof(evidence->'Profile') = 'number'
                AND evidence->>'Profile' IN ('0','4','5','7','8')))
            AND (NOT evidence ? 'ResidualMixed' OR jsonb_typeof(evidence->'ResidualMixed') = 'boolean')
            AND (NOT evidence ? 'ResidualDisabled' OR jsonb_typeof(evidence->'ResidualDisabled') = 'boolean')
            AND (NOT evidence ? 'FrameCount' OR (jsonb_typeof(evidence->'FrameCount') = 'number'
                AND evidence->>'FrameCount' ~ '^(0|[1-9][0-9]{0,18})$'
                AND evidence->'FrameCount' <= to_jsonb(9223372036854775807::bigint)))
            AND (NOT evidence ? 'InBandParameterSets' OR jsonb_typeof(evidence->'InBandParameterSets') = 'boolean')
            AND (NOT evidence ? 'Reason' OR (jsonb_typeof(evidence->'Reason') = 'string'
                AND evidence->>'Reason' IN ('','unsupported_configuration','scan_output_limit','scan_timeout',
                    'scan_failed','decoder_error','invalid_scan','no_frames','rpu_metadata_missing',
                    'residual_flag_unknown','residual_enabled','residual_mixed','rpu_profile_mixed')))
            AND (NOT evidence @> '{"Verified":true}'::jsonb OR
                (evidence->'FrameCount' > '0'::jsonb) IS TRUE)
        END),
    CONSTRAINT dolby_vision_verifications_state_check CHECK (
        (evidence IS NULL) = (retry_at < 'infinity')
        AND (verified_at IS NOT NULL) = COALESCE(evidence @> '{"Verified":true}'::jsonb, false)
        AND (verified_at IS NULL OR isfinite(verified_at))
    )
);
CREATE INDEX dolby_vision_verifications_scan_idx ON dolby_vision_verifications(scan_job_id,item_id,stream_index);
CREATE INDEX dolby_vision_verifications_due_idx ON dolby_vision_verifications(retry_at,item_id,stream_index)
    WHERE evidence IS NULL;

ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check CHECK
    (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested',
        'BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested',
        'SubtitleTimelineGenerationRequested','OnlineImagesRequested','LocalMetadataIngestRequested',
        'OnlineMetadataIngestRequested','VideoSeekIngestRequested','DolbyVisionVerificationRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check CHECK
    (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested',
        'BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested',
        'SubtitleTimelineGenerationRequested','OnlineImagesRequested','LocalMetadataIngestRequested',
        'OnlineMetadataIngestRequested','VideoSeekIngestRequested','DolbyVisionVerificationRequested'));
INSERT INTO task_system_events(name) VALUES ('DolbyVisionVerificationRequested');
