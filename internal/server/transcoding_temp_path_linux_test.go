//go:build linux

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/config"
)

func TestStartupTranscodingTempPathRejectsReservedFilesystemAlias(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "media-alias")
	if err := os.Symlink(private, alias); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{MediaRoots: []string{alias}}}
	if err := server.validateStartupTranscodingTempPath(filepath.Join(private, "transcodes")); err == nil {
		t.Fatal("transcoding cache overlapped a media root through its filesystem alias")
	}
	if err := server.validateStartupTranscodingTempPath(filepath.Join(root, "dedicated-transcodes")); err != nil {
		t.Fatalf("dedicated cache failed read-only startup scope validation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dedicated-transcodes")); !os.IsNotExist(err) {
		t.Fatal("startup scope validation created the requested cache directory")
	}
}

func TestTranscodingExistingRootAliasUsesDirectoryIdentityAndFutureSuffix(t *testing.T) {
	private := filepath.Join(t.TempDir(), "empty-cache")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(private)
	if err != nil {
		t.Fatal(err)
	}
	names, err := os.ReadDir(private)
	if err != nil || len(names) != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("identity fixture must satisfy the empty private-directory cache guards: entries=%d directory=%t mode=%#o read_error=%v", len(names), info.IsDir(), info.Mode().Perm(), err)
	}
	// These distinct path facts model an existing bind-mount alias using a real
	// directory identity. This predicate test does not create a mount or claim
	// that either synthetic path is present on the verification machine.
	selected := transcodingExistingRoot{path: "/private/selected-cache", info: info}
	reserved := transcodingExistingRoot{path: "/private/media-root", info: info}
	if err := config.ValidateTranscodingTempPath(selected.path, []string{reserved.path}); err != nil {
		t.Fatal("fixture paths must pass the earlier lexical scope guard")
	}
	if overlap, err := transcodingExistingRootsOverlap(selected, reserved); err != nil || !overlap {
		t.Fatalf("different path text hid the same private directory identity: %t, %v", overlap, err)
	}
	selected.suffix = "future-cache"
	if overlap, err := transcodingExistingRootsOverlap(selected, reserved); err != nil || !overlap {
		t.Fatal("a future cache suffix bypassed an existing reserved ancestor")
	}
	reserved.suffix = "future-media"
	if overlap, err := transcodingExistingRootsOverlap(selected, reserved); err != nil || overlap {
		t.Fatal("different future sibling directories were incorrectly treated as overlapping")
	}
	reserved.suffix = filepath.Join("future-cache", "nested-store")
	if overlap, err := transcodingExistingRootsOverlap(selected, reserved); err != nil || !overlap {
		t.Fatal("a future reserved suffix bypassed the containing selected cache")
	}
}
