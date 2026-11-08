package activity

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func playbackObservationFixture(t *testing.T, action Action) Event {
	t.Helper()
	fingerprint, err := PlaybackFingerprint(action, "play_canonical-observation")
	if err != nil {
		t.Fatal(err)
	}
	return Event{Action: action, Source: SourceEmby, Severity: SeverityInfo,
		Actor:    Actor{Kind: ActorUser, ID: "playback-user", CredentialID: "existing-authentication"},
		Resource: Resource{Kind: ResourceItem, ID: "media-item"}, ObservationFingerprint: fingerprint}
}

func TestPlaybackFingerprintSeparatesCanonicalPlaysAndTransitions(t *testing.T) {
	first, err := PlaybackFingerprint(ActionPlaybackStarted, "play_first")
	if err != nil || len(first) != 64 || !validObservationFingerprint(first) {
		t.Fatalf("canonical start fingerprint = %q, %v", first, err)
	}
	again, err := PlaybackFingerprint(ActionPlaybackStarted, "play_first")
	if err != nil || first != again {
		t.Fatal("the same canonical transition lost its deterministic fingerprint")
	}
	for _, test := range []struct {
		action Action
		id     string
	}{
		{ActionPlaybackStopped, "play_first"},
		{ActionPlaybackStarted, "play_second"},
	} {
		other, err := PlaybackFingerprint(test.action, test.id)
		if err != nil || other == first {
			t.Fatalf("distinct canonical transition reused fingerprint: %v", err)
		}
	}
	for _, test := range []struct {
		action Action
		id     string
	}{
		{ActionSessionLogin, "play_first"},
		{ActionPlaybackStarted, "client-reference"},
		{ActionPlaybackStopped, ""},
		{ActionPlaybackStopped, "play_/private/media.mkv"},
		{ActionPlaybackStopped, "play_" + strings.Repeat("x", MaxIdentifierBytes)},
	} {
		if value, err := PlaybackFingerprint(test.action, test.id); !errors.Is(err, ErrInvalidInput) || value != "" {
			t.Fatalf("invalid canonical observation acquired a fingerprint: %q, %v", value, err)
		}
	}
}

func TestPlaybackActivityRequiresItsExactObservedFactShape(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Event)
	}{
		{"system source", func(event *Event) { event.Source = SourceSystem }},
		{"warning severity", func(event *Event) { event.Severity = SeverityWarning }},
		{"system actor", func(event *Event) { event.Actor = Actor{Kind: ActorSystem} }},
		{"missing authentication", func(event *Event) { event.Actor.CredentialID = "" }},
		{"path authentication", func(event *Event) { event.Actor.CredentialID = "/private/authentication" }},
		{"native application", func(event *Event) {
			event.Source = SourceNative
			event.Actor.Kind = ActorApplicationKey
		}},
		{"playback resource", func(event *Event) { event.Resource.Kind = ResourceSession }},
		{"source pathname", func(event *Event) { event.Resource.ID = "/private/media.mkv" }},
		{"revision", func(event *Event) { event.Revision = 1 }},
		{"previous revision", func(event *Event) { event.PreviousRevision = 1 }},
		{"affected count", func(event *Event) { event.Count = 1 }},
		{"completion claim", func(event *Event) { event.State = StateCompleted }},
		{"changed fields", func(event *Event) { event.ChangedFields = []Field{FieldName} }},
		{"missing fingerprint", func(event *Event) { event.ObservationFingerprint = "" }},
		{"uppercase fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("A", 64) }},
		{"arbitrary fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("x", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := playbackObservationFixture(t, ActionPlaybackStopped)
			test.edit(&event)
			executor := &activityCaptureExecutor{}
			if ValidPlaybackObservation(event) {
				t.Fatal("invalid observation passed read validation")
			}
			if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) || executor.calls != 0 {
				t.Fatalf("invalid observation reached storage: calls=%d, error=%v", executor.calls, err)
			}
		})
	}
}

func TestPlaybackActivityAcceptsOnlyPersistedUserAndApplicationIdentities(t *testing.T) {
	for _, action := range []Action{ActionPlaybackStarted, ActionPlaybackStopped} {
		for _, test := range []struct {
			source Source
			actor  Actor
		}{
			{SourceNative, Actor{Kind: ActorUser, ID: "administrator", CredentialID: "admin-authentication"}},
			{SourceEmby, Actor{Kind: ActorUser, ID: "viewer", CredentialID: "emby-authentication"}},
			{SourceEmby, Actor{Kind: ActorApplicationKey, ID: "42", CredentialID: "application-authentication"}},
		} {
			event := playbackObservationFixture(t, action)
			event.Source, event.Actor, event.Severity = test.source, test.actor, ""
			executor := &activityCaptureExecutor{}
			if !ValidPlaybackObservation(event) {
				t.Fatal("typed persisted observation failed read validation")
			}
			if err := Record(context.Background(), executor, event); err != nil || executor.calls != 1 {
				t.Fatalf("record typed persisted observation: calls=%d, error=%v", executor.calls, err)
			}
			if executor.args[1] != string(SeverityInfo) || executor.args[11] != event.ObservationFingerprint {
				t.Fatal("storage lost the normalized severity or canonical observation")
			}
			if name, overview := description(action, ""); name == "" || overview == "" {
				t.Fatal("playback observation lacks its fixed presentation")
			}
		}
	}
}
