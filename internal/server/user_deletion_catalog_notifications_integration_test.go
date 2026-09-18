//go:build linux

package server

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
)

func TestHTTPUserDeletionResynchronizesOnlyChangedCollectionCatalog(t *testing.T) {
	for _, surface := range []string{"native", "emby"} {
		for _, impact := range []struct {
			name   string
			owns   bool
			shared bool
		}{
			{name: "unrelated_collections"},
			{name: "owned_collection", owns: true},
			{name: "shared_collection_only", shared: true},
		} {
			t.Run(surface+"/"+impact.name, func(t *testing.T) {
				f, accounts := newClientSessionHTTPAccounts(t)
				create := func(login clientSessionHTTPLogin, name string) string {
					t.Helper()
					response := f.request(t, http.MethodPost, "/emby/Playlists", map[string]any{"Name": name}, login.headers)
					expectStatus(t, response, http.StatusOK)
					return stringValue(t, jsonObject(t, response), "Id")
				}
				share := func(login clientSessionHTTPLogin, id string, users ...string) {
					t.Helper()
					shares := make([]map[string]any, 0, len(users))
					for _, userID := range users {
						shares = append(shares, map[string]any{"UserId": userID, "CanEdit": false})
					}
					expectStatus(t, f.request(t, http.MethodPost, "/emby/Playlists/"+url.PathEscape(id),
						map[string]any{"Shares": shares}, login.headers), http.StatusOK)
				}
				// Keep both catalog tables populated even when the target has no
				// collection state. Another account's rows cannot require a resync.
				retainedID := create(accounts.other, "Retained peer playlist")
				retainedShares := []string{accounts.admin.userID}
				if impact.shared {
					retainedShares = append(retainedShares, accounts.viewer.userID)
				}
				share(accounts.other, retainedID, retainedShares...)
				assertUserDeletionCatalogRoster(t, f, accounts.other, retainedID, retainedShares...)
				ownedID := ""
				if impact.owns {
					ownedID = create(accounts.viewer, "Deleted owner playlist")
					share(accounts.viewer, ownedID, accounts.other.userID)
					expectStatus(t, f.request(t, http.MethodGet, "/emby/Playlists/"+url.PathEscape(ownedID),
						nil, accounts.other.headers), http.StatusOK)
				}
				var owns, shared bool
				if err := f.pool.QueryRow(f.ctx, `SELECT
					EXISTS (SELECT 1 FROM media_collections WHERE owner_id=$1),
					EXISTS (SELECT 1 FROM media_collection_shares WHERE user_id=$1)`, accounts.viewer.userID).Scan(&owns, &shared); err != nil {
					t.Fatalf("read the target's collection state before deletion: %v", err)
				}
				if owns != impact.owns || shared != impact.shared {
					t.Fatal("the deletion fixture does not isolate its intended collection impact")
				}
				csrf := adminSessionHTTPCSRF(t, f, accounts.cookie)
				managed := managedHTTPDetail(t, f, accounts.cookie, accounts.viewer.userID)
				// Collection setup has already committed its own synchronous resync.
				// Disable periodic authorization so deletion must retire its sockets.
				server := websocketHTTPServer(t, f, time.Hour)
				targetSocket := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
				peerSocket := websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
				websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
				websocketHTTPWaitCount(t, f, accounts.other.id, 1)
				targetSocket.ping(t)
				peerSocket.ping(t)
				observer, err := f.app.eventHub.Subscribe(events.Scope{
					UserID: accounts.other.userID, SessionID: "user-deletion-catalog-observer",
				})
				if err != nil {
					t.Fatalf("subscribe to the committed catalog resync: %v", err)
				}
				t.Cleanup(func() { _ = observer.Close() })
				if surface == "native" {
					response := f.request(t, http.MethodDelete, "/admin/v1/users/"+url.PathEscape(accounts.viewer.userID),
						map[string]any{"Revision": managed["Revision"]}, http.Header{"X-CSRF-Token": {csrf}}, accounts.cookie)
					expectStatus(t, response, http.StatusOK)
					if body := jsonObject(t, response); len(body) != 1 || body["CurrentSessionRevoked"] != false {
						t.Fatal("deleting the target changed the surviving administrator session")
					}
				} else {
					response := f.request(t, http.MethodDelete, "/emby/Users/"+url.PathEscape(accounts.viewer.userID),
						nil, accounts.admin.headers)
					expectStatus(t, response, http.StatusOK)
					if response.Body.Len() != 0 {
						t.Fatal("Emby user deletion returned an unexpected response body")
					}
				}
				targetSocket.closed(t)
				websocketHTTPWaitCount(t, f, accounts.viewer.id, 0)
				for _, login := range []clientSessionHTTPLogin{accounts.viewer, accounts.second} {
					expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusUnauthorized)
				}
				if impact.owns || impact.shared {
					peerSocket.closed(t)
					websocketHTTPWaitCount(t, f, accounts.other.id, 0)
					if !errors.Is(observer.Reason(), events.ErrResyncRequired) {
						t.Fatal("committed collection changes did not require a fresh catalog snapshot")
					}
					// Resynchronization must leave the peer credential usable.
					peerSocket = websocketHTTPDial(t, server, "/emby/socket", accounts.other.headers, http.StatusSwitchingProtocols)
				} else if observer.Reason() != nil {
					t.Fatal("deleting an account without collection state disconnected a peer subscription")
				}
				peerSocket.ping(t)
				websocketHTTPWaitCount(t, f, accounts.other.id, 1)
				expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions", nil, accounts.other.headers), http.StatusOK)
				assertUserDeletionCatalogRoster(t, f, accounts.other, retainedID, accounts.admin.userID)
				var removed, retained bool
				if err := f.pool.QueryRow(f.ctx, `SELECT
					NOT EXISTS (SELECT 1 FROM users WHERE id=$1)
					AND NOT EXISTS (SELECT 1 FROM media_collections WHERE owner_id=$1)
					AND NOT EXISTS (SELECT 1 FROM media_collection_shares WHERE user_id=$1)
					AND NOT EXISTS (SELECT 1 FROM items WHERE id=$2)
					AND NOT EXISTS (SELECT 1 FROM media_collections WHERE item_id=$2)
					AND NOT EXISTS (SELECT 1 FROM media_collection_shares WHERE collection_id=$2),
					EXISTS (SELECT 1 FROM items WHERE id=$3)
					AND EXISTS (SELECT 1 FROM media_collections WHERE item_id=$3 AND owner_id=$4)
					AND EXISTS (SELECT 1 FROM media_collection_shares WHERE collection_id=$3 AND user_id=$5)`,
					accounts.viewer.userID, ownedID, retainedID, accounts.other.userID, accounts.admin.userID).Scan(&removed, &retained); err != nil {
					t.Fatalf("read committed collection deletion and surviving peer state: %v", err)
				}
				if !removed || !retained {
					t.Fatal("user deletion did not remove exactly its owned collection state and sharing entry")
				}
				if ownedID != "" {
					expectStatus(t, f.request(t, http.MethodGet, "/emby/Playlists/"+url.PathEscape(ownedID),
						nil, accounts.admin.headers), http.StatusNotFound)
				}
				peerSocket.ping(t)
				websocketHTTPWaitCount(t, f, accounts.other.id, 1)
			})
		}
	}
}

func assertUserDeletionCatalogRoster(t *testing.T, f *serverFixture, owner clientSessionHTTPLogin, id string, users ...string) {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Playlists/"+url.PathEscape(id), nil, owner.headers)
	expectStatus(t, response, http.StatusOK)
	collection := jsonObject(t, response)
	shares, ok := collection["Shares"].([]any)
	if collection["Id"] != id || collection["OwnerId"] != owner.userID || !ok || len(shares) != len(users) {
		t.Fatal("the retained collection lost its identity, owner, or expected sharing roster")
	}
	seen := make(map[string]bool, len(shares))
	for _, raw := range shares {
		share, ok := raw.(map[string]any)
		if !ok || share["CanEdit"] != false {
			t.Fatal("the retained collection changed a sharing permission")
		}
		userID := stringValue(t, share, "UserId")
		if seen[userID] {
			t.Fatal("the retained collection duplicated a sharing recipient")
		}
		seen[userID] = true
	}
	for _, userID := range users {
		if !seen[userID] {
			t.Fatal("the retained collection lost an expected sharing recipient")
		}
	}
}
