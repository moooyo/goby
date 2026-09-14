package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

func TestRefreshMediaManagerReprobesManualAndScheduledRunsWithoutChangingMediaOrUserData(t *testing.T) {
	f := newManagerFixture(t)
	collections := f.addLibraries(t, 2)
	refresh, err := f.store.GetByKey(f.ctx, LibraryRefreshMediaKey)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := f.manager(t, false)
	managerWait(t, f.ctx, "initial manager scheduling readiness", func(context.Context) bool { return manager.Available() })
	seenJobs := make(map[string]bool, 8)
	start := func(definition Definition, requestID string, force bool) Run {
		t.Helper()
		admission, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: definition.ID, RequestID: requestID})
		if err != nil || !admission.Admitted {
			t.Fatalf("admit %s: admitted=%t error=%v", requestID, admission.Admitted, err)
		}
		run := managerWaitRun(t, f, admission.Run.ID, RunCompleted)
		for _, id := range refreshManagerCompletedJobs(t, f, run, definition, "manual", force, collections) {
			if seenJobs[id] {
				t.Fatal("a new task run adopted a scan from an earlier run")
			}
			seenJobs[id] = true
		}
		return run
	}
	start(f.definition, "ordinary-cold", false)
	refreshManagerProbeCounts(t, f, collections, 1)
	lastPlayed := time.Date(2024, 2, 3, 4, 5, 6, 7000, time.UTC)
	for index, collection := range collections {
		var itemID string
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND path=$2 AND type='Movie' AND NOT is_folder`,
			collection.id, filepath.Join(collection.path, "Fixture.mp4")).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data
			(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`, f.actor.Principal.User.ID, itemID, int64(12345*index), 3+index,
			index == 0, index == 0, lastPlayed); err != nil {
			t.Fatal(err)
		}
	}
	mediaBefore, userDataBefore := refreshManagerCatalogSnapshots(t, f)
	assertPreserved := func() {
		t.Helper()
		mediaAfter, userDataAfter := refreshManagerCatalogSnapshots(t, f)
		if mediaAfter != mediaBefore || userDataAfter != userDataBefore {
			t.Fatal("task scanning changed stable media identities, Media JSON, or existing UserData columns and row versions")
		}
	}
	start(f.definition, "ordinary-cached", false)
	refreshManagerProbeCounts(t, f, collections, 1)
	assertPreserved()
	start(refresh, "refresh-manual", true)
	refreshManagerProbeCounts(t, f, collections, 2)
	assertPreserved()

	interval := int64(3600) * ScheduleTicksPerSecond
	refresh, err = f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: refresh.ID,
		Revision: refresh.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleInterval, IntervalTicks: &interval}}})
	if err != nil || len(refresh.Triggers) != 1 {
		t.Fatalf("configure the explicit native refresh schedule: %v", err)
	}
	// Move only this fixture's rule to one lawful past occurrence. The manager
	// creates the real run and children; its next occurrence remains an hour away.
	var due time.Time
	if err := f.pool.QueryRow(f.ctx, `WITH fixture_clock AS (SELECT clock_timestamp() AS now)
		UPDATE task_triggers SET anchor_at=fixture_clock.now-interval '1 hour 1 second',
			next_fire_at=fixture_clock.now-interval '1 second'
		FROM fixture_clock WHERE id=$1 RETURNING next_fire_at`, refresh.Triggers[0].ID).Scan(&due); err != nil {
		t.Fatal(err)
	}
	manager.Wake()
	var scheduled Run
	managerWait(t, f.ctx, "scheduled refresh admission", func(ctx context.Context) bool {
		history, err := f.store.ListRuns(ctx, refresh.ID, Page{Limit: 3})
		if err != nil || history.TotalRecordCount > 2 {
			t.Fatalf("scheduled refresh created unexpected history: count=%d error=%v", history.TotalRecordCount, err)
		}
		for _, run := range history.Items {
			if run.Source == "schedule" {
				scheduled = run
				return true
			}
		}
		return false
	})
	scheduled = managerWaitRun(t, f, scheduled.ID, RunCompleted)
	for _, id := range refreshManagerCompletedJobs(t, f, scheduled, refresh, "schedule", true, collections) {
		if seenJobs[id] {
			t.Fatal("scheduled refresh adopted an earlier scan")
		}
		seenJobs[id] = true
	}
	if scheduled.TriggerID == nil || *scheduled.TriggerID != refresh.Triggers[0].ID || scheduled.TriggerRevision == nil ||
		*scheduled.TriggerRevision != refresh.Revision || scheduled.ScheduledFor == nil || !scheduled.ScheduledFor.Equal(due) ||
		scheduled.ActorKind != "" || scheduled.ActorUserID != "" || scheduled.ActorSessionID != "" {
		t.Fatal("scheduled refresh lost its exact trigger snapshot or system provenance")
	}
	refreshManagerProbeCounts(t, f, collections, 3)
	assertPreserved()
	current, err := f.store.Get(f.ctx, refresh.ID)
	if err != nil || len(current.Triggers) != 1 || current.Triggers[0].NextFireAt == nil ||
		!current.Triggers[0].NextFireAt.Equal(due.Add(time.Hour)) {
		t.Fatalf("scheduled refresh did not advance its sole occurrence by one hour: %v", err)
	}
	var runs, scans, occurrences, admitted int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs), (SELECT count(*) FROM scan_jobs),
		count(*), count(*) FILTER (WHERE disposition='admitted' AND due_at=$1 AND run_id=$2) FROM task_occurrences`, due, scheduled.ID).
		Scan(&runs, &scans, &occurrences, &admitted); err != nil || runs != 4 || scans != 8 || occurrences != 1 || admitted != 1 || len(seenJobs) != 8 {
		t.Fatalf("manager lost or duplicated a run, scan, or occurrence: runs=%d scans=%d occurrences=%d admitted=%d error=%v", runs, scans, occurrences, admitted, err)
	}
}

func TestRefreshMediaManagerStopIsolatesOrdinaryAndIndependentScans(t *testing.T) {
	for _, firstKey := range []string{LibraryRefreshMediaKey, LibraryScanKey} {
		t.Run(firstKey, func(t *testing.T) {
			f := newManagerFixture(t)
			collections := f.addLibraries(t, 2)
			refresh, err := f.store.GetByKey(f.ctx, LibraryRefreshMediaKey)
			if err != nil {
				t.Fatal(err)
			}
			definitions := map[string]Definition{LibraryScanKey: f.definition, LibraryRefreshMediaKey: refresh}
			independentGate, ownedGate := newManagerProbeGate(), newManagerProbeGate()
			f.prober.gates[collections[0].payload] = independentGate
			f.prober.gates[collections[1].payload] = ownedGate
			independent, err := f.catalog.StartScan(f.ctx, collections[0].id)
			if err != nil {
				t.Fatal(err)
			}
			managerWait(t, f.ctx, "independent C0 scan", func(context.Context) bool { return f.prober.observed(collections[0].payload, false) })
			manager, _ := f.manager(t, false)
			first, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: definitions[firstKey].ID, RequestID: "first-blocked-run"})
			if err != nil || !first.Admitted {
				t.Fatalf("admit first blocked run: %v", err)
			}
			managerWait(t, f.ctx, "first task C1 probe", func(context.Context) bool { return f.prober.observed(collections[1].payload, false) })
			var ownedJobID string
			managerWait(t, f.ctx, "first task waiting on C0 and owning C1", func(ctx context.Context) bool {
				children, err := f.store.ListChildren(ctx, first.Run.ID, Page{Limit: 2})
				if err != nil || len(children.Items) != 2 {
					t.Fatalf("read first run children: %v", err)
				}
				c0, c1 := children.Items[0], children.Items[1]
				if c0.LibraryID != collections[0].id || c1.LibraryID != collections[1].id {
					t.Fatal("task child order changed the controlled library slots")
				}
				if c0.State == ChildWaiting && c0.ScanJobID == nil && c1.State == ChildRunning && c1.ScanJobID != nil {
					ownedJobID = *c1.ScanJobID
					return true
				}
				return false
			})
			otherKey := LibraryScanKey
			if firstKey == LibraryScanKey {
				otherKey = LibraryRefreshMediaKey
			}
			other, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: definitions[otherKey].ID, RequestID: "second-waiting-run"})
			if err != nil || !other.Admitted {
				t.Fatalf("admit second definition while both libraries are busy: %v", err)
			}
			managerWaitRun(t, f, other.Run.ID, RunRunning)
			refreshManagerWaitingChildren(t, f, other.Run.ID, collections)
			runIDs := map[string]string{firstKey: first.Run.ID, otherKey: other.Run.ID}
			stopped, err := manager.Stop(f.ctx, f.actor, runIDs[LibraryRefreshMediaKey])
			if err != nil {
				t.Fatal(err)
			}
			if firstKey == LibraryRefreshMediaKey {
				if stopped.State != RunStopping || stopped.FinishedAt != nil {
					t.Fatal("refresh stop reported completion before its blocked worker drained")
				}
				managerWait(t, f.ctx, "refresh-only cancellation signal", func(context.Context) bool { return f.prober.observed(collections[1].payload, true) })
				refreshManagerWaitingChildren(t, f, runIDs[LibraryScanKey], collections)
			} else if stopped.State != RunCancelled || stopped.CancelledChildren != 2 || stopped.FinishedAt == nil {
				t.Fatal("an entirely waiting refresh did not cancel without stopping ordinary work")
			}
			ordinary, err := f.store.GetRun(f.ctx, runIDs[LibraryScanKey])
			if err != nil || ordinary.State != RunRunning || ordinary.StopRequestedAt != nil || ordinary.StopReason != "" {
				t.Fatalf("stopping refresh changed the ordinary run: %v", err)
			}
			job, err := f.catalog.GetJob(f.ctx, ownedJobID)
			if err != nil || job.Status != "Running" || job.FinishedAt != nil || job.ForceProbe != (firstKey == LibraryRefreshMediaKey) ||
				job.CancelRequested != (firstKey == LibraryRefreshMediaKey) || job.ID == independent.ID {
				t.Fatalf("refresh stop crossed the first task's scan ownership or mode: %v", err)
			}
			job, err = f.catalog.GetJob(f.ctx, independent.ID)
			if err != nil || job.Status != "Running" || job.CancelRequested || job.ForceProbe || job.TaskChildID != "" ||
				f.prober.observed(collections[0].payload, true) || (firstKey == LibraryScanKey && f.prober.observed(collections[1].payload, true)) {
				t.Fatalf("refresh stop cancelled independent or ordinary media probing: %v", err)
			}
			ownedGate.open()
			cancelled := managerWaitRun(t, f, runIDs[LibraryRefreshMediaKey], RunCancelled)
			if cancelled.TaskKey != LibraryRefreshMediaKey || cancelled.TaskEmbyKey != "" || cancelled.Source != "manual" ||
				cancelled.TotalChildren != 2 || cancelled.CancelledChildren != 2 || cancelled.StopReason != "administrator" {
				t.Fatal("refresh cancellation lost its own terminal child outcomes")
			}
			refreshChildren, err := f.store.ListChildren(f.ctx, cancelled.ID, Page{Limit: 2})
			if err != nil || refreshChildren.TotalRecordCount != 2 || len(refreshChildren.Items) != 2 {
				t.Fatalf("read cancelled refresh children: %v", err)
			}
			for index, child := range refreshChildren.Items {
				if child.State != ChildCancelled || child.LibraryID != collections[index].id {
					t.Fatal("refresh cancellation changed another library's child")
				}
				if firstKey == LibraryRefreshMediaKey && index == 1 {
					if child.ScanJobID == nil || *child.ScanJobID != ownedJobID {
						t.Fatal("cancelled refresh lost its owned scan identity")
					}
					job := refreshManagerWaitScan(t, f, ownedJobID, "Cancelled")
					if !job.ForceProbe || !job.CancelRequested || job.TaskChildID != child.ID {
						t.Fatal("refresh cancellation did not drain its own forced scan")
					}
				} else if child.ScanJobID != nil {
					t.Fatal("a waiting refresh adopted an independent or ordinary scan")
				}
			}
			independentGate.open()
			job = refreshManagerWaitScan(t, f, independent.ID, "Completed")
			if job.CancelRequested || job.ForceProbe || job.TaskChildID != "" || f.prober.observed(collections[0].payload, true) {
				t.Fatal("the independent scan did not complete without task cancellation")
			}
			ordinary = managerWaitRun(t, f, ordinary.ID, RunCompleted)
			ordinaryJobs := refreshManagerCompletedJobs(t, f, ordinary, f.definition, "manual", false, collections)
			for _, id := range ordinaryJobs {
				if id == independent.ID || (firstKey == LibraryRefreshMediaKey && id == ownedJobID) {
					t.Fatal("ordinary children adopted independent or cancelled refresh scans")
				}
			}
			if firstKey == LibraryScanKey && ordinaryJobs[collections[1].id] != ownedJobID {
				t.Fatal("stopping refresh replaced the ordinary task's already-owned scan")
			}
			wantScans := 3
			if firstKey == LibraryRefreshMediaKey {
				wantScans++
			}
			var runs, scans int
			if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs), (SELECT count(*) FROM scan_jobs)`).Scan(&runs, &scans); err != nil || runs != 2 || scans != wantScans {
				t.Fatalf("stop isolation created extra executions: runs=%d scans=%d want_scans=%d error=%v", runs, scans, wantScans, err)
			}
			if !manager.Available() || !f.catalog.Available() {
				t.Fatal("stopping refresh shut down the shared task manager or scanner")
			}
		})
	}
}

