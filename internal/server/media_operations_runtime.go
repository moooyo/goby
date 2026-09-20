package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// Execution inventory is captured by the trusted process configuration, never
// by the HTTP body. Paths and hashes remain private operation witnesses.
type mediaOperationExecutionSnapshot struct {
	Configuration config.MediaOperationsConfig `json:"Configuration"`
	FFmpegPath    string                       `json:"FFmpegPath"`
	FFprobePath   string                       `json:"FFprobePath"`
	FFmpegSHA256  string                       `json:"FFmpegSHA256"`
	FFprobeSHA256 string                       `json:"FFprobeSHA256"`
}

type mediaOperationExecutor interface {
	Execute(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) (library.MediaOperationResult, error)
	Apply(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) error
	Discard(context.Context, library.MediaOperationWork) error
}

type mediaOperationExecution struct {
	work   library.MediaOperationWork
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// The coordinator owns all worker handles until processes and storage work have
// returned. Cancellation or lost SQL ownership never transfers a live syscall.
type mediaOperationsRuntime struct {
	server            *Server
	store             *library.Store
	loopStarted       bool
	processingEnabled bool
	configuration     config.MediaOperationsConfig
	inventory         mediaOperationExecutionSnapshot
	toolsReady        bool
	ocrReady          bool
	ctx               context.Context
	cancel            context.CancelFunc
	wake              chan struct{}
	done              chan struct{}
	mu                sync.Mutex
	closing           bool
	closeOnce         sync.Once
	operations        sync.WaitGroup
	workers           map[string]*mediaOperationExecution
	executors         map[string]mediaOperationExecutor
}

func newMediaOperationsRuntime(ctx context.Context, s *Server) (*mediaOperationsRuntime, error) {
	if s == nil || s.library == nil {
		return nil, library.ErrUnavailable
	}
	if err := s.cfg.MediaOperations.Validate(); err != nil {
		return nil, err
	}
	if err := s.library.RecoverMediaOperations(ctx); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	r := &mediaOperationsRuntime{server: s, store: s.library, configuration: s.cfg.MediaOperations, ctx: lifetime, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), workers: map[string]*mediaOperationExecution{}, executors: map[string]mediaOperationExecutor{}}
	r.inventory.Configuration = s.cfg.MediaOperations
	if s.cfg.MediaOperations.Enabled && runtime.GOOS == "linux" {
		probe, probeHash, probeErr := mediaOperationToolIdentity(ctx, s.cfg.FFprobePath)
		ffmpeg, ffmpegHash, ffmpegErr := mediaOperationToolIdentity(ctx, s.cfg.FFmpegPath)
		r.inventory.FFprobePath, r.inventory.FFprobeSHA256 = probe, probeHash
		r.inventory.FFmpegPath, r.inventory.FFmpegSHA256 = ffmpeg, ffmpegHash
		r.toolsReady = probeErr == nil && ffmpegErr == nil
		r.ocrReady = probeErr == nil && mediaOperationOCRInventory(ctx, s.cfg.MediaOperations.OCR) == nil
	}
	r.executors[library.MediaOperationRemoveSubtitle] = mediaOperationBuiltinExecutor{runtime: r, kind: library.MediaOperationRemoveSubtitle}
	r.executors[library.MediaOperationOCR] = mediaOperationBuiltinExecutor{runtime: r, kind: library.MediaOperationOCR}
	r.processingEnabled = r.Available()
	r.loopStarted = true
	go r.loop()
	return r, nil
}

