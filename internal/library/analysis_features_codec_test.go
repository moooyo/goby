package library

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisFeaturesCodecFixture() AnalysisFeatures {
	return AnalysisFeatures{
		ContentSHA256:                 strings.Repeat("0123456789abcdef", 4),
		AlgorithmProfile:              "analysis-profile-v1",
		AudioBoundaryUncertaintyTicks: introdetect.TicksPerSecond / 2,
		Audio: []introdetect.AudioSample{
			{StartTicks: 0, EndTicks: introdetect.TicksPerSecond, Fingerprint: 0x01020304},
			{StartTicks: introdetect.TicksPerSecond, EndTicks: 2 * introdetect.TicksPerSecond, Fingerprint: math.MaxUint32},
		},
		Visual: []introdetect.VisualSample{
			{Ticks: 0, Hash: math.MaxUint64, Contrast: 0},
			{Ticks: 2 * introdetect.TicksPerSecond, Hash: 0x123456789abcdef0, Contrast: 1000},
		},
	}
}

func TestAnalysisFeaturesCodecUsesVersionedCompactBinary(t *testing.T) {
	value := AnalysisFeatures{
		ContentSHA256: strings.Repeat("0", 64), AlgorithmProfile: "p",
		AudioBoundaryUncertaintyTicks: 0x010203,
		Audio:                         []introdetect.AudioSample{{StartTicks: 0, EndTicks: 0x01020304, Fingerprint: 0x89abcdef}},
		Visual:                        []introdetect.VisualSample{{Ticks: 0, Hash: 0xfedcba9876543210, Contrast: 1000}},
	}
	// The golden is independent of the codec's layout constants and exercises
	// little-endian fields, an all-zero SHA-256, and a visual hash's high bit.
	want, err := hex.DecodeString(
		"474146420100010001000000010000000302010000000000" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			"70" +
			"00000000000000000403020100000000efcdab89" +
			"00000000000000001032547698badcfee803")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := EncodeAnalysisFeatures(value, 2*introdetect.TicksPerSecond)
	if err != nil || !bytes.Equal(payload, want) {
		t.Fatalf("encoded bytes = %x, want %x, error = %v", payload, want, err)
	}
	decoded, err := DecodeAnalysisFeatures(want, 2*introdetect.TicksPerSecond)
	if err != nil || !reflect.DeepEqual(decoded, value) {
		t.Fatalf("golden decoded as %+v, error = %v", decoded, err)
	}
	if err := ValidateStoredAnalysisFeatures(want, 2*introdetect.TicksPerSecond); err != nil {
		t.Fatalf("golden rejected by stored validation: %v", err)
	}
}

func TestAnalysisFeaturesCodecRoundTripAndCanonicalEmptyEvidence(t *testing.T) {
	for _, evidence := range []string{"both", "audio only", "visual only", "nil", "empty"} {
		t.Run(evidence, func(t *testing.T) {
			value := analysisFeaturesCodecFixture()
			value.AlgorithmProfile = "profile-\u03b1-\U0001f3b5"
			switch evidence {
			case "audio only":
				value.Visual = nil
			case "visual only":
				value.Audio = nil
			case "nil":
				value.Audio, value.Visual = nil, nil
			case "empty":
				value.Audio, value.Visual = []introdetect.AudioSample{}, []introdetect.VisualSample{}
			}
			payload, err := EncodeAnalysisFeatures(value, 10*introdetect.TicksPerSecond)
			if err != nil {
				t.Fatal(err)
			}
			before := bytes.Clone(payload)
			decoded, err := DecodeAnalysisFeatures(payload, 10*introdetect.TicksPerSecond)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Audio == nil || decoded.Visual == nil {
				t.Fatal("decoded evidence slices must be nonnil")
			}
			if value.Audio == nil {
				value.Audio = []introdetect.AudioSample{}
			}
			if value.Visual == nil {
				value.Visual = []introdetect.VisualSample{}
			}
			if !reflect.DeepEqual(decoded, value) {
				t.Fatalf("round trip = %+v, want %+v", decoded, value)
			}
			if err := ValidateStoredAnalysisFeatures(payload, 10*introdetect.TicksPerSecond); err != nil {
				t.Fatalf("stored validation disagrees with decode: %v", err)
			}
			if !bytes.Equal(payload, before) {
				t.Fatal("reading the payload mutated its bytes")
			}
			again, err := EncodeAnalysisFeatures(decoded, 10*introdetect.TicksPerSecond)
			if err != nil || !bytes.Equal(again, payload) {
				t.Fatalf("re-encoding changed the snapshot, error = %v", err)
			}
		})
	}
}

