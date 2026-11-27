package server

import (
	"context"
	"errors"
	"sync"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/tasks"
)

type managementTaskExecutor struct {
	server *Server
	key    string
}

func (executor managementTaskExecutor) Available() bool {
	s := executor.server
	if s.library == nil || s.settings == nil {
		return false
	}
	if executor.key == tasks.CacheMaintainKey {
		return true
	}
	if !s.cfg.OnlineProviders.Enabled || !s.settings.InternetProvidersEnabled() {
		return false
	}
	for _, status := range s.onlineProviderConfig().Status() {
		if !status.Configured {
			continue
		}
		if executor.key == tasks.MetadataRefreshKey && (status.ID == "tmdb" || status.ID == "musicbrainz") {
			return true
		}
		if executor.key == tasks.SubtitleDownloadKey && status.ID == "opensubtitles" {
			return true
		}
	}
	return false
}

func (executor managementTaskExecutor) Execute(ctx context.Context, work tasks.Work, progress func(tasks.Progress) error) (resultErr error) {
	s := executor.server
	if s.library == nil || s.settings == nil || work.TaskKey != executor.key {
		return tasks.ErrUnavailable
	}
	options := s.settings.ManagementSnapshot()
	providerConfig := s.onlineProviderConfigForMetadata(options.Metadata)
	client := providers.New(providerConfig)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if executor.key == tasks.SubtitleDownloadKey {
		work = work.WithContext(workCtx)
		granted, release, err := s.library.BeginTaskSourceOperation(workCtx, work.ChildID, work.Fence)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, release()) }()
		workCtx = granted
	}
	var progressMu sync.Mutex
	var progressErr error
	report := func(value library.ProviderProgress) {
		progressMu.Lock()
		defer progressMu.Unlock()
		if progressErr != nil {
			return
		}
		progressErr = progress(tasks.Progress{Processed: value.Processed, Added: value.Added, Updated: value.Updated})
		if progressErr != nil {
			cancel()
		}
	}
	var err error
	switch executor.key {
	case tasks.MetadataRefreshKey:
		err = s.library.RefreshOnlineMetadata(workCtx, client, work.LibraryID, report)
	case tasks.SubtitleDownloadKey:
		err = s.library.DownloadOnlineSubtitles(workCtx, client, work.LibraryID, options.Subtitles.DownloadLanguages,
			options.Subtitles.DownloadMovieSubtitles, options.Subtitles.DownloadEpisodeSubtitles, report)
	case tasks.CacheMaintainKey:
		err = s.library.PruneOnlineCache(workCtx, options.Tasks.CacheRetentionDays, options.Tasks.CacheMaxEntries, report)
	default:
		return tasks.ErrUnavailable
	}
	progressMu.Lock()
	defer progressMu.Unlock()
	if progressErr != nil {
		return progressErr
	}
	if errors.Is(err, providers.ErrNotConfigured) || errors.Is(err, providers.ErrDisabled) || errors.Is(err, providers.ErrUnavailable) {
		return tasks.ErrUnavailable
	}
	return err
}

