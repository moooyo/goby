// Package recoverydb binds local recovery slots to database generations and
// conditionally empties only an unchanged, explicitly retained inactive copy.
// HTTP authorization and durable local transition intents belong to its caller.
package recoverydb

import (
	"errors"

	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/lifecycle"
)

// Config is supplied by the trusted local deployment coordinator. Each store
// has a fixed slot identity, independent of any marker read from a database.
type Config struct {
	Postgres     backuppg.Options
	DeploymentID string
	Slot         lifecycle.DatabaseSlot
}

const (
	MarkerKey      = "goby.recovery.binding.v1"
	MaxMarkerBytes = 2048
)

var (
	ErrInvalid     = errors.New("invalid recovery database request")
	ErrConflict    = errors.New("recovery database binding or retained data changed")
	ErrUnavailable = errors.New("recovery database operation unavailable")
	ErrLeaseLost   = errors.New("recovery database deployment lease unavailable")
)

// Marker is a database claim, not an authorization token. Its local deployment
// and generation must be independently bound to the protected control store.
type Marker struct {
	Version      int                    `json:"version"`
	DeploymentID string                 `json:"deploymentId"`
	GenerationID string                 `json:"generationId"`
	Slot         lifecycle.DatabaseSlot `json:"slot"`
}

// RawMarker distinguishes an absent row from every present value, including an
// invalid or foreign marker. Stamp compares these exact bytes before writing.
type RawMarker struct {
	Present bool   `json:"present"`
	Value   string `json:"value"`
}

// ReadResult distinguishes a genuinely empty preconfigured schema from an
// existing legacy server_settings table whose binding row is absent.
type ReadResult struct {
	TablePresent bool      `json:"tablePresent"`
	Raw          RawMarker `json:"raw"`
}

// Retained must be persisted only in the trusted local control store after an
// active generation has been drained and retired. An archive or HTTP request
// must never supply this destruction precondition.
type Retained struct {
	Marker    Marker                   `json:"marker"`
	RawMarker RawMarker                `json:"rawMarker"`
	Database  string                   `json:"database"`
	Role      string                   `json:"role"`
	Facts     backupformat.SourceFacts `json:"facts"`
}

type Empty struct {
	Database string `json:"database"`
	Role     string `json:"role"`
	Schema   string `json:"schema"`
}
