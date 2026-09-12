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
	// Change only library_roots so an audit append cannot mask missing binding
	// fields in the retained-data fingerprint.
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
		name, statement string
		args            []any
	}{
		{"revision", `UPDATE library_roots SET binding_revision=2 WHERE id='reset-binding-root'`, nil},
		{"initial_approval", `UPDATE library_roots SET binding_revision=3,storage_binding=$1::jsonb,
			bound_at='2026-09-12T00:00:00.123456Z',bound_by='historical-binding-admin' WHERE id='reset-binding-root'`, []any{approved}},
		{"replacement_identity", `UPDATE library_roots SET storage_binding=$1::jsonb
			WHERE id='reset-binding-root'`, []any{replaced}},
		{"approval_time", `UPDATE library_roots SET bound_at=bound_at+interval '1 microsecond'
			WHERE id='reset-binding-root'`, nil},
		{"historical_actor", `UPDATE library_roots SET bound_by='another-historical-admin'
			WHERE id='reset-binding-root'`, nil},
	}
	for _, mutation := range mutations {
		if !t.Run(mutation.name, func(t *testing.T) {
			before := f.capture(t, f.source)
			f.exec(t, f.source, mutation.statement, mutation.args...)
			after := f.capture(t, f.source)
			if before.RawMarker != after.RawMarker || before.Marker != after.Marker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("the binding-only witness changed the recovery marker or table inventory")
			}
			changed := 0
			for index, previous := range before.Facts.Tables {
				current := after.Facts.Tables[index]
				if reflect.DeepEqual(previous, current) {
					continue
				}
				if previous.Name != "library_roots" || current.Name != previous.Name || current.Rows != previous.Rows {
					t.Fatal("a table other than the existing root binding changed")
				}
				changed++
			}
			if changed != 1 {
				t.Fatal("the retained-data fingerprint omitted a persisted root binding field")
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
