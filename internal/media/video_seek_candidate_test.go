package media

import (
	"encoding/json"
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"
)

func videoSeekCandidateTestIndex(points ...int64) VideoSeekIndex {
	index := VideoSeekIndex{
		Version: VideoSeekIndexVersion, StreamIndex: 0, DurationTicks: 1_000_000_000,
		TimeBaseNumerator: 1, TimeBaseDenominator: TicksPerSecond,
		SourceIdentity: strings.Repeat("a", 64), ToolIdentity: strings.Repeat("b", 64),
		ParameterSetsSHA256:   strings.Repeat("e", 64),
		PacketSideDataChecked: true,
		NALScopeChecked:       true,
		Width:                 64, Height: 64, PixelFormat: "yuv420p", DecodedFrameBytes: 6144,
	}
	for _, pts := range points {
		index.Entries = append(index.Entries, VideoSeekPoint{
			PTS: pts, DTS: pts, CodedSHA256: strings.Repeat("c", 64), DecodedSHA256: strings.Repeat("d", 64),
		})
	}
	return index
}

func videoSeekCandidateTestSelect(t *testing.T, index VideoSeekIndex, requested int64) VideoSeekCandidate {
	t.Helper()
	encoded, err := SelectVideoSeekCandidate(index, requested)
	if err != nil {
		t.Fatalf("select candidate: %v", err)
	}
	candidate, err := ValidateVideoSeekCandidate(encoded)
	if err != nil {
		t.Fatalf("validate selected candidate: %v", err)
	}
	return candidate
}

func TestSelectVideoSeekCandidatePreservesNativeRequestBoundary(t *testing.T) {
	index := videoSeekCandidateTestIndex(30_000_000, 30_000_001, 30_000_002)
	index.TimeBaseDenominator = 30_000_000
	for _, fixture := range []struct {
		requested int64
		lastPTS   int64
	}{
		{10_000_000, 30_000_000},
		{10_000_001, 30_000_002},
	} {
		candidate := videoSeekCandidateTestSelect(t, index, fixture.requested)
		last := candidate.Index.Entries[len(candidate.Index.Entries)-1]
		if last.PTS != fixture.lastPTS || candidate.InputSeekTicks != 10_000_000 ||
			candidate.RequestedStartTicks != fixture.requested {
			t.Fatalf("native boundary selected %+v with last PTS %d; want PTS %d", candidate, last.PTS, fixture.lastPTS)
		}
	}
}

func TestSelectVideoSeekCandidateFloorsSignedSourceClockBeforeSubtractingOrigin(t *testing.T) {
	index := videoSeekCandidateTestIndex(-3, -2, -1)
	index.TimeBaseDenominator = 3
	index.FormatStartTicks = -10_000_000
	candidate := videoSeekCandidateTestSelect(t, index, 3_333_334)
	last := candidate.Index.Entries[len(candidate.Index.Entries)-1]
	if candidate.InputSeekTicks != 3_333_333 || last.PTS != -2 {
		t.Fatalf("signed source clock produced seek %d and PTS %d", candidate.InputSeekTicks, last.PTS)
	}
}

func TestSelectVideoSeekCandidateUsesFormatOriginAndExactLargeIntegers(t *testing.T) {
	const origin = int64(9_007_199_254_740_992)
	index := videoSeekCandidateTestIndex(origin, origin+1, origin+2)
	index.FormatStartTicks = origin
	candidate := videoSeekCandidateTestSelect(t, index, 1)
	last := candidate.Index.Entries[len(candidate.Index.Entries)-1]
	if last.PTS != origin+1 || candidate.InputSeekTicks != 1 {
		t.Fatalf("large source clock selected PTS %d and seek %d", last.PTS, candidate.InputSeekTicks)
	}
	index = videoSeekCandidateTestIndex(180_000, 180_001)
	index.TimeBaseDenominator = 90_000
	index.FormatStartTicks = 15_453_330
	candidate = videoSeekCandidateTestSelect(t, index, 5_000_000)
	if candidate.InputSeekTicks != 4_546_781 {
		t.Fatalf("format origin produced seek %d; want 4546781", candidate.InputSeekTicks)
	}
}

