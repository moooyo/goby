package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"github.com/moooyo/goby/internal/creditsskipper"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func (r *mediaAnalysisRuntime) initializeCreditsProfile(ctx context.Context) error {
	visual, err := r.extractor.CreditsVisualAvailability(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.creditsVisual = visual
	if err != nil || !visual.Available || !r.availability.IntroSkipperAvailable {
		return nil
	}
	audio, err := media.CreditsSkipperAlgorithmProfile(r.availability)
	if err != nil {
		return nil
	}
	r.creditsAudioProfile = audio
	digest := sha256.Sum256([]byte(audio + "\x00" + visual.Profile))
	profile := library.AnalysisExecutionProfile{
		Version: library.AnalysisExecutionProfileVersion, Available: true,
		FFmpegSHA256: visual.FFmpegSHA256, FFprobeSHA256: r.availability.FFprobeSHA256,
		FingerprintSHA256: r.availability.IntroFFmpegSHA256, DetectorVersion: introskipper.CreditsVersion,
		IntroSkipperOptions: introskipper.DefaultOptions(), IntroProfile: "credits-pipeline-v1;sha256=" + hex.EncodeToString(digest[:]),
	}
	if library.ValidateAnalysisExecutionForTask(library.TaskCreditsAnalysisKey, profile) != nil {
		return nil
	}
	r.profiles[library.TaskCreditsAnalysisKey] = profile
	return nil
}

func (r *mediaAnalysisRuntime) publishCreditsAbstention(ctx context.Context, task tasks.Work, work library.AnalysisWork, reason string) error {
	values := make(map[string]library.AnalysisStoredCreditsResult)
	for _, source := range work.Sources {
		if source.Target {
			values[source.ItemID] = library.AnalysisStoredCreditsResult{Version: library.AnalysisCreditsResultVersion,
				ItemID: source.ItemID, SourceRevision: source.SourceRevision, DurationTicks: source.DurationTicks,
				Segments: []library.CreditsInterval{}, Reason: reason}
		}
	}
	return r.server.library.PublishCreditsAnalysis(ctx, task.ChildID, task.Fence, values, nil)
}

// Audio matching uses an independent season cohort. Chapter and visual passes
// run per publication target, including movies and isolated episodes. Raw tail
// fingerprints never enter the prefix-only introduction feature cache.
func (r *mediaAnalysisRuntime) executeCreditsAnalysis(ctx context.Context, task tasks.Work, work library.AnalysisWork, progress func(tasks.Progress) error) error {
	ctx, cancel := context.WithTimeout(ctx, analysisIntroCohortTimeout)
	defer cancel()
	task = task.WithContext(ctx)
	if len(work.Sources) == 0 || len(work.Sources) > introskipper.MaxEpisodes {
		return library.ErrInvalidInput
	}
	audioEligible := len(work.Sources) >= 2
	for _, source := range work.Sources {
		if source.ItemType != "Episode" || source.EpisodeKey == "" {
			audioEligible = false
		}
	}
	hashes := make(map[string]string)
	audioResults := make(map[string]introskipper.EpisodeResult)
	if audioEligible {
		cohort := introskipper.Cohort{Key: work.ScopeKey, Episodes: make([]introskipper.Episode, 0, len(work.Sources))}
		for index, source := range work.Sources {
			hash, fingerprint, err := r.creditsSourceFingerprint(ctx, task, work, source)
			if err != nil {
				return err
			}
			hashes[source.ItemID] = hash
			cohort.Episodes = append(cohort.Episodes, introskipper.Episode{EpisodeKey: source.EpisodeKey,
				SourceKey: source.SourceRevision, ContentIdentity: hash, AlgorithmProfile: work.Execution.IntroProfile,
				DurationTicks: source.DurationTicks, Fingerprint: fingerprint})
			if err := progress(tasks.Progress{Processed: int64(index + 1)}); err != nil {
				return err
			}
		}
		matching, release := context.WithTimeout(ctx, 2*time.Minute)
		result, err := introskipper.AnalyzeCredits(matching, cohort, work.Execution.IntroSkipperOptions)
		release()
		if err != nil {
			return err
		}
		if result.Version != introskipper.CreditsVersion {
			return media.ErrAnalysisUnproven
		}
		for _, value := range result.Episodes {
			if value.Status == introskipper.Qualified {
				audioResults[value.SourceKey] = value
			}
		}
	}
	values := make(map[string]library.AnalysisStoredCreditsResult)
	var processed int64
	for _, source := range work.Sources {
		if !source.Target {
			continue
		}
		var audio *introskipper.EpisodeResult
		if value, exists := audioResults[source.SourceRevision]; exists {
			value := value
			audio = &value
		}
		result, err := r.creditsSourceVisual(ctx, task, work, source, audio)
		if err != nil {
			return err
		}
		values[source.ItemID] = result
		processed++
		if !audioEligible {
			if err := progress(tasks.Progress{Processed: processed}); err != nil {
				return err
			}
		}
	}
	if err := r.server.library.PublishCreditsAnalysis(ctx, task.ChildID, task.Fence, values, hashes); err != nil {
		return err
	}
	return progress(tasks.Progress{Processed: int64(len(work.Sources)), Updated: int64(len(values))})
}

func (r *mediaAnalysisRuntime) creditsSourceFingerprint(ctx context.Context, task tasks.Work, work library.AnalysisWork, source library.AnalysisSource) (hash string, fingerprint []uint32, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(work.Profile.MaxItemRuntimeSeconds)*time.Second)
	defer cancel()
	task = task.WithContext(ctx)
	file, opened, err := r.server.library.OpenAnalysisSource(ctx, task.ChildID, source.ItemID, task.Fence)
	if err != nil {
		return "", nil, err
	}
	read, err := r.server.library.PrepareMediaSourceIO(ctx, opened)
	defer func() { resultErr = errors.Join(resultErr, closeAnalysisSourceRead(file, read)) }()
	if err != nil {
		return "", nil, err
	}
	ctx = read.Context(ctx)
	if opened.Item.Media == nil || opened.Size != source.Size || opened.Size > work.Profile.MaxSourceBytes || opened.Item.Media.DurationTicks != source.DurationTicks {
		return "", nil, media.ErrAnalysisUnproven
	}
	hash, err = analysisSourceDigest(ctx, file, source.Size, work.Profile.MaxSourceBytes)
	if err != nil {
		return "", nil, err
	}
	fingerprint = []uint32{}
	index, found := media.SelectIntroSkipperAudioStream(*opened.Item.Media, "", true)
	if !found {
		return hash, fingerprint, nil
	}
	extractor := r.extractor
	extractor.Limits.Timeout = time.Duration(work.Profile.MaxItemRuntimeSeconds) * time.Second
	value, err := extractor.ExtractCreditsSkipper(ctx, file, *opened.Item.Media, media.IntroSkipperAnalysisRequest{AudioStreamIndex: index, Options: work.Execution.IntroSkipperOptions})
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, media.ErrIntroSkipperFingerprintUnavailable) {
			return hash, fingerprint, nil
		}
		return "", nil, err
	}
	if value.AlgorithmProfile != r.creditsAudioProfile || value.FFmpegSHA256 != work.Execution.FingerprintSHA256 ||
		value.FingerprintStartSeconds != introskipper.CreditsFingerprintStartSeconds(source.DurationTicks) ||
		value.FingerprintEndSeconds != float64(source.DurationTicks)/float64(media.TicksPerSecond) {
		return "", nil, media.ErrAnalysisUnproven
	}
	return hash, value.RawFingerprint, nil
}

