// Package activity records committed administrative facts without storing
// request values, credentials, filesystem paths, or arbitrary diagnostic text.
package activity

import (
	"errors"
	"time"
)

const (
	DefaultPageLimit   = 50
	MaxPageLimit       = 200
	MaxStartIndex      = 2147483647
	MaxIdentifierBytes = 256
	MaxChangedFields   = 40
	PruneBatchSize     = 1000
	DefaultRetention   = 30 * 24 * time.Hour
)

var (
	ErrInvalidInput = errors.New("invalid activity input")
	ErrUnavailable  = errors.New("activity storage is unavailable")
)

type Action string

const (
	ActionUserCreated              Action = "user.created"
	ActionUserUpdated              Action = "user.updated"
	ActionUserPasswordReset        Action = "user.password_reset"
	ActionSessionLogin             Action = "session.login"
	ActionSessionRevoked           Action = "session.revoked"
	ActionApplicationKeyCreated    Action = "application_key.created"
	ActionApplicationKeyRevealed   Action = "application_key.revealed"
	ActionApplicationKeyRevoked    Action = "application_key.revoked"
	ActionDeviceUpdated            Action = "device.updated"
	ActionDeviceRemoved            Action = "device.removed"
	ActionLibraryCreated           Action = "library.created"
	ActionLibraryRemoved           Action = "library.removed"
	ActionScanRequested            Action = "scan.requested"
	ActionScanCancelRequested      Action = "scan.cancel_requested"
	ActionScanFinished             Action = "scan.finished"
	ActionMetadataUpdated          Action = "metadata.updated"
	ActionSettingsUpdated          Action = "settings.updated"
	ActionTaskAdmitted             Action = "task.admitted"
	ActionTaskCancelRequested      Action = "task.cancel_requested"
	ActionTaskFinished             Action = "task.finished"
	ActionTaskScheduleUpdated      Action = "task.schedule_updated"
	ActionBackupRequested          Action = "backup.requested"
	ActionBackupCancelRequested    Action = "backup.cancel_requested"
	ActionBackupFinished           Action = "backup.finished"
	ActionBackupImported           Action = "backup.imported"
	ActionBackupDeleteRequested    Action = "backup.delete_requested"
	ActionBackupDeleted            Action = "backup.deleted"
	ActionBackupDownloaded         Action = "backup.downloaded"
	ActionRestoreRequested         Action = "restore.requested"
	ActionRestorePlanned           Action = "restore.planned"
	ActionRestoreApplyRequested    Action = "restore.apply_requested"
	ActionRestoreApplied           Action = "restore.applied"
	ActionRestoreRollbackRequested Action = "restore.rollback_requested"
	ActionRestoreCancelRequested   Action = "restore.cancel_requested"
	ActionRestoreFailed            Action = "restore.failed"
)

type Severity string

const (
	SeverityDebug   Severity = "Debug"
	SeverityInfo    Severity = "Info"
	SeverityWarning Severity = "Warn"
	SeverityError   Severity = "Error"
	SeverityFatal   Severity = "Fatal"
)

type Source string

const (
	SourceNative Source = "native"
	SourceEmby   Source = "emby"
	SourceSystem Source = "system"
)

type ActorKind string

const (
	ActorUser           ActorKind = "user"
	ActorApplicationKey ActorKind = "application_key"
	ActorSystem         ActorKind = "system"
)

// Actor identifiers are existing database identities, never access tokens,
// hashes, passwords, client-reported device identifiers, or display names.
// ID identifies the user or application-key sidecar. CredentialID identifies
// the existing session row. A system actor has neither identifier.
type Actor struct {
	Kind         ActorKind
	ID           string
	CredentialID string
}

type ResourceKind string

const (
	ResourceUser           ResourceKind = "user"
	ResourceSession        ResourceKind = "session"
	ResourceApplicationKey ResourceKind = "application_key"
	ResourceDevice         ResourceKind = "device"
	ResourceLibrary        ResourceKind = "library"
	ResourceScan           ResourceKind = "scan"
	ResourceItem           ResourceKind = "item"
	ResourceSettings       ResourceKind = "settings"
	ResourceTask           ResourceKind = "task"
	ResourceTaskRun        ResourceKind = "task_run"
	ResourceBackup         ResourceKind = "backup"
	ResourceRestore        ResourceKind = "restore"
)

