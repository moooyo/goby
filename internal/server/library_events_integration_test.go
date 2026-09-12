//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func libraryChangedEnvelope(t *testing.T, messageID string, data any, scopes []events.CatalogScope) events.Envelope {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return events.Envelope{MessageType: "LibraryChanged", MessageID: messageID, Data: raw, CatalogScopes: scopes}
}

func assertLibraryChangedPayload(t *testing.T, payload []byte, messageID string, expected map[string][]string) {
	t.Helper()
	var envelope events.Envelope
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.MessageType != "LibraryChanged" || envelope.MessageID != messageID {
		t.Fatalf("catalog publication lost its envelope identity (%T)", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &fields); err != nil || len(fields) != 7 || string(fields["IsEmpty"]) != "false" {
		t.Fatal("catalog publication must expose exactly the seven known fields and a nonempty projection")
	}
	for _, name := range []string{"FoldersAddedTo", "FoldersRemovedFrom", "ItemsAdded", "ItemsRemoved", "ItemsUpdated", "CollectionFolders"} {
		var ids []string
		if err := json.Unmarshal(fields[name], &ids); err != nil || ids == nil || !slices.Equal(ids, expected[name]) {
			t.Errorf("catalog field %s = %v, want ordered IDs %v with a JSON array", name, ids, expected[name])
		}
	}
	if bytes.Contains(payload, []byte("CatalogScopes")) || bytes.Contains(payload, []byte("LibraryID")) ||
		bytes.Contains(payload, []byte("private-scope-only-item")) || bytes.Contains(payload, []byte("unscoped-secret")) {
		t.Error("catalog publication exposed private scope metadata or an unknown data field")
	}
}

func setLibraryChangedFolders(t *testing.T, f *serverFixture, userID string, folders ...string) {
	t.Helper()
	if folders == nil {
		folders = []string{}
	}
	raw, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": folders})
	if err != nil {
		t.Fatal(err)
	}
	setHTTPUserPolicy(t, f, userID, string(raw))
}

func TestHTTPWebSocketLibraryChangedFiltersBroadcastsWithoutChangingPublication(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, id := range []string{"catalog-a", "catalog-b"} {
		if _, err := f.pool.Exec(f.ctx, "INSERT INTO libraries (id, name, collection_type) VALUES ($1, $1, 'movies')", id); err != nil {
			t.Fatal("create catalog broadcast library")
		}
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "catalog-a")
	setLibraryChangedFolders(t, f, accounts.other.userID, "catalog-b")
	server := websocketHTTPServer(t, f, time.Hour)
	first := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	websocketHTTPWaitCount(t, f, accounts.other.id, 1)
	data := map[string]any{
		"FoldersAddedTo": []string{"folder-a", "folder-b"}, "FoldersRemovedFrom": []string{"folder-a"},
		"ItemsAdded": []string{"item-a", "item-b", "item-a", "no-scope"}, "ItemsRemoved": []string{"removed-a", "removed-b"},
		"ItemsUpdated": []string{"moved", "item-b", "item-a", "item-b"}, "CollectionFolders": []string{"catalog-a", "catalog-b"},
		"IsEmpty": true, "Unknown": "unscoped-secret", "CatalogScopes": []map[string]string{{"ItemID": "no-scope", "LibraryID": "catalog-a"}},
	}
	scopes := []events.CatalogScope{
		{ItemID: "folder-a", LibraryID: "catalog-a"}, {ItemID: "folder-b", LibraryID: "catalog-b"},
		{ItemID: "item-a", LibraryID: "catalog-a"}, {ItemID: "item-b", LibraryID: "catalog-b"},
		{ItemID: "removed-a", LibraryID: "catalog-a"}, {ItemID: "removed-b", LibraryID: "catalog-b"},
		{ItemID: "catalog-a", LibraryID: "catalog-a"}, {ItemID: "catalog-b", LibraryID: "catalog-b"},
		{ItemID: "moved", LibraryID: "catalog-a"}, {ItemID: "moved", LibraryID: "catalog-b"},
		{ItemID: "private-scope-only-item", LibraryID: "catalog-a"},
	}
	envelope := libraryChangedEnvelope(t, "shared-catalog-publication", data, scopes)
	if count, err := f.app.eventHub.PublishAll(envelope); err != nil || count != 2 {
		t.Fatalf("broadcast catalog notification to both users: count=%d error=%T", count, err)
	}
	assertLibraryChangedPayload(t, first.next(t), envelope.MessageID, map[string][]string{
		"FoldersAddedTo": {"folder-a"}, "FoldersRemovedFrom": {"folder-a"}, "ItemsAdded": {"item-a", "item-a"},
		"ItemsRemoved": {"removed-a"}, "ItemsUpdated": {"moved", "item-a"}, "CollectionFolders": {"catalog-a"},
	})
	assertLibraryChangedPayload(t, second.next(t), envelope.MessageID, map[string][]string{
		"FoldersAddedTo": {"folder-b"}, "ItemsAdded": {"item-b"}, "ItemsRemoved": {"removed-b"},
		"ItemsUpdated": {"moved", "item-b", "item-b"}, "CollectionFolders": {"catalog-b"},
	})
	// A JSON scope claim is inert even when its claimed library is authorized.
	if _, err := f.app.eventHub.PublishAll(libraryChangedEnvelope(t, "untrusted-only", map[string]any{
		"ItemsAdded": []string{"no-scope"}, "CatalogScopes": scopes,
	}, nil)); err != nil {
		t.Fatal(err)
	}
	first.ping(t)
	second.ping(t)
	first.quiet(t)
	second.quiet(t)
}

func TestSocketLibraryChangedRechecksPolicyAndRetainsDeletedLibraryScopes(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, id := range []string{"historical-catalog-a", "historical-catalog-b"} {
		if _, err := f.pool.Exec(f.ctx, "INSERT INTO libraries (id, name, collection_type) VALUES ($1, $1, 'movies')", id); err != nil {
			t.Fatal("create historical catalog fixture")
		}
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items (id, library_id, name, sort_name, type)
		VALUES ('removed-item', 'historical-catalog-a', 'Removed', 'Removed', 'Movie')`); err != nil {
		t.Fatal("create historical item fixture")
	}
	principal, err := f.users.ResolveEmby(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal("resolve publication recipient")
	}
	envelope := libraryChangedEnvelope(t, "queued-before-policy-change", map[string]any{
		"ItemsRemoved": []string{"removed-item", "moved-item", "removed-item"}, "CollectionFolders": []string{"historical-catalog-a"},
	}, []events.CatalogScope{
		{ItemID: "removed-item", LibraryID: "historical-catalog-a"},
		{ItemID: "historical-catalog-a", LibraryID: "historical-catalog-a"},
		{ItemID: "moved-item", LibraryID: "historical-catalog-b"}, {ItemID: "moved-item", LibraryID: "historical-catalog-a"},
		{ItemID: "moved-item", LibraryID: "historical-catalog-a"},
	})
	event := websocketHTTPPublishedEvent(t, envelope, accounts.viewer.userID)
	original := event.Bytes()
	// The principal was resolved and the event published before this restriction.
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "historical-catalog-a")
	if _, err := f.pool.Exec(f.ctx, "DELETE FROM libraries WHERE id = 'historical-catalog-a'"); err != nil {
		t.Fatal("remove the historical library and item")
	}
	var exists bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id = 'removed-item')
		OR EXISTS(SELECT 1 FROM libraries WHERE id = 'historical-catalog-a')`).Scan(&exists); err != nil || exists {
		t.Fatal("historical fixture still exists after deletion")
	}
	payload, err := f.app.socketEventPayload(f.ctx, principal, event)
	if err != nil {
		t.Fatalf("authorize deleted catalog scope (%T)", err)
	}
	assertLibraryChangedPayload(t, payload, envelope.MessageID, map[string][]string{
		"ItemsRemoved": {"removed-item", "moved-item", "removed-item"}, "CollectionFolders": {"historical-catalog-a"},
	})
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "historical-catalog-a", "historical-catalog-b")
	payload, err = f.app.socketEventPayload(f.ctx, principal, event)
	if err != nil {
		t.Fatal(err)
	}
	assertLibraryChangedPayload(t, payload, envelope.MessageID, map[string][]string{
		"ItemsRemoved": {"removed-item", "moved-item", "removed-item"}, "CollectionFolders": {"historical-catalog-a"},
	})
	setLibraryChangedFolders(t, f, accounts.viewer.userID)
	if payload, err := f.app.socketEventPayload(f.ctx, principal, event); err != nil || len(payload) != 0 {
		t.Errorf("revoked library policy produced a frame or stale authority: bytes=%d error=%T", len(payload), err)
	}
	if !bytes.Equal(event.Bytes(), original) || !slices.Equal(event.CatalogScopes(), envelope.CatalogScopes) {
		t.Error("filtering changed the shared immutable publication")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", accounts.viewer.userID); err != nil {
		t.Fatal("disable catalog notification recipient")
	}
	if payload, err := f.app.socketEventPayload(f.ctx, principal, event); !errors.Is(err, library.ErrForbidden) || len(payload) != 0 {
		t.Errorf("disabled recipient reused its former catalog policy: bytes=%d error=%T", len(payload), err)
	}
}

