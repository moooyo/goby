package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

func TestSourceMetadataSharedDomainPreserves64IndependentBodyClaims(t *testing.T) {
	registry := &originalMediaReadDomainRegistry{}
	domain := filepath.Clean(t.TempDir())
	var originals []*originalMediaReadDomainClaim
	for range 64 {
		claim, err := registry.acquire(domain)
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, claim)
	}
	alias, err := registry.acquireShared(domain)
	if err != nil || len(registry.claims) != 64 {
		t.Fatalf("metadata alias consumed a body claim: entries=%d error=%v", len(registry.claims), err)
	}
	if claim, err := registry.acquire(domain); claim != nil || !errors.Is(err, ErrBusy) {
		t.Fatal("ordinary body admission exceeded its independent 64-entry limit")
	}
	if claim, err := registry.acquireShared(filepath.Clean(t.TempDir())); claim != nil || !errors.Is(err, ErrBusy) {
		t.Fatal("a nonmatching metadata domain bypassed full registry capacity")
	}
	for _, claim := range originals {
		claim.release()
		claim.release()
	}
	if len(registry.claims) != 1 || alias.backing.refs != 1 {
		t.Fatal("original release dropped the live metadata alias")
	}
	if claim, err := registry.acquireShared(filepath.Join(domain, "nested")); claim != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal("a live alias stopped rejecting overlapping configured domains")
	}
	alias.release()
	alias.release()
	if len(registry.claims) != 0 {
		t.Fatal("the final independent alias did not retire its backing exactly once")
	}
	claim, err := registry.acquire(filepath.Join(domain, "nested"))
	if err != nil {
		t.Fatal("the retired exact domain kept rejecting a later valid configuration")
	}
	claim.release()
}

func TestSourceMetadataSharedDomainChecksEveryOverlapBeforeExactBorrow(t *testing.T) {
	registry := &originalMediaReadDomainRegistry{claims: map[*originalMediaReadDomainClaim]struct{}{}}
	domain := filepath.Clean(t.TempDir())
	exact := &originalMediaReadDomainClaim{registry: registry, domain: domain, refs: 1}
	conflicting := &originalMediaReadDomainClaim{registry: registry, domain: filepath.Join(domain, "nested"), refs: 1}
	registry.claims[exact], registry.claims[conflicting] = struct{}{}, struct{}{}
	if claim, err := registry.acquireShared(domain); claim != nil || !errors.Is(err, ErrUnavailable) || exact.refs != 1 {
		t.Fatal("an exact match borrowed before completing overlap validation")
	}
}

