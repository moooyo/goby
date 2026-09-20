//go:build linux

package analysiscache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cacheTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{Root: filepath.Join(t.TempDir(), "cache"), MaxBytes: 64 << 10, MaxEntries: 4, MaxEntryBytes: 16 << 10, MaxFileBytes: 8 << 10, MaxTemporaryFiles: 8}
}

func cacheTestOpen(t *testing.T, config Config) *Store {
	t.Helper()
	store, err := Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("close cache: %v", err)
		}
	})
	return store
}

func cacheTestKey(index int) string { return fmt.Sprintf("%064x", index) }
func cacheTestProducer(data []byte) func(context.Context, io.Writer) error {
	return func(_ context.Context, writer io.Writer) error { _, err := writer.Write(data); return err }
}

func cacheTestPublish(t *testing.T, store *Store, index int, data []byte) Entry {
	t.Helper()
	builder, err := store.Begin(context.Background(), cacheTestKey(index), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteFile(context.Background(), "240.bif", cacheTestProducer(data)); err != nil {
		t.Fatal(err)
	}
	publication, err := builder.Publish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.Keep(); err != nil {
		t.Fatal(err)
	}
	return publication.Entry
}

func TestCacheStreamsTemporaryFramesAndPublishesOnlySealedArtifacts(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	builder, err := store.Begin(ctx, cacheTestKey(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats := store.Stats(); stats.ReservedBytes != config.MaxEntryBytes || stats.BuildingEntries != 1 || stats.ReadyEntries != 0 {
		t.Fatalf("unwritten workspace was not fully reserved: %+v", stats)
	}
	first, second := []byte("first owned preview JPEG bytes"), []byte("second owned preview JPEG bytes")
	if _, err := builder.WriteTemporary(ctx, "frame-000001.jpg", cacheTestProducer(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteTemporary(ctx, "frame-000002.jpg", cacheTestProducer(second)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"240.bif", "320.bif", "400.bif"} {
		_, err := builder.WriteFile(ctx, name, func(ctx context.Context, writer io.Writer) error {
			// This open occurs inside an active output producer. The temporary
			// reference path must not require that producer's builder mutex.
			lease, err := builder.OpenTemporary(ctx, "frame-000001.jpg")
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(writer, lease)
			return errors.Join(copyErr, lease.Close())
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := builder.WriteFile(ctx, "manifest.json", cacheTestProducer([]byte(`{"widths":[240,320,400]}`))); err != nil {
		t.Fatal(err)
	}
	lease, err := builder.OpenTemporary(ctx, "frame-000002.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.DeleteTemporary(ctx, "frame-000002.jpg"); !errors.Is(err, ErrBusy) {
		t.Fatalf("deleted scratch with a reader: %v", err)
	}
	if _, err := builder.Publish(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("published with an outstanding scratch reader: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	publication, err := builder.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if publication.Entry.Bytes <= 0 || len(publication.Entry.Artifacts) != 4 {
		t.Fatalf("missing sealed content: %+v", publication.Entry)
	}
	entries, err := os.ReadDir(filepath.Join(config.Root, readyDirectory(publication.Entry.Key)))
	if err != nil {
		t.Fatal(err)
	}
	var actualBytes int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		actualBytes += info.Size()
	}
	if actualBytes != publication.Entry.Bytes {
		t.Fatalf("sealed accounting omitted disk files: declared=%d actual=%d", publication.Entry.Bytes, actualBytes)
	}
	if _, err := os.Stat(filepath.Join(config.Root, readyDirectory(publication.Entry.Key), "frame-000001.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published directory retained scratch: %v", err)
	}
	stats := store.Stats()
	if stats.ReservedBytes != 0 || stats.BuildingEntries != 0 || stats.PendingPublications != 1 || stats.ReadyBytes != publication.Entry.Bytes || stats.TotalBytes != publication.Entry.Bytes+stats.ControlBytes {
		t.Fatalf("publication reservation accounting: %+v", stats)
	}
	if err := store.Delete(ctx, publication.Entry.Key, publication.Entry.Seal); !errors.Is(err, ErrBusy) {
		t.Fatalf("DB decision window lost its pin: %v", err)
	}
	if err := store.PruneUnreferenced(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if store.Stats().ReadyEntries != 1 {
		t.Fatal("prune removed a pending database publication")
	}
	if err := publication.Keep(); err != nil {
		t.Fatal(err)
	}
	read, err := store.Acquire(ctx, publication.Entry.Key, publication.Entry.Seal, "320.bif")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(read.File)
	if err != nil || !bytes.Equal(data, first) {
		t.Fatalf("read changed bytes: %q,%v", data, err)
	}
	if _, err := read.File.Write([]byte("mutation")); err == nil {
		t.Fatal("cache descriptor was writable")
	}
	digest := sha256.Sum256(first)
	if read.Artifact.SHA256 != hex.EncodeToString(digest[:]) || read.Artifact.Size != int64(len(first)) {
		t.Fatalf("wrong content metadata: %+v", read.Artifact)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneUnreferenced(ctx, map[string]bool{publication.Entry.Key: true}); err != nil {
		t.Fatal(err)
	}
	if store.Stats().ReadyEntries != 1 {
		t.Fatal("prune removed a referenced entry")
	}
	if err := store.PruneUnreferenced(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if stats := store.Stats(); stats.TotalBytes != stats.ControlBytes || stats.ReadyEntries != 0 {
		t.Fatalf("completed cleanup retained charged bytes: %+v", stats)
	}
}

func TestCacheLRUProtectsReadersBuildersAndPublications(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	config.MaxEntries = 2
	store := cacheTestOpen(t, config)
	first := cacheTestPublish(t, store, 1, []byte("first"))
	second := cacheTestPublish(t, store, 2, []byte("second"))
	lease, err := store.Acquire(ctx, first.Key, first.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	builder, err := store.Begin(ctx, cacheTestKey(3), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(ctx, second.Key, second.Seal, "240.bif"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unpinned LRU entry remained: %v", err)
	}
	if _, err := store.Begin(ctx, cacheTestKey(4), 0); !errors.Is(err, ErrLimit) {
		t.Fatalf("admission ignored reader/build ownership: %v", err)
	}
	if err := store.Delete(ctx, first.Key, first.Seal); !errors.Is(err, ErrBusy) {
		t.Fatalf("explicit deletion ignored a reader: %v", err)
	}
	if _, err := builder.WriteFile(ctx, "240.bif", cacheTestProducer([]byte("third"))); err != nil {
		t.Fatal(err)
	}
	publication, err := builder.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(ctx, cacheTestKey(4), 0); !errors.Is(err, ErrLimit) {
		t.Fatalf("admission evicted an undecided publication: %v", err)
	}
	if err := publication.Discard(ctx); err != nil {
		t.Fatal(err)
	}
	if stats := store.Stats(); stats.ReadyEntries != 1 || stats.Readers != 1 || stats.ReadyBytes != first.Bytes {
		t.Fatalf("discard released another entry's bytes: %+v", stats)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, first.Key, first.Seal); err != nil {
		t.Fatal(err)
	}
}

func TestCacheBudgetsAreStickyEvenWhenProducerIgnoresWriteFailure(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	config.MaxFileBytes = 32
	store := cacheTestOpen(t, config)
	for _, name := range []string{"240.bif", "frame-000001.jpg"} {
		builder, err := store.Begin(ctx, cacheTestKey(10), 8192)
		if err != nil {
			t.Fatal(err)
		}
		producer := func(_ context.Context, writer io.Writer) error {
			_, _ = writer.Write(bytes.Repeat([]byte{1}, 33))
			return nil
		}
		if strings.HasSuffix(name, ".jpg") {
			_, err = builder.WriteTemporary(ctx, name, producer)
		} else {
			_, err = builder.WriteFile(ctx, name, producer)
		}
		if !errors.Is(err, ErrLimit) {
			t.Fatalf("ignored write error became success: %v", err)
		}
		if err := builder.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		if stats := store.Stats(); stats.TotalBytes != stats.ControlBytes || stats.BuildingEntries != 0 {
			t.Fatalf("failed workspace was not retired: %+v", stats)
		}
	}
}

func TestCacheTotalWorkspaceAndTemporaryCountBounds(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	config.MaxTemporaryFiles = 1
	store := cacheTestOpen(t, config)
	builder, err := store.Begin(ctx, cacheTestKey(1), 8192)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteTemporary(ctx, "frame-000001.jpg", cacheTestProducer(bytes.Repeat([]byte{1}, 3000))); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteTemporary(ctx, "frame-000002.jpg", cacheTestProducer([]byte{1})); !errors.Is(err, ErrLimit) {
		t.Fatalf("scratch count was unbounded: %v", err)
	}
	if _, err := builder.WriteFile(ctx, "240.bif", cacheTestProducer(bytes.Repeat([]byte{2}, 3000))); !errors.Is(err, ErrLimit) {
		t.Fatalf("scratch bytes were excluded from output budget: %v", err)
	}
	if err := builder.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	builder, err = store.Begin(ctx, cacheTestKey(2), 8192)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteTemporary(ctx, "frame-000001.jpg", cacheTestProducer(bytes.Repeat([]byte{1}, 3000))); err != nil {
		t.Fatal(err)
	}
	if err := builder.DeleteTemporary(ctx, "frame-000001.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteFile(ctx, "240.bif", cacheTestProducer(bytes.Repeat([]byte{2}, 3000))); err != nil {
		t.Fatalf("actually deleted scratch did not restore workspace capacity: %v", err)
	}
	publication, err := builder.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.Discard(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCacheCloseTimeoutDoesNotReleaseLiveReaderOrOwnership(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	entry := cacheTestPublish(t, store, 1, []byte("leased"))
	lease, err := store.Acquire(ctx, entry.Key, entry.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("close declared a live lease stopped: %v", err)
	}
	if stats := store.Stats(); !stats.Closing || stats.Readers != 1 || stats.ReadyBytes != entry.Bytes {
		t.Fatalf("close timeout freed live bytes: %+v", stats)
	}
	if other, err := Open(ctx, config); !errors.Is(err, ErrOwned) || other != nil {
		if other != nil {
			_ = other.Close(ctx)
		}
		t.Fatalf("timeout released writer ownership: %v", err)
	}
	if _, err := store.Begin(ctx, cacheTestKey(2), 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("close admitted a builder: %v", err)
	}
	if _, err := store.Acquire(ctx, entry.Key, entry.Seal, "240.bif"); !errors.Is(err, ErrClosed) {
		t.Fatalf("close admitted a reader: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := cacheTestOpen(t, config)
	read, err := reopened.Acquire(ctx, entry.Key, entry.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
}

func TestCacheCloseWaitsForActualProducerAndScratchLease(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	builder, err := store.Begin(ctx, cacheTestKey(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteTemporary(ctx, "frame-000001.jpg", cacheTestProducer([]byte("scratch"))); err != nil {
		t.Fatal(err)
	}
	temporary, err := builder.OpenTemporary(ctx, "frame-000001.jpg")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := builder.WriteFile(ctx, "240.bif", func(ctx context.Context, writer io.Writer) error {
			if _, err := writer.Write([]byte("partial")); err != nil {
				return err
			}
			close(started)
			<-release
			return ctx.Err()
		})
		result <- err
	}()
	<-started
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("close did not retain running writer: %v", err)
	}
	if stats := store.Stats(); stats.ReservedBytes != config.MaxEntryBytes || stats.BuildingEntries != 1 {
		t.Fatalf("running producer reservation was freed: %+v", stats)
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled producer succeeded: %v", err)
	}
	if err := builder.Abort(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("abort hid its outstanding scratch lease: %v", err)
	}
	if stats := store.Stats(); stats.ReservedBytes != config.MaxEntryBytes {
		t.Fatalf("scratch lease bytes were freed: %+v", stats)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := builder.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if stats := store.Stats(); stats.TotalBytes != stats.ControlBytes {
		t.Fatalf("completed abort retained bytes: %+v", stats)
	}
}

func TestCacheRestartRecoversReadyAndOnlyOwnedTemporaryDirectories(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	entry := cacheTestPublish(t, store, 1, []byte("persisted"))
	token := strings.Repeat("c", 32)
	directory := store.temporaryPrefix() + token
	if err := store.disk.root.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := openChild(store.disk.root, directory)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(ownerRecord{Marker: "goby-analysis-entry-v1", Owner: store.disk.owner, Key: cacheTestKey(99), Token: token})
	if err := writeControl(root, ownerName, encoded); err != nil {
		t.Fatal(err)
	}
	if err := writeControl(root, "frame-000123.jpg", []byte("interrupted scratch")); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := cacheTestOpen(t, config)
	if _, err := os.Stat(filepath.Join(config.Root, directory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned interrupted workspace remained: %v", err)
	}
	lease, err := reopened.Acquire(ctx, entry.Key, entry.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(lease.File)
	_ = lease.Close()
	if err != nil || string(data) != "persisted" {
		t.Fatalf("restart failed the ready index: %q,%v", data, err)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(config.Root, "operator-data")
	if err := os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknown, "keep"), []byte("do not remove"), 0600); err != nil {
		t.Fatal(err)
	}
	if next, err := Open(ctx, config); !errors.Is(err, ErrUnsafe) || next != nil {
		if next != nil {
			_ = next.Close(ctx)
		}
		t.Fatalf("unknown tree was admitted: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(unknown, "keep")); err != nil || string(data) != "do not remove" {
		t.Fatalf("recovery changed unknown data: %q,%v", data, err)
	}
}

func TestCacheAcquireRejectsSealTamperingPayloadMutationAndSymlinks(t *testing.T) {
	for _, kind := range []string{"wrong_expected_seal", "manifest", "payload", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			config := cacheTestConfig(t)
			store := cacheTestOpen(t, config)
			entry := cacheTestPublish(t, store, 1, []byte("original"))
			expected, want := entry.Seal, ErrUnsafe
			path := filepath.Join(config.Root, readyDirectory(entry.Key))
			switch kind {
			case "wrong_expected_seal":
				expected = strings.Repeat("b", 64)
				want = ErrSealMismatch
			case "manifest":
				data, err := os.ReadFile(filepath.Join(path, manifestName))
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, ' ')
				if err := os.WriteFile(filepath.Join(path, manifestName), data, 0600); err != nil {
					t.Fatal(err)
				}
				want = ErrSealMismatch
			case "payload":
				if err := os.WriteFile(filepath.Join(path, "240.bif"), []byte("modified"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "unrelated")
				if err := os.WriteFile(target, []byte("unrelated"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(path, "240.bif")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(path, "240.bif")); err != nil {
					t.Fatal(err)
				}
			}
			if lease, err := store.Acquire(ctx, entry.Key, expected, "240.bif"); !errors.Is(err, want) || lease != nil {
				if lease != nil {
					_ = lease.Close()
				}
				t.Fatalf("%s returned data or wrong error: %v", kind, err)
			}
			if store.Stats().Readers != 0 {
				t.Fatal("failed read retained an admission")
			}
		})
	}
}

func TestCacheRejectsNamesAndRootReplacementWithoutTouchingReplacement(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store, err := Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	entry := cacheTestPublish(t, store, 1, []byte("owned"))
	for _, key := range []string{"", "../escape", strings.Repeat("A", 64), strings.Repeat("a", 65)} {
		if _, err := store.Begin(ctx, key, 0); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid key %q admitted: %v", key, err)
		}
	}
	builder, err := store.Begin(ctx, cacheTestKey(2), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../240.bif", "sub/240.bif", ".owner.json", "241.bif"} {
		if _, err := builder.WriteFile(ctx, name, cacheTestProducer(nil)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid artifact name %q admitted: %v", name, err)
		}
	}
	for _, name := range []string{"../frame-000001.jpg", "frame-1.jpg", "frame-000001.png", ".entry.json"} {
		if _, err := builder.WriteTemporary(ctx, name, cacheTestProducer(nil)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid scratch name %q admitted: %v", name, err)
		}
	}
	if err := builder.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(config.Root, config.Root+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(config.Root, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(config.Root, "unrelated")
	if err := os.WriteFile(replacement, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(ctx, entry.Key, entry.Seal, "240.bif"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("root replacement returned data: %v", err)
	}
	if _, err := store.Begin(ctx, cacheTestKey(3), 0); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("root replacement admitted writes: %v", err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(replacement); err != nil || string(data) != "preserve" {
		t.Fatalf("replacement root changed: %q,%v", data, err)
	}
}

func TestCachePublicationDecisionSurvivesShutdownAndCanceledDiscard(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	builder, err := store.Begin(ctx, cacheTestKey(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteFile(ctx, "240.bif", cacheTestProducer([]byte("possibly committed"))); err != nil {
		t.Fatal(err)
	}
	publication, err := builder.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := publication.Discard(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discard: %v", err)
	}
	if err := store.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown discarded an unresolved database decision: %v", err)
	}
	if stats := store.Stats(); stats.PendingPublications != 1 || stats.ReadyBytes != publication.Entry.Bytes {
		t.Fatalf("pending publication lost its charged pin: %+v", stats)
	}
	if err := publication.Keep(); err != nil {
		t.Fatal(err)
	}
	if err := publication.Keep(); err != nil {
		t.Fatalf("keep was not idempotent: %v", err)
	}
	if err := publication.Discard(ctx); err != nil {
		t.Fatalf("deferred discard after keep: %v", err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := cacheTestOpen(t, config)
	lease, err := reopened.Acquire(ctx, publication.Entry.Key, publication.Entry.Seal, "240.bif")
	if err != nil {
		t.Fatalf("conservative keep lost potentially referenced bytes: %v", err)
	}
	_ = lease.Close()
	other, err := reopened.Begin(ctx, cacheTestKey(2), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.WriteFile(ctx, "240.bif", cacheTestProducer([]byte("rolled back"))); err != nil {
		t.Fatal(err)
	}
	discarded, err := other.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := discarded.Discard(ctx); err != nil {
		t.Fatal(err)
	}
	if err := discarded.Keep(); !errors.Is(err, ErrClosed) {
		t.Fatalf("discarded publication could later be kept: %v", err)
	}
}

func TestCacheFailedCleanupRetainsBytesAndUnknownContent(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	entry := cacheTestPublish(t, store, 1, []byte("known content"))
	unknownName := "unexpected.txt"
	if err := os.WriteFile(filepath.Join(config.Root, readyDirectory(entry.Key), unknownName), []byte("preserve unknown"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, entry.Key, entry.Seal); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("unknown entry content was deleted: %v", err)
	}
	if stats := store.Stats(); stats.ReadyBytes != entry.Bytes || stats.ReadyEntries != 1 {
		t.Fatalf("failed cleanup falsely released bytes: %+v", stats)
	}
	unknownPath := filepath.Join(config.Root, store.trashDirectory(entry.Key), unknownName)
	if data, err := os.ReadFile(unknownPath); err != nil || string(data) != "preserve unknown" {
		t.Fatalf("unknown content changed: %q, %v", data, err)
	}
	if err := os.Remove(unknownPath); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, entry.Key, entry.Seal); err != nil {
		t.Fatalf("owned cleanup could not be retried: %v", err)
	}
	if stats := store.Stats(); stats.TotalBytes != stats.ControlBytes {
		t.Fatalf("real removal retained bytes: %+v", stats)
	}
}

func TestCacheRestartFinishesOnlyMarkedPartialTrashCleanup(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	store := cacheTestOpen(t, config)
	entry := cacheTestPublish(t, store, 1, []byte("cleanup remainder"))
	trash := store.trashDirectory(entry.Key)
	if err := renameNoReplace(store.disk.root, readyDirectory(entry.Key), trash); err != nil {
		t.Fatal(err)
	}
	// Model a crash after deletion started: the owner marker remains while
	// other controls or artifacts may already have been unlinked.
	if err := os.Remove(filepath.Join(config.Root, trash, manifestName)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := cacheTestOpen(t, config)
	if stats := reopened.Stats(); stats.ReadyEntries != 0 || stats.TotalBytes != stats.ControlBytes {
		t.Fatalf("partial trash became a ready entry: %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(config.Root, trash)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marked partial cleanup was not completed: %v", err)
	}
}

func TestCacheGlobalReservationIncludesRootOwnershipBytes(t *testing.T) {
	ctx := context.Background()
	config := cacheTestConfig(t)
	config.MaxBytes = 2*config.MaxEntryBytes + rootMarkerBytes - 1
	store := cacheTestOpen(t, config)
	marker, err := os.Stat(filepath.Join(config.Root, diskMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if stats := store.Stats(); stats.ControlBytes != marker.Size() || stats.TotalBytes != marker.Size() {
		t.Fatalf("root ownership bytes were not charged: %+v", stats)
	}
	first, err := store.Begin(ctx, cacheTestKey(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := store.Begin(ctx, cacheTestKey(2), 0); !errors.Is(err, ErrLimit) || second != nil {
		if second != nil {
			_ = second.Abort(ctx)
		}
		t.Fatalf("reservation exceeded total budget by omitting control bytes: %v", err)
	}
	if err := first.Abort(ctx); err != nil {
		t.Fatal(err)
	}
}
