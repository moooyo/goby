//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

func TestOriginalMediaReadRechecksAuthorityAfterOpenQueue(t *testing.T) {
	for _, test := range []struct {
		name, statement string
		want            error
	}{
		{"disabled-user", "UPDATE users SET is_disabled=true WHERE id=$1", ErrForbidden},
		{"playback-revoked", `UPDATE users SET policy='{"EnableAllFolders":true,"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, ErrForbidden},
		{"library-revoked", `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			fixture.ctx = ctx
			snapshot := primaryReadTestSnapshot(t, fixture)
			_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			releases := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
			beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
			call := primaryReadTestStartOpen(t, ctx, fixture, snapshot.ETag)
			mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
			if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+1 {
				t.Fatalf("queued reopen did not retain its consumer owner: before=%d after=%d", beforeOwners, owners)
			}
			if _, err := fixture.pool.Exec(ctx, test.statement, fixture.userID); err != nil {
				t.Fatalf("revoke queued source authority: %v", err)
			}
			// Missing storage makes the authorization error prove that the queued
			// reopen checked current authority before touching the source path.
			if err := os.Remove(fixture.path); err != nil {
				t.Fatal(err)
			}
			for _, release := range releases {
				release()
			}
			result := primaryReadTestReceiveOpen(t, ctx, call)
			primaryReadTestOpenFailure(t, result, test.want)
			primaryReadTestWaitOwners(t, ctx, beforeOwners)
		})
	}
}

func TestOriginalMediaReadRejectsQueuedBindingRevisionBeforeIO(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	fixture.ctx = ctx
	snapshot := primaryReadTestSnapshot(t, fixture)
	_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	releases := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	call := primaryReadTestStartOpen(t, ctx, fixture, snapshot.ETag)
	mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+1 {
		t.Fatalf("queued binding check did not retain its consumer owner: before=%d after=%d", beforeOwners, owners)
	}
	if _, err := fixture.pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", root.id); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.path); err != nil {
		t.Fatal(err)
	}
	for _, release := range releases {
		release()
	}
	result := primaryReadTestReceiveOpen(t, ctx, call)
	primaryReadTestOpenFailure(t, result, ErrUnavailable)
	if !errors.Is(result.err, ErrSourceChanged) || !strings.Contains(result.err.Error(), "while queued") {
		t.Fatalf("queued binding change reached missing storage or reused its route: %v", result.err)
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
}

func TestOriginalMediaReadRejectsExpectedSnapshotMismatch(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	fixture.ctx = ctx
	snapshot := primaryReadTestSnapshot(t, fixture)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	staleETag := strings.TrimSuffix(snapshot.ETag, `"`) + `.stale"`
	call := primaryReadTestStartOpen(t, ctx, fixture, staleETag)
	result := primaryReadTestReceiveOpen(t, ctx, call)
	primaryReadTestOpenFailure(t, result, ErrUnavailable)
	if !errors.Is(result.err, ErrSourceChanged) {
		t.Fatalf("stale response snapshot was accepted or lost its source-change error: %v", result.err)
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("failed reopen retained actual-read admission: before=%+v after=%+v", beforeIO, after)
	}
}

