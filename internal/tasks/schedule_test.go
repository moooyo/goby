package tasks

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func scheduleTestPointer[T any](value T) *T { return &value }

func scheduleTestTime(t *testing.T, text string) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatalf("parse fixed schedule instant: %v", err)
	}
	return value
}

func scheduleTestInterval(anchor time.Time, ticks int64) ScheduleRule {
	return ScheduleRule{Kind: ScheduleInterval, AnchorAt: &anchor, IntervalTicks: &ticks}
}

func scheduleTestCalendar(kind ScheduleKind, zone string, hour, minute int, weekday time.Weekday) ScheduleRule {
	rule := ScheduleRule{Kind: kind, Timezone: zone,
		TimeOfDayTicks: scheduleTestPointer(int64(hour*3600+minute*60) * ScheduleTicksPerSecond)}
	if kind == ScheduleWeekly {
		rule.DayOfWeek = scheduleTestPointer(int(weekday))
	}
	return rule
}

func TestScheduleCheckedTicksAndNativeRuntimeBounds(t *testing.T) {
	for _, value := range []int64{0, 1, ScheduleTicksPerSecond, ScheduleMaxDurationTicks} {
		duration, err := ScheduleTicksDuration(value)
		if err != nil || int64(duration) != value*100 {
			t.Fatalf("ticks %d returned duration %d, error %v", value, duration, err)
		}
	}
	for _, value := range []int64{-1, math.MinInt64, ScheduleMaxDurationTicks + 1, math.MaxInt64} {
		if duration, err := ScheduleTicksDuration(value); duration != 0 || !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("overflowing ticks %d returned duration %d, error %v", value, duration, err)
		}
	}
	for _, value := range []*int64{nil, scheduleTestPointer(int64(0))} {
		if duration, err := ScheduleRuntimeLimit(ScheduleRule{MaxRuntimeTicks: value}); err != nil || duration != 0 {
			t.Fatalf("absent/zero runtime is not unlimited: %s, %v", duration, err)
		}
	}
	for _, value := range []int64{ScheduleMinRuntimeTicks, ScheduleMaxDurationTicks} {
		if duration, err := ScheduleRuntimeLimit(ScheduleRule{MaxRuntimeTicks: &value}); err != nil || duration <= 0 {
			t.Fatalf("valid runtime bound %d returned %s, %v", value, duration, err)
		}
	}
	for _, value := range []int64{-1, ScheduleMinRuntimeTicks - 1, ScheduleMaxDurationTicks + 1, math.MaxInt64} {
		if _, err := ScheduleRuntimeLimit(ScheduleRule{MaxRuntimeTicks: &value}); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("invalid runtime %d returned %v", value, err)
		}
	}
}

