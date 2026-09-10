// Package recoverycontrol stores one deployment-bound recovery coordinator
// record. It fences local processes and provides durable compare-and-swap;
// business recovery decisions remain the caller's responsibility.
package recoverycontrol

import "errors"

const (
	DefaultDirectory = "/var/lib/goby/recovery-operations"
	MaxPayloadBytes  = 1 << 20
)

var (
	ErrInvalid          = errors.New("invalid recovery control request")
	ErrBusy             = errors.New("recovery control is locked")
	ErrUnavailable      = errors.New("recovery control unavailable")
	ErrConflict         = errors.New("recovery control compare-and-swap conflict")
	ErrRecoveryRequired = errors.New("recovery control outcome requires inspection")
)

// Snapshot holds the actual published record. Digest binds its revision,
// deployment, store identity, previous digest and normalized JSON payload.
// Payload is an independent byte slice and is empty only at revision zero.
type Snapshot struct {
	Revision uint64
	Digest   string
	Payload  []byte
}
