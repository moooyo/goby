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

func TestBackupRestoreActivityRequiresItsExactTerminalState(t *testing.T) {
	for _, fixture := range []struct {
		action   Action
		resource ResourceKind
		states   []State
	}{
		{ActionBackupRequested, ResourceBackup, []State{""}},
		{ActionBackupCancelRequested, ResourceBackup, []State{""}},
		{ActionBackupFinished, ResourceBackup, []State{StateCompleted, StateFailed, StateCancelled, StateInterrupted}},
		{ActionBackupImported, ResourceBackup, []State{""}},
		{ActionBackupDeleteRequested, ResourceBackup, []State{""}},
		{ActionBackupDeleted, ResourceBackup, []State{""}},
		{ActionBackupDownloaded, ResourceBackup, []State{""}},
		{ActionRestoreRequested, ResourceRestore, []State{""}},
		{ActionRestorePlanned, ResourceRestore, []State{""}},
		{ActionRestoreApplyRequested, ResourceRestore, []State{""}},
		{ActionRestoreApplied, ResourceRestore, []State{StateCompleted}},
		{ActionRestoreRollbackRequested, ResourceRestore, []State{""}},
		{ActionRestoreCancelRequested, ResourceRestore, []State{""}},
		{ActionRestoreFailed, ResourceRestore, []State{StateFailed}},
	} {
		for _, state := range []State{"", StateCompleted, StateFailed, StateCancelled, StateInterrupted, "unknown"} {
			t.Run(string(fixture.action)+"/"+string(state), func(t *testing.T) {
				allowed := false
				for _, permitted := range fixture.states {
					allowed = allowed || state == permitted
				}
				event := Event{Action: fixture.action, Source: SourceSystem, Actor: Actor{Kind: ActorSystem},
					Resource: Resource{Kind: fixture.resource, ID: "persisted-operation-identity"}, State: state}
				executor := &activityCaptureExecutor{}
				err := Record(context.Background(), executor, event)
				if allowed {
					if err != nil || executor.calls != 1 {
						t.Fatalf("allowed backup or restore event: calls=%d error=%v", executor.calls, err)
					}
				} else if !errors.Is(err, ErrInvalidInput) || executor.calls != 0 {
					t.Fatalf("invalid terminal state reached storage: calls=%d error=%v", executor.calls, err)
				}
			})
		}
	}
}

func TestBackupRestoreActivityRejectsAllChangedFieldsAndPrivateValues(t *testing.T) {
	for _, action := range []Action{ActionBackupRequested, ActionBackupCancelRequested, ActionBackupFinished,
		ActionBackupImported, ActionBackupDeleteRequested, ActionBackupDeleted, ActionBackupDownloaded,
		ActionRestoreRequested, ActionRestorePlanned, ActionRestoreApplyRequested, ActionRestoreApplied,
		ActionRestoreRollbackRequested, ActionRestoreCancelRequested, ActionRestoreFailed} {
		t.Run(string(action), func(t *testing.T) {
			event := Event{Action: action, Source: SourceNative,
				Actor:    Actor{Kind: ActorUser, ID: "administrator", CredentialID: "existing-session"},
				Resource: Resource{Kind: actionResource(action), ID: "persisted-operation-identity"}}
			switch action {
			case ActionBackupFinished, ActionRestoreApplied:
				event.State = StateCompleted
			case ActionRestoreFailed:
				event.State = StateFailed
			}
			for _, field := range []Field{FieldName, FieldServerName, FieldOverrides,
				"Passphrase", "MasterKey", "DatabaseURL", "ArchivePath", "Passphrase=synthetic-private-value"} {
				event.ChangedFields = []Field{field}
				executor := &activityCaptureExecutor{}
				if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) || executor.calls != 0 {
					t.Fatalf("backup or restore field reached storage: calls=%d error=%v", executor.calls, err)
				}
			}
			event.ChangedFields = nil
			for _, identifier := range []string{"/private/backups/archive.enc", "postgres://user:secret@database/goby", "private passphrase"} {
				event.Resource.ID = identifier
				executor := &activityCaptureExecutor{}
				if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) || executor.calls != 0 {
					t.Fatalf("private value used as a resource identity reached storage: calls=%d error=%v", executor.calls, err)
				}
			}
		})
	}
	name, overview := description(ActionBackupDownloaded, "")
	if name != "Backup download prepared" || overview != "A backup was prepared for an authorized download." {
		t.Fatal("download activity must describe preparation without promising complete delivery")
	}
}

func TestBackupRestoreAdmissionDescriptionsStateOnlyPersistedRequests(t *testing.T) {
	for _, fixture := range []struct {
		action   Action
		name     string
		overview string
	}{
		{ActionBackupDeleteRequested, "Backup deletion requested", "A backup deletion request was persisted."},
		{ActionRestoreRequested, "Restore requested", "A restore planning request was persisted."},
		{ActionRestoreApplyRequested, "Restore application requested", "A restore application request was persisted."},
		{ActionRestoreRollbackRequested, "Restore rollback requested", "A restore rollback request was persisted."},
		{ActionRestoreCancelRequested, "Restore cancellation requested", "A restore cancellation request was persisted."},
	} {
		t.Run(string(fixture.action), func(t *testing.T) {
			name, overview := description(fixture.action, "")
			if name != fixture.name || overview != fixture.overview {
				t.Fatalf("admission description = %q / %q, want a fixed persisted-request fact", name, overview)
			}
		})
	}
	for _, fixture := range []struct {
		action   Action
		state    State
		name     string
		overview string
	}{
		{ActionBackupDeleted, "", "Backup deleted", "A backup was removed from the managed backup inventory."},
		{ActionRestorePlanned, "", "Restore planned", "A restore plan was prepared."},
		{ActionRestoreApplied, StateCompleted, "Restore applied", "A restored database was activated."},
	} {
		t.Run(string(fixture.action), func(t *testing.T) {
			name, overview := description(fixture.action, fixture.state)
			if name != fixture.name || overview != fixture.overview {
				t.Fatalf("completed phase description = %q / %q, want the existing completed fact", name, overview)
			}
		})
	}
}
