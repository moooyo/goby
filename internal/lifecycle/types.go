// Package lifecycle fences local deployment processes and publishes immutable
// recovery generations. It does not decide whether restored business data is
// healthy, and it never rolls an activated generation back automatically.
package lifecycle

import "errors"

const (
	DefaultDirectory = "/var/lib/goby/recovery"
	MaxConfigBytes   = 1 << 20
	MaxGenerations   = 4096
)

var (
	ErrInvalid          = errors.New("invalid lifecycle request")
	ErrBusy             = errors.New("deployment lifecycle is locked")
	ErrUnavailable      = errors.New("deployment lifecycle unavailable")
	ErrConflict         = errors.New("deployment lifecycle conflict")
	ErrNotFound         = errors.New("deployment generation not found")
	ErrIncomplete       = errors.New("deployment generation is incomplete")
	ErrRecoveryRequired = errors.New("deployment lifecycle recovery required")
)

type DatabaseSlot string

const (
	DatabasePrimary  DatabaseSlot = "primary"
	DatabaseRecovery DatabaseSlot = "recovery"
)

type MasterSource string

const (
	MasterDefault    MasterSource = "default"
	MasterGeneration MasterSource = "generation"
)

// FileDescriptor describes bytes, never an arbitrary filesystem path.
type FileDescriptor struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Generation struct {
	ID       string          `json:"id"`
	Complete bool            `json:"complete"`
	Config   FileDescriptor  `json:"config"`
	Master   *FileDescriptor `json:"master,omitempty"`
}

// GenerationFiles contains sensitive material. Callers must not log it or put
// its contents in lifecycle plans. The caller owns these returned byte slices.
type GenerationFiles struct {
	Generation Generation
	Config     []byte
	Master     []byte
}

// State revision zero selects deployment configuration and the primary
// database. Digest is a CAS token for the complete persisted manifest.
type State struct {
	DeploymentID string       `json:"deploymentId"`
	Revision     uint64       `json:"revision"`
	GenerationID string       `json:"generationId,omitempty"`
	DatabaseSlot DatabaseSlot `json:"databaseSlot"`
	Master       MasterSource `json:"master"`
	Digest       string       `json:"digest"`
}

type Candidate struct {
	GenerationID string
	DatabaseSlot DatabaseSlot
	Master       MasterSource
}

type PlanStatus string

const (
	PlanPrepared  PlanStatus = "prepared"
	PlanActivated PlanStatus = "activated"
)

// Plan is a durable local publication intention. Activated describes manifest
// publication, not application health or successful business verification.
type Plan struct {
	ID     string     `json:"id"`
	Before State      `json:"before"`
	After  State      `json:"after"`
	Status PlanStatus `json:"status"`
}
