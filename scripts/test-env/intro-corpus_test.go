//go:build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

type corpusFixture struct {
	config      corpusConfig
	backend     corpusBackend
	identity    extractionIdentity
	cases       []sourceCase
	kinds       map[string]string
	probes      map[string]int
	extractions map[string]int
	matches     int
	matched     []introdetect.Episode
}

func newCorpusFixture(t *testing.T, kinds ...string) *corpusFixture {
	t.Helper()
	dir := t.TempDir()
	f := &corpusFixture{
		config: corpusConfig{mode: "extract", series: "fixture", sources: filepath.Join(dir, "sources.json"),
			labels: filepath.Join(dir, "labels.json"), features: filepath.Join(dir, "features"), output: filepath.Join(dir, "result.json")},
		identity: extractionIdentity{Version: corpusSchema, AlgorithmProfile: "fixture-profile-v1",
			Tools:               media.AnalysisToolFacts{FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64)},
			VisualIntervalTicks: media.TicksPerSecond / 2, MaxWindowTicks: media.MaxIntroAnalysisTicks, Limits: media.DefaultAnalysisLimits()},
		kinds: make(map[string]string), probes: make(map[string]int), extractions: make(map[string]int),
	}
	labels := []byte(`{"detectorOutputsUsed":false,"review":"source-only"}`)
	mustWriteCorpus(t, f.config.labels, labels)
	f.config.labelHash = digestBytes(labels)
	for i, kind := range kinds {
		id := fmt.Sprintf("E%d", i+1)
		body := []byte("independent original bytes for " + id)
		path := filepath.Join(dir, id+".media")
		mustWriteCorpus(t, path, body)
		f.cases = append(f.cases, sourceCase{ID: id, Series: f.config.series, Role: "calibration", Episode: id, Path: path, SHA256: digestBytes(body)})
		f.kinds[id] = kind
	}
	f.saveSources(t)
	f.backend = corpusBackend{
		identity: func(context.Context) (extractionIdentity, error) { return f.identity, nil },
		probe: func(ctx context.Context, file *os.File) (media.Info, error) {
			id := strings.TrimSuffix(filepath.Base(file.Name()), ".media")
			f.probes[id]++
			if f.kinds[id] == "probe_failed" {
				return media.Info{}, errors.New("private probe error at /private/media/path with stderr")
			}
			info := media.Info{DurationTicks: 60 * media.TicksPerSecond}
			if f.kinds[id] != "missing_audio" {
				info.Streams = append(info.Streams, media.Stream{Index: 1, CodecType: "audio"})
			}
			if f.kinds[id] != "missing_video" {
				info.Streams = append(info.Streams, media.Stream{Index: 0, CodecType: "video"})
			}
			return info, nil
		},
		extract: func(ctx context.Context, file *os.File, info media.Info, request media.IntroAnalysisRequest) (media.IntroFeatures, error) {
			id := strings.TrimSuffix(filepath.Base(file.Name()), ".media")
			f.extractions[id]++
			if f.kinds[id] == "extract_failed" {
				return media.IntroFeatures{}, errors.New("private decoder stderr at /private/decoder")
			}
			stat, err := file.Stat()
			if err != nil {
				return media.IntroFeatures{}, err
			}
			identity, err := media.VideoSeekSourceIdentity(stat)
			if err != nil {
				return media.IntroFeatures{}, err
			}
			return media.IntroFeatures{
				AlgorithmProfile: f.identity.AlgorithmProfile, ToolFacts: f.identity.Tools, SourceIdentity: identity,
				Audio:  []introdetect.AudioSample{{StartTicks: 0, EndTicks: media.TicksPerSecond, Fingerprint: 0x91abcd23}},
				Visual: []introdetect.VisualSample{{Ticks: 0, Hash: 0x8198123412341234, Contrast: 100}},
			}, nil
		},
		analyze: func(ctx context.Context, cohort introdetect.Cohort, options introdetect.Options) (introdetect.Result, introdetect.Diagnostics, error) {
			f.matches++
			f.matched = cohort.Episodes
			result := introdetect.Result{Version: introdetect.Version, Options: options}
			for _, episode := range cohort.Episodes {
				result.Episodes = append(result.Episodes, introdetect.EpisodeResult{EpisodeKey: episode.EpisodeKey, Status: introdetect.NoResult})
			}
			return result, introdetect.Diagnostics{Completed: true}, nil
		},
	}
	return f
}

func mustWriteCorpus(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *corpusFixture) saveSources(t *testing.T) {
	t.Helper()
	raw, err := jsonBytes(f.cases)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCorpus(t, f.config.sources, raw)
}

