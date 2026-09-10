//go:build linux

package backupstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/unix"
)

type storeFixture struct {
	t         *testing.T
	store     *Store
	config    Config
	abandoned bool
}

func testStoreConfig(directory string) Config {
	return Config{Directory: directory, MaxObjectBytes: 1024, MaxTotalBytes: 4096, MaxObjects: 16, MinFreeBytes: 1}
}

func newStoreFixture(t *testing.T, configure ...func(*Config)) *storeFixture {
	t.Helper()
	cfg := testStoreConfig(filepath.Join(t.TempDir(), "store"))
	for _, configure := range configure {
		configure(&cfg)
	}
	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	f := &storeFixture{t: t, store: s, config: cfg}
	t.Cleanup(func() {
		if !f.abandoned {
			_ = f.store.Close()
		}
	})
	return f
}

func (f *storeFixture) crash() {
	f.t.Helper()
	// Close kernel resources without logical shutdown, preserving the actual
	// journal and object bytes exactly as they stood at the crash boundary.
	for _, writer := range f.store.writers {
		if writer.stop != nil {
			writer.stop()
		}
		if err := writer.file.Close(); err != nil {
			f.t.Fatalf("crash close writer: %v", err)
		}
	}
	for reader := range f.store.readers {
		if reader.stop != nil {
			reader.stop()
		}
		if err := reader.file.Close(); err != nil {
			f.t.Fatalf("crash close reader: %v", err)
		}
	}
	for scratch := range f.store.scratchFiles {
		if scratch.stop != nil {
			scratch.stop()
		}
		if err := scratch.file.Close(); err != nil {
			f.t.Fatalf("crash close scratch: %v", err)
		}
	}
	if err := f.store.lock.Close(); err != nil {
		f.t.Fatalf("crash close lock: %v", err)
	}
	if err := f.store.directory.Close(); err != nil {
		f.t.Fatalf("crash close directory: %v", err)
	}
	f.abandoned = true
}

func (f *storeFixture) reopen() *Store {
	f.t.Helper()
	s, err := Open(f.config)
	if err != nil {
		f.t.Fatalf("reopen: %v", err)
	}
	f.store = s
	f.abandoned = false
	return s
}

func beginTestWriter(t *testing.T, s *Store, ctx context.Context) *Writer {
	t.Helper()
	w, err := s.Begin(ctx, BeginOptions{Kind: KindImported, CreatorID: "actor-1", SessionID: "session-1"})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return w
}

func writeTestBytes(t *testing.T, w *Writer, data []byte) {
	t.Helper()
	if n, err := w.Write(data); err != nil || n != len(data) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(data))
	}
}

func prepareTestWriter(t *testing.T, w *Writer, data []byte) Prepared {
	t.Helper()
	writeTestBytes(t, w, data)
	proof, err := w.Prepare(context.Background())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if proof.Size != int64(len(data)) || proof.Digest != testDigest(data) {
		t.Fatalf("incorrect prepared descriptor: %+v", proof)
	}
	return proof
}

func publishTestObject(t *testing.T, s *Store, data []byte) Metadata {
	t.Helper()
	w := beginTestWriter(t, s, context.Background())
	proof := prepareTestWriter(t, w, data)
	metadata, err := w.Publish(context.Background(), proof, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return metadata
}

func testDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func testSummary() SourceSummary {
	return SourceSummary{
		ArchiveID:          "0123456789abcdef0123456789abcdef",
		FormatVersion:      1,
		ApplicationVersion: "goby-test.1",
		SchemaVersion:      1,
		ServerID:           "server-\u6d4b\u8bd5",
		CreatedAt:          time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
		Tables:             []TableCount{{Name: "users", Rows: 3}},
	}
}

func requireError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func requireMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat(%q) = %v, want missing file", path, err)
	}
}

func requireFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("ReadFile(%q) = (%q, %v), want %q", path, got, err, want)
	}
}

func requirePrivateFile(t *testing.T, path string) {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	if !ownedFile(st) {
		t.Fatalf("file is not private and singly linked: mode=%#o uid=%d links=%d", st.Mode, st.Uid, st.Nlink)
	}
}

