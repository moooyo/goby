// Package settings manages explicit overrides of a small startup-default set.
// Runtime readers receive immutable, committed effective values without I/O.
package settings

import (
	"errors"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

var (
	ErrInvalidInput     = errors.New("invalid settings input")
	ErrRevisionConflict = errors.New("settings revision conflict")
	ErrUnavailable      = errors.New("settings service unavailable")
	ErrStoredSettings   = errors.New("stored settings are invalid")
)

// Values contains no deployment paths, credentials, or mutable references.
// ServerName applies immediately; output limits are captured by new plans.
type Values struct {
	ServerName       string
	MaxBitrate       int64
	MaxWidth         int
	MaxHeight        int
	MaxAudioChannels int
}

// Defaults are frozen by New from the already-resolved builtin/environment
// configuration. Reloading a process can change its defaults, never overrides.
type Defaults = Values

// Overrides distinguishes an explicitly saved value from fallback to the
// startup default. Returning a value never exposes the store's own pointers.
type Overrides struct {
	ServerName       *string
	MaxBitrate       *int64
	MaxWidth         *int
	MaxHeight        *int
	MaxAudioChannels *int
}

type Snapshot struct {
	Revision  int64
	Defaults  Values
	Overrides Overrides
	Effective Values
	UpdatedAt time.Time
}

type Actor struct {
	Principal identity.Principal
	Audience  identity.AdministratorAudience
}

// UpdateRequest replaces the complete override set; nil fields resume defaults.
type UpdateRequest struct {
	Revision  int64
	Overrides Overrides
}

type Field string

const (
	FieldServerName       Field = "ServerName"
	FieldMaxBitrate       Field = "MaxBitrate"
	FieldMaxWidth         Field = "MaxWidth"
	FieldMaxHeight        Field = "MaxHeight"
	FieldMaxAudioChannels Field = "MaxAudioChannels"
)

type ResetRequest struct {
	Revision int64
	Fields   []Field
}

type ValidationError struct{ Fields map[string]string }

func (err *ValidationError) Error() string { return "invalid settings input" }
func (err *ValidationError) Unwrap() error { return ErrInvalidInput }

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneOverrides(value Overrides) Overrides {
	return Overrides{
		ServerName:       clonePointer(value.ServerName),
		MaxBitrate:       clonePointer(value.MaxBitrate),
		MaxWidth:         clonePointer(value.MaxWidth),
		MaxHeight:        clonePointer(value.MaxHeight),
		MaxAudioChannels: clonePointer(value.MaxAudioChannels),
	}
}

func cloneSnapshot(value Snapshot) Snapshot {
	value.Overrides = cloneOverrides(value.Overrides)
	return value
}

func effectiveValues(defaults Values, overrides Overrides) Values {
	value := defaults
	if overrides.ServerName != nil {
		value.ServerName = *overrides.ServerName
	}
	if overrides.MaxBitrate != nil {
		value.MaxBitrate = *overrides.MaxBitrate
	}
	if overrides.MaxWidth != nil {
		value.MaxWidth = *overrides.MaxWidth
	}
	if overrides.MaxHeight != nil {
		value.MaxHeight = *overrides.MaxHeight
	}
	if overrides.MaxAudioChannels != nil {
		value.MaxAudioChannels = *overrides.MaxAudioChannels
	}
	return value
}
