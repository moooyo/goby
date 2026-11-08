package recovery

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/backupstore"
)

// recordBackupPublished records an already-observed filesystem fact, never a
// permission to perform another write. A short completion context survives a
// request/task cancellation after Publish has succeeded. Generation shutdown
// drains the owning job before releasing this pool's deployment lease.
func (m *Manager) recordBackupPublished(ctx context.Context, id, digest string, size int64) error {
	if m.operator {
		return nil
	}
	if !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	metadata, err := m.runtime.backups.Get(completion, id)
	if err != nil {
		return err
	}
	if metadata.State != backupstore.StateReady || metadata.Digest != digest || metadata.Size != size {
		return ErrConflict
	}
	tx, err := m.pool.BeginTx(completion, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackRestore(tx)
	if err = activity.RecordBackupPublished(completion, tx, id, digest, size); err != nil {
		return err
	}
	if !m.lease.Protects(m.pool) {
		return ErrUnavailable
	}
	if err = tx.Commit(completion); err != nil {
		return ErrUnavailable
	}
	return nil
}
