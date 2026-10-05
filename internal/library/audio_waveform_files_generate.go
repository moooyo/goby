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
	"slices"
	"sort"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type audioWaveformOutput struct {
	ctx    context.Context
	file   *os.File
	digest hash.Hash
	bytes  int64
}

func (output *audioWaveformOutput) Write(data []byte) (int, error) {
	if err := output.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(data)) > media.MaxAudioWaveformBytes-output.bytes {
		return 0, media.ErrAnalysisBudget
	}
	n, err := output.file.Write(data)
	_, _ = output.digest.Write(data[:n])
	output.bytes += int64(n)
	return n, err
}

func audioWaveformCanReuse(job AudioWaveformJob, artifact AudioWaveformArtifact) bool {
	return artifact.Available && (!job.Force || job.OperationID != "" && artifact.OperationID == job.OperationID)
}

// GenerateAudioWaveform retains existing material, including stale material,
// until a distinct explicit Force operation successfully replaces it. A
// completed Force operation can be replayed after a lost queue acknowledgement.
func (s *Store) GenerateAudioWaveform(ctx context.Context, job AudioWaveformJob, fence AnalysisFence, encode AudioWaveformEncoder) (AudioWaveformArtifact, error) {
	if ctx == nil || fence == nil || encode == nil {
		return AudioWaveformArtifact{}, ErrInvalidInput
	}
	expected, err := s.ValidateAudioWaveformJob(ctx, fence, job)
	if err != nil {
		return AudioWaveformArtifact{}, err
	}
	if artifact, err := s.findAudioWaveformForJob(ctx, expected); err == nil {
		if audioWaveformCanReuse(job, artifact) {
			if _, err := s.ValidateAudioWaveformJob(ctx, fence, job); err != nil {
				return AudioWaveformArtifact{}, err
			}
			artifact.Reused = true
			return artifact, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return AudioWaveformArtifact{}, audioWaveformStorageError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(AudioWaveformItemTimeoutSeconds)*time.Second)
	defer cancel()
	var result AudioWaveformArtifact
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
		directory, err := openAudioWaveformDirectory(parent, sourceName, true)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		if err := claimAudioWaveformDirectory(directory, sourceName); err != nil {
			return nil, MediaFile{}, err
		}
		unlock, err := lockBackgroundClipDirectory(directory)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer unlock()
		stamp, err := audioWaveformSourceStamp(snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		previous, old, artifact, readErr := readAudioWaveform(work, directory, sourceName, stamp, audioWaveformSourceCurrent(parent, snapshot))
		var oldFileInfo os.FileInfo
		if readErr == nil {
			oldFileInfo, err = previous.Stat()
			err = errors.Join(err, previous.Close())
			if err != nil {
				return nil, MediaFile{}, err
			}
			if audioWaveformCanReuse(job, artifact) {
				artifact.Reused = true
				result = artifact
				return nil, snapshot.mediaFile, nil
			}
		} else if !errors.Is(readErr, ErrNotFound) {
			return nil, MediaFile{}, readErr
		} else if err := checkUnpublishedAudioWaveforms(directory, job.Force); err != nil {
			return nil, MediaFile{}, err
		}
		if _, err := s.ValidateAudioWaveformJob(work, fence, job); err != nil {
			return nil, MediaFile{}, err
		}
		artifact, err = s.generateAudioWaveformFile(work, snapshot, expected, job, fence, parent, directory, old, oldFileInfo, encode)
		if err != nil {
			return nil, MediaFile{}, err
		}
		result, committed = artifact, true
		return nil, snapshot.mediaFile, nil
	})
	return audioWaveformWorkerResult(result, committed, err)
}

func audioWaveformWorkerResult(artifact AudioWaveformArtifact, committed bool, err error) (AudioWaveformArtifact, error) {
	if committed && artifact.Available && (err == context.Canceled || err == context.DeadlineExceeded) {
		return artifact, nil
	}
	if err != nil {
		return AudioWaveformArtifact{}, audioWaveformStorageError(err)
	}
	return artifact, nil
}

