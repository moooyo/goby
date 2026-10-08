//go:build linux

package backupstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteBusyDefersInventoryUntilMutation(t *testing.T) {
	for _, reference := range []string{"writer", "snapshot", "protection"} {
		t.Run(reference, func(t *testing.T) {
			f := newStoreFixture(t)
			s := f.store
			ctx := context.Background()
			data := []byte("retained opaque bytes")
			var metadata Metadata
			var release func()
			finalName := reference != "writer"
			if reference == "writer" {
				w := beginTestWriter(t, s, ctx)
				prepareTestWriter(t, w, data)
				metadata = w.Metadata()
				release = func() {
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				metadata = publishTestObject(t, s, data)
				if reference == "snapshot" {
					snapshot, err := s.Snapshot(ctx, metadata.ID)
					if err != nil {
						t.Fatal(err)
					}
					release = func() {
						if err := snapshot.Close(); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					var err error
					release, err = s.Protect(metadata.ID)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			foreign := filepath.Join(f.config.Directory, "unknown-during-delete")
			foreignBytes := []byte("unregistered bytes")
			if err := os.WriteFile(foreign, foreignBytes, 0600); err != nil {
				t.Fatal(err)
			}
			catalogPath := filepath.Join(f.config.Directory, catalogName)
			before, err := os.ReadFile(catalogPath)
			if err != nil {
				t.Fatal(err)
			}
			inventoryAt, inventoryBytes := s.inventoryAt, s.inventoryBytes
			for range 4 {
				requireError(t, s.Delete(ctx, metadata.ID, metadata.Digest), ErrBusy)
			}
			requireError(t, s.Delete(ctx, metadata.ID, testDigest([]byte("wrong digest"))), ErrConflict)
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			requireError(t, s.Delete(cancelled, metadata.ID, metadata.Digest), context.Canceled)
			if !s.Status().Healthy || s.inventoryAt != inventoryAt || s.inventoryBytes != inventoryBytes {
				t.Fatal("busy, conflicting, or cancelled deletion changed inventory health")
			}
			requireFileBytes(t, catalogPath, before)
			if reference == "writer" {
				// Writer retirement has its own full audit, so let it complete
				// before restoring the unknown entry at the deletion boundary.
				if err := os.Remove(foreign); err != nil {
					t.Fatal(err)
				}
			}
			release()
			if reference == "writer" {
				if err := os.WriteFile(foreign, foreignBytes, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err = os.ReadFile(catalogPath)
			if err != nil {
				t.Fatal(err)
			}
			requireError(t, s.Delete(ctx, metadata.ID, metadata.Digest), ErrUnavailable)
			i := s.index(metadata.ID)
			if s.Status().Healthy || i < 0 || s.registry.Entries[i].Deleting {
				t.Fatal("deletion did not audit inventory before persisting its intent")
			}
			requireFileBytes(t, catalogPath, before)
			requireFileBytes(t, foreign, foreignBytes)
			requireFileBytes(t, filepath.Join(f.config.Directory, basename(metadata.ID, finalName)), data)
		})
	}
}

func TestDeleteBusyChecksFixedIdentitiesAndAvailability(t *testing.T) {
	for _, role := range []string{"root", markerName, lockName, catalogName, "closed", "degraded"} {
		t.Run(role, func(t *testing.T) {
			f := newStoreFixture(t)
			metadata := publishTestObject(t, f.store, []byte("retained opaque bytes"))
			release, err := f.store.Protect(metadata.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			switch role {
			case "root":
				if err := os.Chmod(f.config.Directory, 0755); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(f.config.Directory, 0700) })
			case "closed":
				if err := f.store.Close(); err != nil {
					t.Fatal(err)
				}
			case "degraded":
				f.store.markDegraded()
			default:
				path := filepath.Join(f.config.Directory, role)
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				replacement := filepath.Join(t.TempDir(), "replacement")
				if err := os.WriteFile(replacement, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			requireError(t, f.store.Delete(context.Background(), metadata.ID, metadata.Digest), ErrUnavailable)
			i := f.store.index(metadata.ID)
			if f.store.Status().Healthy || i < 0 || f.store.registry.Entries[i].Deleting {
				t.Fatal("busy deletion bypassed a fixed identity or availability guard")
			}
		})
	}
}

func TestDeleteBusyDoesNotOpenPayloadAndPreservesExactUnlink(t *testing.T) {
	for _, attack := range []string{"replacement", "symlink"} {
		t.Run(attack, func(t *testing.T) {
			f := newStoreFixture(t)
			metadata := publishTestObject(t, f.store, []byte("original bytes"))
			release, err := f.store.Protect(metadata.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			path := filepath.Join(f.config.Directory, basename(metadata.ID, true))
			replacement := filepath.Join(t.TempDir(), "replacement")
			replacementBytes := []byte("replacement bytes")
			if err := os.WriteFile(replacement, replacementBytes, 0600); err != nil {
				t.Fatal(err)
			}
			if attack == "replacement" {
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			requireError(t, f.store.Delete(context.Background(), metadata.ID, metadata.Digest), ErrBusy)
			if !f.store.Status().Healthy {
				t.Fatal("busy deletion inspected the payload")
			}
			release()
			requireError(t, f.store.Delete(context.Background(), metadata.ID, metadata.Digest), ErrUnavailable)
			i := f.store.index(metadata.ID)
			if f.store.Status().Healthy || i < 0 || !f.store.registry.Entries[i].Deleting {
				t.Fatal("exact unlink did not retain the persisted deletion intent")
			}
			requireFileBytes(t, path, replacementBytes)
		})
	}
}

func TestDeleteKeepsPinnedRootAfterDirectoryReplacement(t *testing.T) {
	f := newStoreFixture(t)
	metadata := publishTestObject(t, f.store, []byte("original bytes"))
	release, err := f.store.Protect(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	original := filepath.Join(filepath.Dir(f.config.Directory), "pinned-store")
	if err := os.Rename(f.config.Directory, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.config.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(f.config.Directory, basename(metadata.ID, true))
	replacementBytes := []byte("unowned replacement bytes")
	if err := os.WriteFile(replacement, replacementBytes, 0600); err != nil {
		t.Fatal(err)
	}
	requireError(t, f.store.Delete(context.Background(), metadata.ID, metadata.Digest), ErrBusy)
	release()
	if err := f.store.Delete(context.Background(), metadata.ID, metadata.Digest); err != nil {
		t.Fatal(err)
	}
	requireMissing(t, filepath.Join(original, basename(metadata.ID, true)))
	requireFileBytes(t, replacement, replacementBytes)
}
