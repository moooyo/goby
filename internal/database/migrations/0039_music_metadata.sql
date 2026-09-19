-- Audio track and disc numbers are descriptive music metadata, not physical
-- catalog ancestry. Preserve all existing identities, controls and cached facts;
-- the next accepted scan/edit materializes these fields through the same merge.
CREATE OR REPLACE FUNCTION catalog_metadata_automatic_values(
    p_name text, p_sort_name text, p_overview text, p_type text,
    p_index integer, p_parent_index integer, p_source jsonb
) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT source || jsonb_build_object(
        'Name', p_name, 'SortName', p_sort_name, 'Overview', p_overview,
        'OriginalTitle', COALESCE(source->>'OriginalTitle', ''),
        'OfficialRating', COALESCE(source->>'OfficialRating', ''),
        'ProductionYear', source->'ProductionYear',
        'PremiereDate', source->'PremiereDate',
        'CommunityRating', source->'CommunityRating',
        'ProviderIDs', CASE WHEN jsonb_typeof(source->'ProviderIDs') = 'object' THEN source->'ProviderIDs' ELSE '{}'::jsonb END,
        'Genres', CASE WHEN jsonb_typeof(source->'Genres') = 'array' THEN source->'Genres' ELSE '[]'::jsonb END,
        'Tags', CASE WHEN jsonb_typeof(source->'Tags') = 'array' THEN source->'Tags' ELSE '[]'::jsonb END,
        'Studios', CASE WHEN jsonb_typeof(source->'Studios') = 'array' THEN source->'Studios' ELSE '[]'::jsonb END,
        'People', CASE WHEN jsonb_typeof(source->'People') = 'array' THEN source->'People' ELSE '[]'::jsonb END,
        'IndexNumber', CASE WHEN p_type IN ('Season', 'Episode') THEN p_index
            WHEN p_type = 'Audio' THEN CASE WHEN source ? 'IndexNumber' THEN
                CASE WHEN jsonb_typeof(source->'IndexNumber') = 'number' THEN
                    CASE WHEN (source->>'IndexNumber')::numeric BETWEEN 0 AND 2147483647
                        AND trunc((source->>'IndexNumber')::numeric) = (source->>'IndexNumber')::numeric
                        THEN ((source->>'IndexNumber')::numeric)::integer END
                END ELSE NULLIF(p_index, 0) END END,
        'ParentIndexNumber', CASE WHEN p_type = 'Episode' THEN p_parent_index
            WHEN p_type = 'Audio' THEN CASE WHEN source ? 'ParentIndexNumber' THEN
                CASE WHEN jsonb_typeof(source->'ParentIndexNumber') = 'number' THEN
                    CASE WHEN (source->>'ParentIndexNumber')::numeric BETWEEN 0 AND 2147483647
                        AND trunc((source->>'ParentIndexNumber')::numeric) = (source->>'ParentIndexNumber')::numeric
                        THEN ((source->>'ParentIndexNumber')::numeric)::integer END
                END ELSE NULLIF(p_parent_index, 0) END END)
    FROM (SELECT CASE WHEN jsonb_typeof(p_source) = 'object' THEN p_source ELSE '{}'::jsonb END AS source) normalized;
$$;
