//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestDirectoryWriteAccessUsesHeldDirectoryWithoutCreatingFiles(t *testing.T) {
	path := t.TempDir()
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	writable, err := observeDirectoryWriteAccess(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if writable == nil {
		t.Skip("effective descriptor access is unavailable on this kernel")
	}
	if !*writable {
		t.Fatal("owned temporary directory was not writable")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("permission observation changed the directory: entries=%v err=%v", entries, err)
	}
}

func TestDirectoryWriteAccessCancellationAndInvalidDescriptorStayDistinct(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := observeDirectoryWriteAccess(ctx, nil); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was reported as directory permissions: result=%v err=%v", result, err)
	}
	if result, err := observeDirectoryWriteAccess(context.Background(), nil); result != nil || err != nil {
		t.Fatalf("missing descriptor invented permissions: result=%v err=%v", result, err)
	}
}
