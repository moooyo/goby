package transcode

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/moooyo/goby/internal/media"
)

var ErrLiveCaption = errors.New("live caption extraction unavailable")

const maxLiveCaptionSequence = int64(1<<31 - 1)

// LiveCaptionSegment borrows a completed private companion file. Its reference
// and subtitle packets were copied by the same input demuxer into one output,
// with no asynchronous subtitle decoder between that input and its segment CSV.
// EndTicks is the reference-packet watermark, never the end of a long cue.
// The first CSV StartTicks can be zero and is not an observed first-media PTS;
// callers must retain the primary media clock for epoch/presentation mapping.
type LiveCaptionSegment struct {
	Slot     int
	Sequence int64
	// Complete is emitted only after successful FFmpeg exit and successful
	// completion of every closed-file callback. It has File=nil and Sequence
	// equal to the number of completed files; it is not an extraction request.
	Complete            bool
	StartTicks          int64
	EndTicks            int64
	File                *os.File
	SubtitleStreamIndex int
	Codec               string
	Container           string
}

func liveCaptionTrack(p Plan, slot int) (HLSSubtitleTrack, error) {
	if p.SourceMode != "stream" || p.OutputMode != "" || slot < 0 || slot >= MaxHLSSubtitleTracks {
		return HLSSubtitleTrack{}, ErrInvalidPlan
	}
	track, ok := HLSSubtitleTrackAt(p, slot)
	if !ok || track.ExternalTag != "" || track.StreamIndex < 0 || track.StreamIndex > maxStreamIndex ||
		media.SubtitleExtractFormat(track.Codec) == "" {
		return HLSSubtitleTrack{}, ErrInvalidPlan
	}
	return track, nil
}

// LiveCaptionContainer preserves the selected source subtitle codec. Timed
// text from MP4 needs a complete MP4 with edit lists; the other admitted text
// codecs have native Matroska mappings. Public HLS fragment rules do not apply
// to these private, finite, independently readable companion files.
func LiveCaptionContainer(p Plan, slot int) (string, error) {
	track, err := liveCaptionTrack(p, slot)
	if err != nil {
		return "", err
	}
	if track.Codec == "mov_text" {
		return "mp4", nil
	}
	return "matroska", nil
}

func liveCaptionExtension(container string) string {
	if container == "mp4" {
		return "mp4"
	}
	return "mkv"
}

// LiveCaptionName is a private scratch name, never a public media artifact.
func LiveCaptionName(p Plan, slot int, sequence int64) (string, error) {
	container, err := LiveCaptionContainer(p, slot)
	if err != nil || sequence < 0 || sequence > maxLiveCaptionSequence {
		return "", ErrInvalidPlan
	}
	return fmt.Sprintf("caption-s%d-%06d.%s.tmp", slot, sequence, liveCaptionExtension(container)), nil
}

// BuildLiveCaptionCompanionArgs appends one output to the primary producer.
// The caller supplies the CSV descriptor and the existing primary input has
// already applied LiveSourceClockBiasTicks. This helper neither opens another
// input nor applies another timestamp offset. Copying captions avoids the
// demonstrated decoder/heartbeat race that can put old cues into later files.
func BuildLiveCaptionCompanionArgs(p Plan, slot, journalFD int) ([]string, error) {
	if err := ValidatePlan(p); err != nil {
		return nil, err
	}
	track, err := liveCaptionTrack(p, slot)
	if err != nil || journalFD < 4 || journalFD > 1023 {
		return nil, ErrInvalidPlan
	}
	container, err := LiveCaptionContainer(p, slot)
	if err != nil {
		return nil, err
	}
	reference, selector := p.AudioStreamIndex, "a:0"
	if reference < 0 {
		reference, selector = p.VideoStreamIndex, "v:0"
	}
	if reference < 0 || reference == track.StreamIndex {
		return nil, ErrInvalidPlan
	}
	args := []string{"-map", "0:" + strconv.Itoa(reference), "-map", "0:" + strconv.Itoa(track.StreamIndex),
		"-map_metadata", "-1", "-map_chapters", "-1", "-dn", "-c", "copy"}
	if selector == "v:0" {
		// A reordered presentation PTS can run ahead of subtitles not yet read.
		// This private reference is not decoded or served as media. Preserve its
		// payload but use the original decode clock as a conservative watermark.
		args = append(args, "-bsf:v:0", "setts=pts=DTS")
	}
	args = append(args, "-avoid_negative_ts", "disabled", "-max_interleave_delta", "500000",
		"-max_muxing_queue_size", "1024", "-f", "segment", "-segment_format", container)
	if container == "mp4" {
		// empty_moov/use_editlist=0 rebases mov_text independently in every file,
		// even with -copyts. A complete moov with edit lists preserves each track.
		args = append(args, "-segment_format_options", "use_editlist=1")
	}
	args = append(args, "-reference_stream", selector, "-break_non_keyframes", "1",
		"-segment_time", strconv.Itoa(p.SegmentSeconds), "-reset_timestamps", "0",
		// Match the media outputs' AV_TIME_BASE rounding tolerance without
		// changing the copied reference packets or their completeness watermark.
		"-segment_time_delta", "0.000001",
		"-segment_list_size", "0", "-segment_list_type", "csv", "-segment_list", "pipe:"+strconv.Itoa(journalFD),
		fmt.Sprintf("caption-s%d-%%06d.%s.tmp", slot, liveCaptionExtension(container)))
	return args, nil
}

func validLiveCaptionSegment(segment LiveCaptionSegment) bool {
	if segment.Complete || segment.Slot < 0 || segment.Slot >= MaxHLSSubtitleTracks || segment.Sequence < 0 || segment.Sequence > maxLiveCaptionSequence ||
		segment.StartTicks < 0 || segment.EndTicks <= segment.StartTicks ||
		segment.File == nil || segment.SubtitleStreamIndex != 1 || media.SubtitleExtractFormat(segment.Codec) == "" {
		return false
	}
	if segment.Codec == "mov_text" {
		return segment.Container == "mp4"
	}
	return segment.Container == "matroska"
}
