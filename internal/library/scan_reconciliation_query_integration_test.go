//go:build linux

package library

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func reconciliationQueryReadDescendants(tx OwnedTx, statement string, frontier []string) ([]scanReconciliationItem, error) {
	rows, err := tx.Query(statement, frontier, scanReconciliationMaxItems+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	budget := &scanReconciliationBudgetState{}
	var items []scanReconciliationItem
	for rows.Next() {
		item, err := scanReconciliationReadItem(rows)
		if err != nil {
			return nil, err
		}
		if err := budget.retain(item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func reconciliationQueryLegacyDescendants() string {
	return `SELECT ` + scanReconciliationItemColumns + `
		FROM items i LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
		LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
		WHERE ` + scanReconciliationDescendantPredicate + ` ORDER BY i.id LIMIT $2 FOR UPDATE OF i`
}

func TestScanReconciliationUnionDescendantsPreservesAllEdgesAndFacts(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "gone-owner", "Gone.mkv", "Movie", fixture.library.ID, false)
	for _, seed := range []struct{ id, relative, kind, parent string }{
		{"ordinary-child", "Gone/Child.mkv", "Movie", "gone-owner"},
		{"duplicate-edge-theme", "Theme/theme.mp3", "Audio", "gone-owner"},
		{"inactive-extra", "Extras/Clip.mp4", "Video", fixture.library.ID},
		{"foreign-child", "Foreign.mkv", "Movie", "gone-owner"},
		{"cross-role-child", "Cross.mkv", "Movie", "gone-owner"},
	} {
		scanReconciliationCommitInsertItem(t, fixture, seed.id, seed.relative, seed.kind, seed.parent, false)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES ('query-foreign-library','Query foreign','movies');
		UPDATE items SET library_id='query-foreign-library' WHERE id='foreign-child'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES
		($1,'Theme/theme.mp3',false),($1,'Cross.mkv',false)`, fixture.scanRoot.id); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES
		($1,'Extras',true),($1,'Cross.mkv',false)`, fixture.scanRoot.id); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('duplicate-edge-theme','gone-owner','song',false),('cross-role-child','gone-owner','video',false);
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('inactive-extra','gone-owner','clip',false),('cross-role-child','gone-owner','clip',false)`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
		baseline, err := reconciliationQueryReadDescendants(tx, reconciliationQueryLegacyDescendants(), []string{"gone-owner"})
		if err != nil {
			return err
		}
		candidate, err := reconciliationQueryReadDescendants(tx, scanReconciliationDescendantQuery(), []string{"gone-owner"})
		if err != nil {
			return err
		}
		if len(candidate) != 5 || !reflect.DeepEqual(baseline, candidate) {
			return fmt.Errorf("UNION discovery changed inactive, foreign, duplicate-edge or cross-role facts: baseline=%+v candidate=%+v", baseline, candidate)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScanReconciliationUnionDescendantsRechecksParentAfterRowLockWait(t *testing.T) {
	for _, shape := range []struct {
		name      string
		statement string
	}{
		{"legacy", reconciliationQueryLegacyDescendants()},
		{"union", scanReconciliationDescendantQuery()},
	} {
		for _, direction := range []string{"away", "into"} {
			t.Run(shape.name+"-"+direction, func(t *testing.T) {
				fixture := newRootBindingScanFixture(t)
				scanReconciliationCommitInsertItem(t, fixture, "gone-parent", "Gone", "Folder", fixture.library.ID, true)
				scanReconciliationCommitInsertItem(t, fixture, "other-parent", "Other", "Folder", fixture.library.ID, true)
				parent := "gone-parent"
				if direction == "into" {
					parent = "other-parent"
				}
				scanReconciliationCommitInsertItem(t, fixture, "changing-child", "Child.mkv", "Movie", parent, false)
				barrier, err := fixture.pool.Begin(fixture.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(barrier)
				updatedParent := "other-parent"
				if direction == "into" {
					updatedParent = "gone-parent"
					scanReconciliationCommitInsertItem(t, fixture, "gate-child", "Gate.mkv", "Movie", "gone-parent", false)
					if _, err := barrier.Exec(fixture.ctx, `SELECT id FROM items WHERE id='gate-child' FOR UPDATE`); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := barrier.Exec(fixture.ctx, `UPDATE items SET parent_id=$1 WHERE id='changing-child'`, updatedParent); err != nil {
					t.Fatal(err)
				}
				completed := make(chan error, 1)
				var admitted []scanReconciliationItem
				go func() {
					completed <- fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
						var err error
						admitted, err = reconciliationQueryReadDescendants(tx, shape.statement, []string{"gone-parent"})
						return err
					})
				}()
				waitCatalogApplicationBlock(t, fixture.ctx, fixture.pool, barrier.Conn().PgConn().PID())
				if err := barrier.Commit(fixture.ctx); err != nil {
					t.Fatal(err)
				}
				if err := <-completed; err != nil {
					t.Fatal(err)
				}
				if direction == "away" && len(admitted) != 0 || direction == "into" && (len(admitted) != 1 || admitted[0].id != "gate-child") {
					t.Fatalf("row-lock wait changed statement-snapshot membership: %+v", admitted)
				}
			})
		}
	}
}

func TestScanReconciliationUnionDescendantsRetainsBudgetSentinel(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "gone-parent", "Gone", "Folder", fixture.library.ID, true)
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT 'sentinel-child-'||lpad(n::text,6,'0'),$1,$2,'gone-parent','Sentinel','sentinel','Movie',false,
		$3||'/Gone/Child-'||n::text||'.mkv','Gone/Child-'||n::text||'.mkv'
		FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path,
		scanReconciliationMaxItems+1); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{reconciliationQueryLegacyDescendants(), scanReconciliationDescendantQuery()} {
		err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
			_, err := reconciliationQueryReadDescendants(tx, statement, []string{"gone-parent"})
			return err
		})
		if !errors.Is(err, errScanReconciliationEvidenceBudget) {
			t.Fatalf("an oversized closure escaped the sentinel: %v", err)
		}
	}
}

func TestScanReconciliationUnionDescendantsRetainsOversizedFactFence(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "gone-parent", "Gone", "Folder", fixture.library.ID, true)
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		VALUES ('oversized-child-'||repeat('x',257),$1,$2,'gone-parent','Oversized','oversized','Movie',false,
		$3||'/Gone/Child.mkv','Gone/Child.mkv')`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{reconciliationQueryLegacyDescendants(), scanReconciliationDescendantQuery()} {
		err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
			_, err := reconciliationQueryReadDescendants(tx, statement, []string{"gone-parent"})
			return err
		})
		if !errors.Is(err, errScanReconciliationEvidenceBudget) {
			t.Fatalf("an oversized persisted fact became a truncated deletion identity: %v", err)
		}
	}
}

func TestScanReconciliationUnionDescendantsPreservesAuxiliaryStatementSnapshot(t *testing.T) {
	for _, shape := range []struct{ name, statement string }{
		{"legacy", reconciliationQueryLegacyDescendants()},
		{"union", scanReconciliationDescendantQuery()},
	} {
		for _, direction := range []string{"away", "into"} {
			t.Run(shape.name+"-"+direction, func(t *testing.T) {
				fixture := newRootBindingScanFixture(t)
				scanReconciliationCommitInsertItem(t, fixture, "gone-owner", "Gone.mkv", "Movie", fixture.library.ID, false)
				scanReconciliationCommitInsertItem(t, fixture, "other-owner", "Other.mkv", "Movie", fixture.library.ID, false)
				scanReconciliationCommitInsertItem(t, fixture, "changing-theme", "Theme/theme.mp3", "Audio", fixture.library.ID, false)
				owner, updatedOwner := "gone-owner", "other-owner"
				if direction == "into" {
					owner, updatedOwner = updatedOwner, owner
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
					VALUES ($1,'Theme/theme.mp3',false)`, fixture.scanRoot.id); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
					VALUES ('changing-theme',$1,'song',false)`, owner); err != nil {
					t.Fatal(err)
				}
				barrier, err := fixture.pool.Begin(fixture.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(barrier)
				gate := "changing-theme"
				if direction == "into" {
					gate = "gate-child"
					scanReconciliationCommitInsertItem(t, fixture, gate, "Gate.mkv", "Movie", "gone-owner", false)
				}
				if _, err := barrier.Exec(fixture.ctx, `SELECT id FROM items WHERE id=$1 FOR UPDATE`, gate); err != nil {
					t.Fatal(err)
				}
				if _, err := barrier.Exec(fixture.ctx, `UPDATE item_theme_resources SET owner_item_id=$1
					WHERE resource_item_id='changing-theme'`, updatedOwner); err != nil {
					t.Fatal(err)
				}
				completed := make(chan error, 1)
				var admitted []scanReconciliationItem
				go func() {
					completed <- fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
						var err error
						admitted, err = reconciliationQueryReadDescendants(tx, shape.statement, []string{"gone-owner"})
						return err
					})
				}()
				waitCatalogApplicationBlock(t, fixture.ctx, fixture.pool, barrier.Conn().PgConn().PID())
				if err := barrier.Commit(fixture.ctx); err != nil {
					t.Fatal(err)
				}
				if err := <-completed; err != nil {
					t.Fatal(err)
				}
				// Joined auxiliary facts retain the original statement snapshot.
				// Production auxiliary publishers additionally honor catalog
				// ownership and item locks; key discovery does not replace them.
				if len(admitted) != 1 || admitted[0].id != gate || direction == "away" && admitted[0].themeOwner != owner {
					t.Fatalf("auxiliary lock wait changed the original statement snapshot: %+v", admitted)
				}
			})
		}
	}
}