func TestScheduleValidatesKindSpecificPresenceAndRanges(t *testing.T) {
	anchor := scheduleTestTime(t, "2026-09-10T12:00:00Z")
	interval := scheduleTestInterval(anchor, ScheduleTicksPerSecond)
	daily := scheduleTestCalendar(ScheduleDaily, "UTC", 0, 0, 0)
	weekly := scheduleTestCalendar(ScheduleWeekly, "UTC", 23, 59, time.Saturday)
	for _, rule := range []ScheduleRule{interval, daily, weekly, {Kind: ScheduleStartup},
		scheduleTestInterval(time.Time{}, ScheduleTicksPerSecond)} {
		if err := ValidateSchedule(rule); err != nil {
			t.Fatalf("valid native rule rejected: %+v: %v", rule, err)
		}
	}
	for _, test := range []struct {
		name   string
		base   ScheduleRule
		change func(*ScheduleRule)
	}{
		{"unknown kind", daily, func(rule *ScheduleRule) { rule.Kind = "system" }},
		{"wire kind is not native", daily, func(rule *ScheduleRule) { rule.Kind = "DailyTrigger" }},
		{"missing anchor", interval, func(rule *ScheduleRule) { rule.AnchorAt = nil }},
		{"missing interval", interval, func(rule *ScheduleRule) { rule.IntervalTicks = nil }},
		{"short interval", interval, func(rule *ScheduleRule) { rule.IntervalTicks = scheduleTestPointer(ScheduleMinIntervalTicks - 1) }},
		{"overflow interval", interval, func(rule *ScheduleRule) { rule.IntervalTicks = scheduleTestPointer(ScheduleMaxDurationTicks + 1) }},
		{"interval with calendar timezone", interval, func(rule *ScheduleRule) { rule.Timezone = "UTC" }},
		{"interval with time of day", interval, func(rule *ScheduleRule) { rule.TimeOfDayTicks = scheduleTestPointer(int64(0)) }},
		{"interval with weekday", interval, func(rule *ScheduleRule) { rule.DayOfWeek = scheduleTestPointer(0) }},
		{"daily missing midnight", daily, func(rule *ScheduleRule) { rule.TimeOfDayTicks = nil }},
		{"daily negative clock", daily, func(rule *ScheduleRule) { rule.TimeOfDayTicks = scheduleTestPointer(int64(-1)) }},
		{"daily twenty-four-hour clock", daily, func(rule *ScheduleRule) { rule.TimeOfDayTicks = scheduleTestPointer(int64(ScheduleTicksPerDay)) }},
		{"daily with weekday", daily, func(rule *ScheduleRule) { rule.DayOfWeek = scheduleTestPointer(0) }},
		{"calendar with interval", daily, func(rule *ScheduleRule) { rule.IntervalTicks = scheduleTestPointer(ScheduleTicksPerSecond) }},
		{"calendar with anchor", daily, func(rule *ScheduleRule) { rule.AnchorAt = &anchor }},
		{"weekly missing weekday", weekly, func(rule *ScheduleRule) { rule.DayOfWeek = nil }},
		{"weekly negative weekday", weekly, func(rule *ScheduleRule) { rule.DayOfWeek = scheduleTestPointer(-1) }},
		{"weekly excessive weekday", weekly, func(rule *ScheduleRule) { rule.DayOfWeek = scheduleTestPointer(7) }},
		{"startup with timezone", ScheduleRule{Kind: ScheduleStartup}, func(rule *ScheduleRule) { rule.Timezone = "UTC" }},
		{"startup with calendar clock", ScheduleRule{Kind: ScheduleStartup}, func(rule *ScheduleRule) { rule.TimeOfDayTicks = scheduleTestPointer(int64(0)) }},
		{"runtime below minimum", daily, func(rule *ScheduleRule) { rule.MaxRuntimeTicks = scheduleTestPointer(ScheduleMinRuntimeTicks - 1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			rule := test.base
			test.change(&rule)
			if err := ValidateSchedule(rule); !errors.Is(err, ErrInvalidSchedule) {
				t.Fatalf("invalid native rule returned %v", err)
			}
		})
	}
}

func TestScheduleRejectsImplicitAndPathTimezoneNames(t *testing.T) {
	rule := scheduleTestCalendar(ScheduleDaily, "UTC", 12, 0, 0)
	for _, zone := range []string{"", "Local", "localtime", "posixrules", "/etc/localtime", "../UTC", "America/../UTC",
		`C:\Windows\UTC`, `America\New_York`, "America//New_York", "America/New_York/", "America/New_York\x00",
		" UTC", "UTC\n", "+08:00", "UTC+08:00", "posix/UTC", "right/UTC", strings.Repeat("a", 256), "Not_A_Real_Zone"} {
		rule.Timezone = zone
		if err := ValidateSchedule(rule); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("timezone %q returned %v", zone, err)
		}
	}
	for _, zone := range []string{"UTC", "Etc/UTC", "Etc/GMT+5", "America/New_York", "Australia/Lord_Howe", "Pacific/Apia", "Asia/Kathmandu"} {
		rule.Timezone = zone
		if err := ValidateSchedule(rule); err != nil {
			t.Fatalf("named IANA zone %q unavailable: %v", zone, err)
		}
	}
}

func TestScheduleIntervalAnchorIsAnInstantAndNextIsStrict(t *testing.T) {
	anchor := time.Date(2026, time.September, 10, 12, 0, 0, 17, time.FixedZone("Explicit input offset", 90*60))
	rule := scheduleTestInterval(anchor, ScheduleTicksPerSecond+1)
	period := time.Second + 100*time.Nanosecond
	for _, test := range []struct {
		name  string
		after time.Time
		want  time.Time
	}{
		{"before anchor", anchor.Add(-time.Nanosecond), anchor.UTC()},
		{"at anchor", anchor, anchor.UTC().Add(period)},
		{"between instants", anchor.Add(period - time.Nanosecond), anchor.UTC().Add(period)},
		{"at an exact due instant", anchor.Add(3 * period), anchor.UTC().Add(4 * period)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Next(rule, test.after)
			if err != nil || !got.Equal(test.want) || !got.After(test.after) || got.Location() != time.UTC {
				t.Fatalf("Next = %s, %v; want strict UTC %s", got, err, test.want)
			}
		})
	}
}

