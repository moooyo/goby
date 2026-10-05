package media

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
)

var subtitleTimelineMagic = [4]byte{'G', 'S', 'T', 'L'}

// IDs are append-only within GSTL v1 and v2 and retain the decoder's exact warnings.
var subtitleTimelineWarnings = [...]string{
	"PGS final display interval ended at the declared packet duration.",
	"PGS final display interval ended at the indexed source duration.",
	"dvd_palette_missing_monochrome_review",
	"transparent_dvd_display_ignored",
	"dvd_display_closed_at_packet_duration",
	"cue_intervals_clipped_to_source_presentation",
}

// GSTL v1 is little endian: magic[4], version u16, reserved u16, FFprobe
// SHA-256[32], duration i64, and track-count u16. Each track contains index u16,
// codec u8 (1=PGS, 2=DVD), warning-count u8, interval-count u32, warning IDs u8,
// then start/end i64 pairs. It contains no text, pixels, paths, or source IDs.
// GSTL v2 uses the same layout with an i32 track index for public external indexes.
func MarshalSubtitleTimelines(data SubtitleTimelineData) ([]byte, error) {
	if err := validateSubtitleTimelineData(data); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.Write(subtitleTimelineMagic[:])
	put := func(value any) { _ = binary.Write(&output, binary.LittleEndian, value) }
	version := uint16(1)
	if data.Profile == SubtitleTimelineExternalProfile {
		version = 2
	}
	put(version)
	put(uint16(0))
	hash, _ := hex.DecodeString(data.FFprobeSHA256)
	output.Write(hash)
	put(data.DurationTicks)
	put(uint16(len(data.Tracks)))
	for _, track := range data.Tracks {
		if version == 1 {
			put(uint16(track.StreamIndex))
		} else {
			put(int32(track.StreamIndex))
		}
		codec := byte(1)
		if track.Codec == "dvd_subtitle" {
			codec = 2
		}
		output.WriteByte(codec)
		output.WriteByte(byte(len(track.Warnings)))
		put(uint32(len(track.Intervals)))
		for _, warning := range track.Warnings {
			output.WriteByte(subtitleTimelineWarningID(warning))
		}
		for _, interval := range track.Intervals {
			put(interval.StartTicks)
			put(interval.EndTicks)
		}
	}
	return output.Bytes(), nil
}

// ParseSubtitleTimelines rejects unknown and oversized fields before allocating
// interval arrays. Returned data owns its storage and has normalized coverage.
func ParseSubtitleTimelines(encoded []byte) (SubtitleTimelineData, error) {
	bad := func() (SubtitleTimelineData, error) {
		return SubtitleTimelineData{}, fmt.Errorf("%w: invalid subtitle timeline artifact", ErrAnalysisUnproven)
	}
	if len(encoded) < 50 || int64(len(encoded)) > MaxSubtitleTimelineBytes {
		return bad()
	}
	reader := bytes.NewReader(encoded)
	read := func(value any) error { return binary.Read(reader, binary.LittleEndian, value) }
	var magic [4]byte
	var version, reserved, count uint16
	var hash [32]byte
	var data SubtitleTimelineData
	if _, err := io.ReadFull(reader, magic[:]); err != nil || magic != subtitleTimelineMagic ||
		read(&version) != nil || (version != 1 && version != 2) || read(&reserved) != nil || reserved != 0 {
		return bad()
	}
	if _, err := io.ReadFull(reader, hash[:]); err != nil || read(&data.DurationTicks) != nil ||
		read(&count) != nil || count == 0 || count > MaxSubtitleTimelineTracks {
		return bad()
	}
	data.Profile, data.FFprobeSHA256 = SubtitleTimelineProfile, hex.EncodeToString(hash[:])
	if version == 2 {
		data.Profile = SubtitleTimelineExternalProfile
	}
	for range int(count) {
		var index int32
		if version == 1 {
			var internalIndex uint16
			if read(&internalIndex) != nil || internalIndex > 4095 {
				return bad()
			}
			index = int32(internalIndex)
		} else if read(&index) != nil || index < 0 {
			return bad()
		}
		var codec, warnings byte
		var intervals uint32
		if read(&codec) != nil || read(&warnings) != nil || read(&intervals) != nil ||
			(codec != 1 && codec != 2) || int(warnings) > len(subtitleTimelineWarnings) ||
			intervals == 0 || intervals > MaxSubtitleOCRCues || reader.Len() < int(warnings)+int(intervals)*16 {
			return bad()
		}
		track := SubtitleTimelineTrack{SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{StreamIndex: int(index), Codec: "hdmv_pgs_subtitle", IntervalCount: int(intervals)}}
		if codec == 2 {
			track.Codec = "dvd_subtitle"
		}
		for range int(warnings) {
			id, err := reader.ReadByte()
			if err != nil || id == 0 || int(id) > len(subtitleTimelineWarnings) {
				return bad()
			}
			track.Warnings = append(track.Warnings, subtitleTimelineWarnings[id-1])
		}
		track.Intervals = make([]SubtitleTimelineInterval, intervals)
		for index := range track.Intervals {
			if read(&track.Intervals[index].StartTicks) != nil || read(&track.Intervals[index].EndTicks) != nil {
				return bad()
			}
		}
		data.Tracks = append(data.Tracks, track)
	}
	if reader.Len() != 0 {
		return bad()
	}
	if err := validateSubtitleTimelineData(data); err != nil {
		return SubtitleTimelineData{}, err
	}
	return data, nil
}

