package backuppg

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type configurationBatchTx struct {
	pgx.Tx
	batches   []*pgx.Batch
	results   []*configurationBatchResults
	failAt    int
	closeErr  error
	rollbacks int
}

func (tx *configurationBatchTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	result := &configurationBatchResults{failAt: tx.failAt, closeErr: tx.closeErr, contextErr: ctx.Err()}
	tx.batches = append(tx.batches, batch)
	tx.results = append(tx.results, result)
	return result
}

func (tx *configurationBatchTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type configurationBatchResults struct {
	pgx.BatchResults
	failAt     int
	closeErr   error
	contextErr error
	consumed   int
	closed     int
}

func (result *configurationBatchResults) Exec() (pgconn.CommandTag, error) {
	result.consumed++
	if result.contextErr != nil {
		return pgconn.CommandTag{}, result.contextErr
	}
	if result.consumed == result.failAt {
		return pgconn.CommandTag{}, errors.New("configuration statement failed")
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (result *configurationBatchResults) Close() error {
	result.closed++
	return result.closeErr
}

type configurationDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (ctx configurationDeadlineContext) Deadline() (time.Time, bool) { return ctx.deadline, true }

func configurationBatchSettings(t *testing.T, batch *pgx.Batch) map[string]string {
	t.Helper()
	settings := make(map[string]string)
	for _, query := range batch.QueuedQueries {
		if query.SQL != `SELECT pg_catalog.set_config($1,$2,true)` || len(query.Arguments) != 2 {
			t.Fatal("configuration lost its qualified parameterized transaction-local statement")
		}
		name, nameOK := query.Arguments[0].(string)
		value, valueOK := query.Arguments[1].(string)
		if !nameOK || !valueOK {
			t.Fatal("configuration batch lost its text parameters")
		}
		if _, duplicate := settings[name]; duplicate {
			t.Fatal("configuration batch repeated a setting")
		}
		settings[name] = value
	}
	return settings
}

func TestConfigureTransactionBatchesAllSettingsAndRecalculatesBudget(t *testing.T) {
	tx := &configurationBatchTx{}
	for _, remaining := range []time.Duration{time.Minute, 10 * time.Second} {
		ctx := configurationDeadlineContext{Context: context.Background(), deadline: time.Now().Add(remaining)}
		if err := configureTransaction(ctx, tx, "custom_schema"); err != nil {
			t.Fatal(err)
		}
	}
	var previousTimeout int64
	for index, batch := range tx.batches {
		settings := configurationBatchSettings(t, batch)
		if len(settings) != 11 {
			t.Fatalf("configuration set %d options, want all 11", len(settings))
		}
		for name, want := range map[string]string{
			"search_path": `pg_catalog,"custom_schema"`, "statement_timeout": "0", "TimeZone": "UTC",
			"DateStyle": "ISO, YMD", "IntervalStyle": "postgres", "bytea_output": "hex", "extra_float_digits": "3",
			"client_encoding": "UTF8", "row_security": "off", "standard_conforming_strings": "on",
		} {
			if settings[name] != want {
				t.Errorf("configuration %s = %q, want %q", name, settings[name], want)
			}
		}
		timeout, err := strconv.ParseInt(settings["idle_in_transaction_session_timeout"], 10, 64)
		if err != nil || timeout <= 1000 || index > 0 && timeout >= previousTimeout {
			t.Fatal("configuration reused a previous deadline budget")
		}
		previousTimeout = timeout
		if tx.results[index].consumed != 11 || tx.results[index].closed != 1 || tx.rollbacks != 0 {
			t.Fatal("configuration did not retire its entire batch independently of transaction ownership")
		}
	}
}

func TestConfigureTransactionRetiresFailedBatchesBeforeCallerRollback(t *testing.T) {
	for _, test := range []struct {
		name     string
		failAt   int
		closeErr error
	}{
		{"middle_statement", 6, nil},
		{"last_statement", 11, nil},
		{"close", 0, errors.New("configuration close failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &configurationBatchTx{failAt: test.failAt, closeErr: test.closeErr}
			if err := configureTransaction(context.Background(), tx, "public"); !errors.Is(err, ErrDatabase) {
				t.Fatalf("configuration failure changed its error classification: %v", err)
			}
			if len(tx.batches) != 1 || tx.results[0].consumed != 11 || tx.results[0].closed != 1 || tx.rollbacks != 0 {
				t.Fatal("failed configuration left unread results or took over caller rollback")
			}
		})
	}
}

func TestConfigureTransactionRejectsUnpublishedExpiredDeadline(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name    string
		ctx     context.Context
		schema  string
		want    error
		batches int
	}{
		{"pending_deadline", configurationDeadlineContext{context.Background(), time.Now().Add(-time.Second)}, "public", context.DeadlineExceeded, 0},
		{"published_cancellation", configurationDeadlineContext{cancelled, time.Now().Add(-time.Second)}, "public", context.Canceled, 0},
		{"schema_precedes_deadline", configurationDeadlineContext{context.Background(), time.Now().Add(-time.Second)}, "invalid schema", ErrConfiguration, 0},
		{"cancelled_without_deadline", cancelled, "public", ErrDatabase, 1},
		{"cancelled_live_deadline", configurationDeadlineContext{cancelled, time.Now().Add(time.Minute)}, "public", ErrDatabase, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &configurationBatchTx{}
			if err := configureTransaction(test.ctx, tx, test.schema); !errors.Is(err, test.want) || len(tx.batches) != test.batches {
				t.Fatalf("deadline configuration returned %v and sent %d batches, want %v and %d", err, len(tx.batches), test.want, test.batches)
			}
		})
	}
}