func TestScheduleCalendarDSTGapsFoldsAndSkippedDates(t *testing.T) {
	for _, test := range []struct {
		name, zone   string
		kind         ScheduleKind
		hour, minute int
		weekday      time.Weekday
		after, want  string
	}{
		{"New York missing 02:30", "America/New_York", ScheduleDaily, 2, 30, 0, "2024-03-09T07:30:00Z", "2024-03-11T06:30:00Z"},
		{"New York earlier fold", "America/New_York", ScheduleDaily, 1, 30, 0, "2024-11-03T04:00:00Z", "2024-11-03T05:30:00Z"},
		{"New York exact earlier fold skips later fold", "America/New_York", ScheduleDaily, 1, 30, 0, "2024-11-03T05:30:00Z", "2024-11-04T06:30:00Z"},
		{"New York cursor inside later fold", "America/New_York", ScheduleDaily, 1, 30, 0, "2024-11-03T06:15:00Z", "2024-11-04T06:30:00Z"},
		{"New York spring preserves wall hour", "America/New_York", ScheduleDaily, 9, 0, 0, "2024-03-09T14:00:00Z", "2024-03-10T13:00:00Z"},
		{"New York fall preserves wall hour", "America/New_York", ScheduleDaily, 9, 0, 0, "2024-11-02T13:00:00Z", "2024-11-03T14:00:00Z"},
		{"New York weekly missing Sunday", "America/New_York", ScheduleWeekly, 2, 30, time.Sunday, "2024-03-03T07:30:00Z", "2024-03-17T06:30:00Z"},
		{"Lord Howe thirty-minute gap", "Australia/Lord_Howe", ScheduleDaily, 2, 15, 0, "2024-10-05T15:00:00Z", "2024-10-06T15:15:00Z"},
		{"Lord Howe earlier thirty-minute fold", "Australia/Lord_Howe", ScheduleDaily, 1, 45, 0, "2024-04-06T14:00:00Z", "2024-04-06T14:45:00Z"},
		{"Lord Howe skips second fold occurrence", "Australia/Lord_Howe", ScheduleDaily, 1, 45, 0, "2024-04-06T15:00:00Z", "2024-04-07T15:15:00Z"},
		{"Lord Howe weekly exact earlier fold", "Australia/Lord_Howe", ScheduleWeekly, 1, 45, time.Sunday, "2024-04-06T14:45:00Z", "2024-04-13T15:15:00Z"},
		{"Apia skips entire local date", "Pacific/Apia", ScheduleDaily, 12, 0, 0, "2011-12-29T22:00:00Z", "2011-12-30T22:00:00Z"},
		{"Apia missing Friday skips a week", "Pacific/Apia", ScheduleWeekly, 12, 0, time.Friday, "2011-12-29T23:00:00Z", "2012-01-05T22:00:00Z"},
		{"Kathmandu named quarter-hour offset", "Asia/Kathmandu", ScheduleDaily, 9, 0, 0, "2026-09-10T00:00:00Z", "2026-09-10T03:15:00Z"},
		{"Native Sunday is zero", "UTC", ScheduleWeekly, 9, 0, time.Sunday, "2026-09-10T00:00:00Z", "2026-09-13T09:00:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rule := scheduleTestCalendar(test.kind, test.zone, test.hour, test.minute, test.weekday)
			after, want := scheduleTestTime(t, test.after), scheduleTestTime(t, test.want)
			got, err := Next(rule, after)
			if err != nil || !got.Equal(want) || got.Location() != time.UTC || !got.After(after) {
				t.Fatalf("Next = %s, %v; want %s", got, err, want)
			}
		})
	}
}

func TestScheduleCalendarRetainsHundredNanosecondClockPrecision(t *testing.T) {
	start := scheduleTestTime(t, "2026-09-10T00:00:00Z")
	rule := ScheduleRule{Kind: ScheduleDaily, Timezone: "UTC", TimeOfDayTicks: scheduleTestPointer(int64(1))}
	for _, test := range []struct {
		after, want time.Time
	}{
		{start, start.Add(100 * time.Nanosecond)},
		{start.Add(99 * time.Nanosecond), start.Add(100 * time.Nanosecond)},
		{start.Add(100 * time.Nanosecond), start.AddDate(0, 0, 1).Add(100 * time.Nanosecond)},
	} {
		got, err := Next(rule, test.after)
		if err != nil || !got.Equal(test.want) {
			t.Fatalf("subsecond calendar Next = %s, %v; want %s", got, err, test.want)
		}
	}
	rule.TimeOfDayTicks = scheduleTestPointer(int64(ScheduleTicksPerDay - 1))
	want := scheduleTestTime(t, "2026-09-10T23:59:59.9999999Z")
	if got, err := Next(rule, start); err != nil || !got.Equal(want) {
		t.Fatalf("last tick of local day = %s, %v; want %s", got, err, want)
	}
}