func subtitleTimelineWarningID(value string) byte {
	for index, warning := range subtitleTimelineWarnings {
		if warning == value {
			return byte(index + 1)
		}
	}
	return 0
}

func validateSubtitleTimelineData(data SubtitleTimelineData) error {
	bad := func() error { return fmt.Errorf("%w: invalid subtitle timeline artifact", ErrAnalysisUnproven) }
	streamIndexLimit, trackHeaderSize := int64(4095), int64(8)
	switch data.Profile {
	case SubtitleTimelineProfile:
	case SubtitleTimelineExternalProfile:
		streamIndexLimit, trackHeaderSize = math.MaxInt32, 10
	default:
		return bad()
	}
	hash, err := hex.DecodeString(data.FFprobeSHA256)
	if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != data.FFprobeSHA256 ||
		data.DurationTicks <= 0 || data.DurationTicks > subtitleTimelineDurationLimit || len(data.Tracks) == 0 || len(data.Tracks) > MaxSubtitleTimelineTracks {
		return bad()
	}
	lastIndex := -1
	size := int64(50)
	for _, track := range data.Tracks {
		if track.StreamIndex <= lastIndex || int64(track.StreamIndex) > streamIndexLimit || !subtitleTimelineCodecSupported(track.Codec) ||
			track.IntervalCount != len(track.Intervals) || len(track.Intervals) == 0 || len(track.Intervals) > MaxSubtitleOCRCues || len(track.Warnings) > len(subtitleTimelineWarnings) {
			return bad()
		}
		lastIndex = track.StreamIndex
		seen := make(map[byte]bool)
		for _, warning := range track.Warnings {
			id := subtitleTimelineWarningID(warning)
			if id == 0 || seen[id] || (track.Codec == "hdmv_pgs_subtitle" && id >= 3 && id <= 5) || (track.Codec == "dvd_subtitle" && id <= 2) {
				return bad()
			}
			seen[id] = true
		}
		if seen[1] && seen[2] {
			return bad()
		}
		lastEnd := int64(-1)
		for _, interval := range track.Intervals {
			if interval.StartTicks < 0 || interval.StartTicks <= lastEnd || interval.EndTicks <= interval.StartTicks || interval.EndTicks > data.DurationTicks {
				return bad()
			}
			lastEnd = interval.EndTicks
		}
		size += trackHeaderSize + int64(len(track.Warnings)) + int64(len(track.Intervals))*16
		if size > MaxSubtitleTimelineBytes {
			return fmt.Errorf("%w: subtitle timeline artifact limit", ErrAnalysisBudget)
		}
	}
	return nil
}
