package recovery

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/recoverydb"
)

func (r *Runtime) databaseBinding(cfg config.Config, pool *pgxpool.Pool, lease *database.Lease) (*recoverydb.Store, lifecycle.State, error) {
	state, err := r.lifecycle.Current()
	if err != nil {
		return nil, lifecycle.State{}, err
	}
	options := cfg.Recovery.WithDefaults()
	store, err := recoverydb.New(pool, lease, recoverydb.Config{
		DeploymentID: state.DeploymentID, Slot: state.DatabaseSlot,
		Postgres: backuppg.Options{SourceURL: cfg.DatabaseURL, Schema: "public",
			PGDump: options.PGDumpPath, PGRestore: options.PGRestorePath,
			Timeout: options.OperationTimeout, MaxDumpBytes: options.Backups.MaxObjectBytes,
			ProbeVersion: media.CurrentProbeVersion},
	})
	return store, state, err
}

// CheckDatabase runs before migration. An existing foreign or inactive marker
// is not permission to migrate that database, even with a new local directory.
func (r *Runtime) CheckDatabase(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, lease *database.Lease) error {
	store, state, err := r.databaseBinding(cfg, pool, lease)
	if err != nil {
		return err
	}
	observed, err := store.Read(ctx)
	if err != nil {
		return err
	}
	if !observed.Raw.Present {
		if state.Revision == 0 && state.GenerationID == "" && state.DatabaseSlot == lifecycle.DatabasePrimary && state.Master == lifecycle.MasterDefault {
			return nil
		}
		return ErrConflict
	}
	marker, err := recoverydb.DecodeMarker(observed.Raw.Value)
	if err != nil || marker.DeploymentID != state.DeploymentID || marker.Slot != state.DatabaseSlot || marker.GenerationID != state.GenerationID {
		return ErrConflict
	}
	return nil
}

// BindDatabase claims only the original configured primary after its trusted
// migration succeeds. Every subsequent generation must already carry the exact
// binding written atomically with its staged database.
func (r *Runtime) BindDatabase(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, lease *database.Lease) error {
	store, state, err := r.databaseBinding(cfg, pool, lease)
	if err != nil {
		return err
	}
	if state.Revision == 0 {
		_, err = store.BindInitial(ctx, state)
		return err
	}
	return r.CheckDatabase(ctx, cfg, pool, lease)
}