func (r *mediaOperationsRuntime) Available() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	ready := !r.closing && r.configuration.Enabled && (r.ocrReady || r.toolsReady && len(r.configuration.WritableProfiles) > 0)
	r.mu.Unlock()
	return ready && r.store.Available()
}
func (r *mediaOperationsRuntime) Wake() {
	if r != nil {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}
func (r *mediaOperationsRuntime) enter() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return false
	}
	r.operations.Add(1)
	return true
}
func (r *mediaOperationsRuntime) BeginClose() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		r.cancel()
		r.mu.Unlock()
		if !r.loopStarted {
			go func() { r.operations.Wait(); close(r.done) }()
		}
		r.Wake()
	})
}
func (r *mediaOperationsRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.BeginClose()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *mediaOperationsRuntime) authorize(ctx context.Context, actor identity.Principal) error {
	if r == nil {
		return library.ErrUnavailable
	}
	tx, err := r.server.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *mediaOperationsRuntime) Capabilities(ctx context.Context, actor identity.Principal) (adminMediaOperationCapabilities, error) {
	result := adminMediaOperationCapabilities{WritableProfiles: []string{}, OCR: adminMediaOCRCapabilities{Models: []adminMediaOCRModel{}, OutputFormats: []string{}}}
	if err := r.authorize(ctx, actor); err != nil {
		return result, err
	}
	result.Enabled = r.configuration.Enabled
	result.Available = r.Available()
	result.MaxConcurrent = r.configuration.MaxConcurrent
	result.MaxQueued = r.configuration.MaxQueued
	if !result.Enabled {
		result.UnavailableReason = "disabled"
	} else if runtime.GOOS != "linux" {
		result.UnavailableReason = "linux_required"
	} else if !result.Available {
		result.UnavailableReason = "execution_inventory_unavailable"
	}
	if r.toolsReady {
		result.WritableProfiles = append(result.WritableProfiles, r.configuration.WritableProfiles...)
	}
	result.OCR.Available = result.Available && r.ocrReady
	if !result.OCR.Available {
		result.OCR.UnavailableReason = "ocr_inventory_unavailable"
	} else {
		result.OCR.OutputFormats = []string{"srt", "vtt"}
	}
	for _, model := range r.configuration.OCR.Models {
		result.OCR.Models = append(result.OCR.Models, adminMediaOCRModel{ID: model.ID, Language: model.Language})
	}
	return result, nil
}

func (r *mediaOperationsRuntime) Start(ctx context.Context, actor identity.Principal, request library.MediaOperationRequest) (library.MediaOperationAdmission, error) {
	var result library.MediaOperationAdmission
	if !r.enter() {
		return result, library.ErrUnavailable
	}
	defer r.operations.Done()
	defer r.Wake()
	if err := r.authorize(ctx, actor); err != nil {
		return result, err
	}
	if replay, found, err := r.store.ReplayMediaOperationRequest(ctx, actor, request); err != nil || found {
		return replay, err
	}
	if !r.Available() {
		return result, library.ErrUnavailable
	}
	if err := r.verifyInventory(ctx, r.inventory, request.Kind); err != nil {
		return result, err
	}
	switch request.Kind {
	case library.MediaOperationRemoveSubtitle:
		if !r.toolsReady || len(r.configuration.WritableProfiles) == 0 {
			return result, library.ErrUnavailable
		}
		target, err := r.store.GetMediaOperationTarget(ctx, actor, request.ItemID)
		if err != nil {
			return result, err
		}
		profile := ""
		switch target.Container {
		case "mkv", "mka":
			profile = "matroska-v1"
		case "mp4":
			profile = "mp4-movtext-v1"
		}
		if profile == "" || !slices.Contains(r.configuration.WritableProfiles, profile) || request.Parameters.Profile != "" && request.Parameters.Profile != profile {
			return result, library.ErrInvalidInput
		}
	case library.MediaOperationOCR:
		if !r.ocrReady {
			return result, library.ErrUnavailable
		}
		if len(request.Parameters.ModelIDs) < 1 || len(request.Parameters.ModelIDs) > 3 {
			return result, library.ErrInvalidInput
		}
		for _, id := range request.Parameters.ModelIDs {
			found := false
			for _, model := range r.configuration.OCR.Models {
				found = found || model.ID == id
			}
			if !found {
				return result, library.ErrInvalidInput
			}
		}
	default:
		return result, library.ErrInvalidInput
	}
	encoded, err := json.Marshal(r.inventory)
	if err != nil {
		return result, err
	}
	request.ExecutionSnapshot = encoded
	request.MaxQueued = r.configuration.MaxQueued
	return r.store.StartMediaOperation(ctx, actor, request)
}

