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
	if !s.cfg.OnlineProviders.Enabled || !s.settings.Snapshot().Management.Metadata.EnableInternetProviders {
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

func (executor managementTaskExecutor) Execute(ctx context.Context, work tasks.Work, progress func(tasks.Progress) error) error {
	s := executor.server
	if s.library == nil || s.settings == nil || work.TaskKey != executor.key {
		return tasks.ErrUnavailable
	}
	options := s.settings.Snapshot().Management
	providerConfig := s.onlineProviderConfig()
	providerConfig.Enabled = s.cfg.OnlineProviders.Enabled && options.Metadata.EnableInternetProviders
	providerConfig.Language = options.Metadata.PreferredMetadataLanguage
	providerConfig.Country = options.Metadata.MetadataCountryCode
	client := providers.New(providerConfig)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
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

func (s *Server) managementTaskExecutors() (*tasks.ExecutorRegistry, error) {
	intro := mediaAnalysisTaskExecutor{s.mediaAnalysis, library.TaskIntroAnalysisKey}
	previews := mediaAnalysisTaskExecutor{s.mediaAnalysis, library.TaskPreviewGenerationKey}
	return tasks.NewExecutorRegistry(
		tasks.ExecutorRegistration{Key: tasks.MetadataRefreshKey, Name: "Refresh online metadata", Description: "Refresh supported metadata from configured providers for each library.", Category: "Metadata", Executor: managementTaskExecutor{s, tasks.MetadataRefreshKey}},
		tasks.ExecutorRegistration{Key: tasks.SubtitleDownloadKey, Name: "Download missing subtitles", Description: "Download configured subtitle languages for supported movies and episodes.", Category: "Subtitles", Executor: managementTaskExecutor{s, tasks.SubtitleDownloadKey}},
		tasks.ExecutorRegistration{Key: tasks.CacheMaintainKey, Name: "Maintain provider cache", Description: "Remove expired provider cache entries and enforce the configured entry limit.", Category: "Maintenance", Global: true, Executor: managementTaskExecutor{s, tasks.CacheMaintainKey}},
		tasks.ExecutorRegistration{Key: library.TaskIntroAnalysisKey, Name: "Analyze episode introductions", Description: "Match repeated audiovisual introductions in bounded episode cohorts.", Category: "Media analysis", Executor: intro, AnalysisAdmission: intro.admission},
		tasks.ExecutorRegistration{Key: library.TaskPreviewGenerationKey, Name: "Generate seek previews", Description: "Generate bounded BIF and thumbnail previews for local video sources.", Category: "Media analysis", Executor: previews, AnalysisAdmission: previews.admission},
	)
}
