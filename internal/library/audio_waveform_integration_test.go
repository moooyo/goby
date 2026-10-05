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

type waveformFixtureProber struct{}

func (waveformFixtureProber) ProbeFileJoinedContract() bool { return true }
func (waveformFixtureProber) ProbeFile(_ context.Context, file *os.File) (media.Info, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return media.Info{}, err
	}
	info := libraryMediaFixture(data)
	if strings.HasPrefix(string(data), "silent:") {
		info.Streams = info.Streams[:1]
	} else {
		info.Streams = append(info.Streams, media.Stream{Index: 5, Codec: "aac", CodecType: "audio", Channels: 2, SampleRate: 48000, Language: "chi"},
			media.Stream{Index: 6, Codec: "aac", CodecType: "audio", IsExternal: true, Channels: 2, SampleRate: 48000})
	}
	return info, nil
}

func TestAudioWaveformRequestsSerializeAdministratorsAndRejectUnsafeRows(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: waveformFixtureProber{}})
	path := libraryIntegrationFile(t, root, "waveform-admission/Film.mp4", "waveform-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Waveform admission", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO audio_waveform_queue(item_id,manual) VALUES($1,true)`,
		`INSERT INTO audio_waveform_queue(item_id,actor_user_id,actor_session_id) VALUES($1,'user','session')`,
		`INSERT INTO audio_waveform_queue(item_id,force) VALUES($1,true)`,
		`INSERT INTO audio_waveform_queue(item_id,manual,actor_user_id) VALUES($1,true,'user')`,
	} {
		_, err := pool.Exec(ctx, statement, itemID)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("unsafe waveform authority row accepted or failed outside CHECK: %v", err)
		}
	}
	actors := []identity.Principal{metadataEditTestActor(t, ctx, pool, "waveform-concurrent-a"), metadataEditTestActor(t, ctx, pool, "waveform-concurrent-b")}
	type outcome struct {
		result AudioWaveformQueueResult
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
			result, err := store.QueueAudioWaveforms(ctx, actor, AnalysisSelection{ItemIDs: []string{itemID}, Force: true}, "concurrent-waveform-request")
			outcomes <- outcome{result, err}
		}()
	}
	close(start)
	workers.Wait()
	close(outcomes)
	accepted, replayed, conflicts := 0, 0, 0
	for outcome := range outcomes {
		if errors.Is(outcome.err, ErrAudioWaveformConflict) {
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
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audio_waveform_requests),requested_revision FROM audio_waveform_queue WHERE item_id=$1`, itemID).Scan(&receipts, &revision); err != nil || receipts != 1 || revision != 1 {
		t.Fatalf("duplicate Force admission: receipts=%d revision=%d %v", receipts, revision, err)
	}
}

