//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLifecycleReadCurrentPreservesPublicationStates(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	initial, err := store.ReadCurrent(ctx)
	if err != nil || initial.State.Revision != 0 || initial.State.DatabaseSlot != DatabasePrimary || initial.State.Master != MasterDefault || initial.Files.Generation.ID != "" || len(initial.Files.Config) != 0 || len(initial.Files.Master) != 0 || initial.MasterKeyPath != "" {
		t.Fatalf("invalid initial snapshot: %v", err)
	}
	master := bytes.Repeat([]byte{0x47}, 32)
	stageLifecycle(t, store, generationOne, master)
	plan := planLifecycle(t, store, generationOne, MasterDefault)
	prepared, err := store.ReadCurrent(ctx)
	if err != nil || prepared.State != initial.State || prepared.Files.Generation.ID != "" || prepared.MasterKeyPath != "" {
		t.Fatalf("prepared plan changed the active snapshot: %v", err)
	}
	if _, err := store.Activate(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	expectedConfig := []byte(`{"configurationVersion":1}`)
	activated, err := store.ReadCurrent(ctx)
	if err != nil || activated.State != plan.After || activated.Files.Generation.ID != generationOne || !bytes.Equal(activated.Files.Config, expectedConfig) || !bytes.Equal(activated.Files.Master, master) || activated.MasterKeyPath != "" {
		t.Fatalf("activated snapshot selected an incorrect generation or master: %v", err)
	}
	activated.Files.Config[0] = 'x'
	clear(activated.Files.Master)
	if err := store.Finish(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := store.ReadCurrent(ctx)
	if err != nil || finished.State != plan.After || !bytes.Equal(finished.Files.Config, expectedConfig) || !bytes.Equal(finished.Files.Master, master) || finished.MasterKeyPath != "" {
		t.Fatalf("finished snapshot lost publication or reused caller bytes: %v", err)
	}
	clear(finished.Files.Master)
}

func TestLifecycleReadCurrentRejectsDamagedHistoryAndProof(t *testing.T) {
	for _, target := range []string{"historical config", "historical master", "current config", "current master", "active manifest", "journal", "replaced root"} {
		t.Run(target, func(t *testing.T) {
			ctx := context.Background()
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, bytes.Repeat([]byte{0x11}, 32))
			stageLifecycle(t, store, generationTwo, bytes.Repeat([]byte{0x22}, 32))
			plan := planLifecycle(t, store, generationTwo, MasterGeneration)
			if _, err := store.Activate(ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
			if target == "replaced root" {
				if err := os.Rename(directory, directory+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				paths := map[string]string{
					"historical config": filepath.Join(directory, generationName(generationOne), configName),
					"historical master": filepath.Join(directory, generationName(generationOne), masterName),
					"current config":    filepath.Join(directory, generationName(generationTwo), configName),
					"current master":    filepath.Join(directory, generationName(generationTwo), masterName),
					"active manifest":   filepath.Join(directory, activeName),
					"journal":           filepath.Join(directory, journalName),
				}
				if err := os.WriteFile(paths[target], []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if snapshot, err := store.ReadCurrent(ctx); err == nil || snapshot.State != (State{}) || snapshot.Files.Config != nil || snapshot.Files.Master != nil || snapshot.MasterKeyPath != "" {
				clear(snapshot.Files.Master)
				t.Fatal("damaged lifecycle returned a usable snapshot")
			}
		})
	}
}

func TestLifecycleReadCurrentCancellationWhileWaiting(t *testing.T) {
	store := openLifecycle(t, lifecycleDirectory(t))
	<-store.gate
	defer store.leave()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := store.ReadCurrent(ctx)
		finished <- err
	}()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("snapshot gate wait ignored cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot gate wait did not finish")
	}
}

func TestLifecycleReadCurrentRemainsCoherentDuringActivation(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	configs := map[string][]byte{generationOne: []byte("first config"), generationTwo: []byte("second config")}
	masters := map[string][]byte{generationOne: bytes.Repeat([]byte{0x11}, 32), generationTwo: bytes.Repeat([]byte{0x22}, 32)}
	for _, id := range []string{generationOne, generationTwo} {
		if _, err := store.StageGeneration(ctx, id, configs[id], masters[id]); err != nil {
			t.Fatal(err)
		}
	}
	first := planLifecycle(t, store, generationOne, MasterGeneration)
	if _, err := store.Activate(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	second := planLifecycle(t, store, generationTwo, MasterGeneration)
	var group sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			snapshot, err := store.ReadCurrent(ctx)
			if err != nil {
				t.Errorf("read during activation: %v", err)
				return
			}
			defer clear(snapshot.Files.Master)
			id := snapshot.State.GenerationID
			if snapshot.State != first.After && snapshot.State != second.After || snapshot.Files.Generation.ID != id || !bytes.Equal(snapshot.Files.Config, configs[id]) || !bytes.Equal(snapshot.Files.Master, masters[id]) || snapshot.MasterKeyPath != filepath.Join(directory, generationName(id), masterName) {
				t.Error("snapshot mixed state, files, or master paths across activation")
			}
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		<-start
		if _, err := store.Activate(ctx, second.ID); err != nil {
			t.Errorf("concurrent activation: %v", err)
		}
	}()
	close(start)
	group.Wait()
}
