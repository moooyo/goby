//go:build linux

package library

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisHistoricalVisualEvidenceRemainsEffectiveAndHonorsLibraryPolicy(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	f.setIntroDetection(t, true)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, run, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	work = analysisHistoricalVisualWork(t, work)
	result := analysisFixtureLegacyQualifiedResult(t, f, work)
	visual := *analysisVisualDetectionTestValue().Episode.Candidates[0].VisualEvidence
	interval := introdetect.Interval{StartTicks: 10 * media.TicksPerSecond, EndTicks: 18 * media.TicksPerSecond}
	result.Groups[0].Metrics = introdetect.Metrics{}
	result.Groups[0].VisualEvidence = &visual
	for index := range result.Groups[0].Members {
		result.Groups[0].Members[index].Interval = interval
	}
	for index := range result.Episodes {
		candidate := &result.Episodes[index].Candidates[0]
		candidate.Interval, candidate.Metrics = interval, introdetect.Metrics{}
		copy := visual
		candidate.VisualEvidence = &copy
	}
	rejecting := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		return ErrForbidden
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], rejecting, result); !errors.Is(err, ErrForbidden) {
		t.Fatalf("visual publication bypassed its worker fence: %v", err)
	}
	seedAnalysisHistoricalVisualResult(t, f, children[0], fence, work, result)
	for _, source := range work.Sources {
		item, err := f.store.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		if err != nil || item.Detection.Candidate == nil || !reflect.DeepEqual(item.Detection.Candidate.VisualEvidence, &visual) ||
			item.Detection.Candidate.Metrics != (introdetect.Metrics{}) {
			t.Fatalf("persisted visual evidence changed: %+v %v", item.Detection, err)
		}
		intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, "")
		if err != nil || intro == nil || intro.Provenance != "Detected" || intro.StartTicks != interval.StartTicks || intro.EndTicks != interval.EndTicks {
			t.Fatalf("qualified visual interval was not available to playback: %+v %v", intro, err)
		}
	}
	f.setIntroDetection(t, false)
	for _, source := range work.Sources {
		if intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, source.ItemID, ""); err != nil || intro != nil {
			t.Fatalf("disabled library retained visual publication authority: %+v %v", intro, err)
		}
	}
}

// These helpers restore validated historical evidence only inside tests. The
// current task remains native v6, so production cache and publication APIs must
// reject the retired visual execution before its historical rows are seeded.
func analysisHistoricalVisualWork(t *testing.T, current AnalysisWork) AnalysisWork {
	t.Helper()
	current.Execution = analysisLegacyTestIntroExecution()
	profile := analysisStoredProfileV5{
		AutoPublishIntros:      current.Profile.AutoPublishIntros,
		PreviewIntervalSeconds: current.Profile.PreviewIntervalSeconds,
		PreviewQuality:         current.Profile.PreviewQuality,
		MaxSourceBytes:         current.Profile.MaxSourceBytes,
		MaxItemRuntimeSeconds:  current.Profile.MaxItemRuntimeSeconds,
		FeatureCacheMaxBytes:   current.Profile.FeatureCacheMaxBytes,
	}
	execution := analysisStoredExecutionV5{
		Version: current.Execution.Version, Available: current.Execution.Available,
		FFmpegSHA256: current.Execution.FFmpegSHA256, FFprobeSHA256: current.Execution.FFprobeSHA256,
		FingerprintSHA256: current.Execution.FingerprintSHA256, DetectorVersion: current.Execution.DetectorVersion,
		DetectorOptions:     analysisStoredOptionsV5(current.Execution.DetectorOptions),
		VisualIntervalTicks: current.Execution.VisualIntervalTicks, IntroProfile: current.Execution.IntroProfile,
	}
	revision, err := analysisRevision(current.ConfigurationRevision)
	if err != nil {
		t.Fatal(err)
	}
	current.ConfigurationFingerprint = analysisAdmissionFingerprintV5(profile, execution, revision, current.PublicationEpoch)
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	executionRaw, err := json.Marshal(execution)
	if err != nil || ValidateStoredAnalysisAdmission(profileRaw, executionRaw, revision, current.PublicationEpoch, current.ConfigurationFingerprint) != nil {
		t.Fatalf("historical visual fixture lacks a valid frozen v5 admission: %v", err)
	}
	return current
}