func TestOpenRequiresPrivateOwnedDirectory(t *testing.T) {
	for _, mode := range []os.FileMode{0755, 0710, 0701} {
		t.Run(mode.String(), func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "store")
			if err := os.Mkdir(directory, mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(directory, mode); err != nil {
				t.Fatal(err)
			}
			s, err := Open(testStoreConfig(directory))
			if s != nil {
				_ = s.Close()
				t.Fatal("Open accepted a directory with non-private permissions")
			}
			requireError(t, err, ErrUnavailable)
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected directory changed: entries=%v, error=%v", entries, err)
			}
		})
	}
	t.Run("created directory and files", func(t *testing.T) {
		f := newStoreFixture(t)
		var st unix.Stat_t
		if err := unix.Lstat(f.config.Directory, &st); err != nil {
			t.Fatal(err)
		}
		if st.Mode&07777 != 0700 || st.Uid != uint32(os.Geteuid()) {
			t.Fatalf("directory mode=%#o uid=%d", st.Mode, st.Uid)
		}
		for _, name := range []string{markerName, lockName, catalogName} {
			requirePrivateFile(t, filepath.Join(f.config.Directory, name))
		}
	})
	t.Run("foreign owner", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("changing ownership requires root")
		}
		directory := filepath.Join(t.TempDir(), "store")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(directory, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		s, err := Open(testStoreConfig(directory))
		if s != nil {
			_ = s.Close()
			t.Fatal("Open accepted a directory owned by another UID")
		}
		requireError(t, err, ErrUnavailable)
	})
}

func TestOpenRejectsSymlinkAncestors(t *testing.T) {
	for _, leaf := range []bool{false, true} {
		name := "ancestor"
		if leaf {
			name = "leaf"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			target := filepath.Join(base, "target")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(base, "link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			directory := link
			if !leaf {
				directory = filepath.Join(link, "store")
			}
			s, err := Open(testStoreConfig(directory))
			if s != nil {
				_ = s.Close()
				t.Fatal("Open followed a symlink")
			}
			requireError(t, err, ErrUnavailable)
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("symlink target changed: entries=%v error=%v", entries, err)
			}
		})
	}
}

func TestOpenAndLiveStoreRejectUnknownFiles(t *testing.T) {
	t.Run("unclaimed directory", func(t *testing.T) {
		directory := t.TempDir()
		foreign := filepath.Join(directory, "unrelated.age")
		if err := os.WriteFile(foreign, []byte("foreign bytes"), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := Open(testStoreConfig(directory))
		if s != nil {
			_ = s.Close()
			t.Fatal("Open claimed a nonempty directory")
		}
		requireError(t, err, ErrUnavailable)
		requireFileBytes(t, foreign, []byte("foreign bytes"))
		requireMissing(t, filepath.Join(directory, markerName))
	})
	for _, live := range []bool{false, true} {
		name := "reopen"
		if live {
			name = "live operation"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t)
			foreign := filepath.Join(f.config.Directory, "object-unregistered.age")
			if err := os.WriteFile(foreign, []byte("foreign bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if live {
				_, err := f.store.List(context.Background(), 0, 20)
				requireError(t, err, ErrUnavailable)
				if f.store.Status().Healthy {
					t.Fatal("unknown file did not degrade the store")
				}
			} else {
				f.crash()
				s, err := Open(f.config)
				if s != nil {
					_ = s.Close()
					t.Fatal("Open accepted an unregistered file")
				}
				requireError(t, err, ErrUnavailable)
			}
			requireFileBytes(t, foreign, []byte("foreign bytes"))
		})
	}
}

func TestStoreLockAndControlFileIntegrity(t *testing.T) {
	f := newStoreFixture(t)
	second, err := Open(f.config)
	if second != nil {
		_ = second.Close()
		t.Fatal("second Open acquired a live store")
	}
	requireError(t, err, ErrBusy)
	markerPath := filepath.Join(f.config.Directory, markerName)
	original, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(replacement, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, markerPath); err != nil {
		t.Fatal(err)
	}
	_, err = f.store.Begin(context.Background(), BeginOptions{Kind: KindImported})
	requireError(t, err, ErrUnavailable)
	if f.store.Status().Healthy {
		t.Fatal("replacement control inode did not degrade the store")
	}
	requireFileBytes(t, markerPath, original)
}

