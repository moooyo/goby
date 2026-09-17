package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type auxiliaryCatalogJITTxFixture struct {
	pgx.Tx
	setting              string
	rows                 *ownedCallbackRowsFixture
	queryErr, restoreErr error
	restored             bool
}

type auxiliaryCatalogJITRowFixture string

func (row auxiliaryCatalogJITRowFixture) Scan(destinations ...any) error {
	*destinations[0].(*string) = string(row)
	return nil
}

func (tx *auxiliaryCatalogJITTxFixture) QueryRow(context.Context, string, ...any) pgx.Row {
	return auxiliaryCatalogJITRowFixture(tx.setting)
}

func (tx *auxiliaryCatalogJITTxFixture) Exec(_ context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	switch statement {
	case `SELECT set_config('jit', 'off', true)`:
		tx.setting = "off"
	case `SELECT set_config('jit', $1, true)`:
		if tx.rows != nil && tx.rows.closeCalls == 0 {
			return pgconn.CommandTag{}, errors.New("restoration encountered an open snapshot cursor")
		}
		tx.restored = true
		if tx.restoreErr != nil {
			return pgconn.CommandTag{}, tx.restoreErr
		}
		tx.setting = args[0].(string)
	default:
		return pgconn.CommandTag{}, errors.New("unexpected setting statement")
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (tx *auxiliaryCatalogJITTxFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.setting != "off" {
		return nil, errors.New("the snapshot executed with JIT enabled")
	}
	if tx.rows == nil {
		return nil, tx.queryErr
	}
	return tx.rows, tx.queryErr
}

func TestAuxiliaryCatalogSnapshotJITCleanupPreservesFailures(t *testing.T) {
	readFailure := errors.New("snapshot read failed")
	restoreFailure := errors.New("setting restoration failed")
	for _, scenario := range []string{"query", "query with rows", "scan", "iteration", "restore", "scan and restore"} {
		t.Run(scenario, func(t *testing.T) {
			tx := &auxiliaryCatalogJITTxFixture{setting: "on",
				rows: &ownedCallbackRowsFixture{next: func() bool { return false }}}
			wantReadFailure, wantRestoreFailure := scenario != "restore", scenario == "restore" || scenario == "scan and restore"
			switch scenario {
			case "query":
				tx.rows, tx.queryErr = nil, readFailure
			case "query with rows":
				tx.queryErr = readFailure
			case "scan", "scan and restore":
				tx.rows.next = func() bool { return true }
				tx.rows.scan = func(...any) error { return readFailure }
			case "iteration":
				tx.rows.terminal = readFailure
			}
			if wantRestoreFailure {
				tx.restoreErr = restoreFailure
			}
			snapshot, err := readAuxiliaryCatalogRows(context.Background(), tx, []string{"seed"}, nil)
			if err == nil || errors.Is(err, readFailure) != wantReadFailure || errors.Is(err, restoreFailure) != wantRestoreFailure {
				t.Fatalf("cleanup lost the operation or restoration failure: %v", err)
			}
			if !tx.restored || !wantRestoreFailure && tx.setting != "on" {
				t.Fatal("snapshot failure bypassed restoration of the original setting")
			}
			if tx.rows != nil && tx.rows.closeCalls == 0 {
				t.Fatal("snapshot failure retained an open cursor")
			}
			if snapshot.items != nil || snapshot.seeds != nil || snapshot.ids != nil || snapshot.resync {
				t.Fatal("a failed snapshot returned usable partial state")
			}
		})
	}
}

func TestAuxiliaryCatalogSnapshotJITRestoresAfterResync(t *testing.T) {
	for _, scenario := range []string{"row limit", "byte limit"} {
		t.Run(scenario, func(t *testing.T) {
			count := 0
			tx := &auxiliaryCatalogJITTxFixture{setting: "on", rows: &ownedCallbackRowsFixture{}}
			tx.rows.next = func() bool {
				count++
				return count <= maxCatalogChanges+1
			}
			tx.rows.scan = func(destinations ...any) error {
				id := fmt.Sprintf("item-%04d", count)
				values := []any{id, "library", "", false, false, true, true,
					"", "", strings.Repeat("a", 64), true}
				if scenario == "byte limit" {
					values[0] = strings.Repeat("i", catalogLibraryIDBytes-len(id)) + id
					values[1] = strings.Repeat("l", catalogLibraryIDBytes)
					values[2] = strings.Repeat("p", catalogLibraryIDBytes)
				}
				for index, destination := range destinations {
					switch target := destination.(type) {
					case *string:
						*target = values[index].(string)
					case *bool:
						*target = values[index].(bool)
					}
				}
				return nil
			}
			snapshot, err := readAuxiliaryCatalogRows(context.Background(), tx, []string{"seed"}, nil)
			if err != nil || !snapshot.resync || snapshot.items != nil {
				t.Fatalf("an oversized snapshot did not require a complete resynchronization: %+v, %v", snapshot, err)
			}
			if scenario == "row limit" && count != maxCatalogChanges+1 || scenario == "byte limit" && count >= maxCatalogChanges {
				t.Fatal("the snapshot did not stop at the independently exhausted bound")
			}
			if !tx.restored || tx.setting != "on" || tx.rows.closeCalls == 0 {
				t.Fatal("the early resynchronization return left a cursor or a changed setting")
			}
		})
	}
}

func TestAuxiliaryCatalogSnapshotEmptyOrInvalidSeedsNeedNoSession(t *testing.T) {
	for _, ids := range [][]string{nil, {" invalid"}} {
		snapshot, err := readAuxiliaryCatalogRows(context.Background(), nil, ids, nil)
		if err != nil || snapshot.resync != (len(ids) != 0) || len(snapshot.items) != 0 {
			t.Fatalf("empty or invalid seeds changed their pre-query result: %+v, %v", snapshot, err)
		}
	}
}
