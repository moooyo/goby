package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func TestStoreForceProbeRepairsMissingEffectiveGenreOnlyAfterSuccessfulProbe(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		if fail.Load() {
			return media.Info{}, errors.New("entity repair probe fixture failure")
		}
		return forceProbeTestInfo(file)
	})
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "movies/Repair.mp4", "video:stable-entity-repair")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if media.FileChangeTime(info) <= 0 {
		t.Skip("same-version probe caching requires filesystem change time support")
	}
	nfoPath := libraryIntegrationFile(t, root, "movies/Repair.nfo", `<movie><title>Automatic title</title><plot>Locked source overview.</plot><genre>Source genre</genre><tag>Stable tag</tag><studio>Stable studio</studio></movie>`)
	collection := libraryIntegrationCreate(t, ctx, store, "Entity repair movies", "movies", filepath.Dir(path))
	cold := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if cold.ForceProbe || cold.Error != "" || cold.Scanned != 1 || cold.Added != 1 || calls.Load() != 1 {
		t.Fatalf("ordinary cold scan did not accept one probe and its NFO: job=%+v calls=%d", cold, calls.Load())
	}
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	actor := metadataEditTestActor(t, ctx, pool, "entity-repair-editor")
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if !reflect.DeepEqual(detail.Automatic.Genres, []string{"Source genre"}) {
		t.Fatal("cold scan did not accept the NFO genre")
	}
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{
		"Name": json.RawMessage(`"Manual title"`), "Genres": json.RawMessage(`["Manual genre","Retained manual genre"]`),
	}, []string{"Name", "Genres", "Overview"})
	// Compare persisted JSONB representations across scans, including the initial controls.
	detail = metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if detail.Effective.Name != "Manual title" || !reflect.DeepEqual(detail.Effective.Genres, []string{"Manual genre", "Retained manual genre"}) ||
		len(detail.LockedFields) != 3 || len(detail.LockedValues["Name"]) == 0 || len(detail.LockedValues["Genres"]) == 0 ||
		len(detail.LockedValues["Overview"]) == 0 {
		t.Fatal("the fixture did not persist its manual overrides and locked values")
	}
	otherUser := "entity-repair-second-user"
	libraryIntegrationUser(t, ctx, pool, otherUser, false, true, nil)
	playedAt := time.Date(2026, 3, 4, 5, 6, 7, 8000, time.UTC)
	for index, user := range []string{userID, otherUser} {
		userDataSeed(t, ctx, pool, user, UserData{ItemID: item.ID, IsFavorite: index == 0, Played: index == 1,
			PlaybackPositionTicks: int64(12345 * (index + 1)), PlayCount: 3 + index, LastPlayedDate: &playedAt})
	}
	beforeUserData := forceProbeEntityRepairUserData(t, ctx, pool, item.ID)
	assertControls := func() {
		t.Helper()
		current := metadataEditTestDetail(t, ctx, store, actor, item.ID)
		if !reflect.DeepEqual(current.Overrides, detail.Overrides) || !reflect.DeepEqual(current.LockedValues, detail.LockedValues) ||
			!reflect.DeepEqual(current.LockedFields, detail.LockedFields) || !reflect.DeepEqual(current.Effective, detail.Effective) ||
			current.LastEditedBy != detail.LastEditedBy || !reflect.DeepEqual(current.LastEditedAt, detail.LastEditedAt) {
			t.Fatal("entity repair changed administrator metadata, locks, or editor history")
		}
		if forceProbeEntityRepairUserData(t, ctx, pool, item.ID) != beforeUserData {
			t.Fatal("entity repair changed either user's complete history or row version")
		}
		retained := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true,
			IncludeItemTypes: []string{"Movie"}, Genres: []string{"Retained manual genre"}})
		if retained.TotalRecordCount != 1 || len(retained.Items) != 1 || retained.Items[0].ID != item.ID {
			t.Fatal("entity repair discarded a separate accepted Genre association")
		}
	}
	assertGenre := func(want int) {
		t.Helper()
		result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true,
			IncludeItemTypes: []string{"Movie"}, Genres: []string{"Manual genre"}})
		if result.TotalRecordCount != want || len(result.Items) != want || (want == 1 && result.Items[0].ID != item.ID) {
			t.Fatalf("effective Genre query has count=%d, want=%d", result.TotalRecordCount, want)
		}
	}
	assertGenre(1)
	if result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true, Genres: []string{"Source genre"}}); result.TotalRecordCount != 0 {
		t.Fatal("the automatic genre escaped the manual locked override")
	}
	associations := forceProbeEntityRepairAssociations(t, ctx, pool, item.ID)
	removeGenre := func() {
		t.Helper()
		// Corrupt only this fixture's derived association, retaining the entity,
		// accepted automatic source, manual layer, locks, and unrelated credits.
		deleted, err := pool.Exec(ctx, `DELETE FROM item_entities association USING catalog_entities entity
			WHERE association.item_id=$1 AND association.entity_id=entity.id
			AND entity.kind='Genre' AND entity.name='Manual genre'`, item.ID)
		if err != nil || deleted.RowsAffected() != 1 {
			t.Fatalf("remove exactly one fixture Genre association: rows=%d error=%v", deleted.RowsAffected(), err)
		}
		assertGenre(0)
	}
	removeGenre()
	missingBefore := forceProbeEntityRepairSnapshot(t, ctx, pool, item.ID)
	cached := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if cached.ForceProbe || cached.Error != "" || cached.Scanned != 1 || cached.Added != 0 || cached.Updated != 0 || calls.Load() != 1 {
		t.Fatalf("ordinary scan did not reuse the unchanged probe cache: job=%+v calls=%d", cached, calls.Load())
	}
	if forceProbeEntityRepairSnapshot(t, ctx, pool, item.ID) != missingBefore {
		t.Fatal("ordinary cached scanning repaired or changed the deliberately missing index")
	}
	assertGenre(0)
	assertControls()

	forced := forceProbeTestScan(t, ctx, store, collection.ID, "Completed")
	if forced.Error != "" || forced.Scanned != 1 || forced.Added != 0 || forced.Updated != 1 || calls.Load() != 2 {
		t.Fatalf("successful force probe did not refresh the existing item: job=%+v calls=%d", forced, calls.Load())
	}
	// Rebuilt association values must match exactly. Their xmin may change
	// because a successful repair intentionally recreates derived rows.
	if forceProbeEntityRepairAssociations(t, ctx, pool, item.ID) != associations {
		t.Fatal("successful force probe did not restore the exact effective association set")
	}
	assertGenre(1)
	assertControls()
	refreshed := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	if refreshed.ID != item.ID || !reflect.DeepEqual(refreshed.Media, item.Media) {
		t.Fatal("repairing a derived Genre changed stable media facts or identity")
	}

	removeGenre()
	failedBefore := forceProbeEntityRepairSnapshot(t, ctx, pool, item.ID)
	if err := os.WriteFile(nfoPath, []byte(`<movie><title>Unaccepted title</title><genre>Unaccepted genre</genre><tag>Unaccepted tag</tag></movie>`), 0o600); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	failed := forceProbeTestScan(t, ctx, store, collection.ID, "Completed")
	if failed.Error == "" || failed.Scanned != 1 || failed.Added != 0 || failed.Updated != 0 || calls.Load() != 3 {
		t.Fatalf("failed force probe changed scan warning semantics: job=%+v calls=%d", failed, calls.Load())
	}
	if forceProbeEntityRepairSnapshot(t, ctx, pool, item.ID) != failedBefore {
		t.Fatal("a failed probe rebuilt or erased catalog information without an accepted result")
	}
	assertGenre(0)
	assertControls()
}

