package media

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

const (
	VideoCopySeekCandidateVersion  = 1
	MaxVideoCopySeekCandidateBytes = 8 * 1024
	maxVideoCopySeekSourceStreams  = 1024
)

// VideoCopySeekCandidate proposes an exact random-access packet boundary.
// Decoder restart evidence is only a candidate source; it never proves that
// stream copying emits that picture first or preserves the requested clock.
type VideoCopySeekCandidate struct {
	Version                     int                 `json:"version"`
	RequestedStartTicks         int64               `json:"requested_start_ticks"`
	OriginalRequestedStartTicks int64               `json:"original_requested_start_ticks,omitempty"`
	CopyTimestamps              bool                `json:"copy_timestamps,omitempty"`
	QuantizedStart              bool                `json:"quantized_start,omitempty"`
	Index                       VideoSeekIndex      `json:"index"`
	Audio                       *VideoCopySeekAudio `json:"audio,omitempty"`
}

func videoCopySeekSourceCodecSupported(stream Stream) bool {
	profile := strings.ToLower(strings.ReplaceAll(stream.Profile, " ", ""))
	depth := videoCopySeekSourceDepth(stream)
	if stream.Codec != "h264" && (depth == 0 || depth != videoSeekPixelDepth(stream.PixelFormat)) {
		return false
	}
	switch stream.Codec {
	case "h264":
		return true
	case "hevc":
		return profile == "main" && depth == 8 || profile == "main10" && (depth == 8 || depth == 10)
	case "av1":
		return profile == "main" && (depth == 8 || depth == 10)
	default:
		return false
	}
}

func videoCopySeekSourceDepth(stream Stream) int {
	// Share codec-specific reported/decoded facts with playback and GPU planning.
	// Restart eligibility separately requires one of its two bounded decoded
	// representations and rejects contradictory scalar/pixel-format depths.
	return EffectiveVideoBitDepth(stream)
}

// SelectVideoCopySeekCandidateForInfo validates the complete bounded catalog
// evidence before reducing it to one exact copied-packet proposal. Callers may
// retain the reduced candidate during a bounded profile search; stale, mixed,
// or oversized indexes are declined before a copy delivery mode is chosen.
func SelectVideoCopySeekCandidateForInfo(info Info, streamIndex int, requestedTicks int64) (string, error) {
	return SelectVideoCopySeekCandidateForInfoAligned(info, streamIndex, requestedTicks, 0)
}

// SelectVideoCopySeekCandidateForInfoAligned permits explicit backward
// alignment only within the caller's bounded tolerance. Zero means exact.
func SelectVideoCopySeekCandidateForInfoAligned(info Info, streamIndex int, requestedTicks, maxPrerollTicks int64) (string, error) {
	return SelectVideoCopySeekCandidateForStreamsAligned(info, streamIndex, -1, requestedTicks, maxPrerollTicks)
}

