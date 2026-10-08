//go:build linux

package backupstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func streamingStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	f := newStoreFixture(t, func(cfg *Config) {
		cfg.MaxObjectBytes = streamInventoryBytes + transferChunk
		cfg.MaxTotalBytes = 2 * cfg.MaxObjectBytes
	})
	now := time.Now()
	f.store.now = func() time.Time { return now }
	return f
}

func addUnknownStreamingFile(t *testing.T, f *storeFixture) string {
	t.Helper()
	path := filepath.Join(f.config.Directory, "unregistered-streaming-file")
	if err := os.WriteFile(path, []byte("foreign bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriterInventoryByteBudgetIsSharedAcrossCallsAndWriters(t *testing.T) {
	f := streamingStoreFixture(t)
	writers := []*Writer{
		beginTestWriter(t, f.store, context.Background()),
		beginTestWriter(t, f.store, context.Background()),
	}
	foreign := addUnknownStreamingFile(t, f)
	chunk := make([]byte, transferChunk)
	for i := 0; i < streamInventoryBytes/len(chunk); i++ {
		writeTestBytes(t, writers[i%len(writers)], chunk)
	}
	if got := f.store.Status(); !got.Healthy || got.Bytes != streamInventoryBytes {
		t.Fatalf("writes before the shared inventory checkpoint: %+v", got)
	}
	if n, err := writers[0].Write([]byte("x")); n != 0 || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("write after the shared inventory budget = (%d, %v)", n, err)
	}
	if got := f.store.Status(); got.Healthy || got.Bytes != streamInventoryBytes {
		t.Fatalf("inventory rejection changed accepted bytes or stayed healthy: %+v", got)
	}
	requireFileBytes(t, foreign, []byte("foreign bytes"))
}

func TestWriterInventoryTimeBudgetAuditsBeforeNextChunk(t *testing.T) {
	f := streamingStoreFixture(t)
	now := f.store.now()
	f.store.now = func() time.Time { return now }
	w := beginTestWriter(t, f.store, context.Background())
	foreign := addUnknownStreamingFile(t, f)
	now = now.Add(streamInventoryInterval - time.Nanosecond)
	data := make([]byte, 2*transferChunk)
	writeTestBytes(t, w, data)
	now = now.Add(time.Nanosecond)
	if n, err := w.Write([]byte("x")); n != 0 || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("write at the inventory deadline = (%d, %v)", n, err)
	}
	if got := f.store.Status(); got.Healthy || got.Bytes != int64(len(data)) {
		t.Fatalf("timed inventory rejection: %+v", got)
	}
	requireFileBytes(t, foreign, []byte("foreign bytes"))
}

func TestWriterInventoryAuditsAtLifecycleAndManagementBoundaries(t *testing.T) {
	for _, boundary := range []string{"Prepare", "Publish", "List"} {
		t.Run(boundary, func(t *testing.T) {
			f := streamingStoreFixture(t)
			w := beginTestWriter(t, f.store, context.Background())
			data := bytes.Repeat([]byte("x"), 2*transferChunk)
			var proof Prepared
			if boundary == "Publish" {
				proof = prepareTestWriter(t, w, data)
			}
			foreign := addUnknownStreamingFile(t, f)
			if boundary != "Publish" {
				// A single multi-chunk Write must not enumerate the directory on
				// each chunk while both shared inventory budgets remain available.
				writeTestBytes(t, w, data)
			}
			var err error
			switch boundary {
			case "Prepare":
				_, err = w.Prepare(context.Background())
			case "Publish":
				_, err = w.Publish(context.Background(), proof, nil)
			case "List":
				_, err = f.store.List(context.Background(), 0, 1)
			}
			requireError(t, err, ErrUnavailable)
			if f.store.Status().Healthy {
				t.Fatal("boundary did not reject an unknown directory entry")
			}
			requireFileBytes(t, foreign, []byte("foreign bytes"))
			requireFileBytes(t, filepath.Join(f.config.Directory, basename(w.id, false)), data)
			requireMissing(t, filepath.Join(f.config.Directory, basename(w.id, true)))
		})
	}
}

func TestWriterChecksFixedIdentitiesOnEveryChunk(t *testing.T) {
	for _, role := range []string{"root", markerName, lockName, catalogName, "writer registration"} {
		t.Run(role, func(t *testing.T) {
			f := streamingStoreFixture(t)
			w := beginTestWriter(t, f.store, context.Background())
			t.Cleanup(func() { _ = w.Close() })
			calls := 0
			var mutationErr error
			f.store.freeBytes = func(int) (uint64, error) {
				calls++
				if calls == 1 {
					// Change the next chunk's guards after this chunk's admission
					// checks, without advancing either inventory audit budget.
					switch role {
					case "root":
						if err := os.Chmod(f.config.Directory, 0755); err != nil {
							mutationErr = err
							return 0, err
						}
						t.Cleanup(func() { _ = os.Chmod(f.config.Directory, 0700) })
					case "writer registration":
						delete(f.store.writers, w.id)
					default:
						path := filepath.Join(f.config.Directory, role)
						original, err := os.ReadFile(path)
						if err != nil {
							mutationErr = err
							return 0, err
						}
						replacement := filepath.Join(f.config.Directory, "replacement")
						if err := os.WriteFile(replacement, original, 0600); err != nil {
							mutationErr = err
							return 0, err
						}
						if err := os.Rename(replacement, path); err != nil {
							mutationErr = err
							return 0, err
						}
					}
				}
				return uint64(metadataReserve) + uint64(f.config.MinFreeBytes) + uint64(f.config.MaxTotalBytes), nil
			}
			n, err := w.Write(make([]byte, 2*transferChunk))
			if mutationErr != nil {
				t.Fatalf("mutate %s: %v", role, mutationErr)
			}
			if n != transferChunk || !errors.Is(err, ErrUnavailable) {
				t.Fatalf("write after %s changed = (%d, %v)", role, n, err)
			}
			if calls != 1 || w.Metadata().Size != transferChunk {
				t.Fatalf("guard was not checked before the second chunk: calls=%d size=%d", calls, w.Metadata().Size)
			}
		})
	}
}

func TestWriterChecksFreeSpaceAndCancellationOnEveryChunk(t *testing.T) {
	for _, cancelWrite := range []bool{false, true} {
		name := "free space"
		if cancelWrite {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			f := streamingStoreFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := beginTestWriter(t, f.store, ctx)
			calls := 0
			reserve := uint64(metadataReserve) + uint64(f.config.MinFreeBytes)
			f.store.freeBytes = func(int) (uint64, error) {
				calls++
				if calls == 1 {
					if cancelWrite {
						cancel()
					}
					return reserve + transferChunk, nil
				}
				return reserve, nil
			}
			wantErr, wantCalls := ErrQuota, 2
			if cancelWrite {
				wantErr, wantCalls = context.Canceled, 1
			}
			if n, err := w.Write(make([]byte, 2*transferChunk)); n != transferChunk || !errors.Is(err, wantErr) {
				t.Fatalf("write after %s changed = (%d, %v), want one chunk and %v", name, n, err, wantErr)
			}
			if calls != wantCalls {
				t.Fatalf("free-space admissions = %d, want %d", calls, wantCalls)
			}
		})
	}
}
