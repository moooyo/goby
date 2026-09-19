//go:build linux

package recoverydb

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/storagebinding"
)

// Run within the existing exclusive-pair sequence. A second standalone fixture
// must not adopt the populated target left by that sequence's final step.
func assertRecoveryRootBindingResetProtection(t *testing.T, f *recoveryStoreFixture, original Retained) {
	t.Helper()
	// These are offline catalog values, not captured or approved media paths.
	// Change root bindings without an audit append that could mask an omitted
	// fingerprint. Relevant root changes also advance the library edit revision.
	f.exec(t, f.source, `INSERT INTO libraries(id,name,collection_type)
		VALUES('reset-binding-library','Reset binding witness','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('reset-binding-root','reset-binding-library','/offline/reset/media','/offline/reset','media')`)
	identity := storagebinding.Identity{Version: storagebinding.IdentityVersion, Profile: storagebinding.IdentityProfile,
		FilesystemUUID: "00112233445566778899aabbccddeeff", HandleType: 1, Handle: []byte{0, 128, 255, 1}}
	snapshot := storagebinding.Snapshot{Version: storagebinding.TopologyVersion,
		Mapping: storagebinding.Mapping{ApprovedPath: "/offline/reset", RegisteredPath: "/offline/reset/media"},
		Anchor:  identity, RegisteredRoot: identity, Boundaries: []storagebinding.Boundary{}}
	encode := func() string {
		raw, err := storagebinding.EncodeSnapshot(snapshot)
		if err != nil {
			t.Fatalf("encode offline root binding witness: %v", err)
		}
		return string(raw)
	}
	approved := encode()
	snapshot.RegisteredRoot.Handle = []byte{0, 128, 255, 2}
	replaced := encode()
	mutations := []struct {
		name, statement      string
		args                 []any
		libraryRevisionDelta int64
	}{
		{"revision", `UPDATE library_roots SET binding_revision=2 WHERE id='reset-binding-root'`, nil, 1},
		{"initial_approval", `UPDATE library_roots SET binding_revision=3,storage_binding=$1::jsonb,
			bound_at='2026-09-12T00:00:00.123456Z',bound_by='historical-binding-admin' WHERE id='reset-binding-root'`, []any{approved}, 1},
		{"replacement_identity", `UPDATE library_roots SET storage_binding=$1::jsonb
			WHERE id='reset-binding-root'`, []any{replaced}, 1},
		{"approval_time", `UPDATE library_roots SET bound_at=bound_at+interval '1 microsecond'
			WHERE id='reset-binding-root'`, nil, 0},
		{"historical_actor", `UPDATE library_roots SET bound_by='another-historical-admin'
			WHERE id='reset-binding-root'`, nil, 0},
	}
	for _, mutation := range mutations {
		if !t.Run(mutation.name, func(t *testing.T) {
			readLibrary := func() (int64, string) {
				var revision int64
				var unchanged string
				if err := f.source.pool.QueryRow(f.ctx, `SELECT revision,(to_jsonb(l)-'revision')::text
					FROM libraries l WHERE id='reset-binding-library'`).Scan(&revision, &unchanged); err != nil {
					t.Fatalf("read the library edit revision surrounding a root binding change: %v", err)
				}
				return revision, unchanged
			}
			before := f.capture(t, f.source)
			previousRevision, previousLibrary := readLibrary()
			f.exec(t, f.source, mutation.statement, mutation.args...)
			after := f.capture(t, f.source)
			currentRevision, currentLibrary := readLibrary()
			if currentRevision != previousRevision+mutation.libraryRevisionDelta || currentLibrary != previousLibrary {
				t.Fatal("the root binding changed unrelated library fields or advanced its edit revision incorrectly")
			}
			if before.RawMarker != after.RawMarker || before.Marker != after.Marker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("the binding-only witness changed the recovery marker or table inventory")
			}
			changed := map[string]bool{"library_roots": false}
			if mutation.libraryRevisionDelta != 0 {
				changed["libraries"] = false
			}
			for index, previous := range before.Facts.Tables {
				current := after.Facts.Tables[index]
				if reflect.DeepEqual(previous, current) {
					continue
				}
				if _, expected := changed[previous.Name]; !expected || current.Name != previous.Name || current.Rows != previous.Rows {
					t.Fatal("a table outside the existing root binding and library edit revision changed")
				}
				changed[previous.Name] = true
			}
			for table, observed := range changed {
				if !observed {
					t.Fatalf("the retained-data fingerprint omitted a persisted change in %s", table)
				}
			}
			active := recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration)
			if err := f.source.store.ResetOwnedTarget(f.ctx, active, before); !errors.Is(err, ErrConflict) {
				t.Fatalf("a stale root binding proof authorized target reset: %v", err)
			}
			f.assertFacts(t, f.source, after.Facts)
			f.assertNamespace(t, f.source, false)
			f.assertEmpty(t, f.target)
		}) {
			t.Fatal("stop using the exclusive pair after a failed binding reset assertion")
		}
	}
	// Remove only the two rows created above, preserving the sequence's exact
	// earlier source facts and its separate unchanged-proof reset acceptance.
	f.exec(t, f.source, `DELETE FROM libraries WHERE id='reset-binding-library'`)
	f.assertFacts(t, f.source, original.Facts)
	if f.read(t, f.source) != original.RawMarker {
		t.Fatal("binding reset protection changed the preserved recovery claim")
	}
}
