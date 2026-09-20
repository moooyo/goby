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

	"github.com/moooyo/goby/internal/media"
)

// mediaEditFile identifies one retained filesystem object. A publication can
// change ctime, but never its identity, size, modification time or digest.
type mediaEditFile struct {
	Identity     string
	Size         int64
	ModifiedAt   time.Time
	ChangeTimeNs int64
	SHA256       string
}

func mediaEditFileInfo(info os.FileInfo, digest string) mediaEditFile {
	return mediaEditFile{Identity: fileIdentity(info), Size: info.Size(), ModifiedAt: catalogModifiedTime(info), ChangeTimeNs: media.FileChangeTime(info), SHA256: digest}
}

func (f mediaEditFile) valid() bool {
	decoded, err := hex.DecodeString(f.SHA256)
	return validFileDeletionIdentity(f.Identity) && f.Size > 0 && !f.ModifiedAt.IsZero() && f.ChangeTimeNs > 0 && err == nil && len(decoded) == sha256.Size && f.SHA256 == strings.ToLower(f.SHA256)
}

func (f mediaEditFile) matches(info os.FileInfo, renamed bool) bool {
	return info != nil && info.Mode().IsRegular() && fileIdentity(info) == f.Identity && info.Size() == f.Size && catalogModifiedTime(info).Equal(f.ModifiedAt) && media.FileChangeTime(info) > 0 && (renamed || media.FileChangeTime(info) == f.ChangeTimeNs)
}

