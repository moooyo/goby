//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisAdminBatchSerialItems(t *testing.T, f analysisAdminFixture, ids []string) []AnalysisItem {
	t.Helper()
	tx, err := f.store.beginAnalysisAdminRead(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	items := make([]AnalysisItem, 0, len(ids))
	for _, id := range ids {
		item, err := readAnalysisAdminItem(f.ctx, tx, id)
		if err != nil {
			t.Fatal(err)
		}
		// Inventory retains the indexed audit evidence but cannot claim that
		// supporting media bytes were physically revalidated by this read.
		if item.Detection.Effective != nil && item.Detection.Effective.Provenance == "Detected" {
			item.Detection.Effective = nil
		}
		items = append(items, item)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	return items
}

func analysisAdminBatchSeedReview(t *testing.T, f analysisAdminFixture, index int) {
	t.Helper()
	var raw []byte
	var episode, source, content string
	if err := f.pool.QueryRow(f.ctx, `SELECT d.result,s.episode_key,s.source_revision,s.content_sha256
		FROM analysis_detections d JOIN analysis_detection_sources s ON s.item_id=d.item_id
		WHERE d.item_id=$1 AND s.source_item_id=$2`, f.ids[0], f.ids[index]).Scan(&raw, &episode, &source, &content); err != nil {
		t.Fatal(err)
	}
	var value AnalysisStoredResult
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	value.Episode.EpisodeKey, value.Episode.SourceKey, value.Episode.ContentIdentity = episode, source, content
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,
		profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks)
		SELECT $2,revision,$3,profile_fingerprint,profile_revision,publication_epoch,child_id,cohort_revision,status,$4,start_ticks,end_ticks
		FROM analysis_detections WHERE item_id=$1`, f.ids[0], f.ids[index], source, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,
		source_revision,hierarchy_revision,episode_key,content_sha256)
		SELECT $2,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256
		FROM analysis_detection_sources WHERE item_id=$1`, f.ids[0], f.ids[index]); err != nil {
		t.Fatal(err)
	}
}

