package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/media"
)

// This stamp binds automatic bytes to the complete indexed source and root
// approval. It is not a content hash or an authorization credential.
const embeddedArtworkSourceRevisionSQL = `('embedded-source-v1-' || md5(jsonb_build_array(i.root_id,
	i.relative_path,i.file_identity,i.file_size,extract(epoch FROM i.modified_at),i.media,
	(SELECT er.binding_revision FROM library_roots er WHERE er.id=i.root_id))::text))`

const embeddedArtworkCurrentSQL = `i.type='Audio' AND NOT i.is_folder AND e.status='ready'
	AND e.extraction_version=1 AND e.probe_version=COALESCE((i.media->>'ProbeVersion')::integer,0)
	AND e.source_revision=` + embeddedArtworkSourceRevisionSQL

const embeddedArtworkColumns = `e.source_hash,e.mime_type,e.width,e.height,octet_length(e.content),e.inspected_at,e.source_revision`

// mergeEmbeddedImageListing fills only a missing Primary slot. The caller then
// applies provider selections and the managed layer, including its tombstones.
// Every row uses the caller's exact visibility snapshot and current source stamp.
func mergeEmbeddedImageListing(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string, result map[string][]Image) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT i.id,`+embeddedArtworkColumns+` FROM item_embedded_artwork e JOIN items i ON i.id=e.item_id
		WHERE i.id=ANY($1::text[]) AND ($2::boolean OR i.library_id=ANY($3::text[])) AND `+access.directSQL("i")+` AND `+embeddedArtworkCurrentSQL+` ORDER BY i.id`, ids, access.all, access.folders)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		image, err := scanEmbeddedArtworkImage(rows, &id)
		if err != nil {
			return err
		}
		hasPrimary := false
		for _, current := range result[id] {
			if current.ImageType == "Primary" {
				hasPrimary = true
				break
			}
		}
		if !hasPrimary {
			result[id] = append([]Image{image}, result[id]...)
		}
	}
	return rows.Err()
}

func scanEmbeddedArtworkImage(row rowScanner, prefix ...any) (Image, error) {
	result := Image{ImageType: "Primary", ImageIndex: 0, Source: "embedded"}
	fields := []any{&result.Tag, &result.MIMEType, &result.Width, &result.Height, &result.Size, &result.ModifiedAt, &result.SourceRevision}
	if err := row.Scan(append(prefix, fields...)...); err != nil {
		return Image{}, err
	}
	result.ContentTag = result.Tag
	if result.Size <= 0 || result.Size > media.MaxEmbeddedArtworkBytes || result.Width <= 0 || result.Height <= 0 || result.Width > 16384 || result.Height > 16384 || int64(result.Width)*int64(result.Height) > 25<<20 {
		return Image{}, ErrUnavailable
	}
	digest, err := hex.DecodeString(result.Tag)
	if err != nil || len(digest) != sha256.Size {
		return Image{}, ErrUnavailable
	}
	switch result.MIMEType {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return Image{}, ErrUnavailable
	}
	return result, nil
}

type embeddedArtworkContent struct {
	data   []byte
	image  Image
	source indexedMediaSource
}

// readEmbeddedArtworkContent is internal: callers must authorize the item in
// this transaction before using it. It never broadens a subject's item set.
func readEmbeddedArtworkContent(ctx context.Context, tx pgx.Tx, itemID string) (embeddedArtworkContent, error) {
	var result embeddedArtworkContent
	image, err := scanEmbeddedArtworkImage(tx.QueryRow(ctx, `SELECT e.content,`+embeddedArtworkColumns+`
		FROM item_embedded_artwork e JOIN items i ON i.id=e.item_id WHERE i.id=$1 AND `+embeddedArtworkCurrentSQL, itemID), &result.data)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if int64(len(result.data)) != image.Size {
		return result, ErrUnavailable
	}
	digest := sha256.Sum256(result.data)
	if hex.EncodeToString(digest[:]) != image.Tag {
		return result, ErrUnavailable
	}
	validated, err := artwork.InspectContext(ctx, bytes.NewReader(result.data))
	if err != nil || validated.Tag != image.Tag || validated.Width != image.Width || validated.Height != image.Height || validated.MIMEType != image.MIMEType {
		return result, ErrUnavailable
	}
	result.source, err = readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), itemID, "")
	if err != nil {
		return result, err
	}
	if err := captureMediaPublicationRead(ctx, tx, &result.source); err != nil {
		return result, err
	}
	result.image = image
	return result, nil
}

func (s *Store) checkEmbeddedArtworkContentSource(ctx context.Context, result embeddedArtworkContent) error {
	file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		file, err := s.openPublicMediaSource(ctx, result.source)
		return file, result.source.mediaFile, err
	})
	if err != nil {
		return err
	}
	return file.Close()
}

// OpenEmbeddedImageFor is the lowest-priority automatic artwork source. The
// higher-level resolver must first consider managed, provider, and sidecar
// ownership, and call this only when those layers have no matching image.
// Both cached bytes and actual media identity are validated on every open.
func (s *Store) OpenEmbeddedImageFor(ctx context.Context, subject Subject, itemID, imageType string, index int) (io.ReadCloser, Image, error) {
	if !validImageItemID(itemID) {
		return nil, Image{}, ErrInvalidInput
	}
	canonical, err := normalizeStoredImageType(imageType, index)
	if err != nil {
		return nil, Image{}, err
	}
	if canonical != "Primary" || index != 0 {
		return nil, Image{}, ErrNotFound
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, Image{}, err
	}
	defer rollback(tx)
	if _, err := readQueryParent(ctx, tx, itemID, access); err != nil {
		return nil, Image{}, err
	}
	result, err := readEmbeddedArtworkContent(ctx, tx, itemID)
	if err != nil {
		return nil, Image{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, Image{}, fmt.Errorf("complete embedded artwork source read: %w", err)
	}
	if err := s.checkEmbeddedArtworkContentSource(ctx, result); err != nil {
		return nil, Image{}, err
	}
	return io.NopCloser(bytes.NewReader(result.data)), result.image, nil
}

// readEmbeddedImageContent serves already-authorized collage source selection
// inside its shared transaction. Its bounded source worker retains its slot
// until any blocked filesystem operation finishes after caller cancellation.
func (s *Store) readEmbeddedImageContent(ctx context.Context, tx pgx.Tx, itemID string) ([]byte, Image, error) {
	result, err := readEmbeddedArtworkContent(ctx, tx, itemID)
	if err != nil {
		return nil, Image{}, err
	}
	if err := s.checkEmbeddedArtworkContentSource(ctx, result); err != nil {
		return nil, Image{}, err
	}
	return result.data, result.image, nil
}
