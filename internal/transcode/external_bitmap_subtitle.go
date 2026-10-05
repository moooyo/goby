package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/moooyo/goby/internal/media"
)

type bitmapSubtitleSourceContextKey struct{}
type bitmapSubtitleSourceRequest struct {
	spec Spec
	load func(context.Context, Spec, func(context.Context, media.ExternalSubtitleTimelineInput) error) error
}

func withBitmapSubtitleSource(ctx context.Context, spec Spec, load func(context.Context, Spec, func(context.Context, media.ExternalSubtitleTimelineInput) error) error) context.Context {
	return context.WithValue(ctx, bitmapSubtitleSourceContextKey{}, bitmapSubtitleSourceRequest{spec: spec, load: load})
}

func externalBitmapSubtitleFormat(subtitle SubtitlePlan) string {
	if subtitle.ExternalTag == "" {
		return ""
	}
	switch subtitle.Codec {
	case "hdmv_pgs_subtitle":
		return "sup"
	case "dvd_subtitle":
		return "vobsub"
	default:
		return ""
	}
}

func bitmapSubtitleInputStream(plan Plan) int {
	if externalBitmapSubtitleFormat(plan.Subtitle) != "" {
		return plan.Subtitle.ExternalStreamIndex
	}
	return plan.Subtitle.StreamIndex
}

func externalBitmapCanvasFilter(plan Plan) string {
	if externalBitmapSubtitleFormat(plan.Subtitle) == "" {
		return ""
	}
	return "format=rgba,scale=w=" + strconv.Itoa(plan.Subtitle.ExternalCanvasWidth) + ":h=" + strconv.Itoa(plan.Subtitle.ExternalCanvasHeight) + ":flags=bilinear,setsar=1"
}

// prepareExternalBitmapSubtitleAssets materializes a bounded immutable job
// snapshot before the main source enters its reader phase. Only fixed private
// names reach FFmpeg; neither filenames nor descriptors enter the durable plan.
func prepareExternalBitmapSubtitleAssets(ctx context.Context, directory string, primary *os.File, plan Plan) (resultErr error) {
	request, ok := ctx.Value(bitmapSubtitleSourceContextKey{}).(bitmapSubtitleSourceRequest)
	if !ok || request.load == nil || request.spec.Plan != plan {
		return ErrInvalidInput
	}
	var created []string
	defer func() {
		if resultErr != nil {
			for _, path := range created {
				resultErr = errors.Join(resultErr, os.Remove(path))
			}
		}
	}()
	calls := 0
	var consumerErr error
	err := request.load(ctx, request.spec, func(work context.Context, source media.ExternalSubtitleTimelineInput) (consumeErr error) {
		calls++
		defer func() { consumerErr = errors.Join(consumerErr, consumeErr) }()
		if calls != 1 || source.StreamIndex != plan.Subtitle.StreamIndex || source.SourceStreamIndex != plan.Subtitle.ExternalStreamIndex || source.Codec != plan.Subtitle.Codec {
			return ErrInvalidInput
		}
		format := externalBitmapSubtitleFormat(plan.Subtitle)
		if source.Input == nil || format == "" || format == "sup" && source.Companion != nil || format == "vobsub" && source.Companion == nil {
			return ErrInvalidInput
		}
		files, names := []*os.File{source.Input}, []string{"subtitle.sup"}
		if format == "vobsub" {
			files, names = append(files, source.Companion), []string{"subtitle.idx", "subtitle.sub"}
		}
		facts := make([]os.FileInfo, len(files))
		var total int64
		for index, file := range files {
			stat, err := file.Stat()
			if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > media.MaxExternalBitmapSubtitleBytes-total {
				return ErrInvalidInput
			}
			if primary != nil {
				main, err := primary.Stat()
				if err != nil || os.SameFile(main, stat) {
					return ErrInvalidInput
				}
			}
			if index > 0 && os.SameFile(facts[0], stat) {
				return ErrInvalidInput
			}
			facts[index], total = stat, total+stat.Size()
		}
		tracks, err := media.InspectExternalBitmapSubtitles(work, format, source.Input, source.Companion)
		if err != nil || source.SourceStreamIndex >= len(tracks) || tracks[source.SourceStreamIndex].SourceStreamIndex != source.SourceStreamIndex || tracks[source.SourceStreamIndex].Codec != source.Codec {
			return errors.Join(ErrInvalidInput, err)
		}
		remainingBytes := media.MaxExternalBitmapSubtitleBytes
		for index, file := range files {
			path := filepath.Join(directory, names[index])
			output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			created = append(created, path)
			bounded := bitmapSubtitleAssetWriter{writer: output, remaining: &remainingBytes}
			var copyErr error
			if format == "vobsub" && index == 0 {
				// The scanner accepts equivalent IDX whitespace and clock forms
				// that FFmpeg does not. Rebuild only the verified presentation
				// facts so both readers select the same events and languages.
				copyErr = media.WriteCanonicalVobSubIndex(work, source.Input, source.Companion, bounded)
			} else {
				copyErr = copyBitmapSubtitleAsset(work, bounded, file, facts[index].Size())
			}
			if err := errors.Join(copyErr, output.Close()); err != nil {
				return err
			}
		}
		for index, file := range files {
			after, err := file.Stat()
			before := facts[index]
			if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) != media.FileChangeTime(after) {
				return ErrInvalidInput
			}
		}
		return work.Err()
	})
	if err != nil || consumerErr != nil {
		return errors.Join(err, consumerErr)
	}
	if calls != 1 {
		return ErrInvalidInput
	}
	return ctx.Err()
}

type bitmapSubtitleAssetWriter struct {
	writer    io.Writer
	remaining *int64
}

func (writer bitmapSubtitleAssetWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > *writer.remaining {
		return 0, ErrInvalidInput
	}
	count, err := writer.writer.Write(data)
	*writer.remaining -= int64(count)
	return count, err
}

func copyBitmapSubtitleAsset(ctx context.Context, output io.Writer, input *os.File, size int64) error {
	reader := io.NewSectionReader(input, 0, size)
	buffer := make([]byte, 64<<10)
	for remaining := size; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, err := reader.Read(buffer[:min(int64(len(buffer)), remaining)])
		if err != nil {
			return err
		}
		written, err := output.Write(buffer[:count])
		if err != nil {
			return err
		}
		if written != count || count == 0 {
			return io.ErrShortWrite
		}
		remaining -= int64(count)
	}
	return ctx.Err()
}
