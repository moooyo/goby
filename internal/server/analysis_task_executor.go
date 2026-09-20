package server

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

const analysisStreamSelectionProfile = ";selection=default-index-v1"
const analysisIntroCohortTimeout = 2 * time.Hour

type mediaAnalysisTaskExecutor struct {
	runtime *mediaAnalysisRuntime
	key     string
}

func (executor mediaAnalysisTaskExecutor) Available() bool {
	return executor.runtime.Available(executor.key)
}

func (executor mediaAnalysisTaskExecutor) admission(tx library.OwnedTx, request tasks.AnalysisAdmissionRequest) (tasks.AnalysisAdmissionBinding, error) {
	if request.TaskKey != executor.key {
		return tasks.AnalysisAdmissionBinding{}, tasks.ErrInvalidInput
	}
	profile, err := executor.runtime.executionProfile(executor.key)
	if err != nil {
		return tasks.AnalysisAdmissionBinding{}, err
	}
	// The task store has already checked the actor and exact target membership.
	// This library callback snapshots only data while the SQL owner is held.
	binding, err := library.PrepareAnalysis(tx, executor.key, request.Selection, profile)
	if err != nil {
		return tasks.AnalysisAdmissionBinding{}, err
	}
	return tasks.AnalysisAdmissionBinding{ConfigurationFingerprint: binding.ConfigurationFingerprint,
		Bind: binding.Bind, SnapshotChildren: binding.SnapshotChildren}, nil
}

func (executor mediaAnalysisTaskExecutor) Execute(ctx context.Context, task tasks.Work, progress func(tasks.Progress) error) (resultErr error) {
	r := executor.runtime
	defer func() { r.rememberFailure(executor.key, resultErr) }()
	if task.TaskKey != executor.key || !executor.Available() || progress == nil {
		return tasks.ErrUnavailable
	}
	ctx, leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	task = task.WithContext(ctx)
	work, err := r.server.library.GetAnalysisWork(ctx, task.ChildID, task.Fence)
	if err != nil {
		return err
	}
	execution, err := r.executionProfile(executor.key)
	if err != nil || !reflect.DeepEqual(execution, work.Execution) || work.TaskKey != task.TaskKey ||
		work.RunID != task.RunID || work.ChildID != task.ChildID || work.LibraryID != task.LibraryID ||
		work.ScopeKey != task.AnalysisScopeKey || work.ConfigurationFingerprint != task.AnalysisConfigFingerprint {
		return tasks.ErrUnavailable
	}
	if err := library.ValidateAnalysisProfile(work.Profile); err != nil {
		return err
	}
	if work.Reason != "" {
		return r.server.library.PublishAnalysisAbstention(ctx, work.ChildID, task.Fence, work.Reason)
	}
	switch executor.key {
	case library.TaskIntroAnalysisKey:
		return r.executeIntroAnalysis(ctx, task, work, progress)
	case library.TaskPreviewGenerationKey:
		return r.executePreviewAnalysis(ctx, task, work, progress)
	default:
		return tasks.ErrUnavailable
	}
}

func (r *mediaAnalysisRuntime) executeIntroAnalysis(ctx context.Context, task tasks.Work, work library.AnalysisWork, progress func(tasks.Progress) error) error {
	// Per-source budgets do not add up to an unbounded monopoly of the shared
	// analysis slot. This deadline also fences matching and final publication.
	ctx, cancelCohort := context.WithTimeout(ctx, analysisIntroCohortTimeout)
	defer cancelCohort()
	task = task.WithContext(ctx)
	if len(work.Sources) == 0 || len(work.Sources) > work.Execution.DetectorOptions.MaxEpisodes {
		return library.ErrInvalidInput
	}
	cohort := introdetect.Cohort{Key: work.ScopeKey, Episodes: make([]introdetect.Episode, 0, len(work.Sources))}
	abstentions := make(map[string]introdetect.Reason)
	for index, source := range work.Sources {
		features, reason, err := r.introSourceFeatures(ctx, task, work, source)
		if err != nil {
			return err
		}
		cohort.Episodes = append(cohort.Episodes, introdetect.Episode{
			EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: features.ContentSHA256,
			AlgorithmProfile: features.AlgorithmProfile, DurationTicks: source.DurationTicks,
			AudioBoundaryUncertaintyTicks: features.AudioBoundaryUncertaintyTicks, Audio: features.Audio, Visual: features.Visual})
		if reason != "" {
			abstentions[source.SourceRevision] = reason
		}
		if err := progress(tasks.Progress{Processed: int64(index + 1)}); err != nil {
			return err
		}
	}
	matching, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	publicationTask := task.WithContext(matching)
	result, err := introdetect.Analyze(matching, cohort, work.Execution.DetectorOptions)
	if err != nil {
		if errors.Is(err, introdetect.ErrLimit) {
			withdrawErr := r.server.library.PublishAnalysisAbstention(matching, task.ChildID, publicationTask.Fence, "comparison_budget_exceeded")
			return errors.Join(err, withdrawErr)
		}
		return err
	}
	for index := range result.Episodes {
		episode := &result.Episodes[index]
		if reason := abstentions[episode.SourceKey]; reason != "" && episode.Status == introdetect.NoResult && len(episode.Candidates) == 0 {
			found := false
			for _, existing := range episode.Reasons {
				found = found || existing == reason
			}
			if !found {
				episode.Reasons = append(episode.Reasons, reason)
			}
		}
	}
	if err := r.server.library.PublishIntroAnalysis(matching, task.ChildID, publicationTask.Fence, result); err != nil {
		return err
	}
	var targets int64
	for _, source := range work.Sources {
		if source.Target {
			targets++
		}
	}
	return progress(tasks.Progress{Processed: int64(len(work.Sources)), Updated: targets})
}

