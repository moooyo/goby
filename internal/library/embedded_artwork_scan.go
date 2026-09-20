package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/media"
)

const maxEmbeddedArtworkCacheBytes int64 = 1 << 30
const maxEmbeddedArtworkCacheEntries int64 = 25000

type embeddedArtworkExtractor interface {
	ExtractEmbeddedArtwork(context.Context, *os.File, media.Info) (media.EmbeddedArtworkResult, error)
}

// scanEmbeddedArtwork runs after both fresh and cached successful Audio visits.
// Failed extraction records a retryable failure without retaining stale bytes;
// a complete no-picture result is distinct and reusable for this exact source.
// Source replacement invalidates visibility immediately through the projection
// predicate, before a later scan can replace or remove the persisted row.
func (state *scanState) scanEmbeddedArtwork(itemID, itemType, relative string, file *os.File, probe media.Info) error {
	if itemType != "Audio" || !EffectiveLibraryOptions(state.library).EnableLocalImages {
		return nil
	}
	extractor, enabled := state.store.prober.(embeddedArtworkExtractor)
	if !enabled {
		return nil
	}
	ctx := state.task.ctx
	if err := ctx.Err(); err != nil {
		return err
	}
	if file == nil || !validImageScanPath(relative) {
		state.warnings++
		return nil
	}
	tx, err := state.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer rollback(tx)
	snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), itemID, "")
	if err != nil {
		state.warnings++
		return nil
	}
	var source, cachedSource, cachedStatus string
	var cachedVersion int
	err = tx.QueryRow(ctx, `SELECT `+embeddedArtworkSourceRevisionSQL+`,COALESCE(e.source_revision,''),COALESCE(e.status,''),COALESCE(e.extraction_version,0)
		FROM items i LEFT JOIN item_embedded_artwork e ON e.item_id=i.id WHERE i.id=$1`, itemID).Scan(&source, &cachedSource, &cachedStatus, &cachedVersion)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if snapshot.root.id != state.root.id || snapshot.relativePath != filepath.ToSlash(relative) || snapshot.mediaFile.Item.Type != "Audio" {
		state.warnings++
		return nil
	}
	if err := state.checkEmbeddedArtworkSource(relative, file, snapshot); err != nil {
		state.warnings++
		return nil
	}
	if source == cachedSource && cachedVersion == media.EmbeddedArtworkVersion && (cachedStatus == "ready" || cachedStatus == "none") && !state.task.job.ForceProbe {
		return nil
	}
	result, extractErr := extractor.ExtractEmbeddedArtwork(ctx, file, probe)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := state.checkEmbeddedArtworkSource(relative, file, snapshot); err != nil {
		state.warnings++
		return nil
	}
	status, failure := "none", ""
	var chosen *media.EmbeddedPicture
	if extractErr == nil {
		chosen, extractErr = validateEmbeddedArtworkResult(ctx, result)
	}
	if extractErr == nil {
		extractErr = embeddedArtworkMatchesProbe(result, probe)
	}
	if extractErr != nil {
		status, failure = "failed", "extraction_failed"
		state.warnings++
	} else if chosen != nil {
		status = "ready"
	}
	writeTx, err := state.store.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(writeTx)
	var currentSource, libraryID, parentID string
	if err := writeTx.QueryRow(ctx, `SELECT `+embeddedArtworkSourceRevisionSQL+`,i.library_id,COALESCE(i.parent_id,'') FROM items i
		WHERE i.id=$1 AND i.root_id=$2 AND i.type='Audio' AND NOT i.is_folder FOR UPDATE OF i`, itemID, state.root.id).Scan(&currentSource, &libraryID, &parentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			state.warnings++
			return nil
		}
		return err
	}
	if currentSource != source || libraryID != state.library.ID {
		state.warnings++
		return nil
	}
	if err := state.checkEmbeddedArtworkSource(relative, file, snapshot); err != nil {
		state.warnings++
		return nil
	}
	var previousTag, previousSource string
	if err := writeTx.QueryRow(ctx, `SELECT COALESCE((SELECT source_hash FROM item_embedded_artwork WHERE item_id=$1),''),
		COALESCE((SELECT source_revision FROM item_embedded_artwork WHERE item_id=$1),'')`, itemID).Scan(&previousTag, &previousSource); err != nil {
		return err
	}
	if chosen != nil && status == "ready" {
		var bytesUsed, entries, previous int64
		if err := writeTx.QueryRow(ctx, `SELECT COALESCE(sum(octet_length(content)),0),count(*) FILTER(WHERE status='ready'),
			COALESCE((SELECT octet_length(content) FROM item_embedded_artwork WHERE item_id=$1),0) FROM item_embedded_artwork`, itemID).Scan(&bytesUsed, &entries, &previous); err != nil {
			return err
		}
		if bytesUsed-previous+int64(len(chosen.Data)) > maxEmbeddedArtworkCacheBytes || previous == 0 && entries >= maxEmbeddedArtworkCacheEntries {
			status, failure, chosen = "failed", "cache_full", nil
			state.warnings++
		}
	}
	var streamIndex, pictureType, hash, mime, width, height, content any
	nextTag := ""
	if chosen != nil && status == "ready" {
		streamIndex, pictureType, hash, mime, width, height, content = chosen.StreamIndex, chosen.PictureType, chosen.Hash, chosen.MIMEType, chosen.Width, chosen.Height, chosen.Data
		nextTag = chosen.Hash
	}
	_, err = writeTx.Exec(ctx, `INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,status,failure_code,stream_index,picture_type,source_hash,mime_type,width,height,content)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(item_id) DO UPDATE SET
		source_revision=excluded.source_revision,probe_version=excluded.probe_version,extraction_version=excluded.extraction_version,status=excluded.status,failure_code=excluded.failure_code,
		stream_index=excluded.stream_index,picture_type=excluded.picture_type,source_hash=excluded.source_hash,mime_type=excluded.mime_type,width=excluded.width,height=excluded.height,content=excluded.content,inspected_at=clock_timestamp()`,
		itemID, source, probe.ProbeVersion, media.EmbeddedArtworkVersion, status, failure, streamIndex, pictureType, hash, mime, width, height, content)
	if err != nil {
		return err
	}
	if previousTag != nextTag || nextTag != "" && previousSource != source {
		if err := recordCatalogChanges(writeTx, CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: libraryID, ParentID: parentID}); err != nil {
			return err
		}
	}
	return writeTx.Commit(ctx)
}

