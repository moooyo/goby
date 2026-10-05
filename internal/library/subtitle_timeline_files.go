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

const subtitleTimelineFileFormat = "goby-subtitle-timelines-v1"
const subtitleTimelineManifestBytes = 64 << 10

var (
	ErrSubtitleTimelineStorageConflict = errors.New("subtitle timeline storage conflict")
	ErrSubtitleTimelineStale           = errors.New("subtitle timeline belongs to an earlier source snapshot")
)

// Artifact metadata exposes no host path. An available stale artifact remains
// on disk, but OpenSubtitleTimelineFor never returns its previous source timeline.
type SubtitleTimelineArtifact struct {
	Available     bool                                 `json:"Available"`
	Stale         bool                                 `json:"Stale"`
	Profile       string                               `json:"Profile,omitempty"`
	Generation    string                               `json:"Generation,omitempty"`
	DurationTicks int64                                `json:"DurationTicks,omitempty"`
	Size          int64                                `json:"Size,omitempty"`
	Tracks        []media.SubtitleTimelineTrackSummary `json:"Tracks,omitempty"`
	Reused        bool                                 `json:"-"`
	OperationID   string                               `json:"-"`
	SourceStamp   string                               `json:"-"`
	ETag          string                               `json:"-"`
	ModifiedAt    time.Time                            `json:"-"`
}

type SubtitleTimelineEncoder func(context.Context, *os.File, MediaFile, SubtitleTimelineJob, io.Writer) (media.SubtitleTimelineSummary, error)

type SubtitleTimelineExternalEncoder func(context.Context, *os.File, MediaFile, SubtitleTimelineJob, []media.ExternalSubtitleTimelineInput, io.Writer) (media.SubtitleTimelineSummary, error)

type subtitleTimelineManifest struct {
	Format         string                        `json:"Format"`
	SourceName     string                        `json:"SourceName"`
	SourceStamp    string                        `json:"SourceStamp"`
	SourceRevision string                        `json:"SourceRevision"`
	OperationID    string                        `json:"OperationID"`
	Generation     string                        `json:"Generation"`
	SHA256         string                        `json:"SHA256"`
	Summary        media.SubtitleTimelineSummary `json:"Summary"`
	CreatedAt      time.Time                     `json:"CreatedAt"`
}

type subtitleTimelineOwner struct {
	Format     string `json:"Format"`
	SourceName string `json:"SourceName"`
}

func subtitleTimelineStorageError(err error) error {
	if errors.Is(err, ErrBackgroundClipConflict) {
		return fmt.Errorf("%w: %w", ErrSubtitleTimelineStorageConflict, err)
	}
	return err
}

func subtitleTimelineGeneration(name string) bool {
	return strings.HasPrefix(name, "gen-") && strings.HasSuffix(name, ".gstl") &&
		backgroundClipHex(strings.TrimSuffix(strings.TrimPrefix(name, "gen-"), ".gstl"), 32)
}

func sameSubtitleTimelineSummary(first, second media.SubtitleTimelineSummary) bool {
	return first.Profile == second.Profile && first.FFprobeSHA256 == second.FFprobeSHA256 &&
		first.DurationTicks == second.DurationTicks && first.Bytes == second.Bytes && slices.EqualFunc(first.Tracks, second.Tracks, func(a, b media.SubtitleTimelineTrackSummary) bool {
		return a.StreamIndex == b.StreamIndex && a.Codec == b.Codec && a.IntervalCount == b.IntervalCount && slices.Equal(a.Warnings, b.Warnings)
	})
}

func cloneSubtitleTimelineTracks(tracks []media.SubtitleTimelineTrackSummary) []media.SubtitleTimelineTrackSummary {
	result := slices.Clone(tracks)
	for index := range result {
		result[index].Warnings = slices.Clone(result[index].Warnings)
	}
	return result
}

func sameSubtitleTimelineManifest(first, second subtitleTimelineManifest) bool {
	return first.Format == second.Format && first.SourceName == second.SourceName && first.SourceStamp == second.SourceStamp &&
		first.SourceRevision == second.SourceRevision && first.OperationID == second.OperationID && first.Generation == second.Generation &&
		first.SHA256 == second.SHA256 && first.CreatedAt.Equal(second.CreatedAt) && sameSubtitleTimelineSummary(first.Summary, second.Summary)
}

