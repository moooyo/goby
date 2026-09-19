package timeshift

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"sync"
	"time"
)

type artifactState struct {
	artifact   Artifact
	charge     int64
	readers    int
	visible    bool
	graceUntil time.Time
}

type segmentState struct {
	segment            Segment
	published          time.Time
	advertisedDuration int64
}

type epochState struct {
	epoch    Epoch
	segments int
}

type windowState struct {
	id            string
	scope         Scope
	options       WindowOptions
	storage       *storageWindow
	ctx           context.Context
	cancel        context.CancelFunc
	segments      []*segmentState
	epochs        []*epochState
	artifacts     map[string]*artifactState
	readers       map[*ReadHandle]struct{}
	generation    uint64
	nextSequence  uint64
	discontinuity uint64
	revision      uint64
	liveEdge      int64
	bytes         int64
	activeBytes   int64
	pendingBytes  int64
	accessed      time.Time
	state         State
	publishing    bool
	closed        bool
	advertised    bool
}

// Store owns an exclusive directory lock until all publications and readers
// have stopped. All artifact access remains descriptor-relative in that root.
type Store struct {
	mu               sync.Mutex
	options          Options
	storage          *storageRoot
	windows          map[string]*windowState
	bytes            int64
	pendingBytes     int64
	artifacts        int
	pendingArtifacts int
	readers          int
	publishing       int
	ctx              context.Context
	cancel           context.CancelFunc
	workers          sync.WaitGroup
	closing          bool
	failure          error
	closeErr         error
	done             chan struct{}
	copyArtifact     func(context.Context, *storageWindow, string, *os.File, os.FileInfo) (int64, error)
}

