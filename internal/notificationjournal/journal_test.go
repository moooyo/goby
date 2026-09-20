package notificationjournal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

type journalExecutor struct {
	calls int
	err   error
	args  []any
}

func (e *journalExecutor) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	e.calls++
	e.args = args
	return pgconn.NewCommandTag("SELECT 1"), e.err
}
func TestNotificationJournalPreservesTypedSourceAndMapsCapacity(t *testing.T) {
	exec := &journalExecutor{}
	if err := RecordUserData(context.Background(), exec, "owner", Reference{Kind: "Item", ID: "123"}, true); err != nil || exec.calls != 1 || exec.args[1] != "UserDataInvalidated" || exec.args[2] != "owner" || exec.args[4] != true {
		t.Fatal("journal lost source ownership or the caller transaction")
	}
	for _, message := range []string{"notification_source_capacity", "notification_source_scope_required"} {
		exec.err = &pgconn.PgError{Code: "P0001", Message: message}
		if err := RecordCatalog(context.Background(), exec, NewID(), []Reference{{Kind: "Item", ID: "item", LibraryID: "library"}}, false); !errors.Is(err, ErrCapacity) {
			t.Fatal("capacity is not explicitly retryable")
		}
	}
	exec.err = errors.New("private-sql-detail")
	if err := RecordUserResync(context.Background(), exec, "owner"); err != ErrJournal {
		t.Fatal("journal exposed a database error")
	}
}
func TestNotificationStoredReferenceNamespacesAndShape(t *testing.T) {
	for _, test := range []struct {
		kind, owner, raw string
		delivery, valid  bool
	}{
		{"CatalogInvalidated", "", `[{"Kind":"Item","Id":"123","LibraryId":"lib"}]`, false, true},
		{"UserDataInvalidated", "owner", `[{"Kind":"Item","Id":"00123"}]`, false, true},
		{"UserDataInvalidated", "owner", `[{"Kind":"Entity","Id":"123"}]`, false, true},
		{"UserDataInvalidated", "owner", `[{"Kind":"Entity","Id":"00123"}]`, false, false},
		{"CatalogInvalidated", "", `[{"Kind":"Item","Id":"x"}]`, false, false},
		{"UserDataInvalidated", "", `[]`, false, false},
		{"UserDataInvalidated", "owner", `[{"Kind":"Item","Id":"x","LibraryId":"lib"}]`, false, false},
		{"CatalogInvalidated", "", `[{"Kind":"Item","Id":"x","LibraryId":"lib","Secret":"bad"}]`, false, false},
		{"ResyncRequired", "", `[]`, true, true},
		{"Test", "", `[{"Kind":"Item","Id":"x"}]`, true, false},
		{"CatalogInvalidated", "", `null`, false, false},
		{"CatalogInvalidated", "", `[{"kind":"Item","Id":"x","LibraryId":"lib"}]`, false, false},
	} {
		if got := ValidateReferences(test.kind, test.owner, []byte(test.raw), test.delivery) == nil; got != test.valid {
			t.Fatalf("reference validation changed namespace/shape: %+v", test)
		}
	}
}
func TestNotificationTransportValidationRejectsSecretURLsAndUnboundedNetworks(t *testing.T) {
	for _, endpoint := range []string{"http://example.test/events", "https://user:secret@example.test/events", "https://example.test/events?token=x", "https://example.test/events#x", "https://example.test:0/events"} {
		if ValidateTransport(endpoint, []string{}, true, true) == nil {
			t.Fatal("unsafe endpoint admitted")
		}
	}
	if ValidateTransport("https://localhost:8443/events", []string{"127.0.0.1/32"}, true, true) != nil {
		t.Fatal("explicit trusted deployment address rejected")
	}
	if ValidateTransport("", []string{}, false, false) != nil || ValidateTransport("", []string{}, true, true) == nil || ValidateTransport("https://host/events", nil, false, false) == nil {
		t.Fatal("enable/clear/null boundaries changed")
	}
	if ValidateTransport("https://host/events", []string{"10.0.0.1/24"}, true, true) == nil || ValidateEvents([]string{"CatalogInvalidated", "CatalogInvalidated"}) == nil {
		t.Fatal("ambiguous network/event configuration accepted")
	}
}
