package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/storagebinding"
)

func TestRootBindingStateIntegrationAcceptsPersistedDocumentsWithoutFilesystem(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	rootBindingStateTestSeed(t, ctx, pool)
	// No fixture directory is created. Persisted identities remain valid input
	// even when their storage and the original approving user are unavailable.
	var users int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil || users != 0 {
		t.Fatalf("binding fixture unexpectedly requires an approving user: users=%d error=%v", users, err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin read-only binding validation: %v", err)
	}
	rootBindingStateTestRollback(t, tx)
	before := rootBindingStateTestRowsSnapshot(t, ctx, tx)
	if err := database.ValidateRootBindingState(ctx, tx, 28); err != nil {
		t.Fatalf("complete persisted bindings required live storage or row repair: %v", err)
	}
	if after := rootBindingStateTestRowsSnapshot(t, ctx, tx); after != before {
		t.Fatal("successful validation changed persisted root state")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit read-only binding validation: %v", err)
	}
}

func TestRootBindingStateIntegrationRejectsSemanticCorruptionWithoutRepair(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	rootBindingStateTestSeed(t, ctx, pool)
	for _, test := range []struct {
		name      string
		statement string
		arguments []any
	}{
		{name: "incomplete document", statement: `UPDATE library_roots SET storage_binding = '{}'::jsonb WHERE id = 'bound-root'`},
		{name: "unknown field", statement: `UPDATE library_roots SET storage_binding = storage_binding || '{"unexpected":true}'::jsonb WHERE id = 'bound-root'`},
		{name: "unknown identity profile", statement: `UPDATE library_roots SET storage_binding = jsonb_set(storage_binding, '{anchor,profile}', '"future-profile"'::jsonb) WHERE id = 'bound-root'`},
		{name: "mapping outside approved anchor", statement: `UPDATE library_roots SET storage_binding = jsonb_set(storage_binding, '{mapping,registered_path}', '"/outside-anchor/library"'::jsonb) WHERE id = 'bound-root'`},
		{name: "approved path mismatch", statement: `UPDATE library_roots SET allowed_path = allowed_path || '-other' WHERE id = 'bound-root'`},
		{name: "registered path mismatch", statement: `UPDATE library_roots SET path = path || '-other' WHERE id = 'bound-root'`},
		{name: "relative traversal mismatch", statement: `UPDATE library_roots SET relative_path = 'elsewhere' WHERE id = 'bound-root'`},
		{name: "empty nested relative path", statement: `UPDATE library_roots SET relative_path = '' WHERE id = 'bound-root'`},
		{name: "noncanonical anchor-relative path", statement: `UPDATE library_roots SET relative_path = './' WHERE id = 'anchor-root'`},
		{
			name:      "oversized approved path",
			statement: `UPDATE library_roots SET allowed_path = $1 WHERE id = 'bound-root'`,
			arguments: []any{rootBindingStateTestMaximumPath() + "x"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin semantic corruption fixture: %v", err)
			}
			rootBindingStateTestRollback(t, tx)
			if _, err := tx.Exec(ctx, test.statement, test.arguments...); err != nil {
				t.Fatalf("fixture did not reach semantic validation through SQL constraints: %v", err)
			}
			before := rootBindingStateTestRowsSnapshot(t, ctx, tx)
			if err := database.ValidateRootBindingState(ctx, tx, 28); !errors.Is(err, database.ErrRootBindingState) {
				t.Fatalf("invalid persisted binding was not classified as semantic corruption: %v", err)
			}
			if after := rootBindingStateTestRowsSnapshot(t, ctx, tx); after != before {
				t.Fatal("rejected validation repaired or cleared persisted root state")
			}
		})
	}
	t.Run("canceled context", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin canceled binding validation: %v", err)
		}
		rootBindingStateTestRollback(t, tx)
		before := rootBindingStateTestRowsSnapshot(t, ctx, tx)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := database.ValidateRootBindingState(canceled, tx, 28); !errors.Is(err, context.Canceled) || errors.Is(err, database.ErrRootBindingState) {
			t.Fatalf("canceled validation was reclassified as semantic corruption: %v", err)
		}
		if after := rootBindingStateTestRowsSnapshot(t, ctx, tx); after != before {
			t.Fatal("canceled validation changed persisted root state")
		}
	})
}

func rootBindingStateTestSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate binding validation fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, collection_type)
		VALUES ('binding-library', 'Binding Library', 'movies')`); err != nil {
		t.Fatalf("insert binding library fixture: %v", err)
	}
	// Unbound roots keep their historical path semantics and do not acquire
	// the bound-document path budget merely because the schema was upgraded.
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots (id, library_id, path, allowed_path, relative_path)
		VALUES ('legacy-root', 'binding-library', $1, 'legacy anchor', '../legacy')`, strings.Repeat("l", storagebinding.MaxPathBytes+1)); err != nil {
		t.Fatalf("insert historical unbound root fixture: %v", err)
	}
	snapshot := rootBindingStateTestSnapshot()
	for _, id := range []string{"bound-root", "anchor-root", "limit-root"} {
		if id == "anchor-root" {
			snapshot.Mapping.RegisteredPath = snapshot.Mapping.ApprovedPath
			snapshot.RegisteredRoot = snapshot.Anchor
		}
		if id == "limit-root" {
			snapshot.Mapping.ApprovedPath = rootBindingStateTestMaximumPath()
			snapshot.Mapping.RegisteredPath = snapshot.Mapping.ApprovedPath
		}
		row := rootBindingStateTestBoundRow(t, snapshot)
		if id == "anchor-root" {
			// Preserve the one legacy equivalence accepted by CaptureTopology:
			// an empty relative value names the anchor itself, never a child.
			row.relative = rootBindingStateTestString("")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO library_roots
			(id, library_id, path, allowed_path, relative_path, binding_revision, storage_binding, bound_at, bound_by)
			VALUES ($1, 'binding-library', $2, $3, $4, 7, $5::jsonb, '2025-02-03T04:05:06Z'::timestamptz, 'deleted-user:historical')`,
			id, *row.registered, *row.approved, *row.relative, *row.document); err != nil {
			t.Fatalf("insert complete bound root fixture %s: %v", id, err)
		}
	}
}

func rootBindingStateTestMaximumPath() string {
	// Multibyte text reaches the exact persisted-path byte limit.
	return "/" + strings.Repeat("\u00e9", storagebinding.MaxPathBytes/2-1) + "a"
}

func rootBindingStateTestRowsSnapshot(t *testing.T, ctx context.Context, tx pgx.Tx) string {
	t.Helper()
	var snapshot string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(root) ORDER BY id), '[]'::jsonb)::text
		FROM library_roots root`).Scan(&snapshot); err != nil {
		t.Fatalf("read complete root state: %v", err)
	}
	return snapshot
}

func rootBindingStateTestRollback(t *testing.T, tx pgx.Tx) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("roll back binding validation fixture: %v", err)
		}
	})
}
