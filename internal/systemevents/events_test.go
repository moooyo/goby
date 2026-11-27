package systemevents

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIngestRequestsAreCommittedEvents(t *testing.T) {
	for _, test := range []struct {
		name  string
		event Event
	}{
		{"DolbyVisionVerificationRequested", DolbyVisionVerificationRequested},
		{"NFORecoveryRequested", NFORecoveryRequested},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !Valid(test.event) || string(test.event) != test.name {
				t.Fatal("event is missing from the accepted registry")
			}
			calls := 0
			err := Record(func(_ string, args ...any) (pgconn.CommandTag, error) {
				calls++
				if len(args) != 1 || args[0] != test.name {
					t.Fatalf("expected only the durable event name, got %v", args)
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}, test.event)
			if err != nil || calls != 1 {
				t.Fatalf("recorded %d event updates, error: %v", calls, err)
			}
		})
	}
}
