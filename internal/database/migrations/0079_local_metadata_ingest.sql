-- Each item retains at most one pending import. A new accepted scan visit
-- replaces the old request; a refresh never creates or reparents catalog items.
CREATE TABLE local_metadata_ingest (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    scan_job_id text NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
    directory_identity text NOT NULL CHECK (octet_length(directory_identity) BETWEEN 1 AND 128),
    source_key text NOT NULL CHECK (octet_length(source_key) BETWEEN 1 AND 32768),
    index_number integer NOT NULL CHECK (index_number >= 0),
    parent_index_number integer NOT NULL CHECK (parent_index_number >= 0),
    parent_number_defined boolean NOT NULL,
    import_nfo boolean NOT NULL,
    import_images boolean NOT NULL,
    enrich_online boolean NOT NULL,
    CHECK (import_nfo OR import_images OR enrich_online)
);
CREATE INDEX local_metadata_ingest_scan_idx ON local_metadata_ingest(scan_job_id,item_id);

-- Keep optional seek evidence outside the accepted media document and its
-- playback source revision. Rows are either pending, deferred, or ready.
CREATE TABLE video_seek_ingest (
    item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    scan_job_id text NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
    source_revision text NOT NULL CHECK (octet_length(source_revision) BETWEEN 1 AND 128),
    index_version integer NOT NULL CHECK (index_version > 0),
    retry_at timestamptz NOT NULL DEFAULT '-infinity',
    indexes jsonb CHECK (jsonb_typeof(indexes)='array' AND octet_length(indexes::text)<=2097152),
    CHECK (indexes IS NULL OR retry_at='infinity')
);
CREATE INDEX video_seek_ingest_scan_idx ON video_seek_ingest(scan_job_id,item_id);
CREATE INDEX video_seek_ingest_due_idx ON video_seek_ingest(retry_at,item_id) WHERE indexes IS NULL;

ALTER TABLE task_system_events DROP CONSTRAINT task_system_events_name_check;
ALTER TABLE task_system_events ADD CONSTRAINT task_system_events_name_check CHECK
 (name IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested','SubtitleTimelineGenerationRequested','OnlineImagesRequested','LocalMetadataIngestRequested','OnlineMetadataIngestRequested','VideoSeekIngestRequested'));
ALTER TABLE task_triggers DROP CONSTRAINT task_triggers_system_event_check;
ALTER TABLE task_triggers ADD CONSTRAINT task_triggers_system_event_check CHECK
 (system_event IN ('ServerStarted','LibraryChanged','ConfigurationChanged','IntroAnalysisRequested','PreviewGenerationRequested','BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','CreditsAnalysisRequested','SubtitleTimelineGenerationRequested','OnlineImagesRequested','LocalMetadataIngestRequested','OnlineMetadataIngestRequested','VideoSeekIngestRequested'));
INSERT INTO task_system_events(name) VALUES ('LocalMetadataIngestRequested'),('OnlineMetadataIngestRequested'),('VideoSeekIngestRequested');
