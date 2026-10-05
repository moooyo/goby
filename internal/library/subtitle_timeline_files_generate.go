package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type subtitleTimelineOutput struct {
	ctx    context.Context
	file   *os.File
	digest hash.Hash
	bytes  int64
}

func (output *subtitleTimelineOutput) Write(data []byte) (int, error) {
	if err := output.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(data)) > media.MaxSubtitleTimelineBytes-output.bytes {
		return 0, media.ErrAnalysisBudget
	}
	n, err := output.file.Write(data)
	_, _ = output.digest.Write(data[:n])
	output.bytes += int64(n)
	return n, err
}

func subtitleTimelineCanReuse(job SubtitleTimelineJob, artifact SubtitleTimelineArtifact) bool {
	return artifact.Available && (!job.Force || job.OperationID != "" && artifact.OperationID == job.OperationID)
}

// GenerateSubtitleTimeline retains existing material, including stale material,
// until a distinct explicit Force operation successfully replaces it. A
// completed Force operation can be replayed after a lost queue acknowledgement.
func (s *Store) GenerateSubtitleTimeline(ctx context.Context, job SubtitleTimelineJob, fence AnalysisFence, encode SubtitleTimelineEncoder) (SubtitleTimelineArtifact, error) {
	return s.GenerateSubtitleTimelineWithExternal(ctx, job, fence, adaptSubtitleTimelineEncoder(encode))
}

