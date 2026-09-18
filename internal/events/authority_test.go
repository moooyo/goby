package events

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOrdinaryPublicationAuthorityRemainsImmutableAndOffWire(t *testing.T) {
	hub := newTestHub(t, Options{})
	sub := subscribeTestScope(t, hub, "target-user", "target-session", "target-device")
	want := Authority{Kind: "emby", UserID: "sender-user", SessionID: "sender-session", PeerIP: "203.0.113.17"}
	envelope := Envelope{MessageType: "GeneralCommand", Data: json.RawMessage(`{"Name":"VolumeUp"}`), Authority: want}
	if count, err := hub.PublishScope(sub.Scope(), envelope); err != nil || count != 1 {
		t.Fatalf("publish ordinary authority = (%d, %v)", count, err)
	}
	envelope.Authority.UserID = "replacement-user"
	envelope.Authority.SessionID = "replacement-session"
	envelope.Authority.PeerIP = "127.0.0.1"
	event := nextTestEvent(t, sub)
	if event.Authority() != want {
		t.Fatal("queued ordinary authority changed after publication")
	}
	copy := event.Authority()
	copy.SessionID = "another-session"
	if event.Authority() != want {
		t.Fatal("reading ordinary authority exposed mutable queue state")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Bytes(), &fields); err != nil || len(fields) != 3 ||
		fields["MessageType"] == nil || fields["MessageId"] == nil || fields["Data"] == nil {
		t.Fatal("ordinary authority entered the wire envelope")
	}
	if data, err := json.Marshal(want); err != nil || string(data) != "{}" {
		t.Fatal("ordinary publication authority can be serialized")
	}
	want.PeerIP = ""
	if _, err := hub.PublishScope(sub.Scope(), Envelope{MessageType: "Changed", Authority: want}); err != nil {
		t.Fatalf("an absent trusted peer must retain the conservative remote-access context: %v", err)
	}
}

func TestPublicationAuthorityRejectsMixedAndIncompleteIdentities(t *testing.T) {
	hub := newTestHub(t, Options{})
	sub := subscribeTestScope(t, hub, "target-user", "target-session", "target-device")
	for _, invalid := range []Authority{
		{UserID: "sender-user", SessionID: "sender-session"},
		{Kind: "admin", UserID: "sender-user", SessionID: "sender-session"},
		{Kind: "emby", UserID: "sender-user"},
		{Kind: "emby", SessionID: "sender-session"},
		{Kind: "emby", UserID: "sender-user", SessionID: "sender-session", PeerIP: strings.Repeat("x", maxScopeFieldBytes+1)},
		{Kind: "emby", UserID: "sender-user", SessionID: "sender-session", CredentialID: "sender-key"},
		{Kind: "emby", UserID: "sender-user", SessionID: "sender-session", ClientSessionID: "sender-client"},
		{Kind: "emby", UserID: "sender-user", SessionID: "sender-session", ApplicationKeyID: 17},
		{Kind: "emby", CredentialID: "sender-key", ClientSessionID: "sender-client", ApplicationKeyID: 17},
		{UserID: "sender-user", CredentialID: "sender-key", ClientSessionID: "sender-client", ApplicationKeyID: 17},
		{SessionID: "sender-session", CredentialID: "sender-key", ClientSessionID: "sender-client", ApplicationKeyID: 17},
		{PeerIP: "203.0.113.17", CredentialID: "sender-key", ClientSessionID: "sender-client", ApplicationKeyID: 17},
	} {
		if _, err := hub.PublishScope(sub.Scope(), Envelope{MessageType: "Changed", Authority: invalid}); !errors.Is(err, ErrInvalidEvent) {
			t.Errorf("mixed or incomplete publication authority accepted: %v", err)
		}
	}
}