func (r *mediaOperationsRuntime) Apply(ctx context.Context, actor identity.Principal, id string, request library.MediaOperationApplyRequest) (library.MediaOperationAdmission, error) {
	if !r.enter() {
		return library.MediaOperationAdmission{}, library.ErrUnavailable
	}
	defer r.operations.Done()
	defer r.Wake()
	if !r.processingEnabled {
		return r.inactiveApplyReceipt(ctx, actor, id, request, false)
	}
	return r.store.ApplyMediaOperation(ctx, actor, id, request)
}
func (r *mediaOperationsRuntime) Recover(ctx context.Context, actor identity.Principal, id string, request library.MediaOperationApplyRequest) (library.MediaOperationAdmission, error) {
	if !r.enter() {
		return library.MediaOperationAdmission{}, library.ErrUnavailable
	}
	defer r.operations.Done()
	defer r.Wake()
	if !r.processingEnabled {
		return r.inactiveApplyReceipt(ctx, actor, id, request, true)
	}
	return r.store.RecoverMediaOperation(ctx, actor, id, request)
}
func (r *mediaOperationsRuntime) Cancel(ctx context.Context, actor identity.Principal, id string, revision int64) (library.MediaOperation, error) {
	if !r.enter() {
		return library.MediaOperation{}, library.ErrUnavailable
	}
	defer r.operations.Done()
	defer r.Wake()
	return r.store.CancelMediaOperation(ctx, actor, id, revision)
}

func (r *mediaOperationsRuntime) inactiveApplyReceipt(ctx context.Context, actor identity.Principal, id string, request library.MediaOperationApplyRequest, recovery bool) (library.MediaOperationAdmission, error) {
	op, err := r.store.GetMediaOperation(ctx, actor, id)
	if err != nil {
		return library.MediaOperationAdmission{}, err
	}
	if op.ApplyRequestID != request.RequestID {
		return library.MediaOperationAdmission{}, library.ErrUnavailable
	}
	encoded, _ := json.Marshal(struct {
		Request  library.MediaOperationApplyRequest
		Recovery bool
	}{request, recovery})
	digest := sha256.Sum256(encoded)
	if string(op.ApplyFingerprint) != string(digest[:]) {
		return library.MediaOperationAdmission{}, library.ErrMediaOperationConflict
	}
	return library.MediaOperationAdmission{Operation: op}, nil
}

func (r *mediaOperationsRuntime) loop() {
	defer close(r.done)
	// Disabled or unavailable execution stays dormant until an explicit request
	// or an owned worker completion wakes it. It never polls the catalog at idle.
	var ticks <-chan time.Time
	if r.processingEnabled {
		timer := time.NewTicker(500 * time.Millisecond)
		defer timer.Stop()
		ticks = timer.C
	}
	var retry *time.Timer
	var retryTick <-chan time.Time
	retryDelay := 250 * time.Millisecond
	defer func() {
		if retry != nil {
			retry.Stop()
		}
	}()
	for {
		select {
		case <-r.ctx.Done():
			r.drain()
			return
		case <-r.wake:
		case <-ticks:
		case <-retryTick:
		}
		if retry != nil {
			retry.Stop()
			retry = nil
			retryTick = nil
		}
		ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
		if err := r.store.CheckOwnership(ctx); err != nil {
			cancel()
			r.BeginClose()
			continue
		}
		retryNeeded := r.reconcile(ctx)
		cancel()
		if retryNeeded && !r.processingEnabled {
			// Only a failed attempt to finish an admitted request enables this
			// bounded backoff. A quiescent disabled runtime has no retry timer.
			retry = time.NewTimer(retryDelay)
			retryTick = retry.C
			retryDelay = min(30*time.Second, 2*retryDelay)
		} else {
			retryDelay = 250 * time.Millisecond
		}
	}
}

