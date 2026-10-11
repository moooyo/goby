package library

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type scanCollectionAncestorsObservedTx struct {
	pgx.Tx
	t              *testing.T
	protected      context.Context
	budget         *scanReconciliationBudgetState
	queries        int
	recursive      bool
	ids            []string
	libraryIDs     []string
	reservedAtRead int
	failQuery      bool
}

func (tx *scanCollectionAncestorsObservedTx) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	tx.t.Helper()
	if ctx != tx.protected {
		tx.t.Fatal("collection invalidation lost its protected transaction context")
	}
	tx.queries++
	tx.recursive = strings.Contains(statement, "WITH RECURSIVE")
	tx.ids = slices.Clone(args[0].([]string))
	if len(args) > 1 {
		tx.libraryIDs = slices.Clone(args[1].([]string))
	}
	tx.reservedAtRead = tx.budget.bytes
	if tx.failQuery {
		return tx.Tx.QueryRow(ctx, "SELECT 1/0")
	}
	return tx.Tx.QueryRow(ctx, statement, args...)
}

func TestScanCollectionAncestorReusePreservesPrivateScopesAndFailures(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	owner := Subject{UserID: ownerID}
	seed := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Direct seed", ItemIDs: []string{"collection-track-a"}})
	ancestor := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Ancestor", ItemIDs: []string{"collection-album"}})
	collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Same library sibling", ItemIDs: []string{"collection-track-b"}})
	collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Auxiliary owner", ItemIDs: []string{"collection-track-c"}})
	if _, err := pool.Exec(ctx, `INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		VALUES ('collection-track-a','collection-track-c','clip',true)`); err != nil {
		t.Fatal(err)
	}
	known := map[string]scanReconciliationItem{
		"collection-track-a": {id: "collection-track-a", libraryID: "collection-source-a", parentID: "collection-album",
			extraOwner: "collection-track-c", visible: true},
		"collection-album":   {id: "collection-album", libraryID: "collection-source-a", ordinary: true, isFolder: true},
		"collection-track-b": {id: "collection-track-b", libraryID: "collection-source-a", parentID: "collection-album", ordinary: true},
		"collection-track-c": {id: "collection-track-c", libraryID: "collection-source-b", ordinary: true},
	}
	facts := []CatalogChange{known["collection-track-a"].fact()}
	wantIDs := []string{seed.ID, ancestor.ID}
	slices.Sort(wantIDs)
	for _, mode := range []string{"recursive", "memoized", "budget fallback", "query failure"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := store.beginOwnedTx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			owned := tx.(*ownedTx)
			budget := &scanReconciliationBudgetState{bytes: 12345}
			if mode == "budget fallback" {
				budget.bytes = scanReconciliationMaxBytes
			}
			before := budget.bytes
			observed := &scanCollectionAncestorsObservedTx{Tx: owned.Tx, t: t, protected: owned.ctx,
				budget: budget, failQuery: mode == "query failure"}
			owned.Tx = observed
			if mode == "recursive" {
				err = recordCollectionSourceRemovals(tx, facts)
			} else {
				err = recordScanCollectionSourceRemovals(tx, facts, known, budget)
			}
			if budget.bytes != before || observed.queries != 1 {
				t.Fatalf("collection ancestor reuse changed query count or retained scratch: queries=%d, before=%d, after=%d", observed.queries, before, budget.bytes)
			}
			if mode == "query failure" {
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "22012" || owned.catalogChanges.resync || len(owned.notificationReferences) != 0 {
					t.Fatalf("failed ancestor membership query changed rollback or publication facts: %v", err)
				}
				if err := tx.Rollback(ctx); err != nil || !store.Available() {
					t.Fatalf("failed membership query could not roll back while retaining ownership: %v", err)
				}
				return
			}
			if err != nil || !owned.catalogChanges.resync || len(owned.catalogChanges.changes) != 0 {
				t.Fatalf("collection invalidation lost its generic resync: %v, %+v", err, owned.catalogChanges)
			}
			var gotIDs []string
			for _, ref := range owned.notificationReferences {
				if ref.Kind != "Item" || ref.LibraryID != collectionLibraryID || ref.SourceID != ref.ID {
					t.Fatalf("private collection reference gained a public source scope: %+v", ref)
				}
				gotIDs = append(gotIDs, ref.ID)
			}
			slices.Sort(gotIDs)
			if !reflect.DeepEqual(gotIDs, wantIDs) {
				t.Fatalf("ancestor reuse expanded collection membership: got=%v, want=%v", gotIDs, wantIDs)
			}
			if mode == "memoized" {
				slices.Sort(observed.ids)
				if observed.recursive || !reflect.DeepEqual(observed.ids, []string{"collection-album", "collection-track-a"}) ||
					len(observed.libraryIDs) != 0 || observed.reservedAtRead <= before {
					t.Fatalf("scan membership query did not retain its exact budgeted closure: %+v", observed)
				}
			} else if !observed.recursive || !reflect.DeepEqual(observed.ids, []string{"collection-track-a"}) || len(observed.libraryIDs) != 0 {
				t.Fatalf("ordinary ancestor fallback changed the original query scope: %+v", observed)
			}
		})
	}
}

