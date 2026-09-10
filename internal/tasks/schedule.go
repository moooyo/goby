package tasks

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
	_ "time/tzdata" // Keep named-zone data available in minimal Linux deployments.
)

// These are native proposal boundaries, not inferred Emby trigger semantics.
const (
	ScheduleTicksPerSecond     int64 = 10_000_000
	ScheduleTicksPerDay              = 86_400 * ScheduleTicksPerSecond
	ScheduleMinIntervalTicks         = ScheduleTicksPerSecond
	ScheduleMinRuntimeTicks          = ScheduleTicksPerSecond
	ScheduleMaxDurationTicks   int64 = math.MaxInt64 / 100
	ScheduleMaxPreview               = 10
	ScheduleMinYear                  = 1
	ScheduleMaxYear                  = 9999
	ScheduleCalendarSearchDays       = 370

	scheduleNanosecondsPerTick = 100
	scheduleMaxTimezoneBytes   = 255
	scheduleMaxZoneSegments    = 64
	scheduleMaxUTCOffset       = 24 * time.Hour
)

var (
	ErrInvalidSchedule      = errors.New("invalid native task schedule")
	ErrScheduleEvent        = errors.New("event schedule has no timed occurrence")
	ErrScheduleRange        = errors.New("schedule instant is outside supported UTC years 1 through 9999")
	ErrScheduleSearchLimit  = errors.New("schedule calculation exceeded its bounded calendar search")
	ErrSchedulePreviewLimit = errors.New("schedule preview count must be between 1 and 10")
)

type ScheduleKind string

const (
	ScheduleInterval ScheduleKind = "interval"
	ScheduleDaily    ScheduleKind = "daily"
	ScheduleWeekly   ScheduleKind = "weekly"
	ScheduleStartup  ScheduleKind = "startup"
)

// ScheduleRule is the native calculation model, independent of persisted
// trigger identities and Emby Type strings. Only fields applicable to Kind may
// be populated. Calendar rules require an explicit named Timezone. AnchorAt is
// an absolute first occurrence, normalized to UTC; an interval is not measured
// from the completion of a previous run. Tick values use 100 ns units.
type ScheduleRule struct {
	Kind            ScheduleKind
	AnchorAt        *time.Time
	IntervalTicks   *int64
	TimeOfDayTicks  *int64
	DayOfWeek       *int
	Timezone        string
	MaxRuntimeTicks *int64
}

// MissedIntervalSummary describes due instants in (after, through], without
// allocating an entry per occurrence. FirstDue and LastDue are zero only when
// Count is zero; Next is always strictly after through on a successful result.
type MissedIntervalSummary struct {
	Count    int64
	FirstDue time.Time
	LastDue  time.Time
	Next     time.Time
}

type preparedSchedule struct {
	kind      ScheduleKind
	anchor    time.Time
	interval  time.Duration
	timeOfDay time.Duration
	weekday   time.Weekday
	zone      *time.Location
}

func invalidSchedule(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidSchedule, field)
}

// ScheduleTicksDuration performs a checked conversion for nonnegative native
// tick fields. Kind-specific lower bounds are checked separately.
func ScheduleTicksDuration(ticks int64) (time.Duration, error) {
	if ticks < 0 || ticks > ScheduleMaxDurationTicks {
		return 0, invalidSchedule("tick duration is negative or exceeds time.Duration")
	}
	return time.Duration(ticks * scheduleNanosecondsPerTick), nil
}

// ScheduleRuntimeLimit validates only MaxRuntimeTicks. Absence and explicit
// zero mean no limit; a positive limit must be at least one second. Call
// ValidateSchedule when the complete rule must be validated.
func ScheduleRuntimeLimit(rule ScheduleRule) (time.Duration, error) {
	if rule.MaxRuntimeTicks == nil || *rule.MaxRuntimeTicks == 0 {
		return 0, nil
	}
	if *rule.MaxRuntimeTicks < ScheduleMinRuntimeTicks {
		return 0, invalidSchedule("maximum runtime must be zero or at least one second")
	}
	value, err := ScheduleTicksDuration(*rule.MaxRuntimeTicks)
	if err != nil {
		return 0, invalidSchedule("maximum runtime exceeds time.Duration")
	}
	return value, nil
}