func TestSelectVideoSeekCandidateKeepsOnlyTheLast64PrecedingPoints(t *testing.T) {
	var points []int64
	for value := int64(100); value <= 10_000; value += 100 {
		points = append(points, value)
	}
	index := videoSeekCandidateTestIndex(points...)
	candidate := videoSeekCandidateTestSelect(t, index, 9500)
	entries := candidate.Index.Entries
	if len(entries) != 64 || entries[0].PTS != 3200 || entries[len(entries)-1].PTS != 9500 ||
		candidate.InputSeekTicks != 9500 || len(index.Entries) != 100 ||
		candidate.Index.SourceIdentity != index.SourceIdentity || candidate.Index.ToolIdentity != index.ToolIdentity {
		t.Fatalf("candidate did not preserve the bounded evidence window: %+v", candidate)
	}
}

func TestSelectVideoSeekCandidateRejectsUnsupportedRequestsAndIndexes(t *testing.T) {
	for _, requested := range []int64{-1, 0, 1_000_000_000, 1_000_000_001} {
		if _, err := SelectVideoSeekCandidate(videoSeekCandidateTestIndex(10, 20), requested); err == nil {
			t.Errorf("request %d was accepted", requested)
		}
	}
	for _, index := range []VideoSeekIndex{
		videoSeekCandidateTestIndex(20),
		videoSeekCandidateTestIndex(0),
		videoSeekCandidateTestIndex(-10),
		videoSeekCandidateTestIndex(),
		videoSeekCandidateTestIndex(10, 10),
	} {
		if _, err := SelectVideoSeekCandidate(index, 10); err == nil {
			t.Errorf("unsupported evidence was accepted: %+v", index)
		}
	}
	index := videoSeekCandidateTestIndex(10)
	index.TimeBaseDenominator = 0
	if _, err := SelectVideoSeekCandidate(index, 10); err == nil {
		t.Fatal("zero time base denominator was accepted")
	}
}

func TestValidateVideoSeekCandidateRejectsTamperingAndExcessEvidence(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		mutate func(*VideoSeekCandidate)
	}{
		{"version", func(c *VideoSeekCandidate) { c.Version++ }},
		{"input_argument", func(c *VideoSeekCandidate) { c.InputSeekTicks-- }},
		{"zero_input", func(c *VideoSeekCandidate) { c.InputSeekTicks = 0 }},
		{"zero_request", func(c *VideoSeekCandidate) { c.RequestedStartTicks = 0 }},
		{"source_end", func(c *VideoSeekCandidate) { c.RequestedStartTicks = c.Index.DurationTicks }},
		{"request_precedes_input", func(c *VideoSeekCandidate) { c.RequestedStartTicks = c.InputSeekTicks - 1 }},
		{"source_identity", func(c *VideoSeekCandidate) { c.Index.SourceIdentity = "invalid" }},
		{"missing_entries", func(c *VideoSeekCandidate) { c.Index.Entries = nil }},
		{"excess_entries", func(c *VideoSeekCandidate) {
			c.Index.Entries = nil
			for pts := int64(1); pts <= 65; pts++ {
				c.Index.Entries = append(c.Index.Entries, videoSeekCandidateTestIndex(pts).Entries[0])
			}
			c.RequestedStartTicks, c.InputSeekTicks = 65, 65
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			candidate := videoSeekCandidateTestSelect(t, videoSeekCandidateTestIndex(10, 20, 30), 25)
			fixture.mutate(&candidate)
			data, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateVideoSeekCandidate(string(data)); err == nil {
				t.Fatal("tampered candidate was accepted")
			}
		})
	}
	index := videoSeekCandidateTestIndex(30_000_001)
	index.TimeBaseDenominator = 30_000_000
	candidate := VideoSeekCandidate{Version: VideoSeekIndexVersion, RequestedStartTicks: 10_000_000,
		InputSeekTicks: 10_000_000, Index: index}
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateVideoSeekCandidate(string(data)); err == nil {
		t.Fatal("sub-tick future PTS was accepted after input rounding")
	}
}

