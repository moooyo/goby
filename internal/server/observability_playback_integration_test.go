//go:build linux

package server

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const (
	playbackActivityHTTPUserItemID = "playback-activity-user-item"
	playbackActivityHTTPKeyItemID  = "playback-activity-key-item"
)

func seedPlaybackActivityHTTPItems(t *testing.T, f *serverFixture) {
	t.Helper()
	// Activity observes the committed playback state machine, so source files
	// and an encoder are unnecessary for these read projections.
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO libraries(id,name,collection_type)
		VALUES ('playback-activity-library','Playback Activity','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,media) VALUES
		('playback-activity-user-item','playback-activity-library','Initial playback title','initial playback title','Movie',
		 jsonb_build_object('DurationTicks',6000000000::bigint)),
		('playback-activity-key-item','playback-activity-library','Application playback title','application playback title','Movie',
		 jsonb_build_object('DurationTicks',6000000000::bigint))`); err != nil {
		t.Fatalf("seed the playback activity catalog: %v", err)
	}
}

func observePlaybackActivityHTTPPlay(t *testing.T, f *serverFixture, owner library.PlaybackOwner, itemID string) library.PlaySession {
	t.Helper()
	prepared, err := f.app.library.PreparePlayback(f.ctx, owner, itemID, media.SourceID(itemID), "")
	if err != nil {
		t.Fatalf("prepare the activity playback: %v", err)
	}
	current := prepared
	for _, event := range []string{"Started", "Stopped"} {
		current, _, err = f.app.library.ReportPlayback(f.ctx, owner, library.PlaybackReport{
			PlaySessionID: prepared.ID, ItemID: itemID, MediaSourceID: media.SourceID(itemID), Event: event,
		})
		if err != nil {
			t.Fatalf("commit the observed %s playback transition: %v", event, err)
		}
	}
	if current.ID != prepared.ID || current.State != "Stopped" || current.StartedAt == nil || current.StoppedAt == nil {
		t.Fatal("the activity fixture did not commit one complete canonical play")
	}
	return current
}

func TestHTTPNativePlaybackActivityUsesCurrentNamesAndRetainsFactsAfterDeletion(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	f.app.cfg.ActivityRetentionDays = 19
	seedPlaybackActivityHTTPItems(t, f)
	actor, err := f.users.CreateUser(f.ctx, "Native Playback Actor", "native-playback-password", true)
	if err != nil {
		t.Fatalf("create the independent playback actor: %v", err)
	}
	credentials, err := f.users.Authenticate(f.ctx, actor.Name, "native-playback-password",
		identity.Client{Name: "Native Playback Fixture", DeviceID: "native-playback-device"}, "admin")
	if err != nil {
		t.Fatalf("authenticate the native playback actor: %v", err)
	}
	principal, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		t.Fatalf("resolve the native playback actor: %v", err)
	}
	play := observePlaybackActivityHTTPPlay(t, f, playbackOwner(principal), playbackActivityHTTPUserItemID)
	expectedFingerprint, err := activity.PlaybackFingerprint(activity.ActionPlaybackStopped, play.ID)
	if err != nil {
		t.Fatalf("identify the canonical playback stop: %v", err)
	}
	read := func() (map[string]any, map[string]any) {
		t.Helper()
		page := nativeActivityHTTPPage(t, f, cookie, "?Action=playback.stopped")
		items := nativeObservabilityItems(t, page)
		if page["TotalRecordCount"] != float64(1) || len(items) != 1 || page["RetentionDays"] != float64(19) {
			t.Fatal("native playback activity lost its single committed stop or retention policy")
		}
		return page, nativeObservabilityItem(t, items[0])
	}
	_, entry := read()
	actorDTO, resourceDTO := objectValue(t, entry, "Actor"), objectValue(t, entry, "Resource")
	if entry["Action"] != "playback.stopped" || entry["Severity"] != "Info" || entry["Source"] != "native" ||
		entry["Name"] != "Playback stopped" || entry["ObservationFingerprint"] != expectedFingerprint ||
		actorDTO["Kind"] != "user" || actorDTO["Id"] != actor.ID || actorDTO["Name"] != actor.Name ||
		resourceDTO["Kind"] != "item" || resourceDTO["Id"] != playbackActivityHTTPUserItemID || resourceDTO["Name"] != "Initial playback title" {
		t.Fatalf("native activity did not project the real playback and its current identities: %#v", entry)
	}
	nativeObservabilityDecimal(t, entry, "Id")
	stamp := nativeObservabilityDate(t, entry, "Date")
	var storedDate time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT created_at FROM activity_entries
		WHERE action = 'playback.stopped' AND actor_id = $1 AND resource_id = $2`, actor.ID, playbackActivityHTTPUserItemID).Scan(&storedDate); err != nil || !stamp.Equal(storedDate) {
		t.Fatalf("native playback Date did not preserve the committed activity timestamp: %v", err)
	}
	for _, boundary := range []struct {
		date  time.Time
		count float64
	}{
		{stamp, 1},
		{stamp.Add(time.Nanosecond), 0},
	} {
		page := nativeActivityHTTPPage(t, f, cookie, "?Action=playback.stopped&MinDate="+url.QueryEscape(boundary.date.Format(time.RFC3339Nano)))
		if page["TotalRecordCount"] != boundary.count || len(nativeObservabilityItems(t, page)) != int(boundary.count) {
			t.Fatal("native playback activity changed the inclusive Date filter boundary")
		}
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET name = 'Current Playback Actor', normalized_name = 'current playback actor' WHERE id = $1", actor.ID); err != nil {
		t.Fatalf("rename the retained playback actor: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE items SET name = 'Current playback title' WHERE id = $1", playbackActivityHTTPUserItemID); err != nil {
		t.Fatalf("rename the retained playback item: %v", err)
	}
	_, renamed := read()
	if renamed["Id"] != entry["Id"] || renamed["Date"] != entry["Date"] || renamed["ObservationFingerprint"] != expectedFingerprint ||
		objectValue(t, renamed, "Actor")["Name"] != "Current Playback Actor" || objectValue(t, renamed, "Resource")["Name"] != "Current playback title" {
		t.Fatal("playback history did not enrich names from the current user and item")
	}
	if _, err := f.pool.Exec(f.ctx, "DELETE FROM users WHERE id = $1", actor.ID); err != nil {
		t.Fatalf("delete the independent playback actor: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "DELETE FROM items WHERE id = $1", playbackActivityHTTPUserItemID); err != nil {
		t.Fatalf("delete the playback item: %v", err)
	}
	_, deleted := read()
	deletedActor, deletedResource := objectValue(t, deleted, "Actor"), objectValue(t, deleted, "Resource")
	nativeObservabilityFields(t, deletedActor, "Kind", "Id", "Name")
	nativeObservabilityFields(t, deletedResource, "Kind", "Id")
	if deleted["Id"] != entry["Id"] || deleted["Date"] != entry["Date"] || deleted["ObservationFingerprint"] != expectedFingerprint ||
		deletedActor["Kind"] != "user" || deletedActor["Id"] != actor.ID || deletedActor["Name"] != nil ||
		deletedResource["Kind"] != "item" || deletedResource["Id"] != playbackActivityHTTPUserItemID {
		t.Fatal("playback history lost retained identities or invented a deleted display name")
	}
}

func TestHTTPEmbyPlaybackActivityMapsRealUserAndApplicationObservations(t *testing.T) {
	f := newApplicationKeyHTTPFixture(t)
	seedPlaybackActivityHTTPItems(t, f.serverFixture)
	viewer, err := f.users.CreateUser(f.ctx, "Playback Activity Viewer", "playback-viewer-password", false)
	if err != nil {
		t.Fatalf("create the real playback viewer: %v", err)
	}
	viewerToken := stringValue(t, f.embyLogin(t, viewer.Name, "playback-viewer-password"), "AccessToken")
	viewerPrincipal, err := f.users.ResolveEmby(f.ctx, viewerToken)
	if err != nil {
		t.Fatalf("resolve the real Emby playback viewer: %v", err)
	}
	observePlaybackActivityHTTPPlay(t, f.serverFixture, playbackOwner(viewerPrincipal), playbackActivityHTTPUserItemID)
	key := f.create(t, "Playback Activity Application")
	keyPrincipal, err := f.users.ResolveEmbyForClient(f.ctx, key.token, identity.Client{
		Name: "Playback Activity Client", DeviceID: "application-playback-device", Device: "Linux", Version: "1.0",
	})
	if err != nil || !keyPrincipal.IsApplicationKey() || keyPrincipal.User.ID != "" {
		t.Fatalf("resolve the independent application playback client: %v", err)
	}
	observePlaybackActivityHTTPPlay(t, f.serverFixture, playbackOwner(keyPrincipal), playbackActivityHTTPKeyItemID)
	adminToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	const route = "/emby/System/ActivityLog/Entries?Limit=200"
	page := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, route, nil, embyObservabilityClientHeaders(adminToken)))
	seen := make(map[string]bool)
	for _, raw := range nativeObservabilityItems(t, page) {
		entry := nativeObservabilityItem(t, raw)
		var action activity.Action
		switch entry["Type"] {
		case "VideoPlayback":
			action = activity.ActionPlaybackStarted
		case "VideoPlaybackStopped":
			action = activity.ActionPlaybackStopped
		default:
			continue
		}
		itemID := stringValue(t, entry, "ItemId")
		observationID := itemID + "/" + string(action)
		if seen[observationID] {
			t.Fatal("Emby activity repeated a committed playback transition")
		}
		seen[observationID] = true
		switch itemID {
		case playbackActivityHTTPUserItemID:
			nativeObservabilityFields(t, entry, "Id", "Name", "Overview", "Type", "Date", "UserId", "Severity", "ItemId")
			if entry["UserId"] != viewer.ID {
				t.Fatal("Emby playback activity lost its real user association")
			}
		case playbackActivityHTTPKeyItemID:
			nativeObservabilityFields(t, entry, "Id", "Name", "Overview", "Type", "Date", "Severity", "ItemId")
			if _, present := entry["UserId"]; present {
				t.Fatal("Emby application playback activity invented a user identity")
			}
		default:
			t.Fatal("Emby playback ItemId did not identify a canonical catalog item")
		}
		if entry["Severity"] != "Info" || action == activity.ActionPlaybackStarted && entry["Name"] != "Playback started" ||
			action == activity.ActionPlaybackStopped && entry["Name"] != "Playback stopped" {
			t.Fatal("Emby playback activity lost its fixed safe description")
		}
		stamp := embyObservabilityUTCDate(t, entry, "Date")
		var storedDate time.Time
		if err := f.pool.QueryRow(f.ctx, "SELECT created_at FROM activity_entries WHERE action = $1 AND resource_id = $2", string(action), itemID).Scan(&storedDate); err != nil || !stamp.Equal(storedDate) {
			t.Fatalf("Emby playback Date differs from the committed activity timestamp: %v", err)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("Emby activity returned %d playback observations, want four real transitions", len(seen))
	}
	keyPage := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, route, nil, embyObservabilityClientHeaders(key.token)))
	if !reflect.DeepEqual(keyPage, page) {
		t.Fatal("an administrative application key received a different playback activity projection")
	}
	viewerResponse := f.request(t, http.MethodGet, route, nil, embyObservabilityClientHeaders(viewerToken))
	expectEmbyTextError(t, viewerResponse, http.StatusForbidden, "User "+viewer.Name+" does not have access to ManageServer feature.")
	nativeObservabilityNoCache(t, viewerResponse)
	for _, token := range []string{viewerToken, key.token} {
		nativeObservabilityError(t, f.request(t, http.MethodGet, "/admin/v1/activity?Action=playback.stopped", nil,
			embyObservabilityClientHeaders(token)), http.StatusUnauthorized, "authentication_required")
		nativeObservabilityError(t, f.request(t, http.MethodGet, "/admin/v1/activity?Action=playback.stopped", nil, nil,
			&http.Cookie{Name: sessionCookie, Value: token}), http.StatusUnauthorized, "invalid_credentials")
	}
}

