package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
)

func TestNativeActivityRootBindingDTORetainsExactRevisionFacts(t *testing.T) {
	for _, test := range []struct {
		name                      string
		previous, current         int64
		wantPrevious, wantCurrent string
	}{
		{"initial update", 1, 2, "1", "2"},
		{"above JavaScript integer precision", 9007199254740993, 9007199254740994, "9007199254740993", "9007199254740994"},
		{"maximum stored revision", 9223372036854775806, 9223372036854775807, "9223372036854775806", "9223372036854775807"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fingerprint := strings.Repeat("0123456789abcdef", 4)
			page := activity.Page{TotalRecordCount: 1, Limit: 50, Items: []activity.Entry{{
				ID: 7, Date: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC),
				Event: activity.Event{
					Action: activity.ActionLibraryRootBindingUpdated, Severity: activity.SeverityInfo, Source: activity.SourceNative,
					Actor:    activity.Actor{Kind: activity.ActorUser, ID: "administrator", CredentialID: "private-session"},
					Resource: activity.Resource{Kind: activity.ResourceLibraryRoot, ID: "registered-root"},
					Revision: test.current, PreviousRevision: test.previous, ObservationFingerprint: fingerprint,
					RequestID: "private-request",
				},
				Name: "Library root binding updated", Overview: "The registered root binding was updated.",
			}}}
			result, err := nativeActivityDTO(page, 30)
			if err != nil || len(result.Items) != 1 {
				t.Fatalf("project root binding activity: %v", err)
			}
			encoded, err := json.Marshal(result.Items[0])
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 15 || string(fields["PreviousRevision"]) != `"`+test.wantPrevious+`"` ||
				string(fields["Revision"]) != `"`+test.wantCurrent+`"` || string(fields["ObservationFingerprint"]) != `"`+fingerprint+`"` {
				t.Fatalf("root binding facts lost their exact string representation: %s", encoded)
			}
			if string(fields["Action"]) != `"library.root_binding.updated"` ||
				string(fields["Resource"]) != `{"Kind":"library_root","Id":"registered-root"}` {
				t.Fatal("root binding activity lost its action or registered resource identity")
			}
			for _, private := range []string{"CredentialID", "RequestID", "private-session", "private-request"} {
				if strings.Contains(string(encoded), private) {
					t.Fatal("root binding activity exposed a private correlation or credential identifier")
				}
			}
		})
	}
}

func TestNativeActivityLegacyDTOOmitsRootBindingFacts(t *testing.T) {
	page := activity.Page{TotalRecordCount: 1, Items: []activity.Entry{{ID: 1, Event: activity.Event{
		Action: activity.ActionLibraryCreated, Resource: activity.Resource{Kind: activity.ResourceLibrary, ID: "library"},
	}}}}
	result, err := nativeActivityDTO(page, 30)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("project legacy activity: %v", err)
	}
	encoded, err := json.Marshal(result.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 13 || fields["PreviousRevision"] != nil || fields["ObservationFingerprint"] != nil {
		t.Fatalf("legacy activity acquired absent root binding fields: %s", encoded)
	}
	if string(fields["Revision"]) != "null" || string(fields["Count"]) != `"0"` || string(fields["ChangedFields"]) != "[]" {
		t.Fatal("optional root binding facts changed the existing legacy null, decimal, or array contract")
	}
}

func TestNativeActivityDTORejectsNegativePreviousRevisionWithoutPartialPage(t *testing.T) {
	page := activity.Page{TotalRecordCount: 2, Items: []activity.Entry{
		{ID: 1, Event: activity.Event{Revision: 1}},
		{ID: 2, Event: activity.Event{Revision: 2, PreviousRevision: -1}},
	}}
	result, err := nativeActivityDTO(page, 30)
	if !errors.Is(err, activity.ErrUnavailable) || len(result.Items) != 0 {
		t.Fatalf("invalid stored revision returned a successful or partial activity page: %+v, %v", result, err)
	}
}

func TestNativeActivityDTORejectsInconsistentRootBindingFacts(t *testing.T) {
	valid := activity.Event{
		Action: activity.ActionLibraryRootBindingUpdated, Severity: activity.SeverityInfo, Source: activity.SourceNative,
		Actor:    activity.Actor{Kind: activity.ActorUser, ID: "administrator"},
		Resource: activity.Resource{Kind: activity.ResourceLibraryRoot, ID: "registered-root"},
		Revision: 2, PreviousRevision: 1, ObservationFingerprint: strings.Repeat("a", 64),
	}
	for _, test := range []struct {
		name string
		edit func(*activity.Event)
	}{
		{"non-native source", func(event *activity.Event) { event.Source = activity.SourceEmby }},
		{"system actor", func(event *activity.Event) { event.Actor = activity.Actor{Kind: activity.ActorSystem} }},
		{"application actor", func(event *activity.Event) { event.Actor.Kind = activity.ActorApplicationKey }},
		{"wrong resource", func(event *activity.Event) { event.Resource.Kind = activity.ResourceLibrary }},
		{"missing previous revision", func(event *activity.Event) { event.PreviousRevision = 0 }},
		{"overflowing previous revision", func(event *activity.Event) { event.PreviousRevision = 9223372036854775807 }},
		{"missing current revision", func(event *activity.Event) { event.Revision = 0 }},
		{"unchanged revision", func(event *activity.Event) { event.Revision = 1 }},
		{"skipped revision", func(event *activity.Event) { event.Revision = 3 }},
		{"missing fingerprint", func(event *activity.Event) { event.ObservationFingerprint = "" }},
		{"short fingerprint", func(event *activity.Event) { event.ObservationFingerprint = strings.Repeat("a", 63) }},
		{"long fingerprint", func(event *activity.Event) { event.ObservationFingerprint = strings.Repeat("a", 65) }},
		{"uppercase fingerprint", func(event *activity.Event) { event.ObservationFingerprint = strings.Repeat("A", 64) }},
		{"non-hexadecimal fingerprint", func(event *activity.Event) { event.ObservationFingerprint = strings.Repeat("g", 64) }},
		{"affected count", func(event *activity.Event) { event.Count = 1 }},
		{"terminal state", func(event *activity.Event) { event.State = activity.StateCompleted }},
		{"changed field", func(event *activity.Event) { event.ChangedFields = []activity.Field{activity.FieldName} }},
		{"previous revision on a legacy action", func(event *activity.Event) {
			event.Action = activity.ActionLibraryCreated
			event.ObservationFingerprint = ""
		}},
		{"fingerprint on a legacy action", func(event *activity.Event) {
			event.Action = activity.ActionLibraryCreated
			event.PreviousRevision = 0
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			test.edit(&event)
			page := activity.Page{TotalRecordCount: 1, Items: []activity.Entry{{ID: 1, Event: event}}}
			if result, err := nativeActivityDTO(page, 30); !errors.Is(err, activity.ErrUnavailable) || len(result.Items) != 0 {
				t.Fatalf("invalid root binding facts produced a successful activity page: %+v, %v", result, err)
			}
		})
	}
}
