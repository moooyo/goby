package recovery

import "time"

// The administrator projection is an allowlist. Deployment URLs, paths,
// authentication credential IDs, master keys, passphrases, and private table
// fingerprints never appear in these types.
type BackupView struct {
	Id        string
	Kind      string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	SizeBytes string
	SHA256    string
	Verified  bool
	ErrorCode string
	Source    *SourceView
}

type SourceView struct {
	ServerId      string
	ServerName    string
	GobyVersion   string
	SchemaVersion string
	CreatedAt     time.Time
	Tables        []TableView
}

type TableView struct {
	Name string
	Rows string
}

type OperationView struct {
	Id                 string
	RequestId          string
	Revision           string
	Kind               string
	State              string
	Phase              string
	BackupId           string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ErrorCode          string
	Source             *SourceView
	RestoreDefaults    bool
	ReplaceRollback    bool
	CanCancel          bool
	CanApply           bool
	GenerationRevision string
}

type BackupPage struct {
	Items            []BackupView
	TotalRecordCount int
	StartIndex       int
	Limit            int
}

type OperationPage struct {
	Items            []OperationView
	TotalRecordCount int
	StartIndex       int
	Limit            int
}

type StatusView struct {
	Available                bool
	UnavailableReason        string
	RestoreAvailable         bool
	RestoreUnavailableReason string
	Busy                     bool
	ActiveOperationId        string
	GenerationRevision       string
	Limits                   LimitsView
	Storage                  StorageView
	Rollback                 RollbackView
}

type LimitsView struct {
	MaxBackupBytes     string
	MaxStoredBytes     string
	MaxBackups         int
	MinPassphraseBytes int
	MaxPassphraseBytes int
}

type StorageView struct {
	Bytes   string
	Objects int
}

type RollbackView struct {
	Available         bool
	MustReplace       bool
	CreatedAt         *time.Time
	ServerName        string
	Generation        string
	UnavailableReason string
}

// A RequestId is a caller-generated 32-character lowercase hexadecimal value.
// It identifies an admission attempt, not a storage filename. Repeating the
// same admitted attempt never replaces its passphrase or starts another job.
type CreateRequest struct {
	RequestId  string
	Passphrase []byte `json:"-"`
}

type DeleteRequest struct {
	RequestId string
	SHA256    string
}

type PlanRequest struct {
	RequestId          string
	BackupId           string
	SHA256             string
	Passphrase         []byte `json:"-"`
	RestoreDefaults    bool
	ReplaceRollback    bool
	GenerationRevision string
}

type ApplyRequest struct {
	Revision           string
	GenerationRevision string
}

type RollbackRequest struct {
	RequestId          string
	GenerationRevision string
}