func (r *mediaOperationsRuntime) reconcile(ctx context.Context) bool {
	retryNeeded := false
	for id, execution := range r.workers {
		select {
		case <-execution.done:
			if err := r.store.FinishMediaOperationWork(ctx, execution.work, execution.err, false); err == nil {
				execution.cancel()
				delete(r.workers, id)
			} else {
				retryNeeded = true
			}
		default:
			if execution.work.Discard {
				continue
			}
			current, err := r.store.MediaOperationWorkStatus(ctx, execution.work)
			if err != nil || current.CancelRequestedAt != nil {
				execution.cancel()
			}
		}
	}
	concurrency := max(1, min(4, r.configuration.MaxConcurrent))
	if len(r.workers) >= concurrency {
		return retryNeeded
	}
	limit := r.configuration.MaxQueued
	if limit < 1 {
		limit = 16
	}
	limit = min(limit, 128)
	var pending []library.MediaOperation
	var err error
	if r.processingEnabled {
		pending, err = r.store.PendingMediaOperations(ctx, limit)
	} else {
		pending, err = r.store.PendingMediaOperationCancellations(ctx, limit)
	}
	if err != nil {
		return true
	}
	for _, op := range pending {
		if len(r.workers) >= concurrency {
			return retryNeeded
		}
		if _, exists := r.workers[op.ID]; exists {
			continue
		}
		work, err := r.store.ClaimMediaOperation(ctx, op.ID)
		if err != nil {
			if !errors.Is(err, library.ErrMediaOperationConflict) && !errors.Is(err, library.ErrMediaOperationState) && !errors.Is(err, library.ErrMediaOperationRecovery) && !errors.Is(err, library.ErrSourceChanged) && !errors.Is(err, library.ErrForbidden) {
				retryNeeded = true
			}
			continue
		}
		deadline := 5 * time.Minute
		if !work.Discard {
			configuration, decodeErr := decodeMediaOperationExecution(work.Operation.ExecutionSnapshot)
			if !r.processingEnabled {
				decodeErr = library.ErrUnavailable
			}
			if decodeErr != nil {
				// Retain failed claims until their database finalization succeeds;
				// a failed SQL write must not detach an already-owned token.
				execution := &mediaOperationExecution{work: work, cancel: func() {}, done: make(chan struct{}), err: decodeErr}
				close(execution.done)
				r.workers[op.ID] = execution
				r.Wake()
				continue
			}
			deadline = time.Duration(configuration.Configuration.MaxRuntimeSeconds) * time.Second
		}
		workCtx, cancel := context.WithTimeout(r.ctx, deadline)
		execution := &mediaOperationExecution{work: work, cancel: cancel, done: make(chan struct{})}
		r.workers[op.ID] = execution
		go func() {
			defer func() { close(execution.done); r.Wake() }()
			defer func() {
				if recover() != nil {
					execution.err = library.ErrUnavailable
				}
			}()
			execution.err = r.execute(workCtx, execution.work)
		}()
	}
	return retryNeeded
}

