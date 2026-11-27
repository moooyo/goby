package server

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

type dolbyVisionIngestExecutor struct{ server *Server }

func (e dolbyVisionIngestExecutor) Available() bool {
	s := e.server
	return s != nil && s.library.DolbyVisionVerificationConfigured() && s.library.Available()
}

func (e dolbyVisionIngestExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskDolbyVisionVerificationKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	err := e.server.library.VerifyIngestedDolbyVision(ctx, work.ChildID, work.LibraryID, e.server.cfg.FFmpegPath, work.Fence,
		func(progress library.ProviderProgress) error {
			return report(tasks.Progress{Processed: progress.Processed, Added: progress.Added, Updated: progress.Updated})
		})
	if errors.Is(err, library.ErrUnavailable) {
		return errors.Join(tasks.ErrUnavailable, err)
	}
	return err
}
