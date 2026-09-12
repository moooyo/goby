//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func TestHTTPLibraryNotifierPublishesOnlyCommittedMetadataAndClosesWithServer(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	const libraryID, itemID = "notifier-library", "notifier-movie"
	if f.app.catalogNotifier == nil || f.app.catalogNotifier.store != f.app.library {
		t.Fatal("Server.New did not attach its catalog listener to the owned store")
	}
	if _, err := f.pool.Exec(f.ctx, "INSERT INTO libraries (id, name, collection_type) VALUES ($1, $1, 'movies')", libraryID); err != nil {
		t.Fatal("create catalog notification library fixture")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items (id, library_id, name, sort_name, type)
		VALUES ($1, $2, 'Automatic Name', 'Automatic Name', 'Movie')`, itemID, libraryID); err != nil {
		t.Fatal("create metadata notification item fixture")
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, libraryID)
	setLibraryChangedFolders(t, f, accounts.other.userID)
	cookie, csrf := f.adminLogin(t)
	path := "/admin/v1/items/" + itemID + "/metadata"
	detailResponse := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, detailResponse, http.StatusOK)
	revision := adminMetadataHTTPRevision(t, jsonObject(t, detailResponse))
	server := websocketHTTPServer(t, f, time.Hour)
	first := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	second := websocketHTTPDial(t, server, "/emby/socket", accounts.second.headers, http.StatusSwitchingProtocols)
	other := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	websocketHTTPWaitCount(t, f, accounts.second.id, 1)
	websocketHTTPWaitCount(t, f, accounts.other.id, 1)
	observer, err := f.app.eventHub.Subscribe(events.Scope{UserID: accounts.viewer.userID, SessionID: "catalog-commit-observer"})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	beforeCounts := [3]int{}
	readCounts := func() [3]int {
		t.Helper()
		var counts [3]int
		if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM user_item_data),
			(SELECT count(*) FROM play_sessions), (SELECT count(*) FROM encoding_jobs)`).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
			t.Fatal("read metadata notification playback counts")
		}
		return counts
	}
	beforeCounts = readCounts()
	edit := func(revision, name string) map[string]any {
		return map[string]any{"Revision": revision, "Overrides": map[string]any{"Name": name}, "LockedFields": []string{}}
	}
	response := f.request(t, http.MethodPut, path, edit(revision, "Committed Name"), http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusOK)
	updated := jsonObject(t, response)
	newRevision := adminMetadataHTTPRevision(t, updated)
	if newRevision == revision || objectValue(t, updated, "Effective")["Name"] != "Committed Name" {
		t.Fatal("metadata update did not commit its new value and revision")
	}
	readCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	published, err := observer.Next(readCtx)
	cancel()
	if err != nil || published.MessageType() != "LibraryChanged" || published.MessageID() == "" {
		t.Fatalf("committed metadata did not reach the automatically registered listener (%T)", err)
	}
	scopes := published.CatalogScopes()
	if len(scopes) != 1 || scopes[0] != (events.CatalogScope{ItemID: itemID, LibraryID: libraryID}) {
		t.Fatal("the post-commit metadata event lacks its exact trusted catalog scope")
	}
	var persisted string
	if err := f.pool.QueryRow(f.ctx, "SELECT name FROM items WHERE id = $1", itemID).Scan(&persisted); err != nil || persisted != "Committed Name" {
		t.Fatal("notification arrived without the independently visible committed metadata")
	}
	for _, client := range []*websocketHTTPClient{first, second} {
		assertLibraryChangedPayload(t, client.next(t), published.MessageID(), map[string][]string{"ItemsUpdated": {itemID}})
	}
	other.ping(t)
	other.quiet(t)
	// A stale transaction rolls back and a no-op edit does not change metadata.
	// Neither is a committed catalog change that can produce another publication.
	expectStatus(t, f.request(t, http.MethodPut, path, edit(revision, "Rejected Name"),
		http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusConflict)
	noop := f.request(t, http.MethodPut, path, edit(newRevision, "Committed Name"),
		http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, noop, http.StatusOK)
	if adminMetadataHTTPRevision(t, jsonObject(t, noop)) != newRevision {
		t.Fatal("identical metadata edit changed the catalog revision")
	}
	quietCtx, quietCancel := context.WithTimeout(f.ctx, 100*time.Millisecond)
	_, err = observer.Next(quietCtx)
	quietCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("no-op or rejected metadata edit emitted a publication (%T)", err)
	}
	first.quiet(t)
	second.quiet(t)
	other.quiet(t)
	if readCounts() != beforeCounts {
		t.Error("catalog notification delivery created playback or user-data state")
	}
	// A callback captured just before detach must remain safe after shutdown.
	captured := f.app.catalogNotifier.Enqueue
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := f.app.Close(closeCtx); err != nil {
		t.Fatalf("close server and catalog notifier (%T)", err)
	}
	captured(library.CatalogNotification{Resync: true})
	first.closed(t)
	second.closed(t)
	other.closed(t)
	if _, err := f.app.eventHub.Subscribe(events.Scope{UserID: accounts.viewer.userID, SessionID: "after-server-close"}); !errors.Is(err, events.ErrClosed) {
		t.Fatal("server close did not finish the Hub lifetime")
	}
	var public map[string]json.RawMessage
	if err := json.Unmarshal(published.Bytes(), &public); err != nil || public["CatalogScopes"] != nil {
		t.Fatal("trusted commit scope appeared in the encoded publication")
	}
}
