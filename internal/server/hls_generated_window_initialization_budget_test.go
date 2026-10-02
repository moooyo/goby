package server

import (
	"errors"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedWindowInitializationBudgetChargesBeforePublicationAndReusesPages(t *testing.T) {
	if hlsInitializationPageBytes != 64<<10 || hlsInitializationPageCount != 256 || hlsInitializationEntryBytes != 129 ||
		hlsInitializationPageBytes*hlsInitializationPageCount != 16<<20 {
		t.Fatal("the fixed initialization metadata payload contract changed")
	}
	budget := &hlsInitializationBudget{}
	entries := hlsInitializationPageBytes / hlsInitializationEntryBytes
	var leases []*hlsInitializationReservation
	for index := 0; index < hlsInitializationPageCount; index++ {
		lease, err := budget.reserve(entries)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}
	if _, err := budget.reserve(1); !errors.Is(err, transcode.ErrBusy) {
		t.Fatal("metadata payload capacity was enlarged beyond the fixed shared arena")
	}
	oldPage := &budget.pages[0][0]
	leases[0].release()
	replacement, err := budget.reserve(1)
	if err != nil || &budget.pages[0][0] != oldPage || budget.reserved != hlsInitializationPageCount {
		t.Fatal("released metadata page was reallocated or lost its independent reservation charge")
	}
	leases[0].release()
	if budget.owners[0] != replacement || budget.reserved != hlsInitializationPageCount {
		t.Fatal("stale release removed another presentation's page reservation")
	}
	budget.close()
	if budget.reserved != 0 {
		t.Fatal("runtime close retained metadata reservations")
	}
	for index := range budget.pages {
		if budget.pages[index] != nil || budget.owners[index] != nil {
			t.Fatal("closed runtime still references initialization payload storage")
		}
	}
	if _, err := budget.reserve(1); !errors.Is(err, transcode.ErrManagerClosed) {
		t.Fatal("closed metadata arena admitted a new presentation")
	}
}

func TestGeneratedWindowInitializationBudgetSeparatesAdjacentRecordsAndRecycledOwners(t *testing.T) {
	budget := &hlsInitializationBudget{}
	lease, err := budget.reserve(600)
	if err != nil {
		t.Fatal(err)
	}
	var values [3][transcode.MaxHLSRenditions][32]byte
	for index := range values {
		for variant := range values[index] {
			values[index][variant][0] = byte(index + variant + 1)
			values[index][variant][31] = byte(index + 10*variant + 1)
		}
		if err := lease.remember(507+index, transcode.MaxHLSRenditions, values[index]); err != nil {
			t.Fatal("adjacent complete records could not be committed")
		}
	}
	for index := range values {
		if err := lease.remember(507+index, transcode.MaxHLSRenditions, values[index]); err != nil {
			t.Fatal("a neighboring record changed another slot's epoch")
		}
	}
	lease.release()
	replacement, err := budget.reserve(600)
	if err != nil {
		t.Fatal(err)
	}
	changed := values[1]
	changed[3][31]++
	if err := replacement.remember(508, transcode.MaxHLSRenditions, changed); err != nil {
		t.Fatal("recycled pages retained another presentation's digest record")
	}
	if err := lease.remember(508, transcode.MaxHLSRenditions, values[1]); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("a released owner altered its replacement's record")
	}
	lease.release()
	if err := replacement.remember(508, transcode.MaxHLSRenditions, changed); err != nil {
		t.Fatal("a stale release invalidated its replacement")
	}
	budget.close()
	if err := replacement.remember(508, transcode.MaxHLSRenditions, changed); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("closed arena accepted a retained owner")
	}
}

func TestGeneratedWindowInitializationBudgetPageBoundariesAndFailureRollback(t *testing.T) {
	budget := &hlsInitializationBudget{}
	for _, test := range []struct{ entries, pages int }{{508, 1}, {509, 2}, {transcode.MaxTimelineSegments, 33}} {
		lease, err := budget.reserve(test.entries)
		if err != nil || len(lease.pages) != test.pages || budget.reserved != test.pages {
			t.Fatal("presentation metadata reservation rounded to the wrong page count")
		}
		lease.release()
	}
	for _, entries := range []int{-1, 0, transcode.MaxTimelineSegments + 1} {
		if _, err := budget.reserve(entries); !errors.Is(err, transcode.ErrInvalidTimeline) || budget.reserved != 0 {
			t.Fatal("invalid presentation changed initialization capacity")
		}
	}
	for index := 0; index < hlsInitializationPageCount-1; index++ {
		if _, err := budget.reserve(1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := budget.reserve(509); !errors.Is(err, transcode.ErrBusy) || budget.reserved != hlsInitializationPageCount-1 || budget.owners[hlsInitializationPageCount-1] != nil {
		t.Fatal("failed multi-page reservation partially consumed remaining capacity")
	}
	budget.close()
}

func TestGeneratedWindowInitializationBudgetConcurrentRetirementFencesAllReaders(t *testing.T) {
	budget := &hlsInitializationBudget{}
	lease, err := budget.reserve(1)
	if err != nil {
		t.Fatal(err)
	}
	var values [transcode.MaxHLSRenditions][32]byte
	values[0][0] = 1
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 16 {
				if err := lease.remember(0, 1, values); err != nil && !errors.Is(err, transcode.ErrJobNotFound) {
					t.Error("concurrent retirement returned an unrelated epoch error")
				}
			}
		}()
	}
	lease.release()
	budget.close()
	workers.Wait()
	if err := lease.remember(0, 1, values); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("retirement completion left an accessible metadata record")
	}
}

func TestGeneratedWindowInitializationBudgetKeepsEpochAcrossBoundaryAndRejectsChangedSibling(t *testing.T) {
	budget := &hlsInitializationBudget{}
	lease, err := budget.reserve(600)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	var digests [transcode.MaxHLSRenditions][32]byte
	digests[0][0], digests[1][0] = 1, 2
	// Record 508 crosses a 64-KiB page boundary. Reusing a producer does not
	// change this living presentation's first actual initialization bytes.
	if err := lease.remember(508, 2, digests); err != nil {
		t.Fatal(err)
	}
	changed := digests
	changed[1][0] = 3
	if err := lease.remember(508, 2, changed); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatal("one matching rendition replaced a cached sibling MAP across a page boundary")
	}
	if err := lease.remember(508, 2, digests); err != nil {
		t.Fatal("a rejected epoch changed the previously committed full digest set")
	}
	lease.release()
	if err := lease.remember(508, 2, digests); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("retired presentation could read or write recycled metadata pages")
	}
}

func TestGeneratedWindowInitializationBudgetRejectsIncompleteOrOutOfScopeEntry(t *testing.T) {
	budget := &hlsInitializationBudget{}
	lease, err := budget.reserve(17)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	var digests [transcode.MaxHLSRenditions][32]byte
	digests[0][0] = 1
	if err := lease.remember(15, 2, digests); !errors.Is(err, transcode.ErrInvalidTimeline) {
		t.Fatal("incomplete measured digest set acquired a MAP record")
	}
	digests[1][0] = 2
	if err := lease.remember(17, 2, digests); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("another source slot wrote past the presentation's declared capacity")
	}
	if err := lease.remember(15, 2, digests); err != nil {
		t.Fatal("failed incomplete write changed the presentation's later first epoch")
	}
}
