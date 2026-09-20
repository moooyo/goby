package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// InitializeSystemEvents is retried with the same captured lifecycle clock.
// Ordinary catalog/settings events were recorded in their source transaction;
// startup is the sole event created by the task manager itself.
func (s *Store) InitializeSystemEvents(ctx context.Context, startupAt time.Time) error {
	if !supportedScheduleInstant(startupAt) || startupAt.IsZero() {
		return ErrInvalidInput
	}
	return s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return systemevents.RecordStartup(tx.Exec, startupAt.UTC().Format(time.RFC3339Nano))
	})
}

// DispatchSystemEvents consumes a bounded number of trigger ranges. The signal
// is never deleted: independent rules have independent durable cursors. A
// callback loss, process crash, or retry cannot lose or execute a range twice.
func (s *Store) DispatchSystemEvents(ctx context.Context, limit int) (bool, error) {
	if limit < 1 || limit > MaxPageLimit {
		return false, ErrInvalidInput
	}
	processed := false
	deferred := []AnalysisDeferral{}
	excluded := []string{}
	for handled := 0; handled < limit; {
		changed, err := s.dispatchSystemEvent(ctx, excluded...)
		var blocked *analysisAdmissionDeferred
		if errors.As(err, &blocked) {
			if slices.Contains(excluded, blocked.TaskID) || len(excluded) >= 2 {
				return processed, ErrInconsistent
			}
			excluded = append(excluded, blocked.TaskID)
			deferred, err = addAnalysisDeferral(deferred, blocked.AnalysisDeferral)
			if err != nil {
				return processed, err
			}
			continue
		}
		if err != nil {
			return processed, err
		}
		if !changed {
			break
		}
		processed = true
		handled++
	}
	return processed, analysisDeferralResult(deferred)
}

func (s *Store) dispatchSystemEvent(ctx context.Context, excluded ...string) (bool, error) {
	processed := false
	err := s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		definitions := make(map[string]Definition)
		ids := make([]string, 0)
		for _, key := range s.executorKeys() {
			definition, err := s.lockScheduledDefinition(tx, key)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if slices.Contains(excluded, definition.ID) {
				continue
			}
			definitions[definition.ID] = definition
			ids = append(ids, definition.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		var trigger Trigger
		err := decodeRow(tx.QueryRow(`SELECT to_jsonb(t) FROM task_triggers t
            JOIN task_system_events e ON e.name=t.system_event
            WHERE t.task_id=ANY($1::text[]) AND t.kind='system_event'
                AND t.retired_at IS NULL AND t.calculation_error=''
                AND t.last_event_sequence < e.sequence
            ORDER BY e.occurred_at,t.created_at,t.id LIMIT 1 FOR UPDATE OF t`, ids), &trigger)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ValidateSchedule(triggerScheduleRule(trigger)); err != nil {
			return pauseSchedule(tx, trigger.ID, err)
		}
		var sequence int64
		var occurred time.Time
		if err := tx.QueryRow(`SELECT sequence,occurred_at FROM task_system_events WHERE name=$1`, *trigger.SystemEvent).Scan(&sequence, &occurred); err != nil {
			return err
		}
		if sequence <= trigger.LastEventSequence {
			return ErrInconsistent
		}
		// Occurrence identity is microsecond-precise. Preserve the actual source
		// clock separately while avoiding a collision from same-tick commits.
		due := occurred.UTC()
		if trigger.LastDueAt != nil && !due.After(*trigger.LastDueAt) {
			due = trigger.LastDueAt.Add(time.Microsecond)
		}
		if err := s.admitScheduledOccurrence(tx, definitions[trigger.TaskID], trigger, due, "system_event"); err != nil {
			return err
		}
		var runID, disposition string
		if err := tx.QueryRow(`SELECT run_id,disposition FROM task_occurrences
            WHERE trigger_id=$1 AND schedule_revision=$2 AND due_at=$3`, trigger.ID, trigger.ScheduleRevision, due).Scan(&runID, &disposition); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_system_event_receipts
            (trigger_id,task_id,schedule_revision,system_event,first_sequence,last_sequence,occurred_at,run_id,disposition)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, trigger.ID, trigger.TaskID, trigger.ScheduleRevision,
			*trigger.SystemEvent, trigger.LastEventSequence+1, sequence, occurred, runID, disposition); err != nil {
			return fmt.Errorf("record task event range: %w", err)
		}
		if _, err := tx.Exec(`UPDATE task_triggers SET last_event_sequence=$2,last_due_at=$3,
            updated_at=clock_timestamp() WHERE id=$1`, trigger.ID, sequence, due); err != nil {
			return err
		}
		processed = true
		return nil
	})
	return processed, err
}
