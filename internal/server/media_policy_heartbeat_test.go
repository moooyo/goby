package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

func mediaPolicyHeartbeatClock(runtime *mediaPolicyRuntime) *atomic.Int64 {
	clock := &atomic.Int64{}
	clock.Store(time.Now().UnixNano())
	runtime.now = func() time.Time { return time.Unix(0, clock.Load()) }
	return clock
}

func TestMediaPolicyHeartbeatKeepsPausedDeliveryQuotaAndExpiresWithoutClient(t *testing.T) {
	retirements := 0
	runtime := newMediaPolicyRuntime(func(transcode.Scope) { retirements++ })
	defer runtime.stop()
	clock := mediaPolicyHeartbeatClock(runtime)
	principal := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(principal, "paused-dynamic-play")
	work, release, err := runtime.acquire(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	lease := mediaPolicyContextLease(work)
	runtime.touch(principal, scope, lease)
	release()
	originalVersion := lease.version
	for range 3 {
		clock.Add(int64(80 * time.Second))
		if !runtime.heartbeat(principal, scope) || lease.refs != 0 {
			t.Fatal("an authenticated client heartbeat did not retain the existing paused lease")
		}
		runtime.expire(lease, originalVersion)
		if lease.ctx.Err() != nil || retirements != 0 {
			t.Fatal("an obsolete idle timer cancelled a refreshed playback")
		}
		if _, _, err := runtime.acquire(context.Background(), principal, mediaPolicyTestScope(principal, "another-play")); !errors.Is(err, transcode.ErrBusy) {
			t.Fatal("paused playback released its simultaneous-stream quota")
		}
	}
	clock.Add(int64(mediaPolicyIdleTTL - time.Second))
	runtime.expire(lease, lease.version)
	if lease.ctx.Err() != nil {
		t.Fatal("the refreshed lease expired before its client idle deadline")
	}
	clock.Add(int64(2 * time.Second))
	runtime.expire(lease, lease.version)
	if lease.ctx.Err() == nil || len(runtime.leases) != 0 || retirements != 1 {
		t.Fatal("missing client heartbeats did not retire delivery after the idle deadline")
	}
	if runtime.heartbeat(principal, scope) || len(runtime.leases) != 0 {
		t.Fatal("a late heartbeat revived an expired quota lease")
	}
}

func TestMediaPolicyHeartbeatNeverAdmitsOrClaimsUndeliveredMedia(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	clock := mediaPolicyHeartbeatClock(runtime)
	principal := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(principal, "prepared-only")
	if runtime.heartbeat(principal, scope) || len(runtime.leases) != 0 {
		t.Fatal("a prepared playback heartbeat admitted a media lease")
	}
	work, release, err := runtime.acquire(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	lease := mediaPolicyContextLease(work)
	release()
	before, version := lease.last, lease.version
	clock.Add(int64(80 * time.Second))
	if runtime.heartbeat(principal, scope) || lease.delivered || !lease.last.Equal(before) || lease.version != version {
		t.Fatal("a heartbeat claimed media delivery or extended an unused admission")
	}
	clock.Add(int64(11 * time.Second))
	if runtime.heartbeat(principal, scope) || lease.ctx.Err() == nil || len(runtime.leases) != 0 {
		t.Fatal("a heartbeat rescued an idle lease before its timer callback ran")
	}
}

func TestMediaPolicyHeartbeatRequiresExactCredentialClientAndSource(t *testing.T) {
	for _, application := range []bool{false, true} {
		name := "ordinary"
		principal := mediaPolicyTestPrincipal(1)
		if application {
			name = "application-key"
			principal = identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 1,
				SessionID: "key-auth", ClientSessionID: "key-client", Client: identity.Client{DeviceID: "key-device"}}
		}
		t.Run(name, func(t *testing.T) {
			runtime := newMediaPolicyRuntime(nil)
			defer runtime.stop()
			clock := mediaPolicyHeartbeatClock(runtime)
			scope := mediaPolicyTestScope(principal, "owned-play")
			work, release, err := runtime.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatal(err)
			}
			lease := mediaPolicyContextLease(work)
			runtime.touch(principal, scope, lease)
			release()
			before, version := lease.last, lease.version
			clock.Add(int64(30 * time.Second))
			for _, mutate := range []func(*transcode.Scope){
				func(value *transcode.Scope) { value.ApplicationKey = !value.ApplicationKey },
				func(value *transcode.Scope) { value.ApplicationClientID += "-foreign" },
				func(value *transcode.Scope) { value.UserID += "-foreign" },
				func(value *transcode.Scope) { value.AuthSessionID += "-foreign" },
				func(value *transcode.Scope) { value.DeviceID += "-foreign" },
				func(value *transcode.Scope) { value.PlaySessionID += "-foreign" },
				func(value *transcode.Scope) { value.ItemID += "-foreign" },
				func(value *transcode.Scope) { value.SourceID += "-foreign" },
				func(value *transcode.Scope) { value.PlaySessionID = "" },
			} {
				foreign := scope
				mutate(&foreign)
				if runtime.heartbeat(principal, foreign) || !lease.last.Equal(before) || lease.version != version {
					t.Fatal("a foreign or legacy scope extended the canonical media lease")
				}
			}
			foreignPrincipal := principal
			foreignPrincipal.ClientSessionID += "-other-client"
			foreignScope := scope
			foreignScope.ApplicationClientID = foreignPrincipal.ClientSessionID
			if runtime.heartbeat(foreignPrincipal, foreignScope) || !lease.last.Equal(before) {
				t.Fatal("a sibling authenticated client refreshed another client's playback")
			}
			if !runtime.heartbeat(principal, scope) {
				t.Fatal("the exact admitted client and source could not refresh delivery")
			}
		})
	}
}

