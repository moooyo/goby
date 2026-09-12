package library

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/storagebinding"
)

const (
	RootTopologyVersion             = storagebinding.TopologyVersion
	MaxRootMountInfoBytes           = 4 << 20
	MaxRootMountInfoEntries         = 8192
	MaxRootMountInfoLineBytes       = 16384
	MaxRootTopologyBoundaries       = storagebinding.MaxBoundaries
	maxRootTopologyPathBytes        = storagebinding.MaxPathBytes
	maxRootTopologyComponents       = 128
	maxRootTopologyFingerprintBytes = storagebinding.MaxFingerprintBytes
)

var (
	ErrInvalidRootTopology     = storagebinding.ErrInvalidTopology
	ErrRootTopologyLimit       = storagebinding.ErrTopologyLimit
	ErrRootTopologyAmbiguous   = storagebinding.ErrTopologyAmbiguous
	ErrRootTopologyChanged     = storagebinding.ErrTopologyChanged
	ErrRootTopologyUnavailable = storagebinding.ErrTopologyUnavailable
)

// RootTopologyMapping is copied from the caller's registered library root. It
// does not change a lease or approve a path supplied by an unauthenticated user.
type RootTopologyMapping = storagebinding.Mapping

type RootMountNamespaceWitness struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type RootTopologyBoundary = storagebinding.Boundary

// RootTopologySnapshot contains only stable directory identities and paths.
// All nested boundaries must be observed; partial snapshots fail.
type RootTopologySnapshot = storagebinding.Snapshot

type RootTopologyLiveBoundary struct {
	RelativePath string             `json:"relative_path"`
	Witness      RootStorageWitness `json:"witness"`
}

// RootTopologyLiveWitness belongs to one capture in one mount namespace. It is
// deliberately separate from the persistent snapshot and its fingerprint.
type RootTopologyLiveWitness struct {
	Namespace      RootMountNamespaceWitness  `json:"namespace"`
	Anchor         RootStorageWitness         `json:"anchor"`
	RegisteredRoot RootStorageWitness         `json:"registered_root"`
	Boundaries     []RootTopologyLiveBoundary `json:"boundaries"`
}

type rootTopologyHeldPoint struct {
	file        *os.File
	observation RootStorageObservation
}

// RootTopologyCapture owns independent directory and namespace descriptors.
// Callers retain ownership of their original lease and registered directory.
// Snapshot and Fingerprint describe an observation, never deletion permission;
// Revalidate must precede a later use of the held witnesses.
type RootTopologyCapture struct {
	mu        sync.Mutex
	closed    bool
	snapshot  RootTopologySnapshot
	live      RootTopologyLiveWitness
	held      []rootTopologyHeldPoint
	namespace *os.File
	records   []rootMountInfo
}

// CaptureTopology is called outside Store.mu, before an owned transaction. The
// explicit mapping avoids changing the existing lease or consulting Store.mu.
// heldRoot must be the registered directory used by the caller's scan, so a
// later same-name replacement cannot become the subject of an older scan.
func (lease *libraryRootLease) CaptureTopology(ctx context.Context, mapping RootTopologyMapping, heldRoot *os.Root) (*RootTopologyCapture, error) {
	if ctx == nil || lease == nil || lease.approved == nil || heldRoot == nil {
		return nil, ErrInvalidRootTopology
	}
	relative, err := rootTopologyRelative(mapping)
	if err != nil || (lease.relativePath != relative && !(relative == "." && lease.relativePath == "")) {
		return nil, ErrInvalidRootTopology
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mapping.ApprovedPath, mapping.RegisteredPath = strings.Clone(mapping.ApprovedPath), strings.Clone(mapping.RegisteredPath)
	return captureRootTopologyPlatform(ctx, lease, mapping, heldRoot)
}

func (capture *RootTopologyCapture) Snapshot() (RootTopologySnapshot, error) {
	if capture == nil {
		return RootTopologySnapshot{}, ErrInvalidRootTopology
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.snapshot.Version != RootTopologyVersion {
		return RootTopologySnapshot{}, ErrInvalidRootTopology
	}
	return cloneRootTopologySnapshot(capture.snapshot), nil
}

func (capture *RootTopologyCapture) Fingerprint() (string, error) {
	snapshot, err := capture.Snapshot()
	if err != nil {
		return "", err
	}
	return snapshot.Fingerprint()
}

func (capture *RootTopologyCapture) LiveWitness() (RootTopologyLiveWitness, error) {
	if capture == nil {
		return RootTopologyLiveWitness{}, ErrInvalidRootTopology
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.snapshot.Version != RootTopologyVersion {
		return RootTopologyLiveWitness{}, ErrInvalidRootTopology
	}
	value := capture.live
	value.Boundaries = append([]RootTopologyLiveBoundary{}, value.Boundaries...)
	return value, nil
}

// Revalidate performs fresh named opens, observes all live descriptors again,
// and checks scoped mountinfo and namespace stability without acquiring Store.mu.
// The context is checked between bounded operations; it cannot interrupt an
// individual kernel filesystem operation that has already started.
func (capture *RootTopologyCapture) Revalidate(ctx context.Context) error {
	if capture == nil || ctx == nil {
		return ErrInvalidRootTopology
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.closed || capture.snapshot.Version != RootTopologyVersion {
		return ErrRootTopologyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return revalidateRootTopologyPlatform(ctx, capture)
}

func (capture *RootTopologyCapture) Close() error {
	if capture == nil {
		return nil
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.closed {
		return nil
	}
	capture.closed = true
	var result error
	for _, point := range capture.held {
		result = errors.Join(result, point.file.Close())
	}
	capture.held = nil
	if capture.namespace != nil {
		result = errors.Join(result, capture.namespace.Close())
		capture.namespace = nil
	}
	return result
}

func rootTopologyRelative(mapping RootTopologyMapping) (string, error) {
	return mapping.Relative()
}

func validRootTopologyAbsolute(value string) bool {
	return storagebinding.ValidAbsolutePath(value)
}

func validRootTopologyRelative(value string) bool {
	return storagebinding.ValidRelativePath(value)
}

func rootTopologyWithin(parent, child string) bool {
	return storagebinding.PathWithin(parent, child)
}

func cloneRootTopologySnapshot(snapshot RootTopologySnapshot) RootTopologySnapshot {
	return snapshot.Clone()
}
