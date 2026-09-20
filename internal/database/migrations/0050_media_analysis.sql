-- Media analysis profiles and immutable admission facts. Existing source,
-- administrator intro and task history remain unchanged by this migration.
ALTER TABLE task_runs
 ADD COLUMN analysis_input jsonb,
 ADD COLUMN analysis_config_fingerprint text NOT NULL DEFAULT '',
 ADD COLUMN actor_application_key_id bigint NOT NULL DEFAULT 0 CHECK(actor_application_key_id>=0),
 ADD COLUMN actor_client_session_id text NOT NULL DEFAULT '' CHECK(octet_length(actor_client_session_id)<=256),
 ADD COLUMN actor_peer_ip text NOT NULL DEFAULT '' CHECK(octet_length(actor_peer_ip)<=256);
ALTER TABLE task_runs ADD CONSTRAINT task_runs_analysis_input_check CHECK(
 (task_key NOT IN ('media.intro_analysis','media.preview_generation') AND analysis_input IS NULL AND analysis_config_fingerprint='') OR
 (task_key IN ('media.intro_analysis','media.preview_generation') AND analysis_input IS NOT NULL AND jsonb_typeof(analysis_input)='object'
 AND octet_length(analysis_input::text)<=65536 AND analysis_input-'LibraryIds'-'ItemIds'-'Force'='{}'::jsonb
 AND (NOT analysis_input ? 'LibraryIds' OR jsonb_typeof(analysis_input->'LibraryIds')='array')
 AND (NOT analysis_input ? 'ItemIds' OR jsonb_typeof(analysis_input->'ItemIds')='array')
 AND (NOT analysis_input ? 'Force' OR jsonb_typeof(analysis_input->'Force')='boolean')
 AND analysis_config_fingerprint ~ '^[0-9a-f]{64}$'));
ALTER TABLE task_runs ADD CONSTRAINT task_runs_analysis_authority_check CHECK(
 task_key NOT IN ('media.intro_analysis','media.preview_generation') OR
 (source='manual' AND actor_kind='admin' AND actor_user_id<>'' AND actor_session_id<>'' AND actor_application_key_id=0 AND actor_client_session_id='') OR
 (source='compatibility' AND actor_session_id<>'' AND
  ((actor_kind='emby' AND actor_user_id<>'' AND actor_application_key_id=0 AND actor_client_session_id='') OR
   (actor_kind='application_key' AND actor_user_id='' AND actor_application_key_id>0 AND actor_client_session_id<>''))) OR
 (source IN ('schedule','startup','system_event') AND actor_kind='system' AND actor_user_id='' AND actor_session_id=''
  AND actor_application_key_id=0 AND actor_client_session_id='' AND actor_peer_ip=''));
ALTER TABLE task_run_children ADD COLUMN analysis_scope_key text NOT NULL DEFAULT '' CHECK(octet_length(analysis_scope_key)<=256);
ALTER TABLE task_run_children DROP CONSTRAINT task_run_children_run_id_library_id_key;
ALTER TABLE task_run_children ADD CONSTRAINT task_children_library_scope_key UNIQUE(run_id,library_id,analysis_scope_key);
CREATE FUNCTION enforce_task_analysis_scope_key() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE analysis boolean;
BEGIN
 SELECT task_key IN ('media.intro_analysis','media.preview_generation') INTO STRICT analysis FROM task_runs WHERE id=NEW.run_id;
 IF analysis <> (NEW.analysis_scope_key<>'') THEN RAISE EXCEPTION 'task analysis scope key mismatch'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER task_children_analysis_scope_key BEFORE INSERT OR UPDATE OF run_id,analysis_scope_key ON task_run_children
 FOR EACH ROW EXECUTE FUNCTION enforce_task_analysis_scope_key();

