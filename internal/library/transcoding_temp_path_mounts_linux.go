//go:build linux

package library

import (
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

type transcodingMountPath struct {
	major, minor uint32
	physical     string
	record       rootMountInfo
}

// CheckTranscodingTempPathMountIsolation checks only the proposed cache and
// the supplied deployment roots. Paths are already symlink-resolved by the
// caller. Mount-root coordinates recognize a media subdirectory independently
// bind-mounted at an unrelated path without walking the media tree.
func CheckTranscodingTempPathMountIsolation(ctx context.Context, selected string, reserved []string) error {
	if ctx == nil {
		return ErrInvalidRootTopology
	}
	entries, err := readRootTopologyMountInfo(ctx)
	if err != nil {
		return err
	}
	selectedPaths, err := observeTranscodingMountScope(entries, selected)
	if err != nil {
		return err
	}
	used := map[string][]transcodingMountPath{selected: selectedPaths}
	for _, directory := range reserved {
		if err := ctx.Err(); err != nil {
			return err
		}
		other, err := observeTranscodingMountScope(entries, directory)
		if err != nil {
			return err
		}
		used[directory] = other
		if transcodingMountScopesOverlap(selectedPaths, other) {
			return fmt.Errorf("transcoding temporary path overlaps another deployment directory through mount topology: %w", ErrInvalidRootTopology)
		}
	}
	after, err := readRootTopologyMountInfo(ctx)
	if err != nil {
		return err
	}
	for directory, before := range used {
		current, err := observeTranscodingMountScope(after, directory)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, current) {
			return ErrRootTopologyChanged
		}
	}
	return nil
}

func observeTranscodingMountScope(entries []rootMountInfo, directory string) ([]transcodingMountPath, error) {
	base, err := observeTranscodingMountPath(entries, directory)
	if err != nil {
		return nil, err
	}
	return transcodingMountScopeFor(entries, directory, base)
}

// A nested mount can expose the cache inside an otherwise separate media tree,
// or expose media inside the selected cache. Inspect only bounded namespace
// records; no directory inventory or recursive media traversal is needed.
func transcodingMountScopeFor(entries []rootMountInfo, directory string, base transcodingMountPath) ([]transcodingMountPath, error) {
	result := []transcodingMountPath{base}
	for _, entry := range entries {
		if entry.ID == base.record.ID || !transcodingMountPathContains(directory, entry.MountPoint) {
			continue
		}
		point, err := transcodingMountPathFor(entries, entry.MountPoint, uint64(entry.ID), entry.Major, entry.Minor)
		if err != nil {
			return nil, err
		}
		result = append(result, point)
	}
	return result, nil
}

func observeTranscodingMountPath(entries []rootMountInfo, directory string) (transcodingMountPath, error) {
	if !validRootTopologyAbsolute(directory) {
		return transcodingMountPath{}, ErrInvalidRootTopology
	}
	existing := directory
	for {
		var stat unix.Statx_t
		err := unix.Statx(unix.AT_FDCWD, existing, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_TYPE|unix.STATX_MNT_ID, &stat)
		if err == nil {
			if stat.Mask&(unix.STATX_TYPE|unix.STATX_MNT_ID) != unix.STATX_TYPE|unix.STATX_MNT_ID || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
				return transcodingMountPath{}, ErrRootTopologyUnavailable
			}
			return transcodingMountPathFor(entries, directory, stat.Mnt_id, stat.Dev_major, stat.Dev_minor)
		}
		if !os.IsNotExist(err) {
			return transcodingMountPath{}, ErrRootTopologyUnavailable
		}
		parent := path.Dir(existing)
		if parent == existing {
			return transcodingMountPath{}, ErrRootTopologyUnavailable
		}
		existing = parent
	}
}

func transcodingMountPathFor(entries []rootMountInfo, directory string, mountID uint64, major, minor uint32) (transcodingMountPath, error) {
	for _, entry := range entries {
		if uint64(entry.ID) != mountID {
			continue
		}
		if entry.Major != major || entry.Minor != minor || !validRootTopologyAbsolute(entry.Root) ||
			!transcodingMountPathContains(entry.MountPoint, directory) {
			return transcodingMountPath{}, ErrRootTopologyUnavailable
		}
		known, err := validateRootMountOptional(entry.OptionalFields)
		if err != nil || !known {
			return transcodingMountPath{}, ErrRootTopologyAmbiguous
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(directory, strings.TrimRight(entry.MountPoint, "/")), "/")
		return transcodingMountPath{major: major, minor: minor, physical: path.Join(entry.Root, relative), record: entry}, nil
	}
	return transcodingMountPath{}, ErrRootTopologyUnavailable
}

func transcodingMountPathContains(parent, child string) bool {
	return parent == "/" || parent == child || strings.HasPrefix(child, strings.TrimRight(parent, "/")+"/")
}

func transcodingMountPathsOverlap(first, second transcodingMountPath) bool {
	return first.major == second.major && first.minor == second.minor &&
		(transcodingMountPathContains(first.physical, second.physical) || transcodingMountPathContains(second.physical, first.physical))
}

func transcodingMountScopesOverlap(first, second []transcodingMountPath) bool {
	for _, left := range first {
		for _, right := range second {
			if transcodingMountPathsOverlap(left, right) {
				return true
			}
		}
	}
	return false
}
