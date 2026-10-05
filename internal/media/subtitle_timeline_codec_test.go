package media

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"slices"
	"strings"
	"testing"
)

func subtitleTimelineCodecTestData() SubtitleTimelineData {
	return SubtitleTimelineData{
		Profile:       SubtitleTimelineProfile,
		FFprobeSHA256: strings.Repeat("0123456789abcdef", 4),
		DurationTicks: 30*TicksPerSecond + 17,
		Tracks: []SubtitleTimelineTrack{
			{
				SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{
					StreamIndex:   0,
					Codec:         "hdmv_pgs_subtitle",
					IntervalCount: 2,
					Warnings: []string{
						"PGS final display interval ended at the declared packet duration.",
						"cue_intervals_clipped_to_source_presentation",
					},
				},
				Intervals: []SubtitleTimelineInterval{
					{StartTicks: 0, EndTicks: TicksPerSecond + 1},
					{StartTicks: 2*TicksPerSecond + 3, EndTicks: 5*TicksPerSecond + 7},
				},
			},
			{
				SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{
					StreamIndex:   7,
					Codec:         "dvd_subtitle",
					IntervalCount: 1,
					Warnings: []string{
						"dvd_palette_missing_monochrome_review",
						"transparent_dvd_display_ignored",
						"dvd_display_closed_at_packet_duration",
						"cue_intervals_clipped_to_source_presentation",
					},
				},
				Intervals: []SubtitleTimelineInterval{
					{StartTicks: 8*TicksPerSecond + 11, EndTicks: 30*TicksPerSecond + 17},
				},
			},
			{
				SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{
					StreamIndex:   4095,
					Codec:         "hdmv_pgs_subtitle",
					IntervalCount: 1,
					Warnings: []string{
						"PGS final display interval ended at the indexed source duration.",
					},
				},
				Intervals: []SubtitleTimelineInterval{
					{StartTicks: 8*TicksPerSecond + 11, EndTicks: 30*TicksPerSecond + 17},
				},
			},
		},
	}
}

func subtitleTimelineCodecTestIntervals(count int) []SubtitleTimelineInterval {
	intervals := make([]SubtitleTimelineInterval, count)
	for index := range intervals {
		intervals[index] = SubtitleTimelineInterval{StartTicks: int64(index) * 2, EndTicks: int64(index)*2 + 1}
	}
	return intervals
}

func subtitleTimelineCodecTestRoundTrip(t *testing.T, want SubtitleTimelineData) []byte {
	t.Helper()
	encoded, err := MarshalSubtitleTimelines(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 || int64(len(encoded)) > MaxSubtitleTimelineBytes {
		t.Fatalf("unexpected encoded size: %d", len(encoded))
	}
	before := bytes.Clone(encoded)
	got, err := ParseSubtitleTimelines(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, before) {
		t.Fatal("parsing changed the encoded input")
	}
	if got.Profile != want.Profile || got.FFprobeSHA256 != want.FFprobeSHA256 || got.DurationTicks != want.DurationTicks || len(got.Tracks) != len(want.Tracks) {
		t.Fatalf("timeline header changed: got %+v, want %+v", got, want)
	}
	for index, track := range got.Tracks {
		expected := want.Tracks[index]
		if track.StreamIndex != expected.StreamIndex || track.Codec != expected.Codec || track.IntervalCount != expected.IntervalCount ||
			!slices.Equal(track.Warnings, expected.Warnings) || !slices.Equal(track.Intervals, expected.Intervals) {
			t.Fatalf("track %d changed: got %+v, want %+v", index, track, expected)
		}
	}
	reencoded, err := MarshalSubtitleTimelines(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatal("round trip changed the canonical encoding")
	}
	return encoded
}

func TestSubtitleTimelineCodecPreservesExactIntervalsAndWarnings(t *testing.T) {
	subtitleTimelineCodecTestRoundTrip(t, subtitleTimelineCodecTestData())
}

func TestSubtitleTimelineCodecAcceptsBoundaryValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(*SubtitleTimelineData)
	}{
		{
			name: "no warnings",
			change: func(data *SubtitleTimelineData) {
				for index := range data.Tracks {
					data.Tracks[index].Warnings = nil
				}
			},
		},
		{
			name: "one tick presentation",
			change: func(data *SubtitleTimelineData) {
				data.DurationTicks = 1
				data.Tracks = data.Tracks[:1]
				data.Tracks[0].IntervalCount = 1
				data.Tracks[0].Intervals = []SubtitleTimelineInterval{{StartTicks: 0, EndTicks: 1}}
			},
		},
		{
			name: "maximum presentation duration",
			change: func(data *SubtitleTimelineData) {
				data.DurationTicks = 7 * 24 * 60 * 60 * TicksPerSecond
				data.Tracks[1].Intervals[0].EndTicks = data.DurationTicks
			},
		},
		{
			name: "maximum track count",
			change: func(data *SubtitleTimelineData) {
				track := data.Tracks[0]
				data.Tracks = make([]SubtitleTimelineTrack, MaxSubtitleTimelineTracks)
				for index := range data.Tracks {
					data.Tracks[index] = track
					data.Tracks[index].StreamIndex = index
				}
			},
		},
		{
			name: "maximum interval count",
			change: func(data *SubtitleTimelineData) {
				data.Tracks[0].Intervals = subtitleTimelineCodecTestIntervals(10000)
				data.Tracks[0].IntervalCount = len(data.Tracks[0].Intervals)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := subtitleTimelineCodecTestData()
			test.change(&data)
			subtitleTimelineCodecTestRoundTrip(t, data)
		})
	}
}

func TestMarshalSubtitleTimelinesRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name   string
		change func(*SubtitleTimelineData)
	}{
		{"missing profile", func(data *SubtitleTimelineData) { data.Profile = "" }},
		{"unknown profile", func(data *SubtitleTimelineData) { data.Profile += "-unknown" }},
		{"missing digest", func(data *SubtitleTimelineData) { data.FFprobeSHA256 = "" }},
		{"short digest", func(data *SubtitleTimelineData) { data.FFprobeSHA256 = data.FFprobeSHA256[:63] }},
		{"long digest", func(data *SubtitleTimelineData) { data.FFprobeSHA256 += "0" }},
		{"uppercase digest", func(data *SubtitleTimelineData) { data.FFprobeSHA256 = strings.ToUpper(data.FFprobeSHA256) }},
		{"nonhex digest", func(data *SubtitleTimelineData) { data.FFprobeSHA256 = strings.Repeat("g", 64) }},
		{"zero duration", func(data *SubtitleTimelineData) { data.DurationTicks = 0 }},
		{"negative duration", func(data *SubtitleTimelineData) { data.DurationTicks = -1 }},
		{"excess duration", func(data *SubtitleTimelineData) { data.DurationTicks = 7*24*60*60*TicksPerSecond + 1 }},
		{"overflow duration", func(data *SubtitleTimelineData) { data.DurationTicks = 1<<63 - 1 }},
		{"nil tracks", func(data *SubtitleTimelineData) { data.Tracks = nil }},
		{"empty tracks", func(data *SubtitleTimelineData) { data.Tracks = []SubtitleTimelineTrack{} }},
		{
			name: "excess tracks",
			change: func(data *SubtitleTimelineData) {
				track := data.Tracks[0]
				data.Tracks = make([]SubtitleTimelineTrack, MaxSubtitleTimelineTracks+1)
				for index := range data.Tracks {
					data.Tracks[index] = track
					data.Tracks[index].StreamIndex = index
				}
			},
		},
		{"negative stream index", func(data *SubtitleTimelineData) { data.Tracks[0].StreamIndex = -1 }},
		{"excess stream index", func(data *SubtitleTimelineData) { data.Tracks[1].StreamIndex = 4096 }},
		{"duplicate stream index", func(data *SubtitleTimelineData) { data.Tracks[1].StreamIndex = data.Tracks[0].StreamIndex }},
		{"unordered tracks", func(data *SubtitleTimelineData) { data.Tracks[0], data.Tracks[1] = data.Tracks[1], data.Tracks[0] }},
		{"missing codec", func(data *SubtitleTimelineData) { data.Tracks[0].Codec = "" }},
		{"text codec", func(data *SubtitleTimelineData) { data.Tracks[0].Codec = "subrip" }},
		{"noncanonical codec", func(data *SubtitleTimelineData) { data.Tracks[0].Codec = "HDMV_PGS_SUBTITLE" }},
		{"negative interval count", func(data *SubtitleTimelineData) { data.Tracks[0].IntervalCount = -1 }},
		{"zero interval count", func(data *SubtitleTimelineData) { data.Tracks[0].IntervalCount = 0 }},
		{"low interval count", func(data *SubtitleTimelineData) { data.Tracks[0].IntervalCount-- }},
		{"high interval count", func(data *SubtitleTimelineData) { data.Tracks[0].IntervalCount++ }},
		{
			name: "nil intervals",
			change: func(data *SubtitleTimelineData) {
				data.Tracks[0].Intervals = nil
				data.Tracks[0].IntervalCount = 0
			},
		},
		{
			name: "empty intervals",
			change: func(data *SubtitleTimelineData) {
				data.Tracks[0].Intervals = []SubtitleTimelineInterval{}
				data.Tracks[0].IntervalCount = 0
			},
		},
		{
			name: "excess intervals",
			change: func(data *SubtitleTimelineData) {
				data.Tracks[0].Intervals = subtitleTimelineCodecTestIntervals(10001)
				data.Tracks[0].IntervalCount = len(data.Tracks[0].Intervals)
			},
		},
		{"negative start", func(data *SubtitleTimelineData) { data.Tracks[0].Intervals[0].StartTicks = -1 }},
		{"negative end", func(data *SubtitleTimelineData) { data.Tracks[0].Intervals[0].EndTicks = -1 }},
		{"empty interval", func(data *SubtitleTimelineData) {
			data.Tracks[0].Intervals[0].EndTicks = data.Tracks[0].Intervals[0].StartTicks
		}},
		{"reversed interval", func(data *SubtitleTimelineData) {
			data.Tracks[0].Intervals[1].EndTicks = data.Tracks[0].Intervals[1].StartTicks - 1
		}},
		{"end beyond presentation", func(data *SubtitleTimelineData) { data.Tracks[1].Intervals[0].EndTicks = data.DurationTicks + 1 }},
		{"overflow interval end", func(data *SubtitleTimelineData) { data.Tracks[1].Intervals[0].EndTicks = 1<<63 - 1 }},
		{"overlapping intervals", func(data *SubtitleTimelineData) {
			data.Tracks[0].Intervals[1].StartTicks = data.Tracks[0].Intervals[0].EndTicks - 1
		}},
		{"adjacent intervals", func(data *SubtitleTimelineData) {
			data.Tracks[0].Intervals[1].StartTicks = data.Tracks[0].Intervals[0].EndTicks
		}},
		{"duplicate intervals", func(data *SubtitleTimelineData) { data.Tracks[0].Intervals[1] = data.Tracks[0].Intervals[0] }},
		{
			name: "unordered intervals",
			change: func(data *SubtitleTimelineData) {
				intervals := data.Tracks[0].Intervals
				intervals[0], intervals[1] = intervals[1], intervals[0]
			},
		},
		{"unknown warning", func(data *SubtitleTimelineData) { data.Tracks[0].Warnings = []string{"unknown_warning"} }},
		{"empty warning", func(data *SubtitleTimelineData) { data.Tracks[0].Warnings = []string{""} }},
		{"DVD warning on PGS", func(data *SubtitleTimelineData) {
			data.Tracks[0].Warnings = []string{"dvd_palette_missing_monochrome_review"}
		}},
		{"PGS warning on DVD", func(data *SubtitleTimelineData) {
			data.Tracks[1].Warnings = []string{"PGS final display interval ended at the indexed source duration."}
		}},
		{
			name: "conflicting PGS end warnings",
			change: func(data *SubtitleTimelineData) {
				data.Tracks[0].Warnings = []string{
					"PGS final display interval ended at the declared packet duration.",
					"PGS final display interval ended at the indexed source duration.",
				}
			},
		},
		{
			name: "duplicate warning",
			change: func(data *SubtitleTimelineData) {
				data.Tracks[0].Warnings = append(data.Tracks[0].Warnings, data.Tracks[0].Warnings[0])
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := subtitleTimelineCodecTestData()
			test.change(&data)
			if _, err := MarshalSubtitleTimelines(data); err == nil {
				t.Fatal("invalid timeline data was accepted")
			}
		})
	}
}

