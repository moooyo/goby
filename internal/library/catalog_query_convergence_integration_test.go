package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func convergenceDescendantCounts(t *testing.T, ctx context.Context, tx pgx.Tx, statement string, ids []string, userID string) map[string][2]int {
	t.Helper()
	rows, err := tx.Query(ctx, statement, ids, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := make(map[string][2]int)
	for rows.Next() {
		var id string
		var counts [2]int
		if err := rows.Scan(&id, &counts[0], &counts[1]); err != nil {
			t.Fatal(err)
		}
		result[id] = counts
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertConvergenceDescendantCounts(t *testing.T, ctx context.Context, store *Store, userID string, ids []string, collections bool, want map[string][2]int) {
	t.Helper()
	tx, access, err := store.beginSubjectRead(ctx, Subject{UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	root := `SELECT i.id,i.library_id FROM items i WHERE i.id=ANY($1::text[]) AND i.is_folder AND ` + access.ordinarySQL("i")
	statement := folderUserDataCountsSQL(root, 2, access)
	if collections {
		statement = collectionUserDataCountsSQL(root, 2, access)
	}
	// The old aggregate is an independent comparison inside the same snapshot;
	// explicit expected counts also guard against a shared membership defect.
	baseline := strings.ReplaceAll(statement, "count(leaf.id)", "count(DISTINCT leaf.id)")
	if baseline == statement {
		t.Fatal("descendant comparison lost its original DISTINCT aggregate")
	}
	before := convergenceDescendantCounts(t, ctx, tx, baseline, ids, userID)
	after := convergenceDescendantCounts(t, ctx, tx, statement, ids, userID)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(after, want) {
		t.Fatalf("descendant aggregation changed: before=%v after=%v want=%v", before, after, want)
	}
}

func TestDescendantCountsPreservePhysicalCyclesAndEmptyRoots(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryUserDataQueryFixture(t, ctx, store.pool)
	ids := []string{"series-b", "season-b", "album-b", "artist-b", "series-b"}
	want := map[string][2]int{"series-b": {2, 1}, "season-b": {2, 1}, "album-b": {2, 2}, "artist-b": {0, 0}}
	assertConvergenceDescendantCounts(t, ctx, store, "restricted", ids, false, want)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_id='episode-b1' WHERE id='series-b'`); err != nil {
		t.Fatal(err)
	}
	assertConvergenceDescendantCounts(t, ctx, store, "restricted", ids, false, want)
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=policy||'{"ExcludedSubFolders":["episode-b2"]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	want["series-b"], want["season-b"] = [2]int{1, 1}, [2]int{1, 1}
	assertConvergenceDescendantCounts(t, ctx, store, "restricted", ids, false, want)
}

func TestDescendantCountsPreserveCollectionCyclesAndDuplicateMembership(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "aggregate-reader", false, false, []string{"collection-source-a"})
	owner := Subject{UserID: ownerID}
	first := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "First aggregate root", ItemIDs: []string{"collection-track-a", "collection-track-c"}})
	second := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind,
		CollectionInput{Name: "Second aggregate root", ItemIDs: []string{"collection-track-b"}})
	empty := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind, CollectionInput{Name: "Empty aggregate root"})
	for _, collection := range []CollectionInfo{first, second, empty} {
		collectionUserDataShare(t, ctx, store, owner, collection, "aggregate-reader")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_collection_entries(collection_id,item_id,position)
		VALUES($1,$2,10),($2,$1,10),($1,'collection-track-a',11)`, first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	ids := []string{first.ID, second.ID, empty.ID, first.ID}
	assertConvergenceDescendantCounts(t, ctx, store, ownerID, ids, true,
		map[string][2]int{first.ID: {3, 3}, second.ID: {3, 3}, empty.ID: {0, 0}})
	assertConvergenceDescendantCounts(t, ctx, store, "aggregate-reader", ids, true,
		map[string][2]int{first.ID: {2, 2}, second.ID: {2, 2}, empty.ID: {0, 0}})
	if _, err := store.SetPlayedFor(ctx, Subject{UserID: "aggregate-reader"}, "collection-track-a", true, nil); err != nil {
		t.Fatal(err)
	}
	assertConvergenceDescendantCounts(t, ctx, store, "aggregate-reader", ids, true,
		map[string][2]int{first.ID: {2, 1}, second.ID: {2, 1}, empty.ID: {0, 0}})
}

func convergenceExpectedRows(t *testing.T, ctx context.Context, tx pgx.Tx, access libraryAccess, relation, filter string, argument any) map[string]string {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT i.id,to_jsonb(i) FROM (`+relation+`) i WHERE `+filter+` AND `+access.itemPolicySQL("i")+` ORDER BY i.id`, argument)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		result[id] = string(raw)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertConvergenceExpectedLookup(t *testing.T, f episodeRosterFixture, userID, id string, visible bool) {
	t.Helper()
	missing := ExpectedEpisodeID("absent-series", "absent-source", "absent-entry")
	ids := []string{id, missing, id}
	tx, access, err := f.store.beginSubjectRead(f.ctx, Subject{UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	for _, test := range []struct {
		outer, inner string
		argument     any
	}{
		{"i.id=$1", "e.id=$1::text", id},
		{"i.id=ANY($1::text[])", "e.id=ANY($1::text[])", ids},
	} {
		before := convergenceExpectedRows(t, f.ctx, tx, access, expectedEpisodeItemsSQL(access), test.outer, test.argument)
		after := convergenceExpectedRows(t, f.ctx, tx, access, expectedEpisodeItemsFilteredSQL(access, test.inner), test.outer, test.argument)
		_, found := after[id]
		if !reflect.DeepEqual(before, after) || found != visible || len(after) != map[bool]int{false: 0, true: 1}[visible] {
			t.Fatalf("expected ID pushdown changed virtual rows: visible=%t before=%v after=%v", visible, before, after)
		}
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	item, err := f.store.GetItem(f.ctx, userID, id)
	if visible {
		if err != nil || item.ID != id || item.ExpectedEpisode == nil || item.CanPlay {
			t.Fatalf("direct expected item changed: %+v, %v", item, err)
		}
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("invisible expected item escaped direct lookup: %v", err)
	}
	permissions, err := f.store.ItemPermissionsFor(f.ctx, Subject{UserID: userID}, ids)
	permission, found := permissions[id]
	if err != nil || found != visible || found && permission.CanPlay {
		t.Fatalf("expected permission changed: %v, %v", permissions, err)
	}
}

func TestExpectedEpisodeDirectIDPushdownPreservesVisibility(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	detail := f.replace(t, f.edit)
	id := detail.Entries[0].ID
	assertConvergenceExpectedLookup(t, f, "restricted", id, true)
	assertConvergenceExpectedLookup(t, f, "none", id, false)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,index_number,parent_index_number,media)
		VALUES('hidden-physical','library-b','root-b','season-b','Hidden physical','Hidden physical','Episode',
		'/media/b/hidden-physical.mkv','hidden-physical.mkv',2,1,'{"DurationTicks":15000000}'::jsonb);
		UPDATE users SET policy=policy||'{"ExcludedSubFolders":["hidden-physical"]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetItem(f.ctx, "restricted", "hidden-physical"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("physical suppression fixture is not hidden: %v", err)
	}
	assertConvergenceExpectedLookup(t, f, "restricted", id, false)
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM items WHERE id='hidden-physical'`); err != nil {
		t.Fatal(err)
	}
	assertConvergenceExpectedLookup(t, f, "restricted", id, true)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,index_number)
		VALUES('hidden-season','library-b','series-b','Hidden season','Hidden season','Season',true,1);
		UPDATE users SET policy=policy||'{"ExcludedSubFolders":["hidden-season"]}'::jsonb WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	assertConvergenceExpectedLookup(t, f, "restricted", id, false)
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM items WHERE id='hidden-season'`); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := f.store.WithdrawEpisodeRoster(f.ctx, f.actor, "series-b", detail.Revision)
	if err != nil {
		t.Fatal(err)
	}
	assertConvergenceExpectedLookup(t, f, "restricted", id, false)
	edit := f.edit
	edit.Revision, edit.Source.Key = withdrawn.Revision, "replacement-source"
	replacement := f.replace(t, edit)
	assertConvergenceExpectedLookup(t, f, "restricted", id, false)
	assertConvergenceExpectedLookup(t, f, "restricted", replacement.Entries[0].ID, true)
}

func TestItemPermissionAuthorityMatchesTheObservedCatalogPolicy(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	principal := identity.Principal{Kind: "emby", User: identity.User{ID: "restricted"}}
	if err := store.pool.QueryRow(ctx, `SELECT is_administrator,is_disabled,policy FROM users WHERE id=$1`, principal.User.ID).
		Scan(&principal.User.IsAdministrator, &principal.User.IsDisabled, &principal.User.Policy); err != nil {
		t.Fatal(err)
	}
	items, authority, err := store.ItemPermissionsWithAuthorityFor(ctx, Subject{UserID: principal.User.ID}, []string{"movie-b"})
	if err != nil || len(items) != 1 || !authority.MatchesPrincipal(principal) {
		t.Fatalf("permission snapshot lost its actual authority: items=%v error=%v", items, err)
	}
	changed := principal
	changed.User.Policy = json.RawMessage(`{"EnableMediaPlayback":false}`)
	if authority.MatchesPrincipal(changed) {
		t.Fatal("a changed catalog policy matched earlier item authorization")
	}
	changed = principal
	changed.User.IsAdministrator = !changed.User.IsAdministrator
	if authority.MatchesPrincipal(changed) {
		t.Fatal("a changed account role matched earlier item authorization")
	}
	key := identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: "credential", ClientSessionID: "client", ApplicationKeyID: 1}
	keyAuthority := ItemPermissionAuthority{valid: true, credentialID: key.SessionID}
	if !keyAuthority.MatchesPrincipal(key) {
		t.Fatal("userless key authority did not match its freshly checked credential")
	}
	keyAuthority.userID = "selected-user"
	if keyAuthority.MatchesPrincipal(key) || (ItemPermissionAuthority{}).MatchesPrincipal(principal) {
		t.Fatal("unproven target or empty authority matched a principal")
	}
}
