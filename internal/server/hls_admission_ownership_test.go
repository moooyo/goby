package server

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsAdmissionTransportTwin(t *testing.T, h *hlsRuntime, original *hlsSession) *hlsSession {
	t.Helper()
	principal := original.principal
	principal.PeerIP = "203.0.113.10"
	source := library.MediaFile{Item: library.Item{ID: original.key.scope.ItemID}, SourceID: original.key.scope.SourceID, ETag: original.key.stamp}
	session, err := h.register(principal, source, original.key.scope.PlaySessionID,
		playback.ConversionDecision{Plan: &original.key.plan}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if session == original || session.key.scope != original.key.scope || session.key.stamp != original.key.stamp || !session.key.remote {
		t.Fatal("test did not create separate active registrations for the exact same manager spec")
	}
	session.timeline = original.timeline
	return session
}

func TestHLSAdmissionAbandonedReuseCannotCancelAnotherActiveOwner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	for _, cause := range []string{"request_cancel", "error_with_record"} {
		t.Run(cause, func(t *testing.T) {
			h, jobs := hlsAdmissionFixture(t)
			first := hlsRuntimeTestSession(t, h, "owned-reuse", false)
			first.principal.User.Policy, first.principal.PeerIP = []byte("{}"), "127.0.0.1"
			_, firstResult := hlsAdmissionSegmentRequest(t, h, first, context.Background(), 0)
			owned := hlsAdmissionCall(t, jobs)
			owned.finish()
			hlsAdmissionResult(t, firstResult, errHLSRuntimeTestReleased)
			h.workers.Wait()
			second := hlsAdmissionTransportTwin(t, h, first)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, secondResult := hlsAdmissionSegmentRequest(t, h, second, ctx, 0)
			reused := hlsAdmissionCall(t, jobs)
			if reused.id != owned.id || reused.spec != owned.spec {
				t.Fatal("test did not reuse the already attached producer")
			}
			if cause == "request_cancel" {
				cancel()
				hlsAdmissionResult(t, secondResult, context.Canceled)
				reused.finish()
			} else {
				reused.err = transcode.ErrPersistence
				reused.finish()
				hlsAdmissionResult(t, secondResult, transcode.ErrPersistence)
			}
			h.workers.Wait()
			if _, err := jobs.Snapshot(first.key.scope, owned.id); err != nil {
				t.Fatalf("abandoned deduplicated admission revoked another registration: %v", err)
			}
			h.retire(second)
			jobs.mu.Lock()
			cancelled := len(jobs.events)
			jobs.mu.Unlock()
			if cancelled != 2 {
				t.Fatal("a rejected reused ID was treated as an unowned orphan")
			}
			h.retire(first)
			h.retire(first)
			h.producerMu.Lock()
			remaining := len(h.producerOwners)
			h.producerMu.Unlock()
			if remaining != 0 {
				t.Fatal("repeated retirement leaked producer ownership")
			}
			jobs.mu.Lock()
			cancelled = len(jobs.events)
			jobs.mu.Unlock()
			if cancelled != 3 {
				t.Fatal("the last attached owner did not cancel exactly once")
			}
		})
	}
}

func TestHLSAdmissionSharedProducerRetiresOnlyWithLastOwner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	first := hlsRuntimeTestSession(t, h, "shared-producer", false)
	first.principal.User.Policy, first.principal.PeerIP = []byte("{}"), "127.0.0.1"
	_, firstResult := hlsAdmissionSegmentRequest(t, h, first, context.Background(), 0)
	owned := hlsAdmissionCall(t, jobs)
	owned.finish()
	hlsAdmissionResult(t, firstResult, errHLSRuntimeTestReleased)
	second := hlsAdmissionTransportTwin(t, h, first)
	_, secondResult := hlsAdmissionSegmentRequest(t, h, second, context.Background(), 0)
	reused := hlsAdmissionCall(t, jobs)
	reused.finish()
	hlsAdmissionResult(t, secondResult, errHLSRuntimeTestReleased)
	if owned.id != reused.id {
		t.Fatal("test did not attach the same producer twice")
	}
	h.retire(first)
	h.retire(first)
	if _, err := jobs.Snapshot(second.key.scope, reused.id); err != nil || second.ctx.Err() != nil {
		t.Fatalf("retiring one owner revoked a shared active producer: %v", err)
	}
	h.retire(second)
	h.producerMu.Lock()
	remaining := len(h.producerOwners)
	h.producerMu.Unlock()
	if remaining != 0 {
		t.Fatal("last-owner retirement retained ownership state")
	}
	jobs.mu.Lock()
	events := append([]string(nil), jobs.events...)
	jobs.mu.Unlock()
	if len(events) != 3 || events[2] != "cancel:"+owned.id {
		t.Fatalf("shared producer was not cancelled exactly at last-owner retirement: %v", events)
	}
}

func TestHLSAdmissionReusedProducerRetiredBeforeAttachmentRetries(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	first := hlsRuntimeTestSession(t, h, "stale-reuse", false)
	first.principal.User.Policy, first.principal.PeerIP = []byte("{}"), "127.0.0.1"
	_, firstResult := hlsAdmissionSegmentRequest(t, h, first, context.Background(), 0)
	owned := hlsAdmissionCall(t, jobs)
	owned.finish()
	hlsAdmissionResult(t, firstResult, errHLSRuntimeTestReleased)
	second := hlsAdmissionTransportTwin(t, h, first)
	_, secondResult := hlsAdmissionSegmentRequest(t, h, second, context.Background(), 0)
	reused := hlsAdmissionCall(t, jobs)
	if reused.id != owned.id {
		t.Fatal("test did not observe reuse before the last owner retired")
	}
	h.retire(first)
	reused.finish()
	replacement := hlsAdmissionCall(t, jobs)
	if replacement.id == owned.id || replacement.spec != owned.spec {
		t.Fatal("active request did not retry the stale deduplicated producer")
	}
	replacement.finish()
	hlsAdmissionResult(t, secondResult, errHLSRuntimeTestReleased)
	second.mu.Lock()
	attached := append([]hlsProducer(nil), second.producers...)
	second.mu.Unlock()
	if len(attached) != 1 || attached[0].id != replacement.id {
		t.Fatal("stale deduplicated ID was attached or retained in ownership")
	}
	h.retire(second)
}