func refreshManagerCompletedJobs(t *testing.T, f *managerFixture, run Run, definition Definition, source string, force bool, collections []managerTestLibrary) map[string]string {
	t.Helper()
	if run.State != RunCompleted || run.TaskID != definition.ID || run.TaskKey != definition.Key || run.TaskEmbyKey != definition.EmbyKey ||
		run.Source != source || run.TotalChildren != 2 || run.CompletedChildren != 2 || run.Scanned != 2 || run.StopRequestedAt != nil || run.StopReason != "" {
		t.Fatal("completed manager run lost its definition, mode, provenance, or child totals")
	}
	children, err := f.store.ListChildren(f.ctx, run.ID, Page{Limit: 2})
	if err != nil || children.TotalRecordCount != 2 || len(children.Items) != 2 {
		t.Fatalf("read completed manager children: %v", err)
	}
	jobs := make(map[string]string, 2)
	seen := make(map[string]bool, 2)
	for index, child := range children.Items {
		if child.State != ChildCompleted || child.LibraryID != collections[index].id || child.ScanJobID == nil {
			t.Fatal("completed manager child lost its own library scan")
		}
		job, err := f.catalog.GetJob(f.ctx, *child.ScanJobID)
		if err != nil || job.Status != "Completed" || job.TaskChildID != child.ID || job.LibraryID != child.LibraryID ||
			job.ForceProbe != force || job.CancelRequested || job.Error != "" || job.Scanned != 1 || job.StartedAt == nil || job.FinishedAt == nil || seen[job.ID] {
			t.Fatalf("completed child used a foreign scan or the wrong probe mode: %v", err)
		}
		seen[job.ID] = true
		jobs[child.LibraryID] = job.ID
	}
	return jobs
}

