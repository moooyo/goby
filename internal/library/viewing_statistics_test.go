package library

import (
	"context"
	"errors"
	"testing"
)

func TestViewingStatisticsRoundsWithoutLosingTickPrecision(t *testing.T) {
	for _, test := range []struct {
		ticks string
		hours int64
	}{
		{"0", 0},
		{"17999999999", 0},
		{"18000000000", 1},
		{"53999999999", 1},
		{"54000000000", 2},
		{"27670116110564327421", 768614336},
	} {
		got, err := viewingStatisticsFromTicks(test.ticks)
		if err != nil || got.EstimatedContentHours != test.hours || got.EstimatedContentTicks != test.ticks || !got.IsEstimate {
			t.Fatalf("statistics for %s ticks = %+v, %v; want %d hours and exact ticks", test.ticks, got, err, test.hours)
		}
	}
	for _, ticks := range []string{"", "-1", "+1", "00", "1.5", " 1", "324259173170675712000000000"} {
		if _, err := viewingStatisticsFromTicks(ticks); err == nil {
			t.Fatalf("unsupported tick total %q was accepted", ticks)
		}
	}
}

func TestViewingStatisticsRequiresAnExplicitValidUser(t *testing.T) {
	store := &Store{}
	for _, subject := range []Subject{{}, {ApplicationCredentialID: "key"}, {UserID: " "}, {UserID: "user\n"}, {UserID: "user\x00"}} {
		if _, err := store.ViewingStatisticsFor(context.Background(), subject); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid subject %+v returned %v", subject, err)
		}
	}
}
