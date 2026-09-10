package activity

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor must be the caller's existing transaction, never a pool. Record
// neither starts nor completes a transaction, and its failure must be returned
// to the transaction owner. The final fresh authorization check follows it.
type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// OwnedExecutor preserves the catalog owner's protected transaction context
// without exposing connection, cancellation, or transaction-control methods.
type OwnedExecutor interface {
	Exec(string, ...any) (pgconn.CommandTag, error)
}

const insertEntry = `INSERT INTO activity_entries
	(action,severity,source,actor_kind,actor_id,actor_credential_id,
	resource_kind,resource_id,request_id,revision,affected_count,state,changed_fields)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`

func Record(ctx context.Context, tx Executor, event Event) error {
	if tx == nil {
		return ErrUnavailable
	}
	args, err := eventArguments(event)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertEntry, args...); err != nil {
		return fmt.Errorf("record activity: %w", err)
	}
	return nil
}

func RecordOwned(tx OwnedExecutor, event Event) error {
	if tx == nil {
		return ErrUnavailable
	}
	args, err := eventArguments(event)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(insertEntry, args...); err != nil {
		return fmt.Errorf("record owned activity: %w", err)
	}
	return nil
}

func eventArguments(event Event) ([]any, error) {
	if event.Severity == "" {
		event.Severity = SeverityInfo
	}
	if !validAction(event.Action) || !validSeverity(event.Severity) ||
		(event.Source != SourceNative && event.Source != SourceEmby && event.Source != SourceSystem) ||
		!validActor(event.Actor) || event.Resource.Kind != actionResource(event.Action) ||
		!validIdentifier(event.Resource.ID, false) || !validIdentifier(event.RequestID, true) ||
		event.Revision < 0 || event.Count < 0 || len(event.ChangedFields) > MaxChangedFields {
		return nil, ErrInvalidInput
	}
	switch event.Action {
	case ActionScanFinished, ActionTaskFinished, ActionBackupFinished:
		if event.State != StateCompleted && event.State != StateFailed && event.State != StateCancelled && event.State != StateInterrupted {
			return nil, ErrInvalidInput
		}
	case ActionRestoreApplied:
		if event.State != StateCompleted {
			return nil, ErrInvalidInput
		}
	case ActionRestoreFailed:
		if event.State != StateFailed {
			return nil, ErrInvalidInput
		}
	default:
		if event.State != "" {
			return nil, ErrInvalidInput
		}
	}
	fields := make([]string, 0, len(event.ChangedFields))
	for _, field := range event.ChangedFields {
		if !fieldAllowed(event.Action, field) || slices.Contains(fields, string(field)) {
			return nil, ErrInvalidInput
		}
		fields = append(fields, string(field))
	}
	slices.Sort(fields)
	return []any{string(event.Action), string(event.Severity), string(event.Source), string(event.Actor.Kind),
		event.Actor.ID, event.Actor.CredentialID, string(event.Resource.Kind), event.Resource.ID,
		event.RequestID, event.Revision, event.Count, string(event.State), fields}, nil
}

