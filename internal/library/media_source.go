package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// MediaFile describes the exact indexed filesystem snapshot opened for an
// authorized user. ETag is a quoted HTTP validator identifying SourceID, inode
// identity, size, modification time, and Linux change time. It is a server-owned
// snapshot label, not a content hash.
type MediaFile struct {
	Item                          Item
	SourceID, Container, MIMEType string
	ETag                          string
	Size                          int64
	ModifiedAt                    time.Time
}

type indexedMediaSource struct {
	mediaFile    MediaFile
	root         libraryRoot
	relativePath string
	identity     string
}

var mediaSourceWorkers = make(chan struct{}, 4)

// ErrSourceChanged distinguishes a confirmed metadata/identity mismatch from
// transient filesystem or database unavailability. Callers still receive
// ErrUnavailable as well, preserving the existing delivery error contract.
var ErrSourceChanged = errors.New("indexed media source changed")

type mediaSourceWorkResult struct {
	file   *os.File
	source MediaFile
	err    error
}

// A canceled caller leaves its worker slot occupied until storage work and any
// undelivered descriptor cleanup finish. Results have an unbuffered ownership
// handoff, preventing a descriptor from being stranded in an abandoned channel.
func runMediaSourceWorker(ctx context.Context, slots chan struct{}, work func() (*os.File, MediaFile, error)) (*os.File, MediaFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, MediaFile{}, err
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, MediaFile{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slots
		return nil, MediaFile{}, err
	}
	result := make(chan mediaSourceWorkResult)
	go func() {
		defer func() { <-slots }()
		if ctx.Err() != nil {
			return
		}
		file, source, err := work()
		if err != nil && file != nil {
			_ = file.Close()
			file = nil
		}
		select {
		case result <- mediaSourceWorkResult{file: file, source: source, err: err}:
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	select {
	case outcome := <-result:
		if err := ctx.Err(); err != nil {
			if outcome.file != nil {
				_ = outcome.file.Close()
			}
			return nil, MediaFile{}, err
		}
		return outcome.file, outcome.source, outcome.err
	case <-ctx.Done():
		return nil, MediaFile{}, ctx.Err()
	}
}

// OpenMedia opens only the catalog's original local source. The optional source
// identifier selects that same source; it is not a pathname, URL, or credential.
// The caller owns a successful descriptor at offset zero. File contents are not
// hashed or read here, and a regular file remains mutable after it is returned.
func (s *Store) OpenMedia(ctx context.Context, userID, itemID, mediaSourceID string) (*os.File, MediaFile, error) {
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') || strings.ContainsRune(mediaSourceID, '\x00') {
		return nil, MediaFile{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, MediaFile{}, ErrUnavailable
	}
	return runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		snapshot, err := s.readMediaSource(ctx, userID, itemID, mediaSourceID)
		if err != nil {
			return nil, MediaFile{}, err
		}
		file, err := s.openMediaSource(ctx, snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		return file, snapshot.mediaFile, nil
	})
}

func (s *Store) readMediaSource(ctx context.Context, userID, itemID, sourceID string) (indexedMediaSource, error) {
	tx, access, err := s.beginUserRead(ctx, userID)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer tx.Rollback(ctx)
	var policy []byte
	if err := tx.QueryRow(ctx, "SELECT policy FROM users WHERE id = $1", userID).Scan(&policy); err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: read media playback policy: %w", ErrUnavailable, err)
	}
	if !playbackAllowed(policy) {
		return indexedMediaSource{}, ErrForbidden
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
		return indexedMediaSource{}, ErrNotFound
	}
	if err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: read media source: %w", ErrUnavailable, err)
	}
	if item.Media == nil || len(item.Media.Streams) == 0 {
		return indexedMediaSource{}, ErrNotFound
	}
	expectedSourceID := media.SourceID(item.ID)
	if sourceID != "" && sourceID != expectedSourceID {
		return indexedMediaSource{}, ErrNotFound
	}
	if item.Media.ProbeVersion < media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 {
		return indexedMediaSource{}, fmt.Errorf("%w: media probe snapshot is outdated; rescan required", ErrUnavailable)
	}
	if modified == nil || modified.IsZero() || snapshot.identity == "" || snapshot.mediaFile.Size <= 0 {
		return indexedMediaSource{}, fmt.Errorf("%w: media source has no valid indexed snapshot", ErrUnavailable)
	}
	item.CanPlay = true
	items := []Item{item}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		return indexedMediaSource{}, err
	}
	item = items[0]
	snapshot.mediaFile.Item = item
	snapshot.mediaFile.SourceID = expectedSourceID
	snapshot.mediaFile.ModifiedAt = modified.UTC()
	if err := validateMediaSource(snapshot); err != nil {
		return indexedMediaSource{}, err
	}
	snapshot.mediaFile.Container = media.CanonicalContainer(*item.Media, item.Path)
	snapshot.mediaFile.MIMEType = media.SourceMIMEType(*item.Media, item.Path)
	snapshot.mediaFile.ETag = mediaSnapshotTag(snapshot)
	// Release the policy snapshot before potentially blocking filesystem calls;
	// unavailable NFS storage must not retain a database connection indefinitely.
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: complete authorized media source read: %w", ErrUnavailable, err)
	}
	return snapshot, nil
}

