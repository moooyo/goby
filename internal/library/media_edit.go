package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type mediaEditJournal struct {
	Version               int
	OperationID           string
	Target                deletionTargetDocument
	SourceRevision        string
	StreamIndex           int
	Container             string
	Host                  string
	StageName             string
	StageIdentity         string
	SourceSHA256          string
	AttributeSHA256       string
	Candidate             mediaEditFile
	CandidateMedia        media.Info
	Proof                 media.SubtitleRemovalEvidence
	ReadyHash             string
	Published             *mediaEditFile `json:",omitempty"`
	PublishedSeekIdentity string         `json:",omitempty"`
	MaximumOutputBytes    int64
	MaximumRuntimeSeconds int64
}

type mediaEditSummary struct {
	Engine                string `json:"Engine"`
	ContainerProfile      string `json:"ContainerProfile"`
	RemovedStreamIndex    int    `json:"RemovedStreamIndex"`
	PreservedStreamCount  int    `json:"PreservedStreamCount"`
	OriginalBytes         int64  `json:"OriginalBytes"`
	CandidateBytes        int64  `json:"CandidateBytes"`
	BackupRetained        bool   `json:"BackupRetained"`
	MaximumOutputBytes    int64  `json:"MaximumOutputBytes"`
	MaximumRuntimeSeconds int64  `json:"MaximumRuntimeSeconds"`
}

func (j mediaEditJournal) source() fileDeletionTarget {
	target := j.Target.Target
	r := j.Target.Root
	target.File.Root = libraryRoot{r.ID, r.LibraryID, r.Path, r.AllowedPath, r.RelativePath}
	return target
}

func mediaEditContainer(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv":
		return "mkv"
	case ".mka":
		return "mka"
	case ".mp4":
		return "mp4"
	default:
		return ""
	}
}

func readMediaEditTarget(ctx context.Context, tx pgx.Tx, actor identity.Principal, operation MediaOperation) (fileDeletionTarget, string, error) {
	if !validMetadataActor(actor) {
		return fileDeletionTarget{}, "", ErrForbidden
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return fileDeletionTarget{}, "", errors.Join(ErrForbidden, err)
	}
	source, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), operation.ItemID, operation.MediaSourceID)
	if err != nil {
		return fileDeletionTarget{}, "", err
	}
	container := mediaEditContainer(source.relativePath)
	if container == "" {
		return fileDeletionTarget{}, "", fmt.Errorf("%w: embedded subtitle removal supports Matroska and validated MP4 profiles", ErrInvalidInput)
	}
	if operation.Parameters.Profile != "" && (container == "mp4" && operation.Parameters.Profile != "mp4-movtext-v1" || container != "mp4" && operation.Parameters.Profile != "matroska-v1") {
		return fileDeletionTarget{}, "", ErrInvalidInput
	}
	found := false
	for _, stream := range source.mediaFile.Item.Media.Streams {
		if stream.Index == operation.StreamIndex && stream.CodecType == "subtitle" && !stream.IsExternal {
			found = true
		}
	}
	if !found {
		return fileDeletionTarget{}, "", ErrNotFound
	}
	var revision int64
	var sourceRevision string
	if err := tx.QueryRow(ctx, `SELECT r.binding_revision,`+MediaOperationSourceRevisionSQL+` FROM items i JOIN library_roots r ON r.id=i.root_id WHERE i.id=$1`, operation.ItemID).Scan(&revision, &sourceRevision); err != nil {
		return fileDeletionTarget{}, "", err
	}
	if revision < 1 || sourceRevision != operation.SourceRevision || source.root.id != operation.RootID || source.root.libraryID != operation.LibraryID {
		return fileDeletionTarget{}, "", ErrSourceChanged
	}
	target := fileDeletionTarget{Kind: "media", ItemID: operation.ItemID, LibraryID: source.root.libraryID, ParentID: source.mediaFile.Item.ParentID, SubtitleIndex: operation.StreamIndex,
		File:            fileDeletionSpec{Root: source.root, RelativePath: source.relativePath, Identity: source.identity, Size: source.mediaFile.Size, ModifiedAt: source.mediaFile.ModifiedAt, ChangeTimeNs: source.mediaFile.Item.Media.FileChangeTimeNs},
		BindingRevision: revision, SourceTag: source.mediaFile.ETag}
	target.BindingFingerprint, err = readDeletionBindingFingerprint(ctx, tx, target)
	return target, container, err
}

