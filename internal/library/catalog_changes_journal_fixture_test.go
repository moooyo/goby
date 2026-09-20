package library

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/notificationjournal"
)

type catalogJournalFixtureCall struct {
	mutationID string
	refs       []notificationjournal.Reference
	resync     bool
}

// These tests exercise in-memory catalog aggregation, not PostgreSQL delivery
// admission. The explicit executor models the successful no-recipient journal
// gate while still requiring the production SQL boundary and a live context.
// The embedded transaction supplies the rest of pgx.Tx's interface: invoking
// anything other than Exec is a fixture error, not a permissive database mock.
type catalogJournalFixtureTx struct {
	pgx.Tx
	t     *testing.T
	calls []catalogJournalFixtureCall
	err   error
}

func newCatalogAggregationTestTx(t *testing.T) *ownedTx {
	t.Helper()
	return &ownedTx{Tx: &catalogJournalFixtureTx{t: t}, ctx: context.Background()}
}

func (fixture *catalogJournalFixtureTx) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	fixture.t.Helper()
	if ctx == nil || ctx.Err() != nil {
		fixture.t.Fatal("catalog aggregation journal requires a live transaction context")
	}
	const expected = `SELECT goby_record_notification_source($1,$2,NULLIF($3,''),$4::jsonb,$5,$6)`
	if statement != expected || len(args) != 6 {
		fixture.t.Fatal("catalog aggregation fixture received an unexpected database operation")
	}
	id, validID := args[0].(string)
	decoded, err := hex.DecodeString(id)
	if !validID || err != nil || len(id) != 32 || len(decoded) != 16 || args[1] != "CatalogInvalidated" || args[2] != "" || args[4] != false {
		fixture.t.Fatal("catalog journal lost its mutation identity or source ownership")
	}
	raw, validJSON := args[3].([]byte)
	resync, validResync := args[5].(bool)
	var refs []notificationjournal.Reference
	if !validJSON || !validResync || json.Unmarshal(raw, &refs) != nil || refs == nil || len(refs) > 4096 {
		fixture.t.Fatal("catalog journal did not use a bounded typed source batch")
	}
	fixture.calls = append(fixture.calls, catalogJournalFixtureCall{mutationID: id, refs: refs, resync: resync})
	return pgconn.NewCommandTag("SELECT 1"), fixture.err
}

func TestCatalogChangesPropagateJournalAdmissionFailure(t *testing.T) {
	tx := newCatalogAggregationTestTx(t)
	fixture := tx.Tx.(*catalogJournalFixtureTx)
	fixture.err = &pgconn.PgError{Code: "P0001", Message: "notification_source_capacity"}
	change := CatalogChange{Kind: CatalogUpdated, ItemID: "item", LibraryID: "library", ParentID: "parent"}
	if err := recordCatalogChanges(tx, change); !errors.Is(err, notificationjournal.ErrCapacity) {
		t.Fatalf("catalog aggregation swallowed its durable journal admission failure: %v", err)
	}
	if len(fixture.calls) != 1 || len(fixture.calls[0].refs) != 2 || tx.notificationJournalRecorded {
		t.Fatal("failed journal admission was marked as recorded or skipped its source scopes")
	}
	for _, ref := range fixture.calls[0].refs {
		if ref.LibraryID != change.LibraryID || ref.SourceID != change.ItemID {
			t.Fatal("journal admission lost the changed-item authority for its parent")
		}
	}
}