func New(options Options) (*Store, error) {
	options, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	storage, err := openStorage(options.Root)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	store := &Store{options: options, storage: storage, windows: make(map[string]*windowState), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	store.copyArtifact = func(ctx context.Context, window *storageWindow, id string, file *os.File, info os.FileInfo) (int64, error) {
		return window.copy(ctx, id, file, info)
	}
	store.workers.Add(1)
	go store.maintain()
	return store, nil
}

func randomID(prefix string) (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(data[:]), nil
}

func (store *Store) checkedWindow(ctx context.Context, scope Scope, id string, touch bool) (*windowState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if store.closing {
		return nil, ErrClosed
	}
	if !validScope(scope) {
		return nil, ErrInvalid
	}
	window := store.windows[id]
	if window == nil || window.scope != scope {
		return nil, ErrNotFound
	}
	if window.closed {
		return nil, ErrClosed
	}
	now := store.options.Now()
	if now.Sub(window.accessed) >= store.options.IdleTimeout {
		store.revokeLocked(window)
		return nil, ErrClosed
	}
	store.expireLocked(window, now)
	if touch {
		window.accessed = now
	}
	return window, nil
}

func (store *Store) Create(ctx context.Context, scope Scope, options WindowOptions) (WindowSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return WindowSnapshot{}, err
	}
	if !validScope(scope) {
		return WindowSnapshot{}, ErrInvalid
	}
	options, err := normalizeWindow(options, store.options)
	if err != nil {
		return WindowSnapshot{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closing {
		return WindowSnapshot{}, ErrClosed
	}
	if store.failure != nil {
		return WindowSnapshot{}, ErrStorage
	}
	owned := 0
	for _, window := range store.windows {
		if sameOwner(window.scope, scope) {
			owned++
		}
	}
	if len(store.windows) >= store.options.MaxWindows || owned >= store.options.MaxOwnerWindows {
		return WindowSnapshot{}, ErrBusy
	}
	id, err := randomID("w_")
	if err != nil {
		return WindowSnapshot{}, err
	}
	directory, err := store.storage.create(id)
	if err != nil {
		store.failLocked(err)
		return WindowSnapshot{}, errors.Join(ErrStorage, err)
	}
	ctx, cancel := context.WithCancel(store.ctx)
	window := &windowState{id: id, scope: scope, options: options, storage: directory, ctx: ctx, cancel: cancel,
		artifacts: make(map[string]*artifactState), readers: make(map[*ReadHandle]struct{}), accessed: store.options.Now(), revision: 1}
	store.windows[id] = window
	return snapshotLocked(window), nil
}

type preparedArtifact struct {
	input          ArtifactInput
	before         os.FileInfo
	artifact       Artifact
	charge         int64
	initialization bool
}

func (store *Store) preparePublication(window *windowState, publication Publication) ([]preparedArtifact, int64, error) {
	windowTicks := int64(window.options.Window / (100 * time.Nanosecond))
	if publication.Generation == 0 || publication.Generation < window.generation || publication.DurationTicks <= 0 ||
		publication.DurationTicks > min(windowTicks, 600*TicksPerSecond) || publication.DurationTicks > math.MaxInt64-window.liveEdge ||
		window.nextSequence == math.MaxUint64 || publication.Generation > window.generation && window.generation != 0 && window.discontinuity == math.MaxUint64 {
		return nil, 0, ErrInvalid
	}
	newEpoch := publication.Generation != window.generation
	if window.options.TargetDurationTicks > 0 {
		rounded := publication.DurationTicks / TicksPerSecond
		if publication.DurationTicks%TicksPerSecond >= TicksPerSecond/2 {
			rounded++
		}
		if rounded > window.options.TargetDurationTicks/TicksPerSecond {
			return nil, 0, ErrInvalid
		}
	}
	if !newEpoch && len(publication.Initializations) != 0 || len(publication.Segments) != len(window.options.Variants) || len(publication.Initializations) > len(window.options.Variants) {
		return nil, 0, ErrInvalid
	}
	variants := make(map[string]Variant, len(window.options.Variants))
	for _, variant := range window.options.Variants {
		variants[variant.ID] = variant
	}
	seenMedia, seenInit := make(map[string]bool), make(map[string]bool)
	seenIDs := make(map[string]bool)
	var prepared []preparedArtifact
	var reserved int64
	for pass, inputs := range [][]ArtifactInput{publication.Initializations, publication.Segments} {
		for _, input := range inputs {
			variant, exists := variants[input.VariantID]
			seen := seenMedia
			if pass == 0 {
				seen = seenInit
			}
			if !exists || seen[input.VariantID] || input.File == nil || pass == 0 && variant.Format != "fmp4" {
				return nil, 0, ErrInvalid
			}
			seen[input.VariantID] = true
			info, err := input.File.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > store.options.MaxArtifactBytes {
				return nil, 0, ErrInvalid
			}
			id, err := randomID("a_")
			if err != nil {
				return nil, 0, err
			}
			if window.artifacts[id] != nil || seenIDs[id] {
				return nil, 0, ErrBusy
			}
			seenIDs[id] = true
			charge := store.storage.charge(info.Size())
			if charge > math.MaxInt64-reserved {
				return nil, 0, ErrQuota
			}
			reserved += charge
			prepared = append(prepared, preparedArtifact{input: input, before: info, artifact: Artifact{ID: id, VariantID: input.VariantID, Size: info.Size(), Initialization: pass == 0}, charge: charge, initialization: pass == 0})
		}
	}
	if newEpoch {
		for _, variant := range window.options.Variants {
			if variant.Format == "fmp4" && !seenInit[variant.ID] {
				return nil, 0, ErrInvalid
			}
		}
	}
	return prepared, reserved, nil
}

// Publish atomically exposes one complete bundle after bounded private copies.
// Capacity pressure can evict older intervals before copying, but a partial new
// interval is never advertised. Input descriptors remain owned by the caller.
func (store *Store) Publish(ctx context.Context, scope Scope, id string, publication Publication) (WindowSnapshot, error) {
	store.mu.Lock()
	window, err := store.checkedWindow(ctx, scope, id, false)
	if err != nil {
		store.mu.Unlock()
		return WindowSnapshot{}, err
	}
	if store.failure != nil {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrStorage
	}
	if window.state.Ended {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrEnded
	}
	if window.publishing || store.publishing >= store.options.MaxPublishing {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrBusy
	}
	prepared, reserved, err := store.preparePublication(window, publication)
	if err != nil {
		store.mu.Unlock()
		return WindowSnapshot{}, err
	}
	if reserved > window.options.MaxBytes || reserved > store.options.MaxBytes {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrQuota
	}
	newEpoch := publication.Generation != window.generation
	if window.advertised && reserved+store.requiredInitializationBytesLocked(window, publication.Generation) > window.options.MaxBytes/3 {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrQuota
	}
	if !store.canReserveLocked(window, reserved, len(prepared)) {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrQuota
	}
	if window.advertised {
		store.trimActiveLocked(window, reserved, publication.Generation)
		if store.activeBytesForLocked(window, publication.Generation)+reserved > window.options.MaxBytes/3 {
			store.mu.Unlock()
			return WindowSnapshot{}, ErrQuota
		}
	}
	for len(window.segments) > 0 && (window.bytes+reserved > window.options.MaxBytes || store.bytes+store.pendingBytes+reserved > store.options.MaxBytes ||
		store.artifacts+store.pendingArtifacts+len(prepared) > store.options.MaxArtifacts || len(window.segments) >= store.options.MaxSegments ||
		newEpoch && retainedEpochs(window)+1 > store.options.MaxEpochs) {
		store.dropOldestLocked(window)
	}
	if store.failure != nil {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrStorage
	}
	if window.bytes+reserved > window.options.MaxBytes || store.bytes+store.pendingBytes+reserved > store.options.MaxBytes ||
		store.artifacts+store.pendingArtifacts+len(prepared) > store.options.MaxArtifacts {
		store.mu.Unlock()
		return WindowSnapshot{}, ErrQuota
	}
	window.publishing, window.pendingBytes = true, reserved
	store.publishing++
	store.pendingBytes += reserved
	store.pendingArtifacts += len(prepared)
	store.workers.Add(1)
	store.mu.Unlock()
	defer store.workers.Done()
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(window.ctx, cancel)
	defer stop()
	defer cancel()
	var copiedCharge int64
	for index := range prepared {
		item := &prepared[index]
		charge, copyErr := store.copyArtifact(work, window.storage, item.artifact.ID, item.input.File, item.before)
		if copyErr != nil {
			err = copyErr
			break
		}
		item.charge = charge
		copiedCharge += charge
		if copiedCharge > reserved {
			err = ErrQuota
			break
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.publishing--
	store.pendingBytes -= reserved
	store.pendingArtifacts -= len(prepared)
	window.publishing, window.pendingBytes = false, 0
	if err == nil {
		err = work.Err()
	}
	if err == nil && (window.closed || store.closing) {
		err = ErrClosed
	}
	if err == nil && store.failure != nil {
		err = ErrStorage
	}
	if err != nil {
		for _, item := range prepared {
			if removeErr := window.storage.remove(item.artifact.ID); removeErr != nil {
				store.retainFailedCleanupLocked(window, item, removeErr)
			}
		}
		store.destroyClosedLocked(window)
		if store.failure != nil {
			err = errors.Join(err, ErrStorage)
		}
		return WindowSnapshot{}, err
	}
	for _, item := range prepared {
		window.artifacts[item.artifact.ID] = &artifactState{artifact: item.artifact, charge: item.charge, visible: true}
		window.bytes += item.charge
		window.activeBytes += item.charge
		store.bytes += item.charge
		store.artifacts++
	}
	var epoch *epochState
	if newEpoch {
		if window.generation != 0 {
			window.discontinuity++
		}
		epoch = &epochState{epoch: Epoch{Generation: publication.Generation, FirstSequence: window.nextSequence, DiscontinuitySequence: window.discontinuity}}
		for _, item := range prepared {
			if item.initialization {
				epoch.epoch.Initializations = append(epoch.epoch.Initializations, item.artifact)
			}
		}
		window.epochs = append(window.epochs, epoch)
		window.generation = publication.Generation
	} else {
		for _, candidate := range window.epochs {
			if candidate.epoch.Generation == window.generation {
				epoch = candidate
				break
			}
		}
	}
	segment := Segment{Sequence: window.nextSequence, Generation: publication.Generation, StartTicks: window.liveEdge,
		DurationTicks: publication.DurationTicks, Discontinuity: newEpoch && window.nextSequence != 0, DiscontinuitySequence: window.discontinuity}
	for _, item := range prepared {
		if item.initialization {
			continue
		}
		artifact := item.artifact
		for _, initialization := range epoch.epoch.Initializations {
			if initialization.VariantID == artifact.VariantID {
				artifact.InitID = initialization.ID
				break
			}
		}
		window.artifacts[artifact.ID].artifact = artifact
		segment.Artifacts = append(segment.Artifacts, artifact)
	}
	epoch.segments++
	window.nextSequence++
	window.liveEdge += publication.DurationTicks
	window.segments = append(window.segments, &segmentState{segment: segment, published: store.options.Now()})
	window.state.Stalled = false
	bumpRevision(window)
	store.pruneEpochsLocked(window)
	store.expireLocked(window, store.options.Now())
	if store.failure != nil {
		return WindowSnapshot{}, ErrStorage
	}
	return snapshotLocked(window), nil
}

func retainedEpochs(window *windowState) int {
	count := 0
	for _, epoch := range window.epochs {
		if epoch.segments > 0 {
			count++
		}
	}
	return count
}

// Admission cannot reclaim advertised grace by first hiding all current media.
// Check that ordinary unadvertised, unpinned segment bytes can satisfy pressure
// before evicting anything. Initialization reclamation is conservative here.
func (store *Store) canReserveLocked(window *windowState, reserved int64, artifacts int) bool {
	neededBytes := max(int64(0), window.bytes+reserved-window.options.MaxBytes, store.bytes+store.pendingBytes+reserved-store.options.MaxBytes)
	neededArtifacts := max(0, store.artifacts+store.pendingArtifacts+artifacts-store.options.MaxArtifacts)
	if neededBytes == 0 && neededArtifacts == 0 {
		return true
	}
	var reclaimed int64
	count := 0
	for _, state := range window.segments {
		if state.advertisedDuration != 0 {
			continue
		}
		for _, value := range state.segment.Artifacts {
			artifact := window.artifacts[value.ID]
			if artifact != nil && artifact.readers == 0 && !store.options.Now().Before(artifact.graceUntil) {
				reclaimed += artifact.charge
				count++
			}
		}
	}
	return reclaimed >= neededBytes && count >= neededArtifacts
}

func bumpRevision(window *windowState) {
	if window.revision < math.MaxUint64 {
		window.revision++
	}
}

func (store *Store) requiredInitializationBytesLocked(window *windowState, generation uint64) int64 {
	if generation != window.generation {
		return 0
	}
	var bytes int64
	for _, epoch := range window.epochs {
		if epoch.epoch.Generation != generation {
			continue
		}
		for _, value := range epoch.epoch.Initializations {
			if artifact := window.artifacts[value.ID]; artifact != nil && artifact.visible {
				bytes += artifact.charge
			}
		}
	}
	return bytes
}

func (store *Store) activeBytesForLocked(window *windowState, generation uint64) int64 {
	bytes := window.activeBytes
	if generation == window.generation {
		return bytes
	}
	// An empty prior epoch ceases to be active when this new generation commits.
	// Its files remain separately charged and protected by their grace periods.
	for _, epoch := range window.epochs {
		if epoch.epoch.Generation == window.generation && epoch.segments == 0 {
			bytes -= store.requiredInitializationBytesLocked(window, window.generation)
			break
		}
	}
	return bytes
}

func (store *Store) trimActiveLocked(window *windowState, incoming int64, generation uint64) {
	for len(window.segments) > 0 && store.activeBytesForLocked(window, generation)+incoming > window.options.MaxBytes/3 {
		store.dropOldestLocked(window)
	}
}

func hideArtifactLocked(window *windowState, artifact *artifactState) {
	if artifact.visible {
		artifact.visible = false
		window.activeBytes -= artifact.charge
	}
}

func (store *Store) failLocked(cause error) {
	if store.failure == nil {
		store.failure = cause
	}
}

func (store *Store) retainFailedCleanupLocked(window *windowState, item preparedArtifact, cause error) {
	window.artifacts[item.artifact.ID] = &artifactState{artifact: item.artifact, charge: item.charge}
	window.bytes += item.charge
	store.bytes += item.charge
	store.artifacts++
	store.failLocked(cause)
}

func (store *Store) removeExpiredLocked(window *windowState, artifact *artifactState) {
	if artifact.visible || artifact.readers != 0 || !window.closed && store.options.Now().Before(artifact.graceUntil) {
		return
	}
	if err := window.storage.remove(artifact.artifact.ID); err != nil {
		store.failLocked(err)
		return
	}
	delete(window.artifacts, artifact.artifact.ID)
	window.bytes -= artifact.charge
	store.bytes -= artifact.charge
	store.artifacts--
}

func (store *Store) dropOldestLocked(window *windowState) {
	if len(window.segments) == 0 {
		return
	}
	state := window.segments[0]
	segment := state.segment
	window.segments[0] = nil
	window.segments = window.segments[1:]
	for _, value := range segment.Artifacts {
		artifact := window.artifacts[value.ID]
		if state.advertisedDuration > 0 {
			// RFC 8216 section 6.2.2 starts this period when the URI is
			// removed, not when its last playlist was first requested.
			deadline := store.options.Now().Add(time.Duration(state.advertisedDuration+segment.DurationTicks) * 100 * time.Nanosecond)
			if deadline.After(artifact.graceUntil) {
				artifact.graceUntil = deadline
			}
			if initialization := window.artifacts[value.InitID]; initialization != nil && deadline.After(initialization.graceUntil) {
				initialization.graceUntil = deadline
			}
		}
		hideArtifactLocked(window, artifact)
		store.removeExpiredLocked(window, artifact)
	}
	for _, epoch := range window.epochs {
		if epoch.epoch.Generation == segment.Generation {
			epoch.segments--
			break
		}
	}
	bumpRevision(window)
	store.pruneEpochsLocked(window)
}

func (store *Store) pruneEpochsLocked(window *windowState) {
	kept := window.epochs[:0]
	for _, epoch := range window.epochs {
		if epoch.segments != 0 || !window.closed && epoch.epoch.Generation == window.generation {
			kept = append(kept, epoch)
			continue
		}
		for _, value := range epoch.epoch.Initializations {
			artifact := window.artifacts[value.ID]
			hideArtifactLocked(window, artifact)
			store.removeExpiredLocked(window, artifact)
		}
	}
	window.epochs = kept
}

func (store *Store) expireLocked(window *windowState, now time.Time) {
	cutoff := window.liveEdge - int64(window.options.Window/(100*time.Nanosecond))
	for len(window.segments) > 0 {
		first := window.segments[0]
		if first.segment.StartTicks >= cutoff && now.Sub(first.published) < window.options.Window {
			break
		}
		store.dropOldestLocked(window)
	}
	for _, artifact := range window.artifacts {
		store.removeExpiredLocked(window, artifact)
	}
}

func snapshotLocked(window *windowState) WindowSnapshot {
	result := WindowSnapshot{PresentationID: window.id, Revision: window.revision, NextSequence: window.nextSequence, EarliestTicks: window.liveEdge, LiveEdgeTicks: window.liveEdge,
		LiveStartTicks: window.liveEdge, TargetDurationTicks: window.options.TargetDurationTicks, DiscontinuitySequence: window.discontinuity, Bytes: window.bytes, PendingBytes: window.pendingBytes,
		Ended: window.state.Ended, Stalled: window.state.Stalled, Variants: append([]Variant(nil), window.options.Variants...)}
	if len(window.segments) > 0 {
		result.EarliestTicks = window.segments[0].segment.StartTicks
		result.DiscontinuitySequence = window.segments[0].segment.DiscontinuitySequence
		result.LiveStartTicks = window.segments[0].segment.StartTicks
		if window.options.TargetDurationTicks > 0 {
			// Variable segment durations make a three-segment count unsafe.
			// Use actual accumulated media time and the fixed HLS target.
			for index := len(window.segments) - 1; index >= 0; index-- {
				start := window.segments[index].segment.StartTicks
				if window.liveEdge-start >= 3*window.options.TargetDurationTicks {
					result.LiveStartTicks = start
					break
				}
			}
		} else {
			result.LiveStartTicks = window.segments[max(0, len(window.segments)-3)].segment.StartTicks
		}
	}
	for _, state := range window.segments {
		segment := state.segment
		segment.Artifacts = append([]Artifact(nil), segment.Artifacts...)
		result.Segments = append(result.Segments, segment)
	}
	for _, state := range window.epochs {
		epoch := state.epoch
		epoch.Initializations = append([]Artifact(nil), epoch.Initializations...)
		result.Epochs = append(result.Epochs, epoch)
	}
	return result
}

func (store *Store) Snapshot(ctx context.Context, scope Scope, id string) (WindowSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, true)
	if err != nil {
		return WindowSnapshot{}, err
	}
	return snapshotLocked(window), nil
}

// Advertise binds a response to the exact current snapshot before it is sent.
// A changed revision must be read and rendered again. Each segment remembers
// the longest advertised playlist that contained it; eviction starts a separate
// readable grace period including the segment and that full playlist duration.
func (store *Store) Advertise(ctx context.Context, scope Scope, id string, revision uint64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, true)
	if err != nil {
		return err
	}
	if store.failure != nil {
		return ErrStorage
	}
	if revision != window.revision {
		return ErrSnapshotChanged
	}
	if window.options.TargetDurationTicks == 0 {
		return ErrInvalid
	}
	if len(window.segments) == 0 {
		if window.state.Ended {
			window.advertised = true
			return nil
		}
		return ErrNotBuffered
	}
	duration := window.liveEdge - window.segments[0].segment.StartTicks
	if !window.state.Ended && duration < 3*window.options.TargetDurationTicks {
		return ErrNotBuffered
	}
	if !window.advertised {
		// Establish headroom before the first public promise. A renderer that
		// used the larger private snapshot must retry with the trimmed revision.
		window.advertised = true
		store.trimActiveLocked(window, 0, window.generation)
		if store.failure != nil {
			return ErrStorage
		}
		if window.activeBytes > window.options.MaxBytes/3 {
			return ErrQuota
		}
		if revision != window.revision {
			return ErrSnapshotChanged
		}
	}
	for _, state := range window.segments {
		for _, value := range state.segment.Artifacts {
			artifact := window.artifacts[value.ID]
			if artifact == nil || !artifact.visible {
				return ErrSnapshotChanged
			}
			if value.InitID != "" {
				initialization := window.artifacts[value.InitID]
				if initialization == nil || !initialization.visible {
					return ErrSnapshotChanged
				}
			}
		}
	}
	for _, state := range window.segments {
		state.advertisedDuration = max(state.advertisedDuration, duration)
	}
	return nil
}

// ResolveArtifact includes already advertised, unlisted grace resources. It is
// separate from Snapshot so old playlist URLs remain fetchable without making
// their media positions seekable again. Callers use Variant and Initialization
// to enforce the canonical resource extension before opening the opaque ID.
func (store *Store) ResolveArtifact(ctx context.Context, scope Scope, id, artifactID string) (Artifact, Variant, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, true)
	if err != nil {
		return Artifact{}, Variant{}, err
	}
	artifact := window.artifacts[artifactID]
	if artifact == nil || !artifact.visible && !store.options.Now().Before(artifact.graceUntil) {
		return Artifact{}, Variant{}, ErrNotFound
	}
	for _, variant := range window.options.Variants {
		if variant.ID == artifact.artifact.VariantID {
			return artifact.artifact, variant, nil
		}
	}
	return Artifact{}, Variant{}, ErrStorage
}

// RetainsArtifact checks current or advertised-grace availability without
// renewing the consumer lease. Background subtitle pruning can retain matching
// cues without keeping an abandoned playback alive. A missing artifact returns
// false with no error; invalid ownership and expired presentations still fail.
func (store *Store) RetainsArtifact(ctx context.Context, scope Scope, id, artifactID string) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, false)
	if err != nil {
		return false, err
	}
	artifact := window.artifacts[artifactID]
	return artifact != nil && (artifact.visible || store.options.Now().Before(artifact.graceUntil)), nil
}

