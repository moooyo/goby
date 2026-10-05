package library

import (
	"context"
	"fmt"
	"math/big"

	"github.com/moooyo/goby/internal/media"
)

// ViewingStatistics describes watched content, not measured elapsed playback.
// Tick totals are decimal strings so large libraries retain integer precision.
type ViewingStatistics struct {
	EstimatedContentHours int64  `json:"EstimatedContentHours"`
	EstimatedContentTicks string `json:"EstimatedContentTicks"`
	IsEstimate            bool   `json:"IsEstimate"`
}

// ViewingStatisticsFor aggregates one user's currently visible movie and episode
// state. Finished items contribute their runtime once; unfinished items contribute
// their bounded position. Unknown runtimes and all other item kinds contribute zero.
func (s *Store) ViewingStatisticsFor(ctx context.Context, subject Subject) (ViewingStatistics, error) {
	if !validSubject(subject) || !validCatalogLibraryIdentifier(subject.UserID) {
		return ViewingStatistics{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return ViewingStatistics{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL jit = off`); err != nil {
		return ViewingStatistics{}, fmt.Errorf("disable viewing statistics query JIT: %w", err)
	}
	// Numeric accumulation avoids bigint overflow before the total becomes text.
	// A malformed or out-of-range runtime is unknown, not a fabricated duration.
	statement := `SELECT COALESCE(SUM(CASE WHEN state.played THEN duration.ticks
		ELSE LEAST(GREATEST(state.playback_position_ticks::numeric, 0), duration.ticks) END), 0)::text
		FROM user_item_data state JOIN items i ON i.id=state.item_id
		CROSS JOIN LATERAL (SELECT CASE
			WHEN jsonb_typeof(i.media->'DurationTicks')='number'
				AND (i.media->>'DurationTicks') ~ '^[0-9]{1,19}$'
			THEN CASE WHEN (i.media->>'DurationTicks')::numeric <= 9223372036854775807
				THEN (i.media->>'DurationTicks')::numeric ELSE 0 END
			ELSE 0 END AS ticks) duration
		WHERE state.user_id=$1 AND i.type IN ('Movie', 'Episode') AND NOT i.is_folder
			AND duration.ticks > 0 AND ` + access.ordinarySQL("i")
	var ticks string
	if err := tx.QueryRow(ctx, statement, subject.UserID).Scan(&ticks); err != nil {
		return ViewingStatistics{}, fmt.Errorf("aggregate viewing statistics: %w", err)
	}
	result, err := viewingStatisticsFromTicks(ticks)
	if err != nil {
		return ViewingStatistics{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ViewingStatistics{}, fmt.Errorf("complete viewing statistics read: %w", err)
	}
	return result, nil
}

func viewingStatisticsFromTicks(ticks string) (ViewingStatistics, error) {
	total, ok := new(big.Int).SetString(ticks, 10)
	if !ok || total.Sign() < 0 || total.String() != ticks {
		return ViewingStatistics{}, fmt.Errorf("invalid viewing statistics tick total")
	}
	perHour := big.NewInt(3600 * media.TicksPerSecond)
	rounded := new(big.Int).Add(total, new(big.Int).Quo(new(big.Int).Set(perHour), big.NewInt(2)))
	rounded.Quo(rounded, perHour)
	// JSON numbers consumed by the player must remain exact JavaScript integers.
	if !rounded.IsInt64() || rounded.Int64() > 9_007_199_254_740_991 {
		return ViewingStatistics{}, fmt.Errorf("viewing statistics exceed the supported hour range")
	}
	return ViewingStatistics{EstimatedContentHours: rounded.Int64(), EstimatedContentTicks: ticks, IsEstimate: true}, nil
}
