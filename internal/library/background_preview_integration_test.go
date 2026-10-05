//go:build linux

package library

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func TestBackgroundPreviewRequestsSerializeAdministratorsAndRejectUnsafeRows(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: creditsFixtureProber{}})
	path := libraryIntegrationFile(t, root, "background-admission/Film.mp4", "background-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Background admission", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO background_preview_queue(item_id,manual) VALUES($1,true)`,
		`INSERT INTO background_preview_queue(item_id,actor_user_id,actor_session_id) VALUES($1,'user','session')`,
		`INSERT INTO background_preview_queue(item_id,force) VALUES($1,true)`,
		`INSERT INTO background_preview_queue(item_id,manual,actor_user_id) VALUES($1,true,'user')`,
	} {
		_, err := pool.Exec(ctx, statement, itemID)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("unsafe background authority row accepted or failed outside CHECK: %v", err)
		}
	}
	actors := []identity.Principal{metadataEditTestActor(t, ctx, pool, "background-concurrent-a"), metadataEditTestActor(t, ctx, pool, "background-concurrent-b")}
	type outcome struct {
		result BackgroundPreviewQueueResult
		err    error
	}
	outcomes := make(chan outcome, 8)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		actor := actors[index%len(actors)]
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := store.QueueBackgroundPreviews(ctx, actor, AnalysisSelection{ItemIDs: []string{itemID}, Force: true}, "concurrent-background-request")
			outcomes <- outcome{result, err}
		}()
	}
	close(start)
	workers.Wait()
	close(outcomes)
	accepted, replayed, conflicts := 0, 0, 0
	for outcome := range outcomes {
		if errors.Is(outcome.err, ErrBackgroundPreviewConflict) {
			conflicts++
			continue
		}
		if outcome.err != nil {
			t.Fatalf("receipt concurrency produced non-conflict error: %v", outcome.err)
		}
		accepted++
		if outcome.result.Replayed {
			replayed++
		}
	}
	if accepted != 4 || replayed != 3 || conflicts != 4 {
		t.Fatalf("receipt outcomes: accepted=%d replayed=%d conflicts=%d", accepted, replayed, conflicts)
	}
	var receipts int
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM background_preview_requests),requested_revision FROM background_preview_queue WHERE item_id=$1`, itemID).Scan(&receipts, &revision); err != nil || receipts != 1 || revision != 1 {
		t.Fatalf("duplicate Force admission: receipts=%d revision=%d %v", receipts, revision, err)
	}
}

