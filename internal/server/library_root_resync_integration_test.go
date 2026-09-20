//go:build linux

package server

import (
	"bytes"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
)

func TestSocketLibraryRootRemovalResyncUsesOnlyTrustedCurrentLibraryScope(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, id := range []string{"removed-library", "surviving-library"} {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES($1,$1,'movies')`, id); err != nil {
			t.Fatal(err)
		}
		createLibraryChangedItem(t, f, id, id, "CollectionFolder")
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "removed-library")
	principal, err := f.users.ResolveEmby(f.ctx, accounts.viewer.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal(err)
	}
	envelope := libraryChangedEnvelope(t, "committed-root-removal", map[string]any{"ItemsRemoved": []string{"removed-library"}},
		[]events.CatalogScope{{ItemID: "removed-library", LibraryID: "removed-library"}})
	event := websocketHTTPPublishedEvent(t, envelope, accounts.viewer.userID)
	original := event.Bytes()
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM libraries WHERE id='removed-library'`); err != nil {
		t.Fatal(err)
	}
	payload, err := f.app.socketEventPayload(f.ctx, principal, event)
	if !errors.Is(err, events.ErrResyncRequired) || len(payload) != 0 {
		t.Fatalf("allowed removed scope did not require a private resync: bytes=%d error=%T", len(payload), err)
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "surviving-library")
	payload, err = f.app.socketEventPayload(f.ctx, principal, event)
	if err != nil || len(payload) != 0 {
		t.Fatalf("current denied scope affected receiver: bytes=%d error=%T", len(payload), err)
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "removed-library")
	untrusted := websocketHTTPPublishedEvent(t, libraryChangedEnvelope(t, "untrusted-root-claim", map[string]any{
		"ItemsRemoved": []string{"removed-library"}, "CatalogScopes": envelope.CatalogScopes,
	}, nil), accounts.viewer.userID)
	if payload, err := f.app.socketEventPayload(f.ctx, principal, untrusted); err != nil || len(payload) != 0 {
		t.Fatal("wire data invented a trusted root-removal scope")
	}
	ordinary := websocketHTTPPublishedEvent(t, libraryChangedEnvelope(t, "ordinary-filtered-removal", map[string]any{
		"ItemsRemoved": []string{"unknown-leaf"},
	}, []events.CatalogScope{{ItemID: "unknown-leaf", LibraryID: "removed-library"}}), accounts.viewer.userID)
	if payload, err := f.app.socketEventPayload(f.ctx, principal, ordinary); err != nil || len(payload) != 0 {
		t.Fatal("an ordinary filtered removal was promoted to root resync")
	}
	if !bytes.Equal(original, event.Bytes()) {
		t.Fatal("one recipient changed shared removal publication bytes")
	}
}

func TestHTTPWebSocketLibraryDeletionClosesOnlyAllowedReceiverAndRefreshesAfterReconnect(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, id := range []string{"delete-scope-a", "retain-scope-b"} {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES($1,$1,'movies')`, id); err != nil {
			t.Fatal(err)
		}
		createLibraryChangedItem(t, f, id, id, "CollectionFolder")
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "delete-scope-a")
	setLibraryChangedFolders(t, f, accounts.other.userID, "retain-scope-b")
	server := websocketHTTPServer(t, f, time.Hour)
	viewer := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	other := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	websocketHTTPWaitCount(t, f, accounts.other.id, 1)
	_, before := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Views?EnableImages=false", nil, accounts.viewer.headers))
	if before != 1 {
		t.Fatal("fixture viewer did not see its library")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Library/VirtualFolders/Delete", map[string]any{"Id": "delete-scope-a", "RefreshLibrary": false}, accounts.admin.headers), http.StatusOK)
	viewer.closed(t)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
	select {
	case <-viewer.messages:
		t.Fatal("root removal exposed historical identifiers before closure")
	default:
	}
	other.ping(t)
	other.quiet(t)
	reconnected := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	reconnected.ping(t)
	reconnected.quiet(t)
	_, after := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Views?EnableImages=false", nil, accounts.viewer.headers))
	if after != 0 {
		t.Fatal("HTTP refresh retained the deleted library")
	}
	sendSessionSubscription(t, reconnected, "SessionsStart", "0,60000")
	assertClientSessionHTTPIDs(t, sessionSubscriptionSnapshot(t, reconnected), accounts.viewer.id, accounts.second.id)
	websocketHTTPPost(t, server, "/emby/Sessions/Logout", accounts.viewer.headers, http.StatusNoContent)
	reconnected.closed(t)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
	websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusUnauthorized)
	other.ping(t)
	other.quiet(t)
}