func supportedScheduleInstant(instant time.Time) bool {
	year := instant.UTC().Year()
	return year >= ScheduleMinYear && year <= ScheduleMaxYear
}

func scheduleLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" || name == "localtime" || name == "posixrules" || len(name) > scheduleMaxTimezoneBytes ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") ||
		strings.HasPrefix(name, "posix/") || strings.HasPrefix(name, "right/") {
		return nil, invalidSchedule("timezone must be UTC or an explicit IANA name")
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '/' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return nil, invalidSchedule("timezone contains an invalid name component")
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalidSchedule("timezone is not an available IANA name")
	}
	return zone, nil
}

func prepareSchedule(rule ScheduleRule) (preparedSchedule, error) {
	var result preparedSchedule
	if _, err := ScheduleRuntimeLimit(rule); err != nil {
		return result, err
	}
	result.kind = rule.Kind
	switch rule.Kind {
	case ScheduleStartup:
		if rule.AnchorAt != nil || rule.IntervalTicks != nil || rule.TimeOfDayTicks != nil || rule.DayOfWeek != nil || rule.Timezone != "" {
			return result, invalidSchedule("startup accepts no time, interval, weekday, or timezone")
		}
	case ScheduleInterval:
		if rule.AnchorAt == nil || rule.IntervalTicks == nil || rule.TimeOfDayTicks != nil || rule.DayOfWeek != nil || rule.Timezone != "" {
			return result, invalidSchedule("interval requires only an anchor and interval ticks")
		}
		if !supportedScheduleInstant(*rule.AnchorAt) {
			return result, fmt.Errorf("%w: interval anchor: %w", ErrInvalidSchedule, ErrScheduleRange)
		}
		if *rule.IntervalTicks < ScheduleMinIntervalTicks {
			return result, invalidSchedule("interval must be at least one second")
		}
		interval, err := ScheduleTicksDuration(*rule.IntervalTicks)
		if err != nil {
			return result, invalidSchedule("interval exceeds time.Duration")
		}
		result.anchor, result.interval = rule.AnchorAt.UTC(), interval
	case ScheduleDaily, ScheduleWeekly:
		if rule.AnchorAt != nil || rule.IntervalTicks != nil || rule.TimeOfDayTicks == nil {
			return result, invalidSchedule("calendar schedules require time-of-day ticks without an interval or anchor")
		}
		if *rule.TimeOfDayTicks < 0 || *rule.TimeOfDayTicks >= ScheduleTicksPerDay {
			return result, invalidSchedule("time-of-day ticks must be within one local day")
		}
		if rule.Kind == ScheduleDaily && rule.DayOfWeek != nil {
			return result, invalidSchedule("daily schedules have no weekday")
		}
		if rule.Kind == ScheduleWeekly {
			if rule.DayOfWeek == nil || *rule.DayOfWeek < 0 || *rule.DayOfWeek > 6 {
				return result, invalidSchedule("weekly weekday must be between Sunday 0 and Saturday 6")
			}
			result.weekday = time.Weekday(*rule.DayOfWeek)
		}
		zone, err := scheduleLocation(rule.Timezone)
		if err != nil {
			return result, err
		}
		result.timeOfDay = time.Duration(*rule.TimeOfDayTicks * scheduleNanosecondsPerTick)
		result.zone = zone
	default:
		return result, invalidSchedule("unsupported native kind")
	}
	return result, nil
}

// ValidateSchedule checks native shape, bounds, and named-zone availability.
// It performs no database operation and never executes an event or task.
func ValidateSchedule(rule ScheduleRule) error {
	_, err := prepareSchedule(rule)
	return err
}

