package tasks

import (
	"errors"
	"testing"
	"time"
)

func TestPersistedScheduleCeilingPreservesFractionalIntervalPhase(t *testing.T) {
	anchor := scheduleTestTime(t, "2026-01-01T00:00:00.000123Z")
	const ticks int64 = ScheduleTicksPerSecond + 1
	rule := scheduleTestInterval(anchor, ticks)
	period := time.Second + 100*time.Nanosecond
	stored, err := ceilScheduleTime(anchor.Add(period))
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 40; index++ {
		exact := anchor.Add(time.Duration(index) * period)
		if stored.Before(exact) || stored.Sub(exact) >= time.Microsecond {
			t.Fatalf("occurrence %d rounded early or drifted: exact=%s stored=%s", index, exact, stored)
		}
		due, err := summarizeScheduleDue(rule, stored, stored)
		if err != nil || due.Count != 1 || !due.First.Equal(stored) || !due.Last.Equal(stored) || !due.Penultimate.IsZero() {
			t.Fatalf("occurrence %d did not consume exactly once: %+v, %v", index, due, err)
		}
		wanted, err := ceilScheduleTime(anchor.Add(time.Duration(index+1) * period))
		if err != nil || !due.Next.Equal(wanted) {
			t.Fatalf("occurrence %d shifted its immutable anchor: next=%s wanted=%s error=%v", index, due.Next, wanted, err)
		}
		stored = due.Next
	}
}

func TestPersistedScheduleExactIntervalSummaryAcrossMillennia(t *testing.T) {
	anchor := scheduleTestTime(t, "0001-01-01T00:00:00Z")
	through := scheduleTestTime(t, "9998-01-01T00:00:00Z")
	rule := scheduleTestInterval(anchor, ScheduleTicksPerSecond)
	due, err := summarizeScheduleDue(rule, anchor.Add(time.Second), through)
	wantCount := through.Unix() - anchor.Unix()
	if err != nil || due.Count != wantCount || !due.Last.Equal(through) ||
		!due.Penultimate.Equal(through.Add(-time.Second)) || !due.Next.Equal(through.Add(time.Second)) {
		t.Fatalf("constant-count interval summary lost full calendar range: count=%d wanted=%d error=%v", due.Count, wantCount, err)
	}
}

func TestPersistedCalendarMisfiresRespectGapsFoldsAndSkippedDays(t *testing.T) {
	for _, test := range []struct {
		name, zone, first, through, last, next string
		hour, minute                           int
		count                                  int64
	}{
		{"new-york-gap", "America/New_York", "2024-03-09T07:30:00Z", "2024-03-11T06:30:00Z", "2024-03-11T06:30:00Z", "2024-03-12T06:30:00Z", 2, 30, 2},
		{"new-york-fold", "America/New_York", "2024-11-02T05:30:00Z", "2024-11-03T06:45:00Z", "2024-11-03T05:30:00Z", "2024-11-04T06:30:00Z", 1, 30, 2},
		{"lord-howe-fold", "Australia/Lord_Howe", "2024-04-05T14:45:00Z", "2024-04-06T15:30:00Z", "2024-04-06T14:45:00Z", "2024-04-07T15:15:00Z", 1, 45, 2},
		{"lord-howe-gap", "Australia/Lord_Howe", "2024-10-04T15:45:00Z", "2024-10-06T15:15:00Z", "2024-10-06T15:15:00Z", "2024-10-07T15:15:00Z", 2, 15, 2},
		{"apia-skipped-day", "Pacific/Apia", "2011-12-29T22:00:00Z", "2011-12-30T22:00:00Z", "2011-12-30T22:00:00Z", "2011-12-31T22:00:00Z", 12, 0, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			rule := scheduleTestCalendar(ScheduleDaily, test.zone, test.hour, test.minute, 0)
			first := scheduleTestTime(t, test.first)
			due, err := summarizeScheduleDue(rule, first, scheduleTestTime(t, test.through))
			if err != nil || due.Count != test.count || !due.First.Equal(first) ||
				!due.Last.Equal(scheduleTestTime(t, test.last)) || !due.Next.Equal(scheduleTestTime(t, test.next)) ||
				!due.Penultimate.Equal(first) {
				t.Fatalf("calendar summary counted an absent/repeated occurrence: %+v, %v", due, err)
			}
		})
	}
}

func TestPersistedCalendarSummaryHasAnExactBoundAndNoPartialResult(t *testing.T) {
	rule := scheduleTestCalendar(ScheduleDaily, "UTC", 12, 0, 0)
	first := scheduleTestTime(t, "2000-01-01T12:00:00Z")
	last := first.AddDate(0, 0, ScheduleCalendarMisfireLimit-1)
	due, err := summarizeScheduleDue(rule, first, last)
	if err != nil || due.Count != ScheduleCalendarMisfireLimit || !due.Last.Equal(last) {
		t.Fatalf("exact calendar bound rejected: count=%d error=%v", due.Count, err)
	}
	due, err = summarizeScheduleDue(rule, first, last.AddDate(0, 0, 1))
	if !errors.Is(err, errCalendarMisfireLimit) || due != (scheduleDueRange{}) {
		t.Fatalf("oversized calendar range returned an estimate or partial result: %+v error=%v", due, err)
	}
}

func TestPersistedScheduleRejectsInvalidPositionAndUnrepresentableCeiling(t *testing.T) {
	anchor := scheduleTestTime(t, "2026-01-01T00:00:00Z")
	rule := scheduleTestInterval(anchor, ScheduleTicksPerSecond)
	for _, first := range []time.Time{anchor.Add(time.Second + time.Microsecond), anchor.Add(time.Second + 100*time.Nanosecond)} {
		if _, err := summarizeScheduleDue(rule, first, first.Add(time.Second)); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("off-schedule storage position was accepted: %s, %v", first, err)
		}
	}
	end := scheduleTestTime(t, "9999-12-31T23:59:59.999999900Z")
	if _, err := ceilScheduleTime(end); !errors.Is(err, ErrScheduleRange) {
		t.Fatalf("ceiling overflow was not rejected: %v", err)
	}
	last := scheduleTestTime(t, "9999-12-31T23:59:59Z")
	if _, err := summarizeScheduleDue(rule, last, last); !errors.Is(err, ErrScheduleRange) {
		t.Fatalf("interval next-instant overflow was not rejected: %v", err)
	}
	if _, err := summarizeScheduleDue(ScheduleRule{Kind: ScheduleStartup}, anchor, anchor); !errors.Is(err, ErrScheduleEvent) {
		t.Fatalf("startup event was accepted as a timed range: %v", err)
	}
}