func TestOpaquePublishAndVerify(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	data := []byte{0, 0xff, 1, 'x', '\n', 0, 4}
	w := beginTestWriter(t, f.store, ctx)
	id := w.Metadata().ID
	if metadata := w.Metadata(); metadata.State != StateWriting || metadata.Verified || metadata.Summary != nil {
		t.Fatalf("initial metadata: %+v", metadata)
	}
	_, err := f.store.Snapshot(ctx, id)
	requireError(t, err, ErrNotReady)
	proof := prepareTestWriter(t, w, data)
	_, err = f.store.Snapshot(ctx, id)
	requireError(t, err, ErrNotReady)
	modified := proof
	modified.Size++
	_, err = w.Publish(ctx, modified, nil)
	requireError(t, err, ErrConflict)
	if n, err := w.Write([]byte("late")); n != 0 || !errors.Is(err, ErrConflict) {
		t.Fatalf("Write after Prepare = (%d, %v)", n, err)
	}
	metadata, err := w.Publish(ctx, proof, nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.State != StateReady || metadata.Verified || metadata.Summary != nil || metadata.Digest != testDigest(data) || metadata.Size != int64(len(data)) {
		t.Fatalf("unverified published metadata: %+v", metadata)
	}
	if got := f.store.Status(); got.Writers != 0 || got.Objects != 1 || got.Bytes != int64(len(data)) {
		t.Fatalf("published status: %+v", got)
	}
	requireMissing(t, filepath.Join(f.config.Directory, basename(id, false)))
	requirePrivateFile(t, filepath.Join(f.config.Directory, basename(id, true)))
	snapshot, err := f.store.Snapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(snapshot)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("Snapshot bytes = (%v, %v), want %v", got, err, data)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	summary := testSummary()
	verified, err := f.store.Verify(ctx, id, metadata.Digest, summary)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Verified || verified.Summary == nil || verified.Summary.ArchiveID != summary.ArchiveID || verified.Summary.ServerID != summary.ServerID {
		t.Fatalf("verified metadata: %+v", verified)
	}
	summary.Tables[0].Rows = 99
	verified.Summary.Tables[0].Rows = 100
	stored, err := f.store.Get(ctx, id)
	if err != nil || stored.Summary == nil || stored.Summary.Tables[0].Rows != 3 {
		t.Fatalf("summary was not independently copied: %+v, %v", stored, err)
	}
	f.crash()
	stored, err = f.reopen().Get(ctx, id)
	if err != nil || !stored.Verified || stored.Summary == nil || stored.Summary.Tables[0].Rows != 3 {
		t.Fatalf("verified summary was not persisted: %+v, %v", stored, err)
	}
}

func TestWrongExpectedDigestLeavesStoreHealthy(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	metadata := publishTestObject(t, f.store, []byte("opaque"))
	wrong := strings.Repeat("0", 64)
	_, err := f.store.Verify(ctx, metadata.ID, wrong, testSummary())
	requireError(t, err, ErrConflict)
	requireError(t, f.store.Delete(ctx, metadata.ID, wrong), ErrConflict)
	if got := f.store.Status(); !got.Healthy || got.Objects != 1 || got.Readers != 0 {
		t.Fatalf("bad CAS input changed store health or references: %+v", got)
	}
	if _, err := f.store.Verify(ctx, metadata.ID, metadata.Digest, testSummary()); err != nil {
		t.Fatalf("valid Verify after bad CAS input: %v", err)
	}
}

func TestVerifyRehashesActualBytes(t *testing.T) {
	f := newStoreFixture(t)
	metadata := publishTestObject(t, f.store, []byte("original"))
	name := basename(metadata.ID, true)
	path := filepath.Join(f.config.Directory, name)
	var before unix.Stat_t
	if err := unix.Stat(path, &before); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	// Preserve the inode, size, and exact mtime so only a real content hash can
	// distinguish the altered object from the registered descriptor.
	if err := unix.UtimesNanoAt(int(f.store.directory.Fd()), name, []unix.Timespec{before.Atim, before.Mtim}, 0); err != nil {
		t.Fatal(err)
	}
	var after unix.Stat_t
	if err := unix.Stat(path, &after); err != nil {
		t.Fatal(err)
	}
	if identity(after) != identity(before) || after.Size != before.Size || stamp(after) != stamp(before) {
		t.Fatal("tampering fixture did not preserve the registered descriptor")
	}
	_, err := f.store.Verify(context.Background(), metadata.ID, metadata.Digest, testSummary())
	requireError(t, err, ErrIntegrity)
	if f.store.Status().Healthy {
		t.Fatal("content hash mismatch did not degrade the store")
	}
	if got := f.store.Status(); got.Readers != 0 || got.Writers != 0 {
		t.Fatalf("failed Verify leaked references: %+v", got)
	}
	requireFileBytes(t, path, []byte("tampered"))
}

func TestSnapshotRejectsUnsafeKnownObjects(t *testing.T) {
	for _, attack := range []string{"hardlink", "replacement", "symlink", "permissions"} {
		t.Run(attack, func(t *testing.T) {
			f := newStoreFixture(t)
			metadata := publishTestObject(t, f.store, []byte("opaque"))
			path := filepath.Join(f.config.Directory, basename(metadata.ID, true))
			external := filepath.Join(t.TempDir(), "external")
			want := error(ErrUnavailable)
			switch attack {
			case "hardlink":
				if err := os.Link(path, external); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := os.WriteFile(external, []byte("opaque"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(external, path); err != nil {
					t.Fatal(err)
				}
				want = ErrIntegrity
			case "symlink":
				if err := os.Rename(path, external); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := f.store.Snapshot(context.Background(), metadata.ID)
			if snapshot != nil {
				_ = snapshot.Close()
				t.Fatal("Snapshot accepted an unsafe known object")
			}
			requireError(t, err, want)
			if got := f.store.Status(); got.Healthy || got.Readers != 0 {
				t.Fatalf("unsafe object did not fail closed: %+v", got)
			}
			if attack == "hardlink" || attack == "symlink" {
				requireFileBytes(t, external, []byte("opaque"))
			}
		})
	}
}

func TestDeleteRejectsLiveReferences(t *testing.T) {
	f := newStoreFixture(t)
	s := f.store
	ctx := context.Background()
	w := beginTestWriter(t, s, ctx)
	id := w.Metadata().ID
	requireError(t, s.Delete(ctx, id, ""), ErrBusy)
	proof := prepareTestWriter(t, w, []byte("opaque"))
	requireError(t, s.Delete(ctx, id, proof.Digest), ErrBusy)
	metadata, err := w.Publish(ctx, proof, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, s.Delete(ctx, id, metadata.Digest), ErrBusy)
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	release, err := s.Protect(id)
	if err != nil {
		t.Fatal(err)
	}
	secondRelease, err := s.Protect(id)
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, s.Delete(ctx, id, metadata.Digest), ErrBusy)
	release()
	release()
	requireError(t, s.Delete(ctx, id, metadata.Digest), ErrBusy)
	secondRelease()
	if err := s.Delete(ctx, id, metadata.Digest); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(ctx, id)
	requireError(t, err, ErrNotFound)
	requireMissing(t, filepath.Join(f.config.Directory, basename(id, true)))
	if got := s.Status(); !got.Healthy || got.Objects != 0 || got.Bytes != 0 || got.Readers != 0 || got.Writers != 0 {
		t.Fatalf("status after deleting unpinned object: %+v", got)
	}
}

func TestQuotasAndStickyWrite(t *testing.T) {
	t.Run("object bytes", func(t *testing.T) {
		f := newStoreFixture(t, func(cfg *Config) { cfg.MaxObjectBytes = 8 })
		w := beginTestWriter(t, f.store, context.Background())
		writeTestBytes(t, w, []byte("12345678"))
		if n, err := w.Write([]byte("9")); n != 0 || !errors.Is(err, ErrQuota) {
			t.Fatalf("overflow Write = (%d, %v)", n, err)
		}
		for _, data := range [][]byte{nil, []byte("x")} {
			if n, err := w.Write(data); n != 0 || !errors.Is(err, ErrQuota) {
				t.Fatalf("sticky Write = (%d, %v)", n, err)
			}
		}
		_, err := w.Prepare(context.Background())
		requireError(t, err, ErrQuota)
		if w.Metadata().Size != 8 || !f.store.Status().Healthy {
			t.Fatal("quota rejection changed accepted bytes or health")
		}
		if err := w.Abort(context.Background(), CodeQuota); err != nil {
			t.Fatal(err)
		}
		if got := w.Metadata(); got.Size != 0 || got.State != StateFailed || got.ErrorCode != CodeQuota {
			t.Fatalf("quota abort metadata: %+v", got)
		}
	})
	t.Run("total includes active writer", func(t *testing.T) {
		f := newStoreFixture(t, func(cfg *Config) { cfg.MaxObjectBytes, cfg.MaxTotalBytes = 8, 12 })
		publishTestObject(t, f.store, []byte("12345678"))
		w := beginTestWriter(t, f.store, context.Background())
		writeTestBytes(t, w, []byte("1234"))
		if n, err := w.Write([]byte("5")); n != 0 || !errors.Is(err, ErrQuota) {
			t.Fatalf("aggregate quota Write = (%d, %v)", n, err)
		}
		if got := f.store.Status(); !got.Healthy || got.Bytes != 12 || got.Writers != 1 {
			t.Fatalf("aggregate quota accounting: %+v", got)
		}
	})
	t.Run("object count includes failed jobs", func(t *testing.T) {
		f := newStoreFixture(t, func(cfg *Config) { cfg.MaxObjects = 2 })
		first := beginTestWriter(t, f.store, context.Background())
		if err := first.Abort(context.Background(), CodeImport); err != nil {
			t.Fatal(err)
		}
		beginTestWriter(t, f.store, context.Background())
		_, err := f.store.Begin(context.Background(), BeginOptions{Kind: KindImported})
		requireError(t, err, ErrQuota)
		if err := f.store.Delete(context.Background(), first.Metadata().ID, ""); err != nil {
			t.Fatal(err)
		}
		beginTestWriter(t, f.store, context.Background())
		if got := f.store.Status(); got.Objects != 2 || got.Writers != 2 || !got.Healthy {
			t.Fatalf("count quota accounting: %+v", got)
		}
	})
	t.Run("minimum free space and sticky failure", func(t *testing.T) {
		f := newStoreFixture(t)
		w := beginTestWriter(t, f.store, context.Background())
		reserve := uint64(f.config.MinFreeBytes) + uint64(metadataReserve)
		f.store.freeBytes = func(int) (uint64, error) { return reserve, nil }
		if n, err := w.Write([]byte("x")); n != 0 || !errors.Is(err, ErrQuota) {
			t.Fatalf("minimum free space Write = (%d, %v)", n, err)
		}
		f.store.freeBytes = func(int) (uint64, error) { return reserve + 1024, nil }
		if n, err := w.Write([]byte("x")); n != 0 || !errors.Is(err, ErrQuota) {
			t.Fatalf("Write after space recovers = (%d, %v)", n, err)
		}
		if got := f.store.Status(); got.Bytes != 0 || !got.Healthy {
			t.Fatalf("minimum free space accounting: %+v", got)
		}
	})
}

func TestSnapshotReadSeekAndReaderLimit(t *testing.T) {
	f := newStoreFixture(t)
	metadata := publishTestObject(t, f.store, []byte("01234567"))
	ctx := context.Background()
	readers := make([]*Snapshot, 0, MaxReaders)
	for range MaxReaders {
		snapshot, err := f.store.Snapshot(ctx, metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, snapshot)
	}
	_, err := f.store.Snapshot(ctx, metadata.ID)
	requireError(t, err, ErrBusy)
	snapshot := readers[0]
	if snapshot.Name() != metadata.ID+".age" || snapshot.Size() != 8 || snapshot.ModTime().IsZero() {
		t.Fatal("incorrect snapshot descriptors")
	}
	if offset, err := snapshot.Seek(-3, io.SeekEnd); err != nil || offset != 5 {
		t.Fatalf("Seek = (%d, %v)", offset, err)
	}
	data, err := io.ReadAll(snapshot)
	if err != nil || string(data) != "567" {
		t.Fatalf("suffix = (%q, %v)", data, err)
	}
	if _, err := snapshot.Seek(1, io.SeekEnd); !errors.Is(err, ErrInvalid) {
		t.Fatalf("out of bounds Seek: %v", err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := f.store.Snapshot(ctx, metadata.ID)
	if err != nil {
		t.Fatalf("released reader slot was not reusable: %v", err)
	}
	if got := f.store.Status(); got.Readers != MaxReaders {
		t.Fatalf("reader slot accounting: %+v", got)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	for _, reader := range readers[1:] {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.store.Status(); got.Readers != 0 {
		t.Fatalf("reader slots were not released: %+v", got)
	}
}

func TestContextCancellationAndStoreCloseReleaseReferences(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newStoreFixture(t)
			metadata := publishTestObject(t, f.store, []byte("ready"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := beginTestWriter(t, f.store, ctx)
			writeTestBytes(t, w, []byte("unfinished"))
			snapshot, err := f.store.Snapshot(ctx, metadata.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.store.Status(); got.Readers != 1 || got.Writers != 1 {
				t.Fatalf("live references: %+v", got)
			}
			cancel()
			synctest.Wait()
			if got := f.store.Status(); !got.Healthy || got.Readers != 0 || got.Writers != 0 || got.Bytes != metadata.Size {
				t.Fatalf("cancelled references: %+v", got)
			}
			if got := w.Metadata(); got.State != StateCancelled || got.ErrorCode != CodeCancelled || got.Size != 0 {
				t.Fatalf("cancelled writer metadata: %+v", got)
			}
			if _, err := snapshot.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
				t.Fatalf("Read after cancellation: %v", err)
			}
			requireMissing(t, filepath.Join(f.config.Directory, basename(w.Metadata().ID, false)))
		})
	})
	t.Run("store shutdown", func(t *testing.T) {
		f := newStoreFixture(t)
		metadata := publishTestObject(t, f.store, []byte("ready"))
		w := beginTestWriter(t, f.store, context.Background())
		writeTestBytes(t, w, []byte("unfinished"))
		snapshot, err := f.store.Snapshot(context.Background(), metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.store.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.store.Close(); err != nil {
			t.Fatalf("second store Close: %v", err)
		}
		if got := f.store.Status(); !got.Closed || got.Healthy || got.Readers != 0 || got.Writers != 0 || got.Bytes != metadata.Size {
			t.Fatalf("shutdown reference accounting: %+v", got)
		}
		if got := w.Metadata(); got.State != StateInterrupted || got.ErrorCode != CodeInterrupted {
			t.Fatalf("shutdown writer metadata: %+v", got)
		}
		if _, err := snapshot.Read(make([]byte, 1)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Read after shutdown: %v", err)
		}
	})
}

func TestSnapshotConcurrentCloseWaitsForReaderRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newStoreFixture(t)
		metadata := publishTestObject(t, f.store, []byte("opaque"))
		snapshot, err := f.store.Snapshot(context.Background(), metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		entered := make(chan struct{})
		allowRelease := make(chan struct{})
		var releaseOnce sync.Once
		unblockRelease := func() { releaseOnce.Do(func() { close(allowRelease) }) }
		defer unblockRelease()
		release := snapshot.release
		snapshot.release = func(reader *Snapshot) {
			close(entered)
			<-allowRelease
			release(reader)
		}
		first := make(chan error, 1)
		second := make(chan error, 1)
		go func() { first <- snapshot.Close() }()
		<-entered
		go func() { second <- snapshot.Close() }()
		synctest.Wait()
		select {
		case err := <-first:
			t.Fatalf("first Close returned before release: %v", err)
		default:
		}
		select {
		case err := <-second:
			t.Fatalf("second Close returned before release: %v", err)
		default:
		}
		if got := f.store.Status(); got.Readers != 1 {
			t.Fatalf("reader released before close barrier: %+v", got)
		}
		unblockRelease()
		synctest.Wait()
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if got := f.store.Status(); got.Readers != 0 {
			t.Fatalf("Close returned without releasing its slot: %+v", got)
		}
	})
}

func TestCrashWritingRecoversInterruptedJob(t *testing.T) {
	for _, unlinked := range []bool{false, true} {
		name := "partial remains"
		if unlinked {
			name = "unlink completed before catalog update"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t)
			w := beginTestWriter(t, f.store, context.Background())
			writeTestBytes(t, w, []byte("incomplete bytes"))
			id := w.Metadata().ID
			partial := filepath.Join(f.config.Directory, basename(id, false))
			requireFileBytes(t, partial, []byte("incomplete bytes"))
			if unlinked {
				if err := os.Remove(partial); err != nil {
					t.Fatal(err)
				}
				if err := f.store.directory.Sync(); err != nil {
					t.Fatal(err)
				}
			}
			f.crash()
			s := f.reopen()
			metadata, err := s.Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.State != StateInterrupted || metadata.ErrorCode != CodeInterrupted || metadata.Size != 0 || metadata.Digest != "" || metadata.Verified {
				t.Fatalf("recovered writing job: %+v", metadata)
			}
			requireMissing(t, partial)
			_, err = s.Snapshot(context.Background(), id)
			requireError(t, err, ErrNotReady)
			if got := s.Status(); !got.Healthy || got.Objects != 1 || got.Bytes != 0 || got.Writers != 0 {
				t.Fatalf("recovered writing accounting: %+v", got)
			}
		})
	}
}

func TestPreparedRecoveryPreservesBytesAndTerminalState(t *testing.T) {
	for _, code := range []ErrorCode{CodeNone, CodeImport, CodeCancelled} {
		name := string(code)
		if code == CodeNone {
			name = "crash before publication"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t)
			data := []byte("prepared opaque bytes")
			w := beginTestWriter(t, f.store, context.Background())
			proof := prepareTestWriter(t, w, data)
			wantState, wantCode := StateInterrupted, CodeInterrupted
			if code != CodeNone {
				if err := w.Abort(context.Background(), code); err != nil {
					t.Fatal(err)
				}
				wantState, wantCode = StateFailed, code
				if code == CodeCancelled {
					wantState = StateCancelled
				}
			}
			beforeCrash := w.Metadata()
			partial := filepath.Join(f.config.Directory, basename(proof.ID, false))
			f.crash()
			s := f.reopen()
			requireFileBytes(t, partial, data)
			metadata, err := s.Get(context.Background(), proof.ID)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.State != wantState || metadata.ErrorCode != wantCode || metadata.Digest != proof.Digest || metadata.Size != proof.Size || metadata.Verified {
				t.Fatalf("recovered prepared metadata: %+v", metadata)
			}
			if code != CodeNone && !metadata.UpdatedAt.Equal(beforeCrash.UpdatedAt) {
				t.Fatal("reopening rewrote an already terminal prepared job")
			}
			_, err = s.Snapshot(context.Background(), proof.ID)
			requireError(t, err, ErrNotReady)
			_, err = s.ReopenPrepared(context.Background(), proof.ID, strings.Repeat("0", 64))
			requireError(t, err, ErrConflict)
			if !s.Status().Healthy {
				t.Fatal("wrong recovery digest degraded the store")
			}
			recovered, err := s.ReopenPrepared(context.Background(), proof.ID, proof.Digest)
			if err != nil {
				t.Fatal(err)
			}
			recoveredProof, err := recovered.Prepare(context.Background())
			if err != nil || recoveredProof.Digest != proof.Digest || recoveredProof.Size != proof.Size {
				t.Fatalf("recovered Prepare = (%+v, %v)", recoveredProof, err)
			}
			metadata, err = recovered.Publish(context.Background(), recoveredProof, nil)
			if err != nil || metadata.State != StateReady || metadata.Verified || metadata.ErrorCode != CodeNone {
				t.Fatalf("explicit recovered Publish = (%+v, %v)", metadata, err)
			}
			requireFileBytes(t, filepath.Join(f.config.Directory, basename(proof.ID, true)), data)
			requireMissing(t, partial)
		})
	}
}

