package server

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/tasks"
)

type onlineImagesTaskExecutor struct{ server *Server }

func (executor onlineImagesTaskExecutor) Available() bool {
	s := executor.server
	if s == nil || s.library == nil || s.settings == nil {
		return false
	}
	for _, status := range s.onlineProviderConfig().Status() {
		if status.ID == "tmdb" && status.Configured {
			return true
		}
	}
	return false
}

func (executor onlineImagesTaskExecutor) Execute(ctx context.Context, work tasks.Work, progress func(tasks.Progress) error) error {
	s := executor.server
	if !executor.Available() || work.TaskKey != library.TaskOnlineImagesKey || work.LibraryID == "" {
		return tasks.ErrUnavailable
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	work = work.WithContext(workCtx)
	client := providers.New(s.onlineProviderConfig())
	var progressErr error
	err := s.library.FillMissingOnlineImages(workCtx, client, work.LibraryID, work.Fence, func(value library.ProviderProgress) {
		if progressErr == nil {
			progressErr = progress(tasks.Progress{Processed: value.Processed, Added: value.Added, Updated: value.Updated})
			if progressErr != nil {
				cancel()
			}
		}
	})
	if progressErr != nil {
		return progressErr
	}
	if errors.Is(err, providers.ErrNotConfigured) || errors.Is(err, providers.ErrDisabled) || errors.Is(err, providers.ErrUnavailable) {
		return tasks.ErrUnavailable
	}
	return err
}

func (s *Server) onlineImagesTaskRegistration() tasks.ExecutorRegistration {
	return tasks.ExecutorRegistration{Key: library.TaskOnlineImagesKey, Name: "Download missing images",
		Description: "Fill missing movie and TV artwork from known TMDB identities in explicitly enabled libraries. Existing and manually managed artwork is retained.",
		Category:    "Images", Executor: onlineImagesTaskExecutor{s}}
}
