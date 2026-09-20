package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/notificationjournal"
)

// applyAutomaticSorting runs after source precedence is established and before
// manual controls are applied. Raw source titles remain intact; each generated
// key is derived from the full automatic Name so clearing rules is reversible.
func applyAutomaticSorting(ctx context.Context, tx pgx.Tx, itemID string, automatic, source []byte, auxiliary bool) ([]byte, error) {
	fields, err := metadataSourceObject(source)
	if err != nil {
		return nil, err
	}
	var sortTitle string
	if raw, present := fields["SortName"]; present {
		if json.Unmarshal(raw, &sortTitle) != nil {
			return nil, ErrInvalidInput
		}
	}
	explicit := sortTitle != ""
	if _, err = tx.Exec(ctx, `UPDATE item_metadata_state SET automatic_sort_name_explicit=$2 WHERE item_id=$1 AND automatic_sort_name_explicit IS DISTINCT FROM $2`, itemID, explicit); err != nil {
		return nil, fmt.Errorf("store automatic sort provenance: %w", err)
	}
	if explicit {
		return automatic, nil
	}
	var result []byte
	// New auxiliary resources are persisted before their permanent role row.
	// The scanner supplies that role explicitly; refreshes of retained active or
	// inactive resources can also recover it from the permanent association.
	err = tx.QueryRow(ctx, `SELECT jsonb_set($1::jsonb,'{SortName}',to_jsonb(goby_generated_sort_name($1::jsonb->>'Name',sort_remove_words,
		$3 OR EXISTS(SELECT 1 FROM item_theme_resources role WHERE role.resource_item_id=$2)
		OR EXISTS(SELECT 1 FROM item_extra_resources role WHERE role.resource_item_id=$2)))) FROM managed_settings WHERE id=1`, automatic, itemID, auxiliary).Scan(&result)
	if err != nil {
		return nil, fmt.Errorf("derive automatic sort key: %w", err)
	}
	return result, nil
}

// RebuildGeneratedSortNames changes derived catalog keys in the same owner
// transaction as the settings CAS. One bounded invalidation replaces per-item
// facts; durable delivery retains library scopes for current authorization.
func RebuildGeneratedSortNames(tx OwnedTx) (resultErr error) {
	defer func() {
		var databaseError *pgconn.PgError
		if errors.As(resultErr, &databaseError) && databaseError.Code == "57014" {
			resultErr = fmt.Errorf("%w: sort rebuild reached its database statement deadline: %w", ErrUnavailable, resultErr)
		}
	}()
	view, ok := tx.(*ownedCallbackTx)
	if !ok || view == nil || view.catalog == nil {
		return fmt.Errorf("%w: sort rebuild requires a catalog owner", ErrInvalidInput)
	}
	// A server-side timeout leaves enough of the protected owner context for
	// rollback. Context expiry during a large rebuild could instead close the
	// reserved connection. Preserve any stricter deployment statement limit.
	var previousTimeout string
	if err := tx.QueryRow(`SELECT current_setting('statement_timeout')`).Scan(&previousTimeout); err != nil {
		return err
	}
	refreshBudget := func() error {
		budget := 5 * time.Second
		if deadline, ok := view.ctx.Deadline(); ok {
			if remaining := time.Until(deadline) - 2*time.Second; remaining < budget {
				budget = remaining
			}
		}
		if budget < time.Millisecond {
			return fmt.Errorf("%w: insufficient sort rebuild transaction budget", ErrUnavailable)
		}
		_, err := tx.Exec(`SELECT set_config('statement_timeout',CASE WHEN setting::bigint=0 OR setting::bigint>$1::bigint THEN $2 ELSE current_setting('statement_timeout') END,true)
			FROM pg_settings WHERE name='statement_timeout'`, budget.Milliseconds(), strconv.FormatInt(budget.Milliseconds(), 10)+"ms")
		return err
	}
	restoreTimeout := func() error {
		_, err := tx.Exec(`SELECT set_config('statement_timeout',$1,true)`, previousTimeout)
		return err
	}
	var changedLibraries int
	if err := refreshBudget(); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT count(*) FROM goby_rebuild_generated_sort_names()`).Scan(&changedLibraries); err != nil {
		return err
	}
	// Scope comes from the global policy's audience, never from the subset of
	// hidden items whose keys happened to change. Private collections retain
	// item-level authorization because their synthetic library has no root item.
	if err := refreshBudget(); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT i.id,i.library_id,i.type='CollectionFolder' FROM items i
		WHERE (i.type='CollectionFolder' AND i.id=i.library_id) OR EXISTS(SELECT 1 FROM media_collections c WHERE c.item_id=i.id)
		ORDER BY i.id LIMIT 4097`)
	if err != nil {
		return err
	}
	refs := make([]notificationjournal.Reference, 0)
	for rows.Next() {
		var id, libraryID string
		var isLibrary bool
		if err := rows.Scan(&id, &libraryID, &isLibrary); err != nil {
			rows.Close()
			return err
		}
		ref := notificationjournal.Reference{Kind: "Item", ID: id, LibraryID: libraryID, SourceID: id}
		if isLibrary {
			ref.Kind = "Library"
			ref.SourceID = ""
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return restoreTimeout()
	}
	err = func() error {
		view.mu.Lock()
		defer view.mu.Unlock()
		if view.finished {
			return pgx.ErrTxClosed
		}
		view.catalog.catalogChanges.requireResync()
		for _, ref := range refs {
			found := false
			for _, existing := range view.catalog.notificationReferences {
				found = found || existing == ref
			}
			if !found && len(view.catalog.notificationReferences) < 4097 {
				view.catalog.notificationReferences = append(view.catalog.notificationReferences, ref)
			}
		}
		return nil
	}()
	if err != nil {
		return err
	}
	if err := refreshBudget(); err != nil {
		return err
	}
	err = func() error {
		view.mu.Lock()
		defer view.mu.Unlock()
		if view.finished {
			return pgx.ErrTxClosed
		}
		return view.observeLocked(view.catalog.recordNotificationJournal())
	}()
	if err != nil {
		return err
	}
	// RecordCatalog executes one SQL statement (its stored function has the
	// same statement deadline). The unchanged journal stamp makes this flush
	// execute only LibraryChanged's single UPDATE. Refresh between those SQL
	// boundaries, and flush before the caller's final authority check; COMMIT
	// must not introduce another potentially waiting event write afterward.
	if err := refreshBudget(); err != nil {
		return err
	}
	err = func() error {
		view.mu.Lock()
		defer view.mu.Unlock()
		if view.finished {
			return pgx.ErrTxClosed
		}
		return view.observeLocked(view.catalog.flushSystemEvent())
	}()
	if err != nil {
		return err
	}
	return restoreTimeout()
}
