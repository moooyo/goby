-- Preserve the original seven- and eleven-group checks and their raw rows.
-- New writes add four independently managed controls. Project the old eleven
-- groups through their published check instead of duplicating its constraints.
DO $migration$
DECLARE legacy_definition text;
        projected_definition text;
        current_definition text := $current$
    jsonb_typeof(runtime_overrides) = 'object'
    AND runtime_overrides ?& ARRAY['Network','Hardware','Threads','H264','HEVC','SoftwareToneMapping','VulkanToneMapping','Transcoding','TranscodingTempPath','SoftwareToneMapOptions','CodecConfigurations','EnableThrottling','ExtractionThreadCount','CodecProfileLimits','SubtitleOptions']
    AND runtime_overrides - ARRAY['Network','Hardware','Threads','H264','HEVC','SoftwareToneMapping','VulkanToneMapping','Transcoding','TranscodingTempPath','SoftwareToneMapOptions','CodecConfigurations','EnableThrottling','ExtractionThreadCount','CodecProfileLimits','SubtitleOptions']::text[] = '{}'::jsonb
    AND octet_length(runtime_overrides::text) <= 16384
    AND jsonb_typeof(runtime_overrides->'EnableThrottling') IN ('boolean','null')
    AND (runtime_overrides->'ExtractionThreadCount' = 'null'::jsonb OR CASE
        WHEN jsonb_typeof(runtime_overrides->'ExtractionThreadCount') = 'number'
            AND runtime_overrides->>'ExtractionThreadCount' ~ '^[0-9]{1,2}$'
        THEN (runtime_overrides->>'ExtractionThreadCount')::integer BETWEEN 0 AND 16 ELSE false END)
    AND (runtime_overrides->'CodecProfileLimits' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'CodecProfileLimits') = 'object'
        AND runtime_overrides->'CodecProfileLimits' ?& ARRAY['H264','HEVC']
        AND (runtime_overrides->'CodecProfileLimits') - ARRAY['H264','HEVC']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'CodecProfileLimits'->'H264') = 'string'
        AND runtime_overrides->'CodecProfileLimits'->>'H264' IN ('baseline','main','high')
        AND jsonb_typeof(runtime_overrides->'CodecProfileLimits'->'HEVC') = 'string'
        AND runtime_overrides->'CodecProfileLimits'->>'HEVC' IN ('main','main10')))
    AND (runtime_overrides->'SubtitleOptions' = 'null'::jsonb OR (
        jsonb_typeof(runtime_overrides->'SubtitleOptions') = 'object'
        AND runtime_overrides->'SubtitleOptions' ?& ARRAY['AllowAssToText']
        AND (runtime_overrides->'SubtitleOptions') - ARRAY['AllowAssToText']::text[] = '{}'::jsonb
        AND jsonb_typeof(runtime_overrides->'SubtitleOptions'->'AllowAssToText') = 'boolean'))
$current$;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO STRICT legacy_definition
    FROM pg_constraint WHERE conrelid = 'managed_settings'::regclass
        AND conname = 'managed_runtime_overrides_shape' AND contype = 'c';
    IF left(legacy_definition, 7) <> 'CHECK (' THEN
        RAISE EXCEPTION 'managed runtime legacy constraint cannot be extended';
    END IF;
    -- Only the known column token in the published expression is replaced.
    -- Removing the new top-level keys yields the exact historical eleven groups.
    projected_definition := replace(substring(legacy_definition FROM 7),
        'runtime_overrides',
        '(runtime_overrides - ARRAY[''EnableThrottling'',''ExtractionThreadCount'',''CodecProfileLimits'',''SubtitleOptions'']::text[])');
    ALTER TABLE managed_settings DROP CONSTRAINT managed_runtime_overrides_shape;
    EXECUTE format('ALTER TABLE managed_settings ADD CONSTRAINT managed_runtime_overrides_shape CHECK ((%s) OR ((%s) AND (%s)))',
        substring(legacy_definition FROM 7), current_definition, projected_definition);
END $migration$;

ALTER TABLE managed_settings ALTER COLUMN runtime_overrides SET DEFAULT
    '{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null,"Transcoding":null,"TranscodingTempPath":null,"SoftwareToneMapOptions":null,"CodecConfigurations":null,"EnableThrottling":null,"ExtractionThreadCount":null,"CodecProfileLimits":null,"SubtitleOptions":null}'::jsonb;
