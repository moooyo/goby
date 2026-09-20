package transcode

import (
	"math"
	"strconv"
	"strings"
)

func appendHLSVideoMap(args []string, p Plan, decode, encode string) []string {
	if graph, output, _ := BitmapSubtitleGraph(p, videoFilter(p, decode, encode)); graph != "" {
		return append(args, "-filter_complex", graph, "-map", output)
	}
	return append(args, "-map", "0:"+strconv.Itoa(p.VideoStreamIndex))
}

func appendHLSVideoFilter(args []string, p Plan, decode, encode string) []string {
	if graph, _, _ := BitmapSubtitleGraph(p, videoFilter(p, decode, encode)); graph != "" {
		return args
	}
	return append(args, "-vf", videoFilter(p, decode, encode))
}

// buildGeneratedHLSArgs produces each ladder entry as an actual independently
// encoded output. All entries read one input clock and force identical GOP cuts.
func buildGeneratedHLSArgs(p Plan, threads int) ([]string, error) {
	threadCount := strconv.Itoa(threads)
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "level+warning", "-y",
		"-progress", "pipe:1", "-stats_period", "0.5", "-filter_threads", threadCount, "-filter_complex_threads", threadCount}
	args, decode, encode := appendHardwareInputArgs(args, p)
	if p.SourceMode == "stream" {
		args = append(args, "-copyts")
	}
	args = append(args, "-threads", threadCount, "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats)
	if p.StartTicks > 0 && !p.AudioSampleSeek {
		args = append(args, "-ss", tickSeconds(p.StartTicks))
	}
	input := "/proc/self/fd/3"
	if p.SourceMode == "stream" {
		input = "pipe:3"
		args = append(args, "-itsoffset", signedTickSeconds(LiveSourceClockBiasTicks(p)))
	}
	args = append(args, "-i", input)
	args = appendBitmapSubtitleInputArgs(args, p, threads)
	count := max(1, p.HLS.RenditionCount)
	for index := 0; index < count; index++ {
		rendition := p
		if p.HLS.RenditionCount > 0 {
			r := p.HLS.Renditions[index]
			rendition.Width, rendition.Height, rendition.VideoBitrate = r.Width, r.Height, r.VideoBitrate
		}
		if p.SourceMode != "stream" && !p.AudioSampleSeek {
			args = append(args, "-t", tickSeconds(p.DurationTicks-p.StartTicks))
		}
		args = append(args, "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn")
		if p.VideoStreamIndex >= 0 {
			before := len(args)
			args = appendHLSVideoMap(args, rendition, decode, encode)
			for position := before; position < len(args); position++ {
				args[position] = strings.ReplaceAll(args[position], "goby_", "goby_v"+strconv.Itoa(index)+"_")
			}
		} else {
			args = append(args, "-vn")
		}
		if p.AudioStreamIndex >= 0 {
			args = append(args, "-map", "0:"+strconv.Itoa(p.AudioStreamIndex))
		} else {
			args = append(args, "-an")
		}
		if p.VideoCodec == "copy" {
			args = append(args, "-c:v", "copy")
		}
		if VideoEncodingSupported(p.VideoCodec) {
			codec := VideoEncoder(p.VideoCodec, encode)
			keyframes := "expr:gte(t,n_forced*" + strconv.Itoa(p.SegmentSeconds) + ")"
			if p.SourceMode == "stream" {
				keyframes = "expr:if(isnan(prev_forced_t),1,gte(t,prev_forced_t+" + strconv.Itoa(p.SegmentSeconds) + "))"
			}
			args = append(args, "-c:v", codec, "-threads:v", threadCount, "-bf", "0", "-force_key_frames", keyframes)
			args = appendHLSVideoFilter(args, rendition, decode, encode)
			if p.FrameRate > 0 {
				args = append(args, "-r", strconv.FormatFloat(p.FrameRate, 'f', -1, 64), "-g", strconv.Itoa(int(math.Ceil(p.FrameRate*float64(p.SegmentSeconds)))))
			}
			args = appendVideoEncoderOptions(args, rendition, encode, threads)
			args = AppendVideoColorArgs(args, p)
			args = appendVideoRateControl(args, rendition, encode)
		}
		if p.AudioCodec == "copy" {
			args = append(args, "-c:a", "copy")
		} else if p.AudioCodec != "" {
			codec := "aac"
			if p.AudioCodec == "mp3" {
				codec = "libmp3lame"
			}
			bitrate, channels, sampleRate := p.AudioBitrate, p.AudioChannels, p.AudioSampleRate
			if bitrate == 0 {
				bitrate = 192000
			}
			if channels == 0 {
				channels = 2
			}
			if sampleRate == 0 {
				sampleRate = 48000
			}
			args = append(args, "-c:a", codec, "-threads:a", threadCount, "-b:a", strconv.FormatInt(bitrate, 10), "-ac", strconv.Itoa(channels), "-ar", strconv.Itoa(sampleRate))
			if p.AudioSampleSeek {
				args = append(args, "-af", audioSampleSeekFilter(p, sampleRate))
			}
			if p.AudioCodec == "aac" {
				args = append(args, "-profile:a", "aac_low")
			}
		}
		prefix := hlsPrefix(index, p.HLS.RenditionCount)
		if needsHLSClock(p) && !needsHLSCopyClock(p) {
			reference := "v:0"
			if p.VideoStreamIndex < 0 {
				reference = "a:0"
			}
			args = append(args, "-stats_mux_pre:"+reference, "pipe:"+strconv.Itoa(4+index), "-stats_mux_pre_fmt:"+reference, "GOBY {fidx} {n} {tb} {pts}")
		}
		if p.SourceMode == "stream" {
			args = appendLiveSegmentOutputArgs(args, p, index)
			continue
		}
		if p.HLS.SegmentType == "packed" {
			format := "adts"
			if p.AudioCodec == "mp3" {
				format = "mp3"
			}
			if p.AudioCodec == "mp3" {
				args = append(args, "-segment_format_options", "write_xing=0:id3v2_version=0:write_id3v1=0")
			}
			args = append(args, "-avoid_negative_ts", "make_zero", "-f", "segment", "-segment_format", format,
				"-segment_time", strconv.Itoa(p.SegmentSeconds), "-reset_timestamps", "0", "-segment_list", "segment-list.m3u8", "-segment_list_type", "m3u8", "segment-%06d."+p.AudioCodec+".tmp")
			continue
		}
		segmentType, extension := "mpegts", "ts"
		if p.HLS.SegmentType == "fmp4" {
			segmentType, extension = "fmp4", "m4s"
			if p.VideoStreamIndex >= 0 {
				args = append(args, "-tag:v", VideoMP4Tag(p))
			}
		}
		flags := "temp_file"
		if VideoEncodingSupported(p.VideoCodec) {
			flags += "+independent_segments"
		}
		args = append(args, "-avoid_negative_ts", "make_zero", "-max_muxing_queue_size", "1024", "-f", "hls", "-hls_segment_type", segmentType,
			"-hls_time", strconv.Itoa(p.SegmentSeconds), "-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_flags", flags, "-start_number", "0")
		if segmentType == "fmp4" {
			args = append(args, "-hls_fmp4_init_filename", prefix+"init.mp4")
		}
		if segmentType == "mpegts" {
			args = append(args, "-hls_segment_options", "mpegts_copyts=1")
		}
		args = append(args, "-hls_segment_filename", prefix+"segment-%06d."+extension, HLSPlaylistName(index, p.HLS.RenditionCount))
	}
	if p.SourceMode == "stream" {
		tracks := PlanHLSSubtitles(p)
		for slot := 0; slot < tracks.Count; slot++ {
			fd := liveSubtitleFD(p, slot)
			if fd < 0 {
				continue
			}
			companion, err := BuildLiveCaptionCompanionArgs(p, slot, fd)
			if err != nil {
				return nil, err
			}
			args = append(args, companion...)
		}
	}
	if needsHLSCopyClock(p) {
		// Stream copy has no encoder-backed stats_mux_pre writer in FFmpeg.
		// Observe only its first real packet in a second output of this same
		// demuxer. Explicitly disable timestamp shifting on that output; the
		// media output's measured shift is the value we need to discover.
		stream, kind := p.VideoStreamIndex, "v"
		if stream < 0 {
			stream, kind = p.AudioStreamIndex, "a"
		}
		args = append(args, "-map", "0:"+strconv.Itoa(stream), "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn", "-c:"+kind, "copy", "-frames:"+kind+":0", "1",
			"-avoid_negative_ts", "disabled", "-flush_packets", "1", "-f", "framehash", "-format_version", "1", "-hash", "sha256", "pipe:4")
	}
	return appendLiveBitmapHeartbeatOutputArgs(args, p), nil
}

