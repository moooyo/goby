package library

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func validatePreparedPlaybackRoot(source indexedMediaSource, hint mediaSourceRootHint) error {
	if source.root != hint.root || source.rootBindingRevision != hint.bindingRevision {
		return fmt.Errorf("%w: %w: source binding changed before admission", ErrUnavailable, ErrSourceChanged)
	}
	return nil
}

// Acquire the first warm IO lease using only governor memory after AUTH1.
// afterInitialMiss is a private memory-only test boundary; production passes
// nil. It adds no callback, queue or delay to an initial successful try.
func acquirePlaybackMediaInitialIO(ctx context.Context, admission *mediaSourceAdmissionQueue, root mediaSourceRootKey, domain string,
	snapshot *indexedMediaSource, current *PlaybackMediaAuthorization, afterInitialMiss func()) (func(), bool, error) {
	if admission == nil || snapshot == nil || current == nil {
		return nil, false, ErrInvalidInput
	}
	release, granted, err := admission.tryAcquireRoot(ctx, false, root, domain)
	if err != nil {
		return nil, false, err
	}
	if granted {
		mediaSourceAdmissionMeasurement.immediate.Add(1)
		return release, false, nil
	}
	mediaSourceAdmissionMeasurement.misses.Add(1)
	if afterInitialMiss != nil {
		afterInitialMiss()
	}
	queuedAt := time.Now()
	release, wasQueued, err := admission.acquireRootWithQueueFact(ctx, false, root, domain, func() {
		// Never carry AUTH1 facts through a capacity wait, including a waiter
		// whose ready channel closed before wait observed it.
		*snapshot, *current = indexedMediaSource{}, PlaybackMediaAuthorization{}
	})
	mediaSourceAdmissionMeasurement.ioQueueNS.Add(uint64(time.Since(queuedAt)))
	if err != nil {
		// Preserve the existing fallback failure guard even when cancellation
		// prevented enqueue. Initial try errors above keep their direct result.
		return nil, true, err
	}
	mediaSourceAdmissionMeasurement.queued.Add(1)
	if wasQueued {
		mediaSourceAdmissionMeasurement.firstFallbackQueuedGrants.Add(1)
	} else {
		mediaSourceAdmissionMeasurement.firstFallbackImmediateGrants.Add(1)
	}
	return release, wasQueued, nil
}

