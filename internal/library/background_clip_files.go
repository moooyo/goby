package library

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
)

const backgroundClipFormat = "goby-background-clip-v1"
const backgroundClipMaximumBytes int64 = 64 << 20

// ErrBackgroundClipConflict preserves unowned or incomplete on-disk material.
// No automatic task may repair it by deleting or replacing files.
var ErrBackgroundClipConflict = errors.New("background clip storage conflict")

// BackgroundClipArtifact contains no host pathname or catalog-dependent key.
// ETag and ModifiedAt are HTTP validators for the selected immutable generation.
type BackgroundClipArtifact struct {
	Available          bool      `json:"Available"`
	StartPositionTicks int64     `json:"StartPositionTicks,omitempty"`
	RunTimeTicks       int64     `json:"RunTimeTicks,omitempty"`
	Width              int       `json:"Width,omitempty"`
	Height             int       `json:"Height,omitempty"`
	Size               int64     `json:"Size,omitempty"`
	SourceChanged      bool      `json:"SourceChanged,omitempty"`
	Reused             bool      `json:"-"`
	OperationID        string    `json:"-"`
	ETag               string    `json:"-"`
	ModifiedAt         time.Time `json:"-"`
}

// BackgroundClipEncoder writes a fully verified, finite MP4 or returns an error.
// Its source and output are borrowed only for this synchronous invocation.
type BackgroundClipEncoder func(context.Context, *os.File, MediaFile, BackgroundPreviewJob, io.Writer) (media.BackgroundClipSummary, error)

type backgroundClipManifest struct {
	Format         string    `json:"Format"`
	SourceName     string    `json:"SourceName"`
	SourceRevision string    `json:"SourceRevision"`
	SourceSnapshot string    `json:"SourceSnapshot"`
	OperationID    string    `json:"OperationID,omitempty"`
	Generation     string    `json:"Generation"`
	Profile        string    `json:"Profile"`
	StartTicks     int64     `json:"StartTicks"`
	DurationTicks  int64     `json:"DurationTicks"`
	Width          int       `json:"Width"`
	Height         int       `json:"Height"`
	Size           int64     `json:"Size"`
	SHA256         string    `json:"SHA256"`
	FFmpegSHA256   string    `json:"FFmpegSHA256"`
	FFprobeSHA256  string    `json:"FFprobeSHA256"`
	CreatedAt      time.Time `json:"CreatedAt"`
}

type backgroundClipOwner struct {
	Format     string `json:"Format"`
	SourceName string `json:"SourceName"`
}

// The exact original filename is the only identity input. Moving its containing
// folder or rebuilding the database preserves the sidecar association, while
// episodes sharing one season directory retain independent generations.
func backgroundClipDirectoryName(sourceName string) string {
	digest := sha256.Sum256([]byte(sourceName))
	return hex.EncodeToString(digest[:])
}

