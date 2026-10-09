package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
)

const audioWaveformFileFormat = "goby-audio-waveforms-v1"
const audioWaveformManifestBytes = 64 << 10

var (
	ErrAudioWaveformStorageConflict = errors.New("audio waveform storage conflict")
	ErrAudioWaveformStale           = errors.New("audio waveform belongs to an earlier source snapshot")
)

// Artifact metadata exposes no host path. An available stale artifact remains
// on disk, but OpenAudioWaveformFor never returns its previous source timeline.
type AudioWaveformArtifact struct {
	Available     bool                              `json:"Available"`
	Stale         bool                              `json:"Stale"`
	Profile       string                            `json:"Profile,omitempty"`
	Generation    string                            `json:"Generation,omitempty"`
	DurationTicks int64                             `json:"DurationTicks,omitempty"`
	Size          int64                             `json:"Size,omitempty"`
	Tracks        []media.AudioWaveformTrackSummary `json:"Tracks,omitempty"`
	Reused        bool                              `json:"-"`
	OperationID   string                            `json:"-"`
	SourceStamp   string                            `json:"-"`
	ETag          string                            `json:"-"`
	ModifiedAt    time.Time                         `json:"-"`
}

type audioWaveformReadResult struct {
	artifact AudioWaveformArtifact
	data     media.AudioWaveformData
}

type AudioWaveformEncoder func(context.Context, *os.File, MediaFile, AudioWaveformJob, io.Writer) (media.AudioWaveformSummary, error)

type audioWaveformManifest struct {
	Format         string                     `json:"Format"`
	SourceName     string                     `json:"SourceName"`
	SourceStamp    string                     `json:"SourceStamp"`
	SourceRevision string                     `json:"SourceRevision"`
	OperationID    string                     `json:"OperationID"`
	Generation     string                     `json:"Generation"`
	SHA256         string                     `json:"SHA256"`
	Summary        media.AudioWaveformSummary `json:"Summary"`
	CreatedAt      time.Time                  `json:"CreatedAt"`
}

type audioWaveformOwner struct {
	Format     string `json:"Format"`
	SourceName string `json:"SourceName"`
}

func audioWaveformStorageError(err error) error {
	if errors.Is(err, ErrBackgroundClipConflict) {
		return fmt.Errorf("%w: %w", ErrAudioWaveformStorageConflict, err)
	}
	return err
}

func audioWaveformGeneration(name string) bool {
	return strings.HasPrefix(name, "gen-") && strings.HasSuffix(name, ".gawf") &&
		backgroundClipHex(strings.TrimSuffix(strings.TrimPrefix(name, "gen-"), ".gawf"), 32)
}

func sameAudioWaveformSummary(first, second media.AudioWaveformSummary) bool {
	return first.Profile == second.Profile && first.FFmpegSHA256 == second.FFmpegSHA256 &&
		first.DurationTicks == second.DurationTicks && first.Bytes == second.Bytes && slices.Equal(first.Tracks, second.Tracks)
}

func sameAudioWaveformManifest(first, second audioWaveformManifest) bool {
	return first.Format == second.Format && first.SourceName == second.SourceName && first.SourceStamp == second.SourceStamp &&
		first.SourceRevision == second.SourceRevision && first.OperationID == second.OperationID && first.Generation == second.Generation &&
		first.SHA256 == second.SHA256 && first.CreatedAt.Equal(second.CreatedAt) && sameAudioWaveformSummary(first.Summary, second.Summary)
}

func validAudioWaveformManifest(value audioWaveformManifest, sourceName string) bool {
	return value.Format == audioWaveformFileFormat && value.SourceName == sourceName && value.SourceRevision != "" &&
		strings.HasPrefix(value.SourceStamp, "waveform-source-v1-") && backgroundClipHex(strings.TrimPrefix(value.SourceStamp, "waveform-source-v1-"), 64) &&
		backgroundClipHex(value.OperationID, 32) && audioWaveformGeneration(value.Generation) && backgroundClipHex(value.SHA256, 64) &&
		value.Summary.Profile == media.AudioWaveformProfile && backgroundClipHex(value.Summary.FFmpegSHA256, 64) &&
		value.Summary.Bytes > 52 && value.Summary.Bytes <= media.MaxAudioWaveformBytes && value.Summary.DurationTicks > 0 &&
		value.Summary.DurationTicks <= media.MaxAnalysisDurationTicks && len(value.Summary.Tracks) > 0 &&
		len(value.Summary.Tracks) <= media.MaxAudioWaveformTracks && !value.CreatedAt.IsZero()
}