// SelectVideoCopySeekCandidateForStreamsAligned selects a shared packet boundary
// when audioStreamIndex identifies copied AAC. A negative audio index of -1
// requires video only. The complete catalog evidence is validated before any
// point is skipped for missing audio, and alignment retains the original request.
func SelectVideoCopySeekCandidateForStreamsAligned(info Info, streamIndex, audioStreamIndex int, requestedTicks, maxPrerollTicks int64) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("video copy seek catalog evidence is inconsistent or unavailable")
	}
	if info.ProbeVersion != CurrentProbeVersion || !info.FormatStartKnown ||
		audioStreamIndex < -1 ||
		len(info.Streams) == 0 || len(info.Streams) > maxVideoCopySeekSourceStreams ||
		len(info.VideoSeekIndexes) == 0 || len(info.VideoSeekIndexes) > maxVideoCopySeekSourceStreams {
		return invalid()
	}
	var video, audio *Stream
	for position := range info.Streams {
		stream := &info.Streams[position]
		if stream.Index == streamIndex {
			if video != nil || stream.CodecType != "video" || !videoCopySeekSourceCodecSupported(*stream) || stream.IsExternal || stream.IsAttachedPicture {
				return invalid()
			}
			video = stream
		}
		if audioStreamIndex >= 0 && stream.Index == audioStreamIndex {
			if audio != nil || stream.CodecType != "audio" || stream.Codec != "aac" || stream.IsExternal || !strings.EqualFold(stream.Profile, "LC") {
				return invalid()
			}
			audio = stream
		}
	}
	if video == nil || audioStreamIndex >= 0 && audio == nil {
		return invalid()
	}
	timeBase, err := parseVideoSeekTimeBase(video.TimeBase)
	if err != nil {
		return invalid()
	}
	entries, bytes := 0, 2
	var selected *VideoSeekIndex
	var sourceIdentity, toolIdentity string
	seen := make(map[int]bool, len(info.VideoSeekIndexes))
	for position := range info.VideoSeekIndexes {
		index := &info.VideoSeekIndexes[position]
		if len(index.Entries) > MaxVideoSeekEntries-entries || seen[index.StreamIndex] ||
			index.DurationTicks != info.DurationTicks || index.FormatStartTicks != info.FormatStartTicks ||
			len(index.PixelFormat) > 64 || len(index.SourceIdentity) != 64 || len(index.ToolIdentity) != 64 {
			return invalid()
		}
		entries += len(index.Entries)
		seen[index.StreamIndex] = true
		if position == 0 {
			sourceIdentity, toolIdentity = index.SourceIdentity, index.ToolIdentity
		} else if sourceIdentity != index.SourceIdentity || toolIdentity != index.ToolIdentity {
			return invalid()
		}
		if ValidateVideoSeekIndex(*index) != nil {
			return invalid()
		}
		encoded, err := json.Marshal(index)
		if err != nil {
			return invalid()
		}
		bytes += len(encoded)
		if position > 0 {
			bytes++
		}
		if bytes > MaxVideoSeekIndexBytes {
			return invalid()
		}
		if index.StreamIndex == video.Index {
			if VideoSeekCodec(*index) != video.Codec || index.Width != video.Width || index.Height != video.Height || index.PixelFormat != strings.ToLower(video.PixelFormat) ||
				timeBase.Cmp(videoSeekTimeBase(index.TimeBaseNumerator, index.TimeBaseDenominator)) != 0 {
				return invalid()
			}
			selected = index
		}
	}
	if selected == nil {
		return invalid()
	}
	return selectVideoCopySeekCandidateAligned(*selected, requestedTicks, maxPrerollTicks, audio)
}

// SelectVideoCopySeekCandidate accepts only an exact source-clock restart whose
// PTS and DTS agree. It never silently hides earlier pictures or decoder preroll.
func SelectVideoCopySeekCandidate(index VideoSeekIndex, requestedTicks int64) (string, error) {
	return SelectVideoCopySeekCandidateAligned(index, requestedTicks, 0)
}

// SelectVideoCopySeekCandidateAligned retains the real source boundary rather
// than pretending that an earlier keyframe satisfies an exact seek request.
func SelectVideoCopySeekCandidateAligned(index VideoSeekIndex, requestedTicks, maxPrerollTicks int64) (string, error) {
	return selectVideoCopySeekCandidateAligned(index, requestedTicks, maxPrerollTicks, nil)
}

