package server

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type publishedAnalysisPreview struct {
	runtime     *mediaAnalysisRuntime
	principal   identity.Principal
	ctx         context.Context
	cancel      context.CancelFunc
	metadata    analysisPreviewMetadata
	reference   library.AnalysisPreview
	cache       *analysiscache.Lease
	manifest    *analysiscache.Lease
	opened      os.FileInfo
	leave       func()
	stopSource  func() bool
	leaveSource func()
	watched     chan struct{}
	expiryMu    sync.Mutex
	expiresAt   time.Time
	expiryTimer *time.Timer
	closed      bool
	closeOnce   sync.Once
	closeErr    error
}

func (r *mediaAnalysisRuntime) OpenPreview(ctx context.Context, principal identity.Principal, itemID, sourceID string, width int) (analysisPreviewLease, error) {
	if width != 0 && !analysisPreviewWidth(width) {
		return nil, library.ErrInvalidInput
	}
	ctx, leave, err := r.enter(ctx)
	if err != nil {
		return nil, err
	}
	select {
	case r.previewSlots <- struct{}{}:
	default:
		leave()
		return nil, library.ErrBusy
	}
	var stopKnownExpiry context.CancelFunc
	if !principal.IsApplicationKey() && !principal.ExpiresAt.IsZero() {
		ctx, stopKnownExpiry = context.WithDeadline(ctx, principal.ExpiresAt)
	} else {
		ctx, stopKnownExpiry = context.WithCancel(ctx)
	}
	cleanup := func() { stopKnownExpiry(); <-r.previewSlots; leave() }
	fresh, err := r.server.identity.RevalidateSession(ctx, principal)
	if err != nil {
		cleanup()
		return nil, err
	}
	current, err := r.server.library.GetCurrentAnalysisSourceFor(ctx, librarySubject(fresh, fresh.User.ID), itemID, sourceID)
	if err != nil {
		cleanup()
		return nil, err
	}
	// Register the actual source before final authorization, so a concurrent
	// media publication either cancels this reader or fails its last check.
	lifetime, leaveSource, err := r.server.originals.enterSource(fresh, current.ItemID, current.MediaSourceID)
	if err != nil {
		cleanup()
		return nil, err
	}
	var work context.Context
	var cancel context.CancelFunc
	if !fresh.IsApplicationKey() && !fresh.ExpiresAt.IsZero() {
		work, cancel = context.WithDeadline(ctx, fresh.ExpiresAt)
	} else {
		work, cancel = context.WithCancel(ctx)
	}
	lease := &publishedAnalysisPreview{runtime: r, principal: fresh, ctx: work, cancel: cancel, leave: cleanup,
		leaveSource: leaveSource, watched: make(chan struct{}), metadata: analysisPreviewMetadata{
			ItemID: current.ItemID, MediaSourceID: current.MediaSourceID, SourceRevision: current.SourceRevision}}
	if !fresh.IsApplicationKey() {
		lease.expiresAt = fresh.ExpiresAt
	}
	lease.stopSource = context.AfterFunc(lifetime, cancel)
	if lifetime.Err() != nil {
		cancel()
	}
	fail := func(err error) (analysisPreviewLease, error) {
		r.rememberFailure("", err)
		close(lease.watched)
		return nil, errors.Join(err, lease.Close())
	}
	if r.cache != nil {
		references, err := r.server.library.GetAnalysisPreviewsFor(work, librarySubject(fresh, fresh.User.ID), itemID, current.MediaSourceID)
		if err != nil {
			return fail(err)
		}
		// Stored widths are ascending. Width zero chooses the largest physically
		// available generation, rather than trusting an evicted database row.
		for index := len(references) - 1; index >= 0; index-- {
			reference := references[index]
			if width != 0 && reference.Width != width {
				continue
			}
			if reference.SourceRevision != current.SourceRevision {
				return fail(library.ErrAnalysisSourceChanged)
			}
			cached, err := r.cache.Acquire(work, reference.CacheKey, reference.Seal, strconv.Itoa(reference.Width)+".bif")
			if errors.Is(err, analysiscache.ErrNotFound) {
				continue
			}
			if err != nil {
				return fail(err)
			}
			lease.cache, lease.reference = cached, reference
			if cached.Artifact.SHA256 != reference.SHA256 || cached.Artifact.Size != reference.Bytes || cached.Seal != reference.Seal {
				return fail(library.ErrUnavailable)
			}
			manifest, err := r.acquirePreviewManifest(work, []library.AnalysisPreview{reference}, nil)
			if errors.Is(err, analysiscache.ErrNotFound) {
				if closeErr := cached.Close(); closeErr != nil {
					return fail(closeErr)
				}
				lease.cache = nil
				lease.reference = library.AnalysisPreview{}
				continue
			}
			if err != nil {
				return fail(err)
			}
			lease.manifest = manifest
			lease.opened, err = cached.File.Stat()
			if err != nil {
				return fail(err)
			}
			lease.metadata.Ready = true
			lease.metadata.BIFSHA256, lease.metadata.Size = reference.SHA256, reference.Bytes
			lease.metadata.Width, lease.metadata.Height = reference.Width, reference.Height
			break
		}
	}
	if err := lease.Revalidate(work); err != nil {
		return fail(err)
	}
	go lease.watch()
	return lease, nil
}

