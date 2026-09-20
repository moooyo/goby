-- Media operation history and OCR review survive owner removal. Only live
-- publication journals reserve their original catalog resources.
CREATE TABLE media_operations (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    kind text NOT NULL CHECK (kind IN ('remove_embedded_subtitle', 'subtitle_ocr')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    item_id text REFERENCES items(id) ON DELETE SET NULL,
    library_id text REFERENCES libraries(id) ON DELETE SET NULL,
    root_id text REFERENCES library_roots(id) ON DELETE SET NULL,
    source_item_id text NOT NULL CHECK (octet_length(source_item_id) BETWEEN 1 AND 128),
    source_library_id text NOT NULL CHECK (octet_length(source_library_id) BETWEEN 1 AND 128),
    source_root_id text NOT NULL CHECK (octet_length(source_root_id) BETWEEN 1 AND 128),
    request_actor_id text NOT NULL CHECK (octet_length(request_actor_id) BETWEEN 1 AND 128),
    request_credential_id text NOT NULL CHECK (octet_length(request_credential_id) BETWEEN 1 AND 128),
    request_id text NOT NULL CHECK (octet_length(request_id) BETWEEN 1 AND 128),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    media_source_id text NOT NULL CHECK (octet_length(media_source_id) BETWEEN 1 AND 256),
    source_revision text NOT NULL CHECK (octet_length(source_revision) BETWEEN 1 AND 256),
    stream_index integer NOT NULL CHECK (stream_index BETWEEN 0 AND 4095),
    parameters jsonb NOT NULL CHECK (jsonb_typeof(parameters) = 'object' AND octet_length(parameters::text) <= 16384),
    source_snapshot jsonb NOT NULL CHECK (jsonb_typeof(source_snapshot) = 'object' AND octet_length(source_snapshot::text) <= 262144),
    execution_snapshot jsonb NOT NULL CHECK (jsonb_typeof(execution_snapshot) = 'object' AND octet_length(execution_snapshot::text) <= 262144),
    state text NOT NULL CHECK (state IN ('queued', 'running', 'ready', 'applying', 'completed', 'failed', 'cancelled', 'interrupted', 'stale', 'recovery_required')),
    worker_token text NOT NULL DEFAULT '' CHECK (worker_token = '' OR worker_token ~ '^[0-9a-f]{32}$'),
    progress_stage text NOT NULL DEFAULT '' CHECK (octet_length(progress_stage) <= 64),
    processed bigint NOT NULL DEFAULT 0 CHECK (processed >= 0),
    total bigint NOT NULL DEFAULT 0 CHECK (total >= 0),
    cancel_requested_at timestamptz,
    result_summary jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(result_summary) = 'object' AND octet_length(result_summary::text) <= 262144),
    result_hash text NOT NULL DEFAULT '' CHECK (result_hash = '' OR result_hash ~ '^[0-9a-f]{64}$'),
    error_code text NOT NULL DEFAULT '' CHECK (octet_length(error_code) <= 128),
    error_message text NOT NULL DEFAULT '' CHECK (octet_length(error_message) <= 2048),
    publication_phase text NOT NULL DEFAULT 'none' CHECK (publication_phase IN ('none', 'prepared', 'catalog_committed', 'done')),
    journal jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(journal) = 'object' AND octet_length(journal::text) <= 262144),
    apply_actor_id text NOT NULL DEFAULT '' CHECK (octet_length(apply_actor_id) <= 128),
    apply_credential_id text NOT NULL DEFAULT '' CHECK (octet_length(apply_credential_id) <= 128),
    apply_request_id text NOT NULL DEFAULT '' CHECK (octet_length(apply_request_id) <= 128),
    apply_fingerprint bytea CHECK (octet_length(apply_fingerprint) = 32),
    apply_revision bigint CHECK (apply_revision > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    finished_at timestamptz,
    UNIQUE (request_actor_id, request_id)
);

CREATE UNIQUE INDEX media_operations_one_active_source_idx ON media_operations(source_item_id)
    WHERE state IN ('queued', 'running', 'applying') OR publication_phase IN ('prepared', 'catalog_committed');
CREATE INDEX media_operations_history_idx ON media_operations(created_at DESC, id DESC);
CREATE INDEX media_operations_source_history_idx ON media_operations(source_item_id, created_at DESC, id DESC);
CREATE INDEX media_operations_runnable_idx ON media_operations(created_at, id) WHERE state = 'queued';
CREATE INDEX media_operations_publication_library_idx ON media_operations(source_library_id)
    WHERE publication_phase IN ('prepared', 'catalog_committed');