func (s *Store) captureMediaEditTarget(ctx context.Context, actor identity.Principal, operation MediaOperation) (fileDeletionTarget, string, error) {
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return fileDeletionTarget{}, "", err
	}
	defer rollback(tx)
	target, container, err := readMediaEditTarget(ctx, tx, actor, operation)
	if err != nil {
		return fileDeletionTarget{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return fileDeletionTarget{}, "", err
	}
	return target, container, nil
}

func mediaEditReadyHash(journal mediaEditJournal) (string, error) {
	journal.ReadyHash, journal.Published, journal.PublishedSeekIdentity = "", nil, ""
	encoded, err := json.Marshal(journal)
	if err != nil || len(encoded) > MaxMediaOperationDocumentBytes {
		return "", errors.Join(ErrUnavailable, err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func encodeMediaEditResult(journal mediaEditJournal) (MediaOperationResult, error) {
	encoded, err := json.Marshal(journal)
	if err != nil || len(encoded) > MaxMediaOperationDocumentBytes {
		return MediaOperationResult{}, errors.Join(ErrUnavailable, err)
	}
	summary, err := json.Marshal(mediaEditSummary{Engine: journal.Proof.Engine, ContainerProfile: journal.Container, RemovedStreamIndex: journal.StreamIndex, PreservedStreamCount: len(journal.CandidateMedia.Streams), OriginalBytes: journal.source().File.Size, CandidateBytes: journal.Candidate.Size, BackupRetained: journal.Published != nil, MaximumOutputBytes: journal.MaximumOutputBytes, MaximumRuntimeSeconds: journal.MaximumRuntimeSeconds})
	return MediaOperationResult{Summary: summary, ResultHash: journal.ReadyHash, Journal: encoded}, err
}

func decodeMediaEditJournal(operation MediaOperation) (mediaEditJournal, error) {
	var journal mediaEditJournal
	if len(operation.Journal) == 0 || len(operation.Journal) > MaxMediaOperationDocumentBytes || json.Unmarshal(operation.Journal, &journal) != nil {
		return journal, ErrMediaOperationRecovery
	}
	target := journal.source()
	if journal.Version != 1 || journal.OperationID != operation.ID || journal.SourceRevision != operation.SourceRevision || journal.StreamIndex != operation.StreamIndex || target.ItemID != operation.ItemID || target.LibraryID != operation.LibraryID || target.File.Root.id != operation.RootID || journal.StageName != ".goby-edit-"+operation.ID || !validMediaEditStageName(journal.StageName) || !validFileDeletionIdentity(journal.StageIdentity) || !journal.Candidate.valid() || journal.Container != mediaEditContainer(target.File.RelativePath) || target.BindingFingerprint == "" || target.BindingRevision < 1 || journal.CandidateMedia.ProbeVersion < media.CurrentProbeVersion || journal.CandidateMedia.Size != journal.Candidate.Size || len(journal.CandidateMedia.Streams) == 0 || journal.CandidateMedia.FileChangeTimeNs != journal.Candidate.ChangeTimeNs {
		return journal, ErrMediaOperationRecovery
	}
	hash, err := mediaEditReadyHash(journal)
	if err != nil || hash != journal.ReadyHash || hash != operation.ResultHash {
		return journal, ErrMediaOperationRecovery
	}
	for _, digest := range []string{journal.SourceSHA256, journal.AttributeSHA256, journal.Host} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return journal, ErrMediaOperationRecovery
		}
	}
	return journal, nil
}

// StageEmbeddedSubtitleRemoval generates and verifies an unpublished candidate.
// It cannot rename or rewrite the selected media file. The manager owns the
// worker lease, cancellation, durable ready result and admission backpressure.
func (s *Store) StageEmbeddedSubtitleRemoval(ctx context.Context, work MediaOperationWork, ffmpeg, ffprobe string, progress func(MediaOperationProgress)) (result MediaOperationResult, resultErr error) {
	op := work.Operation
	if ctx == nil || s == nil || s.pool == nil || s.prober == nil || !validMediaOperationWork(work) || work.Discard || work.Apply || op.Kind != MediaOperationRemoveSubtitle || !validMediaEditStageName(".goby-edit-"+op.ID) {
		return MediaOperationResult{}, ErrInvalidInput
	}
	currentWork, err := readMediaOperation(ctx, s.pool, op.ID, false)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if err := checkMediaOperationWork(currentWork, work, false); err != nil {
		return MediaOperationResult{}, err
	}
	op, work.Operation = currentWork, currentWork
	target, container, err := s.captureMediaEditTarget(ctx, op.RequestActor, op)
	if err != nil {
		return MediaOperationResult{}, err
	}
	execution, timeout, outputBudget, err := readMediaEditExecution(op, ffmpeg, ffprobe, container)
	if err != nil {
		return MediaOperationResult{}, err
	}
	outputBudget = min(outputBudget, target.File.Size+max(64<<20, target.File.Size/20))
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := verifyMediaEditTools(ctx, execution); err != nil {
		return MediaOperationResult{}, err
	}
	host, err := mediaDeletionHostIdentity()
	if err != nil {
		return MediaOperationResult{}, err
	}
	topology, err := s.captureDeletionTopology(ctx, target)
	if err != nil {
		return MediaOperationResult{}, err
	}
	defer topology.Close()
	capture, err := s.openMediaEditCapture(ctx, target.File, ".goby-edit-"+op.ID, "", nil)
	if err != nil {
		return MediaOperationResult{}, err
	}
	defer capture.Close()
	candidate, err := capture.createCandidate(ctx)
	if err != nil {
		if errors.Is(err, ErrMediaOperationRecovery) {
			result, witnessErr := incompleteMediaEditResult(op, target, host, capture)
			if witnessErr == nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				witnessErr = s.retainIncompleteMediaEdit(cleanup, work, result)
			}
			return result, errors.Join(err, witnessErr)
		}
		return MediaOperationResult{}, err
	}
	defer func() {
		if resultErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if cleanupErr := capture.discardUnpublished(cleanup); cleanupErr != nil {
				var witnessErr error
				result, witnessErr = incompleteMediaEditResult(op, target, host, capture)
				if witnessErr == nil {
					witnessErr = s.retainIncompleteMediaEdit(cleanup, work, result)
				}
				resultErr = errors.Join(resultErr, ErrMediaOperationRecovery, cleanupErr, witnessErr)
			}
		}
	}()
	if progress != nil {
		stage := "remuxing"
		if container == "mp4" {
			stage = "copying"
		}
		progress(MediaOperationProgress{Stage: stage, Total: target.File.Size})
	}
	proof, err := media.RemuxSubtitleRemoval(ctx, capture.base.source, candidate, media.SubtitleRemovalOptions{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Container: container, StreamIndex: op.StreamIndex, MaxOutputBytes: outputBudget, Timeout: timeout})
	if err != nil {
		return MediaOperationResult{}, err
	}
	if err := verifyMediaEditTools(ctx, execution); err != nil {
		return MediaOperationResult{}, err
	}
	if progress != nil {
		progress(MediaOperationProgress{Stage: "validating", Processed: target.File.Size, Total: target.File.Size})
	}
	attributes, err := mediaEditCopyAttributes(capture.base.source, candidate)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if err := candidate.Sync(); err != nil {
		return MediaOperationResult{}, err
	}
	info, err := s.prober.ProbeFile(ctx, candidate)
	if err != nil {
		return MediaOperationResult{}, err
	}
	stat, err := candidate.Stat()
	if err != nil {
		return MediaOperationResult{}, err
	}
	digest, err := mediaEditDigest(ctx, candidate, stat.Size())
	if err != nil {
		return MediaOperationResult{}, err
	}
	sourceDigest, err := mediaEditDigest(ctx, capture.base.source, target.File.Size)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if proof.SourceSHA256 != sourceDigest || proof.CandidateSHA256 != digest {
		return MediaOperationResult{}, ErrSourceChanged
	}
	if err := topology.Revalidate(ctx); err != nil {
		return MediaOperationResult{}, err
	}
	if err := capture.base.verifyPayload(capture.base.parent, filepath.Base(filepath.FromSlash(target.File.RelativePath)), target.File.ChangeTimeNs); err != nil {
		return MediaOperationResult{}, err
	}
	current, _, err := s.captureMediaEditTarget(ctx, op.RequestActor, op)
	if err != nil || !sameFileDeletionTarget(target, current) {
		return MediaOperationResult{}, errors.Join(ErrSourceChanged, err)
	}
	root := target.File.Root
	journal := mediaEditJournal{Version: 1, OperationID: op.ID, Target: deletionTargetDocument{Target: target, Root: deletionRootDocument{root.id, root.libraryID, root.path, root.allowedPath, root.relativePath}}, SourceRevision: op.SourceRevision, StreamIndex: op.StreamIndex, Container: container, Host: host, StageName: capture.base.spec.StageName, StageIdentity: capture.base.stageIdentity, SourceSHA256: sourceDigest, AttributeSHA256: attributes, Candidate: mediaEditFileInfo(stat, digest), CandidateMedia: info, Proof: proof}
	journal.MaximumOutputBytes, journal.MaximumRuntimeSeconds = outputBudget, int64(timeout/time.Second)
	journal.ReadyHash, err = mediaEditReadyHash(journal)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if err := fileDeletionSyncDirectory(capture.base.stage); err != nil {
		return MediaOperationResult{}, err
	}
	return encodeMediaEditResult(journal)
}

