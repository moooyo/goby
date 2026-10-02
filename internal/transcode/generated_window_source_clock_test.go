package transcode

import (
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func generatedNativeClockTestPlan(count int) Plan {
	p := generatedClosureTestFixture(false, false).plan
	p.HLS.Window.NativeClockVersion = GeneratedWindowNativeClockV1
	p.HLS.RenditionCount = count
	for index := 0; index < count; index++ {
		p.HLS.Renditions[index] = HLSRendition{Width: 160 >> index, Height: 96 >> index, VideoBitrate: []int64{256000, 128000, 96000, 64000}[index]}
	}
	return p
}

func generatedNativeClockArgumentValues(arguments []string, option string) []string {
	var values []string
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == option {
			values = append(values, arguments[index+1])
		}
	}
	return values
}

func TestGeneratedWindowNativeClockVersionPreservesLegacyIdentity(t *testing.T) {
	p := generatedNativeClockTestPlan(0)
	legacy := p
	legacy.HLS.Window.NativeClockVersion = 0
	legacyJSON, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(legacyJSON), "NativeClockVersion") {
		t.Fatal("version zero changed the serialized legacy plan")
	}
	nativeJSON, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(nativeJSON), `"NativeClockVersion":1`) || p == legacy {
		t.Fatal("native emission did not receive its own immutable plan identity")
	}
	for _, version := range []uint8{2, 255} {
		invalid := p
		invalid.HLS.Window.NativeClockVersion = version
		if err := ValidatePlan(invalid); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("an unknown source-clock version was accepted: version=%d, error=%v", version, err)
		}
	}
}

func TestGeneratedWindowNativeClockPlanRequiresExactSilentTSContract(t *testing.T) {
	for _, rate := range []float64{24, 25, 30, 60} {
		p := generatedNativeClockTestPlan(4)
		p.FrameRate = rate
		if !GeneratedWindowNativeClockEligible(p) || ValidatePlan(p) != nil {
			t.Fatalf("an exact integer TS cadence was rejected: rate=%v", rate)
		}
	}
	for name, mutate := range map[string]func(*Plan){
		"input proof absent": func(p *Plan) { p.HLS.Window.RequireInputEvidence = false },
		"fragmented MP4":     func(p *Plan) { p.Container, p.HLS.SegmentType = "mp4", "fmp4" },
		"copied input clock": func(p *Plan) {
			p.CopyTimestamps = true
		},
		"copied video":         func(p *Plan) { p.VideoCodec = "copy" },
		"audio stream":         func(p *Plan) { p.AudioStreamIndex, p.AudioCodec = 1, "aac" },
		"fractional cadence":   func(p *Plan) { p.FrameRate = 30000.0 / 1001.0 },
		"rounded TS cadence":   func(p *Plan) { p.FrameRate = 32 },
		"subtick start offset": func(p *Plan) { p.StartTicks++ },
		"subtick end offset":   func(p *Plan) { p.HLS.Window.EndTicks++ },
		"subtick native end":   func(p *Plan) { p.DurationTicks++ },
		"half cycle duration": func(p *Plan) {
			p.DurationTicks = (generatedWindowNativeClockHalfWrap*ticksPerSecond/generatedWindowNativeClockRate + 999) / 1000 * 1000
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := generatedNativeClockTestPlan(0)
			mutate(&p)
			if GeneratedWindowNativeClockEligible(p) || !errors.Is(ValidatePlan(p), ErrInvalidPlan) {
				t.Fatal("an unsupported emission contract entered native clock v1")
			}
		})
	}
}

func TestGeneratedWindowNativeClockArgsBindEveryActualOutput(t *testing.T) {
	for _, count := range []int{0, 2, 4} {
		p := generatedNativeClockTestPlan(count)
		arguments, err := BuildArgs(p, 1)
		if err != nil {
			t.Fatal(err)
		}
		outputs := max(1, count)
		for option, expected := range map[string]string{"-output_ts_offset": "90.0000000", "-avoid_negative_ts": "disabled",
			"-hls_segment_options": "mpegts_copyts=1:avoid_negative_ts=disabled"} {
			values := generatedNativeClockArgumentValues(arguments, option)
			if len(values) != outputs {
				t.Fatalf("native setting was not attached to every actual output: option=%s, values=%v", option, values)
			}
			for _, value := range values {
				if value != expected {
					t.Fatalf("another mux epoch entered native emission: option=%s, value=%s", option, value)
				}
			}
		}
		if slices.Contains(arguments, "-copyts") || slices.Contains(arguments, "-muxdelay") || len(generatedNativeClockArgumentValues(arguments, "-ss")) != 1 {
			t.Fatal("native output altered input seeking or introduced an unproved delay contract")
		}
		p.HLS.Window.NativeClockVersion = 0
		legacy, err := BuildArgs(p, 1)
		if err != nil || len(generatedNativeClockArgumentValues(legacy, "-output_ts_offset")) != 0 ||
			!reflect.DeepEqual(generatedNativeClockArgumentValues(legacy, "-avoid_negative_ts"), slices.Repeat([]string{"make_zero"}, outputs)) ||
			!reflect.DeepEqual(generatedNativeClockArgumentValues(legacy, "-hls_segment_options"), slices.Repeat([]string{"mpegts_copyts=1"}, outputs)) {
			t.Fatal("native clock v1 changed the version-zero mux contract")
		}
	}
}

