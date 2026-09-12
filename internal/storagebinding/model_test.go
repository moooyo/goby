package storagebinding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const modelIdentityGolden = `{"version":1,"profile":"linux-fsuuid-filehandle-v1","filesystem_uuid":"0123456789abcdef0123456789abcdef","handle_type":-2147483648,"handle":"AQID"}`
const modelOtherIdentityGolden = `{"version":1,"profile":"linux-fsuuid-filehandle-v1","filesystem_uuid":"0123456789abcdef0123456789abcdef","handle_type":7,"handle":"BAUG"}`
const modelSnapshotGoldenPrefix = `{"version":1,"mapping":{"approved_path":"/approved","registered_path":"/approved/library"},"anchor":` + modelIdentityGolden + `,"registered_root":` + modelOtherIdentityGolden + `,"boundaries":`
const modelSnapshotGolden = modelSnapshotGoldenPrefix + `[{"relative_path":"nested/\u003ca\u003e","identity":` + modelIdentityGolden + `},{"relative_path":"nested/z","identity":` + modelOtherIdentityGolden + `}]}`

func modelTestIdentity() Identity {
	return Identity{Version: IdentityVersion, Profile: IdentityProfile,
		FilesystemUUID: "0123456789abcdef0123456789abcdef", HandleType: -2147483648, Handle: []byte{1, 2, 3}}
}

func modelTestOtherIdentity() Identity {
	identity := modelTestIdentity()
	identity.HandleType, identity.Handle = 7, []byte{4, 5, 6}
	return identity
}

func modelTestSnapshot() Snapshot {
	return Snapshot{Version: TopologyVersion,
		Mapping: Mapping{ApprovedPath: "/approved", RegisteredPath: "/approved/library"},
		Anchor:  modelTestIdentity(), RegisteredRoot: modelTestOtherIdentity(),
		Boundaries: []Boundary{{RelativePath: "nested/<a>", Identity: modelTestIdentity()},
			{RelativePath: "nested/z", Identity: modelTestOtherIdentity()}}}
}