func TestOriginalMediaReadControlsReadsAndRetainsIdleOwnership(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	fixture.ctx = ctx
	snapshot := primaryReadTestSnapshot(t, fixture)
	hint, _, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	route, err := fixture.store.primaryReadRoute(hint)
	if err != nil {
		t.Fatal(err)
	}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	call := primaryReadTestStartOpen(t, ctx, fixture, snapshot.ETag)
	result := primaryReadTestReceiveOpen(t, ctx, call)
	if result.err != nil || result.file == nil || result.reader == nil || result.source.Item.ID != fixture.item.ID ||
		result.source.SourceID != snapshot.SourceID || result.source.ETag != snapshot.ETag || result.source.Size != snapshot.Size {
		t.Fatalf("controlled reopen lost the authorized source snapshot: file=%v reader=%v source=%+v error=%v",
			result.file, result.reader, result.source, result.err)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+1 {
		t.Fatalf("delivered source did not retain exactly one owner: before=%d after=%d", beforeOwners, owners)
	}
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("idle delivered descriptor consumed an actual-read slot: before=%+v after=%+v", beforeIO, after)
	}

	// A logical seek must remain usable while all actual-read capacity for this
	// captured route is occupied. The following Read must wait for that capacity.
	releases := primaryReadTestHold(t, ctx, route, mediaSourceRootOwnerLimit)
	if offset := primaryReadTestSeek(t, ctx, result.reader, 6, io.SeekStart); offset != 6 {
		t.Fatalf("logical seek offset=%d, want 6", offset)
	}
	read := primaryReadTestStartRead(result.reader, 8)
	primaryReadTestWaitQueued(t, ctx, beforeIO.Queued+1)
	select {
	case got := <-read:
		t.Fatalf("source read bypassed saturated captured-route admission: bytes=%q error=%v", got.data, got.err)
	default:
	}
	if after := originalMediaReadGovernor.Stats(); after.Active != beforeIO.Active+mediaSourceRootOwnerLimit {
		t.Fatalf("queued Read was counted as active storage work: %+v", after)
	}
	releases[0]()
	got := primaryReadTestReceiveRead(t, ctx, read)
	if got.err != nil || string(got.data) != fixture.contents[6:14] {
		t.Fatalf("controlled read did not use the logical seek position: bytes=%q error=%v", got.data, got.err)
	}
	if after := originalMediaReadGovernor.Stats(); after.Active != beforeIO.Active+mediaSourceRootOwnerLimit-1 || after.Queued != beforeIO.Queued {
		t.Fatalf("completed Read kept its active phase while retaining the descriptor: %+v", after)
	}
	for _, release := range releases {
		release()
	}
	if offset := primaryReadTestSeek(t, ctx, result.reader, -5, io.SeekEnd); offset != int64(len(fixture.contents)-5) {
		t.Fatalf("end-relative seek offset=%d", offset)
	}
	tail := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(result.reader, 5))
	if tail.err != nil || string(tail.data) != fixture.contents[len(fixture.contents)-5:] {
		t.Fatalf("controlled source tail=%q error=%v", tail.data, tail.err)
	}
	if !primaryReadTestClose(t, result.reader) {
		return
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("reader retirement retained admission: before=%+v after=%+v", beforeIO, after)
	}
	if _, err := result.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("reader completion preceded descriptor cleanup: %v", err)
	}
}

func TestOriginalMediaReadStoreCloseWaitsForIdleConsumerRetirement(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	fixture.ctx = ctx
	snapshot := primaryReadTestSnapshot(t, fixture)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	call := primaryReadTestStartOpen(t, ctx, fixture, snapshot.ETag)
	result := primaryReadTestReceiveOpen(t, ctx, call)
	if result.err != nil || result.file == nil || result.reader == nil {
		t.Fatalf("open idle controlled source: %v", result.err)
	}
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("idle consumer held an active-read phase: %+v", after)
	}
	hint, _, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	route, err := fixture.store.primaryReadRoute(hint)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this consumer idle at shutdown. Independent holders make the next
	// Read await cancellation even while the Store's AfterFunc is being scheduled.
	releases := primaryReadTestHold(t, ctx, route, mediaSourceRootOwnerLimit)
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := fixture.store.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Store.Close ignored its caller deadline while a consumer retained its source: %v", err)
	}
	read := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(result.reader, 1))
	if len(read.data) != 0 || !errors.Is(read.err, context.Canceled) {
		t.Fatalf("Store close did not cancel future source admission: bytes=%q error=%v", read.data, read.err)
	}
	select {
	case <-fixture.store.done:
		t.Fatal("Store.Close completed before the idle consumer closed its descriptor")
	default:
	}
	if fixture.store.catalogChangesClosed.Load() || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners+1 {
		t.Fatal("Store cancellation retired catalog resources or the delivered source owner")
	}
	if !primaryReadTestClose(t, result.reader) {
		return
	}
	for _, release := range releases {
		release()
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClose()
	if err := fixture.store.Close(closeCtx); err != nil {
		t.Fatalf("Store did not drain after actual consumer retirement: %v", err)
	}
	primaryReadTestWaitOwners(t, closeCtx, beforeOwners)
	if !fixture.store.catalogChangesClosed.Load() {
		t.Fatal("completed Store drain retained its catalog listener")
	}
	if _, err := result.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Store drain completed before the consumer descriptor closed: %v", err)
	}
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("Store drain retained a read phase: before=%+v after=%+v", beforeIO, after)
	}
}

