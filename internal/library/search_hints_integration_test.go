package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func searchHintTestExec(t *testing.T, ctx context.Context, store *Store, statement string, args ...any) {
	t.Helper()
	if _, err := store.pool.Exec(ctx, statement, args...); err != nil {
		t.Fatalf("write search hint fixture: %v", err)
	}
}

func searchHintTestItem(t *testing.T, ctx context.Context, store *Store, id, name, kind, libraryID, parentID string) SearchHintReference {
	t.Helper()
	folder := kind == "Folder" || kind == "Series" || kind == "Season" || kind == "MusicAlbum" || kind == "Playlist" || kind == "BoxSet"
	searchHintTestExec(t, ctx, store, `INSERT INTO items (id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES ($1,$2,$3,$4,$4,$5,$6)`, id, libraryID, parentID, name, kind, folder)
	if kind == "Playlist" || kind == "BoxSet" {
		searchHintTestExec(t, ctx, store, `INSERT INTO media_collections (item_id,owner_id,kind,is_public)
			VALUES ($1,'restricted',$2,true)`, id, kind)
	}
	return SearchHintReference{Kind: "Item", ID: id}
}

func searchHintTestMetadata(t *testing.T, ctx context.Context, store *Store, itemID, metadata string) {
	t.Helper()
	searchHintTestExec(t, ctx, store, `WITH changed AS (
		UPDATE items SET local_metadata=$2::jsonb WHERE id=$1 RETURNING id,local_metadata
	) SELECT sync_catalog_item_entities(id,local_metadata) FROM changed`, itemID, metadata)
}

func searchHintTestEntity(t *testing.T, ctx context.Context, store *Store, kind, name string) SearchHintReference {
	t.Helper()
	id := libraryQueryEntityID(t, ctx, store.pool, kind, name)
	return SearchHintReference{Kind: "Entity", ID: strconv.FormatInt(id, 10)}
}

func searchHintTestQuery(t *testing.T, ctx context.Context, store *Store, subject Subject, query SearchHintsQuery) SearchHintResult {
	t.Helper()
	result, err := store.SearchHints(ctx, subject, query)
	if err != nil {
		t.Fatalf("search hints for %+v with %+v: %v", subject, query, err)
	}
	return result
}

func assertSearchHintPage(t *testing.T, result SearchHintResult, total int, expected ...SearchHintReference) {
	t.Helper()
	if result.SearchHints == nil || result.TotalRecordCount != total || len(result.SearchHints) != len(expected) {
		t.Fatalf("search hint page = %+v, want total %d and references %v", result, total, expected)
	}
	actual := make([]SearchHintReference, len(result.SearchHints))
	for index, hint := range result.SearchHints {
		actual[index] = hint.Reference
		if (hint.Item == nil) == (hint.Entity == nil) {
			t.Fatalf("hint must project exactly one typed owner: %+v", hint)
		}
		if hint.Item != nil && (hint.Reference.Kind != "Item" || hint.Reference.ID != hint.Item.ID) {
			t.Fatalf("physical hint changed owner identity: %+v", hint)
		}
		if hint.Entity != nil && (hint.Reference.Kind != "Entity" || hint.Reference.ID != strconv.FormatInt(hint.Entity.ID, 10)) {
			t.Fatalf("entity hint changed owner identity: %+v", hint)
		}
	}
	if len(expected) != 0 && !reflect.DeepEqual(actual, expected) {
		t.Fatalf("search hint order = %v, want %v", actual, expected)
	}
}

