package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
)

func playbackActivityDTOFixture(t *testing.T, action activity.Action) activity.Entry {
	t.Helper()
	fingerprint, err := activity.PlaybackFingerprint(action, "play_dto-canonical")
	if err != nil {
		t.Fatal(err)
	}
	return activity.Entry{ID: 73, Date: time.Date(2026, 11, 6, 8, 9, 10, 123456000, time.UTC),
		Event: activity.Event{Action: action, Severity: activity.SeverityInfo, Source: activity.SourceEmby,
			Actor:    activity.Actor{Kind: activity.ActorUser, ID: "real-user", CredentialID: "private-authentication"},
			Resource: activity.Resource{Kind: activity.ResourceItem, ID: "real-item"}, ObservationFingerprint: fingerprint,
			RequestID: "private-request"},
		ActorName: "Current User", ResourceName: "Current Media", ResourceMediaType: "Video",
		Name: "Playback observed", Overview: "An observed player transition."}
}

func TestNativePlaybackActivityDTOEnrichesCurrentNamesWithoutPlaybackCredentials(t *testing.T) {
	entry := playbackActivityDTOFixture(t, activity.ActionPlaybackStopped)
	page, err := nativeActivityDTO(activity.Page{Items: []activity.Entry{entry}, TotalRecordCount: 1, Limit: 6}, 17)
	if err != nil || len(page.Items) != 1 || page.RetentionDays != 17 {
		t.Fatalf("project native playback observation: %+v, %v", page, err)
	}
	projected := page.Items[0]
	if projected.Resource.Kind != activity.ResourceItem || projected.Resource.ID != "real-item" ||
		projected.Resource.Name == nil || *projected.Resource.Name != "Current Media" ||
		projected.Actor.Name == nil || *projected.Actor.Name != "Current User" ||
		!projected.Date.Equal(entry.Date) || projected.Revision != nil || projected.State != nil {
		t.Fatal("native playback activity lost its actual item, user, time, or optional fact semantics")
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-authentication", "private-request", "play_dto-canonical", "CredentialID", "RequestID", "DeviceID"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("native playback projection exposed a private playback or authentication identity")
		}
	}
	for _, title := range []string{"", strings.Repeat("a", 1025), "Control\nTitle"} {
		entry.ResourceName = title
		projected, err := nativeActivityDTO(activity.Page{Items: []activity.Entry{entry}}, 30)
		if err != nil || projected.Items[0].Resource.Name != nil {
			t.Fatal("an unavailable or unsafe current item title was projected")
		}
	}
}

func TestEmbyPlaybackActivityDTOKeepsCoreTypesAndRealItemIdentity(t *testing.T) {
	for _, test := range []struct {
		action activity.Action
		kind   string
	}{
		{activity.ActionPlaybackStarted, "VideoPlayback"},
		{activity.ActionPlaybackStopped, "VideoPlaybackStopped"},
	} {
		entry := playbackActivityDTOFixture(t, test.action)
		for _, application := range []bool{false, true} {
			if application {
				entry.Actor.Kind, entry.Actor.ID, entry.ActorName = activity.ActorApplicationKey, "42", ""
			}
			result, err := embyActivityDTO(activity.Page{Items: []activity.Entry{entry}, TotalRecordCount: 1},
				embyObservabilityPageOptions{Limit: 6, LimitPresent: true})
			if err != nil || len(result.Items) != 1 {
				t.Fatalf("project Emby playback observation: %v", err)
			}
			item := result.Items[0]
			if item.Type != test.kind || item.ItemID != "real-item" ||
				item.Date != entry.Date.Format(embyObservabilityDateFormat) ||
				(!application && item.UserID != "real-user") || application && item.UserID != "" {
				t.Fatal("core playback projection lost its observed type, item, time, or real user identity")
			}
		}
	}
}

