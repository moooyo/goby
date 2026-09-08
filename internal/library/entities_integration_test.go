package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func entitiesList(t *testing.T, ctx context.Context, store *Store, kind string, query Query) EntityResult {
	t.Helper()
	result, err := store.ListEntities(ctx, kind, query)
	if err != nil {
		t.Fatalf("list %s entities: %v", kind, err)
	}
	return result
}

func entitiesNamed(t *testing.T, result EntityResult, name string) Entity {
	t.Helper()
	for _, entity := range result.Items {
		if entity.Name == name {
			if entity.ID <= 0 {
				t.Fatalf("entity %q has an invalid numeric ID: %+v", name, entity)
			}
			return entity
		}
	}
	t.Fatalf("entity %q was not returned: %+v", name, result)
	return Entity{}
}

func entitiesRefID(t *testing.T, refs []EntityRef, name string) int64 {
	t.Helper()
	for _, ref := range refs {
		if ref.Name == name {
			if ref.ID <= 0 {
				t.Fatalf("item entity %q has an invalid numeric ID: %+v", name, ref)
			}
			return ref.ID
		}
	}
	t.Fatalf("item entity %q was not returned: %+v", name, refs)
	return 0
}

func entitiesPersonID(t *testing.T, person PersonRef) int64 {
	t.Helper()
	id, err := strconv.ParseInt(person.ID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != person.ID {
		t.Fatalf("person ID is not canonical positive decimal text: person = %+v, error = %v", person, err)
	}
	return id
}

func entitiesLongName(t *testing.T) string {
	t.Helper()
	value := make([]byte, 2048)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate long entity name: %v", err)
	}
	return hex.EncodeToString(value)
}

func entitiesStoreCleanup(t *testing.T, store *Store) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("close entity test store: %v", err)
		}
	})
}