func validSubtitleTimelineManifest(value subtitleTimelineManifest, sourceName string) bool {
	return value.Format == subtitleTimelineFileFormat && value.SourceName == sourceName && value.SourceRevision != "" &&
		validSubtitleTimelineSourceStamp(value.SourceStamp, value.Summary.Profile) &&
		backgroundClipHex(value.OperationID, 32) && subtitleTimelineGeneration(value.Generation) && backgroundClipHex(value.SHA256, 64) &&
		backgroundClipHex(value.Summary.FFprobeSHA256, 64) &&
		value.Summary.Bytes > 52 && value.Summary.Bytes <= media.MaxSubtitleTimelineBytes && value.Summary.DurationTicks > 0 &&
		value.Summary.DurationTicks <= media.MaxAnalysisDurationTicks && len(value.Summary.Tracks) > 0 &&
		len(value.Summary.Tracks) <= media.MaxSubtitleTimelineTracks && !value.CreatedAt.IsZero()
}

func validSubtitleTimelineSourceStamp(stamp, profile string) bool {
	prefix := "subtitle-timeline-source-v1-"
	if profile == media.SubtitleTimelineExternalProfile {
		prefix = "subtitle-timeline-source-v2-"
	} else if profile != media.SubtitleTimelineProfile {
		return false
	}
	return strings.HasPrefix(stamp, prefix) && backgroundClipHex(strings.TrimPrefix(stamp, prefix), 64)
}

// Source identity excludes catalog IDs, parent paths and root-binding revisions.
// A database rebuild or a same-filesystem directory move therefore retains a
// current subtitle timeline. Source mutation and changed subtitle or timeline
// facts do not. Container extradata is not indexed separately, so changes to it
// are bound by the source identity, size, mtime and ctime observations.
func subtitleTimelineSourceStamp(snapshot indexedMediaSource) (string, error) {
	if err := validateSubtitleTimelineBitmapBinding(snapshot); err != nil {
		return "", err
	}
	info := snapshot.mediaFile.Item.Media
	if info == nil || snapshot.identity == "" || snapshot.mediaFile.Size <= 0 || snapshot.mediaFile.ModifiedAt.IsZero() || info.FileChangeTimeNs <= 0 {
		return "", ErrUnavailable
	}
	type streamFacts struct {
		Index, Width, Height, Level                                                     int
		Codec, Profile, TimeBase, CodecTag, CodecTagString                              string
		Language, Title                                                                 string
		IsDefault, IsForced, IsHearingImpaired, IsAttachedPicture, IsTextSubtitleStream bool
	}
	var streams []streamFacts
	for _, stream := range info.Streams {
		if stream.CodecType == "subtitle" && !stream.IsExternal {
			streams = append(streams, streamFacts{Index: stream.Index, Width: stream.Width, Height: stream.Height, Level: stream.Level,
				Codec: stream.Codec, Profile: stream.Profile, TimeBase: stream.TimeBase, CodecTag: stream.CodecTag, CodecTagString: stream.CodecTagString,
				Language: stream.Language, Title: stream.Title, IsDefault: stream.IsDefault, IsForced: stream.IsForced,
				IsHearingImpaired: stream.IsHearingImpaired, IsAttachedPicture: stream.IsAttachedPicture, IsTextSubtitleStream: stream.IsTextSubtitleStream})
		}
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Index < streams[j].Index })
	if len(streams) > 4096 {
		return "", media.ErrSubtitleTimelineUnsupported
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
	legacy := "subtitle-timeline-source-v1-" + hex.EncodeToString(digest[:])
	if len(subtitleTimelineSnapshotBitmap(snapshot)) == 0 {
		return legacy, nil
	}
	bitmap, err := subtitleTimelineBitmapFacts(subtitleTimelineSnapshotBitmap(snapshot))
	if err != nil {
		return "", err
	}
	encoded, err = json.Marshal(struct {
		Source string
		Bitmap []BitmapSubtitle
	}{legacy, bitmap})
	if err != nil {
		return "", err
	}
	digest = sha256.Sum256(encoded)
	return "subtitle-timeline-source-v2-" + hex.EncodeToString(digest[:]), nil
}

func openSubtitleTimelineDirectory(parent *os.Root, sourceName string, create bool) (*os.Root, error) {
	if sourceName == "" || sourceName == "." || sourceName == ".." || filepath.Base(sourceName) != sourceName || !utf8.ValidString(sourceName) {
		return nil, ErrInvalidInput
	}
	current, err := parent.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, component := range []string{"backdrops", "goby-subtitle-timelines", backgroundClipDirectoryName(sourceName)} {
		if create {
			mode := os.FileMode(0700)
			if component == "backdrops" {
				mode = 0755
			}
			mkdirErr := current.Mkdir(component, mode)
			if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				current.Close()
				return nil, fmt.Errorf("%w: subtitle timeline destination is not writable: %w", ErrUnavailable, mkdirErr)
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
			return nil, ErrSubtitleTimelineStorageConflict
		}
		current = next
	}
	return current, nil
}

