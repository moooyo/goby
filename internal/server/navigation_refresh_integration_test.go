//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/media"
)

// This is an HTTP/WebSocket consumer journey. It issues the next read only in
// response to an incoming event; it does not simulate an original Emby UI.
func TestHTTPWebSocketRefreshesGlobalNextUpAfterPlaybackAndMetadataCommit(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	for _, name := range []string{"refresh/Refresh Show/Season 01/Refresh.Show.S01E01.mp4", "refresh/Refresh Show/Season 01/Refresh.Show.S01E02.mp4", "refresh/Refresh Show/Season 02/Refresh.Show.S02E01.mp4"} {
		writeAPIMediaFile(t, p.s.root, name)
	}
	collection, err := f.app.library.CreateLibrary(f.ctx, "Refresh television", "tvshows", []string{filepath.Join(p.s.root, "refresh")})
	if err != nil {
		t.Fatal(err)
	}
	p.s.rescan(t, collection.ID)
	p.s.setPolicy(t, p.s.viewerID, true, []string{collection.ID})
	series, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Items?ParentId="+collection.ID+"&Recursive=true&IncludeItemTypes=Series", "", p.headers, nil))
	if total != 1 || len(series) != 1 {
		t.Fatal("refresh fixture needs one series")
	}
	seriesID := stringValue(t, series[0], "Id")
	episodes, total := responseItems(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Shows/"+seriesID+"/Episodes", "", p.headers, nil))
	if total != 3 || len(episodes) != 3 {
		t.Fatal("refresh fixture needs three ordered episodes")
	}
	ids := []string{stringValue(t, episodes[0], "Id"), stringValue(t, episodes[1], "Id"), stringValue(t, episodes[2], "Id")}
	read := func() ([]map[string]any, int) {
		t.Helper()
		return responseItems(t, playbackTextRequest(t, p, http.MethodGet, "/emby/Shows/NextUp?UserId="+p.s.viewerID, "", p.headers, nil))
	}
	if items, count := read(); count != 0 || len(items) != 0 {
		t.Fatal("unstarted global NextUp must be empty")
	}
	client := websocketHTTPDial(t, p.s.server, "/emby/socket", p.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, p.authSessionID, 1)
	refresh := func(messageType, expectedID, expectedName string, position int64) {
		t.Helper()
		for attempts := 0; attempts < 16; attempts++ {
			var envelope events.Envelope
			if err := json.Unmarshal(client.next(t), &envelope); err != nil {
				t.Fatal("invalid refresh envelope")
			}
			if envelope.MessageType != messageType {
				continue
			}
			items, count := read()
			if count != 1 || len(items) != 1 || items[0]["Id"] != expectedID {
				continue
			}
			if expectedName != "" && items[0]["Name"] != expectedName {
				continue
			}
			data := objectValue(t, items[0], "UserData")
			if data["PlaybackPositionTicks"] != float64(position) {
				continue
			}
			return
		}
		t.Fatal("events did not cause the expected global NextUp refresh")
	}
	playTo := func(position int64) {
		t.Helper()
		prepared, _ := playbackHTTPSource(t, playbackTextRequest(t, p, http.MethodPost, "/emby/Items/"+ids[1]+"/PlaybackInfo", playbackTextJSON(t, matchingPlaybackHTTPBody()), p.headers, nil))
		playID := stringValue(t, prepared, "PlaySessionId")
		for _, report := range []struct {
			path     string
			position int64
		}{{"/emby/Sessions/Playing", 0}, {"/emby/Sessions/Playing/Progress", position}, {"/emby/Sessions/Playing/Stopped", position}} {
			body := playbackTextJSON(t, map[string]any{"ItemId": ids[1], "MediaSourceId": media.SourceID(ids[1]), "PlaySessionId": playID, "SessionId": p.authSessionID, "PositionTicks": report.position, "CanSeek": true, "PlayMethod": "DirectStream", "EventName": "TimeUpdate"})
			expectStatus(t, playbackTextRequest(t, p, http.MethodPost, report.path, body, p.headers, nil), http.StatusNoContent)
		}
	}
	playTo(120 * media.TicksPerSecond)
	refresh("UserDataChanged", ids[1], "", 120*media.TicksPerSecond)
	playTo(600 * media.TicksPerSecond)
	refresh("UserDataChanged", ids[2], "", 0)
	cookie, csrf := f.adminLogin(t)
	metadataPath := "/admin/v1/items/" + ids[2] + "/metadata"
	detail := f.request(t, http.MethodGet, metadataPath, nil, nil, cookie)
	expectStatus(t, detail, http.StatusOK)
	revision := adminMetadataHTTPRevision(t, jsonObject(t, detail))
	expectStatus(t, f.request(t, http.MethodPut, metadataPath, map[string]any{"Revision": revision, "Overrides": map[string]any{"Name": "Committed next episode"}, "LockedFields": []string{}}, http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusOK)
	refresh("LibraryChanged", ids[2], "Committed next episode", 0)
}

func TestHTTPWebSocketEntityUserStateRefreshAndDeliveryRevocation(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	for _, id := range []string{"entity-refresh-library", "entity-refresh-sentinel-library"} {
		if _, err := f.pool.Exec(f.ctx, "INSERT INTO libraries(id,name,collection_type) VALUES($1,$1,'music')", id); err != nil {
			t.Fatal(err)
		}
	}
	createLibraryChangedItem(t, f, "entity-refresh-audio", "entity-refresh-library", "Audio")
	createLibraryChangedItem(t, f, "entity-refresh-sentinel", "entity-refresh-sentinel-library", "Movie")
	var entityID int64
	if err := f.pool.QueryRow(f.ctx, "INSERT INTO catalog_entities(kind,name) VALUES('MusicArtist','Refresh Artist') RETURNING id").Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_type,credit_group) VALUES('entity-refresh-audio',$1,1,'Refresh Artist','Artist',1)`, entityID); err != nil {
		t.Fatal(err)
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "entity-refresh-library", "entity-refresh-sentinel-library")
	server := websocketHTTPServer(t, f, time.Hour)
	client := websocketHTTPDial(t, server, "/emby/socket", accounts.viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, f, accounts.viewer.id, 1)
	id := strconv.FormatInt(entityID, 10)
	path := "/emby/Users/" + accounts.viewer.userID + "/Items/" + id + "/UserData"
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"IsFavorite": true, "Rating": 8.5, "Likes": true}, accounts.viewer.headers), http.StatusOK)
	var publication events.Envelope
	if err := json.Unmarshal(client.next(t), &publication); err != nil || publication.MessageType != "UserDataChanged" {
		t.Fatal("entity mutation did not publish a user-data refresh")
	}
	var changed struct {
		UserID string           `json:"UserId"`
		Items  []map[string]any `json:"UserDataList"`
	}
	if err := json.Unmarshal(publication.Data, &changed); err != nil || changed.UserID != accounts.viewer.userID || len(changed.Items) != 1 {
		t.Fatal("entity refresh lost its user-scoped identity")
	}
	if data := changed.Items[0]; data["ItemId"] != id || data["IsFavorite"] != true || data["Rating"] != 8.5 || data["Likes"] != true {
		t.Fatal("entity event did not retain its independent favorite and rating")
	}
	// The notification triggers the consumer's read; source media state stays
	// independent of the entity's state even though it grants entity visibility.
	response := f.request(t, http.MethodGet, path, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if data := jsonObject(t, response); data["IsFavorite"] != true || data["Rating"] != 8.5 || data["Likes"] != true {
		t.Fatal("entity refresh read did not match the committed state")
	}
	source := f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID+"/Items/entity-refresh-audio/UserData", nil, accounts.viewer.headers)
	expectStatus(t, source, http.StatusOK)
	if data := jsonObject(t, source); data["IsFavorite"] != false || data["Rating"] != nil || data["Likes"] != nil {
		t.Fatal("entity mutation changed associated media state")
	}
	setLibraryChangedFolders(t, f, accounts.viewer.userID, "entity-refresh-sentinel-library")
	expectStatus(t, f.request(t, http.MethodGet, path, nil, accounts.viewer.headers), http.StatusNotFound)
	// Republish a formerly authorized snapshot ahead of a real allowed mutation.
	// FIFO delivery makes the allowed sentinel a deterministic filtering barrier.
	changed.Items = append(changed.Items, map[string]any{"ItemId": "9223372036854775807", "IsFavorite": true}, map[string]any{"ItemId": "unknown-physical-item", "IsFavorite": true})
	publication.Data, _ = json.Marshal(changed)
	if _, err := f.app.eventHub.PublishUser(accounts.viewer.userID, publication); err != nil {
		t.Fatal(err)
	}
	sentinelPath := "/emby/Users/" + accounts.viewer.userID + "/Items/entity-refresh-sentinel/UserData"
	expectStatus(t, f.request(t, http.MethodPost, sentinelPath, map[string]any{"IsFavorite": true}, accounts.viewer.headers), http.StatusOK)
	var delivered events.Envelope
	if err := json.Unmarshal(client.next(t), &delivered); err != nil || delivered.MessageType != "UserDataChanged" {
		t.Fatal("allowed sentinel event was not delivered")
	}
	changed.Items = nil
	if err := json.Unmarshal(delivered.Data, &changed); err != nil || len(changed.Items) != 1 || changed.Items[0]["ItemId"] != "entity-refresh-sentinel" {
		t.Fatal("delivery leaked revoked or unknown notification identities")
	}
	response = f.request(t, http.MethodGet, sentinelPath, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if jsonObject(t, response)["IsFavorite"] != true {
		t.Fatal("sentinel refresh did not observe its committed favorite")
	}
}
