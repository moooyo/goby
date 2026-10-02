//go:build linux

package server

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Copy this addon only to candidate. The shared fixture owns its returned pool
// and closes it only after actual application and HTTP workers have joined.
func init() {
	hlsColdPoolOptionsHook = func(ctx context.Context, data *pgxpool.Pool) ([]Option, *pgxpool.Pool, error) {
		if os.Getenv("GOBY_HLS_PRODUCTION_DATABASE_CAPACITY") != "1" {
			return nil, nil, nil
		}
		control, err := database.OpenPlaybackControlFor(ctx, data)
		if err != nil {
			return nil, nil, err
		}
		return []Option{WithPlaybackControlPool(control)}, control, nil
	}
}
