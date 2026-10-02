package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DataMaxConns            int32 = 12
	PlaybackControlMaxConns int32 = 4
	ApplicationMaxConns           = DataMaxConns + PlaybackControlMaxConns
)

// OpenPlaybackControl reserves application capacity for authenticated playback
// Ping and Stopped routes. The catalog owner remains inside DataMaxConns; the
// generation's hijacked deployment lease adds one physical connection outside
// these application pools, as it did before capacity was partitioned.
func OpenPlaybackControl(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return openPool(ctx, url, PlaybackControlMaxConns, "goby-playback-control")
}

// OpenPlaybackControlFor derives the reserved pool from the generation's
// already-bound data configuration, including a recovery candidate's database,
// schema, TLS, and connection policy. It never reinterprets a deployment URL.
func OpenPlaybackControlFor(ctx context.Context, data *pgxpool.Pool) (*pgxpool.Pool, error) {
	if data == nil || data.Config().MaxConns > DataMaxConns {
		return nil, errors.New("generation data pool exceeds the reserved application budget")
	}
	config := data.Config().Copy()
	config.MaxConns = PlaybackControlMaxConns
	config.MinConns = 1
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["application_name"] = "goby-playback-control"
	return openConfiguredPool(ctx, config)
}

type playbackControlContextKey struct{}

// WithPlaybackControl is called only by fixed server handlers, before their
// authentication middleware. No request header, query, or body can select it.
func WithPlaybackControl(ctx context.Context) context.Context {
	return context.WithValue(ctx, playbackControlContextKey{}, true)
}

func IsPlaybackControl(ctx context.Context) bool {
	reserved, _ := ctx.Value(playbackControlContextKey{}).(bool)
	return reserved
}