func TestAudioWaveformQueuePreservesExistingAndFencesRegeneration(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: waveformFixtureProber{}})
	path := libraryIntegrationFile(t, root, "waveforms/Film.mp4", "video:waveform-source")
	silent := libraryIntegrationFile(t, root, "waveforms/Silent.mp4", "silent:no-audio")
	collection := libraryIntegrationCreate(t, ctx, store, "Waveform movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID, silentID string
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1`, silent).Scan(&silentID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "waveform-administrator")
	fence := AnalysisFence(func(OwnedTx) error { return nil })
	if err := store.PrepareAutomaticAudioWaveforms(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audio_waveform_queue`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("default off admitted work: %d %v", count, err)
	}
	if _, err := store.QueueAudioWaveforms(ctx, actor, AnalysisSelection{ItemIDs: []string{silentID}}, "silent-waveform"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("silent source admitted: %v", err)
	}
	detail, err := store.GetAudioWaveformItem(ctx, actor, itemID)
	if err != nil || detail.AudioStreamCount != 2 || detail.State != "missing" {
		t.Fatalf("internal track inventory: %+v %v", detail, err)
	}
	selected := AnalysisSelection{ItemIDs: []string{itemID}}
	queued, err := store.QueueAudioWaveforms(ctx, actor, selected, "waveform-first")
	if err != nil || queued.Queued != 1 {
		t.Fatalf("manual admission while auto off: %+v %v", queued, err)
	}
	first, err := store.ClaimAudioWaveform(ctx, fence, collection.ID, "waveform-run-1", "waveform-child-1")
	if err != nil || first == nil || first.Force || first.SourceRevision == "" {
		t.Fatalf("first claim: %+v %v", first, err)
	}
	forged := *first
	forged.Force = true
	published := false
	if err := store.WithAudioWaveformPublication(ctx, fence, forged, func() error { published = true; return nil }); !errors.Is(err, ErrAudioWaveformConflict) || published {
		t.Fatalf("in-memory Force bypassed persistent consent: %v published=%t", err, published)
	}
	if _, err := store.QueueAudioWaveforms(ctx, actor, selected, "waveform-first"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateAudioWaveformJob(ctx, fence, *first); err != nil {
		t.Fatalf("receipt replay changed claim: %v", err)
	}
	selected.Force = true
	if _, err := store.QueueAudioWaveforms(ctx, actor, selected, "waveform-force"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateAudioWaveformJob(ctx, fence, *first); !errors.Is(err, ErrAudioWaveformConflict) {
		t.Fatalf("old publication survived newer request: %v", err)
	}
	if err := store.CompleteAudioWaveform(ctx, fence, *first, AudioWaveformResult{ErrorCode: "request_changed"}); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimAudioWaveform(ctx, fence, collection.ID, "waveform-run-2", "waveform-child-2")
	if err != nil || second == nil || !second.Force || second.OperationID == first.OperationID {
		t.Fatalf("forced replacement claim: %+v %v", second, err)
	}
	if err := store.CompleteAudioWaveform(ctx, fence, *second, AudioWaveformResult{Reused: true}); err != nil {
		t.Fatal(err)
	}
	var eventBefore int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='AudioWaveformGenerationRequested'`).Scan(&eventBefore); err != nil {
		t.Fatal(err)
	}
	enabled := true
	editing, err := store.GetLibraryEditingAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID, LibraryUpdate{Revision: editing.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableAudioWaveformGeneration: &enabled}}); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var eventAfter int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='AudioWaveformGenerationRequested'`).Scan(&eventAfter); err != nil || eventAfter != eventBefore+2 {
		t.Fatalf("opt-in and completed scan must each request waveforms: %d %d %v", eventBefore, eventAfter, err)
	}
	if err := store.PrepareAutomaticAudioWaveforms(ctx, fence, collection.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := store.ClaimAudioWaveform(ctx, fence, collection.ID, "waveform-run-3", "waveform-child-3"); err != nil || next != nil {
		t.Fatalf("scan/opt-in repeated existing waveform or admitted silent media: %+v %v", next, err)
	}
	page, err := store.GetAudioWaveformItems(ctx, actor, collection.ID, "Film", "ready", 0, 20)
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || !page.Items[0].Reused {
		t.Fatalf("ready inventory: %+v %v", page, err)
	}
	for _, search := range []string{"bad\x00search", string([]byte{0xff})} {
		if _, err := store.GetAudioWaveformItems(ctx, actor, "", search, "", 0, 20); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid search accepted: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','media.audio_waveform_generation','Waveform test');
  INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,stop_requested_at,stop_reason)
  VALUES('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','stopping','manual','media.audio_waveform_generation','Waveform test',clock_timestamp(),'administrator')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueAudioWaveforms(ctx, actor, selected, "waveform-stopped-force"); err != nil {
		t.Fatal(err)
	}
	var beforeRevision int64
	var beforeOperation string
	if err := pool.QueryRow(ctx, `SELECT requested_revision,operation_id FROM audio_waveform_queue WHERE item_id=$1`, itemID).Scan(&beforeRevision, &beforeOperation); err != nil {
		t.Fatal(err)
	}
	ordinary := selected
	ordinary.Force = false
	if _, err := store.QueueAudioWaveforms(ctx, actor, ordinary, "waveform-after-stop"); err != nil {
		t.Fatal(err)
	}
	var afterRevision int64
	var afterOperation string
	var forced bool
	if err := pool.QueryRow(ctx, `SELECT requested_revision,operation_id,force FROM audio_waveform_queue WHERE item_id=$1`, itemID).Scan(&afterRevision, &afterOperation, &forced); err != nil || afterRevision != beforeRevision+1 || afterOperation == beforeOperation || forced {
		t.Fatalf("late ordinary request inherited stopped Force: %d %d %t %v", beforeRevision, afterRevision, forced, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM task_runs WHERE id='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueAudioWaveforms(ctx, actor, selected, "waveform-revoked"); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.ClaimAudioWaveform(ctx, fence, collection.ID, "waveform-run-4", "waveform-child-4")
	if err != nil || revoked == nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateAudioWaveformJob(ctx, fence, *revoked); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked actor could overwrite waveform: %v", err)
	}
	stopped, err := store.ClaimAudioWaveform(ctx, fence, collection.ID, "waveform-run-5", "waveform-child-5")
	if err != nil || stopped == nil || stopped.SourceRevision != "" {
		t.Fatalf("system retry revived manual authority: %+v %v", stopped, err)
	}
	var state, reason string
	if err := pool.QueryRow(ctx, `SELECT state,error_code FROM audio_waveform_queue WHERE item_id=$1`, itemID).Scan(&state, &reason); err != nil || state != "cancelled" || reason != "request_authority_revoked" {
		t.Fatalf("revoked request state: %s %s %v", state, reason, err)
	}
}