func TestPublishDirectorySyncFailureRecoversUnpublishedFinalFile(t *testing.T) {
	f := newStoreFixture(t)
	data := []byte("durable prepared bytes")
	w := beginTestWriter(t, f.store, context.Background())
	proof := prepareTestWriter(t, w, data)
	directorySyncs := 0
	f.store.syncFile = func(file *os.File) error {
		if file == f.store.directory {
			directorySyncs++
			if directorySyncs == 2 {
				return errors.New("injected directory sync failure after object rename")
			}
		}
		return file.Sync()
	}
	_, err := w.Publish(context.Background(), proof, nil)
	requireError(t, err, ErrUnavailable)
	if directorySyncs != 2 || f.store.Status().Healthy {
		t.Fatalf("failure did not occur after rename: syncs=%d status=%+v", directorySyncs, f.store.Status())
	}
	partial := filepath.Join(f.config.Directory, basename(proof.ID, false))
	final := filepath.Join(f.config.Directory, basename(proof.ID, true))
	requireMissing(t, partial)
	requireFileBytes(t, final, data)
	f.crash()
	s := f.reopen()
	metadata, err := s.Get(context.Background(), proof.ID)
	if err != nil || metadata.State != StateInterrupted || metadata.Digest != proof.Digest || metadata.Size != proof.Size {
		t.Fatalf("recovered publishing metadata: %+v, %v", metadata, err)
	}
	_, err = s.Snapshot(context.Background(), proof.ID)
	requireError(t, err, ErrNotReady)
	recovered, err := s.ReopenPrepared(context.Background(), proof.ID, proof.Digest)
	if err != nil {
		t.Fatal(err)
	}
	recoveredProof, err := recovered.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.Publish(context.Background(), recoveredProof, nil); err != nil {
		t.Fatalf("Publish using retained final name: %v", err)
	}
	requireFileBytes(t, final, data)
	requireMissing(t, partial)
}

