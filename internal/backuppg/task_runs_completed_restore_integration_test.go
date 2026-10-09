//go:build linux

package backuppg

import (
	"fmt"
	"strings"
	"testing"
)

func TestPostgreSQLCompletedRunIndexArchivePreservesSchema64History(t *testing.T) {
	for _, version := range []int64{64, 65} {
		t.Run(fmt.Sprintf("schema%d", version), func(t *testing.T) {
			ctx, source, target, options := recoveryFixtureAtVersion(t, version)
			seedPhase3HistoricalTaskWitness(t, ctx, source)
			if _, err := source.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,created_at,finished_at)
				SELECT repeat(letter,32),repeat('6',32),state,'manual','phase3.historical','Historical startup task',
				created::timestamptz,finished::timestamptz
				FROM (VALUES('9','failed','2020-01-01T00:00:00Z','2020-01-05T00:01:00Z'),
				('a','cancelled','2020-01-04T00:00:00Z','2020-01-05T00:00:00Z'),
				('b','pending','2020-01-06T00:00:00Z',NULL)) fixture(letter,state,created,finished)`); err != nil {
				t.Fatal("seed retained completed-time ties and active task history", err)
			}
			before, sequences := unchangedSourceWitness(t, ctx, source, options)
			archive, facts := sourceArchive(t, ctx, source, options)
			result, err := RestoreOffline(ctx, target, archive, facts, options)
			if err != nil || result.SourceVersion != version || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
				t.Fatalf("restore completed-run archive through the trusted index migration: %v", err)
			}
			targetOptions := options
			targetOptions.SourceURL = target.Config().ConnString()
			after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
			if version < currentRecoveryVersion(t) {
				assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
			} else if !equalJSON(before.Tables, after.Tables) || !equalJSON(sequences, targetSequences) {
				t.Fatal("current completed-run catalog restoration changed retained rows or sequence state")
			}
			var valid bool
			if err := target.QueryRow(ctx, `SELECT to_regclass('task_runs_completed_idx') IS NOT NULL
				AND to_regclass('task_runs_history_idx') IS NOT NULL
				AND (SELECT count(*) FROM task_runs)=4
				AND EXISTS(SELECT 1 FROM task_runs WHERE id=repeat('b',32) AND state='pending' AND finished_at IS NULL)`).Scan(&valid); err != nil || !valid {
				t.Fatalf("restoration lost task history or the completed/history index pair: %v", err)
			}
			var latest string
			if err := target.QueryRow(ctx, `SELECT id FROM task_runs WHERE task_id=repeat('6',32)
				AND finished_at IS NOT NULL ORDER BY finished_at DESC,id DESC LIMIT 1`).Scan(&latest); err != nil || latest != strings.Repeat("9", 32) {
				t.Fatalf("restored completed-run selection changed its time/ID ordering: %s, %v", latest, err)
			}
			assertSourceWitness(t, ctx, source, options, before, sequences)
		})
	}
}
