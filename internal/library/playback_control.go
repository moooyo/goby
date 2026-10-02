package library

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// WithPlaybackControlPool installs immutable startup routing. The generation
// owns both pools, and closing the Store drains workers before either is closed.
func WithPlaybackControlPool(pool *pgxpool.Pool) Option {
	return func(settings *storeOptions) error {
		if pool == nil || settings.playbackControlPool != nil {
			return fmt.Errorf("%w: invalid or repeated playback control pool", ErrInvalidInput)
		}
		settings.playbackControlPool = pool
		return nil
	}
}

func (s *Store) playbackReadPool(ctx context.Context) (*pgxpool.Pool, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	if !database.IsPlaybackControl(ctx) {
		return s.pool, nil
	}
	if s.playbackControlPool == nil {
		return nil, ErrUnavailable
	}
	return s.playbackControlPool, nil
}

// GetPlaybackControlItemFor retains the normal catalog projection and current
// credential policy for dynamic-source heartbeat authorization. Only a fixed
// playback-control handler may select the reserved pool for this readback.
func (s *Store) GetPlaybackControlItemFor(ctx context.Context, subject Subject, id string) (Item, error) {
	pool, err := s.playbackReadPool(ctx)
	if err != nil {
		return Item{}, err
	}
	return s.getItemForOnPool(ctx, subject, id, pool)
}
