-- Activity is an append-only account of committed facts until retention prunes
-- it. Identities are retained values, not cascading references or credentials.
-- Installation intentionally does not infer events from existing business rows.
CREATE TABLE activity_entries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    action text NOT NULL CHECK (action IN (
        'user.created','user.updated','user.password_reset','session.login','session.revoked',
        'application_key.created','application_key.revealed','application_key.revoked',
        'device.updated','device.removed','library.created','library.removed',
        'scan.requested','scan.cancel_requested','scan.finished','metadata.updated','settings.updated',
        'task.admitted','task.cancel_requested','task.finished','task.schedule_updated')),
    severity text NOT NULL CHECK (severity IN ('Debug','Info','Warn','Error','Fatal')),
    source text NOT NULL CHECK (source IN ('native','emby','system')),
    actor_kind text NOT NULL CHECK (actor_kind IN ('user','application_key','system')),
    actor_id text NOT NULL DEFAULT '',
    actor_credential_id text NOT NULL DEFAULT '',
    resource_kind text NOT NULL CHECK (resource_kind IN (
        'user','session','application_key','device','library','scan','item','settings','task','task_run')),
    resource_id text NOT NULL CHECK (resource_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$'),
    request_id text NOT NULL DEFAULT ''
        CHECK (request_id = '' OR request_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$'),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    affected_count bigint NOT NULL DEFAULT 0 CHECK (affected_count >= 0),
    state text NOT NULL DEFAULT '' CHECK (state IN ('','completed','failed','cancelled','interrupted')),
    changed_fields text[] NOT NULL DEFAULT '{}'::text[] CHECK (
        cardinality(changed_fields) <= 40 AND array_position(changed_fields,NULL) IS NULL
        AND (cardinality(changed_fields) = 0 OR array_ndims(changed_fields) = 1)),
    CHECK (
        (actor_kind = 'system' AND actor_id = '' AND actor_credential_id = '')
        OR (actor_kind IN ('user','application_key')
            AND actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$'
            AND (actor_credential_id = '' OR actor_credential_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$'))
    ),
    CHECK ((action IN ('scan.finished','task.finished')) = (state <> '')),
    CHECK (
        (action IN ('user.created','user.updated','user.password_reset') AND resource_kind = 'user')
        OR (action IN ('session.login','session.revoked') AND resource_kind = 'session')
        OR (action IN ('application_key.created','application_key.revealed','application_key.revoked') AND resource_kind = 'application_key')
        OR (action IN ('device.updated','device.removed') AND resource_kind = 'device')
        OR (action IN ('library.created','library.removed') AND resource_kind = 'library')
        OR (action IN ('scan.requested','scan.cancel_requested','scan.finished') AND resource_kind = 'scan')
        OR (action = 'metadata.updated' AND resource_kind = 'item')
        OR (action = 'settings.updated' AND resource_kind = 'settings')
        OR (action IN ('task.admitted','task.cancel_requested','task.finished') AND resource_kind = 'task_run')
        OR (action = 'task.schedule_updated' AND resource_kind = 'task')
    ),
    CHECK (
        cardinality(changed_fields) = 0
        OR (action IN ('user.created','user.updated') AND changed_fields <@ ARRAY[
            'Name','IsAdministrator','IsDisabled','Policy','EnableAllFolders','EnabledFolders',
            'EnableMediaPlayback','EnablePlaybackRemuxing','EnableAudioPlaybackTranscoding','EnableVideoPlaybackTranscoding']::text[])
        OR (action = 'device.updated' AND changed_fields <@ ARRAY['CustomName']::text[])
        OR (action = 'metadata.updated' AND changed_fields <@ ARRAY[
            'Name','SortName','Overview','OriginalTitle','OfficialRating','ProductionYear','IndexNumber',
            'ParentIndexNumber','PremiereDate','CommunityRating','ProviderIds','Genres','Tags','Studios',
            'People','LockedFields','Overrides']::text[])
        OR (action = 'settings.updated' AND changed_fields <@ ARRAY[
            'ServerName','ServerNameMode','MaxBitrate','MaxWidth','MaxHeight','MaxAudioChannels','TranscodingMaxWidth']::text[])
        OR (action = 'task.schedule_updated' AND changed_fields <@ ARRAY['Triggers','ScheduleTimezone']::text[])
    )
);

CREATE INDEX activity_entries_date_idx ON activity_entries(created_at DESC,id DESC);
CREATE INDEX activity_entries_actor_date_idx ON activity_entries(actor_id,created_at DESC,id DESC)
    WHERE actor_id <> '';
CREATE INDEX activity_entries_action_date_idx ON activity_entries(action,created_at DESC,id DESC);
CREATE INDEX activity_entries_severity_date_idx ON activity_entries(severity,created_at DESC,id DESC);
