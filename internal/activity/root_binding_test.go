package activity

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func rootBindingActivityEvent() Event {
	return Event{Action: ActionLibraryRootBindingUpdated, Severity: SeverityInfo, Source: SourceNative,
		Actor:    Actor{Kind: ActorUser, ID: "historical-administrator", CredentialID: "existing-session"},
		Resource: Resource{Kind: ResourceLibraryRoot, ID: "historical-root:1"}, RequestID: "binding-request",
		PreviousRevision: 9007199254740993, Revision: 9007199254740994,
		ObservationFingerprint: strings.Repeat("0a", 32)}
}

type rootBindingOwnedCapture struct{ capture *activityCaptureExecutor }

func (executor rootBindingOwnedCapture) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	return executor.capture.Exec(context.Background(), statement, args...)
}

func TestRootBindingActivityAcceptsAdjacentRevisionsWithoutRounding(t *testing.T) {
	for _, previous := range []int64{1, 9007199254740993, math.MaxInt64 - 1} {
		t.Run(strconv.FormatInt(previous, 10), func(t *testing.T) {
			event := rootBindingActivityEvent()
			event.PreviousRevision, event.Revision = previous, previous+1
			capture := &activityCaptureExecutor{}
			if err := Record(context.Background(), capture, event); err != nil || capture.calls != 1 {
				t.Fatalf("record a valid root binding revision transition: calls=%d error=%v", capture.calls, err)
			}
			owned := &activityCaptureExecutor{}
			if err := RecordOwned(rootBindingOwnedCapture{capture: owned}, event); err != nil || owned.calls != 1 {
				t.Fatalf("record valid root binding facts through the owned executor: calls=%d error=%v", owned.calls, err)
			}
			if !reflect.DeepEqual(capture.args, owned.args) {
				t.Fatal("ordinary and protected activity writers disagreed on the committed root binding facts")
			}
			var previousFound, currentFound bool
			for _, argument := range capture.args {
				if value, ok := argument.(int64); ok {
					previousFound = previousFound || value == previous
					currentFound = currentFound || value == previous+1
				}
			}
			if !previousFound || !currentFound {
				t.Fatal("the activity writer rounded or discarded an exact revision fact")
			}
		})
	}
}

