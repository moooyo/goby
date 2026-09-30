-- Refinement features need a larger per-entry payload while retaining the
-- existing aggregate cache budget, row identity, and historical cache bytes.
ALTER TABLE analysis_feature_cache DROP CONSTRAINT analysis_feature_cache_payload_check;
ALTER TABLE analysis_feature_cache ADD CONSTRAINT analysis_feature_cache_payload_check
    CHECK (octet_length(payload) BETWEEN 1 AND 524288);