func (s *Server) managementTaskExecutors(ctx context.Context) (*tasks.ExecutorRegistry, error) {
	localAutomatic := s.cfg.FastLibraryIngest
	if !localAutomatic {
		// Turning off fast ingest does not abandon local authority markers.
		// Fresh default-off installations need no periodic empty task runs.
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE import_nfo OR import_images)`).Scan(&localAutomatic); err != nil {
			return nil, err
		}
	}
	intro := mediaAnalysisTaskExecutor{s.mediaAnalysis, library.TaskIntroAnalysisKey}
	credits := mediaAnalysisTaskExecutor{s.mediaAnalysis, library.TaskCreditsAnalysisKey}
	previews := mediaAnalysisTaskExecutor{s.mediaAnalysis, library.TaskPreviewGenerationKey}
	registrations := []tasks.ExecutorRegistration{
		{Key: library.TaskLocalMetadataIngestKey, Name: "Import local metadata", Description: "Complete local NFO and artwork imports after a media scan.", Category: "Metadata", AutomaticDisabled: !localAutomatic, Executor: localMetadataIngestExecutor{s}},
		{Key: library.TaskOnlineMetadataIngestKey, Name: "Enrich newly scanned metadata", Description: "Complete opt-in movie and TV metadata imports using a configured provider after local metadata is ready.", Category: "Metadata", AutomaticDisabled: !s.cfg.FastLibraryIngest || !s.cfg.FastLibraryOnlineMetadata || !s.cfg.OnlineProviders.Enabled, Executor: onlineMetadataIngestExecutor{s}},
		{Key: library.TaskVideoSeekIngestKey, Name: "Index video seek points", Description: "Build optional seek evidence after fast scans without changing the cataloged media source.", Category: "Media analysis", AutomaticDisabled: !s.cfg.FastLibraryIngest || s.cfg.FFmpegPath == "", Executor: videoSeekIngestExecutor{s}},
		{Key: library.TaskDolbyVisionVerificationKey, Name: "Verify Dolby Vision", Description: "Complete Dolby Vision RPU verification for video sources accepted by completed fast scans.", Category: "Media analysis", AutomaticDisabled: !s.library.DolbyVisionVerificationConfigured(), Executor: dolbyVisionIngestExecutor{s}},
		tasks.ExecutorRegistration{Key: tasks.MetadataRefreshKey, Name: "Refresh online metadata", Description: "Refresh supported metadata from configured providers for each library.", Category: "Metadata", Executor: managementTaskExecutor{s, tasks.MetadataRefreshKey}},
		tasks.ExecutorRegistration{Key: tasks.SubtitleDownloadKey, Name: "Download missing subtitles", Description: "Download configured subtitle languages for supported movies and episodes.", Category: "Subtitles", Executor: managementTaskExecutor{s, tasks.SubtitleDownloadKey}},
		tasks.ExecutorRegistration{Key: tasks.CacheMaintainKey, Name: "Maintain provider cache", Description: "Remove expired provider cache entries and enforce the configured entry limit.", Category: "Maintenance", Global: true, Executor: managementTaskExecutor{s, tasks.CacheMaintainKey}},
		tasks.ExecutorRegistration{Key: library.TaskIntroAnalysisKey, Name: "Analyze episode introductions", Description: "Match repeated audiovisual introductions in bounded episode cohorts.", Category: "Media analysis", Executor: intro, AnalysisAdmission: intro.admission},
		tasks.ExecutorRegistration{Key: library.TaskCreditsAnalysisKey, Name: "Analyze credits", Description: "Detect source-bound credits using chapter, visual and same-season audio evidence.", Category: "Media analysis", Executor: credits, AnalysisAdmission: credits.admission},
		tasks.ExecutorRegistration{Key: library.TaskPreviewGenerationKey, Name: "Generate seek previews", Description: "Generate bounded BIF and thumbnail previews for local video sources.", Category: "Media analysis", Executor: previews, AnalysisAdmission: previews.admission},
		tasks.ExecutorRegistration{Key: library.TaskBackgroundPreviewGenerationKey, Name: "Generate background clips", Description: "Generate persistent silent background clips beside their original media. Existing clips are retained unless explicitly regenerated.", Category: "Media analysis", Executor: backgroundPreviewTaskExecutor{s}},
		tasks.ExecutorRegistration{Key: library.TaskAudioWaveformGenerationKey, Name: "Generate audio waveforms", Description: "Generate persistent per-track waveform envelopes beside their original media. Existing artifacts are retained unless explicitly regenerated.", Category: "Media analysis", Executor: audioWaveformTaskExecutor{s}},
		tasks.ExecutorRegistration{Key: library.TaskSubtitleTimelineGenerationKey, Name: "Generate subtitle timelines", Description: "Generate persistent display intervals for embedded PGS/DVD subtitles, external SUP files, and multilingual IDX/SUB pairs. Existing artifacts are retained unless explicitly regenerated.", Category: "Media analysis", Executor: subtitleTimelineTaskExecutor{s}},
		s.onlineImagesTaskRegistration(),
	}
	if manager, ok := s.recovery.(interface {
		BackupPolicyTaskRegistration(library.OwnedTransactions) tasks.ExecutorRegistration
	}); ok {
		registrations = append(registrations, manager.BackupPolicyTaskRegistration(s.library))
	}
	return tasks.NewExecutorRegistry(registrations...)
}
