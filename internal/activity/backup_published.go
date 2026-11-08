package activity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func backupPublishedHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

// BackupPublicationFingerprint binds the observable completion to one exact
// object rather than the recovery operation which happened to publish it.
func BackupPublicationFingerprint(id, digest string, size int64) (string, error) {
	if !backupPublishedHex(id, 32) || !backupPublishedHex(digest, 64) || size < 1 {
		return "", ErrInvalidInput
	}
	value := sha256.Sum256([]byte("goby/backup-published/v1\x00" + id + "\x00" + digest + "\x00" + strconv.FormatInt(size, 10)))
	return hex.EncodeToString(value[:]), nil
}

// RecordBackupPublished requires the caller's existing transaction and proof
// that the store has already published the exact object. It never grants file
// publication authority. A database advisory lock serializes all cooperating
// retries by backup id, including after an uncertain COMMIT. An existing receipt
// must match exactly; retry never creates a second activity/outbox source id.
func RecordBackupPublished(ctx context.Context, tx pgx.Tx, id, digest string, size int64) error {
	if tx == nil {
		return ErrUnavailable
	}
	fingerprint, err := BackupPublicationFingerprint(id, digest, size)
	if err != nil {
		return err
	}
	lockHash := sha256.Sum256([]byte("goby/backup-published/lock/v1\x00" + id))
	lockID := int64(binary.BigEndian.Uint64(lockHash[:8]))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		return ErrUnavailable
	}
	var receiptDigest, receiptFingerprint string
	var receiptSize int64
	err = tx.QueryRow(ctx, `SELECT sha256,size_bytes,observation_fingerprint FROM backup_publication_receipts WHERE backup_id=$1`, id).Scan(&receiptDigest, &receiptSize, &receiptFingerprint)
	if err == nil {
		if receiptDigest != digest || receiptSize != size || receiptFingerprint != fingerprint {
			return ErrInvalidInput
		}
		return ctx.Err()
	}
	if err != pgx.ErrNoRows {
		return ErrUnavailable
	}
	var count int64
	var exact bool
	var activityID *int64
	if err = tx.QueryRow(ctx, `SELECT count(*),max(id),COALESCE(bool_and(resource_kind='backup' AND source='system' AND actor_kind='system'
		AND actor_id='' AND actor_credential_id='' AND revision=0 AND previous_revision=0 AND affected_count=0
		AND state='completed' AND cardinality(changed_fields)=0 AND observation_fingerprint=$3),true)
		FROM activity_entries WHERE action=$1 AND resource_id=$2`, string(ActionBackupPublished), id, fingerprint).Scan(&count, &activityID, &exact); err != nil {
		return ErrUnavailable
	}
	if count > 1 || !exact {
		return ErrInvalidInput
	}
	if count == 0 {
		args, err := eventArguments(Event{Action: ActionBackupPublished, Source: SourceSystem, Actor: Actor{Kind: ActorSystem}, Resource: Resource{Kind: ResourceBackup, ID: id}, State: StateCompleted, ObservationFingerprint: fingerprint})
		if err != nil {
			return err
		}
		var inserted int64
		if err := tx.QueryRow(ctx, insertEntry+" RETURNING id", args...).Scan(&inserted); err != nil {
			return ErrUnavailable
		}
		activityID = &inserted
	}
	if activityID == nil {
		return ErrInvalidInput
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backup_publication_receipts(backup_id,sha256,size_bytes,observation_fingerprint,activity_id) VALUES($1,$2,$3,$4,$5)`, id, digest, size, fingerprint, *activityID); err != nil {
		return ErrUnavailable
	}
	return ctx.Err()
}
