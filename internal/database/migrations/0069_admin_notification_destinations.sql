-- Administrator destinations are independent of personal client registrations.
-- Their queue records only committed, bounded activity facts and starts empty.
CREATE TABLE admin_notification_destinations (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 120),
    transport text NOT NULL CHECK (transport IN ('webhook','smtp')),
    enabled boolean NOT NULL DEFAULT false,
    event_ids text[] NOT NULL CHECK (cardinality(event_ids) BETWEEN 1 AND 7 AND array_position(event_ids,NULL) IS NULL
        AND event_ids <@ ARRAY['UserChanges','SignIn','LibraryChanges','ScanFinished','TaskFinished','BackupFinished','MetadataChanges']::text[]),
    allowed_networks text[] NOT NULL DEFAULT '{}' CHECK (cardinality(allowed_networks) <= 32 AND array_position(allowed_networks,NULL) IS NULL),
    endpoint text NOT NULL DEFAULT '' CHECK (octet_length(endpoint) <= 2048),
    smtp_config jsonb NOT NULL CHECK (jsonb_typeof(smtp_config) = 'object' AND octet_length(smtp_config::text) <= 8192),
    owner_user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret_generation bigint NOT NULL DEFAULT 1 CHECK (secret_generation > 0),
    secret_ciphertext bytea CHECK (octet_length(secret_ciphertext) BETWEEN 48 AND 2080),
    last_outcome text NOT NULL DEFAULT '' CHECK (octet_length(last_outcome) <= 64),
    last_delivered_at timestamptz,
    dropped_count integer NOT NULL DEFAULT 0 CHECK (dropped_count >= 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (NOT enabled OR transport <> 'webhook' OR (endpoint <> '' AND secret_ciphertext IS NOT NULL))
);

CREATE TABLE admin_notification_deliveries (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    destination_id text NOT NULL REFERENCES admin_notification_destinations(id) ON DELETE CASCADE,
    destination_revision bigint NOT NULL CHECK (destination_revision > 0),
    source_key text NOT NULL CHECK (octet_length(source_key) BETWEEN 1 AND 80),
    event_kind text NOT NULL CHECK (event_kind IN ('UserChanges','SignIn','LibraryChanges','ScanFinished','TaskFinished','BackupFinished','MetadataChanges','Test')),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4096),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sending','delivered','failed','cancelled')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    due_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_id text NOT NULL DEFAULT '' CHECK (octet_length(lease_id) IN (0,32)),
    lease_until timestamptz,
    outcome text NOT NULL DEFAULT '' CHECK (octet_length(outcome) <= 64),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((state = 'sending' AND lease_id <> '' AND lease_until IS NOT NULL)
        OR (state <> 'sending' AND lease_id = '' AND lease_until IS NULL)),
    UNIQUE (destination_id,source_key)
);
CREATE INDEX admin_notification_delivery_due_idx ON admin_notification_deliveries(due_at,id) WHERE state = 'pending';
CREATE INDEX admin_notification_delivery_target_idx ON admin_notification_deliveries(destination_id,created_at,id);

CREATE FUNCTION goby_queue_admin_activity_notification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    destination record;
    event_type text;
    event_title text;
    delivery_id text;
    queued integer;
BEGIN
    event_type := CASE
        WHEN NEW.action IN ('user.created','user.updated','user.deleted','user.password_reset') THEN 'UserChanges'
        WHEN NEW.action = 'session.login' THEN 'SignIn'
        WHEN NEW.action IN ('library.created','library.updated','library.removed','library.root_binding.updated') THEN 'LibraryChanges'
        WHEN NEW.action = 'scan.finished' THEN 'ScanFinished'
        WHEN NEW.action = 'task.finished' THEN 'TaskFinished'
        WHEN NEW.action = 'backup.published' AND NEW.state = 'completed' THEN 'BackupFinished'
        WHEN NEW.action = 'metadata.updated' THEN 'MetadataChanges'
        ELSE '' END;
    IF event_type = '' THEN RETURN NEW; END IF;
    event_title := CASE event_type
        WHEN 'UserChanges' THEN 'User account changed'
        WHEN 'SignIn' THEN 'User signed in'
        WHEN 'LibraryChanges' THEN 'Media library changed'
        WHEN 'ScanFinished' THEN 'Library scan finished'
        WHEN 'TaskFinished' THEN 'Scheduled task finished'
        WHEN 'BackupFinished' THEN 'Backup published'
        ELSE 'Metadata changed' END;
    FOR destination IN SELECT d.id,d.revision FROM admin_notification_destinations d
        JOIN users u ON u.id=d.owner_user_id
        WHERE d.enabled AND event_type=ANY(d.event_ids) AND u.is_administrator AND NOT u.is_disabled
        ORDER BY d.id FOR UPDATE OF d LOOP
        SELECT count(*) INTO queued FROM admin_notification_deliveries
            WHERE destination_id=destination.id AND state IN ('pending','sending');
        IF queued >= 256 THEN
            UPDATE admin_notification_destinations SET dropped_count=LEAST(dropped_count::bigint+1,2147483647),last_outcome='queue_capacity'
                WHERE id=destination.id;
            CONTINUE;
        END IF;
        delivery_id := md5(destination.id || ':activity:' || NEW.id::text);
        INSERT INTO admin_notification_deliveries(id,destination_id,destination_revision,source_key,event_kind,payload)
        VALUES(delivery_id,destination.id,destination.revision,'activity:' || NEW.id::text,event_type,
            jsonb_build_object('EventId',delivery_id,'Kind',event_type,'Title',event_title,
                'Summary',NEW.action || ' · ' || NEW.resource_kind || ' ' || NEW.resource_id
                    || CASE WHEN NEW.state <> '' THEN ' · ' || NEW.state ELSE '' END,
                'OccurredAt',NEW.created_at)) ON CONFLICT(destination_id,source_key) DO NOTHING;
    END LOOP;
    RETURN NEW;
END $$;

CREATE TRIGGER admin_activity_notification AFTER INSERT ON activity_entries
    FOR EACH ROW EXECUTE FUNCTION goby_queue_admin_activity_notification();
