package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
	"github.com/moooyo/goby/internal/systemevents"
)

const TaskNFORecoveryKey = "metadata.regenerate_nfo"

// A saved copy is bound to the physical source, not the probe JSON version.
// Work claims still use the exact import generation; ctime is bound separately.
const nfoRecoveryPhysicalSourceKeySQL = `jsonb_build_array(i.root_id,i.relative_path,i.path,i.type,i.is_folder,i.parent_id,
	i.file_identity,i.file_size,extract(epoch FROM i.modified_at),r.binding_revision)::text`

var (
	ErrNFORecoveryNoSource      = errors.New("confirmed NFO damage has no trusted recovery document")
	ErrNFORecoveryWriteDisabled = errors.New("confirmed NFO damage is outside authorized sidecar writes")
	ErrNFORecoveryIncomplete    = errors.New("NFO regeneration remains pending after a failed or unverified write")

	errMetadataIngestNFOHandedOff  = errors.New("confirmed NFO damage was handed off for regeneration")
	errNFORecoveryDirectoryChanged = errors.New("NFO recovery directory generation changed")
)

// The catalog source key adds the item, scan ancestry, and root binding to
// these storage facts. Neither a file name nor a document hash is an identity.
type nfoRecoveryBinding struct {
	directory, root, path string
	changeNS              int64
}

type nfoRecoveryEvidence struct {
	nfoRecoveryBinding
	version, reason string
}

func (binding nfoRecoveryBinding) valid() bool {
	return binding.directory != "" && binding.root != "" && binding.path != "" && len(binding.path) <= 4096 && binding.changeNS >= 0
}

func selectedNFOFile(proof nfoIngestProof) os.FileInfo {
	if len(proof.entries) == 0 {
		return nil
	}
	return proof.entries[len(proof.entries)-1].info
}

// Bracket the sidecar read and the directory listing with the same generation.
// Without that bracket a sibling could disappear between the read and listing.
func observeNFORecoveryCandidate(ctx context.Context, dir *os.Root, directory string, candidates []string,
	path, itemType string, folder bool) (localNFOObservation, nfoIngestProof, error) {
	before, beforeErr := dir.Stat(".")
	observation, proof, err := observeMetadataIngestNFO(ctx, dir, directory, candidates)
	if err != nil || beforeErr != nil || media.FileChangeTime(before) <= 0 ||
		!primaryNFORecoveryCandidate(path, folder, itemType, observation.path) {
		return observation, proof, err
	}
	unique, err := uniqueNFORecoveryCandidate(ctx, dir, path, observation.path, folder)
	if err != nil {
		// An ordinary listing error revokes the backup proof, not the import.
		var retirement *storageObservationRetirementFailure
		if errors.As(err, &retirement) {
			return observation, proof, err
		}
		return observation, proof, localNFOFatalReadError(ctx, err)
	}
	if unique {
		after, err := dir.Stat(".")
		if err == nil && sameSubtitleDirectoryInfo(before, after) {
			proof.recoveryDirectory = before
		}
	}
	return observation, proof, ctx.Err()
}

func (proof nfoIngestProof) verifyRecoveryDirectory(current os.FileInfo) error {
	if proof.recoveryDirectory != nil && !sameSubtitleDirectoryInfo(proof.recoveryDirectory, current) {
		return errNFORecoveryDirectoryChanged
	}
	return nil
}

func nfoRecoveryVersion(observation localNFOObservation, proof nfoIngestProof) string {
	info := selectedNFOFile(proof)
	if observation.path == "" || info == nil || int64(len(observation.data)) != info.Size() {
		return ""
	}
	id, changed := fileIdentity(info), media.FileChangeTime(info)
	if id == "" || changed <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d:%d:%x", id, info.Size(), info.ModTime().UnixNano(), changed, sha256.Sum256(observation.data))
}

// A failure to reproduce the complete read remains uncertain. In particular,
// a preferred candidate appearing while the fallback was read is not damage.
func confirmNFORecovery(ctx context.Context, dir *os.Root, directory string, candidates []string,
	observation localNFOObservation, proof nfoIngestProof) (bool, error) {
	second, secondProof, err := observeMetadataIngestNFO(ctx, dir, directory, candidates)
	if err != nil {
		return false, err
	}
	if second.invalid || second.path != observation.path || !bytes.Equal(second.data, observation.data) ||
		!sameLocalNFOFile(selectedNFOFile(proof), selectedNFOFile(secondProof)) {
		return false, nil
	}
	if err := proof.verifyAt(ctx, dir); err != nil {
		return false, localNFOFatalReadError(ctx, err)
	}
	return true, nil
}

const sameNFORecoveryDamageSQL = `(w.directory_identity,w.root_identity,w.relative_path,w.media_change_ns,w.version)
	IS NOT DISTINCT FROM (EXCLUDED.directory_identity,EXCLUDED.root_identity,EXCLUDED.relative_path,EXCLUDED.media_change_ns,EXCLUDED.version)`