func selectVideoCopySeekCandidateAligned(index VideoSeekIndex, requestedTicks, maxPrerollTicks int64, audio *Stream) (string, error) {
	if err := ValidateVideoSeekIndex(index); err != nil {
		return "", err
	}
	if requestedTicks <= 0 || requestedTicks >= index.DurationTicks || maxPrerollTicks < 0 || maxPrerollTicks > 10*TicksPerSecond {
		return "", fmt.Errorf("invalid video copy seek alignment window")
	}
	var audioTimeBase *big.Rat
	if audio != nil {
		var ok bool
		if len(audio.TimeBase) == 0 || len(audio.TimeBase) > 64 {
			return "", fmt.Errorf("video copy seek audio source clock is unavailable")
		}
		audioTimeBase, ok = new(big.Rat).SetString(audio.TimeBase)
		if !ok || audioTimeBase.Sign() <= 0 {
			return "", fmt.Errorf("video copy seek audio source clock is invalid")
		}
	}
	requested := VideoSeekRequestedTime(index, requestedTicks)
	for position := len(index.Entries) - 1; position >= 0; position-- {
		point := index.Entries[position]
		pointTime := VideoSeekPointTime(index, point)
		if point.PTS != point.DTS || pointTime.Cmp(requested) > 0 {
			continue
		}
		aligned, err := videoSeekInputTicks(index, point)
		if err != nil || requestedTicks-aligned > maxPrerollTicks {
			continue
		}
		quantized := pointTime.Cmp(VideoSeekRequestedTime(index, aligned)) != 0
		if quantized && maxPrerollTicks == 0 {
			continue
		}
		var selectedAudio *VideoCopySeekAudio
		if audio != nil {
			for _, proof := range point.Audio {
				if proof.StreamIndex == audio.Index && proof.Codec == audio.Codec && proof.SampleRate == audio.SampleRate && proof.Channels == audio.Channels &&
					audioTimeBase.Cmp(videoSeekTimeBase(proof.TimeBaseNumerator, proof.TimeBaseDenominator)) == 0 {
					selected := proof
					selectedAudio = &selected
					break
				}
			}
			if selectedAudio == nil {
				continue
			}
		}
		index.Entries = []VideoSeekPoint{point}
		candidate := VideoCopySeekCandidate{Version: VideoCopySeekCandidateVersion, RequestedStartTicks: aligned, Index: index, Audio: selectedAudio}
		if quantized {
			// The exact native point remains authoritative. Only the public
			// 100 ns position is rounded down, by strictly less than one tick.
			// This is safe only when the output retains the source packet clock.
			candidate.QuantizedStart, candidate.CopyTimestamps = true, true
		}
		if aligned != requestedTicks {
			candidate.OriginalRequestedStartTicks = requestedTicks
		}
		if err := validateVideoCopySeekCandidate(candidate); err != nil {
			return "", err
		}
		data, err := json.Marshal(candidate)
		if err != nil || len(data) > MaxVideoCopySeekCandidateBytes {
			return "", fmt.Errorf("video copy seek candidate exceeds its byte budget")
		}
		return string(data), nil
	}
	return "", fmt.Errorf("video copy seek has no exact IDR boundary without decode pre-roll")
}

// ValidateVideoCopySeekCandidate checks bounded canonical private preparation
// data. A valid candidate still requires fresh copied-packet verification.
func ValidateVideoCopySeekCandidate(encoded string) (VideoCopySeekCandidate, error) {
	if len(encoded) == 0 || len(encoded) > MaxVideoCopySeekCandidateBytes {
		return VideoCopySeekCandidate{}, fmt.Errorf("video copy seek candidate exceeds its byte budget")
	}
	var candidate VideoCopySeekCandidate
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return VideoCopySeekCandidate{}, fmt.Errorf("decode video copy seek candidate: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return VideoCopySeekCandidate{}, fmt.Errorf("video copy seek candidate has trailing data")
	}
	if err := validateVideoCopySeekCandidate(candidate); err != nil {
		return VideoCopySeekCandidate{}, err
	}
	canonical, err := json.Marshal(candidate)
	if err != nil || string(canonical) != encoded {
		return VideoCopySeekCandidate{}, fmt.Errorf("video copy seek candidate is not canonical")
	}
	return candidate, nil
}

