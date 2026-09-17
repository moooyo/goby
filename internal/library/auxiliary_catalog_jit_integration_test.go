package library

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAuxiliaryCatalogSnapshotJITRestoresTransactionAndSession(t *testing.T) {
	ctx, store, libraryID := auxiliaryCatalogJITTestFixture(t)
	var expected auxiliaryCatalogSnapshot
	for _, incoming := range []string{"off", "on"} {
		for _, finish := range []string{"commit", "rollback"} {
			t.Run(incoming+"/"+finish, func(t *testing.T) {
				conn := auxiliaryCatalogJITTestConnection(t, ctx, store)
				sessionSetting := "on"
				if incoming == "on" {
					sessionSetting = "off"
				}
				if _, err := conn.Exec(ctx, `SELECT set_config('jit', $1, false)`, sessionSetting); err != nil {
					t.Fatalf("set the session JIT baseline: %v", err)
				}
				tx, err := conn.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { rollback(tx) })
				if _, err := tx.Exec(ctx, `SELECT set_config('jit', $1, true)`, incoming); err != nil {
					t.Fatalf("set the caller's transaction JIT value: %v", err)
				}
				observed := &auxiliaryCatalogJITObservedTx{Tx: tx}
				var preparedName string
				for call := 1; call <= 2; call++ {
					snapshot, err := readAuxiliaryCatalogRows(ctx, observed,
						[]string{"jit-owner", "jit-ordinary", "jit-owner"}, []string{"jit-history"})
					if err != nil {
						t.Fatalf("read snapshot with incoming JIT %s on call %d: %v", incoming, call, err)
					}
					assertAuxiliaryCatalogJITSnapshot(t, snapshot, libraryID)
					if expected.items == nil {
						expected = snapshot
					} else if !reflect.DeepEqual(snapshot, expected) {
						t.Fatalf("incoming JIT %s or cached execution changed the complete snapshot: got %+v, want %+v", incoming, snapshot, expected)
					}
					assertAuxiliaryCatalogJITSetting(t, ctx, tx, incoming)
					name, generic, custom := auxiliaryCatalogJITPreparedPlan(t, ctx, tx, observed.statement)
					if call == 1 {
						preparedName = name
					}
					if name != preparedName || generic+custom != int64(call) {
						t.Fatalf("snapshot did not reuse its default cached statement: name = %q, generic = %d, custom = %d, call = %d", name, generic, custom, call)
					}
				}
				if !slices.Equal(observed.settings, []string{"off", "off"}) {
					t.Fatalf("snapshot query did not execute with JIT disabled: %v", observed.settings)
				}
				if finish == "commit" {
					err = tx.Commit(ctx)
				} else {
					err = tx.Rollback(ctx)
				}
				if err != nil {
					t.Fatalf("finish the snapshot transaction: %v", err)
				}
				assertAuxiliaryCatalogJITSetting(t, ctx, conn, sessionSetting)
			})
		}
	}
}

