-- Runtime choices share the existing singleton revision. Deployment defaults,
-- active listeners, device paths and inventory evidence are never copied here.
ALTER TABLE managed_settings ADD COLUMN runtime_overrides jsonb NOT NULL DEFAULT
    '{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}'::jsonb;

ALTER TABLE managed_settings ADD CONSTRAINT managed_runtime_overrides_shape CHECK (
    jsonb_typeof(runtime_overrides) = 'object'
    AND runtime_overrides ?& ARRAY['Network','Hardware','Threads','H264','HEVC','SoftwareToneMapping','VulkanToneMapping']
    AND runtime_overrides - ARRAY['Network','Hardware','Threads','H264','HEVC','SoftwareToneMapping','VulkanToneMapping']::text[] = '{}'::jsonb
    AND octet_length(runtime_overrides::text) <= 8192
    AND (runtime_overrides->'Network' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'Network') = 'object'
        AND runtime_overrides->'Network' ?& ARRAY['BindHost','HttpPort']
        AND (runtime_overrides->'Network') - ARRAY['BindHost','HttpPort']::text[] = '{}'::jsonb
        AND (runtime_overrides->'Network'->'BindHost' = 'null'::jsonb OR (
            jsonb_typeof(runtime_overrides->'Network'->'BindHost') = 'string'
            AND octet_length(runtime_overrides->'Network'->>'BindHost') <= 45
            AND runtime_overrides->'Network'->>'BindHost' ~ '^[0-9A-Fa-f:.]*$'))
        AND (runtime_overrides->'Network'->'HttpPort' = 'null'::jsonb OR CASE
            WHEN jsonb_typeof(runtime_overrides->'Network'->'HttpPort') = 'number'
                AND runtime_overrides->'Network'->>'HttpPort' ~ '^[0-9]{1,5}$'
            THEN (runtime_overrides->'Network'->>'HttpPort')::integer BETWEEN 1 AND 65535 ELSE false END)))
    AND (runtime_overrides->'Hardware' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'Hardware') = 'object'
        AND runtime_overrides->'Hardware' ?& ARRAY['Decode','Encode','DeviceID']
        AND (runtime_overrides->'Hardware') - ARRAY['Decode','Encode','DeviceID']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'Hardware'->'Decode') = 'string'
        AND runtime_overrides->'Hardware'->>'Decode' IN ('software','vaapi')
        AND jsonb_typeof(runtime_overrides->'Hardware'->'Encode') = 'string'
        AND runtime_overrides->'Hardware'->>'Encode' IN ('software','vaapi')
        AND jsonb_typeof(runtime_overrides->'Hardware'->'DeviceID') = 'string'
        AND (runtime_overrides->'Hardware'->>'DeviceID' ~ '^[A-Za-z0-9_-]{1,128}$'
            OR (runtime_overrides->'Hardware'->>'DeviceID' = ''
                AND runtime_overrides->'Hardware'->>'Decode' = 'software'
                AND runtime_overrides->'Hardware'->>'Encode' = 'software'))))
    AND (runtime_overrides->'Threads' = 'null'::jsonb OR CASE
        WHEN jsonb_typeof(runtime_overrides->'Threads') = 'number' AND runtime_overrides->>'Threads' ~ '^[0-9]{1,2}$'
        THEN (runtime_overrides->>'Threads')::integer BETWEEN 1 AND 64 ELSE false END)
    AND (runtime_overrides->'H264' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'H264') = 'object'
        AND runtime_overrides->'H264' ?& ARRAY['Preset','RateControl','CRF']
        AND (runtime_overrides->'H264') - ARRAY['Preset','RateControl','CRF']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'H264'->'Preset') = 'string'
        AND runtime_overrides->'H264'->>'Preset' IN ('veryfast','fast','medium','slow')
        AND jsonb_typeof(runtime_overrides->'H264'->'RateControl') = 'string'
        AND runtime_overrides->'H264'->>'RateControl' IN ('bitrate','capped_crf')
        AND CASE WHEN jsonb_typeof(runtime_overrides->'H264'->'CRF') = 'number' AND runtime_overrides->'H264'->>'CRF' ~ '^[0-9]{2}$'
            THEN (runtime_overrides->'H264'->>'CRF')::integer BETWEEN 18 AND 35 ELSE false END))
    AND (runtime_overrides->'HEVC' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'HEVC') = 'object'
        AND runtime_overrides->'HEVC' ?& ARRAY['Preset','RateControl','CRF']
        AND (runtime_overrides->'HEVC') - ARRAY['Preset','RateControl','CRF']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'HEVC'->'Preset') = 'string'
        AND runtime_overrides->'HEVC'->>'Preset' IN ('veryfast','fast','medium','slow')
        AND jsonb_typeof(runtime_overrides->'HEVC'->'RateControl') = 'string'
        AND runtime_overrides->'HEVC'->>'RateControl' IN ('bitrate','capped_crf')
        AND CASE WHEN jsonb_typeof(runtime_overrides->'HEVC'->'CRF') = 'number' AND runtime_overrides->'HEVC'->>'CRF' ~ '^[0-9]{2}$'
            THEN (runtime_overrides->'HEVC'->>'CRF')::integer BETWEEN 18 AND 35 ELSE false END))
    AND jsonb_typeof(runtime_overrides->'SoftwareToneMapping') IN ('boolean','null')
    AND jsonb_typeof(runtime_overrides->'VulkanToneMapping') IN ('boolean','null')
);

-- Audit names are closed and contain no device, address, or encoder values.
DO $$
DECLARE constraint_name text;
        constraint_definition text;
BEGIN
    SELECT conname, pg_get_constraintdef(oid) INTO STRICT constraint_name, constraint_definition
    FROM pg_constraint WHERE conrelid = 'activity_entries'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%TranscodingMaxWidth%'
      AND pg_get_constraintdef(oid) LIKE '%changed_fields%';
    constraint_definition := replace(constraint_definition,
        '''Management''::text', '''Management''::text, ''Runtime''::text');
    IF constraint_definition NOT LIKE '%Runtime%' THEN
        RAISE EXCEPTION 'runtime activity constraint cannot be extended';
    END IF;
    EXECUTE format('ALTER TABLE activity_entries DROP CONSTRAINT %I', constraint_name);
    EXECUTE format('ALTER TABLE activity_entries ADD CONSTRAINT %I %s', constraint_name, constraint_definition);
END $$;
