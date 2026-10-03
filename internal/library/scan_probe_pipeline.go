package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

const (
	// The same ticket covers preparation, a running probe, a ready result,
	// ordered publication, and descriptor retirement. There is no separate
	// unbounded completed-results queue behind a slow first file.
	scanProbeWindowLimit = 2
	// This is a retained-facts budget, independent of descriptor and process
	// limits. Process output/parser working memory belongs to the media runner.
	scanProbeFactsBytes = 16 << 20
)

var errScanProbeSourceChanged = errors.New("scanned source facts changed before publication")
var errScanProbeFactsBudget = errors.New("scanned probe facts exceed the retained-result budget")

type scanProbeAuthority struct {
	jobID, childID, libraryID string
	root                      libraryRoot
	capture                   *rootBindingScanCapture
	observation               storageObservationLifetime
	closed                    chan struct{}
}

type scanProbeResult struct {
	info media.Info
	err  error
}

type scanInputInspectionRejection struct{ err error }

func (rejection *scanInputInspectionRejection) Error() string { return rejection.err.Error() }
func (rejection *scanInputInspectionRejection) Unwrap() error { return rejection.err }

// Only the original business rejection may pass through RootIO's nested joins.
// A separate close, release or ownership failure must never become a warning.
func scanInputInspectionRejectedOnly(err error, rejection *scanInputInspectionRejection) bool {
	if rejection == nil || err == nil {
		return false
	}
	if current, ok := err.(*scanInputInspectionRejection); ok {
		return current == rejection
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return false
	}
	children := joined.Unwrap()
	if len(children) == 0 {
		return false
	}
	for _, child := range children {
		if child != nil && !scanInputInspectionRejectedOnly(child, rejection) {
			return false
		}
	}
	return true
}

type scanProbePending struct {
	path, kind string
	hierarchy  hierarchy
	input      *scannedMediaInput
	result     chan scanProbeResult
}

type scanProbeWindow struct {
	state   *scanState
	ctx     context.Context
	cancel  context.CancelFunc
	workers sync.WaitGroup
	pending []*scanProbePending
	closed  bool
}

// Only roots retaining complete independent named-storage witnesses use the
// pipeline. Unbound/unsupported roots and the bounded individual-root fallback
// retain the synchronous scanner rather than inventing weaker publication
// authority. A binding revision or an old os.Root alone is not such a witness.
func (state *scanState) newScanProbeWindow() *scanProbeWindow {
	if state.reconciliationPass == nil {
		return nil
	}
	capture := state.reconciliationPass.byRoot[state.root.id]
	if capture == nil || capture.status != RootBindingVerified || capture.closed || capture.capture == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(state.task.ctx)
	return &scanProbeWindow{state: state, ctx: media.WithBackgroundProcess(ctx), cancel: cancel,
		pending: make([]*scanProbePending, 0, scanProbeWindowLimit)}
}

func (window *scanProbeWindow) submit(path, kind string, current hierarchy) (resultErr error) {
	if window.closed {
		return ErrUnavailable
	}
	if err := window.ctx.Err(); err != nil {
		return err
	}
	// Never wait for a ticket while the only publisher is this goroutine.
	// Retire the oldest input before opening another descriptor.
	if len(window.pending) == scanProbeWindowLimit {
		if err := window.acceptOldest(); err != nil {
			return err
		}
	}
	state := window.state
	warnings := state.warnings
	input, err := state.prepareScannedMedia(path, kind, scannedRoleOrdinary)
	if err != nil || input == nil {
		if state.warnings != warnings {
			state.failThemeDirectory(filepath.Dir(path))
		}
		return err
	}
	if !scanProbeFactsFit(input.stored, scanProbeFactsBytes) {
		if err := input.close(); err != nil {
			return err
		}
		state.warnings++
		state.failThemeDirectory(filepath.Dir(path))
		return state.store.persistProgress(state.task)
	}
	if input.unchanged {
		// Warm scans preserve the original cached visit and no-op write path.
		// Flush earlier cold inputs before sidecars, hierarchy, or evidence.
		defer func() { resultErr = errors.Join(resultErr, input.close()) }()
		if err := window.flush(); err != nil {
			return err
		}
		warnings = state.warnings
		accepted, err := state.acceptScannedMedia(input, kind, nil)
		if err == nil && accepted {
			err = state.publishScannedMedia(path, kind, current, input)
		}
		if state.warnings != warnings {
			state.failThemeDirectory(filepath.Dir(path))
		}
		return err
	}
	capture := state.reconciliationPass.byRoot[state.root.id]
	input.authority = &scanProbeAuthority{jobID: state.task.job.ID, childID: state.task.job.TaskChildID,
		libraryID: state.library.ID, root: state.root, capture: capture, closed: make(chan struct{})}
	work := &scanProbePending{path: path, kind: kind, hierarchy: current, input: input,
		result: make(chan scanProbeResult, 1)}
	window.pending = append(window.pending, work)
	// The worker borrows this immutable input and only probes. Its capacity-one
	// handoff cannot block cleanup when a previous result fails publication.
	window.workers.Add(1)
	go func() {
		defer window.workers.Done()
		returned := false
		defer func() {
			_ = recover()
			if !returned {
				work.result <- scanProbeResult{err: scanReadFailure(media.ErrProcessRetirementUnknown)}
			}
		}()
		work.result <- runScanProbe(window.ctx, state.store.prober, input)
		returned = true
	}()
	return nil
}