func backgroundClipHex(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func backgroundClipGeneration(name string) bool {
	return strings.HasPrefix(name, "gen-") && strings.HasSuffix(name, ".mp4") && backgroundClipHex(strings.TrimSuffix(strings.TrimPrefix(name, "gen-"), ".mp4"), 32)
}

func validBackgroundClipManifest(value backgroundClipManifest, sourceName string) bool {
	return value.Format == backgroundClipFormat && value.SourceName == sourceName &&
		backgroundClipGeneration(value.Generation) && value.SourceRevision != "" && value.SourceSnapshot != "" &&
		len(value.OperationID) <= 128 && !strings.ContainsAny(value.OperationID, "\x00\r\n") &&
		value.Profile != "" && len(value.Profile) <= 1024 && value.StartTicks >= 0 &&
		value.DurationTicks > 0 && value.DurationTicks <= 61*media.TicksPerSecond &&
		value.Width >= 2 && value.Width <= 1920 && value.Height >= 2 && value.Height <= 1080 &&
		value.Width%2 == 0 && value.Height%2 == 0 && value.Size > 32 && value.Size <= backgroundClipMaximumBytes &&
		backgroundClipHex(value.SHA256, 64) && backgroundClipHex(value.FFmpegSHA256, 64) &&
		backgroundClipHex(value.FFprobeSHA256, 64) && !value.CreatedAt.IsZero()
}

func backgroundClipArtifact(value backgroundClipManifest, info os.FileInfo, sourceSnapshot string) BackgroundClipArtifact {
	return BackgroundClipArtifact{Available: true, StartPositionTicks: value.StartTicks,
		RunTimeTicks: value.DurationTicks, Width: value.Width, Height: value.Height, Size: value.Size,
		SourceChanged: value.SourceSnapshot != sourceSnapshot, OperationID: value.OperationID,
		ETag: `"background-` + value.Generation + "-" + value.SHA256 + `"`, ModifiedAt: info.ModTime().UTC()}
}

func openBackgroundClipDirectory(parent *os.Root, sourceName string, create bool) (*os.Root, error) {
	if sourceName == "" || sourceName == "." || sourceName == ".." || filepath.Base(sourceName) != sourceName || !utf8.ValidString(sourceName) {
		return nil, ErrInvalidInput
	}
	current, err := parent.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, component := range []string{"backdrops", "goby", backgroundClipDirectoryName(sourceName)} {
		if create {
			mode := os.FileMode(0700)
			if component == "backdrops" {
				mode = 0755
			}
			mkdirErr := current.Mkdir(component, mode)
			if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				current.Close()
				return nil, fmt.Errorf("%w: background clip directory is not writable: %w", ErrUnavailable, mkdirErr)
			}
			if mkdirErr == nil {
				if err := syncBackgroundClipDirectory(current); err != nil {
					current.Close()
					return nil, err
				}
			}
		}
		next, err := openRegisteredRoot(current, component)
		current.Close()
		if err != nil {
			// Unsafe or unavailable sidecar paths never become playback sources.
			if !create {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("%w: background clip directory is unsafe", ErrBackgroundClipConflict)
		}
		current = next
	}
	return current, nil
}

func readBackgroundClipJSON(root *os.Root, name string, target any) (os.FileInfo, error) {
	before, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 16<<10 {
		return nil, ErrBackgroundClipConflict
	}
	file, err := openScanFile(root, name)
	if err != nil {
		return nil, ErrBackgroundClipConflict
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) {
		return nil, ErrBackgroundClipConflict
	}
	data, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil || int64(len(data)) != opened.Size() {
		return nil, ErrBackgroundClipConflict
	}
	if _, err := metadataJSONObject(data); err != nil {
		return nil, ErrBackgroundClipConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrBackgroundClipConflict
	}
	after, err := root.Lstat(name)
	if err != nil || !sameMediaSourceFile(opened, after) {
		return nil, ErrBackgroundClipConflict
	}
	return after, nil
}

func readBackgroundClip(root *os.Root, sourceName, sourceSnapshot string) (*os.File, backgroundClipManifest, BackgroundClipArtifact, error) {
	var value backgroundClipManifest
	if _, err := readBackgroundClipJSON(root, "manifest.json", &value); err != nil {
		return nil, value, BackgroundClipArtifact{}, err
	}
	if !validBackgroundClipManifest(value, sourceName) {
		return nil, value, BackgroundClipArtifact{}, ErrBackgroundClipConflict
	}
	before, err := root.Lstat(value.Generation)
	if err != nil || !before.Mode().IsRegular() || before.Size() != value.Size {
		return nil, value, BackgroundClipArtifact{}, ErrBackgroundClipConflict
	}
	file, err := openScanFile(root, value.Generation)
	if err != nil {
		return nil, value, BackgroundClipArtifact{}, ErrBackgroundClipConflict
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) {
		return nil, value, BackgroundClipArtifact{}, ErrBackgroundClipConflict
	}
	var header [12]byte
	if _, err := file.ReadAt(header[:], 0); err != nil || string(header[4:8]) != "ftyp" {
		return nil, value, BackgroundClipArtifact{}, ErrBackgroundClipConflict
	}
	after, err := root.Lstat(value.Generation)
	if err != nil || !sameMediaSourceFile(opened, after) {
		return nil, value, BackgroundClipArtifact{}, ErrBackgroundClipConflict
	}
	ok = true
	return file, value, backgroundClipArtifact(value, opened, sourceSnapshot), nil
}

// OpenBackgroundPreviewFor opens only a manifest-owned sidecar of an authorized
// Movie or Episode. It never schedules work and deliberately does not compare
// the original source's current filesystem bytes with an old manifest.
func (s *Store) OpenBackgroundPreviewFor(ctx context.Context, subject Subject, itemID string) (*os.File, BackgroundClipArtifact, error) {
	if ctx == nil || s == nil || !analysisOpaque(itemID, 128) {
		return nil, BackgroundClipArtifact{}, ErrInvalidInput
	}
	var artifact BackgroundClipArtifact
	file, _, err := s.runPreparedMediaSourceWorker(ctx, false, func(work context.Context) (mediaSourceRootHint, error) {
		return s.readMediaSourceRootHint(work, itemID)
	}, func(work context.Context) (*os.File, MediaFile, error) {
		snapshot, err := s.readMediaSourceFor(work, subject, itemID, "")
		if err != nil {
			return nil, MediaFile{}, err
		}
		if snapshot.mediaFile.Item.Type != "Movie" && snapshot.mediaFile.Item.Type != "Episode" {
			return nil, MediaFile{}, ErrNotFound
		}
		if err := s.checkMediaSourceRootAdmission(work, snapshot); err != nil {
			return nil, MediaFile{}, err
		}
		root, err := s.openLibraryRoot(snapshot.root)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer root.Close()
		parent, err := openRegisteredRoot(root, filepath.Dir(snapshot.relativePath))
		if err != nil {
			return nil, MediaFile{}, ErrUnavailable
		}
		defer parent.Close()
		directory, err := openBackgroundClipDirectory(parent, filepath.Base(snapshot.relativePath), false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		opened, _, found, err := readBackgroundClip(directory, filepath.Base(snapshot.relativePath), snapshot.mediaFile.ETag)
		if err != nil {
			return nil, MediaFile{}, err
		}
		current, err := s.readMediaSourceFor(work, subject, itemID, "")
		if err != nil || current.root != snapshot.root || current.relativePath != snapshot.relativePath {
			opened.Close()
			if err == nil {
				err = ErrSourceChanged
			}
			return nil, MediaFile{}, err
		}
		if err := s.recheckBackgroundClipDirectory(current, parent, directory); err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		artifact = found
		return opened, snapshot.mediaFile, nil
	}, func(work context.Context) error {
		_, err := s.readMediaSourceFor(work, subject, itemID, "")
		return err
	})
	if err != nil {
		return nil, BackgroundClipArtifact{}, err
	}
	return file, artifact, nil
}

func (s *Store) GetBackgroundPreviewFor(ctx context.Context, subject Subject, itemID string) (BackgroundClipArtifact, error) {
	file, artifact, err := s.OpenBackgroundPreviewFor(ctx, subject, itemID)
	if err != nil {
		return BackgroundClipArtifact{}, err
	}
	return artifact, file.Close()
}

func (s *Store) recheckBackgroundClipDirectory(snapshot indexedMediaSource, heldParent, heldDirectory *os.Root) error {
	root, err := s.openLibraryRoot(snapshot.root)
	if err != nil {
		return err
	}
	defer root.Close()
	parent, err := openRegisteredRoot(root, filepath.Dir(snapshot.relativePath))
	if err != nil {
		return ErrSourceChanged
	}
	defer parent.Close()
	if !sameMediaSourceDirectory(parent, heldParent) {
		return ErrSourceChanged
	}
	directory, err := openBackgroundClipDirectory(parent, filepath.Base(snapshot.relativePath), false)
	if err != nil {
		return err
	}
	defer directory.Close()
	if !sameMediaSourceDirectory(directory, heldDirectory) {
		return ErrSourceChanged
	}
	return nil
}

func claimBackgroundClipDirectory(directory *os.Root, sourceName string) error {
	var owner backgroundClipOwner
	_, err := readBackgroundClipJSON(directory, ".owner.json", &owner)
	if err == nil {
		if owner.Format != backgroundClipFormat || owner.SourceName != sourceName {
			return ErrBackgroundClipConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	file, err := directory.Open(".")
	if err != nil {
		return err
	}
	names, readErr := file.Readdirnames(1)
	closeErr := file.Close()
	if len(names) != 0 || readErr != io.EOF || closeErr != nil {
		return ErrBackgroundClipConflict
	}
	owner = backgroundClipOwner{Format: backgroundClipFormat, SourceName: sourceName}
	if err := writeBackgroundClipJSON(directory, ".owner.json", owner); err != nil {
		return err
	}
	return syncBackgroundClipDirectory(directory)
}

func checkUnpublishedBackgroundClips(directory *os.Root, force bool) error {
	file, err := directory.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	// This source owns only one current generation and a bounded set of
	// interrupted writes. Excessive debris is preserved for explicit repair.
	names, err := file.Readdirnames(1025)
	if err != nil && err != io.EOF || len(names) > 1024 {
		return ErrBackgroundClipConflict
	}
	for _, name := range names {
		if name == ".owner.json" || name == ".generation.lock" {
			continue
		}
		if backgroundClipGeneration(name) && force {
			continue
		}
		// A previous invocation owns these temporary names. They are never
		// cleaned by this invocation and do not authorize overwriting anything.
		if strings.HasPrefix(name, ".clip-") && strings.HasSuffix(name, ".part") ||
			strings.HasPrefix(name, ".manifest-") && strings.HasSuffix(name, ".part") {
			continue
		}
		return ErrBackgroundClipConflict
	}
	return nil
}

func writeBackgroundClipJSON(directory *os.Root, name string, value any) error {
	_, err := writeBackgroundClipJSONOwned(directory, name, value)
	return err
}

func writeBackgroundClipJSONOwned(directory *os.Root, name string, value any) (os.FileInfo, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(data) > 16<<10 {
		return nil, ErrBackgroundClipConflict
	}
	file, err := directory.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(append(data, '\n'))
	created, statErr := file.Stat()
	err = errors.Join(statErr, writeErr, file.Sync(), file.Close())
	if err != nil && created != nil {
		_ = removeOwnedBackgroundClipFile(directory, name, created)
	}
	return created, err
}

func removeOwnedBackgroundClipFile(directory *os.Root, name string, expected os.FileInfo) error {
	if expected == nil || !expected.Mode().IsRegular() {
		return ErrBackgroundClipConflict
	}
	current, err := directory.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// A rename can update ctime, so retain identity, length and modification
	// time rather than requiring the old name's change timestamp.
	if !current.Mode().IsRegular() || !os.SameFile(expected, current) || expected.Size() != current.Size() || !expected.ModTime().Equal(current.ModTime()) {
		return ErrBackgroundClipConflict
	}
	return directory.Remove(name)
}

type backgroundClipOutput struct {
	ctx    context.Context
	file   *os.File
	digest hash.Hash
	bytes  int64
}

func (out *backgroundClipOutput) Write(data []byte) (int, error) {
	if err := out.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(data)) > backgroundClipMaximumBytes-out.bytes {
		return 0, media.ErrAnalysisBudget
	}
	n, err := out.file.Write(data)
	_, _ = out.digest.Write(data[:n])
	out.bytes += int64(n)
	return n, err
}

// GenerateBackgroundClip publishes a descriptor-derived sidecar. A valid
// existing generation is returned before opening or decoding the original
// source; only a claimed explicit Force job can replace that generation.
func (s *Store) GenerateBackgroundClip(ctx context.Context, job BackgroundPreviewJob, fence AnalysisFence, encode BackgroundClipEncoder) (BackgroundClipArtifact, error) {
	if ctx == nil || fence == nil || encode == nil {
		return BackgroundClipArtifact{}, ErrInvalidInput
	}
	expected, err := s.ValidateBackgroundPreviewJob(ctx, fence, job)
	if err != nil {
		return BackgroundClipArtifact{}, err
	}
	{
		artifact, err := s.findBackgroundClipForJob(ctx, expected)
		if err == nil && (!job.Force || job.OperationID != "" && artifact.OperationID == job.OperationID) {
			if _, err := s.ValidateBackgroundPreviewJob(ctx, fence, job); err != nil {
				return BackgroundClipArtifact{}, err
			}
			artifact.Reused = true
			return artifact, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return BackgroundClipArtifact{}, err
		}
	}
	// Supporting intro sources use their own governed workers. Resolve them
	// before retaining this source's worker admission to avoid nested lanes.
	job, err = s.ResolveBackgroundPreviewJobInterval(ctx, fence, job)
	if err != nil {
		return BackgroundClipArtifact{}, err
	}
	if err := ValidateBackgroundPreviewProfile(job.Profile); err != nil {
		return BackgroundClipArtifact{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(job.Profile.MaxItemRuntimeSeconds)*time.Second)
	defer cancel()
	var result BackgroundClipArtifact
	committed := false
	_, _, err = s.runPreparedMediaSourceWorker(ctx, true, func(work context.Context) (mediaSourceRootHint, error) {
		return s.readMediaSourceRootHint(work, job.ItemID)
	}, func(work context.Context) (*os.File, MediaFile, error) {
		snapshot, err := s.readAdmittedAnalysisSource(work, expected)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := s.checkMediaSourceRootAdmission(work, snapshot); err != nil {
			return nil, MediaFile{}, err
		}
		root, err := s.openLibraryRoot(snapshot.root)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer root.Close()
		parent, err := openRegisteredRoot(root, filepath.Dir(snapshot.relativePath))
		if err != nil {
			return nil, MediaFile{}, ErrUnavailable
		}
		defer parent.Close()
		sourceName := filepath.Base(snapshot.relativePath)
		directory, err := openBackgroundClipDirectory(parent, sourceName, true)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		if err := claimBackgroundClipDirectory(directory, sourceName); err != nil {
			return nil, MediaFile{}, err
		}
		unlock, err := lockBackgroundClipDirectory(directory)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer unlock()
		previous, oldManifest, artifact, readErr := readBackgroundClip(directory, sourceName, snapshot.mediaFile.ETag)
		var oldFileInfo os.FileInfo
		if readErr == nil {
			oldFileInfo, err = previous.Stat()
			previous.Close()
			if err != nil {
				return nil, MediaFile{}, err
			}
			if !job.Force || job.OperationID != "" && artifact.OperationID == job.OperationID {
				artifact.Reused = true
				result = artifact
				return nil, snapshot.mediaFile, nil
			}
		} else if !errors.Is(readErr, ErrNotFound) {
			return nil, MediaFile{}, readErr
		} else if err := checkUnpublishedBackgroundClips(directory, job.Force); err != nil {
			return nil, MediaFile{}, err
		}
		if _, err := s.ValidateBackgroundPreviewJob(work, fence, job); err != nil {
			return nil, MediaFile{}, err
		}
		artifact, err = s.generateBackgroundClipFile(work, snapshot, expected, job, fence, parent, directory, oldManifest, oldFileInfo, encode)
		if err != nil {
			return nil, MediaFile{}, err
		}
		result = artifact
		committed = true
		return nil, snapshot.mediaFile, nil
	})
	return backgroundClipWorkerResult(result, committed, err)
}

// The common source worker adds a final cancellation check after its callback.
// Once a durable, verified publication has committed, later cancellation during
// best-effort retirement or this worker epilogue cannot change that result.
// A subsequent queue acknowledgement may still be cancelled; OperationID makes
// recovery reuse the committed generation rather than encode it again.
func backgroundClipWorkerResult(artifact BackgroundClipArtifact, committed bool, err error) (BackgroundClipArtifact, error) {
	if committed && artifact.Available && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return artifact, nil
	}
	if err != nil {
		return BackgroundClipArtifact{}, err
	}
	return artifact, nil
}

func (s *Store) findBackgroundClipForJob(ctx context.Context, expected AnalysisSource) (BackgroundClipArtifact, error) {
	var artifact BackgroundClipArtifact
	file, _, err := s.runPreparedMediaSourceWorker(ctx, true, func(work context.Context) (mediaSourceRootHint, error) {
		return s.readMediaSourceRootHint(work, expected.ItemID)
	}, func(work context.Context) (*os.File, MediaFile, error) {
		snapshot, err := s.readAdmittedAnalysisSource(work, expected)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := s.checkMediaSourceRootAdmission(work, snapshot); err != nil {
			return nil, MediaFile{}, err
		}
		root, err := s.openLibraryRoot(snapshot.root)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer root.Close()
		parent, err := openRegisteredRoot(root, filepath.Dir(snapshot.relativePath))
		if err != nil {
			return nil, MediaFile{}, ErrUnavailable
		}
		defer parent.Close()
		directory, err := openBackgroundClipDirectory(parent, filepath.Base(snapshot.relativePath), false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		opened, _, found, err := readBackgroundClip(directory, filepath.Base(snapshot.relativePath), snapshot.mediaFile.ETag)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := s.recheckBackgroundClipDirectory(snapshot, parent, directory); err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		artifact = found
		return opened, snapshot.mediaFile, nil
	})
	if err != nil {
		return BackgroundClipArtifact{}, err
	}
	return artifact, file.Close()
}

func (s *Store) generateBackgroundClipFile(ctx context.Context, snapshot indexedMediaSource, expected AnalysisSource, job BackgroundPreviewJob,
	fence AnalysisFence, parent, directory *os.Root, old backgroundClipManifest, oldFileInfo os.FileInfo, encode BackgroundClipEncoder) (artifact BackgroundClipArtifact, resultErr error) {
	input, err := s.openPublicMediaSource(ctx, snapshot)
	if err != nil {
		return artifact, err
	}
	read, err := s.PrepareMediaSourceIO(ctx, snapshot.mediaFile)
	if err != nil {
		input.Close()
		return artifact, err
	}
	inputRetired := false
	retireInput := func() error {
		if inputRetired {
			return nil
		}
		inputRetired = true
		closeErr := input.Close()
		if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			closeErr = media.SourceReadRetirementError(closeErr, input)
			closeErr = errors.Join(closeErr, read.MarkUnknown(closeErr))
		} else if errors.Is(closeErr, os.ErrClosed) {
			closeErr = nil
		}
		return errors.Join(closeErr, read.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, retireInput()) }()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return artifact, err
	}
	id := hex.EncodeToString(nonce[:])
	temporary, generation, manifestTemporary := ".clip-"+id+".part", "gen-"+id+".mp4", ".manifest-"+id+".part"
	output, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return artifact, fmt.Errorf("%w: background clip destination is not writable: %w", ErrUnavailable, err)
	}
	defer output.Close()
	var outputIdentity, manifestTemporaryIdentity os.FileInfo
	defer func() {
		if current, err := output.Stat(); err == nil {
			outputIdentity = current
		}
		_ = removeOwnedBackgroundClipFile(directory, temporary, outputIdentity)
	}()
	manifestTemporaryOwned := false
	defer func() {
		if manifestTemporaryOwned {
			_ = removeOwnedBackgroundClipFile(directory, manifestTemporary, manifestTemporaryIdentity)
		}
	}()
	writer := &backgroundClipOutput{ctx: ctx, file: output, digest: sha256.New()}
	var summary media.BackgroundClipSummary
	selected := job
	for _, candidate := range backgroundClipCandidates(job, snapshot.mediaFile.Item) {
		selected.StartTicks = candidate
		if err := output.Truncate(0); err != nil {
			return artifact, err
		}
		if _, err := output.Seek(0, io.SeekStart); err != nil {
			return artifact, err
		}
		writer.bytes = 0
		writer.digest.Reset()
		summary, err = encode(read.Context(ctx), input, snapshot.mediaFile, selected, writer)
		if err == nil || !errors.Is(err, media.ErrBackgroundClipUnusable) {
			break
		}
	}
	if err != nil {
		return artifact, err
	}
	outputIdentity, err = output.Stat()
	if err := errors.Join(err, output.Sync(), output.Close()); err != nil {
		return artifact, err
	}
	value := backgroundClipManifest{Format: backgroundClipFormat, SourceName: filepath.Base(snapshot.relativePath),
		SourceRevision: expected.SourceRevision, SourceSnapshot: snapshot.mediaFile.ETag, OperationID: job.OperationID, Generation: generation,
		Profile: summary.Profile, StartTicks: summary.StartTicks, DurationTicks: summary.DurationTicks, Width: summary.Width, Height: summary.Height,
		Size: writer.bytes, SHA256: hex.EncodeToString(writer.digest.Sum(nil)), FFmpegSHA256: summary.FFmpegSHA256, FFprobeSHA256: summary.FFprobeSHA256, CreatedAt: time.Now().UTC()}
	if !validBackgroundClipManifest(value, value.SourceName) || value.StartTicks != selected.StartTicks || value.DurationTicks > job.DurationTicks+media.TicksPerSecond || summary.Bytes != writer.bytes || value.Width > job.Profile.MaxWidth || value.Height > job.Profile.MaxWidth*9/16 {
		return artifact, ErrBackgroundClipConflict
	}
	manifestTemporaryIdentity, err = writeBackgroundClipJSONOwned(directory, manifestTemporary, value)
	if err != nil {
		return artifact, err
	}
	manifestTemporaryOwned = true
	newManifestIdentity := manifestTemporaryIdentity
	if _, err := s.ValidateBackgroundPreviewJob(ctx, fence, job); err != nil {
		return artifact, err
	}
	current, err := s.readAdmittedAnalysisSource(ctx, expected)
	if err != nil {
		return artifact, err
	}
	if err := s.recheckBackgroundClipDirectory(current, parent, directory); err != nil {
		return artifact, err
	}
	before, err := parent.Lstat(value.SourceName)
	opened, statErr := input.Stat()
	if err != nil || statErr != nil || !snapshot.matches(before) || !snapshot.matches(opened) || !sameMediaSourceFile(before, opened) {
		return artifact, ErrSourceChanged
	}
	// Retirement can fail and therefore must complete before any manifest is
	// changed. A late deferred source-close error cannot undo a committed file.
	if err := retireInput(); err != nil {
		return artifact, err
	}
	// Final MP4 names are immutable. A crash before the manifest rename leaves
	// an unreferenced generation, never a broken previous publication. Automatic
	// cleanup does not enumerate or remove those persistent files.
	if err := backgroundClipRenameNoReplace(directory, temporary, generation); err != nil {
		return artifact, err
	}
	published := false
	defer func() {
		if !published {
			// This invocation alone owns an unpublished new generation.
			_ = removeOwnedBackgroundClipFile(directory, generation, outputIdentity)
		}
	}()
	if err := syncBackgroundClipDirectory(directory); err != nil {
		return artifact, err
	}
	var previousManifestInfo os.FileInfo
	if old.Generation != "" {
		var existing backgroundClipManifest
		previousManifestInfo, err = readBackgroundClipJSON(directory, "manifest.json", &existing)
		if err != nil || existing != old {
			return artifact, ErrBackgroundClipConflict
		}
	}
	manifestReplaced := false
	err = s.WithBackgroundPreviewPublication(ctx, fence, job, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if old.Generation == "" {
			err := backgroundClipRenameNoReplace(directory, manifestTemporary, "manifest.json")
			manifestReplaced = err == nil
			return err
		}
		err := mediaEditExchange(directory, manifestTemporary, directory, "manifest.json")
		manifestReplaced = err == nil
		if manifestReplaced {
			// Until verified after the atomic exchange, the displaced file may
			// be an external replacement and must never be unlinked as ours.
			manifestTemporaryOwned = false
		}
		return err
	})
	if err == nil && old.Generation != "" {
		var displaced backgroundClipManifest
		displacedInfo, readErr := readBackgroundClipJSON(directory, manifestTemporary, &displaced)
		if readErr != nil || displaced != old || !os.SameFile(previousManifestInfo, displacedInfo) {
			err = ErrBackgroundClipConflict
		} else {
			manifestTemporaryOwned = true
			manifestTemporaryIdentity = displacedInfo
		}
	}
	publication := backgroundClipPublication{directory: directory, id: id, current: value, previous: old,
		newManifestIdentity: newManifestIdentity, replaced: manifestReplaced,
		temporaryOwned: manifestTemporaryOwned, temporaryIdentity: manifestTemporaryIdentity}
	if old.Generation != "" {
		publication.displaced = manifestTemporary
	}
	artifact, err = publication.finish(err, func() error { return syncBackgroundClipDirectory(directory) }, func() (BackgroundClipArtifact, error) {
		file, manifest, found, readErr := readBackgroundClip(directory, value.SourceName, snapshot.mediaFile.ETag)
		if readErr != nil {
			return BackgroundClipArtifact{}, readErr
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if manifest != value || statErr != nil || !os.SameFile(outputIdentity, info) {
			return BackgroundClipArtifact{}, errors.Join(ErrBackgroundClipConflict, statErr, closeErr)
		}
		return found, errors.Join(closeErr, ctx.Err())
	})
	published = publication.retainGeneration
	manifestTemporaryOwned, manifestTemporaryIdentity = publication.temporaryOwned, publication.temporaryIdentity
	if err != nil {
		return BackgroundClipArtifact{}, err
	}
	if job.Force && old.Generation != "" && old.Generation != generation {
		// Only an explicitly superseded, validated generation is retired.
		// Current handles remain readable after this unlink on Linux.
		if oldFile, err := directory.Lstat(old.Generation); err == nil && oldFile.Mode().IsRegular() && sameMediaSourceFile(oldFileInfo, oldFile) {
			_ = removeOwnedBackgroundClipFile(directory, old.Generation, oldFileInfo)
			_ = syncBackgroundClipDirectory(directory)
		}
	}
	return artifact, nil
}

// A manifest replacement remains provisional through durable directory sync
// and the complete read-back check, including closing its validation handle.
// After finish succeeds, only best-effort retirement of an explicitly replaced
// generation remains; those maintenance failures do not turn success into failure.
type backgroundClipPublication struct {
	directory           *os.Root
	id                  string
	current, previous   backgroundClipManifest
	newManifestIdentity os.FileInfo
	displaced           string
	replaced            bool
	retainGeneration    bool
	temporaryOwned      bool
	temporaryIdentity   os.FileInfo
}

func (publication *backgroundClipPublication) finish(publishErr error, syncDirectory func() error, readBack func() (BackgroundClipArtifact, error)) (BackgroundClipArtifact, error) {
	var artifact BackgroundClipArtifact
	err := publishErr
	if err == nil && !publication.replaced {
		err = ErrBackgroundClipConflict
	}
	if err == nil {
		err = syncDirectory()
	}
	if err == nil {
		artifact, err = readBack()
		if err == nil && !artifact.Available {
			err = ErrBackgroundClipConflict
		}
	}
	if err == nil {
		publication.retainGeneration = true
		return artifact, nil
	}
	if publication.replaced {
		rollbackErr := rollbackBackgroundClipManifest(publication.directory, publication.id, publication.current, publication.previous,
			publication.displaced, publication.newManifestIdentity)
		if rollbackErr != nil {
			// Failed restoration is not permission to erase either the new
			// generation or the retained previous manifest. An external writer
			// may also have taken ownership of the current directory entry.
			publication.retainGeneration = true
			publication.temporaryOwned = false
		} else {
			publication.temporaryOwned = true
			publication.temporaryIdentity = publication.newManifestIdentity
		}
		err = errors.Join(err, rollbackErr)
	}
	return BackgroundClipArtifact{}, err
}

// Automatic editorial candidates stay near the original deterministic choice.
// An explicit manual start is never silently moved. Every candidate must fit
// the same full interval before known credits or the source end.
func backgroundClipCandidates(job BackgroundPreviewJob, item Item) []int64 {
	result := []int64{job.StartTicks}
	if job.ManualStartTicks != nil || item.Media == nil || job.DurationTicks <= 0 {
		return result
	}
	end := item.Media.DurationTicks
	if validCreditsPoint(item.Credits, end) && item.Credits.StartTicks > 0 {
		end = item.Credits.StartTicks
	}
	step := max(job.DurationTicks, 25*media.TicksPerSecond)
	for index := int64(1); index <= 2; index++ {
		start := job.StartTicks + index*step
		if start < job.StartTicks || start > end || job.DurationTicks > end-start {
			break
		}
		result = append(result, start)
	}
	return result
}

func rollbackBackgroundClipManifest(directory *os.Root, id string, current, previous backgroundClipManifest, displaced string, expectedCurrent ...os.FileInfo) error {
	var observed backgroundClipManifest
	observedInfo, err := readBackgroundClipJSON(directory, "manifest.json", &observed)
	if err != nil || observed != current {
		return ErrBackgroundClipConflict
	}
	if len(expectedCurrent) > 1 || len(expectedCurrent) == 1 && (expectedCurrent[0] == nil || !os.SameFile(expectedCurrent[0], observedInfo)) {
		return ErrBackgroundClipConflict
	}
	if displaced != "" {
		// Exchange restores the exact displaced directory entry, even when an
		// external writer replaced it immediately before our publication.
		if err := mediaEditExchange(directory, displaced, directory, "manifest.json"); err != nil {
			return err
		}
	} else if previous.Generation == "" {
		if err := removeOwnedBackgroundClipFile(directory, "manifest.json", observedInfo); err != nil {
			return err
		}
	} else {
		name := ".manifest-" + id + "-rollback.part"
		identity, err := writeBackgroundClipJSONOwned(directory, name, previous)
		if err != nil {
			return err
		}
		defer removeOwnedBackgroundClipFile(directory, name, identity)
		if err := directory.Rename(name, "manifest.json"); err != nil {
			return err
		}
	}
	return syncBackgroundClipDirectory(directory)
}
