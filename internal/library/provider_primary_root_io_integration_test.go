//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/providers"
)

func TestProviderSubtitleIdleRootIOClosesDuringStoreShutdown(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := ownedSubtitleManagementActor(t, fixture, "provider-root-io-shutdown")
	snapshot, err := fixture.store.readProviderSubtitleSnapshot(fixture.ctx, &actor, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := fixture.store.prepareProviderSubtitle(fixture.ctx, &actor, snapshot, primaryio.Foreground,
		"provider-shutdown:1", "en", "srt", false, false, []byte(subtitleTestSRT))
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	primary, parent, root, anchor := prepared.primary, prepared.parent, prepared.root, prepared.lease.approved
	stagePath := filepath.Join(filepath.Dir(fixture.path), prepared.stageName)
	stageInfo, err := os.Stat(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	owner := prepared.primaryIO.Context()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- fixture.store.Close(shutdown) }()
	select {
	case <-owner.Done():
	case err := <-finished:
		t.Fatalf("Store shutdown completed before retained provider resources: %v", err)
	case <-shutdown.Done():
		t.Fatal("Store shutdown did not cancel the retained provider operation")
	}
	prepared.close()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("idle provider descriptors prevented Store shutdown: %v", err)
		}
	case <-shutdown.Done():
		t.Fatal("Store shutdown did not join provider descriptor retirement")
	}
	if prepared.primaryIO != nil || prepared.retirementFailure.Load() != nil {
		t.Fatal("ordinary shutdown quarantined the idle provider operation")
	}
	if _, err := primary.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("primary descriptor was not retired: %v", err)
	}
	for name, held := range map[string]*os.Root{"parent": parent, "root": root, "anchor": anchor} {
		if _, err := held.Stat("."); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("%s descriptor was not retired: %v", name, err)
		}
	}
	current, err := os.Stat(stagePath)
	if err != nil || !os.SameFile(stageInfo, current) {
		t.Fatalf("shutdown changed a private staging name without admission: %v", err)
	}
}