func readSubtitleTimelineJSON(directory *os.Root, name string, target any) (os.FileInfo, error) {
	before, err := directory.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > subtitleTimelineManifestBytes {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	file, err := openScanFile(directory, name)
	if err != nil {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	data, err := io.ReadAll(io.LimitReader(file, subtitleTimelineManifestBytes+1))
	if err != nil || int64(len(data)) != opened.Size() {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	if _, err := metadataJSONObject(data); err != nil {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	after, err := directory.Lstat(name)
	if err != nil || !sameMediaSourceFile(opened, after) {
		return nil, ErrSubtitleTimelineStorageConflict
	}
	return after, nil
}

func writeSubtitleTimelineJSON(directory *os.Root, name string, value any) (os.FileInfo, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(encoded) >= subtitleTimelineManifestBytes {
		return nil, ErrSubtitleTimelineStorageConflict
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

func claimSubtitleTimelineDirectory(directory *os.Root, sourceName string) error {
	var owner subtitleTimelineOwner
	_, err := readSubtitleTimelineJSON(directory, ".owner.json", &owner)
	if err == nil {
		if owner.Format != subtitleTimelineFileFormat || owner.SourceName != sourceName {
			return ErrSubtitleTimelineStorageConflict
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
		return ErrSubtitleTimelineStorageConflict
	}
	if _, err := writeSubtitleTimelineJSON(directory, ".owner.json", subtitleTimelineOwner{subtitleTimelineFileFormat, sourceName}); err != nil {
		return err
	}
	return syncBackgroundClipDirectory(directory)
}

func checkUnpublishedSubtitleTimelines(directory *os.Root, force bool) error {
	file, err := directory.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	names, err := file.Readdirnames(1025)
	if err != nil && err != io.EOF || len(names) > 1024 {
		return ErrSubtitleTimelineStorageConflict
	}
	for _, name := range names {
		if name == ".owner.json" || name == ".generation.lock" || subtitleTimelineGeneration(name) && force {
			continue
		}
		if strings.HasPrefix(name, ".subtitle-timeline-") && strings.HasSuffix(name, ".part") || strings.HasPrefix(name, ".manifest-") && strings.HasSuffix(name, ".part") {
			continue
		}
		return ErrSubtitleTimelineStorageConflict
	}
	return nil
}

func readSubtitleTimelinePayload(ctx context.Context, directory *os.Root, name string, size int64, digest string) (*os.File, media.SubtitleTimelineSummary, error) {
	if size <= 52 || size > media.MaxSubtitleTimelineBytes || !backgroundClipHex(digest, 64) {
		return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
	}
	before, err := directory.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != size {
		return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
	}
	file, err := openScanFile(directory, name)
	if err != nil {
		return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) {
		return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
	}
	data := make([]byte, int(size))
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, media.SubtitleTimelineSummary{}, err
		}
		end := min(offset+64*1024, len(data))
		n, err := file.ReadAt(data[offset:end], int64(offset))
		if err != nil || n != end-offset {
			return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
		}
		offset = end
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
	}
	decoded, err := media.ParseSubtitleTimelines(data)
	if err != nil {
		return nil, media.SubtitleTimelineSummary{}, fmt.Errorf("%w: %w", ErrSubtitleTimelineStorageConflict, err)
	}
	after, err := directory.Lstat(name)
	current, statErr := file.Stat()
	if err != nil || statErr != nil || !sameMediaSourceFile(opened, after) || !sameMediaSourceFile(opened, current) {
		return nil, media.SubtitleTimelineSummary{}, ErrSubtitleTimelineStorageConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, media.SubtitleTimelineSummary{}, err
	}
	ok = true
	return file, decoded.Summary(size), nil
}

func readSubtitleTimeline(ctx context.Context, directory *os.Root, sourceName, sourceStamp string, sourceCurrent bool) (*os.File, subtitleTimelineManifest, SubtitleTimelineArtifact, error) {
	var manifest subtitleTimelineManifest
	var owner subtitleTimelineOwner
	if _, err := readSubtitleTimelineJSON(directory, ".owner.json", &owner); err != nil {
		return nil, manifest, SubtitleTimelineArtifact{}, err
	}
	if owner.Format != subtitleTimelineFileFormat || owner.SourceName != sourceName {
		return nil, manifest, SubtitleTimelineArtifact{}, ErrSubtitleTimelineStorageConflict
	}
	if _, err := readSubtitleTimelineJSON(directory, "manifest.json", &manifest); err != nil {
		return nil, manifest, SubtitleTimelineArtifact{}, err
	}
	if !validSubtitleTimelineManifest(manifest, sourceName) {
		return nil, manifest, SubtitleTimelineArtifact{}, ErrSubtitleTimelineStorageConflict
	}
	file, summary, err := readSubtitleTimelinePayload(ctx, directory, manifest.Generation, manifest.Summary.Bytes, manifest.SHA256)
	if err != nil {
		return nil, manifest, SubtitleTimelineArtifact{}, err
	}
	if !sameSubtitleTimelineSummary(summary, manifest.Summary) {
		file.Close()
		return nil, manifest, SubtitleTimelineArtifact{}, ErrSubtitleTimelineStorageConflict
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, manifest, SubtitleTimelineArtifact{}, err
	}
	artifact := SubtitleTimelineArtifact{Available: true, Stale: !sourceCurrent || manifest.SourceStamp != sourceStamp,
		Profile: summary.Profile, Generation: manifest.Generation, DurationTicks: summary.DurationTicks, Size: summary.Bytes,
		Tracks: cloneSubtitleTimelineTracks(summary.Tracks), OperationID: manifest.OperationID, SourceStamp: manifest.SourceStamp,
		ETag: `"subtitle-timeline-` + manifest.Generation + "-" + manifest.SHA256 + `"`, ModifiedAt: info.ModTime().UTC()}
	return file, manifest, artifact, nil
}

func (s *Store) recheckSubtitleTimelineDirectory(snapshot indexedMediaSource, heldParent, heldDirectory *os.Root) error {
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
	directory, err := openSubtitleTimelineDirectory(parent, filepath.Base(snapshot.relativePath), false)
	if err != nil {
		return err
	}
	defer directory.Close()
	if !sameMediaSourceDirectory(directory, heldDirectory) {
		return ErrSourceChanged
	}
	return nil
}

func subtitleTimelineSourceCurrent(parent *os.Root, snapshot indexedMediaSource) bool {
	info, err := parent.Lstat(filepath.Base(snapshot.relativePath))
	return err == nil && snapshot.matches(info)
}

func (s *Store) OpenSubtitleTimelineFor(ctx context.Context, subject Subject, itemID string) (*os.File, SubtitleTimelineArtifact, error) {
	return s.openSubtitleTimelineFor(ctx, subject, itemID, false)
}

func (s *Store) GetSubtitleTimelineFor(ctx context.Context, subject Subject, itemID string) (SubtitleTimelineArtifact, error) {
	file, artifact, err := s.openSubtitleTimelineFor(ctx, subject, itemID, true)
	if err != nil {
		return SubtitleTimelineArtifact{}, err
	}
	return artifact, file.Close()
}

func (s *Store) openSubtitleTimelineFor(ctx context.Context, subject Subject, itemID string, allowStale bool) (*os.File, SubtitleTimelineArtifact, error) {
	if ctx == nil || s == nil || !analysisOpaque(itemID, 128) {
		return nil, SubtitleTimelineArtifact{}, ErrInvalidInput
	}
	var artifact SubtitleTimelineArtifact
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
		directory, err := openSubtitleTimelineDirectory(parent, filepath.Base(snapshot.relativePath), false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		stamp, err := subtitleTimelineSourceStamp(snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		currentSource, err := s.subtitleTimelineSourceCurrent(work, parent, snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		opened, _, found, err := readSubtitleTimeline(work, directory, filepath.Base(snapshot.relativePath), stamp, currentSource)
		if err != nil {
			return nil, MediaFile{}, err
		}
		current, err := s.readMediaSourceFor(work, subject, itemID, "")
		if err == nil && (current.root != snapshot.root || current.relativePath != snapshot.relativePath) {
			err = ErrSourceChanged
		}
		if err == nil {
			err = s.recheckSubtitleTimelineDirectory(current, parent, directory)
		}
		if err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		currentStamp, err := subtitleTimelineSourceStamp(current)
		if err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		currentSource, err = s.subtitleTimelineSourceCurrent(work, parent, current)
		if err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		found.Stale = found.Stale || found.SourceStamp != currentStamp || !currentSource
		artifact = found
		if found.Stale && !allowStale {
			opened.Close()
			return nil, MediaFile{}, ErrSubtitleTimelineStale
		}
		return opened, snapshot.mediaFile, nil
	}, func(work context.Context) error {
		_, err := s.readMediaSourceFor(work, subject, itemID, "")
		return err
	})
	if err != nil {
		if errors.Is(err, ErrSubtitleTimelineStale) {
			return nil, artifact, err
		}
		return nil, SubtitleTimelineArtifact{}, subtitleTimelineStorageError(err)
	}
	return file, artifact, nil
}