func TestSchedulePreviewUsesStrictCalendarOccurrences(t *testing.T) {
	rule := scheduleTestCalendar(ScheduleDaily, "America/New_York", 1, 30, 0)
	after := scheduleTestTime(t, "2024-11-02T05:30:00Z")
	values, err := Preview(rule, after, 3)
	want := []string{"2024-11-03T05:30:00Z", "2024-11-04T06:30:00Z", "2024-11-05T06:30:00Z"}
	if err != nil || len(values) != len(want) {
		t.Fatalf("calendar preview = %v, %v", values, err)
	}
	for index, value := range values {
		if !value.Equal(scheduleTestTime(t, want[index])) || !value.After(after) || value.Location() != time.UTC {
			t.Fatalf("preview occurrence %d = %s, want %s", index, value, want[index])
		}
		after = value
	}
	for _, count := range []int{-1, 0, ScheduleMaxPreview + 1, math.MaxInt} {
		if values, err := Preview(rule, after, count); values != nil || !errors.Is(err, ErrSchedulePreviewLimit) {
			t.Fatalf("unbounded preview %d returned %v, %v", count, values, err)
		}
	}
	if values, err := Preview(rule, after, ScheduleMaxPreview); err != nil || len(values) != ScheduleMaxPreview {
		t.Fatalf("maximum bounded preview = %v, %v", values, err)
	}
}

func TestScheduleStartupCannotBecomeADueTimestamp(t *testing.T) {
	rule := ScheduleRule{Kind: ScheduleStartup, MaxRuntimeTicks: scheduleTestPointer(ScheduleMinRuntimeTicks)}
	if err := ValidateSchedule(rule); err != nil {
		t.Fatal(err)
	}
	when := scheduleTestTime(t, "2026-09-10T00:00:00Z")
	if next, err := Next(rule, when); !next.IsZero() || !errors.Is(err, ErrScheduleEvent) {
		t.Fatalf("startup timed result = %s, %v", next, err)
	}
	if preview, err := Preview(rule, when, 1); preview != nil || !errors.Is(err, ErrScheduleEvent) {
		t.Fatalf("startup preview = %v, %v", preview, err)
	}
	if summary, err := MissedIntervals(rule, when, when); summary != (MissedIntervalSummary{}) || !errors.Is(err, ErrScheduleEvent) {
		t.Fatalf("startup missed interval = %+v, %v", summary, err)
	}
}

func TestScheduleMissedIntervalsIncludesThroughAndExcludesAfter(t *testing.T) {
	anchor := scheduleTestTime(t, "2026-09-10T12:00:00Z")
	rule := scheduleTestInterval(anchor, 5*ScheduleTicksPerSecond)
	for _, test := range []struct {
		name              string
		after, through    time.Time
		count             int64
		first, last, next time.Time
	}{
		{"before first occurrence", anchor.Add(-10 * time.Second), anchor.Add(-time.Nanosecond), 0, time.Time{}, time.Time{}, anchor},
		{"anchor included", anchor.Add(-time.Nanosecond), anchor, 1, anchor, anchor, anchor.Add(5 * time.Second)},
		{"exact through included", anchor, anchor.Add(10 * time.Second), 2, anchor.Add(5 * time.Second), anchor.Add(10 * time.Second), anchor.Add(15 * time.Second)},
		{"non-due end", anchor.Add(-time.Second), anchor.Add(16 * time.Second), 4, anchor, anchor.Add(15 * time.Second), anchor.Add(20 * time.Second)},
		{"empty instant range", anchor, anchor, 0, time.Time{}, time.Time{}, anchor.Add(5 * time.Second)},
		{"nanosecond before next due", anchor.Add(time.Nanosecond), anchor.Add(5*time.Second - time.Nanosecond), 0, time.Time{}, time.Time{}, anchor.Add(5 * time.Second)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := MissedIntervals(rule, test.after, test.through)
			if err != nil || got.Count != test.count || !got.FirstDue.Equal(test.first) || !got.LastDue.Equal(test.last) ||
				!got.Next.Equal(test.next) || !got.Next.After(test.through) || got.Next.Location() != time.UTC {
				t.Fatalf("missed range = %+v, %v; want count=%d first=%s last=%s next=%s", got, err, test.count, test.first, test.last, test.next)
			}
		})
	}
	if _, err := MissedIntervals(rule, anchor, anchor.Add(-time.Nanosecond)); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("reversed missed range returned %v", err)
	}
	if _, err := MissedIntervals(scheduleTestCalendar(ScheduleDaily, "UTC", 0, 0, 0), anchor, anchor); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("calendar rule accepted interval-only summary: %v", err)
	}
}

