package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func readMediaDeletionTarget(ctx context.Context, tx pgx.Tx, actor identity.Principal, itemID string, lock bool) (fileDeletionTarget, error) {
	access, err := checkFileMutationActor(ctx, tx, actor, lock)
	if err != nil {
		return fileDeletionTarget{}, err
	}
	if lock {
		var id string
		if err := tx.QueryRow(ctx, `SELECT i.id FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
			WHERE i.id=$1 AND NOT i.is_folder AND `+access.ordinarySQL("i")+` FOR UPDATE OF i FOR SHARE OF r`, itemID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return fileDeletionTarget{}, ErrNotFound
		} else if err != nil {
			return fileDeletionTarget{}, err
		}
	}
	source, err := readIndexedMediaSource(ctx, tx, access, itemID, "")
	if err != nil {
		return fileDeletionTarget{}, err
	}
	var leaf bool
	if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM items WHERE parent_id=$1)
		AND NOT EXISTS(SELECT 1 FROM item_theme_resources WHERE owner_item_id=$1)
		AND NOT EXISTS(SELECT 1 FROM item_extra_resources WHERE owner_item_id=$1)
		AND `+access.ordinarySQL("i")+` FROM items i WHERE i.id=$1`, itemID).Scan(&leaf); err != nil {
		return fileDeletionTarget{}, err
	}
	if !leaf {
		return fileDeletionTarget{}, fmt.Errorf("%w: deletion requires a single ordinary media file without dependent media", ErrInvalidInput)
	}
	if !actor.IsApplicationKey() {
		allowed, err := contentDeletionAllowed(ctx, tx, access, itemID)
		if err != nil {
			return fileDeletionTarget{}, err
		}
		if !allowed {
			return fileDeletionTarget{}, ErrForbidden
		}
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT binding_revision FROM library_roots WHERE id=$1`, source.root.id).Scan(&revision); err != nil {
		return fileDeletionTarget{}, err
	}
	if revision < 1 {
		return fileDeletionTarget{}, ErrUnavailable
	}
	return fileDeletionTarget{Kind: "media", ItemID: itemID, LibraryID: source.mediaFile.Item.LibraryID, ParentID: source.mediaFile.Item.ParentID, SubtitleIndex: -1,
		File:            fileDeletionSpec{Root: source.root, RelativePath: source.relativePath, Identity: source.identity, Size: source.mediaFile.Size, ModifiedAt: source.mediaFile.ModifiedAt, ChangeTimeNs: source.mediaFile.Item.Media.FileChangeTimeNs},
		BindingRevision: revision, SourceTag: source.mediaFile.ETag}, nil
}

func contentDeletionAllowed(ctx context.Context, tx pgx.Tx, access libraryAccess, itemID string) (bool, error) {
	if access.policy.EnableContentDeletion {
		return true, nil
	}
	if len(access.policy.EnableContentDeletionFromFolders) == 0 {
		return false, nil
	}
	var allowed bool
	err := tx.QueryRow(ctx, `WITH RECURSIVE parents AS (
		SELECT id,parent_id,library_id,path,ARRAY[id] AS visited FROM items WHERE id=$1
		UNION ALL SELECT parent.id,parent.parent_id,parent.library_id,parent.path,child.visited||parent.id
		FROM parents child JOIN items parent ON parent.id=child.parent_id AND parent.library_id=child.library_id
		WHERE NOT parent.id=ANY(child.visited)
	) SELECT EXISTS(SELECT 1 FROM parents WHERE id=ANY($2::text[]) OR library_id=ANY($2::text[]) OR path=ANY($2::text[]))`, itemID, access.policy.EnableContentDeletionFromFolders).Scan(&allowed)
	return allowed, err
}

func applyMediaDeletion(ctx context.Context, tx pgx.Tx, target fileDeletionTarget) error {
	change := CatalogChange{Kind: CatalogRemoved, ItemID: target.ItemID, LibraryID: target.LibraryID, ParentID: target.ParentID}
	if err := recordCollectionSourceRemovals(tx, []CatalogChange{change}); err != nil {
		return err
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM items WHERE id=$1 AND library_id=$2 AND NOT is_folder`, target.ItemID, target.LibraryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrSourceChanged
	}
	return nil
}

// MediaDeletionInfo validates the exact same file-only contract and current
// policy as a later DELETE without touching storage or preparing a journal.
func (s *Store) MediaDeletionInfo(ctx context.Context, actor identity.Principal, itemID string) ([]string, error) {
	if !metadataIdentifier(itemID) || s == nil || s.pool == nil {
		return nil, ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	target, err := readMediaDeletionTarget(ctx, tx, actor, itemID, false)
	if err != nil {
		return nil, err
	}
	if _, err := checkFileMutationActor(ctx, tx, actor, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return []string{target.File.Root.path + "/" + target.File.RelativePath}, nil
}