func analysisAdminBatchSeedPreviews(t *testing.T, f analysisAdminFixture, id string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_previews(item_id,width,revision,source_revision,profile_fingerprint,
		profile_revision,publication_epoch,child_id,cache_key,seal,height,content_sha256,bytes,frame_count,interval_ticks,timeline)
		SELECT i.id,w.width,1,CASE WHEN w.width=320 THEN 'stale-source' ELSE `+introSourceRevisionSQL+` END,
		repeat('a',64),settings.revision+CASE WHEN w.width=400 THEN 1 ELSE 0 END,settings.publication_epoch,
		'analysis-admin-batch-preview',repeat('b',64),repeat('c',64),135,repeat('d',64),84,1,100000000,decode(repeat('00',16),'hex')
		FROM items i CROSS JOIN analysis_settings settings CROSS JOIN (VALUES(400),(240),(320)) w(width)
		WHERE i.id=$1 AND settings.id=1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisAdminBatchInventoryMatchesSerialProjectionAcrossMixedItems(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	root := f.store.roots[0].path
	moviePath := libraryIntegrationFile(t, root, "inventory/Feature.mp4", "video:inventory-feature")
	videoPath := libraryIntegrationFile(t, root, "inventory/Clip.mp4", "video:inventory-clip")
	library := libraryIntegrationCreate(t, f.ctx, f.store, "Inventory mixed sources", "mixed", filepath.Join(root, "inventory"))
	libraryIntegrationScan(t, f.ctx, f.store, library.ID, "Completed")
	var movieID, videoID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND NOT is_folder`, moviePath).Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `UPDATE items SET type='Video' WHERE path=$1 AND NOT is_folder RETURNING id`, videoPath).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	chapters, err := json.Marshal([]media.Chapter{{StartTicks: 0, Title: "IntroStart"}, {StartTicks: 5 * media.TicksPerSecond, Title: "IntroEnd"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=jsonb_set(media,'{Chapters}',$2::jsonb) WHERE id=$1`, f.ids[2], chapters); err != nil {
		t.Fatal(err)
	}
	f.seedReview(t)
	analysisAdminBatchSeedReview(t, f, 1)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO item_intro_state(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by)
		SELECT i.id,9007199254740993,`+introSourceRevisionSQL+`,10000000,40000000,'Manual',$2 FROM items i WHERE i.id=$1`, f.ids[1], f.actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_intro_decisions(item_id,revision,source_revision,rejected,updated_by)
		SELECT i.id,9007199254740994,`+introSourceRevisionSQL+`,true,$2 FROM items i WHERE i.id=$1`, f.ids[1], f.actor.User.ID); err != nil {
		t.Fatal(err)
	}
	analysisAdminBatchSeedPreviews(t, f, f.ids[0])
	analysisAdminBatchSeedPreviews(t, f, videoID)
	ordered := []string{videoID, f.ids[2], movieID, f.ids[1], f.ids[0]}
	for index, id := range ordered {
		sortName, name := fmt.Sprintf("rank-%02d", index), fmt.Sprintf("Item %02d", index)
		if index < 2 {
			sortName = "rank-00"
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE items SET sort_name=$2,name=$3 WHERE id=$1`, id, sortName, name); err != nil {
			t.Fatal(err)
		}
	}
	want := analysisAdminBatchSerialItems(t, f, ordered)
	page, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{Limit: 25})
	if err != nil || page.TotalRecordCount != len(ordered) || !reflect.DeepEqual(page.Items, want) {
		t.Fatalf("mixed inventory differs from ordered serial projection: got=%+v want=%+v error=%v", page, want, err)
	}
	if want[0].Type != "Video" || want[2].Type != "Movie" || want[3].Type != "Episode" ||
		want[0].Detection.Status != "not_analyzed" || want[3].Detection.Candidate == nil || !want[3].Detection.Suppressed ||
		want[3].Detection.Revision != "9007199254740994" || want[3].Detection.ManualRevision != "9007199254740993" ||
		want[3].Detection.Effective == nil || want[3].Detection.Effective.Provenance != "Manual" ||
		want[1].Detection.Effective == nil || want[1].Detection.Effective.Provenance != "Chapter" {
		t.Fatal("mixed fixture did not exercise independent source, decision, manual, and chapter projections")
	}
	previews := want[4].Previews
	if len(previews) != 3 || previews[0].Width != 240 || previews[0].Status != "ready" || previews[0].FailureCode != "" ||
		previews[1].Width != 320 || previews[1].Status != "stale" || previews[1].FailureCode != "source_changed" ||
		previews[2].Width != 400 || previews[2].Status != "stale" || previews[2].FailureCode != "configuration_changed" {
		t.Fatalf("preview fixture did not exercise ordered current and stale variants: %+v", previews)
	}
	for _, item := range page.Items {
		if item.Previews == nil || item.Detection.Reasons == nil {
			t.Fatalf("inventory changed an empty array into null: %s", item.ID)
		}
	}
	page, err = f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{StartIndex: 1, Limit: 3})
	if err != nil || page.TotalRecordCount != len(ordered) || !reflect.DeepEqual(page.Items, want[1:4]) {
		t.Fatalf("batch hydration changed page order or count: got=%+v error=%v", page, err)
	}
}

func TestAnalysisAdminBatchInventoryRetainsQualifiedEvidenceWithoutDetectedAuthority(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	review := f.seedReview(t)
	value := analysisDetectionTestValue(introdetect.Qualified)
	support := value.Episode.Candidates[0].Support
	if len(support) != len(review.Detection.Candidate.Support) {
		t.Fatal("qualified fixture requires the same independent support population")
	}
	for index := range support {
		original := review.Detection.Candidate.Support[index]
		support[index].EpisodeKey, support[index].SourceKey, support[index].ContentIdentity = original.EpisodeKey, original.SourceKey, original.ContentIdentity
	}
	value.Episode.EpisodeKey, value.Episode.SourceKey, value.Episode.ContentIdentity = support[0].EpisodeKey, support[0].SourceKey, support[0].ContentIdentity
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	interval := value.Episode.Candidates[0].Interval
	if err := ValidateStoredAnalysisResult(raw, "qualified", &interval.StartTicks, &interval.EndTicks); err != nil {
		t.Fatal("qualified fixture violates the stored result contract", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE analysis_detections SET status='qualified',result=$2,start_ticks=$3,end_ticks=$4,auto_published=true
		WHERE item_id=$1`, f.ids[0], raw, interval.StartTicks, interval.EndTicks); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableIntroDetection}','true'::jsonb) WHERE id=$1`, f.libraryID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.store.beginAnalysisAdminRead(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	want, err := readAnalysisAdminItem(f.ctx, tx, f.ids[0])
	rollback(tx)
	if err != nil || want.Detection.Status != "qualified" || want.Detection.Candidate == nil ||
		want.Detection.Effective == nil || want.Detection.Effective.Provenance != "Detected" {
		t.Fatalf("serial fixture did not expose qualified detected authority: %+v %v", want.Detection, err)
	}
	want.Detection.Effective = nil
	page, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("qualified inventory read failed: %+v %v", page, err)
	}
	for _, item := range page.Items {
		if item.ID == want.ID {
			if !reflect.DeepEqual(item, want) {
				t.Fatalf("inventory either lost qualified evidence or advertised detected authority: got=%+v want=%+v", item, want)
			}
			return
		}
	}
	t.Fatal("inventory omitted the qualified source")
}