func (r *mediaAnalysisRuntime) creditsSourceVisual(ctx context.Context, task tasks.Work, work library.AnalysisWork, source library.AnalysisSource, audio *introskipper.EpisodeResult) (result library.AnalysisStoredCreditsResult, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(work.Profile.MaxItemRuntimeSeconds)*time.Second)
	defer cancel()
	task = task.WithContext(ctx)
	file, opened, err := r.server.library.OpenAnalysisSource(ctx, task.ChildID, source.ItemID, task.Fence)
	if err != nil {
		return result, err
	}
	read, err := r.server.library.PrepareMediaSourceIO(ctx, opened)
	defer func() { resultErr = errors.Join(resultErr, closeAnalysisSourceRead(file, read)) }()
	if err != nil {
		return result, err
	}
	ctx = read.Context(ctx)
	if opened.Item.Media == nil || opened.Size != source.Size || opened.Size > work.Profile.MaxSourceBytes || opened.Item.Media.DurationTicks != source.DurationTicks {
		return result, media.ErrAnalysisUnproven
	}
	video := -1
	for _, stream := range opened.Item.Media.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture && !stream.IsExternal {
			video = stream.Index
			break
		}
	}
	if video < 0 {
		return result, media.ErrAnalysisUnproven
	}
	audioIndex, found := media.SelectIntroSkipperAudioStream(*opened.Item.Media, "", true)
	if !found {
		audioIndex = -1
	}
	request := media.CreditsVisualRequest{VideoStreamIndex: video, AudioStreamIndex: audioIndex, IsMovie: source.ItemType == "Movie"}
	if audio != nil && audio.Candidate != nil {
		request.AudioSegments = []creditsskipper.Segment{{Start: float64(audio.Candidate.Interval.StartTicks) / float64(media.TicksPerSecond),
			End: float64(audio.Candidate.Interval.EndTicks) / float64(media.TicksPerSecond), Source: creditsskipper.ChromaprintSource}}
	}
	extractor := r.extractor
	extractor.Limits.Timeout = time.Duration(work.Profile.MaxItemRuntimeSeconds) * time.Second
	evidence, err := extractor.ExtractCreditsVisual(ctx, file, *opened.Item.Media, request)
	if err != nil {
		return result, err
	}
	if evidence.AlgorithmProfile != r.creditsVisual.Profile || evidence.FFmpegSHA256 != work.Execution.FFmpegSHA256 {
		return result, media.ErrAnalysisUnproven
	}
	result = library.AnalysisStoredCreditsResult{Version: library.AnalysisCreditsResultVersion, ItemID: source.ItemID, SourceRevision: source.SourceRevision,
		DurationTicks: source.DurationTicks, Segments: []library.CreditsInterval{}, Audio: audio, Visual: &evidence.Result}
	for _, segment := range evidence.Result.Segments {
		start, end := math.RoundToEven(segment.Start*float64(media.TicksPerSecond)), math.RoundToEven(segment.End*float64(media.TicksPerSecond))
		if math.IsNaN(start) || math.IsNaN(end) || start < 0 || start >= end || end > float64(source.DurationTicks) {
			return library.AnalysisStoredCreditsResult{}, media.ErrAnalysisUnproven
		}
		result.Segments = append(result.Segments, library.CreditsInterval{StartTicks: int64(start), EndTicks: int64(end), Source: string(segment.Source)})
	}
	if len(result.Segments) == 0 {
		result.Reason = "no_credits_detected"
	}
	return result, nil
}
