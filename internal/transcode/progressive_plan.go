package transcode

import (
	"fmt"
	"strconv"
)

// ProgressiveContainerSupportsCodec describes the deliberately closed audio
// output matrix. Copy callers must check their probed source codec and framing;
// the word "copy" is not evidence that arbitrary media fits a target container.
func ProgressiveContainerSupportsCodec(container, codec string) bool {
	switch container {
	case "mp3":
		return codec == "mp3"
	case "aac", "m4a":
		return codec == "aac"
	case "flac":
		return codec == "flac"
	case "ogg":
		return codec == "vorbis" || codec == "opus" || codec == "flac"
	case "wav":
		return codec == "pcm_s16le"
	}
	return false
}

func validateProgressivePlan(p Plan) error {
	invalid := func(field string) error { return fmt.Errorf("%w: progressive %s", ErrInvalidPlan, field) }
	if p.OutputMode != "progressive" || p.VideoStreamIndex != -1 || p.VideoCodec != "" || p.AudioStreamIndex < 0 || p.AudioStreamIndex > maxStreamIndex {
		return invalid("streams")
	}
	if p.DurationTicks <= 0 || p.DurationTicks > maxDurationTicks || p.StartTicks < 0 || p.StartTicks >= p.DurationTicks {
		return invalid("duration or start")
	}
	if p.AudioSourceSampleRate < 0 || p.AudioSourceSampleRate > 768000 || p.Container == "wav" && p.AudioSourceSampleRate == 0 {
		return invalid("source sample rate")
	}
	_, timingRate := progressiveAudioDimensions(p)
	if p.AudioCodec == "copy" && p.AudioSourceSampleRate > 0 {
		timingRate = p.AudioSourceSampleRate
	}
	if _, err := ProgressiveOutputSamples(p, timingRate); err != nil {
		return err
	}
	if p.AudioSampleSeek {
		if _, _, _, err := audioSampleSeekWindow(p, timingRate); err != nil {
			return err
		}
	}
	if p.SegmentSeconds != 0 || p.SegmentMode != "" || p.SegmentStartNumber != 0 || p.EndTicks != 0 || p.SegmentTimes != "" || p.ReferenceStartTicks != 0 {
		return invalid("HLS options")
	}
	decode, encode := hardwareSelection(p.Hardware)
	if p.Width != 0 || p.Height != 0 || p.FrameRate != 0 || p.VideoBitrate != 0 || decode != "software" || encode != "software" || p.Hardware.Device != "" {
		return invalid("video options")
	}
	switch p.Container {
	case "mp3", "aac", "flac", "ogg", "wav", "m4a":
	default:
		return invalid("container")
	}
	if p.AudioCodec == "copy" {
		if p.AudioBitrate != 0 || p.AudioChannels != 0 || p.AudioSampleRate != 0 || p.AudioBitDepth != 0 {
			return invalid("copy options")
		}
		// A clipped FLAC copy retains the original STREAMINFO sample count and
		// MD5, which cannot be corrected after a non-seekable header is sent.
		if p.Container == "flac" && p.StartTicks != 0 {
			return invalid("FLAC copy seek")
		}
		if p.Container == "wav" {
			if p.AudioStreamIndex != 0 {
				return invalid("native WAV copy stream")
			}
			samples, _ := ProgressiveOutputSamples(p, p.AudioSourceSampleRate)
			if _, _, err := progressiveWAVHeader(progressivePCMFormat(1, p.AudioSourceSampleRate), samples); err != nil {
				return err
			}
		}
		return nil
	}
	if !ProgressiveContainerSupportsCodec(p.Container, p.AudioCodec) {
		return invalid("codec/container combination")
	}
	switch p.AudioCodec {
	case "flac":
		if p.AudioBitDepth != 16 && p.AudioBitDepth != 24 {
			return invalid("FLAC bit depth")
		}
	case "pcm_s16le":
		if p.AudioBitDepth != 0 && p.AudioBitDepth != 16 {
			return invalid("PCM bit depth")
		}
	default:
		if p.AudioBitDepth != 0 {
			return invalid("audio bit depth")
		}
	}
	if p.AudioChannels < 0 || p.AudioChannels > 8 || p.AudioSampleRate < 0 {
		return invalid("audio dimensions")
	}
	if p.AudioSampleRate != 0 {
		switch p.AudioSampleRate {
		case 8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000:
		default:
			return invalid("sample rate")
		}
	}
	if p.AudioCodec == "flac" || p.AudioCodec == "pcm_s16le" {
		if p.AudioBitrate != 0 {
			return invalid("lossless bitrate control")
		}
	} else if p.AudioBitrate < 0 || p.AudioBitrate > 768000 || p.AudioBitrate > 0 && p.AudioBitrate < 8000 {
		return invalid("audio bitrate")
	}
	if p.AudioCodec == "mp3" && (p.AudioChannels > 2 || p.AudioSampleRate > 48000 || p.AudioBitrate > 320000) {
		return invalid("MP3 options")
	}
	channels, rate := progressiveAudioDimensions(p)
	if p.Container == "wav" {
		samples, _ := ProgressiveOutputSamples(p, rate)
		if _, _, err := progressiveWAVHeader(progressivePCMFormat(channels, rate), samples); err != nil {
			return err
		}
	}
	if p.AudioCodec == "aac" && (p.Container == "aac" && channels > 6 || p.AudioBitrate > 6*int64(rate)*int64(channels)) {
		return invalid("AAC options")
	}
	if p.AudioCodec == "mp3" && p.AudioBitrate > 0 && !validProgressiveMP3Bitrate(rate, p.AudioBitrate) {
		return invalid("MP3 CBR bitrate")
	}
	if p.AudioCodec == "vorbis" {
		low, high := progressiveVorbisBitrates(rate, channels)
		if low == 0 || p.AudioBitrate > 0 && (p.AudioBitrate < low || p.AudioBitrate > high) {
			return invalid("Vorbis ABR options")
		}
	}
	if p.AudioCodec == "opus" && p.AudioSampleRate != 0 && p.AudioSampleRate != 48000 {
		return invalid("Opus sample rate")
	}
	if p.AudioCodec == "opus" && p.AudioBitrate > 256000*int64(channels) {
		return invalid("Opus bitrate")
	}
	return nil
}