func TestBackgroundPreviewQueuePersistenceRegenerationAndCAS(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: creditsFixtureProber{}})
	path := libraryIntegrationFile(t, root, "backgrounds/Film.mp4", "background-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Background movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var id string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&id); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "background-administrator")
	fence := AnalysisFence(func(OwnedTx) error { return nil })
	if err := store.PrepareAutomaticBackgroundPreviews(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM background_preview_queue`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("default off admitted automatic work: %d %v", count, err)
	}
	selection := AnalysisSelection{ItemIDs: []string{id}, Force: true}
	queued, err := store.QueueBackgroundPreviews(ctx, actor, selection, "background-request-1")
	if err != nil || queued.Queued != 1 || queued.Replayed {
		t.Fatalf("queue: %+v %v", queued, err)
	}
	first, err := store.ClaimBackgroundPreview(ctx, fence, collection.ID, "background-run-1", "background-child-1")
	if err != nil || first == nil || first.SourceRevision == "" || first.OperationID == "" || !first.Force {
		t.Fatalf("claim: %+v %v", first, err)
	}
	replay, err := store.QueueBackgroundPreviews(ctx, actor, selection, "background-request-1")
	if err != nil || !replay.Replayed {
		t.Fatalf("receipt replay: %+v %v", replay, err)
	}
	if _, err := store.ValidateBackgroundPreviewJob(ctx, fence, *first); err != nil {
		t.Fatalf("replay changed current claim: %v", err)
	}
	different := selection
	different.Force = false
	if _, err := store.QueueBackgroundPreviews(ctx, actor, different, "background-request-1"); !errors.Is(err, ErrBackgroundPreviewConflict) {
		t.Fatalf("different replay accepted: %v", err)
	}
	if _, err := store.QueueBackgroundPreviews(ctx, actor, selection, "background-request-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateBackgroundPreviewJob(ctx, fence, *first); !errors.Is(err, ErrBackgroundPreviewConflict) {
		t.Fatalf("superseded worker retained publication: %v", err)
	}
	if err := store.CompleteBackgroundPreview(ctx, fence, *first, BackgroundPreviewResult{ErrorCode: "superseded"}); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimBackgroundPreview(ctx, fence, collection.ID, "background-run-2", "background-child-2")
	if err != nil || second == nil || second.Revision != first.Revision+1 || second.OperationID == first.OperationID {
		t.Fatalf("late request lost: %+v %v", second, err)
	}
	if err := store.CompleteBackgroundPreview(ctx, fence, *second, BackgroundPreviewResult{Reused: true}); err != nil {
		t.Fatal(err)
	}
	detail, err := store.GetBackgroundPreviewAsAdministrator(ctx, actor, id)
	if err != nil || detail.State != "ready" || !detail.Reused || detail.CompletedRevision != detail.RequestedRevision {
		t.Fatalf("completion: %+v %v", detail, err)
	}
	configuration, err := store.GetBackgroundPreviewConfiguration(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	changedProfile := configuration.Profile
	changedProfile.VideoBitrate = 2000000
	nextConfiguration, err := store.UpdateBackgroundPreviewConfiguration(ctx, actor, BackgroundPreviewConfigurationUpdate{Revision: configuration.Revision, Profile: changedProfile})
	if err != nil || nextConfiguration.Revision == configuration.Revision {
		t.Fatalf("configuration CAS: %+v %v", nextConfiguration, err)
	}
	if _, err := store.UpdateBackgroundPreviewConfiguration(ctx, actor, BackgroundPreviewConfigurationUpdate{Revision: configuration.Revision, Profile: changedProfile}); !errors.Is(err, ErrBackgroundPreviewConflict) {
		t.Fatalf("stale configuration accepted: %v", err)
	}
	start := int64(2) * backgroundTicksPerSecond
	edited, err := store.UpdateBackgroundPreviewAsAdministrator(ctx, actor, id, BackgroundPreviewEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: &start})
	if err != nil || edited.Revision != "1" || edited.StartTicks == nil || *edited.StartTicks != start || edited.State != "ready" {
		t.Fatalf("manual start: %+v %v", edited, err)
	}
	if _, err := store.UpdateBackgroundPreviewAsAdministrator(ctx, actor, id, BackgroundPreviewEdit{Revision: "0", SourceRevision: detail.SourceRevision}); !errors.Is(err, ErrBackgroundPreviewConflict) {
		t.Fatalf("stale manual update accepted: %v", err)
	}
	enabled := true
	editing, err := store.GetLibraryEditingAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID, LibraryUpdate{Revision: editing.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableBackgroundPreviewGeneration: &enabled}}); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareAutomaticBackgroundPreviews(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := store.ClaimBackgroundPreview(ctx, fence, collection.ID, "background-run-3", "background-child-3"); err != nil || next != nil {
		t.Fatalf("opt-in or settings regenerated completed artifact: %+v %v", next, err)
	}
	// A new non-Force request after Stop has fresh intent even when the old
	// Force operation has not yet acknowledged cancellation or been claimed.
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','media.background_preview_generation','Background test');
  INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,stop_requested_at,stop_reason)
  VALUES('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','stopping','manual','media.background_preview_generation','Background test',clock_timestamp(),'administrator')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueBackgroundPreviews(ctx, actor, selection, "background-before-stop"); err != nil {
		t.Fatal(err)
	}
	var beforeRevision int64
	var beforeOperation string
	if err := pool.QueryRow(ctx, `SELECT requested_revision,operation_id FROM background_preview_queue WHERE item_id=$1`, id).Scan(&beforeRevision, &beforeOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueBackgroundPreviews(ctx, actor, different, "background-after-stop"); err != nil {
		t.Fatal(err)
	}
	var afterRevision int64
	var afterOperation string
	var forced bool
	if err := pool.QueryRow(ctx, `SELECT requested_revision,operation_id,force FROM background_preview_queue WHERE item_id=$1`, id).Scan(&afterRevision, &afterOperation, &forced); err != nil || afterRevision != beforeRevision+1 || afterOperation == beforeOperation || forced {
		t.Fatalf("late ordinary request inherited stopped Force: %d %d %t %v", beforeRevision, afterRevision, forced, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM task_runs WHERE id='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueBackgroundPreviews(ctx, identity.Principal{}, selection, "forbidden"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anonymous admission: %v", err)
	}
	closed, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.PrepareAutomaticBackgroundPreviews(closed, fence, collection.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled worker admitted: %v", err)
	}
	if _, err := store.QueueBackgroundPreviews(ctx, actor, selection, "background-request-3"); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.ClaimBackgroundPreview(ctx, fence, collection.ID, "background-run-4", "background-child-4")
	if err != nil || revoked == nil || revoked.SourceRevision == "" {
		t.Fatalf("revocation test claim: %+v %v", revoked, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateBackgroundPreviewJob(ctx, fence, *revoked); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked request retained Force publication: %v", err)
	}
	stopped, err := store.ClaimBackgroundPreview(ctx, fence, collection.ID, "background-run-5", "background-child-5")
	if err != nil || stopped == nil || stopped.SourceRevision != "" {
		t.Fatalf("system retry revived revoked manual request: %+v %v", stopped, err)
	}
	var state, reason string
	if err := pool.QueryRow(ctx, `SELECT state,error_code FROM background_preview_queue WHERE item_id=$1`, id).Scan(&state, &reason); err != nil || state != "cancelled" || reason != "request_authority_revoked" {
		t.Fatalf("revoked queue state: %s %s %v", state, reason, err)
	}
}
