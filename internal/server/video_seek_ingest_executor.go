package server

import (
	"context"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

type videoSeekIngestExecutor struct{ server *Server }

func (e videoSeekIngestExecutor) Available() bool {
	s := e.server
	return s != nil && s.cfg.FastLibraryIngest && s.cfg.FFmpegPath != "" && s.library != nil && s.library.Available()
}

func (e videoSeekIngestExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskVideoSeekIngestKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	return e.server.library.BuildIngestedVideoSeekIndexes(ctx, work.ChildID, work.LibraryID, e.server.cfg.FFmpegPath, work.Fence,
		func(p library.ProviderProgress) error {
			return report(tasks.Progress{Processed: p.Processed, Updated: p.Updated})
		})
}
