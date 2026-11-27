//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

func observeBackdropLockAliases(ctx context.Context, directory *os.Root, path string) (_ []string, resultErr error) {
	held, err := directory.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, held)) }()
	var stat unix.Statx_t
	if err := unix.Statx(int(held.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &stat); err != nil || stat.Mask&unix.STATX_MNT_ID == 0 {
		return nil, ErrRootTopologyUnavailable
	}
	mounts, err := readRootTopologyMountInfo(ctx)
	if err != nil {
		return nil, err
	}
	return backdropLockMountAliases(mounts, path, stat.Mnt_id, stat.Dev_major, stat.Dev_minor)
}

// Translate a held directory into each visible-name candidate on the same
// Linux filesystem, without probing or walking unrelated registered roots.
// Hidden aliases on that filesystem may cause a conservative scan refusal.
// Independent mounts of a remote share need separate operator review.
func backdropLockMountAliases(mounts []rootMountInfo, directory string, mountID uint64, major, minor uint32) ([]string, error) {
	witness, err := transcodingMountPathFor(mounts, directory, mountID, major, minor)
	if err != nil {
		return nil, err
	}
	var aliases []string
	for _, mount := range mounts {
		if mount.Major != major || mount.Minor != minor || !validRootTopologyAbsolute(mount.Root) ||
			!transcodingMountPathContains(mount.Root, witness.physical) {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(witness.physical, strings.TrimRight(mount.Root, "/")), "/")
		alias := filepath.Join(mount.MountPoint, relative)
		if alias == directory || slices.Contains(aliases, alias) {
			continue
		}
		if !validRootTopologyAbsolute(alias) || len(aliases) == maxLocalBackdropLockAliases {
			return nil, ErrRootTopologyLimit
		}
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	return aliases, nil
}
