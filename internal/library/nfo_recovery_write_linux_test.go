//go:build linux && !android

package library

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
)

const (
	nfoRecoveryTestLeaf    = "movie.nfo"
	nfoRecoveryTestDamaged = "<movie><title>"
	nfoRecoveryTestData    = "<movie><title>Restored</title></movie>"
	nfoRecoveryTestWrong   = "<movie><title>Modified</title></movie>"
)

func nfoRecoveryTestDir(t *testing.T) *os.Root {
	t.Helper()
	dir, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := dir.WriteFile(nfoRecoveryTestLeaf, []byte(nfoRecoveryTestDamaged), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func requireNFORecoveryTestFile(t *testing.T, dir *os.Root, name, want string) {
	t.Helper()
	got, err := dir.ReadFile(name)
	if err != nil || string(got) != want {
		t.Fatalf("read %s = %q, %v; want %q", name, got, err, want)
	}
}

func requireNFORecoveryTestNoStage(t *testing.T, dir *os.Root) {
	t.Helper()
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil || len(entries) != 1 || entries[0].Name() != nfoRecoveryTestLeaf {
		t.Fatalf("unexpected files after recovery: %v, %v", entries, err)
	}
}

func newNFORecoveryTestStage(t *testing.T, dir *os.Root, data []byte) *nfoRecoveryStage {
	t.Helper()
	stage, err := stageNFORecovery(t.Context(), dir, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stage.close(t.Context()); err != nil {
			t.Error(err)
		}
	})
	return stage
}

func TestNFORecoveryWritePublishesVerifiedStage(t *testing.T) {
	dir := nfoRecoveryTestDir(t)
	data := []byte(nfoRecoveryTestData)
	stage := newNFORecoveryTestStage(t, dir, data)
	data[0] = '!'
	staged, err := dir.Lstat(stage.name)
	if err != nil || !strings.HasPrefix(stage.name, ".") || staged.Mode().Perm() != 0o600 {
		t.Fatalf("stage is not private and hidden: %q, %v, %v", stage.name, staged, err)
	}
	requireNFORecoveryTestFile(t, dir, stage.name, nfoRecoveryTestData)
	checks := 0
	if err := stage.checkReplacement(t.Context(), nfoRecoveryTestLeaf, func() error {
		checks++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestDamaged)
	requireNFORecoveryTestFile(t, dir, stage.name, nfoRecoveryTestData)

	if err := stage.replace(t.Context(), nfoRecoveryTestLeaf); err != nil {
		t.Fatal(err)
	}
	published, err := dir.Lstat(nfoRecoveryTestLeaf)
	if err != nil || !os.SameFile(staged, published) || checks != 1 {
		t.Fatalf("publication identity or check count mismatch: %v, checks=%d", err, checks)
	}
	if err := stage.verifyPublished(t.Context(), nfoRecoveryTestLeaf); err != nil {
		t.Fatal(err)
	}
	if stage.pin == nil || stage.directory == nil {
		t.Fatal("verification consumed the stage")
	}
	if err := stage.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestData)
	requireNFORecoveryTestNoStage(t, dir)
}

func TestNFORecoveryWriteRechecksCancellationBeforeRename(t *testing.T) {
	dir := nfoRecoveryTestDir(t)
	stage := newNFORecoveryTestStage(t, dir, []byte(nfoRecoveryTestData))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := stage.checkReplacement(ctx, nfoRecoveryTestLeaf, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := stage.replace(ctx, nfoRecoveryTestLeaf); !errors.Is(err, context.Canceled) || errors.Is(err, errNFORecoveryPublicationUncertain) {
		t.Fatalf("replace = %v; want cancellation before the rename", err)
	}
	requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestDamaged)
	requireNFORecoveryTestFile(t, dir, stage.name, nfoRecoveryTestData)
}

func TestNFORecoveryWriteDiscardWithCanceledContext(t *testing.T) {
	dir := nfoRecoveryTestDir(t)
	stage := newNFORecoveryTestStage(t, dir, []byte(nfoRecoveryTestData))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := stage.close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := stage.checkReplacement(t.Context(), nfoRecoveryTestLeaf, func() error { return nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("discarded stage still usable: %v", err)
	}
	requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestDamaged)
	requireNFORecoveryTestNoStage(t, dir)
}

func TestNFORecoveryWriteRejectsIncompleteOrWrongBytes(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want error
	}{
		{"short write without an error", nfoRecoveryTestData[:len(nfoRecoveryTestData)-1], io.ErrShortWrite},
		{"valid wrong XML of the same length", nfoRecoveryTestWrong, ErrSourceChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := nfoRecoveryTestDir(t)
			stage, err := stageNFORecoveryWithWrite(t.Context(), dir, []byte(nfoRecoveryTestData), func(file *os.File, _ []byte) (int, error) {
				return file.WriteString(test.data)
			})
			if stage != nil || !errors.Is(err, test.want) || errors.Is(err, errNFORecoveryPublicationUncertain) {
				t.Fatalf("staging result = %v, %v; want %v without publication", stage, err, test.want)
			}
			requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestDamaged)
			requireNFORecoveryTestNoStage(t, dir)
		})
	}
}

