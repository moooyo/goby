package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func themeTestItem(t *testing.T, ctx context.Context, store *Store, id, kind, parent, libraryID, relative string) {
	t.Helper()
	folder := kind == "Series" || kind == "Season" || kind == "Folder" || kind == "MusicAlbum" || kind == "CollectionFolder"
	if _, err := store.pool.Exec(ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,media)
		SELECT $1,$2,r.id,$3,$1,$1,$4,$5,r.path||'/'||$6,$6,
			CASE WHEN $5 THEN NULL ELSE '{"Container":"mp3","DurationTicks":120000000,
			"Streams":[{"Index":0,"CodecType":"audio","Codec":"mp3"}]}'::jsonb END
		FROM library_roots r WHERE r.library_id=$2`, id, libraryID, parent, kind, folder, relative); err != nil {
		t.Fatalf("insert theme item %s: %v", id, err)
	}
}

func themeTestResource(t *testing.T, ctx context.Context, store *Store, id, kind, owner, libraryID, relative string) {
	t.Helper()
	itemType := "Audio"
	if kind == "video" {
		itemType = "Video"
	}
	themeTestItem(t, ctx, store, id, itemType, owner, libraryID, relative)
	if _, err := store.pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id=$1`, id); err != nil {
		t.Fatalf("reserve theme resource path %s: %v", id, err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES($1,$2,$3,true)`,
		id, owner, kind); err != nil {
		t.Fatalf("insert theme resource relationship %s: %v", id, err)
	}
}

func themeTestFixture(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, item := range []struct{ id, kind, parent, relative string }{
		{"theme-seed", "Movie", "library-b", "Seed/movie.mp4"},
		{"theme-all", "Movie", "library-b", "All/movie.mp4"},
		{"theme-video-owner", "Movie", "library-b", "Video/movie.mp4"},
		{"theme-series", "Series", "library-b", "Series"},
		{"theme-season1", "Season", "theme-series", "Series/Season 01"},
		{"theme-season2", "Season", "theme-series", "Series/Season 02"},
		{"theme-episode1", "Episode", "theme-season1", "Series/Season 01/E01.mp4"},
		{"theme-episode2", "Episode", "theme-season2", "Series/Season 02/E01.mp4"},
	} {
		themeTestItem(t, ctx, store, item.id, item.kind, item.parent, "library-b", item.relative)
	}
	for _, resource := range []struct{ id, kind, owner, relative string }{
		{"theme-song", "song", "theme-seed", "Seed/theme.mp3"},
		{"theme-one", "song", "theme-all", "All/theme-music/one.mp3"},
		{"theme-two", "song", "theme-all", "All/theme-music/two.flac"},
		{"theme-video", "video", "theme-video-owner", "Video/backdrops/video.mp4"},
		{"theme-series-song", "song", "theme-series", "Series/theme.mp3"},
		{"theme-season-song", "song", "theme-season2", "Series/Season 02/theme-music/season.mp3"},
	} {
		themeTestResource(t, ctx, store, resource.id, resource.kind, resource.owner, "library-b", resource.relative)
	}
	themeTestItem(t, ctx, store, "theme-hidden", "Movie", "library-a", "library-a", "Hidden/movie.mp4")
	themeTestResource(t, ctx, store, "theme-hidden-song", "song", "theme-hidden", "library-a", "Hidden/theme.mp3")
	return ctx, store
}

func themeTestOwner(t *testing.T, ctx context.Context, store *Store, item string) int64 {
	t.Helper()
	var id int64
	if err := store.pool.QueryRow(ctx, `SELECT id FROM theme_owner_ids
		WHERE item_id=$1 OR ($1=$2 AND virtual_root)`, item, VirtualRootItemID).Scan(&id); err != nil || id <= 0 {
		t.Fatalf("read durable theme owner for %s: %v", item, err)
	}
	return id
}

func themeTestQuery(t *testing.T, ctx context.Context, store *Store, seed string, query ThemeQuery) ThemeMediaResult {
	t.Helper()
	result, err := store.QueryThemeMedia(ctx, seed, query)
	if err != nil {
		t.Fatalf("query theme media for %s: %v", seed, err)
	}
	for _, group := range []ThemeResult{result.ThemeSongsResult, result.ThemeVideosResult, result.SoundtrackSongsResult} {
		if group.Items == nil || group.TotalRecordCount != len(group.Items) {
			t.Fatal("theme group omitted its non-null complete array or accurate count")
		}
	}
	if result.SoundtrackSongsResult.OwnerID != 0 || result.SoundtrackSongsResult.TotalRecordCount != 0 {
		t.Fatal("unindexed soundtrack relationships acquired fabricated matches")
	}
	return result
}

func themeTestState(t *testing.T, ctx context.Context, store *Store) string {
	t.Helper()
	var state string
	if err := store.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'owners',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM theme_owner_ids o),
		'owner_sequence',(SELECT jsonb_build_array(last_value,is_called) FROM theme_owner_ids_id_seq),
		'reserved',(SELECT jsonb_agg(to_jsonb(p) ORDER BY root_id,relative_path) FROM theme_reserved_paths p),
		'resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_theme_resources r),
		'userdata',(SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id) FROM user_item_data u),
		'subtitles',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id,stream_index) FROM item_subtitles s),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
		'credits',(SELECT jsonb_agg(to_jsonb(e) ORDER BY item_id,entity_id,credit_group,position) FROM item_entities e),
		'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s))::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestStoreThemeMediaMatchesRecordedDirectInheritedAndDisabledGroups(t *testing.T) {
	ctx, store := themeTestFixture(t)
	before := themeTestState(t, ctx, store)
	for _, test := range []struct {
		seed                   string
		inherit, songs, videos bool
		songOwner, videoOwner  string
		songIDs, videoIDs      []string
	}{
		{"theme-seed", false, true, true, "theme-seed", "theme-seed", []string{"theme-song"}, []string{}},
		{"theme-all", false, true, true, "theme-all", "theme-all", []string{"theme-one", "theme-two"}, []string{}},
		{"theme-video-owner", false, true, true, "theme-video-owner", "theme-video-owner", []string{}, []string{"theme-video"}},
		{"theme-season1", false, true, true, "theme-season1", "theme-season1", []string{}, []string{}},
		{"theme-season2", false, true, true, "theme-season2", "theme-season2", []string{"theme-season-song"}, []string{}},
		{"theme-episode1", true, true, true, "theme-series", VirtualRootItemID, []string{"theme-series-song"}, []string{}},
		{"theme-episode2", true, true, true, "theme-season2", VirtualRootItemID, []string{"theme-season-song"}, []string{}},
		{"theme-seed", true, true, true, "theme-seed", VirtualRootItemID, []string{"theme-song"}, []string{}},
		{"theme-seed", false, false, true, "", "theme-seed", []string{}, []string{}},
		{"theme-seed", true, false, false, "", "", []string{}, []string{}},
		{VirtualRootItemID, true, true, true, VirtualRootItemID, VirtualRootItemID, []string{}, []string{}},
	} {
		t.Run(fmt.Sprintf("%s/%t/%t/%t", test.seed, test.inherit, test.songs, test.videos), func(t *testing.T) {
			result := themeTestQuery(t, ctx, store, test.seed, ThemeQuery{Subject: Subject{UserID: "restricted"},
				InheritFromParent: test.inherit, EnableThemeSongs: test.songs, EnableThemeVideos: test.videos})
			for index, group := range []ThemeResult{result.ThemeSongsResult, result.ThemeVideosResult} {
				owner := []string{test.songOwner, test.videoOwner}[index]
				var expected int64
				if owner != "" {
					expected = themeTestOwner(t, ctx, store, owner)
				}
				if group.OwnerID != expected || !reflect.DeepEqual(queryItemIDs(group.Items), [][]string{test.songIDs, test.videoIDs}[index]) {
					t.Fatalf("theme owner/items differ: %+v", group)
				}
			}
		})
	}
	if themeTestState(t, ctx, store) != before {
		t.Fatal("theme GET changed durable identifiers, resources, metadata, credentials, or user state")
	}
}

func TestStoreThemeMediaProjectsResourceIdentityUserDataAndSubtitles(t *testing.T) {
	ctx, store := themeTestFixture(t)
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "theme-video", PlayCount: 4, IsFavorite: true})
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"Container":"mp4","DurationTicks":120000000,
		"Streams":[{"Index":0,"CodecType":"video","Codec":"h264"}]}' WHERE id='theme-video';
		INSERT INTO item_subtitles(item_id,root_id,stream_index,relative_path,file_identity,source_hash,file_size,
		modified_at,change_time_ns,codec,language,title,mime_type)
		VALUES('theme-video','root-b',2,'Video/backdrops/video.en.vtt','fixture-only',repeat('a',64),20,
		clock_timestamp(),1,'vtt','en','English','text/vtt')`); err != nil {
		t.Fatal(err)
	}
	before := themeTestState(t, ctx, store)
	result := themeTestQuery(t, ctx, store, "theme-video-owner", ThemeQuery{Subject: Subject{UserID: "restricted"}, EnableThemeVideos: true})
	item := result.ThemeVideosResult.Items[0]
	if item.ID != "theme-video" || item.ParentID != "theme-video-owner" || item.LibraryID != "library-b" ||
		item.Type != "Video" || item.ThemeKind != "video" || item.Path != "/media/b/Video/backdrops/video.mp4" || item.Media == nil ||
		item.Media.Container != "mp4" || item.UserData == nil || item.UserData.PlayCount != 4 || !item.UserData.IsFavorite ||
		len(item.Subtitles) != 1 || item.Subtitles[0].Index != 2 || !item.CanPlay {
		t.Fatalf("theme resource lost its authorized ordinary item projection: %+v", item)
	}
	if themeTestState(t, ctx, store) != before {
		t.Fatal("resource projection updated playback or subtitle state")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"EnableMediaPlayback":false}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	result = themeTestQuery(t, ctx, store, "theme-video-owner", ThemeQuery{Subject: Subject{UserID: "restricted"}, EnableThemeVideos: true})
	if result.ThemeVideosResult.Items[0].CanPlay {
		t.Fatal("theme browsing granted playback forbidden by current account policy")
	}
}