func validateVideoCopySeekCandidate(candidate VideoCopySeekCandidate) error {
	index := candidate.Index
	if candidate.Version != VideoCopySeekCandidateVersion || candidate.RequestedStartTicks <= 0 ||
		candidate.RequestedStartTicks >= index.DurationTicks || len(index.Entries) != 1 ||
		index.FormatStartTicks < -MaxVideoSeekDurationTicks || index.FormatStartTicks > MaxVideoSeekDurationTicks {
		return fmt.Errorf("invalid video copy seek candidate metadata")
	}
	if candidate.OriginalRequestedStartTicks != 0 && (candidate.OriginalRequestedStartTicks <= candidate.RequestedStartTicks ||
		candidate.OriginalRequestedStartTicks >= index.DurationTicks || candidate.OriginalRequestedStartTicks-candidate.RequestedStartTicks > 10*TicksPerSecond) {
		return fmt.Errorf("invalid video copy seek original request")
	}
	if err := ValidateVideoSeekIndex(index); err != nil {
		return err
	}
	point := index.Entries[0]
	pointTime := VideoSeekPointTime(index, point)
	quantization := new(big.Rat).Sub(pointTime, VideoSeekRequestedTime(index, candidate.RequestedStartTicks))
	if point.PTS != point.DTS || !candidate.QuantizedStart && quantization.Sign() != 0 || candidate.QuantizedStart &&
		(!candidate.CopyTimestamps || candidate.OriginalRequestedStartTicks == 0 || quantization.Sign() <= 0 || quantization.Cmp(big.NewRat(1, TicksPerSecond)) >= 0) {
		return fmt.Errorf("video copy seek is not an exact IDR boundary without decode pre-roll")
	}
	if candidate.Audio != nil {
		matched := false
		for _, audio := range point.Audio {
			if audio == *candidate.Audio {
				matched = true
			}
		}
		if !matched || validateVideoCopySeekAudio(*candidate.Audio) != nil {
			return fmt.Errorf("video copy seek has no matching audio packet evidence")
		}
	}
	if candidate.CopyTimestamps {
		if _, err := videoCopySeekOutputTimestamp(candidate, videoSeekTimeBase(index.TimeBaseNumerator, index.TimeBaseDenominator)); err != nil {
			return err
		}
		if candidate.Audio != nil {
			if _, err := videoCopySeekOutputTimestamp(candidate, videoSeekTimeBase(candidate.Audio.TimeBaseNumerator, candidate.Audio.TimeBaseDenominator)); err != nil {
				return err
			}
		}
	}
	return nil
}

func videoCopySeekOutputTimestamp(candidate VideoCopySeekCandidate, timeBase *big.Rat) (int64, error) {
	if !candidate.CopyTimestamps {
		return 0, nil
	}
	if candidate.QuantizedStart {
		// Native input packets, rather than their rounded public start ticks,
		// define this output clock. The output trim and timestamp restoration
		// cancel each other; only the common input origin remains removed.
		// That origin must itself rescale exactly. Allowing an additional
		// half-native-tick rounding here could move the actual output clock by
		// much more than the permitted sub-100 ns public-position quantization.
		native := new(big.Rat).Quo(VideoSeekPointTime(candidate.Index, candidate.Index.Entries[0]), timeBase)
		if !native.IsInt() {
			return 0, fmt.Errorf("video copy seek source clock is not exactly representable")
		}
		offset := new(big.Rat).Quo(new(big.Rat).SetFrac(new(big.Int).Neg(big.NewInt(candidate.Index.FormatStartTicks)), big.NewInt(TicksPerSecond)), timeBase)
		if !offset.IsInt() {
			return 0, fmt.Errorf("video copy seek source origin requires native clock rounding")
		}
		result := new(big.Int).Add(native.Num(), offset.Num())
		if !result.IsInt64() {
			return 0, fmt.Errorf("video copy seek native output clock overflows")
		}
		return result.Int64(), nil
	}
	timestamp := new(big.Rat).Quo(new(big.Rat).SetFrac(big.NewInt(candidate.RequestedStartTicks), big.NewInt(TicksPerSecond)), timeBase)
	if !timestamp.IsInt() || !timestamp.Num().IsInt64() {
		return 0, fmt.Errorf("video copy seek output clock is not exactly representable")
	}
	return timestamp.Num().Int64(), nil
}