func TestDeletionJournalRecoveryFinishesOnlyRegisteredObject(t *testing.T) {
	for _, unlinked := range []bool{false, true} {
		name := "journal committed before unlink"
		if unlinked {
			name = "unlink completed before catalog removal"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t)
			victim := publishTestObject(t, f.store, []byte("delete me"))
			retained := publishTestObject(t, f.store, []byte("retain me"))
			next := f.store.clone()
			next.Entries[f.store.index(victim.ID)].Deleting = true
			if err := f.store.persist(next); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.config.Directory, basename(victim.ID, true))
			if unlinked {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := f.store.directory.Sync(); err != nil {
					t.Fatal(err)
				}
			} else {
				requireFileBytes(t, path, []byte("delete me"))
			}
			f.crash()
			s := f.reopen()
			_, err := s.Get(context.Background(), victim.ID)
			requireError(t, err, ErrNotFound)
			requireMissing(t, path)
			requireFileBytes(t, filepath.Join(f.config.Directory, basename(retained.ID, true)), []byte("retain me"))
			if got := s.Status(); !got.Healthy || got.Objects != 1 || got.Bytes != retained.Size || got.Readers != 0 || got.Writers != 0 {
				t.Fatalf("recovered deletion accounting: %+v", got)
			}
			f.crash()
			if got := f.reopen().Status(); got.Objects != 1 || got.Bytes != retained.Size {
				t.Fatalf("deletion recovery was not durable: %+v", got)
			}
		})
	}
}