func TestGeneratedWindowNativeClockAnchorRejectsAlternativePremuxEpoch(t *testing.T) {
	p := generatedNativeClockTestPlan(2)
	for index := 0; index < 2; index++ {
		clock := HLSMuxClock{Rendition: index, TimeBaseNumerator: 1, TimeBaseDenominator: 30}
		anchor, err := HLSWindowSourceAnchorTicks(p, clock)
		if err != nil || anchor.Cmp(new(big.Rat).SetInt64(p.StartTicks)) != 0 {
			t.Fatal("the observed native premux zero did not map the explicit output offset once")
		}
		for _, pts := range []int64{1, -1, 2700} {
			clock.PTS = pts
			if _, err := HLSWindowSourceAnchorTicks(p, clock); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("native emission guessed an alternative premux epoch: pts=%d error=%v", pts, err)
			}
		}
	}
	p.HLS.Window.NativeClockVersion = 0
	clock := HLSMuxClock{PTS: 3, TimeBaseNumerator: 1, TimeBaseDenominator: 24}
	anchor, err := HLSWindowSourceAnchorTicks(p, clock)
	if err != nil || anchor.Cmp(new(big.Rat).SetFrac64(721*ticksPerSecond, 8)) != 0 {
		t.Fatal("version zero lost its generic rational premux association")
	}
}

func generatedNativeClockShiftOutput(fixture *generatedClosureFixture, shift int64) {
	for index := 0; index < max(1, fixture.plan.HLS.RenditionCount); index++ {
		video := &fixture.output[index].Video
		video.FirstPTS += shift
		video.LastPTS += shift
		video.EndPTS += shift
		video.FirstDTS += shift
		video.LastDTS += shift
		video.EndDTS += shift
		video.FirstPacketPTS += shift
		video.LastPacketPTS += shift
	}
}

func TestGeneratedWindowNativeClockClosureRequiresActualGlobalBounds(t *testing.T) {
	fixture := generatedClosureTestFixture(true, false)
	fixture.plan.HLS.Window.NativeClockVersion = GeneratedWindowNativeClockV1
	generatedNativeClockShiftOutput(&fixture, 90*generatedWindowNativeClockRate-fixture.output[0].Video.FirstPTS)
	closure, err := fixture.validate()
	if err != nil || closure.NativeClockVersion != GeneratedWindowNativeClockV1 || closure.Source != fixture.source {
		t.Fatalf("observed source-global output lost its native closure version: %+v, %v", closure, err)
	}
	for name, mutate := range map[string]func(*generatedClosureFixture){
		"one TS tick shift": func(f *generatedClosureFixture) { generatedNativeClockShiftOutput(f, 1) },
		"source format origin added": func(f *generatedClosureFixture) {
			generatedNativeClockShiftOutput(f, 2*generatedWindowNativeClockRate)
		},
		"generic local epoch": func(f *generatedClosureFixture) {
			generatedNativeClockShiftOutput(f, 126000-90*generatedWindowNativeClockRate)
		},
		"premux guessed source epoch": func(f *generatedClosureFixture) { f.mux[0].PTS = 2700 },
	} {
		t.Run(name, func(t *testing.T) {
			mutated := fixture
			mutate(&mutated)
			if _, err := mutated.validate(); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatal("a nominal offset authorized a different actual output clock")
			}
		})
	}
	legacy := generatedClosureTestFixture(true, false)
	if closure, err := legacy.validate(); err != nil || closure.NativeClockVersion != 0 {
		t.Fatal("version zero stopped accepting its independently proved generic container epoch")
	}
}
