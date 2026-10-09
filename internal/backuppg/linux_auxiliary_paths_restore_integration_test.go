//go:build linux

package backuppg

import (
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/database"
)

func TestPostgreSQLLinuxAuxiliaryPathsArchivePreservesRolesAndHistory(t *testing.T) {
	for _, version := range []int64{65, 66} {
		t.Run(fmt.Sprintf("schema%d", version), func(t *testing.T) {
			ctx, source, target, options := recoveryFixtureAtVersion(t, version)
			seedExtraSnapshotWitness(t, ctx, source)
			if version == 66 {
				if _, err := source.Exec(ctx, `UPDATE items SET relative_path='C:'||relative_path
					WHERE root_id='theme-root' AND relative_path LIKE 'Owner/%';
					UPDATE theme_reserved_paths SET relative_path='C:'||relative_path WHERE root_id='theme-root';
					UPDATE extra_reserved_paths SET relative_path='C:'||relative_path WHERE root_id='theme-root'`); err != nil {
					t.Fatal("seed current Linux colon paths without changing resource identities", err)
				}
			}
			want := extraSnapshotState(t, ctx, source)
			before, sequences := unchangedSourceWitness(t, ctx, source, options)
			archive, facts := sourceArchive(t, ctx, source, options)
			result, err := RestoreOffline(ctx, target, archive, facts, options)
			if err != nil || result.SourceVersion != version || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
				t.Fatalf("restore auxiliary paths with their original schema semantics: %v", err)
			}
			if extraSnapshotState(t, ctx, target) != want {
				t.Fatal("restoration changed auxiliary paths, active/inactive roles, owner IDs, or user state")
			}
			for _, id := range []string{"theme-song", "extra-clip"} {
				var ordinary, direct bool
				if err := target.QueryRow(ctx, `SELECT `+database.CatalogOrdinaryItemSQL("i")+`,`+database.CatalogDirectItemSQL("i")+
					` FROM items i WHERE id=$1`, id).Scan(&ordinary, &direct); err != nil || ordinary || !direct {
					t.Fatalf("restored auxiliary lost direct visibility or ordinary exclusion: %s, %v", id, err)
				}
			}
			assertSourceWitness(t, ctx, source, options, before, sequences)
		})
	}
}