func analysisAdminBatchSecondSeason(t *testing.T, f analysisAdminFixture) analysisAdminFixture {
	t.Helper()
	second := analysisAdminFixture{ctx: f.ctx, pool: f.pool, store: f.store, actor: f.actor, libraryID: f.libraryID}
	for index := 1; index <= 3; index++ {
		second.paths = append(second.paths, libraryIntegrationFile(t, f.store.roots[0].path,
			fmt.Sprintf("analysis/Show/Season 02/Show.S02E%02d.mp4", index), fmt.Sprintf("video:analysis-source-%d", index)))
	}
	// Complete the expanded catalog before binding either season's evidence.
	libraryIntegrationScan(t, f.ctx, f.store, f.libraryID, "Completed")
	for _, path := range second.paths {
		var id string
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&id); err != nil {
			t.Fatal(err)
		}
		second.ids = append(second.ids, id)
	}
	return second
}

func TestAnalysisAdminBatchInventoryKeepsInterleavedSeasonCohortsIndependent(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	second := analysisAdminBatchSecondSeason(t, f)
	for _, season := range []analysisAdminFixture{f, second} {
		season.seedReview(t)
		analysisAdminBatchSeedReview(t, season, 1)
		analysisAdminBatchSeedReview(t, season, 2)
	}
	ordered := []string{f.ids[2], second.ids[0], f.ids[0], second.ids[2], f.ids[1], second.ids[1]}
	var seasons int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(DISTINCT parent_id) FROM items WHERE id=ANY($1::text[])`, ordered).Scan(&seasons); err != nil || seasons != 2 {
		t.Fatalf("fixture did not create two independent season populations: %d %v", seasons, err)
	}
	for index, id := range ordered {
		if _, err := f.pool.Exec(f.ctx, `UPDATE items SET sort_name=$2 WHERE id=$1`, id, fmt.Sprintf("batch-season-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	want := analysisAdminBatchSerialItems(t, f, ordered)
	for _, item := range want {
		if item.Detection.Status != "review" || item.Detection.Candidate == nil || len(item.Detection.Candidate.Support) != 3 {
			t.Fatalf("season fixture did not retain its current independent support: %+v", item.Detection)
		}
	}
	trace := &analysisAdminBatchTrace{}
	reader := analysisAdminBatchTracedStore(t, f, trace)
	baseQueries := 0
	// The offset changes which season is encountered first, while each page
	// still needs members outside the page to validate both complete cohorts.
	for _, query := range []AnalysisItemQuery{
		{LibraryID: f.libraryID, Limit: 1},
		{LibraryID: f.libraryID, Limit: 6},
		{LibraryID: f.libraryID, StartIndex: 1, Limit: 4},
	} {
		page, err := reader.ListAnalysisItems(f.ctx, f.actor, query)
		if err != nil || page.TotalRecordCount != len(ordered) ||
			!reflect.DeepEqual(page.Items, want[query.StartIndex:query.StartIndex+query.Limit]) {
			t.Fatalf("batch combined or lost a season cohort: query=%+v got=%+v want=%+v error=%v", query, page, want, err)
		}
		observation := trace.takeObservation()
		wantSeasons := 2
		if query.Limit == 1 {
			wantSeasons = 1
			baseQueries = len(observation.queries) - wantSeasons
		}
		if observation.batchStarts != 1 || observation.batchEnds != 1 || observation.batchErr != nil ||
			len(observation.batchQueries) != wantSeasons || len(observation.queries) != baseQueries+wantSeasons {
			t.Fatalf("expected S bounded statements plus page reads in one batch: seasons=%d observation=%+v", wantSeasons, observation)
		}
		for _, statement := range observation.batchQueries {
			if !strings.Contains(statement, "where i.library_id=$1 and i.parent_id=$2") ||
				!strings.HasSuffix(statement, "order by i.id limit 100001") || strings.Contains(statement, "ordinality") {
				t.Fatalf("season batch lost its independent population bound: %s", statement)
			}
		}
	}
}

func TestAnalysisAdminBatchInventoryPreservesIndexedStaleness(t *testing.T) {
	for _, test := range []struct {
		name, statement, reason string
	}{
		{"source", `UPDATE analysis_detections SET source_revision='replaced-source' WHERE item_id=$1`, "source_changed"},
		{"profile", `UPDATE analysis_detections SET profile_revision=profile_revision+1 WHERE item_id=$1`, "profile_changed"},
		{"epoch", `UPDATE analysis_detections SET publication_epoch=publication_epoch+1 WHERE item_id=$1`, "profile_changed"},
		{"profile_precedence", `UPDATE analysis_detections SET source_revision='replaced-source',profile_revision=profile_revision+1 WHERE item_id=$1`, "profile_changed"},
		{"support", `UPDATE analysis_detection_sources SET source_revision='replaced-support' WHERE item_id=$1`, "support_changed"},
		{"cohort", `UPDATE analysis_detections SET cohort_revision=repeat('f',64) WHERE item_id=$1`, "support_changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAnalysisAdminFixture(t)
			f.seedReview(t)
			if _, err := f.pool.Exec(f.ctx, test.statement, f.ids[0]); err != nil {
				t.Fatal(err)
			}
			want := analysisAdminBatchSerialItems(t, f, f.ids[:1])[0]
			if want.Detection.Status != "stale" || want.Detection.Candidate == nil ||
				len(want.Detection.Reasons) == 0 || want.Detection.Reasons[len(want.Detection.Reasons)-1] != test.reason {
				t.Fatalf("fixture did not produce the expected indexed staleness: %+v", want.Detection)
			}
			page, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25})
			if err != nil || page.TotalRecordCount != 3 || len(page.Items) != 3 {
				t.Fatalf("stale evidence made the batch unavailable: %+v %v", page, err)
			}
			found := false
			for _, item := range page.Items {
				if item.ID == want.ID {
					found = true
					if !reflect.DeepEqual(item, want) {
						t.Fatalf("batch changed stale evidence: got=%+v want=%+v", item, want)
					}
				}
			}
			if !found {
				t.Fatal("batch omitted a stale indexed source")
			}
		})
	}
}

func TestAnalysisAdminBatchInventoryRejectsCorruptStoredEvidence(t *testing.T) {
	for _, test := range []struct{ name, statement string }{
		{"malformed_result", `UPDATE analysis_detections SET result='{}'::jsonb WHERE item_id=$1`},
		{"inconsistent_status", `UPDATE analysis_detections SET status='no_result',start_ticks=NULL,end_ticks=NULL WHERE item_id=$1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAnalysisAdminFixture(t)
			f.seedReview(t)
			if _, err := f.pool.Exec(f.ctx, test.statement, f.ids[0]); err != nil {
				t.Fatal(err)
			}
			tx, err := f.store.beginAnalysisAdminRead(f.ctx, f.actor)
			if err != nil {
				t.Fatal(err)
			}
			_, serialErr := readAnalysisAdminItem(f.ctx, tx, f.ids[0])
			rollback(tx)
			if !errors.Is(serialErr, ErrUnavailable) {
				t.Fatalf("corrupt fixture was accepted by the serial reader: %v", serialErr)
			}
			page, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25})
			if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(page, AnalysisItemPage{}) {
				t.Fatalf("corrupt evidence returned a partial inventory: %+v %v", page, err)
			}
		})
	}
}

