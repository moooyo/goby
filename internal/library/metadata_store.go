package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

type metadataRecord struct {
	itemID, libraryID, parentID, parentName, name, itemType, path string
	isFolder                                                      bool
	automatic, localSource, overrides, locked                     []byte
	revision                                                      int64
	lastEditedBy                                                  string
	lastEditedAt                                                  *time.Time
}

const metadataRecordColumns = `i.id, i.library_id, COALESCE(i.parent_id, ''),
	COALESCE(parent.name, ''), i.name, i.type, i.path, i.is_folder,
	ms.automatic, i.local_metadata, ms.music_source, ms.overrides, ms.locked_values, ms.revision,
	COALESCE(ms.last_edited_by, ''), ms.last_edited_at`

func metadataIdentifier(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= 256 &&
		utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func validMetadataActor(actor identity.Principal) bool {
	return actor.Kind == "admin" && metadataIdentifier(actor.User.ID) && metadataIdentifier(actor.SessionID)
}

func (s *Store) beginMetadataRead(ctx context.Context, actor identity.Principal) (pgx.Tx, error) {
	if !validMetadataActor(actor) {
		return nil, ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin metadata read: %w", err)
	}
	if err := authorizeMetadataActor(ctx, tx, actor); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func authorizeMetadataActor(ctx context.Context, tx pgx.Tx, actor identity.Principal) error {
	var authorized bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u JOIN sessions a ON a.user_id = u.id
		WHERE u.id = $1 AND a.id = $2 AND u.is_administrator AND NOT u.is_disabled
		AND a.kind = 'admin' AND a.revoked_at IS NULL AND a.expires_at > clock_timestamp())`,
		actor.User.ID, actor.SessionID).Scan(&authorized)
	if err != nil {
		return fmt.Errorf("revalidate metadata administrator: %w", err)
	}
	if !authorized {
		return ErrForbidden
	}
	return nil
}

// Account, authentication session, and catalog rows are locked in that order.
// Managed account changes take the same account-before-session order, so an
// already authenticated but subsequently revoked caller cannot commit an edit.
func lockMetadataActor(ctx context.Context, tx pgx.Tx, actor identity.Principal) error {
	return (&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}).check(ctx, tx, true)
}

func editableMetadataFields(itemType string) []string {
	switch itemType {
	case "Movie", "Series", "Season", "Episode", "Audio", "Video", "MusicAlbum", "MusicArtist", "Folder":
	default:
		return nil
	}
	fields := make([]string, 0, len(metadataValueFieldNames))
	for _, field := range metadataValueFieldNames {
		// Structural season numbers are scanner facts. Editing an episode's
		// own number does not change its physical parent or season membership.
		if field == "ParentIndexNumber" || (field == "IndexNumber" && itemType != "Episode") {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

func activeMetadataControls(itemType string, controls map[string]json.RawMessage) map[string]json.RawMessage {
	active := make(map[string]json.RawMessage, len(controls))
	for _, field := range editableMetadataFields(itemType) {
		if value, exists := controls[field]; exists {
			active[field] = value
		}
	}
	return active
}

// A scanner may reclassify a stable item ID. Keep saved controls for a possible
// later return to the old type, but do not apply them while they are inactive.
// An editor can retain an inactive value unchanged or explicitly remove it.
func normalizeExistingMetadataEdit(edit MetadataEdit, itemType string, previousOverrides, previousLocks map[string]json.RawMessage) (MetadataEdit, error) {
	editable := editableMetadataFields(itemType)
	active := make(map[string]bool, len(editable))
	allowed := append([]string(nil), editable...)
	for _, field := range editable {
		active[field] = true
	}
	for _, previous := range []map[string]json.RawMessage{previousOverrides, previousLocks} {
		for field := range previous {
			if !active[field] {
				allowed = append(allowed, field)
			}
		}
	}
	normalized, err := normalizeMetadataEdit(edit, allowed)
	if err != nil {
		return MetadataEdit{}, err
	}
	fields := make(map[string]string)
	for field, value := range normalized.Overrides {
		if active[field] {
			continue
		}
		previous, exists := previousOverrides[field]
		canonical, err := normalizeMetadataValue(field, previous)
		if !exists || err != nil || !bytes.Equal(value, canonical) {
			fields[field] = "This field is inactive for the current item type; keep its saved value or remove it."
		}
	}
	for _, field := range normalized.LockedFields {
		if _, exists := previousLocks[field]; !active[field] && !exists {
			fields["LockedFields"] = "Cannot add a lock for an inactive metadata field."
		}
	}
	if len(fields) != 0 {
		return MetadataEdit{}, &MetadataValidationError{Fields: fields}
	}
	return normalized, nil
}

func readMetadataRecord(ctx context.Context, tx pgx.Tx, itemID string, lock bool) (metadataRecord, error) {
	statement := "SELECT " + metadataRecordColumns + ` FROM items i
		JOIN item_metadata_state ms ON ms.item_id = i.id
		LEFT JOIN items parent ON parent.id = i.parent_id AND parent.library_id = i.library_id
		WHERE i.id = $1`
	if lock {
		statement += " FOR UPDATE OF i, ms"
	}
	var record metadataRecord
	var musicSource []byte
	err := tx.QueryRow(ctx, statement, itemID).Scan(&record.itemID, &record.libraryID,
		&record.parentID, &record.parentName, &record.name, &record.itemType, &record.path,
		&record.isFolder, &record.automatic, &record.localSource, &musicSource, &record.overrides,
		&record.locked, &record.revision, &record.lastEditedBy, &record.lastEditedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return metadataRecord{}, ErrNotFound
	}
	if err != nil {
		return metadataRecord{}, fmt.Errorf("read item metadata state: %w", err)
	}
	if len(editableMetadataFields(record.itemType)) == 0 {
		return metadataRecord{}, ErrNotFound
	}
	record.localSource, err = mergeAcceptedMusicSource(record.localSource, musicSource)
	if err != nil {
		return metadataRecord{}, err
	}
	return record, nil
}

func metadataDetail(record metadataRecord) (ItemMetadataDetail, error) {
	automatic, err := decodeMetadataValues(record.automatic)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	overrides, err := metadataSourceObject(record.overrides)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	locked, err := metadataSourceObject(record.locked)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	activeOverrides := activeMetadataControls(record.itemType, overrides)
	activeLocks := activeMetadataControls(record.itemType, locked)
	effective, _, err := composeMetadataValues(record.automatic, activeOverrides, activeLocks)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	fields := make([]string, 0, len(locked))
	for field := range locked {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	inactive := make([]string, 0)
	for _, field := range metadataValueFieldNames {
		_, override := overrides[field]
		_, lock := locked[field]
		_, activeOverride := activeOverrides[field]
		_, activeLock := activeLocks[field]
		if (override || lock) && !activeOverride && !activeLock {
			inactive = append(inactive, field)
		}
	}
	return ItemMetadataDetail{
		ItemID: record.itemID, LibraryID: record.libraryID, ParentID: record.parentID,
		ParentName: record.parentName, Type: record.itemType, Path: record.path,
		Name: record.name, IsFolder: record.isFolder, Revision: strconv.FormatInt(record.revision, 10),
		Automatic: automatic, Effective: effective, Overrides: overrides, LockedValues: locked,
		LockedFields: fields, EditableFields: editableMetadataFields(record.itemType), InactiveFields: inactive,
		LastEditedBy: record.lastEditedBy, LastEditedAt: record.lastEditedAt,
	}, nil
}

// GetItemMetadata returns the last accepted source snapshot without disk I/O.
func (s *Store) GetItemMetadata(ctx context.Context, actor identity.Principal, itemID string) (ItemMetadataDetail, error) {
	if !metadataIdentifier(itemID) {
		return ItemMetadataDetail{}, ErrInvalidInput
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	defer rollback(tx)
	record, err := readMetadataRecord(ctx, tx, itemID, false)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemMetadataDetail{}, fmt.Errorf("commit item metadata read: %w", err)
	}
	return detail, nil
}

// UpdateItemMetadata replaces the sparse administrator layer. Retained locks
// keep their original value; new locks capture this edit's prospective value.
func (s *Store) UpdateItemMetadata(ctx context.Context, actor identity.Principal, itemID string, edit MetadataEdit) (ItemMetadataDetail, error) {
	if !validMetadataActor(actor) {
		return ItemMetadataDetail{}, ErrForbidden
	}
	if !metadataIdentifier(itemID) {
		return ItemMetadataDetail{}, ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	defer rollback(tx)
	if err := lockMetadataActor(ctx, tx, actor); err != nil {
		return ItemMetadataDetail{}, err
	}
	record, err := readMetadataRecord(ctx, tx, itemID, true)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	administrator := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	if err := administrator.check(ctx, tx, false); err != nil {
		return ItemMetadataDetail{}, err
	}
	previousOverrides, err := metadataSourceObject(record.overrides)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	previousLocks, err := metadataSourceObject(record.locked)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	edit, err = normalizeExistingMetadataEdit(edit, record.itemType, previousOverrides, previousLocks)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	if strconv.FormatInt(record.revision, 10) != edit.Revision {
		return ItemMetadataDetail{}, ErrRevisionConflict
	}
	locked := make(map[string]json.RawMessage, len(edit.LockedFields))
	for _, field := range edit.LockedFields {
		if value, exists := previousLocks[field]; exists {
			locked[field] = value
		}
	}
	activeOverrides := activeMetadataControls(record.itemType, edit.Overrides)
	effective, _, err := composeMetadataValues(record.automatic, activeOverrides, activeMetadataControls(record.itemType, locked))
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	values, err := metadataValueObject(effective)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	for _, field := range edit.LockedFields {
		if _, exists := locked[field]; !exists {
			locked[field] = values[metadataInternalField(field)]
		}
	}
	overridesJSON, err := json.Marshal(edit.Overrides)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	lockedJSON, err := json.Marshal(locked)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	var changed bool
	if err := tx.QueryRow(ctx, `SELECT overrides IS DISTINCT FROM $2::jsonb
		OR locked_values IS DISTINCT FROM $3::jsonb FROM item_metadata_state WHERE item_id = $1`,
		itemID, overridesJSON, lockedJSON).Scan(&changed); err != nil {
		return ItemMetadataDetail{}, fmt.Errorf("compare metadata edit: %w", err)
	}
	if changed {
		projection, err := buildMetadataProjection(record.localSource, activeOverrides, activeMetadataControls(record.itemType, locked), effective)
		if err != nil {
			return ItemMetadataDetail{}, err
		}
		if err := tx.QueryRow(ctx, `UPDATE item_metadata_state SET overrides = $2,
			locked_values = $3, effective = $4, revision = revision + 1,
			last_edited_by = $5, last_edited_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE item_id = $1 RETURNING revision, last_edited_at`, itemID, overridesJSON,
			lockedJSON, projection, actor.User.ID).Scan(&record.revision, &record.lastEditedAt); err != nil {
			return ItemMetadataDetail{}, fmt.Errorf("write metadata edit: %w", err)
		}
		if err := applyEffectiveMetadata(ctx, tx, itemID, effective, projection); err != nil {
			return ItemMetadataDetail{}, err
		}
		fields, err := metadataActivityFields(previousOverrides, edit.Overrides, previousLocks, locked)
		if err != nil {
			return ItemMetadataDetail{}, err
		}
		event := administrator.event(activity.ActionMetadataUpdated, activity.Resource{Kind: activity.ResourceItem, ID: itemID})
		event.Revision, event.ChangedFields = record.revision, fields
		if err := activity.RecordOwned(catalogActivityTx{tx: tx}, event); err != nil {
			return ItemMetadataDetail{}, err
		}
		record.overrides, record.locked = overridesJSON, lockedJSON
		record.lastEditedBy, record.name = actor.User.ID, effective.Name
	}
	// Session expiry is clock-based and can pass while waiting for catalog rows.
	if err := administrator.check(ctx, tx, false); err != nil {
		return ItemMetadataDetail{}, err
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	if changed {
		if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: record.itemID, LibraryID: record.libraryID,
			ParentID: record.parentID, IsFolder: record.isFolder, IsCollectionFolder: record.itemType == "CollectionFolder"}); err != nil {
			return ItemMetadataDetail{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemMetadataDetail{}, fmt.Errorf("commit metadata edit: %w", err)
	}
	return detail, nil
}

func applyEffectiveMetadata(ctx context.Context, tx pgx.Tx, itemID string, effective MetadataValues, projection []byte, synchronize ...bool) error {
	_, err := tx.Exec(ctx, `UPDATE items SET name = $2, sort_name = $3, overview = $4,
		index_number = CASE WHEN type = 'Episode' THEN COALESCE($5::integer, 0) ELSE index_number END,
		updated_at = clock_timestamp() WHERE id = $1`, itemID, effective.Name, effective.SortName,
		effective.Overview, effective.IndexNumber)
	if err != nil {
		return fmt.Errorf("apply effective metadata values: %w", err)
	}
	if len(synchronize) != 0 && !synchronize[0] {
		return nil
	}
	return syncItemEntities(ctx, tx, itemID, projection)
}