func (lease *publishedAnalysisPreview) Context() context.Context          { return lease.ctx }
func (lease *publishedAnalysisPreview) Metadata() analysisPreviewMetadata { return lease.metadata }
func (lease *publishedAnalysisPreview) Reader() analysisPreviewReader {
	if lease.cache == nil {
		return nil
	}
	return lease.cache.File
}

func (lease *publishedAnalysisPreview) Revalidate(ctx context.Context) error {
	if err := lease.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := lease.runtime.server
	fresh, err := lease.currentCredential(ctx)
	if err != nil {
		return err
	}
	subject := librarySubject(fresh, fresh.User.ID)
	current, err := s.library.GetCurrentAnalysisSourceFor(ctx, subject, lease.metadata.ItemID, lease.metadata.MediaSourceID)
	if err != nil {
		return err
	}
	if current.SourceRevision != lease.metadata.SourceRevision || current.MediaSourceID != lease.metadata.MediaSourceID {
		return library.ErrAnalysisSourceChanged
	}
	if !lease.metadata.Ready {
		return lease.finishAuthorization(ctx)
	}
	refs, err := s.library.GetAnalysisPreviewsFor(ctx, subject, current.ItemID, current.MediaSourceID)
	if err != nil {
		return err
	}
	found := false
	for _, reference := range refs {
		if reference.Width == lease.reference.Width && reference.Revision == lease.reference.Revision &&
			reference.CacheKey == lease.reference.CacheKey && reference.Seal == lease.reference.Seal &&
			reference.SHA256 == lease.reference.SHA256 && reference.SourceRevision == lease.reference.SourceRevision &&
			reference.ProfileFingerprint == lease.reference.ProfileFingerprint && reference.PublicationEpoch == lease.reference.PublicationEpoch {
			found = true
			break
		}
	}
	if !found {
		return library.ErrAnalysisSourceChanged
	}
	currentFile, err := lease.cache.File.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(lease.opened, currentFile) || lease.opened.Size() != currentFile.Size() ||
		!lease.opened.ModTime().Equal(currentFile.ModTime()) || media.FileChangeTime(lease.opened) != media.FileChangeTime(currentFile) {
		return library.ErrAnalysisSourceChanged
	}
	return lease.finishAuthorization(ctx)
}

func (lease *publishedAnalysisPreview) currentCredential(ctx context.Context) (identity.Principal, error) {
	fresh, err := lease.runtime.server.identity.RevalidateSession(ctx, lease.principal)
	if err != nil {
		return identity.Principal{}, err
	}
	if fresh.IsApplicationKey() != lease.principal.IsApplicationKey() || fresh.SessionID != lease.principal.SessionID ||
		fresh.User.ID != lease.principal.User.ID || fresh.ClientSessionID != lease.principal.ClientSessionID ||
		fresh.Client.DeviceID != lease.principal.Client.DeviceID {
		return identity.Principal{}, identity.ErrUnauthorized
	}
	lease.expiryMu.Lock()
	defer lease.expiryMu.Unlock()
	if lease.closed {
		return identity.Principal{}, context.Canceled
	}
	if !fresh.IsApplicationKey() && !fresh.ExpiresAt.IsZero() &&
		(lease.expiresAt.IsZero() || fresh.ExpiresAt.Before(lease.expiresAt)) {
		lease.expiresAt = fresh.ExpiresAt
		if lease.expiryTimer != nil {
			lease.expiryTimer.Stop()
		}
		lease.expiryTimer = time.AfterFunc(time.Until(fresh.ExpiresAt), lease.cancel)
	}
	return fresh, nil
}

func (lease *publishedAnalysisPreview) finishAuthorization(ctx context.Context) error {
	if err := lease.ctx.Err(); err != nil {
		return err
	}
	// Physical source and cache checks may block. A credential observed before
	// those operations cannot authorize headers after revocation during the wait.
	_, err := lease.currentCredential(ctx)
	return errors.Join(err, ctx.Err(), lease.ctx.Err())
}

func (lease *publishedAnalysisPreview) watch() {
	defer close(lease.watched)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-lease.ctx.Done():
			return
		case <-ticker.C:
			check, cancel := context.WithTimeout(lease.ctx, time.Second)
			err := lease.Revalidate(check)
			cancel()
			// A derivative response is cheap to retry. An unprovable ongoing
			// grant ends this read without mutating catalog or derivative state.
			if err != nil {
				lease.cancel()
				return
			}
		}
	}
}

func (lease *publishedAnalysisPreview) Close() error {
	lease.closeOnce.Do(func() {
		lease.expiryMu.Lock()
		lease.closed = true
		if lease.expiryTimer != nil {
			lease.expiryTimer.Stop()
		}
		lease.expiryMu.Unlock()
		lease.cancel()
		lease.stopSource()
		if lease.cache != nil {
			lease.closeErr = lease.cache.Close()
		}
		if lease.manifest != nil {
			lease.closeErr = errors.Join(lease.closeErr, lease.manifest.Close())
		}
		<-lease.watched
		lease.leaveSource()
		lease.leave()
	})
	return lease.closeErr
}