// GenerateSubtitleTimelineWithExternal includes the exact indexed sidecars in
// the same immutable generation and publication fence as embedded subtitles.
func (s *Store) GenerateSubtitleTimelineWithExternal(ctx context.Context, job SubtitleTimelineJob, fence AnalysisFence, encode SubtitleTimelineExternalEncoder) (SubtitleTimelineArtifact, error) {
	if ctx == nil || fence == nil || encode == nil {
		return SubtitleTimelineArtifact{}, ErrInvalidInput
	}
	expected, err := s.ValidateSubtitleTimelineJob(ctx, fence, job)
	if err != nil {
		return SubtitleTimelineArtifact{}, err
	}
	if artifact, err := s.findSubtitleTimelineForJob(ctx, expected); err == nil {
		if subtitleTimelineCanReuse(job, artifact) {
			if _, err := s.ValidateSubtitleTimelineJob(ctx, fence, job); err != nil {
				return SubtitleTimelineArtifact{}, err
			}
			artifact.Reused = true
			return artifact, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return SubtitleTimelineArtifact{}, subtitleTimelineStorageError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(SubtitleTimelineItemTimeoutSeconds)*time.Second)
	defer cancel()
	var result SubtitleTimelineArtifact
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
		directory, err := openSubtitleTimelineDirectory(parent, sourceName, true)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		if err := claimSubtitleTimelineDirectory(directory, sourceName); err != nil {
			return nil, MediaFile{}, err
		}
		unlock, err := lockBackgroundClipDirectory(directory)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer unlock()
		stamp, err := subtitleTimelineSourceStamp(snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		current, err := s.subtitleTimelineSourceCurrent(work, parent, snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		previous, old, artifact, readErr := readSubtitleTimeline(work, directory, sourceName, stamp, current)
		var oldFileInfo os.FileInfo
		if readErr == nil {
			oldFileInfo, err = previous.Stat()
			err = errors.Join(err, previous.Close())
			if err != nil {
				return nil, MediaFile{}, err
			}
			if subtitleTimelineCanReuse(job, artifact) {
				artifact.Reused = true
				result = artifact
				return nil, snapshot.mediaFile, nil
			}
		} else if !errors.Is(readErr, ErrNotFound) {
			return nil, MediaFile{}, readErr
		} else if err := checkUnpublishedSubtitleTimelines(directory, job.Force); err != nil {
			return nil, MediaFile{}, err
		}
		if _, err := s.ValidateSubtitleTimelineJob(work, fence, job); err != nil {
			return nil, MediaFile{}, err
		}
		artifact, err = s.generateSubtitleTimelineFile(work, snapshot, expected, job, fence, parent, directory, old, oldFileInfo, encode)
		if err != nil {
			return nil, MediaFile{}, err
		}
		result, committed = artifact, true
		return nil, snapshot.mediaFile, nil
	})
	return subtitleTimelineWorkerResult(result, committed, err)
}

func subtitleTimelineWorkerResult(artifact SubtitleTimelineArtifact, committed bool, err error) (SubtitleTimelineArtifact, error) {
	if committed && artifact.Available && (err == context.Canceled || err == context.DeadlineExceeded) {
		return artifact, nil
	}
	if err != nil {
		return SubtitleTimelineArtifact{}, subtitleTimelineStorageError(err)
	}
	return artifact, nil
}

func (s *Store) findSubtitleTimelineForJob(ctx context.Context, expected AnalysisSource) (SubtitleTimelineArtifact, error) {
	var artifact SubtitleTimelineArtifact
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
		directory, err := openSubtitleTimelineDirectory(parent, filepath.Base(snapshot.relativePath), false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		stamp, err := subtitleTimelineSourceStamp(snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		current, err := s.subtitleTimelineSourceCurrent(work, parent, snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		opened, _, found, err := readSubtitleTimeline(work, directory, filepath.Base(snapshot.relativePath), stamp, current)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := s.recheckSubtitleTimelineDirectory(snapshot, parent, directory); err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		artifact = found
		return opened, snapshot.mediaFile, nil
	})
	if err != nil {
		return SubtitleTimelineArtifact{}, err
	}
	return artifact, file.Close()
}

func subtitleTimelineSummaryMatchesSource(summary media.SubtitleTimelineSummary, info *media.Info, external ...[]BitmapSubtitle) bool {
	if info == nil || summary.DurationTicks != info.DurationTicks {
		return false
	}
	var streams []media.Stream
	for _, stream := range info.Streams {
		if stream.CodecType == "subtitle" && !stream.IsExternal && !stream.IsAttachedPicture &&
			(stream.Codec == "hdmv_pgs_subtitle" || stream.Codec == "dvd_subtitle") {
			streams = append(streams, stream)
		}
	}
	profile := media.SubtitleTimelineProfile
	if len(external) != 0 && len(external[0]) != 0 {
		profile = media.SubtitleTimelineExternalProfile
		facts, err := subtitleTimelineBitmapFacts(external[0])
		if err != nil {
			return false
		}
		for _, track := range facts {
			if track.Index <= highestEmbeddedStreamIndex(info) {
				return false
			}
			streams = append(streams, media.Stream{Index: track.Index, Codec: track.Codec})
		}
	}
	if summary.Profile != profile {
		return false
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Index < streams[j].Index })
	if len(streams) != len(summary.Tracks) || len(streams) == 0 {
		return false
	}
	for index, stream := range streams {
		track := summary.Tracks[index]
		if track.StreamIndex != stream.Index || track.Codec != stream.Codec {
			return false
		}
	}
	return true
}

func (s *Store) generateSubtitleTimelineFile(ctx context.Context, snapshot indexedMediaSource, expected AnalysisSource, job SubtitleTimelineJob,
	fence AnalysisFence, parent, directory *os.Root, old subtitleTimelineManifest, oldFileInfo os.FileInfo, encode SubtitleTimelineExternalEncoder) (artifact SubtitleTimelineArtifact, resultErr error) {
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
	external, err := s.openSubtitleTimelineExternalFiles(ctx, parent, snapshot)
	if err != nil {
		return artifact, err
	}
	defer func() { resultErr = errors.Join(resultErr, external.close()) }()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return artifact, err
	}
	id := hex.EncodeToString(random[:])
	temporary, generation, manifestTemporary := ".subtitle-timeline-"+id+".part", "gen-"+id+".gstl", ".manifest-"+id+".part"
	output, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return artifact, fmt.Errorf("%w: subtitle timeline destination is not writable: %w", ErrUnavailable, err)
	}
	defer output.Close()
	var outputIdentity, manifestIdentity os.FileInfo
	defer func() {
		if info, err := output.Stat(); err == nil {
			outputIdentity = info
		}
		_ = removeOwnedBackgroundClipFile(directory, temporary, outputIdentity)
	}()
	manifestOwned := false
	defer func() {
		if manifestOwned {
			_ = removeOwnedBackgroundClipFile(directory, manifestTemporary, manifestIdentity)
		}
	}()
	writer := &subtitleTimelineOutput{ctx: ctx, file: output, digest: sha256.New()}
	summary, err := encode(read.Context(ctx), input, snapshot.mediaFile, job, external.inputs, writer)
	if err != nil {
		return artifact, err
	}
	if err := external.recheck(ctx, parent); err != nil {
		return artifact, err
	}
	summary.Tracks = cloneSubtitleTimelineTracks(summary.Tracks)
	outputIdentity, err = output.Stat()
	if err := errors.Join(err, output.Sync(), output.Close()); err != nil {
		return artifact, err
	}
	digest := hex.EncodeToString(writer.digest.Sum(nil))
	staged, parsed, err := readSubtitleTimelinePayload(ctx, directory, temporary, writer.bytes, digest)
	if err != nil {
		return artifact, err
	}
	if err := staged.Close(); err != nil {
		return artifact, err
	}
	if !sameSubtitleTimelineSummary(summary, parsed) || !subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media, subtitleTimelineSnapshotBitmap(snapshot)) {
		return artifact, ErrSubtitleTimelineStorageConflict
	}
	stamp, err := subtitleTimelineSourceStamp(snapshot)
	if err != nil {
		return artifact, err
	}
	value := subtitleTimelineManifest{Format: subtitleTimelineFileFormat, SourceName: filepath.Base(snapshot.relativePath), SourceStamp: stamp,
		SourceRevision: expected.SourceRevision, OperationID: job.OperationID, Generation: generation, SHA256: digest, Summary: summary, CreatedAt: time.Now().UTC()}
	if !validSubtitleTimelineManifest(value, value.SourceName) {
		return artifact, ErrSubtitleTimelineStorageConflict
	}
	manifestIdentity, err = writeSubtitleTimelineJSON(directory, manifestTemporary, value)
	if err != nil {
		return artifact, err
	}
	manifestOwned = true
	newManifestIdentity := manifestIdentity
	if _, err := s.ValidateSubtitleTimelineJob(ctx, fence, job); err != nil {
		return artifact, err
	}
	current, err := s.readAdmittedAnalysisSource(ctx, expected)
	if err != nil {
		return artifact, err
	}
	if err := s.recheckSubtitleTimelineDirectory(current, parent, directory); err != nil {
		return artifact, err
	}
	currentStamp, err := subtitleTimelineSourceStamp(current)
	if err != nil || currentStamp != stamp {
		return artifact, errors.Join(ErrAnalysisSourceChanged, err)
	}
	before, err := parent.Lstat(value.SourceName)
	opened, statErr := input.Stat()
	if err != nil || statErr != nil || !snapshot.matches(before) || !snapshot.matches(opened) || !sameMediaSourceFile(before, opened) {
		return artifact, ErrSourceChanged
	}
	if err := external.recheck(ctx, parent); err != nil {
		return artifact, err
	}
	if err := external.close(); err != nil {
		return artifact, err
	}
	if err := retireInput(); err != nil {
		return artifact, err
	}
	if err := backgroundClipRenameNoReplace(directory, temporary, generation); err != nil {
		return artifact, err
	}
	retainGeneration := false
	defer func() {
		if !retainGeneration {
			_ = removeOwnedBackgroundClipFile(directory, generation, outputIdentity)
		}
	}()
	if err := syncBackgroundClipDirectory(directory); err != nil {
		return artifact, err
	}
	var oldManifestIdentity os.FileInfo
	if old.Generation != "" {
		var observed subtitleTimelineManifest
		oldManifestIdentity, err = readSubtitleTimelineJSON(directory, "manifest.json", &observed)
		if err != nil || !sameSubtitleTimelineManifest(old, observed) {
			return artifact, ErrSubtitleTimelineStorageConflict
		}
	}
	replaced := false
	err = s.WithSubtitleTimelinePublication(ctx, fence, job, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		if old.Generation == "" {
			err = backgroundClipRenameNoReplace(directory, manifestTemporary, "manifest.json")
		} else {
			err = mediaEditExchange(directory, manifestTemporary, directory, "manifest.json")
			if err == nil {
				manifestOwned = false
			}
		}
		replaced = err == nil
		return err
	})
	if err == nil && old.Generation != "" {
		var observed subtitleTimelineManifest
		displacedInfo, readErr := readSubtitleTimelineJSON(directory, manifestTemporary, &observed)
		if readErr != nil || !sameSubtitleTimelineManifest(observed, old) || !os.SameFile(oldManifestIdentity, displacedInfo) {
			err = ErrSubtitleTimelineStorageConflict
		} else {
			manifestOwned, manifestIdentity = true, displacedInfo
		}
	}
	publication := subtitleTimelinePublication{directory: directory, current: value, previous: old, newManifestIdentity: newManifestIdentity,
		replaced: replaced, temporaryOwned: manifestOwned, temporaryIdentity: manifestIdentity, displacedIdentity: oldManifestIdentity}
	if old.Generation != "" {
		publication.displaced = manifestTemporary
	}
	artifact, err = publication.finish(err, func() error { return syncBackgroundClipDirectory(directory) }, func() (SubtitleTimelineArtifact, error) {
		current, err := s.readAdmittedAnalysisSource(ctx, expected)
		if err != nil {
			return SubtitleTimelineArtifact{}, err
		}
		currentStamp, err := subtitleTimelineSourceStamp(current)
		if err != nil || currentStamp != stamp {
			return SubtitleTimelineArtifact{}, errors.Join(ErrAnalysisSourceChanged, err)
		}
		if err := s.recheckSubtitleTimelineDirectory(current, parent, directory); err != nil {
			return SubtitleTimelineArtifact{}, err
		}
		currentSource, err := s.subtitleTimelineSourceCurrent(ctx, parent, current)
		if err != nil || !currentSource {
			return SubtitleTimelineArtifact{}, errors.Join(ErrSourceChanged, err)
		}
		file, manifest, found, readErr := readSubtitleTimeline(ctx, directory, value.SourceName, stamp, true)
		if readErr != nil {
			return SubtitleTimelineArtifact{}, readErr
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if !sameSubtitleTimelineManifest(manifest, value) || statErr != nil || !os.SameFile(outputIdentity, info) {
			return SubtitleTimelineArtifact{}, errors.Join(ErrSubtitleTimelineStorageConflict, statErr, closeErr)
		}
		return found, errors.Join(closeErr, ctx.Err())
	})
	retainGeneration = publication.retainGeneration
	manifestOwned, manifestIdentity = publication.temporaryOwned, publication.temporaryIdentity
	if err != nil {
		return SubtitleTimelineArtifact{}, err
	}
	if job.Force && old.Generation != "" && old.Generation != generation {
		if current, err := directory.Lstat(old.Generation); err == nil && sameMediaSourceFile(oldFileInfo, current) {
			_ = removeOwnedBackgroundClipFile(directory, old.Generation, oldFileInfo)
			_ = syncBackgroundClipDirectory(directory)
		}
	}
	return artifact, nil
}

