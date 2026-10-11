//go:build linux

package tasks

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisSystemEventDeferralPreservesPreparationAndLockErrorsWithoutDecodingRun(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	key := library.TaskIntroAnalysisKey
	enableTestIntroLibraries(t, f, "library-1")
	run := f.start(t, key, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET analysis_input='{"LibraryIds":[7]}'::jsonb WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	var invalidInput *json.UnmarshalTypeError
	if _, err := f.store.GetRun(f.ctx, run.ID); !errors.As(err, &invalidInput) {
		t.Fatalf("test did not persist an undecodable execution payload: %v", err)
	}
	recordTestIntroRequest(t, f)
	entry := f.store.executors.entries[key]
	prepare := entry.AnalysisAdmission
	prepared := false
	var prepareFailure error
	entry.AnalysisAdmission = func(tx library.OwnedTx, request AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
		prepared = true
		if prepareFailure != nil {
			return AnalysisAdmissionBinding{}, prepareFailure
		}
		return prepare(tx, request)
	}
	f.store.executors.entries[key] = entry
	observer := &executionRefreshObserver{owner: f.owner}
	observed, err := New(f.pool, observer, f.store.executors)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"prepare", "lock", "defer"} {
		t.Run(phase, func(t *testing.T) {
			before := phase3TaskEventSnapshot(t, f.ctx, f.pool)
			prepared = false
			prepareFailure = nil
			observer.statements = nil
			injected := errors.New("injected analysis event admission failure")
			if phase == "prepare" {
				prepareFailure = injected
			}
			observer.fail = func(statement string) error {
				if strings.HasPrefix(statement, "SELECT id FROM task_runs WHERE task_id") {
					if !prepared {
						return errors.New("active analysis lock preceded admission preparation")
					}
					if phase == "lock" {
						return injected
					}
				}
				return nil
			}
			changed, err := observed.DispatchSystemEvents(f.ctx, 1)
			observer.fail = nil
			if phase == "defer" {
				var deferred *AnalysisDeferrals
				if !errors.As(err, &deferred) || len(deferred.Items) != 1 || deferred.Items[0].TaskID != run.TaskID ||
					deferred.Items[0].Source != "system_event" {
					t.Fatalf("unused malformed payload replaced known analysis deferral: %v", err)
				}
			} else if !errors.Is(err, injected) {
				t.Fatalf("known deferral hid an admission preparation or lock error: %v", err)
			}
			wantLocks := 1
			if phase == "prepare" {
				wantLocks = 0
			}
			if changed || !prepared || countScheduledStatements(observer, "SELECT id FROM task_runs WHERE task_id") != wantLocks ||
				countScheduledStatements(observer, "SELECT id,to_jsonb(r)") != 0 ||
				countScheduledStatements(observer, executionRefreshRunRead) != 0 || phase3TaskEventSnapshot(t, f.ctx, f.pool) != before {
				t.Fatalf("deferred analysis consumed its event or crossed a read boundary: %v", observer.statements)
			}
		})
	}
}

func TestAnalysisScheduledOverlapDecodesTheLockedRun(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	key := library.TaskIntroAnalysisKey
	enableTestIntroLibraries(t, f, "library-1")
	run := f.start(t, key, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	definition := f.definitions[key]
	saved, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
		Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}})
	if err != nil {
		t.Fatal(err)
	}
	startup, err := f.store.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	observer := &executionRefreshObserver{owner: f.owner}
	observed, err := New(f.pool, observer, f.store.executors)
	if err != nil {
		t.Fatal(err)
	}
	if err := observed.InitializeSchedules(f.ctx, startup); err != nil {
		t.Fatal(err)
	}
	var runID, disposition string
	if err := f.pool.QueryRow(f.ctx, `SELECT run_id,disposition FROM task_occurrences WHERE trigger_id=$1`, saved.Triggers[0].ID).
		Scan(&runID, &disposition); err != nil {
		t.Fatal(err)
	}
	if runID != run.ID || disposition != "overlap" || countScheduledStatements(observer, "SELECT id,to_jsonb(r)") != 1 ||
		countScheduledStatements(observer, executionRefreshRunRead) != 0 {
		t.Fatalf("matching analysis overlap was not compared from its locked Run: %v", observer.statements)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET analysis_input='{"LibraryIds":[7]}'::jsonb WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	before := phase3TaskEventSnapshot(t, f.ctx, f.pool)
	observer.statements = nil
	err = observed.InitializeSchedules(f.ctx, startup.Add(time.Microsecond))
	var invalidInput *json.UnmarshalTypeError
	if !errors.As(err, &invalidInput) || countScheduledStatements(observer, "SELECT id,to_jsonb(r)") != 1 ||
		countScheduledStatements(observer, executionRefreshRunRead) != 0 || phase3TaskEventSnapshot(t, f.ctx, f.pool) != before {
		t.Fatalf("invalid locked analysis payload was accepted or changed its occurrence: %v", err)
	}
}
