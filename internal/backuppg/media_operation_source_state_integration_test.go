//go:build linux

package backuppg

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedMediaOperationSourceArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('source-cache-library','Source cache history','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('source-cache-root','source-cache-library','/retired/source-cache','/retired','source-cache');
		INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type,file_identity,file_size,modified_at,media)
		VALUES('source-cache-item','source-cache-library','source-cache-root','Film.mkv','Film','film','Movie',
		'retained-file',12345,'2026-01-01T00:00:00.123456Z','{"DurationTicks":100000000,"Streams":[]}')`); err != nil {
		t.Fatal("seed retained media operation source facts", err)
	}
}

func assertMigratedMediaOperationSourceCache(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM items item LEFT JOIN library_roots root ON root.id=item.root_id
		WHERE CASE WHEN item.media IS NULL OR root.id IS NULL THEN
			item.media_operation_source_revision IS NOT NULL OR item.media_operation_source_binding_revision IS NOT NULL
		ELSE item.media_operation_source_binding_revision IS DISTINCT FROM root.binding_revision
			OR item.media_operation_source_revision IS DISTINCT FROM 'media-operation-source-v1-'||md5(jsonb_build_array(
				item.root_id,item.relative_path,item.file_identity,item.file_size,
				extract(epoch FROM item.modified_at),item.media,root.binding_revision)::text) END)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("source cache migration did not preserve exact derived values and null fallback: valid=%v error=%v", valid, err)
	}
}

func TestPostgreSQLMediaOperationSourceCacheValidatesItsObservedBinding(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedMediaOperationSourceArchiveState(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		valid          bool
	}{
		{"current_cache", "", true},
		{"rebound_root", `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id='source-cache-root'`, true},
		{"uncached_facts", `UPDATE items SET media_operation_source_revision=NULL WHERE id='source-cache-item'`, true},
		{"removed_media", `UPDATE items SET media=NULL WHERE id='source-cache-item'`, true},
		{"forged_cache", `UPDATE items SET media_operation_source_revision='media-operation-source-v1-'||repeat('0',32) WHERE id='source-cache-item'`, false},
		{"changed_sampled_binding", `UPDATE items SET media_operation_source_binding_revision=media_operation_source_binding_revision+1 WHERE id='source-cache-item'`, false},
		{"changed_source_without_cache_refresh", `ALTER TABLE items DISABLE TRIGGER USER;
			UPDATE items SET file_identity='changed-file' WHERE id='source-cache-item'; ALTER TABLE items ENABLE TRIGGER USER`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatal("apply SQL-valid source cache mutation", err)
				}
			}
			err = validateMediaOperationSourceState(ctx, tx, 63)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("source cache validity=%v result=%v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLMediaOperationSourceCacheRestoreRetainsStaleBinding(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedMediaOperationSourceArchiveState(t, ctx, source)
	if _, err := source.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id='source-cache-root'`); err != nil {
		t.Fatal(err)
	}
	before := historicalArchiveRows(t, ctx, source, "items", 63)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := RestoreOffline(ctx, target, archive, facts, options); err != nil {
		t.Fatalf("restore a valid cache that predates a root rebind: %v", err)
	}
	if after := historicalArchiveRows(t, ctx, target, "items", 63); before != after {
		t.Fatal("restore rewrote cached source facts before preserving the archive fingerprint")
	}
	var cached, current int64
	if err := target.QueryRow(ctx, `SELECT item.media_operation_source_binding_revision,root.binding_revision
		FROM items item JOIN library_roots root ON root.id=item.root_id WHERE item.id='source-cache-item'`).Scan(&cached, &current); err != nil || cached != 1 || current != 2 {
		t.Fatalf("restore lost the legitimate cache miss: cached=%d current=%d error=%v", cached, current, err)
	}
}

func TestPostgreSQLMediaOperationSourceCacheSnapshotAndRestoreRejectForgedStamp(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedMediaOperationSourceArchiveState(t, ctx, source)
	valid, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		t.Fatal("read trusted source identity before corrupting the cache", err)
	}
	plan := valid.plan
	_ = valid.Close()
	if _, err := source.Exec(ctx, `UPDATE items SET media_operation_source_revision='media-operation-source-v1-'||repeat('0',32) WHERE id='source-cache-item'`); err != nil {
		t.Fatal(err)
	}
	before := historicalArchiveRows(t, ctx, source, "items", 63)
	snapshot, err := OpenSnapshot(ctx, source, options)
	if snapshot != nil {
		_ = snapshot.Close()
	}
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("snapshot accepted a cache unrelated to its source facts: %v", err)
	}
	archive, facts := uncheckedThemeArchive(t, ctx, source, plan)
	result, err := RestoreOffline(ctx, target, archive, facts, options)
	if !errors.Is(err, ErrArchive) || result.CurrentVersion != 0 {
		t.Fatalf("restore trusted row fingerprints without validating the cached stamp: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	if historicalArchiveRows(t, ctx, source, "items", 63) != before {
		t.Fatal("rejected restore repaired the invalid source cache")
	}
}

func TestPostgreSQLMediaOperationSourceCacheMigrationPreservesSchema62Archive(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 62)
	seedMediaOperationSourceArchiveState(t, ctx, source)
	before := historicalArchiveRows(t, ctx, source, "items", 62)
	archive, facts := sourceArchive(t, ctx, source, options)
	result, err := RestoreOffline(ctx, target, archive, facts, options)
	if err != nil || result.SourceVersion != 62 || result.CurrentVersion != currentRecoveryVersion(t) {
		t.Fatalf("restore and upgrade schema62 source facts: source=%d current=%d error=%v", result.SourceVersion, result.CurrentVersion, err)
	}
	if after := historicalArchiveRows(t, ctx, target, "items", 62); before != after {
		t.Fatal("source cache migration rewrote historical item facts")
	}
	assertMigratedMediaOperationSourceCache(t, ctx, target)
	var populated bool
	if err := target.QueryRow(ctx, `SELECT media_operation_source_revision IS NOT NULL
		AND media_operation_source_binding_revision=1 FROM items WHERE id='source-cache-item'`).Scan(&populated); err != nil || !populated {
		t.Fatalf("source cache migration did not prepare retained media: populated=%v error=%v", populated, err)
	}
}