CREATE TABLE analysis_settings(
 id integer PRIMARY KEY CHECK(id=1), revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 publication_epoch bigint NOT NULL DEFAULT 1 CHECK(publication_epoch>0),
 auto_publish_intros boolean NOT NULL DEFAULT true,
 preview_interval_seconds integer NOT NULL DEFAULT 10 CHECK(preview_interval_seconds BETWEEN 2 AND 120),
 preview_quality integer NOT NULL DEFAULT 80 CHECK(preview_quality BETWEEN 40 AND 95),
 max_source_bytes bigint NOT NULL DEFAULT 137438953472 CHECK(max_source_bytes BETWEEN 1 AND 1099511627776),
 max_item_runtime_seconds integer NOT NULL DEFAULT 1200 CHECK(max_item_runtime_seconds BETWEEN 1 AND 7200),
 feature_cache_max_bytes bigint NOT NULL DEFAULT 134217728 CHECK(feature_cache_max_bytes BETWEEN 1048576 AND 536870912),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
INSERT INTO analysis_settings(id) VALUES(1);
CREATE TABLE analysis_run_profiles(
 run_id text PRIMARY KEY REFERENCES task_runs(id) ON DELETE CASCADE,
 configuration_revision bigint NOT NULL CHECK(configuration_revision>0), publication_epoch bigint NOT NULL CHECK(publication_epoch>0),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 profile jsonb NOT NULL CHECK(jsonb_typeof(profile)='object' AND octet_length(profile::text)<=32768),
 execution jsonb NOT NULL CHECK(jsonb_typeof(execution)='object' AND octet_length(execution::text)<=32768));
CREATE TABLE analysis_work(
 child_id text PRIMARY KEY REFERENCES task_run_children(id) ON DELETE CASCADE,
 run_id text NOT NULL REFERENCES analysis_run_profiles(run_id) ON DELETE CASCADE,
 task_key text NOT NULL CHECK(task_key IN ('media.intro_analysis','media.preview_generation')),
 library_id text NOT NULL, scope_key text NOT NULL CHECK(octet_length(scope_key) BETWEEN 1 AND 256),
 cohort_revision text NOT NULL CHECK(cohort_revision ~ '^[0-9a-f]{64}$'), force boolean NOT NULL,
 reason text NOT NULL DEFAULT '' CHECK(reason IN ('','insufficient_cohort','unsupported_item_type','unsupported_hierarchy')),
 UNIQUE(run_id,scope_key));
CREATE TABLE analysis_work_sources(
 child_id text NOT NULL REFERENCES analysis_work(child_id) ON DELETE CASCADE,
 item_id text NOT NULL, position integer NOT NULL CHECK(position BETWEEN 0 AND 31), target boolean NOT NULL,
 library_id text NOT NULL,root_id text NOT NULL,series_id text NOT NULL,season_id text NOT NULL,
 episode_key text NOT NULL CHECK(octet_length(episode_key)<=512),item_type text NOT NULL CHECK(item_type IN ('Movie','Episode','Video')),
 source_revision text NOT NULL CHECK(octet_length(source_revision) BETWEEN 1 AND 256),
 hierarchy_revision text NOT NULL CHECK(octet_length(hierarchy_revision) BETWEEN 1 AND 256),
 duration_ticks bigint NOT NULL CHECK(duration_ticks BETWEEN 1 AND 432000000000),
 size bigint NOT NULL CHECK(size BETWEEN 1 AND 1099511627776),
 manual_revision bigint NOT NULL CHECK(manual_revision>=0),decision_revision bigint NOT NULL CHECK(decision_revision>=0),
 preview_revision bigint NOT NULL CHECK(preview_revision>=0),
 PRIMARY KEY(child_id,item_id),UNIQUE(child_id,position));
CREATE FUNCTION prevent_analysis_snapshot_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'analysis admission snapshot is immutable'; END $$;
CREATE TRIGGER analysis_profile_immutable BEFORE UPDATE ON analysis_run_profiles FOR EACH ROW EXECUTE FUNCTION prevent_analysis_snapshot_update();
CREATE TRIGGER analysis_work_immutable BEFORE UPDATE ON analysis_work FOR EACH ROW EXECUTE FUNCTION prevent_analysis_snapshot_update();
CREATE TRIGGER analysis_sources_immutable BEFORE UPDATE ON analysis_work_sources FOR EACH ROW EXECUTE FUNCTION prevent_analysis_snapshot_update();

CREATE TABLE analysis_feature_cache(
 cache_key text PRIMARY KEY CHECK(cache_key ~ '^[0-9a-f]{64}$'),
 item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,source_revision text NOT NULL,
 profile_fingerprint text NOT NULL CHECK(profile_fingerprint ~ '^[0-9a-f]{64}$'),
 content_sha256 text NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
 algorithm_profile text NOT NULL CHECK(octet_length(algorithm_profile) BETWEEN 1 AND 512),
 duration_ticks bigint NOT NULL CHECK(duration_ticks BETWEEN 1 AND 432000000000),
 payload bytea NOT NULL CHECK(octet_length(payload) BETWEEN 1 AND 262144),
 bytes bigint NOT NULL CHECK(bytes=octet_length(payload)),last_used_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(item_id,source_revision,profile_fingerprint));
CREATE INDEX analysis_feature_cache_lru ON analysis_feature_cache(last_used_at,cache_key);
CREATE TABLE analysis_detections(
 item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,revision bigint NOT NULL CHECK(revision>0),
 source_revision text NOT NULL,profile_fingerprint text NOT NULL CHECK(profile_fingerprint ~ '^[0-9a-f]{64}$'),
 profile_revision bigint NOT NULL CHECK(profile_revision>0),publication_epoch bigint NOT NULL CHECK(publication_epoch>0),
 child_id text NOT NULL,cohort_revision text NOT NULL CHECK(cohort_revision ~ '^[0-9a-f]{64}$'),
 status text NOT NULL CHECK(status IN ('qualified','review','no_result')),
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=131072),
 start_ticks bigint,end_ticks bigint,auto_published boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((start_ticks IS NULL)=(end_ticks IS NULL)),CHECK(start_ticks IS NULL OR (start_ticks>=0 AND end_ticks>start_ticks AND end_ticks<=432000000000)),
 CHECK(NOT auto_published OR (status='qualified' AND start_ticks IS NOT NULL)));
