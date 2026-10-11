//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLifecycleGenerationSnapshotKeepsRetainedImageAndMasterPath(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	master := bytes.Repeat([]byte{0x37}, 32)
	stageLifecycle(t, store, generationOne, master)
	stageLifecycle(t, store, generationTwo, nil)
	plan := planLifecycle(t, store, generationTwo, MasterDefault)
	if _, err := store.Activate(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		snapshot, err := store.ReadGenerationSnapshot(ctx, generationOne)
		if err != nil || snapshot.Files.Generation.ID != generationOne ||
			!bytes.Equal(snapshot.Files.Config, []byte(`{"configurationVersion":1}`)) || !bytes.Equal(snapshot.Files.Master, master) ||
			snapshot.MasterKeyPath != filepath.Join(directory, generationName(generationOne), masterName) {
			t.Fatalf("requested retained generation was replaced by the active image: %v", err)
		}
		clear(snapshot.Files.Config)
		clear(snapshot.Files.Master)
	}
	snapshot, err := store.ReadGenerationSnapshot(ctx, generationTwo)
	if err != nil || snapshot.Files.Generation.ID != generationTwo || len(snapshot.Files.Master) != 0 || snapshot.MasterKeyPath != "" {
		t.Fatalf("generation without a master returned a fabricated path: %v", err)
	}
}

func TestLifecycleGenerationSnapshotChecksAllRetainedHistory(t *testing.T) {
	for _, target := range []string{"requested_config", "sibling_config", "sibling_master", "sibling_permissions"} {
		t.Run(target, func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, bytes.Repeat([]byte{0x11}, 32))
			stageLifecycle(t, store, generationTwo, bytes.Repeat([]byte{0x22}, 32))
			id, name := generationTwo, configName
			if target == "requested_config" {
				id = generationOne
			} else if target == "sibling_master" || target == "sibling_permissions" {
				name = masterName
			}
			path := filepath.Join(directory, generationName(id), name)
			var err error
			if target == "sibling_permissions" {
				err = os.Chmod(path, 0644)
			} else {
				err = os.WriteFile(path, []byte("damaged retained history"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := store.ReadGenerationSnapshot(context.Background(), generationOne)
			if err == nil || snapshot.Files.Config != nil || snapshot.Files.Master != nil || snapshot.MasterKeyPath != "" {
				clear(snapshot.Files.Master)
				t.Fatal("damaged or unowned retained history returned a usable generation observation")
			}
		})
	}
}

func TestLifecycleGenerationSnapshotRetainsIDAndCancellationGuards(t *testing.T) {
	store := openLifecycle(t, lifecycleDirectory(t))
	if _, err := store.ReadGenerationSnapshot(context.Background(), "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid generation changed its error: %v", err)
	}
	if _, err := store.ReadGenerationSnapshot(context.Background(), generationOne); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent generation changed its error: %v", err)
	}
	<-store.gate
	defer store.leave()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ReadGenerationSnapshot(ctx, generationOne); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled generation observation waited for ownership: %v", err)
	}
}
