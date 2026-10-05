//go:build linux

package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/creditsskipper"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
)

// Empty decoded evidence permits deterministic chapter/audio fixtures without
// opening a source file or claiming a visual detector found nonexistent pixels.
type creditsArchiveProbe struct{}

func (creditsArchiveProbe) ScanKeyframes(context.Context, creditsskipper.Range, int) (creditsskipper.KeyframeEvidence, error) {
	return creditsskipper.KeyframeEvidence{}, nil
}
func (creditsArchiveProbe) ScanBlackIntervals(context.Context, creditsskipper.Range, int, int) ([]creditsskipper.Range, error) {
	return nil, nil
}
func (creditsArchiveProbe) ScanBoundary(context.Context, creditsskipper.Range, int, int) ([]creditsskipper.BlackFrame, error) {
	return nil, nil
}
func (creditsArchiveProbe) ScanSilence(context.Context, creditsskipper.Range) ([]creditsskipper.Range, error) {
	return nil, nil
}
func (creditsArchiveProbe) ScanKeyframesAtBoundary(context.Context, creditsskipper.Range) ([]float64, error) {
	return nil, nil
}

func seedAnalysisArchiveCreditsHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execution := creditsArchiveExecution()
	profileRaw, executionRaw, fingerprint := creditsArchiveAdmission(t, execution)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('credits-library','Credits history','mixed');
		UPDATE libraries SET options=jsonb_set(options,'{EnableCreditsDetection}','true') WHERE id='credits-library';
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('credits-episode-1','credits-library','Episode one','episode one','Episode',false),
		('credits-episode-2','credits-library','Episode two','episode two','Episode',false),
		('credits-movie','credits-library','Movie','movie','Movie',false);
		INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'media.credits_analysis','Credits analysis');`); err != nil {
		t.Fatal(err)
	}
	run := strings.Repeat("b", 32)
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
		VALUES($1,repeat('a',32),'completed','manual','backup-admin','retired-credits-session','admin','media.credits_analysis','Credits analysis',
		'{}',$2,2,2,2,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, profileRaw, executionRaw); err != nil {
		t.Fatal(err)
	}
	for group := 0; group < 2; group++ {
		scope := "analysis:" + strings.Repeat(string(rune('c'+group)), 64)
		childDigest := sha256.Sum256([]byte(run + ":" + scope))
		child := hex.EncodeToString(childDigest[:16])
		if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
			VALUES($1,$2,'credits-library','Credits history',$3,$4,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, run, group, scope); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
			VALUES($1,$2,'media.credits_analysis','credits-library',$3,repeat('e',64),false)`, child, run, scope); err != nil {
			t.Fatal(err)
		}
		sources := []creditsStateSource{
			{ItemID: "credits-episode-1", LibraryID: "credits-library", RootID: "retired-root", SeriesID: "retired-series", SeasonID: "retired-season", EpisodeKey: "credits-episode-key-1", ItemType: "Episode", SourceRevision: "credits-source-1", ContentSHA256: strings.Repeat("1", 64), DurationTicks: 600 * introskipper.TicksPerSecond},
			{ItemID: "credits-episode-2", LibraryID: "credits-library", RootID: "retired-root", SeriesID: "retired-series", SeasonID: "retired-season", EpisodeKey: "credits-episode-key-2", ItemType: "Episode", SourceRevision: "credits-source-2", ContentSHA256: strings.Repeat("2", 64), DurationTicks: 620 * introskipper.TicksPerSecond},
		}
		if group == 1 {
			sources = []creditsStateSource{{ItemID: "credits-movie", LibraryID: "credits-library", RootID: "retired-root", ItemType: "Movie", SourceRevision: "credits-movie-source", DurationTicks: 1200 * introskipper.TicksPerSecond}}
		}
		for position, source := range sources {
			if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
				source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'retired-hierarchy',$12,1024,0,0,0)`, child, source.ItemID, position, position == 0, source.LibraryID, source.RootID, source.SeriesID, source.SeasonID, source.EpisodeKey, source.ItemType, source.SourceRevision, source.DurationTicks); err != nil {
				t.Fatal(err)
			}
		}
		own := sources[0]
		value := library.AnalysisStoredCreditsResult{Version: library.AnalysisCreditsResultVersion, ItemID: own.ItemID, ItemType: own.ItemType, SourceRevision: own.SourceRevision, DurationTicks: own.DurationTicks, Segments: []library.CreditsInterval{}}
		request := creditsskipper.Request{DurationSeconds: float64(own.DurationTicks) / float64(introskipper.TicksPerSecond), IsMovie: group == 1}
		if group == 0 {
			interval := introskipper.Interval{StartTicks: 500 * introskipper.TicksPerSecond, EndTicks: 580 * introskipper.TicksPerSecond}
			support := []introskipper.Support{}
			for index, source := range sources {
				matched := interval
				if index > 0 {
					matched.StartTicks += 10 * introskipper.TicksPerSecond
					matched.EndTicks += 10 * introskipper.TicksPerSecond
				}
				support = append(support, introskipper.Support{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: source.ContentSHA256, AlgorithmProfile: execution.IntroProfile, Interval: matched})
				value.AudioSources = append(value.AudioSources, library.AnalysisCreditsAudioSource{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: source.ContentSHA256, AlgorithmProfile: execution.IntroProfile, DurationTicks: source.DurationTicks})
			}
			value.Audio = &introskipper.EpisodeResult{EpisodeKey: own.EpisodeKey, SourceKey: own.SourceRevision, ContentIdentity: own.ContentSHA256, AlgorithmProfile: execution.IntroProfile, DurationTicks: own.DurationTicks, Status: introskipper.Qualified, Reasons: []string{}, Candidate: &introskipper.Candidate{Interval: interval, UpstreamCommit: introskipper.UpstreamCommit, Support: support}}
			options := execution.IntroSkipperOptions
			value.AudioOptions = &options
			request.AudioSegments = []creditsskipper.Segment{{Start: 500, End: 580, Source: creditsskipper.ChromaprintSource}}
		} else {
			request.Chapters = []creditsskipper.Chapter{{Name: "Credits", StartSeconds: 1000}}
		}
		visual, err := creditsskipper.Detect(ctx, request, creditsArchiveProbe{})
		if err != nil {
			t.Fatal(err)
		}
		value.Visual = &visual
		for _, segment := range visual.Segments {
			value.Segments = append(value.Segments, library.CreditsInterval{StartTicks: int64(segment.Start * float64(introskipper.TicksPerSecond)), EndTicks: int64(segment.End * float64(introskipper.TicksPerSecond)), Source: string(segment.Source)})
		}
		if len(value.Segments) == 0 {
			t.Fatal("credits fixture did not retain its chapter/audio interval")
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		first := value.Segments[0]
		if library.ValidateStoredCreditsAnalysisResult(raw, "qualified", &first.StartTicks, &first.EndTicks) != nil {
			t.Fatal("invalid full credits result fixture")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_credits_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
			child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published) VALUES($1,1,$2,$3,1,1,$4,repeat('e',64),'qualified',$5,$6,$7,true)`, own.ItemID, own.SourceRevision, fingerprint, child, raw, first.StartTicks, first.EndTicks); err != nil {
			t.Fatal(err)
		}
		for _, source := range sources {
			if _, err := pool.Exec(ctx, `INSERT INTO analysis_credits_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
			VALUES($1,$2,$3,$4,$5,'retired-hierarchy',$6,$7)`, own.ItemID, source.ItemID, source.LibraryID, source.RootID, source.SourceRevision, source.EpisodeKey, source.ContentSHA256); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPostgreSQLAnalysisCreditsArchiveBindsCompleteEvidenceAndPreservesHistory(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAnalysisArchiveCreditsHistory(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		version        int64
		valid          bool
	}{
		{"complete", "", 59, true},
		{"future_task_in_schema_58", "", 58, false},
		{"retired_current_source", `UPDATE items SET media=NULL,file_identity='',file_size=0 WHERE id='credits-episode-1'`, 59, true},
		{"source_abstention", `UPDATE analysis_credits_detections SET status='no_result',start_ticks=NULL,end_ticks=NULL,auto_published=false,
			result=jsonb_build_object('Version','goby-credits-result-v1','ItemID',item_id,'ItemType','Movie','SourceRevision',source_revision,'DurationTicks',12000000000,
			'Segments','[]'::jsonb,'Audio',NULL,'AudioOptions',NULL,'AudioSources','[]'::jsonb,'Visual',NULL,'Reason','source_unavailable') WHERE item_id='credits-movie'`, 59, true},
		{"disabled_current_policy", `UPDATE libraries SET options=jsonb_set(options,'{EnableCreditsDetection}','false') WHERE id='credits-library'`, 59, true},
		{"stale_unpublished_profile", `UPDATE analysis_settings SET revision=2;UPDATE analysis_credits_detections SET auto_published=false`, 59, true},
		{"wrong_result_source", `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{SourceRevision}','"other-source"') WHERE item_id='credits-episode-1'`, 59, false},
		{"wrong_result_item_type", `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{ItemType}','"Movie"') WHERE item_id='credits-episode-1'`, 59, false},
		{"wrong_result_duration", `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{DurationTicks}','6000000001') WHERE item_id='credits-episode-1'`, 59, false},
		{"wrong_child_binding", `UPDATE analysis_credits_detections SET child_id='missing-child' WHERE item_id='credits-episode-1'`, 59, false},
		{"wrong_cohort_binding", `UPDATE analysis_credits_detections SET cohort_revision=repeat('9',64) WHERE item_id='credits-episode-1'`, 59, false},
		{"wrong_source_binding", `UPDATE analysis_credits_detection_sources SET root_id='other-root' WHERE item_id='credits-episode-1' AND source_item_id='credits-episode-2'`, 59, false},
		{"missing_pair_member", `DELETE FROM analysis_credits_detection_sources WHERE item_id='credits-episode-1' AND source_item_id='credits-episode-2'`, 59, false},
		{"missing_visual_source", `DELETE FROM analysis_credits_detection_sources WHERE item_id='credits-movie'`, 59, false},
		{"wrong_pair_content", `UPDATE analysis_credits_detection_sources SET content_sha256=repeat('9',64) WHERE item_id='credits-episode-1' AND source_item_id='credits-episode-2'`, 59, false},
		{"invented_visual_hash", `UPDATE analysis_credits_detection_sources SET content_sha256=repeat('9',64) WHERE item_id='credits-movie'`, 59, false},
		{"wrong_audio_clock", `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{AudioSources,1,DurationTicks}','6300000000') WHERE item_id='credits-episode-1'`, 59, false},
		{"wrong_audio_options", `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{AudioOptions,MaximumTimeSkip}','4') WHERE item_id='credits-episode-1'`, 59, false},
		{"unknown_visual_field", `UPDATE analysis_credits_detections SET result=jsonb_set(result,'{Visual,Unexpected}','true') WHERE item_id='credits-movie'`, 59, false},
		{"missing_nullable_field", `UPDATE analysis_credits_detections SET result=result-'AudioOptions' WHERE item_id='credits-movie'`, 59, false},
		{"stale_published_profile", `UPDATE analysis_settings SET revision=2`, 59, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatal(err)
				}
			}
			err = validateAnalysisState(ctx, tx, test.version)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("credits archive validity=%v result=%v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLAnalysisCreditsTaskCannotEnterSchema58AsGenericHistory(t *testing.T) {
	ctx, source, _, options := recoveryFixtureAtVersion(t, 58)
	if _, err := source.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'media.credits_analysis','Future task');
		INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,finished_at)
		VALUES(repeat('b',32),repeat('a',32),'completed','manual','backup-admin','retired-session','admin','media.credits_analysis','Future task',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if err := configureTransaction(ctx, tx, options.Schema); err != nil {
		t.Fatal(err)
	}
	if err := validateAnalysisState(ctx, tx, 58); !errors.Is(err, ErrSchema) {
		t.Fatalf("future credits task was accepted as historical generic task: %v", err)
	}
}

func TestPostgreSQLAnalysisCreditsHistorySurvivesRawRestore(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveCreditsHistory(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore full credits evidence: %v", err)
	}
	const witness = `SELECT jsonb_build_object(
		'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY run_id) FROM analysis_run_profiles p),
		'work',(SELECT jsonb_agg(to_jsonb(w) ORDER BY child_id) FROM analysis_work w),
		'sources',(SELECT jsonb_agg(to_jsonb(s) ORDER BY child_id,position) FROM analysis_work_sources s),
		'detections',(SELECT jsonb_agg(to_jsonb(d) ORDER BY item_id) FROM analysis_credits_detections d),
		'evidence',(SELECT jsonb_agg(to_jsonb(e) ORDER BY item_id,source_item_id) FROM analysis_credits_detection_sources e))::text`
	var before, after string
	if err := source.QueryRow(ctx, witness).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, witness).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("raw recovery changed full credits source or result evidence")
	}
}