func themeTestMetadata(t *testing.T, ctx context.Context, store *Store, id, source, overrides, locks string) {
	t.Helper()
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "UPDATE items SET local_metadata=$2::jsonb WHERE id=$1", id, source); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE item_metadata_state SET effective=$2::jsonb,overrides=$3::jsonb,locked_values=$4::jsonb
		WHERE item_id=$1`, id, source, overrides, locks); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT sync_catalog_item_entities($1,$2::jsonb)", id, source); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStoreThemeMediaInheritsOnlyGenresAndHonorsResourceControls(t *testing.T) {
	ctx, store := themeTestFixture(t)
	themeTestMetadata(t, ctx, store, "theme-seed", `{"Genres":["Drama","Adventure"],"Tags":["OwnerTag"],
		"Studios":["OwnerStudio"],"ProductionYear":2000,"People":[{"Name":"OwnerActor","Type":"Actor"}]}`, `{}`, `{}`)
	query := ThemeQuery{Subject: Subject{UserID: "restricted"}, EnableThemeSongs: true}
	for _, test := range []struct {
		name, source, overrides, locks string
		genres                         []string
	}{
		{"default", `{"Tags":["ResourceTag"],"Overview":"Resource overview"}`, `{}`, `{}`, []string{"Drama", "Adventure"}},
		{"own source", `{"Genres":["OwnGenre"],"Tags":["ResourceTag"],"Overview":"Resource overview"}`, `{}`, `{}`, []string{"OwnGenre"}},
		{"empty override", `{"Genres":[],"Tags":["ResourceTag"],"Overview":"Resource overview"}`, `{"Genres":[]}`, `{}`, []string{}},
		{"empty saved lock", `{"Genres":[],"Tags":["ResourceTag"],"Overview":"Resource overview"}`, `{}`, `{"Genres":[]}`, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			themeTestMetadata(t, ctx, store, "theme-song", test.source, test.overrides, test.locks)
			before := themeTestState(t, ctx, store)
			item := themeTestQuery(t, ctx, store, "theme-seed", query).ThemeSongsResult.Items[0]
			if item.ThemeKind != "song" || item.Metadata == nil || !reflect.DeepEqual(item.Metadata.Genres, test.genres) ||
				!reflect.DeepEqual(item.Metadata.Tags, []string{"ResourceTag"}) || item.Metadata.Overview != "Resource overview" ||
				item.Metadata.ProductionYear != nil || len(item.Metadata.People) != 0 || len(item.Metadata.Studios) != 0 ||
				len(item.Entities.Genres) != len(test.genres) || len(item.Entities.People) != 0 || len(item.Entities.Studios) != 0 {
				t.Fatalf("theme genre fallback copied other metadata or ignored resource controls: %+v", item)
			}
			for index, ref := range item.Entities.Genres {
				if ref.ID <= 0 || ref.Name != test.genres[index] {
					t.Fatal("theme genre inheritance invented or mismatched a persistent entity identity")
				}
			}
			direct, err := store.GetItemFor(ctx, query.Subject, "theme-song")
			if err != nil || direct.ThemeKind != item.ThemeKind || !reflect.DeepEqual(direct.Metadata, item.Metadata) ||
				!reflect.DeepEqual(direct.Entities.Genres, item.Entities.Genres) {
				t.Fatalf("direct item projection disagrees with ThemeMedia attributes: %v", err)
			}
			if themeTestState(t, ctx, store) != before {
				t.Fatal("dynamic theme inheritance persisted parent metadata or rewrote native controls")
			}
		})
	}
}

func TestStoreThemeMediaAuthorizesDisabledEmptyEntityAndApplicationQueries(t *testing.T) {
	ctx, store := themeTestFixture(t)
	for _, test := range []struct {
		seed, user string
		want       error
	}{
		{"missing", "restricted", ErrNotFound}, {"theme-hidden", "restricted", ErrNotFound},
		{"theme-seed", "none", ErrNotFound}, {"theme-seed", "disabled", ErrForbidden},
		{VirtualRootItemID, "disabled", ErrForbidden},
	} {
		if _, err := store.QueryThemeMedia(ctx, test.seed, ThemeQuery{Subject: Subject{UserID: test.user}}); !errors.Is(err, test.want) {
			t.Fatalf("disabled-group request bypassed seed/subject authority: %v", err)
		}
	}
	if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities('theme-seed','{"Genres":["ThemeGenre"]}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	var entityID int64
	if err := store.pool.QueryRow(ctx, "SELECT id FROM catalog_entities WHERE kind='Genre' AND name='ThemeGenre'").Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		user string
		want error
	}{{"restricted", ErrUnsupportedFilter}, {"none", ErrNotFound}} {
		if _, err := store.QueryThemeMedia(ctx, strconv.FormatInt(entityID, 10), ThemeQuery{Subject: Subject{UserID: test.user}}); !errors.Is(err, test.want) {
			t.Fatalf("numeric entity seed acquired an invented empty item contract: %v", err)
		}
	}
	key := seedCatalogApplicationKey(t, ctx, store.pool, "theme-key", true)
	query := ThemeQuery{Subject: key, EnableThemeSongs: true}
	result := themeTestQuery(t, ctx, store, "theme-hidden", query)
	if len(result.ThemeSongsResult.Items) != 1 || result.ThemeSongsResult.Items[0].UserData != nil || !result.ThemeSongsResult.Items[0].CanPlay {
		t.Fatal("userless application authority lost access or acquired a user's history")
	}
	query.Subject.UserID = "restricted"
	if _, err := store.QueryThemeMedia(ctx, "theme-hidden", query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("application target scope did not hide another library: %v", err)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueryThemeMedia(ctx, "theme-seed", query); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application credential retained theme access: %v", err)
	}
}

func TestStoreThemeMediaInheritanceIsIndependentAndRetiredResourcesStayHidden(t *testing.T) {
	ctx, store := themeTestFixture(t)
	// Video ancestry competition is a symmetric Goby policy, not a claim that
	// the recorded reference sample contains parent/child video competition.
	themeTestResource(t, ctx, store, "theme-series-video", "video", "theme-series", "library-b", "Series/backdrops/video.mp4")
	query := ThemeQuery{Subject: Subject{UserID: "restricted"}, InheritFromParent: true, EnableThemeSongs: true, EnableThemeVideos: true}
	result := themeTestQuery(t, ctx, store, "theme-episode2", query)
	if result.ThemeSongsResult.OwnerID != themeTestOwner(t, ctx, store, "theme-season2") ||
		result.ThemeVideosResult.OwnerID != themeTestOwner(t, ctx, store, "theme-series") {
		t.Fatal("song and video owners were coupled to the same ancestor")
	}
	if _, err := store.pool.Exec(ctx, "UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-season-song'"); err != nil {
		t.Fatal(err)
	}
	result = themeTestQuery(t, ctx, store, "theme-episode2", query)
	if !reflect.DeepEqual(queryItemIDs(result.ThemeSongsResult.Items), []string{"theme-series-song"}) {
		t.Fatal("inactive local song suppressed the actual active ancestor")
	}
	if _, err := store.QueryThemeMedia(ctx, "theme-season-song", query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired resource became an ordinary seed: %v", err)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE items SET parent_id='theme-hidden' WHERE id='theme-season1'"); err != nil {
		t.Fatal(err)
	}
	result = themeTestQuery(t, ctx, store, "theme-episode1", query)
	if result.ThemeSongsResult.TotalRecordCount != 0 || result.ThemeSongsResult.OwnerID != themeTestOwner(t, ctx, store, VirtualRootItemID) {
		t.Fatal("theme inheritance crossed the seed's library boundary")
	}
}

func TestStoreThemeMediaRejectsInvalidDirectRelationshipsAndMissingMappings(t *testing.T) {
	ctx, store := themeTestFixture(t)
	themeTestResource(t, ctx, store, "theme-wrong-parent", "song", "theme-seed", "library-b", "Seed/theme-music/wrong.mp3")
	themeTestResource(t, ctx, store, "theme-wrong-type", "song", "theme-seed", "library-b", "Seed/theme-music/type.mp3")
	themeTestResource(t, ctx, store, "theme-wrong-library", "song", "theme-seed", "library-a", "Foreign/theme.mp3")
	themeTestItem(t, ctx, store, "theme-unreserved", "Audio", "theme-seed", "library-b", "Seed/ordinary.mp3")
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_id='theme-all' WHERE id='theme-wrong-parent';
		UPDATE items SET type='Movie' WHERE id='theme-wrong-type';
		INSERT INTO item_theme_resources VALUES('theme-unreserved','theme-seed','song',true)`); err != nil {
		t.Fatal(err)
	}
	query := ThemeQuery{Subject: Subject{UserID: "restricted"}, EnableThemeSongs: true}
	result := themeTestQuery(t, ctx, store, "theme-seed", query)
	if !reflect.DeepEqual(queryItemIDs(result.ThemeSongsResult.Items), []string{"theme-song"}) {
		t.Fatal("invalid type, owner, reserved path, or library relationships became playable theme candidates")
	}
	for _, id := range []string{"theme-wrong-parent", "theme-wrong-type", "theme-wrong-library", "theme-unreserved"} {
		if _, err := store.QueryThemeMedia(ctx, id, query); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid direct resource seed %s was admitted: %v", id, err)
		}
	}
	if _, err := store.pool.Exec(ctx, "DELETE FROM theme_owner_ids WHERE item_id='theme-seed'"); err != nil {
		t.Fatal(err)
	}
	before := themeTestState(t, ctx, store)
	if _, err := store.QueryThemeMedia(ctx, "theme-seed", query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing item mapping was silently allocated or treated as empty: %v", err)
	}
	if themeTestState(t, ctx, store) != before {
		t.Fatal("theme GET repaired a missing durable owner")
	}
	if _, err := store.pool.Exec(ctx, "DELETE FROM theme_owner_ids WHERE virtual_root"); err != nil {
		t.Fatal(err)
	}
	query.InheritFromParent = true
	if _, err := store.QueryThemeMedia(ctx, "movie-b", query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing virtual-root mapping was fabricated: %v", err)
	}
}