func TestParseSubtitleTimelinesRejectsDamagedEncoding(t *testing.T) {
	encoded := subtitleTimelineCodecTestRoundTrip(t, subtitleTimelineCodecTestData())
	t.Run("every truncation", func(t *testing.T) {
		for size := 0; size < len(encoded); size++ {
			if _, err := ParseSubtitleTimelines(encoded[:size]); err == nil {
				t.Fatalf("truncation to %d of %d bytes was accepted", size, len(encoded))
			}
		}
	})
	for _, field := range []struct {
		name   string
		offset int
	}{
		{name: "magic", offset: 0},
		{name: "version", offset: 4},
		{name: "flags", offset: 6},
	} {
		t.Run(field.name, func(t *testing.T) {
			data := bytes.Clone(encoded)
			data[field.offset] ^= 0xff
			if _, err := ParseSubtitleTimelines(data); err == nil {
				t.Fatal("damaged header was accepted")
			}
		})
	}
	t.Run("trailing byte", func(t *testing.T) {
		data := append(bytes.Clone(encoded), 0)
		if _, err := ParseSubtitleTimelines(data); err == nil {
			t.Fatal("trailing byte was accepted")
		}
	})
	t.Run("concatenated artifacts", func(t *testing.T) {
		data := append(bytes.Clone(encoded), encoded...)
		if _, err := ParseSubtitleTimelines(data); err == nil {
			t.Fatal("concatenated artifacts were accepted")
		}
	})
	t.Run("oversized input", func(t *testing.T) {
		data := make([]byte, MaxSubtitleTimelineBytes+1)
		copy(data, encoded)
		if _, err := ParseSubtitleTimelines(data); err == nil {
			t.Fatal("oversized encoding was accepted")
		}
	})
}

