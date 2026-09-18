package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// ErrEmbeddedSubtitle identifies an immutable stream inside the original file.
// Subtitle management never rewrites or removes the primary media container.
var ErrEmbeddedSubtitle = errors.New("embedded subtitles cannot be deleted")

// readSubtitleDeletionTarget reads only catalog facts. The journal coordinator
// separately captures and stages the exact external file outside transactions.
func readSubtitleDeletionTarget(ctx context.Context, tx pgx.Tx, actor identity.Principal, itemID string, index int, lock bool) (fileDeletionTarget, error) {
	if ctx == nil || tx == nil || !metadataIdentifier(itemID) || index < 0 || index > maxSubtitleStreamIndex {
		return fileDeletionTarget{}, ErrInvalidInput
	}
	access, err := checkFileMutationActor(ctx, tx, actor, lock)
	if err != nil {
		return fileDeletionTarget{}, err
	}
	if !actor.IsApplicationKey() && (!access.policy.EnableSubtitleManagement || !access.policy.AllowsFeature(identity.FeatureSubtitleManagement)) {
		return fileDeletionTarget{}, ErrForbidden
	}
	query := "SELECT " + itemColumns + `,
		i.relative_path, i.file_identity, i.file_size, i.modified_at,
		r.id, r.library_id, r.path, r.allowed_path, r.relative_path, r.binding_revision
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE i.id = $1 AND NOT i.is_folder AND i.media IS NOT NULL
		AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')
		AND ($2::boolean OR i.library_id = ANY($3::text[])) AND ` + access.directSQL("i")
	if lock {
		query += " FOR UPDATE OF i FOR SHARE OF r"
	}
	var primary indexedMediaSource
	var modified *time.Time
	var revision int64
	item, err := scanItem(tx.QueryRow(ctx, query, itemID, access.all, access.folders),
		&primary.relativePath, &primary.identity, &primary.mediaFile.Size, &modified,
		&primary.root.id, &primary.root.libraryID, &primary.root.path, &primary.root.allowedPath, &primary.root.relativePath, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return fileDeletionTarget{}, ErrNotFound
	}
	if err != nil {
		return fileDeletionTarget{}, fmt.Errorf("read subtitle deletion target: %w", err)
	}
	if item.Media == nil || item.Media.ProbeVersion < media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 ||
		modified == nil || modified.IsZero() || primary.identity == "" || primary.mediaFile.Size <= 0 || revision < 1 {
		return fileDeletionTarget{}, ErrUnavailable
	}
	for _, stream := range item.Media.Streams {
		if stream.Index == index {
			if stream.CodecType == "subtitle" {
				return fileDeletionTarget{}, ErrEmbeddedSubtitle
			}
			return fileDeletionTarget{}, ErrNotFound
		}
	}
	primary.mediaFile.Item = item
	primary.mediaFile.SourceID = media.SourceID(item.ID)
	primary.mediaFile.ModifiedAt = modified.UTC()
	if err := validateMediaSource(primary); err != nil {
		return fileDeletionTarget{}, err
	}
	query = "SELECT " + subtitleColumns + ` FROM item_subtitles s
		WHERE s.item_id = $1 AND s.root_id = $2 AND s.stream_index = $3 AND s.active`
	if lock {
		query += " FOR UPDATE OF s"
	}
	track, err := scanStoredSubtitle(tx.QueryRow(ctx, query, itemID, primary.root.id, index))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && track.Index <= highestEmbeddedStreamIndex(item.Media) {
		return fileDeletionTarget{}, ErrNotFound
	}
	if err != nil {
		return fileDeletionTarget{}, fmt.Errorf("read mutable subtitle deletion target: %w", err)
	}
	if err := validateSubtitleSnapshot(primary, track); err != nil {
		return fileDeletionTarget{}, err
	}
	return fileDeletionTarget{Kind: "subtitle", ItemID: item.ID, LibraryID: item.LibraryID, ParentID: item.ParentID,
		SubtitleIndex: index, BindingRevision: revision, SourceTag: mediaSnapshotTag(primary), SourceHash: track.Tag,
		PrimaryFile: &fileDeletionSpec{Root: primary.root, RelativePath: primary.relativePath, Identity: primary.identity,
			Size: primary.mediaFile.Size, ModifiedAt: primary.mediaFile.ModifiedAt, ChangeTimeNs: item.Media.FileChangeTimeNs},
		File: fileDeletionSpec{Root: primary.root, RelativePath: track.relativePath, Identity: track.identity,
			Size: track.Size, ModifiedAt: track.ModifiedAt, ChangeTimeNs: track.changeTimeNs}}, nil
}

// applySubtitleDeletion retires rather than deletes the stream identity. A
// later scan or provider download must allocate a new index for restored bytes.
func applySubtitleDeletion(ctx context.Context, tx pgx.Tx, target fileDeletionTarget) error {
	if target.Kind != "subtitle" || target.SubtitleIndex < 0 || target.SourceHash == "" {
		return ErrInvalidInput
	}
	result, err := tx.Exec(ctx, `UPDATE item_subtitles SET active = false
		WHERE item_id = $1 AND stream_index = $2 AND active AND root_id = $3
		AND relative_path = $4 AND file_identity = $5 AND source_hash = $6
		AND file_size = $7 AND modified_at = $8 AND change_time_ns = $9`,
		target.ItemID, target.SubtitleIndex, target.File.Root.id, target.File.RelativePath, target.File.Identity,
		target.SourceHash, target.File.Size, target.File.ModifiedAt, target.File.ChangeTimeNs)
	if err != nil {
		return fmt.Errorf("retire deleted subtitle: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrSourceChanged
	}
	if _, err := tx.Exec(ctx, `DELETE FROM item_subtitle_provider_sources WHERE item_id = $1 AND stream_index = $2`, target.ItemID, target.SubtitleIndex); err != nil {
		return fmt.Errorf("retire deleted subtitle provider selection: %w", err)
	}
	return recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: target.ItemID, LibraryID: target.LibraryID, ParentID: target.ParentID})
}