func buildProgressiveArgs(p Plan, threads int) []string {
	threadCount := strconv.Itoa(threads)
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "level+warning", "-y", "-progress", "pipe:1", "-stats_period", "0.5",
		"-filter_threads", threadCount, "-filter_complex_threads", threadCount, "-threads", threadCount, "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats}
	if seek := progressiveSampleSeekTicks(p); seek > 0 && !p.AudioSampleSeek {
		args = append(args, "-ss", tickSeconds(seek))
	}
	args = append(args, "-i", "/proc/self/fd/3")
	if !p.AudioSampleSeek {
		args = append(args, "-t", tickSeconds(p.DurationTicks-p.StartTicks))
	}
	args = append(args, "-map", "0:"+strconv.Itoa(p.AudioStreamIndex), "-vn", "-sn", "-dn", "-map_metadata", "-1", "-map_metadata:s:a", "-1", "-map_chapters", "-1", "-metadata_header_padding", "0")
	codec := p.AudioCodec
	switch codec {
	case "mp3":
		codec = "libmp3lame"
	case "vorbis":
		codec = "libvorbis"
	case "opus":
		codec = "libopus"
	}
	args = append(args, "-c:a", codec)
	if p.AudioCodec != "copy" {
		channels, rate := progressiveAudioDimensions(p)
		layout := []string{"", "mono", "stereo", "3.0", "quad", "5.0", "5.1", "6.1", "7.1"}[channels]
		if p.AudioCodec == "aac" && channels == 4 {
			layout = "4.0"
		}
		args = append(args, "-threads:a", threadCount, "-ac", strconv.Itoa(channels), "-ar", strconv.Itoa(rate), "-channel_layout", layout)
		samples, _ := ProgressiveOutputSamples(p, rate)
		filter := "aresample=" + strconv.Itoa(rate) + ",atrim=end_sample=" + strconv.FormatInt(samples, 10) + ",asetpts=N/SR/TB"
		if p.AudioSampleSeek {
			filter = audioSampleSeekFilter(p, rate)
		}
		args = append(args, "-af", filter)
		if p.AudioCodec != "flac" && p.AudioCodec != "pcm_s16le" {
			bitrate := p.AudioBitrate
			if bitrate == 0 {
				bitrate = 192000
				switch p.AudioCodec {
				case "aac":
					bitrate = min(bitrate, 6*int64(rate)*int64(channels))
				case "mp3":
					for bitrate > 8000 && !validProgressiveMP3Bitrate(rate, bitrate) {
						bitrate -= 1000
					}
				case "vorbis":
					low, high := progressiveVorbisBitrates(rate, channels)
					bitrate = max(low, min(high, bitrate))
				}
			}
			args = append(args, "-b:a", strconv.FormatInt(bitrate, 10))
		}
		switch p.AudioCodec {
		case "aac":
			args = append(args, "-profile:a", "aac_low")
		case "flac":
			format := "s16"
			if p.AudioBitDepth == 24 {
				format = "s32"
			}
			args = append(args, "-sample_fmt", format, "-bits_per_raw_sample", strconv.Itoa(p.AudioBitDepth), "-frame_size", "4096")
		case "opus":
			family := "0"
			if channels > 2 {
				family = "1"
			}
			args = append(args, "-vbr", "off", "-application", "audio", "-mapping_family", family, "-frame_duration", "20")
		}
	}
	args = append(args, "-avoid_negative_ts", "make_zero", "-flush_packets", "1")
	bitstreamFilters := ""
	if p.AudioCodec == "copy" && p.StartTicks > 0 && p.Container != "wav" {
		bitstreamFilters = "noise=drop='lt(pts,0)'"
	}
	switch p.Container {
	case "mp3":
		args = append(args, "-f", "mp3", "-write_xing", "0", "-id3v2_version", "0", "-write_id3v1", "0")
	case "aac":
		args = append(args, "-f", "adts", "-write_id3v2", "0", "-write_apetag", "0")
	case "flac":
		args = append(args, "-f", "flac", "-write_header", "1")
	case "ogg":
		args = append(args, "-f", "ogg", "-page_duration", "200000")
	case "wav":
		// The parent writes an exact RIFF header before the child starts.
		// A non-seekable WAV muxer would leave an inaccurate 0xffffffff size.
		args = append(args, "-f", "s16le")
	case "m4a":
		args = append(args, "-f", "ipod", "-movflags", "+empty_moov+delay_moov+default_base_moof+skip_trailer", "-frag_duration", "1000000")
		if p.AudioCodec == "copy" {
			if bitstreamFilters != "" {
				bitstreamFilters += ","
			}
			bitstreamFilters += "aac_adtstoasc"
		}
	}
	if bitstreamFilters != "" {
		args = append(args, "-bsf:a", bitstreamFilters)
	}
	return append(args, "pipe:4")
}

func progressiveAudioDimensions(p Plan) (int, int) {
	channels, rate := p.AudioChannels, p.AudioSampleRate
	if channels == 0 {
		channels = 2
	}
	if rate == 0 {
		rate = 48000
	}
	return channels, rate
}

func validProgressiveMP3Bitrate(rate int, bitrate int64) bool {
	var values []int64
	switch {
	case rate < 16000:
		values = []int64{8, 16, 24, 32, 40, 48, 56, 64}
	case rate < 32000:
		values = []int64{8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	default:
		values = []int64{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	}
	for _, value := range values {
		if bitrate == value*1000 {
			return true
		}
	}
	return false
}

func progressiveVorbisBitrates(rate, channels int) (int64, int64) {
	var low, high int64
	switch rate {
	case 8000:
		low, high = 8000, 32000
	case 11025, 12000:
		low, high = 12000, 44000
	case 16000, 22050, 24000:
		low, high = 16000, 86000
	case 32000:
		low, high = 30000, 190000
	case 44100, 48000:
		low, high = 32000, 240000
	}
	return low * int64(channels), min(high*int64(channels), 768000)
}