func TestStoreEntityIDsSurviveRescansRestartAndAssociationDeletion(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Film.mp4", "video:entity-lifecycle")
	document := `<movie><title>Entity Movie</title><genre>Drama</genre><tag>Favorite</tag><studio>Example Studio</studio><actor><name>Example Person</name><role>Lead</role><order>0</order></actor></movie>`
	sidecar := libraryIntegrationFile(t, allowedRoot, "movies/Film.nfo", document)
	library := libraryIntegrationCreate(t, ctx, store, "Entity movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if len(first.Entities.Genres) != 1 || len(first.Entities.Tags) != 1 || len(first.Entities.Studios) != 1 || len(first.Entities.People) != 1 {
		t.Fatalf("initial entity references = %+v", first.Entities)
	}
	genreID := entitiesRefID(t, first.Entities.Genres, "Drama")
	tagID := entitiesRefID(t, first.Entities.Tags, "Favorite")
	studioID := entitiesRefID(t, first.Entities.Studios, "Example Studio")
	personID := entitiesPersonID(t, first.Entities.People[0])
	initialIDs := []int64{genreID, tagID, studioID, personID}
	for _, fixture := range []struct {
		kind, name string
		id         int64
	}{{"Genre", "Drama", genreID}, {"Tag", "Favorite", tagID}, {"Studio", "Example Studio", studioID}, {"Person", "Example Person", personID}} {
		entity, err := store.GetEntity(ctx, userID, fixture.kind, fixture.name)
		if err != nil || entity.ID != fixture.id || entity.Type != fixture.kind || entity.Count != 1 {
			t.Errorf("initial entity lookup = %+v, error = %v", entity, err)
		}
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Added != 0 || job.Updated != 0 {
		t.Errorf("unchanged entity scan modified media: %+v", job)
	}
	if again := nfoCatalogItem(t, ctx, store, userID, library.ID, path); !reflect.DeepEqual(again.Entities, first.Entities) {
		t.Errorf("rescan changed entity IDs or credits: %+v", again.Entities)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("close store before entity persistence check: %v", err)
	}
	reopened, err := New(pool, prober, []string{allowedRoot})
	if err != nil {
		t.Fatalf("reopen entity catalog: %v", err)
	}
	entitiesStoreCleanup(t, reopened)
	store = reopened
	if persisted, err := store.GetItem(ctx, userID, first.ID); err != nil || !reflect.DeepEqual(persisted.Entities, first.Entities) {
		t.Fatalf("restart changed persisted entity references: item = %+v, error = %v", persisted, err)
	}
	if calls := len(prober.calls()); calls != 1 {
		t.Fatalf("unchanged scan or restart repeated probing: calls = %d, want 1", calls)
	}
	libraryIntegrationFile(t, allowedRoot, "movies/Film.nfo", `<movie><title>Changed Entity Movie</title><genre>Comedy</genre><tag>Reviewed</tag></movie>`)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	changed := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if changed.ID != first.ID || len(changed.Entities.Genres) != 1 || len(changed.Entities.Tags) != 1 || len(changed.Entities.Studios) != 0 || len(changed.Entities.People) != 0 {
		t.Fatalf("NFO change retained stale entity associations: %+v", changed)
	}
	entitiesRefID(t, changed.Entities.Genres, "Comedy")
	entitiesRefID(t, changed.Entities.Tags, "Reviewed")
	for _, id := range initialIDs {
		if _, err := store.GetEntityByID(ctx, userID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("orphan entity %d remained visible: got %v, want ErrNotFound", id, err)
		}
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM catalog_entities WHERE id = $1)", id).Scan(&exists); err != nil || !exists {
			t.Errorf("orphan entity %d lost its stable ID: exists = %v, error = %v", id, exists, err)
		}
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatalf("remove entity NFO fixture: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	cleared := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if cleared.Metadata != nil || len(cleared.Entities.Genres)+len(cleared.Entities.Tags)+len(cleared.Entities.Studios)+len(cleared.Entities.People) != 0 {
		t.Errorf("sidecar deletion left entity references: %+v", cleared)
	}
	var associationCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_entities WHERE item_id = $1", first.ID).Scan(&associationCount); err != nil || associationCount != 0 {
		t.Errorf("sidecar deletion retained %d associations, error = %v", associationCount, err)
	}
	libraryIntegrationFile(t, allowedRoot, "movies/Film.nfo", document)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if restored := nfoCatalogItem(t, ctx, store, userID, library.ID, path); !reflect.DeepEqual(restored.Entities, first.Entities) {
		t.Errorf("recreating NFO metadata allocated new entity IDs: %+v", restored.Entities)
	}
	if err := store.DeleteLibrary(ctx, library.ID); err != nil {
		t.Fatalf("delete associated library: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_entities").Scan(&associationCount); err != nil || associationCount != 0 {
		t.Errorf("library deletion retained %d associations, error = %v", associationCount, err)
	}
	for _, id := range initialIDs {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM catalog_entities WHERE id = $1)", id).Scan(&exists); err != nil || !exists {
			t.Errorf("library deletion removed entity %d: exists = %v, error = %v", id, exists, err)
		}
		if _, err := store.GetEntityByID(ctx, userID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("library deletion left orphan entity %d visible: %v", id, err)
		}
	}
	for _, fixture := range []struct{ kind, name string }{{"Genre", "Drama"}, {"Tag", "Favorite"}, {"Studio", "Example Studio"}, {"Person", "Example Person"}} {
		if listed := entitiesList(t, ctx, store, fixture.kind, Query{UserID: userID}); listed.TotalRecordCount != 0 || len(listed.Items) != 0 {
			t.Errorf("orphan %s records remained browsable: %+v", fixture.kind, listed)
		}
		if _, err := store.GetEntity(ctx, userID, fixture.kind, fixture.name); !errors.Is(err, ErrNotFound) {
			t.Errorf("orphan %s name lookup: got %v, want ErrNotFound", fixture.kind, err)
		}
	}
	replacement := libraryIntegrationCreate(t, ctx, store, "Replacement library", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, replacement.ID, "Completed")
	if rebound := nfoCatalogItem(t, ctx, store, userID, replacement.ID, path); !reflect.DeepEqual(rebound.Entities, first.Entities) {
		t.Errorf("new library did not reuse retained entity IDs: %+v", rebound.Entities)
	}
}

