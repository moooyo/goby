package server

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	// This is an additional lifetime bound, not additional media capacity.
	// Admission owners include metadata, source loans and existing bounded
	// worker/resource owners. Stop retains its separate existing Control4 lane.
	maxPlaybackAdmissionReferences = 640
	maxPlaybackStopReservations    = int(database.PlaybackControlMaxConns)
	maxPlaybackIntentEntries       = maxPlaybackAdmissionReferences + maxPlaybackStopReservations
	maxPlaybackIntentIDBytes       = 256
	maxPlaybackIntentStringBytes   = maxPlaybackIntentEntries * 7 * maxPlaybackIntentIDBytes
)

type playbackStopIntentKey struct {
	auth, play string
}

type playbackStopIntentEntry struct {
	scope          transcode.Scope
	references     int
	stopOwners     int
	stopped        bool
	terminal       bool
	stopReserved   bool
	identifierSize int
}

// The gate records cancellation lifetimes, never reusable authorization.
// A new reference must be minted in the fresh validation transaction's
// memory-only admission stage, before its authority locks are released.
// Every subsequent AUTH, filesystem and delivery check remains mandatory.
// No resource admission may reconstruct a reference from an old scope value.
type playbackStopIntentGate struct {
	mu              sync.Mutex
	entries         map[playbackStopIntentKey]*playbackStopIntentEntry
	references      int
	stopOwners      int
	stopReserved    int
	identifierBytes int
	closing         bool
}

type playbackAdmissionReference struct {
	gate     *playbackStopIntentGate
	entry    *playbackStopIntentEntry
	released *bool // Shared by copied handles and protected by gate.mu.
}

type playbackValidatedStopReference struct {
	gate     *playbackStopIntentGate
	entry    *playbackStopIntentEntry
	released *bool // Shared by copied handles and protected by gate.mu.
}

type playbackStopIntentUsage struct {
	Entries, References, StopOwners, StopReservations, IdentifierBytes int
	Closing                                                            bool
}

