-- Extend the closed managed runtime contract without rewriting legacy rows.
-- Their original seven-field check remains an independent accepted branch.
-- New writes carry all eleven groups, preserving revision and provenance.
DO $migration$
DECLARE legacy_definition text;
        current_definition text := $current$
    jsonb_typeof(runtime_overrides) = 'object'
    AND runtime_overrides ?& ARRAY['Network','Hardware','Threads','H264','HEVC','SoftwareToneMapping','VulkanToneMapping','Transcoding','TranscodingTempPath','SoftwareToneMapOptions','CodecConfigurations']
    AND runtime_overrides - ARRAY['Network','Hardware','Threads','H264','HEVC','SoftwareToneMapping','VulkanToneMapping','Transcoding','TranscodingTempPath','SoftwareToneMapOptions','CodecConfigurations']::text[] = '{}'::jsonb
    AND octet_length(runtime_overrides::text) <= 16384
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
        AND (runtime_overrides->'H264') - ARRAY['Preset','RateControl','CRF','ThreadCount','ThreadCountSet','X264Options']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'H264'->'Preset') = 'string'
        AND runtime_overrides->'H264'->>'Preset' IN ('superfast','veryfast','faster','fast','medium','slow','slower','veryslow')
        AND jsonb_typeof(runtime_overrides->'H264'->'RateControl') = 'string'
        AND runtime_overrides->'H264'->>'RateControl' IN ('bitrate','capped_crf')
        AND CASE WHEN jsonb_typeof(runtime_overrides->'H264'->'CRF') = 'number' AND runtime_overrides->'H264'->>'CRF' ~ '^[0-9]{1,2}$'
            THEN (runtime_overrides->'H264'->>'CRF')::integer BETWEEN 1 AND 51 ELSE false END
        AND (NOT (runtime_overrides->'H264' ? 'ThreadCountSet') OR runtime_overrides->'H264'->'ThreadCountSet' = 'true'::jsonb)
        AND (NOT (runtime_overrides->'H264' ? 'ThreadCount') OR (
            COALESCE(runtime_overrides->'H264'->'ThreadCountSet' = 'true'::jsonb, false)
            AND CASE WHEN jsonb_typeof(runtime_overrides->'H264'->'ThreadCount') = 'number' AND runtime_overrides->'H264'->>'ThreadCount' ~ '^[0-9]{1,2}$'
                THEN (runtime_overrides->'H264'->>'ThreadCount')::integer BETWEEN 0 AND 16 ELSE false END))
        AND (NOT (runtime_overrides->'H264' ? 'X264Options') OR (
            jsonb_typeof(runtime_overrides->'H264'->'X264Options') = 'string'
            AND octet_length(runtime_overrides->'H264'->>'X264Options') <= 512
            AND runtime_overrides->'H264'->>'X264Options' ~ '^[A-Za-z0-9_,.=:-]*$'))))
    AND (runtime_overrides->'HEVC' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'HEVC') = 'object'
        AND runtime_overrides->'HEVC' ?& ARRAY['Preset','RateControl','CRF']
        AND (runtime_overrides->'HEVC') - ARRAY['Preset','RateControl','CRF','ThreadCount','ThreadCountSet','X264Options']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'HEVC'->'Preset') = 'string'
        AND runtime_overrides->'HEVC'->>'Preset' IN ('ultrafast','superfast','veryfast','faster','fast','medium','slow','slower','veryslow','placebo')
        AND jsonb_typeof(runtime_overrides->'HEVC'->'RateControl') = 'string'
        AND runtime_overrides->'HEVC'->>'RateControl' IN ('bitrate','capped_crf')
        AND CASE WHEN jsonb_typeof(runtime_overrides->'HEVC'->'CRF') = 'number' AND runtime_overrides->'HEVC'->>'CRF' ~ '^[0-9]{1,2}$'
            THEN (runtime_overrides->'HEVC'->>'CRF')::integer BETWEEN 1 AND 51 ELSE false END
        AND (NOT (runtime_overrides->'HEVC' ? 'ThreadCountSet') OR runtime_overrides->'HEVC'->'ThreadCountSet' = 'true'::jsonb)
        AND (NOT (runtime_overrides->'HEVC' ? 'ThreadCount') OR (
            COALESCE(runtime_overrides->'HEVC'->'ThreadCountSet' = 'true'::jsonb, false)
            AND CASE WHEN jsonb_typeof(runtime_overrides->'HEVC'->'ThreadCount') = 'number' AND runtime_overrides->'HEVC'->>'ThreadCount' ~ '^[0-9]{1,2}$'
                THEN (runtime_overrides->'HEVC'->>'ThreadCount')::integer BETWEEN 0 AND 16 ELSE false END))
        AND (NOT (runtime_overrides->'HEVC' ? 'X264Options') OR (
            jsonb_typeof(runtime_overrides->'HEVC'->'X264Options') = 'string'
            AND octet_length(runtime_overrides->'HEVC'->>'X264Options') <= 512
            AND runtime_overrides->'HEVC'->>'X264Options' = ''))))
    AND jsonb_typeof(runtime_overrides->'SoftwareToneMapping') IN ('boolean','null')
    AND jsonb_typeof(runtime_overrides->'VulkanToneMapping') IN ('boolean','null')
    AND (runtime_overrides->'Transcoding' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'Transcoding') = 'object'
        AND runtime_overrides->'Transcoding' ?& ARRAY['EnableHevcEncoding','EnableHardwareEncoding','DownMixAudioBoost','EnableSubtitleExtraction','EnableOnTheFlyAttachmentExtraction']
        AND (runtime_overrides->'Transcoding') - ARRAY['EnableHevcEncoding','EnableHardwareEncoding','DownMixAudioBoost','EnableSubtitleExtraction','EnableOnTheFlyAttachmentExtraction']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'Transcoding'->'EnableHevcEncoding') = 'boolean'
        AND jsonb_typeof(runtime_overrides->'Transcoding'->'EnableHardwareEncoding') = 'boolean'
        AND jsonb_typeof(runtime_overrides->'Transcoding'->'EnableSubtitleExtraction') = 'boolean'
        AND jsonb_typeof(runtime_overrides->'Transcoding'->'EnableOnTheFlyAttachmentExtraction') = 'boolean'
        AND CASE WHEN jsonb_typeof(runtime_overrides->'Transcoding'->'DownMixAudioBoost') = 'number'
            THEN (runtime_overrides->'Transcoding'->>'DownMixAudioBoost')::numeric BETWEEN 0.1 AND 10 ELSE false END))
    AND (runtime_overrides->'TranscodingTempPath' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'TranscodingTempPath') = 'string'
        AND octet_length(runtime_overrides->>'TranscodingTempPath') <= 4096
        AND (runtime_overrides->>'TranscodingTempPath' = '' OR (
            runtime_overrides->>'TranscodingTempPath' LIKE '/%'
            AND runtime_overrides->>'TranscodingTempPath' <> '/'
            AND runtime_overrides->>'TranscodingTempPath' = btrim(runtime_overrides->>'TranscodingTempPath')
            AND position(chr(92) in runtime_overrides->>'TranscodingTempPath') = 0
            AND runtime_overrides->>'TranscodingTempPath' !~ '[[:cntrl:]]|//|/[.](/|$)|/[.][.](/|$)|/$'))))
    AND (runtime_overrides->'SoftwareToneMapOptions' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'SoftwareToneMapOptions') = 'object'
        AND runtime_overrides->'SoftwareToneMapOptions' ?& ARRAY['ToneMapSizeLimit','SoftwareToneMapAlgorithm']
        AND (runtime_overrides->'SoftwareToneMapOptions') - ARRAY['ToneMapSizeLimit','SoftwareToneMapAlgorithm']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'SoftwareToneMapOptions'->'ToneMapSizeLimit') = 'string'
        AND runtime_overrides->'SoftwareToneMapOptions'->>'ToneMapSizeLimit' IN ('None','Limit420','Limit720','Limit1080')
        AND jsonb_typeof(runtime_overrides->'SoftwareToneMapOptions'->'SoftwareToneMapAlgorithm') = 'string'
        AND runtime_overrides->'SoftwareToneMapOptions'->>'SoftwareToneMapAlgorithm' IN ('Hable','Reinhard','Mobius')))
    AND (runtime_overrides->'CodecConfigurations' = 'null'::jsonb OR (
        CASE WHEN jsonb_typeof(runtime_overrides->'CodecConfigurations') = 'array'
            THEN jsonb_array_length(runtime_overrides->'CodecConfigurations') = 2 ELSE false END
        AND (jsonb_typeof(runtime_overrides->'CodecConfigurations'->0) = 'object'
        AND runtime_overrides->'CodecConfigurations'->0 ?& ARRAY['CodecId','IsEnabled','Priority']
        AND (runtime_overrides->'CodecConfigurations'->0) - ARRAY['CodecId','IsEnabled','Priority']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'CodecConfigurations'->0->'CodecId') = 'string'
        AND runtime_overrides->'CodecConfigurations'->0->>'CodecId' = 'V-E-libx264'
        AND jsonb_typeof(runtime_overrides->'CodecConfigurations'->0->'IsEnabled') = 'boolean'
        AND CASE WHEN jsonb_typeof(runtime_overrides->'CodecConfigurations'->0->'Priority') = 'number'
            AND runtime_overrides->'CodecConfigurations'->0->>'Priority' ~ '^-?[0-9]{1,4}$'
            THEN (runtime_overrides->'CodecConfigurations'->0->>'Priority')::integer BETWEEN -1000 AND 1000 ELSE false END)
        AND (jsonb_typeof(runtime_overrides->'CodecConfigurations'->1) = 'object'
        AND runtime_overrides->'CodecConfigurations'->1 ?& ARRAY['CodecId','IsEnabled','Priority']
        AND (runtime_overrides->'CodecConfigurations'->1) - ARRAY['CodecId','IsEnabled','Priority']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'CodecConfigurations'->1->'CodecId') = 'string'
        AND runtime_overrides->'CodecConfigurations'->1->>'CodecId' = 'V-E-libx265'
        AND jsonb_typeof(runtime_overrides->'CodecConfigurations'->1->'IsEnabled') = 'boolean'
        AND CASE WHEN jsonb_typeof(runtime_overrides->'CodecConfigurations'->1->'Priority') = 'number'
            AND runtime_overrides->'CodecConfigurations'->1->>'Priority' ~ '^-?[0-9]{1,4}$'
            THEN (runtime_overrides->'CodecConfigurations'->1->>'Priority')::integer BETWEEN -1000 AND 1000 ELSE false END)))
$current$;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO STRICT legacy_definition
    FROM pg_constraint WHERE conrelid = 'managed_settings'::regclass
        AND conname = 'managed_runtime_overrides_shape' AND contype = 'c';
    IF left(legacy_definition, 7) <> 'CHECK (' THEN
        RAISE EXCEPTION 'managed runtime legacy constraint cannot be extended';
    END IF;
    ALTER TABLE managed_settings DROP CONSTRAINT managed_runtime_overrides_shape;
    EXECUTE format('ALTER TABLE managed_settings ADD CONSTRAINT managed_runtime_overrides_shape CHECK ((%s) OR (%s))',
        substring(legacy_definition FROM 7), current_definition);
END $migration$;

ALTER TABLE managed_settings ALTER COLUMN runtime_overrides SET DEFAULT
    '{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null,"Transcoding":null,"TranscodingTempPath":null,"SoftwareToneMapOptions":null,"CodecConfigurations":null}'::jsonb;