func analysisHistoricalVisualCache(t *testing.T, f analysisWorkFixture, work AnalysisWork, source AnalysisSource, features AnalysisFeatures) AnalysisFeatures {
	t.Helper()
	if err := f.store.PutAnalysisFeatures(f.ctx, work.ChildID, source.ItemID, f.fence(work.ChildID), features); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("native worker accepted retired visual cache evidence: %v", err)
	}
	var cached AnalysisFeatures
	if err := f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		if err := putAnalysisFeatureCache(tx, source, work.ConfigurationFingerprint, work.Profile, features); err != nil {
			return err
		}
		var found bool
		var err error
		cached, found, err = loadAnalysisFeatureCache(tx, source, work.ConfigurationFingerprint)
		if err == nil && !found {
			return ErrUnavailable
		}
		return err
	}); err != nil || !reflect.DeepEqual(cached, features) {
		t.Fatalf("historical cache changed measured evidence or source identity: %v", err)
	}
	var payload []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT payload FROM analysis_feature_cache WHERE item_id=$1 AND profile_fingerprint=$2`,
		source.ItemID, work.ConfigurationFingerprint).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) < 6 || string(payload[:4]) != "GAFB" || binary.LittleEndian.Uint16(payload[4:6]) != 3 {
		t.Fatal("historical visual cache was promoted out of GAFB v3")
	}
	if _, found, err := f.store.GetAnalysisFeatures(f.ctx, work.ChildID, source.ItemID, f.fence(work.ChildID)); err != nil || found {
		t.Fatalf("native worker acquired a historical visual snapshot: found=%v error=%v", found, err)
	}
	return cached
}

func seedAnalysisHistoricalVisualResult(t *testing.T, f analysisWorkFixture, child string, fence AnalysisFence, work AnalysisWork, result introdetect.Result) {
	t.Helper()
	values, err := validateAnalysisResultForWork(work, result)
	if err != nil {
		t.Fatalf("actual visual matcher output fails historical evidence validation: %v", err)
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, child, fence, result); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("native worker published retired visual matcher output: %v", err)
	}
	var detections, audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_detections),(SELECT count(*) FROM analysis_intro_audit)`).Scan(&detections, &audits); err != nil || detections != 0 || audits != 0 {
		t.Fatalf("rejected retired publication changed durable evidence: detections=%d audits=%d error=%v", detections, audits, err)
	}
	// Model rows restored from an earlier installation. This does not create or
	// resume a v5 task; each serialized result and audit is checked by the frozen
	// readers before insertion, and every support retains its original identity.
	if err := f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		for _, source := range work.Sources {
			if !source.Target {
				continue
			}
			value := values[source.ItemID]
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			start, end := analysisStoredInterval(value)
			if err := ValidateStoredAnalysisResult(raw, string(value.Episode.Status), start, end); err != nil {
				return err
			}
			evidence, err := json.Marshal(AnalysisAuditEvidence{Result: &value})
			if err != nil {
				return err
			}
			if err := ValidateStoredAnalysisAudit(evidence, string(value.Episode.Status)); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,
				profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
				SELECT $1,1,$2,$3,revision,publication_epoch,$4,$5,$6,$7,$8,$9,true FROM analysis_settings WHERE id=1`,
				source.ItemID, source.SourceRevision, work.ConfigurationFingerprint, "historical-v5-"+child, work.CohortRevision,
				string(value.Episode.Status), raw, start, end); err != nil {
				return err
			}
			contents := map[string]string{value.Episode.SourceKey: value.Episode.ContentIdentity}
			for _, candidate := range value.Episode.Candidates {
				for _, support := range candidate.Support {
					contents[support.SourceKey] = support.ContentIdentity
				}
			}
			for _, support := range work.Sources {
				content, exists := contents[support.SourceRevision]
				if !exists {
					continue
				}
				if _, err := tx.Exec(`INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,
					source_revision,hierarchy_revision,episode_key,content_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
					source.ItemID, support.ItemID, support.LibraryID, support.RootID, support.SourceRevision,
					support.HierarchyRevision, support.EpisodeKey, content); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
				VALUES($1,1,$2,$3,$4,$5)`, source.ItemID, source.SourceRevision, work.ConfigurationFingerprint, string(value.Episode.Status), evidence); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("restore validated historical visual evidence: %v", err)
	}
}