CREATE INDEX media_operations_publication_root_idx ON media_operations(source_root_id)
    WHERE publication_phase IN ('prepared', 'catalog_committed');

CREATE TABLE media_operation_cues (
    operation_id text NOT NULL REFERENCES media_operations(id) ON DELETE CASCADE,
    ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 49999),
    original_start_ticks bigint NOT NULL CHECK (original_start_ticks >= 0),
    original_end_ticks bigint NOT NULL CHECK (original_end_ticks > original_start_ticks),
    original_text text NOT NULL CHECK (octet_length(original_text) <= 262144),
    start_ticks bigint NOT NULL CHECK (start_ticks >= 0),
    end_ticks bigint NOT NULL CHECK (end_ticks > start_ticks),
    text text NOT NULL CHECK (octet_length(text) <= 262144),
    included boolean NOT NULL DEFAULT true,
    confidence double precision CHECK (confidence BETWEEN 0 AND 100 AND confidence <> 'NaN'::double precision),
    warnings jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(warnings) = 'array' AND octet_length(warnings::text) <= 16384),
    image_sha256 text NOT NULL DEFAULT '' CHECK (image_sha256 = '' OR image_sha256 ~ '^[0-9a-f]{64}$'),
    image_png bytea NOT NULL DEFAULT ''::bytea CHECK (octet_length(image_png) <= 1048576),
    is_forced boolean NOT NULL DEFAULT false,
    is_hearing_impaired boolean NOT NULL DEFAULT false,
    PRIMARY KEY (operation_id, ordinal)
);

-- Published OCR subtitles own their bytes independently from temporary workers.
CREATE TABLE item_owned_subtitles (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
    stream_index integer NOT NULL CHECK (stream_index BETWEEN 0 AND 2147483647),
    operation_id text UNIQUE REFERENCES media_operations(id) ON DELETE SET NULL,
    source_revision text NOT NULL CHECK (octet_length(source_revision) <= 256),
    active boolean NOT NULL DEFAULT true,
    codec text NOT NULL CHECK (codec IN ('srt', 'vtt')),
    language text NOT NULL DEFAULT '' CHECK (octet_length(language) <= 128),
    title text NOT NULL DEFAULT '' CHECK (octet_length(title) <= 1024),
    is_default boolean NOT NULL DEFAULT false,
    is_forced boolean NOT NULL DEFAULT false,
    is_hearing_impaired boolean NOT NULL DEFAULT false,
    content bytea NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 8388608),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    retired_at timestamptz,
    PRIMARY KEY (item_id, stream_index)
);
CREATE INDEX item_owned_subtitles_root_idx ON item_owned_subtitles(root_id);

-- Immutable request evidence is separate from nullable live foreign keys, so
-- deleting historical owners can still detach retained review and audit data.
CREATE FUNCTION preserve_media_operation_request() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id, NEW.kind, NEW.source_item_id, NEW.source_library_id, NEW.source_root_id,
           NEW.media_source_id, NEW.source_revision, NEW.stream_index,
           NEW.parameters, NEW.source_snapshot, NEW.execution_snapshot,
           NEW.request_actor_id, NEW.request_credential_id, NEW.request_id, NEW.request_fingerprint)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.kind, OLD.source_item_id, OLD.source_library_id, OLD.source_root_id,
           OLD.media_source_id, OLD.source_revision, OLD.stream_index,
           OLD.parameters, OLD.source_snapshot, OLD.execution_snapshot,
           OLD.request_actor_id, OLD.request_credential_id, OLD.request_id, OLD.request_fingerprint) THEN
        RAISE EXCEPTION 'media operation request evidence is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER media_operations_immutable_request
BEFORE UPDATE ON media_operations
FOR EACH ROW EXECUTE FUNCTION preserve_media_operation_request();