// ApplyEmbeddedSubtitleRemoval requires a separately authorized apply claim.
// The original inode is retained in the private directory after atomic exchange.
// No automatic startup, cancellation or backup-restore path calls this method.
func (s *Store) ApplyEmbeddedSubtitleRemoval(ctx context.Context, work MediaOperationWork, retire func(context.Context, string, string) error) (MediaOperationResult, error) {
	op := work.Operation
	if ctx == nil || s == nil || s.pool == nil || !validMediaOperationWork(work) || work.Discard || !work.Apply || op.Kind != MediaOperationRemoveSubtitle || retire == nil {
		return MediaOperationResult{}, ErrInvalidInput
	}
	currentWork, err := readMediaOperation(ctx, s.pool, op.ID, false)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if err := checkMediaOperationWork(currentWork, work, currentWork.PublicationPhase == "catalog_committed"); err != nil {
		return MediaOperationResult{}, err
	}
	op, work.Operation = currentWork, currentWork
	journal, err := decodeMediaEditJournal(op)
	if err != nil {
		return MediaOperationResult{}, err
	}
	authority, err := s.beginMetadataRead(ctx, op.ApplyActor)
	if err != nil {
		return MediaOperationResult{}, err
	}
	if err := verifyDeletionRoot(ctx, authority, journal.source(), false); err != nil {
		rollback(authority)
		return MediaOperationResult{}, err
	}
	if op.PublicationPhase == "catalog_committed" {
		var identity string
		var size, changeTime int64
		var modified time.Time
		if err := authority.QueryRow(ctx, `SELECT file_identity,file_size,modified_at,(media->>'FileChangeTimeNs')::bigint FROM items WHERE id=$1 AND library_id=$2 AND root_id=$3`, op.ItemID, op.LibraryID, op.RootID).Scan(&identity, &size, &modified, &changeTime); err != nil {
			rollback(authority)
			return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
		}
		if identity != journal.Candidate.Identity || size != journal.Candidate.Size || !modified.Equal(journal.Candidate.ModifiedAt) || changeTime <= 0 {
			rollback(authority)
			return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, ErrSourceChanged)
		}
	}
	if err := authority.Commit(ctx); err != nil {
		return MediaOperationResult{}, err
	}
	host, err := mediaDeletionHostIdentity()
	if err != nil || host != journal.Host {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, ErrForbidden, err)
	}
	target := journal.source()
	if op.PublicationPhase != "catalog_committed" {
		current, _, err := s.captureMediaEditTarget(ctx, op.ApplyActor, op)
		if err != nil || !sameFileDeletionTarget(target, current) {
			return MediaOperationResult{}, errors.Join(ErrSourceChanged, err)
		}
	}
	topology, err := s.captureDeletionTopology(ctx, target)
	if err != nil {
		return MediaOperationResult{}, err
	}
	defer topology.Close()
	capture, published, err := s.openMediaEditPublication(ctx, journal, op.PublicationPhase)
	if err != nil {
		return MediaOperationResult{}, err
	}
	defer capture.Close()
	if err := verifyMediaEditDigests(ctx, capture, journal, published); err != nil {
		return MediaOperationResult{}, err
	}
	if err := topology.Revalidate(ctx); err != nil {
		return MediaOperationResult{}, err
	}
	if op.PublicationPhase == "none" || op.PublicationPhase == "" {
		if err := s.PrepareMediaOperationPublication(ctx, work, op.Journal); err != nil {
			return MediaOperationResult{}, err
		}
	}
	if !published {
		if _, err := capture.publish(ctx, journal.Candidate); err != nil {
			if capture.attempted {
				return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
			}
			return MediaOperationResult{}, err
		}
	}
	// Once the prepared journal exists every error retains the source barrier.
	if err := topology.Revalidate(ctx); err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	if err := verifyMediaEditDigests(ctx, capture, journal, true); err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	if err := fileDeletionSyncDirectory(capture.base.stage); err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	if err := fileDeletionSyncDirectory(capture.base.parent); err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	stat, err := capture.candidate.Stat()
	if err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	publishedFile := mediaEditFileInfo(stat, journal.Candidate.SHA256)
	journal.Published = &publishedFile
	journal.PublishedSeekIdentity, err = media.VideoSeekSourceIdentity(stat)
	if err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	if op.PublicationPhase != "catalog_committed" {
		if err := s.CommitMediaOperationApply(ctx, work, func(writeContext context.Context, tx pgx.Tx, current MediaOperation) error {
			return applyMediaEditCatalog(writeContext, tx, current, journal)
		}); err != nil {
			return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
		}
	}
	if err := retire(ctx, op.ItemID, op.MediaSourceID); err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	result, err := encodeMediaEditResult(journal)
	if err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	if err := s.CompleteMediaOperationPublication(ctx, work, result.Journal); err != nil {
		return MediaOperationResult{}, errors.Join(ErrMediaOperationRecovery, err)
	}
	return result, nil
}

