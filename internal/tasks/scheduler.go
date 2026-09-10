package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

// Calendar misfires are counted exactly up to this bound. A longer range is
// suspended at its original next_fire_at, never approximated or silently lost.
const ScheduleCalendarMisfireLimit = 4096

var errCalendarMisfireLimit = errors.New("calendar misfire range exceeds the exact counting limit")

type scheduleDueRange struct {
	Count       int64
	First       time.Time
	Last        time.Time
	Penultimate time.Time
	Next        time.Time
}

// ScheduleClock lets a coordinator capture one database startup instant and
// reuse it for every initialization retry. Wall-clock deadlines remain audit
// data; active run limits must use the coordinator's monotonic elapsed time.
func (s *Store) ScheduleClock(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read scheduler clock: %w", err)
	}
	if !supportedScheduleInstant(now) {
		return time.Time{}, ErrScheduleRange
	}
	return now.UTC(), nil
}

// InitializeSchedules follows scanner/run recovery. Timed rules skip every
// historical occurrence through the fixed startupAt, recording one exact range
// summary. Startup rules record a distinct event keyed by that same instant.
// Each rule commits independently; retries are safe after a partial pass.
func (s *Store) InitializeSchedules(ctx context.Context, startupAt time.Time) error {
	startupAt, err := ceilScheduleTime(startupAt)
	if err != nil {
		return err
	}
	// Only the registered library executor is runnable. Its active rule count
	// is bounded by the schema's unique positions, independently of history.
	rows, err := s.pool.Query(ctx, `SELECT t.id FROM task_triggers t
        JOIN task_definitions d ON d.id = t.task_id
        WHERE d.key = $1 AND d.enabled AND t.retired_at IS NULL
            AND t.calculation_error = '' AND t.created_at <= $2
        ORDER BY t.position, t.id LIMIT $3`, LibraryScanKey, startupAt, MaxTriggers)
	if err != nil {
		return fmt.Errorf("list startup task rules: %w", err)
	}
	ids := make([]string, 0, MaxTriggers)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("read startup task rule: %w", err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read startup task rules: %w", err)
	}
	for _, id := range ids {
		if err := s.initializeSchedule(ctx, id, startupAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) initializeSchedule(ctx context.Context, id string, startupAt time.Time) error {
	return s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		definition, err := lockScheduledDefinition(tx)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var trigger Trigger
		err = decodeRow(tx.QueryRow(`SELECT to_jsonb(t) FROM task_triggers t
            WHERE id = $1 AND task_id = $2 AND retired_at IS NULL
                AND calculation_error = '' AND created_at <= $3 FOR UPDATE`, id, definition.ID, startupAt), &trigger)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if trigger.Kind == string(ScheduleStartup) {
			if err := ValidateSchedule(triggerScheduleRule(trigger)); err != nil {
				return pauseSchedule(tx, trigger.ID, err)
			}
			if err := admitScheduledOccurrence(tx, definition, trigger, startupAt, "startup"); err != nil {
				return err
			}
			_, err := tx.Exec(`UPDATE task_triggers SET last_due_at = $2, updated_at = clock_timestamp() WHERE id = $1`, trigger.ID, startupAt)
			return err
		}
		if trigger.NextFireAt == nil {
			return pauseSchedule(tx, trigger.ID, invalidSchedule("timed trigger has no next occurrence"))
		}
		if trigger.NextFireAt.After(startupAt) {
			return nil
		}
		due, err := summarizeScheduleDue(triggerScheduleRule(trigger), *trigger.NextFireAt, startupAt)
		if err != nil {
			return pauseSchedule(tx, trigger.ID, err)
		}
		if err := recordMissedOccurrences(tx, trigger, due.First, due.Last, due.Count); err != nil {
			return err
		}
		return advanceSchedule(tx, trigger.ID, due)
	})
}

// DispatchDue processes a bounded number of rules using a fresh database clock
// per transaction. Delayed normal dispatch coalesces to its latest due instant:
// older occurrences become one exact missed summary, then the latest is either
// admitted or linked to an overlapping active run. The boolean reports any
// processed rule, including one suspended for a calculation error; it does not
// promise another due rule or a newly admitted run.
func (s *Store) DispatchDue(ctx context.Context, limit int) (bool, error) {
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit < 1 || limit > MaxPageLimit {
		return false, &ValidationError{Fields: map[string]string{"Limit": "dispatch limit must be between 1 and 200"}}
	}
	processed := false
	for range limit {
		changed, err := s.dispatchOneSchedule(ctx)
		if err != nil {
			return processed, err
		}
		if !changed {
			break
		}
		processed = true
	}
	return processed, nil
}

