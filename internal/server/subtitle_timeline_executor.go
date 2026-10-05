package server

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type subtitleTimelineRuntime struct {
	config    media.BitmapSubtitleConfig
	available bool
	reason    string
}

func newSubtitleTimelineRuntime(ctx context.Context, s *Server) (*subtitleTimelineRuntime, error) {
	config := media.BitmapSubtitleConfig{FFprobePath: s.cfg.FFprobePath}
	capability, err := media.SubtitleTimelineAvailability(ctx, config)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		capability.Available = false
		if capability.Reason == "" {
			capability.Reason = "ffprobe_unavailable"
		}
	}
	config.FFprobeSHA256 = capability.FFprobeSHA256
	return &subtitleTimelineRuntime{config: config, available: capability.Available, reason: capability.Reason}, nil
}

type subtitleTimelineTaskExecutor struct{ server *Server }

func (e subtitleTimelineTaskExecutor) Available() bool {
	return e.server != nil && e.server.subtitleTimelines != nil && e.server.subtitleTimelines.available && e.server.library != nil && e.server.library.Available()
}

func subtitleTimelineErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, media.ErrAnalysisBudget):
		return "processing_limit"
	case errors.Is(err, library.ErrSubtitleTimelineStorageConflict):
		return "material_conflict"
	case errors.Is(err, library.ErrSubtitleTimelineConflict):
		return "request_changed"
	case errors.Is(err, library.ErrSubtitleTimelineStale):
		return "source_changed"
	case errors.Is(err, media.ErrSubtitleTimelineUnsupported):
		return "unsupported_source"
	case errors.Is(err, library.ErrAnalysisSourceChanged), errors.Is(err, library.ErrSourceChanged):
		return "source_changed"
	case errors.Is(err, media.ErrAnalysisUnavailable):
		return "dependencies_unavailable"
	case errors.Is(err, media.ErrBitmapSubtitle):
		return "decode_failed"
	case errors.Is(err, os.ErrPermission):
		return "media_directory_not_writable"
	default:
		return "generation_failed"
	}
}

func (e subtitleTimelineTaskExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskSubtitleTimelineGenerationKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	store := e.server.library
	if err := store.PrepareAutomaticSubtitleTimelines(ctx, work.Fence, work.LibraryID); err != nil {
		return err
	}
	var progress tasks.Progress
	failed := false
	for count := 0; ; count++ {
		job, err := store.ClaimSubtitleTimeline(ctx, work.Fence, work.LibraryID, work.RunID, work.ChildID)
		if err != nil {
			return err
		}
		if job == nil {
			break
		}
		if job.SourceRevision == "" {
			failed = true
		} else {
			artifact, generationErr := store.GenerateSubtitleTimelineWithExternal(ctx, *job, work.Fence, e.encodeExternal)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result := library.SubtitleTimelineResult{Reused: artifact.Reused, ErrorCode: subtitleTimelineErrorCode(generationErr)}
			if err := store.CompleteSubtitleTimeline(ctx, work.Fence, *job, result); err != nil {
				return err
			}
			if generationErr != nil {
				failed = true
				if e.server.log != nil {
					e.server.log.Warn("subtitle timeline generation failed", "item_id", job.ItemID, "code", result.ErrorCode)
				}
			} else if !artifact.Reused {
				progress.Updated++
			}
		}
		progress.Processed++
		if err := report(progress); err != nil {
			return err
		}
		if (count+1)%8 == 0 {
			canYield, err := store.SubtitleTimelineBatchCanYield(ctx, work.Fence)
			if err != nil {
				return err
			}
			if canYield {
				break
			}
		}
	}
	if err := store.RequestSubtitleTimelineContinuation(ctx, work.Fence, work.LibraryID); err != nil {
		return err
	}
	if failed {
		return errors.New("one or more subtitle timelines could not be generated")
	}
	return nil
}

func (e subtitleTimelineTaskExecutor) encode(ctx context.Context, file *os.File, source library.MediaFile, job library.SubtitleTimelineJob, output io.Writer) (media.SubtitleTimelineSummary, error) {
	return e.encodeExternal(ctx, file, source, job, nil, output)
}

func (e subtitleTimelineTaskExecutor) encodeExternal(ctx context.Context, file *os.File, source library.MediaFile, job library.SubtitleTimelineJob, external []media.ExternalSubtitleTimelineInput, output io.Writer) (media.SubtitleTimelineSummary, error) {
	if source.Item.Media == nil {
		return media.SubtitleTimelineSummary{}, library.ErrUnavailable
	}
	return media.GenerateSubtitleTimelinesWithExternal(ctx, e.server.subtitleTimelines.config, file, *source.Item.Media, external, output, nil)
}