func TestParseSubtitleTimelinesRejectsInvalidEncodedFields(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	data.Tracks = data.Tracks[:1]
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	// These offsets come from the public GSTL v1 layout, not decoder helpers.
	const (
		durationOffset = 40
		trackOffset    = 50
		warningOffset  = trackOffset + 8
	)
	intervalOffset := warningOffset + len(data.Tracks[0].Warnings)
	tests := []struct {
		name   string
		offset int
		width  int
		value  uint64
	}{
		{"zero duration", durationOffset, 8, 0},
		{"excess duration", durationOffset, 8, uint64(7*24*60*60*TicksPerSecond + 1)},
		{"maximum signed duration", durationOffset, 8, 1<<63 - 1},
		{"negative duration", durationOffset, 8, 1 << 63},
		{"maximum unsigned duration", durationOffset, 8, 1<<64 - 1},
		{"zero tracks", 48, 2, 0},
		{"excess tracks", 48, 2, uint64(MaxSubtitleTimelineTracks + 1)},
		{"maximum track declaration", 48, 2, 1<<16 - 1},
		{"missing declared tracks", 48, 2, uint64(MaxSubtitleTimelineTracks)},
		{"excess stream index", trackOffset, 2, 4096},
		{"maximum stream index", trackOffset, 2, 1<<16 - 1},
		{"zero codec", trackOffset + 2, 1, 0},
		{"unknown codec", trackOffset + 2, 1, 3},
		{"maximum codec", trackOffset + 2, 1, 255},
		{"PGS warnings on DVD", trackOffset + 2, 1, 2},
		{"excess warning count", trackOffset + 3, 1, 7},
		{"maximum warning count", trackOffset + 3, 1, 255},
		{"zero interval count", trackOffset + 4, 4, 0},
		{"excess interval count", trackOffset + 4, 4, 10001},
		{"maximum interval declaration", trackOffset + 4, 4, 1<<32 - 1},
		{"short interval payload", trackOffset + 4, 4, 10000},
		{"zero warning ID", warningOffset, 1, 0},
		{"unknown warning ID", warningOffset, 1, 7},
		{"maximum warning ID", warningOffset, 1, 255},
		{"DVD warning on PGS", warningOffset, 1, 3},
		{"duplicate warning", warningOffset + 1, 1, 1},
		{"conflicting PGS end warnings", warningOffset + 1, 1, 2},
		{"negative interval start", intervalOffset, 8, 1 << 63},
		{"maximum unsigned interval start", intervalOffset, 8, 1<<64 - 1},
		{"empty interval", intervalOffset + 8, 8, 0},
		{"end beyond presentation", intervalOffset + 8, 8, uint64(data.DurationTicks + 1)},
		{"negative interval end", intervalOffset + 8, 8, 1 << 63},
		{"maximum unsigned interval end", intervalOffset + 8, 8, 1<<64 - 1},
		{"overlapping intervals", intervalOffset + 16, 8, uint64(data.Tracks[0].Intervals[0].EndTicks - 1)},
		{"adjacent intervals", intervalOffset + 16, 8, uint64(data.Tracks[0].Intervals[0].EndTicks)},
		{"reversed interval", intervalOffset + 24, 8, uint64(data.Tracks[0].Intervals[1].StartTicks - 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := bytes.Clone(encoded)
			switch test.width {
			case 1:
				mutated[test.offset] = byte(test.value)
			case 2:
				binary.LittleEndian.PutUint16(mutated[test.offset:], uint16(test.value))
			case 4:
				binary.LittleEndian.PutUint32(mutated[test.offset:], uint32(test.value))
			case 8:
				binary.LittleEndian.PutUint64(mutated[test.offset:], test.value)
			}
			if _, err := ParseSubtitleTimelines(mutated); err == nil {
				t.Fatal("invalid encoded field was accepted")
			}
		})
	}
}

