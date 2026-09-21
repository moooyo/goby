//go:build linux

package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func analysisRecoveryFingerprint(profile library.AnalysisProfile, execution library.AnalysisExecutionProfile) string {
	raw, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         library.AnalysisProfile
		Execution       library.AnalysisExecutionProfile
	}{library.AnalysisExecutionProfileVersion, 1, 1, profile, execution})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// This is a logical archive fixture, not a media-analysis accuracy fixture.
// The deliberately absent filesystem cache must never be opened by recovery.
func seedAnalysisRecoveryFixture(t *testing.T, f *engineRecoveryFixture) {
	t.Helper()
	items := []string{"analysis-restore-one", "analysis-restore-two", "analysis-restore-three"}
	sources := []string{"source-one", "source-two", "source-three"}
	episodes := []string{"episode-one", "episode-two", "episode-three"}
	contents := []string{strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)}
	for _, item := range items {
		if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)VALUES($1,$2,$1,$1,'Movie',false)`, item, f.libraryID); err != nil {
			t.Fatal("seed reclassified retained analysis item")
		}
	}
	profile := library.DefaultAnalysisProfile()
	profileRaw, _ := json.Marshal(profile)
	intro := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, Available: true, FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64), DetectorVersion: introdetect.Version, DetectorOptions: introdetect.DefaultOptions(), VisualIntervalTicks: media.TicksPerSecond / 2, IntroProfile: "archive-intro-v1"}
	preview := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, Available: true, FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), PreviewProfile: media.PreviewAnalysisProfile, PreviewWidths: []int{240, 320, 400}}
	children := make([]string, 2)
	fingerprints := make([]string, 2)
	for index, execution := range []library.AnalysisExecutionProfile{intro, preview} {
		key := library.TaskIntroAnalysisKey
		if index == 1 {
			key = library.TaskPreviewGenerationKey
		}
		run, definition, scope := strings.Repeat("5", 32), strings.Repeat("7", 32), "analysis:"+strings.Repeat("a", 64)
		if index == 1 {
			run, definition, scope = strings.Repeat("6", 32), strings.Repeat("8", 32), "analysis:"+strings.Repeat("b", 64)
		}
		childDigest := sha256.Sum256([]byte(run + ":" + scope))
		children[index] = hex.EncodeToString(childDigest[:16])
		fingerprints[index] = analysisRecoveryFingerprint(profile, execution)
		encoded, _ := json.Marshal(execution)
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_definitions(id,key,name)VALUES($1,$2,$2)`, definition, key); err != nil {
			t.Fatal("seed analysis task definition")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,analysis_input,analysis_config_fingerprint,total_children,started_at)
			VALUES($1,$2,'running','manual',$3,$4,'admin',$5,$5,'{}',$6,1,clock_timestamp())`, run, definition, f.actor.User.ID, f.actor.SessionID, key, fingerprints[index]); err != nil {
			t.Fatal("seed unfinished analysis task authority")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)VALUES($1,1,1,$2,$3,$4)`, run, fingerprints[index], profileRaw, encoded); err != nil {
			t.Fatal("seed exact analysis admission profile")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,executor_token)
			VALUES($1,$2,$3,'Analysis recovery',0,$4,'running',clock_timestamp(),md5($1||':executor'))`, children[index], run, f.libraryID, scope); err != nil {
			t.Fatal("seed unfinished analysis child")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)VALUES($1,$2,$3,$4,$5,$6,false)`, children[index], run, key, f.libraryID, scope, strings.Repeat("c", 64)); err != nil {
			t.Fatal("seed immutable analysis work")
		}
		count := 3
		if index == 1 {
			count = 1
		}
		for position := 0; position < count; position++ {
			if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
				VALUES($1,$2,$3,$4,$5,'retired-root','retired-series','retired-season',$6,'Episode',$7,'retired-hierarchy',600000000,1024,1,1,3)`, children[index], items[position], position, position == 0, f.libraryID, episodes[position], sources[position]); err != nil {
				t.Fatal("seed retained analysis source window")
			}
		}
	}
	interval := introdetect.Interval{StartTicks: 50000000, EndTicks: 400000000}
	support := []introdetect.Support{}
	for index := range items {
		support = append(support, introdetect.Support{EpisodeKey: episodes[index], SourceKey: sources[index], ContentIdentity: contents[index], Interval: interval})
	}
	metrics := introdetect.Metrics{AudioAgreementPermille: 1000, AudioInformativePermille: 1000, AudioSimilarityPermille: 1000, AudioSamples: 100, AudioDistinct: 100, VisualAgreementPermille: 1000, VisualSimilarityPermille: 1000, VisualCoveragePermille: 1000, VisualSamples: 30, VisualTransitions: 10, VisualChangeCoveragePermille: 1000, PairCount: 3}
	metrics.VisualAnchorCount, metrics.VisualMinBandMatchedPermille, metrics.VisualMatchedTimePermille = 35, 1000, 1000
	metrics.VisualDistinctStates, metrics.VisualDominantStatePermille = 8, 125
	stored := library.AnalysisStoredResult{Version: introdetect.Version, Episode: introdetect.EpisodeResult{EpisodeKey: episodes[0], SourceKey: sources[0], ContentIdentity: contents[0], Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{{Interval: interval, GroupID: "retained-group", Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Metrics: metrics, Support: support}}}}
	resultRaw, _ := json.Marshal(stored)
	if library.ValidateStoredAnalysisResult(resultRaw, "qualified", &interval.StartTicks, &interval.EndTicks) != nil {
		t.Fatal("logical detection fixture violated its typed evidence contract")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
		VALUES($1,2,$2,$3,1,1,$4,$5,'qualified',$6,$7,$8,true)`, items[0], sources[0], fingerprints[0], children[0], strings.Repeat("c", 64), resultRaw, interval.StartTicks, interval.EndTicks); err != nil {
		t.Fatal("seed retained automatic result")
	}
	for index := range items {
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)VALUES($1,$2,$3,'retired-root',$4,'retired-hierarchy',$5,$6)`, items[0], items[index], f.libraryID, sources[index], episodes[index], contents[index]); err != nil {
			t.Fatal("seed exact target and support references")
		}
	}
	evidence, _ := json.Marshal(library.AnalysisAuditEvidence{Result: &stored})
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)VALUES($1,2,$2,$3,'qualified',$4)`, items[0], sources[0], fingerprints[0], evidence); err != nil {
		t.Fatal("seed retained detection audit")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_intro_decisions(item_id,revision,source_revision,rejected,updated_by)VALUES($1,1,$2,false,$3)`, items[0], sources[0], f.actor.User.ID); err != nil {
		t.Fatal("seed explicit analysis decision")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_preview_state(item_id,revision)VALUES($1,3)`, items[0]); err != nil {
		t.Fatal("seed preview clear tombstone")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO item_intro_state(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by)VALUES($1,1,$2,0,10000000,'Manual',$3)`, items[0], sources[0], f.actor.User.ID); err != nil {
		t.Fatal("seed explicit manual state and preview tombstone")
	}
	decisionEvidence, _ := json.Marshal(library.AnalysisAuditEvidence{Decision: &library.AnalysisDecision{Revision: "0", ManualRevision: "0", SourceRevision: sources[0], Action: "reset"}})
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence,actor_id)VALUES($1,1,$2,'','reset',$3,$4)`, items[0], sources[0], decisionEvidence, f.actor.User.ID); err != nil {
		t.Fatal("seed explicit decision audit with nullable result evidence")
	}
	feature := library.AnalysisFeatures{ContentSHA256: contents[0], AlgorithmProfile: intro.IntroProfile}
	payload, err := library.EncodeAnalysisFeatures(feature, 600000000)
	if err != nil {
		t.Fatal("encode retained feature fixture")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("goby.analysis.feature-cache.v1"))
	for _, value := range []string{items[0], sources[0], fingerprints[0]} {
		var length [8]byte
		binary.LittleEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_feature_cache(cache_key,item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,duration_ticks,payload,bytes)VALUES($1,$2,$3,$4,$5,$6,600000000,$7,$8)`, hex.EncodeToString(hash.Sum(nil)), items[0], sources[0], fingerprints[0], contents[0], feature.AlgorithmProfile, payload, len(payload)); err != nil {
		t.Fatal("seed source-bound compact features")
	}
	timeline, err := library.EncodeAnalysisPreviewTimeline([]int64{0, 100000000, 200000000, 300000000, 400000000, 500000000}, []int64{0, 100000000, 200000000, 300000000, 400000000, 500000000})
	if err != nil {
		t.Fatal("encode retained preview timeline")
	}
	for _, width := range []int{240, 320, 400} {
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_previews(item_id,width,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,cache_key,seal,height,content_sha256,bytes,frame_count,interval_ticks,timeline)
		VALUES($1,$2,1,$3,$4,1,1,$5,repeat('a',64),repeat('b',64),180,repeat('c',64),8192,6,100000000,$6)`, items[0], width, sources[0], fingerprints[1], children[1], timeline); err != nil {
			t.Fatal("seed complete derivative-reference family without filesystem cache bytes")
		}
	}
}

