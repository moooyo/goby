package identity_test

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestPreparedClientSessionControlPreservesFeatureAndTargetBoundaries(t *testing.T) {
	targets := []identity.ClientSession{
		{Kind: "emby", UserID: "controller"},
		{Kind: "emby", UserID: "other"},
		{Kind: identity.ApplicationKeyKind},
		{Kind: "emby"},
		{Kind: identity.ApplicationKeyKind, UserID: "other"},
		{Kind: "admin", UserID: "controller"},
	}
	for _, test := range []struct {
		name       string
		controller identity.ClientSessionController
		want       [6]bool
	}{
		{"own login", identity.ClientSessionController{ID: "controller", Policy: json.RawMessage(`{}`)}, [6]bool{true}},
		{"other users", identity.ClientSessionController{ID: "controller", Policy: json.RawMessage(`{"EnableRemoteControlOfOtherUsers":true}`)}, [6]bool{true, true}},
		{"shared devices", identity.ClientSessionController{ID: "controller", Policy: json.RawMessage(`{"EnableSharedDeviceControl":true}`)}, [6]bool{true, false, true}},
		{"administrator", identity.ClientSessionController{ID: "controller", IsAdministrator: true, Policy: json.RawMessage(`{}`)}, [6]bool{true, true, true, true, true, true}},
		{"restricted administrator", identity.ClientSessionController{ID: "controller", IsAdministrator: true, Policy: json.RawMessage(`{"RestrictedFeatures":["goby_remote_control"]}`)}, [6]bool{}},
		{"disabled administrator", identity.ClientSessionController{ID: "controller", IsAdministrator: true, IsDisabled: true, Policy: json.RawMessage(`{}`)}, [6]bool{}},
		{"malformed administrator policy", identity.ClientSessionController{ID: "controller", IsAdministrator: true, Policy: json.RawMessage(`{"EnableSharedDeviceControl":null}`)}, [6]bool{}},
		{"userless controller", identity.ClientSessionController{IsAdministrator: true, Policy: json.RawMessage(`{}`)}, [6]bool{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := identity.PrepareClientSessionControl(test.controller)
			for index, target := range targets {
				if got := control.CanControl(target); got != test.want[index] {
					t.Errorf("target %d control = %t, want %t", index, got, test.want[index])
				}
			}
		})
	}
	if (identity.ClientSessionControl{}).CanControl(targets[0]) {
		t.Fatal("the zero decision granted control")
	}
}

func TestPreparedClientSessionControlOwnsOnlyTheObservedDecision(t *testing.T) {
	controller := identity.ClientSessionController{ID: "controller", Policy: json.RawMessage(`{"EnableSharedDeviceControl":true}`)}
	control := identity.PrepareClientSessionControl(controller)
	controller.Policy[0] = '['
	controller.ID = "changed"
	controller.IsDisabled = true
	if !control.CanControl(identity.ClientSession{Kind: "emby", UserID: "controller"}) ||
		!control.CanControl(identity.ClientSession{Kind: identity.ApplicationKeyKind}) ||
		control.CanControl(identity.ClientSession{Kind: "emby", UserID: "changed"}) {
		t.Fatal("input mutation changed a response-local decision")
	}
	if identity.PrepareClientSessionControl(controller).CanControl(identity.ClientSession{Kind: identity.ApplicationKeyKind}) {
		t.Fatal("a new response reused the previous decision")
	}
}