func mediaEditDigest(ctx context.Context, file *os.File, size int64) (string, error) {
	if ctx == nil || file == nil || size <= 0 || size > 1<<40 {
		return "", ErrInvalidInput
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != size {
		return "", ErrSourceChanged
	}
	hash := sha256.New()
	reader := io.NewSectionReader(file, 0, size)
	buffer := make([]byte, 256<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			_, _ = hash.Write(buffer[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if total != size {
		return "", ErrSourceChanged
	}
	after, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) != media.FileChangeTime(after) {
		return "", ErrSourceChanged
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validMediaEditStageName(name string) bool {
	const prefix = ".goby-edit-"
	return strings.HasPrefix(name, prefix) && validFileDeletionStageName(".goby-delete-"+strings.TrimPrefix(name, prefix))
}

// mediaEditCapture reuses the descriptor and topology checks of deletion but
// not its journal or destructive purge operation. Its only publication syscall
// exchanges two existing names, retaining every byte of the previous source.
type mediaEditCapture struct {
	base          *fileDeletionCapture
	candidate     *os.File
	candidateInfo os.FileInfo
	attempted     bool
}

func (s *Store) openMediaEditCapture(ctx context.Context, spec fileDeletionSpec, stageName, stageIdentity string, candidate *mediaEditFile) (_ *mediaEditCapture, resultErr error) {
	if ctx == nil || s == nil || !fileDeletionSupported() || !validMediaEditStageName(stageName) {
		return nil, ErrInvalidInput
	}
	// Validate the shared source grammar without giving deletion access to the
	// edit directory. The real name is restored before any filesystem access.
	validation := spec
	validation.StageName, validation.StageIdentity, validation.StagedChangeTimeNs = ".goby-delete-"+strings.TrimPrefix(stageName, ".goby-edit-"), "", 0
	if !validFileDeletionSpec(validation) || (candidate != nil && (!candidate.valid() || !validFileDeletionIdentity(stageIdentity))) {
		return nil, ErrInvalidInput
	}
	spec.StageName, spec.StageIdentity, spec.StagedChangeTimeNs = stageName, "", 0
	base := &fileDeletionCapture{store: s, spec: spec}
	capture := &mediaEditCapture{base: base}
	defer func() {
		if resultErr != nil {
			_ = capture.Close()
		}
	}()
	var err error
	base.lease, err = s.leaseLibraryRoot(spec.Root)
	if err != nil {
		return nil, err
	}
	base.root, err = base.lease.Open()
	if err != nil {
		return nil, err
	}
	base.parent, err = openRegisteredRoot(base.root, filepath.Dir(filepath.FromSlash(spec.RelativePath)))
	if err != nil {
		return nil, err
	}
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	base.source, base.sourceInfo, err = openFileDeletionPayload(base.parent, filepath.Base(filepath.FromSlash(spec.RelativePath)), spec, spec.ChangeTimeNs)
	if err != nil || base.source == nil {
		return nil, errors.Join(ErrSourceChanged, err)
	}
	stage, info, err := openFileDeletionStage(base.parent, stageName)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		if stage != nil {
			_ = stage.Close()
			return nil, fmt.Errorf("%w: edit staging directory already exists", ErrBusy)
		}
		return capture, nil
	}
	if stage == nil || fileIdentity(info) != stageIdentity {
		if stage != nil {
			_ = stage.Close()
		}
		return nil, ErrSourceChanged
	}
	base.stage, base.stageExists, base.stageIdentity = stage, true, stageIdentity
	capture.candidate, capture.candidateInfo, err = openMediaEditFile(stage, "payload", *candidate, false)
	if err != nil {
		return nil, err
	}
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	return capture, nil
}

func openMediaEditFile(root *os.Root, name string, expected mediaEditFile, renamed bool) (*os.File, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if err != nil || !expected.matches(before, renamed) {
		return nil, nil, ErrSourceChanged
	}
	file, err := openScanFile(root, name)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	current, currentErr := root.Lstat(name)
	if err != nil || currentErr != nil || !expected.matches(after, renamed) || !expected.matches(current, renamed) || !sameMediaSourceFile(before, after) || !sameMediaSourceFile(current, after) {
		_ = file.Close()
		return nil, nil, ErrSourceChanged
	}
	return file, after, nil
}

func (capture *mediaEditCapture) createCandidate(ctx context.Context) (*os.File, error) {
	return capture.createCandidateWithIO(ctx, func(file *os.File) (os.FileInfo, error) { return file.Stat() }, fileDeletionSyncDirectory)
}

// Injected operations expose the post-create failure boundary to tests without
// weakening the independent descriptor checks used for compensating cleanup.
func (capture *mediaEditCapture) createCandidateWithIO(ctx context.Context, stat func(*os.File) (os.FileInfo, error), syncDirectory func(*os.Root) error) (_ *os.File, resultErr error) {
	if capture == nil {
		return nil, ErrInvalidInput
	}
	base := capture.base
	if base == nil || stat == nil || syncDirectory == nil || capture.candidate != nil || base.stage != nil {
		return nil, ErrBusy
	}
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	if err := base.verifyPayload(base.parent, filepath.Base(filepath.FromSlash(base.spec.RelativePath)), base.spec.ChangeTimeNs); err != nil {
		return nil, err
	}
	if err := base.parent.Mkdir(base.spec.StageName, 0o700); err != nil {
		return nil, err
	}
	stage, info, err := openFileDeletionStage(base.parent, base.spec.StageName)
	if err != nil || stage == nil {
		return nil, errors.Join(ErrSourceChanged, err)
	}
	base.stage, base.stageExists, base.stageIdentity = stage, true, fileIdentity(info)
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return nil, err
	}
	capture.candidate, err = stage.OpenFile("payload", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	// The owned descriptor exists before Stat or directory durability can fail.
	// Cleanup rechecks both the held object and the private named entry itself;
	// neither a failed injected Stat nor a pathname alone authorizes unlinking.
	defer func() {
		if resultErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := capture.discardUnpublished(cleanup); err != nil {
				resultErr = errors.Join(resultErr, ErrMediaOperationRecovery, err)
			}
		}
	}()
	capture.candidateInfo, err = stat(capture.candidate)
	if err != nil || capture.candidateInfo == nil || !capture.candidateInfo.Mode().IsRegular() || !mediaEditSameFilesystem(base.sourceInfo, capture.candidateInfo) {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if err := syncDirectory(stage); err != nil {
		return nil, err
	}
	if err := syncDirectory(base.parent); err != nil {
		return nil, err
	}
	return capture.candidate, nil
}

func (capture *mediaEditCapture) validateCandidate(ctx context.Context, expected mediaEditFile, renamed bool) error {
	if capture == nil || capture.base == nil || capture.candidate == nil || !expected.valid() {
		return ErrUnavailable
	}
	base := capture.base
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return err
	}
	root, name := base.stage, "payload"
	if renamed {
		root, name = base.parent, filepath.Base(filepath.FromSlash(base.spec.RelativePath))
	}
	named, err := root.Lstat(name)
	opened, openErr := capture.candidate.Stat()
	if err != nil || openErr != nil || !expected.matches(named, renamed) || !expected.matches(opened, renamed) || !sameMediaSourceFile(named, opened) {
		return ErrSourceChanged
	}
	digest, err := mediaEditDigest(ctx, capture.candidate, expected.Size)
	if err != nil {
		return err
	}
	if digest != expected.SHA256 {
		return ErrSourceChanged
	}
	return nil
}

// publish never compensates an uncertain exchange. The caller must persist a
// prepared publication intent first and retain it on every attempted failure.
// The old source remains at the private payload name after success; no cleanup
// routine is permitted to interpret that name as a disposable candidate.
func (capture *mediaEditCapture) publish(ctx context.Context, expected mediaEditFile) (os.FileInfo, error) {
	if capture == nil || capture.base == nil || capture.attempted {
		return nil, ErrUnavailable
	}
	base := capture.base
	if err := capture.validateCandidate(ctx, expected, false); err != nil {
		return nil, err
	}
	name := filepath.Base(filepath.FromSlash(base.spec.RelativePath))
	if err := base.verifyPayload(base.parent, name, base.spec.ChangeTimeNs); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	capture.attempted = true
	if err := mediaEditExchange(base.parent, name, base.stage, "payload"); err != nil {
		return nil, err
	}
	original, err := base.stage.Lstat("payload")
	held, heldErr := base.source.Stat()
	if err != nil || heldErr != nil || !base.spec.matches(original, 0) || !base.spec.matches(held, 0) || !sameMediaSourceFile(original, held) {
		return nil, ErrSourceChanged
	}
	if err := capture.validateCandidate(ctx, expected, true); err != nil {
		return nil, err
	}
	if err := fileDeletionSyncDirectory(base.stage); err != nil {
		return nil, err
	}
	if err := fileDeletionSyncDirectory(base.parent); err != nil {
		return nil, err
	}
	return capture.candidate.Stat()
}

func (capture *mediaEditCapture) Close() error {
	if capture == nil {
		return nil
	}
	var err error
	if capture.candidate != nil {
		err = capture.candidate.Close()
		capture.candidate = nil
	}
	if capture.base != nil {
		err = errors.Join(err, capture.base.Close())
	}
	return err
}

func (capture *mediaEditCapture) discardUnpublished(ctx context.Context) error {
	if capture == nil || capture.base == nil || capture.candidate == nil || capture.attempted {
		return ErrMediaOperationRecovery
	}
	base := capture.base
	if err := base.verifyNamedDirectories(ctx); err != nil {
		return err
	}
	named, err := base.stage.Lstat("payload")
	held, heldErr := capture.candidate.Stat()
	if err != nil || heldErr != nil || !named.Mode().IsRegular() || !held.Mode().IsRegular() || !sameMediaSourceFile(named, held) {
		return ErrSourceChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fileDeletionUnlink(base.stage, "payload"); err != nil {
		return err
	}
	return fileDeletionSyncDirectory(base.stage)
}

func (capture *mediaEditCapture) verifyOriginal(published bool) error {
	base := capture.base
	if !published {
		return base.verifyPayload(base.parent, filepath.Base(filepath.FromSlash(base.spec.RelativePath)), base.spec.ChangeTimeNs)
	}
	named, err := base.stage.Lstat("payload")
	held, heldErr := base.source.Stat()
	if err != nil || heldErr != nil || !base.spec.matches(named, 0) || !base.spec.matches(held, 0) || !sameMediaSourceFile(named, held) {
		return ErrSourceChanged
	}
	return nil
}
