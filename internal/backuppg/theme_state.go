package backuppg

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

// Catalog constraints cannot establish total owner coverage or all cross-row
// resource rules. Historical archives retain their version's theme semantics;
// extra and root-binding state are checked only after their own migrations.
func validateResourceState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := validateThemeState(ctx, tx, version); err != nil {
		return err
	}
	if err := validateExtraState(ctx, tx, version); err != nil {
		return err
	}
	return validateRootBindingState(ctx, tx, version)
}

func validateThemeState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return classifyResourceStateError(ctx, database.ValidateThemeState(ctx, tx, version))
}

func validateExtraState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return classifyResourceStateError(ctx, database.ValidateExtraState(ctx, tx, version))
}

func validateRootBindingState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return classifyResourceStateError(ctx, database.ValidateRootBindingState(ctx, tx, version))
}

func classifyResourceStateError(ctx context.Context, err error) error {
	// A cancelled finalizer or a deadline during the semantic read must keep
	// its caller-visible context error, even if the driver reports another error.
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, database.ErrThemeState) || errors.Is(err, database.ErrExtraState) || errors.Is(err, database.ErrRootBindingState) {
		return ErrSchema
	}
	if err != nil {
		return ErrDatabase
	}
	return nil
}