func verifyMediaEditDigests(ctx context.Context, capture *mediaEditCapture, journal mediaEditJournal, published bool) error {
	if err := capture.validateCandidate(ctx, journal.Candidate, published); err != nil {
		return err
	}
	if err := capture.verifyOriginal(published); err != nil {
		return err
	}
	digest, err := mediaEditDigest(ctx, capture.base.source, journal.source().File.Size)
	if err != nil || digest != journal.SourceSHA256 {
		return errors.Join(ErrSourceChanged, err)
	}
	if err := capture.verifyOriginal(published); err != nil {
		return err
	}
	for _, file := range []*os.File{capture.base.source, capture.candidate} {
		attributes, err := mediaEditAttributeDigest(file)
		if err != nil || attributes != journal.AttributeSHA256 {
			return errors.Join(ErrSourceChanged, err)
		}
	}
	return nil
}

func applyMediaEditCatalog(ctx context.Context, tx pgx.Tx, operation MediaOperation, journal mediaEditJournal) error {
	if journal.Published == nil {
		return ErrMediaOperationRecovery
	}
	current, _, err := readMediaEditTarget(ctx, tx, operation.ApplyActor, operation)
	if err != nil || !sameFileDeletionTarget(current, journal.source()) {
		return errors.Join(ErrSourceChanged, err)
	}
	info := journal.CandidateMedia
	info.FileChangeTimeNs, info.Size = journal.Published.ChangeTimeNs, journal.Published.Size
	// These indexes were proved from this candidate inode before the exchange.
	// Full digest equality above permits rebinding only its rename-changed ctime.
	info.VideoSeekIndexes = append([]media.VideoSeekIndex(nil), info.VideoSeekIndexes...)
	for index := range info.VideoSeekIndexes {
		info.VideoSeekIndexes[index].SourceIdentity = journal.PublishedSeekIdentity
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE items SET media=$2,file_identity=$3,file_size=$4,modified_at=$5,updated_at=clock_timestamp() WHERE id=$1 AND library_id=$6 AND root_id=$7`, operation.ItemID, encoded, journal.Published.Identity, journal.Published.Size, journal.Published.ModifiedAt, operation.LibraryID, operation.RootID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrSourceChanged
	}
	// A source ID is stable across this edit. Old play IDs must become terminal
	// in the same transaction as the replacement snapshot, before admission can
	// reopen, so a historical prepared play cannot acquire the new source.
	if _, err := tx.Exec(ctx, `UPDATE play_sessions SET state='Expired',
		stopped_at=COALESCE(stopped_at,clock_timestamp()),expires_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE item_id=$1 AND media_source_id=$2 AND state IN ('Prepared','Playing','Paused')`, operation.ItemID, operation.MediaSourceID); err != nil {
		return err
	}
	result, err := encodeMediaEditResult(journal)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE media_operations SET journal=$2,result_summary=$3 WHERE id=$1`, operation.ID, result.Journal, result.Summary); err != nil {
		return err
	}
	// Preserve IDs, metadata overrides, sidecar indexes and user state. Source
	// bound intro/owned-subtitle projections become stale by their own stamps.
	return recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: operation.ItemID, LibraryID: operation.LibraryID, ParentID: current.ParentID})
}
