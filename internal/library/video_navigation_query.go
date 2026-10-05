package library

import (
	"fmt"
	"strings"
)

func navigationStreamsSQL(item string) string {
	return "jsonb_array_elements(CASE WHEN jsonb_typeof(" + item + ".media->'Streams')='array' THEN " + item + ".media->'Streams' ELSE '[]'::jsonb END)"
}

// Extended video types describe stored source facts, independently of decoder
// support. Unknown color data is not classified as SDR. The current prober has
// no HDR10+ side-data fact, so Hdr10Plus alone cannot match an invented subtype.
const navigationExtendedVideoTypeSQL = `(CASE
	WHEN stream->>'VideoRangeKnown'='true' THEN CASE upper(stream->>'VideoRange')
		WHEN 'DOVI' THEN 'DolbyVision' WHEN 'HDR10' THEN 'Hdr10'
		WHEN 'HLG' THEN 'HyperLogGamma' WHEN 'SDR' THEN 'None' END
	WHEN jsonb_typeof(stream->'DolbyVision')='object' THEN 'DolbyVision'
	ELSE CASE lower(stream->>'ColorTransfer')
		WHEN 'smpte2084' THEN 'Hdr10' WHEN 'arib-std-b67' THEN 'HyperLogGamma'
		WHEN 'bt709' THEN 'None' WHEN 'smpte170m' THEN 'None' WHEN 'iec61966-2-1' THEN 'None' END
END)`

func addVideoNavigationConditions(query Query, conditions []string, args []any) ([]string, []any) {
	if query.Is4K == nil && len(query.ExtendedVideoTypes) == 0 {
		return conditions, args
	}
	streamConditions := []string{"stream->>'CodecType'='video'", "stream->>'IsAttachedPicture' IS DISTINCT FROM 'true'"}
	selfConditions := []string{"NOT i.is_folder"}
	if query.Is4K != nil {
		args = append(args, *query.Is4K)
		width := navigationNumberSQL("stream", "Width")
		// Official Emby 4.9.5.0 uses the 3800-pixel width boundary, including
		// cropped scope video. A portrait height alone does not establish 4K.
		streamConditions = append(streamConditions, width+">0", fmt.Sprintf("(%s>=3800)=$%d::boolean", width, len(args)))
		primaryWidth := navigationNumberSQL("primary_stream", "Width")
		selfConditions = append(selfConditions, fmt.Sprintf(`(SELECT CASE WHEN %s>0 THEN %s>=3800 END
			FROM %s WITH ORDINALITY AS primary_source(primary_stream, position)
			WHERE primary_stream->>'CodecType'='video' AND primary_stream->>'IsAttachedPicture' IS DISTINCT FROM 'true'
			ORDER BY position LIMIT 1)=$%d::boolean`, primaryWidth, primaryWidth, navigationStreamsSQL("i"), len(args)))
	}
	if len(query.ExtendedVideoTypes) > 0 {
		args = append(args, query.ExtendedVideoTypes)
		rangeCondition := fmt.Sprintf("%s=ANY($%d::text[])", navigationExtendedVideoTypeSQL, len(args))
		streamConditions = append(streamConditions, rangeCondition)
		selfConditions = append(selfConditions, "EXISTS (SELECT 1 FROM "+navigationStreamsSQL("i")+" stream WHERE stream->>'CodecType'='video' AND stream->>'IsAttachedPicture' IS DISTINCT FROM 'true' AND "+rangeCondition+")")
	}
	match := strings.Join(streamConditions, " AND ")
	// The official server takes resolution from the primary video stream, but
	// extended range selectors inspect all video streams. Preserve that contract
	// for ordinary item queries, including files with differently sized tracks.
	self := "(" + strings.Join(selfConditions, " AND ") + ")"
	if query.GobyAggregateVideoFilters == nil || !*query.GobyAggregateVideoFilters {
		return append(conditions, self), args
	}
	// This is an explicit Goby extension. Standard Emby filters inspect the
	// item's own video stream, so Series and Season do not normally match.
	// Reusing the policy-scoped child/leaf aliases prevents hidden descendants,
	// cross-library edges and permanently reserved attachments from leaking.
	descendants := `(EXISTS (WITH RECURSIVE video_descendants AS (
		SELECT child.id, child.library_id, child.is_folder FROM items child
		WHERE child.parent_id=i.id AND child.library_id=i.library_id AND ` + ordinaryItemSQL("child") + `
		UNION
		SELECT child.id, child.library_id, child.is_folder FROM items child
		JOIN video_descendants parent ON child.parent_id=parent.id AND child.library_id=parent.library_id
		WHERE parent.is_folder AND child.library_id=i.library_id AND ` + ordinaryItemSQL("child") + `
	) SELECT 1 FROM video_descendants descendant JOIN items leaf ON leaf.id=descendant.id
	JOIN library_roots video_root ON video_root.id=leaf.root_id AND video_root.library_id=leaf.library_id
	CROSS JOIN LATERAL ` + navigationStreamsSQL("leaf") + ` stream
	WHERE leaf.type='Episode' AND NOT leaf.is_folder AND leaf.library_id=i.library_id AND ` + ordinaryItemSQL("leaf") + ` AND ` + match + `))`
	return append(conditions, "(CASE WHEN i.type IN ('Series','Season') AND i.is_folder THEN "+descendants+" ELSE "+self+" END)"), args
}