func runScanProbe(ctx context.Context, prober Prober, input *scannedMediaInput) scanProbeResult {
	if input.primary == nil {
		return scanProbeResult{err: scanReadFailure(ErrUnavailable)}
	}
	info, err := input.primary.probeContext(ctx, prober, input.file)
	if err != nil {
		return scanProbeResult{err: err}
	}
	// Enforce the retained-result envelope before handing a ready result to
	// the queue. A blocked oldest input must never retain an over-budget
	// later result. The serial walker records its warning and checkpoint.
	if err == nil && !scanProbeFactsFit(struct {
		Stored storedFile
		Info   media.Info
	}{input.stored, info}, scanProbeFactsBytes) {
		info, err = media.Info{}, errScanProbeFactsBudget
	}
	return scanProbeResult{info: info, err: err}
}

func (window *scanProbeWindow) flush() error {
	for len(window.pending) != 0 {
		if err := window.acceptOldest(); err != nil {
			return err
		}
	}
	return window.ctx.Err()
}

func (window *scanProbeWindow) acceptOldest() (resultErr error) {
	work := window.pending[0]
	var result scanProbeResult
	select {
	case result = <-work.result:
	case <-window.ctx.Done():
		return window.ctx.Err()
	}
	// Remove only after the worker has returned. This defer closes the input
	// after every serial publication step, before the ticket can be reused.
	copy(window.pending, window.pending[1:])
	window.pending[len(window.pending)-1] = nil
	window.pending = window.pending[:len(window.pending)-1]
	defer func() {
		if resultErr != nil {
			// A timed-out storage proof can retain this file while its actual
			// syscall finishes. Cancel siblings before waiting for that retirement,
			// rather than depending on walk's later window-close defer.
			window.cancel()
		}
		resultErr = errors.Join(resultErr, work.input.close())
	}()
	state := window.state
	warnings := state.warnings
	defer func() {
		if state.warnings != warnings {
			state.failThemeDirectory(filepath.Dir(work.path))
		}
	}()
	work.input.probe = &result.info
	if result.err == nil && !scanProbeFactsFit(struct {
		Stored storedFile
		Info   media.Info
	}{work.input.stored, result.info}, scanProbeFactsBytes) {
		state.warnings++
		return state.store.persistProgress(state.task)
	}
	accepted, err := state.acceptScannedMedia(work.input, work.kind, result.err)
	if err != nil || !accepted {
		return err
	}
	err = state.publishScannedMedia(work.path, work.kind, work.hierarchy, work.input)
	var failure *primaryScanReadFailure
	if !errors.As(err, &failure) && !errors.Is(err, media.ErrProcessRetirementUnknown) &&
		!errors.Is(err, errScanPublicationRetirementUnknown) && !errors.Is(err, errSidecarRollbackUnknown) &&
		(errors.Is(err, errScanProbeSourceChanged) || errors.Is(err, errScannedMediaRoleConflict)) {
		// publishScannedMedia has already rolled back and released ownership.
		// Rejected facts never reach sidecars or accepted-identity evidence.
		state.warnings++
		return state.store.persistProgress(state.task)
	}
	return err
}

