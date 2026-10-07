package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

const maxEmbeddedArtworkCacheBytes int64 = 1 << 30
const maxEmbeddedArtworkCacheEntries int64 = 25000

// Internal extraction fences the media facts independently from permission
// revisions. Publication still records the current external projection stamp.
const embeddedArtworkScanSourceSQL = `md5(jsonb_build_array(i.root_id,
	i.relative_path,i.file_identity,i.file_size,extract(epoch FROM i.modified_at),i.media)::text)`

// Source facts and cache metadata share one statement snapshot. The source
// read needs only cache reuse fields and never loads the artwork bytes.
const embeddedArtworkScanSnapshotSQL = `/* embedded_artwork_scan_snapshot */ SELECT i.id, i.library_id, i.type, i.path, i.media,
	i.relative_path, i.file_identity, i.file_size, i.modified_at,
	r.id, r.library_id, r.path, r.allowed_path, r.relative_path, r.binding_revision,
	` + embeddedArtworkSourceRevisionSQL + `,` + embeddedArtworkScanSourceSQL + `,
	COALESCE(e.source_revision,''),COALESCE(e.status,''),COALESCE(e.extraction_version,0)
	FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
	LEFT JOIN item_embedded_artwork e ON e.item_id = i.id
	WHERE i.id = $1 AND NOT i.is_folder AND i.media IS NOT NULL
	AND i.type IN ('Movie', 'Episode', 'Video', 'Audio') AND `

type embeddedArtworkScanSnapshot struct {
	snapshot                                        indexedMediaSource
	source, sourceFacts, cachedSource, cachedStatus string
	cachedVersion                                   int
}

// A false validity result means the source is missing or has invalid indexed
// facts. Query and scan failures remain errors rather than successful skips.
func (s *Store) readEmbeddedArtworkScanSnapshot(ctx context.Context, itemID string) (embeddedArtworkScanSnapshot, bool, error) {
	var result embeddedArtworkScanSnapshot
	var modified *time.Time
	var encoded []byte
	snapshot := &result.snapshot
	item := &snapshot.mediaFile.Item
	access := unrestrictedLibraryAccess()
	err := s.pool.QueryRow(ctx, embeddedArtworkScanSnapshotSQL+access.directSQL("i"), itemID).
		Scan(&item.ID, &item.LibraryID, &item.Type, &item.Path, &encoded,
			&snapshot.relativePath, &snapshot.identity, &snapshot.mediaFile.Size, &modified,
			&snapshot.root.id, &snapshot.root.libraryID, &snapshot.root.path, &snapshot.root.allowedPath, &snapshot.root.relativePath, &snapshot.rootBindingRevision,
			&result.source, &result.sourceFacts, &result.cachedSource, &result.cachedStatus, &result.cachedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return embeddedArtworkScanSnapshot{}, false, nil
	}
	if err != nil {
		return embeddedArtworkScanSnapshot{}, false, err
	}
	if len(encoded) != 0 && string(encoded) != "null" {
		item.Media = &media.Info{}
		if err := json.Unmarshal(encoded, item.Media); err != nil {
			return embeddedArtworkScanSnapshot{}, false, nil
		}
	}
	result.snapshot, err = prepareIndexedMediaSourceSnapshot(result.snapshot, "", modified, access.canPlay)
	if err != nil {
		return embeddedArtworkScanSnapshot{}, false, nil
	}
	result.snapshot, err = finishIndexedMediaSourceSnapshot(result.snapshot)
	if err != nil {
		return embeddedArtworkScanSnapshot{}, false, nil
	}
	return result, true, nil
}

type embeddedArtworkExtractor interface {
	ExtractEmbeddedArtwork(context.Context, *os.File, media.Info) (media.EmbeddedArtworkResult, error)
}