func TestScheduleIntervalArithmeticSpansTheSupportedCalendarRange(t *testing.T) {
	anchor := time.Date(1, time.January, 1, 0, 0, 0, 17, time.UTC)
	after := time.Date(9999, time.January, 1, 0, 0, 0, 16, time.UTC)
	rule := scheduleTestInterval(anchor, ScheduleTicksPerSecond)
	want := after.Add(time.Nanosecond)
	if got, err := Next(rule, after); err != nil || !got.Equal(want) {
		t.Fatalf("long-span exact phase = %s, %v; want %s", got, err, want)
	}
	anchor = time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)
	rule = scheduleTestInterval(anchor, ScheduleTicksPerSecond)
	got, err := MissedIntervals(rule, anchor, through)
	wantCount := through.Unix() - anchor.Unix()
	if err != nil || got.Count != wantCount || got.Count <= math.MaxInt32 || !got.FirstDue.Equal(anchor.Add(time.Second)) ||
		!got.LastDue.Equal(through) || !got.Next.Equal(through.Add(time.Second)) {
		t.Fatalf("large missed summary = %+v, %v; want count %d", got, err, wantCount)
	}
	anchor = scheduleTestTime(t, "2000-01-01T00:00:00Z")
	rule = scheduleTestInterval(anchor, ScheduleMaxDurationTicks)
	period := time.Duration(ScheduleMaxDurationTicks * 100)
	if got, err := Next(rule, anchor); err != nil || !got.Equal(anchor.Add(period)) {
		t.Fatalf("maximum representable duration = %s, %v", got, err)
	}
}

func TestScheduleRejectsUnrepresentableNextInstantsWithoutPartialPreview(t *testing.T) {
	anchor := scheduleTestTime(t, "2026-09-10T00:00:00Z")
	last := time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC)
	for _, rule := range []ScheduleRule{
		scheduleTestInterval(anchor, ScheduleTicksPerSecond),
		scheduleTestCalendar(ScheduleDaily, "UTC", 0, 0, 0),
		scheduleTestCalendar(ScheduleWeekly, "America/New_York", 0, 0, time.Sunday),
	} {
		if next, err := Next(rule, last); !next.IsZero() || !errors.Is(err, ErrScheduleRange) {
			t.Fatalf("unrepresentable next = %s, %v", next, err)
		}
	}
	for _, after := range []time.Time{time.Date(0, 12, 31, 23, 59, 59, 0, time.UTC), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Unix(math.MaxInt64, 0)} {
		if next, err := Next(scheduleTestInterval(anchor, ScheduleTicksPerSecond), after); !next.IsZero() || !errors.Is(err, ErrScheduleRange) {
			t.Fatalf("out-of-range cursor = %s, %v", next, err)
		}
	}
	invalidAnchor := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateSchedule(scheduleTestInterval(invalidAnchor, ScheduleTicksPerSecond)); !errors.Is(err, ErrInvalidSchedule) || !errors.Is(err, ErrScheduleRange) {
		t.Fatalf("out-of-range anchor returned %v", err)
	}
	rule := scheduleTestInterval(anchor, ScheduleTicksPerSecond)
	after := time.Date(9999, time.December, 31, 23, 59, 58, 0, time.UTC)
	if preview, err := Preview(rule, after, 2); preview != nil || !errors.Is(err, ErrScheduleRange) {
		t.Fatalf("partial preview escaped its range error: %v, %v", preview, err)
	}
	if _, err := MissedIntervals(rule, anchor, last); !errors.Is(err, ErrScheduleRange) {
		t.Fatalf("missed summary invented an unrepresentable future: %v", err)
	}
}
