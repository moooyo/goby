package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func rootTopologyTestSnapshot() RootTopologySnapshot {
	identity := rootStorageIdentityFixture()
	return RootTopologySnapshot{Version: RootTopologyVersion,
		Mapping: RootTopologyMapping{ApprovedPath: "/approved", RegisteredPath: "/approved/library"},
		Anchor:  identity, RegisteredRoot: identity,
		Boundaries: []RootTopologyBoundary{{RelativePath: "nested/z", Identity: identity}, {RelativePath: "nested/a", Identity: identity}}}
}

func TestRootTopologyFingerprintIsCanonicalAndExcludesLiveWitnesses(t *testing.T) {
	snapshot := rootTopologyTestSnapshot()
	want, err := snapshot.Fingerprint()
	if err != nil || len(want) != 64 {
		t.Fatalf("canonical topology fingerprint failed (%T)", err)
	}
	reordered := cloneRootTopologySnapshot(snapshot)
	reordered.Boundaries[0], reordered.Boundaries[1] = reordered.Boundaries[1], reordered.Boundaries[0]
	if got, err := reordered.Fingerprint(); err != nil || got != want {
		t.Fatal("boundary input order changed the canonical stable fingerprint")
	}
	capture := &RootTopologyCapture{snapshot: snapshot, live: RootTopologyLiveWitness{
		Namespace: RootMountNamespaceWitness{Device: 1, Inode: 2}, Anchor: RootStorageWitness{MountID: 17},
		RegisteredRoot: RootStorageWitness{MountID: 18}, Boundaries: []RootTopologyLiveBoundary{{RelativePath: "nested/a", Witness: RootStorageWitness{MountID: 19}}},
	}}
	if got, err := capture.Fingerprint(); err != nil || got != want {
		t.Fatal("the capture did not use its stable snapshot fingerprint")
	}
	capture.live.Namespace.Inode++
	capture.live.Anchor.Device++
	capture.live.RegisteredRoot.Inode++
	capture.live.Boundaries[0].Witness.MountID++
	if got, err := capture.Fingerprint(); err != nil || got != want {
		t.Fatal("a live namespace or mount witness was included in persistent identity")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"namespace", "mount_id", "device", "inode", "source"} {
		if bytes.Contains(raw, []byte(`"`+field+`"`)) {
			t.Errorf("stable snapshot exposed live field %s", field)
		}
	}
}

func TestRootTopologyFingerprintIncludesEveryApprovedIdentityAndBoundary(t *testing.T) {
	before := rootTopologyTestSnapshot()
	want, err := before.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*RootTopologySnapshot)
	}{
		{"approved mapping", func(s *RootTopologySnapshot) { s.Mapping.ApprovedPath = "/" }},
		{"registered mapping", func(s *RootTopologySnapshot) { s.Mapping.RegisteredPath += "-other" }},
		{"anchor identity", func(s *RootTopologySnapshot) { s.Anchor.Handle[0]++ }},
		{"registered identity", func(s *RootTopologySnapshot) { s.RegisteredRoot.HandleType++ }},
		{"nested path", func(s *RootTopologySnapshot) { s.Boundaries[0].RelativePath += "-other" }},
		{"nested identity", func(s *RootTopologySnapshot) { s.Boundaries[0].Identity.Handle[0]++ }},
		{"lost nested mount", func(s *RootTopologySnapshot) { s.Boundaries = s.Boundaries[1:] }},
		{"new nested mount", func(s *RootTopologySnapshot) {
			s.Boundaries = append(s.Boundaries, RootTopologyBoundary{RelativePath: "added", Identity: rootStorageIdentityFixture()})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := cloneRootTopologySnapshot(before)
			test.change(&after)
			got, err := after.Fingerprint()
			if err != nil || got == want {
				t.Fatalf("a stable topology change did not change the fingerprint (%T)", err)
			}
		})
	}
}