func TestStorageBindingModelIdentityContract(t *testing.T) {
	if IdentityVersion != 1 || IdentityProfile != "linux-fsuuid-filehandle-v1" || MaxHandleBytes != 128 ||
		TopologyVersion != 1 || MaxBoundaries != 256 || MaxPathBytes != 4096 || MaxFingerprintBytes != 2<<20 {
		t.Fatal("the extracted storage model changed its persisted profile or bounds")
	}
	identity := modelTestIdentity()
	if err := identity.Validate(); err != nil {
		t.Fatalf("the complete opaque identity was rejected: %v", err)
	}
	raw, err := json.Marshal(identity)
	if err != nil || string(raw) != modelIdentityGolden {
		t.Fatalf("identity JSON changed its field order, exact integer or handle encoding: %s, %v", raw, err)
	}
	var decoded Identity
	if err := json.Unmarshal([]byte(modelIdentityGolden), &decoded); err != nil || !decoded.Equal(identity) || !identity.Equal(decoded) {
		t.Fatalf("the fixed identity JSON no longer round trips: %+v, %v", decoded, err)
	}
	maximum := identity
	maximum.Handle = make([]byte, MaxHandleBytes)
	if err := maximum.Validate(); err != nil || !maximum.Equal(maximum) {
		t.Fatalf("the maximum opaque handle was rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(*Identity)
	}{
		{"unknown version", func(i *Identity) { i.Version++ }},
		{"unknown profile", func(i *Identity) { i.Profile = "future-profile" }},
		{"zero UUID", func(i *Identity) { i.FilesystemUUID = strings.Repeat("0", 32) }},
		{"nonhex UUID", func(i *Identity) { i.FilesystemUUID = "g" + i.FilesystemUUID[1:] }},
		{"empty handle", func(i *Identity) { i.Handle = nil }},
		{"oversized handle", func(i *Identity) { i.Handle = make([]byte, MaxHandleBytes+1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := modelTestIdentity()
			test.change(&invalid)
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidIdentity) || invalid.Equal(invalid) || invalid.Equal(identity) || identity.Equal(invalid) {
				t.Fatalf("an incomplete identity established equality: %+v, %v", invalid, err)
			}
		})
	}
	changed := modelTestIdentity()
	changed.Handle[len(changed.Handle)-1]++
	if changed.Validate() != nil || changed.Equal(identity) || identity.Equal(changed) {
		t.Fatal("a valid change to the opaque handle did not change identity equality")
	}
}

func TestStorageBindingModelSnapshotCanonicalJSONAndFingerprint(t *testing.T) {
	snapshot := modelTestSnapshot()
	raw, err := json.Marshal(snapshot)
	if err != nil || string(raw) != modelSnapshotGolden {
		t.Fatalf("snapshot JSON changed its complete wire shape or escaping: %s, %v", raw, err)
	}
	wantDigest := sha256.Sum256([]byte(modelSnapshotGolden))
	want := hex.EncodeToString(wantDigest[:])
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if digest, err := snapshot.Fingerprint(); err != nil || digest != want {
		t.Fatalf("fingerprint does not cover the fixed canonical JSON: %q, %v", digest, err)
	}
	reordered := snapshot.Clone()
	reordered.Boundaries[0], reordered.Boundaries[1] = reordered.Boundaries[1], reordered.Boundaries[0]
	if digest, err := reordered.Fingerprint(); err != nil || digest != want || reordered.Boundaries[0].RelativePath != "nested/z" {
		t.Fatalf("canonical ordering changed the digest or mutated the caller: %q, %v", digest, err)
	}
	var decoded Snapshot
	if err := json.Unmarshal([]byte(modelSnapshotGolden), &decoded); err != nil {
		t.Fatal(err)
	}
	if digest, err := decoded.Fingerprint(); err != nil || digest != want {
		t.Fatalf("decoded boundary identities changed the canonical digest: %q, %v", digest, err)
	}
	changed := snapshot.Clone()
	changed.Boundaries[1].Identity.Handle[2]++
	if digest, err := changed.Fingerprint(); err != nil || digest == want {
		t.Fatalf("the last nested identity did not participate in the fingerprint: %q, %v", digest, err)
	}
	emptyDigest := sha256.Sum256([]byte(modelSnapshotGoldenPrefix + `[]}`))
	for _, boundaries := range [][]Boundary{nil, {}} {
		empty := snapshot.Clone()
		empty.Boundaries = boundaries
		if digest, err := empty.Fingerprint(); err != nil || digest != hex.EncodeToString(emptyDigest[:]) {
			t.Fatalf("nil and empty boundaries did not use the same canonical array: %q, %v", digest, err)
		}
	}
}

func TestStorageBindingModelRejectsPartialAndOversizedSnapshots(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Snapshot)
	}{
		{"unknown version", func(s *Snapshot) { s.Version++ }},
		{"unknown anchor profile", func(s *Snapshot) { s.Anchor.Profile = "unknown" }},
		{"missing registered identity", func(s *Snapshot) { s.RegisteredRoot = Identity{} }},
		{"missing nested identity", func(s *Snapshot) { s.Boundaries[1].Identity = Identity{} }},
		{"unrelated registered path", func(s *Snapshot) { s.Mapping.RegisteredPath = "/approved-other/library" }},
		{"duplicate boundary", func(s *Snapshot) { s.Boundaries[1].RelativePath = s.Boundaries[0].RelativePath }},
		{"root as nested boundary", func(s *Snapshot) { s.Boundaries[1].RelativePath = "." }},
		{"oversized boundary path", func(s *Snapshot) { s.Boundaries[1].RelativePath = strings.Repeat("x", MaxPathBytes+1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := modelTestSnapshot()
			test.change(&snapshot)
			if err := snapshot.Validate(); !errors.Is(err, ErrInvalidTopology) {
				t.Fatalf("partial topology validation returned %v", err)
			}
			if digest, err := snapshot.Fingerprint(); !errors.Is(err, ErrInvalidTopology) || digest != "" {
				t.Fatalf("partial topology produced a fingerprint: %q, %v", digest, err)
			}
		})
	}
	maximum := modelTestSnapshot()
	maximum.Boundaries = make([]Boundary, MaxBoundaries)
	for index := range maximum.Boundaries {
		maximum.Boundaries[index] = Boundary{RelativePath: fmt.Sprintf("nested/%03d", index), Identity: modelTestIdentity()}
	}
	if err := maximum.Validate(); err != nil {
		t.Fatalf("the complete maximum boundary count was rejected: %v", err)
	}
	excessive := maximum.Clone()
	excessive.Boundaries = append(excessive.Boundaries, Boundary{RelativePath: "one-more", Identity: modelTestIdentity()})
	if err := excessive.Validate(); !errors.Is(err, ErrTopologyLimit) {
		t.Fatalf("an extra complete boundary bypassed the count limit: %v", err)
	}
	if digest, err := excessive.Fingerprint(); !errors.Is(err, ErrTopologyLimit) || digest != "" {
		t.Fatalf("an excessive boundary population produced a partial digest: %q, %v", digest, err)
	}
	// These canonical Linux path strings fit the individual byte bound. JSON
	// escaping expands the complete population beyond the fingerprint budget.
	for index := range maximum.Boundaries {
		maximum.Boundaries[index].RelativePath = fmt.Sprintf("%03d", index) + strings.Repeat("\n", MaxPathBytes-3)
		if !ValidRelativePath(maximum.Boundaries[index].RelativePath) {
			t.Fatal("the encoded-byte fixture exceeded an individual path bound")
		}
	}
	if err := maximum.Validate(); !errors.Is(err, ErrTopologyLimit) {
		t.Fatalf("encoded JSON expansion bypassed topology validation: %v", err)
	}
	if digest, err := maximum.Fingerprint(); !errors.Is(err, ErrTopologyLimit) || digest != "" {
		t.Fatalf("encoded JSON expansion produced an oversized fingerprint input: %q, %v", digest, err)
	}
}

