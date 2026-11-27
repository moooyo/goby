package tasks

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/moooyo/goby/internal/library"
)

var otherSystemEventDeferralKeys = [...]string{
	library.TaskOnlineImagesKey, library.TaskLocalMetadataIngestKey, library.TaskOnlineMetadataIngestKey, library.TaskNFORecoveryKey,
}

const maxSystemEventDeferrals = len(mediaAnalysisTaskRotation) + len(otherSystemEventDeferralKeys)

// Selectable analysis tasks may defer every startup rule and one timed rule in
// a pass. Event dispatch defers at most one rule per eligible task in that pass.
const MaxAnalysisDeferrals = len(analysisSelectionTaskKeys)*(MaxTriggers+1) + maxSystemEventDeferrals

func preservesOverlappingSystemEvent(key string) bool {
	return isMediaAnalysisExecution(key) || slices.Contains(otherSystemEventDeferralKeys[:], key)
}

type AnalysisDeferral struct {
	TaskID    string
	TaskKey   string
	TriggerID string
	Source    string
	// BudgetExhausted differentiates a failed admission from an active overlap.
	BudgetExhausted bool
}

type AnalysisDeferrals struct{ Items []AnalysisDeferral }

func (*AnalysisDeferrals) Error() string { return "analysis admissions are deferred" }

func (d *AnalysisDeferrals) Is(target error) bool {
	for _, entry := range d.Items {
		if entry.BudgetExhausted && target == library.ErrAnalysisAdmissionBudget ||
			!entry.BudgetExhausted && target == ErrActiveRunConflict {
			return true
		}
	}
	return false
}

type analysisAdmissionDeferred struct {
	AnalysisDeferral
	cause error
}

func (d *analysisAdmissionDeferred) Error() string { return d.Unwrap().Error() }

func (d *analysisAdmissionDeferred) Unwrap() error {
	if d.cause != nil {
		return d.cause
	}
	return ErrActiveRunConflict
}

func deferAnalysisBudget(err error, definition Definition, trigger Trigger, source string) error {
	if isAnalysisTask(definition.Key) && errors.Is(err, library.ErrAnalysisAdmissionBudget) {
		return &analysisAdmissionDeferred{AnalysisDeferral: AnalysisDeferral{
			TaskID: definition.ID, TaskKey: definition.Key, TriggerID: trigger.ID, Source: source, BudgetExhausted: true,
		}, cause: err}
	}
	return err
}

// Classify only after the owned transaction returns: a failed rollback can
// carry an ownership failure alongside the callback's otherwise retryable error.
func scheduledAnalysisDeferral(err error) (AnalysisDeferral, bool) {
	var deferred *analysisAdmissionDeferred
	if !errors.Is(err, library.ErrUnavailable) && errors.As(err, &deferred) {
		return deferred.AnalysisDeferral, true
	}
	return AnalysisDeferral{}, false
}

// Retry clocks are coordinator-local; skipping an admission does not otherwise
// alter the persisted schedule, cursor, or occurrence receipts.
type analysisAdmissionBackoff struct {
	excluded func() []string
	onBudget func(AnalysisDeferral)
}

func (b analysisAdmissionBackoff) exclusions() []string {
	if b.excluded == nil {
		return nil
	}
	return b.excluded()
}

func (b analysisAdmissionBackoff) blockedStartupTasks() map[string]bool {
	blocked := make(map[string]bool, len(analysisSelectionTaskKeys))
	for _, id := range b.exclusions() {
		blocked[id] = true
	}
	return blocked
}

func (b analysisAdmissionBackoff) deferOccurrence(items []AnalysisDeferral, entry AnalysisDeferral) ([]AnalysisDeferral, error) {
	if entry.BudgetExhausted && b.onBudget != nil {
		b.onBudget(entry)
	}
	return addAnalysisDeferral(items, entry)
}

