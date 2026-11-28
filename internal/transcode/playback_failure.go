package transcode

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/moooyo/goby/internal/diagnostics"
)

func captureCacheAccountingFailure(ctx context.Context, unknown bool) {
	class := "accounting_pending"
	if unknown {
		class = "accounting_unknown"
	}
	diagnostics.CapturePlaybackFailure(ctx, diagnostics.PlaybackFailure{Stage: "hls_cache_accounting", ErrorClass: class})
}

func captureEncodingCreateFailure(ctx context.Context, stage string, err error) {
	if !diagnostics.CapturesPlaybackFailure(ctx) {
		return
	}
	class := diagnostics.ErrorClass(err)
	switch {
	case diagnostics.ErrorMatches(err, ErrInvalidRecord):
		class = "invalid_record"
	case diagnostics.ErrorMatches(err, ErrRecordUnauthorized), diagnostics.ErrorMatches(err, pgx.ErrNoRows):
		class = "record_unauthorized"
	case diagnostics.ErrorMatches(err, ErrRecordConflict):
		class = "record_conflict"
	case class == "unclassified":
		class = "database_error"
		// Only consume the concrete PostgreSQL error at the query site. Do not
		// retain its message, detail, SQL text, arguments, or raw SQLSTATE.
		if pg, ok := err.(*pgconn.PgError); ok && pg != nil {
			switch pg.Code {
			case "23505":
				class = "record_conflict"
			case "23502", "23503", "23514":
				class = "database_constraint"
			case "40001", "40P01":
				class = "database_conflict"
			case "55P03", "57014":
				class = "database_busy"
			case "08000", "08001", "08003", "08006", "53300", "57P01", "57P02", "57P03":
				class = "database_unavailable"
			}
		}
	}
	diagnostics.CapturePlaybackFailure(ctx, diagnostics.PlaybackFailure{Stage: stage, ErrorClass: class})
}
