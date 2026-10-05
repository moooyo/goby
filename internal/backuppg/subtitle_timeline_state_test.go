package backuppg

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestValidateSubtitleTimelineStatePreservesVersionAndContext(t *testing.T) {
	for _, version := range []int64{18, 19, 49, 50, 59, 60} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := validateSubtitleTimelineState(ctx, nil, version); !errors.Is(err, context.Canceled) {
			t.Fatalf("schema %d lost cancellation: %v", version, err)
		}
		want := error(nil)
		if version >= 19 {
			want = ErrDatabase
		}
		if err := validateSubtitleTimelineState(context.Background(), nil, version); !errors.Is(err, want) {
			t.Fatalf("schema %d crossed its migration boundary: %v", version, err)
		}
	}
	for _, test := range []struct {
		name   string
		err    error
		cancel bool
		want   error
	}{
		{name: "invalid_relations", want: ErrSchema},
		{name: "read_failure", err: errors.New("read failed"), want: ErrDatabase},
		{name: "driver_deadline", err: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
		{name: "cancelled_invalid_read", cancel: true, want: context.Canceled},
		{name: "cancelled_failed_read", cancel: true, err: errors.New("read interrupted"), want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			row := themeStateTestRow{err: test.err}
			if test.cancel {
				row.onScan = cancel
			}
			tx := &themeStateTestTx{row: row}
			if err := validateSubtitleTimelineState(ctx, tx, 60); !errors.Is(err, test.want) {
				t.Fatalf("subtitle timeline recovery result = %v, want %v", err, test.want)
			}
			if tx.queries != 1 {
				t.Fatal("invalid relationships should stop further inspection")
			}
		})
	}
}