func playbackStopIntentScopeValid(scope transcode.Scope) bool {
	if scope.ApplicationKey && (scope.UserID != "" || scope.ApplicationClientID == "") ||
		!scope.ApplicationKey && (scope.UserID == "" || scope.ApplicationClientID != "") {
		return false
	}
	for index, value := range []string{scope.AuthSessionID, scope.PlaySessionID, scope.ItemID, scope.SourceID,
		scope.UserID, scope.ApplicationClientID, scope.DeviceID} {
		if index < 4 && value == "" || len(value) > maxPlaybackIntentIDBytes || !utf8.ValidString(value) ||
			strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

func playbackStopIntentKeyFor(scope transcode.Scope) playbackStopIntentKey {
	return playbackStopIntentKey{auth: scope.AuthSessionID, play: scope.PlaySessionID}
}

func clonePlaybackStopIntentScope(scope transcode.Scope) (transcode.Scope, int) {
	// Clone bounded identities so a short substring cannot retain an unrelated
	// large request buffer. Map keys share these same owned strings.
	fields := []*string{&scope.AuthSessionID, &scope.PlaySessionID, &scope.ItemID, &scope.SourceID,
		&scope.UserID, &scope.ApplicationClientID, &scope.DeviceID}
	bytes := 0
	for _, field := range fields {
		*field = strings.Clone(*field)
		bytes += len(*field)
	}
	return scope, bytes
}

// entryLocked only accepts a complete canonical scope from trusted validation.
// Shape validation is not ownership, credential, source or clock authority.
func (gate *playbackStopIntentGate) entryLocked(scope transcode.Scope) (*playbackStopIntentEntry, error) {
	if !playbackStopIntentScopeValid(scope) {
		return nil, transcode.ErrInvalidScope
	}
	key := playbackStopIntentKeyFor(scope)
	if entry := gate.entries[key]; entry != nil {
		if entry.scope != scope {
			return nil, transcode.ErrInvalidScope
		}
		return entry, nil
	}
	if len(gate.entries) >= maxPlaybackIntentEntries {
		return nil, transcode.ErrBusy
	}
	owned, bytes := clonePlaybackStopIntentScope(scope)
	if gate.identifierBytes > maxPlaybackIntentStringBytes-bytes {
		return nil, transcode.ErrBusy
	}
	if gate.entries == nil {
		gate.entries = make(map[playbackStopIntentKey]*playbackStopIntentEntry)
	}
	entry := &playbackStopIntentEntry{scope: owned, identifierSize: bytes}
	gate.entries[playbackStopIntentKeyFor(owned)] = entry
	gate.identifierBytes += bytes
	return entry, nil
}

// acquireValidated must run at the fresh transaction's memory-only admission
// stage. The caller owns this reference until the actual input/metadata is
// consumed or rejected, and transfers/forks it for asynchronous work.
func (gate *playbackStopIntentGate) acquireValidated(scope transcode.Scope) (*playbackAdmissionReference, error) {
	if !playbackStopIntentScopeValid(scope) {
		return nil, transcode.ErrInvalidScope
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.closing {
		return nil, transcode.ErrManagerClosed
	}
	if gate.references >= maxPlaybackAdmissionReferences {
		return nil, transcode.ErrBusy
	}
	entry, err := gate.entryLocked(scope)
	if err != nil {
		return nil, err
	}
	if entry.stopped {
		return nil, transcode.ErrJobCancelled
	}
	entry.references++
	gate.references++
	return &playbackAdmissionReference{gate: gate, entry: entry, released: new(bool)}, nil
}

// fork holds the same lifetime before duplicating a descriptor or starting an
// existing bounded worker. It cannot refresh authority or resurrect a release.
func (reference *playbackAdmissionReference) fork(scope transcode.Scope) (*playbackAdmissionReference, error) {
	if reference == nil || reference.gate == nil || reference.entry == nil || reference.released == nil {
		return nil, transcode.ErrInvalidScope
	}
	gate := reference.gate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry := reference.entry
	if scope != entry.scope {
		return nil, transcode.ErrInvalidScope
	}
	if *reference.released || entry.stopped || gate.closing || gate.entries[playbackStopIntentKeyFor(entry.scope)] != entry {
		return nil, transcode.ErrJobCancelled
	}
	if gate.references >= maxPlaybackAdmissionReferences {
		return nil, transcode.ErrBusy
	}
	entry.references++
	gate.references++
	return &playbackAdmissionReference{gate: gate, entry: entry, released: new(bool)}, nil
}

func (gate *playbackStopIntentGate) collectLocked(entry *playbackStopIntentEntry) {
	if entry.references != 0 || entry.stopOwners != 0 || entry.stopped && !entry.terminal && !gate.closing {
		return
	}
	key := playbackStopIntentKeyFor(entry.scope)
	if gate.entries[key] != entry {
		return
	}
	if entry.stopReserved {
		entry.stopReserved = false
		gate.stopReserved--
	}
	delete(gate.entries, key)
	gate.identifierBytes -= entry.identifierSize
	if len(gate.entries) == 0 {
		gate.entries = nil
	}
}

func (reference *playbackAdmissionReference) release() {
	if reference == nil || reference.gate == nil || reference.entry == nil || reference.released == nil {
		return
	}
	gate := reference.gate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if *reference.released {
		return
	}
	*reference.released = true
	reference.entry.references--
	gate.references--
	gate.collectLocked(reference.entry)
}

// acceptValidatedStop installs the monotonic intent before normal cancellation.
// It must run only after complete fresh Stopped body/owner/policy/catalog
// validation. Gate.mu is released before touching any resource-domain mutex.
func (gate *playbackStopIntentGate) acceptValidatedStop(scope transcode.Scope) (*playbackValidatedStopReference, error) {
	if !playbackStopIntentScopeValid(scope) {
		return nil, transcode.ErrInvalidScope
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.closing {
		return nil, transcode.ErrManagerClosed
	}
	if gate.stopOwners >= maxPlaybackStopReservations {
		return nil, transcode.ErrBusy
	}
	key := playbackStopIntentKeyFor(scope)
	prior := gate.entries[key]
	if prior != nil && prior.scope != scope {
		return nil, transcode.ErrInvalidScope
	}
	if (prior == nil || !prior.stopReserved && !prior.terminal) && gate.stopReserved >= maxPlaybackStopReservations {
		return nil, transcode.ErrBusy
	}
	entry, err := gate.entryLocked(scope)
	if err != nil {
		return nil, err
	}
	if !entry.stopReserved && !entry.terminal {
		entry.stopReserved = true
		gate.stopReserved++
	}
	entry.stopped = true
	entry.stopOwners++
	gate.stopOwners++
	return &playbackValidatedStopReference{gate: gate, entry: entry, released: new(bool)}, nil
}

// finish receives true only after the complete authorized report has committed
// terminal state. Error/cancellation never clears the intent. A same-play fresh
// retry reuses its reserved Stop lane; unrelated media remains independently
// admissible. Durable completion releases the Stop lane while old refs drain.
func (reference *playbackValidatedStopReference) finish(terminalCommitted bool) {
	if reference == nil || reference.gate == nil || reference.entry == nil || reference.released == nil {
		return
	}
	gate := reference.gate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if *reference.released {
		return
	}
	*reference.released = true
	entry := reference.entry
	entry.terminal = entry.terminal || terminalCommitted
	entry.stopOwners--
	gate.stopOwners--
	if entry.terminal && entry.stopReserved {
		entry.stopReserved = false
		gate.stopReserved--
	}
	gate.collectLocked(entry)
}

// blocked is checked inside each domain's actual admission mutex. Intent
// installation then scans cancellation under that same domain mutex: an older
// insertion is retired and a newer insertion observes stopped. No gate mutex
// may be held while cancellation, SQL, IO or a resource callback runs.
func (gate *playbackStopIntentGate) blocked(scope transcode.Scope) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.closing {
		return true
	}
	entry := gate.entries[playbackStopIntentKeyFor(scope)]
	return entry != nil && (entry.stopped || entry.scope != scope)
}

func (gate *playbackStopIntentGate) usage() playbackStopIntentUsage {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return playbackStopIntentUsage{Entries: len(gate.entries), References: gate.references, StopOwners: gate.stopOwners,
		StopReservations: gate.stopReserved, IdentifierBytes: gate.identifierBytes, Closing: gate.closing}
}

// close only begins the closing fence. Actual owners still release after
// descriptor closure/consumption and worker join; their entries remain visible
// until then. An uncommitted stop can be discarded only in this closed gate.
func (gate *playbackStopIntentGate) close() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.closing = true
	for _, entry := range gate.entries {
		gate.collectLocked(entry)
	}
	return gate.references == 0 && gate.stopOwners == 0 && len(gate.entries) == 0
}
