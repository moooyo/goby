package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

func mediaPolicyTestPrincipal(limit int) identity.Principal {
	return identity.Principal{Kind: "emby", SessionID: "auth", Client: identity.Client{DeviceID: "device"},
		User: identity.User{ID: "owner", Policy: json.RawMessage(fmt.Sprintf(`{"SimultaneousStreamLimit":%d}`, limit))}}
}

func mediaPolicyTestScope(p identity.Principal, play string) transcode.Scope {
	return transcode.Scope{ApplicationKey: p.IsApplicationKey(), ApplicationClientID: p.ClientSessionID,
		UserID: p.User.ID, AuthSessionID: p.SessionID, DeviceID: p.Client.DeviceID, PlaySessionID: play,
		ItemID: "item", SourceID: "source"}
}

func TestMediaPolicyCountsDeliveriesAndSharesRangesVariantsAndTransports(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "canonical-play")
	// Constructing prepared identities does not reserve playback capacity.
	for index := range 100 {
		_ = mediaPolicyTestScope(p, fmt.Sprint(index))
	}
	var done []func()
	var work context.Context
	for range 12 {
		current, release, err := runtime.acquire(context.Background(), p, scope)
		if err != nil {
			t.Fatalf("a range, revision, or transport of the same play used another slot: %v", err)
		}
		done = append(done, release)
		work = current
	}
	if len(runtime.leases) != 1 {
		t.Fatal("one actual play was counted more than once")
	}
	sibling := p
	sibling.SessionID, sibling.Client.DeviceID = "other-auth", "other-device"
	if _, _, err := runtime.acquire(context.Background(), sibling, mediaPolicyTestScope(sibling, "second")); !errors.Is(err, transcode.ErrBusy) {
		t.Fatalf("another login bypassed the account-wide delivery limit: %v", err)
	}
	for _, release := range done {
		release()
		release()
	}
	if len(runtime.leases) != 1 {
		t.Fatal("an ordinary gap between ranges released the logical playback")
	}
	runtime.complete(scope, mediaPolicyContextLease(work))
	if len(runtime.leases) != 0 {
		t.Fatal("completed playback retained its capacity")
	}
}

func TestMediaPolicyAdmissionIsAtomicAcrossDifferentPlays(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	var group sync.WaitGroup
	results := make(chan error, 32)
	for index := range 32 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, done, err := runtime.acquire(context.Background(), p, mediaPolicyTestScope(p, fmt.Sprint(index)))
			if err == nil {
				done()
			}
			results <- err
		}(index)
	}
	group.Wait()
	close(results)
	admitted := 0
	for err := range results {
		if err == nil {
			admitted++
		} else if !errors.Is(err, transcode.ErrBusy) {
			t.Fatal(err)
		}
	}
	if admitted != 1 {
		t.Fatalf("concurrent admissions exceeded the policy: %d", admitted)
	}
}

func TestMediaPolicyIdleExpiryAndCancellationReleaseSharedCapacity(t *testing.T) {
	var retired []transcode.Scope
	runtime := newMediaPolicyRuntime(func(scope transcode.Scope) { retired = append(retired, scope) })
	defer runtime.stop()
	now := time.Now()
	runtime.now = func() time.Time { return now }
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "first")
	_, done, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	done()
	now = now.Add(mediaPolicyIdleTTL + time.Second)
	work, done, err := runtime.acquire(context.Background(), p, mediaPolicyTestScope(p, "second"))
	if err != nil || len(retired) != 1 || retired[0] != scope {
		t.Fatalf("idle delivery did not release its shared capacity: %v, %+v", err, retired)
	}
	defer done()
	runtime.cancelMatching(p.SessionID, "second")
	select {
	case <-work.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelled playback left its active response alive")
	}
	if len(runtime.leases) != 0 {
		t.Fatal("cancelled playback retained its quota")
	}
}

