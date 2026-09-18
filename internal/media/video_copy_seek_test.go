package media

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func videoCopySeekTestCandidate(t *testing.T) (string, VideoCopySeekCandidate) {
	t.Helper()
	index := videoSeekCandidateTestIndex(0, 20_000_000, 40_000_000)
	encoded, err := SelectVideoCopySeekCandidate(index, 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, candidate
}

func TestVideoCopySeekCandidateRequiresAnExactIDRWithoutPreroll(t *testing.T) {
	index := videoSeekCandidateTestIndex(0, 20_000_000, 40_000_000)
	for _, start := range []int64{0, 19_999_999, 20_000_001, index.DurationTicks} {
		if _, err := SelectVideoCopySeekCandidate(index, start); err == nil {
			t.Fatalf("unproven boundary %d was accepted", start)
		}
	}
	encoded, candidate := videoCopySeekTestCandidate(t)
	if len(candidate.Index.Entries) != 1 || candidate.Index.Entries[0].PTS != 20_000_000 || len(encoded) > MaxVideoCopySeekCandidateBytes {
		t.Fatalf("unexpected bounded copy candidate: %+v", candidate)
	}
	index.Entries[1].DTS--
	if _, err := SelectVideoCopySeekCandidate(index, 20_000_000); err == nil {
		t.Fatal("a reordered IDR cannot authorize stream-copy pre-roll")
	}
	index = videoSeekCandidateTestIndex(1)
	index.TimeBaseDenominator = 3
	if _, err := SelectVideoCopySeekCandidate(index, 3_333_333); err == nil {
		t.Fatal("a rounded native timestamp cannot become an exact copy boundary")
	}
}

func TestVideoCopySeekCandidatePreservesSignedFormatClock(t *testing.T) {
	index := videoSeekCandidateTestIndex(-10_000_000)
	index.FormatStartTicks = -30_000_000
	encoded, err := SelectVideoCopySeekCandidate(index, 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	args, err := BuildVideoCopySeekCommandArgs(encoded, 2)
	if err != nil {
		t.Fatal(err)
	}
	var seeks []string
	for i, arg := range args {
		if arg == "-ss" {
			seeks = append(seeks, args[i+1])
		}
	}
	if !slices.Equal(seeks, []string{"-1.0000000", "2.0000000"}) || !strings.Contains(strings.Join(args, " "), "-itsoffset 3.0000000") {
		t.Fatalf("copy proof lost the shared source clock: %v", args)
	}
}

func TestVideoCopySeekCatalogSelectionRejectsStaleMixedAndOversizedEvidence(t *testing.T) {
	_, candidate := videoCopySeekTestCandidate(t)
	base := Info{ProbeVersion: CurrentProbeVersion, FormatStartKnown: true, DurationTicks: candidate.Index.DurationTicks,
		Streams: []Stream{{Index: 0, CodecType: "video", Codec: "h264", Width: 64, Height: 64,
			PixelFormat: "yuv420p", TimeBase: "1/10000000"}}, VideoSeekIndexes: []VideoSeekIndex{candidate.Index}}
	if _, err := SelectVideoCopySeekCandidateForInfo(base, 0, candidate.RequestedStartTicks); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Info){
		"old probe version": func(info *Info) { info.ProbeVersion-- },
		"missing clock":     func(info *Info) { info.FormatStartKnown = false },
		"duplicate index":   func(info *Info) { info.VideoSeekIndexes = append(info.VideoSeekIndexes, info.VideoSeekIndexes[0]) },
		"mixed source": func(info *Info) {
			other := info.VideoSeekIndexes[0]
			other.StreamIndex, other.SourceIdentity = 2, strings.Repeat("9", 64)
			info.VideoSeekIndexes = append(info.VideoSeekIndexes, other)
		},
		"mixed tool": func(info *Info) {
			other := info.VideoSeekIndexes[0]
			other.StreamIndex, other.ToolIdentity = 2, strings.Repeat("9", 64)
			info.VideoSeekIndexes = append(info.VideoSeekIndexes, other)
		},
		"too many indexes":   func(info *Info) { info.VideoSeekIndexes = make([]VideoSeekIndex, maxVideoCopySeekSourceStreams+1) },
		"too many entries":   func(info *Info) { info.VideoSeekIndexes[0].Entries = make([]VideoSeekPoint, MaxVideoSeekEntries+1) },
		"source duration":    func(info *Info) { info.DurationTicks++ },
		"source origin":      func(info *Info) { info.FormatStartTicks++ },
		"external video":     func(info *Info) { info.Streams[0].IsExternal = true },
		"attached picture":   func(info *Info) { info.Streams[0].IsAttachedPicture = true },
		"wrong codec":        func(info *Info) { info.Streams[0].Codec = "hevc" },
		"wrong dimensions":   func(info *Info) { info.Streams[0].Width++ },
		"wrong pixel format": func(info *Info) { info.Streams[0].PixelFormat = "yuv420p10le" },
		"wrong time base":    func(info *Info) { info.Streams[0].TimeBase = "1/90000" },
		"duplicate stream":   func(info *Info) { info.Streams = append(info.Streams, info.Streams[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			info := base
			info.Streams = append([]Stream(nil), base.Streams...)
			info.VideoSeekIndexes = append([]VideoSeekIndex(nil), base.VideoSeekIndexes...)
			mutate(&info)
			if _, err := SelectVideoCopySeekCandidateForInfo(info, 0, candidate.RequestedStartTicks); err == nil {
				t.Fatal("inconsistent or unbounded catalog evidence was accepted")
			}
		})
	}
}

func TestVideoCopySeekRejectsDecoderCandidatesAndNoncanonicalData(t *testing.T) {
	encoded, candidate := videoCopySeekTestCandidate(t)
	decoded, err := SelectVideoSeekCandidate(candidate.Index, candidate.RequestedStartTicks)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{decoded, encoded + "\n", strings.Replace(encoded, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(encoded, `"version":1`, `"version":2`, 1), strings.Repeat("x", MaxVideoCopySeekCandidateBytes+1)} {
		if _, err := ValidateVideoCopySeekCandidate(value); err == nil {
			t.Fatal("unproven or noncanonical copy candidate was accepted")
		}
	}
	for _, threads := range []int{0, MaxVideoSeekDecoderThreads + 1} {
		if _, err := BuildVideoCopySeekCommandArgs(encoded, threads); err == nil {
			t.Fatal("invalid proof thread count was accepted")
		}
	}
	candidate.Index.Entries[0].DTS--
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateVideoCopySeekCandidate(string(data)); err == nil {
		t.Fatal("a mutated decode timestamp was accepted")
	}
}

func videoCopySeekTestHash(candidate VideoCopySeekCandidate) string {
	var output strings.Builder
	output.WriteString("#format: frame checksums\n#version: 2\n#hash: SHA256\n")
	for number := range 2 {
		fmt.Fprintf(&output, "#tb %d: %d/%d\n#media_type %d: video\n#codec_id %d: h264\n#dimensions %d: %dx%d\n",
			number, candidate.Index.TimeBaseNumerator, candidate.Index.TimeBaseDenominator, number, number, number,
			candidate.Index.Width, candidate.Index.Height)
	}
	fmt.Fprintf(&output, "0, 0, 0, 1, 128, %s\n1, 0, 0, 1, 64, %s\n", strings.Repeat("f", 64), candidate.Index.Entries[0].CodedSHA256)
	return output.String()
}

func TestVideoCopySeekProofChecksActualFirstCopiedPacket(t *testing.T) {
	_, candidate := videoCopySeekTestCandidate(t)
	valid := videoCopySeekTestHash(candidate)
	if err := parseVideoCopySeekProof(strings.NewReader(valid), candidate); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"presentation pre-roll": strings.Replace(valid, "0, 0, 0, 1", "0, 0, -1, 1", 1),
		"decode pre-roll":       strings.Replace(valid, "0, 0, 0, 1", "0, -1, 0, 1", 1),
		"later first packet":    strings.Replace(valid, "0, 0, 0, 1", "0, 1, 1, 1", 1),
		"later IDR":             strings.Replace(valid, "1, 0, 0, 1", "1, 1, 1, 1", 1),
		"zero duration":         strings.Replace(valid, "0, 0, 0, 1", "0, 0, 0, 0", 1),
		"different IDR":         strings.Replace(valid, candidate.Index.Entries[0].CodedSHA256, strings.Repeat("a", 64), 1),
		"missing packet":        valid[:strings.LastIndex(valid, "1, 0, 0, 1")],
		"extra packet":          valid + "0, 1, 1, 1, 128, " + strings.Repeat("f", 64) + "\n",
		"different time base":   strings.Replace(valid, "#tb 1: 1/10000000", "#tb 1: 1/90000", 1),
		"decoded branch":        strings.Replace(valid, "#codec_id 1: h264", "#codec_id 1: rawvideo", 1),
		"unknown side data":     strings.Replace(valid, "128, "+strings.Repeat("f", 64), "128, "+strings.Repeat("f", 64)+", S=1, 8, "+strings.Repeat("a", 64), 1),
		"truncated output":      strings.TrimSuffix(valid, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := parseVideoCopySeekProof(strings.NewReader(output), candidate); err == nil {
				t.Fatal("incomplete copied-packet evidence was accepted")
			}
		})
	}
}
