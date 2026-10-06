package library

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type scanAuxiliaryUpdateTrace struct {
	themeLocks, extraLocks     atomic.Int64
	themeUpdates, extraUpdates atomic.Int64
}

func (trace *scanAuxiliaryUpdateTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if strings.HasPrefix(statement, "update item_theme_resources") {
		trace.themeUpdates.Add(1)
	}
	if strings.HasPrefix(statement, "update item_extra_resources") {
		trace.extraUpdates.Add(1)
	}
	if strings.Contains(statement, "for update of resource") {
		if strings.Contains(statement, "join item_theme_resources relationship") {
			trace.themeLocks.Add(1)
		}
		if strings.Contains(statement, "join item_extra_resources relationship") {
			trace.extraLocks.Add(1)
		}
	}
	return ctx
}

func (*scanAuxiliaryUpdateTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *scanAuxiliaryUpdateTrace) assert(t *testing.T, themeUpdates, extraUpdates int64) {
	t.Helper()
	if trace.themeLocks.Load() != 1 || trace.extraLocks.Load() != 1 ||
		trace.themeUpdates.Load() != themeUpdates || trace.extraUpdates.Load() != extraUpdates {
		t.Fatalf("unexpected auxiliary statements: theme locks=%d updates=%d; extra locks=%d updates=%d; want one lock each and updates=%d/%d",
			trace.themeLocks.Load(), trace.themeUpdates.Load(), trace.extraLocks.Load(), trace.extraUpdates.Load(), themeUpdates, extraUpdates)
	}
}

