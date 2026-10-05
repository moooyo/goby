package library

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestVideoNavigationFiltersUseSourceFactsBeforeCountAndPaging(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	video := func(width, height int, transfer string) media.Stream {
		return media.Stream{CodecType: "video", Width: width, Height: height, ColorTransfer: transfer}
	}
	attached := video(4096, 2160, "smpte2084")
	attached.IsAttachedPicture = true
	dolby := video(1920, 1080, "smpte2084")
	dolby.VideoRange, dolby.VideoRangeKnown = "DOVI", true
	for id, streams := range map[string][]media.Stream{
		"movie-a":        {video(3840, 2160, "smpte2084")},
		"movie-b":        {video(3840, 1600, "bt709")},
		"episode-b1":     {video(1920, 1080, "smpte2084")},
		"episode-b2":     {video(3800, 2100, "arib-std-b67")},
		"boundary3798":   {video(3798, 2100, "bt709")},
		"boundary3800":   {video(3800, 1600, "bt709")},
		"portrait":       {video(1920, 2160, "bt709")},
		"attached":       {attached},
		"unknown":        {video(0, 0, "")},
		"mixed":          {video(3840, 2160, "bt709"), video(1920, 1080, "smpte2084")},
		"mixed-hd-first": {video(1920, 1080, "smpte2084"), video(3840, 2160, "bt709")},
		"dolby":          {dolby},
	} {
		encoded, err := json.Marshal(media.Info{Streams: streams})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
			VALUES($1,'library-b','library-b',$1,$1,'Movie') ON CONFLICT(id) DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, "UPDATE items SET media=$2 WHERE id=$1", id, encoded); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	for _, test := range []struct {
		name  string
		query Query
		want  []string
	}{
		{"4K width boundary", Query{Is4K: &yes}, []string{"boundary3800", "episode-b2", "mixed", "movie-b"}},
		{"non-4K uses primary width and excludes unknown width", Query{Is4K: &no}, []string{"boundary3798", "dolby", "episode-b1", "mixed-hd-first", "portrait"}},
		{"PQ", Query{ExtendedVideoTypes: []string{"Hdr10"}}, []string{"episode-b1", "mixed", "mixed-hd-first"}},
		{"HLG", Query{ExtendedVideoTypes: []string{"HyperLogGamma"}}, []string{"episode-b2"}},
		{"DV takes precedence over PQ", Query{ExtendedVideoTypes: []string{"DolbyVision"}}, []string{"dolby"}},
		{"known SDR", Query{ExtendedVideoTypes: []string{"None"}}, []string{"boundary3798", "boundary3800", "mixed", "mixed-hd-first", "movie-b", "portrait"}},
		{"unknown dynamic subtype", Query{ExtendedVideoTypes: []string{"Hdr10Plus"}}, []string{}},
		{"OR within enum", Query{ExtendedVideoTypes: []string{"Hdr10", "HyperLogGamma"}}, []string{"episode-b1", "episode-b2", "mixed", "mixed-hd-first"}},
		{"standard primary resolution and any range", Query{Is4K: &yes, ExtendedVideoTypes: []string{"Hdr10", "HyperLogGamma"}}, []string{"episode-b2", "mixed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID, query.Recursive, query.Limit = "restricted", true, 100
			result, err := store.QueryItems(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				ids = append(ids, item.ID)
			}
			slices.Sort(ids)
			if result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(ids, test.want) {
				t.Fatalf("source filter/count/ACL = %v (%d), want %v", ids, result.TotalRecordCount, test.want)
			}
			query.StartIndex, query.Limit = len(test.want), 1
			page, err := store.QueryItems(ctx, query)
			if err != nil || page.TotalRecordCount != len(test.want) || len(page.Items) != 0 {
				t.Fatalf("source filter count depended on the selected page: %+v, %v", page, err)
			}
		})
	}
}

