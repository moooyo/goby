package library

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

// A prepared publication may have exchanged directory entries while the
// catalog still describes the original source. The reservation remains active
// until the new catalog is committed and old readers have been retired.
const mediaPublicationLibraryActiveSQL = `SELECT EXISTS (
	SELECT 1 FROM media_operations WHERE source_library_id=$1
	AND publication_phase IN ('prepared','catalog_committed'))`

func readMediaPublicationRevision(ctx context.Context, query mediaOperationQuerier, source indexedMediaSource) (string, error) {
	var revision string
	var publishing bool
	err := query.QueryRow(ctx, `SELECT `+MediaOperationSourceRevisionSQL+`,EXISTS (
		SELECT 1 FROM media_operations publication WHERE publication.source_item_id=i.id
		AND publication.publication_phase IN ('prepared','catalog_committed'))
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND i.root_id=$2 AND i.library_id=$3`, source.mediaFile.Item.ID, source.root.id, source.root.libraryID).
		Scan(&revision, &publishing)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("%w: read media publication barrier: %w", ErrUnavailable, err)
	}
	if publishing {
		return "", fmt.Errorf("%w: this media source is being published", ErrBusy)
	}
	return revision, nil
}

// captureMediaPublicationRead follows authorization inside the caller's media
// snapshot. It is intentionally separate from readIndexedMediaSource: the
// publication executor must still inspect its own old and candidate snapshots.
func captureMediaPublicationRead(ctx context.Context, query mediaOperationQuerier, source *indexedMediaSource) error {
	if source == nil {
		return ErrInvalidInput
	}
	revision, err := readMediaPublicationRevision(ctx, query, *source)
	if err != nil {
		return err
	}
	source.publicationRevision = revision
	return nil
}

// checkOpenedMediaPublication observes a fresh statement after descriptor
// opening. No database connection is retained across filesystem work. If a
// publication committed between the authorized snapshot and the open, either
// its reservation or its changed complete source stamp rejects the new reader.
func (s *Store) checkOpenedMediaPublication(ctx context.Context, source indexedMediaSource) error {
	if source.publicationRevision == "" {
		return fmt.Errorf("%w: public media source lacks a publication snapshot", ErrUnavailable)
	}
	revision, err := readMediaPublicationRevision(ctx, s.pool, source)
	if err != nil {
		return err
	}
	if revision != source.publicationRevision {
		return fmt.Errorf("%w: %w: catalog changed while opening media", ErrUnavailable, ErrSourceChanged)
	}
	return nil
}

func (s *Store) openPublicMediaSource(ctx context.Context, source indexedMediaSource) (*os.File, error) {
	file, err := s.openMediaSource(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := s.checkOpenedMediaPublication(ctx, source); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