func TestMediaPolicyTighteningCancelsNewestPlaybackWithoutNewAdmission(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(2)
	first, doneFirst, err := runtime.acquire(context.Background(), p, mediaPolicyTestScope(p, "first"))
	if err != nil {
		t.Fatal(err)
	}
	defer doneFirst()
	second, doneSecond, err := runtime.acquire(context.Background(), p, mediaPolicyTestScope(p, "second"))
	if err != nil {
		t.Fatal(err)
	}
	defer doneSecond()
	p = mediaPolicyTestPrincipal(1)
	if err := runtime.check(p, mediaPolicyTestScope(p, "second")); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("an active stream ignored a tightened policy: %v", err)
	}
	select {
	case <-second.Done():
	case <-time.After(time.Second):
		t.Fatal("tightening did not cancel the newer transport")
	}
	if first.Err() != nil || len(runtime.leases) != 1 {
		t.Fatal("tightening cancelled the oldest admitted play or kept excess capacity")
	}
}

func TestMediaPolicyDoesNotConfuseSourcesAccountsOrApplicationKeys(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "")
	_, done, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	other := scope
	other.ItemID, other.SourceID = "other-item", "other-source"
	if _, _, err := runtime.acquire(context.Background(), p, other); !errors.Is(err, transcode.ErrBusy) {
		t.Fatal("a reused or missing play ID combined unrelated original sources")
	}
	app := identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 1, SessionID: "application", ClientSessionID: "application-client"}
	_, release, err := runtime.acquire(context.Background(), app, mediaPolicyTestScope(app, "application-play"))
	if err != nil {
		t.Fatal("application credentials borrowed a login user's limit")
	}
	release()
	for _, raw := range []string{"", "null", `{"SimultaneousStreamLimit":-1}`} {
		invalid := p
		invalid.User.Policy = json.RawMessage(raw)
		if _, _, err := runtime.acquire(context.Background(), invalid, scope); !errors.Is(err, library.ErrForbidden) {
			t.Fatalf("invalid stored policy granted a delivery lease: %q, %v", raw, err)
		}
	}
}

func TestMediaPolicyRetirementCallbackCanCancelRelatedConversions(t *testing.T) {
	var runtime *mediaPolicyRuntime
	retirements := 0
	runtime = newMediaPolicyRuntime(func(scope transcode.Scope) {
		retirements++
		runtime.cancelMatching(scope.AuthSessionID, scope.PlaySessionID)
	})
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "play")
	work, done, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	runtime.fail(scope, mediaPolicyContextLease(work))
	done()
	if len(runtime.leases) != 0 || retirements != 1 {
		t.Fatal("conversion cancellation retained the retired shared lease")
	}
}

