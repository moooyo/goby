//go:build linux

package library

import "testing"

func TestTranscodingTempPathMountCoordinatesDetectIndependentlyBoundMediaChild(t *testing.T) {
	// Mount facts model an independent bind mount. No mount is created by this
	// parser/coordinate test, and no directory is scanned or modified.
	raw := "10 10 8:1 / / rw - ext4 /dev/vda rw\n" +
		"20 10 8:1 /srv/media/cache /var/cache/custom rw - ext4 /dev/vda rw\n" +
		"30 10 8:1 /srv/private/cache /var/cache/dedicated rw - ext4 /dev/vda rw\n" +
		"40 10 9:1 / /var/cache/other-volume rw - ext4 /dev/vdb rw\n" +
		"50 10 0:4 net:[4026532415] /run/docker/netns/isolated rw - nsfs nsfs rw\n"
	entries, err := parseRootMountInfo([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := transcodingMountPathFor(entries, "/srv/media", 10, 8, 1)
	if err != nil || reserved.physical != "/srv/media" {
		t.Fatal("root filesystem coordinates were not retained")
	}
	selected, err := transcodingMountPathFor(entries, "/var/cache/custom", 20, 8, 1)
	if err != nil || selected.physical != "/srv/media/cache" || !transcodingMountPathsOverlap(selected, reserved) {
		t.Fatal("independently bind-mounted media child bypassed dedicated cache scope")
	}
	future, err := transcodingMountPathFor(entries, "/var/cache/custom/new-cache", 20, 8, 1)
	if err != nil || future.physical != "/srv/media/cache/new-cache" || !transcodingMountPathsOverlap(future, reserved) {
		t.Fatal("a future cache component hid its bind-mounted reserved ancestor")
	}
	dedicated, err := transcodingMountPathFor(entries, "/var/cache/dedicated/new-cache", 30, 8, 1)
	if err != nil || dedicated.physical != "/srv/private/cache/new-cache" || transcodingMountPathsOverlap(dedicated, reserved) {
		t.Fatal("dedicated directory on the same filesystem was incorrectly rejected")
	}
	otherVolume, err := transcodingMountPathFor(entries, "/var/cache/other-volume/srv/media", 40, 9, 1)
	if err != nil || transcodingMountPathsOverlap(otherVolume, reserved) {
		t.Fatal("equal relative directory names on separate filesystems overlapped")
	}
}

func TestTranscodingTempPathMountMappingUsesObservedIDAndRejectsUnknownCoordinates(t *testing.T) {
	raw := "10 10 8:1 / / rw - ext4 /dev/vda rw\n" +
		"20 10 8:1 /srv/media/cache /var/cache/custom rw - ext4 /dev/vda rw\n" +
		"21 10 8:1 /srv/private/cache /var/cache/custom rw - ext4 /dev/vda rw\n" +
		"30 10 8:1 /srv/private/future /var/cache/unknown rw future:opaque - ext4 /dev/vda rw\n"
	entries, err := parseRootMountInfo([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := transcodingMountPathFor(entries, "/var/cache/custom", 21, 8, 1)
	if err != nil || selected.physical != "/srv/private/cache" {
		t.Fatal("overmounted path was resolved by path text instead of its observed mount identity")
	}
	for _, test := range []struct {
		path         string
		mountID      uint64
		major, minor uint32
	}{
		{path: "/var/cache/custom", mountID: 999, major: 8, minor: 1},
		{path: "/var/cache/custom", mountID: 20, major: 8, minor: 2},
		{path: "/outside/custom", mountID: 20, major: 8, minor: 1},
		{path: "/var/cache/unknown", mountID: 30, major: 8, minor: 1},
	} {
		if _, err := transcodingMountPathFor(entries, test.path, test.mountID, test.major, test.minor); err == nil {
			t.Fatal("unavailable or ambiguous mount coordinates were admitted")
		}
	}
}

func TestTranscodingTempPathMountScopesDetectCacheExposedInsideMediaTree(t *testing.T) {
	baseRaw := "10 10 8:1 / / rw - ext4 /dev/vda rw\n"
	entries, err := parseRootMountInfo([]byte(baseRaw))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := transcodingMountPathFor(entries, "/srv/private/cache", 10, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := transcodingMountPathFor(entries, "/srv/media", 10, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	before, err := transcodingMountScopeFor(entries, "/srv/media", reserved)
	if err != nil || transcodingMountScopesOverlap([]transcodingMountPath{selected}, before) {
		t.Fatal("independent base directories overlapped without an alias mount")
	}
	withAlias, err := parseRootMountInfo([]byte(baseRaw + "20 10 8:1 /srv/private/cache /srv/media/cache-alias rw - ext4 /dev/vda rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := transcodingMountScopeFor(withAlias, "/srv/media", reserved)
	if err != nil || len(after) != 2 || after[1].physical != "/srv/private/cache" || !transcodingMountScopesOverlap([]transcodingMountPath{selected}, after) {
		t.Fatal("a nested media mount exposing the private cache bypassed directory isolation")
	}
	if len(before) == len(after) {
		t.Fatal("related scope did not retain the inserted nested mount record")
	}
	reverse, err := parseRootMountInfo([]byte(baseRaw + "21 10 8:1 /srv/media /srv/private/cache/media-alias rw - ext4 /dev/vda rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	selectedScope, err := transcodingMountScopeFor(reverse, "/srv/private/cache", selected)
	if err != nil || !transcodingMountScopesOverlap(selectedScope, []transcodingMountPath{reserved}) {
		t.Fatal("a nested cache mount exposing media bypassed directory isolation")
	}
}
