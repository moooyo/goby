package server

import (
	"context"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

type localMetadataIngestExecutor struct {
	server *Server
	key    string
}

func (e localMetadataIngestExecutor) Available() bool {
	return e.server != nil && e.server.library != nil && e.server.library.Available()
}

func (e localMetadataIngestExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != e.key || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	progress := func(p library.ProviderProgress) error {
		return report(tasks.Progress{Processed: p.Processed, Updated: p.Updated})
	}
	switch e.key {
	case library.TaskLocalMetadataIngestKey:
		return e.server.library.ImportLocalScanMetadata(ctx, work.ChildID, work.LibraryID, work.Fence, progress)
	case library.TaskLocalArtworkIngestKey:
		return e.server.library.ImportLocalScanArtwork(ctx, work.ChildID, work.LibraryID, work.Fence, progress)
	default:
		return tasks.ErrUnavailable
	}
}
