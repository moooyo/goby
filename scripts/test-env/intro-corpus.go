//go:build ignore

// intro-corpus extracts production features and evaluates frozen source cohorts.
// It never downloads media, assigns labels, or publishes application markers.
// Run its focused tests with: go test intro-corpus.go intro-corpus_test.go
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

const corpusSchema = "intro-corpus-v2"

var safeCaseID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var safeReasonCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

type sourceCase struct {
	ID      string `json:"id"`
	Series  string `json:"series"`
	Role    string `json:"role"`
	Episode string `json:"episode"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

type sourceFeatures struct {
	Case     sourceCase
	Info     media.Info
	Features media.IntroFeatures
}

type extractionIdentity struct {
	Version             string
	AlgorithmProfile    string
	Tools               media.AnalysisToolFacts
	Fingerprint         media.AudioFingerprintMetadata
	VisualIntervalTicks int64
	MaxWindowTicks      int64
	Limits              media.AnalysisLimits
}

// The checkpoint is private: it retains raw errors and source paths. Public
// progress and analysis accounting expose only validated IDs and reason codes.
type corpusAttempt struct {
	Number         int
	StartedAt      time.Time
	CompletedAt    time.Time `json:",omitempty"`
	Status         string
	Reason         string      `json:",omitempty"`
	RawError       string      `json:",omitempty"`
	FeatureFile    string      `json:",omitempty"`
	FeatureSHA256  string      `json:",omitempty"`
	SourceIdentity string      `json:",omitempty"`
	ProbeInfo      *media.Info `json:",omitempty"`
}

type corpusRecord struct {
	Case     sourceCase
	Attempts []corpusAttempt
}

type corpusCheckpoint struct {
	Schema         string
	Series         string
	SourcesSHA256  string
	LabelsSHA256   string
	Identity       extractionIdentity
	IdentitySHA256 string
	Records        []corpusRecord
}

type corpusCaseResult struct {
	ID     string
	Status string
	Reason string `json:",omitempty"`
}

type corpusReport struct {
	Schema         string
	LabelsSHA256   string
	SourcesSHA256  string
	IdentitySHA256 string
	MatcherRan     bool
	Reason         string `json:",omitempty"`
	Cases          []corpusCaseResult
	Result         introdetect.Result
	Diagnostics    introdetect.Diagnostics
	Options        introdetect.Options
	// VisualExperiment is a separate observation, never a publishable marker.
	VisualExperiment *introdetect.VisualSequenceResult `json:",omitempty"`
}

type corpusConfig struct {
	mode, sources, labels, labelHash, series, features, output, options string
	ffmpeg, ffprobe, helper                                             string
	resume, retryFailed                                                 bool
	visualExperiment                                                    bool
}

type corpusBackend struct {
	identity func(context.Context) (extractionIdentity, error)
	probe    func(context.Context, *os.File) (media.Info, error)
	extract  func(context.Context, *os.File, media.Info, media.IntroAnalysisRequest) (media.IntroFeatures, error)
	analyze  func(context.Context, introdetect.Cohort, introdetect.Options) (introdetect.Result, introdetect.Diagnostics, error)
	visual   func(context.Context, introdetect.Cohort, introdetect.Options) (introdetect.VisualSequenceResult, error)
}

// A classified error never exposes command output or filesystem paths.
type corpusFailure string

func (e corpusFailure) Error() string { return string(e) }

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && value == strings.ToLower(value)
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func jsonBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	return append(data, '\n'), err
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, corpusFailure("invalid_file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = corpusFailure("file_budget_exceeded")
	}
	return data, err
}

func openRegular(path string) (*os.File, error) {
	// O_NONBLOCK prevents a replaced source or checkpoint FIFO from hanging
	// before the regular-file check, where cancellation cannot reach os.Open.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, corpusFailure("invalid_file")
	}
	return file, nil
}

func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func saveCheckpoint(path string, checkpoint corpusCheckpoint) error {
	data, err := jsonBytes(checkpoint)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".intro-corpus-checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	// Persist the rename as well as its bytes. An interrupted process cannot
	// expose a partially rewritten checkpoint or authorize an orphan feature.
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func selectedStream(info media.Info, kind string) (int, bool) {
	selected, defaultStream := -1, false
	for _, stream := range info.Streams {
		if stream.CodecType != kind || stream.IsExternal || stream.IsAttachedPicture {
			continue
		}
		if selected == -1 || stream.IsDefault && !defaultStream || stream.IsDefault == defaultStream && stream.Index < selected {
			selected, defaultStream = stream.Index, stream.IsDefault
		}
	}
	return selected, selected >= 0
}

func parseCorpusConfig(args []string) (corpusConfig, error) {
	var c corpusConfig
	f := flag.NewFlagSet("intro-corpus", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&c.mode, "mode", "", "extract or analyze")
	f.StringVar(&c.sources, "sources", "", "frozen public-source JSON inventory")
	f.StringVar(&c.labels, "labels", "", "pre-detection source labels")
	f.StringVar(&c.labelHash, "labels-sha256", "", "frozen label digest")
	f.StringVar(&c.series, "series", "", "one exact selected source series")
	f.StringVar(&c.features, "features", "", "absolute feature input/output directory")
	f.StringVar(&c.output, "output", "", "new result JSON for analyze, including blocked runs")
	f.StringVar(&c.options, "options", "", "optional complete calibration-only detector options JSON")
	f.StringVar(&c.ffmpeg, "ffmpeg", "/opt/ffmpeg/9.0.1/bin/ffmpeg", "production FFmpeg")
	f.StringVar(&c.ffprobe, "ffprobe", "/opt/ffmpeg/9.0.1/bin/ffprobe", "production FFprobe")
	f.StringVar(&c.helper, "fingerprint", "/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint", "production fingerprint helper")
	f.BoolVar(&c.resume, "resume", false, "reuse only verified checkpoint entries")
	f.BoolVar(&c.retryFailed, "retry-failed", false, "append new attempts for failed entries; requires resume")
	f.BoolVar(&c.visualExperiment, "visual-experiment", false, "include separate experimental visual observations; never publish markers")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return c, corpusFailure("invalid_arguments")
	}
	if (c.mode != "extract" && c.mode != "analyze") || c.series == "" || !filepath.IsAbs(c.features) ||
		c.mode == "analyze" && c.output == "" || c.retryFailed && !c.resume ||
		c.mode != "extract" && (c.resume || c.retryFailed) || c.mode != "analyze" && (c.options != "" || c.visualExperiment) {
		return c, corpusFailure("invalid_arguments")
	}
	return c, nil
}

func productionBackend(c corpusConfig) corpusBackend {
	extractor := media.AnalysisExtractor{FFmpegPath: c.ffmpeg, FFprobePath: c.ffprobe, FingerprintPath: c.helper}
	return corpusBackend{
		identity: func(ctx context.Context) (extractionIdentity, error) {
			available, err := extractor.Availability(ctx)
			if err != nil {
				return extractionIdentity{}, err
			}
			interval := media.TicksPerSecond / 2
			profile, err := media.IntroAlgorithmProfile(available, interval)
			if err != nil {
				return extractionIdentity{}, err
			}
			return extractionIdentity{Version: corpusSchema, AlgorithmProfile: profile,
				Tools:       media.AnalysisToolFacts{FFmpegSHA256: available.FFmpegSHA256, FFprobeSHA256: available.FFprobeSHA256, FingerprintSHA256: available.FingerprintSHA256},
				Fingerprint: available.Fingerprint, VisualIntervalTicks: interval, MaxWindowTicks: media.MaxIntroAnalysisTicks, Limits: media.DefaultAnalysisLimits()}, nil
		},
		probe:   (media.Prober{FFprobePath: c.ffprobe, FFmpegPath: c.ffmpeg}).ProbeFile,
		extract: extractor.ExtractIntro,
		analyze: introdetect.AnalyzeWithDiagnostics,
		visual:  introdetect.DiscoverVisualSequences,
	}
}

func loadCorpus(c corpusConfig) ([]sourceCase, string, error) {
	labelBytes, err := readBounded(c.labels, 8<<20)
	if err != nil {
		return nil, "", corpusFailure("labels_read_failed")
	}
	if !validDigest(c.labelHash) || digestBytes(labelBytes) != c.labelHash {
		return nil, "", corpusFailure("frozen_label_digest_mismatch")
	}
	var frozen map[string]any
	if json.Unmarshal(labelBytes, &frozen) != nil || frozen["detectorOutputsUsed"] != false {
		return nil, "", corpusFailure("independent_source_labels_required")
	}
	raw, err := readBounded(c.sources, 8<<20)
	if err != nil {
		return nil, "", corpusFailure("sources_read_failed")
	}
	var all []sourceCase
	if json.Unmarshal(raw, &all) != nil {
		return nil, "", corpusFailure("invalid_source_inventory")
	}
	var cases []sourceCase
	ids := make(map[string]bool)
	for _, item := range all {
		// IDs are filenames shared by series, so aliases that differ only by
		// case and duplicate IDs anywhere in the inventory are invalid.
		key := strings.ToLower(item.ID)
		if !safeCaseID.MatchString(item.ID) || ids[key] || !validDigest(item.SHA256) ||
			item.Series == "" || item.Episode == "" || item.Role == "" || !filepath.IsAbs(item.Path) {
			return nil, "", corpusFailure("invalid_source_inventory")
		}
		ids[key] = true
		if item.Series == c.series {
			cases = append(cases, item)
		}
	}
	if len(cases) < 3 || len(cases) > 32 {
		return nil, "", corpusFailure("bounded_complete_cohort_required")
	}
	return cases, digestBytes(raw), nil
}

func verifySource(ctx context.Context, item sourceCase) (*os.File, string, error) {
	file, err := openRegular(item.Path)
	if err != nil {
		return nil, "", err
	}
	failed := true
	defer func() {
		if failed {
			file.Close()
		}
	}()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 {
		return nil, "", corpusFailure("invalid_source_file")
	}
	identity, err := media.VideoSeekSourceIdentity(before)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, "", readErr
		}
	}
	after, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	afterIdentity, err := media.VideoSeekSourceIdentity(after)
	if err != nil || identity != afterIdentity || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return nil, "", corpusFailure("source_identity_mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	failed = false
	return file, identity, nil
}

func featureName(id string, attempt int) string {
	if attempt == 1 {
		return id + ".json"
	}
	return fmt.Sprintf("%s.attempt-%d.json", id, attempt)
}

func verifiedFeature(dir string, item sourceCase, attempt corpusAttempt, identity extractionIdentity, sourceIdentity string) (sourceFeatures, error) {
	var stored sourceFeatures
	if attempt.FeatureFile != featureName(item.ID, attempt.Number) || !validDigest(attempt.FeatureSHA256) || attempt.SourceIdentity != sourceIdentity {
		return stored, corpusFailure("feature_receipt_mismatch")
	}
	raw, err := readBounded(filepath.Join(dir, attempt.FeatureFile), 8<<20)
	if err != nil {
		return stored, corpusFailure("feature_read_failed")
	}
	if digestBytes(raw) != attempt.FeatureSHA256 {
		return stored, corpusFailure("feature_digest_mismatch")
	}
	if json.Unmarshal(raw, &stored) != nil || stored.Case != item {
		return stored, corpusFailure("feature_source_inventory_mismatch")
	}
	if stored.Features.AlgorithmProfile != identity.AlgorithmProfile || stored.Features.ToolFacts != identity.Tools ||
		stored.Features.SourceIdentity != sourceIdentity {
		return stored, corpusFailure("feature_extraction_identity_mismatch")
	}
	if len(stored.Features.Audio) == 0 || len(stored.Features.Visual) == 0 || stored.Info.DurationTicks <= 0 {
		return stored, corpusFailure("incomplete_feature_data")
	}
	return stored, nil
}

func lastAttempt(record corpusRecord) corpusAttempt {
	if len(record.Attempts) == 0 {
		return corpusAttempt{Status: "failed", Reason: "not_attempted"}
	}
	return record.Attempts[len(record.Attempts)-1]
}

func validateCheckpoint(checkpoint corpusCheckpoint, expected corpusCheckpoint) error {
	if checkpoint.Schema != expected.Schema || checkpoint.Series != expected.Series ||
		checkpoint.SourcesSHA256 != expected.SourcesSHA256 || checkpoint.LabelsSHA256 != expected.LabelsSHA256 ||
		checkpoint.IdentitySHA256 != expected.IdentitySHA256 || !reflect.DeepEqual(checkpoint.Identity, expected.Identity) ||
		len(checkpoint.Records) != len(expected.Records) {
		return corpusFailure("checkpoint_identity_mismatch")
	}
	for i, record := range checkpoint.Records {
		if record.Case != expected.Records[i].Case {
			return corpusFailure("checkpoint_inventory_mismatch")
		}
		for j, attempt := range record.Attempts {
			if attempt.Number != j+1 || attempt.StartedAt.IsZero() ||
				(attempt.Status != "extracted" && attempt.Status != "excluded" && attempt.Status != "failed") ||
				attempt.Reason != "" && !safeReasonCode.MatchString(attempt.Reason) ||
				attempt.Status != "extracted" && attempt.Reason == "" ||
				attempt.Status == "excluded" && attempt.Reason != "missing_audio" && attempt.Reason != "missing_video" {
				return corpusFailure("invalid_checkpoint_attempt")
			}
			if attempt.Status == "excluded" {
				if attempt.ProbeInfo == nil || !validDigest(attempt.SourceIdentity) {
					return corpusFailure("invalid_exclusion_receipt")
				}
				_, audio := selectedStream(*attempt.ProbeInfo, "audio")
				_, video := selectedStream(*attempt.ProbeInfo, "video")
				if attempt.Reason == "missing_audio" && audio || attempt.Reason == "missing_video" && video {
					return corpusFailure("invalid_exclusion_receipt")
				}
			}
		}
	}
	return nil
}

func safeReason(err error, fallback string) string {
	var coded corpusFailure
	if errors.As(err, &coded) {
		return string(coded)
	}
	if errors.Is(err, context.Canceled) {
		return "interrupted"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return fallback
}

func extractCase(ctx context.Context, c corpusConfig, backend corpusBackend, identity extractionIdentity, item sourceCase, attempt *corpusAttempt) error {
	bounded, stop := context.WithTimeout(ctx, 15*time.Minute)
	defer stop()
	file, sourceIdentity, err := verifySource(bounded, item)
	if err != nil {
		attempt.Reason = safeReason(err, "source_read_failed")
		return err
	}
	defer file.Close()
	attempt.SourceIdentity = sourceIdentity
	info, err := backend.probe(bounded, file)
	if err != nil {
		attempt.Reason = safeReason(err, "probe_failed")
		return err
	}
	audio, audioPresent := selectedStream(info, "audio")
	video, videoPresent := selectedStream(info, "video")
	if !audioPresent || !videoPresent {
		// An exclusion has no ExtractIntro ToolFacts receipt. Recheck the
		// tools and source after probing before authorizing that abstention.
		currentIdentity, err := backend.identity(bounded)
		if err != nil || !reflect.DeepEqual(currentIdentity, identity) {
			attempt.Reason = "probe_tool_identity_changed"
			if err == nil {
				err = corpusFailure(attempt.Reason)
			}
			return err
		}
		verified, finalIdentity, err := verifySource(bounded, item)
		if err == nil {
			err = verified.Close()
		}
		if err != nil || finalIdentity != sourceIdentity {
			attempt.Reason = "source_changed_during_probe"
			if err == nil {
				err = corpusFailure(attempt.Reason)
			}
			return err
		}
		attempt.Status, attempt.Reason = "excluded", "missing_audio"
		attempt.ProbeInfo = &info
		if audioPresent {
			attempt.Reason = "missing_video"
		}
		return nil
	}
	data, err := backend.extract(bounded, file, info, media.IntroAnalysisRequest{AudioStreamIndex: audio, VideoStreamIndex: video, VisualIntervalTicks: identity.VisualIntervalTicks})
	if err != nil {
		attempt.Reason = safeReason(err, "extraction_failed")
		return err
	}
	if data.AlgorithmProfile != identity.AlgorithmProfile || data.ToolFacts != identity.Tools || data.SourceIdentity != sourceIdentity ||
		len(data.Audio) == 0 || len(data.Visual) == 0 || info.DurationTicks <= 0 {
		attempt.Reason = "extraction_identity_or_data_mismatch"
		return corpusFailure(attempt.Reason)
	}
	// Rehash the original after extraction. Production extraction also checks
	// source metadata, but a corpus receipt binds the entire original bytes.
	verified, finalIdentity, err := verifySource(bounded, item)
	if err == nil {
		err = verified.Close()
	}
	if err != nil || finalIdentity != sourceIdentity {
		attempt.Reason = "source_changed_during_extraction"
		if err == nil {
			err = corpusFailure(attempt.Reason)
		}
		return err
	}
	raw, err := jsonBytes(sourceFeatures{item, info, data})
	if err == nil {
		attempt.FeatureFile = featureName(item.ID, attempt.Number)
		err = writeNew(filepath.Join(c.features, attempt.FeatureFile), raw)
	}
	if err != nil {
		attempt.Reason = "feature_write_failed"
		return err
	}
	attempt.FeatureSHA256, attempt.Status, attempt.Reason = digestBytes(raw), "extracted", ""
	return nil
}

func runExtraction(ctx context.Context, c corpusConfig, backend corpusBackend, checkpoint *corpusCheckpoint, statePath string, out io.Writer) error {
	failed := false
	for i := range checkpoint.Records {
		record := &checkpoint.Records[i]
		previous := lastAttempt(*record)
		if len(record.Attempts) > 0 && previous.Status == "failed" && !c.retryFailed {
			fmt.Fprintf(out, "case=%s status=failed reason=%s reused=true\n", record.Case.ID, previous.Reason)
			failed = true
			continue
		}
		var reuseErr error
		if len(record.Attempts) > 0 && previous.Status != "failed" {
			bounded, stop := context.WithTimeout(ctx, 15*time.Minute)
			file, sourceIdentity, err := verifySource(bounded, record.Case)
			stop()
			if err == nil {
				err = file.Close()
			}
			if err == nil && previous.SourceIdentity != sourceIdentity {
				err = corpusFailure("source_identity_mismatch")
			}
			if err == nil && previous.Status == "extracted" {
				_, err = verifiedFeature(c.features, record.Case, previous, checkpoint.Identity, sourceIdentity)
			}
			if err == nil {
				fmt.Fprintf(out, "case=%s status=%s reason=%s reused=true\n", record.Case.ID, previous.Status, previous.Reason)
				continue
			}
			reuseErr = err
		}
		// An incomplete durable attempt is a failure, never an extracted sample.
		// A retry gets a new feature filename and cannot overwrite old evidence.
		record.Attempts = append(record.Attempts, corpusAttempt{Number: len(record.Attempts) + 1, StartedAt: time.Now().UTC(), Status: "failed", Reason: "interrupted"})
		if err := saveCheckpoint(statePath, *checkpoint); err != nil {
			return corpusFailure("checkpoint_write_failed")
		}
		attempt := &record.Attempts[len(record.Attempts)-1]
		if reuseErr != nil {
			attempt.Reason = safeReason(reuseErr, "source_read_failed")
			attempt.RawError = reuseErr.Error()
		} else if err := extractCase(ctx, c, backend, checkpoint.Identity, record.Case, attempt); err != nil {
			attempt.RawError = err.Error()
		}
		attempt.CompletedAt = time.Now().UTC()
		if err := saveCheckpoint(statePath, *checkpoint); err != nil {
			return corpusFailure("checkpoint_write_failed")
		}
		fmt.Fprintf(out, "case=%s status=%s reason=%s\n", record.Case.ID, attempt.Status, attempt.Reason)
		failed = failed || attempt.Status == "failed"
		if ctx.Err() != nil {
			return corpusFailure("interrupted")
		}
	}
	if failed {
		return corpusFailure("extraction_has_failed_cases")
	}
	return nil
}

func analyzeCorpus(ctx context.Context, c corpusConfig, backend corpusBackend, checkpoint corpusCheckpoint, options introdetect.Options, out io.Writer) error {
	report := corpusReport{Schema: corpusSchema, LabelsSHA256: checkpoint.LabelsSHA256, SourcesSHA256: checkpoint.SourcesSHA256, IdentitySHA256: checkpoint.IdentitySHA256, Options: options}
	cohort := introdetect.Cohort{Key: c.series}
	episodes, contents := make(map[string]bool), make(map[string]bool)
	for _, record := range checkpoint.Records {
		attempt := lastAttempt(record)
		account := corpusCaseResult{ID: record.Case.ID, Status: attempt.Status, Reason: attempt.Reason}
		if attempt.Status == "failed" {
			report.Reason = "incomplete_corpus"
			report.Cases = append(report.Cases, account)
			continue
		}
		bounded, stop := context.WithTimeout(ctx, 15*time.Minute)
		file, sourceIdentity, err := verifySource(bounded, record.Case)
		stop()
		if err == nil {
			err = file.Close()
		}
		if err == nil && attempt.SourceIdentity != sourceIdentity {
			err = corpusFailure("source_identity_mismatch")
		}
		var stored sourceFeatures
		if err == nil && attempt.Status == "extracted" {
			stored, err = verifiedFeature(c.features, record.Case, attempt, checkpoint.Identity, sourceIdentity)
		}
		if err != nil {
			account.Status, account.Reason = "failed", safeReason(err, "source_read_failed")
			report.Reason = "invalid_corpus"
		} else if attempt.Status == "extracted" {
			key := record.Case.Episode
			if episodes[key] || contents[record.Case.SHA256] {
				account.Status, account.Reason = "failed", "duplicate_independent_identity"
				report.Reason = "invalid_corpus"
			} else {
				episodes[key], contents[record.Case.SHA256] = true, true
				cohort.Episodes = append(cohort.Episodes, introdetect.Episode{EpisodeKey: record.Case.Series + ":" + record.Case.Episode,
					SourceKey: record.Case.SHA256, ContentIdentity: record.Case.SHA256, AlgorithmProfile: stored.Features.AlgorithmProfile,
					DurationTicks: stored.Info.DurationTicks, AudioBoundaryUncertaintyTicks: stored.Features.AudioBoundaryUncertaintyTicks,
					Audio: stored.Features.Audio, Visual: stored.Features.Visual, Refinement: stored.Features.Refinement})
			}
		}
		report.Cases = append(report.Cases, account)
	}
	if len(cohort.Episodes) < max(3, options.MinSupport) && report.Reason == "" {
		report.Reason = "insufficient_independent_episodes"
	}
	if report.Reason == "" {
		bounded, stop := context.WithTimeout(ctx, 5*time.Minute)
		result, diagnostics, err := backend.analyze(bounded, cohort, options)
		stop()
		report.MatcherRan = true
		report.Diagnostics = diagnostics
		if err != nil {
			report.Reason = safeReason(err, "analysis_failed")
		} else {
			report.Result = result
		}
	}
	if report.Reason == "" && c.visualExperiment {
		bounded, stop := context.WithTimeout(ctx, 5*time.Minute)
		experiment, err := backend.visual(bounded, cohort, options)
		stop()
		if err != nil {
			report.Reason = safeReason(err, "visual_experiment_failed")
		} else {
			report.VisualExperiment = &experiment
		}
	}
	raw, err := jsonBytes(report)
	if err != nil || writeNew(c.output, raw) != nil {
		return corpusFailure("result_write_failed")
	}
	for _, account := range report.Cases {
		fmt.Fprintf(out, "case=%s status=%s reason=%s\n", account.ID, account.Status, account.Reason)
	}
	if report.Reason != "" {
		return corpusFailure(report.Reason)
	}
	fmt.Fprintf(out, "analyzed episodes=%d groups=%d comparisons=%d\n", len(report.Result.Episodes), len(report.Result.Groups), report.Result.Comparisons)
	if report.VisualExperiment != nil {
		fmt.Fprintf(out, "visual_experiment groups=%d comparisons=%d publishable=false\n", len(report.VisualExperiment.Groups), report.VisualExperiment.Comparisons)
	}
	return nil
}

func runCorpus(ctx context.Context, c corpusConfig, backend corpusBackend, out io.Writer) (runErr error) {
	cases, sourcesHash, err := loadCorpus(c)
	if err != nil {
		return err
	}
	options := introdetect.DefaultOptions()
	analysisStarted := false
	identityHash := ""
	defer func() {
		// Once the inventory and labels are admitted, failures before matcher
		// admission still receive complete accounting in the requested report.
		if runErr == nil || c.mode != "analyze" || analysisStarted {
			return
		}
		reason := safeReason(runErr, "corpus_failed")
		report := corpusReport{Schema: corpusSchema, LabelsSHA256: c.labelHash, SourcesSHA256: sourcesHash, IdentitySHA256: identityHash, Reason: reason, Options: options}
		for _, item := range cases {
			report.Cases = append(report.Cases, corpusCaseResult{ID: item.ID, Status: "failed", Reason: reason})
		}
		raw, err := jsonBytes(report)
		if err != nil || writeNew(c.output, raw) != nil {
			runErr = corpusFailure("result_write_failed")
		}
	}()
	if c.options != "" {
		for _, item := range cases {
			if item.Role != "calibration" {
				return corpusFailure("options_require_calibration_only_sources")
			}
		}
		raw, err := readBounded(c.options, 1<<20)
		if err != nil {
			return corpusFailure("options_read_failed")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		// Decode into zero Options so an incomplete experiment is not silently
		// filled with defaults. The matcher validates the complete option set.
		options = introdetect.Options{}
		if decoder.Decode(&options) != nil || decoder.Decode(new(any)) != io.EOF || options == (introdetect.Options{}) {
			return corpusFailure("invalid_detector_options")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return corpusFailure("invalid_detector_options")
		}
		optionType := reflect.TypeOf(options)
		if len(fields) != optionType.NumField() {
			return corpusFailure("complete_detector_options_required")
		}
		for i := 0; i < optionType.NumField(); i++ {
			if _, ok := fields[optionType.Field(i).Name]; !ok {
				return corpusFailure("complete_detector_options_required")
			}
		}
	}
	identity, err := backend.identity(ctx)
	if err != nil {
		return corpusFailure("extraction_tools_unavailable")
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil || identity.Version != corpusSchema || identity.AlgorithmProfile == "" || !validDigest(identity.Tools.FFmpegSHA256) ||
		!validDigest(identity.Tools.FFprobeSHA256) || !validDigest(identity.Tools.FingerprintSHA256) {
		return corpusFailure("invalid_extraction_identity")
	}
	expected := corpusCheckpoint{Schema: corpusSchema, Series: c.series, SourcesSHA256: sourcesHash, LabelsSHA256: c.labelHash,
		Identity: identity, IdentitySHA256: digestBytes(identityBytes)}
	identityHash = expected.IdentitySHA256
	for _, item := range cases {
		expected.Records = append(expected.Records, corpusRecord{Case: item, Attempts: []corpusAttempt{}})
	}
	if c.mode == "extract" {
		if err := os.MkdirAll(c.features, 0700); err != nil {
			return corpusFailure("feature_directory_unavailable")
		}
	}
	// Linux advisory locking is released by the kernel on interruption. The
	// retained lock file is not a stale-process marker and is safe to reuse.
	lock, err := os.OpenFile(filepath.Join(c.features, ".intro-corpus.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return corpusFailure("checkpoint_lock_unavailable")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return corpusFailure("corpus_in_use")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	statePath := filepath.Join(c.features, ".intro-corpus-"+digestBytes([]byte(c.series))+".json")
	checkpoint := expected
	raw, readErr := readBounded(statePath, 16<<20)
	if c.mode == "extract" && !c.resume {
		if !errors.Is(readErr, os.ErrNotExist) {
			return corpusFailure("checkpoint_exists_or_unreadable_use_resume")
		}
		// Old unreceipted features are never promoted into trusted cache data.
		for _, item := range cases {
			if _, err := os.Lstat(filepath.Join(c.features, featureName(item.ID, 1))); !errors.Is(err, os.ErrNotExist) {
				return corpusFailure("unreceipted_features_require_new_directory")
			}
		}
		if err := saveCheckpoint(statePath, checkpoint); err != nil {
			return corpusFailure("checkpoint_write_failed")
		}
	} else {
		if readErr != nil || json.Unmarshal(raw, &checkpoint) != nil {
			return corpusFailure("checkpoint_missing_or_invalid")
		}
		if err := validateCheckpoint(checkpoint, expected); err != nil {
			return err
		}
	}
	if c.mode == "extract" {
		return runExtraction(ctx, c, backend, &checkpoint, statePath, out)
	}
	analysisStarted = true
	return analyzeCorpus(ctx, c, backend, checkpoint, options, out)
}

func main() {
	c, err := parseCorpusConfig(os.Args[1:])
	if err == nil {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = runCorpus(ctx, c, productionBackend(c), os.Stdout)
		cancel()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "intro-corpus reason=%s\n", safeReason(err, "corpus_failed"))
		os.Exit(1)
	}
}