// Source identity excludes catalog IDs, parent paths and root-binding revisions.
// A database rebuild or a same-filesystem directory move therefore retains a
// current waveform. Source mutation and changed audio/timeline facts do not.
func audioWaveformSourceStamp(snapshot indexedMediaSource) (string, error) {
	info := snapshot.mediaFile.Item.Media
	if info == nil || snapshot.identity == "" || snapshot.mediaFile.Size <= 0 || snapshot.mediaFile.ModifiedAt.IsZero() || info.FileChangeTimeNs <= 0 {
		return "", ErrUnavailable
	}
	type streamFacts struct {
		Index                   int
		Codec                   string
		Channels, SampleRate    int
		ChannelLayout, TimeBase string
		AudioTiming             *media.AudioTiming
	}
	var streams []streamFacts
	for _, stream := range info.Streams {
		if stream.CodecType == "audio" && !stream.IsExternal && !stream.IsAttachedPicture {
			streams = append(streams, streamFacts{stream.Index, stream.Codec, stream.Channels, stream.SampleRate, stream.ChannelLayout, stream.TimeBase, stream.AudioTiming})
		}
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Index < streams[j].Index })
	if len(streams) > 4096 {
		return "", media.ErrAudioWaveformUnsupported
	}
	facts := struct {
		Identity                                                                              string
		Size, ModifiedNS, ChangedNS, DurationTicks, FormatStartTicks, PresentationOriginTicks int64
		FormatStartKnown, AudioDurationExact                                                  bool
		Streams                                                                               []streamFacts
	}{snapshot.identity, snapshot.mediaFile.Size, snapshot.mediaFile.ModifiedAt.UnixNano(), info.FileChangeTimeNs,
		info.DurationTicks, info.FormatStartTicks, info.PresentationOriginTicks, info.FormatStartKnown, info.AudioDurationExact, streams}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "waveform-source-v1-" + hex.EncodeToString(digest[:]), nil
}

func openAudioWaveformDirectory(parent *os.Root, sourceName string, create bool) (*os.Root, error) {
	if sourceName == "" || sourceName == "." || sourceName == ".." || filepath.Base(sourceName) != sourceName || !utf8.ValidString(sourceName) {
		return nil, ErrInvalidInput
	}
	current, err := parent.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, component := range []string{"backdrops", "goby-waveforms", backgroundClipDirectoryName(sourceName)} {
		if create {
			mode := os.FileMode(0700)
			if component == "backdrops" {
				mode = 0755
			}
			mkdirErr := current.Mkdir(component, mode)
			if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				current.Close()
				return nil, fmt.Errorf("%w: waveform destination is not writable: %w", ErrUnavailable, mkdirErr)
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
			if !create {
				return nil, ErrNotFound
			}
			return nil, ErrAudioWaveformStorageConflict
		}
		current = next
	}
	return current, nil
}

func readAudioWaveformJSON(directory *os.Root, name string, target any) (os.FileInfo, error) {
	before, err := directory.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > audioWaveformManifestBytes {
		return nil, ErrAudioWaveformStorageConflict
	}
	file, err := openScanFile(directory, name)
	if err != nil {
		return nil, ErrAudioWaveformStorageConflict
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) {
		return nil, ErrAudioWaveformStorageConflict
	}
	data, err := io.ReadAll(io.LimitReader(file, audioWaveformManifestBytes+1))
	if err != nil || int64(len(data)) != opened.Size() {
		return nil, ErrAudioWaveformStorageConflict
	}
	if _, err := metadataJSONObject(data); err != nil {
		return nil, ErrAudioWaveformStorageConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrAudioWaveformStorageConflict
	}
	after, err := directory.Lstat(name)
	if err != nil || !sameMediaSourceFile(opened, after) {
		return nil, ErrAudioWaveformStorageConflict
	}
	return after, nil
}

func writeAudioWaveformJSON(directory *os.Root, name string, value any) (os.FileInfo, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(encoded) >= audioWaveformManifestBytes {
		return nil, ErrAudioWaveformStorageConflict
	}
	file, err := directory.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	created, statErr := file.Stat()
	err = errors.Join(writeErr, statErr, file.Sync(), file.Close())
	if err != nil && created != nil {
		_ = removeOwnedBackgroundClipFile(directory, name, created)
	}
	return created, err
}

