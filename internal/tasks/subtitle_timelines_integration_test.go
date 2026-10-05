//go:build linux

package tasks

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func newSubtitleTimelineTaskFixture(t *testing.T) *analysisTestFixture {
	t.Helper()
	f := newAnalysisTestFixture(t, 0)
	entries := make([]ExecutorRegistration, 0, len(f.store.executors.entries)+2)
	for _, entry := range f.store.executors.entries {
		entries = append(entries, entry)
	}
	for _, key := range []string{library.TaskSubtitleTimelineGenerationKey, library.TaskAudioWaveformGenerationKey} {
		entries = append(entries, ExecutorRegistration{Key: key, Name: key, Executor: &analysisTestExecutor{}})
	}
	registry, err := NewExecutorRegistry(entries...)
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = New(f.pool, f.owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{library.TaskSubtitleTimelineGenerationKey, library.TaskAudioWaveformGenerationKey} {
		definition, err := f.store.GetByKey(f.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		f.definitions[key] = definition
	}
	return f
}

func TestSubtitleTimelineSchedulesAndCancellationPreserveLateRequests(t *testing.T) {
	f := newSubtitleTimelineTaskFixture(t)
	definition := f.definitions[library.TaskSubtitleTimelineGenerationKey]
	if definition.Revision != 2 || len(definition.Triggers) != 2 {
		t.Fatalf("default subtitle timeline schedules were not installed: %+v", definition)
	}
	interval, event := definition.Triggers[0], definition.Triggers[1]
	if interval.Kind != string(ScheduleInterval) || interval.IntervalTicks == nil ||
		*interval.IntervalTicks != 86400*ScheduleTicksPerSecond || interval.AnchorAt == nil || interval.NextFireAt == nil ||
		interval.NextFireAt.Sub(*interval.AnchorAt) != 24*time.Hour || event.Kind != string(ScheduleSystemEvent) ||
		event.SystemEvent == nil || *event.SystemEvent != string(systemevents.SubtitleTimelineGenerationRequested) || event.LastEventSequence != 0 {
		t.Fatal("default subtitle timeline schedule has the wrong interval or event cursor")
	}
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := f.store.GetByKey(f.ctx, definition.Key); err != nil || !reflect.DeepEqual(after, definition) {
		t.Fatalf("reconciliation rewrote the subtitle timeline schedule: %+v %v", after, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":true}'::jsonb WHERE id='library-1';
  INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('subtitle-root','library-1','/subtitle','/','subtitle');
  INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES
   ('subtitle-first','library-1','subtitle-root','First','first','Movie',false,'first.mp4'),
   ('subtitle-waiting','library-1','subtitle-root','Waiting','waiting','Movie',false,'waiting.mp4'),
   ('subtitle-late','library-1','subtitle-root','Late','late','Movie',false,'late.mp4');
  INSERT INTO subtitle_timeline_queue(item_id) VALUES('subtitle-first'),('subtitle-waiting')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE subtitle_timeline_queue SET manual=true,actor_user_id=$1,actor_session_id=$2`, f.actor.Principal.User.ID, f.actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	run := f.start(t, library.TaskSubtitleTimelineGenerationKey, nil)
	children := f.children(t, run)
	if run.TotalChildren != 1 || len(children) != 1 || children[0].LibraryID != "library-1" {
		t.Fatalf("subtitle timeline snapshot included a disabled library: %+v %+v", run, children)
	}
	child := children[0]
	token := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := f.store.claimExecution(f.ctx, run.ID, child.ID, token); err != nil {
		t.Fatal(err)
	}
	work := executionWork(f.ctx, run, child, token)
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
		t.Fatalf("manual subtitle timeline work fence: %v", err)
	}
	if allowed, err := f.owner.SubtitleTimelineBatchCanYield(f.ctx, work.Fence); err != nil || !allowed {
		t.Fatalf("default event cannot resume a yielded subtitle timeline batch: %t %v", allowed, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET retired_at=clock_timestamp() WHERE task_id=$1 AND kind='system_event'`, definition.ID); err != nil {
		t.Fatal(err)
	}
	if allowed, err := f.owner.SubtitleTimelineBatchCanYield(f.ctx, work.Fence); err != nil || allowed {
		t.Fatalf("subtitle timeline batch yielded to a removed event trigger: %t %v", allowed, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET retired_at=NULL WHERE task_id=$1 AND kind='system_event'`, definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE subtitle_timeline_queue SET state='running',claimed_revision=1,run_id=$1,child_id=$2 WHERE item_id='subtitle-first'`, run.ID, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('subtitle-root-2','library-2','/subtitle-2','/','subtitle-2');
  INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES('subtitle-other-library','library-2','subtitle-root-2','Other','other','Movie',false,'other.mp4')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO subtitle_timeline_queue(item_id,manual,actor_user_id,actor_session_id) VALUES('subtitle-other-library',true,$1,$2)`, f.actor.Principal.User.ID, f.actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Stop(f.ctx, f.actor, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO subtitle_timeline_queue(item_id,manual,actor_user_id,actor_session_id) VALUES('subtitle-late',true,$1,$2)`, f.actor.Principal.User.ID, f.actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.finishExecution(f.ctx, run.ID, child.ID, token, context.Canceled, false); err != nil {
		t.Fatal(err)
	}
	for _, itemID := range []string{"subtitle-first", "subtitle-waiting", "subtitle-other-library", "subtitle-late"} {
		var state string
		var requested, completed int64
		if err := f.pool.QueryRow(f.ctx, `SELECT state,requested_revision,completed_revision FROM subtitle_timeline_queue WHERE item_id=$1`, itemID).Scan(&state, &requested, &completed); err != nil {
			t.Fatal(err)
		}
		if itemID == "subtitle-late" {
			if state != "pending" || requested <= completed {
				t.Fatalf("cancellation lost a late request: %s %d %d", state, requested, completed)
			}
		} else if state != "cancelled" || requested != completed {
			t.Fatalf("cancellation retained earlier work for %s: %s %d %d", itemID, state, requested, completed)
		}
	}
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled subtitle timeline fence remained live: %v", err)
	}
	recordTestAnalysisRequest(t, f, library.TaskSubtitleTimelineGenerationKey)
	if _, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.GetByKey(f.ctx, definition.Key)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.ActorKind != "system" || current.CurrentRun.TotalChildren != 1 {
		t.Fatalf("subtitle timeline event did not admit the retained request with system authority: %+v %v", current.CurrentRun, err)
	}
	recordTestAnalysisRequest(t, f, library.TaskSubtitleTimelineGenerationKey)
	if _, err := f.store.DispatchSystemEvents(f.ctx, 10); !errors.Is(err, ErrActiveRunConflict) {
		t.Fatalf("active subtitle timeline run consumed a late event: %v", err)
	}
}

func TestSubtitleTimelineSharesIntroAndAudioWaveformExecutionSlot(t *testing.T) {
	for _, activeKey := range []string{library.TaskIntroAnalysisKey, library.TaskAudioWaveformGenerationKey} {
		t.Run(activeKey, func(t *testing.T) {
			f := newSubtitleTimelineTaskFixture(t)
			if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":true,"EnableAudioWaveformGeneration":true}'::jsonb WHERE id='library-1'`); err != nil {
				t.Fatal(err)
			}
			var selection *library.AnalysisSelection
			if activeKey == library.TaskIntroAnalysisKey {
				selection = &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
			}
			active := f.start(t, activeKey, selection)
			activeChildren := f.children(t, active)
			if len(activeChildren) != 1 {
				t.Fatalf("expected one active media child: %+v", activeChildren)
			}
			activeChild := activeChildren[0]
			activeToken := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			if _, err := f.store.claimExecution(f.ctx, active.ID, activeChild.ID, activeToken); err != nil {
				t.Fatal(err)
			}
			timeline := f.start(t, library.TaskSubtitleTimelineGenerationKey, nil)
			timelineChildren := f.children(t, timeline)
			if len(timelineChildren) != 1 {
				t.Fatalf("expected one subtitle timeline child: %+v", timelineChildren)
			}
			if timeline.AnalysisInput != nil || timeline.AnalysisConfigFingerprint != "" {
				t.Fatal("subtitle timeline task inherited analysis profile invalidation")
			}
			timelineChild := timelineChildren[0]
			timelineToken := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			if _, err := f.store.claimExecution(f.ctx, timeline.ID, timelineChild.ID, timelineToken); !errors.Is(err, errAnalysisGroupBusy) {
				t.Fatalf("subtitle timeline ran concurrently with %s: %v", activeKey, err)
			}
			if err := f.store.finishExecution(f.ctx, active.ID, activeChild.ID, activeToken, nil, false); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.claimExecution(f.ctx, timeline.ID, timelineChild.ID, timelineToken); err != nil {
				t.Fatalf("subtitle timeline did not resume after %s released its slot: %v", activeKey, err)
			}
		})
	}
}

func TestSubtitleTimelineRecoveryDistinguishesStopFromShutdown(t *testing.T) {
	for _, reason := range []string{"administrator", "shutdown"} {
		t.Run(reason, func(t *testing.T) {
			f := newSubtitleTimelineTaskFixture(t)
			if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":true}'::jsonb WHERE id='library-1';
  INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('subtitle-root','library-1','/subtitle','/','subtitle');
  INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES('subtitle-item','library-1','subtitle-root','Film','film','Movie',false,'film.mp4');
  INSERT INTO subtitle_timeline_queue(item_id) VALUES('subtitle-item')`); err != nil {
				t.Fatal(err)
			}
			run := f.start(t, library.TaskSubtitleTimelineGenerationKey, nil)
			children := f.children(t, run)
			if len(children) != 1 {
				t.Fatalf("expected one subtitle timeline child: %+v", children)
			}
			child := children[0]
			if _, err := f.store.claimExecution(f.ctx, run.ID, child.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
				t.Fatal(err)
			}
			var operation string
			if err := f.pool.QueryRow(f.ctx, `UPDATE subtitle_timeline_queue SET state='running',claimed_revision=1,run_id=$1,child_id=$2 WHERE item_id='subtitle-item' RETURNING operation_id`, run.ID, child.ID).Scan(&operation); err != nil {
				t.Fatal(err)
			}
			var err error
			if reason == "administrator" {
				_, err = f.store.Stop(f.ctx, f.actor, run.ID)
			} else {
				_, err = f.store.SystemStopRun(f.ctx, run.ID, reason)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.RecoverRuns(f.ctx); err != nil {
				t.Fatal(err)
			}
			var state, afterOperation string
			var requested, completed int64
			if err := f.pool.QueryRow(f.ctx, `SELECT state,operation_id,requested_revision,completed_revision FROM subtitle_timeline_queue WHERE item_id='subtitle-item'`).Scan(&state, &afterOperation, &requested, &completed); err != nil {
				t.Fatal(err)
			}
			if afterOperation != operation {
				t.Fatal("subtitle timeline recovery changed operation identity")
			}
			if reason == "administrator" && (state != "cancelled" || requested != completed) {
				t.Fatalf("explicit stop revived subtitle timeline work: %s %d %d", state, requested, completed)
			}
			if reason == "shutdown" && (state != "pending" || requested <= completed) {
				t.Fatalf("shutdown lost unfinished subtitle timeline work: %s %d %d", state, requested, completed)
			}
		})
	}
}