func (window *scanProbeWindow) close() (resultErr error) {
	if window.closed {
		return nil
	}
	window.closed = true
	window.cancel()
	// Cancellation abandons publication, never the actual worker. A prober
	// stuck in I/O keeps its descriptor and scan ownership until it returns.
	window.workers.Wait()
	for _, work := range window.pending {
		resultErr = errors.Join(resultErr, work.input.close())
	}
	window.pending = nil
	return resultErr
}

// The walker owns both this input and its rooted directory. It joins each
// actual observation before returning to walk's directory/root close. A timed
// out proof therefore releases the writer transaction while retaining borrowed
// descriptors until the underlying filesystem operation actually finishes.
func (input *scannedMediaInput) close() error {
	if input == nil {
		return nil
	}
	closeFile := func() error {
		if input.file == nil {
			return nil
		}
		if input.authority == nil {
			return input.file.Close()
		}
		authority := input.authority
		var closeErr error
		err := authority.observation.retire(func() error { defer close(authority.closed); closeErr = input.file.Close(); return closeErr })
		<-authority.closed
		return errors.Join(err, closeErr)
	}
	if input.primary != nil {
		err := input.primary.close(closeFile)
		if err != nil {
			input.primary.state.recordPrimaryScanReadFailure(err)
		}
		return err
	}
	return closeFile()
}

// prepareScannedMedia is owned by the serial walker. Claim ordering remains
// the directory order even if probes finish in the opposite order.
func (state *scanState) prepareScannedMedia(path, kind string, role scannedMediaRole) (_ *scannedMediaInput, resultErr error) {
	primary, err := state.preparePrimaryScanRead()
	if err != nil {
		return nil, scanReadFailure(err)
	}
	input := &scannedMediaInput{primary: primary}
	accepted := false
	defer func() {
		if !accepted {
			resultErr = errors.Join(resultErr, input.close())
		}
	}()
	var file *os.File
	var info os.FileInfo
	var rejected *scanInputInspectionRejection
	err = state.runPrimaryScanMetadataWithRouting(state.task.ctx, primary.row, func(context.Context) error {
		var err error
		file, err = openScanFile(state.opened, path)
		if err != nil {
			rejected = &scanInputInspectionRejection{err: err}
			return rejected
		}
		input.file = file
		// Retain the exact FD even if its first Stat rejects a nonregular input.
		primary.file, primary.path = file, path
		info, err = file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			rejected = &scanInputInspectionRejection{err: errors.Join(err, errScanProbeSourceChanged)}
			return rejected
		}
		return primary.attach(file, path)
	})
	if err != nil {
		if scanInputInspectionRejectedOnly(err, rejected) {
			state.warnings++
			return nil, nil
		}
		return nil, scanReadFailure(err)
	}
	state.task.job.Scanned++
	if err := state.store.persistProgress(state.task); err != nil {
		return nil, err
	}
	stored, err := state.findStoredFileForRole(filepath.ToSlash(path), info, role)
	if errors.Is(err, errScannedMediaRoleConflict) {
		state.warnings++
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state.themes != nil && stored.id != "" {
		claims := state.themes.claimed
		if state.themeLibrary != nil {
			claims = state.themeLibrary.claimed
		}
		key := string(role) + ":" + state.root.id + ":" + filepath.ToSlash(path)
		if previous, exists := claims[stored.id]; exists && previous != key {
			stored = storedFile{}
		} else {
			claims[stored.id] = key
		}
	}
	probe := stored.media
	unchanged := !state.task.job.ForceProbe && probe != nil && stored.size == info.Size() && stored.modified != nil &&
		stored.modified.Equal(catalogModifiedTime(info)) && (stored.identity == "" || stored.identity == fileIdentity(info))
	versioned, checksVersion := state.store.prober.(interface{ CacheVersion() int })
	if checksVersion {
		unchanged = unchanged && probe.ProbeVersion == versioned.CacheVersion() &&
			probe.FileChangeTimeNs > 0 && probe.FileChangeTimeNs == media.FileChangeTime(info)
	}
	if versioned, ok := state.store.prober.(interface{ MusicMetadataVersion() int }); kind == "audio" && ok {
		unchanged = unchanged && probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == versioned.MusicMetadataVersion()
	}
	accepted = true
	input.info, input.stored, input.probe = info, stored, probe
	input.unchanged, input.checksVersion = unchanged, checksVersion
	return input, nil
}

