package activity

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type activityCaptureExecutor struct {
	calls int
	args  []any
	err   error
}

func (executor *activityCaptureExecutor) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	executor.calls++
	executor.args = args
	return pgconn.NewCommandTag("INSERT 0 1"), executor.err
}

func validationEvent() Event {
	return Event{Action: ActionSettingsUpdated, Severity: SeverityInfo, Source: SourceNative,
		Actor:    Actor{Kind: ActorUser, ID: "historical-administrator", CredentialID: "existing-session-id"},
		Resource: Resource{Kind: ResourceSettings, ID: "1"}, Revision: 2,
		ChangedFields: []Field{FieldMaxWidth, FieldServerName}}
}

func TestRecordRejectsValuesOutsideTheTypedActivityContract(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Event)
	}{
		{"unknown action", func(event *Event) { event.Action = Action("request.body") }},
		{"unknown severity", func(event *Event) { event.Severity = Severity("notice") }},
		{"unknown source", func(event *Event) { event.Source = Source("external") }},
		{"implicit actor", func(event *Event) { event.Actor = Actor{} }},
		{"system impersonation", func(event *Event) { event.Actor.Kind = ActorSystem }},
		{"missing user identity", func(event *Event) { event.Actor.ID = "" }},
		{"missing key identity", func(event *Event) { event.Actor = Actor{Kind: ActorApplicationKey} }},
		{"path as identifier", func(event *Event) { event.Resource.ID = "/private/media/movie.mkv" }},
		{"control as identifier", func(event *Event) { event.Actor.ID = "user\ncredential" }},
		{"oversized identifier", func(event *Event) { event.Resource.ID = strings.Repeat("a", MaxIdentifierBytes+1) }},
		{"resource mismatch", func(event *Event) { event.Resource.Kind = ResourceItem }},
		{"negative revision", func(event *Event) { event.Revision = -1 }},
		{"negative count", func(event *Event) { event.Count = -1 }},
		{"unexpected state", func(event *Event) { event.State = StateCompleted }},
		{"arbitrary field value", func(event *Event) { event.ChangedFields = []Field{"ServerName=Private Value"} }},
		{"credential field", func(event *Event) { event.ChangedFields = []Field{"Password"} }},
		{"unrelated known field", func(event *Event) { event.ChangedFields = []Field{FieldOverview} }},
		{"duplicate field", func(event *Event) { event.ChangedFields = []Field{FieldMaxWidth, FieldMaxWidth} }},
		{"oversized fields", func(event *Event) { event.ChangedFields = make([]Field, MaxChangedFields+1) }},
		{"terminal without state", func(event *Event) {
			event.Action, event.Resource.Kind, event.ChangedFields = ActionTaskFinished, ResourceTaskRun, nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := validationEvent()
			test.change(&event)
			executor := &activityCaptureExecutor{}
			if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid event returned %v, want ErrInvalidInput", err)
			}
			if executor.calls != 0 {
				t.Fatal("invalid event reached the database executor")
			}
		})
	}
}

func TestRecordKeepsHistoricalIdentifiersAndExplicitSystemFacts(t *testing.T) {
	for _, event := range []Event{
		validationEvent(),
		{Action: ActionApplicationKeyCreated, Source: SourceEmby,
			Actor:    Actor{Kind: ActorApplicationKey, ID: "123", CredentialID: "historical-key-credential"},
			Resource: Resource{Kind: ResourceApplicationKey, ID: "124"}},
		{Action: ActionTaskFinished, Source: SourceSystem, Actor: Actor{Kind: ActorSystem},
			Resource: Resource{Kind: ResourceTaskRun, ID: "retained-run:1"}, State: StateInterrupted},
	} {
		executor := &activityCaptureExecutor{}
		if err := Record(context.Background(), executor, event); err != nil {
			t.Fatalf("record explicit historical event: %v", err)
		}
		if executor.calls != 1 {
			t.Fatal("valid event must issue exactly one INSERT")
		}
	}
}

func TestRecordCopiesFieldNamesAndPreservesTransactionErrors(t *testing.T) {
	event := validationEvent()
	executor := &activityCaptureExecutor{}
	if err := Record(context.Background(), executor, event); err != nil {
		t.Fatal(err)
	}
	event.ChangedFields[0] = FieldOverview
	if fields := executor.args[len(executor.args)-1]; !reflect.DeepEqual(fields, []string{"MaxWidth", "ServerName"}) {
		t.Fatalf("recorded field names changed with caller data: %v", fields)
	}
	sentinel := errors.New("owned transaction rejected activity")
	executor = &activityCaptureExecutor{err: sentinel}
	if err := Record(context.Background(), executor, validationEvent()); !errors.Is(err, sentinel) {
		t.Fatal("database error lost its identity before transaction rollback")
	}
}

func TestQueryRejectsInvalidBoundsAndFiltersBeforeReading(t *testing.T) {
	tooLate := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	for _, options := range []QueryOptions{
		{StartIndex: -1}, {Limit: -1}, {Limit: MaxPageLimit + 1},
		{Action: "arbitrary-action"}, {Severity: "Warning"}, {ActorID: "../private"},
		{MinDate: &tooLate},
	} {
		called := false
		_, err := QueryOwned(func(string, ...any) Row {
			called = true
			return nil
		}, options)
		if !errors.Is(err, ErrInvalidInput) || called {
			t.Fatalf("invalid query returned %v and database access %t", err, called)
		}
	}
}
