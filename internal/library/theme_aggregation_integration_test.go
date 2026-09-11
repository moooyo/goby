package library

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestThemeVisibilityDerivedCatalogQueriesKeepOnlyOrdinarySources(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedThemeVisibilityFixture(t, ctx, store)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path) VALUES
		('theme-ordinary-match','library-b','root-b','library-b','Ordinary Match','g match','Movie',false,
		 '/media/b/Match/movie.mp4','Match/movie.mp4'),
		('theme-legacy-movie','library-b','root-b','movie-b','Legacy Match','0 hidden match','Movie',false,
		 '/media/b/Owner/backdrops/legacy.mp4','Owner/backdrops/legacy.mp4'),
		('theme-legacy-episode','library-b','root-b','season-b','Legacy Episode','0 hidden episode','Episode',false,
		 '/media/b/Owner/backdrops/episode.mp4','Owner/backdrops/episode.mp4'),
		('theme-legacy-album','library-b','root-b','movie-b','Legacy Album','0 hidden album','MusicAlbum',true,
		 '/media/b/Owner/theme-music/album','Owner/theme-music/album'),
		('theme-ordinary-orphan','library-b','root-b','theme-legacy-album','Ordinary Outside Track','h outside','Audio',false,
		 '/media/b/Elsewhere/outside.mp3','Elsewhere/outside.mp3');
		UPDATE items SET index_number=3,parent_index_number=1 WHERE id='theme-legacy-episode';
		INSERT INTO sessions(id,user_id,kind,token_hash,expires_at)
		VALUES('theme-native-session','admin','admin',decode(repeat('ab',32),'hex'),clock_timestamp()+interval '1 hour')`); err != nil {
		t.Fatalf("seed derived theme-query boundaries: %v", err)
	}
	for _, id := range []string{"movie-b", "theme-ordinary-match", "theme-legacy-movie"} {
		if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities($1,'{"Genres":["Shared Genre"],"Tags":["Shared Tag"]}'::jsonb)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `SELECT sync_catalog_item_entities('theme-song-b','{"Genres":["Theme-only Genre"]}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	shared := libraryQueryEntityID(t, ctx, store.pool, "Genre", "Shared Genre")
	hidden := libraryQueryEntityID(t, ctx, store.pool, "Genre", "Theme-only Genre")
	entity, err := store.GetEntityByID(ctx, "restricted", shared)
	if err != nil || entity.Count != 2 {
		t.Fatalf("shared entity counted hidden theme sources: %+v, %v", entity, err)
	}
	if _, err := store.GetEntityByID(ctx, "restricted", hidden); !errors.Is(err, ErrNotFound) {
		t.Fatalf("theme-only entity detail returned %v, want ErrNotFound", err)
	}
	if _, err := store.GetEntity(ctx, "restricted", "Genre", "Theme-only Genre"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("theme-only entity name returned %v, want ErrNotFound", err)
	}
	entities, err := store.ListEntities(ctx, "Genre", Query{UserID: "restricted", SearchTerm: "Theme-only"})
	if err != nil || entities.TotalRecordCount != 0 || len(entities.Items) != 0 {
		t.Fatalf("theme-only entity search leaked membership: %+v, %v", entities, err)
	}
	similar, err := store.QuerySimilar(ctx, "movie-b", SimilarQuery{Query: Query{UserID: "restricted", Limit: 10}})
	if err != nil || similar.TotalRecordCount != 1 || !reflect.DeepEqual(queryItemIDs(similar.Items), []string{"theme-ordinary-match"}) {
		t.Fatalf("Similar used a permanently hidden Movie candidate: %+v, %v", similar, err)
	}
	for _, seed := range []string{"theme-legacy-movie", strconv.FormatInt(hidden, 10)} {
		if _, err := store.QuerySimilar(ctx, seed, SimilarQuery{Query: Query{UserID: "restricted", Limit: 10}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden Similar seed %s returned %v, want ErrNotFound", seed, err)
		}
	}
	if _, err := store.SetPlayed(ctx, "restricted", "episode-b1", true, nil); err != nil {
		t.Fatal(err)
	}
	next, err := store.NextUp(ctx, NextUpQuery{UserID: "restricted", SeriesID: "series-b", Limit: 10})
	if err != nil || next.TotalRecordCount != 1 || !reflect.DeepEqual(queryItemIDs(next.Items), []string{"episode-b2"}) {
		t.Fatalf("NextUp used a legacy theme Episode: %+v, %v", next, err)
	}
	if _, err := store.SetPlayed(ctx, "restricted", "episode-b2", true, nil); err != nil {
		t.Fatal(err)
	}
	next, err = store.NextUp(ctx, NextUpQuery{UserID: "restricted", SeriesID: "series-b", Limit: 10})
	if err != nil || next.TotalRecordCount != 0 || len(next.Items) != 0 {
		t.Fatalf("a legacy theme Episode became next after every ordinary episode was played: %+v, %v", next, err)
	}
	latest, err := store.QueryLatest(ctx, Query{UserID: "restricted", IncludeItemTypes: []string{"Audio"}, Limit: 100}, true)
	if err != nil {
		t.Fatal(err)
	}
	foundOutside := false
	for _, item := range latest {
		if item.Item.ID == "theme-ordinary-orphan" {
			foundOutside = true
		}
		if item.Item.ID == "theme-legacy-album" || item.Item.ID == "theme-song-b" || item.Item.ID == "theme-inactive-b" {
			t.Fatalf("Latest projected a hidden source or representative: %s", item.Item.ID)
		}
	}
	if !foundOutside {
		t.Fatal("a hidden ancestor replaced or swallowed an ordinary source's Latest projection")
	}
	outside, err := store.GetItem(ctx, "restricted", "theme-ordinary-orphan")
	if err != nil || outside.Album != nil {
		t.Fatalf("ordinary track inherited a hidden album: %+v, %v", outside.Album, err)
	}
	filtered, err := store.QueryItems(ctx, Query{UserID: "restricted", AlbumIds: []string{"theme-legacy-album"}})
	if err != nil || filtered.TotalRecordCount != 0 {
		t.Fatalf("a hidden album matched ordinary music filtering: %+v, %v", filtered, err)
	}
	actor := identity.Principal{Kind: "admin", SessionID: "theme-native-session", User: identity.User{ID: "admin"}}
	metadata, err := store.QueryMetadataItems(ctx, actor, "library-b", MetadataItemQuery{Types: []string{"Movie"}, Limit: 100})
	if err != nil || metadata.TotalRecordCount != 2 || len(metadata.Items) != 2 {
		t.Fatalf("administrator metadata count included hidden media: %+v, %v", metadata, err)
	}
	for _, item := range metadata.Items {
		if item.ItemID != "movie-b" && item.ItemID != "theme-ordinary-match" {
			t.Fatalf("administrator metadata listing exposed %s", item.ItemID)
		}
	}
}

func TestThemeVisibilityFolderStateWritesAndNotificationsPreserveAttachments(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET root_id='root-b',path='/media/b/Show',relative_path='Show' WHERE id='series-b';
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,media) VALUES
		('theme-state-song','library-b','root-b','series-b','Theme state song','theme state song','Audio',false,
		 '/media/b/Show/theme.mp3','Show/theme.mp3','{"DurationTicks":6000000000}'::jsonb),
		('theme-state-directory','library-b','root-b','series-b','Backdrop directory','backdrop directory','Folder',true,
		 '/media/b/Show/backdrops','Show/backdrops',NULL),
		('theme-state-legacy','library-b','root-b','theme-state-directory','Legacy theme video','legacy theme video','Episode',false,
		 '/media/b/Show/backdrops/old.mp4','Show/backdrops/old.mp4','{"DurationTicks":6000000000}'::jsonb);
		INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES
		('root-b','Show/theme.mp3',false),('root-b','Show/backdrops',true);
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('theme-state-song','series-b','song',true);
		INSERT INTO user_item_data(user_id,item_id,play_count,playback_position_ticks,is_favorite) VALUES
		('restricted','theme-state-song',7,1800000000,true),('restricted','theme-state-legacy',9,1800000000,true)`); err != nil {
		t.Fatalf("seed theme state preservation boundary: %v", err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := store.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(data) ORDER BY item_id)::text FROM user_item_data data
			WHERE item_id IN ('theme-state-song','theme-state-legacy')`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	series, err := store.GetItem(ctx, "restricted", "series-b")
	if err != nil || series.UserData == nil || series.UserData.UnplayedItemCount == nil || *series.UserData.UnplayedItemCount != 2 {
		t.Fatalf("folder summary counted theme resources: %+v, %v", series.UserData, err)
	}
	for _, played := range []bool{true, false} {
		data, err := store.SetPlayed(ctx, "restricted", "series-b", played, nil)
		if err != nil || data.Played != played || data.UnplayedItemCount == nil || *data.UnplayedItemCount != map[bool]int{true: 0, false: 2}[played] {
			t.Fatalf("ordinary folder state is incorrect: %+v, %v", data, err)
		}
		if snapshot() != before {
			t.Fatal("marking an ordinary folder changed permanent attachment history")
		}
	}
	resume, err := store.QueryResume(ctx, Query{UserID: "restricted", Limit: 20})
	if err != nil || resume.TotalRecordCount != 0 {
		t.Fatalf("attachment progress appeared in ordinary resume: %+v, %v", resume, err)
	}
	notifications, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{
		UserID: "restricted", ItemID: "series-b", Recursive: true, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range notifications.Items {
		if item.ItemID == "theme-state-song" || item.ItemID == "theme-state-legacy" {
			t.Fatal("ordinary folder notifications included a permanent attachment")
		}
	}
	direct, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{UserID: "restricted", ItemID: "theme-state-song", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range direct.Items {
		if item.ItemID == "theme-state-song" && item.PlayCount == 7 {
			found = true
		}
	}
	if !found || snapshot() != before {
		t.Fatal("the explicit active theme lost its own unchanged notification state")
	}
	if _, err := store.pool.Exec(ctx, "UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-state-song'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{UserID: "restricted", ItemID: "theme-state-song", Limit: 100}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an inactive direct notification remained visible: %v", err)
	}
	if snapshot() != before {
		t.Fatal("theme retirement changed retained user history")
	}
}

func TestThemeVisibilityAlbumAggregationAndChildCountIgnoreAllReservedMembers(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path) VALUES
		('theme-aggregate-album','library-b','root-b','library-b','Physical Album','physical album','MusicAlbum',true,
		 '/media/b/Album','Album'),
		('theme-aggregate-track','library-b','root-b','theme-aggregate-album','Ordinary track','ordinary track','Audio',false,
		 '/media/b/Album/01.mp3','Album/01.mp3'),
		('theme-aggregate-song','library-b','root-b','theme-aggregate-album','Theme track','theme track','Audio',false,
		 '/media/b/Album/theme-music/theme.mp3','Album/theme-music/theme.mp3'),
		('theme-aggregate-hidden-album','library-b','root-b','theme-aggregate-album','Legacy Album','legacy album','MusicAlbum',true,
		 '/media/b/Album/theme-music','Album/theme-music'),
		('theme-aggregate-legacy','library-b','root-b','theme-aggregate-hidden-album','Legacy track','legacy track','Audio',false,
		 '/media/b/Album/theme-music/legacy.mp3','Album/theme-music/legacy.mp3');
		INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES ('root-b','Album/theme-music',true);
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
		VALUES ('theme-aggregate-song','theme-aggregate-album','song',true)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"theme-aggregate-track", "theme-aggregate-song", "theme-aggregate-legacy"} {
		album, artist := "Published Album", "Ordinary Artist"
		if id != "theme-aggregate-track" {
			album, artist = "Theme Poison Album", "Theme Poison Artist"
		}
		raw, err := json.Marshal(media.Info{DurationTicks: 180 * media.TicksPerSecond, EmbeddedMusic: &media.MusicMetadata{
			Version: media.CurrentMusicMetadataVersion, Album: album, Artist: artist, AlbumArtist: artist,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, "UPDATE items SET media=$2::jsonb WHERE id=$1", id, raw); err != nil {
			t.Fatal(err)
		}
	}
	refresh := func(id string) {
		t.Helper()
		tx, err := store.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		ready, err := refreshAcceptedMusicAlbum(ctx, tx, "library-b", id)
		if err != nil || !ready {
			t.Fatalf("refresh ordinary accepted album: ready=%v error=%v", ready, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh("theme-aggregate-album")
	expected := musicMetadataSource{Version: musicSourceVersion, Name: "Published Album", Album: "Published Album",
		Artists: []string{"Ordinary Artist"}, AlbumArtists: []string{"Ordinary Artist"}}
	metadataMusicScanAssertSource(t, ctx, store.pool, "theme-aggregate-album", expected)
	album, err := store.GetItem(ctx, "restricted", "theme-aggregate-album")
	if err != nil || album.ChildCount == nil || *album.ChildCount != 1 {
		t.Fatalf("album ChildCount included attached or reserved rows: %+v, %v", album.ChildCount, err)
	}
	latest, err := store.QueryLatest(ctx, Query{UserID: "restricted", ParentID: album.ID, IncludeItemTypes: []string{"Audio"}, Limit: 10}, true)
	if err != nil || len(latest) != 1 || latest[0].Item.ID != album.ID || latest[0].ChildCount != 1 {
		t.Fatalf("Latest album grouping included attached members: %+v, %v", latest, err)
	}
	var before, after string
	if err := store.pool.QueryRow(ctx, "SELECT to_jsonb(state)::text FROM item_metadata_state state WHERE item_id='theme-aggregate-hidden-album'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	refresh("theme-aggregate-hidden-album")
	if err := store.pool.QueryRow(ctx, "SELECT to_jsonb(state)::text FROM item_metadata_state state WHERE item_id='theme-aggregate-hidden-album'").Scan(&after); err != nil || after != before {
		t.Fatal("an ordinary album refresh changed hidden album metadata")
	}
	if _, err := store.pool.Exec(ctx, "UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-aggregate-song'"); err != nil {
		t.Fatal(err)
	}
	refresh("theme-aggregate-album")
	metadataMusicScanAssertSource(t, ctx, store.pool, "theme-aggregate-album", expected)
}
