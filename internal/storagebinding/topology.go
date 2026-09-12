package storagebinding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	TopologyVersion     = 1
	MaxBoundaries       = 256
	MaxPathBytes        = 4096
	MaxFingerprintBytes = 2 << 20
)

var (
	ErrInvalidTopology     = errors.New("root mount topology is invalid")
	ErrTopologyLimit       = errors.New("root mount topology exceeds its observation limit")
	ErrTopologyAmbiguous   = errors.New("root mount topology has an unverifiable overlapping or hidden mount")
	ErrTopologyChanged     = errors.New("root mount topology changed during observation")
	ErrTopologyUnavailable = errors.New("root mount topology is unavailable")
)

// Mapping describes an approved anchor and registered root. Validating its
// shape does not approve a path supplied by an unauthenticated caller.
type Mapping struct {
	ApprovedPath   string `json:"approved_path"`
	RegisteredPath string `json:"registered_path"`
}

type Boundary struct {
	RelativePath string   `json:"relative_path"`
	Identity     Identity `json:"identity"`
}

// Snapshot contains only stable directory identities and paths. A producer
// must observe every nested boundary; structural validation cannot establish
// that its population matches a live filesystem.
type Snapshot struct {
	Version        int        `json:"version"`
	Mapping        Mapping    `json:"mapping"`
	Anchor         Identity   `json:"anchor"`
	RegisteredRoot Identity   `json:"registered_root"`
	Boundaries     []Boundary `json:"boundaries"`
}

// Validate applies the same complete structural and byte bounds as Fingerprint.
// It never opens a path or establishes live storage continuity.
func (snapshot Snapshot) Validate() error {
	_, err := snapshot.canonicalBytes()
	return err
}

// Fingerprint is a canonical digest of the approved mapping and every stable
// identity. It deliberately excludes mount IDs, namespace IDs, device/inode
// witnesses, mount source names and display options. This is not a proof of a
// system reboot or evidence that any previously approved binding still matches.
func (snapshot Snapshot) Fingerprint() (string, error) {
	raw, err := snapshot.canonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (snapshot Snapshot) canonicalBytes() ([]byte, error) {
	if snapshot.Version != TopologyVersion || snapshot.Anchor.Validate() != nil ||
		snapshot.RegisteredRoot.Validate() != nil {
		return nil, ErrInvalidTopology
	}
	if _, err := snapshot.Mapping.Relative(); err != nil {
		return nil, err
	}
	if len(snapshot.Boundaries) > MaxBoundaries {
		return nil, ErrTopologyLimit
	}
	type boundary struct {
		RelativePath string   `json:"relative_path"`
		Identity     Identity `json:"identity"`
	}
	boundaries := make([]boundary, 0, len(snapshot.Boundaries))
	seen := make(map[string]bool, len(snapshot.Boundaries))
	for _, entry := range snapshot.Boundaries {
		if !ValidRelativePath(entry.RelativePath) || entry.RelativePath == "." || seen[entry.RelativePath] || entry.Identity.Validate() != nil {
			return nil, ErrInvalidTopology
		}
		seen[entry.RelativePath] = true
		boundaries = append(boundaries, boundary{entry.RelativePath, entry.Identity})
	}
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].RelativePath < boundaries[j].RelativePath })
	value := struct {
		Version        int        `json:"version"`
		Mapping        Mapping    `json:"mapping"`
		Anchor         Identity   `json:"anchor"`
		RegisteredRoot Identity   `json:"registered_root"`
		Boundaries     []boundary `json:"boundaries"`
	}{snapshot.Version, snapshot.Mapping, snapshot.Anchor, snapshot.RegisteredRoot, boundaries}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxFingerprintBytes {
		return nil, ErrTopologyLimit
	}
	return raw, nil
}

// Relative validates the complete mapping and returns its canonical root path
// relative to the anchor. Equal anchor and root paths use the literal dot.
func (mapping Mapping) Relative() (string, error) {
	if !ValidAbsolutePath(mapping.ApprovedPath) || !ValidAbsolutePath(mapping.RegisteredPath) ||
		!PathWithin(mapping.ApprovedPath, mapping.RegisteredPath) {
		return "", ErrInvalidTopology
	}
	if mapping.ApprovedPath == mapping.RegisteredPath {
		return ".", nil
	}
	return strings.TrimPrefix(mapping.RegisteredPath, strings.TrimSuffix(mapping.ApprovedPath, "/")+"/"), nil
}

// ValidAbsolutePath applies the Linux persisted-path syntax and byte bound.
// It does not consult the caller's host filesystem or resolve symbolic links.
func ValidAbsolutePath(value string) bool {
	return len(value) > 0 && len(value) <= MaxPathBytes && utf8.ValidString(value) &&
		!strings.ContainsRune(value, 0) && path.IsAbs(value) && path.Clean(value) == value
}

// ValidRelativePath accepts a canonical path inside an already approved root.
// A nested Boundary additionally excludes the dot representing that root.
func ValidRelativePath(value string) bool {
	return len(value) > 0 && len(value) <= MaxPathBytes && utf8.ValidString(value) &&
		!strings.ContainsRune(value, 0) && !path.IsAbs(value) && path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../")
}

// PathWithin compares paths already validated with ValidAbsolutePath.
func PathWithin(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}

// Clone gives the caller independent handle bytes and boundary slice storage.
func (snapshot Snapshot) Clone() Snapshot {
	snapshot.Anchor.Handle = append([]byte(nil), snapshot.Anchor.Handle...)
	snapshot.RegisteredRoot.Handle = append([]byte(nil), snapshot.RegisteredRoot.Handle...)
	snapshot.Boundaries = append([]Boundary{}, snapshot.Boundaries...)
	for index := range snapshot.Boundaries {
		snapshot.Boundaries[index].Identity.Handle = append([]byte(nil), snapshot.Boundaries[index].Identity.Handle...)
	}
	return snapshot
}