func recordNFORecovery(ctx context.Context, tx pgx.Tx, target metadataIngestTarget, evidence nfoRecoveryEvidence) error {
	// A new scan's import key can reauthorize the same damaged file. A probe
	// result or a repeated observation must not grant another write budget.
	var signal bool
	err := tx.QueryRow(ctx, `INSERT INTO local_nfo_recovery_work AS w
		(item_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,version,reason)
		SELECT item_id,source_key,$3,$4,$5,$6,$7,$8 FROM local_metadata_ingest WHERE item_id=$1 AND scan_job_id=$2 AND import_nfo
		ON CONFLICT(item_id) DO UPDATE SET source_key=EXCLUDED.source_key,directory_identity=EXCLUDED.directory_identity,
		root_identity=EXCLUDED.root_identity,relative_path=EXCLUDED.relative_path,media_change_ns=EXCLUDED.media_change_ns,
		version=EXCLUDED.version,reason=EXCLUDED.reason,
		confirmed_at=CASE WHEN `+sameNFORecoveryDamageSQL+` THEN w.confirmed_at ELSE EXCLUDED.confirmed_at END,
		attempts=CASE WHEN `+sameNFORecoveryDamageSQL+` THEN w.attempts ELSE 0 END,
		retry_at=CASE WHEN `+sameNFORecoveryDamageSQL+` THEN w.retry_at ELSE '-infinity' END,
		status=CASE WHEN `+sameNFORecoveryDamageSQL+` THEN w.status ELSE 'pending' END
		WHERE w.source_key<>EXCLUDED.source_key OR NOT (`+sameNFORecoveryDamageSQL+`)
		RETURNING status='pending' AND attempts=0`, target.id, target.jobID, evidence.directory, evidence.root,
		evidence.path, evidence.changeNS, evidence.version, evidence.reason).Scan(&signal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || !signal {
		return err
	}
	return systemevents.Record(taskSourcePublicationTx{ctx: ctx, tx: tx}.Exec, systemevents.NFORecoveryRequested)
}

// Only the primary sidecar name can be eligible for recovery. Movie fallbacks
// are excluded even when imported; the directory must prove uniqueness separately.
func primaryNFORecoveryCandidate(path string, folder bool, itemType, observed string) bool {
	var candidates []string
	if folder {
		if itemType != "Series" && itemType != "Season" {
			return false
		}
		candidates, _ = folderNFOCandidate(path, itemType)
	} else {
		candidates, _ = fileNFOCandidates(path, itemType)
	}
	return len(candidates) != 0 && filepath.ToSlash(candidates[0]) == observed &&
		!(itemType == "Movie" && strings.EqualFold(filepath.Base(observed), "movie.nfo"))
}

// This is deliberately stricter than importing external single-root NFOs. An
// accepted display override or differently numbered file is not a backup for
// regenerating an episode. A paired name requires the complete bound document.
func trustedNFORecoveryDocument(path string, folder bool, itemType string, request scanMetadataImport, local localMetadata, data []byte) bool {
	if local.value == nil || !primaryNFORecoveryCandidate(path, folder, itemType, local.path) {
		return false
	}
	v := local.value
	switch itemType {
	case "Episode":
		if _, paired := episodeFilePair(path); paired {
			return local.pair != nil
		}
		return request.parentDefined && request.index > 0 && (v.IndexNumber == nil || *v.IndexNumber == request.index) &&
			(v.ParentIndexNumber == nil || *v.ParentIndexNumber == request.parentIndex)
	case "Season":
		season, physical := tvSeasonNumber(filepath.Base(path))
		return physical && season == request.index && (v.IndexNumber == nil || *v.IndexNumber == season) &&
			metadata.NFOSeasonMatchesRecovery(data, season)
	default:
		return true
	}
}

func finishNFORecoveryObservation(ctx context.Context, tx pgx.Tx, row rootBindingRow, itemID, jobID string,
	binding nfoRecoveryBinding, data []byte, digest string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM local_nfo_recovery_work WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	if !binding.valid() || len(data) == 0 || digest == "" {
		_, err := tx.Exec(ctx, `DELETE FROM local_nfo_recovery_sources WHERE item_id=$1`, itemID)
		return err
	}
	exclusive, err := ownsNFORecoveryDirectory(ctx, tx, row.root, filepath.Dir(filepath.FromSlash(binding.path)))
	if err != nil || !exclusive {
		return err
	}
	// Only newly accepted original bytes are retained, not an export of edited
	// or provider metadata. Pending media operations can hide a competing leaf.
	_, err = tx.Exec(ctx, `INSERT INTO local_nfo_recovery_sources
		(item_id,scan_job_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,document,document_hash)
		SELECT i.id,$2,`+nfoRecoveryPhysicalSourceKeySQL+`,$3,$4,$5,$6,$7,$8 FROM items i
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND i.root_id=$9 AND r.binding_revision=$10 AND i.local_metadata_hash=$8 AND i.local_metadata_path=$5
		AND `+nfoRecoveryStableRootSQL+`
		ON CONFLICT(item_id) DO UPDATE SET scan_job_id=EXCLUDED.scan_job_id,source_key=EXCLUDED.source_key,
		directory_identity=EXCLUDED.directory_identity,root_identity=EXCLUDED.root_identity,relative_path=EXCLUDED.relative_path,
		media_change_ns=EXCLUDED.media_change_ns,document=EXCLUDED.document,document_hash=EXCLUDED.document_hash
		WHERE (local_nfo_recovery_sources.source_key,local_nfo_recovery_sources.directory_identity,
		local_nfo_recovery_sources.root_identity,local_nfo_recovery_sources.relative_path,local_nfo_recovery_sources.media_change_ns,
		local_nfo_recovery_sources.document_hash) IS DISTINCT FROM (EXCLUDED.source_key,EXCLUDED.directory_identity,
		EXCLUDED.root_identity,EXCLUDED.relative_path,EXCLUDED.media_change_ns,EXCLUDED.document_hash)
		OR NOT EXISTS(SELECT 1 FROM scan_jobs j WHERE j.id=local_nfo_recovery_sources.scan_job_id AND j.status='Completed')`,
		itemID, jobID, binding.directory, binding.root, binding.path, binding.changeNS, data, digest, row.root.id, row.revision)
	return err
}
