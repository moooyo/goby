//go:build linux

package analysiscache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheMissingDeclaredPayloadIsAChargedMissAndCanBeReclaimed(t *testing.T) {
	ctx := context.Background()
	configuration := cacheTestConfig(t)
	store := cacheTestOpen(t, configuration)
	entry := cacheTestPublish(t, store, 1, []byte("disposable derivative"))
	artifact := entry.Artifacts[0]
	if !store.HasArtifact(entry.Key, entry.Seal, artifact.Name, artifact.SHA256, artifact.Size) {
		t.Fatal("sealed payload was not registered before deletion")
	}
	if err := os.Remove(filepath.Join(configuration.Root, readyDirectory(entry.Key), artifact.Name)); err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(ctx, entry.Key, entry.Seal, artifact.Name)
	if lease != nil || !errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsafe) {
		t.Fatalf("an owned missing derivative was confused with missing authority: %v", err)
	}
	if store.HasArtifact(entry.Key, entry.Seal, artifact.Name, artifact.SHA256, artifact.Size) || store.Stats().ReadyBytes != entry.Bytes {
		t.Fatal("missing data remained advertised or unretired generation ownership was released")
	}
	result, err := store.PruneUnreferencedResult(ctx, nil)
	if err != nil || result.RemovedEntries != 1 || result.RemovedBytes != entry.Bytes || store.Stats().ReadyBytes != 0 {
		t.Fatalf("a verified partial generation was not reclaimed: %+v, %v", result, err)
	}
	cacheTestPublish(t, store, 2, []byte("replacement derivative"))
}

func TestCacheRestartReclaimsOnlyIntactOwnershipWithMissingDeclaredPayload(t *testing.T) {
	ctx := context.Background()
	configuration := cacheTestConfig(t)
	store := cacheTestOpen(t, configuration)
	entry := cacheTestPublish(t, store, 1, []byte("disposable derivative"))
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(configuration.Root, readyDirectory(entry.Key), "240.bif")); err != nil {
		t.Fatal(err)
	}
	reopened := cacheTestOpen(t, configuration)
	if reopened.Stats().ReadyEntries != 0 || reopened.Stats().TotalBytes != reopened.Stats().ControlBytes {
		t.Fatal("startup fabricated a ready generation for an absent declared payload")
	}
	cacheTestPublish(t, reopened, 2, []byte("replacement derivative"))
}

func TestCacheMissingReadyNameDoesNotReleaseAnExistingTrashGeneration(t *testing.T) {
	ctx := context.Background()
	configuration := cacheTestConfig(t)
	store := cacheTestOpen(t, configuration)
	entry := cacheTestPublish(t, store, 1, []byte("still-owned bytes after an uncertain move"))
	ready := filepath.Join(configuration.Root, readyDirectory(entry.Key))
	trash := filepath.Join(configuration.Root, store.trashDirectory(entry.Key))
	if err := os.Rename(ready, trash); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, entry.Key, entry.Seal); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("an absent old name concealed an extant trash generation: %v", err)
	}
	if store.Stats().ReadyBytes != entry.Bytes || store.Stats().ReadyEntries != 1 {
		t.Fatal("uncertain rename released bytes that still occupy the owned root")
	}
	if _, err := os.Stat(trash); err != nil {
		t.Fatal("unresolved cleanup lost its retained directory")
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := cacheTestOpen(t, configuration)
	if reopened.Stats().ReadyEntries != 0 || reopened.Stats().TotalBytes != reopened.Stats().ControlBytes {
		t.Fatal("fresh owned recovery did not finish the retained trash cleanup")
	}
}

func TestCacheMissingControlProofOrUnexpectedContentIsNeverADisposableMiss(t *testing.T) {
	for _, scenario := range []string{"owner", "sealed_manifest", "seal", "unknown_payload"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			configuration := cacheTestConfig(t)
			store := cacheTestOpen(t, configuration)
			entry := cacheTestPublish(t, store, 1, []byte("preserved derivative"))
			directory := filepath.Join(configuration.Root, readyDirectory(entry.Key))
			name := map[string]string{"owner": ownerName, "sealed_manifest": manifestName, "seal": sealName}[scenario]
			if scenario == "unknown_payload" {
				if err := os.Remove(filepath.Join(directory, "240.bif")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "operator-notes.txt"), []byte("preserve unknown content"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
			lease, err := store.Acquire(ctx, entry.Key, entry.Seal, "240.bif")
			if lease != nil || !errors.Is(err, ErrUnsafe) || errors.Is(err, ErrNotFound) {
				t.Fatalf("lost control or unknown content was treated as a regenerable miss: %v", err)
			}
			if err := store.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if recovered, err := Open(ctx, configuration); err == nil {
				_ = recovered.Close(ctx)
				t.Fatal("startup discarded an entry without complete ownership proof")
			}
			if _, err := os.Stat(directory); err != nil {
				t.Fatal("failed recovery did not preserve the untrusted directory")
			}
		})
	}
}
