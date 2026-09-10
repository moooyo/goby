//go:build linux

package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const generationOne = "11111111111111111111111111111111"
const generationTwo = "22222222222222222222222222222222"

func lifecycleDirectory(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state")
}
func openLifecycle(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
func stageLifecycle(t *testing.T, store *Store, id string, master []byte) Generation {
	t.Helper()
	generation, err := store.StageGeneration(context.Background(), id, []byte(`{"configurationVersion":1}`), master)
	if err != nil {
		t.Fatal(err)
	}
	return generation
}
func planLifecycle(t *testing.T, store *Store, id string, master MasterSource) Plan {
	t.Helper()
	state, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.Plan(context.Background(), state, Candidate{GenerationID: id, DatabaseSlot: DatabaseRecovery, Master: master})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestLifecyclePublicationSurvivesReopenAndPreservesSourceFiles(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	initial, err := store.Current()
	if err != nil || initial.Revision != 0 {
		t.Fatalf("initial state: %v", err)
	}
	master := bytes.Repeat([]byte{0x5d}, 32)
	generation := stageLifecycle(t, store, generationOne, master)
	if !generation.Complete || generation.Master == nil {
		t.Fatal("generation not complete")
	}
	loaded, err := store.ReadGeneration(ctx, generationOne)
	if err != nil || !bytes.Equal(loaded.Master, master) {
		t.Fatal("registered master could not be read")
	}
	loaded.Config[0] = 'x'
	loaded.Master[0] = 0
	loaded, err = store.ReadGeneration(ctx, generationOne)
	if err != nil || !bytes.Equal(loaded.Master, master) || loaded.Config[0] != '{' {
		t.Fatal("read exposed mutable backing state")
	}
	path, err := store.MasterKeyPath(ctx, generationOne)
	if err != nil || path != filepath.Join(directory, generationName(generationOne), masterName) {
		t.Fatal("unexpected master path")
	}
	plan := planLifecycle(t, store, generationOne, MasterGeneration)
	if err := store.Finish(ctx, plan.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("finished a prepared plan")
	}
	if _, err := store.Plan(ctx, initial, Candidate{GenerationID: generationOne, DatabaseSlot: DatabasePrimary, Master: MasterGeneration}); !errors.Is(err, ErrConflict) {
		t.Fatal("second plan was admitted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openLifecycle(t, directory)
	pending, err := store.Pending(ctx)
	if err != nil || pending == nil || pending.Status != PlanPrepared || pending.ID != plan.ID {
		t.Fatal("prepared plan was not retained")
	}
	current, err := store.Current()
	if err != nil || current != initial {
		t.Fatal("reopen activated a pending intention")
	}
	activated, err := store.Activate(ctx, plan.ID)
	if err != nil || activated != plan.After {
		t.Fatalf("activation failed: %v", err)
	}
	if again, err := store.Activate(ctx, plan.ID); err != nil || again != activated {
		t.Fatal("activation retry was not idempotent")
	}
	if err := store.Abort(ctx, plan.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("abort rolled back an activated generation")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openLifecycle(t, directory)
	pending, err = store.Pending(ctx)
	if err != nil || pending.Status != PlanActivated {
		t.Fatal("published plan was not recovered")
	}
	if err := store.Finish(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openLifecycle(t, directory)
	current, err = store.Current()
	if err != nil || current != activated {
		t.Fatal("finished manifest changed")
	}
	if pending, err := store.Pending(ctx); err != nil || pending != nil {
		t.Fatal("finished journal remained")
	}
	if _, err := store.Plan(ctx, initial, Candidate{GenerationID: generationOne, DatabaseSlot: DatabaseRecovery, Master: MasterGeneration}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale CAS was accepted")
	}
	if _, err := store.StageGeneration(ctx, generationOne, []byte(`{"configurationVersion":1}`), master); err != nil {
		t.Fatal("equal-content stage retry failed")
	}
	if _, err := store.StageGeneration(ctx, generationOne, []byte(`{"configurationVersion":2}`), master); !errors.Is(err, ErrConflict) {
		t.Fatal("stage overwrote immutable config")
	}
	for _, name := range []string{markerName, registryName, activeName, lockName} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("metadata permissions: %s", name)
		}
	}
}

func TestLifecycleAbortRetainsGenerationAndNeverSelectsMissingMaster(t *testing.T) {
	ctx := context.Background()
	store := openLifecycle(t, lifecycleDirectory(t))
	stageLifecycle(t, store, generationOne, nil)
	if _, err := store.MasterKeyPath(ctx, generationOne); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing master path was exposed")
	}
	state, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Plan(ctx, state, Candidate{GenerationID: generationOne, DatabaseSlot: DatabaseRecovery, Master: MasterGeneration}); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing generation master selected")
	}
	plan := planLifecycle(t, store, generationOne, MasterDefault)
	if err := store.Abort(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if current, err := store.Current(); err != nil || current != state {
		t.Fatal("abort changed the active generation")
	}
	if _, err := store.ReadGeneration(ctx, generationOne); err != nil {
		t.Fatal("abort removed staged generation")
	}
}

func TestLifecycleSequentialGenerationsUseDeploymentBoundCAS(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	other := openLifecycle(t, lifecycleDirectory(t))
	foreign, err := other.Current()
	if err != nil {
		t.Fatal(err)
	}
	stageLifecycle(t, store, generationOne, nil)
	if _, err := store.Plan(ctx, foreign, Candidate{GenerationID: generationOne, DatabaseSlot: DatabaseRecovery, Master: MasterDefault}); !errors.Is(err, ErrConflict) {
		t.Fatal("initial CAS crossed deployment ownership")
	}
	first := planLifecycle(t, store, generationOne, MasterDefault)
	if _, err := store.Activate(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	stageLifecycle(t, store, generationTwo, nil)
	second := planLifecycle(t, store, generationTwo, MasterDefault)
	if second.Before != first.After || second.After.Revision != 2 || second.After.DeploymentID != first.After.DeploymentID {
		t.Fatal("second plan did not preserve the exact previous generation")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openLifecycle(t, directory)
	if current, err := store.Current(); err != nil || current != first.After {
		t.Fatal("prepared second generation changed the selected database")
	}
	if _, err := store.Activate(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{generationOne, generationTwo} {
		if _, err := store.ReadGeneration(ctx, id); err != nil {
			t.Fatal("publication removed historical generation")
		}
	}
}

func TestLifecycleConcurrentPlansHaveOneDurableWinner(t *testing.T) {
	ctx := context.Background()
	store := openLifecycle(t, lifecycleDirectory(t))
	stageLifecycle(t, store, generationOne, nil)
	current, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.Plan(ctx, current, Candidate{GenerationID: generationOne, DatabaseSlot: DatabaseRecovery, Master: MasterDefault})
			results <- err
		}()
	}
	group.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatalf("unexpected planning failure: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("durable plan winners: %d", winners)
	}
	pending, err := store.Pending(ctx)
	if err != nil || pending == nil || pending.Before != current || pending.Status != PlanPrepared {
		t.Fatal("concurrent planning did not leave one coherent journal")
	}
}

func TestLifecycleProcessLockHelper(t *testing.T) {
	if os.Getenv("GOBY_LIFECYCLE_LOCK_HELPER") != "1" {
		return
	}
	store, err := Open(context.Background(), os.Getenv("GOBY_LIFECYCLE_LOCK_DIRECTORY"))
	if store != nil {
		store.Close()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("other process was not fenced: %v", err)
	}
}

func TestLifecycleLocksFenceProcessesAndSurviveNamedLockReplacement(t *testing.T) {
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	if second, err := Open(context.Background(), directory); !errors.Is(err, ErrBusy) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("same-process second open: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			if err := os.Rename(filepath.Join(directory, lockName), filepath.Join(directory, "old-lock")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, lockName), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Current(); err == nil {
				t.Fatal("named lock replacement was accepted")
			}
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, executable, "-test.run=^TestLifecycleProcessLockHelper$")
		command.Env = append(os.Environ(), "GOBY_LIFECYCLE_LOCK_HELPER=1", "GOBY_LIFECYCLE_LOCK_DIRECTORY="+directory)
		output, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("subprocess lock check: %v\n%s", err, output)
		}
	}
}

func TestLifecycleRejectsUnownedPathsAndFilesystemAliases(t *testing.T) {
	ctx := context.Background()
	t.Run("arbitrary existing directory", func(t *testing.T) {
		directory := lifecycleDirectory(t)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(directory, "unrelated")
		if err := os.WriteFile(filename, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if store, err := Open(ctx, directory); err == nil {
			store.Close()
			t.Fatal("claimed arbitrary files")
		}
		names, err := os.ReadDir(directory)
		if err != nil || len(names) != 1 || names[0].Name() != "unrelated" {
			t.Fatal("unowned directory was mutated")
		}
	})
	t.Run("symlink ancestor", func(t *testing.T) {
		parent := t.TempDir()
		actual := filepath.Join(parent, "actual")
		if err := os.Mkdir(actual, 0700); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(parent, "alias")
		if err := os.Symlink(actual, alias); err != nil {
			t.Fatal(err)
		}
		if store, err := Open(ctx, filepath.Join(alias, "state")); err == nil {
			store.Close()
			t.Fatal("followed ancestor symlink")
		}
	})
	t.Run("readonly root rejected without chmod", func(t *testing.T) {
		directory := lifecycleDirectory(t)
		if err := os.Mkdir(directory, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
		if store, err := Open(ctx, directory); err == nil {
			store.Close()
			t.Fatal("claimed wrong-mode root")
		}
		info, err := os.Stat(directory)
		if err != nil || info.Mode().Perm() != 0500 {
			t.Fatal("root permissions were changed")
		}
	})
	for _, kind := range []string{"symlink", "hardlink", "replacement", "wrong mode"} {
		t.Run(kind, func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, nil)
			filename := filepath.Join(directory, generationName(generationOne), configName)
			switch kind {
			case "hardlink":
				if err := os.Link(filename, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "wrong mode":
				if err := os.Chmod(filename, 0644); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				replacement := filepath.Join(t.TempDir(), "replacement")
				if err := os.WriteFile(replacement, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, filename); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(filename); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/dev/null", filename); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.ReadGeneration(ctx, generationOne); err == nil {
				t.Fatal("accepted modified registered file")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if reopened, err := Open(ctx, directory); err == nil {
				reopened.Close()
				t.Fatal("reopen accepted modified registered file")
			}
		})
	}
}

func TestLifecycleRejectsDirectoryReplacementAndUnsafeGenerationIDs(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	for _, id := range []string{"../other", "/tmp/other", "..", strings.Repeat("A", 32), generationOne + "/x", ""} {
		if _, err := store.StageGeneration(ctx, id, []byte("{}"), nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted id %q", id)
		}
		if _, err := store.ReadGeneration(ctx, id); !errors.Is(err, ErrInvalid) {
			t.Fatalf("read id %q", id)
		}
	}
	if err := os.Rename(directory, directory+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Current(); err == nil {
		t.Fatal("continued after root replacement")
	}
	names, err := os.ReadDir(directory)
	if err != nil || len(names) != 0 {
		t.Fatal("replacement root was mutated")
	}
}

func TestLifecycleCancelledGateWaitAndConcurrentReaders(t *testing.T) {
	ctx := context.Background()
	store := openLifecycle(t, lifecycleDirectory(t))
	stageLifecycle(t, store, generationOne, nil)
	<-store.gate
	waitContext, cancel := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { _, err := store.ReadGeneration(waitContext, generationOne); finished <- err }()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled gate wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gate wait did not observe cancellation")
	}
	store.leave()
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 10 {
				if _, err := store.ReadGeneration(ctx, generationOne); err != nil {
					t.Errorf("concurrent read: %v", err)
					return
				}
			}
		}()
	}
	group.Wait()
	preCancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.StageGeneration(preCancelled, generationTwo, []byte("{}"), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled stage started")
	}
	entries, err := store.Generations(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatal("cancelled operation created a generation")
	}
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := store.Close(); err != nil {
				t.Errorf("concurrent close: %v", err)
			}
		}()
	}
	group.Wait()
}