func TestProviderSubtitleFinalCapacityBusyRetriesAfterRollback(t *testing.T) {
	for _, changeSource := range []bool{false, true} {
		name := "same-source"
		if changeSource {
			name = "changed-binding"
		}
		t.Run(name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			work, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			snapshot, err := fixture.store.readProviderSubtitleSnapshot(work, nil, fixture.item.ID)
			if err != nil {
				t.Fatal(err)
			}
			route, err := fixture.store.primaryReadRoute(mediaSourceRootHint{root: snapshot.primary.root, bindingRevision: snapshot.bindingRevision})
			if err != nil {
				t.Fatal(err)
			}
			var owners []*primaryio.Owner
			var leases []*primaryio.PrimaryReadLease
			var leasesMu sync.Mutex
			var releaseOnce sync.Once
			release := func() {
				releaseOnce.Do(func() {
					leasesMu.Lock()
					defer leasesMu.Unlock()
					for _, lease := range leases {
						_ = lease.Release()
					}
					for _, owner := range owners {
						_ = owner.Complete()
					}
				})
			}
			defer release()
			for range 2 {
				owner, err := originalMediaReadOwners.Register(work)
				if err != nil {
					t.Fatal(err)
				}
				owners = append(owners, owner)
			}
			saturated := make(chan struct{}, 1)
			finished := make(chan error, 1)
			attempts := 0
			var previous *os.File
			publish := func(ctx context.Context, actor *identity.Principal, prepared *preparedProviderSubtitle, download providers.SubtitleDownload) error {
				attempts++
				if attempts > 1 {
					if _, err := previous.Stat(); !errors.Is(err, os.ErrClosed) {
						return fmt.Errorf("previous provider descriptor was not retired before retry: %w", err)
					}
					return fixture.store.publishProviderSubtitle(ctx, actor, prepared, download)
				}
				previous = prepared.primary
				return fixture.store.publishProviderSubtitleBeforeFinal(ctx, actor, prepared, download, func() error {
					leasesMu.Lock()
					defer leasesMu.Unlock()
					for _, owner := range owners {
						lease, err := owner.TryAcquire(route, primaryio.Background)
						if err != nil {
							return err
						}
						leases = append(leases, lease)
					}
					saturated <- struct{}{}
					return nil
				})
			}
			download := providers.SubtitleDownload{Provider: "opensubtitles", RemoteID: "provider-capacity:1", Language: "en", Format: "srt", Data: []byte(subtitleTestSRT)}
			go func() {
				finished <- fixture.store.registerProviderSubtitleSnapshot(work, nil, snapshot, download, "en", "srt", download.Data, publish)
			}()
			select {
			case <-saturated:
			case err := <-finished:
				t.Fatalf("provider attempt did not reach real final-admission contention: %v", err)
			case <-work.Done():
				t.Fatal("provider attempt did not reach final admission")
			}
			// Capacity remains occupied. Catalog ownership must already be
			// released while descriptor cleanup queues outside the transaction.
			check, checkCancel := context.WithTimeout(work, 2*time.Second)
			tick := time.NewTicker(time.Millisecond)
			for !fixture.store.ownership.mu.TryLock() {
				select {
				case <-tick.C:
				case <-check.Done():
					tick.Stop()
					checkCancel()
					release()
					t.Fatal("provider capacity wait retained catalog ownership")
				}
			}
			fixture.store.ownership.mu.Unlock()
			tick.Stop()
			tx, err := fixture.store.beginOwnedTx(check)
			if err != nil {
				checkCancel()
				t.Fatalf("provider capacity wait retained catalog ownership: %v", err)
			}
			var rows, provenance int
			err = tx.QueryRow(work, `SELECT
				(SELECT count(*) FROM item_subtitles WHERE item_id=$1),
				(SELECT count(*) FROM item_subtitle_provider_sources WHERE item_id=$1)`, fixture.item.ID).Scan(&rows, &provenance)
			rollback(tx)
			checkCancel()
			if err != nil || rows != 0 || provenance != 0 {
				t.Fatalf("busy final admission did not roll back catalog rows: subtitles=%d provenance=%d error=%v", rows, provenance, err)
			}
			if changeSource {
				if _, err := fixture.pool.Exec(work, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, snapshot.primary.root.id); err != nil {
					t.Fatal(err)
				}
			}
			release()
			select {
			case err := <-finished:
				if changeSource {
					if !errors.Is(err, ErrSourceChanged) || attempts != 1 {
						t.Fatalf("changed source was retried: attempts=%d error=%v", attempts, err)
					}
					return
				}
				if err != nil || attempts != 2 {
					t.Fatalf("same-source capacity retry did not complete: attempts=%d error=%v", attempts, err)
				}
			case <-work.Done():
				t.Fatal("provider did not complete after capacity was released")
			}
			var digest, filename string
			err = fixture.pool.QueryRow(work, `SELECT count(*), count(p.provider_id), min(s.source_hash), min(s.relative_path)
				FROM item_subtitles s LEFT JOIN item_subtitle_provider_sources p
				ON p.item_id=s.item_id AND p.stream_index=s.stream_index WHERE s.item_id=$1`, fixture.item.ID).
				Scan(&rows, &provenance, &digest, &filename)
			expectedDigest := sha256.Sum256(download.Data)
			if err != nil || rows != 1 || provenance != 1 || digest != hex.EncodeToString(expectedDigest[:]) {
				t.Fatalf("retry duplicated or lost provider identity: subtitles=%d provenance=%d digest=%q error=%v", rows, provenance, digest, err)
			}
			data, err := os.ReadFile(filepath.Join(snapshot.primary.root.path, filepath.FromSlash(filename)))
			if err != nil || string(data) != subtitleTestSRT {
				t.Fatalf("retried sidecar payload changed: %v", err)
			}
		})
	}
}