// An exhausted protected transaction can outlive the scheduler's shorter
// context. Yield instead of starting more database work with an expired context.
func yieldAfterAnalysisBudget(ctx context.Context, items []AnalysisDeferral) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded) && slices.ContainsFunc(items, func(entry AnalysisDeferral) bool {
		return entry.BudgetExhausted
	})
}

func dispatchSchedulePass(ctx context.Context, limit, maxDeferrals int, backoff analysisAdmissionBackoff, dispatch func(context.Context, ...string) (bool, error)) (bool, error) {
	processed := false
	deferred := []AnalysisDeferral{}
	excluded := backoff.exclusions()
	for handled := 0; handled < limit; {
		changed, err := dispatch(ctx, excluded...)
		if entry, ok := scheduledAnalysisDeferral(err); ok {
			if slices.Contains(excluded, entry.TaskID) || len(deferred) >= maxDeferrals {
				return processed, ErrInconsistent
			}
			excluded = append(excluded, entry.TaskID)
			deferred, err = backoff.deferOccurrence(deferred, entry)
			if err != nil {
				return processed, err
			}
			if yieldAfterAnalysisBudget(ctx, deferred) {
				break
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

func addAnalysisDeferral(values []AnalysisDeferral, value AnalysisDeferral) ([]AnalysisDeferral, error) {
	for _, existing := range values {
		if existing.TaskID == value.TaskID && existing.TriggerID == value.TriggerID && existing.Source == value.Source {
			return values, nil
		}
	}
	if len(values) >= MaxAnalysisDeferrals {
		return nil, ErrInconsistent
	}
	return append(values, value), nil
}

func analysisDeferralResult(values []AnalysisDeferral) error {
	if len(values) == 0 {
		return nil
	}
	return &AnalysisDeferrals{Items: append([]AnalysisDeferral{}, values...)}
}

func splitAnalysisDeferrals(err error) ([]AnalysisDeferral, error) {
	if err == nil {
		return nil, nil
	}
	var deferred *AnalysisDeferrals
	if !errors.Is(err, library.ErrUnavailable) && errors.As(err, &deferred) {
		return append([]AnalysisDeferral{}, deferred.Items...), nil
	}
	return nil, err
}

// Retry only the previously deferred startup rules. Already consumed startup
// occurrences and unrelated schedule timestamps are not rewritten each tick.
func (s *Store) retryAnalysisStartups(ctx context.Context, startupAt time.Time, pending []AnalysisDeferral, backoff analysisAdmissionBackoff) error {
	if len(pending) > len(analysisSelectionTaskKeys)*MaxTriggers {
		return ErrInconsistent
	}
	deferred := []AnalysisDeferral{}
	blockedTasks := backoff.blockedStartupTasks()
	for index, entry := range pending {
		if entry.Source != "startup" || !isAnalysisTask(entry.TaskKey) {
			return ErrInconsistent
		}
		var err error
		if budget, blocked := blockedTasks[entry.TaskID]; blocked {
			entry.BudgetExhausted = budget
			deferred, err = addAnalysisDeferral(deferred, entry)
		} else if cause := s.initializeSchedule(ctx, entry.TriggerID, entry.TaskKey, startupAt); cause != nil {
			found, ok := scheduledAnalysisDeferral(cause)
			if !ok {
				return cause
			}
			blockedTasks[found.TaskID] = found.BudgetExhausted
			deferred, err = backoff.deferOccurrence(deferred, found)
			if err == nil && yieldAfterAnalysisBudget(ctx, deferred) {
				deferred = append(deferred, pending[index+1:]...)
				break
			}
		}
		if err != nil {
			return err
		}
	}
	return analysisDeferralResult(deferred)
}

// DeferredAnalyses is a bounded diagnostic snapshot, never a successful run or
// an occurrence receipt. The coordinator retries these unconsumed inputs.
func (m *Manager) DeferredAnalyses() []AnalysisDeferral {
	if m == nil {
		return []AnalysisDeferral{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AnalysisDeferral{}, m.analysisDeferrals...)
}