// BuildVideoCopySeekCommandArgs mirrors the production video input and output
// seek arguments. The first branch exposes the actual first copied packet;
// codec-specific branches prove IDR contents or decoded AV1/HEVC pixels. Copied
// audio uses its own single-packet proof: a per-stream frame limit can end the
// entire FFmpeg output before a different branch has flushed its first frame.
func BuildVideoCopySeekCommandArgs(encoded string, threads int) ([]string, error) {
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil {
		return nil, err
	}
	if threads < 1 || threads > MaxVideoSeekDecoderThreads {
		return nil, fmt.Errorf("invalid video copy seek thread count")
	}
	index := candidate.Index
	seconds := func(ticks *big.Int) string {
		return new(big.Rat).SetFrac(ticks, big.NewInt(TicksPerSecond)).FloatString(7)
	}
	absolute := new(big.Int).Add(big.NewInt(index.FormatStartTicks), big.NewInt(candidate.RequestedStartTicks))
	args := []string{"-v", "level+warning", "-nostdin", "-max_alloc", strconv.Itoa(MaxVideoSeekAllocationBytes),
		"-copyts", "-threads", strconv.Itoa(threads), "-max_pixels", strconv.FormatInt(MaxVideoSeekPixels, 10), "-filter_threads", "1", "-filter_complex_threads", "1",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-seek_timestamp", "1", "-noaccurate_seek", "-ss", seconds(absolute),
		"-itsoffset", seconds(new(big.Int).Neg(big.NewInt(index.FormatStartTicks))), "-i", "/proc/self/fd/3",
		"-ss", seconds(big.NewInt(candidate.RequestedStartTicks)),
		"-t", seconds(big.NewInt(index.DurationTicks - candidate.RequestedStartTicks)),
		"-map", "0:" + strconv.Itoa(index.StreamIndex), "-map", "0:" + strconv.Itoa(index.StreamIndex),
		"-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn",
		"-c:v:0", "copy"}
	switch VideoSeekCodec(index) {
	case "h264":
		args = append(args, "-c:v:1", "copy", "-bsf:v:1", "filter_units=pass_types=5")
	case "hevc":
		args = append(args, "-c:v:1", "copy", "-bsf:v:1", "filter_units=pass_types=19|20", "-map", "0:"+strconv.Itoa(index.StreamIndex),
			"-c:v:2", "rawvideo", "-threads:v:2", "1", "-fps_mode:v:2", "passthrough", "-enc_time_base:v:2", "demux", "-frames:v:2", "1")
	case "av1":
		args = append(args, "-c:v:1", "rawvideo", "-threads:v:1", "1", "-fps_mode:v:1", "passthrough", "-enc_time_base:v:1", "demux")
	}
	args = append(args, "-frames:v:0", "1", "-frames:v:1", "1")
	if candidate.CopyTimestamps {
		args = append(args, "-output_ts_offset", seconds(big.NewInt(candidate.RequestedStartTicks)))
	}
	return append(args, "-avoid_negative_ts", "disabled", "-f", "framehash", "-hash", "sha256", "pipe:1"), nil
}

// BuildVideoCopySeekAudioCommandArgs uses the identical common input, output
// trim, and timestamp-restoration arguments as the video proof. Splitting the
// two outputs preserves a strict single-packet audio proof without depending on
// which interleaved stream happens to flush its first packet first.
func BuildVideoCopySeekAudioCommandArgs(encoded string, threads int) ([]string, error) {
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil || candidate.Audio == nil {
		return nil, fmt.Errorf("video copy seek has no copied audio candidate")
	}
	videoArgs, err := BuildVideoCopySeekCommandArgs(encoded, threads)
	if err != nil {
		return nil, err
	}
	boundary := -1
	for position, argument := range videoArgs {
		if argument == "-map" {
			boundary = position
			break
		}
	}
	if boundary < 0 {
		return nil, fmt.Errorf("video copy seek input arguments are unavailable")
	}
	args := append([]string(nil), videoArgs[:boundary]...)
	args = append(args, "-map", "0:"+strconv.Itoa(candidate.Audio.StreamIndex), "-map_metadata", "-1", "-map_chapters", "-1",
		"-vn", "-sn", "-dn", "-c:a", "copy", "-frames:a", "1")
	if candidate.CopyTimestamps {
		args = append(args, "-output_ts_offset", new(big.Rat).SetFrac(big.NewInt(candidate.RequestedStartTicks), big.NewInt(TicksPerSecond)).FloatString(7))
	}
	return append(args, "-avoid_negative_ts", "disabled", "-f", "framehash", "-hash", "sha256", "pipe:1"), nil
}
