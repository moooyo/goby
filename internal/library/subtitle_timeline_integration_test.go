//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type subtitleTimelineFixtureProber struct{}

func (subtitleTimelineFixtureProber) ProbeFileJoinedContract() bool { return true }
func (subtitleTimelineFixtureProber) ProbeFile(_ context.Context, file *os.File) (media.Info, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return media.Info{}, err
	}
	info := libraryMediaFixture(data)
	pgs := media.Stream{Index: 3, Codec: "hdmv_pgs_subtitle", CodecType: "subtitle", Language: "eng"}
	dvd := media.Stream{Index: 4, Codec: "dvd_subtitle", CodecType: "subtitle", Language: "fra"}
	switch strings.SplitN(string(data), ":", 2)[0] {
	case "text":
	case "pgs":
		info.Streams = append(info.Streams, pgs)
	case "dvd":
		info.Streams = append(info.Streams, dvd)
	case "external":
		pgs.IsExternal, dvd.IsExternal = true, true
		info.Streams = append(info.Streams, pgs, dvd)
	case "unsupported":
		pgs.Codec, dvd.Codec = "dvb_subtitle", "pgs"
		info.Streams = append(info.Streams, pgs, dvd)
	case "wrong-type":
		pgs.CodecType, dvd.CodecType = "attachment", "data"
		info.Streams = append(info.Streams, pgs, dvd)
	default:
		info.Streams = append(info.Streams, pgs, dvd,
			media.Stream{Index: 5, Codec: "hdmv_pgs_subtitle", CodecType: "subtitle", IsExternal: true},
			media.Stream{Index: 6, Codec: "dvd_subtitle", CodecType: "subtitle", IsExternal: true})
	}
	return info, nil
}

