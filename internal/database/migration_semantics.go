package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The two migration entry points must apply the same transactional transition.
// Lock before source validation so a concurrent schema-26 writer cannot make
// the checked state stale while the new reservations are being derived.
func beforeMigration(ctx context.Context, tx pgx.Tx, version int64) error {
	if version != 27 {
		return nil
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE libraries, library_roots, items, theme_owner_ids,
		theme_reserved_paths, item_theme_resources IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock source auxiliary state: %w", err)
	}
	if err := ValidateThemeState(ctx, tx, 26); err != nil {
		return fmt.Errorf("validate source auxiliary state: %w", err)
	}
	return nil
}

func afterMigration(ctx context.Context, tx pgx.Tx, version int64) error {
	if version != 27 {
		return nil
	}
	if err := ValidateThemeState(ctx, tx, version); err != nil {
		return fmt.Errorf("validate migrated theme state: %w", err)
	}
	if err := ValidateExtraState(ctx, tx, version); err != nil {
		return fmt.Errorf("validate migrated extra state: %w", err)
	}
	return nil
}