func (state *scanState) acceptScannedMedia(input *scannedMediaInput, kind string, probeErr error) (accepted bool, resultErr error) {
	if probeErr != nil {
		var failure *primaryScanReadFailure
		if errors.As(probeErr, &failure) || errors.Is(probeErr, media.ErrProcessRetirementUnknown) {
			return false, probeErr
		}
		if state.task.ctx.Err() != nil {
			return false, state.task.ctx.Err()
		}
		state.warnings++
		return false, state.store.persistProgress(state.task)
	}
	if !input.unchanged {
		if input.primary == nil {
			return false, scanReadFailure(ErrUnavailable)
		}
		operation, err := input.primary.preparePublicationIO()
		if err != nil {
			return false, scanReadFailure(err)
		}
		defer func() {
			if err := closeScanPublicationIO(operation); err != nil {
				accepted = false
				resultErr = errors.Join(resultErr, err)
			}
		}()
		var after os.FileInfo
		var statErr error
		if err := input.primary.runPublicationMetadata(operation, func(context.Context) error {
			after, statErr = input.file.Stat()
			return nil
		}); err != nil {
			return false, err
		}
		err = statErr
		if err != nil || !os.SameFile(input.info, after) || after.Size() != input.info.Size() ||
			!after.ModTime().Equal(input.info.ModTime()) ||
			((input.authority != nil || input.checksVersion) && media.FileChangeTime(after) != media.FileChangeTime(input.info)) {
			state.warnings++
			return false, state.store.persistProgress(state.task)
		}
	}
	if err := state.task.ctx.Err(); err != nil {
		return false, err
	}
	probe := input.probe
	if kind == "audio" && probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == media.CurrentMusicMetadataVersion {
		if !validTrackMusic(*probe.EmbeddedMusic) {
			state.warnings++
			return false, state.store.persistProgress(state.task)
		}
		raw, err := json.Marshal(probe.EmbeddedMusic)
		if _, valid := acceptedTrackMusic(raw); err != nil || !valid {
			state.warnings++
			return false, state.store.persistProgress(state.task)
		}
	}
	return true, nil
}

// The bounded walker counts inline storage once, then dynamic payloads. It
// stops before iterating an over-budget container. It bounds accepted facts,
// not allocator overhead or temporary media-parser working memory.
func scanProbeFactsFit(value any, budget int64) bool {
	if budget < 0 {
		return false
	}
	var visit func(reflect.Value, bool) bool
	charge := func(size uint64) bool {
		if size > uint64(budget) {
			return false
		}
		budget -= int64(size)
		return true
	}
	visit = func(value reflect.Value, inline bool) bool {
		if !value.IsValid() {
			return true
		}
		if inline && !charge(uint64(value.Type().Size())) {
			return false
		}
		switch value.Kind() {
		case reflect.String:
			return charge(uint64(value.Len()))
		case reflect.Pointer:
			return value.IsNil() || visit(value.Elem(), true)
		case reflect.Struct:
			// Locations are immutable shared timezone tables, not retained probe
			// payloads. Do not walk their internal caches or pointer graphs.
			if value.Type() == reflect.TypeFor[time.Time]() {
				return true
			}
			for i := 0; i < value.NumField(); i++ {
				if !visit(value.Field(i), false) {
					return false
				}
			}
		case reflect.Slice, reflect.Array:
			if value.Kind() == reflect.Slice {
				size := uint64(value.Type().Elem().Size())
				if size != 0 && uint64(value.Cap()) > uint64(budget)/size {
					return false
				}
				if !charge(uint64(value.Cap()) * size) {
					return false
				}
			}
			for i := 0; i < value.Len(); i++ {
				if !visit(value.Index(i), false) {
					return false
				}
			}
		case reflect.Map:
			size := uint64(value.Type().Key().Size()+value.Type().Elem().Size()) + 64
			if uint64(value.Len()) > uint64(budget)/size || !charge(uint64(value.Len())*size) {
				return false
			}
			entries := value.MapRange()
			for entries.Next() {
				if !visit(entries.Key(), false) || !visit(entries.Value(), false) {
					return false
				}
			}
		}
		return true
	}
	return visit(reflect.ValueOf(value), true)
}

