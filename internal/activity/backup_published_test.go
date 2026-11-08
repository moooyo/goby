package activity

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestBackupPublicationFingerprintBindsExactObjectIdentity(t *testing.T) {
	id, digest := strings.Repeat("a", 32), strings.Repeat("b", 64)
	const size int64 = 9007199254740993
	fingerprint, err := BackupPublicationFingerprint(id, digest, size)
	if err != nil || !validObservationFingerprint(fingerprint) {
		t.Fatalf("valid published object did not produce a canonical fingerprint: %v", err)
	}
	again, err := BackupPublicationFingerprint(id, digest, size)
	if err != nil || again != fingerprint {
		t.Fatal("an exact publication retry changed its fingerprint")
	}
	for _, test := range []struct {
		name, id, digest string
		size             int64
	}{
		{"different_id", strings.Repeat("c", 32), digest, size},
		{"different_digest", id, strings.Repeat("d", 64), size},
		{"adjacent_large_size", id, digest, size - 1},
		{"larger_size", id, digest, size + 1},
		{"minimum_size", id, digest, 1},
		{"maximum_size", id, digest, math.MaxInt64},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed, err := BackupPublicationFingerprint(test.id, test.digest, test.size)
			if err != nil || !validObservationFingerprint(changed) || changed == fingerprint {
				t.Fatalf("publication fingerprint did not bind the exact changed object: %v", err)
			}
		})
	}
}

func TestBackupPublicationFingerprintRejectsInvalidObjects(t *testing.T) {
	id, digest := strings.Repeat("a", 32), strings.Repeat("b", 64)
	for _, test := range []struct {
		name, id, digest string
		size             int64
	}{
		{"empty_id", "", digest, 1},
		{"short_id", id[:31], digest, 1},
		{"long_id", id + "a", digest, 1},
		{"uppercase_id", strings.ToUpper(id), digest, 1},
		{"nonhex_id", strings.Repeat("g", 32), digest, 1},
		{"private_path", "/private/backup.archive", digest, 1},
		{"empty_digest", id, "", 1},
		{"short_digest", id, digest[:63], 1},
		{"long_digest", id, digest + "b", 1},
		{"uppercase_digest", id, strings.ToUpper(digest), 1},
		{"nonhex_digest", id, strings.Repeat("z", 64), 1},
		{"zero_size", id, digest, 0},
		{"negative_size", id, digest, -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fingerprint, err := BackupPublicationFingerprint(test.id, test.digest, test.size)
			if !errors.Is(err, ErrInvalidInput) || fingerprint != "" {
				t.Fatalf("invalid object produced a publication receipt: %q, %v", fingerprint, err)
			}
		})
	}
}

func backupPublishedTestEvent(observation string) Event {
	return Event{Action: ActionBackupPublished, Source: SourceSystem, Actor: Actor{Kind: ActorSystem},
		Resource: Resource{Kind: ResourceBackup, ID: strings.Repeat("a", 32)}, State: StateCompleted,
		ObservationFingerprint: observation}
}

func TestBackupPublishedActivityAcceptsOnlyBoundedCompletionFacts(t *testing.T) {
	for _, observation := range []string{strings.Repeat("a", 64), strings.Repeat("0", 64)} {
		event := backupPublishedTestEvent(observation)
		args, err := eventArguments(event)
		want := []any{string(ActionBackupPublished), string(SeverityInfo), string(SourceSystem), string(ActorSystem),
			"", "", string(ResourceBackup), event.Resource.ID, "", int64(0), int64(0), observation, int64(0), string(StateCompleted), []string{}}
		if err != nil || !reflect.DeepEqual(args, want) {
			t.Fatalf("valid publication facts were changed or rejected: %#v, %v", args, err)
		}
	}
}

func TestBackupPublishedActivityRejectsNonpublicationOrPrivateFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Event)
	}{
		{"no_state", func(e *Event) { e.State = "" }},
		{"failed", func(e *Event) { e.State = StateFailed }},
		{"cancelled", func(e *Event) { e.State = StateCancelled }},
		{"interrupted", func(e *Event) { e.State = StateInterrupted }},
		{"native_source", func(e *Event) { e.Source = SourceNative }},
		{"emby_source", func(e *Event) { e.Source = SourceEmby }},
		{"user_actor", func(e *Event) { e.Actor = Actor{Kind: ActorUser, ID: "administrator", CredentialID: "credential"} }},
		{"application_actor", func(e *Event) {
			e.Actor = Actor{Kind: ActorApplicationKey, ID: "application-key", CredentialID: "credential"}
		}},
		{"system_actor_id", func(e *Event) { e.Actor.ID = "administrator" }},
		{"system_credential", func(e *Event) { e.Actor.CredentialID = "credential" }},
		{"empty_observation", func(e *Event) { e.ObservationFingerprint = "" }},
		{"short_observation", func(e *Event) { e.ObservationFingerprint = strings.Repeat("a", 63) }},
		{"long_observation", func(e *Event) { e.ObservationFingerprint = strings.Repeat("a", 65) }},
		{"uppercase_observation", func(e *Event) { e.ObservationFingerprint = strings.Repeat("A", 64) }},
		{"nonhex_observation", func(e *Event) { e.ObservationFingerprint = strings.Repeat("g", 64) }},
		{"revision", func(e *Event) { e.Revision = 1 }},
		{"previous_revision", func(e *Event) { e.PreviousRevision = 1 }},
		{"negative_revision", func(e *Event) { e.Revision = -1 }},
		{"affected_count", func(e *Event) { e.Count = 1 }},
		{"negative_count", func(e *Event) { e.Count = -1 }},
		{"changed_fields", func(e *Event) { e.ChangedFields = []Field{FieldName} }},
		{"private_changed_field", func(e *Event) { e.ChangedFields = []Field{"Passphrase=private-value"} }},
		{"wrong_resource", func(e *Event) { e.Resource.Kind = ResourceRestore }},
		{"private_resource", func(e *Event) { e.Resource.ID = "/private/backup.archive" }},
		{"earlier_finish_phase", func(e *Event) { e.Action = ActionBackupFinished }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := backupPublishedTestEvent(strings.Repeat("a", 64))
			test.change(&event)
			if args, err := eventArguments(event); !errors.Is(err, ErrInvalidInput) || args != nil {
				t.Fatalf("invalid publication facts were admitted: %#v, %v", args, err)
			}
		})
	}
}
