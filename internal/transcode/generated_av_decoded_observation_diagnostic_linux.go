//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/moooyo/goby/internal/media"
)

type generatedAVJoinedFrameProjection struct {
	data       []byte
	inputBytes int64
	hashes     [][32]byte
	fence      func() error
}

// Both APIs call this single joined probe pipeline once. Its projected stdout,
// original flags, budgets, private capture and input identity fences are shared;
// the strict source parser and unknown-preserving observer remain separate.
func measureGeneratedAVJoinedFrameProjection(ctx context.Context, ffprobe string, files []*os.File) (generatedAVJoinedFrameProjection, error) {
	var empty generatedAVJoinedFrameProjection
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	fence, err := generatedAVDiagnosticOuterFence(files)
	if err != nil {
		return empty, err
	}
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &generatedAVDiagnosticBuffer{limit: generatedAVEffectiveJSONBytes, cancel: cancel}
	args := []string{"-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", inputFormats, "-show_frames", "-show_streams", "-show_entries",
		"frame=media_type,stream_index,pts,pkt_dts,duration,nb_samples,key_frame:frame_side_data=side_data_type,skip_samples,discard_padding,skip_reason,discard_reason:stream=index,codec_type,codec_name,time_base,sample_rate,channels,sample_fmt,channel_layout:stream_tags=:stream_disposition=:stream_side_data=",
		"-of", "json", "-i", "pipe:0"}
	count, hashes, err := generatedAVDiagnosticDecode(processCtx, ffprobe, files, args, output)
	if err := errors.Join(err, output.err); err != nil {
		return empty, err
	}
	if err := fence(); err != nil {
		return empty, err
	}
	if err := generatedAVCaptureEffectiveJSON(ctx, output.Bytes(), files); err != nil {
		return empty, err
	}
	if err := fence(); err != nil {
		return empty, err
	}
	return generatedAVJoinedFrameProjection{data: output.Bytes(), inputBytes: count, hashes: hashes, fence: fence}, nil
}

// MeasureGeneratedAVDecodedObservation completes the same bounded probe without
// inventing absent returned PTS/DTS/duration. These are FFprobe-returned fields;
// pkt_dts presence does not prove a physical DTS field in a timestamp-only PES.
func MeasureGeneratedAVDecodedObservation(ctx context.Context, ffprobe string, files []*os.File) (GeneratedAVDecodedObservation, error) {
	var empty GeneratedAVDecodedObservation
	projection, err := measureGeneratedAVJoinedFrameProjection(ctx, ffprobe, files)
	if err != nil {
		return empty, err
	}
	result, err := ParseGeneratedAVDecodedObservation(ctx, projection.data)
	if err != nil {
		return empty, err
	}
	if err := projection.fence(); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.InputBytes, result.InputSHA256, result.Complete = projection.inputBytes, projection.hashes, true
	return result, nil
}