func TestSocketLibraryChangedRejectsUnscopedAndMalformedPublications(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	principal, err := f.users.ResolveEmby(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal("resolve catalog recipient")
	}
	for _, scopes := range [][]events.CatalogScope{nil, {}, {{ItemID: "other-item", LibraryID: "catalog-a"}}} {
		event := websocketHTTPPublishedEvent(t, libraryChangedEnvelope(t, "missing-scope", map[string]any{
			"ItemsUpdated": []string{"item-a"}, "LibraryId": "catalog-a", "UserId": accounts.viewer.userID,
		}, scopes), accounts.viewer.userID)
		if payload, err := f.app.socketEventPayload(f.ctx, principal, event); err != nil || len(payload) != 0 {
			t.Errorf("unscoped ID must not receive default authority: bytes=%d error=%T", len(payload), err)
		}
	}
	for _, scope := range []events.CatalogScope{
		{ItemID: "item-a", LibraryID: "catalog\nother"}, {ItemID: "item\tother", LibraryID: "catalog-a"},
	} {
		event := websocketHTTPPublishedEvent(t, libraryChangedEnvelope(t, "invalid-private-id", map[string]any{
			"ItemsUpdated": []string{"item-a"},
		}, []events.CatalogScope{scope}), accounts.viewer.userID)
		if payload, err := f.app.socketEventPayload(f.ctx, principal, event); !errors.Is(err, events.ErrInvalidEvent) || len(payload) != 0 {
			t.Errorf("malformed trusted scope must fail closed: bytes=%d error=%T", len(payload), err)
		}
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM user_item_data").Scan(&count); err != nil || count != 0 {
		t.Fatal("catalog notification reads created user data")
	}
}

