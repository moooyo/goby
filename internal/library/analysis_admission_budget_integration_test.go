package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestAnalysisAdmissionRefreshesSQLBudgetAndPreservesOwnerAfterTimeout(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	var pid int
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { return tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid) }); err != nil {
		t.Fatal(err)
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock(5050,$1)`, pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- store.WithOwnedTx(ctx, func(tx OwnedTx) error {
			view := tx.(*ownedCallbackTx)
			originalView, originalCatalog := view.ctx, view.catalog.ctx
			short, cancel := context.WithTimeout(view.ctx, 5*time.Second)
			defer cancel()
			view.ctx, view.catalog.ctx = short, short
			defer func() { view.ctx, view.catalog.ctx = originalView, originalCatalog }()
			refresh, _, err := analysisAdmissionSQLBudget(tx)
			if err != nil {
				return err
			}
			if err := refresh(); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE analysis_settings SET preview_quality=81 WHERE id=1`); err != nil {
				return err
			}
			if _, err := tx.Exec(`SELECT pg_sleep(2.6)`); err != nil {
				return err
			}
			if err := refresh(); err != nil {
				return err
			}
			_, err = tx.Exec(`SELECT pg_advisory_xact_lock(5050,$1)`, pid)
			return err
		})
	}()
	timeout := time.NewTimer(6 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for waiting := false; !waiting; {
		if err := pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("no actual later lock wait: %v", err)
		case <-timeout.C:
			t.Fatal("no actual PostgreSQL analysis lock wait observed")
		case <-tick.C:
		}
	}
	select {
	case err := <-done:
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "57014" {
			t.Fatalf("later wait did not use refreshed server budget: %v", err)
		}
	case <-timeout.C:
		t.Fatal("analysis admission wait exhausted the protected owner deadline")
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { var one int; return tx.QueryRow(`SELECT 1`).Scan(&one) }); err != nil {
		t.Fatalf("analysis timeout destroyed reserved owner: %v", err)
	}
	var quality int
	if err := pool.QueryRow(ctx, `SELECT preview_quality FROM analysis_settings WHERE id=1`).Scan(&quality); err != nil || quality != 80 {
		t.Fatalf("timed out analysis transaction partly committed: %d %v", quality, err)
	}
}