func TestServiceUIDCanPublishAnonymousObject(t *testing.T) {
	const directoryEnvironment = "GOBY_BACKUPSTORE_SERVICE_UID_DIRECTORY"
	if directory := os.Getenv(directoryEnvironment); directory != "" {
		if os.Geteuid() != 65534 {
			t.Fatalf("service helper UID = %d, want 65534", os.Geteuid())
		}
		requireServiceUIDPublication(t, directory)
		return
	}
	if os.Geteuid() != 0 {
		requireServiceUIDPublication(t, filepath.Join(t.TempDir(), "store"))
		return
	}
	// A fresh directory directly below /tmp avoids root-owned test or build
	// ancestors that would make the copied executable inaccessible to nobody.
	base, err := os.MkdirTemp("/tmp", "goby-backupstore-service-uid-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(base, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	helper := filepath.Join(base, "service-uid.test")
	destination, err := os.OpenFile(helper, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy helper: copy=%v close=%v", copyErr, closeErr)
	}
	if err := os.Chown(helper, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helper, 0500); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, helper, "-test.run=^TestServiceUIDCanPublishAnonymousObject$", "-test.v")
	command.Dir = base
	command.Env = append(os.Environ(), directoryEnvironment+"="+filepath.Join(base, "store"))
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("service UID publication: %v\n%s", err, output)
	}
}