func TestHTTPWebSocketLibraryChangedUsesIndependentApplicationCredential(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	keys := applicationMediaIssueKeys(t, f, accounts.admin.headers.Get("X-Emby-Token"))
	server := websocketHTTPServer(t, f, time.Hour)
	first := websocketHTTPDial(t, server, "/emby/socket", keys[0].headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", keys[1].headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, keys[0].principal.ClientSessionID, 1)
	websocketHTTPWaitCount(t, f, keys[1].principal.ClientSessionID, 1)
	// Key authority does not come from the creator's current account or policy.
	setLibraryChangedFolders(t, f, accounts.admin.userID)
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_disabled = true WHERE id = $1", accounts.admin.userID); err != nil {
		t.Fatal("disable the creator independently of the application keys")
	}
	envelope := libraryChangedEnvelope(t, "application-catalog-event", map[string]any{"ItemsRemoved": []string{"deleted-item"}},
		[]events.CatalogScope{{ItemID: "deleted-item", LibraryID: "deleted-library"}})
	queued := websocketHTTPPublishedEvent(t, envelope, accounts.viewer.userID)
	if _, err := f.app.eventHub.PublishAll(envelope); err != nil {
		t.Fatal(err)
	}
	assertLibraryChangedPayload(t, first.next(t), envelope.MessageID, map[string][]string{"ItemsRemoved": {"deleted-item"}})
	assertLibraryChangedPayload(t, second.next(t), envelope.MessageID, map[string][]string{"ItemsRemoved": {"deleted-item"}})
	if _, err := f.users.RevokeApplicationKey(f.ctx, keys[1].principal, keys[0].key.ID); err != nil {
		t.Fatalf("revoke one independent credential (%T)", err)
	}
	if payload, err := f.app.socketEventPayload(f.ctx, keys[0].principal, queued); !errors.Is(err, library.ErrForbidden) || len(payload) != 0 {
		t.Errorf("a stale key principal consumed its queued event: bytes=%d error=%T", len(payload), err)
	}
	empty := websocketHTTPPublishedEvent(t, libraryChangedEnvelope(t, "empty-after-revocation", map[string]any{"ItemsAdded": []string{}}, nil), accounts.viewer.userID)
	if payload, err := f.app.socketEventPayload(f.ctx, keys[0].principal, empty); !errors.Is(err, library.ErrForbidden) || len(payload) != 0 {
		t.Errorf("empty scope bypassed current credential validation: bytes=%d error=%T", len(payload), err)
	}
	// Maintenance is an hour away. Sending a new event must itself revalidate the
	// old socket credential and close it before any queued payload is written.
	envelope.MessageID = "after-credential-revocation"
	if _, err := f.app.eventHub.PublishAll(envelope); err != nil {
		t.Fatal(err)
	}
	first.closed(t)
	select {
	case <-first.messages:
		t.Error("revoked application socket received an additional catalog frame")
	default:
	}
	assertLibraryChangedPayload(t, second.next(t), envelope.MessageID, map[string][]string{"ItemsRemoved": {"deleted-item"}})
}

