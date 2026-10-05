package server

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type audioWaveformRuntime struct {
	extractor media.AnalysisExtractor
	available bool
	reason    string
}

func newAudioWaveformRuntime(ctx context.Context, s *Server) (*audioWaveformRuntime, error) {
	r := &audioWaveformRuntime{reason: "unsupported_platform"}
	if runtime.GOOS != "linux" {
		return r, nil
	}
	r.extractor = media.AnalysisExtractor{FFmpegPath: s.cfg.FFmpegPath, FFprobePath: s.cfg.FFprobePath}
	capability, err := r.extractor.AudioWaveformAvailability(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r.reason = capability.Reason
	if err != nil || !capability.Available {
		if r.reason == "" {
			r.reason = "dependencies_unavailable"
		}
		return r, nil
	}
	r.extractor.ExpectedFFmpegSHA256 = capability.FFmpegSHA256
	r.available, r.reason = true, ""
	return r, nil
}

type audioWaveformTaskExecutor struct{ server *Server }

func (e audioWaveformTaskExecutor) Available() bool {
	return e.server != nil && e.server.audioWaveforms != nil && e.server.audioWaveforms.available && e.server.library != nil && e.server.library.Available()
}

func audioWaveformErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, media.ErrAnalysisBudget):
		return "processing_limit"
	case errors.Is(err, library.ErrAudioWaveformStorageConflict):
		return "material_conflict"
	case errors.Is(err, library.ErrAudioWaveformConflict):
		return "request_changed"
	case errors.Is(err, library.ErrAudioWaveformStale):
		return "source_changed"
	case errors.Is(err, media.ErrAudioWaveformUnsupported):
		return "unsupported_source"
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

func (e audioWaveformTaskExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskAudioWaveformGenerationKey || report == nil {
		return tasks.ErrUnavailable
	}
	work = work.WithContext(ctx)
	store := e.server.library
	if err := store.PrepareAutomaticAudioWaveforms(ctx, work.Fence, work.LibraryID); err != nil {
		return err
	}
	var progress tasks.Progress
	failed := false
	for count := 0; ; count++ {
		job, err := store.ClaimAudioWaveform(ctx, work.Fence, work.LibraryID, work.RunID, work.ChildID)
		if err != nil {
			return err
		}
		if job == nil {
			break
		}
		if job.SourceRevision == "" {
			failed = true
		} else {
			artifact, generationErr := store.GenerateAudioWaveform(ctx, *job, work.Fence, e.encode)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result := library.AudioWaveformResult{Reused: artifact.Reused, ErrorCode: audioWaveformErrorCode(generationErr)}
			if err := store.CompleteAudioWaveform(ctx, work.Fence, *job, result); err != nil {
				return err
			}
			if generationErr != nil {
				failed = true
				if e.server.log != nil {
					e.server.log.Warn("audio waveform generation failed", "item_id", job.ItemID, "code", result.ErrorCode)
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
			canYield, err := store.AudioWaveformBatchCanYield(ctx, work.Fence)
			if err != nil {
				return err
			}
			if canYield {
				break
			}
		}
	}
	if err := store.RequestAudioWaveformContinuation(ctx, work.Fence, work.LibraryID); err != nil {
		return err
	}
	if failed {
		return errors.New("one or more audio waveforms could not be generated")
	}
	return nil
}

func (e audioWaveformTaskExecutor) encode(ctx context.Context, file *os.File, source library.MediaFile, job library.AudioWaveformJob, output io.Writer) (media.AudioWaveformSummary, error) {
	if source.Item.Media == nil {
		return media.AudioWaveformSummary{}, library.ErrUnavailable
	}
	extractor := e.server.audioWaveforms.extractor
	extractor.Limits.Timeout = library.AudioWaveformItemTimeoutSeconds * time.Second
	extractor.Limits.MaxOutputBytes = media.MaxAudioWaveformBytes
	return extractor.GenerateAudioWaveforms(ctx, file, *source.Item.Media, output, nil)
}