// This adapter borrows the existing primary-item transaction; it neither
// commits it nor opens a second checkout while catalog ownership is held.
type scanProbeAuthorityTx struct {
	ctx context.Context
	tx  pgx.Tx
}

func (tx scanProbeAuthorityTx) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	return tx.tx.Exec(tx.ctx, statement, args...)
}

func (tx scanProbeAuthorityTx) QueryRow(statement string, args ...any) OwnedRow {
	return tx.tx.QueryRow(tx.ctx, statement, args...)
}

func (tx scanProbeAuthorityTx) Query(statement string, args ...any) (OwnedRows, error) {
	return tx.tx.Query(tx.ctx, statement, args...)
}

func (state *scanState) checkScanProbeAuthority(tx pgx.Tx, path string, input *scannedMediaInput) error {
	return state.checkScanProbeAuthorityWithRelation(tx, path, input, nil)
}

// A publication may supply task rows already locked before any root or item
// rows. Reusing that exact transaction's relation preserves the task lock order.
func (state *scanState) checkScanProbeAuthorityWithRelation(tx pgx.Tx, path string, input *scannedMediaInput, locked *taskScanRelation) error {
	authority := input.authority
	if authority == nil || authority.root != state.root || authority.libraryID != state.library.ID ||
		authority.jobID != state.task.job.ID || authority.childID != state.task.job.TaskChildID {
		return ErrTaskScanInactive
	}
	// Ownership is already held, preserving the ownership -> Store lock order.
	state.store.mu.Lock()
	active := !state.store.closed && !state.store.closing.Load() && state.store.active[authority.jobID] == state.task &&
		state.store.rootBindingPathConfiguredLocked(authority.root.allowedPath)
	state.store.mu.Unlock()
	if !active {
		return ErrTaskScanInactive
	}
	var relation taskScanRelation
	var err error
	if locked != nil {
		relation = *locked
	} else {
		relation, err = lockTaskScanRelation(scanProbeAuthorityTx{ctx: state.task.ctx, tx: tx}, authority.jobID, authority.childID)
		if err != nil {
			return err
		}
	}
	if relation.missing || relation.job.ID != authority.jobID || relation.job.LibraryID != authority.libraryID || relation.job.Status != "Running" ||
		relation.job.TaskChildID != authority.childID || relation.job.ForceProbe != state.task.job.ForceProbe {
		return ErrTaskScanInactive
	}
	if relation.job.CancelRequested || relation.child != nil && (!activeTaskRun(relation.child.runState) || relation.child.state != "running") {
		return context.Canceled
	}
	capture := authority.capture
	if capture == nil || capture.closed || capture.status != RootBindingVerified || capture.capture == nil {
		return ErrRootBindingConflict
	}
	current, err := readRootBindingForUpdate(state.task.ctx, tx, authority.libraryID, authority.root.id)
	if err != nil {
		return err
	}
	if !capture.row.same(current) || current.root != authority.root {
		return ErrRootBindingConflict
	}
	if err := state.revalidateScanProbeStorage(tx, path, input); err != nil {
		return err
	}
	stored := input.stored
	relative := filepath.ToSlash(path)
	ancestors := []string{relative}
	for parent := relative; strings.LastIndexByte(parent, '/') >= 0; {
		parent = parent[:strings.LastIndexByte(parent, '/')]
		ancestors = append(ancestors, parent)
	}
	var reserved bool
	if err := tx.QueryRow(state.task.ctx, `SELECT EXISTS (
		SELECT 1 FROM theme_reserved_paths WHERE root_id=$1 AND relative_path=ANY($2::text[])
		AND (is_directory OR relative_path=$3)
		UNION ALL
		SELECT 1 FROM extra_reserved_paths WHERE root_id=$1 AND relative_path=ANY($2::text[])
		AND (is_directory OR relative_path=$3))`, authority.root.id, ancestors, relative).Scan(&reserved); err != nil {
		return err
	}
	if reserved {
		return errScannedMediaRoleConflict
	}
	if stored.id != "" {
		current, err := readStoredFile(scannedRoleRow{row: tx.QueryRow(state.task.ctx,
			"SELECT "+storedFileColumns+", ("+ordinaryItemSQL("items")+") FROM items WHERE id=$1 AND library_id=$2 FOR UPDATE OF items",
			stored.id, authority.libraryID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return errScanProbeSourceChanged
		}
		if err != nil {
			return err
		}
		if !sameScanProbeSource(current, stored) {
			return errScanProbeSourceChanged
		}
	}
	// A retained rename candidate may own another pathname. The target must
	// still be vacant; a newly occupied target cannot borrow that candidate ID.
	var occupant string
	err = tx.QueryRow(state.task.ctx, `SELECT id FROM items WHERE root_id=$1 AND relative_path=$2 FOR UPDATE`,
		authority.root.id, relative).Scan(&occupant)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	wantOccupant := ""
	if stored.rootID == authority.root.id && stored.relativePath == relative {
		wantOccupant = stored.id
	}
	if occupant != wantOccupant {
		return errScanProbeSourceChanged
	}
	return state.task.ctx.Err()
}

func (state *scanState) checkScanProbeFile(path string, input *scannedMediaInput) error {
	if input.primary == nil {
		return scanReadFailure(ErrUnavailable)
	}
	return input.primary.runPublicationMetadata(input.primary.publicationIO, func(ctx context.Context) error {
		return checkScanProbeFileAt(ctx, state.opened, input.file, input.info, path)
	})
}

func (state *scanState) revalidateScanProbeStorage(tx pgx.Tx, path string, input *scannedMediaInput) error {
	authority := input.authority
	if authority == nil || authority.capture == nil {
		return ErrRootBindingConflict
	}
	// Capture immutable values before the observation starts. The worker cannot
	// touch scanner state, Store locks, or its borrowed database transaction.
	owned, ok := tx.(*ownedTx)
	if !ok || owned.ctx == nil {
		return ErrUnavailable
	}
	deadline, ok := owned.ctx.Deadline()
	if !ok {
		return ErrUnavailable
	}
	// Storage proof never spends the protected transaction's rollback reserve.
	// Each observation also retains its independent five-second runtime bound.
	ctx, cancel := context.WithDeadline(state.task.ctx, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	root, file, expected := state.opened, input.file, input.info
	capture := authority.capture
	if input.primary == nil || input.primary.publicationIO == nil {
		return scanReadFailure(ErrUnavailable)
	}
	err := input.primary.publicationIO.RunImmediate(ctx, authority.root.id, primaryio.Background, func(work context.Context) error {
		return runStorageObservation(work, []*storageObservationLifetime{&capture.observation, &authority.observation},
			func(observation context.Context) error {
				if err := capture.Revalidate(observation); err != nil {
					return err
				}
				return checkScanProbeFileAt(observation, root, file, expected, path)
			})
	})
	if err != nil {
		return fmt.Errorf("revalidate cold probe storage: %w", err)
	}
	return nil
}

// An automatic source projection separates editable display columns from the
// captured source facts. Publishing still reads current overrides and locks;
// concurrent edits must not discard the independently completed probe.
func sameScanProbeSource(current, prepared storedFile) bool {
	if current.automatic != nil && prepared.automatic != nil {
		current.name, current.sortName, current.overview = prepared.name, prepared.sortName, prepared.overview
		if prepared.itemType == "Episode" || prepared.itemType == "Audio" {
			current.indexNumber = prepared.indexNumber
		}
		if prepared.itemType == "Audio" {
			current.parentIndexNumber = prepared.parentIndexNumber
		}
	}
	return reflect.DeepEqual(current, prepared)
}

func checkScanProbeFileAt(ctx context.Context, root *os.Root, file *os.File, expected os.FileInfo, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil {
		return errScanProbeSourceChanged
	}
	current, err := root.Lstat(path)
	if err != nil {
		return errScanProbeSourceChanged
	}
	for _, info := range []os.FileInfo{opened, current} {
		if !info.Mode().IsRegular() || !os.SameFile(expected, info) || info.Size() != expected.Size() ||
			!info.ModTime().Equal(expected.ModTime()) || media.FileChangeTime(info) != media.FileChangeTime(expected) {
			return errScanProbeSourceChanged
		}
	}
	return ctx.Err()
}
