package transcode

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	ErrInvalidPlan      = errors.New("invalid transcode plan")
	ErrInvalidThreads   = errors.New("invalid transcode thread limit")
	ErrInvalidInput     = errors.New("invalid transcode input")
	ErrInvalidDirectory = errors.New("invalid transcode output directory")
	ErrStart            = errors.New("transcode process could not start")
	ErrProcess          = errors.New("transcode process failed")
	ErrProgress         = errors.New("invalid transcode progress output")
	ErrUnsupported      = errors.New("transcoding requires Linux")
)

const (
	ticksPerSecond       = int64(10_000_000)
	maxDurationTicks     = 30 * 24 * 60 * 60 * ticksPerSecond
	maxStreamIndex       = 4095
	maxThreads           = 64
	maxSegmentTimesBytes = 64 * 1024
	inputFormats         = "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,avi,asf,flv,ogg," +
		"mp3,flac,wav,aiff,aac,ac3,eac3,dts,truehd,ape,wv,tta,tak,mpc,mpc8,dsf,iff,amr," +
		"mpeg,mpegts,mpegvideo,h264,hevc,av1,ivf,mjpeg"
)

// ValidatePlan checks a closed set of conversion options. It does not probe a
// GPU, validate the source's codec, or perform HDR tone mapping. The planner must
// select compatible, authorized streams and reject unsupported HDR conversions.
func ValidatePlan(p Plan) error {
	invalid := func(field string) error { return fmt.Errorf("%w: %s", ErrInvalidPlan, field) }
	if p.OutputMode == "progressive" {
		return validateProgressivePlan(p)
	}
	if p.OutputMode != "" {
		return invalid("output mode")
	}
	if p.AudioBitDepth != 0 || !p.AudioSampleSeek && (p.AudioSourceSampleRate != 0 || p.AudioSourceSampleCount != 0) {
		return invalid("audio bit depth")
	}
	if p.Container != "ts" && p.Container != "mpegts" {
		return invalid("container")
	}
	if p.DurationTicks <= 0 || p.DurationTicks > maxDurationTicks || p.StartTicks < 0 || p.StartTicks >= p.DurationTicks {
		return invalid("duration or start")
	}
	if p.SegmentSeconds < 1 || p.SegmentSeconds > 10 {
		return invalid("segment duration")
	}
	if _, err := planSegmentTimes(p); err != nil {
		return err
	}
	if p.VideoStreamIndex < -1 || p.VideoStreamIndex > maxStreamIndex || p.AudioStreamIndex < -1 || p.AudioStreamIndex > maxStreamIndex ||
		(p.VideoStreamIndex < 0 && p.AudioStreamIndex < 0) || (p.VideoStreamIndex >= 0 && p.VideoStreamIndex == p.AudioStreamIndex) {
		return invalid("stream indexes")
	}
	if (p.VideoStreamIndex < 0 && p.VideoCodec != "") || (p.VideoStreamIndex >= 0 && p.VideoCodec != "copy" && p.VideoCodec != "h264") {
		return invalid("video codec")
	}
	if (p.AudioStreamIndex < 0 && p.AudioCodec != "") || (p.AudioStreamIndex >= 0 && p.AudioCodec != "copy" && p.AudioCodec != "aac" && p.AudioCodec != "mp3") {
		return invalid("audio codec")
	}
	if p.Width < 0 || p.Width > 8192 || p.Width%2 != 0 || p.Height < 0 || p.Height > 8192 || p.Height%2 != 0 || (p.Width == 0) != (p.Height == 0) {
		return invalid("dimensions")
	}
	if math.IsNaN(p.FrameRate) || math.IsInf(p.FrameRate, 0) || p.FrameRate < 0 || p.FrameRate > 240 || (p.FrameRate > 0 && p.FrameRate < 1) {
		return invalid("frame rate")
	}
	if p.VideoBitrate < 0 || p.VideoBitrate > 200_000_000 || (p.VideoBitrate > 0 && p.VideoBitrate < 64_000) {
		return invalid("video bitrate")
	}
	if p.AudioBitrate < 0 || p.AudioBitrate > 768_000 || (p.AudioBitrate > 0 && p.AudioBitrate < 8_000) || p.AudioChannels < 0 || p.AudioChannels > 8 {
		return invalid("audio bitrate or channels")
	}
	if p.AudioSampleRate != 0 {
		switch p.AudioSampleRate {
		case 8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000:
		default:
			return invalid("audio sample rate")
		}
	}
	if p.AudioCodec == "mp3" && (p.AudioBitrate > 320_000 || p.AudioChannels > 2 || p.AudioSampleRate > 48000) {
		return invalid("MP3 options")
	}
	decode, encode := hardwareSelection(p.Hardware)
	if decode != "software" && decode != "vaapi" && decode != "qsv" && decode != "cuda" {
		return invalid("hardware decoder")
	}
	if encode != "software" && encode != "vaapi" && encode != "qsv" && encode != "nvenc" {
		return invalid("hardware encoder")
	}
	backend := encode
	if backend == "nvenc" {
		backend = "cuda"
	}
	if decode != "software" && backend != "software" && decode != backend {
		return invalid("mixed hardware backends")
	}
	if backend == "software" {
		backend = decode
	}
	if backend == "software" {
		if p.Hardware.Device != "" {
			return invalid("software device")
		}
	} else if !validHardwareDevice(backend, p.Hardware.Device) {
		return invalid("hardware device")
	}
	if p.VideoCodec != "h264" && (p.Width != 0 || p.Height != 0 || p.FrameRate != 0 || p.VideoBitrate != 0 || backend != "software") {
		return invalid("video options require encoding")
	}
	if p.AudioCodec != "aac" && p.AudioCodec != "mp3" && (p.AudioBitrate != 0 || p.AudioChannels != 0 || p.AudioSampleRate != 0) {
		return invalid("audio options require encoding")
	}
	if p.AudioSampleSeek {
		_, sampleRate := progressiveAudioDimensions(p)
		if _, _, _, err := audioSampleSeekWindow(p, sampleRate); err != nil {
			return err
		}
	}
	return nil
}