func (s *Store) findAudioWaveformForJob(ctx context.Context, expected AnalysisSource) (AudioWaveformArtifact, error) {
	var artifact AudioWaveformArtifact
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
		directory, err := openAudioWaveformDirectory(parent, filepath.Base(snapshot.relativePath), false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer directory.Close()
		stamp, err := audioWaveformSourceStamp(snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		opened, _, found, err := readAudioWaveform(work, directory, filepath.Base(snapshot.relativePath), stamp, audioWaveformSourceCurrent(parent, snapshot))
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := s.recheckAudioWaveformDirectory(snapshot, parent, directory); err != nil {
			opened.Close()
			return nil, MediaFile{}, err
		}
		artifact = found
		return opened, snapshot.mediaFile, nil
	})
	if err != nil {
		return AudioWaveformArtifact{}, err
	}
	return artifact, file.Close()
}

func audioWaveformSummaryMatchesSource(summary media.AudioWaveformSummary, info *media.Info) bool {
	if info == nil || summary.DurationTicks != info.DurationTicks {
		return false
	}
	var streams []media.Stream
	for _, stream := range info.Streams {
		if stream.CodecType == "audio" && !stream.IsExternal && !stream.IsAttachedPicture {
			streams = append(streams, stream)
		}
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Index < streams[j].Index })
	if len(streams) != len(summary.Tracks) || len(streams) == 0 {
		return false
	}
	for index, stream := range streams {
		track := summary.Tracks[index]
		if track.StreamIndex != stream.Index || track.Channels != stream.Channels || track.SampleRate != stream.SampleRate || track.ChannelLayout != stream.ChannelLayout {
			return false
		}
	}
	return true
}