func searchHintTestArtwork(t *testing.T, ctx context.Context, store *Store, owner SearchHintReference, tag string) {
	t.Helper()
	var ownerColumn string
	var ownerID any
	switch owner.Kind {
	case "Item":
		ownerColumn, ownerID = "item_id", owner.ID
	case "Entity":
		id, err := strconv.ParseInt(owner.ID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		ownerColumn, ownerID = "entity_id", id
	default:
		t.Fatalf("invalid fixture artwork owner: %+v", owner)
	}
	searchHintTestExec(t, ctx, store, `WITH state AS (
		INSERT INTO artwork_state (`+ownerColumn+`,managed_types) VALUES ($1,ARRAY['Primary']) RETURNING id
	) INSERT INTO artwork_images (state_id,image_type,image_index,content,mime_type,width,height,source_hash)
		SELECT id,'Primary',0,decode('01','hex'),'image/png',1,1,$2 FROM state`, ownerID, tag)
}

func assertSearchHintImage(t *testing.T, result SearchHintResult, owner SearchHintReference, tag string, legacy bool) {
	t.Helper()
	for _, hint := range result.SearchHints {
		if hint.Reference != owner {
			continue
		}
		if len(hint.Images) != 1 || hint.Images[0].ImageType != "Primary" || hint.Images[0].Tag != tag || hint.LegacyImageReference != legacy {
			t.Fatalf("artwork for %+v = %+v, want tag %q and legacy reference %t", owner, hint, tag, legacy)
		}
		if hint.Entity != nil && !reflect.DeepEqual(hint.Entity.Images, hint.Images) {
			t.Fatalf("entity and hint artwork disagree for %+v", owner)
		}
		return
	}
	t.Fatalf("missing artwork owner %+v in %+v", owner, result)
}

func TestStoreSearchHintsMixedRankingPagingAndFilters(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	physical := make(map[string]SearchHintReference)
	for _, fixture := range []struct{ id, name, kind string }{
		{"hint-upper", "NEEDLE", "Movie"},
		{"hint-audio", "Needle", "Audio"},
		{"hint-box", "Needle", "BoxSet"},
		{"hint-folder", "Needle", "Folder"},
		{"hint-movie-10", "Needle", "Movie"},
		{"hint-movie-2", "Needle", "Movie"},
		{"hint-album", "Needle", "MusicAlbum"},
		{"hint-music-video", "Needle", "MusicVideo"},
		{"hint-playlist", "Needle", "Playlist"},
		{"hint-series", "Needle", "Series"},
		{"hint-lower", "needle", "Movie"},
		{"hint-prefix", "Needle trail", "Episode"},
		{"hint-substring", "A Needle", "Video"},
	} {
		physical[fixture.id] = searchHintTestItem(t, ctx, store, fixture.id, fixture.name, fixture.kind, "library-b", "library-b")
	}
	searchHintTestExec(t, ctx, store, "UPDATE items SET name='Needle root',sort_name='Needle root' WHERE id='library-b'")
	searchHintTestMetadata(t, ctx, store, "audio-b", `{"Genres":["Needle"],"Tags":["Needle"],"Studios":["Needle"],"Artists":["Needle"],"AlbumArtists":["Needle"],"People":[{"Name":"Needle","Type":"Actor"},{"Name":"Needle","Type":"Director"}]}`)
	searchHintTestMetadata(t, ctx, store, "movie-b", `{"Genres":["Needle"]}`)
	genre := searchHintTestEntity(t, ctx, store, "Genre", "Needle")
	artist := searchHintTestEntity(t, ctx, store, "MusicArtist", "Needle")
	person := searchHintTestEntity(t, ctx, store, "Person", "Needle")
	studio := searchHintTestEntity(t, ctx, store, "Studio", "Needle")
	want := []SearchHintReference{
		physical["hint-upper"], genre, artist, person, studio,
		physical["hint-audio"], physical["hint-box"], physical["hint-folder"],
		physical["hint-movie-10"], physical["hint-movie-2"], physical["hint-album"],
		physical["hint-music-video"], physical["hint-playlist"], physical["hint-series"],
		physical["hint-lower"], physical["hint-prefix"], physical["hint-substring"],
	}
	subject := Subject{UserID: "restricted"}
	query := SearchHintsQuery{SearchTerm: " \tneedle\n", Limit: 1000}
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, query), len(want), want...)
	for _, page := range []struct{ start, limit, from, to int }{
		{2, 4, 2, 6}, {0, 0, 0, 0}, {len(want), 1, 0, 0}, {2147483647, 1000, 0, 0},
	} {
		query.StartIndex, query.Limit = page.start, page.limit
		assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, query), len(want), want[page.from:page.to]...)
	}

	yes, no := true, false
	for _, test := range []struct {
		name  string
		query SearchHintsQuery
		want  []SearchHintReference
	}{
		{"entities without media", SearchHintsQuery{IncludeMedia: &no}, []SearchHintReference{genre, artist, person, studio}},
		{"physical categories", SearchHintsQuery{IncludePeople: &no, IncludeGenres: &no, IncludeStudios: &no, IncludeArtists: &no}, append(append([]SearchHintReference{}, want[:1]...), want[5:]...)},
		{"all categories disabled", SearchHintsQuery{IncludeMedia: &no, IncludePeople: &no, IncludeGenres: &no, IncludeStudios: &no, IncludeArtists: &no}, nil},
		{"include intersects category and exclude wins", SearchHintsQuery{IncludeGenres: &no, IncludeItemTypes: []string{"Genre", "Person", "Movie"}, ExcludeItemTypes: []string{"Person"}}, []SearchHintReference{physical["hint-upper"], physical["hint-movie-10"], physical["hint-movie-2"], physical["hint-lower"]}},
		{"disabled media cannot be reenabled by type", SearchHintsQuery{IncludeMedia: &no, IncludeItemTypes: []string{"Movie"}}, nil},
		{"disabled entity category cannot be reenabled by type", SearchHintsQuery{IncludePeople: &no, IncludeItemTypes: []string{"Genre", "Person"}}, []SearchHintReference{genre}},
		{"audio qualifies associated entities", SearchHintsQuery{MediaTypes: []string{"Audio"}}, []SearchHintReference{genre, artist, person, studio, physical["hint-audio"]}},
		{"video qualifies only video associations", SearchHintsQuery{MediaTypes: []string{"Video"}}, []SearchHintReference{physical["hint-upper"], genre, physical["hint-movie-10"], physical["hint-movie-2"], physical["hint-music-video"], physical["hint-lower"], physical["hint-prefix"], physical["hint-substring"]}},
		{"media type does not suppress entity category", SearchHintsQuery{MediaTypes: []string{"Video"}, IncludeMedia: &no}, []SearchHintReference{genre}},
		{"movie flag", SearchHintsQuery{IsMovie: &yes}, []SearchHintReference{physical["hint-upper"], physical["hint-movie-10"], physical["hint-movie-2"], physical["hint-lower"]}},
		{"series flag", SearchHintsQuery{IsSeries: &yes}, []SearchHintReference{physical["hint-series"]}},
		{"negative movie flag", SearchHintsQuery{IsMovie: &no, IncludeItemTypes: []string{"Movie", "Series"}}, []SearchHintReference{physical["hint-series"]}},
		{"contradictory positive flags", SearchHintsQuery{IsMovie: &yes, IsSeries: &yes}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.SearchTerm, test.query.Limit = "Needle", 1000
			assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, test.query), len(test.want), test.want...)
		})
	}

	literal := searchHintTestItem(t, ctx, store, "hint-literal", `100%_\Exact`, "Movie", "library-b", "library-b")
	searchHintTestItem(t, ctx, store, "hint-percent-decoy", `100xyzQ\Exact`, "Movie", "library-b", "library-b")
	searchHintTestItem(t, ctx, store, "hint-underscore-decoy", `100%a\Exact`, "Movie", "library-b", "library-b")
	searchHintTestItem(t, ctx, store, "hint-backslash-decoy", `100%_Exact`, "Movie", "library-b", "library-b")
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, SearchHintsQuery{SearchTerm: `100%_\`, Limit: 1000}), 1, literal)
	chinese := searchHintTestItem(t, ctx, store, "hint-chinese", "\u661f\u9645", "Movie", "library-b", "library-b")
	chinesePrefix := searchHintTestItem(t, ctx, store, "hint-chinese-prefix", "\u661f\u9645\u8fdc\u822a", "Movie", "library-b", "library-b")
	chineseSubstring := searchHintTestItem(t, ctx, store, "hint-chinese-substring", "\u8fdc\u65b9\u661f\u9645", "Movie", "library-b", "library-b")
	searchHintTestMetadata(t, ctx, store, "audio-b", `{"Genres":["\u661f\u9645"]}`)
	chineseGenre := searchHintTestEntity(t, ctx, store, "Genre", "\u661f\u9645")
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, SearchHintsQuery{SearchTerm: "\u3000\u661f\u9645\t", Limit: 1000}), 4, chineseGenre, chinese, chinesePrefix, chineseSubstring)
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, SearchHintsQuery{SearchTerm: " \t\u3000", Limit: 1000}), 0)
}

func TestStoreSearchHintsScopesEntitySourcesAndSeparatesTypedArtwork(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	searchHintTestMetadata(t, ctx, store, "movie-b", `{"OfficialRating":"PG","Genres":["Scope shared","Scope hidden collision"],"People":[{"Name":"Scope visible","Type":"Actor"}],"Artists":["Scope fake"]}`)
	searchHintTestMetadata(t, ctx, store, "movie-a", `{"Genres":["Scope private","Scope shared"]}`)
	searchHintTestMetadata(t, ctx, store, "series-b", `{"OfficialRating":"TV-MA"}`)
	searchHintTestMetadata(t, ctx, store, "episode-b1", `{"Studios":["Scope adult"]}`)
	searchHintTestMetadata(t, ctx, store, "cross-library-child", `{"Genres":["Scope cross library"]}`)
	searchHintTestMetadata(t, ctx, store, "library-b", `{"People":[{"Name":"Scope container","Type":"Actor"}]}`)
	searchHintTestItem(t, ctx, store, "scope-album", "Ordinary album", "MusicAlbum", "library-b", "library-b")
	searchHintTestMetadata(t, ctx, store, "scope-album", `{"AlbumArtists":["Scope album"]}`)
	searchHintTestItem(t, ctx, store, "scope-video", "Ordinary music video", "MusicVideo", "library-b", "library-b")
	searchHintTestMetadata(t, ctx, store, "scope-video", `{"Artists":["Scope video"]}`)
	searchHintTestExec(t, ctx, store, `INSERT INTO catalog_entities(kind,name) VALUES ('Genre','Scope orphan'),('MusicArtist','Scope legacy');
		INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_type,credit_group)
			SELECT 'audio-b',id,1,name,'Artist',0 FROM catalog_entities WHERE kind='MusicArtist' AND name='Scope legacy';
		UPDATE items SET name='Scope hidden item' WHERE id='movie-a';
		UPDATE items SET name='Scope adult episode' WHERE id='episode-b1';
		UPDATE items SET name='Scope cross item' WHERE id='cross-library-child';
		UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"MaxParentalRating":7}' WHERE id='restricted'`)
	album := searchHintTestEntity(t, ctx, store, "MusicArtist", "Scope album")
	hiddenCollision := searchHintTestEntity(t, ctx, store, "Genre", "Scope hidden collision")
	shared := searchHintTestEntity(t, ctx, store, "Genre", "Scope shared")
	video := searchHintTestEntity(t, ctx, store, "MusicArtist", "Scope video")
	visibleEntity := searchHintTestEntity(t, ctx, store, "Person", "Scope visible")
	visibleItem := searchHintTestItem(t, ctx, store, visibleEntity.ID, "Scope visible", "Movie", "library-b", "library-b")
	hiddenItem := searchHintTestItem(t, ctx, store, hiddenCollision.ID, "Scope hidden physical", "Movie", "library-a", "library-a")
	itemTag, entityTag, hiddenEntityTag := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	searchHintTestArtwork(t, ctx, store, visibleItem, itemTag)
	searchHintTestArtwork(t, ctx, store, visibleEntity, entityTag)
	searchHintTestArtwork(t, ctx, store, hiddenCollision, hiddenEntityTag)
	searchHintTestArtwork(t, ctx, store, hiddenItem, strings.Repeat("d", 64))
	searchHintTestArtwork(t, ctx, store, searchHintTestEntity(t, ctx, store, "Genre", "Scope orphan"), strings.Repeat("e", 64))
	query := SearchHintsQuery{SearchTerm: "Scope", Limit: 1000}
	result := searchHintTestQuery(t, ctx, store, Subject{UserID: "restricted"}, query)
	assertSearchHintPage(t, result, 6, album, hiddenCollision, shared, video, visibleEntity, visibleItem)
	assertSearchHintImage(t, result, visibleItem, itemTag, true)
	assertSearchHintImage(t, result, visibleEntity, entityTag, false)
	assertSearchHintImage(t, result, hiddenCollision, hiddenEntityTag, true)
	no := false
	query.IncludeMedia = &no
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, Subject{UserID: "restricted"}, query), 5, album, hiddenCollision, shared, video, visibleEntity)
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, Subject{UserID: "none"}, query), 0)
	query.MediaTypes = []string{"Audio"}
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, Subject{UserID: "restricted"}, query), 0)
	query.MediaTypes = []string{"Video"}
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, Subject{UserID: "restricted"}, query), 4, hiddenCollision, shared, video, visibleEntity)
}

