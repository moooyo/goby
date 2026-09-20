package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

func selectedPhase3ValidImport(t *testing.T) selectedPhase3Import {
	t.Helper()
	record := selectedPhase3Import{seriesID: "retained-series", action: "replace", current: true,
		parserVersion: 1, source: library.EpisodeRosterSourceInput{Key: "local-roster", Label: "Explicit source", Revision: "edition-1"},
		payload: []byte(`{"ParserVersion":1,"Source":{"Key":"local-roster","Label":"Explicit source","Revision":"edition-1"},"Entries":[{"Key":"unknown-date","SeasonNumber":1,"EpisodeNumber":2,"Name":""},{"Key":"known-date","SeasonNumber":1,"EpisodeNumber":4,"Name":"Fourth episode","PremiereDate":"2020-01-01"}]}`)}
	digest := sha256.Sum256(record.payload)
	record.payloadHash = hex.EncodeToString(digest[:])
	facts := []selectedPhase3Fact{
		{ID: library.ExpectedEpisodeID(record.seriesID, record.source.Key, "unknown-date"), SourceKey: record.source.Key, EntryKey: "unknown-date", SeasonNumber: 1, EpisodeNumber: 2},
		{ID: library.ExpectedEpisodeID(record.seriesID, record.source.Key, "known-date"), SourceKey: record.source.Key, EntryKey: "known-date", SeasonNumber: 1, EpisodeNumber: 4, Name: "Fourth episode", PremiereDate: "2020-01-01"},
	}
	record.facts, _ = json.Marshal(facts)
	return record
}

func TestSelectedPhase3ImportBindsCanonicalSourceAndExactFacts(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		mutate func(*selectedPhase3Import)
		valid  bool
	}{
		{"current_complete", func(*selectedPhase3Import) {}, true},
		{"retired_subset", func(record *selectedPhase3Import) { record.current = false; record.facts = []byte(`[]`) }, true},
		{"withdrawn_history", func(record *selectedPhase3Import) {
			record.current = false
			record.action = "withdraw"
			record.facts = []byte(`[]`)
		}, true},
		{"payload_hash", func(record *selectedPhase3Import) { record.payloadHash = "wrong" }, false},
		{"parser_version", func(record *selectedPhase3Import) { record.parserVersion++ }, false},
		{"source_label", func(record *selectedPhase3Import) { record.source.Label = "Changed source" }, false},
		{"source_revision", func(record *selectedPhase3Import) { record.source.Revision = "changed" }, false},
		{"noncanonical_payload", func(record *selectedPhase3Import) {
			record.payload = append([]byte(" "), record.payload...)
			digest := sha256.Sum256(record.payload)
			record.payloadHash = hex.EncodeToString(digest[:])
		}, false},
		{"current_coverage", func(record *selectedPhase3Import) { record.facts = []byte(`[]`) }, false},
		{"null_facts", func(record *selectedPhase3Import) { record.current = false; record.facts = []byte(`null`) }, false},
		{"withdrawn_facts", func(record *selectedPhase3Import) { record.current = false; record.action = "withdraw" }, false},
		{"withdrawn_current", func(record *selectedPhase3Import) { record.action = "withdraw"; record.facts = []byte(`[]`) }, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			record := selectedPhase3ValidImport(t)
			fixture.mutate(&record)
			if got := validSelectedPhase3Import(record); got != fixture.valid {
				t.Fatalf("explicit roster semantic admission = %t, want %t", got, fixture.valid)
			}
		})
	}
	for _, field := range []string{"stable_id", "source_key", "entry_key", "season", "episode", "name", "unknown_date", "known_date", "duplicate_entry"} {
		t.Run(field, func(t *testing.T) {
			record := selectedPhase3ValidImport(t)
			var facts []selectedPhase3Fact
			if err := json.Unmarshal(record.facts, &facts); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "stable_id":
				facts[0].ID = library.ExpectedEpisodeID("other-series", record.source.Key, facts[0].EntryKey)
			case "source_key":
				facts[0].SourceKey = "other-source"
			case "entry_key":
				facts[0].EntryKey = "other-entry"
			case "season":
				facts[0].SeasonNumber++
			case "episode":
				facts[0].EpisodeNumber++
			case "name":
				facts[0].Name = "Inferred title"
			case "unknown_date":
				facts[0].PremiereDate = "2020-01-01"
			case "known_date":
				facts[1].PremiereDate = ""
			case "duplicate_entry":
				facts[1] = facts[0]
			}
			record.facts, _ = json.Marshal(facts)
			if validSelectedPhase3Import(record) {
				t.Fatal("a changed or inferred fact passed its immutable payload witness")
			}
		})
	}
}

type selectedPhase3StateTx struct {
	themeStateTestTx
	rows       *selectedPhase3StateRows
	queryErr   error
	rowQueries int
}

func (tx *selectedPhase3StateTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	tx.rowQueries++
	return tx.rows, tx.queryErr
}

type selectedPhase3StateRows struct {
	pgx.Rows
	err    error
	closed bool
}

func (*selectedPhase3StateRows) Next() bool      { return false }
func (rows *selectedPhase3StateRows) Err() error { return rows.err }
func (rows *selectedPhase3StateRows) Close()     { rows.closed = true }

func TestValidateSelectedPhase3StatePreservesVersionAndFailureBoundaries(t *testing.T) {
	for _, version := range []int64{43, 44, 45, 46} {
		t.Run(fmt.Sprintf("schema_%d", version), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := validateSelectedPhase3State(ctx, nil, version); !errors.Is(err, context.Canceled) {
				t.Fatalf("lost caller cancellation: %v", err)
			}
			if version < 46 {
				if err := validateSelectedPhase3State(context.Background(), nil, version); err != nil {
					t.Fatalf("queried roster state before migration: %v", err)
				}
			} else if err := validateSelectedPhase3State(context.Background(), nil, version); !errors.Is(err, ErrDatabase) {
				t.Fatalf("nil transaction = %v", err)
			}
		})
	}
	for _, fixture := range []struct {
		name                        string
		row                         themeStateTestRow
		queryErr, terminalErr, want error
		queries                     int
	}{
		{name: "valid_empty", row: themeStateTestRow{valid: true}, queries: 1},
		{name: "invalid_relations", row: themeStateTestRow{}, want: ErrSchema},
		{name: "relations_read", row: themeStateTestRow{err: errors.New("unavailable")}, want: ErrDatabase},
		{name: "payload_read", row: themeStateTestRow{valid: true}, queryErr: errors.New("unavailable"), want: ErrDatabase, queries: 1},
		{name: "payload_iteration", row: themeStateTestRow{valid: true}, terminalErr: errors.New("unavailable"), want: ErrDatabase, queries: 1},
		{name: "payload_deadline", row: themeStateTestRow{valid: true}, terminalErr: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded, queries: 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			rows := &selectedPhase3StateRows{err: fixture.terminalErr}
			tx := &selectedPhase3StateTx{themeStateTestTx: themeStateTestTx{row: fixture.row}, rows: rows, queryErr: fixture.queryErr}
			if err := validateSelectedPhase3State(context.Background(), tx, 46); !errors.Is(err, fixture.want) {
				t.Fatalf("roster failure classification = %v, want %v", err, fixture.want)
			}
			wantRelations := 1
			if fixture.want == nil {
				wantRelations++
			}
			if tx.queries != wantRelations || tx.rowQueries != fixture.queries {
				t.Fatal("the semantic gate read past its first failure")
			}
			if fixture.queries != 0 && fixture.queryErr == nil && !rows.closed {
				t.Fatal("roster validation retained its row stream")
			}
		})
	}
}
