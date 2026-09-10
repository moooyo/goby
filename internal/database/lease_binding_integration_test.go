package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func TestDeploymentLeaseTransactionBinding(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		t.Fatalf("acquire owned lease: %v", err)
	}
	cleanupDeploymentLease(t, lease)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin transaction on leased pool")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if !lease.ProtectsTransaction(pool, tx) {
		t.Fatal("matching declared connection and server peer rejected")
	}
	if lease.ProtectsTransaction(nil, tx) || lease.ProtectsTransaction(pool, nil) {
		t.Fatal("missing pool or transaction accepted")
	}
	var missing *database.Lease
	if missing.ProtectsTransaction(pool, tx) {
		t.Fatal("missing lease accepted transaction")
	}
	other, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal("create second pool for the same owned database")
	}
	defer other.Close()
	otherTx, err := other.Begin(ctx)
	if err != nil {
		t.Fatal("begin same-database transaction")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = otherTx.Rollback(cleanup)
	}()
	if lease.ProtectsTransaction(other, otherTx) {
		t.Fatal("different pool bypassed the explicit lease/pool boundary")
	}
	if !lease.ProtectsTransaction(pool, otherTx) {
		t.Fatal("same declared database instance was mistaken for a foreign server")
	}
	if err := lease.Close(); err != nil {
		t.Fatal("close owned lease")
	}
	if lease.ProtectsTransaction(pool, tx) || lease.ProtectsTransaction(pool, otherTx) {
		t.Fatal("closed lease retained transaction authority")
	}
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var one int
	if otherTx.QueryRow(readCtx, `SELECT 1`).Scan(&one) != nil || one != 1 {
		t.Fatal("binding checks ended a caller-owned transaction")
	}
}
