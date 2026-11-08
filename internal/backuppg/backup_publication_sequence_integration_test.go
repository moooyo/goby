//go:build linux

package backuppg

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
)

func TestPostgreSQLRestoreRejectsPublicationReceiptSequenceCollisionAfterLogPruning(t *testing.T) {
	for _, called := range []bool{false, true} {
		name := "uncalled_at_retained_id"
		if called {
			name = "called_below_retained_id"
		}
		t.Run(name, func(t *testing.T) {
			ctx, source, target, options := recoveryFixture(t)
			const start int64 = 9007199254740993
			if _, err := source.Exec(ctx, `SELECT setval('activity_entries_id_seq',$1,true)`, start); err != nil {
				t.Fatal(err)
			}
			backupID, digest := strings.Repeat("a", 32), strings.Repeat("b", 64)
			tx, err := source.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if err = activity.RecordBackupPublished(ctx, tx, backupID, digest, 1024); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var retainedID int64
			if err = source.QueryRow(ctx, `SELECT activity_id FROM backup_publication_receipts WHERE backup_id=$1`, backupID).Scan(&retainedID); err != nil || retainedID <= start {
				t.Fatalf("publication receipt lost exact large activity identity: %d %v", retainedID, err)
			}
			if _, err = source.Exec(ctx, `DELETE FROM activity_entries WHERE action='backup.published' AND resource_id=$1`, backupID); err != nil {
				t.Fatal(err)
			}
			var displayMax int64
			if err = source.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM activity_entries`).Scan(&displayMax); err != nil || displayMax >= retainedID-1 {
				t.Fatal("the fixture did not isolate the pruned publication consumer")
			}
			unsafeValue := retainedID
			if called {
				unsafeValue--
			}
			if _, err = source.Exec(ctx, `SELECT setval('activity_entries_id_seq',$1,$2)`, unsafeValue, called); err != nil {
				t.Fatal(err)
			}
			before, sequences := unchangedSourceWitness(t, ctx, source, options)
			archive, facts := sourceArchive(t, ctx, source, options)
			if _, err = Restore(ctx, source, target, archive, facts, options); !errors.Is(err, ErrArchive) {
				t.Fatalf("restore accepted a next activity id colliding with pruned publication history: %v", err)
			}
			var objects int
			if err = target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, options.Schema).Scan(&objects); err != nil || objects != 0 {
				t.Fatal("rejected sequence state retained target schema objects")
			}
			assertSourceWitness(t, ctx, source, options, before, sequences)
			// A safe state remains restorable even though the retained receipt
			// deliberately has no foreign key to the pruned display-log row.
			if _, err = source.Exec(ctx, `SELECT setval('activity_entries_id_seq',$1,true)`, retainedID); err != nil {
				t.Fatal(err)
			}
			safeBefore, safeSequences := unchangedSourceWitness(t, ctx, source, options)
			safeArchive, safeFacts := sourceArchive(t, ctx, source, options)
			if _, err = Restore(ctx, source, target, safeArchive, safeFacts, options); err != nil {
				t.Fatalf("safe retained-publication sequence state was rejected: %v", err)
			}
			var nextID int64
			if err = target.QueryRow(ctx, `SELECT nextval('activity_entries_id_seq')`).Scan(&nextID); err != nil || nextID <= retainedID {
				t.Fatalf("restoration would reuse retained publication identity: %d %v", nextID, err)
			}
			assertSourceWitness(t, ctx, source, options, safeBefore, safeSequences)
		})
	}
}