func TestParseSubtitleTimelinesRejectsDuplicateAndUnorderedStreams(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	secondOffset := 50 + 8 + len(data.Tracks[0].Warnings) + len(data.Tracks[0].Intervals)*16
	thirdOffset := secondOffset + 8 + len(data.Tracks[1].Warnings) + len(data.Tracks[1].Intervals)*16
	for _, test := range []struct {
		name   string
		offset int
		index  uint16
	}{
		{name: "duplicate", offset: secondOffset, index: uint16(data.Tracks[0].StreamIndex)},
		{name: "unordered", offset: thirdOffset, index: uint16(data.Tracks[1].StreamIndex - 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := bytes.Clone(encoded)
			binary.LittleEndian.PutUint16(mutated[test.offset:], test.index)
			if _, err := ParseSubtitleTimelines(mutated); err == nil {
				t.Fatal("nonascending stream indexes were accepted")
			}
		})
	}
}

func TestMarshalSubtitleTimelinesRejectsAggregateByteOverflow(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	track := data.Tracks[0]
	track.Warnings = nil
	track.Intervals = subtitleTimelineCodecTestIntervals(10000)
	track.IntervalCount = len(track.Intervals)
	data.Tracks = make([]SubtitleTimelineTrack, MaxSubtitleTimelineTracks)
	for index := range data.Tracks {
		data.Tracks[index] = track
		data.Tracks[index].StreamIndex = index
	}
	if _, err := MarshalSubtitleTimelines(data); err == nil {
		t.Fatal("individually valid tracks exceeding the aggregate byte budget were accepted")
	}
}

func TestSubtitleTimelineCodecAcceptsExactByteLimit(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	const trackCount = 53
	const warningCount = 6
	// Six single-byte warnings align the header and 53 track headers to 16 bytes.
	const overhead = 50 + trackCount*8 + warningCount
	remaining := int((MaxSubtitleTimelineBytes - overhead) / 16)
	intervals := subtitleTimelineCodecTestIntervals(10000)
	track := data.Tracks[0]
	track.Warnings = nil
	data.Tracks = make([]SubtitleTimelineTrack, trackCount)
	for index := range data.Tracks {
		count := min(remaining, len(intervals))
		data.Tracks[index] = track
		data.Tracks[index].StreamIndex = index
		data.Tracks[index].IntervalCount = count
		data.Tracks[index].Intervals = intervals[:count]
		if index < warningCount {
			data.Tracks[index].Warnings = []string{"PGS final display interval ended at the declared packet duration."}
		}
		remaining -= count
	}
	if remaining != 0 {
		t.Fatal("fixture does not fit the per-track interval limit")
	}
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	if int64(len(encoded)) != MaxSubtitleTimelineBytes {
		t.Fatalf("fixture size is %d, want exactly %d", len(encoded), MaxSubtitleTimelineBytes)
	}
	data.Tracks[warningCount].Warnings = []string{"PGS final display interval ended at the declared packet duration."}
	if _, err := MarshalSubtitleTimelines(data); err == nil {
		t.Fatal("one byte beyond the aggregate byte budget was accepted")
	}
}

