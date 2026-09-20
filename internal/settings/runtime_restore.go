package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

// TargetHostSettings is a trusted coordinator capture, never incoming archive
// metadata. Nil overrides preserve target deployment defaults after activation.
type TargetHostSettings struct {
	Network  *NetworkOverrides
	Hardware *HardwareSelection
	Threads  *int
}

// ValidateTargetHostSettings validates a trusted target capture without reading
// database state, inventory availability, devices or deployment configuration.
func ValidateTargetHostSettings(value TargetHostSettings) error {
	return validateRuntimeOverrides(RuntimeOverrides{Network: cloneNetworkOverrides(value.Network),
		Hardware: clonePointer(value.Hardware), Threads: clonePointer(value.Threads)})
}

func CaptureTargetHostSettings(snapshot Snapshot) TargetHostSettings {
	return TargetHostSettings{Network: cloneNetworkOverrides(snapshot.Runtime.Overrides.Network),
		Hardware: clonePointer(snapshot.Runtime.Overrides.Hardware), Threads: clonePointer(snapshot.Runtime.Overrides.Threads)}
}

// ReadTargetHostSettings captures target-owned choices in a coordinator-owned
// transaction before an incoming archive can replace them. The caller supplies
// the target lease, snapshot isolation and transition authority.
func ReadTargetHostSettings(ctx context.Context, tx pgx.Tx) (TargetHostSettings, error) {
	if tx == nil {
		return TargetHostSettings{}, ErrInvalidInput
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&raw); err != nil {
		return TargetHostSettings{}, fmt.Errorf("read target host choices: %w", err)
	}
	value, err := decodeStoredRuntime(raw)
	if err != nil {
		return TargetHostSettings{}, err
	}
	return CaptureTargetHostSettings(Snapshot{Runtime: RuntimeSnapshot{Overrides: value}}), nil
}

// NormalizeRestoredHostSettings runs only after the raw archive has been fully
// verified, inside the trusted recovery transaction and before activation. It
// changes target-owned execution choices, preserving portable quality and tone
// policy. It does not publish runtime state or fabricate an administrator event.
func NormalizeRestoredHostSettings(ctx context.Context, tx pgx.Tx, target TargetHostSettings) (bool, error) {
	if tx == nil {
		return false, ErrInvalidInput
	}
	targetRuntime := RuntimeOverrides{Network: cloneNetworkOverrides(target.Network), Hardware: clonePointer(target.Hardware), Threads: clonePointer(target.Threads)}
	if err := validateRuntimeOverrides(targetRuntime); err != nil {
		return false, err
	}
	var revision int64
	var encoded []byte
	if err := tx.QueryRow(ctx, `SELECT revision,runtime_overrides FROM managed_settings WHERE id=1 FOR UPDATE`).Scan(&revision, &encoded); err != nil {
		return false, fmt.Errorf("read restored host settings: %w", err)
	}
	previous, err := decodeStoredRuntime(encoded)
	if err != nil {
		return false, err
	}
	next := cloneRuntimeOverrides(previous)
	next.Network, next.Hardware, next.Threads = targetRuntime.Network, targetRuntime.Hardware, targetRuntime.Threads
	if equalRuntimeOverrides(previous, next) {
		return false, nil
	}
	if revision < 1 || revision == math.MaxInt64 {
		return false, fmt.Errorf("%w: restored settings revision cannot advance", ErrStoredSettings)
	}
	data, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE managed_settings SET runtime_overrides=$1,revision=revision+1,
		updated_at=GREATEST(updated_at,clock_timestamp()) WHERE id=1`, data); err != nil {
		return false, fmt.Errorf("normalize restored host settings: %w", err)
	}
	return true, nil
}
