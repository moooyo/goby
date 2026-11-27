package server

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

type nfoRecoveryExecutor struct{ server *Server }

func (e nfoRecoveryExecutor) Available() bool {
	return e.server != nil && e.server.library.Available()
}

func (e nfoRecoveryExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskNFORecoveryKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	err := e.server.library.RegenerateLocalNFO(ctx, work.ChildID, work.LibraryID, work.Fence, func(p library.ProviderProgress) error {
		return report(tasks.Progress{Processed: p.Processed, Added: p.Added, Updated: p.Updated})
	})
	if errors.Is(err, library.ErrUnavailable) {
		return errors.Join(tasks.ErrUnavailable, err)
	}
	return err
}