func TestMediaPolicyHeartbeatCannotUndoCompletionFailureStopOrTightening(t *testing.T) {
	for _, disposition := range []string{"complete", "failure", "stopped", "shutdown", "tightened"} {
		t.Run(disposition, func(t *testing.T) {
			runtime := newMediaPolicyRuntime(nil)
			defer runtime.stop()
			clock := mediaPolicyHeartbeatClock(runtime)
			principal := mediaPolicyTestPrincipal(2)
			if disposition == "tightened" {
				first, done, err := runtime.acquire(context.Background(), principal, mediaPolicyTestScope(principal, "older-play"))
				if err != nil {
					t.Fatal(err)
				}
				runtime.touch(principal, mediaPolicyTestScope(principal, "older-play"), mediaPolicyContextLease(first))
				done()
			}
			scope := mediaPolicyTestScope(principal, "active-play")
			work, release, err := runtime.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			lease := mediaPolicyContextLease(work)
			if disposition != "failure" {
				runtime.touch(principal, scope, lease)
			}
			switch disposition {
			case "complete":
				runtime.complete(scope, lease)
			case "failure":
				runtime.fail(scope, lease)
			case "stopped":
				runtime.cancelMatching(principal.SessionID, scope.PlaySessionID)
			case "shutdown":
				runtime.stop()
			case "tightened":
				principal = mediaPolicyTestPrincipal(1)
			}
			before := lease.last
			clock.Add(int64(10 * time.Second))
			if runtime.heartbeat(principal, scope) || !lease.last.Equal(before) {
				t.Fatal("a heartbeat revived revoked or terminal delivery")
			}
			if disposition == "tightened" && (lease.ctx.Err() == nil || len(runtime.leases) != 1) {
				t.Fatal("heartbeat did not enforce the current simultaneous-stream limit")
			}
		})
	}
}

func TestDynamicMediaPolicyHeartbeatUsesStoredPlayAndRejectsTerminalOrLocalState(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	app := &Server{mediaPolicy: runtime}
	app.mediaPolicyOnce.Do(func() {})
	clock := mediaPolicyHeartbeatClock(runtime)
	principal := mediaPolicyTestPrincipal(1)
	scope := mediaPolicyTestScope(principal, "dynamic-play")
	work, release, err := runtime.acquire(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	lease := mediaPolicyContextLease(work)
	runtime.touch(principal, scope, lease)
	release()
	play := library.PlaySession{ID: scope.PlaySessionID, UserID: scope.UserID, AuthSessionID: scope.AuthSessionID,
		DeviceID: scope.DeviceID, ItemID: scope.ItemID, MediaSourceID: scope.SourceID, State: "Paused", IsDynamic: true}
	before := lease.last
	clock.Add(int64(30 * time.Second))
	for _, mutate := range []func(*library.PlaySession){
		func(value *library.PlaySession) { value.IsDynamic = false },
		func(value *library.PlaySession) { value.ID = "" },
		func(value *library.PlaySession) { value.State = "Stopped" },
		func(value *library.PlaySession) { value.State = "Expired" },
		func(value *library.PlaySession) { value.StoppedAt = &before },
		func(value *library.PlaySession) { value.AuthSessionID = "foreign" },
		func(value *library.PlaySession) { value.DeviceID = "foreign" },
		func(value *library.PlaySession) { value.MediaSourceID = "foreign" },
	} {
		invalid := play
		mutate(&invalid)
		if app.heartbeatDynamicMediaPolicy(principal, invalid) || !lease.last.Equal(before) {
			t.Fatal("an ineligible playback report extended a dynamic delivery lease")
		}
	}
	if !app.heartbeatDynamicMediaPolicy(principal, play) {
		t.Fatal("an authorized paused dynamic play did not retain its admitted delivery")
	}
}