func TestRootTopologyFingerprintRejectsPartialOrUnboundedSnapshots(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*RootTopologySnapshot)
		want   error
	}{
		{"version", func(s *RootTopologySnapshot) { s.Version++ }, ErrInvalidRootTopology},
		{"unbound anchor", func(s *RootTopologySnapshot) { s.Anchor = RootStorageIdentity{} }, ErrInvalidRootTopology},
		{"unbound root", func(s *RootTopologySnapshot) { s.RegisteredRoot = RootStorageIdentity{} }, ErrInvalidRootTopology},
		{"unbound nested boundary", func(s *RootTopologySnapshot) { s.Boundaries[0].Identity = RootStorageIdentity{} }, ErrInvalidRootTopology},
		{"prefix escape", func(s *RootTopologySnapshot) { s.Mapping.RegisteredPath = "/approved-other/library" }, ErrInvalidRootTopology},
		{"traversal", func(s *RootTopologySnapshot) { s.Mapping.RegisteredPath = "/approved/../other" }, ErrInvalidRootTopology},
		{"duplicate boundary", func(s *RootTopologySnapshot) { s.Boundaries[0].RelativePath = s.Boundaries[1].RelativePath }, ErrInvalidRootTopology},
		{"absolute boundary", func(s *RootTopologySnapshot) { s.Boundaries[0].RelativePath = "/nested" }, ErrInvalidRootTopology},
		{"boundary traversal", func(s *RootTopologySnapshot) { s.Boundaries[0].RelativePath = "../nested" }, ErrInvalidRootTopology},
		{"root as nested boundary", func(s *RootTopologySnapshot) { s.Boundaries[0].RelativePath = "." }, ErrInvalidRootTopology},
		{"path bytes", func(s *RootTopologySnapshot) {
			s.Boundaries[0].RelativePath = strings.Repeat("x", maxRootTopologyPathBytes+1)
		}, ErrInvalidRootTopology},
		{"boundary count", func(s *RootTopologySnapshot) {
			s.Boundaries = make([]RootTopologyBoundary, MaxRootTopologyBoundaries+1)
		}, ErrRootTopologyLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := rootTopologyTestSnapshot()
			test.change(&value)
			if digest, err := value.Fingerprint(); !errors.Is(err, test.want) || digest != "" {
				t.Fatalf("partial topology produced a fingerprint: %T", err)
			}
		})
	}
}

func TestRootTopologyCaptureCopiesSnapshotsAndClosedWitnessesCannotRevalidate(t *testing.T) {
	capture := &RootTopologyCapture{snapshot: rootTopologyTestSnapshot(), live: RootTopologyLiveWitness{
		Boundaries: []RootTopologyLiveBoundary{{RelativePath: "nested", Witness: RootStorageWitness{MountID: 1}}},
	}}
	before, err := capture.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	copy, err := capture.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	copy.Anchor.Handle[0]++
	copy.RegisteredRoot.Handle[0]++
	copy.Boundaries[0].Identity.Handle[0]++
	copy.Boundaries[0].RelativePath = "rewritten"
	live, err := capture.LiveWitness()
	if err != nil {
		t.Fatal(err)
	}
	live.Boundaries[0].Witness.MountID++
	if got, err := capture.Fingerprint(); err != nil || got != before || capture.live.Boundaries[0].Witness.MountID != 1 {
		t.Fatal("a caller mutated the retained topology through a returned snapshot")
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal("topology Close is not idempotent")
	}
	if err := capture.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyUnavailable) {
		t.Fatalf("closed witnesses revalidated: %T", err)
	}
	if got, err := capture.Fingerprint(); err != nil || got != before {
		t.Fatal("closing descriptors destroyed the historical stable observation")
	}
	var absent *RootTopologyCapture
	if _, err := absent.Snapshot(); !errors.Is(err, ErrInvalidRootTopology) {
		t.Fatal("an absent capture exposed a complete snapshot")
	}
	if _, err := absent.Fingerprint(); !errors.Is(err, ErrInvalidRootTopology) {
		t.Fatal("an absent capture produced a fingerprint")
	}
}
