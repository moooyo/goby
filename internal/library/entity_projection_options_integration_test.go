package library

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func assertNoArtworkProjectionQueries(t *testing.T, trace *itemCountTracer) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	for _, statement := range trace.statements {
		for _, table := range []string{"artwork_state", "artwork_images", "item_images", "item_provider_images", "item_embedded_artwork"} {
			if strings.Contains(statement, table) {
				t.Fatalf("disabled images still queried %s", table)
			}
		}
	}
}

func TestEntityProjectionDisablesArtworkWithoutChangingVisibilityOrUserData(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, fixture.pool)
	id := libraryQueryEntityID(t, ctx, fixture.pool, "Genre", "Drama")
	searchHintTestArtwork(t, ctx, fixture, SearchHintReference{Kind: "Entity", ID: strconv.FormatInt(id, 10)}, strings.Repeat("a", 64))
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO entity_user_data(user_id,entity_id,is_favorite) VALUES('restricted',$1,true)`, id); err != nil {
		t.Fatal(err)
	}
	store, trace := countItemsTestStore(t, ctx, fixture)
	query := Query{UserID: "restricted", Limit: 100}
	complete, err := store.ListEntities(ctx, "Genre", query)
	if err != nil || len(complete.Items) < 2 {
		t.Fatalf("entity fixture = %+v, %v", complete, err)
	}
	for index := range complete.Items {
		complete.Items[index].Images = []Image{}
	}
	query.Projection.ImagesDisabled = true
	trace.reset()
	narrow, err := store.ListEntities(ctx, "Genre", query)
	if err != nil || !reflect.DeepEqual(complete, narrow) {
		t.Fatalf("disabled entity images changed non-image data: complete=%+v narrow=%+v err=%v", complete, narrow, err)
	}
	assertNoArtworkProjectionQueries(t, trace)
	trace.reset()
	detail, err := store.GetEntityByIDFor(ctx, Subject{UserID: "restricted"}, id, query.Projection)
	if err != nil || len(detail.Images) != 0 || detail.UserData == nil || !detail.UserData.IsFavorite {
		t.Fatalf("disabled detail lost user data or loaded images: %+v, %v", detail, err)
	}
	assertNoArtworkProjectionQueries(t, trace)
}

func TestSearchHintsProjectionDisablesAllArtworkReads(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	item := searchHintTestItem(t, ctx, fixture, "projection-hint", "Projection hint", "Movie", "library-b", "library-b")
	searchHintTestMetadata(t, ctx, fixture, item.ID, `{"Genres":["Projection hint genre"]}`)
	genre := searchHintTestEntity(t, ctx, fixture, "Genre", "Projection hint genre")
	searchHintTestArtwork(t, ctx, fixture, item, strings.Repeat("b", 64))
	searchHintTestArtwork(t, ctx, fixture, genre, strings.Repeat("c", 64))
	store, trace := countItemsTestStore(t, ctx, fixture)
	query := SearchHintsQuery{SearchTerm: "Projection hint", Limit: 20}
	subject := Subject{UserID: "restricted"}
	complete, err := store.SearchHints(ctx, subject, query)
	if err != nil || len(complete.SearchHints) != 2 {
		t.Fatalf("hint fixture = %+v, %v", complete, err)
	}
	for index := range complete.SearchHints {
		hint := &complete.SearchHints[index]
		hint.Images = []Image{}
		if hint.Entity != nil {
			hint.Entity.Images = []Image{}
		}
	}
	query.Projection.ImagesDisabled = true
	trace.reset()
	narrow, err := store.SearchHints(ctx, subject, query)
	if err != nil || !reflect.DeepEqual(complete, narrow) {
		t.Fatalf("disabled hint images changed hint contents: complete=%+v narrow=%+v err=%v", complete, narrow, err)
	}
	assertNoArtworkProjectionQueries(t, trace)
}
