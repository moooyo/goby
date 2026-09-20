CREATE TABLE notification_transport (
    id integer PRIMARY KEY CHECK (id=1),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision>0),
    enabled boolean NOT NULL DEFAULT false,
    endpoint text NOT NULL DEFAULT '' CHECK (octet_length(endpoint)<=2048),
    allowed_networks text[] NOT NULL DEFAULT '{}' CHECK (cardinality(allowed_networks)<=32 AND array_position(allowed_networks,NULL) IS NULL),
    credential_ciphertext bytea CHECK (octet_length(credential_ciphertext) BETWEEN 48 AND 2080),
    credential_generation bigint NOT NULL DEFAULT 1 CHECK (credential_generation>0),
    CHECK (NOT enabled OR (endpoint<>'' AND credential_ciphertext IS NOT NULL))
);
INSERT INTO notification_transport(id) VALUES(1);

CREATE TABLE notification_journal_state (
    id integer PRIMARY KEY CHECK(id=1),
    sequence bigint NOT NULL DEFAULT 0 CHECK(sequence>=0)
);
INSERT INTO notification_journal_state(id) VALUES(1);

CREATE TABLE notification_registrations (
    id text PRIMARY KEY CHECK (octet_length(id)=32),
    session_id text NOT NULL UNIQUE REFERENCES sessions(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id text NOT NULL CHECK(octet_length(device_id)<=256),
    peer_ip text NOT NULL CHECK(octet_length(peer_ip)<=64),
    revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
    enabled boolean NOT NULL DEFAULT true,
    event_ids text[] NOT NULL CHECK(cardinality(event_ids) BETWEEN 1 AND 2 AND array_position(event_ids,NULL) IS NULL AND event_ids <@ ARRAY['CatalogInvalidated','UserDataInvalidated']::text[]),
    token_ciphertext bytea NOT NULL CHECK(octet_length(token_ciphertext) BETWEEN 48 AND 2080),
    token_generation bigint NOT NULL DEFAULT 1 CHECK(token_generation>0),
    source_cursor bigint NOT NULL DEFAULT 0 CHECK(source_cursor>=0),
    last_outcome text NOT NULL DEFAULT '' CHECK(octet_length(last_outcome)<=64),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE notification_source_events (
    id text PRIMARY KEY CHECK(octet_length(id)=32),
    sequence bigint NOT NULL UNIQUE CHECK(sequence>0),
    kind text NOT NULL CHECK(kind IN ('CatalogInvalidated','UserDataInvalidated')),
    user_id text REFERENCES users(id) ON DELETE CASCADE,
    refs jsonb NOT NULL CHECK(jsonb_typeof(refs)='array' AND jsonb_array_length(refs)<=4096 AND octet_length(refs::text)<=524288),
    recursive boolean NOT NULL DEFAULT false,
    resync boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK((kind='CatalogInvalidated' AND user_id IS NULL) OR (kind='UserDataInvalidated' AND user_id IS NOT NULL))
);

CREATE TABLE notification_deliveries (
    id text PRIMARY KEY CHECK(octet_length(id)=32),
    registration_id text NOT NULL REFERENCES notification_registrations(id) ON DELETE CASCADE,
    registration_revision bigint NOT NULL CHECK(registration_revision>0),
    transport_revision bigint NOT NULL CHECK(transport_revision>0),
    source_sequence bigint NOT NULL CHECK(source_sequence>=0),
    kind text NOT NULL CHECK(kind IN ('CatalogInvalidated','UserDataInvalidated','ResyncRequired','Test')),
    refs jsonb NOT NULL CHECK(jsonb_typeof(refs)='array' AND jsonb_array_length(refs)<=4096 AND octet_length(refs::text)<=524288),
    recursive boolean NOT NULL DEFAULT false,
    state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sending','delivered','failed','cancelled','suppressed')),
    attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
    due_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_id text NOT NULL DEFAULT '' CHECK(octet_length(lease_id) IN (0,32)),
    lease_until timestamptz,
    outcome text NOT NULL DEFAULT '' CHECK(octet_length(outcome)<=64),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK((state='sending' AND lease_id<>'' AND lease_until IS NOT NULL) OR (state<>'sending' AND lease_id='' AND lease_until IS NULL)),
    UNIQUE(registration_id,registration_revision,source_sequence,kind)
);
CREATE INDEX notification_delivery_due_idx ON notification_deliveries(due_at,id) WHERE state='pending';
CREATE INDEX notification_delivery_registration_idx ON notification_deliveries(registration_id,created_at,id);

-- Source writes and this bounded journal share the caller's transaction. There
-- is no network callback, actor impersonation, or after-commit durability gap.
CREATE FUNCTION goby_record_notification_source(p_id text,p_kind text,p_user text,p_refs jsonb,p_recursive boolean,p_resync boolean)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE next_sequence bigint;
BEGIN
    PERFORM id FROM notification_transport WHERE id=1 FOR SHARE;
    PERFORM id FROM notification_journal_state WHERE id=1 FOR UPDATE;
    IF NOT EXISTS(SELECT 1 FROM notification_transport WHERE id=1 AND enabled)
       OR NOT EXISTS(SELECT 1 FROM notification_registrations r JOIN sessions s ON s.id=r.session_id JOIN users u ON u.id=r.user_id
         WHERE r.enabled AND s.kind='emby' AND s.user_id=r.user_id AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
         AND NOT u.is_disabled AND p_kind=ANY(r.event_ids) AND (p_user IS NULL OR r.user_id=p_user)) THEN RETURN; END IF;
    IF p_kind='CatalogInvalidated' AND p_resync AND jsonb_array_length(p_refs)=0 THEN
        RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='notification_source_scope_required';
    END IF;
    DELETE FROM notification_source_events WHERE sequence <= COALESCE((SELECT min(source_cursor) FROM notification_registrations WHERE enabled),9223372036854775807);
    IF octet_length(p_refs::text)>524288 OR jsonb_array_length(p_refs)>4096 OR
       (NOT EXISTS(SELECT 1 FROM notification_source_events WHERE id=p_id) AND (SELECT count(*) FROM notification_source_events)>=512) OR
        (SELECT COALESCE(sum(octet_length(refs::text)),0) FROM notification_source_events WHERE id<>p_id)+octet_length(p_refs::text)>4194304 THEN
        RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='notification_source_capacity';
    END IF;
    UPDATE notification_journal_state SET sequence=sequence+1 WHERE id=1 RETURNING sequence INTO next_sequence;
    INSERT INTO notification_source_events(id,sequence,kind,user_id,refs,recursive,resync)
    VALUES(p_id,next_sequence,p_kind,p_user,p_refs,p_recursive,p_resync)
    ON CONFLICT(id) DO UPDATE SET sequence=EXCLUDED.sequence,refs=EXCLUDED.refs,recursive=EXCLUDED.recursive,resync=EXCLUDED.resync;
END $$;