func TestValidateVideoSeekCandidateRejectsUnknownFieldsAndTrailingInput(t *testing.T) {
	encoded, err := SelectVideoSeekCandidate(videoSeekCandidateTestIndex(10, 20), 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"", "null", "[]", `"candidate"`, encoded + `{}`,
		strings.TrimSuffix(encoded, "}") + `,"unexpected":true}`,
		strings.Replace(encoded, `"stream_index":0`, `"stream_index":0,"unexpected":true`, 1),
		strings.Replace(encoded, `"pts":10`, `"pts":10,"unexpected":true`, 1),
	} {
		if _, err := ValidateVideoSeekCandidate(invalid); err == nil {
			t.Errorf("malformed candidate was accepted: %q", invalid)
		}
	}
	padded := encoded + strings.Repeat(" ", MaxVideoSeekCandidateBytes-len(encoded))
	if _, err := ValidateVideoSeekCandidate(padded); err != nil {
		t.Fatalf("candidate at the byte limit was rejected: %v", err)
	}
	if _, err := ValidateVideoSeekCandidate(padded + " "); err == nil {
		t.Fatal("candidate above the byte limit was accepted")
	}
}

func TestVideoSeekTimeHelpersAvoidIntermediateOverflow(t *testing.T) {
	index := videoSeekCandidateTestIndex(1)
	index.FormatStartTicks = math.MaxInt64
	actual := VideoSeekRequestedTime(index, 1)
	want := new(big.Rat).SetFrac(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(TicksPerSecond))
	if actual.Cmp(want) != 0 {
		t.Fatalf("requested source time = %s; want %s", actual.RatString(), want.RatString())
	}
	index.TimeBaseNumerator = math.MaxInt64
	index.TimeBaseDenominator = 3
	actual = VideoSeekPointTime(index, VideoSeekPoint{PTS: 3})
	if actual.Cmp(new(big.Rat).SetInt64(math.MaxInt64)) != 0 {
		t.Fatalf("native source time overflowed: %s", actual.RatString())
	}
	index = videoSeekCandidateTestIndex(1)
	index.FormatStartTicks = math.MinInt64
	if _, err := videoSeekInputTicks(index, index.Entries[0]); err == nil {
		t.Fatal("unrepresentable relative input argument was accepted")
	}
}

func TestVideoSeekCandidateAttemptsUseOnlyBoundedIndexedPTSAndDTS(t *testing.T) {
	index := videoSeekCandidateTestIndex(100, 200, 300, 400, 500, 600)
	for position := range index.Entries {
		index.Entries[position].DTS = index.Entries[position].PTS - 20
	}
	candidate := VideoSeekCandidate{Version: VideoSeekIndexVersion, RequestedStartTicks: 650, InputSeekTicks: 600, Index: index}
	if got, want := videoSeekCandidateAttempts(candidate), []int64{600, 580, 500, 480, 400, 380, 300, 280}; !slices.Equal(got, want) {
		t.Fatalf("bounded indexed proposal order = %v; want %v", got, want)
	}
	for position := range index.Entries {
		index.Entries[position].DTS = index.Entries[position].PTS
	}
	candidate.Index = index
	if got, want := videoSeekCandidateAttempts(candidate), []int64{600, 500, 400, 300}; !slices.Equal(got, want) {
		t.Fatalf("equal PTS/DTS proposals were repeated: %v; want %v", got, want)
	}
	candidate.Index = videoSeekCandidateTestIndex(-1, 0, 1)
	candidate.Index.Entries[0].DTS = -2
	candidate.Index.Entries[1].DTS = -1
	candidate.Index.Entries[2].DTS = 0
	candidate.RequestedStartTicks = 1
	if got := videoSeekCandidateAttempts(candidate); !slices.Equal(got, []int64{1}) {
		t.Fatalf("nonpositive input proposals were retained: %v", got)
	}
}