func TestSourceMetadataWarmCleanupRetainsPinThroughCanonicalOverlap(t *testing.T) {
	domain := filepath.Clean(t.TempDir())
	approved, err := os.OpenRoot(domain)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{}
	var warm *warmMediaSourceRoot
	var reservation *sourceMetadataReservation
	var conflict *originalMediaReadDomainClaim
	var result chan error
	var rootJoined, workerJoined chan struct{}
	var registryLocked, received bool
	t.Cleanup(func() {
		if registryLocked {
			originalMediaReadDomains.mu.Unlock()
			registryLocked = false
		}
		conflict.release()
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		if result != nil && !received {
			select {
			case err := <-result:
				received = true
				if err != nil {
					t.Errorf("join warm metadata cleanup after removing overlap: %v", err)
				}
			case <-cleanupCtx.Done():
				t.Error("warm metadata cleanup did not join after its conflict was removed")
			}
		}
		// A timed-out join cannot prove that this helper stopped using the pin.
		// Keep its original worker reference until the helper has actually joined.
		if warm != nil && (result == nil || received) {
			if err := warm.releaseChecked(); err != nil {
				t.Errorf("release failed-test warm root pin: %v", err)
			}
		} else if warm == nil {
			_ = approved.Close()
		}
		if reservation != nil && (result == nil || received) {
			reservation.finishUnused()
		}
		for name, joined := range map[string]chan struct{}{"root closure": rootJoined, "source worker": workerJoined} {
			if joined == nil {
				continue
			}
			select {
			case <-joined:
			case <-time.After(time.Second):
				t.Errorf("failed-test %s did not join", name)
			}
		}
	})

	root := libraryRoot{id: "warm-cleanup-overlap", libraryID: "warm-cleanup-library",
		path: domain, allowedPath: domain, relativePath: "."}
	store.mu.Lock()
	reference := store.borrowRootAnchorLocked(approved)
	store.retireRootAnchorLocked(approved)
	warm = &warmMediaSourceRoot{store: store, root: root, reference: reference}
	store.mu.Unlock()
	rootJoined = make(chan struct{})
	go func() { store.rootClosures.Wait(); close(rootJoined) }()
	work, finishWorker, err := store.beginMediaSourceLifetime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	reservation = &sourceMetadataReservation{work: work, refs: 1, finish: func() {
		finishWorker()
		close(finished)
	}}
	reservation.setRoutes(map[string]primaryRootIORoute{root.id: {
		route:  primaryio.Route{Roots: []primaryio.RootKey{{Catalog: "warm-cleanup-catalog", RootID: root.id}}, Domains: []string{domain}},
		domain: domain,
	}})
	workerJoined = make(chan struct{})
	go func() { store.mediaSourceOwners.owners.Wait(); close(workerJoined) }()
	claimCount := func() int {
		originalMediaReadDomains.mu.Lock()
		defer originalMediaReadDomains.mu.Unlock()
		return len(originalMediaReadDomains.claims)
	}
	beforeClaims := claimCount()
	beforeOwners := sourceMetadataReadOwners.Stats()
	beforeIO := originalMediaReadGovernor.Stats()
	conflict, err = originalMediaReadDomains.acquire(filepath.Join(domain, "nested"))
	if err != nil {
		t.Fatalf("retain conflicting canonical domain: %v", err)
	}
	if claim, err := originalMediaReadDomains.acquireShared(domain); claim != nil || !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrBusy) {
		claim.release()
		t.Fatalf("canonical overlap did not supply a non-capacity factory rejection: claim=%v error=%v", claim, err)
	}

	waitFor := func(description string, ready func() bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for !ready() {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s: metadata=%+v", description, sourceMetadataReadOwners.Stats())
			case <-ticker.C:
			}
		}
	}
	// Registration precedes the blocked canonical-domain claim, giving an
	// observable first factory attempt without replacing any production budget.
	originalMediaReadDomains.mu.Lock()
	registryLocked = true
	result = make(chan error, 1)
	go func() { result <- closeSourceMetadataWarm(context.Background(), reservation, warm) }()
	waitFor("the first registered warm cleanup factory attempt", func() bool {
		return sourceMetadataReadOwners.Stats().RegisteredOwners == beforeOwners.RegisteredOwners+1
	})
	originalMediaReadDomains.mu.Unlock()
	registryLocked = false
	waitFor("the rejected factory attempt to retire its registration", func() bool {
		return sourceMetadataReadOwners.Stats() == beforeOwners
	})
	// Block the next claim attempt as well. Seeing another live registration
	// proves that a non-capacity rejection retried while retaining the exact pin.
	originalMediaReadDomains.mu.Lock()
	registryLocked = true
	waitFor("the next registered cleanup attempt after canonical rejection", func() bool {
		return sourceMetadataReadOwners.Stats().RegisteredOwners == beforeOwners.RegisteredOwners+1
	})
	select {
	case err := <-result:
		received = true
		t.Fatalf("warm cleanup abandoned its held pin after canonical overlap: %v", err)
	default:
	}
	store.mu.Lock()
	released, borrowed, retired, mapped := warm.released, reference.borrowed, reference.retired, store.rootAnchorReferences[approved] == reference
	store.mu.Unlock()
	if released || borrowed != 1 || !retired || !mapped {
		t.Fatalf("rejected cleanup dropped its actual retired anchor pin: released=%v borrowed=%d retired=%v mapped=%v", released, borrowed, retired, mapped)
	}
	reservation.mu.Lock()
	refs := reservation.refs
	reservation.mu.Unlock()
	if refs < 2 {
		t.Fatal("retried cleanup did not retain both its actual worker and attempt references")
	}
	for name, done := range map[string]<-chan struct{}{"reservation finish": finished, "root closure": rootJoined, "source worker": workerJoined} {
		select {
		case <-done:
			t.Fatalf("canonical overlap completed %s before actual root cleanup", name)
		default:
		}
	}
	if _, err := approved.Stat("."); err != nil {
		t.Fatalf("canonical overlap closed the actually pinned root: %v", err)
	}

	originalMediaReadDomains.mu.Unlock()
	registryLocked = false
	conflict.release()
	select {
	case err := <-result:
		received = true
		if err != nil {
			t.Fatalf("warm cleanup did not recover after canonical overlap retired: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("warm cleanup did not join after canonical overlap retired")
	}
	if _, err := approved.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("successful admitted cleanup returned before actual root closure: %v", err)
	}
	store.mu.Lock()
	released, borrowed, mapped = warm.released, reference.borrowed, store.rootAnchorReferences[approved] == reference
	store.mu.Unlock()
	if !released || borrowed != 0 || mapped {
		t.Fatalf("successful cleanup retained its retired root reference: released=%v borrowed=%d mapped=%v", released, borrowed, mapped)
	}
	select {
	case <-finished:
		t.Fatal("cleanup capability completed the original source-worker lifetime")
	default:
	}
	reservation.finishUnused()
	for name, done := range map[string]<-chan struct{}{"reservation finish": finished, "root closure": rootJoined, "source worker": workerJoined} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("actual warm cleanup did not join %s", name)
		}
	}
	if stats := sourceMetadataReadOwners.Stats(); stats != beforeOwners {
		t.Fatalf("recovered warm cleanup retained metadata ownership: before=%+v after=%+v", beforeOwners, stats)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
		t.Fatalf("recovered warm cleanup retained actual I/O admission: before=%+v after=%+v", beforeIO, stats)
	}
	if count := claimCount(); count != beforeClaims {
		t.Fatalf("recovered warm cleanup retained canonical claims: before=%d after=%d", beforeClaims, count)
	}
}
