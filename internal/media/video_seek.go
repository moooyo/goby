package media

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

const (
	VideoSeekIndexVersion             = 1
	MaxVideoSeekEntries               = 8192
	MaxVideoSeekIndexBytes            = 2 * 1024 * 1024
	MaxVideoSeekPixels          int64 = 4096 * 2160
	MaxVideoSeekDimension             = 32768
	MaxVideoSeekDurationTicks   int64 = 30 * 24 * 60 * 60 * TicksPerSecond
	MaxVideoSeekAllocationBytes       = 64 * 1024 * 1024
	MaxVideoSeekDecoderThreads        = 64
)

// VideoSeekIndex records paired compressed restart packets and decoded images
// on the source clock. H.264/HEVC use IDR and NAL-scope checks; AV1 additionally
// checks the complete key packet's sequence/frame syntax. All codecs require
// stable configuration and packet-side-data evidence. This private metadata
// does not authorize a seek alone.
type VideoSeekIndex struct {
	Version               int              `json:"version"`
	Codec                 string           `json:"codec,omitempty"`
	PacketRestartChecked  bool             `json:"packet_restart_checked,omitempty"`
	StreamIndex           int              `json:"stream_index"`
	FormatStartTicks      int64            `json:"format_start_ticks"`
	DurationTicks         int64            `json:"duration_ticks"`
	TimeBaseNumerator     int64            `json:"time_base_numerator"`
	TimeBaseDenominator   int64            `json:"time_base_denominator"`
	SourceIdentity        string           `json:"source_identity"`
	ToolIdentity          string           `json:"tool_identity"`
	ParameterSetsSHA256   string           `json:"parameter_sets_sha256"`
	PacketSideDataChecked bool             `json:"packet_side_data_checked"`
	NALScopeChecked       bool             `json:"nal_scope_checked"`
	Width                 int              `json:"width"`
	Height                int              `json:"height"`
	PixelFormat           string           `json:"pixel_format,omitempty"`
	DecodedFrameBytes     int64            `json:"decoded_frame_bytes"`
	Entries               []VideoSeekPoint `json:"entries"`
}

// VideoSeekPoint retains original demuxer timestamps without tick rounding.
type VideoSeekPoint struct {
	PTS           int64                `json:"pts"`
	DTS           int64                `json:"dts"`
	CodedSHA256   string               `json:"coded_sha256"`
	DecodedSHA256 string               `json:"decoded_sha256"`
	PacketSHA256  string               `json:"packet_sha256,omitempty"`
	Audio         []VideoCopySeekAudio `json:"audio,omitempty"`
}

// VideoSeekCodec resolves legacy H.264 indexes without changing their wire form.
func VideoSeekCodec(index VideoSeekIndex) string {
	if index.Codec == "" {
		return "h264"
	}
	return index.Codec
}

func videoSeekSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// ValidateVideoSeekIndex validates bounded evidence without opening a source.
func ValidateVideoSeekIndex(index VideoSeekIndex) error {
	frameBytes, supported := videoSeekFrameBytes(index.Width, index.Height, index.PixelFormat)
	codec := VideoSeekCodec(index)
	parameterEvidence := videoSeekSHA256(index.ParameterSetsSHA256) && index.NALScopeChecked
	if codec == "av1" {
		parameterEvidence = index.PacketRestartChecked && videoSeekSHA256(index.ParameterSetsSHA256) && !index.NALScopeChecked
	}
	if index.Version != VideoSeekIndexVersion || index.StreamIndex < 0 ||
		(codec != "h264" && codec != "hevc" && codec != "av1") ||
		index.DurationTicks <= 0 || index.DurationTicks > MaxVideoSeekDurationTicks || index.TimeBaseNumerator <= 0 || index.TimeBaseDenominator <= 0 ||
		!supported || index.DecodedFrameBytes != frameBytes ||
		len(index.PixelFormat) > 64 || !videoSeekSHA256(index.SourceIdentity) || !videoSeekSHA256(index.ToolIdentity) || !parameterEvidence || !index.PacketSideDataChecked ||
		len(index.Entries) == 0 || len(index.Entries) > MaxVideoSeekEntries {
		return fmt.Errorf("invalid video seek index metadata")
	}
	for position, entry := range index.Entries {
		if entry.PTS == -1<<63 || entry.DTS == -1<<63 || !videoSeekSHA256(entry.CodedSHA256) || !videoSeekSHA256(entry.DecodedSHA256) {
			return fmt.Errorf("invalid video seek entry")
		}
		if entry.PacketSHA256 != "" && !videoSeekSHA256(entry.PacketSHA256) || codec == "av1" && entry.PacketSHA256 != entry.CodedSHA256 {
			return fmt.Errorf("invalid video seek packet identity")
		}
		if len(entry.Audio) > 32 {
			return fmt.Errorf("video seek audio exceeds its stream budget")
		}
		seenAudio := make(map[int]bool, len(entry.Audio))
		for _, audio := range entry.Audio {
			if validateVideoCopySeekAudio(audio) != nil || seenAudio[audio.StreamIndex] {
				return fmt.Errorf("invalid video seek audio packet evidence")
			}
			audioTime := new(big.Rat).Mul(new(big.Rat).SetInt64(audio.PTS), videoSeekTimeBase(audio.TimeBaseNumerator, audio.TimeBaseDenominator))
			if audioTime.Cmp(VideoSeekPointTime(index, entry)) != 0 {
				return fmt.Errorf("video seek audio packet is not on the video restart clock")
			}
			seenAudio[audio.StreamIndex] = true
		}
		if position > 0 && (entry.PTS <= index.Entries[position-1].PTS || entry.DTS <= index.Entries[position-1].DTS) {
			return fmt.Errorf("video seek entries are not strictly ordered")
		}
	}
	data, err := json.Marshal(index)
	if err != nil || len(data) > MaxVideoSeekIndexBytes {
		return fmt.Errorf("video seek index exceeds its byte budget")
	}
	return nil
}

func videoSeekPixelDepth(pixelFormat string) int {
	switch pixelFormat {
	case "yuv420p":
		return 8
	case "yuv420p10le":
		return 10
	default:
		return 0
	}
}

func videoSeekFrameBytes(width, height int, pixelFormat string) (int64, bool) {
	depth := videoSeekPixelDepth(pixelFormat)
	if depth == 0 || width <= 0 || height <= 0 || width > MaxVideoSeekDimension || height > MaxVideoSeekDimension ||
		int64(width)*int64(height) > MaxVideoSeekPixels {
		return 0, false
	}
	// Raw planar 4:2:0 stores each chroma plane at rounded-up half dimensions.
	samples := int64(width)*int64(height) + 2*int64((width+1)/2)*int64((height+1)/2)
	if depth == 10 {
		samples *= 2
	}
	return samples, true
}

// ParseVideoSeekIndex rejects unknown fields, trailing input, and excess bytes.
func ParseVideoSeekIndex(data []byte) (VideoSeekIndex, error) {
	if len(data) == 0 || len(data) > MaxVideoSeekIndexBytes {
		return VideoSeekIndex{}, fmt.Errorf("video seek index exceeds its byte budget")
	}
	var index VideoSeekIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil {
		return VideoSeekIndex{}, fmt.Errorf("decode video seek index: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return VideoSeekIndex{}, fmt.Errorf("video seek index has trailing data")
	}
	if err := ValidateVideoSeekIndex(index); err != nil {
		return VideoSeekIndex{}, err
	}
	return index, nil
}

func videoSeekTimeBase(numerator, denominator int64) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(numerator), big.NewInt(denominator))
}

func parseVideoSeekTimeBase(value string) (*big.Rat, error) {
	if len(value) == 0 || len(value) > 64 {
		return nil, fmt.Errorf("invalid video seek time base")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid video seek time base")
	}
	numerator, numeratorErr := strconv.ParseInt(parts[0], 10, 64)
	denominator, denominatorErr := strconv.ParseInt(parts[1], 10, 64)
	if numeratorErr != nil || denominatorErr != nil || numerator <= 0 || denominator <= 0 {
		return nil, fmt.Errorf("invalid video seek time base")
	}
	return videoSeekTimeBase(numerator, denominator), nil
}

// BuildVideoSeekCommandArgs shares the hash representation between sequential
// indexing and a one-picture preflight. Preflight uses normal input decoding so
// demuxer delay discovery matches playback. A candidate is an absolute decimal
// source timestamp, not a decoded landing point.
func BuildVideoSeekCommandArgs(streamIndex int, candidateSeconds *string, decoderThreads int) ([]string, error) {
	return BuildVideoSeekCommandArgsForCodec(streamIndex, "h264", candidateSeconds, decoderThreads)
}

