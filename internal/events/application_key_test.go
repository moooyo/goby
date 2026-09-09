package events

import (
	"encoding/json"
	"errors"
	"testing"
)

func applicationTestSubscription(t *testing.T, hub *Hub, credentialID, clientID string) *Subscription {
	t.Helper()
	sub, err := hub.Subscribe(Scope{ApplicationKey: true, CredentialID: credentialID, SessionID: clientID, DeviceID: "shared-device"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

func TestApplicationClientScopesKeepTypedDeliveryAndParentRevocation(t *testing.T) {
	hub := newTestHub(t, Options{})
	alpha := applicationTestSubscription(t, hub, "parent-key", "alpha")
	beta := applicationTestSubscription(t, hub, "parent-key", "beta")
	sibling := applicationTestSubscription(t, hub, "sibling-key", "sibling")
	ordinary := subscribeTestScope(t, hub, "ordinary-user", "ordinary-login", "shared-device")
	// Matching strings across credential kinds must never confer event access.
	alias := subscribeTestScope(t, hub, "parent-key", "alpha", "shared-device")
	envelope := Envelope{MessageType: "Playstate", Data: json.RawMessage(`{"Command":"Pause"}`)}
	if count, err := hub.PublishScope(alpha.Scope(), envelope); err != nil || count != 1 {
		t.Fatalf("publish application context = (%d, %v)", count, err)
	}
	nextTestEvent(t, alpha)
	for _, sub := range []*Subscription{beta, sibling, ordinary, alias} {
		assertNoTestEvent(t, sub)
	}
	if count, err := hub.PublishUser("parent-key", envelope); err != nil || count != 1 {
		t.Fatalf("publish user alias = (%d, %v)", count, err)
	}
	nextTestEvent(t, alias)
	assertNoTestEvent(t, alpha)
	assertNoTestEvent(t, beta)
	wrong := alpha.Scope()
	wrong.CredentialID = "sibling-key"
	if count, err := hub.PublishScope(wrong, envelope); err != nil || count != 0 {
		t.Fatalf("publish mismatched parent = (%d, %v)", count, err)
	}
	hub.DisconnectCredential("parent-key")
	for _, sub := range []*Subscription{alpha, beta} {
		if !errors.Is(sub.Reason(), ErrSessionRevoked) {
			t.Error("parent revocation left an application context connected")
		}
	}
	for _, sub := range []*Subscription{sibling, ordinary, alias} {
		if sub.Reason() != nil {
			t.Error("parent revocation disconnected another credential scope")
		}
	}
	hub.DisconnectCredential("ordinary-login")
	if !errors.Is(ordinary.Reason(), ErrSessionRevoked) {
		t.Error("ordinary credential revocation did not close its login")
	}
}

func TestApplicationScopeRejectsFabricatedUsersAndInvalidParents(t *testing.T) {
	hub := newTestHub(t, Options{})
	for _, scope := range []Scope{
		{ApplicationKey: true, CredentialID: "key", SessionID: "client", UserID: "synthetic-user"},
		{ApplicationKey: true, SessionID: "client"},
		{CredentialID: "key", SessionID: "client", UserID: "user"},
		{SessionID: "client"},
	} {
		if _, err := hub.Subscribe(scope); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("invalid subscription scope accepted: %v", err)
		}
		if _, err := hub.PublishScope(scope, Envelope{MessageType: "Changed"}); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("invalid publication scope accepted: %v", err)
		}
	}
}

func TestApplicationCredentialConnectionQuotaIsIndependentOfUsersAndOtherKeys(t *testing.T) {
	hub := newTestHub(t, Options{MaxConnectionsPerUser: 2})
	alpha := applicationTestSubscription(t, hub, "key", "alpha")
	applicationTestSubscription(t, hub, "key", "beta")
	if _, err := hub.Subscribe(Scope{ApplicationKey: true, CredentialID: "key", SessionID: "third"}); !errors.Is(err, ErrCredentialConnectionLimit) {
		t.Errorf("application parent connection limit = %v", err)
	}
	applicationTestSubscription(t, hub, "other-key", "other-client")
	subscribeTestScope(t, hub, "key", "ordinary", "")
	if hub.CountForUser("") != 0 || hub.CountForUser("key") != 1 {
		t.Error("application clients entered ordinary user connection counts")
	}
	_ = alpha.Close()
	applicationTestSubscription(t, hub, "key", "replacement")
}

func TestApplicationPublicationAuthorityRemainsImmutableAndOffWire(t *testing.T) {
	hub := newTestHub(t, Options{})
	sub := applicationTestSubscription(t, hub, "target-key", "target-client")
	want := Authority{CredentialID: "sender-key", ClientSessionID: "sender-client", ApplicationKeyID: 17}
	envelope := Envelope{MessageType: "GeneralCommand", Data: json.RawMessage(`{"Name":"VolumeUp"}`), Authority: want}
	if count, err := hub.PublishScope(sub.Scope(), envelope); err != nil || count != 1 {
		t.Fatalf("publish = (%d, %v)", count, err)
	}
	envelope.Authority.CredentialID = "replacement"
	event := nextTestEvent(t, sub)
	if event.Authority() != want {
		t.Error("queued authority changed after publication")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Bytes(), &fields); err != nil || len(fields) != 3 {
		t.Fatal("internal authority entered the wire envelope")
	}
	for _, invalid := range []Authority{
		{CredentialID: "sender-key", ClientSessionID: "sender-client"},
		{ApplicationKeyID: 17, ClientSessionID: "sender-client"},
		{ApplicationKeyID: 17, CredentialID: "sender-key"},
	} {
		if _, err := hub.PublishScope(sub.Scope(), Envelope{MessageType: "Changed", Authority: invalid}); !errors.Is(err, ErrInvalidEvent) {
			t.Errorf("invalid authority accepted: %v", err)
		}
	}
}