func TestPlaybackActivityDTORejectsMalformedObservationWithoutPartialPage(t *testing.T) {
	valid := playbackActivityDTOFixture(t, activity.ActionPlaybackStopped)
	for _, test := range []struct {
		name string
		edit func(*activity.Entry)
	}{
		{"missing fingerprint", func(entry *activity.Entry) { entry.ObservationFingerprint = "" }},
		{"wrong source", func(entry *activity.Entry) { entry.Source = activity.SourceSystem }},
		{"wrong actor", func(entry *activity.Entry) { entry.Actor = activity.Actor{Kind: activity.ActorSystem} }},
		{"unconfirmed completion", func(entry *activity.Entry) { entry.State = activity.StateCompleted }},
		{"wrong resource", func(entry *activity.Entry) { entry.Resource.Kind = activity.ResourceSession }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := valid
			test.edit(&invalid)
			page := activity.Page{Items: []activity.Entry{valid, invalid}, TotalRecordCount: 2}
			if result, err := nativeActivityDTO(page, 30); !errors.Is(err, activity.ErrUnavailable) || len(result.Items) != 0 {
				t.Fatal("native activity projected a malformed or partial playback page")
			}
			if result, err := embyActivityDTO(page, embyObservabilityPageOptions{Limit: 6, LimitPresent: true}); !errors.Is(err, activity.ErrUnavailable) || len(result.Items) != 0 {
				t.Fatal("Emby activity projected a malformed or partial playback page")
			}
		})
	}
}

func TestEmbyPlaybackActivityDTOUsesCurrentMediaTypeWithoutInventingDeletedItems(t *testing.T) {
	for _, action := range []activity.Action{activity.ActionPlaybackStarted, activity.ActionPlaybackStopped} {
		for _, test := range []struct {
			mediaType string
			name      string
			want      string
		}{
			{"Audio", "Current Audio", "AudioPlayback"},
			{"Video", "Current Video", "VideoPlayback"},
			{"", "", string(action)},
			{"Unknown", "Unclassified Item", string(action)},
		} {
			entry := playbackActivityDTOFixture(t, action)
			entry.ResourceMediaType, entry.ResourceName = test.mediaType, test.name
			if (test.mediaType == "Audio" || test.mediaType == "Video") && action == activity.ActionPlaybackStopped {
				test.want += "Stopped"
			}
			result, err := embyActivityDTO(activity.Page{Items: []activity.Entry{entry}, TotalRecordCount: 1},
				embyObservabilityPageOptions{Limit: 6, LimitPresent: true})
			if err != nil || len(result.Items) != 1 || result.Items[0].Type != test.want || result.Items[0].ItemID != "real-item" {
				t.Fatalf("current %q media projection = %+v, %v; want type %q and retained item identity", test.mediaType, result, err, test.want)
			}
			native, err := nativeActivityDTO(activity.Page{Items: []activity.Entry{entry}}, 30)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(native.Items[0])
			if err != nil || strings.Contains(string(encoded), "MediaType") {
				t.Fatal("an internal current-item classification leaked into the native activity DTO")
			}
		}
	}
}

func TestNativePublishedBackupActivityDTOAcceptsItsTypedObservationFingerprint(t *testing.T) {
	entry := activity.Entry{ID: 74, Event: activity.Event{Action: activity.ActionBackupPublished,
		Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem},
		Resource: activity.Resource{Kind: activity.ResourceBackup, ID: "existing-backup"},
		State:    activity.StateCompleted, ObservationFingerprint: strings.Repeat("a", 64)}}
	page := activity.Page{Items: []activity.Entry{entry}, TotalRecordCount: 1}
	result, err := nativeActivityDTO(page, 30)
	if err != nil || len(result.Items) != 1 || result.Items[0].ObservationFingerprint != entry.ObservationFingerprint {
		t.Fatalf("published backup observation poisoned the native activity page: %+v, %v", result, err)
	}
	page.Items[0].ObservationFingerprint = strings.Repeat("A", 64)
	if _, err := nativeActivityDTO(page, 30); !errors.Is(err, activity.ErrUnavailable) {
		t.Fatal("a malformed backup observation fingerprint passed native projection")
	}
}
