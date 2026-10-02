//go:build linux && primary_io_measure

package server

import (
	"context"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/transcode"
)

// This deterministic interleaving proves the production bookkeeping state that
// the controller must classify. It creates no HTTP request, source reader or
// native process and cannot stand in for the actual matched TCP measurement.
func TestPrimaryHTTPMeasurePolicyClassifiesShared304FailBeforeFinish(t *testing.T) {
	control, gate, principal, scope := primaryHTTPMeasurePolicyTestControl(t)
	first, finishFirst, err := gate.acquire(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	second, finishSecond, err := gate.acquire(context.Background(), principal, scope)
	if err != nil {
		finishFirst()
		t.Fatal(err)
	}
	t.Cleanup(finishFirst)
	t.Cleanup(finishSecond)
	gate.fail(scope, mediaPolicyContextLease(first))
	gate.fail(scope, mediaPolicyContextLease(second))
	active, accepted := control.policyObservation(&primaryHTTPMeasureWave{Purpose: "original", Mode: "not-modified", Concurrency: 1})
	if accepted || active.Total != 1 || active.References != 2 {
		t.Fatalf("shared active conditional identity was accepted: observation=%+v accepted=%v", active, accepted)
	}
	finishFirst()
	finishSecond()
	idle, accepted := control.policyObservation(&primaryHTTPMeasureWave{Purpose: "original", Mode: "not-modified", Concurrency: 1})
	if !accepted || idle.Total != 1 || idle.UndeliveredIdle != 1 || idle.DeliveredIdle != 0 || idle.References != 0 ||
		idle.Terminal != 0 || idle.Cancelled != 0 || idle.MissingTimer != 0 || idle.UnexpectedIdentity != 0 {
		t.Fatalf("natural shared conditional idle bookkeeping was misclassified: observation=%+v accepted=%v", idle, accepted)
	}
	control.fixture.f.app.cancelMediaPolicy(principal.SessionID, "")
	cleared, accepted := control.policyObservation(&primaryHTTPMeasureWave{Purpose: "original", Mode: "not-modified", Concurrency: 1})
	if !accepted || cleared.Total != 0 {
		t.Fatalf("explicit post-counter metadata cleanup did not remove identity: %+v", cleared)
	}
}

func TestPrimaryHTTPMeasurePolicyRejectsActiveDeliveredTerminalAndForeign304State(t *testing.T) {
	for _, mutation := range []string{"active", "delivered", "terminal", "cancelled", "missing-timer", "foreign-key", "foreign-scope", "extra-account"} {
		t.Run(mutation, func(t *testing.T) {
			control, gate, principal, scope := primaryHTTPMeasurePolicyTestControl(t)
			if mutation == "foreign-key" {
				scope.ItemID = "another-source-item"
			}
			work, finish, err := gate.acquire(context.Background(), principal, scope)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(finish)
			lease := mediaPolicyContextLease(work)
			if mutation != "active" {
				finish()
			}
			gate.mu.Lock()
			switch mutation {
			case "delivered":
				lease.delivered = true
			case "terminal":
				lease.complete, lease.failed = true, true
			case "cancelled":
				lease.cancel()
			case "missing-timer":
				lease.timer.Stop()
				lease.timer = nil
			case "foreign-scope":
				lease.scope.SourceID = "another-source-binding"
			}
			gate.mu.Unlock()
			if mutation == "extra-account" {
				sibling := principal
				sibling.User.ID, sibling.SessionID = "another-owner", "another-auth"
				other := scope
				other.UserID, other.AuthSessionID = sibling.User.ID, sibling.SessionID
				_, finishOther, err := gate.acquire(context.Background(), sibling, other)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(finishOther)
				finishOther()
			}
			observed, accepted := control.policyObservation(&primaryHTTPMeasureWave{Purpose: "original", Mode: "not-modified", Concurrency: 1})
			if accepted {
				t.Fatalf("conditional observation accepted %s state: %+v", mutation, observed)
			}
		})
	}
}

func TestPrimaryHTTPMeasurePolicyPreservesFullRangeHeadAndDownloadContracts(t *testing.T) {
	control, gate, principal, scope := primaryHTTPMeasurePolicyTestControl(t)
	_, finish, err := gate.acquire(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(finish)
	finish()
	for _, wave := range []primaryHTTPMeasureWave{
		{Purpose: "original", Mode: "full", Concurrency: 1},
		{Purpose: "original", Mode: "head", Concurrency: 1},
		{Purpose: "download", Mode: "not-modified", Concurrency: 1},
		{Purpose: "download", Mode: "range", Concurrency: 1},
	} {
		if observed, accepted := control.policyObservation(&wave); accepted {
			t.Fatalf("%s/%s accepted an idle conditional identity: %+v", wave.Purpose, wave.Mode, observed)
		}
	}
	gate.mu.Lock()
	for _, lease := range gate.leases {
		lease.delivered = true
	}
	gate.mu.Unlock()
	if observed, accepted := control.policyObservation(&primaryHTTPMeasureWave{Purpose: "original", Mode: "range", Concurrency: 1}); !accepted || observed.DeliveredIdle != 1 || observed.References != 0 {
		t.Fatalf("exact delivered original range identity was rejected: %+v", observed)
	}
	control.fixture.f.app.cancelMediaPolicy(principal.SessionID, "")
	for _, wave := range []primaryHTTPMeasureWave{
		{Purpose: "original", Mode: "full", Concurrency: 1},
		{Purpose: "original", Mode: "head", Concurrency: 1},
		{Purpose: "download", Mode: "full", Concurrency: 1},
		{Purpose: "download", Mode: "range", Concurrency: 1},
		{Purpose: "download", Mode: "head", Concurrency: 1},
		{Purpose: "download", Mode: "not-modified", Concurrency: 1},
	} {
		if observed, accepted := control.policyObservation(&wave); !accepted || observed.Total != 0 {
			t.Fatalf("%s/%s rejected empty natural policy state: %+v", wave.Purpose, wave.Mode, observed)
		}
	}
}

func primaryHTTPMeasurePolicyTestControl(t *testing.T) (*primaryHTTPMeasureControl, *mediaPolicyRuntime, identity.Principal, transcode.Scope) {
	t.Helper()
	principal := mediaPolicyTestPrincipal(0)
	scope := mediaPolicyTestScope(principal, "")
	app := &Server{}
	gate := app.playbackPolicyGate()
	t.Cleanup(gate.stop)
	control := &primaryHTTPMeasureControl{fixture: &streamHTTPFixture{
		f: &serverFixture{app: app}, video: streamHTTPItem{id: scope.ItemID}},
		principals: []identity.Principal{principal}, sourceID: scope.SourceID}
	return control, gate, principal, scope
}