func TestScanCollectionAncestorReuseKeepsOriginalFactLimit(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	members, known := scanReconciliationAncestorTestFixture(100, 1000)
	var ids, parents, kinds []string
	var folders []bool
	for id, item := range known {
		item.libraryID = "collection-source-a"
		known[id] = item
		if _, removed := members[id]; removed {
			members[id] = item
		}
		ids, parents, kinds = append(ids, id), append(parents, item.parentID), append(kinds, item.typeName)
		folders = append(folders, item.isFolder)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		SELECT id,'collection-source-a',NULLIF(parent,''),id,id,kind,folder
		FROM unnest($1::text[],$2::text[],$3::text[],$4::boolean[]) seed(id,parent,kind,folder)`, ids, parents, kinds, folders); err != nil {
		t.Fatal(err)
	}
	var facts []CatalogChange
	for _, id := range scanReconciliationIDs(members) {
		facts = append(facts, members[id].fact())
	}
	for _, test := range []struct {
		name     string
		facts    []CatalogChange
		resync   bool
		queries  int
		wantSync bool
	}{
		{name: "large closure", facts: facts, queries: 1},
		{name: "empty"},
		{name: "no removals", facts: []CatalogChange{{Kind: CatalogUpdated, ItemID: "leaf-0000", LibraryID: "collection-source-a"}}},
		{name: "existing resync", facts: facts, resync: true, wantSync: true},
		{name: "exact fact limit", facts: slices.Repeat(facts[:1], maxCatalogChanges), queries: 1},
		{name: "fact overflow", facts: slices.Repeat(facts[:1], maxCatalogChanges+1), wantSync: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := store.beginOwnedTx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			owned := tx.(*ownedTx)
			owned.catalogChanges.resync = test.resync
			budget := &scanReconciliationBudgetState{bytes: 321}
			observed := &scanCollectionAncestorsObservedTx{Tx: owned.Tx, t: t, protected: owned.ctx, budget: budget}
			owned.Tx = observed
			if err := recordScanCollectionSourceRemovals(tx, test.facts, known, budget); err != nil {
				t.Fatal(err)
			}
			if observed.queries != test.queries || owned.catalogChanges.resync != test.wantSync || len(owned.notificationReferences) != 0 || budget.bytes != 321 {
				t.Fatalf("scan closure changed fact/empty/resync boundaries: queries=%d, batch=%+v, refs=%d, budget=%+v", observed.queries, owned.catalogChanges, len(owned.notificationReferences), budget)
			}
			if test.name == "large closure" && (observed.recursive || len(observed.ids) != len(known) || len(observed.ids) <= maxCatalogChanges) {
				t.Fatalf("the fact limit was incorrectly applied to the ancestor argument: closure=%d, recursive=%v", len(observed.ids), observed.recursive)
			}
		})
	}
}

func TestScanCollectionAncestorReuseRetainsCollectionOverflowSentinel(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	collectionUserDataCreate(t, ctx, store, Subject{UserID: ownerID}, BoxSetKind, CollectionInput{Name: "Initialize collection library"})
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		SELECT 'scan-collection-'||n,$1,'Collection','collection','BoxSet',true FROM generate_series(1,4098) n`, collectionLibraryID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_collections(item_id,owner_id,kind)
		SELECT 'scan-collection-'||n,$1,'BoxSet' FROM generate_series(1,4098) n`, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_collection_entries(collection_id,item_id,position)
		SELECT 'scan-collection-'||n,'collection-track-a',0 FROM generate_series(1,4098) n`); err != nil {
		t.Fatal(err)
	}
	tx, err := store.beginOwnedTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	owned := tx.(*ownedTx)
	known := map[string]scanReconciliationItem{
		"collection-track-a": {id: "collection-track-a", libraryID: "collection-source-a", parentID: "collection-album", ordinary: true, visible: true},
		"collection-album":   {id: "collection-album", libraryID: "collection-source-a", ordinary: true, isFolder: true},
	}
	budget := &scanReconciliationBudgetState{}
	if err := recordScanCollectionSourceRemovals(tx, []CatalogChange{known["collection-track-a"].fact()}, known, budget); err != nil {
		t.Fatal(err)
	}
	if len(owned.notificationReferences) != notificationReferenceOverflow || !owned.catalogChanges.resync || budget.bytes != 0 {
		t.Fatalf("collection overflow lost its sentinel or retained closure scratch: refs=%d, batch=%+v, budget=%+v", len(owned.notificationReferences), owned.catalogChanges, budget)
	}
	for _, ref := range owned.notificationReferences {
		if ref.LibraryID != collectionLibraryID || ref.SourceID != ref.ID {
			t.Fatalf("collection overflow exposed a library-wide private identity: %+v", ref)
		}
	}
}