type analysisAdminBatchTraceKey struct{}

type analysisAdminBatchTrace struct {
	mu              sync.Mutex
	queries         []string
	batchQueries    []string
	batchStarts     int
	batchEnds       int
	batchErr        error
	afterPopulation func(context.Context) error
	fired           bool
	err             error
}

type analysisAdminBatchObservation struct {
	queries, batchQueries  []string
	batchStarts, batchEnds int
	batchErr               error
}

func (trace *analysisAdminBatchTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if strings.HasPrefix(statement, "select ") || strings.HasPrefix(statement, "with ") {
		trace.queries = append(trace.queries, statement)
	}
	if trace.afterPopulation != nil && !trace.fired && strings.HasPrefix(statement, "select count(*) from items i ") {
		return context.WithValue(ctx, analysisAdminBatchTraceKey{}, true)
	}
	return ctx
}

func (trace *analysisAdminBatchTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil || ctx.Value(analysisAdminBatchTraceKey{}) != true {
		return
	}
	trace.mu.Lock()
	if trace.fired {
		trace.mu.Unlock()
		return
	}
	trace.fired = true
	hook := trace.afterPopulation
	trace.mu.Unlock()
	err := hook(ctx)
	trace.mu.Lock()
	trace.err = err
	trace.mu.Unlock()
}