func TestHTTPEmbyPlaybackActivityUsesCurrentAudioTypeAndFallsBackAfterItemDeletion(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	seedPlaybackActivityHTTPItems(t, f)
	if _, err := f.pool.Exec(f.ctx, "UPDATE items SET type='Audio' WHERE id=$1", playbackActivityHTTPUserItemID); err != nil {
		t.Fatalf("classify the playback activity item as audio: %v", err)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Audio Activity Viewer", "audio-activity-password", false)
	if err != nil {
		t.Fatalf("create the real audio playback viewer: %v", err)
	}
	viewerToken := stringValue(t, f.embyLogin(t, viewer.Name, "audio-activity-password"), "AccessToken")
	principal, err := f.users.ResolveEmby(f.ctx, viewerToken)
	if err != nil {
		t.Fatalf("resolve the real audio playback viewer: %v", err)
	}
	observePlaybackActivityHTTPPlay(t, f, playbackOwner(principal), playbackActivityHTTPUserItemID)
	adminToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	read := func(types map[string]activity.Action) map[activity.Action]map[string]any {
		t.Helper()
		page := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, "/emby/System/ActivityLog/Entries?Limit=200", nil,
			embyObservabilityClientHeaders(adminToken)))
		facts := make(map[activity.Action]map[string]any)
		for _, raw := range nativeObservabilityItems(t, page) {
			entry := nativeObservabilityItem(t, raw)
			if entry["ItemId"] != playbackActivityHTTPUserItemID {
				continue
			}
			action, present := types[stringValue(t, entry, "Type")]
			if !present {
				t.Fatalf("Emby playback Type did not reflect the current audio classification or its absence: %#v", entry)
			}
			if _, duplicate := facts[action]; duplicate {
				t.Fatal("Emby audio activity repeated a committed playback transition")
			}
			nativeObservabilityFields(t, entry, "Id", "Name", "Overview", "Type", "Date", "UserId", "Severity", "ItemId")
			if entry["UserId"] != viewer.ID || entry["Severity"] != "Info" {
				t.Fatal("Emby audio activity lost the real viewer or committed severity")
			}
			stamp := embyObservabilityUTCDate(t, entry, "Date")
			var storedDate time.Time
			if err := f.pool.QueryRow(f.ctx, "SELECT created_at FROM activity_entries WHERE action=$1 AND resource_id=$2",
				string(action), playbackActivityHTTPUserItemID).Scan(&storedDate); err != nil || !stamp.Equal(storedDate) {
				t.Fatalf("Emby audio activity did not preserve the committed playback timestamp: %v", err)
			}
			facts[action] = entry
		}
		if len(facts) != 2 {
			t.Fatalf("Emby audio activity returned %d observations, want the real start and stop", len(facts))
		}
		return facts
	}
	before := read(map[string]activity.Action{
		"AudioPlayback": activity.ActionPlaybackStarted, "AudioPlaybackStopped": activity.ActionPlaybackStopped,
	})
	if _, err := f.pool.Exec(f.ctx, "DELETE FROM items WHERE id=$1", playbackActivityHTTPUserItemID); err != nil {
		t.Fatalf("delete the observed audio playback item: %v", err)
	}
	deleted := read(map[string]activity.Action{
		"playback.started": activity.ActionPlaybackStarted, "playback.stopped": activity.ActionPlaybackStopped,
	})
	for action, entry := range before {
		for _, field := range []string{"Id", "Name", "Overview", "Date", "UserId", "Severity", "ItemId"} {
			if !reflect.DeepEqual(entry[field], deleted[action][field]) {
				t.Fatalf("deleting the audio item changed its retained playback %s field %s", action, field)
			}
		}
	}
}
