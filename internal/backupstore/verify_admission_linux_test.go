//go:build linux

package backupstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyAuditsOnceBeforeAcquiringItsReader(t *testing.T) {
	f := newStoreFixture(t)
	metadata := publishTestObject(t, f.store, []byte("verified bytes"))
	now := time.Now().UTC()
	clockCalls, callsBeforeReader := 0, 0
	f.store.now = func() time.Time {
		clockCalls++
		if len(f.store.readers) == 0 {
			callsBeforeReader++
		}
		return now.Add(time.Duration(clockCalls) * time.Second)
	}
	verified, err := f.store.Verify(context.Background(), metadata.ID, metadata.Digest, testSummary())
	if err != nil {
		t.Fatal(err)
	}
	// Inventory audits sample the clock before admission and after hashing;
	// publishing the verification summary samples it once more for UpdatedAt.
	if callsBeforeReader != 1 || clockCalls != 3 || !f.store.inventoryAt.Equal(now.Add(2*time.Second)) || !verified.UpdatedAt.Equal(now.Add(3*time.Second)) {
		t.Fatalf("unexpected verification audits: before reader=%d clocks=%d inventory=%v updated=%v", callsBeforeReader, clockCalls, f.store.inventoryAt, verified.UpdatedAt)
	}
	if got := f.store.Status(); !got.Healthy || got.Readers != 0 || !verified.Verified {
		t.Fatalf("verification did not retire its reader: %+v", got)
	}
}

func TestVerifyCancellationAfterHealthPreservesDigestPrecedence(t *testing.T) {
	for _, wrongDigest := range []bool{false, true} {
		name := "matching digest"
		if wrongDigest {
			name = "conflicting digest"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t)
			metadata := publishTestObject(t, f.store, []byte("verified bytes"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.store.now = func() time.Time { cancel(); return time.Now().UTC() }
			expected, want := metadata.Digest, context.Canceled
			if wrongDigest {
				expected, want = testDigest([]byte("different bytes")), ErrConflict
			}
			_, err := f.store.Verify(ctx, metadata.ID, expected, testSummary())
			requireError(t, err, want)
			if got := f.store.Status(); !got.Healthy || got.Readers != 0 {
				t.Fatalf("cancelled admission changed health or readers: %+v", got)
			}
			if f.store.registry.Entries[f.store.index(metadata.ID)].Metadata.Verified {
				t.Fatal("cancelled admission published a verification summary")
			}
		})
	}
}

func TestVerifyDigestConflictPrecedesReaderAndPayloadChecks(t *testing.T) {
	for _, obstacle := range []string{"reader limit", "permissions", "replacement"} {
		t.Run(obstacle, func(t *testing.T) {
			f := newStoreFixture(t)
			metadata := publishTestObject(t, f.store, []byte("verified bytes"))
			path := filepath.Join(f.config.Directory, basename(metadata.ID, true))
			wantReaders, wantError := 0, ErrUnavailable
			switch obstacle {
			case "reader limit":
				for range MaxReaders {
					reader, err := f.store.Snapshot(context.Background(), metadata.ID)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = reader.Close() })
				}
				wantReaders, wantError = MaxReaders, ErrBusy
			case "permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				replacement := filepath.Join(t.TempDir(), "replacement")
				if err := os.WriteFile(replacement, []byte("replaced bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
				wantError = ErrIntegrity
			}
			_, err := f.store.Verify(context.Background(), metadata.ID, testDigest([]byte("different bytes")), testSummary())
			requireError(t, err, ErrConflict)
			if got := f.store.Status(); !got.Healthy || got.Readers != wantReaders {
				t.Fatalf("conflicting digest opened or charged a reader: %+v", got)
			}
			_, err = f.store.Verify(context.Background(), metadata.ID, metadata.Digest, testSummary())
			requireError(t, err, wantError)
			if got := f.store.Status(); got.Readers != wantReaders || got.Healthy != (obstacle == "reader limit") {
				t.Fatalf("matching digest bypassed the remaining admission checks: %+v", got)
			}
		})
	}
}

func TestVerifyRetainsPostHashObjectCheck(t *testing.T) {
	f := newStoreFixture(t)
	metadata := publishTestObject(t, f.store, []byte("verified bytes"))
	path := filepath.Join(f.config.Directory, basename(metadata.ID, true))
	replacement := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(replacement, []byte("replaced bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var replaceErr error
	f.store.now = func() time.Time {
		calls++
		if calls == 2 {
			// Replace the named object after the pinned descriptor was hashed,
			// while the final inventory audit still holds the store mutex.
			replaceErr = os.Rename(replacement, path)
		}
		return time.Now().UTC()
	}
	_, err := f.store.Verify(context.Background(), metadata.ID, metadata.Digest, testSummary())
	if replaceErr != nil {
		t.Fatal(replaceErr)
	}
	requireError(t, err, ErrIntegrity)
	if got := f.store.Status(); got.Healthy || got.Readers != 0 {
		t.Fatalf("post-hash replacement was accepted or retained a reader: %+v", got)
	}
	requireFileBytes(t, path, []byte("replaced bytes"))
}