func TestMediaPolicySuccessfulCompletionKeepsProgressivePresenceAndCachedOutput(t *testing.T) {
	fixture := newAudioRuntimeFixture(t)
	fixture.principal.User.Policy = []byte(`{"SimultaneousStreamLimit":1}`)
	app := &Server{hls: fixture.h}
	t.Cleanup(app.stopMediaPolicy)
	scope := fixture.session.key.scope
	work, policyDone, err := app.acquireMediaPolicy(context.Background(), fixture.principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer policyDone()
	release, id := audioRuntimeLease(t, fixture, fixture.session)
	fixture.jobs.mu.Lock()
	fixture.jobs.records[id].record.State = "completed"
	fixture.jobs.mu.Unlock()
	app.touchMediaPolicy(work, fixture.principal, scope)
	app.completeMediaPolicy(work, scope)
	// Match HTTP cleanup: release the progressive reader before its policy
	// reference, preserving the existing completed-output registry contract.
	release()
	policyDone()
	gate := app.playbackPolicyGate()
	gate.mu.Lock()
	remaining := len(gate.leases)
	gate.mu.Unlock()
	fixture.h.mu.Lock()
	registered := fixture.h.sessions[fixture.session.id]
	indexed := fixture.h.byKey[fixture.session.key]
	fixture.h.mu.Unlock()
	fixture.session.mu.Lock()
	closed := fixture.session.closed
	fixture.session.mu.Unlock()
	state, stateErr := fixture.jobs.Snapshot(scope, id)
	if remaining != 0 || registered != fixture.session || indexed != fixture.session || closed || fixture.session.ctx.Err() != nil || stateErr != nil || state.State != "completed" {
		t.Fatal("successful EOF retained delivery quota or removed its presence revision")
	}
	nextScope := scope
	nextScope.PlaySessionID = "next-play"
	nextWork, nextDone, err := app.acquireMediaPolicy(context.Background(), fixture.principal, nextScope)
	if err != nil {
		t.Fatal("completed cached output still occupied the user's only delivery slot")
	}
	app.completeMediaPolicy(nextWork, nextScope)
	nextDone()
	reused := fixture.register(t)
	retryRelease, retryID := audioRuntimeLease(t, fixture, reused)
	retryRelease()
	fixture.jobs.mu.Lock()
	created, fences := fixture.jobs.created, fixture.jobs.fences
	fixture.jobs.mu.Unlock()
	if reused != fixture.session || retryID != id || created != 1 || fences != 0 {
		t.Fatal("normal delivery completion revoked same-scope completed output reuse")
	}
	app.cancelPlaybackResources(scope.AuthSessionID, scope.PlaySessionID)
	fixture.h.mu.Lock()
	registered = fixture.h.sessions[fixture.session.id]
	fixture.h.mu.Unlock()
	fixture.session.mu.Lock()
	closed = fixture.session.closed
	fixture.session.mu.Unlock()
	if registered != nil || !closed {
		t.Fatal("explicit cancellation failed to retire an output after its quota was released")
	}
}

func TestMediaPolicySurvivingConsumerCompletionDoesNotCancelOutput(t *testing.T) {
	for _, releaseBeforeComplete := range []bool{false, true} {
		t.Run(fmt.Sprintf("release-first-%t", releaseBeforeComplete), func(t *testing.T) {
			retirements := 0
			runtime := newMediaPolicyRuntime(func(transcode.Scope) { retirements++ })
			defer runtime.stop()
			principal := mediaPolicyTestPrincipal(1)
			scope := mediaPolicyTestScope(principal, "shared")
			_, firstDone, err := runtime.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatal(err)
			}
			work, lastDone, err := runtime.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatal(err)
			}
			firstDone()
			if work.Err() != nil || retirements != 0 {
				t.Fatal("one consumer leaving interrupted the remaining delivery")
			}
			runtime.touch(principal, scope, mediaPolicyContextLease(work))
			if releaseBeforeComplete {
				lastDone()
			}
			runtime.complete(scope, mediaPolicyContextLease(work))
			lastDone()
			if len(runtime.leases) != 0 || retirements != 0 {
				t.Fatal("successful surviving consumer completion revoked its completed output")
			}
		})
	}
}

func TestMediaPolicyOldRequestCannotFinishOrTouchReplacementLease(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "play")
	old, oldDone, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	runtime.cancelMatching(p.SessionID, scope.PlaySessionID)
	replacement, replacementDone, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	runtime.fail(scope, mediaPolicyContextLease(old))
	runtime.complete(scope, mediaPolicyContextLease(old))
	runtime.touch(p, scope, mediaPolicyContextLease(old))
	oldDone()
	if replacement.Err() != nil || mediaPolicyContextLease(replacement).complete || mediaPolicyContextLease(replacement).delivered {
		t.Fatal("a late callback from a cancelled request mutated its replacement lease")
	}
	replacementDone()
	if len(runtime.leases) != 1 {
		t.Fatal("the replacement lease was retired by an obsolete request")
	}
}

func TestMediaPolicyFailedObsoleteSegmentDoesNotCompleteSharedPlayback(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "play")
	old, oldDone, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	current, currentDone, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	runtime.fail(scope, mediaPolicyContextLease(old))
	oldDone()
	runtime.touch(p, scope, mediaPolicyContextLease(current))
	currentDone()
	if len(runtime.leases) != 1 || mediaPolicyContextLease(current).complete {
		t.Fatal("a cancelled pre-seek segment completed its succeeding media delivery")
	}
}

func TestMediaPolicyFailedInitialRequestRollsBackItsUnusedLease(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	p := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(p, "failed")
	work, done, err := runtime.acquire(context.Background(), p, scope)
	if err != nil {
		t.Fatal(err)
	}
	runtime.fail(scope, mediaPolicyContextLease(work))
	done()
	if len(runtime.leases) != 0 {
		t.Fatal("failed initial production retained a policy slot without media")
	}
	_, release, err := runtime.acquire(context.Background(), p, mediaPolicyTestScope(p, "replacement"))
	if err != nil {
		t.Fatal(err)
	}
	release()
}