func TestVideoNavigationAggregationIsExplicitAndPreservesDescendantScope(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET root_id='root-b',relative_path=id || '.mp4' WHERE id IN ('episode-b1','episode-b2');
		UPDATE items SET media='{"Streams":[{"CodecType":"video","Width":1920,"Height":1080,"ColorTransfer":"smpte2084"}]}' WHERE id='episode-b1';
		UPDATE items SET media='{"Streams":[{"CodecType":"video","Width":3840,"Height":2160,"ColorTransfer":"bt709"}]}' WHERE id='episode-b2';
		UPDATE items SET root_id='root-c',media='{"Streams":[{"CodecType":"video","Width":3840,"ColorTransfer":"smpte2084"}]}' WHERE id='cross-library-child'`); err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	assertCount := func(query Query, count int) {
		t.Helper()
		query.UserID, query.Recursive, query.Limit = "restricted", true, 1
		query.IncludeItemTypes = []string{"Series", "Season"}
		result, err := store.QueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != count || len(result.Items) != min(count, 1) {
			t.Fatalf("aggregate scope/count = %+v, %v; want %d", result, err, count)
		}
		query.StartIndex = count
		result, err = store.QueryItems(ctx, query)
		if err != nil || result.TotalRecordCount != count || len(result.Items) != 0 {
			t.Fatalf("aggregate count depended on page = %+v, %v", result, err)
		}
	}
	assertCount(Query{Is4K: &yes}, 0)
	assertCount(Query{Is4K: &no}, 0)
	assertCount(Query{ExtendedVideoTypes: []string{"Hdr10"}}, 0)
	assertCount(Query{Is4K: &yes, GobyAggregateVideoFilters: &yes}, 2)
	assertCount(Query{Is4K: &no, GobyAggregateVideoFilters: &yes}, 2)
	combined := Query{Is4K: &yes, ExtendedVideoTypes: []string{"Hdr10"}, GobyAggregateVideoFilters: &yes}
	// Separate 4K and HDR episodes cannot combine into one qualifying source.
	assertCount(combined, 0)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"Streams":[{"CodecType":"video","Width":3840,"ColorTransfer":"bt709"},{"CodecType":"video","Width":1920,"ColorTransfer":"smpte2084"}]}' WHERE id='episode-b2'`); err != nil {
		t.Fatal(err)
	}
	// The explicit aggregate also cannot combine distinct tracks of one episode.
	assertCount(combined, 0)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"Streams":[{"CodecType":"video","Width":3840,"Height":2160,"ColorTransfer":"smpte2084"}]}' WHERE id='episode-b2'`); err != nil {
		t.Fatal(err)
	}
	assertCount(combined, 2)
	for _, test := range []struct{ name, setup, restore string }{
		{"foreign root", `UPDATE items SET root_id='root-a' WHERE id='episode-b2'`, `UPDATE items SET root_id='root-b' WHERE id='episode-b2'`},
		{"excluded descendant", `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"ExcludedSubFolders":["episode-b2"]}' WHERE id='restricted'`, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"]}' WHERE id='restricted'`},
		{"reserved theme", `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES('root-b','episode-b2.mp4',false)`, `DELETE FROM theme_reserved_paths WHERE root_id='root-b' AND relative_path='episode-b2.mp4'`},
		{"reserved extra", `INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('root-b','episode-b2.mp4',false)`, `DELETE FROM extra_reserved_paths WHERE root_id='root-b' AND relative_path='episode-b2.mp4'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.pool.Exec(ctx, test.setup); err != nil {
				t.Fatal(err)
			}
			assertCount(combined, 0)
			if _, err := store.pool.Exec(ctx, test.restore); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Traversal must terminate even if corrupt parent rows form a cycle.
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_id='season-b' WHERE id='series-b'`); err != nil {
		t.Fatal(err)
	}
	assertCount(combined, 2)
}

func TestVideoNavigationRejectsUnknownEnumsAndCanonicalizesKnownValues(t *testing.T) {
	for _, value := range [][]string{{"HDR"}, {"Hlg"}, {"Dovi"}, {""}, {"None", "None", "None", "None", "None", "None"}} {
		if _, err := normalizeNavigationFilters(Query{ExtendedVideoTypes: value}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unsupported video selector accepted: %v, %v", value, err)
		}
	}
	query, err := normalizeNavigationFilters(Query{ExtendedVideoTypes: []string{" hdr10 ", "HYPERLOGGAMMA", "Hdr10"}})
	if err != nil || !reflect.DeepEqual(query.ExtendedVideoTypes, []string{"Hdr10", "HyperLogGamma"}) {
		t.Fatalf("video selector normalization = %+v, %v", query, err)
	}
}
