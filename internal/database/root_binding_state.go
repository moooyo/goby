package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/storagebinding"
)

// ErrRootBindingState reports an inconsistent persisted approval document.
var ErrRootBindingState = errors.New("incomplete or inconsistent storage root binding state")

// ValidateRootBindingState reads structure without observing, approving, or
// repairing storage. Historical schemas never reference the new columns.
// Existing unbound roots retain their old path semantics; a bound document
// must match the exact registered mapping and relative traversal path.
func ValidateRootBindingState(ctx context.Context, tx pgx.Tx, version int64) error {
	if version < 28 {
		return nil
	}
	if ctx == nil || tx == nil {
		return errors.New("root binding validation requires a context and transaction")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Inspect lengths before returning document or path bytes to the process.
	// Oversized or incoherent rows yield a false flag or NULL bounded value,
	// rather than allocating their untrusted text into the decoder.
	rows, err := tx.Query(ctx, `SELECT COALESCE(binding_revision > 0 AND (
		(storage_binding IS NULL AND bound_at IS NULL AND bound_by IS NULL) OR
		(storage_binding IS NOT NULL AND jsonb_typeof(storage_binding) = 'object'
			AND octet_length(storage_binding::text) <= $1 AND bound_at IS NOT NULL AND isfinite(bound_at)
			AND bound_by IS NOT NULL AND octet_length(bound_by) BETWEEN 1 AND 256
			AND bound_by COLLATE "C" ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$')), false),
		CASE WHEN storage_binding IS NOT NULL AND jsonb_typeof(storage_binding) = 'object'
			AND octet_length(storage_binding::text) <= $1 THEN storage_binding::text END,
		CASE WHEN storage_binding IS NOT NULL AND octet_length(allowed_path) <= $2 THEN allowed_path END,
		CASE WHEN storage_binding IS NOT NULL AND octet_length(path) <= $2 THEN path END,
		CASE WHEN storage_binding IS NOT NULL AND octet_length(relative_path) <= $2 THEN relative_path END
		FROM library_roots ORDER BY id`, storagebinding.MaxDocumentBytes, storagebinding.MaxPathBytes)
	if err != nil {
		return fmt.Errorf("read storage root binding state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var coherent bool
		var document, approved, registered, relative *string
		if err := rows.Scan(&coherent, &document, &approved, &registered, &relative); err != nil {
			return fmt.Errorf("read storage root binding row: %w", err)
		}
		if !coherent {
			return ErrRootBindingState
		}
		if document == nil {
			continue
		}
		if approved == nil || registered == nil || relative == nil {
			return ErrRootBindingState
		}
		snapshot, err := storagebinding.DecodeSnapshot([]byte(*document))
		if err != nil || snapshot.Mapping.ApprovedPath != *approved || snapshot.Mapping.RegisteredPath != *registered {
			return ErrRootBindingState
		}
		mappedRelative, err := snapshot.Mapping.Relative()
		// The existing root lease accepts an empty legacy relative path only
		// when the registered root is the approved anchor itself.
		if err != nil || mappedRelative != *relative && !(mappedRelative == "." && *relative == "") {
			return ErrRootBindingState
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("finish storage root binding state read: %w", err)
	}
	return ctx.Err()
}