// Resource.ID is a persisted public resource identifier. Bounded historical
// identifiers remain supported; an identifier is not an arbitrary text field.
type Resource struct {
	Kind ResourceKind
	ID   string
}

type State string

const (
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateCancelled   State = "cancelled"
	StateInterrupted State = "interrupted"
)

type Field string

const (
	FieldName                           Field = "Name"
	FieldIsAdministrator                Field = "IsAdministrator"
	FieldIsDisabled                     Field = "IsDisabled"
	FieldPolicy                         Field = "Policy"
	FieldEnableAllFolders               Field = "EnableAllFolders"
	FieldEnabledFolders                 Field = "EnabledFolders"
	FieldEnableMediaPlayback            Field = "EnableMediaPlayback"
	FieldEnablePlaybackRemuxing         Field = "EnablePlaybackRemuxing"
	FieldEnableAudioPlaybackTranscoding Field = "EnableAudioPlaybackTranscoding"
	FieldEnableVideoPlaybackTranscoding Field = "EnableVideoPlaybackTranscoding"
	FieldCustomName                     Field = "CustomName"
	FieldServerName                     Field = "ServerName"
	FieldServerNameMode                 Field = "ServerNameMode"
	FieldMaxBitrate                     Field = "MaxBitrate"
	FieldMaxWidth                       Field = "MaxWidth"
	FieldMaxHeight                      Field = "MaxHeight"
	FieldMaxAudioChannels               Field = "MaxAudioChannels"
	FieldTranscodingMaxWidth            Field = "TranscodingMaxWidth"
	FieldSortName                       Field = "SortName"
	FieldOverview                       Field = "Overview"
	FieldOriginalTitle                  Field = "OriginalTitle"
	FieldOfficialRating                 Field = "OfficialRating"
	FieldProductionYear                 Field = "ProductionYear"
	FieldIndexNumber                    Field = "IndexNumber"
	FieldParentIndexNumber              Field = "ParentIndexNumber"
	FieldPremiereDate                   Field = "PremiereDate"
	FieldCommunityRating                Field = "CommunityRating"
	FieldProviderIDs                    Field = "ProviderIds"
	FieldGenres                         Field = "Genres"
	FieldTags                           Field = "Tags"
	FieldStudios                        Field = "Studios"
	FieldPeople                         Field = "People"
	FieldLockedFields                   Field = "LockedFields"
	FieldOverrides                      Field = "Overrides"
	FieldTriggers                       Field = "Triggers"
	FieldScheduleTimezone               Field = "ScheduleTimezone"
)

// Event contains facts selected by the owning repository. RequestID is an
// optional server-generated correlation identifier, not a client request body
// value. Revision and Count are nonnegative numerical facts; their zero values
// mean no revision and no affected resources respectively. State is required
// for terminal scan, task, backup, and restore facts. ChangedFields contains
// names alone, never before/after values; backup and restore events have none.
// The writer does not authorize the actor.
type Event struct {
	Action        Action
	Severity      Severity
	Source        Source
	Actor         Actor
	Resource      Resource
	RequestID     string
	Revision      int64
	Count         int64
	State         State
	ChangedFields []Field
}

// Entry projects only explicit stored facts. ActorName is optional current-user
// enrichment, not an immutable historical name. Name and Overview are fixed
// application templates, independent of user-supplied text.
type Entry struct {
	ID   int64
	Date time.Time
	Event
	ActorName string
	Name      string
	Overview  string
}

type QueryOptions struct {
	StartIndex int
	Limit      int
	MinDate    *time.Time
	Severity   Severity
	Action     Action
	ActorID    string
}

type Page struct {
	Items            []Entry
	TotalRecordCount int64
	StartIndex       int
	Limit            int
}
