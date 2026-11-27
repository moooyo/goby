package systemevents

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDolbyVisionVerificationRequestedIsACommittedEvent(t *testing.T) {
	const name = "DolbyVisionVerificationRequested"
	if !Valid(DolbyVisionVerificationRequested) || string(DolbyVisionVerificationRequested) != name {
		t.Fatal("Dolby Vision verification event is missing from the accepted registry")
	}
	calls := 0
	err := Record(func(_ string, args ...any) (pgconn.CommandTag, error) {
		calls++
		if len(args) != 1 || args[0] != name {
			t.Fatalf("expected only the durable event name, got %v", args)
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}, DolbyVisionVerificationRequested)
	if err != nil || calls != 1 {
		t.Fatalf("recorded %d event updates, error: %v", calls, err)
	}
}
