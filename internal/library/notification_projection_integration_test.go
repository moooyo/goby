package library

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/notificationjournal"
)

func assertNotificationProjectionQueries(t *testing.T, trace *itemCountTracer, wantItems, wantEntities, wantCommits int) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	items, entities, commits := 0, 0, 0
	for _, statement := range trace.statements {
		if strings.Contains(statement, "SELECT i.id,i.library_id FROM items i WHERE i.id=ANY(") {
			items++
		}
		if strings.Contains(statement, "SELECT DISTINCT entity.id::text FROM catalog_entities entity JOIN item_entities") {
			entities++
		}
		if strings.EqualFold(strings.TrimSpace(statement), "commit") {
			commits++
		}
	}
	if items != wantItems || entities != wantEntities || commits != wantCommits {
		t.Fatalf("notification projection issued item/entity/commit queries %d/%d/%d, want %d/%d/%d", items, entities, commits, wantItems, wantEntities, wantCommits)
	}
}

func TestNotificationProjectionQueriesOnlyPresentReferenceCategories(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, fixture.pool)
	store, trace := countItemsTestStore(t, ctx, fixture)
	item := notificationjournal.Reference{Kind: "Item", ID: "movie-b", LibraryID: "library-b", SourceID: "movie-b"}
	otherSource := notificationjournal.Reference{Kind: "Item", ID: "movie-b", LibraryID: "library-b", SourceID: "episode-b1"}
	parent := notificationjournal.Reference{Kind: "Library", ID: "library-b", LibraryID: "library-b", SourceID: "movie-b"}
	hidden := notificationjournal.Reference{Kind: "Item", ID: "movie-a", LibraryID: "library-a"}
	hiddenSource := notificationjournal.Reference{Kind: "Item", ID: "library-b", LibraryID: "library-b", SourceID: "movie-a"}
	wrongLibrary := notificationjournal.Reference{Kind: "Item", ID: "movie-b", LibraryID: "library-a"}
	entity := notificationjournal.Reference{Kind: "Entity", ID: strconv.FormatInt(libraryQueryEntityID(t, ctx, fixture.pool, "Genre", "Drama"), 10)}
	hiddenEntity := notificationjournal.Reference{Kind: "Entity", ID: strconv.FormatInt(libraryQueryEntityID(t, ctx, fixture.pool, "Genre", "Private Genre"), 10)}
	for _, test := range []struct {
		name     string
		refs     []notificationjournal.Reference
		want     []notificationjournal.Reference
		items    int
		entities int
	}{
		{name: "nil references", want: []notificationjournal.Reference{}},
		{name: "empty references", refs: []notificationjournal.Reference{}, want: []notificationjournal.Reference{}},
		{name: "items and source scopes", refs: []notificationjournal.Reference{item, hidden, otherSource, hiddenSource, wrongLibrary, item}, want: []notificationjournal.Reference{item, otherSource}, items: 1},
		{name: "library source scope", refs: []notificationjournal.Reference{parent, parent, hiddenSource}, want: []notificationjournal.Reference{parent}, items: 1},
		{name: "entities", refs: []notificationjournal.Reference{hiddenEntity, entity, entity}, want: []notificationjournal.Reference{entity}, entities: 1},
		{name: "mixed ordered references", refs: []notificationjournal.Reference{entity, item, hiddenEntity, parent, otherSource, item, entity}, want: []notificationjournal.Reference{entity, item, parent, otherSource}, items: 1, entities: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.reset()
			actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, test.refs)
			if err != nil || actual == nil || !slices.Equal(actual, test.want) {
				t.Fatalf("notification projection changed visible source scopes, order or deduplication: actual=%+v, want=%+v, error=%v", actual, test.want, err)
			}
			assertNotificationProjectionQueries(t, trace, test.items, test.entities, 1)
		})
	}
}

