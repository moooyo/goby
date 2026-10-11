//go:build linux

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPClientSessionSummaryConsumersKeepCapabilityAndAuthorityBoundaries(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	controller, err := f.users.Resolve(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := f.users.Resolve(f.ctx, accounts.second.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	const omittedMalformed = `{"PlayableMediaTypes":["Audio"],"SupportedCommands":["SetVolume"],"SupportsMediaControl":true,"DeviceProfile":7,"SupportsSync":[]}`
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET client_capabilities=$2::jsonb WHERE id=$1`, receiver.SessionID, omittedMalformed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.ListClientSessions(f.ctx, controller, identity.ClientSessionFilter{SessionID: receiver.SessionID}); err == nil {
		t.Fatal("complete capability read accepted malformed omitted fields")
	}
	session := clientSessionHTTPOne(t, f, accounts.viewer, receiver.SessionID)
	if !reflect.DeepEqual(session["PlayableMediaTypes"], []any{"Audio"}) || !reflect.DeepEqual(session["SupportedCommands"], []any{"SetVolume"}) {
		t.Fatal("HTTP session summary lost its consumed capabilities")
	}
	assertClientSessionHTTPPrivate(t, session)
	payload, err := f.app.socketSessionPayload(f.ctx, controller)
	if err != nil {
		t.Fatal(err)
	}
	var envelope events.Envelope
	var sessions []map[string]any
	if json.Unmarshal(payload, &envelope) != nil || envelope.MessageType != "Sessions" || json.Unmarshal(envelope.Data, &sessions) != nil {
		t.Fatal("Sessions subscription could not encode the capability summary")
	}
	assertClientSessionHTTPIDs(t, sessions, accounts.viewer.id, accounts.second.id)
	path := "/emby/Sessions/" + receiver.SessionID + "/Command/SetVolume"
	expectRemoteCommandAccepted(t, f, path, map[string]any{"Arguments": map[string]string{"Volume": "37"}}, accounts.viewer.headers)
	queuedEnvelope, err := remoteCommandEnvelope("GeneralCommand", map[string]any{
		"Name": "SetVolume", "Arguments": map[string]string{"Volume": "37"},
	}, controller, identity.ClientSession{SessionID: receiver.SessionID, UserID: receiver.User.ID, Kind: receiver.Kind}, true)
	if err != nil {
		t.Fatal(err)
	}
	queued := remoteCommandQueuedEvent(t, queuedEnvelope, receiver)
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, queued); err != nil || !allowed {
		t.Fatalf("delivery failed because of an unused capability field: allowed=%t error=%v", allowed, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET client_capabilities=jsonb_set(client_capabilities,'{SupportsMediaControl}','"invalid"'::jsonb) WHERE id=$1`, receiver.SessionID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions?Id="+receiver.SessionID, nil, accounts.viewer.headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, path, nil, accounts.viewer.headers), http.StatusBadRequest)
	if _, err := f.app.socketSessionPayload(f.ctx, controller); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("subscription ignored a malformed consumed capability: %v", err)
	}
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, queued); allowed || !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("delivery ignored a malformed consumed capability: allowed=%t error=%v", allowed, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET client_capabilities=$2::jsonb WHERE id=$1`, receiver.SessionID, omittedMalformed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, controller.SessionID); err != nil {
		t.Fatal(err)
	}
	if allowed, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, queued); err != nil || allowed {
		t.Fatalf("delivery reused the controller's earlier authority: allowed=%t error=%v", allowed, err)
	}
}