func TestOriginalMediaReadCancellationRetainsLateOpenerUntilDescriptorCleanup(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
	defer cancel()
	fixture.ctx = ctx
	snapshot := primaryReadTestSnapshot(t, fixture)
	hint, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	rootActive := func() int {
		mediaSourceAdmission.mu.Lock()
		defer mediaSourceAdmission.mu.Unlock()
		return mediaSourceAdmission.roots[root].active
	}
	claimCount := func() int {
		originalMediaReadDomains.mu.Lock()
		defer originalMediaReadDomains.mu.Unlock()
		return len(originalMediaReadDomains.claims)
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for rootActive() != 0 {
		select {
		case <-ctx.Done():
			t.Fatalf("snapshot source-open owner did not finish: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeClaims := claimCount()
	beforeIO := originalMediaReadGovernor.Stats()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	started, allowLateOpen := make(chan struct{}), make(chan struct{})
	captured := make(chan indexedMediaSource, 1)
	type lateOpenResult struct {
		file *os.File
		err  error
	}
	lateFiles := make(chan lateOpenResult, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(allowLateOpen) }) }
	call := &primaryReadTestOpenCall{results: make(chan primaryReadTestOpenResult, 1)}
	var observed *os.File
	var observedLateResult bool
	t.Cleanup(func() {
		cancelCaller()
		release()
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		if !call.received {
			select {
			case result := <-call.results:
				call.received = true
				primaryReadTestRetireOpen(t, result)
			case <-cleanupCtx.Done():
				t.Error("late-opener caller did not finish within bounded cleanup")
			}
		}
		drained := fixture.store.Close(cleanupCtx) == nil
		if !drained {
			t.Error("late source-open owner did not finish within bounded Store cleanup")
		}
		if !observedLateResult {
			select {
			case result := <-lateFiles:
				observed, observedLateResult = result.file, true
				if result.err != nil {
					t.Errorf("open real late source descriptor during cleanup: %v", result.err)
				}
			default:
			}
		}
		if observed != nil && drained {
			if _, err := observed.Stat(); !errors.Is(err, os.ErrClosed) {
				// A fallback descriptor close follows the failed ownership assertion;
				// it must never supply the test's retirement proof.
				t.Errorf("late descriptor survived actual Store drain: %v", err)
				_ = observed.Close()
			}
		}
	})
	go func() {
		file, source, reader, err := fixture.store.openOriginalMediaFor(caller, Subject{UserID: fixture.userID},
			fixture.item.ID, snapshot.SourceID, snapshot.ETag, func(_ context.Context, source indexedMediaSource) (*os.File, error) {
				captured <- source
				close(started)
				// This real opener deliberately ignores caller and Store cancellation.
				// Its independent deadline bounds a broken test gate without declaring
				// any successful source descriptor or consumer retirement.
				gate := time.NewTimer(30 * time.Second)
				defer gate.Stop()
				select {
				case <-allowLateOpen:
				case <-gate.C:
					return nil, errors.New("late source opener gate timed out")
				}
				opened, err := os.Open(fixture.path)
				lateFiles <- lateOpenResult{file: opened, err: err}
				return opened, err
			})
		call.results <- primaryReadTestOpenResult{file: file, source: source, reader: reader, err: err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("real source opener did not start after fresh authorization: %v", ctx.Err())
	}
	var selected indexedMediaSource
	select {
	case selected = <-captured:
	case <-ctx.Done():
		t.Fatalf("real source opener did not record its authorized snapshot: %v", ctx.Err())
	}
	if selected.root != hint.root || selected.mediaFile.Item.ID != fixture.item.ID ||
		selected.mediaFile.Item.Path != fixture.path || selected.mediaFile.ETag != snapshot.ETag {
		t.Fatalf("private opener received an unvalidated or unrelated source: %+v", selected)
	}
	if originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners+1 || claimCount() != beforeClaims+1 || rootActive() != 1 {
		t.Fatal("actual opener did not retain separate consumer, domain, and source-open ownership")
	}
	actualOwnerDone := make(chan struct{})
	go func() {
		fixture.store.mediaSourceOwners.owners.Wait()
		close(actualOwnerDone)
	}()
	cancelCaller()
	result := primaryReadTestReceiveOpen(t, ctx, call)
	primaryReadTestOpenFailure(t, result, context.Canceled)
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if claimCount() != beforeClaims || originalMediaReadGovernor.Stats() != beforeIO {
		t.Fatal("undelivered consumer retained a domain claim or actual-read admission")
	}
	if rootActive() != 1 {
		t.Fatal("caller cancellation released the actual source-open slot before the opener returned")
	}
	closeCaller, cancelCloseCaller := context.WithCancel(context.Background())
	cancelCloseCaller()
	if err := fixture.store.Close(closeCaller); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store.Close did not observe its canceled waiting context: %v", err)
	}
	select {
	case <-actualOwnerDone:
		t.Fatal("Store source ownership retired while the real opener was still blocked")
	default:
	}
	select {
	case <-fixture.store.done:
		t.Fatal("Store.Close finished before late descriptor creation and cleanup")
	default:
	}
	if fixture.store.catalogChangesClosed.Load() || rootActive() != 1 {
		t.Fatal("Store shutdown retired catalog resources or the active source-open slot")
	}
	release()
	select {
	case late := <-lateFiles:
		observed, observedLateResult = late.file, true
		if late.err != nil || observed == nil {
			t.Fatalf("real late descriptor was not opened: file=%v error=%v", observed, late.err)
		}
	case <-ctx.Done():
		t.Fatalf("released real source opener did not return its descriptor: %v", ctx.Err())
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClose()
	if err := fixture.store.Close(closeCtx); err != nil {
		t.Fatalf("Store did not finish actual late source cleanup: %v", err)
	}
	select {
	case <-actualOwnerDone:
	case <-closeCtx.Done():
		t.Fatalf("actual source owner did not join after Store drain: %v", closeCtx.Err())
	}
	if _, err := observed.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("undelivered late descriptor outlived actual worker cleanup: %v", err)
	}
	if rootActive() != 0 || claimCount() != beforeClaims || originalMediaReadGovernor.Stats() != beforeIO {
		t.Fatal("late opener cleanup retained source-open, domain, or actual-read admission")
	}
}

func TestOriginalMediaReadRejectsLiveOverlappingStoreDomainsUntilRetirement(t *testing.T) {
	for _, relation := range []string{"descendant", "ancestor"} {
		t.Run(relation, func(t *testing.T) {
			first := mediaSourceTestCatalog(t, nil)
			_, _, firstDomain := mediaSourceRootAdmissionTestLane(t, first, first.item.ID)
			domain := filepath.Join(firstDomain, "nested-domain")
			if relation == "ancestor" {
				domain = filepath.Dir(firstDomain)
			}
			second := primaryReadTestCatalogAt(t, domain, "claim-overlap")
			ctx, cancel := context.WithTimeout(first.ctx, 20*time.Second)
			defer cancel()
			first.ctx, second.ctx = ctx, ctx
			firstSnapshot, secondSnapshot := primaryReadTestSnapshot(t, first), primaryReadTestSnapshot(t, second)
			beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
			beforeIO := originalMediaReadGovernor.Stats()
			firstCaller, cancelFirst := context.WithCancel(ctx)
			defer cancelFirst()
			openedFirst := primaryReadTestOpenOwned(t, firstCaller, first, firstSnapshot)
			// Request cancellation does not retire the delivered descriptor or
			// its live domain mapping. Only the actual consumer can finish it.
			cancelFirst()
			blocked := primaryReadTestReceiveOpen(t, ctx, primaryReadTestStartOpen(t, ctx, second, secondSnapshot.ETag))
			primaryReadTestOpenFailure(t, blocked, ErrUnavailable)
			primaryReadTestWaitOwners(t, ctx, beforeOwners+1)
			if after := originalMediaReadGovernor.Stats(); after != beforeIO {
				t.Fatalf("rejected overlapping mapping retained active admission: before=%+v after=%+v", beforeIO, after)
			}
			if !primaryReadTestClose(t, openedFirst.reader) {
				return
			}
			primaryReadTestWaitOwners(t, ctx, beforeOwners)
			retried := primaryReadTestOpenOwned(t, ctx, second, secondSnapshot)
			read := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(retried.reader, 8))
			if read.err != nil || string(read.data) != second.contents[:8] {
				t.Fatalf("retired overlap did not allow the new mapping to read: bytes=%q error=%v", read.data, read.err)
			}
			if !primaryReadTestClose(t, retried.reader) {
				return
			}
			primaryReadTestWaitOwners(t, ctx, beforeOwners)
			if after := originalMediaReadGovernor.Stats(); after != beforeIO {
				t.Fatalf("replacement domain reader retained admission: before=%+v after=%+v", beforeIO, after)
			}
		})
	}
}

func TestOriginalMediaReadSharesCanonicalDomainAcrossCatalogs(t *testing.T) {
	first := mediaSourceTestCatalog(t, nil)
	_, _, domain := mediaSourceRootAdmissionTestLane(t, first, first.item.ID)
	second := primaryReadTestCatalogAt(t, domain, "claim-shared")
	ctx, cancel := context.WithTimeout(first.ctx, 20*time.Second)
	defer cancel()
	first.ctx, second.ctx = ctx, ctx
	firstSnapshot, secondSnapshot := primaryReadTestSnapshot(t, first), primaryReadTestSnapshot(t, second)
	firstRoute, secondRoute := primaryReadTestRoute(t, first), primaryReadTestRoute(t, second)
	if reflect.DeepEqual(firstRoute.Roots, secondRoute.Roots) || firstRoute.Roots[0].Catalog == secondRoute.Roots[0].Catalog ||
		!reflect.DeepEqual(firstRoute.Domains, secondRoute.Domains) {
		t.Fatalf("shared-domain fixtures did not have independent catalogs and equal configured domains: first=%+v second=%+v", firstRoute, secondRoute)
	}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	openedFirst := primaryReadTestOpenOwned(t, ctx, first, firstSnapshot)
	openedSecond := primaryReadTestOpenOwned(t, ctx, second, secondSnapshot)
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+2 {
		t.Fatalf("equal mapped domains did not retain both catalog consumers: before=%d after=%d", beforeOwners, owners)
	}
	// Independent catalog/root identities must still share the configured
	// domain budget. Global capacity alone would admit this fourth operation.
	releases := primaryReadTestHold(t, ctx, firstRoute, mediaSourceRootOwnerLimit)
	read := primaryReadTestStartRead(openedSecond.reader, 8)
	primaryReadTestWaitQueued(t, ctx, beforeIO.Queued+1)
	select {
	case got := <-read:
		t.Fatalf("another catalog bypassed the shared domain budget: bytes=%q error=%v", got.data, got.err)
	default:
	}
	releases[0]()
	got := primaryReadTestReceiveRead(t, ctx, read)
	if got.err != nil || string(got.data) != second.contents[:8] {
		t.Fatalf("shared-domain read did not resume after capacity retired: bytes=%q error=%v", got.data, got.err)
	}
	for _, release := range releases {
		release()
	}
	if !primaryReadTestClose(t, openedFirst.reader) {
		return
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners+1)
	if !primaryReadTestClose(t, openedSecond.reader) {
		return
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("shared-domain consumers retained actual-read admission: before=%+v after=%+v", beforeIO, after)
	}
}

func TestOriginalMediaReadAllowsUnrelatedDomainsAndSharesGlobalBudget(t *testing.T) {
	first, second := mediaSourceTestCatalog(t, nil), mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(first.ctx, 20*time.Second)
	defer cancel()
	first.ctx, second.ctx = ctx, ctx
	firstSnapshot, secondSnapshot := primaryReadTestSnapshot(t, first), primaryReadTestSnapshot(t, second)
	firstRoute, secondRoute := primaryReadTestRoute(t, first), primaryReadTestRoute(t, second)
	if reflect.DeepEqual(firstRoute.Roots, secondRoute.Roots) || reflect.DeepEqual(firstRoute.Domains, secondRoute.Domains) ||
		pathWithin(first.allowedRoot, second.allowedRoot) || pathWithin(second.allowedRoot, first.allowedRoot) {
		t.Fatalf("unrelated-domain fixtures share a catalog lane or governing path: first=%+v second=%+v", firstRoute, secondRoute)
	}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	openedFirst := primaryReadTestOpenOwned(t, ctx, first, firstSnapshot)
	openedSecond := primaryReadTestOpenOwned(t, ctx, second, secondSnapshot)
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+2 {
		t.Fatalf("unrelated domains did not retain both actual consumers: before=%d after=%d", beforeOwners, owners)
	}
	firstHolders := primaryReadTestHold(t, ctx, firstRoute, mediaSourceRootOwnerLimit)
	secondHolders := primaryReadTestHold(t, ctx, secondRoute, 1)
	// The second route has unused root and domain capacity. Only the process
	// budget is full, so its next read must share the four global active slots.
	read := primaryReadTestStartRead(openedSecond.reader, 8)
	primaryReadTestWaitQueued(t, ctx, beforeIO.Queued+1)
	select {
	case got := <-read:
		t.Fatalf("unrelated Store bypassed process-wide read admission: bytes=%q error=%v", got.data, got.err)
	default:
	}
	if after := originalMediaReadGovernor.Stats(); after.Active != beforeIO.Active+4 {
		t.Fatalf("global capacity fixture did not occupy exactly four active slots: %+v", after)
	}
	secondHolders[0]()
	got := primaryReadTestReceiveRead(t, ctx, read)
	if got.err != nil || string(got.data) != second.contents[:8] {
		t.Fatalf("unrelated-domain read did not resume after a global slot retired: bytes=%q error=%v", got.data, got.err)
	}
	for _, release := range firstHolders {
		release()
	}
	if !primaryReadTestClose(t, openedFirst.reader) || !primaryReadTestClose(t, openedSecond.reader) {
		return
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	if after := originalMediaReadGovernor.Stats(); after != beforeIO {
		t.Fatalf("unrelated-domain consumers retained admission: before=%+v after=%+v", beforeIO, after)
	}
}

type primaryReadTestOpenResult struct {
	file   *os.File
	source MediaFile
	reader *primaryio.ReadSeeker
	err    error
}

type primaryReadTestOpenCall struct {
	results  chan primaryReadTestOpenResult
	received bool
}

func primaryReadTestCatalogAt(t *testing.T, allowedPath, directory string) mediaSourceFixture {
	t.Helper()
	ctx, pool, store, configured, userID := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	if configured != allowedPath {
		store.mu.Lock()
		store.roots = append(store.roots, approvedRoot{path: allowedPath})
		store.mu.Unlock()
	}
	contents := "video:other-catalog-source"
	path := libraryIntegrationFile(t, allowedPath, filepath.Join(directory, "Feature.mkv"), contents)
	library := libraryIntegrationCreate(t, ctx, store, "Other Catalog", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, ctx, store, Query{
		UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
	}).Items, path)
	if item.Media == nil || item.Media.Size != int64(len(contents)) ||
		item.Media.ProbeVersion < media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 {
		t.Fatalf("shared-domain source was not fully probed and indexed: %+v", item)
	}
	return mediaSourceFixture{ctx: ctx, pool: pool, store: store, allowedRoot: allowedPath, userID: userID,
		path: path, contents: contents, library: library, item: item}
}

func primaryReadTestRoute(t *testing.T, fixture mediaSourceFixture) primaryio.Route {
	t.Helper()
	hint, _, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	route, err := fixture.store.primaryReadRoute(hint)
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func primaryReadTestOpenOwned(t *testing.T, ctx context.Context, fixture mediaSourceFixture, snapshot MediaFile) primaryReadTestOpenResult {
	t.Helper()
	result := primaryReadTestReceiveOpen(t, ctx, primaryReadTestStartOpen(t, ctx, fixture, snapshot.ETag))
	if result.err != nil || result.file == nil || result.reader == nil || result.source.Item.ID != fixture.item.ID ||
		result.source.SourceID != snapshot.SourceID || result.source.ETag != snapshot.ETag || result.source.Size != snapshot.Size {
		t.Fatalf("controlled source ownership was not delivered: file=%v reader=%v source=%+v error=%v",
			result.file, result.reader, result.source, result.err)
	}
	return result
}

func primaryReadTestSnapshot(t *testing.T, fixture mediaSourceFixture) MediaFile {
	t.Helper()
	file, source, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if file != nil {
		defer file.Close()
	}
	if err != nil || file == nil || source.ETag == "" {
		t.Fatalf("capture original response snapshot: %v", err)
	}
	return source
}

func primaryReadTestStartOpen(t *testing.T, ctx context.Context, fixture mediaSourceFixture, expectedETag string) *primaryReadTestOpenCall {
	t.Helper()
	work, cancel := context.WithCancel(ctx)
	call := &primaryReadTestOpenCall{results: make(chan primaryReadTestOpenResult, 1)}
	t.Cleanup(func() {
		cancel()
		if call.received {
			return
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		select {
		case result := <-call.results:
			primaryReadTestRetireOpen(t, result)
		case <-cleanupCtx.Done():
			t.Error("original source caller did not retire within bounded cleanup")
		}
	})
	go func() {
		file, source, reader, err := fixture.store.OpenOriginalMediaFor(work, Subject{UserID: fixture.userID},
			fixture.item.ID, media.SourceID(fixture.item.ID), expectedETag)
		call.results <- primaryReadTestOpenResult{file: file, source: source, reader: reader, err: err}
	}()
	return call
}

func primaryReadTestReceiveOpen(t *testing.T, ctx context.Context, call *primaryReadTestOpenCall) primaryReadTestOpenResult {
	t.Helper()
	select {
	case result := <-call.results:
		call.received = true
		t.Cleanup(func() { primaryReadTestRetireOpen(t, result) })
		return result
	case <-ctx.Done():
		t.Fatalf("original source caller did not return: %v", ctx.Err())
		return primaryReadTestOpenResult{}
	}
}

func primaryReadTestRetireOpen(t *testing.T, result primaryReadTestOpenResult) {
	t.Helper()
	if result.reader != nil {
		primaryReadTestClose(t, result.reader)
	} else if result.file != nil {
		_ = result.file.Close()
	}
}

func primaryReadTestOpenFailure(t *testing.T, result primaryReadTestOpenResult, want error) {
	t.Helper()
	if result.file != nil || result.reader != nil || !reflect.DeepEqual(result.source, MediaFile{}) || !errors.Is(result.err, want) {
		t.Fatalf("failed controlled reopen delivered ownership or lost its error: file=%v reader=%v source=%+v error=%v want=%v",
			result.file, result.reader, result.source, result.err, want)
	}
}

func primaryReadTestClose(t *testing.T, reader *primaryio.ReadSeeker) bool {
	t.Helper()
	results := make(chan error, 1)
	go func() { results <- reader.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case err := <-results:
		if err != nil {
			t.Errorf("retire original source reader: %v", err)
			return false
		}
		return true
	case <-ctx.Done():
		t.Error("original source reader did not finish its actual cleanup within the bounded wait")
		return false
	}
}

func primaryReadTestHold(t *testing.T, ctx context.Context, route primaryio.Route, count int) []func() {
	t.Helper()
	owners, err := primaryio.NewOwnerRuntime(originalMediaReadGovernor, count)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := owners.Close(closeCtx); err != nil {
			t.Errorf("retire controlled-read capacity holders: %v", err)
		}
	})
	releases := make([]func(), 0, count)
	for range count {
		owner, err := owners.Register(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := owner.Complete(); err != nil {
				t.Errorf("complete controlled-read capacity holder: %v", err)
			}
		})
		lease, err := owner.Acquire(route, primaryio.Foreground)
		if err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		release := func() {
			once.Do(func() {
				if err := lease.Release(); err != nil {
					t.Errorf("release controlled-read capacity: %v", err)
				}
				if err := owner.Complete(); err != nil {
					t.Errorf("complete released controlled-read capacity holder: %v", err)
				}
			})
		}
		t.Cleanup(release)
		releases = append(releases, release)
	}
	return releases
}

