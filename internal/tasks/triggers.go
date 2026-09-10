package tasks

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/library"
)

type ReplaceTriggersRequest struct {
	TaskID           string
	Revision         int64
	ScheduleTimezone string
	Triggers         []ScheduleRule
}

type TriggerPreview struct {
	Index       int         `json:"index"`
	Occurrences []time.Time `json:"occurrences"`
	Event       *string     `json:"event"`
}

type PreviewResult struct {
	ServerTime time.Time        `json:"server_time"`
	Items      []TriggerPreview `json:"items"`
}

const persistedScheduleTimezoneBytes = 128

// ReplaceTriggers replaces one complete native rule set. Retired rules remain
// available to the immutable trigger references in runs and occurrences.
func (s *Store) ReplaceTriggers(ctx context.Context, actor Actor, request ReplaceTriggersRequest) (Definition, error) {
	if request.Revision < 1 || request.Revision == math.MaxInt64 {
		return Definition{}, &ValidationError{Fields: map[string]string{"Revision": "revision must be positive and leave room for its successor"}}
	}
	// Validate without touching state, then bind the same copied input to the
	// transaction's database clock before any rule is retired.
	rules, err := prepareTriggerInput(request.TaskID, request.ScheduleTimezone, request.Triggers, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return Definition{}, err
	}
	var result Definition
	err = s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		var definition Definition
		if err := decodeRow(tx.QueryRow(`SELECT to_jsonb(d) FROM task_definitions d WHERE id = $1 FOR UPDATE`, request.TaskID), &definition); err != nil {
			return err
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		if definition.Revision != request.Revision {
			return ErrRevisionConflict
		}
		if definition.Key != LibraryScanKey {
			return ErrUnavailable
		}
		changed := changedTaskScheduleFields(definition, request.ScheduleTimezone)
		var now time.Time
		if err := tx.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
			return fmt.Errorf("read trigger replacement clock: %w", err)
		}
		now = now.UTC()
		nextTimes := make([]*time.Time, len(rules))
		ids := make([]string, len(rules))
		for index := range rules {
			if rules[index].Kind == ScheduleInterval {
				anchor := now
				rules[index].AnchorAt = &anchor
			}
			if err := ValidateSchedule(rules[index]); err != nil {
				return triggerValidationError(index, err)
			}
			if rules[index].Kind != ScheduleStartup {
				next, err := Next(rules[index], now)
				if err != nil {
					return triggerValidationError(index, err)
				}
				next, err = ceilScheduleTime(next)
				if err != nil {
					return triggerValidationError(index, err)
				}
				nextTimes[index] = &next
			}
			var err error
			ids[index], err = randomID()
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE task_triggers SET retired_at = $2, updated_at = $2
            WHERE task_id = $1 AND retired_at IS NULL`, definition.ID, now); err != nil {
			return fmt.Errorf("retire task triggers: %w", err)
		}
		if _, err := tx.Exec(`UPDATE task_definitions SET revision = revision + 1,
            schedule_timezone = $2, updated_at = $3 WHERE id = $1`, definition.ID, request.ScheduleTimezone, now); err != nil {
			return fmt.Errorf("replace task schedule revision: %w", err)
		}
		for index, rule := range rules {
			var zone any
			if rule.Timezone != "" {
				zone = rule.Timezone
			}
			if _, err := tx.Exec(`INSERT INTO task_triggers
                (id,task_id,schedule_revision,position,kind,interval_ticks,anchor_at,
                 time_of_day_ticks,day_of_week,timezone,max_runtime_ticks,next_fire_at,created_at,updated_at)
                VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`, ids[index], definition.ID,
				definition.Revision+1, index, string(rule.Kind), rule.IntervalTicks, rule.AnchorAt,
				rule.TimeOfDayTicks, rule.DayOfWeek, zone, rule.MaxRuntimeTicks, nextTimes[index], now); err != nil {
				return fmt.Errorf("insert task trigger: %w", err)
			}
		}
		if err := decodeRow(tx.QueryRow(`SELECT `+definitionProjection+` FROM task_definitions d WHERE id = $1`, definition.ID), &result); err != nil {
			return err
		}
		if err := recordTaskScheduleActivity(tx, actor, result, changed); err != nil {
			return err
		}
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return Definition{}, err
	}
	return result, nil
}

// PreviewTriggers uses the database clock but does not reserve work or write a
// rule. Its caller supplies administrator read authorization.
func (s *Store) PreviewTriggers(ctx context.Context, taskID, timezone string, input []ScheduleRule) (PreviewResult, error) {
	rules, err := prepareTriggerInput(taskID, timezone, input, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return PreviewResult{}, err
	}
	result := PreviewResult{Items: make([]TriggerPreview, 0, len(rules))}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT clock_timestamp(), EXISTS(SELECT 1 FROM task_definitions WHERE id = $1)`, taskID).
		Scan(&result.ServerTime, &exists); err != nil {
		return PreviewResult{}, fmt.Errorf("read task preview clock: %w", err)
	}
	if !exists {
		return PreviewResult{}, ErrNotFound
	}
	result.ServerTime = result.ServerTime.UTC()
	for index, rule := range rules {
		item := TriggerPreview{Index: index, Occurrences: []time.Time{}}
		if rule.Kind == ScheduleStartup {
			event := "startup"
			item.Event = &event
		} else {
			if rule.Kind == ScheduleInterval {
				anchor := result.ServerTime
				rule.AnchorAt = &anchor
			}
			occurrences, err := Preview(rule, result.ServerTime, 3)
			if err != nil {
				return PreviewResult{}, triggerValidationError(index, err)
			}
			for _, occurrence := range occurrences {
				due, err := ceilScheduleTime(occurrence)
				if err != nil {
					return PreviewResult{}, triggerValidationError(index, err)
				}
				item.Occurrences = append(item.Occurrences, due)
			}
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func prepareTriggerInput(taskID, timezone string, input []ScheduleRule, anchor time.Time) ([]ScheduleRule, error) {
	if strings.TrimSpace(taskID) == "" || len(taskID) > 128 {
		return nil, &ValidationError{Fields: map[string]string{"TaskID": "task ID is required and must be at most 128 bytes"}}
	}
	if len(timezone) > persistedScheduleTimezoneBytes {
		return nil, &ValidationError{Fields: map[string]string{"ScheduleTimezone": "timezone must be at most 128 bytes"}}
	}
	if _, err := scheduleLocation(timezone); err != nil {
		return nil, &ValidationError{Fields: map[string]string{"ScheduleTimezone": err.Error()}}
	}
	if len(input) > MaxTriggers {
		return nil, &ValidationError{Fields: map[string]string{"Triggers": "at most 32 triggers are allowed"}}
	}
	result := make([]ScheduleRule, len(input))
	for index, value := range input {
		if value.AnchorAt != nil || value.Timezone != "" {
			return nil, triggerValidationError(index, invalidSchedule("anchor and per-rule timezone are server-owned"))
		}
		rule := ScheduleRule{Kind: value.Kind, IntervalTicks: copyScheduleInt64(value.IntervalTicks),
			TimeOfDayTicks: copyScheduleInt64(value.TimeOfDayTicks), MaxRuntimeTicks: copyScheduleInt64(value.MaxRuntimeTicks)}
		if value.DayOfWeek != nil {
			day := *value.DayOfWeek
			rule.DayOfWeek = &day
		}
		if rule.Kind == ScheduleInterval {
			instant := anchor.UTC()
			rule.AnchorAt = &instant
		}
		if rule.Kind == ScheduleDaily || rule.Kind == ScheduleWeekly {
			rule.Timezone = timezone
		}
		if err := ValidateSchedule(rule); err != nil {
			return nil, triggerValidationError(index, err)
		}
		if rule.MaxRuntimeTicks != nil && *rule.MaxRuntimeTicks == 0 {
			rule.MaxRuntimeTicks = nil
		}
		result[index] = rule
	}
	return result, nil
}

func triggerValidationError(index int, err error) error {
	return &ValidationError{Fields: map[string]string{fmt.Sprintf("Triggers[%d]", index): err.Error()}}
}

func copyScheduleInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// PostgreSQL timestamps have microsecond precision, while schedule ticks are
// 100 ns. Always round an occurrence upward so storage can never make it due
// early. Subsequent calculations retain the immutable anchor and exact ticks.
func ceilScheduleTime(value time.Time) (time.Time, error) {
	value = value.UTC()
	if !supportedScheduleInstant(value) {
		return time.Time{}, ErrScheduleRange
	}
	if remainder := value.Nanosecond() % 1000; remainder != 0 {
		value = value.Add(time.Duration(1000-remainder) * time.Nanosecond)
	}
	if !supportedScheduleInstant(value) {
		return time.Time{}, ErrScheduleRange
	}
	return value, nil
}

func triggerScheduleRule(trigger Trigger) ScheduleRule {
	rule := ScheduleRule{Kind: ScheduleKind(trigger.Kind), AnchorAt: trigger.AnchorAt,
		IntervalTicks: trigger.IntervalTicks, TimeOfDayTicks: trigger.TimeOfDayTicks,
		DayOfWeek: trigger.DayOfWeek, MaxRuntimeTicks: trigger.MaxRuntimeTicks}
	if trigger.Timezone != nil {
		rule.Timezone = *trigger.Timezone
	}
	return rule
}
