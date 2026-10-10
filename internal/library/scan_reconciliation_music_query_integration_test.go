//go:build linux

package library

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type musicAncestryQueryRow struct {
	id, parentID     string
	album, oversized bool
}

type musicAncestryQueryTx struct {
	pgx.Tx
	legacy        bool
	excluded      []string
	frontiers     [][]string
	rows          []musicAncestryQueryRow
	memberQueries int
	argumentBytes int
}

func (tx *musicAncestryQueryTx) Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error) {
	if strings.Contains(statement, "FROM items i WHERE i.id=ANY($1::text[]) AND i.library_id=$2") {
		if len(args) != 2 {
			return nil, fmt.Errorf("music ancestor query retained %d arguments, want 2", len(args))
		}
		frontier := args[0].([]string)
		for _, id := range frontier {
			if slices.Contains(tx.excluded, id) {
				return nil, fmt.Errorf("removed parent reached ancestor query: %q", id)
			}
			tx.argumentBytes += len(id)
		}
		tx.argumentBytes += len(args[1].(string))
		tx.frontiers = append(tx.frontiers, slices.Clone(frontier))
		if tx.legacy {
			statement = strings.Replace(statement, "ORDER BY i.id", "AND NOT (i.id=ANY($3::text[])) ORDER BY i.id", 1)
			args = append(args, tx.excluded)
			for _, id := range tx.excluded {
				tx.argumentBytes += len(id)
			}
		}
		rows, err := tx.Tx.Query(ctx, statement, args...)
		if err != nil {
			return nil, err
		}
		return &musicAncestryQueryRows{Rows: rows, owner: tx}, nil
	}
	if strings.Contains(statement, "FROM items i WHERE i.parent_id=ANY($1::text[]) AND i.library_id=$2") {
		if len(args) != 6 || !reflect.DeepEqual(args[2], tx.excluded) {
			return nil, fmt.Errorf("music member discovery changed its removed-member exclusion")
		}
		tx.memberQueries++
	}
	return tx.Tx.Query(ctx, statement, args...)
}

type musicAncestryQueryRows struct {
	pgx.Rows
	owner *musicAncestryQueryTx
}

func (rows *musicAncestryQueryRows) Scan(dest ...any) error {
	if err := rows.Rows.Scan(dest...); err != nil {
		return err
	}
	rows.owner.rows = append(rows.owner.rows, musicAncestryQueryRow{
		id: *dest[0].(*string), parentID: *dest[1].(*string),
		album: *dest[2].(*bool), oversized: *dest[3].(*bool),
	})
	return nil
}

func TestScanReconciliationMusicAncestorQueryOmitsRemovedIDs(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	const albumCount = acceptedMusicAlbumReadBatch + 1
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT 'surviving-album-'||lpad(n::text,3,'0'),$1,$2,$1,'Album','album','MusicAlbum',true,
			$3||'/Album-'||n,'Album-'||n FROM generate_series(1,$4::integer) n`,
		fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path, albumCount); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path)
		SELECT 'removed-member-'||lpad(n::text,3,'0'),$1,$2,'surviving-album-'||lpad(n::text,3,'0'),
			'Missing','missing','Audio',$3||'/Album-'||n||'/Missing.flac','Album-'||n||'/Missing.flac'
		FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path, albumCount); err != nil {
		t.Fatal(err)
	}
	parents := make(map[string]bool)
	removed := make(map[string]scanReconciliationItem)
	for index := 1; index <= albumCount; index++ {
		parents[fmt.Sprintf("surviving-album-%03d", index)] = true
		id := fmt.Sprintf("removed-member-%03d", index)
		removed[id] = scanReconciliationItem{id: id}
	}
	for _, seed := range []struct{ id, parent string }{
		{"shared-disc-a", "surviving-album-001"},
		{"shared-disc-b", "surviving-album-001"},
		{"removed-parent", fixture.library.ID},
		{"child-of-removed-parent", "removed-parent"},
		{"cycle-a", fixture.library.ID},
		{"cycle-b", "cycle-a"},
		{"foreign-parent", fixture.library.ID},
	} {
		scanReconciliationCommitInsertItem(t, fixture, seed.id, seed.id, "Folder", seed.parent, true)
		parents[seed.id] = true
	}
	removed["removed-parent"] = scanReconciliationItem{id: "removed-parent"}
	parents["missing-parent"] = true
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('foreign-music-library','Foreign music','music');
		UPDATE items SET library_id='foreign-music-library' WHERE id='foreign-parent';
		UPDATE items SET parent_id='cycle-b' WHERE id='cycle-a'`); err != nil {
		t.Fatal(err)
	}
	excluded := make([]string, 0, len(removed))
	for id := range removed {
		excluded = append(excluded, id)
	}
	slices.Sort(excluded)
	tx, err := fixture.pool.BeginTx(fixture.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	baseline := &musicAncestryQueryTx{Tx: tx, legacy: true, excluded: excluded}
	if err := checkScanReconciliationMusic(fixture.ctx, baseline, fixture.library.ID, parents, removed); err != nil {
		t.Fatalf("legacy music ancestor discovery: %v", err)
	}
	candidate := &musicAncestryQueryTx{Tx: tx, excluded: excluded}
	if err := checkScanReconciliationMusic(fixture.ctx, candidate, fixture.library.ID, parents, removed); err != nil {
		t.Fatalf("narrow music ancestor discovery: %v", err)
	}
	if !reflect.DeepEqual(candidate.frontiers, baseline.frontiers) || !reflect.DeepEqual(candidate.rows, baseline.rows) {
		t.Fatal("omitting removed IDs changed surviving, missing, shared, cyclic or cross-library ancestry")
	}
	if len(candidate.frontiers) < 2 || candidate.memberQueries < albumCount || candidate.memberQueries != baseline.memberQueries {
		t.Fatalf("fixture did not retain batched ancestors and member validation: batches=%d members=%d/%d",
			len(candidate.frontiers), candidate.memberQueries, baseline.memberQueries)
	}
	removedBytes := 0
	for _, id := range excluded {
		removedBytes += len(id)
	}
	if baseline.argumentBytes-candidate.argumentBytes != len(candidate.frontiers)*removedBytes {
		t.Fatalf("ancestor argument bytes = %d/%d, want %d removed bytes per batch",
			candidate.argumentBytes, baseline.argumentBytes, removedBytes)
	}
	if _, ready, err := readAcceptedMusicAlbumSource(fixture.ctx, tx, fixture.library.ID, "surviving-album-001", nil, nil); err != nil || ready {
		t.Fatalf("fixture missing member did not require descendant exclusion: ready=%v error=%v", ready, err)
	}
	t.Logf("ancestor batches=%d, ID argument content=%d bytes (previously %d), member queries=%d",
		len(candidate.frontiers), candidate.argumentBytes, baseline.argumentBytes, candidate.memberQueries)
}