func TestHLSAdmissionProgressiveRegistrationsShareProducerOwnership(t *testing.T) {
	fixture := newAudioRuntimeFixture(t)
	firstRelease, id := audioRuntimeLease(t, fixture, fixture.session)
	principal := fixture.principal
	principal.PeerIP = "127.0.0.1"
	second, err := fixture.h.register(principal, fixture.source, "play_audio-runtime", fixture.decision, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondRelease, reusedID := audioRuntimeLease(t, fixture, second)
	if id != reusedID || second == fixture.session {
		t.Fatal("test did not attach a shared progressive producer to separate registrations")
	}
	firstRelease()
	if _, err := fixture.jobs.Snapshot(second.key.scope, id); err != nil || second.ctx.Err() != nil {
		t.Fatalf("departing progressive registration revoked another owner: %v", err)
	}
	secondRelease()
	fixture.jobs.mu.Lock()
	fences := fixture.jobs.fences
	fixture.jobs.mu.Unlock()
	fixture.h.producerMu.Lock()
	remaining := len(fixture.h.producerOwners)
	fixture.h.producerMu.Unlock()
	if fences != 1 || remaining != 0 {
		t.Fatalf("progressive final cleanup produced %d fences and retained %d ownership records", fences, remaining)
	}
}

func TestHLSAdmissionSnapshotHardFailureDoesNotRetryCreation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	cases := []struct {
		name  string
		err   error
		state string
		want  error
	}{
		{"runner_failed", transcode.ErrJobFailed, "", transcode.ErrJobFailed},
		{"quota", transcode.ErrQuota, "", transcode.ErrQuota},
		{"persistence", transcode.ErrPersistence, "", transcode.ErrPersistence},
		{"cache_unavailable", transcode.ErrOutputUnavailable, "", transcode.ErrOutputUnavailable},
		{"manager_closed", transcode.ErrManagerClosed, "", transcode.ErrManagerClosed},
		{"failed_state", nil, "failed", transcode.ErrJobFailed},
	}
	for _, generated := range []bool{false, true} {
		for _, test := range cases {
			mode := "legacy/"
			if generated {
				mode = "generated/"
			}
			t.Run(mode+test.name, func(t *testing.T) {
				h, jobs := hlsAdmissionFixture(t)
				session := hlsRuntimeTestSession(t, h, "immediate-hard-failure", false)
				if generated {
					session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
				}
				input := hlsRuntimeInput(t)
				result := make(chan error, 1)
				go func() {
					var err error
					if generated {
						_, err = h.generatedArtifact(context.Background(), session, input, "main.m3u8")
					} else {
						_, err = h.segment(context.Background(), session, input, 0)
					}
					result <- err
				}()
				call := hlsAdmissionCall(t, jobs)
				jobs.mu.Lock()
				jobs.snapshotErr, jobs.snapshotState = test.err, test.state
				jobs.mu.Unlock()
				call.finish()
				hlsAdmissionResult(t, result, test.want)
				h.workers.Wait()
				jobs.mu.Lock()
				ensured := len(jobs.calls)
				jobs.mu.Unlock()
				if ensured != 1 {
					t.Fatalf("hard failure triggered %d admission attempts", ensured)
				}
				h.producerMu.Lock()
				owners := len(h.producerOwners)
				h.producerMu.Unlock()
				if owners != 0 {
					t.Fatal("failed job acquired an attached ownership lease")
				}
				_, inputErr := input.Stat()
				if (generated && inputErr != nil) || (!generated && !errors.Is(inputErr, os.ErrClosed)) {
					t.Fatalf("hard failure changed request input ownership: %v", inputErr)
				}
			})
		}
	}
}

func TestHLSAdmissionRepeatedStaleSnapshotHasBoundedRetry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "repeated-stale-snapshot", false)
	_, result := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	first := hlsAdmissionCall(t, jobs)
	jobs.mu.Lock()
	jobs.snapshotErr = transcode.ErrJobNotFound
	jobs.mu.Unlock()
	first.finish()
	second := hlsAdmissionCall(t, jobs)
	second.finish()
	hlsAdmissionResult(t, result, transcode.ErrJobNotFound)
	h.workers.Wait()
	jobs.mu.Lock()
	ensured := len(jobs.calls)
	jobs.mu.Unlock()
	if ensured != 2 {
		t.Fatalf("repeated stale snapshot triggered %d attempts, want one retry", ensured)
	}
}

func TestHLSAdmissionUnownedDescriptorCannotRevokeAttachedProducer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("HLS admission calls transcode.DuplicateInput, which requires Linux")
	}
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "unowned-descriptor", false)
	_, result := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	call := hlsAdmissionCall(t, jobs)
	call.finish()
	hlsAdmissionResult(t, result, errHLSRuntimeTestReleased)
	unowned := hlsProducer{id: call.id, first: 0, last: 3}
	h.releaseProducer(session.key.scope, unowned)
	if !h.producerReleased(unowned) {
		t.Fatal("an unowned descriptor was considered usable")
	}
	if _, err := jobs.Snapshot(session.key.scope, call.id); err != nil {
		t.Fatalf("an unowned descriptor revoked an attached producer: %v", err)
	}
	h.retire(session)
}
