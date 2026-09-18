package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// fileDeletionSpec is a server-owned filesystem precondition. StageIdentity is
// the private directory identity; Identity always identifies the media payload.
// The journal persists root mapping separately from this portable document.
type fileDeletionSpec struct {
	Root               libraryRoot `json:"-"`
	RelativePath       string
	Identity           string
	Size               int64
	ModifiedAt         time.Time
	ChangeTimeNs       int64
	StageName          string
	StageIdentity      string
	StagedChangeTimeNs int64
}

// fileDeletionCapture owns descriptors only. Its operations are filesystem-only
// and must run outside every database transaction. Close never removes data.
// A prepared capture cannot purge: reopen with journaled stage facts after the
// catalog removal has committed to authorize that irreversible phase.
type fileDeletionCapture struct {
	mu              sync.Mutex
	store           *Store
	spec            fileDeletionSpec
	lease           *libraryRootLease
	root            *os.Root
	parent          *os.Root
	stage           *os.Root
	source          *os.File
	sourceInfo      os.FileInfo
	stageIdentity   string
	stageChangeTime int64
	stageExists     bool
	payloadExists   bool
	purgeAuthorized bool
	restored        bool
	closed          bool
}

func (s *Store) prepareFileDeletion(ctx context.Context, spec fileDeletionSpec) (_ *fileDeletionCapture, resultErr error) {
	if ctx == nil || !validFileDeletionSpec(spec) {
		return nil, ErrInvalidInput
	}
	if s == nil || !fileDeletionSupported() {
		return nil, fmt.Errorf("%w: safe file deletion requires Linux", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	capture := &fileDeletionCapture{store: s, spec: spec, purgeAuthorized: spec.StageIdentity != ""}
	defer func() {
		if resultErr != nil {
			_ = capture.Close()
		}
	}()
	var err error
	capture.lease, err = s.leaseLibraryRoot(spec.Root)
	if err != nil {
		return nil, err
	}
	capture.root, err = capture.lease.Open()
	if err != nil {
		return nil, err
	}
	path := filepath.FromSlash(spec.RelativePath)
	capture.parent, err = openRegisteredRoot(capture.root, filepath.Dir(path))
	if err != nil {
		return nil, fileDeletionError("open media parent", err)
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	stage, stageInfo, err := openFileDeletionStage(capture.parent, spec.StageName)
	if err != nil {
		return nil, err
	}
	if stage != nil {
		capture.stage, capture.stageExists = stage, true
		capture.stageIdentity = fileIdentity(stageInfo)
		if spec.StageIdentity != "" && spec.StageIdentity != capture.stageIdentity {
			return nil, fileDeletionError("staging directory identity changed", ErrSourceChanged)
		}
		payload, info, err := openFileDeletionPayload(stage, "payload", spec, spec.StagedChangeTimeNs)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			capture.source, capture.sourceInfo = payload, info
			capture.payloadExists = true
			capture.stageChangeTime = media.FileChangeTime(info)
		}
	}
	if !capture.payloadExists && !capture.purgeAuthorized {
		// An empty private directory can be left either before rename or after a
		// successful restoration whose journal cleanup did not commit. The latter
		// changes ctime, but only recovery (never another Stage) may accept it.
		expectedChangeTime := spec.ChangeTimeNs
		if capture.stageExists {
			expectedChangeTime = 0
		}
		capture.source, capture.sourceInfo, err = openFileDeletionPayload(capture.parent, filepath.Base(path), spec, expectedChangeTime)
		if err != nil {
			return nil, err
		}
		if capture.source == nil {
			return nil, fileDeletionError("original and staged payload are unavailable", os.ErrNotExist)
		}
		capture.restored = capture.stageExists
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	return capture, nil
}

func validFileDeletionSpec(spec fileDeletionSpec) bool {
	path := filepath.FromSlash(spec.RelativePath)
	root := spec.Root
	if path == "." || len(path) > 4096 || !validMediaSourceRelativePath(path) || filepath.Clean(path) != path ||
		!validFileDeletionIdentity(spec.Identity) || spec.Size <= 0 || spec.ModifiedAt.IsZero() || spec.ChangeTimeNs <= 0 ||
		!validCatalogLibraryIdentifier(root.id) || !validCatalogLibraryIdentifier(root.libraryID) ||
		len(root.path) > 4096 || len(root.allowedPath) > 4096 || len(root.relativePath) > 4096 ||
		!filepath.IsAbs(root.path) || !filepath.IsAbs(root.allowedPath) || hasTraversal(root.path) || hasTraversal(root.allowedPath) ||
		strings.ContainsRune(root.path, '\x00') || strings.ContainsRune(root.allowedPath, '\x00') ||
		!validMediaSourceRelativePath(root.relativePath) || filepath.Clean(root.path) != filepath.Join(root.allowedPath, root.relativePath) ||
		!pathWithin(root.path, filepath.Join(root.path, path)) || !validFileDeletionStageName(spec.StageName) {
		return false
	}
	return spec.StageIdentity == "" && spec.StagedChangeTimeNs == 0 ||
		validFileDeletionIdentity(spec.StageIdentity) && spec.StagedChangeTimeNs > 0
}

func validFileDeletionStageName(name string) bool {
	const prefix = ".goby-delete-"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
		return false
	}
	for _, character := range name[len(prefix):] {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func validFileDeletionIdentity(identity string) bool {
	device, inode, found := strings.Cut(identity, ":")
	if !found || len(identity) > 41 || device == "" || inode == "" {
		return false
	}
	for _, component := range []string{device, inode} {
		for _, character := range component {
			if character < '0' || character > '9' {
				return false
			}
		}
		if _, err := strconv.ParseUint(component, 10, 64); err != nil {
			return false
		}
	}
	return true
}

func (spec fileDeletionSpec) matches(info os.FileInfo, changeTime int64) bool {
	return info != nil && info.Mode().IsRegular() && fileIdentity(info) == spec.Identity &&
		info.Size() == spec.Size && catalogModifiedTime(info).Equal(spec.ModifiedAt) &&
		media.FileChangeTime(info) > 0 && (changeTime == 0 || media.FileChangeTime(info) == changeTime)
}

// Staged includes an empty private staging directory. Prepared-journal retries
// must restore and rescan after either an interrupted rename or a restoration.
func (capture *fileDeletionCapture) Staged() bool {
	if capture == nil {
		return false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.stageExists
}

// HasStagedPayload is conservative after a rename attempt: a filesystem error
// can leave its outcome uncertain. False means no rename was attempted and no
// staged payload was observed, so the caller may discard an unused intent.
func (capture *fileDeletionCapture) HasStagedPayload() bool {
	if capture == nil {
		return false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.payloadExists
}

// deletionStageIsEmpty is an explicit recovery observation, never permission to
// delete a pathname. Callers must serialize the complete deletion operation
// through this observation and journal cleanup. A private directory containing
// any entry, including an unexpected payload type, retains its recovery intent.
func (s *Store) deletionStageIsEmpty(ctx context.Context, spec fileDeletionSpec) (bool, error) {
	if ctx == nil || !validFileDeletionSpec(spec) {
		return false, ErrInvalidInput
	}
	if s == nil || !fileDeletionSupported() {
		return false, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	capture := &fileDeletionCapture{store: s, spec: spec}
	defer capture.Close()
	var err error
	capture.lease, err = s.leaseLibraryRoot(spec.Root)
	if err != nil {
		return false, err
	}
	capture.root, err = capture.lease.Open()
	if err != nil {
		return false, err
	}
	capture.parent, err = openRegisteredRoot(capture.root, filepath.Dir(filepath.FromSlash(spec.RelativePath)))
	if err != nil {
		return false, fileDeletionError("open deletion recovery parent", err)
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return false, err
	}
	stage, info, err := openFileDeletionStage(capture.parent, spec.StageName)
	if err != nil {
		return false, err
	}
	if stage == nil {
		return true, ctx.Err()
	}
	capture.stage, capture.stageExists, capture.stageIdentity = stage, true, fileIdentity(info)
	if spec.StageIdentity != "" && spec.StageIdentity != capture.stageIdentity {
		return false, fileDeletionError("recovery staging identity changed", ErrSourceChanged)
	}
	directory, err := openScanFile(stage, ".")
	if err != nil {
		return false, fileDeletionError("inspect recovery staging directory", err)
	}
	defer directory.Close()
	entries, readErr := directory.ReadDir(1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return false, fileDeletionError("read recovery staging directory", readErr)
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return false, err
	}
	return len(entries) == 0, ctx.Err()
}

func (capture *fileDeletionCapture) Stage(ctx context.Context) (string, int64, error) {
	if capture == nil {
		return "", 0, ErrUnavailable
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if err := capture.check(ctx); err != nil {
		return "", 0, err
	}
	if capture.stageExists || capture.purgeAuthorized || capture.restored {
		return "", 0, fileDeletionError("staging directory already exists or recovery is required", os.ErrExist)
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return "", 0, err
	}
	name := filepath.Base(filepath.FromSlash(capture.spec.RelativePath))
	if err := capture.verifyPayload(capture.parent, name, capture.spec.ChangeTimeNs); err != nil {
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if err := capture.parent.Mkdir(capture.spec.StageName, 0o700); err != nil {
		return "", 0, fileDeletionError("create private staging directory without replacement", err)
	}
	capture.stageExists = true
	stage, info, err := openFileDeletionStage(capture.parent, capture.spec.StageName)
	if err != nil {
		return "", 0, err
	}
	if stage == nil {
		return "", 0, fileDeletionError("new staging directory disappeared", os.ErrNotExist)
	}
	capture.stage, capture.stageExists = stage, true
	capture.stageIdentity = fileIdentity(info)
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return "", 0, err
	}
	if err := capture.verifyPayload(capture.parent, name, capture.spec.ChangeTimeNs); err != nil {
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	// Once the syscall is attempted, preserve the journal even if storage
	// reports an error. A later explicit recovery observes whether it took effect.
	capture.payloadExists = true
	if err := fileDeletionRenameNoReplace(capture.parent, name, stage, "payload"); err != nil {
		return "", 0, fileDeletionError("move indexed media into private staging without replacement", err)
	}
	// Rename changes ctime. Check the captured inode, regular-file type, size
	// and mtime before recording the new ctime. Never unlink an unexpected entry.
	current, statErr := stage.Lstat("payload")
	opened, openErr := capture.source.Stat()
	if statErr != nil || openErr != nil || !capture.spec.matches(current, 0) || !capture.spec.matches(opened, 0) ||
		!sameMediaSourceFile(current, opened) || !os.SameFile(capture.sourceInfo, current) {
		return "", 0, fileDeletionError("source identity changed during staging; retained for recovery", ErrSourceChanged)
	}
	capture.payloadExists, capture.sourceInfo = true, opened
	capture.stageChangeTime = media.FileChangeTime(opened)
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return capture.stageIdentity, capture.stageChangeTime, err
	}
	if err := fileDeletionSyncDirectory(stage); err != nil {
		return capture.stageIdentity, capture.stageChangeTime, err
	}
	if err := fileDeletionSyncDirectory(capture.parent); err != nil {
		return capture.stageIdentity, capture.stageChangeTime, err
	}
	return capture.stageIdentity, capture.stageChangeTime, ctx.Err()
}

func (capture *fileDeletionCapture) Restore(ctx context.Context) error {
	if capture == nil {
		return ErrUnavailable
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if err := capture.check(ctx); err != nil {
		return err
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return err
	}
	name := filepath.Base(filepath.FromSlash(capture.spec.RelativePath))
	if !capture.payloadExists {
		// A known source is necessary: a committed, already-purged capture must
		// not report that it restored a missing file or a replacement pathname.
		changeTime := capture.spec.ChangeTimeNs
		if capture.restored {
			changeTime = media.FileChangeTime(capture.sourceInfo)
		}
		return capture.verifyPayload(capture.parent, name, changeTime)
	}
	if err := capture.verifyPayload(capture.stage, "payload", capture.stageChangeTime); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fileDeletionRenameNoReplace(capture.stage, "payload", capture.parent, name); err != nil {
		return fileDeletionError("restore media without replacing an existing pathname", err)
	}
	current, statErr := capture.parent.Lstat(name)
	opened, openErr := capture.source.Stat()
	if statErr != nil || openErr != nil || !capture.spec.matches(current, 0) || !capture.spec.matches(opened, 0) ||
		!sameMediaSourceFile(current, opened) || !os.SameFile(capture.sourceInfo, current) {
		return fileDeletionError("restored media identity changed; no file was removed", ErrSourceChanged)
	}
	capture.payloadExists, capture.restored, capture.sourceInfo = false, true, opened
	if err := fileDeletionSyncDirectory(capture.parent); err != nil {
		return err
	}
	if err := fileDeletionSyncDirectory(capture.stage); err != nil {
		return err
	}
	return ctx.Err()
}

func (capture *fileDeletionCapture) Purge(ctx context.Context) error {
	if capture == nil {
		return ErrUnavailable
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if err := capture.check(ctx); err != nil {
		return err
	}
	if !capture.purgeAuthorized {
		return fileDeletionError("permanent deletion requires journaled staging identity and change time", ErrForbidden)
	}
	if err := capture.verifyNamedDirectories(ctx); err != nil {
		return err
	}
	if capture.stage == nil {
		if _, err := capture.parent.Lstat(capture.spec.StageName); !errors.Is(err, os.ErrNotExist) {
			return fileDeletionError("unexpected staging directory appeared after completed purge", ErrSourceChanged)
		}
		return ctx.Err()
	}
	if !capture.payloadExists {
		if _, err := capture.stage.Lstat("payload"); !errors.Is(err, os.ErrNotExist) {
			return fileDeletionError("unexpected payload appeared after completed purge", ErrSourceChanged)
		}
		return fileDeletionSyncDirectory(capture.stage)
	}
	if err := capture.verifyPayload(capture.stage, "payload", capture.spec.StagedChangeTimeNs); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The source namespace is never unlinked. Only the captured file in an
	// owner-only, identity-checked private directory can reach this primitive.
	if err := fileDeletionUnlink(capture.stage, "payload"); err != nil {
		return fileDeletionError("remove verified staged media", err)
	}
	capture.payloadExists = false
	if err := fileDeletionSyncDirectory(capture.stage); err != nil {
		return err
	}
	return ctx.Err()
}

func (capture *fileDeletionCapture) verifyPayload(directory *os.Root, name string, changeTime int64) error {
	if directory == nil || capture.source == nil {
		return fileDeletionError("captured media descriptor is unavailable", os.ErrNotExist)
	}
	current, err := directory.Lstat(name)
	opened, openedErr := capture.source.Stat()
	if err != nil || openedErr != nil || !capture.spec.matches(current, changeTime) || !capture.spec.matches(opened, changeTime) ||
		!sameMediaSourceFile(current, opened) || !os.SameFile(capture.sourceInfo, current) {
		return fileDeletionError("indexed media identity changed", ErrSourceChanged)
	}
	return nil
}

// Reopen the configured name independently of the cached anchor. A cached
// approved descriptor alone would silently retain a renamed, replaced root.
func (capture *fileDeletionCapture) verifyNamedDirectories(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := capture.store.configuredRootBindingPath(capture.spec.Root)
	if err != nil {
		return err
	}
	approved := approvedRoot{path: path}
	if err := openApprovedRoot(&approved); err != nil {
		return err
	}
	defer approved.root.Close()
	if !sameMediaSourceDirectory(capture.lease.approved, approved.root) {
		return fileDeletionError("approved media root was replaced", ErrSourceChanged)
	}
	root, err := openRegisteredRoot(approved.root, capture.spec.Root.relativePath)
	if err != nil {
		return fileDeletionError("registered media root changed", err)
	}
	defer root.Close()
	if !sameMediaSourceDirectory(capture.root, root) {
		return fileDeletionError("registered media root was replaced", ErrSourceChanged)
	}
	parent, err := openRegisteredRoot(root, filepath.Dir(filepath.FromSlash(capture.spec.RelativePath)))
	if err != nil {
		return fileDeletionError("media parent changed", err)
	}
	defer parent.Close()
	if !sameMediaSourceDirectory(capture.parent, parent) {
		return fileDeletionError("media parent was replaced", ErrSourceChanged)
	}
	if capture.stage != nil {
		stage, info, err := openFileDeletionStage(parent, capture.spec.StageName)
		if err != nil {
			return err
		}
		if stage == nil {
			return fileDeletionError("staging directory disappeared", os.ErrNotExist)
		}
		defer stage.Close()
		if fileIdentity(info) != capture.stageIdentity || !sameMediaSourceDirectory(capture.stage, stage) {
			return fileDeletionError("staging directory was replaced", ErrSourceChanged)
		}
	}
	return ctx.Err()
}

func openFileDeletionStage(parent *os.Root, name string) (*os.Root, os.FileInfo, error) {
	before, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil || !fileDeletionPrivateDirectory(before) {
		return nil, nil, fileDeletionError("staging name is not a private owned directory", err)
	}
	root, err := openRegisteredRoot(parent, name)
	if err != nil {
		return nil, nil, fileDeletionError("open private staging directory", err)
	}
	after, err := root.Stat(".")
	current, currentErr := parent.Lstat(name)
	if err != nil || currentErr != nil || !fileDeletionPrivateDirectory(after) || !fileDeletionPrivateDirectory(current) ||
		!os.SameFile(before, after) || !os.SameFile(after, current) {
		_ = root.Close()
		return nil, nil, fileDeletionError("staging directory changed while opening", ErrSourceChanged)
	}
	return root, after, nil
}

func openFileDeletionPayload(root *os.Root, name string, spec fileDeletionSpec, changeTime int64) (*os.File, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil || !spec.matches(before, changeTime) {
		return nil, nil, fileDeletionError("media entry does not match its indexed snapshot", ErrSourceChanged)
	}
	file, err := openScanFile(root, name)
	if err != nil {
		return nil, nil, fileDeletionError("open indexed media without following links", err)
	}
	opened, err := file.Stat()
	current, currentErr := root.Lstat(name)
	if err != nil || currentErr != nil || !spec.matches(opened, changeTime) || !spec.matches(current, changeTime) ||
		!sameMediaSourceFile(before, opened) || !sameMediaSourceFile(opened, current) {
		_ = file.Close()
		return nil, nil, fileDeletionError("media changed while opening", ErrSourceChanged)
	}
	return file, opened, nil
}

func fileDeletionSyncDirectory(root *os.Root) error {
	file, err := openScanFile(root, ".")
	if err != nil {
		return fileDeletionError("open directory for deletion durability", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fileDeletionError("synchronize deletion directory", err)
	}
	return nil
}

func (capture *fileDeletionCapture) check(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	if capture.closed {
		return ErrUnavailable
	}
	return ctx.Err()
}

func (capture *fileDeletionCapture) Close() error {
	if capture == nil {
		return nil
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.closed {
		return nil
	}
	capture.closed = true
	var err error
	if capture.source != nil {
		err = errors.Join(err, capture.source.Close())
	}
	if capture.stage != nil {
		err = errors.Join(err, capture.stage.Close())
	}
	if capture.parent != nil {
		err = errors.Join(err, capture.parent.Close())
	}
	if capture.root != nil {
		err = errors.Join(err, capture.root.Close())
	}
	if capture.lease != nil {
		err = errors.Join(err, capture.lease.Close())
	}
	return err
}

func fileDeletionError(operation string, err error) error {
	if err == nil {
		err = ErrSourceChanged
	}
	return fmt.Errorf("%w: %s: %w", ErrUnavailable, operation, err)
}