func claimAudioWaveformDirectory(directory *os.Root, sourceName string) error {
	var owner audioWaveformOwner
	_, err := readAudioWaveformJSON(directory, ".owner.json", &owner)
	if err == nil {
		if owner.Format != audioWaveformFileFormat || owner.SourceName != sourceName {
			return ErrAudioWaveformStorageConflict
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
	if err := errors.Join(file.Close()); err != nil || len(names) != 0 || readErr != io.EOF {
		return ErrAudioWaveformStorageConflict
	}
	if _, err := writeAudioWaveformJSON(directory, ".owner.json", audioWaveformOwner{audioWaveformFileFormat, sourceName}); err != nil {
		return err
	}
	return syncBackgroundClipDirectory(directory)
}

func checkUnpublishedAudioWaveforms(directory *os.Root, force bool) error {
	file, err := directory.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	names, err := file.Readdirnames(1025)
	if err != nil && err != io.EOF || len(names) > 1024 {
		return ErrAudioWaveformStorageConflict
	}
	for _, name := range names {
		if name == ".owner.json" || name == ".generation.lock" || audioWaveformGeneration(name) && force {
			continue
		}
		if strings.HasPrefix(name, ".waveform-") && strings.HasSuffix(name, ".part") || strings.HasPrefix(name, ".manifest-") && strings.HasSuffix(name, ".part") {
			continue
		}
		return ErrAudioWaveformStorageConflict
	}
	return nil
}

func readAudioWaveformPayload(ctx context.Context, directory *os.Root, name string, size int64, digest string) (*os.File, media.AudioWaveformSummary, error) {
	file, data, err := readAudioWaveformPayloadData(ctx, directory, name, size, digest)
	if err != nil {
		return nil, media.AudioWaveformSummary{}, err
	}
	return file, data.Summary(size), nil
}

func readAudioWaveformPayloadData(ctx context.Context, directory *os.Root, name string, size int64, digest string) (*os.File, media.AudioWaveformData, error) {
	if size <= 52 || size > media.MaxAudioWaveformBytes || !backgroundClipHex(digest, 64) {
		return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
	}
	before, err := directory.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != size {
		return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
	}
	file, err := openScanFile(directory, name)
	if err != nil {
		return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) {
		return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
	}
	data := make([]byte, int(size))
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, media.AudioWaveformData{}, err
		}
		end := min(offset+64*1024, len(data))
		n, err := file.ReadAt(data[offset:end], int64(offset))
		if err != nil || n != end-offset {
			return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
		}
		offset = end
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
	}
	decoded, err := media.ParseAudioWaveforms(data)
	if err != nil {
		return nil, media.AudioWaveformData{}, fmt.Errorf("%w: %w", ErrAudioWaveformStorageConflict, err)
	}
	after, err := directory.Lstat(name)
	current, statErr := file.Stat()
	if err != nil || statErr != nil || !sameMediaSourceFile(opened, after) || !sameMediaSourceFile(opened, current) {
		return nil, media.AudioWaveformData{}, ErrAudioWaveformStorageConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, media.AudioWaveformData{}, err
	}
	ok = true
	return file, decoded, nil
}

func readAudioWaveform(ctx context.Context, directory *os.Root, sourceName, sourceStamp string, sourceCurrent bool) (*os.File, audioWaveformManifest, AudioWaveformArtifact, error) {
	file, manifest, result, err := readAudioWaveformData(ctx, directory, sourceName, sourceStamp, sourceCurrent)
	return file, manifest, result.artifact, err
}

func readAudioWaveformData(ctx context.Context, directory *os.Root, sourceName, sourceStamp string, sourceCurrent bool) (*os.File, audioWaveformManifest, audioWaveformReadResult, error) {
	var manifest audioWaveformManifest
	if _, err := readAudioWaveformJSON(directory, "manifest.json", &manifest); err != nil {
		return nil, manifest, audioWaveformReadResult{}, err
	}
	if !validAudioWaveformManifest(manifest, sourceName) {
		return nil, manifest, audioWaveformReadResult{}, ErrAudioWaveformStorageConflict
	}
	file, data, err := readAudioWaveformPayloadData(ctx, directory, manifest.Generation, manifest.Summary.Bytes, manifest.SHA256)
	if err != nil {
		return nil, manifest, audioWaveformReadResult{}, err
	}
	summary := data.Summary(manifest.Summary.Bytes)
	if !sameAudioWaveformSummary(summary, manifest.Summary) {
		file.Close()
		return nil, manifest, audioWaveformReadResult{}, ErrAudioWaveformStorageConflict
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, manifest, audioWaveformReadResult{}, err
	}
	artifact := AudioWaveformArtifact{Available: true, Stale: !sourceCurrent || manifest.SourceStamp != sourceStamp,
		Profile: summary.Profile, Generation: manifest.Generation, DurationTicks: summary.DurationTicks, Size: summary.Bytes,
		Tracks: slices.Clone(summary.Tracks), OperationID: manifest.OperationID, SourceStamp: manifest.SourceStamp,
		ETag: `"waveform-` + manifest.Generation + "-" + manifest.SHA256 + `"`, ModifiedAt: info.ModTime().UTC()}
	return file, manifest, audioWaveformReadResult{artifact: artifact, data: data}, nil
}

