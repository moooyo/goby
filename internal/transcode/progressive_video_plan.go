package transcode

import (
	"fmt"
	"strconv"
)

func validateProgressiveVideoPlan(p Plan) error {
	invalid := func(field string) error { return fmt.Errorf("%w: progressive video %s", ErrInvalidPlan, field) }
	if p.OutputMode != "progressive" || p.Container != "mp4" || p.VideoStreamIndex < 0 || p.VideoCodec != "copy" && p.VideoCodec != "h264" ||
		p.AudioStreamIndex < -1 || p.AudioStreamIndex < 0 && p.AudioCodec != "" || p.AudioStreamIndex >= 0 && p.AudioCodec != "copy" && p.AudioCodec != "aac" {
		return invalid("streams")
	}
	if !p.SourceFormatStartKnown || p.SourceFormatStartTicks < -maxDurationTicks || p.SourceFormatStartTicks > maxDurationTicks {
		return invalid("source format clock")
	}
	if p.SegmentSeconds != 0 || p.SegmentMode != "" || p.SegmentStartNumber != 0 || p.EndTicks != 0 || p.SegmentTimes != "" || p.ReferenceStartTicks != 0 ||
		p.AudioBitDepth != 0 || p.AudioSourceSampleRate != 0 || p.AudioSourceSampleCount != 0 || p.AudioSampleSeek {
		return invalid("inapplicable options")
	}
	// Fragmented MP4 edit lists do not consistently hide copied video pre-roll
	// across clients. Until that contract is established, a requested seek must
	// use the precise decoded-video path or fail instead of returning an earlier
	// keyframe as though it were the requested presentation position.
	if p.VideoCodec == "copy" && p.StartTicks != 0 {
		return invalid("video copy seek")
	}
	if err := validateProgressiveVideoSeekCandidate(p); err != nil {
		return err
	}
	// Reuse the HLS engine's closed stream, video, hardware, and resource limits.
	// Only its output-specific fields differ; no caller option bypasses checks.
	video := p
	video.OutputMode, video.Container, video.SegmentSeconds = "", "ts", 1
	video.SourceFormatStartKnown, video.SourceFormatStartTicks = false, 0
	video.VideoSeekCandidate = ""
	if err := ValidatePlan(video); err != nil {
		return err
	}
	if p.AudioCodec == "aac" {
		// AAC in MP4 has the same native encoder/layout limits as progressive
		// M4A. Copy streams retain their probed framing and codec responsibility
		// in the planner rather than inventing transform parameters here.
		audio := Plan{OutputMode: "progressive", Container: "m4a", VideoStreamIndex: -1, AudioStreamIndex: p.AudioStreamIndex,
			AudioCodec: "aac", DurationTicks: p.DurationTicks, StartTicks: p.StartTicks,
			AudioBitrate: p.AudioBitrate, AudioChannels: p.AudioChannels, AudioSampleRate: p.AudioSampleRate}
		if err := validateProgressivePlan(audio); err != nil {
			return err
		}
	}
	return nil
}

func buildProgressiveVideoArgs(p Plan, threads int) []string {
	// Constructing a plan or previewing arguments must never authorize a fast
	// restart. Only Run can supply a positive value after its fresh preflight.
	return buildProgressiveVideoArgsWithSeek(p, threads, 0)
}

