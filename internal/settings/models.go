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

// Overrides retains configured values. Numeric nil fields use startup defaults;
// the raw ServerName must be interpreted with Snapshot.ServerNameMode. Returning
// a value never exposes the store's own pointers.
type Overrides struct {
	ServerName       *string
	MaxBitrate       *int64
	MaxWidth         *int
	MaxHeight        *int
	MaxAudioChannels *int
}

type ServerNameMode string

const (
	ServerNameDeployment ServerNameMode = "deployment"
	ServerNameCustom     ServerNameMode = "custom"
	ServerNameEmpty      ServerNameMode = "empty"
	ServerNameUnset      ServerNameMode = "unset"
)

// Encoding is an independent compatibility setting. It never replaces or
// resets the native MaxWidth. Execution combines supported limits separately.
type Encoding struct {
	TranscodingMaxWidth int
}

type Snapshot struct {
	Revision       int64
	Defaults       Values
	Overrides      Overrides
	Effective      Values
	ServerNameMode ServerNameMode
	HostName       string
	Encoding       Encoding
	UpdatedAt      time.Time
}

type Actor struct {
	Principal identity.Principal
	Audience  identity.AdministratorAudience
}

// UpdateRequest replaces the native override set. Without NameMode, schema-20
// semantics apply: nil name means deployment and non-nil name means custom.
// Omitted Encoding preserves the current independent compatibility setting.
type UpdateRequest struct {
	Revision  int64
	Overrides Overrides
	NameMode  *ServerNameMode
	Encoding  *Encoding
}

type Field string

const (
	FieldServerName          Field = "ServerName"
	FieldMaxBitrate          Field = "MaxBitrate"
	FieldMaxWidth            Field = "MaxWidth"
	FieldMaxHeight           Field = "MaxHeight"
	FieldMaxAudioChannels    Field = "MaxAudioChannels"
	FieldTranscodingMaxWidth Field = "TranscodingMaxWidth"
)

type ResetRequest struct {
	Revision int64
	Fields   []Field
}

// Configuration never contains management values for a viewer. Its zero
// Snapshot and initialization value must not be projected when CanManage=false.
type Configuration struct {
	Snapshot               Snapshot
	StartupWizardCompleted bool
	CanManage              bool
}

type ConfigurationSection string

const (
	ConfigurationFull     ConfigurationSection = "full"
	ConfigurationPartial  ConfigurationSection = "partial"
	ConfigurationEncoding ConfigurationSection = "encoding"
)

// ConfigurationMutation is already decoded by the compatibility boundary.
// Presence is independent of null: a present nil name requests Unset, while a
// missing Partial name preserves the current state. Full omissions reset only
// that configuration section, never unrelated native overrides.
type ConfigurationMutation struct {
	Section                    ConfigurationSection
	ServerNamePresent          bool
	ServerName                 *string
	TranscodingMaxWidthPresent bool
	TranscodingMaxWidth        int
	StartupWizardCompleted     *bool
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

func effectiveValues(defaults Values, overrides Overrides, mode ServerNameMode, hostName string) Values {
	value := defaults
	if mode == ServerNameCustom && overrides.ServerName != nil {
		value.ServerName = *overrides.ServerName
	} else if mode == ServerNameEmpty || mode == ServerNameUnset {
		value.ServerName = hostName
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
