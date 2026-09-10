//go:build linux

package recoverycontrol

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

	"golang.org/x/sys/unix"
)

const deploymentOne = "11111111111111111111111111111111"
const deploymentTwo = "22222222222222222222222222222222"

func controlDirectory(t *testing.T) string { t.Helper(); return filepath.Join(t.TempDir(), "control") }
func openControl(t *testing.T, directory string) *Store {
	t.Helper()
	store, err := Open(context.Background(), directory, deploymentOne)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
func readControl(t *testing.T, store *Store) Snapshot {
	t.Helper()
	snapshot, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func writeControl(t *testing.T, store *Store, expected string, payload []byte) Snapshot {
	t.Helper()
	snapshot, err := store.CompareAndSwap(context.Background(), expected, payload)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestControlCASPersistsDeploymentBoundRevisionAndIndependentPayload(t *testing.T) {
	ctx := context.Background()
	directory := controlDirectory(t)
	store := openControl(t, directory)
	initial := readControl(t, store)
	if initial.Revision != 0 || len(initial.Payload) != 0 || !validHex(initial.Digest, 64) {
		t.Fatal("invalid initial snapshot")
	}
	input := []byte(` { "phase": "prepared", "number": 9007199254740993 } `)
	first := writeControl(t, store, initial.Digest, input)
	input[3] = 'x'
	first.Payload[0] = 'x'
	first = readControl(t, store)
	if first.Revision != 1 || first.Digest == initial.Digest || string(first.Payload) != `{"phase":"prepared","number":9007199254740993}` {
		t.Fatal("published payload lost precision or retained caller storage")
	}
	if _, err := store.CompareAndSwap(ctx, initial.Digest, []byte(`{"phase":"wrong"}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS did not conflict: %v", err)
	}
	second := writeControl(t, store, first.Digest, first.Payload)
	if second.Revision != 2 || second.Digest == first.Digest {
		t.Fatal("equal-payload CAS did not advance revision")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openControl(t, directory)
	reopened := readControl(t, store)
	if reopened.Revision != second.Revision || reopened.Digest != second.Digest || !bytes.Equal(reopened.Payload, second.Payload) {
		t.Fatal("reopen changed the published record")
	}
	if store.proof.Before == nil || store.proof.Before.Digest != first.Digest || store.proof.Candidate.Digest != second.Digest {
		t.Fatal("latest bounded publication proof did not preserve its predecessor")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if foreign, err := Open(ctx, directory, deploymentTwo); !errors.Is(err, ErrConflict) {
		if foreign != nil {
			foreign.Close()
		}
		t.Fatalf("foreign deployment accepted: %v", err)
	}
	other := openControl(t, controlDirectory(t))
	foreignInitial := readControl(t, other)
	if foreignInitial.Digest == initial.Digest {
		t.Fatal("independent stores shared a genesis CAS token")
	}
	if _, err := other.CompareAndSwap(ctx, initial.Digest, []byte("{}")); !errors.Is(err, ErrConflict) {
		t.Fatal("CAS token crossed store ownership")
	}
}

func TestControlConcurrentCASHasOneWinnerAndCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openControl(t, controlDirectory(t))
	initial := readControl(t, store)
	var group sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.CompareAndSwap(ctx, initial.Digest, []byte(`{"phase":"prepared"}`))
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
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if winners != 1 || readControl(t, store).Revision != 1 {
		t.Fatalf("CAS winners: %d", winners)
	}
	for range 6 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := store.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
		}()
	}
	group.Wait()
	if _, err := store.Read(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed store remained readable")
	}
}

func TestControlCancellationBeforeAndDuringGateWaitDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	store := openControl(t, controlDirectory(t))
	initial := readControl(t, store)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.CompareAndSwap(cancelled, initial.Digest, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled CAS: %v", err)
	}
	<-store.gate
	waiting, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := store.Read(waiting); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled gate wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		store.leave()
		t.Fatal("gate wait ignored cancellation")
	}
	store.leave()
	if current := readControl(t, store); current.Digest != initial.Digest {
		t.Fatal("cancelled operation wrote state")
	}
}

func TestControlProcessLockHelper(t *testing.T) {
	if os.Getenv("GOBY_RECOVERY_CONTROL_LOCK_HELPER") != "1" {
		return
	}
	store, err := Open(context.Background(), os.Getenv("GOBY_RECOVERY_CONTROL_DIRECTORY"), deploymentOne)
	if store != nil {
		store.Close()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("helper was not fenced: %v", err)
	}
}

func TestControlFencesAnotherProcessEvenAfterMutexReplacement(t *testing.T) {
	directory := controlDirectory(t)
	store := openControl(t, directory)
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			if err := os.Rename(filepath.Join(directory, lockName), filepath.Join(directory, "old-lock")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, lockName), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(context.Background()); err == nil {
				t.Fatal("named mutex replacement was accepted")
			}
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, executable, "-test.run=^TestControlProcessLockHelper$")
		command.Env = append(os.Environ(), "GOBY_RECOVERY_CONTROL_LOCK_HELPER=1", "GOBY_RECOVERY_CONTROL_DIRECTORY="+directory)
		output, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("process fencing: %v\n%s", err, output)
		}
	}
}

func TestControlRejectsUnownedDirectoryAndUnsafePathsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"relative", "/", "/tmp/../state", "/tmp/state/", "/tmp/state\x00"} {
		if store, err := Open(ctx, path, deploymentOne); !errors.Is(err, ErrInvalid) {
			if store != nil {
				store.Close()
			}
			t.Fatalf("accepted path %q", path)
		}
	}
	for _, id := range []string{"", strings.Repeat("A", 32), "../deployment", strings.Repeat("1", 31)} {
		if store, err := Open(ctx, controlDirectory(t), id); !errors.Is(err, ErrInvalid) {
			if store != nil {
				store.Close()
			}
			t.Fatal("invalid deployment ID accepted")
		}
	}
	t.Run("nonempty directory", func(t *testing.T) {
		directory := controlDirectory(t)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "foreign"), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if store, err := Open(ctx, directory, deploymentOne); !errors.Is(err, ErrRecoveryRequired) {
			if store != nil {
				store.Close()
			}
			t.Fatalf("unowned directory: %v", err)
		}
		names, err := os.ReadDir(directory)
		if err != nil || len(names) != 1 || names[0].Name() != "foreign" {
			t.Fatal("unowned directory was mutated")
		}
	})
	t.Run("ancestor symlink", func(t *testing.T) {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(parent, "alias")
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
		if store, err := Open(ctx, filepath.Join(alias, "control"), deploymentOne); err == nil {
			store.Close()
			t.Fatal("followed an ancestor symlink")
		}
	})
	t.Run("readonly directory", func(t *testing.T) {
		directory := controlDirectory(t)
		if err := os.Mkdir(directory, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
		if store, err := Open(ctx, directory, deploymentOne); err == nil {
			store.Close()
			t.Fatal("claimed wrong-mode directory")
		}
		info, err := os.Stat(directory)
		if err != nil || info.Mode().Perm() != 0500 {
			t.Fatal("directory mode was changed")
		}
	})
}

func TestControlRejectsHardlinksSymlinksModesAndInodeReplacement(t *testing.T) {
	ctx := context.Background()
	for _, target := range []string{markerName, currentName, proofName, lockName} {
		for _, change := range []string{"hardlink", "symlink", "wrong mode", "new inode"} {
			t.Run(target+"/"+change, func(t *testing.T) {
				directory := controlDirectory(t)
				store := openControl(t, directory)
				filename := filepath.Join(directory, target)
				switch change {
				case "hardlink":
					if err := os.Link(filename, filepath.Join(t.TempDir(), "hardlink")); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Remove(filename); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("/dev/null", filename); err != nil {
						t.Fatal(err)
					}
				case "wrong mode":
					if err := os.Chmod(filename, 0644); err != nil {
						t.Fatal(err)
					}
				case "new inode":
					data, err := os.ReadFile(filename)
					if err != nil {
						t.Fatal(err)
					}
					replacement := filepath.Join(t.TempDir(), "new-inode")
					if err := os.WriteFile(replacement, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(replacement, filename); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := store.Read(ctx); err == nil {
					t.Fatal("live store accepted modified fixed file")
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				if change != "new inode" || target == currentName || target == lockName {
					if reopened, err := Open(ctx, directory, deploymentOne); err == nil {
						reopened.Close()
						t.Fatal("reopen accepted modified registered file")
					}
				}
			})
		}
	}
}

func TestControlRejectsRootReplacementMissingStateAndUnexpectedSpecialFiles(t *testing.T) {
	ctx := context.Background()
	t.Run("root replacement", func(t *testing.T) {
		directory := controlDirectory(t)
		store := openControl(t, directory)
		if err := os.Rename(directory, directory+"-old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Read(ctx); err == nil {
			t.Fatal("root replacement was accepted")
		}
		names, err := os.ReadDir(directory)
		if err != nil || len(names) != 0 {
			t.Fatal("replacement root was mutated")
		}
	})
	for _, missing := range []string{currentName, proofName, markerName} {
		t.Run("missing "+missing, func(t *testing.T) {
			directory := controlDirectory(t)
			store := openControl(t, directory)
			initial := readControl(t, store)
			writeControl(t, store, initial.Digest, []byte("{}"))
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(directory, missing)); err != nil {
				t.Fatal(err)
			}
			if reopened, err := Open(ctx, directory, deploymentOne); err == nil {
				reopened.Close()
				t.Fatal("missing state was silently reinitialized")
			}
		})
	}
	t.Run("fifo temporary", func(t *testing.T) {
		directory := controlDirectory(t)
		store := openControl(t, directory)
		filename := filepath.Join(directory, ".next-"+strings.Repeat("a", 32))
		if err := unix.Mkfifo(filename, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Read(ctx); err == nil {
			t.Fatal("special temporary file was accepted")
		}
	})
}
