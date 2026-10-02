package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// WithPlaybackControlPool returns a startup-configured store. Its caller owns
// both pools and must keep them open until ingress and application workers drain.
// The original store is unchanged, so routing configuration is never mutated
// while authentication requests are running.
func (s *Store) WithPlaybackControlPool(pool *pgxpool.Pool) *Store {
	return &Store{pool: s.pool, playbackControlPool: pool, applicationKeyVault: s.applicationKeyVault}
}

// ForPlaybackControl selects the complete identity dependency chain, including
// application client binding and due activity writes. A marked request must
// never borrow Data capacity, even when its reserved pool is unavailable.
func (s *Store) ForPlaybackControl(ctx context.Context) (*Store, error) {
	if !database.IsPlaybackControl(ctx) {
		return s, nil
	}
	if s == nil || s.playbackControlPool == nil {
		return nil, errors.New("playback control database is unavailable")
	}
	return &Store{pool: s.playbackControlPool, applicationKeyVault: s.applicationKeyVault}, nil
}
