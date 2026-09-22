package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

var ErrLibraryConflict = errors.New("library editing revision changed")

type RegisteredPath struct {
	ID        string `json:"Id"`
	Path      string
	ItemCount int64
}

type LibraryEditing struct {
	Library         Library
	RegisteredPaths []RegisteredPath
}

// A replacement declares an already completed filesystem move with unchanged
// relative media names. It remaps catalog records without moving any files.
type LibraryPathReplacement struct {
	From string
	To   string
}

type LibraryUpdate struct {
	Revision               string
	Name                   *string
	Paths                  *[]string
	PathReplacements       []LibraryPathReplacement
	LibraryOptions         *LibraryOptionsUpdate
	AcknowledgePathRemoval bool
}

const libraryEditingColumns = libraryColumns + `, COALESCE((SELECT jsonb_agg(jsonb_build_object(
	'Id', r.id, 'Path', r.path, 'ItemCount', (SELECT count(*) FROM items i WHERE i.root_id=r.id)) ORDER BY r.path, r.id)
	FROM library_roots r WHERE r.library_id=l.id), '[]'::jsonb)`

func scanLibraryEditing(row rowScanner) (LibraryEditing, error) {
	var result LibraryEditing
	var options, paths []byte
	library := &result.Library
	err := row.Scan(&library.ID, &library.Name, &library.CollectionType, &library.Paths,
		&library.CreatedAt, &library.LastScanAt, &library.Revision, &options, &paths)
	if errors.Is(err, pgx.ErrNoRows) {
		return LibraryEditing{}, ErrNotFound
	}
	if err != nil {
		return LibraryEditing{}, err
	}
	library.Options = &LibraryOptions{}
	if err := json.Unmarshal(options, library.Options); err != nil {
		return LibraryEditing{}, err
	}
	if err := json.Unmarshal(paths, &result.RegisteredPaths); err != nil {
		return LibraryEditing{}, err
	}
	return result, nil
}

func (s *Store) GetLibraryEditing(ctx context.Context, id string) (LibraryEditing, error) {
	if s == nil || s.pool == nil {
		return LibraryEditing{}, ErrUnavailable
	}
	if ctx == nil || !validCatalogLibraryIdentifier(id) {
		return LibraryEditing{}, ErrInvalidInput
	}
	if id == collectionLibraryID {
		return LibraryEditing{}, ErrNotFound
	}
	return scanLibraryEditing(s.pool.QueryRow(ctx, `SELECT `+libraryEditingColumns+` FROM libraries l WHERE l.id=$1`, id))
}

func (s *Store) GetLibraryEditingAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, id string) (LibraryEditing, error) {
	if s == nil || s.pool == nil {
		return LibraryEditing{}, ErrUnavailable
	}
	if ctx == nil {
		return LibraryEditing{}, ErrInvalidInput
	}
	administrator := &catalogAdministrator{actor: actor, audience: audience}
	if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
		return LibraryEditing{}, err
	}
	result, err := s.GetLibraryEditing(ctx, id)
	if err != nil {
		return LibraryEditing{}, err
	}
	if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
		return LibraryEditing{}, err
	}
	return result, nil
}

func (s *Store) UpdateLibraryAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, id string, input LibraryUpdate) (LibraryEditing, error) {
	return s.updateLibrary(ctx, &catalogAdministrator{actor: actor, audience: audience}, id, input)
}

func libraryEditRevision(value string) (int64, error) {
	if len(value) == 0 || len(value) > 19 || value[0] < '1' || value[0] > '9' {
		return 0, ErrInvalidInput
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidInput
		}
	}
	valueInt, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidInput
	}
	return valueInt, nil
}

func libraryEditPath(value string) (string, error) {
	if len(value) == 0 || len(value) > 4096 || strings.ContainsRune(value, 0) || !filepath.IsAbs(value) || hasTraversal(value) {
		return "", ErrInvalidInput
	}
	return filepath.Clean(value), nil
}

