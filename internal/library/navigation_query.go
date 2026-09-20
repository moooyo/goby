package library

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func normalizeNavigationFilters(query Query) (Query, error) {
	if len(query.ExcludeItemTypes) > 32 || len(query.Years) > 256 {
		return Query{}, ErrInvalidInput
	}
	var err error
	query.ExcludeItemTypes, err = normalizeQueryValues(query.ExcludeItemTypes, map[string]string{
		"collectionfolder": "CollectionFolder", "folder": "Folder", "movie": "Movie", "series": "Series", "season": "Season", "episode": "Episode",
		"video": "Video", "audio": "Audio", "musicalbum": "MusicAlbum", "musicartist": "MusicArtist", "playlist": "Playlist", "boxset": "BoxSet", "musicvideo": "MusicVideo", "trailer": "Trailer",
	})
	if err != nil || len(query.ExcludeItemTypes) > 32 || len(query.Years) > 256 {
		return Query{}, ErrInvalidInput
	}
	years, seen := make([]int, 0, len(query.Years)), make(map[int]bool)
	for _, year := range query.Years {
		if year < 1 || year > 9999 {
			return Query{}, ErrInvalidInput
		}
		if !seen[year] {
			years = append(years, year)
			seen[year] = true
		}
	}
	query.Years = years
	for _, value := range []string{query.NameStartsWith, query.NameStartsWithOrGreater, query.NameLessThan} {
		if len(value) > 256 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return Query{}, ErrInvalidInput
		}
	}
	for _, interval := range [][2]*time.Time{{query.MinPremiereDate, query.MaxPremiereDate}, {query.MinDateCreated, query.MaxDateCreated}} {
		for _, value := range interval {
			if value != nil && (value.Year() < 1 || value.Year() > 9999) {
				return Query{}, ErrInvalidInput
			}
		}
		if interval[0] != nil && interval[1] != nil && interval[0].After(*interval[1]) {
			return Query{}, ErrInvalidInput
		}
	}
	if value := query.MinCommunityRating; value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 10) {
		return Query{}, ErrInvalidInput
	}
	return query, nil
}

func navigationNumberSQL(object, field string) string {
	return "(CASE WHEN jsonb_typeof(" + object + "->'" + field + "')='number' THEN (" + object + "->>'" + field + "')::numeric END)"
}

func navigationPremiereSQL() string {
	return "(CASE WHEN jsonb_typeof(" + itemMetadataColumn + "->'PremiereDate')='string' THEN NULLIF(" + itemMetadataColumn + "->>'PremiereDate','')::timestamptz END)"
}

// addNavigationConditions uses only package-owned SQL fragments. Values remain
// parameters and missing metadata is never substituted with a matching default.
func addNavigationConditions(query Query, conditions []string, args []any) ([]string, []any) {
	add := func(expression, operator, cast string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s %s $%d%s", expression, operator, len(args), cast))
	}
	if len(query.ExcludeItemTypes) > 0 {
		typeSQL := "(CASE WHEN EXISTS(SELECT 1 FROM item_extra_resources excluded_extra WHERE excluded_extra.resource_item_id=i.id AND excluded_extra.active AND excluded_extra.kind='trailer') THEN 'Trailer' ELSE i.type END)"
		add(typeSQL, "<> ALL(", "::text[])", query.ExcludeItemTypes)
	}
	if len(query.Years) > 0 {
		add(navigationNumberSQL(itemMetadataColumn, "ProductionYear"), "= ANY(", "::integer[])", query.Years)
	}
	if query.MinCommunityRating != nil {
		add(navigationNumberSQL(itemMetadataColumn, "CommunityRating"), ">=", "::numeric", *query.MinCommunityRating)
	}
	for _, field := range []struct {
		value                *time.Time
		expression, operator string
	}{
		{query.MinPremiereDate, navigationPremiereSQL(), ">="}, {query.MaxPremiereDate, navigationPremiereSQL(), "<="},
		{query.MinDateCreated, "i.created_at", ">="}, {query.MaxDateCreated, "i.created_at", "<="},
	} {
		if field.value != nil {
			add(field.expression, field.operator, "::timestamptz", field.value.UTC())
		}
	}
	if query.NameStartsWith != "" {
		args = append(args, escapeLikeLiteral(query.NameStartsWith)+"%")
		conditions = append(conditions, fmt.Sprintf("i.name ILIKE $%d ESCAPE E'\\\\'", len(args)))
	}
	if query.NameStartsWithOrGreater != "" {
		add("lower(i.name)", ">=", "::text", strings.ToLower(query.NameStartsWithOrGreater))
	}
	if query.NameLessThan != "" {
		add("lower(i.name)", "<", "::text", strings.ToLower(query.NameLessThan))
	}
	if query.HasOverview != nil {
		add("(btrim(i.overview)<>'')", "=", "::boolean", *query.HasOverview)
	}
	streams := "jsonb_array_elements(CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN i.media->'Streams' ELSE '[]'::jsonb END)"
	if query.HasSubtitles != nil {
		predicate := "(EXISTS(SELECT 1 FROM item_subtitles subtitle JOIN library_roots subtitle_root ON subtitle_root.id=subtitle.root_id AND subtitle_root.library_id=i.library_id " +
			"WHERE subtitle.item_id=i.id AND subtitle.root_id=i.root_id AND subtitle.active AND NOT i.is_folder AND i.media IS NOT NULL " +
			"AND subtitle.stream_index>COALESCE((SELECT max(" + navigationNumberSQL("embedded", "Index") + ") FROM " + streams + " embedded),-1)) " +
			"OR EXISTS(SELECT 1 FROM item_owned_subtitles subtitle JOIN library_roots subtitle_root ON subtitle_root.id=subtitle.root_id AND subtitle_root.library_id=i.library_id " +
			"WHERE subtitle.item_id=i.id AND subtitle.root_id=i.root_id AND subtitle.active AND NOT i.is_folder AND i.media IS NOT NULL " +
			"AND subtitle.source_revision=" + ownedSubtitleSourceRevisionSQL + " " +
			"AND subtitle.stream_index>COALESCE((SELECT max(" + navigationNumberSQL("embedded", "Index") + ") FROM " + streams + " embedded),-1)) " +
			"OR EXISTS(SELECT 1 FROM " + streams + " stream WHERE stream->>'CodecType'='subtitle'))"
		add(predicate, "=", "::boolean", *query.HasSubtitles)
	}
	if query.IsHD != nil {
		predicate := "EXISTS(SELECT 1 FROM " + streams + " stream WHERE stream->>'CodecType'='video' AND stream->>'IsAttachedPicture' IS DISTINCT FROM 'true' AND (" +
			navigationNumberSQL("stream", "Width") + ">=1280 OR " + navigationNumberSQL("stream", "Height") + ">=720))"
		add("("+predicate+")", "=", "::boolean", *query.IsHD)
	}
	conditions, args = addExpectedEpisodeConditions(query, conditions, args)
	return conditions, args
}