// scanEmbeddedArtwork runs after both fresh and cached successful Audio visits.
// Failed extraction records a retryable failure without retaining stale bytes;
// a complete no-picture result is distinct and reusable for this exact source.
// Source replacement invalidates visibility immediately through the projection
// predicate, before a later scan can replace or remove the persisted row.
func (state *scanState) scanEmbeddedArtwork(itemID, itemType, relative string, file *os.File, probe media.Info) error {
	return state.retrySidecarScan(func() error {
		return state.scanEmbeddedArtworkAttempt(itemID, itemType, relative, file, probe)
	})
}

func (state *scanState) scanEmbeddedArtworkAttempt(itemID, itemType, relative string, file *os.File, probe media.Info) (resultErr error) {
	options := EffectiveLibraryOptions(state.library)
	if itemType != "Audio" || !options.EnableLocalImages || !options.EnableEmbeddedArtwork {
		return nil
	}
	extractor, enabled := state.store.prober.(embeddedArtworkExtractor)
	if !enabled {
		return nil
	}
	ctx := media.WithBackgroundProcess(state.task.ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if file == nil || !validImageScanPath(relative) {
		state.warnings++
		return nil
	}
	read, valid, err := state.store.readEmbeddedArtworkScanSnapshot(ctx, itemID)
	if err != nil {
		return err
	}
	if !valid {
		state.warnings++
		return nil
	}
	snapshot := read.snapshot
	source, sourceFacts := read.source, read.sourceFacts
	if snapshot.root.id != state.root.id || snapshot.relativePath != filepath.ToSlash(relative) || snapshot.mediaFile.Item.Type != "Audio" {
		state.warnings++
		return nil
	}
	operation, row, err := state.prepareSidecarScanIO()
	if err != nil {
		return scanReadFailure(err)
	}
	defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	var result media.EmbeddedArtworkResult
	var extractErr error
	stable, cached := false, false
	if err := operation.Run(ctx, state.root.id, primaryio.Background, func(work context.Context) error {
		if err := state.checkSidecarScanAuthority(work, row); err != nil {
			return err
		}
		if err := state.checkEmbeddedArtworkSource(relative, file, snapshot); err != nil {
			state.warnings++
			return nil
		}
		if source == read.cachedSource && read.cachedVersion == media.EmbeddedArtworkVersion && (read.cachedStatus == "ready" || read.cachedStatus == "none") && !state.task.job.ForceProbe {
			stable, cached = true, true
			return nil
		}
		// Extraction joins each exact FFprobe/FFmpeg reader and its cleanup.
		// Unknown retirement keeps this operation and phase quarantined.
		result, extractErr = extractor.ExtractEmbeddedArtwork(work, file, probe)
		if errors.Is(extractErr, media.ErrProcessRetirementUnknown) {
			return extractErr
		}
		if err := work.Err(); err != nil {
			return err
		}
		if err := state.checkEmbeddedArtworkSource(relative, file, snapshot); err != nil {
			state.warnings++
			return nil
		}
		stable = true
		return nil
	}); err != nil {
		return scanReadFailure(err)
	}
	if !stable || cached {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
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
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(writeTx, resultErr)) }()
	if err := state.checkSidecarScanRootTx(writeTx, row); err != nil {
		return err
	}
	var currentSource, currentFacts, libraryID, parentID string
	if err := writeTx.QueryRow(ctx, `SELECT `+embeddedArtworkSourceRevisionSQL+`,`+embeddedArtworkScanSourceSQL+`,i.library_id,COALESCE(i.parent_id,'') FROM items i
		WHERE i.id=$1 AND i.root_id=$2 AND i.type='Audio' AND NOT i.is_folder FOR UPDATE OF i`, itemID, state.root.id).Scan(&currentSource, &currentFacts, &libraryID, &parentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			state.warnings++
			return nil
		}
		return err
	}
	if currentFacts != sourceFacts || libraryID != state.library.ID {
		state.warnings++
		return nil
	}
	source = currentSource
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
	if err := operation.RunImmediate(ctx, state.root.id, primaryio.Background, func(context.Context) error {
		return state.checkEmbeddedArtworkSource(relative, file, snapshot)
	}); err != nil {
		return scanReadFailure(err)
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