type primaryReadTestReadResult struct {
	data []byte
	err  error
}

func primaryReadTestStartRead(reader *primaryio.ReadSeeker, size int) <-chan primaryReadTestReadResult {
	results := make(chan primaryReadTestReadResult, 1)
	go func() {
		buffer := make([]byte, size)
		n, err := reader.Read(buffer)
		results <- primaryReadTestReadResult{data: buffer[:n], err: err}
	}()
	return results
}

func primaryReadTestReceiveRead(t *testing.T, ctx context.Context, results <-chan primaryReadTestReadResult) primaryReadTestReadResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatalf("controlled original read did not return: %v", ctx.Err())
		return primaryReadTestReadResult{}
	}
}

func primaryReadTestSeek(t *testing.T, ctx context.Context, reader *primaryio.ReadSeeker, offset int64, whence int) int64 {
	t.Helper()
	type result struct {
		offset int64
		err    error
	}
	results := make(chan result, 1)
	go func() {
		position, err := reader.Seek(offset, whence)
		results <- result{offset: position, err: err}
	}()
	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("seek controlled original source: %v", got.err)
		}
		return got.offset
	case <-ctx.Done():
		t.Fatalf("logical source seek waited for actual-read admission: %v", ctx.Err())
		return 0
	}
}

func primaryReadTestWaitQueued(t *testing.T, ctx context.Context, count int) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if originalMediaReadGovernor.Stats().Queued >= count {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original source read did not queue: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func primaryReadTestWaitOwners(t *testing.T, ctx context.Context, count int) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if originalMediaReadOwners.Stats().RegisteredOwners == count {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original source ownership did not drain to %d: stats=%+v error=%v", count, originalMediaReadOwners.Stats(), ctx.Err())
		case <-ticker.C:
		}
	}
}