func forceProbeEntityRepairAssociations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(association)
		ORDER BY association.entity_id,association.position),'[]'::jsonb)::text
		FROM item_entities association WHERE association.item_id=$1`, itemID).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func forceProbeEntityRepairUserData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var count int
	var value string
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(jsonb_agg(to_jsonb(d) ||
		jsonb_build_object('row_version',d.xmin::text) ORDER BY d.user_id),'[]'::jsonb)::text
		FROM user_item_data d WHERE d.item_id=$1`, itemID).Scan(&count, &value); err != nil || count != 2 {
		t.Fatalf("read both preserved UserData rows: count=%d error=%v", count, err)
	}
	return value
}

func forceProbeEntityRepairSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'item',(SELECT to_jsonb(i)||jsonb_build_object('row_version',i.xmin::text) FROM items i WHERE i.id=$1),
		'metadata',(SELECT to_jsonb(s)||jsonb_build_object('row_version',s.xmin::text) FROM item_metadata_state s WHERE s.item_id=$1),
		'associations',(SELECT COALESCE(jsonb_agg(to_jsonb(a)||jsonb_build_object('row_version',a.xmin::text)
			ORDER BY a.entity_id,a.position),'[]'::jsonb) FROM item_entities a WHERE a.item_id=$1))::text`, itemID).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
