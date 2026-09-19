package media

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
)

func TestVideoCopySeekCodecDepthUsesExplicitDecodedPixelFormat(t *testing.T) {
	for _, test := range []struct {
		name   string
		stream Stream
		want   bool
	}{
		{"HEVC omitted scalar depth", Stream{Codec: "hevc", Profile: "Main", PixelFormat: "yuv420p"}, true},
		{"HEVC Main10 omitted scalar depth", Stream{Codec: "hevc", Profile: "Main 10", PixelFormat: "yuv420p10le"}, true},
		{"AV1 omitted scalar depth", Stream{Codec: "av1", Profile: "Main", PixelFormat: "yuv420p"}, true},
		{"AV1 ten-bit format", Stream{Codec: "av1", Profile: "Main", PixelFormat: "yuv420p10le"}, true},
		{"HEVC contradictory scalar", Stream{Codec: "hevc", Profile: "Main", PixelFormat: "yuv420p", BitDepth: 10}, false},
		{"AV1 contradictory scalar", Stream{Codec: "av1", Profile: "Main", PixelFormat: "yuv420p10le", BitDepth: 8}, false},
		{"HEVC unknown pixel representation", Stream{Codec: "hevc", Profile: "Main"}, false},
		{"AV1 unsupported chroma representation", Stream{Codec: "av1", Profile: "High", PixelFormat: "yuv444p"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.stream.CodecType = "video"
			if got := videoCopySeekSourceCodecSupported(test.stream); got != test.want {
				t.Fatalf("source codec evidence eligibility=%t, want %t", got, test.want)
			}
		})
	}
	if depth := videoCopySeekSourceDepth(Stream{Codec: "h264", PixelFormat: "yuv420p"}); depth != 0 {
		t.Fatal("the legacy H.264 unknown-depth contract was changed")
	}
}

func TestVideoCopySeekAlignmentReportsTheActualBoundary(t *testing.T) {
	index := videoSeekCandidateTestIndex(0, 20_000_000, 40_000_000)
	if _, err := SelectVideoCopySeekCandidate(index, 23_700_000); err == nil {
		t.Fatal("an exact request was silently aligned")
	}
	encoded, err := SelectVideoCopySeekCandidateAligned(index, 23_700_000, 5_000_000)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil || candidate.RequestedStartTicks != 20_000_000 || candidate.OriginalRequestedStartTicks != 23_700_000 {
		t.Fatalf("alignment did not preserve its actual and requested clocks: %+v, %v", candidate, err)
	}
	for _, tolerance := range []int64{-1, 3_699_999, 10*TicksPerSecond + 1} {
		if _, err := SelectVideoCopySeekCandidateAligned(index, 23_700_000, tolerance); err == nil {
			t.Fatal("alignment exceeded its explicit tolerance")
		}
	}
	candidate.OriginalRequestedStartTicks = candidate.RequestedStartTicks
	data, _ := json.Marshal(candidate)
	if _, err := ValidateVideoCopySeekCandidate(string(data)); err == nil {
		t.Fatal("a false alignment claim was accepted")
	}
}