func TestSubtitleTimelineCodecPreservesV1Bytes(t *testing.T) {
	data := SubtitleTimelineData{
		Profile:       SubtitleTimelineProfile,
		FFprobeSHA256: strings.Repeat("00", 32),
		DurationTicks: 1,
		Tracks: []SubtitleTimelineTrack{{
			SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{
				StreamIndex:   4095,
				Codec:         "dvd_subtitle",
				IntervalCount: 1,
				Warnings:      []string{"dvd_palette_missing_monochrome_review"},
			},
			Intervals: []SubtitleTimelineInterval{{StartTicks: 0, EndTicks: 1}},
		}},
	}
	// This fixed fixture records the original GSTL v1 format, including its u16 index.
	want, err := hex.DecodeString("4753544c01000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"01000000000000000100ff0f0201010000000300000000000000000100000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if encoded := subtitleTimelineCodecTestRoundTrip(t, data); !bytes.Equal(encoded, want) {
		t.Fatalf("GSTL v1 bytes changed: got %x, want %x", encoded, want)
	}
	parsed, err := ParseSubtitleTimelines(want)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Profile != SubtitleTimelineProfile {
		t.Fatalf("GSTL v1 profile changed to %q", parsed.Profile)
	}
}

func TestSubtitleTimelineCodecV2PreservesPublicStreamIndexes(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	data.Profile = SubtitleTimelineExternalProfile
	data.Tracks[1].StreamIndex = 1 << 16
	data.Tracks[2].StreamIndex = math.MaxInt32
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	if version := binary.LittleEndian.Uint16(encoded[4:]); version != 2 {
		t.Fatalf("GSTL version is %d, want 2", version)
	}
	// These offsets use the public GSTL v2 layout with ten-byte track headers.
	offset := 50
	for _, track := range data.Tracks {
		if index := binary.LittleEndian.Uint32(encoded[offset:]); index != uint32(track.StreamIndex) {
			t.Fatalf("encoded stream index is %d, want %d", index, track.StreamIndex)
		}
		offset += 10 + len(track.Warnings) + len(track.Intervals)*16
	}
	if len(encoded) != offset {
		t.Fatalf("GSTL v2 size is %d, want %d", len(encoded), offset)
	}
}

func TestMarshalSubtitleTimelinesV2RejectsOutOfRangeIndexes(t *testing.T) {
	for _, test := range []struct {
		name  string
		index int64
	}{
		{name: "negative index", index: -1},
		{name: "minimum int32 index", index: math.MinInt32},
		{name: "above maximum int32 index", index: int64(math.MaxInt32) + 1},
		{name: "maximum uint32 index", index: math.MaxUint32},
	} {
		t.Run(test.name, func(t *testing.T) {
			if int64(int(test.index)) != test.index {
				t.Skip("stream index is not representable by int on this architecture")
			}
			data := subtitleTimelineCodecTestData()
			data.Profile = SubtitleTimelineExternalProfile
			data.Tracks = data.Tracks[:1]
			data.Tracks[0].StreamIndex = int(test.index)
			if _, err := MarshalSubtitleTimelines(data); err == nil {
				t.Fatal("stream index outside the nonnegative int32 range was accepted")
			}
		})
	}
}

func TestParseSubtitleTimelinesV2RejectsDamagedHeaders(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	data.Profile = SubtitleTimelineExternalProfile
	data.Tracks = data.Tracks[:1]
	data.Tracks[0].StreamIndex = 1 << 16
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	t.Run("every truncation", func(t *testing.T) {
		for size := 0; size < len(encoded); size++ {
			if _, err := ParseSubtitleTimelines(encoded[:size]); err == nil {
				t.Fatalf("truncation to %d of %d bytes was accepted", size, len(encoded))
			}
		}
	})
	t.Run("trailing byte", func(t *testing.T) {
		if _, err := ParseSubtitleTimelines(append(bytes.Clone(encoded), 0)); err == nil {
			t.Fatal("trailing byte was accepted")
		}
	})
	// These offsets come from the public GSTL v2 layout, not decoder helpers.
	const trackOffset = 50
	for _, test := range []struct {
		name   string
		offset int
		width  int
		value  uint32
	}{
		{"invalid magic", 0, 1, 0},
		{"zero version", 4, 2, 0},
		{"v2 track with v1 version", 4, 2, 1},
		{"unknown version", 4, 2, 3},
		{"reserved bits", 6, 2, 1},
		{"zero tracks", 48, 2, 0},
		{"excess tracks", 48, 2, uint32(MaxSubtitleTimelineTracks + 1)},
		{"missing declared track", 48, 2, 2},
		{"minimum signed stream index", trackOffset, 4, 1 << 31},
		{"negative stream index", trackOffset, 4, math.MaxUint32},
		{"zero codec", trackOffset + 4, 1, 0},
		{"unknown codec", trackOffset + 4, 1, 3},
		{"PGS warnings on DVD", trackOffset + 4, 1, 2},
		{"excess warning count", trackOffset + 5, 1, 7},
		{"zero interval count", trackOffset + 6, 4, 0},
		{"excess interval count", trackOffset + 6, 4, 10001},
		{"maximum interval declaration", trackOffset + 6, 4, math.MaxUint32},
		{"short interval payload", trackOffset + 6, 4, 10000},
		{"zero warning ID", trackOffset + 10, 1, 0},
		{"unknown warning ID", trackOffset + 10, 1, 7},
		{"duplicate warning", trackOffset + 11, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := bytes.Clone(encoded)
			switch test.width {
			case 1:
				mutated[test.offset] = byte(test.value)
			case 2:
				binary.LittleEndian.PutUint16(mutated[test.offset:], uint16(test.value))
			case 4:
				binary.LittleEndian.PutUint32(mutated[test.offset:], test.value)
			}
			if _, err := ParseSubtitleTimelines(mutated); err == nil {
				t.Fatal("damaged GSTL v2 header was accepted")
			}
		})
	}
}