func TestStoreThemeMediaRejectsCyclesDepthAndResourceOverflowWithoutTruncation(t *testing.T) {
	ctx, store := themeTestFixture(t)
	query := ThemeQuery{Subject: Subject{UserID: "restricted"}, EnableThemeSongs: true, EnableThemeVideos: true, InheritFromParent: true}
	if _, err := store.pool.Exec(ctx, "UPDATE items SET parent_id=id WHERE id='theme-seed'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueryThemeMedia(ctx, "theme-seed", query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cyclic theme ancestry was silently shortened: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		SELECT 'theme-depth-'||n,'library-b',CASE WHEN n=$1 THEN NULL ELSE 'theme-depth-'||(n+1) END,
		'theme-depth-'||n,'theme-depth-'||n,'Folder',true FROM generate_series(0,$1::int) n`, MaxThemeAncestorDepth+1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueryThemeMedia(ctx, "theme-depth-0", query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("over-depth theme ancestry was represented as a complete empty result: %v", err)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE items SET parent_id=NULL WHERE id=$1", fmt.Sprintf("theme-depth-%d", MaxThemeAncestorDepth)); err != nil {
		t.Fatal(err)
	}
	_ = themeTestQuery(t, ctx, store, "theme-depth-0", query)
	query.InheritFromParent = false
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path)
		SELECT 'theme-bulk-'||n,'library-b','root-b','theme-seed','theme-bulk-'||n,'theme-bulk-'||n,
		CASE WHEN n%2=0 THEN 'Audio' ELSE 'Video' END,'Seed/reserved-'||n FROM generate_series(1,$1::int) n`, MaxThemeResourcesPerOwner-1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id LIKE 'theme-bulk-%';
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
		SELECT id,'theme-seed',CASE WHEN type='Audio' THEN 'song' ELSE 'video' END,true FROM items WHERE id LIKE 'theme-bulk-%'`); err != nil {
		t.Fatal(err)
	}
	result := themeTestQuery(t, ctx, store, "theme-seed", query)
	if result.ThemeSongsResult.TotalRecordCount+result.ThemeVideosResult.TotalRecordCount != MaxThemeResourcesPerOwner {
		t.Fatal("the boundary population was paged or truncated")
	}
	themeTestResource(t, ctx, store, "theme-overflow-video", "video", "theme-seed", "library-b", "Seed/backdrops/overflow.mp4")
	query.EnableThemeVideos = false
	if _, err := store.QueryThemeMedia(ctx, "theme-seed", query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled video projection bypassed the combined owner resource limit: %v", err)
	}
}

type themeSeedTraceKey struct{}

type themeSnapshotTracer struct {
	writer *pgxpool.Pool
	once   sync.Once
	err    error
	begins []string
}

func (trace *themeSnapshotTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.ToLower(data.SQL), "begin") {
		trace.begins = append(trace.begins, strings.ToLower(data.SQL))
	}
	return context.WithValue(ctx, themeSeedTraceKey{}, strings.HasPrefix(data.SQL, "SELECT i.id, i.library_id, mapping.id FROM items i"))
}

func (trace *themeSnapshotTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if selected, _ := ctx.Value(themeSeedTraceKey{}).(bool); selected {
		trace.once.Do(func() {
			_, trace.err = trace.writer.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted';
				UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-song';
				UPDATE user_item_data SET play_count=99 WHERE user_id='restricted' AND item_id='theme-song'`)
		})
	}
}

