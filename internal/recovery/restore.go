package recovery

import (
	"context"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

// RestoredDatabase describes completed staging, not active deployment state.
// The raw restore was verified before any normalization reported here. No
// network listener, scheduled task manager, media scan, or encoder was started.
type RestoredDatabase struct {
	SourceVersion        int64
	CurrentVersion       int64
	RevokedCredentials   int64
	ExpiredPlayback      int64
	InterruptedEncodings int64
	InterruptedScans     int64
	InterruptedTasks     int64
	RegisteredRoots      int64
	Administrators       int64
}

// RestoreInto requires an exclusively leased, separately configured empty
// target. Master, root, administrator, and credential checks share the raw
// restore transaction; failure leaves the target empty. The optional trusted
// binding callback runs after normalization but before that same commit. Later
// scan/task normalization can leave a locally bound incomplete inactive stage;
// it must never be activated or silently cleared after failure.
func (a *Archive) RestoreInto(ctx context.Context, target *pgxpool.Pool, lease *database.Lease, targetConfig config.Config, binding ...func(context.Context, pgx.Tx) error) (RestoredDatabase, error) {
	var result RestoredDatabase
	if a == nil || target == nil || !lease.Protects(target) || target.Config().ConnString() != targetConfig.DatabaseURL || len(binding) > 1 || len(binding) == 1 && binding[0] == nil {
		return result, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !a.use.TryLock() {
		return result, ErrBusy
	}
	defer a.use.Unlock()
	if a.closed {
		return result, ErrUnavailable
	}
	work, cancel := context.WithCancel(a.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer func() { stop(); cancel() }()
	go func() {
		select {
		case <-lease.Done():
			cancel()
		case <-work.Done():
		}
	}()
	if err := work.Err(); err != nil {
		return result, err
	}
	finalize := func(work context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
		if !lease.ProtectsTransaction(target, tx) {
			return ErrUnavailable
		}
		normalized, err := normalizeRestoredIdentity(work, tx, a.Master, targetConfig)
		if err != nil {
			return err
		}
		normalized.SourceVersion, normalized.CurrentVersion = raw.SourceVersion, raw.CurrentVersion
		if len(binding) == 1 {
			if err := binding[0](work, tx); err != nil {
				return err
			}
		}
		if !lease.ProtectsTransaction(target, tx) {
			return ErrUnavailable
		}
		result = normalized
		return nil
	}
	var err error
	if a.engine.pool == nil {
		_, err = backuppg.RestoreOfflineFinalized(work, target, a.Database(), a.Manifest.Source, a.engine.options, finalize)
	} else {
		_, err = backuppg.RestoreFinalized(work, a.engine.pool, target, a.Database(), a.Manifest.Source, a.engine.options, finalize)
	}
	if err != nil {
		return RestoredDatabase{}, err
	}
	// No task Manager is created, so restored schedules cannot execute here.
	if err := normalizeRestoredWork(work, target, targetConfig); err != nil {
		return RestoredDatabase{}, err
	}
	if err := work.Err(); err != nil || !lease.Protects(target) {
		return RestoredDatabase{}, ErrUnavailable
	}
	return result, nil
}

func normalizeRestoredIdentity(ctx context.Context, tx pgx.Tx, master []byte, targetConfig config.Config) (RestoredDatabase, error) {
	var result RestoredDatabase
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, tx, master); err != nil {
		return result, err
	}
	var err error
	result.RegisteredRoots, err = validateRestoredRoots(ctx, tx, targetConfig.MediaRoots)
	if err != nil {
		return RestoredDatabase{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_administrator AND NOT is_disabled`).Scan(&result.Administrators); err != nil || result.Administrators < 1 {
		return RestoredDatabase{}, ErrArchive
	}
	result.RevokedCredentials, err = identity.RevokeRecoveredCredentials(ctx, tx)
	if err != nil {
		return RestoredDatabase{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE play_sessions SET state='Expired',
		stopped_at=COALESCE(stopped_at,clock_timestamp()),updated_at=GREATEST(updated_at,clock_timestamp())
		WHERE state IN ('Prepared','Playing','Paused')`)
	if err != nil {
		return RestoredDatabase{}, ErrUnavailable
	}
	result.ExpiredPlayback = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `UPDATE encoding_jobs SET state='interrupted',error_code='backup_restored',
		updated_at=GREATEST(updated_at,clock_timestamp()) WHERE state IN ('queued','running')`)
	if err != nil {
		return RestoredDatabase{}, ErrUnavailable
	}
	result.InterruptedEncodings = tag.RowsAffected()
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
		(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping'))`).Scan(&result.InterruptedScans, &result.InterruptedTasks); err != nil {
		return RestoredDatabase{}, ErrUnavailable
	}
	return result, nil
}

func normalizeRestoredWork(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	catalog, err := library.New(pool, media.Prober{FFprobePath: cfg.FFprobePath, FFmpegPath: cfg.FFmpegPath, Timeout: 30 * time.Second}, cfg.MediaRoots)
	if err != nil {
		return ErrArchive
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(closeCtx); err != nil && resultErr == nil {
			resultErr = ErrUnavailable
		}
	}()
	store, err := tasks.New(pool, catalog)
	if err != nil {
		return ErrUnavailable
	}
	if err := store.Reconcile(ctx); err != nil {
		return ErrArchive
	}
	if err := store.RecoverRuns(ctx); err != nil {
		return ErrArchive
	}
	return nil
}

// Only operator-approved root identities survive. An unavailable mount is
// allowed and does not cause row deletion; normal safe scanner opening still
// applies if the operator later starts a scan. This does not remap paths.
func validateRestoredRoots(ctx context.Context, tx pgx.Tx, configured []string) (int64, error) {
	approved := make(map[string]bool, len(configured))
	for _, root := range configured {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return 0, ErrInvalid
		}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolved
		}
		approved[filepath.Clean(absolute)] = true
	}
	rows, err := tx.Query(ctx, `SELECT path,allowed_path,relative_path FROM library_roots ORDER BY id`)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		var full, allowed, relative string
		if err := rows.Scan(&full, &allowed, &relative); err != nil {
			return 0, ErrUnavailable
		}
		if !validRestoredRoot(full, allowed, relative, approved) {
			return 0, ErrArchive
		}
		count++
	}
	if rows.Err() != nil {
		return 0, ErrUnavailable
	}
	return count, nil
}

func validRestoredRoot(full, allowed, relative string, approved map[string]bool) bool {
	for _, value := range []string{full, allowed, relative} {
		if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return false
		}
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
			if part == ".." {
				return false
			}
		}
	}
	return approved[allowed] && path.IsAbs(allowed) && path.Clean(allowed) == allowed &&
		path.IsAbs(full) && path.Clean(full) == full && !path.IsAbs(relative) &&
		path.Clean(relative) == relative && path.Join(allowed, relative) == full
}

func rollbackRestore(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
