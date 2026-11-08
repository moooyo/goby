-- Player observations join the existing bounded activity log. No event is
-- inferred from historical sessions, expiry, or an accepted remote command.
-- Preserve every published historical check while extending its typed cases.
DO $$
DECLARE
    constraint_name text;
    definition text;
    addition text;
BEGIN
    FOREACH constraint_name IN ARRAY ARRAY[
        'activity_entries_action_check',
        'activity_entries_action_resource_check',
        'activity_entries_root_binding_fact_check'
    ] LOOP
        SELECT pg_get_constraintdef(oid) INTO STRICT definition
        FROM pg_constraint
        WHERE conrelid='activity_entries'::regclass AND contype='c' AND conname=constraint_name;
        IF left(definition,6) <> 'CHECK ' OR position('playback.started' IN definition)>0
            OR position('playback.stopped' IN definition)>0 THEN
            RAISE EXCEPTION 'Unexpected playback activity constraint';
        END IF;
        addition := CASE constraint_name
            WHEN 'activity_entries_action_resource_check' THEN
                'action IN (''playback.started'',''playback.stopped'') AND resource_kind=''item'''
            ELSE 'action IN (''playback.started'',''playback.stopped'')'
        END;
        EXECUTE format('ALTER TABLE activity_entries DROP CONSTRAINT %I',constraint_name);
        EXECUTE format('ALTER TABLE activity_entries ADD CONSTRAINT %I CHECK ((%s) OR (%s))',
            constraint_name,substring(definition FROM 7),addition);
    END LOOP;
END $$;

ALTER TABLE activity_entries ADD CONSTRAINT activity_entries_playback_fact_check CHECK (
    action NOT IN ('playback.started','playback.stopped')
    OR (severity='Info' AND source IN ('native','emby') AND actor_kind IN ('user','application_key')
        AND (source<>'native' OR actor_kind='user') AND actor_credential_id<>''
        AND resource_kind='item' AND state='' AND revision=0 AND previous_revision=0
        AND affected_count=0 AND cardinality(changed_fields)=0
        AND observation_fingerprint COLLATE "C" ~ '^[0-9a-f]{64}$')
);

-- A canonical play has at most one start and one stop observation. Retention
-- removes display rows after the configured activity period; terminal plays
-- remain terminal and cannot re-emit a pruned observation on a later retry.
CREATE UNIQUE INDEX activity_entries_playback_observation_idx
    ON activity_entries(action,observation_fingerprint)
    WHERE action IN ('playback.started','playback.stopped');