func (r *mediaAnalysisRuntime) introSourceFeatures(ctx context.Context, task tasks.Work, work library.AnalysisWork, source library.AnalysisSource) (library.AnalysisFeatures, introdetect.Reason, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(work.Profile.MaxItemRuntimeSeconds)*time.Second)
	defer cancel()
	task = task.WithContext(ctx)
	file, opened, err := r.server.library.OpenAnalysisSource(ctx, task.ChildID, source.ItemID, task.Fence)
	if err != nil {
		return library.AnalysisFeatures{}, "", err
	}
	defer file.Close()
	if opened.Item.Media == nil || opened.Size != source.Size || opened.Size > work.Profile.MaxSourceBytes ||
		opened.Item.Media.DurationTicks != source.DurationTicks {
		return library.AnalysisFeatures{}, "", media.ErrAnalysisUnproven
	}
	value, cached, err := r.server.library.GetAnalysisFeatures(ctx, task.ChildID, source.ItemID, task.Fence)
	if err != nil {
		return library.AnalysisFeatures{}, "", err
	}
	if cached {
		if value.AlgorithmProfile != work.Execution.IntroProfile {
			return library.AnalysisFeatures{}, "", library.ErrAnalysisSourceChanged
		}
		return value, "", nil
	}
	digest, err := analysisSourceDigest(ctx, file, source.Size, work.Profile.MaxSourceBytes)
	if err != nil {
		return library.AnalysisFeatures{}, "", err
	}
	value = library.AnalysisFeatures{ContentSHA256: digest, AlgorithmProfile: work.Execution.IntroProfile,
		Audio: []introdetect.AudioSample{}, Visual: []introdetect.VisualSample{}}
	info := *opened.Item.Media
	audio, hasAudio := analysisStream(info, "audio")
	video, hasVideo := analysisStream(info, "video")
	if !hasAudio {
		return value, introdetect.MissingAudio, nil
	}
	if !hasVideo {
		return value, introdetect.MissingVisual, nil
	}
	extractor := r.extractor
	extractor.Limits.Timeout = time.Duration(work.Profile.MaxItemRuntimeSeconds) * time.Second
	features, err := extractor.ExtractIntro(ctx, file, info, media.IntroAnalysisRequest{
		AudioStreamIndex: audio, VideoStreamIndex: video, VisualIntervalTicks: work.Execution.VisualIntervalTicks})
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, media.ErrAnalysisUnproven) {
			return value, introdetect.Reason("source_timeline_unproven"), nil
		}
		return library.AnalysisFeatures{}, "", err
	}
	if features.AlgorithmProfile+analysisStreamSelectionProfile != work.Execution.IntroProfile ||
		features.ToolFacts.FFmpegSHA256 != work.Execution.FFmpegSHA256 ||
		features.ToolFacts.FFprobeSHA256 != work.Execution.FFprobeSHA256 ||
		features.ToolFacts.FingerprintSHA256 != work.Execution.FingerprintSHA256 {
		return library.AnalysisFeatures{}, "", media.ErrAnalysisUnavailable
	}
	value.Audio, value.Visual = features.Audio, features.Visual
	value.AudioBoundaryUncertaintyTicks = features.AudioBoundaryUncertaintyTicks
	if err := r.server.library.PutAnalysisFeatures(ctx, task.ChildID, source.ItemID, task.Fence, value); err != nil {
		return library.AnalysisFeatures{}, "", err
	}
	return value, "", nil
}

func (r *mediaAnalysisRuntime) executePreviewAnalysis(ctx context.Context, task tasks.Work, work library.AnalysisWork, progress func(tasks.Progress) error) error {
	if len(work.Sources) != 1 || !work.Sources[0].Target {
		return library.ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(work.Profile.MaxItemRuntimeSeconds)*time.Second)
	defer cancel()
	task = task.WithContext(ctx)
	source := work.Sources[0]
	file, opened, err := r.server.library.OpenAnalysisSource(ctx, task.ChildID, source.ItemID, task.Fence)
	if err != nil {
		return err
	}
	defer file.Close()
	if opened.Item.Media == nil || opened.Size != source.Size || opened.Size > work.Profile.MaxSourceBytes ||
		opened.Item.Media.DurationTicks != source.DurationTicks {
		return media.ErrAnalysisUnproven
	}
	if reused, err := r.reusePreview(ctx, task, work); err != nil {
		return err
	} else if reused {
		return progress(tasks.Progress{Processed: 1})
	}
	publication, previews, err := r.buildPreview(ctx, work, source, file, *opened.Item.Media)
	if err != nil {
		return err
	}
	if publication == nil {
		return library.ErrUnavailable
	}
	unlock, err := r.lockPublication(ctx)
	if err != nil {
		// No database publication was attempted, so this generation is known
		// to be unreferenced. Keep ownership until actual cleanup returns.
		discardErr := publication.Discard(context.Background())
		if discardErr != nil {
			discardErr = errors.Join(discardErr, publication.Keep())
		}
		return errors.Join(err, discardErr)
	}
	defer unlock()
	err = r.server.library.PublishAnalysisPreview(ctx, task.ChildID, task.Fence, previews)
	// A commit error can be ambiguous. Keep the immutable generation even on
	// error; only a later current-reference snapshot may reclaim the orphan.
	keepErr := publication.Keep()
	if err != nil || keepErr != nil {
		return errors.Join(err, keepErr)
	}
	return progress(tasks.Progress{Processed: 1, Updated: 1})
}