-- Repository admission also checks authority and exact source snapshots. This
-- guard serializes durable publication admission against scans and deletions.
CREATE FUNCTION guard_media_operation_publication() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.publication_phase NOT IN ('prepared', 'catalog_committed') THEN
        RETURN NEW;
    END IF;
    IF NEW.item_id IS DISTINCT FROM NEW.source_item_id
       OR NEW.library_id IS DISTINCT FROM NEW.source_library_id
       OR NEW.root_id IS DISTINCT FROM NEW.source_root_id THEN
        RAISE EXCEPTION 'media publication requires its live source owners' USING ERRCODE = '55000';
    END IF;
    -- A retained reservation may advance even after the catalog commit. It
    -- must not be admitted again or conflict with its own publication journal.
    IF TG_OP = 'UPDATE' AND OLD.publication_phase IN ('prepared', 'catalog_committed') THEN
        RETURN NEW;
    END IF;
    PERFORM id FROM libraries WHERE id = NEW.source_library_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'media publication library is unavailable' USING ERRCODE = '55000';
    END IF;
    PERFORM id FROM library_roots
        WHERE id = NEW.source_root_id AND library_id = NEW.source_library_id FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'media publication root is unavailable' USING ERRCODE = '55000';
    END IF;
    PERFORM id FROM items
        WHERE id = NEW.source_item_id AND library_id = NEW.source_library_id
          AND root_id = NEW.source_root_id FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'media publication item is unavailable' USING ERRCODE = '55000';
    END IF;
    IF EXISTS (SELECT 1 FROM scan_jobs
               WHERE library_id = NEW.source_library_id AND status IN ('Queued', 'Running')) THEN
        RAISE EXCEPTION 'a scan is active for the media publication library' USING ERRCODE = '55000';
    END IF;
    IF EXISTS (SELECT 1 FROM media_deletion_operations WHERE library_id = NEW.source_library_id) THEN
        RAISE EXCEPTION 'media deletion recovery is pending for this library' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER media_operations_publication_admission
BEFORE INSERT OR UPDATE OF publication_phase, item_id, library_id, root_id ON media_operations
FOR EACH ROW EXECUTE FUNCTION guard_media_operation_publication();

CREATE FUNCTION prevent_scans_during_media_publication() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('Queued', 'Running') THEN
        PERFORM id FROM libraries WHERE id = NEW.library_id FOR KEY SHARE;
        IF EXISTS (SELECT 1 FROM media_operations
                   WHERE source_library_id = NEW.library_id
                     AND publication_phase IN ('prepared', 'catalog_committed')) THEN
            RAISE EXCEPTION 'media publication recovery is pending for this library' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER scan_jobs_pending_media_publication
BEFORE INSERT OR UPDATE OF status, library_id ON scan_jobs
FOR EACH ROW EXECUTE FUNCTION prevent_scans_during_media_publication();

CREATE FUNCTION prevent_owner_changes_during_media_publication() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'library_roots' THEN
        IF EXISTS (SELECT 1 FROM media_operations
                   WHERE source_root_id = OLD.id
                     AND publication_phase IN ('prepared', 'catalog_committed')) THEN
            RAISE EXCEPTION 'media publication recovery is pending for this root' USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'items' THEN
        IF EXISTS (SELECT 1 FROM media_operations
                   WHERE source_item_id = OLD.id
                     AND publication_phase IN ('prepared', 'catalog_committed')) THEN
            RAISE EXCEPTION 'media publication recovery is pending for this item' USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'libraries' THEN
        IF EXISTS (SELECT 1 FROM media_operations
                   WHERE source_library_id = OLD.id
                     AND publication_phase IN ('prepared', 'catalog_committed')) THEN
            RAISE EXCEPTION 'media publication recovery is pending for this library' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER library_roots_pending_media_publication
BEFORE DELETE OR UPDATE OF path, allowed_path, relative_path, binding_revision, storage_binding ON library_roots
FOR EACH ROW EXECUTE FUNCTION prevent_owner_changes_during_media_publication();

CREATE TRIGGER items_pending_media_publication
BEFORE DELETE ON items
FOR EACH ROW EXECUTE FUNCTION prevent_owner_changes_during_media_publication();

CREATE TRIGGER libraries_pending_media_publication
BEFORE DELETE ON libraries
FOR EACH ROW EXECUTE FUNCTION prevent_owner_changes_during_media_publication();

CREATE FUNCTION prevent_deletions_during_media_publication() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM id FROM libraries WHERE id = NEW.library_id FOR KEY SHARE;
    IF EXISTS (SELECT 1 FROM media_operations
               WHERE source_library_id = NEW.library_id
                 AND publication_phase IN ('prepared', 'catalog_committed')) THEN
        RAISE EXCEPTION 'media publication recovery is pending for this library' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER media_deletion_operations_pending_media_publication
BEFORE INSERT ON media_deletion_operations
FOR EACH ROW EXECUTE FUNCTION prevent_deletions_during_media_publication();
