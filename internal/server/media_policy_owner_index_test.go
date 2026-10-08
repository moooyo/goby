package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

func TestMediaPolicyOwnerQuotaSeparatesCredentialKindsAndGroupsLogins(t *testing.T) {
	runtime := newMediaPolicyRuntime(nil)
	defer runtime.stop()
	principal := mediaPolicyTestPrincipal(2)
	principal.User.ID = "shared-owner"
	firstScope := mediaPolicyTestScope(principal, "first")
	first, firstDone, err := runtime.acquire(context.Background(), principal, firstScope)
	if err != nil {
		t.Fatal(err)
	}
	defer firstDone()
	sibling := principal
	sibling.SessionID = "sibling-auth"
	secondScope := mediaPolicyTestScope(sibling, "second")
	second, secondDone, err := runtime.acquire(context.Background(), sibling, secondScope)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDone()
	application := identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 1,
		SessionID: principal.User.ID, ClientSessionID: "application-client"}
	applicationScope := mediaPolicyTestScope(application, "application-play")
	applicationWork, applicationDone, err := runtime.acquire(context.Background(), application, applicationScope)
	if err != nil {
		t.Fatal(err)
	}
	defer applicationDone()
	runtime.touch(application, applicationScope, mediaPolicyContextLease(applicationWork))
	sibling.User.Policy = mediaPolicyTestPrincipal(1).User.Policy
	if err := runtime.check(sibling, secondScope); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("a sibling login bypassed the tightened owner quota: %v", err)
	}
	if mediaPolicyContextLease(second).ctx.Err() == nil || first.Err() != nil {
		t.Fatal("tightening did not retain the owner's oldest play")
	}
	if applicationWork.Err() != nil || !runtime.heartbeat(application, applicationScope) {
		t.Fatal("a user policy retired an application key with the same owner text")
	}
	if len(runtime.leases) != 2 {
		t.Fatal("tightening retained a revoked play or removed another credential kind")
	}
}

func TestMediaPolicyRetirementReturnsOwnerQuota(t *testing.T) {
	for _, disposition := range []string{"complete-active", "complete-idle", "fail-active", "fail-idle", "cancel", "release", "expire", "reconcile"} {
		t.Run(disposition, func(t *testing.T) {
			retirements := 0
			runtime := newMediaPolicyRuntime(func(transcode.Scope) { retirements++ })
			defer runtime.stop()
			clock := mediaPolicyHeartbeatClock(runtime)
			principal := mediaPolicyTestPrincipal(1)
			scope := mediaPolicyTestScope(principal, "retired")
			work, done, err := runtime.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatal(err)
			}
			defer done()
			lease := mediaPolicyContextLease(work)
			switch disposition {
			case "complete-active":
				runtime.complete(scope, lease)
				done()
			case "complete-idle":
				done()
				runtime.complete(scope, lease)
			case "fail-active":
				runtime.fail(scope, lease)
				done()
			case "fail-idle":
				done()
				runtime.fail(scope, lease)
			case "cancel":
				runtime.cancelMatching(principal.SessionID, scope.PlaySessionID)
			case "release":
				runtime.release(scope)
			case "expire", "reconcile":
				done()
				clock.Add(int64(mediaPolicyIdleTTL + time.Second))
				if disposition == "expire" {
					runtime.expire(lease, lease.version)
				} else if err := runtime.check(principal, scope); !errors.Is(err, library.ErrForbidden) {
					t.Fatalf("reconciliation did not revoke the expired play: %v", err)
				}
			}
			wantRetirements := 1
			if disposition == "complete-active" || disposition == "complete-idle" {
				wantRetirements = 0
			}
			if retirements != wantRetirements {
				t.Fatalf("retirement callbacks = %d, want %d", retirements, wantRetirements)
			}
			replacement, replacementDone, err := runtime.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatalf("a retired play retained the owner's only slot: %v", err)
			}
			defer replacementDone()
			runtime.expire(lease, lease.version)
			runtime.complete(scope, lease)
			runtime.fail(scope, lease)
			done()
			current := mediaPolicyContextLease(replacement)
			if current.ctx.Err() != nil || current.complete || current.failed || current.refs != 1 {
				t.Fatal("an obsolete request or timer retired its replacement")
			}
			if _, _, err := runtime.acquire(context.Background(), principal, mediaPolicyTestScope(principal, "excess")); !errors.Is(err, transcode.ErrBusy) {
				t.Fatalf("the replacement did not retain the owner's quota: %v", err)
			}
		})
	}
}

func TestMediaPolicyGlobalCapacityReclaimsDelayedIdleLeases(t *testing.T) {
	var retired []transcode.Scope
	runtime := newMediaPolicyRuntime(func(scope transcode.Scope) { retired = append(retired, scope) })
	defer runtime.stop()
	clock := mediaPolicyHeartbeatClock(runtime)
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	var firstLease, lastLease *mediaPolicyLease
	for index := range maxMediaPolicyLeases {
		principal := mediaPolicyTestPrincipal(1)
		principal.User.ID = fmt.Sprintf("owner-%d", index)
		work, done, err := runtime.acquire(context.Background(), principal, mediaPolicyTestScope(principal, "play"))
		if err != nil {
			t.Fatalf("global capacity filled prematurely at %d: %v", index, err)
		}
		releases = append(releases, done)
		lastLease = mediaPolicyContextLease(work)
		if index == 0 {
			firstLease = lastLease
		}
	}
	principal := mediaPolicyTestPrincipal(1)
	principal.User.ID = "new-owner"
	scope := mediaPolicyTestScope(principal, "replacement")
	if _, _, err := runtime.acquire(context.Background(), principal, scope); !errors.Is(err, transcode.ErrBusy) {
		t.Fatalf("new owners bypassed the global capacity: %v", err)
	}
	releases[0]()
	clock.Add(int64(mediaPolicyIdleTTL + time.Second))
	work, done, err := runtime.acquire(context.Background(), principal, scope)
	if err != nil {
		t.Fatalf("an expired lease with a delayed timer retained global capacity: %v", err)
	}
	releases = append(releases, done)
	if firstLease.ctx.Err() == nil || lastLease.ctx.Err() != nil || work.Err() != nil {
		t.Fatal("capacity reclamation cancelled an active lease or retained the expired lease")
	}
	if len(retired) != 1 || retired[0] != firstLease.scope || len(runtime.leases) != maxMediaPolicyLeases {
		t.Fatalf("unexpected global-capacity retirement: retired=%v remaining=%d", retired, len(runtime.leases))
	}
	runtime.expire(firstLease, firstLease.version)
	if len(retired) != 1 || work.Err() != nil {
		t.Fatal("a delayed expiry callback repeated retirement or cancelled the replacement")
	}
}
