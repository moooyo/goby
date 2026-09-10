//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLifecycleInterruptedManifestPublicationRetainsDeterministicOutcome(t *testing.T) {
	ctx := context.Background()
	for _, point := range []string{"before rename", "after rename"} {
		t.Run(point, func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, nil)
			plan := planLifecycle(t, store, generationOne, MasterDefault)
			if point == "before rename" {
				store.syncFile = func(*os.File) error { return errors.New("injected file fsync failure") }
			} else {
				store.syncDirectory = func(*os.File) error { return errors.New("injected directory fsync failure") }
			}
			_, err := store.Activate(ctx, plan.ID)
			if err == nil {
				t.Fatal("uncertain publication reported success")
			}
			if point == "after rename" && !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("uncertain publication error: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, journalName)); err != nil {
				t.Fatal("publication removed its recovery journal")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openLifecycle(t, directory)
			pending, err := store.Pending(ctx)
			if err != nil || pending == nil {
				t.Fatalf("recovery did not retain a plan: %v", err)
			}
			wanted := PlanPrepared
			if point == "after rename" {
				wanted = PlanActivated
			}
			if pending.Status != wanted {
				t.Fatalf("pending status %q, want %q", pending.Status, wanted)
			}
			if point == "before rename" {
				if current, err := store.Current(); err != nil || current != plan.Before {
					t.Fatal("failed pre-rename write altered current")
				}
				if _, err := store.Activate(ctx, plan.ID); err != nil {
					t.Fatal(err)
				}
			} else if err := store.Abort(ctx, plan.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("uncertain publication was rolled back")
			}
			if err := store.Finish(ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLifecycleInterruptedFinishSupportsBothDurableOrderings(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []int{1, 2} {
		t.Run(map[int]string{1: "registry committed", 2: "journal unlinked"}[failure], func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, nil)
			plan := planLifecycle(t, store, generationOne, MasterDefault)
			if _, err := store.Activate(ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
			calls := 0
			store.syncDirectory = func(file *os.File) error {
				calls++
				if calls == failure {
					return errors.New("injected finish fsync failure")
				}
				return file.Sync()
			}
			if err := store.Finish(ctx, plan.ID); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("finish reported wrong outcome: %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openLifecycle(t, directory)
			if current, err := store.Current(); err != nil || current != plan.After {
				t.Fatal("interrupted finish lost published state")
			}
			pending, err := store.Pending(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if failure == 1 {
				if pending == nil || pending.Status != PlanActivated {
					t.Fatal("durable registry with retained journal not recognized")
				}
				if err := store.Finish(ctx, plan.ID); err != nil {
					t.Fatal(err)
				}
			} else if pending != nil {
				t.Fatal("unlinked journal was recreated")
			}
		})
	}
}

func TestLifecycleInterruptedGenerationIsRetainedAndCannotActivate(t *testing.T) {
	ctx := context.Background()
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	store.syncFile = func(file *os.File) error {
		if file.Name() == configName {
			return errors.New("injected generation file fsync failure")
		}
		return file.Sync()
	}
	if _, err := store.StageGeneration(ctx, generationOne, []byte(`{"schema":1}`), nil); err == nil {
		t.Fatal("partial generation reported success")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openLifecycle(t, directory)
	entries, err := store.Generations(ctx)
	if err != nil || len(entries) != 1 || entries[0].Complete {
		t.Fatalf("incomplete stage missing: %v", err)
	}
	if _, err := store.ReadGeneration(ctx, generationOne); !errors.Is(err, ErrIncomplete) {
		t.Fatal("partial config was exposed")
	}
	if _, err := store.StageGeneration(ctx, generationOne, []byte(`{"schema":1}`), nil); !errors.Is(err, ErrIncomplete) {
		t.Fatal("partial generation was adopted")
	}
	state, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Plan(ctx, state, Candidate{GenerationID: generationOne, DatabaseSlot: DatabaseRecovery, Master: MasterDefault}); !errors.Is(err, ErrIncomplete) {
		t.Fatal("partial generation was activated")
	}
	stageLifecycle(t, store, generationTwo, nil)
	if _, err := os.Stat(filepath.Join(directory, generationName(generationOne), configName)); err != nil {
		t.Fatal("new stage removed the incomplete generation")
	}
}

func TestLifecycleMissingOrReplacedActiveManifestNeverFallsBackToPrimary(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"missing", "corrupt", "same bytes replacement"} {
		t.Run(change, func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, nil)
			plan := planLifecycle(t, store, generationOne, MasterDefault)
			if _, err := store.Activate(ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.Finish(ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(directory, activeName)
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filename, filepath.Join(directory, "saved-active")); err != nil {
				t.Fatal(err)
			}
			if change != "missing" {
				if change == "corrupt" {
					data = []byte("{}\n")
				}
				if err := os.WriteFile(filename, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.Current(); err == nil {
				t.Fatal("live process accepted active manifest replacement")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			// The retained unrelated pathname itself must also prevent adoption.
			if reopened, err := Open(ctx, directory); err == nil {
				reopened.Close()
				t.Fatal("reopen accepted ambiguous state")
			}
			if err := os.Remove(filepath.Join(directory, "saved-active")); err != nil {
				t.Fatal(err)
			}
			if change != "same bytes replacement" {
				if reopened, err := Open(ctx, directory); err == nil {
					reopened.Close()
					t.Fatal("reopen fell back to primary after losing active manifest")
				}
			}
		})
	}
}

func TestLifecycleInitializationAndUnregisteredStageRecoveryRemainFailClosed(t *testing.T) {
	ctx := context.Background()
	t.Run("marker publication before registry", func(t *testing.T) {
		directory := lifecycleDirectory(t)
		store := openLifecycle(t, directory)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(directory, registryName)); err != nil {
			t.Fatal(err)
		}
		store = openLifecycle(t, directory)
		if state, err := store.Current(); err != nil || state.Revision != 0 {
			t.Fatal("empty owned initialization could not recover")
		}
	})
	t.Run("unregistered generation directory", func(t *testing.T) {
		directory := lifecycleDirectory(t)
		store := openLifecycle(t, directory)
		store.registry.Generations = append(store.registry.Generations, registeredGeneration{ID: generationOne, Config: registeredFile{FileDescriptor: FileDescriptor{Name: configName, Size: 2, SHA256: digest([]byte("{}"))}}})
		if err := store.persistRegistry(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(directory, generationName(generationOne)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if reopened, err := Open(ctx, directory); !errors.Is(err, ErrRecoveryRequired) {
			if reopened != nil {
				reopened.Close()
			}
			t.Fatalf("unregistered directory was adopted: %v", err)
		}
		if _, err := os.Stat(filepath.Join(directory, generationName(generationOne))); err != nil {
			t.Fatal("unregistered directory was removed")
		}
	})
	t.Run("unpublished initial claim debris", func(t *testing.T) {
		directory := lifecycleDirectory(t)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, lockName), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if reopened, err := Open(ctx, directory); !errors.Is(err, ErrRecoveryRequired) {
			if reopened != nil {
				reopened.Close()
			}
			t.Fatalf("unknown lock was claimed: %v", err)
		}
	})
}
