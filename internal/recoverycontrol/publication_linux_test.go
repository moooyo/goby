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

func TestControlFailedWritesHaveOneInspectableBeforeOrCandidate(t *testing.T) {
	ctx := context.Background()
	for _, point := range []string{"candidate file sync", "proof file sync", "proof rename", "proof directory sync", "current rename", "current directory sync"} {
		t.Run(point, func(t *testing.T) {
			directory := controlDirectory(t)
			store := openControl(t, directory)
			initial := readControl(t, store)
			fileSyncs, directorySyncs := 0, 0
			store.syncFile = func(file *os.File) error {
				fileSyncs++
				if point == "candidate file sync" && fileSyncs == 1 || point == "proof file sync" && fileSyncs == 2 {
					return unix.EIO
				}
				return file.Sync()
			}
			store.syncDirectory = func(file *os.File) error {
				directorySyncs++
				if point == "proof directory sync" && directorySyncs == 1 || point == "current directory sync" && directorySyncs == 2 {
					return unix.EIO
				}
				return file.Sync()
			}
			store.rename = func(oldFD int, old string, newFD int, name string, flags uint) error {
				if point == "proof rename" && name == proofName || point == "current rename" && name == currentName {
					return unix.EIO
				}
				return unix.Renameat2(oldFD, old, newFD, name, flags)
			}
			_, err := store.CompareAndSwap(ctx, initial.Digest, []byte(`{"phase":"published"}`))
			if err == nil {
				t.Fatal("failed publication reported success")
			}
			uncertain := point == "proof directory sync" || point == "current directory sync"
			if uncertain {
				if !errors.Is(err, ErrRecoveryRequired) {
					t.Fatalf("uncertain outcome error: %v", err)
				}
				if _, err := store.Read(ctx); !errors.Is(err, ErrRecoveryRequired) {
					t.Fatal("uncertain store remained available")
				}
			} else if current := readControl(t, store); current.Digest != initial.Digest {
				t.Fatal("failed pre-publication operation changed current")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openControl(t, directory)
			actual := readControl(t, store)
			if point == "current directory sync" {
				if actual.Revision != 1 || string(actual.Payload) != `{"phase":"published"}` {
					t.Fatal("visible candidate was lost after a failed parent sync")
				}
				if _, err := store.CompareAndSwap(ctx, initial.Digest, []byte(`{"phase":"blind retry"}`)); !errors.Is(err, ErrConflict) {
					t.Fatal("blind retry overwrote a visible candidate")
				}
			} else if actual.Digest != initial.Digest || actual.Revision != 0 {
				t.Fatal("reopen applied a candidate that was never renamed")
			}
			next := writeControl(t, store, actual.Digest, []byte(`{"phase":"next"}`))
			if next.Revision != actual.Revision+1 || store.proof.Before.Digest != actual.Digest {
				t.Fatal("subsequent CAS used an uncommitted candidate as its before-state")
			}
		})
	}
}

func TestControlCancellationAroundPublicationUsesActualVisibility(t *testing.T) {
	for _, point := range []string{"after proof", "after current rename"} {
		t.Run(point, func(t *testing.T) {
			store := openControl(t, controlDirectory(t))
			initial := readControl(t, store)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			store.syncDirectory = func(file *os.File) error {
				calls++
				if err := file.Sync(); err != nil {
					return err
				}
				if point == "after proof" && calls == 1 || point == "after current rename" && calls == 2 {
					cancel()
				}
				return nil
			}
			result, err := store.CompareAndSwap(ctx, initial.Digest, []byte(`{"phase":"new"}`))
			if point == "after proof" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("pre-rename cancellation: %v", err)
				}
				if actual := readControl(t, store); actual.Digest != initial.Digest {
					t.Fatal("cancelled pre-rename operation published data")
				}
			} else {
				if err != nil || result.Revision != 1 {
					t.Fatalf("durably published late cancellation lost outcome: %v", err)
				}
				if actual := readControl(t, store); actual.Digest != result.Digest {
					t.Fatal("late cancellation rolled back publication")
				}
			}
		})
	}
}