func (s *Store) recheckAudioWaveformDirectory(snapshot indexedMediaSource, heldParent, heldDirectory *os.Root) error {
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
	directory, err := openAudioWaveformDirectory(parent, filepath.Base(snapshot.relativePath), false)
	if err != nil {
		return err
	}
	defer directory.Close()
	if !sameMediaSourceDirectory(directory, heldDirectory) {
		return ErrSourceChanged
	}
	return nil
}

func audioWaveformSourceCurrent(parent *os.Root, snapshot indexedMediaSource) bool {
	info, err := parent.Lstat(filepath.Base(snapshot.relativePath))
	return err == nil && snapshot.matches(info)
}

func (s *Store) OpenAudioWaveformFor(ctx context.Context, subject Subject, itemID string) (*os.File, AudioWaveformArtifact, error) {
	file, result, err := s.openAudioWaveformFor(ctx, subject, itemID, false, true)
	return file, result.artifact, err
}

func (s *Store) GetAudioWaveformFor(ctx context.Context, subject Subject, itemID string) (AudioWaveformArtifact, error) {
	_, result, err := s.openAudioWaveformFor(ctx, subject, itemID, true, false)
	if err != nil {
		return AudioWaveformArtifact{}, err
	}
	return result.artifact, nil
}

// ReadAudioWaveformFor returns owned decoded arrays only after current access
// and source checks. Its worker closes all descriptors before retiring, so a
// level response can reuse the validated data without reopening or rereading it.
func (s *Store) ReadAudioWaveformFor(ctx context.Context, subject Subject, itemID string) (media.AudioWaveformData, AudioWaveformArtifact, error) {
	_, result, err := s.openAudioWaveformFor(ctx, subject, itemID, false, false)
	if err != nil {
		return media.AudioWaveformData{}, AudioWaveformArtifact{}, err
	}
	return result.data, result.artifact, nil
}

func (s *Store) openAudioWaveformFor(ctx context.Context, subject Subject, itemID string, allowStale, keepFile bool) (*os.File, audioWaveformReadResult, error) {
	if ctx == nil || s == nil || !analysisOpaque(itemID, 128) {
		return nil, audioWaveformReadResult{}, ErrInvalidInput
	}
	// The mailbox owns only immutable data, never a descriptor. Read it only
	// after the media worker's result handoff; cancellation may return while
	// that worker is still finishing its checks and descriptor cleanup.
	completed := make(chan audioWaveformReadResult, 1)
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
		directory, err := openAudioWaveformDirectory(parent, filepath.Base(snapshot.relativePath), false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		stamp, err := audioWaveformSourceStamp(snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		opened, _, result, err := readAudioWaveformData(work, directory, filepath.Base(snapshot.relativePath), stamp, audioWaveformSourceCurrent(parent, snapshot))
		if err != nil {
			return nil, MediaFile{}, err
		}
		current, err := s.readMediaSourceFor(work, subject, itemID, "")
		if err == nil && (current.root != snapshot.root || current.relativePath != snapshot.relativePath) {
			err = ErrSourceChanged
		}
		if err == nil {
			err = s.recheckAudioWaveformDirectory(current, parent, directory)
		}
		if err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		currentStamp, err := audioWaveformSourceStamp(current)
		if err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		result.artifact.Stale = result.artifact.Stale || result.artifact.SourceStamp != currentStamp || !audioWaveformSourceCurrent(parent, current)
		if result.artifact.Stale && !allowStale {
			opened.Close()
			completed <- audioWaveformReadResult{artifact: result.artifact}
			return nil, MediaFile{}, ErrAudioWaveformStale
		}
		if !keepFile {
			if err := opened.Close(); err != nil {
				return nil, MediaFile{}, err
			}
			opened = nil
		}
		completed <- result
		return opened, snapshot.mediaFile, nil
	}, func(work context.Context) error {
		_, err := s.readMediaSourceFor(work, subject, itemID, "")
		return err
	})
	if err != nil && !errors.Is(err, ErrAudioWaveformStale) {
		return nil, audioWaveformReadResult{}, audioWaveformStorageError(err)
	}
	return file, <-completed, err
}