func buildProgressiveVideoArgsWithSeek(p Plan, threads int, inputSeekTicks int64) []string {
	threadCount := strconv.Itoa(threads)
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "level+warning", "-y", "-progress", "pipe:1", "-stats_period", "0.5",
		"-filter_threads", threadCount, "-filter_complex_threads", threadCount, "-copyts"}
	args, decode, encode := appendHardwareInputArgs(args, p.Hardware)
	// FFmpeg may otherwise change its effective input origin when a track is
	// disabled. Use one probed container clock for every stream and selection.
	args = append(args, "-threads", threadCount, "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats)
	if inputSeekTicks > 0 {
		// Reuse the preflighted argument exactly. An observed earlier landing
		// is evidence about this argument, not a replacement seek argument.
		args = append(args, "-seek_timestamp", "1", "-noaccurate_seek", "-ss", signedTickSeconds(p.SourceFormatStartTicks+inputSeekTicks))
	}
	args = append(args, "-itsoffset", signedTickSeconds(-p.SourceFormatStartTicks), "-i", "/proc/self/fd/3")
	audioInput := "0:"
	if inputSeekTicks > 0 && p.AudioStreamIndex >= 0 {
		// Reopen the same inherited source independently. Linear audio keeps
		// decoder history and the existing sample/timestamp behavior, while
		// discarded video avoids the expensive prefix video decoding. This
		// path may still read the audio input's container linearly.
		args = append(args, "-threads", threadCount, "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats,
			"-discard:v", "all", "-itsoffset", signedTickSeconds(-p.SourceFormatStartTicks), "-i", "/proc/self/fd/3")
		audioInput = "1:"
	}
	if p.StartTicks > 0 {
		// Trim presentation on the shared source clock, whether video starts
		// from the beginning or from a freshly verified restart. Audio retains
		// the same linear decoding history in both cases.
		args = append(args, "-ss", tickSeconds(p.StartTicks))
	}
	args = append(args, "-t", tickSeconds(p.DurationTicks-p.StartTicks), "-map", "0:"+strconv.Itoa(p.VideoStreamIndex),
		"-map_metadata", "-1", "-map_metadata:s:v", "-1", "-map_chapters", "-1", "-sn", "-dn", "-tag:v", "avc1")
	if p.VideoCodec == "copy" {
		args = append(args, "-c:v", "copy")
	} else {
		codec := "libx264"
		if encode != "software" {
			codec = "h264_" + encode
		}
		filter, timeBase := videoFilter(p, decode, encode), "demux"
		if p.FrameRate > 0 {
			// A requested frame rate is a real frame conversion. Without this
			// option, retain the source timestamps and do not claim a CFR stream.
			filter += ",fps=" + strconv.FormatFloat(p.FrameRate, 'f', -1, 64)
			timeBase = "filter"
		}
		args = append(args, "-c:v", codec, "-threads:v", threadCount, "-vf", filter, "-bf", "0",
			"-fps_mode", "passthrough", "-enc_time_base:v", timeBase, "-force_key_frames", "expr:gte(t,n_forced)")
		args = appendVideoEncoderOptions(args, encode)
		bitrate := p.VideoBitrate
		if bitrate == 0 {
			bitrate = 4_000_000
		}
		args = append(args, "-b:v", strconv.FormatInt(bitrate, 10), "-maxrate", strconv.FormatInt(bitrate, 10), "-bufsize", strconv.FormatInt(bitrate*2, 10))
	}
	if p.AudioStreamIndex < 0 {
		args = append(args, "-an")
	} else {
		args = append(args, "-map", audioInput+strconv.Itoa(p.AudioStreamIndex), "-map_metadata:s:a", "-1", "-tag:a", "mp4a")
		if p.AudioCodec == "copy" {
			// Transport-stream AAC needs its ADTS framing converted before the
			// delayed immutable moov is written. MP4/MKV AAC is also accepted.
			args = append(args, "-c:a", "copy", "-bsf:a", "aac_adtstoasc")
		} else {
			channels, rate := progressiveAudioDimensions(p)
			layout := []string{"", "mono", "stereo", "3.0", "4.0", "5.0", "5.1", "6.1", "7.1"}[channels]
			bitrate := p.AudioBitrate
			if bitrate == 0 {
				bitrate = min(int64(192000), 6*int64(rate)*int64(channels))
			}
			// Preserve the common audio/video clock. The audio-only pipeline's
			// asetpts=N/SR/TB would erase a legitimate source track offset here.
			args = append(args, "-c:a", "aac", "-threads:a", threadCount, "-profile:a", "aac_low",
				"-b:a", strconv.FormatInt(bitrate, 10), "-ac", strconv.Itoa(channels), "-ar", strconv.Itoa(rate), "-channel_layout", layout)
		}
	}
	return append(args, "-avoid_negative_ts", "disabled", "-flush_packets", "1", "-max_muxing_queue_size", "1024", "-f", "mp4",
		// Delayed initialization preserves B-frame edit lists and AAC priming.
		// Duration and size are independent fragment triggers, not a promise
		// that each subsequent fragment starts with a random-access picture.
		"-movflags", "+empty_moov+delay_moov+default_base_moof+skip_trailer", "-frag_duration", "1000000", "-frag_size", "1048576", "pipe:4")
}

func signedTickSeconds(ticks int64) string {
	if ticks < 0 {
		return "-" + tickSeconds(-ticks)
	}
	return tickSeconds(ticks)
}