func TestStoreSearchHintsChecksApplicationAuthorityAndTargetScope(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	searchHintTestExec(t, ctx, store, `UPDATE items SET name='Key hidden' WHERE id='movie-a';
		UPDATE items SET name='Key visible' WHERE id='movie-b';
		UPDATE users SET is_administrator=false,policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"EnableMediaPlayback":false}' WHERE id='disabled'`)
	searchHintTestMetadata(t, ctx, store, "movie-a", `{"Genres":["Key private"]}`)
	searchHintTestMetadata(t, ctx, store, "movie-b", `{"Genres":["Key shared"]}`)
	hidden := SearchHintReference{Kind: "Item", ID: "movie-a"}
	visible := SearchHintReference{Kind: "Item", ID: "movie-b"}
	private := searchHintTestEntity(t, ctx, store, "Genre", "Key private")
	shared := searchHintTestEntity(t, ctx, store, "Genre", "Key shared")
	key := seedCatalogApplicationKey(t, ctx, store.pool, "search-hints-key", true)
	target := Subject{UserID: "restricted", ApplicationCredentialID: key.ApplicationCredentialID}
	disabledTarget := Subject{UserID: "disabled", ApplicationCredentialID: key.ApplicationCredentialID}
	query := SearchHintsQuery{SearchTerm: "Key", Limit: 1000}
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, key, query), 4, hidden, private, shared, visible)
	for _, subject := range []Subject{target, disabledTarget} {
		assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, subject, query), 2, shared, visible)
	}
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, Subject{UserID: "none", ApplicationCredentialID: key.ApplicationCredentialID}, query), 0)
	searchHintTestExec(t, ctx, store, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-a"]}' WHERE id='restricted'`)
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, target, query), 2, hidden, private)
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, key, query), 4, hidden, private, shared, visible)
	for _, term := range []string{"Key", " ", "no matching hint"} {
		query.SearchTerm = term
		if result, err := store.SearchHints(ctx, Subject{UserID: "missing-target", ApplicationCredentialID: key.ApplicationCredentialID}, query); !errors.Is(err, ErrNotFound) || result.TotalRecordCount != 0 || len(result.SearchHints) != 0 {
			t.Fatalf("missing application target with term %q returned %+v, %v", term, result, err)
		}
		for _, userID := range []string{"disabled", "missing-user", "malformed"} {
			if result, err := store.SearchHints(ctx, Subject{UserID: userID}, query); !errors.Is(err, ErrForbidden) || result.TotalRecordCount != 0 || len(result.SearchHints) != 0 {
				t.Fatalf("invalid user %q with term %q returned %+v, %v", userID, term, result, err)
			}
		}
	}
	searchHintTestExec(t, ctx, store, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", key.ApplicationCredentialID)
	orphan := seedCatalogApplicationKey(t, ctx, store.pool, "search-hints-orphan-key", false)
	for _, subject := range []Subject{key, target, disabledTarget, orphan, {ApplicationCredentialID: "missing-key"}} {
		for _, term := range []string{"Key", " ", "no matching hint"} {
			query.SearchTerm = term
			if result, err := store.SearchHints(ctx, subject, query); !errors.Is(err, ErrForbidden) || result.TotalRecordCount != 0 || len(result.SearchHints) != 0 {
				t.Fatalf("invalid key %+v with term %q returned %+v, %v", subject, term, result, err)
			}
		}
	}
}

