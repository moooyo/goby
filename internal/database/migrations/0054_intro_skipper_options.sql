-- Add upstream matcher options without changing historical admission authority,
-- configuration revisions, publication epochs, or existing analysis results.
ALTER TABLE analysis_settings ADD COLUMN intro_skipper_options jsonb NOT NULL DEFAULT
    '{"AnalysisPercent":25,"AnalysisLengthLimit":10,"MinimumIntroDuration":15,"MaximumIntroDuration":120,"MaximumFingerprintPointDifferences":6,"MaximumTimeSkip":3.5,"InvertedIndexShift":2}'::jsonb;
ALTER TABLE analysis_settings ADD CONSTRAINT analysis_settings_intro_skipper_options_check CHECK (
    jsonb_typeof(intro_skipper_options)='object'
    AND octet_length(intro_skipper_options::text)<=4096
    AND intro_skipper_options ?& ARRAY['AnalysisPercent','AnalysisLengthLimit','MinimumIntroDuration','MaximumIntroDuration','MaximumFingerprintPointDifferences','MaximumTimeSkip','InvertedIndexShift']
    AND intro_skipper_options-'AnalysisPercent'-'AnalysisLengthLimit'-'MinimumIntroDuration'-'MaximumIntroDuration'-'MaximumFingerprintPointDifferences'-'MaximumTimeSkip'-'InvertedIndexShift'='{}'::jsonb
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'AnalysisPercent')='number'
        AND (intro_skipper_options->>'AnalysisPercent') ~ '^[0-9]+$'
        THEN (intro_skipper_options->>'AnalysisPercent')::numeric BETWEEN 1 AND 50 ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'AnalysisLengthLimit')='number'
        AND (intro_skipper_options->>'AnalysisLengthLimit') ~ '^[0-9]+$'
        THEN (intro_skipper_options->>'AnalysisLengthLimit')::numeric BETWEEN 1 AND 10 ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'MinimumIntroDuration')='number'
        AND (intro_skipper_options->>'MinimumIntroDuration') ~ '^[0-9]+$'
        THEN (intro_skipper_options->>'MinimumIntroDuration')::numeric BETWEEN 1 AND 600 ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'MaximumIntroDuration')='number'
        AND (intro_skipper_options->>'MaximumIntroDuration') ~ '^[0-9]+$'
        THEN (intro_skipper_options->>'MaximumIntroDuration')::numeric BETWEEN 1 AND 600 ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'MinimumIntroDuration')='number'
        AND jsonb_typeof(intro_skipper_options->'MaximumIntroDuration')='number'
        THEN (intro_skipper_options->>'MaximumIntroDuration')::numeric >= (intro_skipper_options->>'MinimumIntroDuration')::numeric ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'MaximumFingerprintPointDifferences')='number'
        AND (intro_skipper_options->>'MaximumFingerprintPointDifferences') ~ '^[0-9]+$'
        THEN (intro_skipper_options->>'MaximumFingerprintPointDifferences')::numeric BETWEEN 0 AND 32 ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'MaximumTimeSkip')='number'
        THEN (intro_skipper_options->>'MaximumTimeSkip')::numeric BETWEEN 0 AND 30 ELSE false END
    AND CASE WHEN jsonb_typeof(intro_skipper_options->'InvertedIndexShift')='number'
        AND (intro_skipper_options->>'InvertedIndexShift') ~ '^[0-9]+$'
        THEN (intro_skipper_options->>'InvertedIndexShift')::numeric BETWEEN 0 AND 32 ELSE false END
);
