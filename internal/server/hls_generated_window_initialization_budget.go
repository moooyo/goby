package server

import (
	"sync"

	"github.com/moooyo/goby/internal/transcode"
)

const (
	hlsInitializationPageBytes  = 64 << 10
	hlsInitializationPageCount  = 256
	hlsInitializationEntryBytes = 1 + transcode.MaxHLSRenditions*32
)

// The shared arena retains at most 16 MiB of digest-record backing storage.
// One arena belongs to one HLS runtime and is shared by its presentations.
// Pages are allocated lazily and reused; Go object headers and other runtime
// allocations are separate from this payload limit. No job byte quota or
// physical/RSS claim is inferred from this metadata reservation.
// Callers hold session.mu before the arena mutex. The arena never takes a
// session mutex, starts work or borrows a cache reader.
type hlsInitializationBudget struct {
	mu       sync.Mutex
	pages    [hlsInitializationPageCount][]byte
	owners   [hlsInitializationPageCount]*hlsInitializationReservation
	closed   bool
	reserved int
}

// A reservation owns enough fixed pages for the entire immutable presentation
// before publication. Its encoded hashes survive all producer/cache eviction.
// Release fences later reads before returning pages to another presentation;
// a stale handle never releases or alters the new owner.
type hlsInitializationReservation struct {
	budget   *hlsInitializationBudget
	pages    []int
	entries  int
	released bool
}

func (budget *hlsInitializationBudget) reserve(entries int) (*hlsInitializationReservation, error) {
	if budget == nil || entries < 1 || entries > transcode.MaxTimelineSegments {
		return nil, transcode.ErrInvalidTimeline
	}
	needed := (entries*hlsInitializationEntryBytes + hlsInitializationPageBytes - 1) / hlsInitializationPageBytes
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.closed {
		return nil, transcode.ErrManagerClosed
	}
	if needed > hlsInitializationPageCount-budget.reserved {
		return nil, transcode.ErrBusy
	}
	reservation := &hlsInitializationReservation{budget: budget, entries: entries}
	for index, owner := range budget.owners {
		if owner == nil {
			reservation.pages = append(reservation.pages, index)
			if len(reservation.pages) == needed {
				break
			}
		}
	}
	if len(reservation.pages) != needed {
		return nil, transcode.ErrBusy
	}
	for _, index := range reservation.pages {
		if budget.pages[index] == nil {
			budget.pages[index] = make([]byte, hlsInitializationPageBytes)
		} else {
			clear(budget.pages[index])
		}
		budget.owners[index] = reservation
	}
	budget.reserved += needed
	return reservation, nil
}

func (reservation *hlsInitializationReservation) validLocked() bool {
	if reservation == nil || reservation.budget == nil || reservation.released || reservation.budget.closed ||
		reservation.entries < 1 || reservation.entries > transcode.MaxTimelineSegments || len(reservation.pages) == 0 {
		return false
	}
	for _, page := range reservation.pages {
		if page < 0 || page >= hlsInitializationPageCount || reservation.budget.owners[page] != reservation ||
			len(reservation.budget.pages[page]) != hlsInitializationPageBytes {
			return false
		}
	}
	return true
}

func (reservation *hlsInitializationReservation) transferLocked(offset int, data []byte, write bool) {
	for len(data) > 0 {
		page, inPage := offset/hlsInitializationPageBytes, offset%hlsInitializationPageBytes
		part := min(len(data), hlsInitializationPageBytes-inPage)
		storage := reservation.budget.pages[reservation.pages[page]][inPage : inPage+part]
		if write {
			copy(storage, data[:part])
		} else {
			copy(data[:part], storage)
		}
		offset += part
		data = data[part:]
	}
}

// remember compares the whole negotiated set before one encoded record write.
// Independent closure and actual initialization SHA are caller prerequisites.
// The record grants no source clock, scope, reader or admission authority.
func (reservation *hlsInitializationReservation) remember(number, count int, digests [transcode.MaxHLSRenditions][32]byte) error {
	if reservation == nil || reservation.budget == nil {
		return transcode.ErrJobNotFound
	}
	budget := reservation.budget
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if !reservation.validLocked() || number < 0 || number >= reservation.entries || count < 1 || count > transcode.MaxHLSRenditions {
		return transcode.ErrJobNotFound
	}
	var record [hlsInitializationEntryBytes]byte
	for variant, digest := range digests {
		if variant < count && digest == ([32]byte{}) || variant >= count && digest != ([32]byte{}) {
			return transcode.ErrInvalidTimeline
		}
		copy(record[1+variant*32:1+(variant+1)*32], digest[:])
	}
	record[0] = 1
	var current [hlsInitializationEntryBytes]byte
	offset := number * hlsInitializationEntryBytes
	reservation.transferLocked(offset, current[:], false)
	if current[0] != 0 {
		if current != record {
			return transcode.ErrOutputUnavailable
		}
		return nil
	}
	reservation.transferLocked(offset, record[:], true)
	return nil
}

func (reservation *hlsInitializationReservation) release() {
	if reservation == nil || reservation.budget == nil {
		return
	}
	budget := reservation.budget
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if reservation.released {
		return
	}
	reservation.released = true
	for _, page := range reservation.pages {
		if page >= 0 && page < hlsInitializationPageCount && budget.owners[page] == reservation {
			clear(budget.pages[page])
			budget.owners[page] = nil
			budget.reserved--
		}
	}
}

func (budget *hlsInitializationBudget) close() {
	if budget == nil {
		return
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.closed = true
	for _, owner := range budget.owners {
		if owner != nil {
			owner.released = true
		}
	}
	for index := range budget.pages {
		clear(budget.pages[index])
		budget.pages[index] = nil
		budget.owners[index] = nil
	}
	budget.reserved = 0
}