func (s *Store) generateAudioWaveformFile(ctx context.Context, snapshot indexedMediaSource, expected AnalysisSource, job AudioWaveformJob,
	fence AnalysisFence, parent, directory *os.Root, old audioWaveformManifest, oldFileInfo os.FileInfo, encode AudioWaveformEncoder) (artifact AudioWaveformArtifact, resultErr error) {
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
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return artifact, err
	}
	id := hex.EncodeToString(random[:])
	temporary, generation, manifestTemporary := ".waveform-"+id+".part", "gen-"+id+".gawf", ".manifest-"+id+".part"
	output, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return artifact, fmt.Errorf("%w: waveform destination is not writable: %w", ErrUnavailable, err)
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
	writer := &audioWaveformOutput{ctx: ctx, file: output, digest: sha256.New()}
	summary, err := encode(read.Context(ctx), input, snapshot.mediaFile, job, writer)
	if err != nil {
		return artifact, err
	}
	summary.Tracks = slices.Clone(summary.Tracks)
	outputIdentity, err = output.Stat()
	if err := errors.Join(err, output.Sync(), output.Close()); err != nil {
		return artifact, err
	}
	digest := hex.EncodeToString(writer.digest.Sum(nil))
	staged, parsed, err := readAudioWaveformPayload(ctx, directory, temporary, writer.bytes, digest)
	if err != nil {
		return artifact, err
	}
	if err := staged.Close(); err != nil {
		return artifact, err
	}
	if !sameAudioWaveformSummary(summary, parsed) || !audioWaveformSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		return artifact, ErrAudioWaveformStorageConflict
	}
	stamp, err := audioWaveformSourceStamp(snapshot)
	if err != nil {
		return artifact, err
	}
	value := audioWaveformManifest{Format: audioWaveformFileFormat, SourceName: filepath.Base(snapshot.relativePath), SourceStamp: stamp,
		SourceRevision: expected.SourceRevision, OperationID: job.OperationID, Generation: generation, SHA256: digest, Summary: summary, CreatedAt: time.Now().UTC()}
	if !validAudioWaveformManifest(value, value.SourceName) {
		return artifact, ErrAudioWaveformStorageConflict
	}
	manifestIdentity, err = writeAudioWaveformJSON(directory, manifestTemporary, value)
	if err != nil {
		return artifact, err
	}
	manifestOwned = true
	newManifestIdentity := manifestIdentity
	if _, err := s.ValidateAudioWaveformJob(ctx, fence, job); err != nil {
		return artifact, err
	}
	current, err := s.readAdmittedAnalysisSource(ctx, expected)
	if err != nil {
		return artifact, err
	}
	if err := s.recheckAudioWaveformDirectory(current, parent, directory); err != nil {
		return artifact, err
	}
	before, err := parent.Lstat(value.SourceName)
	opened, statErr := input.Stat()
	if err != nil || statErr != nil || !snapshot.matches(before) || !snapshot.matches(opened) || !sameMediaSourceFile(before, opened) {
		return artifact, ErrSourceChanged
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
		var observed audioWaveformManifest
		oldManifestIdentity, err = readAudioWaveformJSON(directory, "manifest.json", &observed)
		if err != nil || !sameAudioWaveformManifest(old, observed) {
			return artifact, ErrAudioWaveformStorageConflict
		}
	}
	replaced := false
	err = s.WithAudioWaveformPublication(ctx, fence, job, func() error {
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
		var observed audioWaveformManifest
		displacedInfo, readErr := readAudioWaveformJSON(directory, manifestTemporary, &observed)
		if readErr != nil || !sameAudioWaveformManifest(observed, old) || !os.SameFile(oldManifestIdentity, displacedInfo) {
			err = ErrAudioWaveformStorageConflict
		} else {
			manifestOwned, manifestIdentity = true, displacedInfo
		}
	}
	publication := audioWaveformPublication{directory: directory, current: value, previous: old, newManifestIdentity: newManifestIdentity,
		replaced: replaced, temporaryOwned: manifestOwned, temporaryIdentity: manifestIdentity}
	if old.Generation != "" {
		publication.displaced = manifestTemporary
	}
	artifact, err = publication.finish(err, func() error { return syncBackgroundClipDirectory(directory) }, func() (AudioWaveformArtifact, error) {
		file, manifest, found, readErr := readAudioWaveform(ctx, directory, value.SourceName, stamp, true)
		if readErr != nil {
			return AudioWaveformArtifact{}, readErr
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if !sameAudioWaveformManifest(manifest, value) || statErr != nil || !os.SameFile(outputIdentity, info) {
			return AudioWaveformArtifact{}, errors.Join(ErrAudioWaveformStorageConflict, statErr, closeErr)
		}
		return found, errors.Join(closeErr, ctx.Err())
	})
	retainGeneration = publication.retainGeneration
	manifestOwned, manifestIdentity = publication.temporaryOwned, publication.temporaryIdentity
	if err != nil {
		return AudioWaveformArtifact{}, err
	}
	if job.Force && old.Generation != "" && old.Generation != generation {
		if current, err := directory.Lstat(old.Generation); err == nil && sameMediaSourceFile(oldFileInfo, current) {
			_ = removeOwnedBackgroundClipFile(directory, old.Generation, oldFileInfo)
			_ = syncBackgroundClipDirectory(directory)
		}
	}
	return artifact, nil
}

type audioWaveformPublication struct {
	directory                                  *os.Root
	current, previous                          audioWaveformManifest
	newManifestIdentity                        os.FileInfo
	displaced                                  string
	replaced, retainGeneration, temporaryOwned bool
	temporaryIdentity                          os.FileInfo
}

// Completion includes directory durability and verified read-back. Source
// retirement precedes publication; later cancellation in the worker epilogue
// does not turn a committed generation into failure.
func (publication *audioWaveformPublication) finish(publishErr error, syncDirectory func() error, readBack func() (AudioWaveformArtifact, error)) (AudioWaveformArtifact, error) {
	var artifact AudioWaveformArtifact
	err := publishErr
	if err == nil && !publication.replaced {
		err = ErrAudioWaveformStorageConflict
	}
	if err == nil {
		err = syncDirectory()
	}
	if err == nil {
		artifact, err = readBack()
		if err == nil && (!artifact.Available || artifact.Stale) {
			err = ErrAudioWaveformStorageConflict
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
	return AudioWaveformArtifact{}, err
}

func (publication *audioWaveformPublication) rollback() error {
	var current audioWaveformManifest
	info, err := readAudioWaveformJSON(publication.directory, "manifest.json", &current)
	if err != nil || !sameAudioWaveformManifest(current, publication.current) || publication.newManifestIdentity == nil || !os.SameFile(info, publication.newManifestIdentity) {
		return ErrAudioWaveformStorageConflict
	}
	if publication.displaced != "" {
		if err := mediaEditExchange(publication.directory, publication.displaced, publication.directory, "manifest.json"); err != nil {
			return err
		}
	} else if err := removeOwnedBackgroundClipFile(publication.directory, "manifest.json", info); err != nil {
		return err
	}
	return syncBackgroundClipDirectory(publication.directory)
}
