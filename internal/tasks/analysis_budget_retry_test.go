package tasks

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestSchedulePassYieldsWhenAnalysisBudgetOutlivesSchedulerContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// Model a protected transaction that returns only after the scheduler's
	// context expires. There is no live timer dependency in this test.
	expiring := &analysisBudgetTestContext{Context: ctx}
	definition := Definition{ID: "preview", Key: library.TaskPreviewGenerationKey}
	trigger := Trigger{ID: "preview-event"}
	var deferred []AnalysisDeferral
	calls := 0
	changed, err := dispatchSchedulePass(expiring, 2, maxSystemEventDeferrals,
		analysisAdmissionBackoff{onBudget: func(entry AnalysisDeferral) { deferred = append(deferred, entry) }},
		func(context.Context, ...string) (bool, error) {
			calls++
			if calls > 1 {
				t.Fatal("dispatcher accessed the database again after yielding")
			}
			expiring.expired = true
			return false, deferAnalysisBudget(library.ErrAnalysisAdmissionBudget, definition, trigger, "system_event")
		})
	entries, splitErr := splitAnalysisDeferrals(err)
	if changed || calls != 1 || !errors.Is(err, library.ErrAnalysisAdmissionBudget) || splitErr != nil || !slices.Equal(entries, deferred) || len(entries) != 1 {
		t.Fatalf("expired schedule pass lost its scoped retry: changed=%t entries=%+v error=%v split=%v", changed, entries, err, splitErr)
	}
}

type analysisBudgetTestContext struct {
	context.Context
	expired bool
}

func (ctx *analysisBudgetTestContext) Err() error {
	if ctx.expired {
		return context.DeadlineExceeded
	}
	return ctx.Context.Err()
}

func TestSchedulePassDoesNotHideOwnershipLossDuringAnalysisRollback(t *testing.T) {
	for _, cause := range []error{library.ErrAnalysisAdmissionBudget, ErrActiveRunConflict} {
		definition := Definition{ID: "preview", Key: library.TaskPreviewGenerationKey}
		trigger := Trigger{ID: "preview-event"}
		scoped := deferAnalysisBudget(cause, definition, trigger, "system_event")
		if cause == ErrActiveRunConflict {
			scoped = &analysisAdmissionDeferred{AnalysisDeferral: AnalysisDeferral{TaskID: definition.ID, TaskKey: definition.Key, TriggerID: trigger.ID, Source: "system_event"}}
		}
		combined := errors.Join(scoped, library.ErrUnavailable)
		manager := newManagerOwnershipFixture(t, context.Background(), nil, &managerOwnershipScans{})
		changed, err := dispatchSchedulePass(manager.ctx, 1, maxSystemEventDeferrals,
			analysisAdmissionBackoff{onBudget: func(AnalysisDeferral) { t.Fatal("owner loss was deferred") }},
			func(context.Context, ...string) (bool, error) { return false, combined })
		manager.observeFailure(err)
		if changed || !errors.Is(err, library.ErrUnavailable) || !errors.Is(err, cause) || manager.ctx.Err() == nil {
			t.Fatalf("rollback lost its ownership failure: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		closeErr := manager.Close(ctx)
		cancel()
		if !errors.Is(closeErr, library.ErrUnavailable) {
			t.Fatalf("manager did not fence on owner loss: %v", closeErr)
		}
	}
}