func TestStorageBindingModelCloneOwnsEveryHandleAndBoundary(t *testing.T) {
	original := modelTestSnapshot()
	clone := original.Clone()
	clone.Anchor.Handle[0]++
	clone.RegisteredRoot.Handle[1]++
	clone.Boundaries[0].Identity.Handle[2]++
	clone.Boundaries[1].Identity.HandleType++
	clone.Boundaries[0].RelativePath = "caller-replaced-boundary"
	clone.Mapping.RegisteredPath = "/approved/caller-replaced-root"
	clone.Boundaries = append(clone.Boundaries, Boundary{RelativePath: "caller-added", Identity: modelTestIdentity()})
	raw, err := json.Marshal(original)
	if err != nil || string(raw) != modelSnapshotGolden {
		t.Fatalf("mutating a clone changed the retained model: %s, %v", raw, err)
	}
}

func TestStorageBindingModelPathMappingKeepsCanonicalContainment(t *testing.T) {
	for _, test := range []struct {
		mapping Mapping
		want    string
	}{
		{Mapping{ApprovedPath: "/", RegisteredPath: "/"}, "."},
		{Mapping{ApprovedPath: "/", RegisteredPath: "/library"}, "library"},
		{Mapping{ApprovedPath: "/approved", RegisteredPath: "/approved"}, "."},
		{Mapping{ApprovedPath: "/approved", RegisteredPath: "/approved/library/with space"}, "library/with space"},
	} {
		if !PathWithin(test.mapping.ApprovedPath, test.mapping.RegisteredPath) {
			t.Fatalf("a valid mapping lost canonical containment: %+v", test.mapping)
		}
		if relative, err := test.mapping.Relative(); err != nil || relative != test.want {
			t.Fatalf("canonical relative mapping = %q, %v; want %q", relative, err, test.want)
		}
	}
	for _, mapping := range []Mapping{
		{ApprovedPath: "/approved", RegisteredPath: "/approved-other/library"},
		{ApprovedPath: "/approved", RegisteredPath: "/approved/../outside"},
		{ApprovedPath: "relative", RegisteredPath: "relative/library"},
		{ApprovedPath: "/approved/", RegisteredPath: "/approved/library"},
	} {
		if relative, err := mapping.Relative(); !errors.Is(err, ErrInvalidTopology) || relative != "" {
			t.Fatalf("an invalid mapping returned a usable relative path: %+v, %q, %v", mapping, relative, err)
		}
	}
	if PathWithin("/approved", "/approved-other/library") {
		t.Fatal("a shared string prefix was accepted as a path boundary")
	}
	for _, invalid := range []string{"", "bad\x00path", string([]byte{0xff}), "../outside", "nested//child", strings.Repeat("x", MaxPathBytes+1)} {
		if ValidRelativePath(invalid) {
			t.Fatalf("a malformed relative path was accepted: %q", invalid)
		}
	}
	for _, invalid := range []string{"", "relative", "/bad\x00path", "/" + string([]byte{0xff}), "/approved/..", "/" + strings.Repeat("x", MaxPathBytes)} {
		if ValidAbsolutePath(invalid) {
			t.Fatalf("a malformed absolute path was accepted: %q", invalid)
		}
	}
	if !ValidAbsolutePath("/"+strings.Repeat("x", MaxPathBytes-1)) || !ValidRelativePath(strings.Repeat("x", MaxPathBytes)) || !ValidRelativePath(".") {
		t.Fatal("an exact path boundary or the root-relative dot was rejected")
	}
}