func validIdentifier(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	if len(value) > MaxIdentifierBytes {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func validActor(actor Actor) bool {
	switch actor.Kind {
	case ActorSystem:
		return actor.ID == "" && actor.CredentialID == ""
	case ActorUser, ActorApplicationKey:
		return validIdentifier(actor.ID, false) && validIdentifier(actor.CredentialID, true)
	default:
		return false
	}
}

func validSeverity(value Severity) bool {
	return value == SeverityDebug || value == SeverityInfo || value == SeverityWarning || value == SeverityError || value == SeverityFatal
}

func validAction(value Action) bool { return actionResource(value) != "" }

func actionResource(action Action) ResourceKind {
	switch action {
	case ActionUserCreated, ActionUserUpdated, ActionUserPasswordReset:
		return ResourceUser
	case ActionSessionLogin, ActionSessionRevoked:
		return ResourceSession
	case ActionApplicationKeyCreated, ActionApplicationKeyRevealed, ActionApplicationKeyRevoked:
		return ResourceApplicationKey
	case ActionDeviceUpdated, ActionDeviceRemoved:
		return ResourceDevice
	case ActionLibraryCreated, ActionLibraryRemoved:
		return ResourceLibrary
	case ActionScanRequested, ActionScanCancelRequested, ActionScanFinished:
		return ResourceScan
	case ActionMetadataUpdated:
		return ResourceItem
	case ActionSettingsUpdated:
		return ResourceSettings
	case ActionTaskAdmitted, ActionTaskCancelRequested, ActionTaskFinished:
		return ResourceTaskRun
	case ActionTaskScheduleUpdated:
		return ResourceTask
	case ActionBackupRequested, ActionBackupCancelRequested, ActionBackupFinished, ActionBackupImported,
		ActionBackupDeleteRequested, ActionBackupDeleted, ActionBackupDownloaded:
		return ResourceBackup
	case ActionRestoreRequested, ActionRestorePlanned, ActionRestoreApplyRequested, ActionRestoreApplied,
		ActionRestoreRollbackRequested, ActionRestoreCancelRequested, ActionRestoreFailed:
		return ResourceRestore
	default:
		return ""
	}
}

func fieldAllowed(action Action, field Field) bool {
	switch action {
	case ActionUserCreated, ActionUserUpdated:
		switch field {
		case FieldName, FieldIsAdministrator, FieldIsDisabled, FieldPolicy, FieldEnableAllFolders,
			FieldEnabledFolders, FieldEnableMediaPlayback, FieldEnablePlaybackRemuxing,
			FieldEnableAudioPlaybackTranscoding, FieldEnableVideoPlaybackTranscoding:
			return true
		}
	case ActionDeviceUpdated:
		return field == FieldCustomName
	case ActionMetadataUpdated:
		switch field {
		case FieldName, FieldSortName, FieldOverview, FieldOriginalTitle, FieldOfficialRating,
			FieldProductionYear, FieldIndexNumber, FieldParentIndexNumber, FieldPremiereDate,
			FieldCommunityRating, FieldProviderIDs, FieldGenres, FieldTags, FieldStudios,
			FieldPeople, FieldLockedFields, FieldOverrides:
			return true
		}
	case ActionSettingsUpdated:
		switch field {
		case FieldServerName, FieldServerNameMode, FieldMaxBitrate, FieldMaxWidth,
			FieldMaxHeight, FieldMaxAudioChannels, FieldTranscodingMaxWidth:
			return true
		}
	case ActionTaskScheduleUpdated:
		return field == FieldTriggers || field == FieldScheduleTimezone
	}
	return false
}

// PruneExpired deletes at most one bounded batch using an independent pool
// transaction. It never takes catalog ownership or locks business records.
// Retention uses the database clock. Callers may schedule another batch later;
// this method deliberately does not promise a hard total-row ceiling.
func PruneExpired(ctx context.Context, pool *pgxpool.Pool, retention time.Duration) (int64, error) {
	if pool == nil {
		return 0, ErrUnavailable
	}
	if retention < time.Microsecond {
		return 0, ErrInvalidInput
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(bounded, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin activity retention: %w", err)
	}
	defer func() {
		cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		_ = tx.Rollback(cleanup)
	}()
	// Round the duration upward so a fractional-microsecond retention never
	// deletes a record sooner than the requested duration permits.
	microseconds := retention.Microseconds()
	if retention%time.Microsecond != 0 {
		microseconds++
	}
	tag, err := tx.Exec(bounded, `DELETE FROM activity_entries WHERE id IN (
		SELECT id FROM activity_entries
		WHERE created_at < clock_timestamp() - ($1::bigint * interval '1 microsecond')
		ORDER BY created_at,id LIMIT 1000 FOR UPDATE SKIP LOCKED)`, microseconds)
	if err != nil {
		return 0, fmt.Errorf("prune activity entries: %w", err)
	}
	if err := tx.Commit(bounded); err != nil {
		return 0, fmt.Errorf("commit activity retention: %w", err)
	}
	return tag.RowsAffected(), nil
}