func (state *scanState) checkEmbeddedArtworkSource(relative string, file *os.File, snapshot indexedMediaSource) error {
	held, err := file.Stat()
	if err != nil || !snapshot.matches(held) {
		return ErrSourceChanged
	}
	current, err := state.opened.Lstat(relative)
	if err != nil || !snapshot.matches(current) || !sameMediaSourceFile(held, current) {
		return ErrSourceChanged
	}
	return nil
}

// Validate the capability result even for alternate Prober implementations;
// untrusted declared hashes/dimensions cannot enter the automatic artwork layer.
func validateEmbeddedArtworkResult(ctx context.Context, result media.EmbeddedArtworkResult) (*media.EmbeddedPicture, error) {
	if result.Version != media.EmbeddedArtworkVersion || len(result.Pictures) > media.MaxEmbeddedArtworkPictures {
		return nil, ErrInvalidInput
	}
	var selected *media.EmbeddedPicture
	seen, total := make(map[int]bool), 0
	for _, picture := range result.Pictures {
		total += len(picture.Data)
		if picture.StreamIndex < 0 || picture.StreamIndex > 4095 || seen[picture.StreamIndex] || len(picture.Data) == 0 || len(picture.Data) > media.MaxEmbeddedArtworkBytes || total > media.MaxEmbeddedArtworkTotalBytes {
			return nil, ErrInvalidInput
		}
		if picture.PictureType != "Front" && picture.PictureType != "Other" && picture.PictureType != "Back" {
			return nil, ErrInvalidInput
		}
		seen[picture.StreamIndex] = true
		info, err := artwork.InspectContext(ctx, bytes.NewReader(picture.Data))
		if err != nil || info.Tag != picture.Hash || info.Width != picture.Width || info.Height != picture.Height || info.MIMEType != picture.MIMEType {
			return nil, ErrInvalidInput
		}
		if selected == nil || embeddedPictureOrder(picture) < embeddedPictureOrder(*selected) {
			copy := picture
			selected = &copy
		}
	}
	return selected, nil
}

func embeddedPictureOrder(picture media.EmbeddedPicture) int {
	rank := 1
	if picture.PictureType == "Front" {
		rank = 0
	} else if picture.PictureType == "Back" {
		rank = 2
	}
	return rank*4096 + picture.StreamIndex
}

func embeddedArtworkMatchesProbe(result media.EmbeddedArtworkResult, probe media.Info) error {
	attached := make(map[int]media.Stream)
	for _, stream := range probe.Streams {
		if !stream.IsAttachedPicture {
			continue
		}
		if _, exists := attached[stream.Index]; exists || stream.IsExternal || stream.CodecType != "video" {
			return ErrInvalidInput
		}
		attached[stream.Index] = stream
	}
	if len(attached) != len(result.Pictures) {
		return ErrInvalidInput
	}
	for _, picture := range result.Pictures {
		stream, found := attached[picture.StreamIndex]
		if !found || stream.Width != picture.Width || stream.Height != picture.Height {
			return ErrInvalidInput
		}
	}
	return nil
}
