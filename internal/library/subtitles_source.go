package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

var subtitleSourceWorkers = make(chan struct{}, 4)

type subtitleWorkResult struct {
	content SubtitleContent
	err     error
}

// Cancellation abandons delivery, not the worker's slot. Blocking filesystem
// work retains its slot until every descriptor and buffer has been cleaned up.
func runSubtitleWorker(ctx context.Context, slots chan struct{}, work func() (SubtitleContent, error)) (SubtitleContent, error) {
	if err := ctx.Err(); err != nil {
		return SubtitleContent{}, err
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return SubtitleContent{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slots
		return SubtitleContent{}, err
	}
	result := make(chan subtitleWorkResult)
	go func() {
		defer func() { <-slots }()
		if ctx.Err() != nil {
			return
		}
		content, err := work()
		if err != nil {
			content = SubtitleContent{}
		}
		select {
		case result <- subtitleWorkResult{content: content, err: err}:
		case <-ctx.Done():
		}
	}()
	select {
	case result := <-result:
		if err := ctx.Err(); err != nil {
			return SubtitleContent{}, err
		}
		return result.content, result.err
	case <-ctx.Done():
		return SubtitleContent{}, ctx.Err()
	}
}

// ReadSubtitle accepts catalog identifiers only. Every request checks current
// authorization, the primary media snapshot, and the sidecar bytes even when
// an HTTP caller already holds a converted or conditional response cache.
func (s *Store) ReadSubtitle(ctx context.Context, userID, itemID, mediaSourceID string, index int) (SubtitleContent, error) {
	return s.ReadSubtitleFor(ctx, Subject{UserID: userID}, itemID, mediaSourceID, index)
}

// ReadSubtitleFor authorizes and checks both catalog sources before storage work.
func (s *Store) ReadSubtitleFor(ctx context.Context, subject Subject, itemID, mediaSourceID string, index int) (SubtitleContent, error) {
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') ||
		strings.ContainsRune(mediaSourceID, '\x00') || index < 0 || index > maxSubtitleStreamIndex {
		return SubtitleContent{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return SubtitleContent{}, ErrUnavailable
	}
	return runSubtitleWorker(ctx, subtitleSourceWorkers, func() (SubtitleContent, error) {
		primary, source, err := s.readSubtitleSnapshotFor(ctx, subject, itemID, mediaSourceID, index)
		if err != nil {
			return SubtitleContent{}, err
		}
		file, err := s.openMediaSource(ctx, primary)
		if err != nil {
			return SubtitleContent{}, err
		}
		defer file.Close()
		data, err := s.readSubtitleSource(ctx, primary, source)
		if err != nil {
			return SubtitleContent{}, err
		}
		// Recheck the original descriptor and pathname after sidecar storage work.
		// Reading the video contents is unnecessary for this snapshot contract.
		after, err := file.Stat()
		if err != nil || !primary.matches(after) {
			return SubtitleContent{}, fmt.Errorf("%w: primary media changed during subtitle reading", ErrUnavailable)
		}
		current, err := s.openMediaSource(ctx, primary)
		if err != nil {
			return SubtitleContent{}, err
		}
		defer current.Close()
		currentInfo, err := current.Stat()
		if err != nil || !sameMediaSourceFile(after, currentInfo) {
			return SubtitleContent{}, fmt.Errorf("%w: primary media was replaced during subtitle reading", ErrUnavailable)
		}
		if err := ctx.Err(); err != nil {
			return SubtitleContent{}, err
		}
		modifiedAt := source.ModifiedAt
		if changedAt := time.Unix(0, source.changeTimeNs).UTC(); changedAt.After(modifiedAt) {
			modifiedAt = changedAt
		}
		return SubtitleContent{Data: data, Info: source.Subtitle, ModifiedAt: modifiedAt}, nil
	})
}

func (s *Store) readSubtitleSnapshot(ctx context.Context, userID, itemID, sourceID string, index int) (indexedMediaSource, storedSubtitle, error) {
	return s.readSubtitleSnapshotFor(ctx, Subject{UserID: userID}, itemID, sourceID, index)
}

func (s *Store) readSubtitleSnapshotFor(ctx context.Context, subject Subject, itemID, sourceID string, index int) (indexedMediaSource, storedSubtitle, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return indexedMediaSource{}, storedSubtitle{}, err
	}
	defer tx.Rollback(ctx)
	if !access.canPlay {
		return indexedMediaSource{}, storedSubtitle{}, ErrForbidden
	}
	var snapshot indexedMediaSource
	var modified *time.Time
	item, err := scanItem(tx.QueryRow(ctx, "SELECT "+itemColumns+`,
		i.relative_path, i.file_identity, i.file_size, i.modified_at,
		r.id, r.library_id, r.path, r.allowed_path, r.relative_path
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE i.id = $1 AND NOT i.is_folder AND i.media IS NOT NULL
		AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')
		AND ($2::boolean OR i.library_id = ANY($3::text[]))`, itemID, access.all, access.folders),
		&snapshot.relativePath, &snapshot.identity, &snapshot.mediaFile.Size, &modified,
		&snapshot.root.id, &snapshot.root.libraryID, &snapshot.root.path, &snapshot.root.allowedPath, &snapshot.root.relativePath)
	if errors.Is(err, pgx.ErrNoRows) {
		return indexedMediaSource{}, storedSubtitle{}, ErrNotFound
	}
	if err != nil {
		return indexedMediaSource{}, storedSubtitle{}, fmt.Errorf("%w: read subtitle media snapshot: %w", ErrUnavailable, err)
	}
	if item.Media == nil || len(item.Media.Streams) == 0 || sourceID != media.SourceID(item.ID) {
		return indexedMediaSource{}, storedSubtitle{}, ErrNotFound
	}
	track, err := scanStoredSubtitle(tx.QueryRow(ctx, "SELECT "+subtitleColumns+` FROM item_subtitles s
		WHERE s.item_id = $1 AND s.root_id = $2 AND s.stream_index = $3 AND s.active`, itemID, snapshot.root.id, index))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && track.Index <= highestEmbeddedStreamIndex(item.Media)) {
		return indexedMediaSource{}, storedSubtitle{}, ErrNotFound
	}
	if err != nil {
		return indexedMediaSource{}, storedSubtitle{}, fmt.Errorf("%w: read indexed subtitle: %w", ErrUnavailable, err)
	}
	if item.Media.ProbeVersion < media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 ||
		modified == nil || modified.IsZero() || snapshot.identity == "" || snapshot.mediaFile.Size <= 0 {
		return indexedMediaSource{}, storedSubtitle{}, fmt.Errorf("%w: primary media snapshot is outdated; rescan required", ErrUnavailable)
	}
	item.CanPlay = true
	snapshot.mediaFile.Item = item
	snapshot.mediaFile.SourceID = sourceID
	snapshot.mediaFile.ModifiedAt = modified.UTC()
	if err := validateMediaSource(snapshot); err != nil {
		return indexedMediaSource{}, storedSubtitle{}, err
	}
	if err := validateSubtitleSnapshot(snapshot, track); err != nil {
		return indexedMediaSource{}, storedSubtitle{}, err
	}
	// Authorization and both catalog sources share one repeatable read snapshot;
	// release the database before touching potentially unavailable storage.
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, storedSubtitle{}, fmt.Errorf("%w: complete authorized subtitle read: %w", ErrUnavailable, err)
	}
	return snapshot, track, nil
}

