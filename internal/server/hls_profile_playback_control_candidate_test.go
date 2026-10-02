//go:build linux

package server

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// This addon is copied only to the candidate checkout. The shared profiles
// compile unchanged on the baseline and retain nil hooks with Data16 there.
func init() {
	hlsProfileDataConfigurationHook = func(configuration *pgxpool.Config) {
		if os.Getenv("GOBY_HLS_PRODUCTION_DATABASE_CAPACITY") == "1" {
			configuration.MaxConns = database.DataMaxConns
		}
	}
	hlsProfileApplicationOptionsHook = func(t *testing.T, ctx context.Context, data *pgxpool.Pool) []Option {
		if os.Getenv("GOBY_HLS_PRODUCTION_DATABASE_CAPACITY") != "1" {
			return nil
		}
		t.Helper()
		control, err := database.OpenPlaybackControlFor(ctx, data)
		if err != nil {
			t.Fatal("open the bound production-capacity profile reservation")
		}
		// Registered before New and its cleanup, so actual app worker drain
		// always precedes this reservation's close, including a failed New.
		t.Cleanup(control.Close)
		held := make([]*pgxpool.Conn, 0, database.PlaybackControlMaxConns)
		for range database.PlaybackControlMaxConns {
			connection, err := control.Acquire(ctx)
			if err != nil {
				for _, acquired := range held {
					acquired.Release()
				}
				t.Fatal("warm every reserved profile database connection")
			}
			held = append(held, connection)
		}
		for _, connection := range held {
			connection.Release()
		}
		return []Option{WithPlaybackControlPool(control)}
	}
	hlsProfileControlStatHook = func(app *Server) *pgxpool.Stat {
		if app == nil || app.playbackControlDB == nil {
			return nil
		}
		return app.playbackControlDB.Stat()
	}
}
