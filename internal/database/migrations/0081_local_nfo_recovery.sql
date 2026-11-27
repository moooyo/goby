-- Retain at most one scan-bound recovery document and one work row per item.
CREATE TABLE local_nfo_recovery_sources (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    scan_job_id text NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
    source_key text NOT NULL,
    directory_identity text NOT NULL,
    root_identity text NOT NULL,
    relative_path text NOT NULL,
    media_change_ns bigint NOT NULL CHECK (media_change_ns >= 0),
    document bytea NOT NULL CHECK (octet_length(document) BETWEEN 1 AND 2097152),
    document_hash text NOT NULL CHECK (document_hash COLLATE "C" ~ '^[0-9a-f]{64}$')
);
CREATE INDEX local_nfo_recovery_sources_scan_idx ON local_nfo_recovery_sources(scan_job_id,item_id);

CREATE TABLE local_nfo_recovery_work (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    source_key text NOT NULL,
    directory_identity text NOT NULL,
    root_identity text NOT NULL,
    relative_path text NOT NULL,
    media_change_ns bigint NOT NULL CHECK (media_change_ns >= 0),
    version text NOT NULL CHECK (octet_length(version) <= 512),
    reason text NOT NULL CHECK (reason IN ('empty','malformed')),
    confirmed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    retry_at timestamptz NOT NULL DEFAULT '-infinity',
    status text NOT NULL DEFAULT 'pending' CHECK
        (status IN ('pending','no_source','write_disabled','retry_exhausted','awaiting_import','verify_outcome')),
    -- After reserving the final write, only read-only outcome verification can be due.
    CONSTRAINT local_nfo_recovery_work_state_check CHECK (
        (status IN ('pending','verify_outcome')) = (retry_at < 'infinity')
        AND (status <> 'pending' OR attempts < 3)
        AND (status <> 'verify_outcome' OR attempts = 3)
    )
);
CREATE INDEX local_nfo_recovery_work_due_idx ON local_nfo_recovery_work(retry_at,item_id)
    WHERE status IN ('pending','verify_outcome');

ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check CHECK
    (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested',
        'BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested',
        'SubtitleTimelineGenerationRequested','OnlineImagesRequested','LocalMetadataIngestRequested',
        'OnlineMetadataIngestRequested','VideoSeekIngestRequested','DolbyVisionVerificationRequested','NFORecoveryRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check CHECK
    (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested',
        'BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested',
        'SubtitleTimelineGenerationRequested','OnlineImagesRequested','LocalMetadataIngestRequested',
        'OnlineMetadataIngestRequested','VideoSeekIngestRequested','DolbyVisionVerificationRequested','NFORecoveryRequested'));
INSERT INTO task_system_events(name) VALUES ('NFORecoveryRequested');