const analysisRecoveryRetainedSQL = `SELECT jsonb_build_object(
	'settings',(SELECT to_jsonb(s)-'publication_epoch' FROM analysis_settings s),
	'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY run_id) FROM analysis_run_profiles p),
	'work',(SELECT jsonb_agg(to_jsonb(w) ORDER BY child_id) FROM analysis_work w),
	'sources',(SELECT jsonb_agg(to_jsonb(s) ORDER BY child_id,position) FROM analysis_work_sources s),
	'detections',(SELECT jsonb_agg(to_jsonb(d)-'auto_published' ORDER BY item_id) FROM analysis_detections d),
	'support',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id,source_item_id) FROM analysis_detection_sources s),
	'decisions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY item_id) FROM analysis_intro_decisions d),
	'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM analysis_intro_audit a),
	'preview_state',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id) FROM analysis_preview_state s),
	'manual',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_intro_state m))::text`

func TestEngineAnalysisRestoreVerifiesRawThenInvalidatesEveryExecutionProof(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	seedAnalysisRecoveryFixture(t, f)
	before := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL)
	manifest, metadata := f.create(t)
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open encrypted analysis archive")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatal("decrypt owned analysis archive")
	}
	defer archive.Close()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("acquire target recovery lease")
	}
	defer lease.Close()
	refused := errors.New("raw analysis witness completed")
	called := false
	_, err = backuppg.RestoreFinalized(f.ctx, f.source, f.target, archive.Database(), manifest.Source, f.engine.options, func(ctx context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
		called = true
		if !reflect.DeepEqual(raw.Tables, manifest.Source.Tables) {
			t.Error("raw analysis table fingerprints changed before normalization")
		}
		var valid bool
		if tx.QueryRow(ctx, `SELECT (SELECT publication_epoch=1 FROM analysis_settings WHERE id=1) AND (SELECT count(*) FROM analysis_previews)=3 AND (SELECT count(*) FROM analysis_feature_cache)=1 AND (SELECT count(*) FROM analysis_detections WHERE auto_published)=1`).Scan(&valid) != nil || !valid {
			t.Error("normalization ran before raw analysis proof")
		}
		return refused
	})
	if !called || !errors.Is(err, refused) {
		t.Fatal("raw witness did not roll back completely")
	}
	if _, err := archive.Database().Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind same authenticated analysis archive")
	}
	result, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil || result.AnalysisPublicationEpoch != 2 || result.DisabledAnalysisDetections != 1 || result.RemovedAnalysisPreviews != 3 || result.RemovedAnalysisFeatures != 1 {
		t.Fatalf("analysis restore counts=%+v error=%v", result, err)
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.target, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("analysis recovery changed explicit settings, manual state, immutable admission or audit evidence")
	}
	var safe bool
	if f.target.QueryRow(f.ctx, `SELECT NOT EXISTS(SELECT 1 FROM analysis_previews) AND NOT EXISTS(SELECT 1 FROM analysis_feature_cache)
		AND NOT EXISTS(SELECT 1 FROM analysis_detections WHERE auto_published) AND (SELECT publication_epoch=2 FROM analysis_settings WHERE id=1)
		AND (SELECT count(*) FROM task_runs WHERE task_key IN ('media.intro_analysis','media.preview_generation') AND state='interrupted')=2
		AND NOT EXISTS(SELECT 1 FROM task_run_children WHERE analysis_scope_key<>'' AND state<>'interrupted')`).Scan(&safe) != nil || !safe {
		t.Fatal("restored analysis retained a cache reference, automatic result or executable task")
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("analysis recovery mutated its original source")
	}
}
