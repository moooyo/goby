// Package backupstore owns encrypted backup objects and their durable job catalog.
// It does not encrypt, authenticate, inspect PostgreSQL, or authorize callers.
package backupstore

import (
	"errors"
	"time"
)

const (
	DefaultDirectory      = "/var/lib/goby/backups"
	DefaultMaxObjectBytes = int64(8 << 30)
	DefaultMaxTotalBytes  = int64(32 << 30)
	DefaultMaxObjects     = 128
	DefaultMinFreeBytes   = int64(512 << 20)
	MaxReaders            = 8
	MaxScratchFiles       = 8
)

var (
	ErrInvalid     = errors.New("invalid backup store request")
	ErrUnavailable = errors.New("backup store unavailable")
	ErrNotFound    = errors.New("backup object not found")
	ErrNotReady    = errors.New("backup object is not ready")
	ErrBusy        = errors.New("backup store busy")
	ErrQuota       = errors.New("backup store quota exceeded")
	ErrConflict    = errors.New("backup object changed")
	ErrIntegrity   = errors.New("backup object integrity check failed")
)

type Config struct {
	Directory      string
	MaxObjectBytes int64
	MaxTotalBytes  int64
	MaxObjects     int
	MinFreeBytes   int64
}

func (c Config) WithDefaults() Config {
	if c.Directory == "" {
		c.Directory = DefaultDirectory
	}
	if c.MaxObjectBytes == 0 {
		c.MaxObjectBytes = DefaultMaxObjectBytes
	}
	if c.MaxTotalBytes == 0 {
		c.MaxTotalBytes = DefaultMaxTotalBytes
	}
	if c.MaxObjects == 0 {
		c.MaxObjects = DefaultMaxObjects
	}
	if c.MinFreeBytes == 0 {
		c.MinFreeBytes = DefaultMinFreeBytes
	}
	return c
}

type Kind string

const (
	KindGenerated Kind = "generated"
	KindImported  Kind = "imported"
)

type State string

const (
	StateWriting     State = "writing"
	StateReady       State = "ready"
	StateFailed      State = "failed"
	StateCancelled   State = "cancelled"
	StateInterrupted State = "interrupted"
)

// ErrorCode contains only fixed classifications, never system errors or input.
type ErrorCode string

const (
	CodeNone         ErrorCode = ""
	CodeCancelled    ErrorCode = "cancelled"
	CodeInterrupted  ErrorCode = "interrupted"
	CodeStorage      ErrorCode = "storage_unavailable"
	CodeQuota        ErrorCode = "quota_exceeded"
	CodeIntegrity    ErrorCode = "integrity_failed"
	CodeGeneration   ErrorCode = "generation_failed"
	CodeImport       ErrorCode = "import_failed"
	CodeVerification ErrorCode = "verification_failed"
)

type TableCount struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// SourceSummary is a bounded whitelist supplied only after full archive and
// PostgreSQL inspection. It must never contain credentials or deployment paths.
type SourceSummary struct {
	ArchiveID          string       `json:"archiveId"`
	FormatVersion      int          `json:"formatVersion"`
	ApplicationVersion string       `json:"applicationVersion"`
	SchemaVersion      int          `json:"schemaVersion"`
	ServerID           string       `json:"serverId"`
	CreatedAt          time.Time    `json:"createdAt"`
	Tables             []TableCount `json:"tables"`
}

// Metadata is private storage metadata. HTTP adapters must omit SessionID and
// make their own explicit decisions about exposing actor and source identifiers.
type Metadata struct {
	ID        string         `json:"id"`
	Kind      Kind           `json:"kind"`
	State     State          `json:"state"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Size      int64          `json:"size"`
	Digest    string         `json:"digest,omitempty"`
	Verified  bool           `json:"verified"`
	Summary   *SourceSummary `json:"summary,omitempty"`
	ErrorCode ErrorCode      `json:"errorCode,omitempty"`
	CreatorID string         `json:"creatorId,omitempty"`
	SessionID string         `json:"sessionId,omitempty"`
}

type BeginOptions struct {
	Kind      Kind
	CreatorID string
	SessionID string
}

type Page struct {
	Items            []Metadata
	TotalRecordCount int
	StartIndex       int
	Limit            int
}

// Prepared binds a durable object to its owning writer. Exported fields are
// useful audit descriptors; modifying them invalidates publication.
type Prepared struct {
	ID     string
	Size   int64
	Digest string
	writer *Writer
}

type Status struct {
	Healthy      bool
	Closed       bool
	Objects      int
	Bytes        int64
	Readers      int
	Writers      int
	ScratchFiles int
	ScratchBytes int64
}
