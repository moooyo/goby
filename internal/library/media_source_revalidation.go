package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// RevalidateMediaSourceFor opens a fresh authorized source for an already
// captured delivery plan. Item contains only ID, LibraryID, Type, Path, CanPlay,
// the complete primary media facts, and optionally current external subtitles.
// Catalog metadata, entities, parents and intro projections are deliberately
// absent. Initial playback planning and public item projection must continue
// to use OpenMediaFor. No permission or filesystem observations are cached.
func (s *Store) RevalidateMediaSourceFor(ctx context.Context, subject Subject, itemID, sourceID string, includeSubtitles bool) (*os.File, MediaFile, error) {
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') || strings.ContainsRune(sourceID, '\x00') {
		return nil, MediaFile{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, MediaFile{}, ErrUnavailable
	}
	return s.runPreparedMediaSourceWorker(ctx, false, func(ctx context.Context) (mediaSourceRootHint, error) {
		return s.readMediaSourceRootHint(ctx, itemID)
	}, func(ctx context.Context) (*os.File, MediaFile, error) {
		snapshot, err := s.readMediaRevalidationFor(ctx, subject, itemID, sourceID, includeSubtitles)
		if err != nil {
			return nil, MediaFile{}, err
		}
		// Reuse rooted pathname/descriptor identity checks and the fresh
		// post-open publication barrier, including a replaced directory entry.
		file, err := s.openPublicMediaSource(ctx, snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		return file, snapshot.mediaFile, nil
	}, func(ctx context.Context) error {
		_, err := s.readMediaRevalidationFor(ctx, subject, itemID, sourceID, includeSubtitles)
		return err
	})
}

func (s *Store) readMediaRevalidationFor(ctx context.Context, subject Subject, itemID, sourceID string, includeSubtitles bool) (indexedMediaSource, error) {
	ctx, release, err := beginMediaSourceAuthorization(ctx)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer release()
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer tx.Rollback(ctx)
	if !access.canPlay {
		return indexedMediaSource{}, ErrForbidden
	}
	snapshot, err := readIndexedMediaRevalidation(ctx, tx, access, itemID, sourceID, includeSubtitles)
	if err != nil {
		return indexedMediaSource{}, err
	}
	if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
		return indexedMediaSource{}, err
	}
	// A blocked storage operation must not retain the policy transaction.
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: complete authorized media revalidation: %w", ErrUnavailable, err)
	}
	if err := s.sealPrimaryMediaReadSnapshot(ctx, &snapshot); err != nil {
		return indexedMediaSource{}, err
	}
	return snapshot, nil
}

func readIndexedMediaRevalidation(ctx context.Context, tx pgx.Tx, access libraryAccess, itemID, sourceID string, includeSubtitles bool) (indexedMediaSource, error) {
	var snapshot indexedMediaSource
	var modified *time.Time
	var encoded []byte
	item := &snapshot.mediaFile.Item
	err := tx.QueryRow(ctx, `SELECT i.id, i.library_id, i.type, i.path, i.media,
		i.relative_path, i.file_identity, i.file_size, i.modified_at,
		r.id, r.library_id, r.path, r.allowed_path, r.relative_path, r.binding_revision
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE i.id = $1 AND NOT i.is_folder AND i.media IS NOT NULL
		AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')
		AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+access.directSQL("i"), itemID, access.all, access.folders).
		Scan(&item.ID, &item.LibraryID, &item.Type, &item.Path, &encoded,
			&snapshot.relativePath, &snapshot.identity, &snapshot.mediaFile.Size, &modified,
			&snapshot.root.id, &snapshot.root.libraryID, &snapshot.root.path, &snapshot.root.allowedPath, &snapshot.root.relativePath, &snapshot.rootBindingRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return indexedMediaSource{}, ErrNotFound
	}
	if err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: read media revalidation snapshot: %w", ErrUnavailable, err)
	}
	// Retain all primary probe facts: copied-track bitrate checks and bound
	// embedded subtitle validation must observe the same current stream facts
	// as complete opens, including fields added by future probe versions.
	if len(encoded) != 0 && string(encoded) != "null" {
		item.Media = &media.Info{}
		if err := json.Unmarshal(encoded, item.Media); err != nil {
			return indexedMediaSource{}, fmt.Errorf("%w: decode media revalidation snapshot: %w", ErrUnavailable, err)
		}
	}
	return completeIndexedMediaSource(ctx, tx, access, snapshot, sourceID, modified, includeSubtitles)
}