type searchHintSnapshotTraceKey struct{}

type searchHintSnapshotTracer struct {
	writer  *pgxpool.Pool
	changed bool
	err     error
}

func (trace *searchHintSnapshotTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.changed && strings.Contains(data.SQL, "SELECT is_administrator, is_disabled, policy") && strings.Contains(data.SQL, "FROM users WHERE id = $1") {
		return context.WithValue(ctx, searchHintSnapshotTraceKey{}, true)
	}
	return ctx
}

func (trace *searchHintSnapshotTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if trace.changed || data.Err != nil || ctx.Value(searchHintSnapshotTraceKey{}) != true {
		return
	}
	trace.changed = true
	_, trace.err = trace.writer.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false}' WHERE id='restricted';
		UPDATE items SET name='Snapshot replacement' WHERE id='snapshot-item';
		DELETE FROM item_entities WHERE item_id='snapshot-item';
		UPDATE artwork_images SET source_hash=repeat('f',64)`)
}

func TestStoreSearchHintsKeepsOneAuthorizedSnapshot(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	item := searchHintTestItem(t, ctx, store, "snapshot-item", "Snapshot item", "Movie", "library-b", "library-b")
	searchHintTestMetadata(t, ctx, store, item.ID, `{"Genres":["Snapshot entity"]}`)
	entity := searchHintTestEntity(t, ctx, store, "Genre", "Snapshot entity")
	itemTag, entityTag := strings.Repeat("a", 64), strings.Repeat("b", 64)
	searchHintTestArtwork(t, ctx, store, item, itemTag)
	searchHintTestArtwork(t, ctx, store, entity, entityTag)
	trace := &searchHintSnapshotTracer{writer: store.pool}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	query := SearchHintsQuery{SearchTerm: "Snapshot", Limit: 1000}
	// Commit a catalog change after the policy read establishes the snapshot.
	// Ranking, both populations, projections, and artwork must keep that view.
	result := searchHintTestQuery(t, ctx, &Store{pool: reader}, Subject{UserID: "restricted"}, query)
	if !trace.changed || trace.err != nil {
		t.Fatalf("snapshot fixture did not commit its concurrent change: changed=%t, error=%v", trace.changed, trace.err)
	}
	assertSearchHintPage(t, result, 2, entity, item)
	assertSearchHintImage(t, result, item, itemTag, true)
	assertSearchHintImage(t, result, entity, entityTag, true)
	if result.SearchHints[1].Item.Name != "Snapshot item" {
		t.Fatalf("item projection escaped the authorized snapshot: %+v", result.SearchHints[1])
	}
	assertSearchHintPage(t, searchHintTestQuery(t, ctx, store, Subject{UserID: "restricted"}, query), 0)
	updated := searchHintTestQuery(t, ctx, store, Subject{UserID: "default"}, query)
	assertSearchHintPage(t, updated, 1, item)
	assertSearchHintImage(t, updated, item, strings.Repeat("f", 64), true)
	if updated.SearchHints[0].Item.Name != "Snapshot replacement" {
		t.Fatalf("next snapshot did not observe the committed projection: %+v", updated)
	}
}
