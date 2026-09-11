package backuppg

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

// Catalog constraints cannot establish total owner coverage or all cross-row
// resource rules. Use the same shape predicates as ordinary and direct reads.
func validateThemeState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := database.ValidateThemeState(ctx, tx, version)
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
	if errors.Is(err, database.ErrThemeState) {
		return ErrSchema
	}
	if err != nil {
		return ErrDatabase
	}
	return nil
}