// BuildVideoSeekCommandArgsForCodec preserves codec-specific restart evidence.
func BuildVideoSeekCommandArgsForCodec(streamIndex int, codec string, candidateSeconds *string, decoderThreads int) ([]string, error) {
	if codec != "h264" && codec != "hevc" {
		return nil, fmt.Errorf("unsupported NAL restart codec")
	}
	if streamIndex < 0 || decoderThreads < 1 || decoderThreads > MaxVideoSeekDecoderThreads || candidateSeconds == nil && decoderThreads != 1 {
		return nil, fmt.Errorf("invalid video seek stream or decoder thread count")
	}
	// These constrain decoded display dimensions and individual allocations.
	// They are not an aggregate process RSS limit. Apply them before decoding,
	// including when a later parameter set declares larger dimensions.
	args := []string{"-v", "level+warning", "-nostdin", "-max_alloc", strconv.Itoa(MaxVideoSeekAllocationBytes),
		"-copyts", "-threads", strconv.Itoa(decoderThreads), "-max_pixels", strconv.FormatInt(MaxVideoSeekPixels, 10),
		"-filter_threads", "1", "-filter_complex_threads", "1"}
	if candidateSeconds == nil {
		args = append(args, "-skip_frame", "nokey")
	}
	if candidateSeconds != nil {
		value := *candidateSeconds
		if len(value) == 0 || len(value) > 64 {
			return nil, fmt.Errorf("invalid video seek timestamp")
		}
		digits, dots := 0, 0
		for position, character := range value {
			switch {
			case character >= '0' && character <= '9':
				digits++
			case character == '.':
				dots++
			case position == 0 && (character == '+' || character == '-'):
			default:
				return nil, fmt.Errorf("invalid video seek timestamp")
			}
		}
		if digits == 0 || dots > 1 {
			return nil, fmt.Errorf("invalid video seek timestamp")
		}
		if _, ok := new(big.Rat).SetString(value); !ok {
			return nil, fmt.Errorf("invalid video seek timestamp")
		}
		args = append(args, "-seek_timestamp", "1", "-noaccurate_seek", "-ss", value)
	}
	args = append(args, "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3", "-map", "0:"+strconv.Itoa(streamIndex), "-map", "0:"+strconv.Itoa(streamIndex), "-map", "0:"+strconv.Itoa(streamIndex))
	if candidateSeconds == nil {
		args = append(args, "-map", "0:"+strconv.Itoa(streamIndex), "-map", "0:"+strconv.Itoa(streamIndex))
	}
	restartFilter, parameterFilter, scopeFilter := "filter_units=pass_types=5", "h264_mp4toannexb,filter_units=pass_types=7|8", "filter_units=remove_types=1|5|6|7|8|9|10|11|12"
	if codec == "hevc" {
		restartFilter = "filter_units=pass_types=19|20"
		parameterFilter = "hevc_mp4toannexb,filter_units=pass_types=32|33|34"
		scopeFilter = "filter_units=remove_types=0|1|2|3|4|5|6|7|8|9|16|17|18|19|20|21|32|33|34|35|36|37|38|39|40"
	}
	args = append(args,
		"-c:v:0", "copy", "-copyinkf:v:0", "-copypriorss:v:0", "1", "-bsf:v:0", restartFilter,
		"-c:v:1", "rawvideo", "-threads:v:1", "1", "-fps_mode:v:1", "passthrough", "-enc_time_base:v:1", "demux",
		"-c:v:2", "copy", "-copyinkf:v:2", "-copypriorss:v:2", "1", "-bsf:v:2", parameterFilter)
	if candidateSeconds != nil {
		args = append(args, "-filter:v:1", "select=key", "-frames:v:0", "1", "-frames:v:1", "1", "-frames:v:2", "1")
	} else {
		args = append(args, "-c:v:3", "copy", "-copyinkf:v:3", "-copypriorss:v:3", "1",
			"-c:v:4", "copy", "-copyinkf:v:4", "-copypriorss:v:4", "1",
			"-bsf:v:4", scopeFilter)
	}
	return append(args, "-f", "framehash", "-hash", "sha256", "pipe:1"), nil
}