func TestLibraryChangedDataBoundsAndKnownFieldProjection(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{"ItemsAdded":null}`, `{"ItemsAdded":[1]}`, `{"ItemsAdded":[""]}`,
		`{"ItemsAdded":[" leading"]}`, `{"ItemsAdded":["trailing "]}`, `{"ItemsAdded":["a\u0000b"]}`,
		`{"ItemsAdded":["a\u0085b"]}`, `{"ItemsAdded":{},"IsEmpty":false}`, `{"IsEmpty":null}`, `{"IsEmpty":1}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseLibraryChangedData(json.RawMessage(raw)); !errors.Is(err, events.ErrInvalidEvent) {
				t.Errorf("invalid catalog data was accepted: %T", err)
			}
		})
	}
	for _, id := range []string{strings.Repeat("x", 257), strings.Repeat("界", 86)} {
		raw, _ := json.Marshal(map[string]any{"ItemsAdded": []string{id}})
		if _, err := parseLibraryChangedData(raw); !errors.Is(err, events.ErrInvalidEvent) {
			t.Error("the identifier byte bound was not enforced")
		}
	}
	ids := make([]string, maxLibraryChangedIDs)
	for index := range ids {
		ids[index] = "repeated-item"
	}
	raw, _ := json.Marshal(map[string]any{"ItemsAdded": ids, "Unknown": "unscoped-secret", "IsEmpty": false})
	data, err := parseLibraryChangedData(raw)
	if err != nil || !slices.Equal(data.ItemsAdded, ids) || data.IsEmpty {
		t.Fatalf("bounded repeated IDs must retain their order and multiplicity: %T", err)
	}
	raw, _ = json.Marshal(map[string]any{"ItemsAdded": ids, "ItemsRemoved": []string{"one-too-many"}})
	if _, err := parseLibraryChangedData(raw); !errors.Is(err, events.ErrInvalidEvent) {
		t.Error("the aggregate ID count was only enforced per field")
	}
	raw, _ = json.Marshal(map[string]any{"ItemsAdded": []string{"visible"}, "Unknown": strings.Repeat("x", events.DefaultMaxMessageBytes)})
	if _, err := parseLibraryChangedData(raw); !errors.Is(err, events.ErrInvalidEvent) {
		t.Error("unknown fields bypassed the total byte bound")
	}
	data, err = parseLibraryChangedData(json.RawMessage(`{"itemsadded":["unscoped-secret"],"IsEmpty":false}`))
	if err != nil || !data.IsEmpty || len(data.ItemsAdded) != 0 {
		t.Error("an unknown case variant was treated as a known catalog field")
	}
}
