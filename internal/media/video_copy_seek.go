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

// VideoCopySeekCandidate proposes an exact IDR boundary for packet copying.
// Decoder restart evidence is only a candidate source; it never proves that
// stream copying emits that picture first or preserves the requested clock.
type VideoCopySeekCandidate struct {
	Version             int            `json:"version"`
	RequestedStartTicks int64          `json:"requested_start_ticks"`
	Index               VideoSeekIndex `json:"index"`
}

// SelectVideoCopySeekCandidateForInfo validates the complete bounded catalog
// evidence before reducing it to one exact copied-packet proposal. Callers may
// retain the reduced candidate during a bounded profile search; stale, mixed,
// or oversized indexes are declined before a copy delivery mode is chosen.
func SelectVideoCopySeekCandidateForInfo(info Info, streamIndex int, requestedTicks int64) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("video copy seek catalog evidence is inconsistent or unavailable")
	}
	if info.ProbeVersion != CurrentProbeVersion || !info.FormatStartKnown ||
		len(info.Streams) == 0 || len(info.Streams) > maxVideoCopySeekSourceStreams ||
		len(info.VideoSeekIndexes) == 0 || len(info.VideoSeekIndexes) > maxVideoCopySeekSourceStreams {
		return invalid()
	}
	var video *Stream
	for position := range info.Streams {
		stream := &info.Streams[position]
		if stream.Index != streamIndex {
			continue
		}
		if video != nil || stream.CodecType != "video" || stream.Codec != "h264" || stream.IsExternal || stream.IsAttachedPicture {
			return invalid()
		}
		video = stream
	}
	if video == nil {
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
			len(index.PixelFormat) > 64 || len(index.SourceIdentity) != 64 || len(index.ToolIdentity) != 64 || len(index.ParameterSetsSHA256) != 64 {
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
			if index.Width != video.Width || index.Height != video.Height || index.PixelFormat != strings.ToLower(video.PixelFormat) ||
				timeBase.Cmp(videoSeekTimeBase(index.TimeBaseNumerator, index.TimeBaseDenominator)) != 0 {
				return invalid()
			}
			selected = index
		}
	}
	if selected == nil {
		return invalid()
	}
	return SelectVideoCopySeekCandidate(*selected, requestedTicks)
}

// SelectVideoCopySeekCandidate accepts only an exact source-clock IDR whose
// PTS and DTS agree. Earlier keyframes, rounded boundaries, and decoder pre-roll
// cannot be hidden safely by every fragmented-MP4 client and are declined.
func SelectVideoCopySeekCandidate(index VideoSeekIndex, requestedTicks int64) (string, error) {
	if err := ValidateVideoSeekIndex(index); err != nil {
		return "", err
	}
	requested := VideoSeekRequestedTime(index, requestedTicks)
	for _, point := range index.Entries {
		if point.PTS != point.DTS || VideoSeekPointTime(index, point).Cmp(requested) != 0 {
			continue
		}
		index.Entries = []VideoSeekPoint{point}
		candidate := VideoCopySeekCandidate{Version: VideoCopySeekCandidateVersion, RequestedStartTicks: requestedTicks, Index: index}
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
	if err := ValidateVideoSeekIndex(index); err != nil {
		return err
	}
	point := index.Entries[0]
	if point.PTS != point.DTS || VideoSeekPointTime(index, point).Cmp(VideoSeekRequestedTime(index, candidate.RequestedStartTicks)) != 0 {
		return fmt.Errorf("video copy seek is not an exact IDR boundary without decode pre-roll")
	}
	return nil
}

// BuildVideoCopySeekCommandArgs mirrors the production video input and output
// seek arguments. Both branches copy packets: the first exposes the actual
// first output packet and the second establishes its indexed IDR contents.
// Audio is intentionally absent from this packet proof; production reopens it
// linearly and encodes AAC using the existing shared-clock trim contract.
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
	args := []string{"-v", "level+warning", "-nostdin", "-copyts", "-threads", strconv.Itoa(threads),
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-seek_timestamp", "1", "-noaccurate_seek", "-ss", seconds(absolute),
		"-itsoffset", seconds(new(big.Int).Neg(big.NewInt(index.FormatStartTicks))), "-i", "/proc/self/fd/3",
		"-ss", seconds(big.NewInt(candidate.RequestedStartTicks)),
		"-t", seconds(big.NewInt(index.DurationTicks - candidate.RequestedStartTicks)),
		"-map", "0:" + strconv.Itoa(index.StreamIndex), "-map", "0:" + strconv.Itoa(index.StreamIndex),
		"-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn",
		"-c:v:0", "copy", "-c:v:1", "copy", "-bsf:v:1", "filter_units=pass_types=5",
		"-frames:v:0", "1", "-frames:v:1", "1", "-avoid_negative_ts", "disabled",
		"-f", "framehash", "-hash", "sha256", "pipe:1"}
	return args, nil
}