func (r *mediaOperationsRuntime) execute(ctx context.Context, work library.MediaOperationWork) error {
	executor := r.executors[work.Operation.Kind]
	if executor == nil {
		return library.ErrUnavailable
	}
	if work.Discard {
		if err := executor.Discard(ctx, work); err != nil {
			return errors.Join(library.ErrMediaOperationRecovery, err)
		}
		return nil
	}
	snapshot, err := decodeMediaOperationExecution(work.Operation.ExecutionSnapshot)
	if err != nil {
		return err
	}
	if !work.Apply {
		if err = r.verifyInventory(ctx, snapshot, work.Operation.Kind); err != nil {
			return err
		}
	}
	progress := func(value library.MediaOperationProgress) error {
		return r.store.UpdateMediaOperationProgress(ctx, work, value)
	}
	if work.Apply {
		applyErr := executor.Apply(ctx, work, progress)
		if applyErr != nil && work.Operation.Kind == library.MediaOperationRemoveSubtitle {
			cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
			defer stop()
			current, _ := r.store.MediaOperationWorkStatus(cleanup, work)
			if current.WorkerToken == work.Token && current.CancelRequestedAt != nil && current.PublicationPhase == "none" {
				work.Operation = current
				work.Discard = true
				if discardErr := executor.Discard(cleanup, work); discardErr != nil {
					return errors.Join(library.ErrMediaOperationRecovery, applyErr, discardErr)
				}
				return errors.Join(context.Canceled, applyErr)
			}
		}
		return applyErr
	}
	result, err := executor.Execute(ctx, work, progress)
	if err == nil {
		err = r.store.ReadyMediaOperation(ctx, work, result)
	}
	if err != nil && work.Operation.Kind == library.MediaOperationRemoveSubtitle && len(result.Journal) > 2 {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if e := r.store.AbandonEmbeddedSubtitleCandidate(cleanup, work, result); e != nil {
			retained := r.store.RetainMediaOperationJournal(cleanup, work, result.Journal, result.ResultHash)
			return errors.Join(library.ErrMediaOperationRecovery, err, e, retained)
		}
	}
	return err
}

func (r *mediaOperationsRuntime) drain() {
	r.operations.Wait()
	for _, execution := range r.workers {
		execution.cancel()
	}
	for id, execution := range r.workers {
		<-execution.done
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = r.store.FinishMediaOperationWork(ctx, execution.work, execution.err, true)
		cancel()
		delete(r.workers, id)
	}
}

type mediaOperationBuiltinExecutor struct {
	runtime *mediaOperationsRuntime
	kind    string
}

func (executor mediaOperationBuiltinExecutor) Execute(ctx context.Context, work library.MediaOperationWork, progress func(library.MediaOperationProgress) error) (library.MediaOperationResult, error) {
	snapshot, err := decodeMediaOperationExecution(work.Operation.ExecutionSnapshot)
	if err != nil {
		return library.MediaOperationResult{}, err
	}
	if executor.kind == library.MediaOperationRemoveSubtitle {
		workCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		var progressErr error
		result, err := executor.runtime.store.StageEmbeddedSubtitleRemoval(workCtx, work, snapshot.FFmpegPath, snapshot.FFprobePath, func(value library.MediaOperationProgress) {
			if progressErr == nil {
				progressErr = progress(value)
				if progressErr != nil {
					cancel()
				}
			}
		})
		return result, errors.Join(err, progressErr)
	}
	if err = progress(library.MediaOperationProgress{Stage: "recognizing"}); err != nil {
		return library.MediaOperationResult{}, err
	}
	ocr := media.SubtitleOCRConfig{FFprobePath: snapshot.FFprobePath, FFprobeSHA256: snapshot.FFprobeSHA256, TesseractPath: snapshot.Configuration.OCR.Executable, TesseractSHA256: snapshot.Configuration.OCR.ToolSHA256,
		ScratchDirectory: snapshot.Configuration.ScratchDirectory, MaxScratchBytes: snapshot.Configuration.MaxScratchBytes}
	selected := []string{}
	for _, model := range snapshot.Configuration.OCR.Models {
		ocr.Models = append(ocr.Models, media.SubtitleOCRModel{ID: model.Language, Path: filepath.Join(snapshot.Configuration.OCR.TessdataDirectory, model.Filename), SHA256: model.SHA256})
		if slices.Contains(work.Operation.Parameters.ModelIDs, model.ID) {
			selected = append(selected, model.Language)
		}
	}
	if len(selected) != len(work.Operation.Parameters.ModelIDs) {
		return library.MediaOperationResult{}, library.ErrInvalidInput
	}
	work.Operation.Parameters.ModelIDs = selected
	return executor.runtime.store.PrepareMediaOCR(ctx, work, ocr)
}
func (executor mediaOperationBuiltinExecutor) Apply(ctx context.Context, work library.MediaOperationWork, _ func(library.MediaOperationProgress) error) error {
	if executor.kind == library.MediaOperationOCR {
		return executor.runtime.store.ApplyMediaOCR(ctx, work)
	}
	_, err := executor.runtime.store.ApplyEmbeddedSubtitleRemoval(ctx, work, executor.runtime.retireSource)
	return err
}
func (executor mediaOperationBuiltinExecutor) Discard(ctx context.Context, work library.MediaOperationWork) error {
	if executor.kind == library.MediaOperationOCR {
		return nil
	}
	return executor.runtime.store.DiscardEmbeddedSubtitleCandidate(ctx, work)
}