func (s *Store) updateLibrary(ctx context.Context, administrator *catalogAdministrator, id string, input LibraryUpdate) (LibraryEditing, error) {
	if ctx == nil || !validCatalogLibraryIdentifier(id) || len(input.PathReplacements) > 32 {
		return LibraryEditing{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return LibraryEditing{}, ErrUnavailable
	}
	revision, err := libraryEditRevision(input.Revision)
	if err != nil {
		return LibraryEditing{}, err
	}
	if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
		return LibraryEditing{}, err
	}
	previous, err := s.GetLibraryEditing(ctx, id)
	if err != nil {
		return LibraryEditing{}, err
	}
	if previous.Library.Revision != input.Revision {
		return LibraryEditing{}, ErrLibraryConflict
	}
	name := previous.Library.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 128 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return LibraryEditing{}, ErrInvalidInput
		}
	}
	options := applyLibraryOptions(EffectiveLibraryOptions(previous.Library), input.LibraryOptions)
	paths := previous.Library.Paths
	if input.Paths != nil {
		paths = *input.Paths
	}
	if len(paths) < 1 || len(paths) > 32 || input.Paths == nil && len(input.PathReplacements) != 0 {
		return LibraryEditing{}, ErrInvalidInput
	}
	oldPaths := make(map[string]RegisteredPath, len(previous.RegisteredPaths))
	for _, old := range previous.RegisteredPaths {
		oldPaths[old.Path] = old
	}
	// Canonicalize new paths using held approved descriptors. Existing unchanged
	// roots need not be online just to rename the library or change import options.
	registrations := make(map[string]*rootBindingRegistration)
	defer func() {
		for _, registration := range registrations {
			_ = registration.Close()
		}
	}()
	desired := make(map[string]bool, len(paths))
	aliases := make(map[string]string, len(paths))
	for _, requested := range paths {
		path, err := libraryEditPath(requested)
		if err != nil {
			return LibraryEditing{}, err
		}
		canonical := path
		if _, unchanged := oldPaths[path]; !unchanged {
			registration, err := s.authorizePath(path)
			if err != nil {
				return LibraryEditing{}, err
			}
			canonical = registration.root.path
			if desired[canonical] {
				_ = registration.Close()
				return LibraryEditing{}, ErrInvalidInput
			}
			if _, unchanged := oldPaths[canonical]; unchanged {
				_ = registration.Close()
			} else {
				registrations[canonical] = registration
			}
		}
		if desired[canonical] {
			return LibraryEditing{}, ErrInvalidInput
		}
		for other := range desired {
			if pathWithin(other, canonical) || pathWithin(canonical, other) {
				return LibraryEditing{}, fmt.Errorf("%w: library directories must not overlap", ErrInvalidInput)
			}
		}
		desired[canonical], aliases[path] = true, canonical
	}
	replacements := make(map[string]string)
	replacementTargets := make(map[string]bool)
	for _, replacement := range input.PathReplacements {
		from, err := libraryEditPath(replacement.From)
		if err != nil {
			return LibraryEditing{}, err
		}
		to, err := libraryEditPath(replacement.To)
		if err != nil {
			return LibraryEditing{}, err
		}
		if canonical, exists := aliases[to]; exists {
			to = canonical
		}
		_, exists := oldPaths[from]
		if !exists || desired[from] || to == "" || registrations[to] == nil || replacements[from] != "" || replacementTargets[to] {
			return LibraryEditing{}, ErrInvalidInput
		}
		replacements[from], replacementTargets[to] = to, true
	}
	removed := make([]RegisteredPath, 0)
	for path, old := range oldPaths {
		if !desired[path] && replacements[path] == "" {
			if !input.AcknowledgePathRemoval {
				return LibraryEditing{}, fmt.Errorf("%w: removing a registered path requires explicit acknowledgment", ErrInvalidInput)
			}
			removed = append(removed, old)
		}
	}
	changedPaths := len(registrations)+len(removed) != 0
	// An edit may advance once for the library and once per changed root. Reserve
	// that bounded headroom before any trigger can encounter bigint overflow.
	if revision > math.MaxInt64-int64(1+len(registrations)+len(removed)) {
		return LibraryEditing{}, ErrLibraryConflict
	}
	boundBy, err := rootBindingRegistrationActorID(administrator)
	if err != nil {
		return LibraryEditing{}, err
	}
	for path, registration := range registrations {
		registration.root.libraryID = id
		for from, to := range replacements {
			if to == path {
				registration.root.id = oldPaths[from].ID
			}
		}
		if registration.root.id == "" {
			registration.root.id, err = randomID()
			if err != nil {
				return LibraryEditing{}, err
			}
		}
		if err := registration.prepare(ctx, captureRootBindingRegistrationTopology); err != nil {
			return LibraryEditing{}, err
		}
		if err := registration.Revalidate(ctx); err != nil {
			return LibraryEditing{}, err
		}
	}
	tx, err := s.beginOwnedAdmission(ctx, false)
	if err != nil {
		return LibraryEditing{}, err
	}
	defer s.mu.Unlock()
	defer rollback(tx)
	for _, registration := range registrations {
		if !s.rootBindingPathConfiguredLocked(registration.root.allowedPath) {
			return LibraryEditing{}, ErrUnavailable
		}
	}
	protected := tx.(*ownedTx).ctx
	if err := administrator.check(protected, tx, true); err != nil {
		return LibraryEditing{}, err
	}
	var currentRevision int64
	if err := tx.QueryRow(protected, `SELECT revision FROM libraries WHERE id=$1 FOR UPDATE`, id).Scan(&currentRevision); errors.Is(err, pgx.ErrNoRows) {
		return LibraryEditing{}, ErrNotFound
	} else if err != nil {
		return LibraryEditing{}, err
	}
	if currentRevision != revision {
		return LibraryEditing{}, ErrLibraryConflict
	}
	var publishing bool
	if err := tx.QueryRow(protected, mediaPublicationLibraryActiveSQL, id).Scan(&publishing); err != nil {
		return LibraryEditing{}, err
	}
	if publishing {
		return LibraryEditing{}, ErrBusy
	}
	var busy bool
	if err := tx.QueryRow(protected, `SELECT EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=$1 AND status IN ('Queued','Running'))
		OR EXISTS(SELECT 1 FROM media_deletion_operations WHERE library_id=$1)`, id).Scan(&busy); err != nil {
		return LibraryEditing{}, err
	}
	if busy {
		return LibraryEditing{}, ErrBusy
	}
	changed := changedPaths || name != previous.Library.Name || options != EffectiveLibraryOptions(previous.Library)
	if changed {
		encodedOptions, err := json.Marshal(options)
		if err != nil {
			return LibraryEditing{}, err
		}
		if _, err := tx.Exec(protected, `UPDATE libraries SET name=$2, options=$3, revision=revision+1 WHERE id=$1`, id, name, encodedOptions); err != nil {
			return LibraryEditing{}, err
		}
		if name != previous.Library.Name {
			if _, err := tx.Exec(protected, `UPDATE items SET name=$2, sort_name=$3, updated_at=now() WHERE id=$1 AND library_id=$1`, id, name, strings.ToLower(name)); err != nil {
				return LibraryEditing{}, err
			}
			if err := syncScannedMetadata(protected, tx, id); err != nil {
				return LibraryEditing{}, err
			}
		}
		for _, old := range removed {
			if _, err := tx.Exec(protected, `DELETE FROM library_roots WHERE id=$1 AND library_id=$2`, old.ID, id); err != nil {
				return LibraryEditing{}, err
			}
		}
		for path, registration := range registrations {
			root := registration.root
			var document, actor any
			if registration.document != nil {
				document, actor = string(registration.document), boundBy
			}
			if replacementTargets[path] {
				var bindingRevision int64
				if err := tx.QueryRow(protected, `SELECT binding_revision FROM library_roots WHERE id=$1 AND library_id=$2 FOR UPDATE`, root.id, id).Scan(&bindingRevision); err != nil {
					return LibraryEditing{}, err
				}
				if bindingRevision == math.MaxInt64 {
					return LibraryEditing{}, ErrLibraryConflict
				}
				if _, err := tx.Exec(protected, `UPDATE library_roots SET path=$3, allowed_path=$4, relative_path=$5,
					binding_revision=binding_revision+1, storage_binding=$6::jsonb,
					bound_at=CASE WHEN $6::jsonb IS NOT NULL THEN clock_timestamp() END, bound_by=$7
					WHERE id=$1 AND library_id=$2`, root.id, id, root.path, root.allowedPath, root.relativePath, document, actor); err != nil {
					return LibraryEditing{}, err
				}
				if err := remapLibraryRootItems(protected, tx, root); err != nil {
					return LibraryEditing{}, err
				}
			} else if _, err := tx.Exec(protected, `INSERT INTO library_roots
				(id,library_id,path,allowed_path,relative_path,binding_revision,storage_binding,bound_at,bound_by)
				VALUES($1,$2,$3,$4,$5,1,$6::jsonb,CASE WHEN $6::jsonb IS NOT NULL THEN clock_timestamp() END,$7)`,
				root.id, id, root.path, root.allowedPath, root.relativePath, document, actor); err != nil {
				return LibraryEditing{}, err
			}
		}
		if changedPaths {
			tx.(*ownedTx).rememberNotificationScope(id, id)
			// Large root removals/moves invalidate private collection membership
			// and all catalog projections without retaining an unbounded ID list.
			tx.(*ownedTx).catalogChanges.requireResync()
		} else if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: id, LibraryID: id, IsFolder: true, IsCollectionFolder: true}); err != nil {
			return LibraryEditing{}, err
		}
	}
	result, err := scanLibraryEditing(tx.QueryRow(protected, `SELECT `+libraryEditingColumns+` FROM libraries l WHERE l.id=$1`, id))
	if err != nil {
		return LibraryEditing{}, err
	}
	if changed {
		event := administrator.event(activity.ActionLibraryUpdated, activity.Resource{Kind: activity.ResourceLibrary, ID: id})
		event.Revision, _ = strconv.ParseInt(result.Library.Revision, 10, 64)
		event.Count = int64(len(registrations) + len(removed))
		if err := activity.RecordOwned(catalogActivityTx{tx: tx}, event); err != nil {
			return LibraryEditing{}, err
		}
	}
	finalCtx, cancel := context.WithTimeout(protected, storageObservationTimeout)
	defer cancel()
	for _, registration := range registrations {
		if err := registration.RevalidateBounded(finalCtx); err != nil {
			return LibraryEditing{}, err
		}
	}
	if err := administrator.check(protected, tx, false); err != nil {
		return LibraryEditing{}, err
	}
	if err := tx.Commit(protected); err != nil {
		return LibraryEditing{}, err
	}
	for _, old := range removed {
		if anchor, exists := s.rootBindingAnchors[old.ID]; exists {
			delete(s.rootBindingAnchors, old.ID)
			s.retireRootAnchorLocked(anchor.approved)
		}
	}
	for _, registration := range registrations {
		s.installRootBindingAnchorLocked(registration.root, registration.anchor)
		registration.anchor = nil
	}
	return result, nil
}

func remapLibraryRootItems(ctx context.Context, tx pgx.Tx, root libraryRoot) error {
	// Relative paths are the stable catalog key, including virtual folders with
	// an empty physical path. Only physical paths change; source controls survive.
	separator := string(filepath.Separator)
	base := strings.TrimSuffix(root.path, separator)
	if _, err := tx.Exec(ctx, `UPDATE items SET path=CASE WHEN relative_path IN ('','.') THEN $3
		ELSE $4 || $5 || replace(relative_path,'/',$5) END, updated_at=now()
		WHERE root_id=$1 AND library_id=$2 AND path<>''`, root.id, root.libraryID, root.path, base, separator); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE item_metadata_state ms SET source_key=jsonb_set(ms.source_key,'{Path}',to_jsonb(i.path)),
		revision=ms.revision+1, updated_at=now() FROM items i
		WHERE i.id=ms.item_id AND i.root_id=$1 AND i.library_id=$2
		AND ms.source_key->>'Path' IS DISTINCT FROM i.path`, root.id, root.libraryID)
	return err
}