func appendLiveSegmentOutputArgs(args []string, p Plan, rendition int) []string {
	format, extension := "mpegts", "ts"
	// The segment muxer opens a new TS muxer for each file, resetting its
	// continuity counters. Signal that reset in each stream's first packet
	// while preserving the uninterrupted presentation clock.
	options := "mpegts_copyts=1:mpegts_flags=+initial_discontinuity"
	if p.HLS.SegmentType == "fmp4" {
		format, extension = "mp4", "m4s"
		// Each closed file is independently verifiable. The publisher separates
		// its stable initialization from complete default-base-is-moof media.
		// frag_discont preserves the continuous native tfdt across these files.
		// Child muxers do not inherit the outer segment muxer's options. Pin
		// the movie time base so the first header and later headers agree, and
		// explicitly disable each child's automatic timestamp rebasing.
		options = "movflags=+empty_moov+frag_keyframe+default_base_moof+skip_trailer+frag_discont:use_editlist=0:movie_timescale=1000:avoid_negative_ts=disabled"
		if p.VideoStreamIndex >= 0 {
			args = append(args, "-tag:v", VideoMP4Tag(p))
		}
	}
	reference := "v:0"
	if p.VideoStreamIndex < 0 {
		reference = "a:0"
	}
	return append(args, "-avoid_negative_ts", "disabled", "-max_muxing_queue_size", "1024", "-f", "segment", "-segment_format", format,
		"-segment_format_options", options, "-reference_stream", reference, "-segment_time", strconv.Itoa(p.SegmentSeconds), "-reset_timestamps", "0",
		// segment converts its initial reference PTS to AV_TIME_BASE before
		// calculating cuts. A rational keyframe can precede that rounded cut by
		// less than one microsecond; do not accidentally skip a whole GOP.
		"-segment_time_delta", "0.000001",
		"-segment_list_size", "0", "-segment_list_type", "csv", "-segment_list", "pipe:"+strconv.Itoa(liveJournalFD(p, rendition)),
		hlsPrefix(rendition, p.HLS.RenditionCount)+"segment-%06d."+extension+".tmp")
}