CREATE TABLE analysis_detection_sources(
 item_id text NOT NULL REFERENCES analysis_detections(item_id) ON DELETE CASCADE,
 source_item_id text NOT NULL,library_id text NOT NULL,root_id text NOT NULL,
 source_revision text NOT NULL,hierarchy_revision text NOT NULL,episode_key text NOT NULL,
 content_sha256 text NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(item_id,source_item_id));
CREATE TABLE analysis_intro_decisions(
 item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,revision bigint NOT NULL CHECK(revision>0),
 source_revision text NOT NULL,rejected boolean NOT NULL,
 updated_by text REFERENCES users(id) ON DELETE SET NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE analysis_intro_audit(
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,item_id text NOT NULL,revision bigint NOT NULL CHECK(revision>0),
 source_revision text NOT NULL,profile_fingerprint text NOT NULL CHECK(profile_fingerprint='' OR profile_fingerprint ~ '^[0-9a-f]{64}$'),
 action text NOT NULL CHECK(action IN ('qualified','review','no_result','accept','reject','reset')),
 evidence jsonb NOT NULL CHECK(jsonb_typeof(evidence)='object' AND octet_length(evidence::text)<=131072),
 actor_id text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE INDEX analysis_intro_audit_item ON analysis_intro_audit(item_id,id DESC);
CREATE TABLE analysis_preview_state(
 item_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0));
CREATE TABLE analysis_previews(
 item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,width integer NOT NULL CHECK(width IN (240,320,400)),
 revision bigint NOT NULL CHECK(revision>0),source_revision text NOT NULL,
 profile_fingerprint text NOT NULL CHECK(profile_fingerprint ~ '^[0-9a-f]{64}$'),
 profile_revision bigint NOT NULL CHECK(profile_revision>0),publication_epoch bigint NOT NULL CHECK(publication_epoch>0),
 child_id text NOT NULL,cache_key text NOT NULL CHECK(cache_key ~ '^[0-9a-f]{64}$'),seal text NOT NULL CHECK(seal ~ '^[0-9a-f]{64}$'),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 2048),content_sha256 text NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
 bytes bigint NOT NULL CHECK(bytes BETWEEN 1 AND 134217728),frame_count integer NOT NULL CHECK(frame_count BETWEEN 1 AND 4096),
 interval_ticks bigint NOT NULL CHECK(interval_ticks BETWEEN 20000000 AND 1200000000),timeline bytea NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(item_id,width),
 CHECK(octet_length(timeline)=frame_count*16),CHECK(bytes>=72+12::bigint*frame_count));
