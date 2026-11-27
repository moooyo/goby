package server

import (
	"context"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

type localMetadataIngestExecutor struct{ server *Server }

func (e localMetadataIngestExecutor) Available() bool {
	return e.server != nil && e.server.library != nil && e.server.library.Available()
}

func (e localMetadataIngestExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskLocalMetadataIngestKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	return e.server.library.ImportLocalScanMetadata(ctx, work.ChildID, work.LibraryID, work.Fence, func(p library.ProviderProgress) error {
		return report(tasks.Progress{Processed: p.Processed, Updated: p.Updated})
	})
}