func TestRootBindingActivityRejectsInvalidOrUnrelatedFactsBeforeStorage(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Event)
	}{
		{"compatibility source", func(event *Event) { event.Source = SourceEmby }},
		{"system source", func(event *Event) { event.Source = SourceSystem }},
		{"application actor", func(event *Event) { event.Actor.Kind = ActorApplicationKey }},
		{"system actor", func(event *Event) { event.Actor = Actor{Kind: ActorSystem} }},
		{"wrong resource kind", func(event *Event) { event.Resource.Kind = ResourceLibrary }},
		{"path as root identity", func(event *Event) { event.Resource.ID = "/private/media/registered" }},
		{"missing previous revision", func(event *Event) { event.PreviousRevision = 0 }},
		{"negative previous revision", func(event *Event) { event.PreviousRevision = -1 }},
		{"overflowing previous revision", func(event *Event) { event.PreviousRevision = math.MaxInt64; event.Revision = math.MaxInt64 }},
		{"unchanged revision", func(event *Event) { event.Revision = event.PreviousRevision }},
		{"skipped revision", func(event *Event) { event.Revision = event.PreviousRevision + 2 }},
		{"decreasing revision", func(event *Event) { event.Revision = event.PreviousRevision - 1 }},
		{"missing fingerprint", func(event *Event) { event.ObservationFingerprint = "" }},
		{"short fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("a", 63) }},
		{"long fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("a", 65) }},
		{"uppercase fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("A", 64) }},
		{"nonhex fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("g", 64) }},
		{"control fingerprint", func(event *Event) { event.ObservationFingerprint = strings.Repeat("a", 63) + "\n" }},
		{"path fingerprint", func(event *Event) { event.ObservationFingerprint = "/private/storage/observed-directory" }},
		{"opaque observation handle", func(event *Event) { event.ObservationFingerprint = "opaque-observation-handle" }},
		{"affected count", func(event *Event) { event.Count = 1 }},
		{"terminal state", func(event *Event) { event.State = StateCompleted }},
		{"changed field", func(event *Event) { event.ChangedFields = []Field{FieldName} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := rootBindingActivityEvent()
			test.edit(&event)
			capture := &activityCaptureExecutor{}
			if err := Record(context.Background(), capture, event); !errors.Is(err, ErrInvalidInput) || capture.calls != 0 {
				t.Fatalf("invalid binding facts reached the ordinary executor: calls=%d error=%v", capture.calls, err)
			}
			if err := RecordOwned(rootBindingOwnedCapture{capture: capture}, event); !errors.Is(err, ErrInvalidInput) || capture.calls != 0 {
				t.Fatalf("invalid binding facts reached the protected executor: calls=%d error=%v", capture.calls, err)
			}
		})
	}
	for _, action := range []Action{ActionSettingsUpdated, ActionLibraryCreated, ActionMetadataUpdated, ActionBackupImported} {
		for _, field := range []string{"previous revision", "fingerprint", "both"} {
			t.Run(string(action)+"/"+field, func(t *testing.T) {
				event := rootBindingActivityEvent()
				event.Action, event.Resource.Kind = action, actionResource(action)
				if field == "previous revision" {
					event.ObservationFingerprint = ""
				} else if field == "fingerprint" {
					event.PreviousRevision = 0
				}
				capture := &activityCaptureExecutor{}
				if err := Record(context.Background(), capture, event); !errors.Is(err, ErrInvalidInput) || capture.calls != 0 {
					t.Fatal("an unrelated action retained root binding-only audit facts")
				}
			})
		}
	}
}

func TestRootBindingActivityJSONOmitsNewFieldsForLegacyEvents(t *testing.T) {
	encoded, err := json.Marshal(validationEvent())
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy["PreviousRevision"] != nil || legacy["ObservationFingerprint"] != nil {
		t.Fatal("legacy activity JSON acquired empty root binding fields")
	}
	event := rootBindingActivityEvent()
	encoded, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &current); err != nil {
		t.Fatal(err)
	}
	if string(current["PreviousRevision"]) != "9007199254740993" || string(current["Revision"]) != "9007199254740994" ||
		string(current["ObservationFingerprint"]) != `"`+event.ObservationFingerprint+`"` {
		t.Fatal("root binding JSON did not preserve the exact revisions and canonical fingerprint")
	}
}

type rootBindingActivityRow struct{ encoded []byte }

func (row rootBindingActivityRow) Scan(destinations ...any) error {
	*destinations[0].(*int64) = 1
	*destinations[1].(*[]byte) = row.encoded
	return nil
}

func TestRootBindingActivityQueryRetainsSafeSummaryFacts(t *testing.T) {
	event := rootBindingActivityEvent()
	encoded, err := json.Marshal([]Entry{{ID: 7, Date: time.Date(2026, 9, 12, 1, 2, 3, 0, time.UTC), Event: event, ActorName: "Current Administrator"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := QueryOwned(func(_ string, args ...any) Row {
		if args[2] != string(ActionLibraryRootBindingUpdated) {
			t.Fatal("the root binding action filter did not reach the authorized query")
		}
		return rootBindingActivityRow{encoded: encoded}
	}, QueryOptions{Action: ActionLibraryRootBindingUpdated})
	if err != nil || len(page.Items) != 1 || page.TotalRecordCount != 1 {
		t.Fatalf("query a root binding event: %+v, %v", page, err)
	}
	entry := page.Items[0]
	if entry.PreviousRevision != event.PreviousRevision || entry.Revision != event.Revision || entry.ObservationFingerprint != event.ObservationFingerprint ||
		entry.Actor != event.Actor || entry.Resource != event.Resource || entry.Name != "Library root binding updated" {
		t.Fatal("the activity query discarded explicit root binding facts")
	}
	want := "A registered media directory binding changed from revision 9007199254740993 to 9007199254740994. Observation fingerprint: " + event.ObservationFingerprint + "."
	if entry.Overview != want || strings.Contains(entry.Overview, event.RequestID) || strings.Contains(entry.Overview, event.Resource.ID) {
		t.Fatal("the activity summary did not restrict itself to the committed revision pair and fingerprint")
	}
	event.ObservationFingerprint = "/private/media/observed-directory"
	encoded, err = json.Marshal([]Entry{{ID: 8, Event: event}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := QueryOwned(func(string, ...any) Row { return rootBindingActivityRow{encoded: encoded} }, QueryOptions{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("malformed stored binding facts reached the user-visible activity summary")
	}
}
