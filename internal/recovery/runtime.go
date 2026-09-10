package recovery

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverycontrol"
)

// Runtime owns the local lifecycle fence and encrypted backup store. Open it
// before connecting or migrating either database, and close it only after all
// HTTP handlers, jobs, database leases, and pools have stopped. The deployment
// configuration retains fixed slot identities; ActiveConfig swaps the active
// and inactive URLs only in its returned runtime copy.
type Runtime struct {
	deployment config.Config
	lifecycle  *lifecycle.Store
	backups    *backupstore.Store
	control    *recoverycontrol.Store
	closeOnce  sync.Once
	closeErr   error
}

func Open(ctx context.Context, deployment config.Config) (*Runtime, error) {
	deployment.Recovery = deployment.Recovery.WithDefaults()
	if err := deployment.Recovery.Validate(deployment.DatabaseURL); err != nil {
		return nil, ErrInvalid
	}
	// Legacy media configuration accepts relative paths. Resolve them with
	// Linux runtime semantics before excluding overlap with private stores.
	for _, root := range deployment.MediaRoots {
		resolved, err := filepath.Abs(root)
		if err != nil {
			return nil, ErrInvalid
		}
		for _, private := range []string{deployment.Recovery.Directory, deployment.Recovery.Backups.Directory, deployment.Recovery.OperationsDirectory} {
			if directoriesOverlap(private, resolved) {
				return nil, ErrInvalid
			}
		}
	}
	control, err := lifecycle.Open(ctx, deployment.Recovery.Directory)
	if err != nil {
		return nil, err
	}
	backups, err := backupstore.Open(deployment.Recovery.Backups)
	if err != nil {
		_ = control.Close()
		return nil, err
	}
	state, err := control.Current()
	if err != nil {
		_ = backups.Close()
		_ = control.Close()
		return nil, err
	}
	operations, err := recoverycontrol.Open(ctx, deployment.Recovery.OperationsDirectory, state.DeploymentID)
	if err != nil {
		_ = backups.Close()
		_ = control.Close()
		return nil, err
	}
	return &Runtime{deployment: deployment, lifecycle: control, backups: backups, control: operations}, nil
}

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.closeErr = errors.Join(r.control.Close(), r.backups.Close(), r.lifecycle.Close())
	})
	return r.closeErr
}

// ActiveConfig never falls back to the primary database after a generation
// has been activated. Missing files, invalid defaults, or an unavailable slot
// prevent startup. A pending activated plan still resolves its new generation;
// application acceptance and explicit rollback are separate coordinator work.
func (r *Runtime) ActiveConfig(ctx context.Context) (config.Config, lifecycle.State, error) {
	if r == nil {
		return config.Config{}, lifecycle.State{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return config.Config{}, lifecycle.State{}, err
	}
	state, err := r.lifecycle.Current()
	if err != nil {
		return config.Config{}, lifecycle.State{}, err
	}
	active := r.deployment
	switch state.DatabaseSlot {
	case lifecycle.DatabasePrimary:
	case lifecycle.DatabaseRecovery:
		if r.deployment.Recovery.DatabaseURL == "" {
			return config.Config{}, lifecycle.State{}, ErrUnavailable
		}
		active.DatabaseURL, active.Recovery.DatabaseURL = r.deployment.Recovery.DatabaseURL, r.deployment.DatabaseURL
	default:
		return config.Config{}, lifecycle.State{}, ErrUnavailable
	}
	if state.GenerationID == "" {
		if state.DatabaseSlot != lifecycle.DatabasePrimary || state.Master != lifecycle.MasterDefault || state.Revision != 0 {
			return config.Config{}, lifecycle.State{}, ErrUnavailable
		}
		return active, state, nil
	}
	files, err := r.lifecycle.ReadGeneration(ctx, state.GenerationID)
	if err != nil {
		return config.Config{}, lifecycle.State{}, err
	}
	defer clear(files.Master)
	defaults, err := config.DecodeBackupDefaults(files.Config)
	if err != nil {
		return config.Config{}, lifecycle.State{}, ErrArchive
	}
	active, err = defaults.Apply(active)
	if err != nil {
		return config.Config{}, lifecycle.State{}, ErrArchive
	}
	switch state.Master {
	case lifecycle.MasterDefault:
	case lifecycle.MasterGeneration:
		active.APIKeyMasterKeyFile, err = r.lifecycle.MasterKeyPath(ctx, state.GenerationID)
		if err != nil {
			return config.Config{}, lifecycle.State{}, err
		}
	default:
		return config.Config{}, lifecycle.State{}, ErrUnavailable
	}
	return active, state, nil
}

func (r *Runtime) Backups() *backupstore.Store { return r.backups }

func directoriesOverlap(first, second string) bool {
	within := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		return err == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	return within(first, second) || within(second, first)
}