func (r *mediaOperationsRuntime) retireSource(ctx context.Context, itemID, sourceID string) error {
	return r.server.retireReplacedMediaSource(ctx, itemID, sourceID)
}

func decodeMediaOperationExecution(raw []byte) (mediaOperationExecutionSnapshot, error) {
	var snapshot mediaOperationExecutionSnapshot
	if len(raw) == 0 || len(raw) > library.MaxMediaOperationDocumentBytes || json.Unmarshal(raw, &snapshot) != nil || snapshot.Configuration.Validate() != nil || !snapshot.Configuration.Enabled {
		return snapshot, library.ErrUnavailable
	}
	return snapshot, nil
}

func (r *mediaOperationsRuntime) verifyInventory(ctx context.Context, snapshot mediaOperationExecutionSnapshot, kind string) error {
	_, digest, err := mediaOperationToolIdentity(ctx, snapshot.FFprobePath)
	if err != nil || digest != snapshot.FFprobeSHA256 {
		return library.ErrUnavailable
	}
	if kind == library.MediaOperationRemoveSubtitle {
		_, digest, err = mediaOperationToolIdentity(ctx, snapshot.FFmpegPath)
		if err != nil || digest != snapshot.FFmpegSHA256 {
			return library.ErrUnavailable
		}
		return nil
	}
	return mediaOperationOCRInventory(ctx, snapshot.Configuration.OCR)
}

func mediaOperationOCRInventory(ctx context.Context, ocr config.MediaOperationsOCRConfig) error {
	if ocr.Engine != "tesseract" || len(ocr.Models) == 0 {
		return library.ErrUnavailable
	}
	_, digest, err := mediaOperationToolIdentity(ctx, ocr.Executable)
	if err != nil || digest != ocr.ToolSHA256 {
		return library.ErrUnavailable
	}
	for _, model := range ocr.Models {
		digest, err := mediaOperationFileHash(ctx, filepath.Join(ocr.TessdataDirectory, model.Filename), 256<<20, false)
		if err != nil || digest != model.SHA256 {
			return library.ErrUnavailable
		}
	}
	return nil
}

func mediaOperationToolIdentity(ctx context.Context, name string) (string, string, error) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", "", library.ErrUnavailable
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", "", library.ErrUnavailable
	}
	digest, err := mediaOperationFileHash(ctx, resolved, 256<<20, true)
	return resolved, digest, err
}

// Hashing an admitted local file does not execute or probe it. The process
// adapter independently pins its descriptors and validates actual output.
func mediaOperationFileHash(ctx context.Context, name string, maximum int64, executable bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(name)
	if err != nil {
		return "", library.ErrUnavailable
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maximum || executable && before.Mode().Perm()&0111 == 0 {
		return "", library.ErrUnavailable
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buffer)
		total += int64(n)
		if total > maximum {
			return "", library.ErrUnavailable
		}
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", library.ErrUnavailable
		}
	}
	after, err := file.Stat()
	named, namedErr := os.Stat(name)
	if err != nil || namedErr != nil || !os.SameFile(before, after) || !os.SameFile(before, named) || before.Size() != after.Size() || before.Size() != total || !before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) != media.FileChangeTime(after) {
		return "", library.ErrUnavailable
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
