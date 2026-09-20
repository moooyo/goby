package tasks

import (
	"context"
	"errors"
	"time"
)

// At most two analysis definitions have 32 startup rules each, plus one
// interval and one event conflict per definition in a bounded dispatch pass.
const MaxAnalysisDeferrals = 2*MaxTriggers + 4

type AnalysisDeferral struct {
	TaskID    string
	TaskKey   string
	TriggerID string
	Source    string
}

type AnalysisDeferrals struct{ Items []AnalysisDeferral }

func (*AnalysisDeferrals) Error() string {
	return "analysis admissions are deferred behind an incompatible active run"
}
func (*AnalysisDeferrals) Unwrap() error { return ErrActiveRunConflict }

type analysisAdmissionDeferred struct{ AnalysisDeferral }

func (*analysisAdmissionDeferred) Error() string {
	return "analysis occurrence is deferred behind an incompatible active run"
}
func (*analysisAdmissionDeferred) Unwrap() error { return ErrActiveRunConflict }

func addAnalysisDeferral(values []AnalysisDeferral, value AnalysisDeferral) ([]AnalysisDeferral, error) {
	for _, existing := range values {
		if existing == value {
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
	if errors.As(err, &deferred) {
		return append([]AnalysisDeferral{}, deferred.Items...), nil
	}
	return nil, err
}

// Retry only the previously deferred startup rules. Already consumed startup
// occurrences and unrelated schedule timestamps are not rewritten each tick.
func (s *Store) retryAnalysisStartups(ctx context.Context, startupAt time.Time, pending []AnalysisDeferral) error {
	if len(pending) > 2*MaxTriggers {
		return ErrInconsistent
	}
	deferred := []AnalysisDeferral{}
	blockedTasks := make(map[string]bool, 2)
	for _, entry := range pending {
		if entry.Source != "startup" || !isAnalysisTask(entry.TaskKey) {
			return ErrInconsistent
		}
		if blockedTasks[entry.TaskID] {
			var err error
			deferred, err = addAnalysisDeferral(deferred, entry)
			if err != nil {
				return err
			}
			continue
		}
		err := s.initializeSchedule(ctx, entry.TriggerID, entry.TaskKey, startupAt)
		var blocked *analysisAdmissionDeferred
		if errors.As(err, &blocked) {
			blockedTasks[blocked.TaskID] = true
			var appendErr error
			deferred, appendErr = addAnalysisDeferral(deferred, blocked.AnalysisDeferral)
			if appendErr != nil {
				return appendErr
			}
		} else if err != nil {
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