func (f *corpusFixture) statePath() string {
	return filepath.Join(f.config.features, ".intro-corpus-"+digestBytes([]byte(f.config.series))+".json")
}

func (f *corpusFixture) state(t *testing.T) corpusCheckpoint {
	t.Helper()
	raw, err := os.ReadFile(f.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint corpusCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

func (f *corpusFixture) run(t *testing.T, want string) string {
	t.Helper()
	var out bytes.Buffer
	err := runCorpus(context.Background(), f.config, f.backend, &out)
	got := ""
	if err != nil {
		got = err.Error()
	}
	if got != want {
		t.Fatalf("run error = %q, want %q; output=%s", got, want, out.String())
	}
	return out.String()
}

func (f *corpusFixture) report(t *testing.T) corpusReport {
	t.Helper()
	raw, err := os.ReadFile(f.config.output)
	if err != nil {
		t.Fatal(err)
	}
	var report corpusReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestCorpusExtractionContinuesAndRetainsPrivateFailures(t *testing.T) {
	f := newCorpusFixture(t, "missing_audio", "ok", "probe_failed", "missing_video", "extract_failed", "ok")
	out := f.run(t, "extraction_has_failed_cases")
	checkpoint := f.state(t)
	wantStatus := []string{"excluded", "extracted", "failed", "excluded", "failed", "extracted"}
	for i, record := range checkpoint.Records {
		if got := lastAttempt(record).Status; got != wantStatus[i] {
			t.Fatalf("case %s status=%s want=%s", record.Case.ID, got, wantStatus[i])
		}
		if f.probes[record.Case.ID] != 1 {
			t.Fatalf("case %s was not attempted once", record.Case.ID)
		}
	}
	for _, i := range []int{0, 3} {
		if lastAttempt(checkpoint.Records[i]).ProbeInfo == nil {
			t.Fatal("excluded source lost its probe evidence")
		}
	}
	if !strings.Contains(lastAttempt(checkpoint.Records[2]).RawError, "/private/media/path") ||
		!strings.Contains(lastAttempt(checkpoint.Records[4]).RawError, "/private/decoder") {
		t.Fatal("original failure details were discarded")
	}
	if strings.Contains(out, "/private/") || strings.Contains(out, f.config.features) {
		t.Fatal("private details leaked into progress")
	}
	if f.extractions["E1"] != 0 || f.extractions["E4"] != 0 {
		t.Fatal("an excluded source reached extraction")
	}
	info, err := os.Stat(f.statePath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("checkpoint with private errors is not private")
	}
}

func TestCorpusResumeRetriesOnlyFailuresAndPreservesHistory(t *testing.T) {
	f := newCorpusFixture(t, "ok", "extract_failed", "missing_audio", "ok")
	f.run(t, "extraction_has_failed_cases")
	original, err := os.ReadFile(filepath.Join(f.config.features, "E1.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.config.resume = true
	f.kinds["E2"] = "ok"
	f.run(t, "extraction_has_failed_cases")
	if f.probes["E2"] != 1 {
		t.Fatal("resume retried a failure without explicit retry")
	}
	f.config.retryFailed = true
	f.run(t, "")
	state := f.state(t)
	if len(state.Records[1].Attempts) != 2 || state.Records[1].Attempts[0].Status != "failed" ||
		state.Records[1].Attempts[0].RawError == "" || lastAttempt(state.Records[1]).Status != "extracted" {
		t.Fatal("retry did not retain the original failed attempt")
	}
	for _, id := range []string{"E1", "E3", "E4"} {
		if f.probes[id] != 1 {
			t.Fatalf("verified source %s was re-extracted", id)
		}
	}
	after, err := os.ReadFile(filepath.Join(f.config.features, "E1.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("resume overwrote a successful feature")
	}
	if _, err := os.Stat(filepath.Join(f.config.features, "E2.attempt-2.json")); err != nil {
		t.Fatal("retry feature was not retained separately")
	}
}

func TestCorpusExcludedAccountingAndIndependentGate(t *testing.T) {
	for _, test := range []struct {
		name    string
		kinds   []string
		want    string
		matches int
	}{
		{"three_valid_plus_exclusion", []string{"ok", "missing_audio", "ok", "ok"}, "", 1},
		{"two_valid_plus_exclusion", []string{"ok", "missing_video", "ok"}, "insufficient_independent_episodes", 0},
		{"three_valid_plus_failure", []string{"ok", "probe_failed", "ok", "ok"}, "incomplete_corpus", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCorpusFixture(t, test.kinds...)
			extractWant := ""
			if strings.Contains(strings.Join(test.kinds, ","), "failed") {
				extractWant = "extraction_has_failed_cases"
			}
			f.run(t, extractWant)
			f.config.mode = "analyze"
			f.run(t, test.want)
			report := f.report(t)
			if len(report.Cases) != len(test.kinds) || f.matches != test.matches || report.MatcherRan != (test.matches == 1) {
				t.Fatal("complete accounting or matcher gate was lost")
			}
			if test.matches == 1 && (len(f.matched) != 3 || !report.Diagnostics.Completed) {
				t.Fatal("exclusion reached matcher or diagnostics were lost")
			}
		})
	}
}

func TestCorpusAnalysisRejectsChangedEvidence(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, *corpusFixture)
		want   string
	}{
		{"source_bytes", func(t *testing.T, f *corpusFixture) { mustWriteCorpus(t, f.cases[0].Path, []byte("changed bytes")) }, "invalid_corpus"},
		{"missing_feature", func(t *testing.T, f *corpusFixture) {
			if err := os.Remove(filepath.Join(f.config.features, "E1.json")); err != nil {
				t.Fatal(err)
			}
		}, "invalid_corpus"},
		{"feature_bytes", func(t *testing.T, f *corpusFixture) {
			path := filepath.Join(f.config.features, "E1.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mustWriteCorpus(t, path, append(raw, ' '))
		}, "invalid_corpus"},
		{"labels", func(t *testing.T, f *corpusFixture) {
			raw := []byte(`{"detectorOutputsUsed":false,"review":"changed frozen labels"}`)
			mustWriteCorpus(t, f.config.labels, raw)
			f.config.labelHash = digestBytes(raw)
		}, "checkpoint_identity_mismatch"},
		{"inventory", func(t *testing.T, f *corpusFixture) {
			f.cases[0].Episode = "changed"
			f.saveSources(t)
		}, "checkpoint_identity_mismatch"},
		{"profile", func(t *testing.T, f *corpusFixture) { f.identity.AlgorithmProfile = "changed-profile" }, "checkpoint_identity_mismatch"},
		{"tools", func(t *testing.T, f *corpusFixture) { f.identity.Tools.FFmpegSHA256 = strings.Repeat("d", 64) }, "checkpoint_identity_mismatch"},
		{"limits", func(t *testing.T, f *corpusFixture) { f.identity.Limits.MaxAudioFrames++ }, "checkpoint_identity_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newCorpusFixture(t, "ok", "ok", "ok")
			f.run(t, "")
			test.change(t, f)
			f.config.mode = "analyze"
			f.run(t, test.want)
			if f.matches != 0 {
				t.Fatal("changed evidence reached matcher")
			}
			report := f.report(t)
			if len(report.Cases) != len(f.cases) || report.MatcherRan {
				t.Fatal("blocked analysis lost complete case accounting")
			}
		})
	}
}

func TestCorpusResumeInvalidationRequiresNewAttempt(t *testing.T) {
	f := newCorpusFixture(t, "ok", "ok", "ok")
	f.run(t, "")
	mustWriteCorpus(t, filepath.Join(f.config.features, "E1.json"), []byte("corrupted retained feature"))
	f.config.resume, f.config.retryFailed = true, true
	f.run(t, "extraction_has_failed_cases")
	state := f.state(t)
	if len(state.Records[0].Attempts) != 2 || lastAttempt(state.Records[0]).Reason != "feature_digest_mismatch" || f.extractions["E1"] != 1 {
		t.Fatal("invalidated evidence was silently repaired or admitted")
	}
	f.run(t, "")
	state = f.state(t)
	if len(state.Records[0].Attempts) != 3 || lastAttempt(state.Records[0]).Status != "extracted" || f.extractions["E1"] != 2 {
		t.Fatal("explicit later retry failed to produce a new receipt")
	}
}

func TestCorpusInterruptedAttemptCannotAuthorizeOrphanFeature(t *testing.T) {
	f := newCorpusFixture(t, "ok", "ok", "ok")
	f.run(t, "")
	state := f.state(t)
	state.Records[0].Attempts[0].Status = "failed"
	state.Records[0].Attempts[0].Reason = "interrupted"
	state.Records[0].Attempts[0].CompletedAt = time.Time{}
	state.Records[0].Attempts[0].FeatureSHA256 = ""
	state.Records[0].Attempts[0].FeatureFile = ""
	if err := saveCheckpoint(f.statePath(), state); err != nil {
		t.Fatal(err)
	}
	f.config.mode = "analyze"
	f.run(t, "incomplete_corpus")
	if f.matches != 0 {
		t.Fatal("orphan feature authorized matching")
	}
	f.config.mode, f.config.resume, f.config.retryFailed = "extract", true, true
	f.run(t, "")
	if f.extractions["E1"] != 2 || lastAttempt(f.state(t).Records[0]).Number != 2 {
		t.Fatal("interrupted attempt could not resume without overwriting evidence")
	}
}

func TestCorpusDuplicateContentOrEpisodeCannotCountAsSupport(t *testing.T) {
	for _, dimension := range []string{"episode", "content"} {
		t.Run(dimension, func(t *testing.T) {
			f := newCorpusFixture(t, "ok", "ok", "ok", "ok")
			if dimension == "episode" {
				f.cases[1].Episode = f.cases[0].Episode
			} else {
				raw, err := os.ReadFile(f.cases[0].Path)
				if err != nil {
					t.Fatal(err)
				}
				mustWriteCorpus(t, f.cases[1].Path, raw)
				f.cases[1].SHA256 = f.cases[0].SHA256
			}
			f.saveSources(t)
			f.run(t, "")
			f.config.mode = "analyze"
			f.run(t, "invalid_corpus")
			if f.matches != 0 || f.report(t).Cases[1].Reason != "duplicate_independent_identity" {
				t.Fatal("duplicate identity entered matcher")
			}
		})
	}
}

func TestCorpusOptionsRemainCalibrationOnlyAndComplete(t *testing.T) {
	f := newCorpusFixture(t, "ok", "ok", "ok")
	f.cases[1].Role = "holdout"
	f.saveSources(t)
	f.config.mode = "analyze"
	f.config.options = filepath.Join(filepath.Dir(f.config.sources), "options.json")
	raw, err := jsonBytes(introdetect.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCorpus(t, f.config.options, raw)
	f.run(t, "options_require_calibration_only_sources")
	if f.matches != 0 {
		t.Fatal("holdout option experiment reached matcher")
	}
	f.cases[1].Role = "calibration"
	f.saveSources(t)
	f.config.output = filepath.Join(filepath.Dir(f.config.output), "options-blocked.json")
	var options map[string]json.RawMessage
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	delete(options, "MaxAudioGapTicks")
	raw, err = json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCorpus(t, f.config.options, raw)
	f.run(t, "complete_detector_options_required")
}

func TestCorpusRejectsUnsafeIDsUnreceiptedCacheAndConcurrentRun(t *testing.T) {
	t.Run("unsafe_id", func(t *testing.T) {
		f := newCorpusFixture(t, "ok", "ok", "ok")
		f.cases[0].ID = "../private-secret"
		f.saveSources(t)
		f.run(t, "invalid_source_inventory")
	})
	t.Run("legacy_cache", func(t *testing.T) {
		f := newCorpusFixture(t, "ok", "ok", "ok")
		if err := os.MkdirAll(f.config.features, 0700); err != nil {
			t.Fatal(err)
		}
		mustWriteCorpus(t, filepath.Join(f.config.features, "E1.json"), []byte("{}"))
		f.run(t, "unreceipted_features_require_new_directory")
	})
	t.Run("concurrent_run", func(t *testing.T) {
		f := newCorpusFixture(t, "ok", "ok", "ok")
		if err := os.MkdirAll(f.config.features, 0700); err != nil {
			t.Fatal(err)
		}
		lock, err := os.OpenFile(filepath.Join(f.config.features, ".intro-corpus.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		f.run(t, "corpus_in_use")
		if len(f.probes) != 0 {
			t.Fatal("concurrent run started extraction")
		}
	})
	t.Run("unsafe_checkpoint_reason", func(t *testing.T) {
		f := newCorpusFixture(t, "probe_failed", "ok", "ok")
		f.run(t, "extraction_has_failed_cases")
		state := f.state(t)
		state.Records[0].Attempts[0].Reason = "failure /private/source"
		if err := saveCheckpoint(f.statePath(), state); err != nil {
			t.Fatal(err)
		}
		f.config.resume = true
		f.run(t, "invalid_checkpoint_attempt")
	})
}

func TestCorpusSelectedStreamHandlesAbsentAndDefaultStreams(t *testing.T) {
	info := media.Info{Streams: []media.Stream{
		{Index: 0, CodecType: "video", IsAttachedPicture: true},
		{Index: 1, CodecType: "audio", IsExternal: true},
		{Index: 4, CodecType: "audio"},
		{Index: 3, CodecType: "audio", IsDefault: true},
		{Index: 2, CodecType: "audio", IsDefault: true},
	}}
	if index, ok := selectedStream(info, "audio"); !ok || index != 2 {
		t.Fatal("default stream selection changed")
	}
	if _, ok := selectedStream(info, "video"); ok {
		t.Fatal("attached picture counted as video")
	}
}

func TestCorpusFailureDoesNotExposeRawError(t *testing.T) {
	err := fmt.Errorf("failed opening /private/source: %w", io.ErrUnexpectedEOF)
	if got := safeReason(err, "source_read_failed"); got != "source_read_failed" {
		t.Fatalf("unsafe reason: %s", got)
	}
}

func TestCorpusNonRegularSourceDoesNotBlockRemainingCases(t *testing.T) {
	f := newCorpusFixture(t, "ok", "ok", "ok", "ok")
	if err := os.Remove(f.cases[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(f.cases[0].Path, 0600); err != nil {
		t.Fatal(err)
	}
	f.run(t, "extraction_has_failed_cases")
	state := f.state(t)
	if lastAttempt(state.Records[0]).Status != "failed" || len(f.probes) != 3 {
		t.Fatal("non-regular source blocked the remaining corpus")
	}
}

func TestCorpusExclusionRejectsToolOrSourceDriftDuringProbe(t *testing.T) {
	for _, changed := range []string{"tools", "source"} {
		t.Run(changed, func(t *testing.T) {
			f := newCorpusFixture(t, "missing_audio", "ok", "ok")
			probe := f.backend.probe
			f.backend.probe = func(ctx context.Context, file *os.File) (media.Info, error) {
				info, err := probe(ctx, file)
				if strings.HasSuffix(file.Name(), "E1.media") {
					if changed == "tools" {
						f.identity.Tools.FFprobeSHA256 = strings.Repeat("d", 64)
					} else {
						mustWriteCorpus(t, file.Name(), []byte("changed while probing"))
					}
				}
				return info, err
			}
			f.run(t, "extraction_has_failed_cases")
			attempt := lastAttempt(f.state(t).Records[0])
			want := "source_changed_during_probe"
			if changed == "tools" {
				want = "probe_tool_identity_changed"
			}
			if attempt.Status != "failed" || attempt.Reason != want {
				t.Fatal("changed evidence authorized an exclusion")
			}
		})
	}
}

func TestCorpusVisualExperimentIsOptInAndSeparateFromProductionResult(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			f := newCorpusFixture(t, "ok", "ok", "ok", "missing_audio")
			visualCalls := 0
			f.backend.visual = func(ctx context.Context, cohort introdetect.Cohort, options introdetect.Options) (introdetect.VisualSequenceResult, error) {
				visualCalls++
				if len(cohort.Episodes) != 3 {
					t.Fatal("excluded case reached visual experiment")
				}
				return introdetect.VisualSequenceResult{Version: introdetect.VisualSequenceVersion, Experimental: true,
					Groups: []introdetect.VisualSequenceGroup{{Members: []introdetect.Support{{EpisodeKey: cohort.Episodes[0].EpisodeKey}}}}}, nil
			}
			f.run(t, "")
			f.config.mode, f.config.visualExperiment = "analyze", enabled
			out := f.run(t, "")
			report := f.report(t)
			wantCalls := 0
			if enabled {
				wantCalls = 1
			}
			if (report.VisualExperiment != nil) != enabled || visualCalls != wantCalls {
				t.Fatal("visual experiment was not opt-in")
			}
			if !report.MatcherRan || len(report.Result.Groups) != 0 || !report.Diagnostics.Completed {
				t.Fatal("visual experiment replaced the production result")
			}
			if enabled && (!report.VisualExperiment.Experimental || !strings.Contains(out, "visual_experiment groups=1 comparisons=0 publishable=false")) {
				t.Fatal("experiment output lost its non-publishable boundary")
			}
		})
	}
	t.Run("invalid_corpus", func(t *testing.T) {
		f := newCorpusFixture(t, "ok", "ok", "probe_failed")
		f.backend.visual = func(context.Context, introdetect.Cohort, introdetect.Options) (introdetect.VisualSequenceResult, error) {
			t.Fatal("invalid corpus reached visual experiment")
			return introdetect.VisualSequenceResult{}, nil
		}
		f.run(t, "extraction_has_failed_cases")
		f.config.mode, f.config.visualExperiment = "analyze", true
		f.run(t, "incomplete_corpus")
		if f.report(t).VisualExperiment != nil {
			t.Fatal("invalid corpus produced experimental observations")
		}
	})
}
