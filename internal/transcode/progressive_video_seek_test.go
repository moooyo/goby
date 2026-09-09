package transcode

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func videoSeekCandidatePlan(t *testing.T) Plan {
	t.Helper()
	p := progressiveVideoPlan()
	p.StartTicks = 23700000
	index := media.VideoSeekIndex{Version: media.VideoSeekIndexVersion, StreamIndex: p.VideoStreamIndex,
		DurationTicks: p.DurationTicks, TimeBaseNumerator: 1, TimeBaseDenominator: 1000,
		SourceIdentity: strings.Repeat("a", 64), ToolIdentity: strings.Repeat("b", 64), ParameterSetsSHA256: strings.Repeat("c", 64),
		PacketSideDataChecked: true, NALScopeChecked: true, Width: 160, Height: 90, PixelFormat: "yuv420p", DecodedFrameBytes: 21600,
		Entries: []media.VideoSeekPoint{
			{PTS: 0, DTS: -80, CodedSHA256: strings.Repeat("d", 64), DecodedSHA256: strings.Repeat("e", 64)},
			{PTS: 2000, DTS: 1920, CodedSHA256: strings.Repeat("f", 64), DecodedSHA256: strings.Repeat("1", 64)},
		}}
	var err error
	p.VideoSeekCandidate, err = media.SelectVideoSeekCandidate(index, p.StartTicks)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProgressiveVideoSeekCandidateDoesNotAuthorizeArgumentPreview(t *testing.T) {
	p := videoSeekCandidatePlan(t)
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if countVideoSeekArgument(args, "-i") != 1 || countVideoSeekArgument(args, "-ss") != 1 || slices.Index(args, "-ss") < slices.Index(args, "-i") ||
		slices.Contains(args, "-seek_timestamp") || !hasArgumentPair(args, "-map", "0:1") {
		t.Fatalf("a candidate must leave argument preview on the original linear path: %v", args)
	}
	if len(p.VideoSeekCandidate) > media.MaxVideoSeekCandidateBytes {
		t.Fatal("candidate exceeds the private bounded field")
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > 128*1024 {
		t.Fatalf("candidate must fit the existing comparable plan contract: %d, %v", len(encoded), err)
	}
	var roundtrip Plan
	if err := json.Unmarshal(encoded, &roundtrip); err != nil || roundtrip != p {
		t.Fatalf("plan lost comparable roundtrip equality: %v", err)
	}
}

func TestProgressiveVideoSeekPreflightOnlyCoversSoftwareDecoding(t *testing.T) {
	p := videoSeekCandidatePlan(t)
	for _, hardware := range []Hardware{{}, {Decode: "software"}, {Encode: "vaapi"}, {Encode: "qsv"}, {Encode: "nvenc"}} {
		p.Hardware = hardware
		if !progressiveVideoSeekPreflightEnabled(p) {
			t.Fatalf("software decoder evidence was disabled by independent encoding: %+v", hardware)
		}
	}
	for _, backend := range []string{"vaapi", "qsv", "cuda"} {
		p.Hardware = Hardware{Decode: backend}
		if progressiveVideoSeekPreflightEnabled(p) {
			t.Fatalf("software decoder evidence cannot authorize %s decoding", backend)
		}
	}
	p.Hardware, p.VideoSeekCandidate = Hardware{}, ""
	if progressiveVideoSeekPreflightEnabled(p) {
		t.Fatal("missing evidence must not start optional preflight")
	}
}

func TestProgressiveVideoVerifiedSeekUsesIndependentLinearAudio(t *testing.T) {
	p := videoSeekCandidatePlan(t)
	args := buildProgressiveVideoArgsWithSeek(p, 2, 20000000)
	if countVideoSeekArgument(args, "-i") != 2 || countVideoSeekArgument(args, "-ss") != 2 ||
		countVideoSeekArgument(args, "-itsoffset") != 2 || countVideoSeekArgument(args, "-protocol_whitelist") != 2 || countVideoSeekArgument(args, "-format_whitelist") != 2 ||
		!hasArgumentPair(args, "-seek_timestamp", "1") || !slices.Contains(args, "-noaccurate_seek") ||
		!hasArgumentPair(args, "-ss", "2.0000000") || !hasArgumentPair(args, "-ss", "2.3700000") ||
		!hasArgumentPair(args, "-map", "0:0") || !hasArgumentPair(args, "-map", "1:1") || !hasArgumentPair(args, "-discard:v", "all") {
		t.Fatalf("verified seek did not retain a separate linear audio input: %v", args)
	}
	firstInput, firstSeek := slices.Index(args, "-i"), slices.Index(args, "-ss")
	if firstSeek > firstInput || !hasArgumentPair(args, "-i", "/proc/self/fd/3") || args[len(args)-1] != "pipe:4" {
		t.Fatalf("verified input boundary or fixed file descriptors changed: %v", args)
	}
	if slices.Contains(args, "-af") || slices.Contains(args, "-start_at_zero") {
		t.Fatal("independent input clocks must retain the shared source origin")
	}
	// Copying audio has the same independent input contract. Its packet
	// framing conversion occurs after mapping the original selected index.
	p.AudioCodec, p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = "copy", 0, 0, 0
	args = buildProgressiveVideoArgsWithSeek(p, 1, 20000000)
	if countVideoSeekArgument(args, "-i") != 2 || !hasArgumentPair(args, "-map", "1:1") || !hasArgumentPair(args, "-bsf:a", "aac_adtstoasc") {
		t.Fatalf("audio copy lost its linear input: %v", args)
	}
	p.AudioCodec, p.AudioStreamIndex = "", -1
	args = buildProgressiveVideoArgsWithSeek(p, 1, 20000000)
	if countVideoSeekArgument(args, "-i") != 1 || !slices.Contains(args, "-an") || slices.Contains(args, "-discard:v") {
		t.Fatalf("video-only output must not introduce another input: %v", args)
	}
}

func TestProgressiveVideoSeekCandidateIsBoundToItsPlan(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"duration":     func(p *Plan) { p.DurationTicks++ },
		"origin":       func(p *Plan) { p.SourceFormatStartTicks++ },
		"request":      func(p *Plan) { p.StartTicks++ },
		"stream":       func(p *Plan) { p.VideoStreamIndex = 2 },
		"zero request": func(p *Plan) { p.StartTicks = 0 },
		"copy video": func(p *Plan) {
			p.VideoCodec, p.StartTicks, p.Width, p.Height, p.VideoBitrate = "copy", 0, 0, 0, 0
		},
		"invalid JSON": func(p *Plan) { p.VideoSeekCandidate = "{" },
		"whitespace":   func(p *Plan) { p.VideoSeekCandidate += "\n" },
		"excess bytes": func(p *Plan) { p.VideoSeekCandidate = strings.Repeat("x", media.MaxVideoSeekCandidateBytes+1) },
	} {
		t.Run(name, func(t *testing.T) {
			p := videoSeekCandidatePlan(t)
			mutate(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("mismatched seek candidate accepted: %v", err)
			}
		})
	}
	encoded := videoSeekCandidatePlan(t).VideoSeekCandidate
	for _, p := range []Plan{commandPlan(), progressivePlan("m4a", "aac")} {
		p.VideoSeekCandidate = encoded
		if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("unrelated output accepted video seek preparation data: %v", err)
		}
	}
}

func countVideoSeekArgument(args []string, target string) int {
	count := 0
	for _, value := range args {
		if value == target {
			count++
		}
	}
	return count
}