func (store *Store) Seek(ctx context.Context, scope Scope, id string, ticks int64) (Position, error) {
	if ticks < 0 {
		return Position{}, ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, true)
	if err != nil {
		return Position{}, err
	}
	return seekLocked(window, ticks)
}

func seekLocked(window *windowState, ticks int64) (Position, error) {
	if ticks >= window.liveEdge {
		return Position{}, ErrNotBuffered
	}
	if len(window.segments) == 0 || ticks < window.segments[0].segment.StartTicks {
		return Position{}, ErrWindowExpired
	}
	for _, state := range window.segments {
		segment := state.segment
		if ticks < segment.StartTicks+segment.DurationTicks {
			return Position{Sequence: segment.Sequence, Generation: segment.Generation, StartTicks: segment.StartTicks, RequestedTicks: ticks}, nil
		}
	}
	return Position{}, ErrNotBuffered
}

func (store *Store) Live(ctx context.Context, scope Scope, id string) (Position, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, true)
	if err != nil {
		return Position{}, err
	}
	return seekLocked(window, snapshotLocked(window).LiveStartTicks)
}

func (store *Store) SetState(ctx context.Context, scope Scope, id string, state State) error {
	if state.Ended && state.Stalled {
		return ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, false)
	if err != nil {
		return err
	}
	if window.state.Ended && !state.Ended {
		return ErrEnded
	}
	if window.publishing {
		return ErrBusy
	}
	if window.state != state {
		window.state = state
		bumpRevision(window)
	}
	return nil
}