func TestControlCloseWaitsUntilAnInFlightPublicationHasFinished(t *testing.T) {
	store := openControl(t, controlDirectory(t))
	initial := readControl(t, store)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	store.syncFile = func(file *os.File) error { once.Do(func() { close(entered); <-release }); return file.Sync() }
	written := make(chan error, 1)
	closed := make(chan error, 1)
	go func() {
		_, err := store.CompareAndSwap(context.Background(), initial.Digest, []byte("{}"))
		written <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not enter its filesystem call")
	}
	go func() { closed <- store.Close() }()
	select {
	case <-closed:
		close(release)
		t.Fatal("Close released process locks during publication")
	default:
	}
	close(release)
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write did not finish")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish")
	}
}

func TestControlAbruptProcessExitHelper(t *testing.T) {
	point := os.Getenv("GOBY_RECOVERY_CONTROL_CRASH_POINT")
	if point == "" {
		return
	}
	store, err := Open(context.Background(), os.Getenv("GOBY_RECOVERY_CONTROL_DIRECTORY"), deploymentOne)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	store.syncDirectory = func(file *os.File) error {
		calls++
		if point == "after proof" && calls == 1 {
			if err := file.Sync(); err != nil {
				return err
			}
			os.Exit(29)
		}
		if point == "after current rename" && calls == 2 {
			os.Exit(29)
		}
		return file.Sync()
	}
	if _, err := store.CompareAndSwap(context.Background(), current.Digest, []byte(`{"phase":"crashed"}`)); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash point was not reached")
}

func TestControlReopensAfterActualProcessExitWithoutReplayingCandidates(t *testing.T) {
	for _, point := range []string{"after proof", "after current rename"} {
		t.Run(point, func(t *testing.T) {
			directory := controlDirectory(t)
			store := openControl(t, directory)
			initial := readControl(t, store)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			command := exec.CommandContext(ctx, executable, "-test.run=^TestControlAbruptProcessExitHelper$")
			command.Env = append(os.Environ(), "GOBY_RECOVERY_CONTROL_CRASH_POINT="+point, "GOBY_RECOVERY_CONTROL_DIRECTORY="+directory)
			output, err := command.CombinedOutput()
			cancel()
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 29 {
				t.Fatalf("unexpected helper exit: %v\n%s", err, output)
			}
			store = openControl(t, directory)
			actual := readControl(t, store)
			if point == "after proof" {
				if actual.Digest != initial.Digest {
					t.Fatal("unpublished candidate was replayed")
				}
				names, err := os.ReadDir(directory)
				if err != nil {
					t.Fatal(err)
				}
				debris := 0
				for _, entry := range names {
					if temporaryName(entry.Name()) {
						debris++
					}
				}
				if debris == 0 {
					t.Fatal("crash candidate was silently removed")
				}
			} else if actual.Revision != 1 || string(actual.Payload) != `{"phase":"crashed"}` {
				t.Fatal("renamed candidate was not recognized")
			}
			writeControl(t, store, actual.Digest, []byte(`{"phase":"continued"}`))
		})
	}
}

func TestControlFirstClaimCrashAndOrphanFilesAreNeverAutomaticallyClaimed(t *testing.T) {
	ctx := context.Background()
	for _, debris := range []string{lockName, currentName, proofName, ".next-" + strings.Repeat("a", 32)} {
		t.Run(debris, func(t *testing.T) {
			directory := controlDirectory(t)
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(directory, debris)
			if err := os.WriteFile(filename, []byte("private evidence"), 0600); err != nil {
				t.Fatal(err)
			}
			if store, err := Open(ctx, directory, deploymentOne); !errors.Is(err, ErrRecoveryRequired) {
				if store != nil {
					store.Close()
				}
				t.Fatalf("unpublished claim debris adopted: %v", err)
			}
			data, err := os.ReadFile(filename)
			if err != nil || !bytes.Equal(data, []byte("private evidence")) {
				t.Fatal("unowned evidence was modified")
			}
		})
	}
	t.Run("claimed store inert temporary", func(t *testing.T) {
		directory := controlDirectory(t)
		store := openControl(t, directory)
		initial := readControl(t, store)
		filename := filepath.Join(directory, ".next-"+strings.Repeat("b", 32))
		contents := []byte(`{"not":"a coordinator record"}`)
		if err := os.WriteFile(filename, contents, 0600); err != nil {
			t.Fatal(err)
		}
		if current := readControl(t, store); current.Digest != initial.Digest {
			t.Fatal("inert temporary changed current")
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store = openControl(t, directory)
		writeControl(t, store, initial.Digest, []byte("{}"))
		if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, contents) {
			t.Fatal("temporary file was adopted or deleted")
		}
	})
}