func (trace *analysisAdminBatchTrace) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	trace.mu.Lock()
	trace.batchStarts++
	trace.mu.Unlock()
	return ctx
}

func (trace *analysisAdminBatchTrace) TraceBatchQuery(_ context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	trace.mu.Lock()
	trace.queries = append(trace.queries, statement)
	trace.batchQueries = append(trace.batchQueries, statement)
	trace.mu.Unlock()
}

func (trace *analysisAdminBatchTrace) TraceBatchEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	trace.mu.Lock()
	trace.batchEnds++
	trace.batchErr = data.Err
	trace.mu.Unlock()
}

func (trace *analysisAdminBatchTrace) takeObservation() analysisAdminBatchObservation {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	result := analysisAdminBatchObservation{queries: append([]string(nil), trace.queries...),
		batchQueries: append([]string(nil), trace.batchQueries...), batchStarts: trace.batchStarts,
		batchEnds: trace.batchEnds, batchErr: trace.batchErr}
	trace.queries = nil
	trace.batchQueries = nil
	trace.batchStarts, trace.batchEnds, trace.batchErr = 0, 0, nil
	return result
}

func analysisAdminBatchTracedStore(t *testing.T, f analysisAdminFixture, trace *analysisAdminBatchTrace) *Store {
	t.Helper()
	config := f.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Store{pool: pool}
}

