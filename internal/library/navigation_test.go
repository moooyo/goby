package library

import (
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestNavigationAncestorsAndCountsUseCurrentVisibleCatalog(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	ancestors, err := store.Ancestors(ctx, Subject{UserID: "restricted"}, "episode-b1")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range ancestors {
		ids = append(ids, item.ID)
	}
	if !reflect.DeepEqual(ids, []string{"season-b", "series-b", "library-b"}) {
		t.Fatalf("ancestor order or identity = %v", ids)
	}
	for _, id := range []string{"movie-a", "not-present", "cross-library-child"} {
		if _, err := store.Ancestors(ctx, Subject{UserID: "restricted"}, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden or missing ancestor seed %q returned %v", id, err)
		}
	}
	counts, err := store.CountItems(ctx, Subject{UserID: "restricted"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts.ItemCount != 6 || counts.MovieCount != 1 || counts.SeriesCount != 1 || counts.EpisodeCount != 2 || counts.SongCount != 1 {
		t.Fatalf("counts leaked or lost visible catalog: %+v", counts)
	}
	favorite := true
	if _, err := store.SetFavorite(ctx, "restricted", "movie-b", true); err != nil {
		t.Fatal(err)
	}
	favorites, err := store.CountItems(ctx, Subject{UserID: "restricted"}, &favorite)
	if err != nil || favorites.ItemCount != 1 || favorites.MovieCount != 1 {
		t.Fatalf("favorite counts = %+v, %v", favorites, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	counts, err = store.CountItems(ctx, Subject{UserID: "restricted"}, nil)
	if err != nil || counts.ItemCount != 0 {
		t.Fatalf("counts retained revoked access: %+v, %v", counts, err)
	}
	if _, err := store.Ancestors(ctx, Subject{UserID: "restricted"}, "episode-b1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ancestor read retained revoked access: %v", err)
	}
}

func TestNavigationAncestorsStopCyclesAndCrossLibraryParents(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_id='season-b' WHERE id='series-b'`); err != nil {
		t.Fatal(err)
	}
	items, err := store.Ancestors(ctx, Subject{UserID: "restricted"}, "episode-b1")
	if err != nil || len(items) != 2 || items[0].ID != "season-b" || items[1].ID != "series-b" {
		t.Fatalf("cycle navigation = %+v, %v", items, err)
	}
	items, err = store.Ancestors(ctx, Subject{UserID: "admin"}, "cross-library-child")
	if err != nil || len(items) != 0 {
		t.Fatalf("ancestor crossed a library boundary: %+v, %v", items, err)
	}
}

func TestNavigationArtistCountsAndNotificationVisibilityKeepEntityStateIndependent(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	var artist, albumArtist, hidden int64
	for _, entry := range []struct {
		name   string
		target *int64
	}{{"Visible Artist", &artist}, {"Visible Album Artist", &albumArtist}, {"Hidden Artist", &hidden}} {
		if err := store.pool.QueryRow(ctx, "INSERT INTO catalog_entities(kind,name) VALUES('MusicArtist',$1) RETURNING id", entry.name).Scan(entry.target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type) VALUES('hidden-audio','library-a','library-a','Hidden Audio','Hidden Audio','Audio')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
		VALUES('audio-b',$1,1,'Visible Artist',1,'Artist'),('audio-b',$2,1,'Visible Album Artist',2,'AlbumArtist'),('hidden-audio',$3,1,'Hidden Artist',1,'Artist')`, artist, albumArtist, hidden); err != nil {
		t.Fatal(err)
	}
	counts, err := store.CountItems(ctx, Subject{UserID: "restricted"}, nil)
	if err != nil || counts.ArtistCount != 2 {
		t.Fatalf("authorized artist count = %+v, %v", counts, err)
	}
	if _, err := store.SetFavorite(ctx, "restricted", "audio-b", true); err != nil {
		t.Fatal(err)
	}
	favorite, likes := true, true
	counts, err = store.CountItems(ctx, Subject{UserID: "restricted"}, &favorite)
	if err != nil || counts.ArtistCount != 0 || counts.SongCount != 1 {
		t.Fatalf("artist inherited its track's favorite: %+v, %v", counts, err)
	}
	if _, err := store.UpdateEntityUserDataFor(ctx, Subject{UserID: "restricted"}, artist, UserDataPatch{IsFavorite: &favorite, Likes: &likes}); err != nil {
		t.Fatal(err)
	}
	counts, err = store.CountItems(ctx, Subject{UserID: "restricted"}, &favorite)
	if err != nil || counts.ArtistCount != 1 {
		t.Fatalf("independent artist favorite = %+v, %v", counts, err)
	}
	visibleID, hiddenID := strconv.FormatInt(artist, 10), strconv.FormatInt(hidden, 10)
	visible, err := store.VisibleUserDataFor(ctx, Subject{UserID: "restricted"}, []string{"audio-b", visibleID, hiddenID, "missing"})
	if err != nil || len(visible) != 2 || !visible["audio-b"] || !visible[visibleID] {
		t.Fatalf("notification visibility = %v, %v", visible, err)
	}
	if _, err := store.pool.Exec(ctx, "DELETE FROM item_entities WHERE entity_id=$1", artist); err != nil {
		t.Fatal(err)
	}
	visible, err = store.VisibleUserDataFor(ctx, Subject{UserID: "restricted"}, []string{visibleID})
	if err != nil || len(visible) != 0 {
		t.Fatalf("orphaned entity stayed addressable: %v, %v", visible, err)
	}
}

func TestNavigationFiltersComposeBeforeCountPagingAndSort(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata='{"ProductionYear":2025,"PremiereDate":"2025-05-01T00:00:00Z","CommunityRating":8.5}',overview='Overview',media='{"DurationTicks":30000000,"Streams":[{"CodecType":"video","Width":1920,"Height":1080},{"CodecType":"subtitle"}]}' WHERE id='movie-b';
		UPDATE items SET local_metadata='{"ProductionYear":2025,"PremiereDate":"2025-06-01T00:00:00Z","CommunityRating":7.0}',media='{"DurationTicks":10000000,"Streams":[{"CodecType":"video","Width":640,"Height":360}]}' WHERE id='episode-b1';
		UPDATE items SET local_metadata='{"ProductionYear":2025,"CommunityRating":10}',media='{"DurationTicks":50000000,"Streams":[{"CodecType":"video","Width":3840,"Height":2160}]}' WHERE id='movie-a'`); err != nil {
		t.Fatal(err)
	}
	trueValue := true
	rating := 8.0
	start, end := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	query := Query{UserID: "restricted", Recursive: true, Years: []int{2025}, MinPremiereDate: &start, MaxPremiereDate: &end, MinCommunityRating: &rating, HasOverview: &trueValue, HasSubtitles: &trueValue, IsHD: &trueValue, SortBy: "CommunityRating,Runtime", SortOrder: "Descending", Limit: 1}
	result, err := store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].ID != "movie-b" {
		t.Fatalf("combined filters/count/ACL = %+v, %v", result, err)
	}
	query.StartIndex = 1
	result, err = store.QueryItems(ctx, query)
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 0 {
		t.Fatalf("filtered count depended on page: %+v, %v", result, err)
	}
	result, err = store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, NameStartsWith: "B 100%_", ExcludeItemTypes: []string{"Movie"}, Limit: 10})
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != "episode-b2" {
		t.Fatalf("literal name prefix was treated as a wildcard: %+v, %v", result, err)
	}
	likes := true
	if _, err := store.UpdateUserDataFor(ctx, Subject{UserID: "restricted"}, "episode-b1", UserDataPatch{Likes: &likes}); err != nil {
		t.Fatal(err)
	}
	result, err = store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, IsFavoriteOrLikes: &trueValue, Limit: 10})
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != "episode-b1" {
		t.Fatalf("liked-only item did not satisfy favorite-or-likes: %+v, %v", result, err)
	}
}

func TestNavigationAndMusicPrefixesTreatSQLPatternCharactersLiterally(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	// Backslashes are valid in catalog display names, even though indexed media
	// relative paths deliberately reject them. Each LIKE metacharacter is data.
	const prefix = `Literal%_\`
	for _, entry := range []struct{ id, name string }{
		{"movie-b", prefix + "Target"},
		{"episode-b1", "LiteralABTarget"},
		{"episode-b2", "Literal%_Target"},
		{"movie-a", prefix + "Hidden"},
	} {
		if _, err := store.pool.Exec(ctx, "UPDATE items SET name=$2,sort_name=$2 WHERE id=$1", entry.id, entry.name); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, NameStartsWith: prefix, Limit: 10})
	if err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].ID != "movie-b" {
		t.Fatalf("literal navigation prefix expanded wildcards, consumed an escape, or crossed ACL: %+v, %v", result, err)
	}
	var targetID int64
	for index, name := range []string{prefix + "Artist", "LiteralABArtist", "Literal%_Artist"} {
		var id int64
		if err := store.pool.QueryRow(ctx, "INSERT INTO catalog_entities(kind,name) VALUES('MusicArtist',$1) RETURNING id", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,credit_type)
			VALUES('audio-b',$1,$2,$3,1,'Artist')`, id, index+1, name); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			targetID = id
		}
	}
	for _, query := range []Query{
		{UserID: "restricted", NameStartsWith: prefix, Limit: 10},
		{UserID: "restricted", SearchTerm: prefix, Limit: 10},
	} {
		artists, err := store.ListMusicEntities(ctx, "artists", query)
		if err != nil || artists.TotalRecordCount != 1 || len(artists.Items) != 1 || artists.Items[0].ID != targetID {
			t.Fatalf("literal music name filter expanded a pattern or lost a backslash: %+v, %v", artists, err)
		}
	}
}

func TestNavigationFilterValidationRejectsUnsupportedOrUnboundedSelectors(t *testing.T) {
	invalidRating := math.NaN()
	after, before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, query := range []Query{{UserID: "viewer", Years: []int{0}}, {UserID: "viewer", Years: make([]int, 257)}, {UserID: "viewer", MinCommunityRating: &invalidRating},
		{UserID: "viewer", MinPremiereDate: &after, MaxPremiereDate: &before}, {UserID: "viewer", ExcludeItemTypes: []string{"UnknownKind"}}, {UserID: "viewer", NameStartsWith: "bad\x00name"}, {UserID: "viewer", SortBy: "InventedRank"}} {
		if _, err := normalizeItemQuery(query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid selector accepted: %+v, %v", query, err)
		}
	}
}