func TestStoreThemeMediaAuthorizationResourcesAndUserDataShareOneReadSnapshot(t *testing.T) {
	ctx, store := themeTestFixture(t)
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "theme-song", PlayCount: 3})
	trace := &themeSnapshotTracer{writer: store.pool}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	query := ThemeQuery{Subject: Subject{UserID: "restricted"}, EnableThemeSongs: true}
	result := themeTestQuery(t, ctx, &Store{pool: reader}, "theme-seed", query)
	if trace.err != nil || len(trace.begins) != 1 || !strings.Contains(trace.begins[0], "repeatable read") ||
		!strings.Contains(trace.begins[0], "read only") || len(result.ThemeSongsResult.Items) != 1 ||
		result.ThemeSongsResult.Items[0].UserData == nil || result.ThemeSongsResult.Items[0].UserData.PlayCount != 3 {
		t.Fatalf("theme data escaped one authorized read-only snapshot: begins=%v, writer=%v", trace.begins, trace.err)
	}
	if _, err := store.QueryThemeMedia(ctx, "theme-seed", query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the next independent read did not observe committed policy revocation: %v", err)
	}
}

func TestQueryThemeMediaRejectsInvalidInputsBeforeDatabaseAccess(t *testing.T) {
	for _, seed := range []string{"", " seed", "seed ", "bad\x00seed", "bad\nseed", string([]byte{0xff}), strings.Repeat("x", 257)} {
		if _, err := (&Store{}).QueryThemeMedia(context.Background(), seed, ThemeQuery{Subject: Subject{UserID: "user"}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid theme seed reached database access: %v", err)
		}
	}
}