func (s *Store) dispatchOneSchedule(ctx context.Context) (bool, error) {
	processed := false
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		definition, err := lockScheduledDefinition(tx)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var now time.Time
		if err := tx.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
			return fmt.Errorf("read dispatch clock: %w", err)
		}
		now = now.UTC()
		var trigger Trigger
		err = decodeRow(tx.QueryRow(`SELECT to_jsonb(t) FROM task_triggers t
            WHERE task_id = $1 AND retired_at IS NULL AND calculation_error = ''
                AND kind <> 'startup' AND next_fire_at <= $2
            ORDER BY next_fire_at, position, id LIMIT 1 FOR UPDATE`, definition.ID, now), &trigger)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		processed = true
		due, err := summarizeScheduleDue(triggerScheduleRule(trigger), *trigger.NextFireAt, now)
		if err != nil {
			return pauseSchedule(tx, trigger.ID, err)
		}
		if due.Count > 1 {
			if err := recordMissedOccurrences(tx, trigger, due.First, due.Penultimate, due.Count-1); err != nil {
				return err
			}
		}
		if err := admitScheduledOccurrence(tx, definition, trigger, due.Last, "schedule"); err != nil {
			return err
		}
		return advanceSchedule(tx, trigger.ID, due)
	})
	if err != nil {
		return false, err
	}
	return processed, nil
}

// NextDue excludes suspended calculations while retaining their stored recovery
// positions. Startup is an event, and can never become a timed wakeup here.
func (s *Store) NextDue(ctx context.Context) (*time.Time, error) {
	var next *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT min(t.next_fire_at) FROM task_triggers t
        JOIN task_definitions d ON d.id = t.task_id
        WHERE d.key = $1 AND d.enabled AND t.retired_at IS NULL
            AND t.calculation_error = '' AND t.kind <> 'startup'`, LibraryScanKey).Scan(&next); err != nil {
		return nil, fmt.Errorf("read next task occurrence: %w", err)
	}
	utcPointer(&next)
	return next, nil
}

func lockScheduledDefinition(tx library.OwnedTx) (Definition, error) {
	var definition Definition
	err := decodeRow(tx.QueryRow(`SELECT to_jsonb(d) FROM task_definitions d
        WHERE key = $1 AND enabled FOR UPDATE`, LibraryScanKey), &definition)
	return definition, err
}

// The persisted due is the ceiling of an exact occurrence. Subtracting one
// microsecond recovers its exclusive predecessor without losing 100 ns ticks.
// Intervals are at least one second, so this cannot merge legal occurrences.
func summarizeScheduleDue(rule ScheduleRule, storedNext, through time.Time) (scheduleDueRange, error) {
	var result scheduleDueRange
	storedNext, through = storedNext.UTC(), through.UTC()
	if storedNext.Nanosecond()%1000 != 0 || !supportedScheduleInstant(storedNext) ||
		!supportedScheduleInstant(through) || through.Before(storedNext) {
		return result, invalidSchedule("persisted due range is invalid")
	}
	prepared, err := prepareSchedule(rule)
	if err != nil {
		return result, err
	}
	cursor := storedNext.Add(-time.Microsecond)
	if prepared.kind == ScheduleInterval {
		summary, err := MissedIntervals(rule, cursor, through)
		if err != nil {
			return result, err
		}
		result.Count, result.First, result.Last, result.Next = summary.Count, summary.FirstDue, summary.LastDue, summary.Next
		if result.Count > 1 {
			result.Penultimate = result.Last.Add(-prepared.interval)
		}
	} else {
		due, err := nextSchedule(prepared, cursor)
		if err != nil {
			return result, err
		}
		result.First = due
		for !due.After(through) {
			if result.Count == ScheduleCalendarMisfireLimit {
				return scheduleDueRange{}, errCalendarMisfireLimit
			}
			result.Count++
			result.Penultimate, result.Last = result.Last, due
			due, err = nextSchedule(prepared, due)
			if err != nil {
				return scheduleDueRange{}, err
			}
		}
		result.Next = due
	}
	if result.Count < 1 {
		return scheduleDueRange{}, invalidSchedule("persisted due has no corresponding occurrence")
	}
	for _, value := range []*time.Time{&result.First, &result.Last, &result.Next} {
		*value, err = ceilScheduleTime(*value)
		if err != nil {
			return scheduleDueRange{}, err
		}
	}
	if result.Count > 1 {
		result.Penultimate, err = ceilScheduleTime(result.Penultimate)
		if err != nil {
			return scheduleDueRange{}, err
		}
	}
	if !result.First.Equal(storedNext) || !result.Next.After(through) {
		return scheduleDueRange{}, invalidSchedule("persisted due is not on the immutable schedule")
	}
	return result, nil
}

func pauseSchedule(tx library.OwnedTx, id string, cause error) error {
	code := "invalid_schedule"
	switch {
	case errors.Is(cause, errCalendarMisfireLimit):
		code = "calendar_misfire_limit"
	case errors.Is(cause, ErrScheduleRange):
		code = "schedule_range"
	case errors.Is(cause, ErrScheduleSearchLimit):
		code = "calendar_search_limit"
	}
	// Do not clear or advance next_fire_at/last_due_at. Replacement can reset
	// the error explicitly while the original recovery position stays visible.
	if _, err := tx.Exec(`UPDATE task_triggers SET calculation_error = $2,
        updated_at = clock_timestamp() WHERE id = $1`, id, code); err != nil {
		return fmt.Errorf("suspend task schedule calculation: %w", err)
	}
	return nil
}

func advanceSchedule(tx library.OwnedTx, id string, due scheduleDueRange) error {
	if _, err := tx.Exec(`UPDATE task_triggers SET next_fire_at = $2,
        last_due_at = $3, updated_at = clock_timestamp() WHERE id = $1`, id, due.Next, due.Last); err != nil {
		return fmt.Errorf("advance task schedule: %w", err)
	}
	return nil
}

func recordMissedOccurrences(tx library.OwnedTx, trigger Trigger, first, last time.Time, count int64) error {
	if count < 1 || last.Before(first) {
		return ErrInconsistent
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO task_occurrences
        (id,task_id,trigger_id,schedule_revision,due_at,last_due_at,occurrence_count,disposition)
        VALUES ($1,$2,$3,$4,$5,$6,$7,'missed')`, id, trigger.TaskID,
		trigger.ID, trigger.ScheduleRevision, first, last, count); err != nil {
		return fmt.Errorf("record missed task occurrences: %w", err)
	}
	return nil
}