func TestAuxiliaryCatalogSnapshotJITPreservesExistingGenericPlanResults(t *testing.T) {
	ctx, store, libraryID := auxiliaryCatalogJITTestFixture(t)
	// Capture the production statement without copying its SQL or selecting a
	// different pgx query mode. The fresh connection below has no cached plan.
	captureTx := beginCatalogTestTransaction(t, ctx, store)
	capture := &auxiliaryCatalogJITObservedTx{Tx: captureTx}
	expected, err := readAuxiliaryCatalogRows(captureTx.(*ownedTx).ctx, capture,
		[]string{"jit-owner", "jit-ordinary"}, []string{"jit-history"})
	if err != nil {
		t.Fatal(err)
	}
	if err := captureTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	conn := auxiliaryCatalogJITTestConnection(t, ctx, store)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rollback(tx) })
	if _, err := tx.Exec(ctx, `SET LOCAL jit = on; SET LOCAL plan_cache_mode = force_generic_plan`); err != nil {
		t.Fatalf("prepare a generic plan with the caller's JIT enabled: %v", err)
	}
	rows, err := tx.Query(ctx, capture.statement, capture.args...)
	if err != nil {
		t.Fatalf("execute the original statement with JIT enabled: %v", err)
	}
	defer rows.Close()
	original := make(map[string]auxiliaryCatalogItem)
	for rows.Next() {
		var item auxiliaryCatalogItem
		if err := rows.Scan(&item.change.ItemID, &item.change.LibraryID, &item.change.ParentID,
			&item.change.IsFolder, &item.change.IsCollectionFolder, &item.ordinary, &item.visible,
			&item.themeOwner, &item.extraOwner, &item.properties, &item.subject); err != nil {
			t.Fatal(err)
		}
		original[item.change.ItemID] = item
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	name, generic, custom := auxiliaryCatalogJITPreparedPlan(t, ctx, tx, capture.statement)
	if generic != 1 || custom != 0 {
		t.Fatalf("the original statement did not prepare a generic plan: generic = %d, custom = %d", generic, custom)
	}
	observed := &auxiliaryCatalogJITObservedTx{Tx: tx}
	snapshot, err := readAuxiliaryCatalogRows(ctx, observed,
		[]string{"jit-owner", "jit-ordinary"}, []string{"jit-history"})
	if err != nil {
		t.Fatalf("read the existing generic plan with JIT disabled: %v", err)
	}
	assertAuxiliaryCatalogJITSnapshot(t, snapshot, libraryID)
	if !reflect.DeepEqual(snapshot.items, original) || !reflect.DeepEqual(snapshot, expected) {
		t.Fatalf("JIT suppression changed original generic-plan rows or snapshot bookkeeping: got %+v, original rows %+v, expected %+v", snapshot, original, expected)
	}
	if !slices.Equal(observed.settings, []string{"off"}) {
		t.Fatalf("the existing generic plan was not executed with JIT disabled: %v", observed.settings)
	}
	assertAuxiliaryCatalogJITSetting(t, ctx, tx, "on")
	reusedName, generic, custom := auxiliaryCatalogJITPreparedPlan(t, ctx, tx, observed.statement)
	if reusedName != name || generic != 2 || custom != 0 {
		t.Fatalf("the snapshot replaced or bypassed the existing generic plan: name = %q, generic = %d, custom = %d", reusedName, generic, custom)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAuxiliaryCatalogSnapshotJITCallerCancellationPreservesOwnership(t *testing.T) {
	ctx, store, libraryID := auxiliaryCatalogJITTestFixture(t)
	ownerPID := int32(store.ownership.conn.Conn().PgConn().PID())
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			callerCtx, cancelCaller := context.WithCancel(ctx)
			defer cancelCaller()
			tx := beginCatalogTestTransaction(t, callerCtx, store)
			var sessionSetting string
			if err := tx.QueryRow(ctx, `SELECT current_setting('jit')`).Scan(&sessionSetting); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `SET LOCAL jit = on`); err != nil {
				t.Fatal(err)
			}
			expected, err := readAuxiliaryCatalogRows(callerCtx, tx,
				[]string{"jit-owner", "jit-ordinary"}, []string{"jit-history"})
			if err != nil {
				t.Fatal(err)
			}
			cancelCaller()
			// Pass the actual ownedTx and cancelled caller context so the reader
			// itself must protect Query as well as the JIT setting operations.
			snapshot, err := readAuxiliaryCatalogRows(callerCtx, tx,
				[]string{"jit-owner", "jit-ordinary"}, []string{"jit-history"})
			if err != nil {
				t.Fatalf("caller cancellation escaped the owned snapshot context: %v", err)
			}
			assertAuxiliaryCatalogJITSnapshot(t, snapshot, libraryID)
			if !reflect.DeepEqual(snapshot, expected) {
				t.Fatalf("caller cancellation truncated the complete snapshot: got %+v, want %+v", snapshot, expected)
			}
			assertAuxiliaryCatalogJITSetting(t, callerCtx, tx, "on")
			if finish == "commit" {
				err = tx.Commit(callerCtx)
			} else {
				err = tx.Rollback(callerCtx)
			}
			if err != nil {
				t.Fatalf("finish the protected snapshot after caller cancellation: %v", err)
			}
			ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
			next := beginCatalogTestTransaction(t, ctx, store)
			assertAuxiliaryCatalogJITSetting(t, ctx, next, sessionSetting)
			if err := next.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func auxiliaryCatalogJITTestFixture(t *testing.T) (context.Context, *Store, string) {
	t.Helper()
	ctx, _, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := libraryIntegrationCreate(t, ctx, store, "Auxiliary JIT snapshots", "movies", root)
	themeTestItem(t, ctx, store, "jit-owner", "Movie", library.ID, library.ID, "Film/Main.mp4")
	themeTestItem(t, ctx, store, "jit-ordinary", "Movie", library.ID, library.ID, "Ordinary/Main.mp4")
	themeTestItem(t, ctx, store, "jit-history", "Folder", library.ID, library.ID, "Historical")
	themeTestResource(t, ctx, store, "jit-theme", "song", "jit-owner", library.ID, "Film/theme.mp3")
	extraTestResource(t, ctx, store, "jit-trailer", "jit-owner", library.ID, "Film/trailers/Clip.mp4", ExtraKindTrailer)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata = '{"Genres":["Drama"]}' WHERE id = 'jit-owner'`); err != nil {
		t.Fatalf("seed inherited owner metadata: %v", err)
	}
	return ctx, store, library.ID
}

func auxiliaryCatalogJITTestConnection(t *testing.T, ctx context.Context, store *Store) *pgx.Conn {
	t.Helper()
	config := store.pool.Config().ConnConfig.Copy()
	if config.DefaultQueryExecMode != pgx.QueryExecModeCacheStatement || config.StatementCacheCapacity <= 0 {
		t.Fatal("the integration fixture must preserve the default pgx statement cache")
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("open the snapshot test session: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.Close(closeCtx); err != nil {
			t.Errorf("close the snapshot test session: %v", err)
		}
	})
	return conn
}

type auxiliaryCatalogJITObservedTx struct {
	pgx.Tx
	settings  []string
	statement string
	args      []any
}

func (tx *auxiliaryCatalogJITObservedTx) Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error) {
	var setting string
	if err := tx.Tx.QueryRow(ctx, `SELECT current_setting('jit')`).Scan(&setting); err != nil {
		return nil, fmt.Errorf("observe JIT at the snapshot query boundary: %w", err)
	}
	tx.settings = append(tx.settings, setting)
	tx.statement, tx.args = statement, append([]any(nil), args...)
	// The reader reuses its seed backing array while collecting related IDs.
	// Keep the captured parameters independent for the original-query oracle.
	for index, arg := range tx.args {
		if ids, ok := arg.([]string); ok {
			tx.args[index] = slices.Clone(ids)
		}
	}
	return tx.Tx.Query(ctx, statement, args...)
}

func assertAuxiliaryCatalogJITSetting(t *testing.T, ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, want string) {
	t.Helper()
	var setting string
	if err := query.QueryRow(ctx, `SELECT current_setting('jit')`).Scan(&setting); err != nil || setting != want {
		t.Fatalf("JIT setting was not restored: got %q, want %q, error = %v", setting, want, err)
	}
}

func auxiliaryCatalogJITPreparedPlan(t *testing.T, ctx context.Context, tx pgx.Tx, statement string) (string, int64, int64) {
	t.Helper()
	var name string
	var generic, custom int64
	if err := tx.QueryRow(ctx, `SELECT name, generic_plans, custom_plans FROM pg_prepared_statements WHERE statement = $1`,
		statement).Scan(&name, &generic, &custom); err != nil {
		t.Fatalf("read the cached snapshot statement: %v", err)
	}
	return name, generic, custom
}

func assertAuxiliaryCatalogJITSnapshot(t *testing.T, snapshot auxiliaryCatalogSnapshot, libraryID string) {
	t.Helper()
	expected := map[string]auxiliaryCatalogItem{
		libraryID: {change: CatalogChange{ItemID: libraryID, LibraryID: libraryID, IsFolder: true, IsCollectionFolder: true},
			ordinary: true, visible: true},
		"jit-owner": {change: CatalogChange{ItemID: "jit-owner", LibraryID: libraryID, ParentID: libraryID},
			ordinary: true, visible: true, subject: true},
		"jit-ordinary": {change: CatalogChange{ItemID: "jit-ordinary", LibraryID: libraryID, ParentID: libraryID},
			ordinary: true, visible: true, subject: true},
		"jit-history": {change: CatalogChange{ItemID: "jit-history", LibraryID: libraryID, ParentID: libraryID, IsFolder: true},
			ordinary: true, visible: true},
		"jit-theme": {change: CatalogChange{ItemID: "jit-theme", LibraryID: libraryID, ParentID: "jit-owner"},
			visible: true, subject: true, themeOwner: "jit-owner"},
		"jit-trailer": {change: CatalogChange{ItemID: "jit-trailer", LibraryID: libraryID, ParentID: "jit-owner"},
			visible: true, subject: true, extraOwner: "jit-owner"},
	}
	ids := make([]string, 0, len(expected))
	for id, item := range expected {
		properties := snapshot.items[id].properties
		if len(properties) != 64 {
			t.Fatalf("snapshot omitted the complete property digest for %s: %q", id, properties)
		}
		item.properties = properties
		expected[id] = item
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if snapshot.resync || !reflect.DeepEqual(snapshot.items, expected) || !slices.Equal(snapshot.ids, ids) ||
		!slices.Equal(snapshot.seeds, []string{"jit-ordinary", "jit-owner"}) {
		t.Fatalf("snapshot lost ordinary, auxiliary, or retained relationship facts: %+v", snapshot)
	}
}