func TestNFORecoveryWritePreservesEntriesWhenFenceFails(t *testing.T) {
	for _, test := range []string{"check error", "replacement target", "replacement stage", "modified stage", "cancellation"} {
		t.Run(test, func(t *testing.T) {
			dir := nfoRecoveryTestDir(t)
			stage := newNFORecoveryTestStage(t, dir, []byte(nfoRecoveryTestData))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fault := errors.New("source fence rejected")
			wantErr, wantLeaf := ErrSourceChanged, nfoRecoveryTestDamaged
			err := stage.checkReplacement(ctx, nfoRecoveryTestLeaf, func() error {
				switch test {
				case "check error":
					wantErr = fault
					return fault
				case "replacement target":
					wantLeaf = nfoRecoveryTestWrong
					if err := dir.WriteFile("replacement", []byte(wantLeaf), 0o600); err != nil {
						t.Fatal(err)
					}
					return dir.Rename("replacement", nfoRecoveryTestLeaf)
				case "replacement stage":
					if err := dir.Remove(stage.name); err != nil {
						t.Fatal(err)
					}
					return dir.WriteFile(stage.name, []byte(nfoRecoveryTestData), 0o600)
				case "modified stage":
					return dir.WriteFile(stage.name, []byte(nfoRecoveryTestData[:len(nfoRecoveryTestData)-1]), 0o600)
				case "cancellation":
					wantErr = context.Canceled
					cancel()
				}
				return nil
			})
			if !errors.Is(err, wantErr) || errors.Is(err, errNFORecoveryPublicationUncertain) {
				t.Fatalf("publication = %v; want %v before the rename", err, wantErr)
			}
			requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, wantLeaf)
			staged := nfoRecoveryTestData
			if test == "modified stage" {
				staged = staged[:len(staged)-1]
			}
			requireNFORecoveryTestFile(t, dir, stage.name, staged)
			closeErr := stage.close(ctx)
			if test == "replacement stage" {
				if !errors.Is(closeErr, ErrSourceChanged) {
					t.Fatalf("close = %v; want source change", closeErr)
				}
				requireNFORecoveryTestFile(t, dir, stage.name, nfoRecoveryTestData)
			} else {
				if closeErr != nil {
					t.Fatal(closeErr)
				}
				requireNFORecoveryTestNoStage(t, dir)
			}
		})
	}
}