// Next returns one UTC occurrence strictly after after. Startup is an event,
// so it returns a zero time with ErrScheduleEvent rather than a due timestamp.
func Next(rule ScheduleRule, after time.Time) (time.Time, error) {
	prepared, err := prepareSchedule(rule)
	if err != nil {
		return time.Time{}, err
	}
	return nextSchedule(prepared, after)
}

func nextSchedule(rule preparedSchedule, after time.Time) (time.Time, error) {
	if rule.kind == ScheduleStartup {
		return time.Time{}, ErrScheduleEvent
	}
	if !supportedScheduleInstant(after) {
		return time.Time{}, ErrScheduleRange
	}
	after = after.UTC()
	if rule.kind == ScheduleInterval {
		return nextInterval(rule, after)
	}
	return nextCalendar(rule, after)
}

// UnixNano and time.Sub cannot represent the whole supported calendar range.
// Keep the seconds/nanoseconds decomposition exact until the final conversion.
func scheduleNanoseconds(instant time.Time) *big.Int {
	value := new(big.Int).Mul(big.NewInt(instant.Unix()), big.NewInt(int64(time.Second)))
	return value.Add(value, big.NewInt(int64(instant.Nanosecond())))
}

func scheduleInstant(nanoseconds *big.Int) (time.Time, error) {
	seconds, remainder := new(big.Int), new(big.Int)
	seconds.DivMod(nanoseconds, big.NewInt(int64(time.Second)), remainder)
	if !seconds.IsInt64() {
		return time.Time{}, ErrScheduleRange
	}
	instant := time.Unix(seconds.Int64(), remainder.Int64()).UTC()
	if !supportedScheduleInstant(instant) {
		return time.Time{}, ErrScheduleRange
	}
	return instant, nil
}

func nextInterval(rule preparedSchedule, after time.Time) (time.Time, error) {
	if after.Before(rule.anchor) {
		return rule.anchor, nil
	}
	anchor := scheduleNanoseconds(rule.anchor)
	elapsed := new(big.Int).Sub(scheduleNanoseconds(after), anchor)
	period := big.NewInt(int64(rule.interval))
	steps := new(big.Int).Quo(elapsed, period)
	steps.Add(steps, big.NewInt(1))
	return scheduleInstant(new(big.Int).Add(anchor, steps.Mul(steps, period)))
}

// resolveCalendarOccurrence never asks time.Date to guess a gap/fold. Every
// candidate is formed from an actual zone interval's offset and round-tripped
// through the location. Gaps produce no candidate; folds retain the earliest
// UTC instant even if that occurrence is already before the caller's cursor.
func resolveCalendarOccurrence(date time.Time, clock time.Duration, zone *time.Location) (time.Time, bool, error) {
	wall := date.Add(clock)
	windowEnd := wall.Add(scheduleMaxUTCOffset)
	cursor := wall.Add(-scheduleMaxUTCOffset)
	seen := make(map[int]struct{})
	var earliest time.Time
	found := false
	for segment := 0; segment < scheduleMaxZoneSegments; segment++ {
		local := cursor.In(zone)
		_, offset := local.Zone()
		if offset < -int(scheduleMaxUTCOffset/time.Second) || offset > int(scheduleMaxUTCOffset/time.Second) {
			return time.Time{}, false, invalidSchedule("timezone offset exceeds the supported IANA range")
		}
		if _, exists := seen[offset]; !exists {
			seen[offset] = struct{}{}
			candidate := wall.Add(-time.Duration(offset) * time.Second)
			actual := candidate.In(zone)
			if actual.Year() == wall.Year() && actual.Month() == wall.Month() && actual.Day() == wall.Day() &&
				actual.Hour() == wall.Hour() && actual.Minute() == wall.Minute() && actual.Second() == wall.Second() &&
				actual.Nanosecond() == wall.Nanosecond() && (!found || candidate.Before(earliest)) {
				earliest, found = candidate.UTC(), true
			}
		}
		_, end := local.ZoneBounds()
		if end.IsZero() || end.After(windowEnd) || !cursor.Before(windowEnd) {
			return earliest, found, nil
		}
		if !end.After(cursor) {
			return time.Time{}, false, ErrScheduleSearchLimit
		}
		cursor = end.UTC()
	}
	return time.Time{}, false, ErrScheduleSearchLimit
}