func refreshManagerProbeCounts(t *testing.T, f *managerFixture, collections []managerTestLibrary, want int) {
	t.Helper()
	f.prober.mu.Lock()
	counts := []int{f.prober.entered[collections[0].payload], f.prober.entered[collections[1].payload]}
	cancelled := f.prober.cancelled[collections[0].payload] + f.prober.cancelled[collections[1].payload]
	active, payloads := f.prober.active, len(f.prober.entered)
	f.prober.mu.Unlock()
	if counts[0] != want || counts[1] != want || cancelled != 0 || active != 0 || payloads != 2 {
		t.Fatalf("unexpected per-file probe work: counts=%v want=%d cancelled=%d active=%d payloads=%d", counts, want, cancelled, active, payloads)
	}
}

func refreshManagerCatalogSnapshots(t *testing.T, f *managerFixture) (string, string) {
	t.Helper()
	var mediaRows, nonnull, userRows int
	var mediaSnapshot, userSnapshot string
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*), count(media), COALESCE(jsonb_agg(jsonb_build_object(
		'id',id,'library_id',library_id,'path',path,'media',media) ORDER BY library_id,id),'[]'::jsonb)::text
		FROM items WHERE NOT is_folder`).Scan(&mediaRows, &nonnull, &mediaSnapshot); err != nil || mediaRows != 2 || nonnull != 2 {
		t.Fatalf("read the two stable media projections: rows=%d nonnull=%d error=%v", mediaRows, nonnull, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*), COALESCE(jsonb_agg(to_jsonb(d) ||
		jsonb_build_object('row_version',d.xmin::text) ORDER BY d.user_id,d.item_id),'[]'::jsonb)::text
		FROM user_item_data d`).Scan(&userRows, &userSnapshot); err != nil || userRows != 2 {
		t.Fatalf("read the two complete UserData rows: count=%d error=%v", userRows, err)
	}
	return mediaSnapshot, userSnapshot
}

func refreshManagerWaitingChildren(t *testing.T, f *managerFixture, runID string, collections []managerTestLibrary) {
	t.Helper()
	children, err := f.store.ListChildren(f.ctx, runID, Page{Limit: 2})
	if err != nil || children.TotalRecordCount != 2 || len(children.Items) != 2 {
		t.Fatalf("read the waiting definition's children: %v", err)
	}
	for index, child := range children.Items {
		if child.LibraryID != collections[index].id || child.State != ChildWaiting || child.ScanJobID != nil || child.FinishedAt != nil {
			t.Fatal("another definition adopted or cancelled a busy library scan")
		}
	}
}

func refreshManagerWaitScan(t *testing.T, f *managerFixture, id, status string) library.Job {
	t.Helper()
	var job library.Job
	managerWait(t, f.ctx, "scan "+status, func(ctx context.Context) bool {
		var err error
		job, err = f.catalog.GetJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != status && (job.Status == "Completed" || job.Status == "Cancelled" || job.Status == "Failed" || job.Status == "Interrupted") {
			t.Fatalf("scan reached %s, want %s", job.Status, status)
		}
		return job.Status == status
	})
	return job
}