type subtitleTimelinePublication struct {
	directory                                  *os.Root
	current, previous                          subtitleTimelineManifest
	newManifestIdentity                        os.FileInfo
	displaced                                  string
	displacedIdentity                          os.FileInfo
	replaced, retainGeneration, temporaryOwned bool
	temporaryIdentity                          os.FileInfo
}

// Completion includes directory durability and verified read-back. Source
// retirement precedes publication; later cancellation in the worker epilogue
// does not turn a committed generation into failure.
func (publication *subtitleTimelinePublication) finish(publishErr error, syncDirectory func() error, readBack func() (SubtitleTimelineArtifact, error)) (SubtitleTimelineArtifact, error) {
	var artifact SubtitleTimelineArtifact
	err := publishErr
	if err == nil && !publication.replaced {
		err = ErrSubtitleTimelineStorageConflict
	}
	if err == nil {
		err = syncDirectory()
	}
	if err == nil {
		artifact, err = readBack()
		if err == nil && (!artifact.Available || artifact.Stale) {
			err = ErrSubtitleTimelineStorageConflict
		}
	}
	if err == nil {
		publication.retainGeneration = true
		return artifact, nil
	}
	if publication.replaced {
		rollbackErr := publication.rollback()
		if rollbackErr != nil {
			publication.retainGeneration, publication.temporaryOwned = true, false
		} else {
			publication.temporaryOwned, publication.temporaryIdentity = true, publication.newManifestIdentity
		}
		err = errors.Join(err, rollbackErr)
	}
	return SubtitleTimelineArtifact{}, err
}

func (publication *subtitleTimelinePublication) rollback() error {
	var current subtitleTimelineManifest
	info, err := readSubtitleTimelineJSON(publication.directory, "manifest.json", &current)
	if err != nil || !sameSubtitleTimelineManifest(current, publication.current) || publication.newManifestIdentity == nil || !os.SameFile(info, publication.newManifestIdentity) {
		return ErrSubtitleTimelineStorageConflict
	}
	if publication.displaced != "" {
		var displaced subtitleTimelineManifest
		displacedInfo, err := readSubtitleTimelineJSON(publication.directory, publication.displaced, &displaced)
		if err != nil || !sameSubtitleTimelineManifest(displaced, publication.previous) || publication.displacedIdentity == nil || !os.SameFile(displacedInfo, publication.displacedIdentity) {
			return ErrSubtitleTimelineStorageConflict
		}
		if err := mediaEditExchange(publication.directory, publication.displaced, publication.directory, "manifest.json"); err != nil {
			return err
		}
	} else if err := removeOwnedBackgroundClipFile(publication.directory, "manifest.json", info); err != nil {
		return err
	}
	return syncBackgroundClipDirectory(publication.directory)
}