func TestAnalysisFeaturesCodecAcceptsExactBounds(t *testing.T) {
	value := analysisFeaturesCodecFixture()
	value.AlgorithmProfile = strings.Repeat("\u03b1", 256)
	value.AudioBoundaryUncertaintyTicks = 30 * introdetect.TicksPerSecond
	value.Audio = make([]introdetect.AudioSample, 8192)
	value.Visual = make([]introdetect.VisualSample, 4096)
	for index := range value.Audio {
		value.Audio[index] = introdetect.AudioSample{StartTicks: int64(index), EndTicks: int64(index + 1), Fingerprint: uint32(index)}
	}
	for index := range value.Visual {
		value.Visual[index] = introdetect.VisualSample{Ticks: int64(index), Hash: uint64(index), Contrast: 1000}
	}
	payload, err := EncodeAnalysisFeatures(value, media.MaxAnalysisDurationTicks)
	if err != nil || len(payload) != 238136 || len(payload) > 256<<10 {
		t.Fatalf("maximum snapshot has %d bytes, error = %v", len(payload), err)
	}
	decoded, err := DecodeAnalysisFeatures(payload, media.MaxAnalysisDurationTicks)
	if err != nil || !reflect.DeepEqual(decoded, value) {
		t.Fatalf("maximum snapshot failed round trip, error = %v", err)
	}
	if err := ValidateStoredAnalysisFeatures(payload, media.MaxAnalysisDurationTicks); err != nil {
		t.Fatal(err)
	}
	for _, duration := range []int64{1, 2 * introdetect.TicksPerSecond, 600 * introdetect.TicksPerSecond, media.MaxAnalysisDurationTicks} {
		window := min(duration, 600*introdetect.TicksPerSecond)
		value.Audio = []introdetect.AudioSample{{StartTicks: max(0, window-2*introdetect.TicksPerSecond), EndTicks: window}}
		value.Visual = []introdetect.VisualSample{{Ticks: window - 1, Contrast: 1000}}
		payload, err := EncodeAnalysisFeatures(value, duration)
		if err != nil {
			t.Fatalf("duration %d rejected its legal boundary: %v", duration, err)
		}
		decoded, err := DecodeAnalysisFeatures(payload, duration)
		if err != nil || !reflect.DeepEqual(decoded, value) {
			t.Fatalf("duration %d boundary failed round trip: %v", duration, err)
		}
	}
}

