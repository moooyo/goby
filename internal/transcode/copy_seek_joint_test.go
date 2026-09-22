package transcode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func jointVideoCopySeekPlan() (Plan, media.Info) {
	plan := Plan{OutputMode: "progressive", Container: "mp4", VideoStreamIndex: 0, AudioStreamIndex: 1,
		VideoCodec: "copy", AudioCodec: "copy", StartTicks: 30 * ticksPerSecond, DurationTicks: 120 * ticksPerSecond,
		SourceFormatStartKnown: true, CopyTimestamps: true}
	audio := media.VideoCopySeekAudio{StreamIndex: 1, Codec: "aac", TimeBaseNumerator: 1, TimeBaseDenominator: 48000,
		SampleRate: 48000, Channels: 1, PTS: 1152000, Duration: 1024, PacketSHA256: strings.Repeat("f", 64)}
	index := media.VideoSeekIndex{Version: media.VideoSeekIndexVersion, StreamIndex: 0, DurationTicks: plan.DurationTicks,
		TimeBaseNumerator: 1, TimeBaseDenominator: 12288, SourceIdentity: strings.Repeat("a", 64),
		ToolIdentity: strings.Repeat("b", 64), ParameterSetsSHA256: strings.Repeat("c", 64),
		PacketSideDataChecked: true, NALScopeChecked: true, Width: 128, Height: 72, PixelFormat: "yuv420p", DecodedFrameBytes: 13824,
		Entries: []media.VideoSeekPoint{
			{PTS: 294912, DTS: 294912, CodedSHA256: strings.Repeat("d", 64), DecodedSHA256: strings.Repeat("e", 64), Audio: []media.VideoCopySeekAudio{audio}},
			{PTS: 368640, DTS: 368640, CodedSHA256: strings.Repeat("1", 64), DecodedSHA256: strings.Repeat("2", 64)},
		}}
	info := media.Info{ProbeVersion: media.CurrentProbeVersion, FormatStartKnown: true, DurationTicks: plan.DurationTicks,
		Streams: []media.Stream{
			{Index: 0, CodecType: "video", Codec: "h264", Profile: "Constrained Baseline", Width: 128, Height: 72,
				PixelFormat: "yuv420p", TimeBase: "1/12288"},
			{Index: 1, CodecType: "audio", Codec: "aac", Profile: "LC", SampleRate: 48000, Channels: 1, TimeBase: "1/48000"},
		}, VideoSeekIndexes: []media.VideoSeekIndex{index}}
	return plan, info
}

func TestVideoCopySeekAlignmentSkipsNearerVideoOnlyPoint(t *testing.T) {
	plan, info := jointVideoCopySeekPlan()
	before, _ := json.Marshal(info)
	if !AttachVideoCopySeekCandidateAligned(&plan, info, 10*ticksPerSecond) {
		t.Fatal("a nearer video-only point hid the preceding shared packet boundary")
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(plan.VideoCopySeekCandidate)
	if err != nil || plan.StartTicks != 24*ticksPerSecond || !plan.CopyTimestamps ||
		plan.VideoCodec != "copy" || plan.AudioCodec != "copy" ||
		candidate.RequestedStartTicks != 24*ticksPerSecond || candidate.OriginalRequestedStartTicks != 30*ticksPerSecond ||
		!candidate.CopyTimestamps || len(candidate.Index.Entries) != 1 || candidate.Index.Entries[0].PTS != 294912 ||
		candidate.Audio == nil || candidate.Audio.StreamIndex != 1 || candidate.Audio.PTS != 1152000 {
		t.Fatalf("joint alignment lost the requested clock or copied packet proofs: %+v, %+v, %v", plan, candidate, err)
	}
	after, _ := json.Marshal(info)
	if string(before) != string(after) {
		t.Fatal("joint selection mutated retained source evidence")
	}
	args := buildProgressiveVideoArgsWithSeek(plan, 1, plan.StartTicks)
	if countVideoSeekArgument(args, "-i") != 1 || !hasArgumentPair(args, "-c:v", "copy") ||
		!hasArgumentPair(args, "-c:a", "copy") || !hasArgumentPair(args, "-output_ts_offset", "24.0000000") {
		t.Fatalf("joint remux introduced encoding or changed its native output clock: %v", args)
	}
}

func TestVideoCopySeekJointAlignmentKeepsExactWindowAndEvidenceGuards(t *testing.T) {
	for _, test := range []struct {
		name   string
		window int64
		mutate func(*Plan, *media.Info)
	}{
		{name: "exact video point without audio", window: 0},
		{name: "shared point outside requested window", window: 6*ticksPerSecond - 1},
		{name: "shared point outside ten seconds", window: 10 * ticksPerSecond, mutate: func(p *Plan, _ *media.Info) { p.StartTicks = 35 * ticksPerSecond }},
		{name: "excess alignment window", window: 10*ticksPerSecond + 1},
		{name: "unmatched audio source", window: 10 * ticksPerSecond, mutate: func(_ *Plan, info *media.Info) { info.Streams[1].Channels = 2 }},
		{name: "invalid skipped point", window: 10 * ticksPerSecond, mutate: func(_ *Plan, info *media.Info) { info.VideoSeekIndexes[0].Entries[1].CodedSHA256 = "invalid" }},
		{name: "mixed source index", window: 10 * ticksPerSecond, mutate: func(_ *Plan, info *media.Info) {
			other := info.VideoSeekIndexes[0]
			other.StreamIndex, other.SourceIdentity = 3, strings.Repeat("9", 64)
			info.VideoSeekIndexes = append(info.VideoSeekIndexes, other)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, info := jointVideoCopySeekPlan()
			if test.mutate != nil {
				test.mutate(&plan, &info)
			}
			before := plan
			if AttachVideoCopySeekCandidateAligned(&plan, info, test.window) || plan != before {
				t.Fatal("an unavailable or unproven joint boundary changed the plan")
			}
		})
	}
}

func TestVideoCopySeekJointSelectionKeepsExactAndVideoOnlyBoundaries(t *testing.T) {
	plan, info := jointVideoCopySeekPlan()
	plan.StartTicks = 24 * ticksPerSecond
	if !AttachVideoCopySeekCandidate(&plan, info) || plan.StartTicks != 24*ticksPerSecond {
		t.Fatal("an exact shared boundary unexpectedly required alignment")
	}
	plan, info = jointVideoCopySeekPlan()
	plan.AudioCodec, plan.AudioStreamIndex = "", -1
	if !AttachVideoCopySeekCandidate(&plan, info) || plan.StartTicks != 30*ticksPerSecond {
		t.Fatal("video-only seeking was moved to an unnecessary audio boundary")
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(plan.VideoCopySeekCandidate)
	if err != nil || candidate.Audio != nil || candidate.OriginalRequestedStartTicks != 0 {
		t.Fatalf("exact video-only seeking acquired an audio or alignment claim: %+v, %v", candidate, err)
	}
}
