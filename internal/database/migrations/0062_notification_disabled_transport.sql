-- Retain the transport lock while avoiding journal contention when disabled.
-- Registration eligibility remains protected by the journal ordering boundary.
CREATE OR REPLACE FUNCTION goby_record_notification_source(p_id text,p_kind text,p_user text,p_refs jsonb,p_recursive boolean,p_resync boolean)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE next_sequence bigint;
BEGIN
    PERFORM id FROM notification_transport WHERE id=1 FOR SHARE;
    IF NOT EXISTS(SELECT 1 FROM notification_transport WHERE id=1 AND enabled) THEN RETURN; END IF;
    PERFORM id FROM notification_journal_state WHERE id=1 FOR UPDATE;
    IF NOT EXISTS(SELECT 1 FROM notification_registrations r JOIN sessions s ON s.id=r.session_id JOIN users u ON u.id=r.user_id
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
