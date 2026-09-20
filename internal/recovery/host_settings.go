package recovery

import (
	"context"
	"reflect"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/settings"
)

// hostSettingsCapture is local coordinator evidence. It is not accepted in a
// restore request or copied from an archive. The shared settings revision also
// rejects a stale capture when an administrator changes choices while staging.
type hostSettingsCapture struct {
	Revision string                      `json:"revision"`
	Settings settings.TargetHostSettings `json:"settings"`
}

func (capture *hostSettingsCapture) valid(operator bool) bool {
	if capture == nil || settings.ValidateTargetHostSettings(capture.Settings) != nil {
		return false
	}
	value, err := strconv.ParseInt(capture.Revision, 10, 64)
	if err != nil || strconv.FormatInt(value, 10) != capture.Revision || value < 0 {
		return false
	}
	if operator {
		return value == 0 && reflect.DeepEqual(capture.Settings, settings.TargetHostSettings{})
	}
	return value > 0
}

func readHostSettingsCapture(ctx context.Context, tx pgx.Tx, lock bool) (*hostSettingsCapture, error) {
	query := `SELECT revision::text FROM managed_settings WHERE id=1`
	if lock {
		query += ` FOR SHARE`
	}
	var result hostSettingsCapture
	if err := tx.QueryRow(ctx, query).Scan(&result.Revision); err != nil {
		return nil, ErrUnavailable
	}
	value, err := settings.ReadTargetHostSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	result.Settings = value
	if !result.valid(false) {
		return nil, ErrInvalid
	}
	return &result, nil
}

func (m *Manager) captureHostSettings(ctx context.Context) (*hostSettingsCapture, error) {
	if m.operator {
		return &hostSettingsCapture{Revision: "0"}, nil
	}
	if !m.lease.Protects(m.pool) {
		return nil, ErrUnavailable
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rollbackRestore(tx)
	if !m.lease.ProtectsTransaction(m.pool, tx) {
		return nil, ErrUnavailable
	}
	value, err := readHostSettingsCapture(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil || !m.lease.Protects(m.pool) {
		return nil, ErrUnavailable
	}
	return value, nil
}

func (m *Manager) checkHostSettings(ctx context.Context, expected *hostSettingsCapture) error {
	if !expected.valid(m.operator) {
		return ErrConflict
	}
	actual, err := m.captureHostSettings(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return ErrConflict
	}
	return nil
}

func checkHostSettingsTx(ctx context.Context, tx pgx.Tx, expected *hostSettingsCapture) error {
	if !expected.valid(false) {
		return ErrConflict
	}
	actual, err := readHostSettingsCapture(ctx, tx, true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return ErrConflict
	}
	return nil
}
