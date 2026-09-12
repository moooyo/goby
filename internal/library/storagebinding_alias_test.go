package library

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/storagebinding"
)

func TestStorageBindingAliasesPreserveLibraryAPI(t *testing.T) {
	// Assignments in both directions require aliases, not parallel defined
	// types that merely happen to expose matching fields.
	var sharedIdentity storagebinding.Identity = rootStorageIdentityFixture()
	var legacyIdentity RootStorageIdentity = sharedIdentity
	var sharedMapping storagebinding.Mapping = RootTopologyMapping{ApprovedPath: "/approved", RegisteredPath: "/approved/library"}
	var legacyMapping RootTopologyMapping = sharedMapping
	var sharedBoundary storagebinding.Boundary = RootTopologyBoundary{RelativePath: "nested", Identity: legacyIdentity}
	var legacyBoundary RootTopologyBoundary = sharedBoundary
	var sharedSnapshot storagebinding.Snapshot = RootTopologySnapshot{Version: RootTopologyVersion,
		Mapping: legacyMapping, Anchor: legacyIdentity, RegisteredRoot: sharedIdentity, Boundaries: []RootTopologyBoundary{legacyBoundary}}
	var legacySnapshot RootTopologySnapshot = sharedSnapshot
	if RootStorageIdentityVersion != storagebinding.IdentityVersion || RootStorageIdentityProfile != storagebinding.IdentityProfile ||
		MaxRootStorageHandleBytes != storagebinding.MaxHandleBytes || RootTopologyVersion != storagebinding.TopologyVersion ||
		MaxRootTopologyBoundaries != storagebinding.MaxBoundaries || maxRootTopologyPathBytes != storagebinding.MaxPathBytes ||
		maxRootTopologyFingerprintBytes != storagebinding.MaxFingerprintBytes {
		t.Fatal("library compatibility constants diverged from the shared model")
	}
	for _, pair := range []struct {
		legacy error
		shared error
	}{
		{ErrRootStorageIdentityUnsupported, storagebinding.ErrIdentityUnsupported},
		{ErrRootStorageIdentityUnavailable, storagebinding.ErrIdentityUnavailable},
		{ErrInvalidRootStorageIdentity, storagebinding.ErrInvalidIdentity},
		{ErrInvalidRootTopology, storagebinding.ErrInvalidTopology},
		{ErrRootTopologyLimit, storagebinding.ErrTopologyLimit},
		{ErrRootTopologyAmbiguous, storagebinding.ErrTopologyAmbiguous},
		{ErrRootTopologyChanged, storagebinding.ErrTopologyChanged},
		{ErrRootTopologyUnavailable, storagebinding.ErrTopologyUnavailable},
	} {
		if pair.legacy != pair.shared {
			t.Fatalf("library compatibility errors lost sentinel identity: %v, %v", pair.legacy, pair.shared)
		}
	}
	if legacyIdentity.Validate() != nil || !legacyIdentity.Equal(sharedIdentity) || legacySnapshot.Validate() != nil {
		t.Fatal("the original identity and snapshot methods no longer accept shared values")
	}
	want, err := sharedSnapshot.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := legacySnapshot.Fingerprint(); err != nil || got != want {
		t.Fatalf("the legacy snapshot changed the shared fingerprint: %q, %v", got, err)
	}
	legacyJSON, legacyErr := json.Marshal(legacySnapshot)
	sharedJSON, sharedErr := json.Marshal(sharedSnapshot)
	if legacyErr != nil || sharedErr != nil || string(legacyJSON) != string(sharedJSON) {
		t.Fatal("the library aliases changed the shared JSON representation")
	}
	if relative, err := rootTopologyRelative(legacyMapping); err != nil || relative != "library" {
		t.Fatalf("the original relative mapping helper changed: %q, %v", relative, err)
	}
	if !validRootTopologyAbsolute(legacyMapping.RegisteredPath) || !validRootTopologyRelative("nested") ||
		!rootTopologyWithin(legacyMapping.ApprovedPath, legacyMapping.RegisteredPath) {
		t.Fatal("the original path helpers no longer delegate the shared contract")
	}
	clone := cloneRootTopologySnapshot(legacySnapshot)
	clone.Anchor.Handle[0]++
	clone.Boundaries[0].Identity.Handle[0]++
	if got, err := legacySnapshot.Fingerprint(); err != nil || got != want {
		t.Fatal("the original clone helper returned shared mutable model storage")
	}
	capture := &RootTopologyCapture{snapshot: legacySnapshot, live: RootTopologyLiveWitness{
		Namespace: RootMountNamespaceWitness{Device: 11, Inode: 22}, RegisteredRoot: RootStorageWitness{MountID: 33},
	}}
	if got, err := capture.Fingerprint(); err != nil || got != want {
		t.Fatalf("the existing live capture no longer accepts the shared snapshot: %q, %v", got, err)
	}
}