func TestSubtitleTimelineEligibilityAndIndependentAutomaticPolicy(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: subtitleTimelineFixtureProber{}})
	fixtures := []struct {
		name, contents, mutation string
		count                    int
		eligible                 bool
	}{
		{name: "PGS", contents: "pgs:source", count: 1, eligible: true},
		{name: "DVD", contents: "dvd:source", count: 1, eligible: true},
		{name: "Mixed", contents: "mixed:source", count: 2, eligible: true},
		{name: "Episode", contents: "pgs:source", mutation: `type='Episode'`, count: 1, eligible: true},
		{name: "Text", contents: "text:source"},
		{name: "External", contents: "external:source"},
		{name: "Unsupported", contents: "unsupported:source"},
		{name: "WrongType", contents: "wrong-type:source"},
		{name: "Video", contents: "mixed:source", mutation: `type='Video'`},
		{name: "Folder", contents: "mixed:source", mutation: `is_folder=true`},
		{name: "Rootless", contents: "mixed:source", mutation: `root_id=NULL`},
		{name: "Empty", contents: "mixed:source", mutation: `file_size=0`, count: 2},
		{name: "Stale", contents: "mixed:source", mutation: `media=jsonb_set(media,'{ProbeVersion}','0'::jsonb)`, count: 2},
	}
	paths := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		paths[fixture.name] = libraryIntegrationFile(t, root, "timeline-eligibility/"+fixture.name+".mp4", fixture.contents)
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Subtitle timeline eligibility", "movies", filepath.Dir(paths["Mixed"]))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	actor := metadataEditTestActor(t, ctx, pool, "timeline-eligibility-administrator")
	ids := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, paths[fixture.name]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[fixture.name] = id
		if fixture.mutation != "" {
			if _, err := pool.Exec(ctx, `UPDATE items SET `+fixture.mutation+` WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
		if fixture.name != "Video" && fixture.name != "Folder" && fixture.name != "Rootless" {
			detail, err := store.GetSubtitleTimelineItem(ctx, actor, id)
			if err != nil || detail.SubtitleStreamCount != fixture.count || detail.State != "missing" {
				t.Fatalf("%s inventory: %+v %v", fixture.name, detail, err)
			}
		}
		if !fixture.eligible {
			if _, err := store.QueueSubtitleTimelines(ctx, actor, AnalysisSelection{ItemIDs: []string{id}}, "timeline-reject-"+fixture.name); !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s source admitted: %v", fixture.name, err)
			}
		}
	}
	fence := AnalysisFence(func(OwnedTx) error { return nil })
	if err := store.PrepareAutomaticSubtitleTimelines(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subtitle_timeline_queue`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("default-off policy admitted work: %d %v", count, err)
	}
	queued, err := store.QueueSubtitleTimelines(ctx, actor, AnalysisSelection{ItemIDs: []string{ids["Mixed"]}}, "timeline-manual-while-disabled")
	if err != nil || queued.Queued != 1 {
		t.Fatalf("manual admission while automatic generation is disabled: %+v %v", queued, err)
	}
	var eventBefore int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested'`).Scan(&eventBefore); err != nil {
		t.Fatal(err)
	}
	enabled := true
	editing, err := store.GetLibraryEditingAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID, LibraryUpdate{Revision: editing.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableSubtitleTimelineGeneration: &enabled}}); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareAutomaticSubtitleTimelines(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subtitle_timeline_queue`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("automatic admission included unsupported sources: %d %v", count, err)
	}
	var eventAfter int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested'`).Scan(&eventAfter); err != nil || eventAfter != eventBefore+1 {
		t.Fatalf("opt-in must request subtitle timelines: %d %d %v", eventBefore, eventAfter, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audio_waveform_queue`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("subtitle policy admitted audio waveforms: %d %v", count, err)
	}
}

func TestSubtitleTimelineReceiptsSerializeForceAndRejectUnsafeAuthority(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: subtitleTimelineFixtureProber{}})
	path := libraryIntegrationFile(t, root, "timeline-receipts/Film.mp4", "mixed:source")
	collection := libraryIntegrationCreate(t, ctx, store, "Subtitle timeline receipts", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO subtitle_timeline_queue(item_id,manual) VALUES($1,true)`,
		`INSERT INTO subtitle_timeline_queue(item_id,actor_user_id,actor_session_id) VALUES($1,'user','session')`,
		`INSERT INTO subtitle_timeline_queue(item_id,force) VALUES($1,true)`,
		`INSERT INTO subtitle_timeline_queue(item_id,manual,actor_user_id) VALUES($1,true,'user')`,
	} {
		_, err := pool.Exec(ctx, statement, itemID)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("unsafe subtitle timeline authority accepted or failed outside CHECK: %v", err)
		}
	}
	actors := []identity.Principal{metadataEditTestActor(t, ctx, pool, "timeline-concurrent-a"), metadataEditTestActor(t, ctx, pool, "timeline-concurrent-b")}
	type outcome struct {
		result SubtitleTimelineQueueResult
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
			result, err := store.QueueSubtitleTimelines(ctx, actor, AnalysisSelection{ItemIDs: []string{itemID}, Force: true}, "concurrent-timeline-request")
			outcomes <- outcome{result, err}
		}()
	}
	close(start)
	workers.Wait()
	close(outcomes)
	accepted, replayed, conflicts := 0, 0, 0
	for outcome := range outcomes {
		if errors.Is(outcome.err, ErrSubtitleTimelineConflict) {
			conflicts++
			continue
		}
		if outcome.err != nil || outcome.result.Queued != 1 {
			t.Fatalf("receipt concurrency produced an unexpected result: %+v %v", outcome.result, outcome.err)
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
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM subtitle_timeline_requests),requested_revision FROM subtitle_timeline_queue WHERE item_id=$1`, itemID).Scan(&receipts, &revision); err != nil || receipts != 1 || revision != 1 {
		t.Fatalf("duplicate Force admission: receipts=%d revision=%d %v", receipts, revision, err)
	}
}

func TestSubtitleTimelinePublicationFencesAndManualAuthority(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: subtitleTimelineFixtureProber{}})
	path := libraryIntegrationFile(t, root, "timeline-fences/Film.mp4", "mixed:source")
	collection := libraryIntegrationCreate(t, ctx, store, "Subtitle timeline fences", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "timeline-fence-administrator")
	fence := AnalysisFence(func(OwnedTx) error { return nil })
	selection := AnalysisSelection{ItemIDs: []string{itemID}}
	if _, err := store.QueueSubtitleTimelines(ctx, actor, selection, "timeline-first"); err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, "timeline-run-1", "timeline-child-1")
	if err != nil || first == nil || first.Force || first.SourceRevision == "" {
		t.Fatalf("first claim: %+v %v", first, err)
	}
	published := false
	publish := func() error { published = true; return nil }
	forged := *first
	forged.Force = true
	if err := store.WithSubtitleTimelinePublication(ctx, fence, forged, publish); !errors.Is(err, ErrSubtitleTimelineConflict) || published {
		t.Fatalf("in-memory Force bypassed persisted consent: %v published=%t", err, published)
	}
	if err := store.WithSubtitleTimelinePublication(ctx, nil, *first, publish); !errors.Is(err, ErrInvalidInput) || published {
		t.Fatalf("publication bypassed the task fence: %v published=%t", err, published)
	}
	stoppedFence := AnalysisFence(func(OwnedTx) error { return context.Canceled })
	if err := store.WithSubtitleTimelinePublication(ctx, stoppedFence, *first, publish); !errors.Is(err, context.Canceled) || published {
		t.Fatalf("stopped task published: %v published=%t", err, published)
	}
	replay, err := store.QueueSubtitleTimelines(ctx, actor, selection, "timeline-first")
	if err != nil || !replay.Replayed {
		t.Fatalf("receipt replay: %+v %v", replay, err)
	}
	if _, err := store.ValidateSubtitleTimelineJob(ctx, fence, *first); err != nil {
		t.Fatalf("receipt replay changed the active claim: %v", err)
	}
	selection.Force = true
	if _, err := store.QueueSubtitleTimelines(ctx, actor, selection, "timeline-force"); err != nil {
		t.Fatal(err)
	}
	if err := store.WithSubtitleTimelinePublication(ctx, fence, *first, publish); !errors.Is(err, ErrSubtitleTimelineConflict) || published {
		t.Fatalf("old publication survived a newer request: %v published=%t", err, published)
	}
	if err := store.CompleteSubtitleTimeline(ctx, fence, *first, SubtitleTimelineResult{ErrorCode: "request_changed"}); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, "timeline-run-2", "timeline-child-2")
	if err != nil || second == nil || !second.Force || second.OperationID == first.OperationID || second.Revision != first.Revision+1 {
		t.Fatalf("forced replacement claim: %+v %v", second, err)
	}
	if err := store.WithSubtitleTimelinePublication(ctx, fence, *second, publish); err != nil || !published {
		t.Fatalf("current request could not publish: %v published=%t", err, published)
	}
	if err := store.CompleteSubtitleTimeline(ctx, fence, *second, SubtitleTimelineResult{Reused: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableSubtitleTimelineGeneration":true}'::jsonb WHERE id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	var eventBefore int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested'`).Scan(&eventBefore); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var eventAfter int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested'`).Scan(&eventAfter); err != nil || eventAfter != eventBefore+1 {
		t.Fatalf("completed scan must request subtitle timelines: %d %d %v", eventBefore, eventAfter, err)
	}
	if err := store.PrepareAutomaticSubtitleTimelines(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, "timeline-run-3", "timeline-child-3"); err != nil || next != nil {
		t.Fatalf("automatic admission repeated a completed timeline: %+v %v", next, err)
	}
	page, err := store.GetSubtitleTimelineItems(ctx, actor, collection.ID, "Film", "ready", 0, 20)
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || !page.Items[0].Reused || page.Items[0].SubtitleStreamCount != 2 {
		t.Fatalf("ready inventory: %+v %v", page, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','media.subtitle_timeline_generation','Subtitle timeline test');
  INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,stop_requested_at,stop_reason)
  VALUES('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','stopping','manual','media.subtitle_timeline_generation','Subtitle timeline test',clock_timestamp(),'administrator')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueSubtitleTimelines(ctx, actor, selection, "timeline-stopped-force"); err != nil {
		t.Fatal(err)
	}
	var beforeRevision int64
	var beforeOperation string
	if err := pool.QueryRow(ctx, `SELECT requested_revision,operation_id FROM subtitle_timeline_queue WHERE item_id=$1`, itemID).Scan(&beforeRevision, &beforeOperation); err != nil {
		t.Fatal(err)
	}
	ordinary := selection
	ordinary.Force = false
	if _, err := store.QueueSubtitleTimelines(ctx, actor, ordinary, "timeline-after-stop"); err != nil {
		t.Fatal(err)
	}
	var afterRevision int64
	var afterOperation string
	var forced bool
	if err := pool.QueryRow(ctx, `SELECT requested_revision,operation_id,force FROM subtitle_timeline_queue WHERE item_id=$1`, itemID).Scan(&afterRevision, &afterOperation, &forced); err != nil || afterRevision != beforeRevision+1 || afterOperation == beforeOperation || forced {
		t.Fatalf("late ordinary request inherited stopped Force: %d %d %t %v", beforeRevision, afterRevision, forced, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM task_runs WHERE id='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueSubtitleTimelines(ctx, actor, selection, "timeline-revoked"); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, "timeline-run-4", "timeline-child-4")
	if err != nil || revoked == nil {
		t.Fatalf("manual claim: %+v %v", revoked, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	published = false
	if err := store.WithSubtitleTimelinePublication(ctx, fence, *revoked, publish); !errors.Is(err, ErrForbidden) || published {
		t.Fatalf("revoked actor could overwrite the timeline: %v published=%t", err, published)
	}
	stopped, err := store.ClaimSubtitleTimeline(ctx, fence, collection.ID, "timeline-run-5", "timeline-child-5")
	if err != nil || stopped == nil || stopped.SourceRevision != "" {
		t.Fatalf("system retry revived manual authority: %+v %v", stopped, err)
	}
	var state, reason string
	if err := pool.QueryRow(ctx, `SELECT state,error_code FROM subtitle_timeline_queue WHERE item_id=$1`, itemID).Scan(&state, &reason); err != nil || state != "cancelled" || reason != "request_authority_revoked" {
		t.Fatalf("revoked request state: %s %s %v", state, reason, err)
	}
}
