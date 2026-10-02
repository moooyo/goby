package library

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// The data pool has eleven application connections after catalog ownership.
// Eight complete source-authority reads permit overlap with binding/barrier
// queries. Seven per root/domain preserve one auth slot for another domain.
// This is an admission budget, not physical reservation against other catalog
// operations sharing that pool. Unclassified reads are limited to one and
// conservatively count against every root/domain, including future arrivals.
var mediaSourceAuthorizationAdmission = newMediaSourceAdmissionWithLimits(mediaSourceAdmissionLimits{8, 3, 7, 2, 1})

// A HLS handoff owner spans authority preparation, opened source descriptors,
// publication readback and ACK. Unlike actual IO, it may remain owned during
// database waits. Temporary root/parent descriptors and non-HLS source opens
// have separate bounds; this is not a process-wide total descriptor limit.
var mediaSourceHandoffAdmission = newMediaSourceAdmissionWithLimits(mediaSourceAdmissionLimits{8, 3, 7, 2, 1})

type mediaSourceAuthorizationRouteKey struct{}
type mediaSourceAuthorizationOwnedKey struct{}

type mediaSourceAuthorizationRoute struct {
	root       mediaSourceRootKey
	domain     string
	background bool
}

type mediaSourceAdmissionMeasurements struct {
	authorizations, authorizationWaitNS, authorizationNS    atomic.Uint64
	immediate, misses, reauthorizations                     atomic.Uint64
	queued                                                  atomic.Uint64
	retryGrants, releasedQueueGrants, heldRefreshGrants     atomic.Uint64
	firstFallbackQueuedGrants, firstFallbackImmediateGrants atomic.Uint64
	ioQueueNS, ioHeldNS, fileOpenNS                         atomic.Uint64
	handoffWaitNS, handoffHeldNS, publicationNS             atomic.Uint64
	cleanupWaitNS, cleanupHeldNS, ackNS, warm, cold         atomic.Uint64
}

var mediaSourceAdmissionMeasurement mediaSourceAdmissionMeasurements

func mediaSourceAuthorizationContext(ctx context.Context, root mediaSourceRootKey, domain string, background bool) context.Context {
	return context.WithValue(ctx, mediaSourceAuthorizationRouteKey{}, mediaSourceAuthorizationRoute{root: root, domain: domain, background: background})
}

func beginMediaSourceAuthorization(ctx context.Context) (context.Context, func(), error) {
	if owned, _ := ctx.Value(mediaSourceAuthorizationOwnedKey{}).(bool); owned {
		return ctx, func() {}, nil
	}
	route, _ := ctx.Value(mediaSourceAuthorizationRouteKey{}).(mediaSourceAuthorizationRoute)
	waitStarted := time.Now()
	waiter, err := mediaSourceAuthorizationAdmission.requestRoot(ctx, route.background, true, route.root, route.domain, mediaSourceScopedQueueLimit)
	if err != nil {
		return nil, nil, err
	}
	release, err := mediaSourceAuthorizationAdmission.wait(waiter)
	mediaSourceAdmissionMeasurement.authorizationWaitNS.Add(uint64(time.Since(waitStarted)))
	if err != nil {
		return nil, nil, err
	}
	started := time.Now()
	mediaSourceAdmissionMeasurement.authorizations.Add(1)
	return context.WithValue(ctx, mediaSourceAuthorizationOwnedKey{}, true), func() {
		mediaSourceAdmissionMeasurement.authorizationNS.Add(uint64(time.Since(started)))
		release()
	}, nil
}

func measureMediaSourceIOOwner(release func()) func() {
	started := time.Now()
	var once sync.Once
	return func() {
		once.Do(func() {
			release()
			mediaSourceAdmissionMeasurement.ioHeldNS.Add(uint64(time.Since(started)))
		})
	}
}

// MediaSourceAdmissionProfile exposes cumulative aggregate measurements without
// item, principal, root or path labels. Profile harnesses take before/after
// deltas; normal requests allocate no measurement maps. Authorization hold time
// includes pool/row waits and commit after the DB budget grant. IO version 5
// records warm policy/file work, cleanup and the first fair grant's empty span;
// first queued fresh AUTH is outside IO, and only a second miss enters held-IO
// fresh AUTH/commit. Immediate grants count initial tries, retry grants count
// second tries. Queued grants retain their fallback-acquisition meaning, even
// for immediate fallback acquisitions; first-fallback queue facts distinguish
// actual enqueue from a direct grant. Released grants still mean yielding a
// first fallback before complete fresh AUTH, and held-refresh grants still
// mean the conservative second-miss path. No old counter becomes a wait timer.
// Warm publication/ACK use only handoff ownership. Cold paths
// conservatively retain IO through AUTH, binding, publication and ACK. File-open
// time excludes binding and publication queries. None of these aggregate spans
// is a pure filesystem-blocking measurement or a per-request authority proof.
func (s *Store) MediaSourceAdmissionProfile() map[string]uint64 {
	measurements := &mediaSourceAdmissionMeasurement
	return map[string]uint64{
		"authorizations":                      measurements.authorizations.Load(),
		"authorization_wait_ns":               measurements.authorizationWaitNS.Load(),
		"authorization_ns":                    measurements.authorizationNS.Load(),
		"immediate_grants":                    measurements.immediate.Load(),
		"try_misses":                          measurements.misses.Load(),
		"reauthorizations":                    measurements.reauthorizations.Load(),
		"queued_grants":                       measurements.queued.Load(),
		"retry_grants":                        measurements.retryGrants.Load(),
		"released_queue_grants":               measurements.releasedQueueGrants.Load(),
		"held_refresh_grants":                 measurements.heldRefreshGrants.Load(),
		"first_fallback_actual_queued_grants": measurements.firstFallbackQueuedGrants.Load(),
		"first_fallback_immediate_grants":     measurements.firstFallbackImmediateGrants.Load(),
		"io_queue_ns":                         measurements.ioQueueNS.Load(),
		"io_held_ns":                          measurements.ioHeldNS.Load(),
		"file_open_ns":                        measurements.fileOpenNS.Load(),
		"handoff_wait_ns":                     measurements.handoffWaitNS.Load(),
		"handoff_held_ns":                     measurements.handoffHeldNS.Load(),
		"publication_ns":                      measurements.publicationNS.Load(),
		"cleanup_wait_ns":                     measurements.cleanupWaitNS.Load(),
		"cleanup_held_ns":                     measurements.cleanupHeldNS.Load(),
		"ack_ns":                              measurements.ackNS.Load(),
		"warm_opens":                          measurements.warm.Load(),
		"cold_fallbacks":                      measurements.cold.Load(),
		"io_measurement_version":              5,
	}
}