func TestNFORecoveryWriteDoesNotFollowTargetSymlink(t *testing.T) {
	dir := nfoRecoveryTestDir(t)
	if err := dir.Symlink(nfoRecoveryTestLeaf, "linked.nfo"); err != nil {
		t.Fatal(err)
	}
	stage := newNFORecoveryTestStage(t, dir, []byte(nfoRecoveryTestData))
	err := stage.checkReplacement(t.Context(), "linked.nfo", func() error {
		t.Fatal("publication fence reached for a symlink")
		return nil
	})
	if !errors.Is(err, ErrSourceChanged) || errors.Is(err, errNFORecoveryPublicationUncertain) {
		t.Fatalf("publish symlink: %v", err)
	}
	if link, err := dir.Readlink("linked.nfo"); err != nil || link != nfoRecoveryTestLeaf {
		t.Fatalf("symlink changed: %q, %v", link, err)
	}
	requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestDamaged)
}

func publishNFORecoveryWithIO(ctx context.Context, stage *nfoRecoveryStage, leaf string,
	rename func(*os.Root, string, string) error, syncDir func(*os.File) error) error {
	if err := stage.checkReplacement(ctx, leaf, func() error { return nil }); err != nil {
		return err
	}
	if err := stage.replaceWithRename(ctx, leaf, rename); err != nil {
		return err
	}
	return stage.verifyPublishedWithSync(ctx, leaf, syncDir)
}

func TestNFORecoveryWriteReportsUncertaintyAfterRename(t *testing.T) {
	fault := errors.New("injected storage failure")
	for _, test := range []struct {
		name     string
		wantErr  error
		wantLeaf string
	}{
		{"rename error before replacement", fault, nfoRecoveryTestDamaged},
		{"rename error after replacement", fault, nfoRecoveryTestData},
		{"directory sync error", fault, nfoRecoveryTestData},
		{"cancellation after replacement", context.Canceled, nfoRecoveryTestData},
		{"destination has wrong bytes", ErrSourceChanged, nfoRecoveryTestWrong},
		{"destination has a different inode", ErrSourceChanged, nfoRecoveryTestData},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := nfoRecoveryTestDir(t)
			stage := newNFORecoveryTestStage(t, dir, []byte(nfoRecoveryTestData))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			renames, syncs := 0, 0
			err := publishNFORecoveryWithIO(ctx, stage, nfoRecoveryTestLeaf,
				func(root *os.Root, from, to string) error {
					renames++
					if test.name == "rename error before replacement" {
						return fault
					}
					if err := root.Rename(from, to); err != nil {
						t.Fatal(err)
					}
					if test.name == "rename error after replacement" {
						return fault
					}
					return nil
				}, func(directory *os.File) error {
					syncs++
					requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, nfoRecoveryTestData)
					switch test.name {
					case "directory sync error":
						return fault
					case "cancellation after replacement":
						cancel()
					case "destination has wrong bytes":
						if err := dir.WriteFile(nfoRecoveryTestLeaf, []byte(nfoRecoveryTestWrong), 0o600); err != nil {
							t.Fatal(err)
						}
					case "destination has a different inode":
						if err := dir.WriteFile("replacement", []byte(nfoRecoveryTestData), 0o600); err != nil {
							t.Fatal(err)
						}
						if err := dir.Rename("replacement", nfoRecoveryTestLeaf); err != nil {
							t.Fatal(err)
						}
					}
					return directory.Sync()
				})
			if renames != 1 || !errors.Is(err, errNFORecoveryPublicationUncertain) || !errors.Is(err, test.wantErr) {
				t.Fatalf("publication = %v, renames = %d; want uncertain %v after one rename attempt", err, renames, test.wantErr)
			}
			wantSyncs := 1
			if strings.HasPrefix(test.name, "rename error") {
				wantSyncs = 0
			}
			if syncs != wantSyncs {
				t.Fatalf("directory syncs = %d; want %d", syncs, wantSyncs)
			}
			if test.name == "rename error before replacement" {
				requireNFORecoveryTestFile(t, dir, stage.name, nfoRecoveryTestData)
			}
			if err := stage.close(ctx); err != nil {
				t.Fatal(err)
			}
			requireNFORecoveryTestFile(t, dir, nfoRecoveryTestLeaf, test.wantLeaf)
			requireNFORecoveryTestNoStage(t, dir)
		})
	}
}