func hardwareSelection(h Hardware) (decode, encode string) {
	decode, encode = h.Decode, h.Encode
	if decode == "" {
		decode = "software"
	}
	if encode == "" {
		encode = "software"
	}
	return
}

func validHardwareDevice(backend, device string) bool {
	if device == "" {
		return true
	}
	if backend == "cuda" {
		value, err := strconv.Atoi(device)
		return err == nil && value >= 0 && value <= 31 && strconv.Itoa(value) == device
	}
	if !strings.HasPrefix(device, "/dev/dri/renderD") {
		return false
	}
	value, err := strconv.Atoi(strings.TrimPrefix(device, "/dev/dri/renderD"))
	return err == nil && value >= 128 && value <= 255 && device == "/dev/dri/renderD"+strconv.Itoa(value)
}

// BuildArgs constructs arguments, never a shell command. Both media input and
// output names are fixed. Manifest demuxers and network protocols are excluded
// so a media file cannot redirect decoding to an unrelated file or URL.
func BuildArgs(p Plan, threads int) ([]string, error) {
	if err := ValidatePlan(p); err != nil {
		return nil, err
	}
	if threads < 1 || threads > maxThreads {
		return nil, ErrInvalidThreads
	}
	if p.OutputMode == "progressive" {
		return buildProgressiveArgs(p, threads), nil
	}
	threadCount := strconv.Itoa(threads)
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "level+warning", "-y",
		"-progress", "pipe:1", "-stats_period", "0.5", "-filter_threads", threadCount,
		"-filter_complex_threads", threadCount}
	decode, encode := hardwareSelection(p.Hardware)
	backend := encode
	if backend == "nvenc" {
		backend = "cuda"
	}
	if backend == "software" {
		backend = decode
	}
	device := p.Hardware.Device
	if device == "" {
		device = "/dev/dri/renderD128"
		if backend == "cuda" {
			device = "0"
		}
	}
	switch backend {
	case "vaapi":
		args = append(args, "-init_hw_device", "vaapi=goby:"+device, "-filter_hw_device", "goby")
	case "qsv":
		args = append(args, "-init_hw_device", "vaapi=gobyva:"+device, "-init_hw_device", "qsv=goby@gobyva", "-filter_hw_device", "goby")
	case "cuda":
		args = append(args, "-init_hw_device", "cuda=goby:"+device, "-filter_hw_device", "goby")
	}
	if decode != "software" {
		args = append(args, "-hwaccel", decode, "-hwaccel_device", "goby", "-hwaccel_output_format", decode)
	}
	args = append(args, "-threads", threadCount, "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats)
	if p.StartTicks > 0 && !p.AudioSampleSeek {
		args = append(args, "-ss", tickSeconds(p.StartTicks))
	}
	end := p.DurationTicks
	if p.EndTicks > 0 {
		end = p.EndTicks
	}
	args = append(args, "-i", "/proc/self/fd/3")
	if !p.AudioSampleSeek {
		args = append(args, "-t", tickSeconds(end-p.StartTicks))
	}
	args = append(args, "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn")
	if p.VideoStreamIndex >= 0 {
		args = append(args, "-map", "0:"+strconv.Itoa(p.VideoStreamIndex))
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
	} else if p.VideoCodec == "h264" {
		codec := "libx264"
		switch encode {
		case "vaapi", "qsv", "nvenc":
			codec = "h264_" + encode
		}
		forcedFrames := "expr:gte(t,n_forced*" + strconv.Itoa(p.SegmentSeconds) + ")"
		if p.SegmentMode == "vod" {
			forcedFrames = "0"
			cuts, _ := planSegmentTimes(p)
			for _, cut := range cuts {
				forcedFrames += "," + tickSeconds(cut-p.StartTicks)
			}
		}
		args = append(args, "-c:v", codec, "-threads:v", threadCount, "-vf", videoFilter(p, decode, encode), "-bf", "0",
			"-force_key_frames", forcedFrames)
		if p.FrameRate > 0 {
			args = append(args, "-r", strconv.FormatFloat(p.FrameRate, 'f', -1, 64), "-g", strconv.Itoa(int(math.Ceil(p.FrameRate*float64(p.SegmentSeconds)))))
		}
		switch encode {
		case "software":
			args = append(args, "-preset", "veryfast", "-pix_fmt", "yuv420p", "-sc_threshold", "0", "-flags", "+cgop")
		case "qsv":
			args = append(args, "-idr_interval", "0", "-forced_idr", "1")
		case "nvenc":
			args = append(args, "-forced-idr", "1")
		}
		bitrate := p.VideoBitrate
		if bitrate == 0 {
			bitrate = 4_000_000
		}
		args = append(args, "-b:v", strconv.FormatInt(bitrate, 10), "-maxrate", strconv.FormatInt(bitrate, 10), "-bufsize", strconv.FormatInt(bitrate*2, 10))
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
			bitrate = 192_000
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
	if p.SegmentMode == "vod" {
		if p.AudioStreamIndex >= 0 {
			// Demuxer seeking may retain substantial audio pre-roll. Drop only
			// packets before this run's source origin; never use output -ss,
			// which can also discard a keyframe whose decode timestamp is negative.
			args = append(args, "-bsf:a", "noise=drop='lt(pts,0)'")
		}
		args = append(args, "-avoid_negative_ts", "disabled", "-max_muxing_queue_size", "1024", "-f", "segment",
			// Each segment has a new MPEG-TS muxer and continuity-counter
			// origin. Declare that transport reset on every PID's first packet;
			// the HLS manifest separately declares the actual segment boundaries.
			"-segment_format", "mpegts", "-segment_format_options", "mpegts_copyts=1:mpegts_flags=+initial_discontinuity", "-reset_timestamps", "0",
			// Segment scheduling converts the first reference timestamp to
			// integer microseconds. One microsecond prevents that rounding from
			// skipping an otherwise exact cut (notably MP3's 47/48000 origin).
			"-segment_time_delta", "0.000001",
			"-initial_offset", tickSeconds(p.StartTicks+ticksPerSecond), "-segment_start_number", strconv.Itoa(p.SegmentStartNumber),
			"-segment_list", "segment-list.m3u8", "-segment_list_type", "m3u8")
		cuts, _ := planSegmentTimes(p)
		if len(cuts) > 0 {
			times := make([]string, len(cuts))
			for i, cut := range cuts {
				times[i] = tickSeconds(cut - p.StartTicks - p.ReferenceStartTicks)
			}
			args = append(args, "-segment_times", strings.Join(times, ","))
		} else {
			// A single requested segment must not fall back to the muxer's
			// default periodic split, even if it contains many source keyframes.
			args = append(args, "-segment_time", tickSeconds(end-p.StartTicks+ticksPerSecond))
		}
		return append(args, "segment-%06d.ts.tmp"), nil
	}
	// Stream copy retains the source's keyframe spacing. Neither mode claims
	// independent segments without inspecting the encoded result; split_by_time
	// is deliberately absent because it can create undecodable segment starts.
	args = append(args, "-avoid_negative_ts", "make_zero", "-max_muxing_queue_size", "1024", "-f", "hls",
		"-hls_segment_type", "mpegts", "-hls_time", strconv.Itoa(p.SegmentSeconds), "-hls_list_size", "0",
		"-hls_playlist_type", "event", "-hls_flags", "temp_file", "-start_number", "0",
		"-hls_segment_filename", "segment-%06d.ts", "main.m3u8")
	return args, nil
}

func planSegmentTimes(p Plan) ([]int64, error) {
	invalid := func() ([]int64, error) { return nil, fmt.Errorf("%w: segment timeline", ErrInvalidPlan) }
	if p.SegmentMode == "" {
		if p.SegmentStartNumber != 0 || p.EndTicks != 0 || p.SegmentTimes != "" || p.ReferenceStartTicks != 0 {
			return invalid()
		}
		return nil, nil
	}
	end := p.DurationTicks
	if p.EndTicks > 0 {
		end = p.EndTicks
	}
	if p.SegmentMode != "vod" || p.EndTicks < 0 || end > p.DurationTicks || end <= p.StartTicks ||
		p.SegmentStartNumber < 0 || p.SegmentStartNumber >= MaxPlaylistSegments || len(p.SegmentTimes) > maxSegmentTimesBytes ||
		p.ReferenceStartTicks < 0 || p.ReferenceStartTicks >= end-p.StartTicks {
		return invalid()
	}
	if p.SegmentTimes == "" {
		return nil, nil
	}
	parts := strings.Split(p.SegmentTimes, ",")
	if len(parts)+p.SegmentStartNumber >= MaxPlaylistSegments {
		return invalid()
	}
	result := make([]int64, len(parts))
	previous := p.StartTicks + p.ReferenceStartTicks
	for i, part := range parts {
		cut, err := strconv.ParseInt(part, 10, 64)
		if err != nil || strconv.FormatInt(cut, 10) != part || cut <= previous || cut >= end {
			return invalid()
		}
		result[i], previous = cut, cut
	}
	return result, nil
}

func tickSeconds(ticks int64) string {
	return fmt.Sprintf("%d.%07d", ticks/ticksPerSecond, ticks%ticksPerSecond)
}

func videoFilter(p Plan, decode, encode string) string {
	width, height := "trunc(iw/2)*2", "trunc(ih/2)*2"
	if p.Width > 0 {
		width, height = strconv.Itoa(p.Width), strconv.Itoa(p.Height)
	}
	if decode != "software" {
		filter := "scale_" + decode
		if decode == "qsv" {
			filter = "vpp_qsv"
		}
		filter += "=w=" + width + ":h=" + height + ":format=nv12"
		if encode == "software" {
			filter += ",hwdownload,format=nv12,format=yuv420p"
		}
		return filter
	}
	filter := "scale=w=" + width + ":h=" + height
	if encode == "software" {
		return filter + ",format=yuv420p"
	}
	return filter + ",format=nv12,hwupload=extra_hw_frames=64"
}