// The caller holds the definition and trigger locks, in that order. Admission,
// occurrence identity, library snapshot, and rule advancement share its fenced
// transaction; no public Start call can open a second owner transaction.
func admitScheduledOccurrence(tx library.OwnedTx, definition Definition, trigger Trigger, due time.Time, source string) error {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_occurrences
        WHERE trigger_id = $1 AND schedule_revision = $2 AND due_at = $3)`,
		trigger.ID, trigger.ScheduleRevision, due).Scan(&exists); err != nil {
		return fmt.Errorf("read task occurrence receipt: %w", err)
	}
	if exists {
		// Startup retries must not create a second run after the first one has
		// already completed (including an empty-library run).
		return nil
	}
	var runID string
	err := tx.QueryRow(`SELECT id FROM task_runs WHERE task_id = $1
        AND state IN ('pending','running','stopping') FOR UPDATE`, definition.ID).Scan(&runID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read overlapping scheduled run: %w", err)
	}
	disposition := "overlap"
	if runID == "" {
		disposition = "admitted"
		runID, err = randomID()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_runs
            (id,task_id,state,source,task_key,task_emby_key,task_name,
             trigger_id,trigger_revision,scheduled_for,max_runtime_ticks)
            VALUES ($1,$2,'pending',$3,$4,$5,$6,$7,$8,$9,$10)`, runID, definition.ID,
			source, definition.Key, definition.EmbyKey, definition.Name, trigger.ID,
			trigger.ScheduleRevision, due, trigger.MaxRuntimeTicks); err != nil {
			return fmt.Errorf("admit scheduled task run: %w", err)
		}
		children, err := tx.Exec(`INSERT INTO task_run_children (id,run_id,library_id,library_name,ordinal)
            SELECT md5($1 || ':' || id), $1, id, name,
                (row_number() OVER (ORDER BY id) - 1)::integer FROM libraries`, runID)
		if err != nil {
			return fmt.Errorf("snapshot scheduled task libraries: %w", err)
		}
		if _, err := tx.Exec(`UPDATE task_runs SET total_children = $2 WHERE id = $1`, runID, children.RowsAffected()); err != nil {
			return fmt.Errorf("record scheduled task child count: %w", err)
		}
		if _, err := refreshRun(tx, runID); err != nil {
			return err
		}
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO task_occurrences
        (id,task_id,trigger_id,schedule_revision,due_at,disposition,run_id)
        VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, definition.ID, trigger.ID, trigger.ScheduleRevision,
		due, disposition, runID); err != nil {
		return fmt.Errorf("record scheduled task admission: %w", err)
	}
	return nil
}
