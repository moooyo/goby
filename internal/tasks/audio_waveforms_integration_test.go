//go:build linux

package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func TestAudioWaveformSchedulesAndCancellationPreserveLateRequests(t *testing.T) {
	ctx, pool, owner, _, actor, _ := taskRepository(t, 2)
	registry, err := NewExecutorRegistry(ExecutorRegistration{Key: library.TaskAudioWaveformGenerationKey, Name: "Audio waveforms", Executor: &analysisTestExecutor{}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	definition, err := store.GetByKey(ctx, library.TaskAudioWaveformGenerationKey)
	if err != nil || len(definition.Triggers) != 2 {
		t.Fatalf("default schedules: %+v %v", definition, err)
	}
	if definition.Triggers[1].SystemEvent == nil || *definition.Triggers[1].SystemEvent != string(systemevents.AudioWaveformGenerationRequested) {
		t.Fatal("missing independent event trigger")
	}
	if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableAudioWaveformGeneration":true}'::jsonb WHERE id='library-1';
  INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('waveform-root','library-1','/waveform','/','waveform');
  INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES
   ('waveform-first','library-1','waveform-root','First','first','Movie',false,'first.mp4'),
   ('waveform-waiting','library-1','waveform-root','Waiting','waiting','Movie',false,'waiting.mp4'),
   ('waveform-late','library-1','waveform-root','Late','late','Movie',false,'late.mp4');
  INSERT INTO audio_waveform_queue(item_id) VALUES('waveform-first'),('waveform-waiting')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE audio_waveform_queue SET manual=true,actor_user_id=$1,actor_session_id=$2`, actor.Principal.User.ID, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID, RequestID: "waveform-cancel"})
	if err != nil || admission.Run.TotalChildren != 1 {
		t.Fatalf("snapshot only enabled/pending libraries: %+v %v", admission, err)
	}
	run, err := store.BeginRun(ctx, admission.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	children, err := store.ListChildren(ctx, run.ID, Page{})
	if err != nil || len(children.Items) != 1 {
		t.Fatal(err)
	}
	child := children.Items[0]
	token := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := store.claimExecution(ctx, run.ID, child.ID, token); err != nil {
		t.Fatal(err)
	}
	work, err := store.executionWork(ctx, run, child, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
		t.Fatalf("manual work fence: %v", err)
	}
	if allowed, err := owner.AudioWaveformBatchCanYield(ctx, work.Fence); err != nil || !allowed {
		t.Fatalf("default event cannot resume a yielded batch: %t %v", allowed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET retired_at=clock_timestamp() WHERE task_id=$1 AND kind='system_event'`, definition.ID); err != nil {
		t.Fatal(err)
	}
	if allowed, err := owner.AudioWaveformBatchCanYield(ctx, work.Fence); err != nil || allowed {
		t.Fatalf("batch yielded to a removed event trigger: %t %v", allowed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task_triggers SET retired_at=NULL WHERE task_id=$1 AND kind='system_event'`, definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE audio_waveform_queue SET state='running',claimed_revision=1,run_id=$1,child_id=$2 WHERE item_id='waveform-first'`, run.ID, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('waveform-root-2','library-2','/waveform-2','/','waveform-2');
  INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES('waveform-other-library','library-2','waveform-root-2','Other','other','Movie',false,'other.mp4')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audio_waveform_queue(item_id,manual,actor_user_id,actor_session_id) VALUES('waveform-other-library',true,$1,$2)`, actor.Principal.User.ID, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stop(ctx, actor, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audio_waveform_queue(item_id,manual,actor_user_id,actor_session_id) VALUES('waveform-late',true,$1,$2)`, actor.Principal.User.ID, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.finishExecution(ctx, run.ID, child.ID, token, context.Canceled, false); err != nil {
		t.Fatal(err)
	}
	var cancelled, pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='cancelled'),count(*) FILTER(WHERE state='pending') FROM audio_waveform_queue`).Scan(&cancelled, &pending); err != nil || cancelled != 3 || pending != 1 {
		t.Fatalf("cancellation/late request lost: %d %d %v", cancelled, pending, err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fence remained live: %v", err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return systemevents.Record(tx.Exec, systemevents.AudioWaveformGenerationRequested)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DispatchSystemEvents(ctx, 10); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetByKey(ctx, definition.Key)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.ActorKind != "system" {
		t.Fatalf("system event authority missing: %+v %v", current.CurrentRun, err)
	}
	scheduled, err := store.BeginRun(ctx, current.CurrentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	scheduledChildren, err := store.ListChildren(ctx, scheduled.ID, Page{})
	if err != nil || len(scheduledChildren.Items) != 1 {
		t.Fatal(err)
	}
	scheduledChild := scheduledChildren.Items[0]
	if _, err := store.claimExecution(ctx, scheduled.ID, scheduledChild.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	scheduledWork, err := store.executionWork(ctx, scheduled, scheduledChild, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error { return scheduledWork.Fence(tx) }); err != nil {
		t.Fatalf("scheduled work fence: %v", err)
	}
	if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return systemevents.Record(tx.Exec, systemevents.AudioWaveformGenerationRequested)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DispatchSystemEvents(ctx, 10); !errors.Is(err, ErrActiveRunConflict) {
		t.Fatalf("late event consumed by active run: %v", err)
	}
}

func TestAudioWaveformSharesMediaExecutionSlotWithoutAnalysisSchema(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	entries := []ExecutorRegistration{}
	for _, entry := range f.store.executors.entries {
		entries = append(entries, entry)
	}
	entries = append(entries, ExecutorRegistration{Key: library.TaskAudioWaveformGenerationKey, Name: "Audio waveforms", Executor: &analysisTestExecutor{}})
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
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableAudioWaveformGeneration":true}'::jsonb WHERE id='library-1'`); err != nil {
		t.Fatal(err)
	}
	waveform, err := f.store.GetByKey(f.ctx, library.TaskAudioWaveformGenerationKey)
	if err != nil {
		t.Fatal(err)
	}
	f.definitions[waveform.Key] = waveform
	intro := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	introChild := f.children(t, intro)[0]
	if _, err := f.store.claimExecution(f.ctx, intro.ID, introChild.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	bg := f.start(t, library.TaskAudioWaveformGenerationKey, nil)
	bgChild := f.children(t, bg)[0]
	if bg.AnalysisInput != nil || bg.AnalysisConfigFingerprint != "" {
		t.Fatal("waveform inherited old analysis profile invalidation")
	}
	if _, err := f.store.claimExecution(f.ctx, bg.ID, bgChild.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); !errors.Is(err, errAnalysisGroupBusy) {
		t.Fatalf("waveform ran concurrently with intro: %v", err)
	}
	if err := f.store.finishExecution(f.ctx, intro.ID, introChild.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.claimExecution(f.ctx, bg.ID, bgChild.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatalf("waveform slot did not resume: %v", err)
	}
}

func TestAudioWaveformRecoveryDistinguishesStopFromShutdown(t *testing.T) {
	for _, reason := range []string{"administrator", "shutdown"} {
		t.Run(reason, func(t *testing.T) {
			ctx, pool, owner, _, actor, _ := taskRepository(t, 1)
			registry, err := NewExecutorRegistry(ExecutorRegistration{Key: library.TaskAudioWaveformGenerationKey, Name: "Audio waveforms", Executor: &analysisTestExecutor{}})
			if err != nil {
				t.Fatal(err)
			}
			store, err := New(pool, owner, registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			definition, err := store.GetByKey(ctx, library.TaskAudioWaveformGenerationKey)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableAudioWaveformGeneration":true}'::jsonb;
   INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('waveform-root','library-1','/waveform','/','waveform');
   INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES('waveform-item','library-1','waveform-root','Film','film','Movie',false,'film.mp4');
   INSERT INTO audio_waveform_queue(item_id) VALUES('waveform-item')`); err != nil {
				t.Fatal(err)
			}
			admission, err := store.Start(ctx, actor, StartRequest{TaskID: definition.ID})
			if err != nil {
				t.Fatal(err)
			}
			run, err := store.BeginRun(ctx, admission.Run.ID)
			if err != nil {
				t.Fatal(err)
			}
			children, err := store.ListChildren(ctx, run.ID, Page{})
			if err != nil || len(children.Items) != 1 {
				t.Fatal(err)
			}
			child := children.Items[0]
			if _, err := store.claimExecution(ctx, run.ID, child.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
				t.Fatal(err)
			}
			var operation string
			if err := pool.QueryRow(ctx, `UPDATE audio_waveform_queue SET state='running',claimed_revision=1,run_id=$1,child_id=$2 WHERE item_id='waveform-item' RETURNING operation_id`, run.ID, child.ID).Scan(&operation); err != nil {
				t.Fatal(err)
			}
			if reason == "administrator" {
				_, err = store.Stop(ctx, actor, run.ID)
			} else {
				_, err = store.SystemStopRun(ctx, run.ID, reason)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := store.RecoverRuns(ctx); err != nil {
				t.Fatal(err)
			}
			var state, afterOperation string
			var requested, completed int64
			if err := pool.QueryRow(ctx, `SELECT state,operation_id,requested_revision,completed_revision FROM audio_waveform_queue WHERE item_id='waveform-item'`).Scan(&state, &afterOperation, &requested, &completed); err != nil {
				t.Fatal(err)
			}
			if afterOperation != operation {
				t.Fatal("recovery changed operation identity")
			}
			if reason == "administrator" && (state != "cancelled" || requested != completed) {
				t.Fatalf("explicit stop revived work: %s %d %d", state, requested, completed)
			}
			if reason == "shutdown" && (state != "pending" || requested <= completed) {
				t.Fatalf("shutdown lost unfinished work: %s %d %d", state, requested, completed)
			}
		})
	}
}