func validateMediaSource(snapshot indexedMediaSource) error {
	if snapshot.mediaFile.Item.Media == nil {
		return fmt.Errorf("%w: indexed media facts are missing", ErrUnavailable)
	}
	path := filepath.FromSlash(snapshot.relativePath)
	root := snapshot.root
	if path == "." || !validMediaSourceRelativePath(path) || filepath.Clean(path) != path ||
		!filepath.IsAbs(root.path) || !filepath.IsAbs(root.allowedPath) ||
		strings.ContainsRune(root.path, '\x00') || strings.ContainsRune(root.allowedPath, '\x00') ||
		hasTraversal(root.path) || hasTraversal(root.allowedPath) || !validMediaSourceRelativePath(root.relativePath) ||
		filepath.Clean(root.path) != filepath.Join(root.allowedPath, root.relativePath) {
		return fmt.Errorf("%w: invalid indexed media path or root", ErrUnavailable)
	}
	expectedPath := filepath.Join(root.path, path)
	if !pathWithin(root.path, expectedPath) || snapshot.mediaFile.Item.Path != expectedPath ||
		snapshot.mediaFile.Item.LibraryID != root.libraryID {
		return fmt.Errorf("%w: indexed media path does not match its root", ErrUnavailable)
	}
	if snapshot.mediaFile.Item.Media.Size > 0 && snapshot.mediaFile.Item.Media.Size != snapshot.mediaFile.Size {
		return fmt.Errorf("%w: probed media size disagrees with its file snapshot", ErrUnavailable)
	}
	return nil
}

func validMediaSourceRelativePath(path string) bool {
	return path != "" && filepath.IsLocal(path) && !hasTraversal(path) && !strings.ContainsAny(path, "\\\x00")
}

func mediaSnapshotTag(snapshot indexedMediaSource) string {
	value := fmt.Sprintf("goby.media.snapshot.v2\x00%s\x00%s\x00%d\x00%d\x00%d", snapshot.mediaFile.SourceID,
		snapshot.identity, snapshot.mediaFile.Size, snapshot.mediaFile.ModifiedAt.UnixNano(), snapshot.mediaFile.Item.Media.FileChangeTimeNs)
	digest := sha256.Sum256([]byte(value))
	return `"snapshot-` + hex.EncodeToString(digest[:]) + `"`
}

func (s *Store) openMediaSource(ctx context.Context, snapshot indexedMediaSource) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.openLibraryRoot(snapshot.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	path := filepath.FromSlash(snapshot.relativePath)
	parent, err := openRegisteredRoot(root, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: media parent directory cannot be opened safely", ErrUnavailable)
	}
	defer parent.Close()
	name := filepath.Base(path)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("%w: indexed media metadata cannot be read", ErrUnavailable)
	}
	if !snapshot.matches(before) {
		return nil, fmt.Errorf("%w: %w before opening; rescan required", ErrUnavailable, ErrSourceChanged)
	}
	file, err := openScanFile(parent, name)
	if err != nil {
		return nil, fmt.Errorf("%w: indexed media cannot be opened safely", ErrUnavailable)
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: open media metadata cannot be read", ErrUnavailable)
	}
	if !snapshot.matches(opened) || !sameMediaSourceFile(before, opened) {
		return nil, fmt.Errorf("%w: %w while opening; rescan required", ErrUnavailable, ErrSourceChanged)
	}
	currentRoot, err := s.openLibraryRoot(snapshot.root)
	if err != nil {
		return nil, err
	}
	defer currentRoot.Close()
	if !sameMediaSourceDirectory(root, currentRoot) {
		return nil, fmt.Errorf("%w: registered media root changed while opening", ErrUnavailable)
	}
	currentParent, err := openRegisteredRoot(currentRoot, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: media directory changed while opening", ErrUnavailable)
	}
	defer currentParent.Close()
	if !sameMediaSourceDirectory(parent, currentParent) {
		return nil, fmt.Errorf("%w: media directory was replaced while opening", ErrUnavailable)
	}
	current, err := currentParent.Lstat(name)
	after, afterErr := file.Stat()
	if err != nil || afterErr != nil {
		return nil, fmt.Errorf("%w: media metadata cannot be rechecked", ErrUnavailable)
	}
	if !snapshot.matches(current) || !snapshot.matches(after) ||
		!sameMediaSourceFile(opened, current) || !sameMediaSourceFile(opened, after) {
		return nil, fmt.Errorf("%w: %w while rechecking; rescan required", ErrUnavailable, ErrSourceChanged)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: media descriptor cannot be positioned", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return file, nil
}

func (snapshot indexedMediaSource) matches(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Size() == snapshot.mediaFile.Size &&
		fileIdentity(info) == snapshot.identity && catalogModifiedTime(info).Equal(snapshot.mediaFile.ModifiedAt) &&
		media.FileChangeTime(info) == snapshot.mediaFile.Item.Media.FileChangeTimeNs
}

func sameMediaSourceFile(first, second os.FileInfo) bool {
	return first != nil && second != nil && os.SameFile(first, second) && first.Size() == second.Size() &&
		first.ModTime().Equal(second.ModTime()) && media.FileChangeTime(first) == media.FileChangeTime(second)
}

func sameMediaSourceDirectory(first, second *os.Root) bool {
	before, err := first.Stat(".")
	if err != nil || !before.IsDir() {
		return false
	}
	after, err := second.Stat(".")
	return err == nil && after.IsDir() && os.SameFile(before, after)
}