func TestNotificationProjectionValidatesAllReferencesBeforeCategoryQueries(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	store, trace := countItemsTestStore(t, ctx, fixture)
	for _, id := range []string{"", "0", "-1", "01", "+1", "not-an-entity", "9223372036854775808"} {
		t.Run("invalid entity "+id, func(t *testing.T) {
			refs := []notificationjournal.Reference{{Kind: "Item", ID: "movie-b"}, {Kind: "Entity", ID: id}}
			trace.reset()
			if actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs); !errors.Is(err, ErrInvalidInput) || actual != nil {
				t.Fatalf("invalid entity reference was accepted: actual=%+v, error=%v", actual, err)
			}
			assertNotificationProjectionQueries(t, trace, 0, 0, 0)
		})
	}
	for _, refs := range [][]notificationjournal.Reference{
		{{Kind: "Item", ID: "movie-b"}, {Kind: "Unknown", ID: "movie-b"}},
		make([]notificationjournal.Reference, 4097),
	} {
		trace.reset()
		if actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs); !errors.Is(err, ErrInvalidInput) || actual != nil {
			t.Fatalf("invalid notification references were accepted: actual=%+v, error=%v", actual, err)
		}
		assertNotificationProjectionQueries(t, trace, 0, 0, 0)
	}
}

func TestNotificationProjectionEmptyReferencesRetainSubjectAuthorization(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	application := seedCatalogApplicationKey(t, ctx, fixture.pool, "notification-empty-projection", true)
	store, trace := countItemsTestStore(t, ctx, fixture)
	if actual, err := store.FilterNotificationReferences(ctx, application, nil); err != nil || actual == nil || len(actual) != 0 {
		t.Fatalf("authorized empty application projection = %+v, error=%v", actual, err)
	}
	assertNotificationProjectionQueries(t, trace, 0, 0, 1)
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, application.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	if actual, err := store.FilterNotificationReferences(ctx, application, nil); err == nil || actual != nil {
		t.Fatalf("revoked application credential bypassed empty projection authorization: actual=%+v, error=%v", actual, err)
	}
	assertNotificationProjectionQueries(t, trace, 0, 0, 0)
	for _, userID := range []string{"disabled", "missing-notification-user"} {
		trace.reset()
		if actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: userID}, nil); !errors.Is(err, ErrForbidden) || actual != nil {
			t.Fatalf("unavailable subject bypassed empty projection authorization: actual=%+v, error=%v", actual, err)
		}
		assertNotificationProjectionQueries(t, trace, 0, 0, 0)
	}
}

func TestNotificationProjectionRequiresVisibleSourceBeforeParentInvalidation(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "notification-hidden-source", "Movie", "library-b", "library-b", map[string]any{"Tags": []string{"Blocked"}})
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy || '{"BlockedTags":["Blocked"]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	refs := []notificationjournal.Reference{{Kind: "Item", ID: "notification-hidden-source", LibraryID: "library-b", SourceID: "notification-hidden-source"}, {Kind: "Item", ID: "library-b", LibraryID: "library-b", SourceID: "notification-hidden-source"}}
	actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs)
	if err != nil || len(actual) != 0 {
		t.Fatalf("a visible parent leaked a hidden-only source change: %+v %v", actual, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy-'BlockedTags' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	actual, err = store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs)
	if err != nil || len(actual) != 2 {
		t.Fatalf("current source permission was not observed: %+v %v", actual, err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM items WHERE id='notification-hidden-source'`); err != nil {
		t.Fatal(err)
	}
	actual, err = store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, refs)
	if err != nil || len(actual) != 0 {
		t.Fatal("deleted source authority was inferred from historical parent visibility")
	}
}
func TestNotificationProjectionNeverConfusesNumericItemAndEntityIDs(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	similarInsert(t, ctx, store, "notification-visible-audio", "Audio", "library-b", "library-b", map[string]any{"Artists": []string{"Notification artist"}})
	var id int64
	if store.pool.QueryRow(ctx, `SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Notification artist'`).Scan(&id) != nil {
		t.Fatal("read entity")
	}
	text := strconv.FormatInt(id, 10)
	similarInsert(t, ctx, store, text, "Audio", "library-a", "library-a", nil)
	actual, err := store.FilterNotificationReferences(ctx, Subject{UserID: "restricted"}, []notificationjournal.Reference{{Kind: "Item", ID: text}, {Kind: "Entity", ID: text}})
	if err != nil || len(actual) != 1 || actual[0].Kind != "Entity" {
		t.Fatalf("typed reference changed its authority namespace: %+v %v", actual, err)
	}
}