func TestStorePersonEntitiesPreserveMultipleOrderedCredits(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	firstPath := libraryIntegrationFile(t, allowedRoot, "movies/First.mp4", "video:multiple-credits")
	libraryIntegrationFile(t, allowedRoot, "movies/Second.mp4", "video:shared-person")
	libraryIntegrationFile(t, allowedRoot, "movies/First.nfo", `<movie><title>First Movie</title><actor><name>Multi Credit</name><role>Second</role><order>2</order></actor><actor><name>Multi Credit</name><role>First</role><order>0</order></actor><director>Multi Credit</director></movie>`)
	libraryIntegrationFile(t, allowedRoot, "movies/Second.nfo", `<movie><title>Second Movie</title><actor><name>Multi Credit</name><role>Guest</role><order>0</order></actor></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "People", "movies", filepath.Dir(firstPath))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	people := first.Entities.People
	if len(people) != 3 {
		t.Fatalf("multiple credits were collapsed: %+v", people)
	}
	id := entitiesPersonID(t, people[0])
	for index, person := range people {
		if person.Name != "Multi Credit" || entitiesPersonID(t, person) != id {
			t.Errorf("credit %d has a different person identity: %+v", index, person)
		}
	}
	if people[0].Type != "Actor" || people[0].Role != "First" || people[0].SortOrder == nil || *people[0].SortOrder != 0 || people[1].Type != "Actor" || people[1].Role != "Second" || people[1].SortOrder == nil || *people[1].SortOrder != 2 || people[2].Type != "Director" || people[2].Role != "" || people[2].SortOrder != nil {
		t.Errorf("person credit ordering or per-item fields were lost: %+v", people)
	}
	listed := entitiesList(t, ctx, store, "Person", Query{UserID: userID, ParentID: library.ID})
	entity := entitiesNamed(t, listed, "Multi Credit")
	if listed.TotalRecordCount != 1 || entity.ID != id || entity.Count != 2 {
		t.Errorf("person count must use distinct source items, not credits: %+v", listed)
	}
	directors := Query{UserID: userID, Recursive: true, PersonIds: []string{strconv.FormatInt(id, 10)}, PersonTypes: []string{"Director"}}
	items := libraryIntegrationQuery(t, ctx, store, directors)
	if items.TotalRecordCount != 1 || len(items.Items) != 1 || items.Items[0].ID != first.ID {
		t.Errorf("person credit type filter selected incorrect source items: %+v", items)
	}
	directorEntities := entitiesList(t, ctx, store, "Person", directors)
	if directorEntities.TotalRecordCount != 1 || len(directorEntities.Items) != 1 || directorEntities.Items[0].Count != 1 {
		t.Errorf("entity aggregation ignored the person credit type source filter: %+v", directorEntities)
	}
}

func TestStoreEntityQueriesAuthorizeSourcesBeforeCountingAndPaging(t *testing.T) {
	ctx, pool, store, allowedRoot, unrestrictedID := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, allowedRoot, "hidden/A Hidden.mp4", "video:hidden-entities")
	libraryIntegrationFile(t, allowedRoot, "hidden/A Hidden.nfo", `<movie><title>A Hidden</title><genre>A Hidden Genre</genre><genre>Shared</genre><tag>Common</tag><studio>Shared Studio</studio><actor><name>Shared Person</name><role>Hidden</role></actor></movie>`)
	firstPath := libraryIntegrationFile(t, allowedRoot, "visible/B Visible.mp4", "video:visible-b")
	lastPath := libraryIntegrationFile(t, allowedRoot, "visible/C Visible.mp4", "video:visible-c")
	libraryIntegrationFile(t, allowedRoot, "visible/B Visible.nfo", `<movie><title>B Visible</title><genre>Shared</genre><genre>Visible B</genre><tag>Common</tag><studio>Shared Studio</studio><actor><name>Shared Person</name><role>Lead</role></actor><director>Shared Person</director></movie>`)
	libraryIntegrationFile(t, allowedRoot, "visible/C Visible.nfo", `<movie><title>C Visible</title><genre>Shared</genre><genre>Visible C</genre><tag>Common</tag><studio>Shared Studio</studio><actor><name>Shared Person</name><role>Guest</role></actor></movie>`)
	libraryIntegrationFile(t, allowedRoot, "visible/D Unmatched.mp4", "video:visible-unmatched")
	libraryIntegrationFile(t, allowedRoot, "visible/D Unmatched.nfo", `<movie><title>D Unmatched</title><genre>Z Other Genre</genre><tag>Other Tag</tag><studio>Other Studio</studio><actor><name>Other Person</name><role>Other</role></actor></movie>`)
	libraryIntegrationFile(t, allowedRoot, "visible/Album/01 Track.flac", "audio:entity-album")
	libraryIntegrationFile(t, allowedRoot, "visible/Album/album.nfo", `<album><title>Album Source</title><genre>Shared</genre><genre>Album Only</genre></album>`)
	hidden := libraryIntegrationCreate(t, ctx, store, "Hidden entities", "movies", filepath.Join(allowedRoot, "hidden"))
	visible := libraryIntegrationCreate(t, ctx, store, "Visible entities", "mixed", filepath.Join(allowedRoot, "visible"))
	libraryIntegrationScan(t, ctx, store, hidden.ID, "Completed")
	libraryIntegrationScan(t, ctx, store, visible.ID, "Completed")
	viewerID, emptyID, disabledID := "entity-viewer", "entity-empty", "entity-disabled"
	libraryIntegrationUser(t, ctx, pool, viewerID, false, false, []string{visible.ID})
	libraryIntegrationUser(t, ctx, pool, emptyID, false, false, nil)
	libraryIntegrationUser(t, ctx, pool, disabledID, true, true, nil)
	genres := entitiesList(t, ctx, store, "genres", Query{UserID: viewerID, ParentID: visible.ID})
	shared := entitiesNamed(t, genres, "Shared")
	visibleB := entitiesNamed(t, genres, "Visible B")
	visibleC := entitiesNamed(t, genres, "Visible C")
	entitiesNamed(t, genres, "Album Only")
	if genres.TotalRecordCount != 5 || len(genres.Items) != 5 || shared.Count != 3 {
		t.Fatalf("entity list failed to use authorized recursive sources: %+v", genres)
	}
	page := entitiesList(t, ctx, store, "Genre", Query{UserID: viewerID, StartIndex: 1, Limit: 1})
	if page.TotalRecordCount != 5 || len(page.Items) != 1 || page.Items[0].ID != shared.ID || page.Items[0].Count != 3 {
		t.Errorf("entity authorization must precede counting and pagination: %+v", page)
	}
	movieGenres := entitiesList(t, ctx, store, "Genre", Query{UserID: viewerID, ParentID: visible.ID, IncludeItemTypes: []string{"Movie"}})
	if movieGenres.TotalRecordCount != 4 || entitiesNamed(t, movieGenres, "Shared").Count != 2 {
		t.Errorf("entity counts ignored source item types: %+v", movieGenres)
	}
	searched := entitiesList(t, ctx, store, "Genre", Query{UserID: viewerID, SearchTerm: "Visible B"})
	if searched.TotalRecordCount != 1 || len(searched.Items) != 1 || searched.Items[0].ID != visibleB.ID {
		t.Errorf("entity search term was applied to source item names: %+v", searched)
	}
	byName, err := store.GetEntity(ctx, viewerID, "genre", "sHaReD")
	if err != nil || byName.ID != shared.ID || byName.Count != 3 {
		t.Errorf("authorized entity detail = %+v, error = %v", byName, err)
	}
	hiddenEntity, err := store.GetEntity(ctx, unrestrictedID, "Genre", "A Hidden Genre")
	if err != nil {
		t.Fatalf("read hidden entity as unrestricted user: %v", err)
	}
	if _, err := store.GetEntity(ctx, viewerID, "Genre", hiddenEntity.Name); !errors.Is(err, ErrNotFound) {
		t.Errorf("hidden entity name lookup: got %v, want ErrNotFound", err)
	}
	if _, err := store.GetEntityByID(ctx, viewerID, hiddenEntity.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("hidden entity ID lookup: got %v, want ErrNotFound", err)
	}
	if result := entitiesList(t, ctx, store, "Genre", Query{UserID: emptyID}); result.TotalRecordCount != 0 || len(result.Items) != 0 {
		t.Errorf("empty library policy exposed entities: %+v", result)
	}
	if _, err := store.ListEntities(ctx, "Genre", Query{UserID: disabledID}); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled entity listing: got %v, want ErrForbidden", err)
	}
	if _, err := store.GetEntityByID(ctx, disabledID, shared.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabled entity detail: got %v, want ErrForbidden", err)
	}
	first := nfoCatalogItem(t, ctx, store, viewerID, visible.ID, firstPath)
	tagID := entitiesRefID(t, first.Entities.Tags, "Common")
	studioID := entitiesRefID(t, first.Entities.Studios, "Shared Studio")
	if len(first.Entities.People) != 2 {
		t.Fatalf("source item lost multiple person credits: %+v", first.Entities.People)
	}
	personID := first.Entities.People[0].ID
	for _, filter := range []struct {
		name  string
		query Query
	}{
		{"GenreIDs", Query{GenreIds: []int64{shared.ID, hiddenEntity.ID}}},
		{"GenreNames", Query{Genres: []string{"Shared", "A Hidden Genre"}}},
		{"TagIDs", Query{TagIds: []int64{tagID}}},
		{"TagNames", Query{Tags: []string{"Common"}}},
		{"StudioIDs", Query{StudioIds: []int64{studioID}}},
		{"StudioNames", Query{Studios: []string{"Shared Studio"}}},
		{"PersonIDs", Query{PersonIds: []string{personID}}},
		{"PersonName", Query{Person: "Shared Person"}},
	} {
		t.Run(filter.name, func(t *testing.T) {
			query := filter.query
			query.UserID, query.Recursive, query.IncludeItemTypes = viewerID, true, []string{"Movie"}
			query.SortBy, query.SortOrder, query.StartIndex, query.Limit = "Name", "Ascending", 1, 1
			result := libraryIntegrationQuery(t, ctx, store, query)
			if result.TotalRecordCount != 2 || len(result.Items) != 1 || result.Items[0].Path != lastPath {
				t.Errorf("source facet filtering counted hidden or duplicate items before paging: %+v", result)
			}
		})
	}
	combined := libraryIntegrationQuery(t, ctx, store, Query{UserID: viewerID, Recursive: true, Genres: []string{"Visible B", "Visible C"}, Tags: []string{"Common"}})
	if combined.TotalRecordCount != 2 || len(combined.Items) != 2 {
		t.Errorf("name facets must OR within a dimension and AND across dimensions: %+v", combined)
	}
	combinedIDs := libraryIntegrationQuery(t, ctx, store, Query{UserID: viewerID, Recursive: true, GenreIds: []int64{visibleB.ID, visibleC.ID}, StudioIds: []int64{studioID}})
	if combinedIDs.TotalRecordCount != 2 || len(combinedIDs.Items) != 2 {
		t.Errorf("numeric facets must OR within a dimension and AND across dimensions: %+v", combinedIDs)
	}
	noMatch := libraryIntegrationQuery(t, ctx, store, Query{UserID: viewerID, Recursive: true, GenreIds: []int64{shared.ID}, Tags: []string{"Missing Tag"}})
	if noMatch.TotalRecordCount != 0 || len(noMatch.Items) != 0 {
		t.Errorf("cross-dimension filters were combined with OR: %+v", noMatch)
	}
	filteredEntities := entitiesList(t, ctx, store, "Genre", Query{UserID: viewerID, TagIds: []int64{tagID}, IncludeItemTypes: []string{"Movie"}})
	if filteredEntities.TotalRecordCount != 3 || entitiesNamed(t, filteredEntities, "Shared").Count != 2 {
		t.Errorf("entity listing did not apply source facet filters: %+v", filteredEntities)
	}
}

func TestStoreEntityAssociationFailureRollsBackMediaAndMetadata(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Atomic.mp4", "video:original")
	libraryIntegrationFile(t, allowedRoot, "movies/Atomic.nfo", `<movie><title>Original Atomic Movie</title><genre>Shared</genre><tag>Old Tag</tag><studio>Old Studio</studio></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Atomic metadata", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	original := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	sharedID := entitiesRefID(t, original.Entities.Genres, "Shared")
	if original.Media == nil || len(original.Entities.Tags) != 1 || len(original.Entities.Studios) != 1 {
		t.Fatalf("initial atomic fixture is incomplete: %+v", original)
	}
	snapshot := func() (string, string) {
		t.Helper()
		var item, associations string
		if err := pool.QueryRow(ctx, "SELECT to_jsonb(i)::text FROM items i WHERE id = $1", original.ID).Scan(&item); err != nil {
			t.Fatalf("snapshot technical item and local metadata: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY entity_id, position), '[]'::jsonb)::text
			FROM item_entities e WHERE item_id = $1`, original.ID).Scan(&associations); err != nil {
			t.Fatalf("snapshot item entity associations: %v", err)
		}
		return item, associations
	}
	beforeItem, beforeAssociations := snapshot()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin entity failure fixture setup: %v", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `CREATE SEQUENCE entity_association_failure_hits;
		CREATE FUNCTION reject_entity_association_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM nextval('entity_association_failure_hits');
			RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'Injected entity association failure';
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_entity_association_for_test BEFORE INSERT ON item_entities
		FOR EACH ROW EXECUTE FUNCTION reject_entity_association_for_test()`); err != nil {
		t.Fatalf("install entity association failure fixture: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit entity failure fixture setup: %v", err)
	}
	removeInjection := func(cleanupCtx context.Context) error {
		cleanupTx, err := pool.Begin(cleanupCtx)
		if err != nil {
			return err
		}
		defer rollback(cleanupTx)
		if _, err := cleanupTx.Exec(cleanupCtx, `SET LOCAL lock_timeout = '2s';
			DROP TRIGGER IF EXISTS reject_entity_association_for_test ON item_entities;
			DROP FUNCTION IF EXISTS reject_entity_association_for_test();
			DROP SEQUENCE IF EXISTS entity_association_failure_hits`); err != nil {
			return err
		}
		return cleanupTx.Commit(cleanupCtx)
	}
	injectionActive := true
	t.Cleanup(func() {
		if injectionActive {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := removeInjection(cleanupCtx); err != nil {
				t.Errorf("remove owned entity failure fixture: %v", err)
			}
		}
	})
	libraryIntegrationFile(t, allowedRoot, "movies/Atomic.mp4", "video:changed-technical-media-for-rollback")
	libraryIntegrationFile(t, allowedRoot, "movies/Atomic.nfo", `<movie><title>Updated Atomic Movie</title><genre>Shared</genre><genre>New Genre</genre><tag>New Tag</tag></movie>`)
	failed := libraryIntegrationScan(t, ctx, store, library.ID, "Failed")
	if failed.Error == "" {
		t.Error("failed association insert did not record a scan error")
	}
	var injected bool
	if err := pool.QueryRow(ctx, "SELECT is_called FROM entity_association_failure_hits").Scan(&injected); err != nil || !injected {
		t.Fatalf("scan did not reach the injected association failure: reached = %v, error = %v", injected, err)
	}
	if calls := len(prober.calls()); calls != 2 {
		t.Fatalf("atomic rollback fixture did not reprobe changed media: calls = %d, want 2", calls)
	}
	afterItem, afterAssociations := snapshot()
	if afterItem != beforeItem || afterAssociations != beforeAssociations {
		t.Errorf("association failure partially committed item or metadata changes: item before = %s, item after = %s, associations before = %s, associations after = %s", beforeItem, afterItem, beforeAssociations, afterAssociations)
	}
	var failedEntityCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM catalog_entities WHERE name = ANY($1::text[])", []string{"New Genre", "New Tag"}).Scan(&failedEntityCount); err != nil || failedEntityCount != 0 {
		t.Errorf("failed association transaction committed %d new entities, error = %v", failedEntityCount, err)
	}
	if retained, err := store.GetItem(ctx, userID, original.ID); err != nil || !reflect.DeepEqual(retained, original) {
		t.Errorf("association failure changed public item metadata or references: item = %+v, error = %v", retained, err)
	}
	if !store.Available() {
		t.Error("ordinary association SQL error made ownership unavailable")
	}
	if err := store.CheckOwnership(ctx); err != nil {
		t.Fatalf("association rollback damaged ownership: %v", err)
	}
	if err := removeInjection(ctx); err != nil {
		t.Fatalf("remove association failure before retry: %v", err)
	}
	injectionActive = false
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	updated := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if updated.ID != original.ID || updated.Name != "Updated Atomic Movie" || updated.Metadata == nil || updated.Metadata.Name != updated.Name || updated.Media == nil || updated.Media.Size == original.Media.Size || len(updated.Entities.Genres) != 2 || len(updated.Entities.Tags) != 1 || len(updated.Entities.Studios) != 0 {
		t.Fatalf("retry did not commit technical media, metadata, and references together: %+v", updated)
	}
	if entitiesRefID(t, updated.Entities.Genres, "Shared") != sharedID {
		t.Error("successful retry changed a shared entity ID")
	}
	entitiesRefID(t, updated.Entities.Genres, "New Genre")
	entitiesRefID(t, updated.Entities.Tags, "New Tag")
}

func TestStoreLongEntityNamesRemainFullyAddressable(t *testing.T) {
	ctx, _, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	genreName, tagName, studioName, personName := entitiesLongName(t), entitiesLongName(t), entitiesLongName(t), entitiesLongName(t)
	path := libraryIntegrationFile(t, allowedRoot, "movies/Long Entities.mp4", "video:long-entities")
	document := `<movie><title>Long Entities</title><genre>` + genreName + `</genre><tag>` + tagName +
		`</tag><studio>` + studioName + `</studio><actor><name>` + personName + `</name><role>Lead</role><order>0</order></actor></movie>`
	libraryIntegrationFile(t, allowedRoot, "movies/Long Entities.nfo", document)
	library := libraryIntegrationCreate(t, ctx, store, "Long entity names", "movies", filepath.Dir(path))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" {
		t.Errorf("valid long entity names produced a scan warning: %q", job.Error)
	}
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if item.Name != "Long Entities" || len(item.Entities.Genres) != 1 || len(item.Entities.Tags) != 1 || len(item.Entities.Studios) != 1 || len(item.Entities.People) != 1 {
		t.Fatalf("long entity names were not persisted in item references: %+v", item)
	}
	for _, fixture := range []struct {
		kind, name string
		id         int64
	}{
		{"Genre", genreName, entitiesRefID(t, item.Entities.Genres, genreName)},
		{"Tag", tagName, entitiesRefID(t, item.Entities.Tags, tagName)},
		{"Studio", studioName, entitiesRefID(t, item.Entities.Studios, studioName)},
		{"Person", personName, entitiesPersonID(t, item.Entities.People[0])},
	} {
		entity, err := store.GetEntity(ctx, userID, fixture.kind, fixture.name)
		if err != nil || entity.ID != fixture.id || entity.Name != fixture.name || entity.Type != fixture.kind || entity.Count != 1 {
			t.Errorf("long %s name lookup lost its original name or ID: entity = %+v, error = %v", fixture.kind, entity, err)
		}
	}
	if item.Entities.People[0].Name != personName {
		t.Error("long person credit name was truncated")
	}
}

func entitiesLegacyTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create legacy migration administrator pool")
	}
	libraryIntegrationPoolCleanup(t, admin)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate legacy schema name: %v", err)
	}
	schema := "goby_entities_legacy_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create owned legacy schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned legacy schema: %v", err)
		}
	})
	config := admin.Config()
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated legacy migration pool")
	}
	libraryIntegrationPoolCleanup(t, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin legacy schema setup: %v", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create legacy migration history: %v", err)
	}
	for index, name := range []string{"0001_identity.sql", "0002_library.sql", "0003_local_metadata.sql"} {
		content, err := os.ReadFile(filepath.Join("..", "database", "migrations", name))
		if err != nil {
			t.Fatalf("read legacy migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", int64(index+1), name); err != nil {
			t.Fatalf("record legacy migration %s: %v", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit legacy schema setup: %v", err)
	}
	return ctx, pool
}

func TestStoreEntityMigrationBackfillsLocalMetadataWithoutScanning(t *testing.T) {
	ctx, pool := entitiesLegacyTestPool(t)
	const userID, libraryID, rootID, itemID = "legacy-entity-viewer", "legacy-entity-library", "legacy-entity-root", "legacy-entity-movie"
	allowedRoot := t.TempDir()
	libraryIntegrationUser(t, ctx, pool, userID, false, true, nil)
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 3 {
		t.Fatalf("legacy schema version = %d, error = %v, want 3", version, err)
	}
	var entitiesAbsent bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('catalog_entities') IS NULL").Scan(&entitiesAbsent); err != nil || !entitiesAbsent {
		t.Fatalf("legacy schema unexpectedly contains entity tables: absent = %v, error = %v", entitiesAbsent, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ($1, 'Legacy library', 'movies')`, libraryID); err != nil {
		t.Fatalf("insert legacy library: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ($1, $2, $3, $3, '.')`, rootID, libraryID, allowedRoot); err != nil {
		t.Fatalf("insert legacy library root: %v", err)
	}
	longGenre := entitiesLongName(t)
	legacyMetadata := []byte(`{"Kind":"movie","Name":"Legacy Movie","Overview":"Legacy overview.","Genres":["Legacy Genre","` + longGenre + `"],"Tags":["Legacy Tag"],"Studios":["Legacy Studio"],"People":[{"Name":"Legacy Person","Role":"Lead","Type":"Actor","SortOrder":0}]}`)
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, is_folder, path, overview, local_metadata, local_metadata_hash, local_metadata_path)
		VALUES ($1, $1, NULL, NULL, 'Legacy library', 'legacy library', 'CollectionFolder', true, '', '', NULL, '', ''),
		($2, $1, $3, $1, 'Legacy Movie', 'legacy movie', 'Movie', false, $4, 'Legacy overview.', $5::jsonb, repeat('a', 64), 'Legacy.nfo')`,
		libraryID, itemID, rootID, filepath.Join(allowedRoot, "Legacy.mp4"), legacyMetadata); err != nil {
		t.Fatalf("insert legacy catalog items: %v", err)
	}
	assertPersistedCounts := func() {
		t.Helper()
		var entities, associations, jobs int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM catalog_entities),
			(SELECT count(*) FROM item_entities WHERE item_id = $1), (SELECT count(*) FROM scan_jobs)`, itemID).
			Scan(&entities, &associations, &jobs); err != nil {
			t.Fatalf("read backfilled catalog counts: %v", err)
		}
		if entities != 5 || associations != 5 || jobs != 0 {
			t.Fatalf("backfilled counts = entities %d, associations %d, jobs %d; want 5, 5, 0", entities, associations, jobs)
		}
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade legacy metadata catalog: %v", err)
	}
	// Check backfill before constructing a store, with no media file to scan.
	assertPersistedCounts()
	prober := &libraryFixtureProber{}
	store, err := New(pool, prober, []string{allowedRoot})
	if err != nil {
		t.Fatalf("open migrated catalog without scanning: %v", err)
	}
	entitiesStoreCleanup(t, store)
	item, err := store.GetItem(ctx, userID, itemID)
	if err != nil || item.Metadata == nil || item.Metadata.Name != "Legacy Movie" || item.Metadata.Overview != "Legacy overview." {
		t.Fatalf("migration lost legacy local metadata: item = %+v, error = %v", item, err)
	}
	assertVisibleEntity := func(kind, name string, id int64) {
		t.Helper()
		want := Entity{ID: id, Name: name, Type: kind, Count: 1}
		listed := entitiesList(t, ctx, store, kind, Query{UserID: userID, ParentID: libraryID})
		wantCount := 1
		if kind == "Genre" {
			wantCount = 2
		}
		if listed.TotalRecordCount != wantCount || len(listed.Items) != wantCount || entitiesNamed(t, listed, name) != want {
			t.Fatalf("migrated %s listing = %+v, want %+v", kind, listed, want)
		}
		if byName, err := store.GetEntity(ctx, userID, kind, name); err != nil || byName != want {
			t.Errorf("migrated %s name lookup = %+v, error = %v", kind, byName, err)
		}
		if byID, err := store.GetEntityByID(ctx, userID, id); err != nil || byID != want {
			t.Errorf("migrated %s ID lookup = %+v, error = %v", kind, byID, err)
		}
	}
	for _, fixture := range []struct {
		kind, name string
		refs       []EntityRef
	}{{"Genre", "Legacy Genre", item.Entities.Genres}, {"Tag", "Legacy Tag", item.Entities.Tags}, {"Studio", "Legacy Studio", item.Entities.Studios}} {
		wantCount := 1
		if fixture.kind == "Genre" {
			wantCount = 2
		}
		if len(fixture.refs) != wantCount {
			t.Fatalf("migrated %s references = %+v", fixture.kind, fixture.refs)
		}
		assertVisibleEntity(fixture.kind, fixture.name, entitiesRefID(t, fixture.refs, fixture.name))
	}
	assertVisibleEntity("Genre", longGenre, entitiesRefID(t, item.Entities.Genres, longGenre))
	if len(item.Entities.People) != 1 {
		t.Fatalf("migrated people = %+v, want one credit", item.Entities.People)
	}
	person := item.Entities.People[0]
	if person.Name != "Legacy Person" || person.Role != "Lead" || person.Type != "Actor" || person.SortOrder == nil || *person.SortOrder != 0 {
		t.Fatalf("migrated person credit = %+v", person)
	}
	assertVisibleEntity("Person", person.Name, entitiesPersonID(t, person))
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat entity migration: %v", err)
	}
	if repeated, err := store.GetItem(ctx, userID, itemID); err != nil || !reflect.DeepEqual(repeated, item) {
		t.Fatalf("repeated migration changed catalog data or entity IDs: item = %+v, error = %v", repeated, err)
	}
	assertPersistedCounts()
	if calls := len(prober.calls()); calls != 0 {
		t.Errorf("migration or startup probed media %d times, want 0", calls)
	}
}