func (store *Store) Usage() Usage {
	store.mu.Lock()
	defer store.mu.Unlock()
	return Usage{Bytes: store.bytes, PendingBytes: store.pendingBytes, Windows: len(store.windows), Artifacts: store.artifacts,
		Readers: store.readers, Publishing: store.publishing}
}

// Touch renews an already authorized consumer lease without copying its whole
// metadata snapshot. It is intended for explicit playback/pause keepalives;
// background media production must not call it as a substitute for a client.
func (store *Store) Touch(ctx context.Context, scope Scope, id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	_, err := store.checkedWindow(ctx, scope, id, true)
	return err
}

func (store *Store) ClosePresentation(ctx context.Context, scope Scope, id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, false)
	if err != nil {
		return err
	}
	store.revokeLocked(window)
	if store.failure != nil {
		return ErrStorage
	}
	return nil
}

func (store *Store) revokeLocked(window *windowState) {
	window.closed = true
	window.cancel()
	for reader := range window.readers {
		store.closeReaderLocked(reader)
	}
	for _, artifact := range window.artifacts {
		hideArtifactLocked(window, artifact)
		store.removeExpiredLocked(window, artifact)
	}
	window.segments, window.epochs = nil, nil
	store.destroyClosedLocked(window)
}

func (store *Store) destroyClosedLocked(window *windowState) {
	if !window.closed || window.publishing || len(window.artifacts) != 0 || len(window.readers) != 0 {
		return
	}
	if _, exists := store.windows[window.id]; !exists {
		return
	}
	if err := window.storage.destroy(); err != nil {
		store.failLocked(err)
		return
	}
	if err := window.storage.close(); err != nil {
		store.failLocked(err)
	}
	delete(store.windows, window.id)
}

func (store *Store) maintain() {
	defer store.workers.Done()
	ticker := time.NewTicker(store.options.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-store.ctx.Done():
			return
		case <-ticker.C:
		}
		store.mu.Lock()
		now := store.options.Now()
		for _, window := range store.windows {
			if window.closed || now.Sub(window.accessed) >= store.options.IdleTimeout {
				store.revokeLocked(window)
			} else {
				store.expireLocked(window, now)
			}
		}
		store.mu.Unlock()
	}
}

func (store *Store) Close(ctx context.Context) error {
	store.mu.Lock()
	if !store.closing {
		store.closing = true
		store.cancel()
		for _, window := range store.windows {
			store.revokeLocked(window)
		}
		go func() {
			store.workers.Wait()
			store.mu.Lock()
			for _, window := range store.windows {
				store.revokeLocked(window)
				_ = window.storage.close()
			}
			store.closeErr = errors.Join(store.failure, store.storage.close())
			store.mu.Unlock()
			close(store.done)
		}()
	}
	store.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-store.done:
		return store.closeErr
	}
}