func requireServiceUIDPublication(t *testing.T, directory string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("publication must be exercised by an unprivileged service UID")
	}
	s, err := Open(testStoreConfig(directory))
	if err != nil {
		t.Fatalf("service UID Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	data := []byte{0, 0xff, 'o', 'p', 'a', 'q', 'u', 'e'}
	w := beginTestWriter(t, s, context.Background())
	proof := prepareTestWriter(t, w, data)
	requirePrivateFile(t, filepath.Join(directory, basename(proof.ID, false)))
	metadata, err := w.Publish(context.Background(), proof, nil)
	if err != nil || metadata.Verified || metadata.Digest != testDigest(data) {
		t.Fatalf("service UID Publish: metadata=%+v error=%v", metadata, err)
	}
	requirePrivateFile(t, filepath.Join(directory, basename(proof.ID, true)))
	snapshot, err := s.Snapshot(context.Background(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(snapshot)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("service UID read = (%v, %v), want %v", got, err, data)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(); !got.Healthy || got.Readers != 0 || got.Writers != 0 || got.Bytes != int64(len(data)) {
		t.Fatalf("service UID accounting: %+v", got)
	}
	scratch, err := s.Scratch(context.Background(), 1024)
	if err != nil {
		t.Fatalf("service UID anonymous scratch: %v", err)
	}
	if _, err := scratch.File().Write([]byte("ephemeral working bytes")); err != nil {
		t.Fatalf("service UID scratch write: %v", err)
	}
	if err := scratch.Close(); err != nil {
		t.Fatalf("service UID scratch close: %v", err)
	}
	if got := s.Status(); got.ScratchFiles != 0 || got.ScratchBytes != 0 || got.Objects != 1 {
		t.Fatalf("service UID scratch accounting: %+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