func scanAuxiliaryUpdateFixture(t *testing.T) (context.Context, *Store, *pgx.Conn, *scanAuxiliaryUpdateTrace) {
	t.Helper()
	ctx, store := extraTestFixture(t)
	themeTestResource(t, ctx, store, "theme-history", "song", "theme-seed", "library-b", "Seed/theme-music/history.mp3")
	extraTestResource(t, ctx, store, "extra-unrelated", "theme-all", "library-b", "All/featurettes/Other.mp4", ExtraKindClip)
	if _, err := store.pool.Exec(ctx, `UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-history';
		UPDATE item_extra_resources SET active=false WHERE resource_item_id='extra-zeta';
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('aux-moved-root','library-b','/media/b-moved','/media','b-moved')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"theme-song", "theme-history", "extra-alpha", "extra-zeta", "extra-unrelated"} {
		userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: id, PlayCount: 7, IsFavorite: true, PlaybackPositionTicks: 91})
	}
	trace := &scanAuxiliaryUpdateTrace{}
	config := store.pool.Config().ConnConfig.Copy()
	config.Tracer = trace
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.Close(cleanup); err != nil {
			t.Errorf("close auxiliary update session: %v", err)
		}
	})
	return ctx, store, conn, trace
}

// Exclude only the deliberately moved owner and the expected active flags.
// Every other relationship, item, identity, metadata and user row must retain
// both its complete contents and its tuple version.
func scanAuxiliaryRetainedSnapshot(t *testing.T, ctx context.Context, source themeRowQuerier, ownerID string, changing []string) string {
	t.Helper()
	var snapshot string
	if err := source.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('version',i.xmin::text) ORDER BY id) FROM items i WHERE id<>$1),
		'owners',(SELECT jsonb_agg(to_jsonb(o)||jsonb_build_object('version',o.xmin::text) ORDER BY id) FROM theme_owner_ids o),
		'owner_sequence',(SELECT jsonb_build_array(last_value,is_called) FROM theme_owner_ids_id_seq),
		'metadata',(SELECT jsonb_agg(to_jsonb(m)||jsonb_build_object('version',m.xmin::text) ORDER BY item_id) FROM item_metadata_state m),
		'credits',(SELECT jsonb_agg(to_jsonb(e)||jsonb_build_object('version',e.xmin::text) ORDER BY item_id,entity_id,credit_group,position) FROM item_entities e),
		'userdata',(SELECT jsonb_agg(to_jsonb(u)||jsonb_build_object('version',u.xmin::text) ORDER BY user_id,item_id) FROM user_item_data u),
		'theme_reserved',(SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('version',p.xmin::text) ORDER BY root_id,relative_path) FROM theme_reserved_paths p),
		'extra_reserved',(SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('version',p.xmin::text) ORDER BY root_id,relative_path) FROM extra_reserved_paths p),
		'themes',(SELECT jsonb_agg(CASE WHEN r.resource_item_id=ANY($2::text[]) THEN to_jsonb(r)-'active'
			ELSE to_jsonb(r)||jsonb_build_object('version',r.xmin::text) END ORDER BY resource_item_id) FROM item_theme_resources r),
		'extras',(SELECT jsonb_agg(CASE WHEN r.resource_item_id=ANY($2::text[]) THEN to_jsonb(r)-'active'
			ELSE to_jsonb(r)||jsonb_build_object('version',r.xmin::text) END ORDER BY resource_item_id) FROM item_extra_resources r))::text`,
		ownerID, changing).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func scanAuxiliaryAssertActive(t *testing.T, ctx context.Context, source themeRowQuerier, ids []string, active bool) {
	t.Helper()
	var total, matching int
	if err := source.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE active=$2) FROM (
		SELECT resource_item_id,active FROM item_theme_resources
		UNION ALL SELECT resource_item_id,active FROM item_extra_resources) relationships
		WHERE resource_item_id=ANY($1::text[])`, ids, active).Scan(&total, &matching); err != nil {
		t.Fatal(err)
	}
	if total != len(ids) || matching != total {
		t.Fatalf("auxiliary rows lost their expected state: ids=%v active=%t total=%d matching=%d", ids, active, total, matching)
	}
}

func TestScanAuxiliaryEmptyChildrenSkipUpdates(t *testing.T) {
	for _, scenario := range []string{"no_relationships", "inactive_history"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, store, conn, trace := scanAuxiliaryUpdateFixture(t)
			ownerID := "theme-episode1"
			if scenario == "inactive_history" {
				ownerID = "theme-seed"
				if _, err := store.pool.Exec(ctx, `UPDATE item_theme_resources SET active=false WHERE owner_item_id='theme-seed' AND active;
					UPDATE item_extra_resources SET active=false WHERE owner_item_id='theme-seed' AND active`); err != nil {
					t.Fatal(err)
				}
			}
			before := scanAuxiliaryRetainedSnapshot(t, ctx, store.pool, "", nil)
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := deactivateInvalidThemeChildren(ctx, tx, []string{ownerID}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			trace.assert(t, 0, 0)
			if after := scanAuxiliaryRetainedSnapshot(t, ctx, store.pool, "", nil); after != before {
				t.Fatal("an empty active population rewrote retained auxiliary state")
			}
		})
	}
}

func TestScanAuxiliaryInvalidChildrenRepairWithinOwnerTransaction(t *testing.T) {
	for _, finish := range []string{"rollback", "commit"} {
		t.Run(finish, func(t *testing.T) {
			ctx, store, conn, trace := scanAuxiliaryUpdateFixture(t)
			changing := []string{"theme-song", "extra-alpha", "extra-middle", "extra-trailer"}
			scanAuxiliaryAssertActive(t, ctx, store.pool, changing, true)
			before := extraTestState(t, ctx, store)
			retained := scanAuxiliaryRetainedSnapshot(t, ctx, store.pool, "theme-seed", changing)
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if _, err := tx.Exec(ctx, `UPDATE items SET root_id='aux-moved-root',path='/media/b-moved/Seed/movie.mp4'
				WHERE id='theme-seed'`); err != nil {
				t.Fatal(err)
			}
			if err := deactivateInvalidThemeChildren(ctx, tx, []string{"theme-seed"}); err != nil {
				t.Fatal(err)
			}
			trace.assert(t, 1, 1)
			scanAuxiliaryAssertActive(t, ctx, tx, changing, false)
			if observed := extraTestState(t, ctx, store); observed != before {
				t.Fatal("uncommitted owner repair escaped to another database session")
			}
			if finish == "rollback" {
				if err := tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				if after := extraTestState(t, ctx, store); after != before {
					t.Fatal("owner rollback retained a partial auxiliary repair")
				}
				scanAuxiliaryAssertActive(t, ctx, store.pool, changing, true)
			} else {
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				scanAuxiliaryAssertActive(t, ctx, store.pool, changing, false)
				var rootID string
				if err := store.pool.QueryRow(ctx, `SELECT root_id FROM items WHERE id='theme-seed'`).Scan(&rootID); err != nil || rootID != "aux-moved-root" {
					t.Fatalf("auxiliary repair did not commit its owner move: root=%q error=%v", rootID, err)
				}
			}
			if after := scanAuxiliaryRetainedSnapshot(t, ctx, store.pool, "theme-seed", changing); after != retained {
				t.Fatal("owner repair changed resource identity, inactive history, unrelated relationships, metadata or user data")
			}
		})
	}
}

func TestScanAuxiliaryEmptyThemesStillRepairExtraChildren(t *testing.T) {
	ctx, store, conn, trace := scanAuxiliaryUpdateFixture(t)
	if _, err := store.pool.Exec(ctx, `UPDATE item_theme_resources SET active=false WHERE resource_item_id='theme-song'`); err != nil {
		t.Fatal(err)
	}
	changing := []string{"extra-alpha", "extra-middle", "extra-trailer"}
	scanAuxiliaryAssertActive(t, ctx, store.pool, changing, true)
	before := scanAuxiliaryRetainedSnapshot(t, ctx, store.pool, "theme-seed", changing)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `UPDATE items SET type='Episode' WHERE id='theme-seed'`); err != nil {
		t.Fatal(err)
	}
	if err := deactivateInvalidThemeChildren(ctx, tx, []string{"theme-seed"}); err != nil {
		t.Fatal(err)
	}
	trace.assert(t, 0, 1)
	scanAuxiliaryAssertActive(t, ctx, tx, changing, false)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	scanAuxiliaryAssertActive(t, ctx, store.pool, changing, false)
	if after := scanAuxiliaryRetainedSnapshot(t, ctx, store.pool, "theme-seed", changing); after != before {
		t.Fatal("extra-only repair changed retained theme history, resource identity, metadata or user data")
	}
}