func TestVideoCopySeekAlignedNativeTSClockQuantizesOnlyThePublicPosition(t *testing.T) {
	index := videoSeekCandidateTestIndex(358320)
	index.TimeBaseDenominator = 90000
	index.FormatStartTicks = 14_000_000
	const actual, requested int64 = 25_813_333, 29_513_333
	if _, err := SelectVideoCopySeekCandidate(index, actual); err == nil {
		t.Fatal("legacy exact selection accepted a fractional native timestamp")
	}
	encoded, err := SelectVideoCopySeekCandidateAligned(index, requested, 10*TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil || !candidate.QuantizedStart || !candidate.CopyTimestamps || candidate.RequestedStartTicks != actual || candidate.OriginalRequestedStartTicks != requested {
		t.Fatalf("aligned native clock lost its precise packet identity: %+v, %v", candidate, err)
	}
	quantization := new(big.Rat).Sub(VideoSeekPointTime(index, index.Entries[0]), VideoSeekRequestedTime(index, actual))
	if quantization.Sign() <= 0 || quantization.Cmp(big.NewRat(1, TicksPerSecond)) >= 0 {
		t.Fatal("public-position quantization exceeded one playback tick")
	}
	got, err := videoCopySeekOutputTimestamp(candidate, big.NewRat(1, 90000))
	if err != nil || got != 232320 {
		t.Fatalf("native output clock was derived from rounded public ticks: %d, %v", got, err)
	}
	valid := videoCopySeekTestHash(candidate)
	valid = strings.ReplaceAll(valid, ", 0, 0, 1,", ", 232320, 232320, 1,")
	if err := parseVideoCopySeekProof(strings.NewReader(valid), candidate); err != nil {
		t.Fatal(err)
	}
	wrong := strings.ReplaceAll(valid, ", 232320, 232320, 1,", ", 232319, 232319, 1,")
	if err := parseVideoCopySeekProof(strings.NewReader(wrong), candidate); err == nil {
		t.Fatal("an entire native packet tick was treated as public-position quantization")
	}
	for name, mutate := range map[string]func(*VideoCopySeekCandidate){
		"zero-normalized output":      func(c *VideoCopySeekCandidate) { c.CopyTimestamps = false },
		"unmarked quantization":       func(c *VideoCopySeekCandidate) { c.QuantizedStart = false },
		"missing original request":    func(c *VideoCopySeekCandidate) { c.OriginalRequestedStartTicks = 0 },
		"quantization above one tick": func(c *VideoCopySeekCandidate) { c.RequestedStartTicks-- },
		"later than actual packet":    func(c *VideoCopySeekCandidate) { c.RequestedStartTicks++ },
	} {
		t.Run(name, func(t *testing.T) {
			mutated := candidate
			mutate(&mutated)
			if err := validateVideoCopySeekCandidate(mutated); err == nil {
				t.Fatal("a different timeline was accepted as the proven native boundary")
			}
		})
	}
}

func TestVideoCopySeekQuantizationDoesNotHideNativeOriginRounding(t *testing.T) {
	index := videoSeekCandidateTestIndex(10)
	index.TimeBaseDenominator = 3
	index.FormatStartTicks = 5_000_000
	// The native key position is fractional in public ticks, but this origin
	// also needs a half-native-tick rounding. That is a separate, larger clock
	// change and must not be smuggled through a sub-100 ns alignment allowance.
	if _, err := SelectVideoCopySeekCandidateAligned(index, 30_000_000, 10*TicksPerSecond); err == nil {
		t.Fatal("public tick quantization also changed the native source origin")
	}
	index.FormatStartTicks = -10_000_000
	encoded, err := SelectVideoCopySeekCandidateAligned(index, 45_000_000, 10*TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil {
		t.Fatal(err)
	}
	native, err := videoCopySeekOutputTimestamp(candidate, big.NewRat(1, 3))
	if err != nil || native != 13 {
		t.Fatalf("negative source origin changed sign during native-clock normalization: %d, %v", native, err)
	}
}

func TestVideoCopySeekHEVCRequiresIndependentDecodedRestart(t *testing.T) {
	index := videoSeekCandidateTestIndex(20_000_000)
	index.Codec = "hevc"
	encoded, err := SelectVideoCopySeekCandidate(index, 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil {
		t.Fatal(err)
	}
	args, err := BuildVideoCopySeekCommandArgs(encoded, 1)
	if err != nil || !strings.Contains(strings.Join(args, " "), "filter_units=pass_types=19|20") || !strings.Contains(strings.Join(args, " "), "-c:v:2 rawvideo") {
		t.Fatalf("HEVC proof omitted codec-specific restart checks: %v, %v", args, err)
	}
	var output strings.Builder
	output.WriteString("#format: frame checksums\n#version: 2\n#hash: SHA256\n")
	for number, codec := range []string{"hevc", "hevc", "rawvideo"} {
		fmt.Fprintf(&output, "#tb %d: 1/10000000\n#media_type %d: video\n#codec_id %d: %s\n#dimensions %d: 64x64\n", number, number, number, codec, number)
	}
	fmt.Fprintf(&output, "0, 0, 0, 1, 128, %s\n1, 0, 0, 1, 64, %s\n2, 0, 0, 1, 6144, %s\n", strings.Repeat("f", 64), index.Entries[0].CodedSHA256, index.Entries[0].DecodedSHA256)
	if err := parseVideoCopySeekProof(strings.NewReader(output.String()), candidate); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(output.String(), index.Entries[0].DecodedSHA256, strings.Repeat("9", 64), 1)
	if err := parseVideoCopySeekProof(strings.NewReader(mutated), candidate); err == nil {
		t.Fatal("HEVC matching packets without matching decoded pixels were accepted")
	}
}

func TestVideoCopySeekSourceClockProofRejectsZeroNormalizedPackets(t *testing.T) {
	_, candidate := videoCopySeekTestCandidate(t)
	candidate.CopyTimestamps = true
	zero := videoCopySeekTestHash(candidate)
	if err := parseVideoCopySeekProof(strings.NewReader(zero), candidate); err == nil {
		t.Fatal("a source-global contract accepted zero-normalized output")
	}
	global := strings.ReplaceAll(zero, ", 0, 0, 1,", ", 20000000, 20000000, 1,")
	if err := parseVideoCopySeekProof(strings.NewReader(global), candidate); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(candidate)
	args, err := BuildVideoCopySeekCommandArgs(string(data), 1)
	if err != nil || !strings.Contains(strings.Join(args, " "), "-output_ts_offset 2.0000000") {
		t.Fatalf("fresh proof did not use the production output clock: %v, %v", args, err)
	}
}

func TestVideoCopySeekAudioScanRetainsOnlySharedPacketBoundaries(t *testing.T) {
	index := videoSeekCandidateTestIndex(20_000_000, 40_000_000)
	source := Stream{Index: 1, CodecType: "audio", Codec: "aac", Profile: "LC", SampleRate: 48000, Channels: 2, TimeBase: "1/48000"}
	timeBase, _ := parseVideoSeekTimeBase(source.TimeBase)
	packetHash := strings.Repeat("a", 64)
	output := "#format: frame checksums\n#version: 2\n#hash: SHA256\n#tb 0: 1/48000\n#media_type 0: audio\n#codec_id 0: aac\n" +
		"0, 96000, 96000, 1024, 123, " + packetHash + "\n0, 192000, 192000, 1024, 123, " + packetHash + ", S=1, 10, " + strings.Repeat("b", 64) + "\n"
	proofs, err := parseVideoCopySeekAudio(strings.NewReader(output), source, timeBase, index)
	if err != nil || len(proofs) != 2 || proofs[0] == nil || proofs[1] != nil {
		t.Fatalf("audio scan did not distinguish aligned packets from side-data packets: %+v, %v", proofs, err)
	}
	index.Entries[0].Audio = []VideoCopySeekAudio{*proofs[0]}
	if err := ValidateVideoSeekIndex(index); err != nil {
		t.Fatal(err)
	}
	index.Entries[0].Audio[0].PTS++
	if err := ValidateVideoSeekIndex(index); err == nil {
		t.Fatal("an audio packet on a different source clock was accepted")
	}
}
