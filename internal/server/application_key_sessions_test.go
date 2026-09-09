package server

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

func TestApplicationClientDTOAndCommandUseRealUserlessContext(t *testing.T) {
	principal := identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 7,
		SessionID: "parent-credential", ClientSessionID: "alpha-context",
		Client: identity.Client{Name: "Alpha", DeviceID: "alpha-device", Device: "Alpha Device", Version: "9.8.7"}}
	if clientSessionID(principal) != "alpha-context" {
		t.Fatal("application wire session collapsed to its parent credential")
	}
	app := &Server{serverID: "server"}
	target := identity.ClientSession{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 7,
		CredentialID: principal.SessionID, SessionID: principal.ClientSessionID, Client: principal.Client}
	dto := app.clientSessionDTO(target)
	if dto["Id"] != "alpha-context" || dto["Client"] != "Alpha" || dto["DeviceId"] != "alpha-device" || dto["ApplicationVersion"] != "9.8.7" {
		t.Fatal("application client projection changed its actual metadata")
	}
	for _, field := range []string{"UserId", "UserName", "ExpiresAt", "CredentialId", "ApplicationKeyId", "AccessToken"} {
		if _, found := dto[field]; found {
			t.Errorf("application client exposes %s", field)
		}
	}
	envelope, err := remoteCommandEnvelope("Playstate", map[string]any{
		"Command": "Pause", "ControllingUserId": "forged-user", "Id": "forged-session",
	}, principal, target, true)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data["Id"] != "alpha-context" {
		t.Fatal("remote command did not use the actual target context")
	}
	if _, found := data["ControllingUserId"]; found {
		t.Fatal("application command fabricated a controlling user")
	}
	if envelope.Authority != (events.Authority{CredentialID: "parent-credential", ClientSessionID: "alpha-context", ApplicationKeyID: 7}) {
		t.Fatal("application command omitted its internal revalidation authority")
	}
	if got := targetEventScope(target); got != clientEventScope(principal) {
		t.Fatal("application sender and receiver used different scope types")
	}
}