func TestAnalysisAdminBatchInventoryQueryCountDoesNotGrowWithinOneSeason(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	f.seedReview(t)
	analysisAdminBatchSeedReview(t, f, 1)
	analysisAdminBatchSeedReview(t, f, 2)
	for _, id := range f.ids {
		analysisAdminBatchSeedPreviews(t, f, id)
	}
	trace := &analysisAdminBatchTrace{}
	reader := analysisAdminBatchTracedStore(t, f, trace)
	var first []string
	for _, limit := range []int{1, 2, 3} {
		page, err := reader.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: limit})
		if err != nil || page.TotalRecordCount != 3 || len(page.Items) != limit {
			t.Fatalf("read page of %d current shared-season detections: %+v %v", limit, page, err)
		}
		for _, item := range page.Items {
			if item.Detection.Status != "review" || item.Detection.Candidate == nil || len(item.Previews) != 3 {
				t.Fatalf("query count fixture did not hydrate current evidence and previews: %+v", item)
			}
		}
		observation := trace.takeObservation()
		queries := observation.queries
		if len(queries) == 0 {
			t.Fatal("query tracer observed no inventory reads")
		}
		if observation.batchStarts != 1 || observation.batchEnds != 1 || len(observation.batchQueries) != 1 || observation.batchErr != nil {
			t.Fatalf("shared season was not read exactly once in one completed batch: %+v", observation)
		}
		if first == nil {
			first = queries
		} else if len(queries) != len(first) {
			t.Fatalf("inventory SELECT count grew with page size: one=%d page=%d reads=%d\none=%v\npage=%v", len(first), limit, len(queries), first, queries)
		}
	}
}

func TestAnalysisAdminBatchInventoryKeepsOneSnapshotAndRechecksAuthority(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "snapshot"
		if revoke {
			name = "revoked_after_count"
		}
		t.Run(name, func(t *testing.T) {
			f := newAnalysisAdminFixture(t)
			f.seedReview(t)
			analysisAdminBatchSeedPreviews(t, f, f.ids[0])
			for index, id := range f.ids {
				if _, err := f.pool.Exec(f.ctx, `UPDATE items SET sort_name=$2 WHERE id=$1`, id, fmt.Sprintf("rank-%d", index)); err != nil {
					t.Fatal(err)
				}
			}
			want := analysisAdminBatchSerialItems(t, f, f.ids)
			trace := &analysisAdminBatchTrace{afterPopulation: func(ctx context.Context) error {
				if revoke {
					_, err := f.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.SessionID)
					return err
				}
				_, err := f.pool.Exec(ctx, `UPDATE items SET name='Concurrent replacement' WHERE id=$1`, f.ids[0])
				if err != nil {
					return err
				}
				_, err = f.pool.Exec(ctx, `DELETE FROM analysis_previews WHERE item_id=$1`, f.ids[0])
				return err
			}}
			reader := analysisAdminBatchTracedStore(t, f, trace)
			page, err := reader.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25})
			trace.mu.Lock()
			fired, hookErr := trace.fired, trace.err
			trace.mu.Unlock()
			if !fired || hookErr != nil {
				t.Fatalf("concurrent writer did not run after the population snapshot: fired=%t error=%v", fired, hookErr)
			}
			if revoke {
				if !errors.Is(err, ErrForbidden) || !reflect.DeepEqual(page, AnalysisItemPage{}) {
					t.Fatalf("revoked actor received a hydrated inventory: %+v %v", page, err)
				}
				return
			}
			if err != nil || page.TotalRecordCount != 3 || !reflect.DeepEqual(page.Items, want) {
				t.Fatalf("batch hydration escaped the population snapshot: got=%+v want=%+v error=%v", page, want, err)
			}
			page, err = f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25})
			if err != nil || len(page.Items) != 3 || page.Items[0].Name != "Concurrent replacement" || len(page.Items[0].Previews) != 0 {
				t.Fatalf("next snapshot did not observe the committed projection changes: %+v %v", page, err)
			}
		})
	}
}