func validateSubtitleSnapshot(primary indexedMediaSource, source storedSubtitle) error {
	path := filepath.FromSlash(source.relativePath)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(primary.relativePath), filepath.Ext(primary.relativePath)))
	stem := strings.ToLower(strings.TrimSuffix(source.Filename, filepath.Ext(source.Filename)))
	validName := stem == base || strings.HasPrefix(stem, base+".")
	if !validMediaSourceRelativePath(path) || filepath.Clean(path) != path ||
		filepath.Dir(path) != filepath.Dir(filepath.FromSlash(primary.relativePath)) ||
		source.rootID != primary.root.id || !validName || source.identity == "" || source.changeTimeNs <= 0 ||
		source.Size < 1 || source.Size > subtitle.MaxInputBytes || source.ModifiedAt.IsZero() ||
		source.MIMEType == "" || source.MIMEType != subtitleMIME(source.Codec) ||
		strings.ToLower(filepath.Ext(source.Filename)) != "."+source.Codec || len(source.Tag) != 64 {
		return fmt.Errorf("%w: invalid indexed subtitle path or metadata", ErrUnavailable)
	}
	if _, err := hex.DecodeString(source.Tag); err != nil || strings.ToLower(source.Tag) != source.Tag {
		return fmt.Errorf("%w: invalid indexed subtitle hash", ErrUnavailable)
	}
	if _, ok := subtitleNameMetadata(source.Filename, stem[len(base):], source.Codec); !ok {
		return fmt.Errorf("%w: invalid indexed subtitle filename", ErrUnavailable)
	}
	return nil
}