func nextCalendar(rule preparedSchedule, after time.Time) (time.Time, error) {
	year, month, day := after.In(rule.zone).Date()
	// A UTC carrier represents a civil date only. AddDate advances that local
	// date, never the previous occurrence's UTC instant by a fixed 24 hours.
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	step, searched := 1, 0
	if rule.kind == ScheduleWeekly {
		searched = (int(rule.weekday) - int(date.Weekday()) + 7) % 7
		date = date.AddDate(0, 0, searched)
		step = 7
	}
	for ; searched <= ScheduleCalendarSearchDays; searched += step {
		candidate, found, err := resolveCalendarOccurrence(date, rule.timeOfDay, rule.zone)
		if err != nil {
			return time.Time{}, err
		}
		if found && candidate.After(after) {
			if !supportedScheduleInstant(candidate) {
				return time.Time{}, ErrScheduleRange
			}
			return candidate, nil
		}
		date = date.AddDate(0, 0, step)
	}
	return time.Time{}, ErrScheduleSearchLimit
}

// Preview uses the same strict-after calculation as dispatch. It returns no
// partial preview when any requested occurrence is not representable.
func Preview(rule ScheduleRule, after time.Time, count int) ([]time.Time, error) {
	if count < 1 || count > ScheduleMaxPreview {
		return nil, ErrSchedulePreviewLimit
	}
	prepared, err := prepareSchedule(rule)
	if err != nil {
		return nil, err
	}
	result := make([]time.Time, 0, count)
	for range count {
		next, err := nextSchedule(prepared, after)
		if err != nil {
			return nil, err
		}
		result = append(result, next)
		after = next
	}
	return result, nil
}

// MissedIntervals summarizes (after, through] in constant-count arithmetic.
// It does not replay missed work, advance persisted state, or apply this policy
// to calendar/event rules. Count and the next instant are checked before return.
func MissedIntervals(rule ScheduleRule, after, through time.Time) (MissedIntervalSummary, error) {
	var result MissedIntervalSummary
	prepared, err := prepareSchedule(rule)
	if err != nil {
		return result, err
	}
	if prepared.kind == ScheduleStartup {
		return result, ErrScheduleEvent
	}
	if prepared.kind != ScheduleInterval {
		return result, invalidSchedule("missed-interval summaries require an interval rule")
	}
	if !supportedScheduleInstant(after) || !supportedScheduleInstant(through) {
		return result, ErrScheduleRange
	}
	after, through = after.UTC(), through.UTC()
	if through.Before(after) {
		return result, invalidSchedule("missed-interval range ends before its exclusive start")
	}
	first, err := nextInterval(prepared, after)
	if err != nil {
		return result, err
	}
	future, err := nextInterval(prepared, through)
	if err != nil {
		return result, err
	}
	result.Next = future
	if first.After(through) {
		return result, nil
	}
	period := big.NewInt(int64(prepared.interval))
	lastNanos := new(big.Int).Sub(scheduleNanoseconds(future), period)
	count := new(big.Int).Sub(lastNanos, scheduleNanoseconds(first))
	count.Quo(count, period).Add(count, big.NewInt(1))
	if !count.IsInt64() || count.Sign() <= 0 {
		return MissedIntervalSummary{}, ErrScheduleRange
	}
	last, err := scheduleInstant(lastNanos)
	if err != nil {
		return MissedIntervalSummary{}, err
	}
	result.Count, result.FirstDue, result.LastDue = count.Int64(), first, last
	return result, nil
}
