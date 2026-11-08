//go:build !linux

package library

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestDirectoryCapacityUnsupportedPlatformStaysUnknown(t *testing.T) {
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	for _, file := range []*os.File{nil, directory} {
		got, err := observeDirectoryCapacity(context.Background(), file)
		if got != nil || err != nil {
			t.Fatalf("unsupported platform invented filesystem capacity: got=%+v err=%v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, file := range []*os.File{nil, directory} {
		got, err := observeDirectoryCapacity(ctx, file)
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("unsupported platform lost caller cancellation: got=%+v err=%v", got, err)
		}
	}
}