func (source storedSubtitle) matches(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Size() == source.Size && fileIdentity(info) == source.identity &&
		catalogModifiedTime(info).Equal(source.ModifiedAt) && media.FileChangeTime(info) == source.changeTimeNs
}

func (s *Store) readSubtitleSource(ctx context.Context, primary indexedMediaSource, source storedSubtitle) ([]byte, error) {
	root, err := s.openLibraryRoot(primary.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	directoryPath := filepath.Dir(filepath.FromSlash(source.relativePath))
	parent, err := openRegisteredRoot(root, directoryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: subtitle directory cannot be opened safely", ErrUnavailable)
	}
	defer parent.Close()
	before, err := parent.Lstat(source.Filename)
	if err != nil || !source.matches(before) {
		return nil, fmt.Errorf("%w: indexed subtitle changed before opening; rescan required", ErrUnavailable)
	}
	file, err := openScanFile(parent, source.Filename)
	if err != nil {
		return nil, fmt.Errorf("%w: indexed subtitle cannot be opened safely", ErrUnavailable)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !source.matches(opened) || !sameMediaSourceFile(before, opened) {
		return nil, fmt.Errorf("%w: indexed subtitle changed while opening", ErrUnavailable)
	}
	data, err := readSubtitleBytes(ctx, file)
	if err != nil {
		return nil, fmt.Errorf("%w: read subtitle bytes: %w", ErrUnavailable, err)
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != source.Size || hex.EncodeToString(digest[:]) != source.Tag {
		return nil, fmt.Errorf("%w: indexed subtitle content changed; rescan required", ErrUnavailable)
	}
	if _, err := subtitle.Parse(data, subtitle.Format(source.Codec)); err != nil {
		return nil, fmt.Errorf("%w: indexed subtitle is invalid: %w", ErrUnavailable, err)
	}
	currentRoot, err := s.openLibraryRoot(primary.root)
	if err != nil {
		return nil, err
	}
	defer currentRoot.Close()
	currentParent, err := openRegisteredRoot(currentRoot, directoryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: subtitle directory changed during reading", ErrUnavailable)
	}
	defer currentParent.Close()
	current, currentErr := currentParent.Lstat(source.Filename)
	after, afterErr := file.Stat()
	if !sameMediaSourceDirectory(root, currentRoot) || !sameMediaSourceDirectory(parent, currentParent) ||
		currentErr != nil || afterErr != nil || !source.matches(current) || !source.matches(after) ||
		!sameMediaSourceFile(opened, current) || !sameMediaSourceFile(opened, after) {
		return nil, fmt.Errorf("%w: indexed subtitle changed during reading; rescan required", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
