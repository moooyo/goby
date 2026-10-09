package server

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type backgroundPreviewRuntime struct {
	extractor   media.AnalysisExtractor
	available   bool
	reason      string
	dolbyVision backgroundPreviewDolbyVisionState
}

// Persistent source-side clips do not depend on the disposable BIF cache.
// Tool inspection happens once, outside task repository transactions.
func newBackgroundPreviewRuntime(ctx context.Context, s *Server) (*backgroundPreviewRuntime, error) {
	r := &backgroundPreviewRuntime{}
	r.extractor = media.AnalysisExtractor{FFmpegPath: s.cfg.FFmpegPath, FFprobePath: s.cfg.FFprobePath}
	capabilities, err := r.extractor.BackgroundClipAvailability(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r.reason = capabilities.Reason
	if err != nil || !capabilities.Available {
		if r.reason == "" {
			r.reason = "dependencies_unavailable"
		}
		return r, nil
	}
	r.extractor.ExpectedFFmpegSHA256, r.extractor.ExpectedFFprobeSHA256 = capabilities.FFmpegSHA256, capabilities.FFprobeSHA256
	r.available, r.reason = true, ""
	return r, nil
}

type backgroundPreviewTaskExecutor struct{ server *Server }

func (e backgroundPreviewTaskExecutor) Available() bool {
	return e.server != nil && e.server.backgroundPreviews != nil && e.server.backgroundPreviews.available && e.server.library != nil && e.server.library.Available()
}

func backgroundPreviewErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, media.ErrAnalysisBudget):
		return "processing_limit"
	case errors.Is(err, library.ErrBackgroundClipConflict):
		return "material_conflict"
	case errors.Is(err, library.ErrBackgroundPreviewConflict):
		return "request_changed"
	case errors.Is(err, media.ErrBackgroundClipDolbyVisionUnavailable):
		return "dolby_vision_unavailable"
	case errors.Is(err, media.ErrBackgroundClipUnsupported):
		return "unsupported_source"
	case errors.Is(err, media.ErrBackgroundClipUnusable):
		return "unusable_pictures"
	case errors.Is(err, library.ErrAnalysisSourceChanged), errors.Is(err, library.ErrSourceChanged):
		return "source_changed"
	case errors.Is(err, media.ErrAnalysisUnavailable):
		return "dependencies_unavailable"
	case errors.Is(err, os.ErrPermission):
		return "media_directory_not_writable"
	default:
		return "generation_failed"
	}
}

func (e backgroundPreviewTaskExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskBackgroundPreviewGenerationKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	store := e.server.library
	if err := store.PrepareAutomaticBackgroundPreviews(ctx, work.Fence, work.LibraryID); err != nil {
		return err
	}
	var progress tasks.Progress
	failed := false
	// Bound each library turn so the shared analysis slot can serve intros and
	// seek previews between batches. Late requests remain in the durable queue.
	for count := 0; ; count++ {
		job, err := store.ClaimBackgroundPreview(ctx, work.Fence, work.LibraryID, work.RunID, work.ChildID)
		if err != nil {
			return err
		}
		if job == nil {
			break
		}
		if job.SourceRevision == "" {
			failed = true
		} else {
			artifact, generationErr := store.GenerateBackgroundClip(ctx, *job, work.Fence, e.encode)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result := library.BackgroundPreviewResult{Reused: artifact.Reused, ErrorCode: backgroundPreviewErrorCode(generationErr)}
			if err := store.CompleteBackgroundPreview(ctx, work.Fence, *job, result); err != nil {
				return err
			}
			if generationErr != nil {
				failed = true
				if e.server.log != nil {
					e.server.log.Warn("background clip generation failed", "item_id", job.ItemID, "code", result.ErrorCode)
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
			canYield, err := store.BackgroundPreviewBatchCanYield(ctx, work.Fence)
			if err != nil {
				return err
			}
			if canYield {
				break
			}
		}
	}
	if err := store.RequestBackgroundPreviewContinuation(ctx, work.Fence, work.LibraryID); err != nil {
		return err
	}
	if failed {
		return errors.New("one or more background clips could not be generated")
	}
	return nil
}

func (e backgroundPreviewTaskExecutor) encode(ctx context.Context, file *os.File, source library.MediaFile, job library.BackgroundPreviewJob, output io.Writer) (media.BackgroundClipSummary, error) {
	if source.Item.Media == nil {
		return media.BackgroundClipSummary{}, library.ErrUnavailable
	}
	index := -1
	for _, stream := range source.Item.Media.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture && !stream.IsExternal {
			index = stream.Index
			break
		}
	}
	if index < 0 {
		return media.BackgroundClipSummary{}, media.ErrBackgroundClipUnsupported
	}
	extractor := e.server.backgroundPreviews.extractor
	extractor.Limits.Timeout = time.Duration(job.Profile.MaxItemRuntimeSeconds) * time.Second
	extractor.Limits.MaxOutputBytes = media.MaxBackgroundClipBytes
	extractor.Limits.MaxStderrBytes = 1 << 20
	options := media.BackgroundClipOptions{MaxWidth: job.Profile.MaxWidth, VideoBitrate: job.Profile.VideoBitrate}
	for _, stream := range source.Item.Media.Streams {
		if stream.Index == index && (stream.DolbyVision != nil || stream.VideoRange == "DOVI") {
			var err error
			options.DolbyVision, err = e.server.backgroundDolbyVisionOptions(ctx)
			if err != nil {
				return media.BackgroundClipSummary{}, err
			}
			options.ValidateHardware = e.server.backgroundClipDeviceCheck(options.DolbyVision.Device)
			break
		}
	}
	return extractor.GenerateBackgroundClipWithOptions(ctx, file, *source.Item.Media, index, job.StartTicks, job.DurationTicks,
		options, output)
}