type analysisAdminSeasonFaultTx struct {
	pgx.Tx
	queryFailure             int
	scanFailure              error
	closeFailure             error
	closed                   bool
	copies                   int
	itemID                   string
	batchContext             context.Context
	scanCancelledBeforeClose bool
	scanCalls                int
}

func (tx *analysisAdminSeasonFaultTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	tx.batchContext = ctx
	injected := &pgx.Batch{}
	for index, query := range batch.QueuedQueries {
		if index == tx.queryFailure {
			injected.Queue("SELECT 1/0")
		} else if index == 1 && tx.scanFailure != nil {
			injected.Queue("SELECT pg_sleep(10)")
		} else if index == 0 && tx.copies != 0 {
			// Expand one real indexed source without creating 100000 media files.
			// Keep the production ORDER BY and LIMIT to exercise the SQL bound.
			statement := strings.Replace(query.SQL, " FROM items i ", " FROM items i CROSS JOIN generate_series(1,$3::integer) copies(n) ", 1)
			statement = strings.Replace(statement, "WHERE i.library_id=$1", "WHERE i.id=$4 AND i.library_id=$1", 1)
			arguments := append(append([]any(nil), query.Arguments...), tx.copies, tx.itemID)
			injected.Queue(statement, arguments...)
		} else {
			injected.Queue(query.SQL, query.Arguments...)
		}
	}
	return &analysisAdminSeasonFaultResults{BatchResults: tx.Tx.SendBatch(ctx, injected), owner: tx}
}

type analysisAdminSeasonFaultResults struct {
	pgx.BatchResults
	owner *analysisAdminSeasonFaultTx
	index int
}

func (results *analysisAdminSeasonFaultResults) Query() (pgx.Rows, error) {
	rows, err := results.BatchResults.Query()
	results.index++
	if err == nil && results.index == 1 && results.owner.scanFailure != nil {
		return &analysisAdminSeasonFaultRows{Rows: rows, owner: results.owner}, nil
	}
	return rows, err
}

func (results *analysisAdminSeasonFaultResults) Close() error {
	err := results.BatchResults.Close()
	results.owner.closed = true
	return errors.Join(err, results.owner.closeFailure)
}

type analysisAdminSeasonFaultRows struct {
	pgx.Rows
	owner *analysisAdminSeasonFaultTx
}

func (rows *analysisAdminSeasonFaultRows) Scan(...any) error {
	rows.owner.scanCalls++
	return rows.owner.scanFailure
}

func (rows *analysisAdminSeasonFaultRows) Close() {
	rows.owner.scanCancelledBeforeClose = rows.owner.batchContext.Err() != nil
	rows.Rows.Close()
}

