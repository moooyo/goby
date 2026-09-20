package server

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// The task manager owns scheduling. This runtime owns media tools, derivative
// storage and all operations until their actual processes and I/O have returned.
// Publication and reference pruning share one cancellable serialization gate.
type mediaAnalysisRuntime struct {
	server           *Server
	configuration    config.MediaAnalysisConfig
	extractor        media.AnalysisExtractor
	availability     media.AnalysisAvailability
	profiles         map[string]library.AnalysisExecutionProfile
	cache            *analysiscache.Store
	ctx              context.Context
	cancel           context.CancelFunc
	publish          chan struct{}
	previewSlots     chan struct{}
	mu               sync.Mutex
	closing          bool
	failures         map[string]string
	activeOperations map[*mediaAnalysisOperation]struct{}
	operations       sync.WaitGroup
	closeOnce        sync.Once
	done             chan struct{}
	closeErr         error
}

type mediaAnalysisOperation struct {
	cancel context.CancelFunc
}

func newMediaAnalysisRuntime(ctx context.Context, server *Server) (*mediaAnalysisRuntime, error) {
	if ctx == nil || server == nil || server.library == nil {
		return nil, library.ErrUnavailable
	}
	if err := server.cfg.MediaAnalysis.Validate(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	r := &mediaAnalysisRuntime{server: server, configuration: server.cfg.MediaAnalysis,
		ctx: lifetime, cancel: cancel, publish: make(chan struct{}, 1), previewSlots: make(chan struct{}, 4), done: make(chan struct{}),
		profiles: make(map[string]library.AnalysisExecutionProfile, 2)}
	r.setUnavailableProfiles("not_configured")
	if !r.configuration.Enabled {
		return r, nil
	}
	if runtime.GOOS != "linux" {
		r.setUnavailableProfiles("unsupported_platform")
		return r, nil
	}
	cache, err := analysiscache.Open(ctx, r.configuration.CacheOptions())
	if err != nil {
		cancel()
		return nil, err
	}
	r.cache = cache
	r.extractor = media.AnalysisExtractor{FFmpegPath: server.cfg.FFmpegPath, FFprobePath: server.cfg.FFprobePath,
		FingerprintPath: r.configuration.FingerprintPath, ExpectedFingerprintSHA256: r.configuration.FingerprintSHA256}
	availability, err := r.extractor.Availability(ctx)
	if err != nil {
		cancel()
		return nil, errors.Join(err, cache.Close(context.Background()))
	}
	r.availability = availability
	r.extractor.FFmpegPath, r.extractor.ExpectedFFmpegSHA256 = availability.FFmpegPath, availability.FFmpegSHA256
	r.extractor.FFprobePath, r.extractor.ExpectedFFprobeSHA256 = availability.FFprobePath, availability.FFprobeSHA256
	r.extractor.FingerprintPath, r.extractor.ExpectedFingerprintSHA256 = availability.FingerprintPath, availability.FingerprintSHA256
	r.setUnavailableProfiles("dependencies_unavailable")
	if availability.PreviewAvailable {
		r.profiles[library.TaskPreviewGenerationKey] = library.AnalysisExecutionProfile{
			Version: library.AnalysisProfileVersion, Available: true,
			FFmpegSHA256: availability.FFmpegSHA256, FFprobeSHA256: availability.FFprobeSHA256,
			PreviewProfile: media.PreviewAnalysisProfile, PreviewWidths: []int{240, 320, 400}}
	}
	if availability.AudioAvailable && availability.VisualAvailable {
		profile, err := media.IntroAlgorithmProfile(availability, media.TicksPerSecond/2)
		if err != nil {
			cancel()
			return nil, errors.Join(err, cache.Close(context.Background()))
		}
		r.profiles[library.TaskIntroAnalysisKey] = library.AnalysisExecutionProfile{
			Version: library.AnalysisProfileVersion, Available: true,
			FFmpegSHA256: availability.FFmpegSHA256, FFprobeSHA256: availability.FFprobeSHA256,
			FingerprintSHA256: availability.FingerprintSHA256, DetectorVersion: introdetect.Version,
			DetectorOptions: introdetect.DefaultOptions(), VisualIntervalTicks: media.TicksPerSecond / 2,
			IntroProfile: profile + analysisStreamSelectionProfile}
	}
	return r, nil
}

func (r *mediaAnalysisRuntime) setUnavailableProfiles(reason string) {
	for _, key := range []string{library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey} {
		r.profiles[key] = library.AnalysisExecutionProfile{Version: library.AnalysisProfileVersion, UnavailableReason: reason}
	}
}

func (r *mediaAnalysisRuntime) Available(key string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	available := !r.closing && r.configuration.Enabled && r.cache != nil && r.profiles[key].Available && r.failures[key] == ""
	r.mu.Unlock()
	return available && r.server.library.Available()
}

// Admission snapshots are detached from the immutable runtime inventory. Paths
// never enter the database profile or the administrator response.
func (r *mediaAnalysisRuntime) executionProfile(key string) (library.AnalysisExecutionProfile, error) {
	if r == nil {
		return library.AnalysisExecutionProfile{}, library.ErrUnavailable
	}
	r.mu.Lock()
	profile, exists := r.profiles[key]
	reason := r.failures[key]
	r.mu.Unlock()
	if !exists {
		return library.AnalysisExecutionProfile{}, library.ErrInvalidInput
	}
	if reason != "" {
		return library.AnalysisExecutionProfile{Version: library.AnalysisProfileVersion, UnavailableReason: reason}, nil
	}
	profile.PreviewWidths = append([]int(nil), profile.PreviewWidths...)
	return profile, nil
}

// A known tool or cache identity failure withdraws future execution availability
// until a new runtime admits repaired inventory. Capacity/cancellation/source
// failures are not sticky dependency faults and do not disable unrelated work.
func (r *mediaAnalysisRuntime) rememberFailure(key string, err error) {
	if r == nil || err == nil {
		return
	}
	reason := ""
	if errors.Is(err, analysiscache.ErrUnsafe) || errors.Is(err, analysiscache.ErrSealMismatch) {
		reason, key = "cache_unavailable", ""
	} else if errors.Is(err, media.ErrAnalysisUnavailable) {
		reason = "dependencies_unavailable"
	}
	if reason == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures == nil {
		r.failures = make(map[string]string)
	}
	for _, candidate := range []string{library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey} {
		if key == "" || key == candidate {
			r.failures[candidate] = reason
		}
	}
}

func (r *mediaAnalysisRuntime) Status() adminMediaAnalysisRuntimeStatus {
	result := adminMediaAnalysisRuntimeStatus{Reasons: []string{}}
	if r == nil {
		result.Reasons = append(result.Reasons, "not_configured")
		return result
	}
	result.Configured = r.configuration.Enabled
	result.IntroAvailable = r.Available(library.TaskIntroAnalysisKey)
	result.PreviewAvailable = r.Available(library.TaskPreviewGenerationKey)
	r.mu.Lock()
	closing := r.closing
	introFailure, previewFailure := r.failures[library.TaskIntroAnalysisKey], r.failures[library.TaskPreviewGenerationKey]
	r.mu.Unlock()
	if closing {
		result.Reasons = append(result.Reasons, "closing")
	} else if !r.configuration.Enabled {
		result.Reasons = append(result.Reasons, "not_configured")
	} else {
		seen := make(map[string]bool)
		for _, reason := range []string{r.availability.AudioReason, r.availability.VideoReason, introFailure, previewFailure} {
			if reason != "" && !seen[reason] {
				result.Reasons = append(result.Reasons, reason)
				seen[reason] = true
			}
		}
		if len(result.Reasons) == 0 && !result.IntroAvailable && !result.PreviewAvailable {
			reason := r.profiles[library.TaskIntroAnalysisKey].UnavailableReason
			if reason == "" {
				reason = "catalog_unavailable"
			}
			result.Reasons = append(result.Reasons, reason)
		}
	}
	if r.cache != nil {
		stats := r.cache.Stats()
		result.Cache = &adminMediaAnalysisCacheStatus{ReadyEntries: stats.ReadyEntries, BuildingEntries: stats.BuildingEntries,
			PendingPublications: stats.PendingPublications, Readers: stats.Readers, ReadyBytes: stats.ReadyBytes,
			ReservedBytes: stats.ReservedBytes, ControlBytes: stats.ControlBytes, TotalBytes: stats.TotalBytes,
			MaxBytes: r.configuration.CacheMaxBytes}
	}
	return result
}

func (r *mediaAnalysisRuntime) enter(ctx context.Context) (context.Context, func(), error) {
	if r == nil || ctx == nil {
		return nil, nil, library.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	work, cancel := context.WithCancel(ctx)
	operation := &mediaAnalysisOperation{cancel: cancel}
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		cancel()
		return nil, nil, library.ErrUnavailable
	}
	if r.activeOperations == nil {
		r.activeOperations = make(map[*mediaAnalysisOperation]struct{})
	}
	r.activeOperations[operation] = struct{}{}
	r.operations.Add(1)
	stop := context.AfterFunc(r.ctx, cancel)
	r.mu.Unlock()
	if r.ctx.Err() != nil {
		cancel()
	}
	var once sync.Once
	leave := func() {
		once.Do(func() {
			stop()
			cancel()
			r.mu.Lock()
			delete(r.activeOperations, operation)
			r.mu.Unlock()
			r.operations.Done()
		})
	}
	return work, leave, nil
}

func (r *mediaAnalysisRuntime) lockPublication(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case r.publish <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-r.publish
			return nil, err
		}
		return func() { <-r.publish }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *mediaAnalysisRuntime) Prune(ctx context.Context, actor identity.Principal, revision string) (adminMediaAnalysisPruneResult, error) {
	var result adminMediaAnalysisPruneResult
	work, leave, err := r.enter(ctx)
	if err != nil {
		return result, err
	}
	defer leave()
	if r.cache == nil {
		return result, library.ErrUnavailable
	}
	unlock, err := r.lockPublication(work)
	if err != nil {
		return result, err
	}
	defer unlock()
	configuration, err := r.server.library.GetAnalysisConfiguration(work, actor)
	if err != nil {
		return result, err
	}
	if revision != configuration.Revision {
		return result, library.ErrAnalysisConflict
	}
	keys, err := r.server.library.CurrentAnalysisPreviewCacheKeys(work, actor)
	if err != nil {
		return result, err
	}
	// No SQL owner is held during storage I/O. Recheck the user's reviewed
	// configuration and current authority immediately before the first delete.
	configuration, err = r.server.library.GetAnalysisConfiguration(work, actor)
	if err != nil {
		return result, err
	}
	if revision != configuration.Revision {
		return result, library.ErrAnalysisConflict
	}
	keep := make(map[string]bool, len(keys))
	for _, key := range keys {
		keep[key] = true
	}
	pruned, err := r.cache.PruneUnreferencedResult(work, keep)
	r.rememberFailure("", err)
	result.RemovedEntries, result.RemovedBytes = pruned.RemovedEntries, pruned.RemovedBytes
	result.RemainingBytes, result.BusyEntries = pruned.RemainingBytes, pruned.BusyEntries
	if err != nil {
		return result, err
	}
	_, err = r.server.library.GetAnalysisConfiguration(work, actor)
	return result, err
}

func (r *mediaAnalysisRuntime) BeginClose() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		r.cancel()
		// AfterFunc propagation is asynchronous. Cancel every admitted operation
		// before returning, while ownership remains with its eventual leave call.
		for operation := range r.activeOperations {
			operation.cancel()
		}
		r.mu.Unlock()
		go func() {
			r.operations.Wait()
			if r.cache != nil {
				r.closeErr = r.cache.Close(context.Background())
			}
			close(r.done)
		}()
	})
}

func (r *mediaAnalysisRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.BeginClose()
	select {
	case <-r.done:
		return r.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
