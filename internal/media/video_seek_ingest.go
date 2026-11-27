package media

import (
	"context"
	"os"
)

// ProgressiveSeekIngestStream selects one software-decodable H.264/HEVC
// track. This excludes joint AAC and AV1 copy analysis from background ingest.
func ProgressiveSeekIngestStream(info Info) *Stream {
	if info.ProbeVersion != CurrentProbeVersion || !info.FormatStartKnown || info.DurationTicks <= 0 || info.DurationTicks > MaxVideoSeekDurationTicks {
		return nil
	}
	var selected *Stream
	for position := range info.Streams {
		stream := &info.Streams[position]
		if stream.CodecType != "video" || stream.IsAttachedPicture || stream.IsExternal || stream.Index < 0 ||
			(stream.Codec != "h264" && stream.Codec != "hevc") || !videoCopySeekSourceCodecSupported(*stream) {
			continue
		}
		if _, ok := videoSeekFrameBytes(stream.Width, stream.Height, stream.PixelFormat); !ok ||
			videoCopySeekSourceDepth(*stream) != videoSeekPixelDepth(stream.PixelFormat) {
			continue
		}
		if _, err := parseVideoSeekTimeBase(stream.TimeBase); err != nil {
			continue
		}
		if selected == nil || !selected.IsDefault && stream.IsDefault {
			selected = stream
		}
	}
	return selected
}

// AnalyzeProgressiveSeekIngest reuses the established source, decoder,
// executable, and retirement checks with a single video track and no audio.
// A missing result is optional evidence, not proof the media is unplayable.
func AnalyzeProgressiveSeekIngest(ctx context.Context, executable string, file *os.File, info Info) ([]VideoSeekIndex, error) {
	stream := ProgressiveSeekIngestStream(info)
	if stream == nil {
		return nil, ctx.Err()
	}
	info.Streams = []Stream{*stream}
	info.VideoSeekIndexes = nil
	return AnalyzeVideoSeekIndexes(WithBackgroundProcess(ctx), executable, file, info)
}