// Handoff ownership bounds AUTH, root pins, source descriptors, publication and
// ACK. Warm roots release actual IO before database readback and descriptor ACK.
func (s *Store) openPlaybackMediaAuthorization(ctx context.Context, principal identity.Principal, owner PlaybackOwner, playID, itemID, sourceID string, includeSubtitles bool, check func(PlaybackMediaAuthorization) error) (*os.File, PlaybackMediaAuthorization, error) {
	worker, finish, err := s.beginMediaSourceLifetime(ctx)
	if err != nil {
		return nil, PlaybackMediaAuthorization{}, err
	}
	var ioRelease, handoffRelease func()
	var root mediaSourceRootKey
	var domain string
	var warm *warmMediaSourceRoot
	var authorized PlaybackMediaAuthorization
	var handedAt time.Time
	cleanup := func(file *os.File) {
		if file == nil && warm == nil && ioRelease == nil {
			return
		}
		if ioRelease == nil {
			waited := time.Now()
			release, err := mediaSourceAdmission.acquireCleanupRoot(root, domain)
			mediaSourceAdmissionMeasurement.cleanupWaitNS.Add(uint64(time.Since(waited)))
			if err != nil {
				panic("source cleanup admission invariant failed")
			}
			ioRelease = measureMediaSourceIOOwner(release)
		}
		closedAt := time.Now()
		if file != nil {
			_ = file.Close()
		}
		if warm != nil {
			warm.release()
			warm = nil
		}
		ioRelease()
		ioRelease = nil
		mediaSourceAdmissionMeasurement.cleanupHeldNS.Add(uint64(time.Since(closedAt)))
	}
	file, source, err := runOwnedMediaSourceHandoff(worker, func() {
		cleanup(nil)
		if !handedAt.IsZero() {
			mediaSourceAdmissionMeasurement.ackNS.Add(uint64(time.Since(handedAt)))
		}
		if handoffRelease != nil {
			handoffRelease()
		}
		finish()
	}, func() (*os.File, MediaFile, error) {
		var opened *os.File
		returned := false
		defer func() {
			if !returned && opened != nil {
				cleanup(opened)
			}
		}()
		failureGuard := func(ctx context.Context) error {
			snapshot, current, err := s.readPlaybackMediaAuthorization(ctx, principal, owner, playID, itemID, sourceID, includeSubtitles)
			if err != nil {
				return err
			}
			if check != nil {
				current.Source = snapshot.mediaFile
				return check(current)
			}
			return nil
		}
		failedAdmission := func(cause error) (*os.File, MediaFile, error) {
			return nil, MediaFile{}, mediaSourceAdmissionFailure(worker, false, cause, []func(context.Context) error{failureGuard})
		}
		hint, err := s.prepareMediaSourceRoot(worker, false, func(ctx context.Context) (mediaSourceRootHint, error) { return s.readMediaSourceRootHint(ctx, itemID) })
		if err != nil {
			return failedAdmission(err)
		}
		root, domain, err = s.mediaSourceRootLane(hint)
		if err != nil {
			return failedAdmission(err)
		}
		worker = mediaSourceAuthorizationContext(worker, root, domain, false)
		waited := time.Now()
		releaseHandoff, err := mediaSourceHandoffAdmission.acquireRoot(worker, false, root, domain)
		mediaSourceAdmissionMeasurement.handoffWaitNS.Add(uint64(time.Since(waited)))
		if err != nil {
			return failedAdmission(err)
		}
		heldAt := time.Now()
		handoffRelease = func() {
			releaseHandoff()
			mediaSourceAdmissionMeasurement.handoffHeldNS.Add(uint64(time.Since(heldAt)))
		}
		warm, err = s.borrowWarmMediaSourceRoot(hint.root)
		if err != nil {
			return failedAdmission(err)
		}
		var snapshot indexedMediaSource
		var current PlaybackMediaAuthorization
		if warm == nil {
			mediaSourceAdmissionMeasurement.cold.Add(1)
			queuedAt := time.Now()
			release, err := mediaSourceAdmission.acquireRoot(worker, false, root, domain)
			mediaSourceAdmissionMeasurement.ioQueueNS.Add(uint64(time.Since(queuedAt)))
			if err != nil {
				return failedAdmission(err)
			}
			ioRelease = measureMediaSourceIOOwner(release)
			snapshot, current, err = s.readPlaybackMediaAuthorization(worker, principal, owner, playID, itemID, sourceID, includeSubtitles)
		} else {
			mediaSourceAdmissionMeasurement.warm.Add(1)
			snapshot, current, err = s.readPlaybackMediaAuthorizationPrepared(worker, principal, owner, playID, itemID, sourceID, includeSubtitles, &hint, nil)
			if err == nil {
				// AUTH1 has committed and released its lease. The helper may retain
				// its local facts only across memory operations with no enqueue.
				release, refresh, acquireErr := acquirePlaybackMediaInitialIO(worker, mediaSourceAdmission, root, domain, &snapshot, &current, nil)
				if acquireErr != nil {
					if refresh {
						return failedAdmission(acquireErr)
					}
					return nil, MediaFile{}, acquireErr
				}
				if !refresh {
					ioRelease = measureMediaSourceIOOwner(release)
				} else {
					// Yield the fair grant without DB or filesystem work. The
					// handoff owner and warm pin remain owned while complete fresh
					// AUTH runs outside IO. No priority transfers to the next try.
					measureMediaSourceIOOwner(release)()
					mediaSourceAdmissionMeasurement.releasedQueueGrants.Add(1)
					mediaSourceAdmissionMeasurement.reauthorizations.Add(1)
					snapshot, current, err = s.readPlaybackMediaAuthorizationPrepared(worker, principal, owner, playID, itemID, sourceID, includeSubtitles, &hint, nil)
					if err == nil {
						release, granted, tryErr := mediaSourceAdmission.tryAcquireRoot(worker, false, root, domain)
						if tryErr != nil {
							return nil, MediaFile{}, tryErr
						}
						if granted {
							ioRelease = measureMediaSourceIOOwner(release)
							mediaSourceAdmissionMeasurement.retryGrants.Add(1)
						} else {
							mediaSourceAdmissionMeasurement.misses.Add(1)
						}
					}
				}
			}
			if err == nil && ioRelease == nil {
				// A second miss ends yielding. The original fair queued grant and
				// full fresh AUTH while holding IO guarantee the same progress as
				// the conservative path. There is no unbounded authorization loop.
				snapshot, current = indexedMediaSource{}, PlaybackMediaAuthorization{}
				queuedAt := time.Now()
				release, waitErr := mediaSourceAdmission.acquireRoot(worker, false, root, domain)
				mediaSourceAdmissionMeasurement.ioQueueNS.Add(uint64(time.Since(queuedAt)))
				if waitErr != nil {
					return failedAdmission(waitErr)
				}
				ioRelease = measureMediaSourceIOOwner(release)
				mediaSourceAdmissionMeasurement.queued.Add(1)
				mediaSourceAdmissionMeasurement.heldRefreshGrants.Add(1)
				mediaSourceAdmissionMeasurement.reauthorizations.Add(1)
				snapshot, current, err = s.readPlaybackMediaAuthorizationPrepared(worker, principal, owner, playID, itemID, sourceID, includeSubtitles, &hint, nil)
			}
		}
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := validatePreparedPlaybackRoot(snapshot, hint); err != nil {
			return nil, MediaFile{}, err
		}
		current.Source = snapshot.mediaFile
		if check != nil {
			if err := check(current); err != nil {
				return nil, MediaFile{}, err
			}
		}
		if warm == nil {
			worker = context.WithValue(worker, mediaSourceRootContextKey{}, hint)
			opened, err = s.openPublicMediaSource(worker, snapshot)
		} else {
			opened, err = warm.openMediaSource(worker, snapshot)
			if err == nil {
				// All original named/root/parent/descriptor checks have completed.
				// A retired last anchor pin can Close, so release it inside IO.
				warm.release()
				warm = nil
				ioRelease()
				ioRelease = nil
				publishedAt := time.Now()
				err = s.checkOpenedMediaPublication(worker, snapshot)
				mediaSourceAdmissionMeasurement.publicationNS.Add(uint64(time.Since(publishedAt)))
			}
		}
		if err != nil {
			return nil, MediaFile{}, err
		}
		authorized = current
		handedAt = time.Now()
		returned = true
		return opened, snapshot.mediaFile, nil
	}, cleanup)
	if err != nil {
		return nil, PlaybackMediaAuthorization{}, err
	}
	authorized.Source = source
	return file, authorized, nil
}