func analysisAdminBatchSeasonInputs(t *testing.T, f, second analysisAdminFixture, tx pgx.Tx) ([]analysisAdminSeason, map[string]bool) {
	t.Helper()
	seasons := []analysisAdminSeason{}
	cohorts := map[string]bool{}
	for _, id := range []string{f.ids[0], second.ids[0]} {
		source, err := readAnalysisSourceUsing(f.ctx, tx, unrestrictedLibraryAccess(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		seasons = append(seasons, analysisAdminSeason{source.LibraryID, source.SeasonID})
		cohorts[analysisCohortKey(source)] = true
	}
	return seasons, cohorts
}

func TestAnalysisAdminBatchInventorySeasonPipelineDrainsFailures(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	second := analysisAdminBatchSecondSeason(t, f)
	scanFailure, closeFailure := errors.New("injected season scan failure"), errors.New("injected season completion failure")
	for _, test := range []struct {
		name         string
		queryFailure int
		scan, close  error
	}{
		{name: "first_scan", queryFailure: -1, scan: scanFailure, close: closeFailure},
		{name: "later_sql", queryFailure: 1, close: closeFailure},
		{name: "completion", queryFailure: -1, close: closeFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, err := f.pool.Acquire(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Release()
			tx, err := connection.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			seasons, needed := analysisAdminBatchSeasonInputs(t, f, second, tx)
			faults := &analysisAdminSeasonFaultTx{Tx: tx, queryFailure: test.queryFailure, scanFailure: test.scan, closeFailure: test.close}
			if test.scan != nil {
				// PostgreSQL can buffer a small first result until the pipeline's
				// final Sync, after the later sleep. Fill its output buffer so the
				// first real row reaches Scan before testing early cancellation.
				faults.copies, faults.itemID = 4096, f.ids[0]
			}
			callContext, cancel := context.WithTimeout(f.ctx, 3*time.Second)
			defer cancel()
			counts, cohorts, err := readAnalysisAdminSeasonCohorts(callContext, faults, seasons, needed, 1<<40)
			if err == nil || counts != nil || cohorts != nil || !faults.closed || connection.Conn().PgConn().IsBusy() {
				t.Fatalf("failed season pipeline leaked a result or left the connection busy: counts=%v cohorts=%v closed=%t error=%v", counts, cohorts, faults.closed, err)
			}
			if test.scan != nil {
				if faults.scanCalls != 1 {
					t.Fatalf("scan fault was not reached before the pipeline failed: scans=%d error=%v", faults.scanCalls, err)
				}
				if !errors.Is(err, scanFailure) || errors.Is(err, closeFailure) || !faults.scanCancelledBeforeClose || callContext.Err() != nil {
					t.Fatalf("batch drain changed row failure or cancellation ownership: cancelled_before_close=%t caller_error=%v error=%v", faults.scanCancelledBeforeClose, callContext.Err(), err)
				}
			} else if test.queryFailure >= 0 {
				var postgresError *pgconn.PgError
				if !errors.As(err, &postgresError) || postgresError.Code != "22012" || errors.Is(err, closeFailure) {
					t.Fatalf("batch drain replaced the later SQL error: %v", err)
				}
			} else if !errors.Is(err, closeFailure) {
				t.Fatalf("successful rowsets concealed batch completion failure: %v", err)
			}
			// Cancellation may retire the physical connection. The pool must
			// still recover after ownership of that transaction is released.
			_ = tx.Rollback(f.ctx)
			connection.Release()
			var one int
			if err := f.pool.QueryRow(f.ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
				t.Fatalf("retired pipeline prevented pool reuse: value=%d error=%v", one, err)
			}
		})
	}
}

func TestAnalysisAdminBatchInventorySeasonPipelineKeepsPopulationBounds(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	second := analysisAdminBatchSecondSeason(t, f)
	for _, copies := range []int{analysisMaximumAdmissionSources, analysisMaximumAdmissionSources + 2} {
		t.Run(fmt.Sprintf("population_%d", copies), func(t *testing.T) {
			tx, err := f.store.beginAnalysisAdminRead(f.ctx, f.actor)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			seasons, needed := analysisAdminBatchSeasonInputs(t, f, second, tx)
			faults := &analysisAdminSeasonFaultTx{Tx: tx, queryFailure: -1, copies: copies, itemID: f.ids[0]}
			counts, cohorts, err := readAnalysisAdminSeasonCohorts(f.ctx, faults, seasons, needed, 1<<40)
			if err != nil || !faults.closed || counts[seasons[0]] != min(copies, analysisMaximumAdmissionSources+1) || counts[seasons[1]] != 3 {
				t.Fatalf("season SQL limit or next rowset changed: counts=%v closed=%t error=%v", counts, faults.closed, err)
			}
			wantCohorts := 2
			if copies > analysisMaximumAdmissionSources {
				wantCohorts = 1
			}
			if len(cohorts) != wantCohorts {
				t.Fatalf("oversized population was hashed or discarded the following season: got=%d want=%d", len(cohorts), wantCohorts)
			}
			var one int
			if err := tx.QueryRow(f.ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
				t.Fatalf("completed season pipeline prevented the next transaction read: value=%d error=%v", one, err)
			}
		})
	}
}
