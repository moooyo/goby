package transcode

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func videoCopySeekPlan(t *testing.T) (Plan, media.Info) {
	t.Helper()
	p := progressiveVideoPlan()
	p.VideoCodec, p.Width, p.Height, p.VideoBitrate = "copy", 0, 0, 0
	p.StartTicks = 20_000_000
	index := media.VideoSeekIndex{Version: media.VideoSeekIndexVersion, StreamIndex: p.VideoStreamIndex,
		FormatStartTicks: p.SourceFormatStartTicks, DurationTicks: p.DurationTicks, TimeBaseNumerator: 1, TimeBaseDenominator: ticksPerSecond,
		SourceIdentity: strings.Repeat("a", 64), ToolIdentity: strings.Repeat("b", 64), ParameterSetsSHA256: strings.Repeat("e", 64),
		PacketSideDataChecked: true, NALScopeChecked: true, Width: 64, Height: 64, PixelFormat: "yuv420p", DecodedFrameBytes: 6144,
		Entries: []media.VideoSeekPoint{{PTS: p.StartTicks, DTS: p.StartTicks, CodedSHA256: strings.Repeat("c", 64), DecodedSHA256: strings.Repeat("d", 64)}}}
	info := media.Info{ProbeVersion: media.CurrentProbeVersion, FormatStartKnown: true, FormatStartTicks: p.SourceFormatStartTicks, DurationTicks: p.DurationTicks,
		Streams: []media.Stream{{Index: p.VideoStreamIndex, CodecType: "video", Codec: "h264", Width: 64, Height: 64,
			PixelFormat: "yuv420p", TimeBase: "1/10000000"}},
		VideoSeekIndexes: []media.VideoSeekIndex{index}}
	if !AttachVideoCopySeekCandidate(&p, info) {
		t.Fatal("exact indexed packet candidate was not attached")
	}
	return p, info
}

func TestVideoCopySeekPlanKeepsIndependentLinearAACInput(t *testing.T) {
	p, _ := videoCopySeekPlan(t)
	preview, err := BuildArgs(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	if countVideoSeekArgument(preview, "-i") != 1 || slices.Contains(preview, "-seek_timestamp") {
		t.Fatal("argument preview cannot authorize an input seek")
	}
	args := buildProgressiveVideoArgsWithSeek(p, 2, p.StartTicks)
	if countVideoSeekArgument(args, "-i") != 2 || countVideoSeekArgument(args, "-ss") != 2 ||
		!hasArgumentPair(args, "-c:v", "copy") || !hasArgumentPair(args, "-c:a", "aac") ||
		!hasArgumentPair(args, "-map", "1:1") || !hasArgumentPair(args, "-discard:v", "all") ||
		strings.Contains(strings.Join(args, " "), "asetpts") || slices.Contains(args, "-copyinkf") {
		t.Fatalf("copy seek changed the exact audio trim or allowed leading pictures: %v", args)
	}
	p.AudioStreamIndex, p.AudioCodec = -1, ""
	p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = 0, 0, 0
	if err := ValidatePlan(p); err != nil {
		t.Fatal(err)
	}
	args = buildProgressiveVideoArgsWithSeek(p, 1, p.StartTicks)
	if countVideoSeekArgument(args, "-i") != 1 || !slices.Contains(args, "-an") {
		t.Fatalf("explicitly disabled audio unexpectedly reopened an input: %v", args)
	}
}

func TestVideoCopySeekCandidateCannotAuthorizeDifferentPlans(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"no evidence":      func(p *Plan) { p.VideoCopySeekCandidate = "" },
		"requested time":   func(p *Plan) { p.StartTicks++ },
		"stream":           func(p *Plan) { p.VideoStreamIndex = 2 },
		"source clock":     func(p *Plan) { p.SourceFormatStartTicks++ },
		"source duration":  func(p *Plan) { p.DurationTicks++ },
		"zero start":       func(p *Plan) { p.StartTicks = 0 },
		"encoding":         func(p *Plan) { p.VideoCodec = "h264" },
		"whitespace":       func(p *Plan) { p.VideoCopySeekCandidate += "\n" },
		"unverified audio": func(p *Plan) { p.AudioCodec, p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = "copy", 0, 0, 0 },
		"decoded proof":    func(p *Plan) { p.VideoSeekCandidate = p.VideoCopySeekCandidate },
		"HLS":              func(p *Plan) { p.OutputMode, p.Container, p.SegmentSeconds = "", "ts", 2 },
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := videoCopySeekPlan(t)
			mutate(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("invalid copy proof plan returned %v", err)
			}
		})
	}
}

func TestVideoCopySeekAttachFailurePreservesPlan(t *testing.T) {
	p, info := videoCopySeekPlan(t)
	p.VideoCopySeekCandidate = ""
	p.StartTicks++
	before := p
	if AttachVideoCopySeekCandidate(&p, info) || p != before {
		t.Fatal("an unproven boundary must leave the caller free to choose encoding")
	}
	p.StartTicks--
	info.VideoSeekIndexes[0].Entries[0].DTS--
	before = p
	if AttachVideoCopySeekCandidate(&p, info) || p != before {
		t.Fatal("decoder pre-roll was incorrectly authorized for packet copying")
	}
}