func TestParseSubtitleTimelinesV2RejectsNonascendingStreams(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	data.Profile = SubtitleTimelineExternalProfile
	data.Tracks[0].StreamIndex = 1 << 16
	data.Tracks[1].StreamIndex = 1<<16 + 1
	data.Tracks[2].StreamIndex = math.MaxInt32
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	secondOffset := 50 + 10 + len(data.Tracks[0].Warnings) + len(data.Tracks[0].Intervals)*16
	thirdOffset := secondOffset + 10 + len(data.Tracks[1].Warnings) + len(data.Tracks[1].Intervals)*16
	for _, test := range []struct {
		name   string
		offset int
		index  uint32
	}{
		{name: "duplicate", offset: secondOffset, index: uint32(data.Tracks[0].StreamIndex)},
		{name: "unordered", offset: thirdOffset, index: uint32(data.Tracks[1].StreamIndex - 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := bytes.Clone(encoded)
			binary.LittleEndian.PutUint32(mutated[test.offset:], test.index)
			if _, err := ParseSubtitleTimelines(mutated); err == nil {
				t.Fatal("nonascending GSTL v2 stream indexes were accepted")
			}
		})
	}
}

func TestSubtitleTimelineCodecV2AcceptsExactByteLimit(t *testing.T) {
	data := subtitleTimelineCodecTestData()
	data.Profile = SubtitleTimelineExternalProfile
	const trackCount = 53
	const warningCount = 12
	// Twelve single-byte warnings align the header and 53 v2 track headers to 16 bytes.
	const overhead = 50 + trackCount*10 + warningCount
	remaining := int((MaxSubtitleTimelineBytes - overhead) / 16)
	intervals := subtitleTimelineCodecTestIntervals(10000)
	track := data.Tracks[0]
	track.Warnings = nil
	data.Tracks = make([]SubtitleTimelineTrack, trackCount)
	for index := range data.Tracks {
		count := min(remaining, len(intervals))
		data.Tracks[index] = track
		data.Tracks[index].StreamIndex = 1<<16 + index
		data.Tracks[index].IntervalCount = count
		data.Tracks[index].Intervals = intervals[:count]
		if index < warningCount {
			data.Tracks[index].Warnings = []string{"PGS final display interval ended at the declared packet duration."}
		}
		remaining -= count
	}
	if remaining != 0 {
		t.Fatal("fixture does not fit the per-track interval limit")
	}
	encoded := subtitleTimelineCodecTestRoundTrip(t, data)
	if int64(len(encoded)) != MaxSubtitleTimelineBytes {
		t.Fatalf("fixture size is %d, want exactly %d", len(encoded), MaxSubtitleTimelineBytes)
	}
	data.Tracks[warningCount].Warnings = []string{"PGS final display interval ended at the declared packet duration."}
	if _, err := MarshalSubtitleTimelines(data); err == nil {
		t.Fatal("one byte beyond the GSTL v2 aggregate byte budget was accepted")
	}
}
