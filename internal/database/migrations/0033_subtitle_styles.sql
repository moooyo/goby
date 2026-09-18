-- Styled sidecars retain the same immutable stream identity and size bounds as
-- SRT/WebVTT. ASS and SSA share the established text/x-ssa media type.
ALTER TABLE item_subtitles
    DROP CONSTRAINT item_subtitles_codec_check,
    DROP CONSTRAINT item_subtitles_mime_type_check,
    DROP CONSTRAINT item_subtitles_check;

ALTER TABLE item_subtitles
    ADD CONSTRAINT item_subtitles_codec_check CHECK (codec IN ('srt', 'vtt', 'ass', 'ssa')),
    ADD CONSTRAINT item_subtitles_mime_type_check CHECK (mime_type IN ('application/x-subrip', 'text/vtt', 'text/x-ssa')),
    ADD CONSTRAINT item_subtitles_check CHECK (
        (codec = 'srt' AND mime_type = 'application/x-subrip') OR
        (codec = 'vtt' AND mime_type = 'text/vtt') OR
        (codec IN ('ass', 'ssa') AND mime_type = 'text/x-ssa')
    );