func TestEncodeAnalysisFeaturesRejectsInvalidSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AnalysisFeatures)
	}{
		{"empty hash", func(v *AnalysisFeatures) { v.ContentSHA256 = "" }},
		{"short hash", func(v *AnalysisFeatures) { v.ContentSHA256 = strings.Repeat("a", 63) }},
		{"long hash", func(v *AnalysisFeatures) { v.ContentSHA256 = strings.Repeat("a", 65) }},
		{"uppercase hash", func(v *AnalysisFeatures) { v.ContentSHA256 = strings.Repeat("A", 64) }},
		{"mixed case hash", func(v *AnalysisFeatures) { v.ContentSHA256 = "A" + strings.Repeat("a", 63) }},
		{"nonhex hash", func(v *AnalysisFeatures) { v.ContentSHA256 = strings.Repeat("g", 64) }},
		{"empty profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "" }},
		{"blank profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = " \u2003 " }},
		{"long profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = strings.Repeat("p", 513) }},
		{"long UTF8 profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = strings.Repeat("\u03b1", 256) + "p" }},
		{"invalid UTF8 profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "profile\xff" }},
		{"NUL profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "profile\x00" }},
		{"newline profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "profile\n" }},
		{"DEL profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "profile\x7f" }},
		{"C1 profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "profile\u0085" }},
		{"negative uncertainty", func(v *AnalysisFeatures) { v.AudioBoundaryUncertaintyTicks = -1 }},
		{"excess uncertainty", func(v *AnalysisFeatures) { v.AudioBoundaryUncertaintyTicks = 30*introdetect.TicksPerSecond + 1 }},
		{"excess audio count", func(v *AnalysisFeatures) { v.Audio = make([]introdetect.AudioSample, 8193) }},
		{"excess visual count", func(v *AnalysisFeatures) { v.Visual = make([]introdetect.VisualSample, 4097) }},
		{"negative audio start", func(v *AnalysisFeatures) { v.Audio[0].StartTicks = -1 }},
		{"negative audio end", func(v *AnalysisFeatures) { v.Audio[0].EndTicks = -1 }},
		{"empty audio bin", func(v *AnalysisFeatures) { v.Audio[0].EndTicks = v.Audio[0].StartTicks }},
		{"backward audio bin", func(v *AnalysisFeatures) { v.Audio[1].EndTicks = v.Audio[1].StartTicks - 1 }},
		{"overlapping audio", func(v *AnalysisFeatures) { v.Audio[1].StartTicks-- }},
		{"unordered audio", func(v *AnalysisFeatures) { v.Audio[0], v.Audio[1] = v.Audio[1], v.Audio[0] }},
		{"long audio bin", func(v *AnalysisFeatures) {
			v.Audio = []introdetect.AudioSample{{EndTicks: 2*introdetect.TicksPerSecond + 1}}
		}},
		{"audio after duration", func(v *AnalysisFeatures) {
			v.Audio = []introdetect.AudioSample{{StartTicks: 10 * introdetect.TicksPerSecond, EndTicks: 10*introdetect.TicksPerSecond + 1}}
		}},
		{"negative visual tick", func(v *AnalysisFeatures) { v.Visual[0].Ticks = -1 }},
		{"duplicate visual ticks", func(v *AnalysisFeatures) { v.Visual[1].Ticks = v.Visual[0].Ticks }},
		{"unordered visual ticks", func(v *AnalysisFeatures) { v.Visual[0], v.Visual[1] = v.Visual[1], v.Visual[0] }},
		{"visual at duration", func(v *AnalysisFeatures) { v.Visual[1].Ticks = 10 * introdetect.TicksPerSecond }},
		{"visual after duration", func(v *AnalysisFeatures) { v.Visual[1].Ticks = 10*introdetect.TicksPerSecond + 1 }},
		{"excess contrast", func(v *AnalysisFeatures) { v.Visual[0].Contrast = 1001 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := analysisFeaturesCodecFixture()
			test.mutate(&value)
			if payload, err := EncodeAnalysisFeatures(value, 10*introdetect.TicksPerSecond); !errors.Is(err, ErrInvalidInput) || payload != nil {
				t.Fatalf("invalid snapshot returned %d bytes, error = %v", len(payload), err)
			}
		})
	}
}

func assertAnalysisFeaturesPayloadRejected(t *testing.T, payload []byte, duration int64) {
	t.Helper()
	decoded, err := DecodeAnalysisFeatures(payload, duration)
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(decoded, AnalysisFeatures{}) {
		t.Fatalf("invalid payload returned %+v, error = %v", decoded, err)
	}
	if err := ValidateStoredAnalysisFeatures(payload, duration); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stored validation accepted invalid payload, error = %v", err)
	}
}

func TestDecodeAnalysisFeaturesRejectsMalformedBinary(t *testing.T) {
	value := analysisFeaturesCodecFixture()
	duration := 10 * introdetect.TicksPerSecond
	payload, err := EncodeAnalysisFeatures(value, duration)
	if err != nil {
		t.Fatal(err)
	}
	for length := 0; length < len(payload); length++ {
		assertAnalysisFeaturesPayloadRejected(t, payload[:length], duration)
	}
	assertAnalysisFeaturesPayloadRejected(t, append(bytes.Clone(payload), 0), duration)
	assertAnalysisFeaturesPayloadRejected(t, make([]byte, 256<<10), duration)
	assertAnalysisFeaturesPayloadRejected(t, make([]byte, (256<<10)+1), duration)
	audioOffset := 56 + len(value.AlgorithmProfile)
	visualOffset := audioOffset + len(value.Audio)*20
	mutations := []struct {
		name   string
		mutate func([]byte)
	}{
		{"bad magic", func(p []byte) { p[0] = 'X' }},
		{"unknown version", func(p []byte) { binary.LittleEndian.PutUint16(p[4:6], 2) }},
		{"zero version", func(p []byte) { binary.LittleEndian.PutUint16(p[4:6], 0) }},
		{"empty profile", func(p []byte) { binary.LittleEndian.PutUint16(p[6:8], 0) }},
		{"long profile", func(p []byte) { binary.LittleEndian.PutUint16(p[6:8], 513) }},
		{"maximum profile declaration", func(p []byte) { binary.LittleEndian.PutUint16(p[6:8], math.MaxUint16) }},
		{"excess audio count", func(p []byte) { binary.LittleEndian.PutUint32(p[8:12], 8193) }},
		{"maximum audio declaration", func(p []byte) { binary.LittleEndian.PutUint32(p[8:12], math.MaxUint32) }},
		{"excess visual count", func(p []byte) { binary.LittleEndian.PutUint32(p[12:16], 4097) }},
		{"maximum visual declaration", func(p []byte) { binary.LittleEndian.PutUint32(p[12:16], math.MaxUint32) }},
		{"smaller audio declaration", func(p []byte) { binary.LittleEndian.PutUint32(p[8:12], 1) }},
		{"larger visual declaration", func(p []byte) { binary.LittleEndian.PutUint32(p[12:16], 3) }},
		{"invalid UTF8 profile", func(p []byte) { p[56] = 0xff }},
		{"NUL profile", func(p []byte) { p[56] = 0 }},
		{"DEL profile", func(p []byte) { p[56] = 0x7f }},
		{"C1 profile", func(p []byte) { p[56], p[57] = 0xc2, 0x85 }},
		{"blank profile", func(p []byte) { copy(p[56:audioOffset], strings.Repeat(" ", len(value.AlgorithmProfile))) }},
		{"excess contrast", func(p []byte) { binary.LittleEndian.PutUint16(p[visualOffset+16:visualOffset+18], 1001) }},
		{"maximum contrast", func(p []byte) { binary.LittleEndian.PutUint16(p[visualOffset+16:visualOffset+18], math.MaxUint16) }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			changed := bytes.Clone(payload)
			test.mutate(changed)
			assertAnalysisFeaturesPayloadRejected(t, changed, duration)
		})
	}
	fields := []struct {
		name   string
		offset int
		value  int64
	}{
		{"negative uncertainty", 16, -1},
		{"excess uncertainty", 16, 30*introdetect.TicksPerSecond + 1},
		{"negative audio start", audioOffset, -1},
		{"minimum audio start", audioOffset, math.MinInt64},
		{"negative audio end", audioOffset + 8, -1},
		{"empty audio bin", audioOffset + 8, 0},
		{"backward audio bin", audioOffset + 28, introdetect.TicksPerSecond - 1},
		{"overlapping audio", audioOffset + 20, introdetect.TicksPerSecond - 1},
		{"long audio bin", audioOffset + 28, 3*introdetect.TicksPerSecond + 1},
		{"audio after duration", audioOffset + 28, duration + 1},
		{"maximum audio end", audioOffset + 28, math.MaxInt64},
		{"negative visual tick", visualOffset, -1},
		{"minimum visual tick", visualOffset, math.MinInt64},
		{"duplicate visual tick", visualOffset + 18, 0},
		{"unordered visual ticks", visualOffset, 3 * introdetect.TicksPerSecond},
		{"visual at duration", visualOffset + 18, duration},
		{"visual after duration", visualOffset + 18, duration + 1},
		{"maximum visual tick", visualOffset + 18, math.MaxInt64},
	}
	for _, test := range fields {
		t.Run(test.name, func(t *testing.T) {
			changed := bytes.Clone(payload)
			binary.LittleEndian.PutUint64(changed[test.offset:test.offset+8], uint64(test.value))
			assertAnalysisFeaturesPayloadRejected(t, changed, duration)
		})
	}
}

func TestAnalysisFeaturesCodecRequiresBoundedSourceDuration(t *testing.T) {
	value := analysisFeaturesCodecFixture()
	payload, err := EncodeAnalysisFeatures(value, 10*introdetect.TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, duration := range []int64{0, -1, math.MinInt64, media.MaxAnalysisDurationTicks + 1, math.MaxInt64} {
		if encoded, err := EncodeAnalysisFeatures(value, duration); !errors.Is(err, ErrInvalidInput) || encoded != nil {
			t.Fatalf("duration %d returned %d bytes, error = %v", duration, len(encoded), err)
		}
		assertAnalysisFeaturesPayloadRejected(t, payload, duration)
	}
	assertAnalysisFeaturesPayloadRejected(t, payload, introdetect.TicksPerSecond)
}

func TestAnalysisFeaturesCodecEnforcesTenMinuteWindow(t *testing.T) {
	window := 600 * introdetect.TicksPerSecond
	for _, evidence := range []string{"audio", "visual"} {
		t.Run(evidence, func(t *testing.T) {
			value := analysisFeaturesCodecFixture()
			value.Audio = []introdetect.AudioSample{{StartTicks: window - 1, EndTicks: window}}
			value.Visual = []introdetect.VisualSample{{Ticks: window - 1}}
			payload, err := EncodeAnalysisFeatures(value, window+introdetect.TicksPerSecond)
			if err != nil {
				t.Fatal(err)
			}
			audioOffset := 56 + len(value.AlgorithmProfile)
			if evidence == "audio" {
				value.Audio[0].EndTicks = window + 1
				binary.LittleEndian.PutUint64(payload[audioOffset+8:audioOffset+16], uint64(window+1))
			} else {
				value.Visual[0].Ticks = window
				binary.LittleEndian.PutUint64(payload[audioOffset+20:audioOffset+28], uint64(window))
			}
			if encoded, err := EncodeAnalysisFeatures(value, media.MaxAnalysisDurationTicks); !errors.Is(err, ErrInvalidInput) || encoded != nil {
				t.Fatalf("out-of-window %s returned %d bytes, error = %v", evidence, len(encoded), err)
			}
			assertAnalysisFeaturesPayloadRejected(t, payload, media.MaxAnalysisDurationTicks)
		})
	}
}
