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
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/media"
)

func analysisRawFeaturesFixture() AnalysisFeatures {
	return AnalysisFeatures{
		ContentSHA256:         strings.Repeat("0", 64),
		AlgorithmProfile:      "p",
		RawFingerprint:        []uint32{0, 0x01020304, math.MaxUint32, 0xdeadbeef},
		FingerprintEndSeconds: 1.5,
	}
}

func TestAnalysisFeaturesCodecPreservesRawSequenceAndHorizon(t *testing.T) {
	value := analysisRawFeaturesFixture()
	// Independent bytes preserve zero words, high bits, point ordering, and the
	// binary64 horizon without converting the sequence into timestamped bins.
	want, err := hex.DecodeString("4741464204000100" + strings.Repeat("00", 16) +
		strings.Repeat("00", 32) + "0000000004000000000000000000f83f" +
		"70" + "0000000004030201ffffffffefbeadde")
	if err != nil {
		t.Fatal(err)
	}
	duration := 2 * introskipper.TicksPerSecond
	payload, err := EncodeAnalysisFeatures(value, duration)
	if err != nil || !bytes.Equal(payload, want) {
		t.Fatalf("version 4 wire differs from golden: error=%v size=%d", err, len(payload))
	}
	before := bytes.Clone(want)
	decoded, err := DecodeAnalysisFeatures(want, duration)
	if err != nil || !reflect.DeepEqual(decoded, value) {
		t.Fatalf("version 4 golden failed round trip: %+v %v", decoded, err)
	}
	if err := ValidateStoredAnalysisFeatures(want, duration); err != nil || !bytes.Equal(before, want) {
		t.Fatalf("version 4 validation changed or rejected the payload: %v", err)
	}
	if decoded.Audio != nil || decoded.Visual != nil || decoded.Refinement != nil || decoded.AudioBoundaryUncertaintyTicks != 0 {
		t.Fatal("raw fingerprints acquired legacy feature evidence")
	}
	decoded.RawFingerprint[0] = 1
	if !bytes.Equal(before, want) {
		t.Fatal("decoded raw fingerprint aliases the encoded payload")
	}
	value.FingerprintEndSeconds = math.Nextafter(1.5, 2)
	payload, err = EncodeAnalysisFeatures(value, duration)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeAnalysisFeatures(payload, duration)
	if err != nil || math.Float64bits(decoded.FingerprintEndSeconds) != math.Float64bits(value.FingerprintEndSeconds) {
		t.Fatalf("binary64 extraction horizon lost precision: %v", err)
	}
	for length := 0; length < len(want); length++ {
		assertAnalysisFeaturesPayloadRejected(t, want[:length], duration)
	}
	assertAnalysisFeaturesPayloadRejected(t, append(bytes.Clone(want), 0), duration)
}

func TestAnalysisFeaturesCodecRejectsInvalidRawDeclarations(t *testing.T) {
	value := analysisRawFeaturesFixture()
	duration := 2 * introskipper.TicksPerSecond
	payload, err := EncodeAnalysisFeatures(value, duration)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func([]byte)
	}{
		{"legacy audio", func(p []byte) { binary.LittleEndian.PutUint32(p[8:12], 1) }},
		{"legacy visual", func(p []byte) { binary.LittleEndian.PutUint32(p[12:16], 1) }},
		{"legacy uncertainty", func(p []byte) { binary.LittleEndian.PutUint64(p[16:24], 1) }},
		{"legacy refinement", func(p []byte) { binary.LittleEndian.PutUint32(p[56:60], 1) }},
		{"zero count", func(p []byte) { binary.LittleEndian.PutUint32(p[60:64], 0) }},
		{"smaller count", func(p []byte) { binary.LittleEndian.PutUint32(p[60:64], 3) }},
		{"larger count", func(p []byte) { binary.LittleEndian.PutUint32(p[60:64], 5) }},
		{"excess count", func(p []byte) { binary.LittleEndian.PutUint32(p[60:64], introskipper.MaxFingerprintPoints+1) }},
		{"maximum count", func(p []byte) { binary.LittleEndian.PutUint32(p[60:64], math.MaxUint32) }},
		{"empty profile", func(p []byte) { binary.LittleEndian.PutUint16(p[6:8], 0) }},
		{"long profile", func(p []byte) { binary.LittleEndian.PutUint16(p[6:8], 513) }},
		{"invalid UTF8 profile", func(p []byte) { p[72] = 0xff }},
		{"control profile", func(p []byte) { p[72] = 0 }},
		{"blank profile", func(p []byte) { p[72] = ' ' }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			changed := bytes.Clone(payload)
			test.mutate(changed)
			assertAnalysisFeaturesPayloadRejected(t, changed, duration)
		})
	}
	for _, horizon := range []float64{0, math.Copysign(0, -1), -1, math.NaN(), math.Inf(1), math.Inf(-1), math.Nextafter(2, 3), math.MaxFloat64} {
		changed := bytes.Clone(payload)
		binary.LittleEndian.PutUint64(changed[64:72], math.Float64bits(horizon))
		assertAnalysisFeaturesPayloadRejected(t, changed, duration)
		current := value
		current.FingerprintEndSeconds = horizon
		if encoded, err := EncodeAnalysisFeatures(current, duration); !errors.Is(err, ErrInvalidInput) || encoded != nil {
			t.Fatalf("invalid raw horizon %g returned %d bytes, error=%v", horizon, len(encoded), err)
		}
	}
	for _, duration := range []int64{0, -1, math.MinInt64, introskipper.TicksPerSecond, media.MaxAnalysisDurationTicks + 1, math.MaxInt64} {
		assertAnalysisFeaturesPayloadRejected(t, payload, duration)
		if encoded, err := EncodeAnalysisFeatures(value, duration); !errors.Is(err, ErrInvalidInput) || encoded != nil {
			t.Fatalf("invalid raw duration %d returned %d bytes, error=%v", duration, len(encoded), err)
		}
	}
}

func TestAnalysisFeaturesCodecRejectsMixedAndIncompleteRawEvidence(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*AnalysisFeatures)
	}{
		{"legacy audio", func(v *AnalysisFeatures) { v.Audio = []introdetect.AudioSample{{StartTicks: 0, EndTicks: 1}} }},
		{"legacy visual", func(v *AnalysisFeatures) { v.Visual = []introdetect.VisualSample{{Ticks: 0}} }},
		{"legacy refinement", func(v *AnalysisFeatures) { v.Refinement = []introdetect.RefinementSample{{Ticks: 0}} }},
		{"legacy uncertainty", func(v *AnalysisFeatures) { v.AudioBoundaryUncertaintyTicks = 1 }},
		{"missing words", func(v *AnalysisFeatures) { v.RawFingerprint = nil }},
		{"empty words", func(v *AnalysisFeatures) { v.RawFingerprint = []uint32{} }},
		{"missing horizon", func(v *AnalysisFeatures) { v.FingerprintEndSeconds = 0 }},
		{"excess words", func(v *AnalysisFeatures) { v.RawFingerprint = make([]uint32, introskipper.MaxFingerprintPoints+1) }},
		{"uppercase hash", func(v *AnalysisFeatures) { v.ContentSHA256 = strings.Repeat("A", 64) }},
		{"invalid hash", func(v *AnalysisFeatures) { v.ContentSHA256 = strings.Repeat("g", 64) }},
		{"short hash", func(v *AnalysisFeatures) { v.ContentSHA256 = "0" }},
		{"invalid profile", func(v *AnalysisFeatures) { v.AlgorithmProfile = "\x00" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			value := analysisRawFeaturesFixture()
			test.mutate(&value)
			if encoded, err := EncodeAnalysisFeatures(value, 2*introskipper.TicksPerSecond); !errors.Is(err, ErrInvalidInput) || encoded != nil {
				t.Fatalf("invalid raw features returned %d bytes, error=%v", len(encoded), err)
			}
		})
	}
}

func TestAnalysisFeaturesCodecBoundsRawWindowAndCounts(t *testing.T) {
	value := analysisRawFeaturesFixture()
	value.RawFingerprint = make([]uint32, introskipper.MaxFingerprintPoints)
	value.FingerprintEndSeconds = 600
	payload, err := EncodeAnalysisFeatures(value, media.MaxAnalysisDurationTicks)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAnalysisFeatures(payload, media.MaxAnalysisDurationTicks)
	if err != nil || len(decoded.RawFingerprint) != introskipper.MaxFingerprintPoints || decoded.FingerprintEndSeconds != 600 {
		t.Fatalf("maximum raw fingerprint bounds rejected: %v", err)
	}
	value.FingerprintEndSeconds = math.Nextafter(600, 601)
	if encoded, err := EncodeAnalysisFeatures(value, media.MaxAnalysisDurationTicks); !errors.Is(err, ErrInvalidInput) || encoded != nil {
		t.Fatalf("horizon beyond ten minutes returned %d bytes, error=%v", len(encoded), err)
	}
	binary.LittleEndian.PutUint64(payload[64:72], math.Float64bits(value.FingerprintEndSeconds))
	assertAnalysisFeaturesPayloadRejected(t, payload, media.MaxAnalysisDurationTicks)
}

func TestAnalysisFeaturesCodecVersionSupportsHistoricalBackupGuards(t *testing.T) {
	for _, version := range []uint16{1, 2, 3, 4} {
		headerSize := 56
		if version == 3 {
			headerSize = 60
		} else if version == 4 {
			headerSize = 72
		}
		payload := make([]byte, headerSize)
		copy(payload, "GAFB")
		binary.LittleEndian.PutUint16(payload[4:6], version)
		got, err := AnalysisFeaturesCodecVersion(payload)
		if err != nil || got != version {
			t.Fatalf("supported version %d returned %d, %v", version, got, err)
		}
		if _, err := AnalysisFeaturesCodecVersion(payload[:headerSize-1]); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("incomplete version %d header accepted: %v", version, err)
		}
		payload[0] = 'X'
		if _, err := AnalysisFeaturesCodecVersion(payload); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid magic accepted: %v", err)
		}
	}
	for _, version := range []uint16{0, 5, math.MaxUint16} {
		payload := make([]byte, 72)
		copy(payload, "GAFB")
		binary.LittleEndian.PutUint16(payload[4:6], version)
		if _, err := AnalysisFeaturesCodecVersion(payload); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unsupported version %d accepted: %v", version, err)
		}
	}
}

func TestAnalysisFeaturesCacheRepresentationBindsNativeExecution(t *testing.T) {
	source := AnalysisSource{DurationTicks: 800 * introskipper.TicksPerSecond}
	options := introskipper.DefaultOptions()
	work := AnalysisWork{Execution: AnalysisExecutionProfile{
		DetectorVersion:     introskipper.Version,
		IntroProfile:        "p",
		IntroSkipperOptions: options,
	}}
	value := analysisRawFeaturesFixture()
	value.FingerprintEndSeconds = introskipper.FingerprintEndSeconds(source.DurationTicks, options)
	if !analysisFeaturesMatchWork(value, source, work) {
		t.Fatal("native extraction matching its admitted horizon was rejected")
	}
	work.Execution.IntroSkipperOptions.AnalysisPercent++
	if analysisFeaturesMatchWork(value, source, work) {
		t.Fatal("native execution reused a different extraction horizon")
	}
	work.Execution.IntroSkipperOptions = options
	legacy := AnalysisFeatures{AlgorithmProfile: "p"}
	if analysisFeaturesMatchWork(legacy, source, work) {
		t.Fatal("native execution reused legacy or absent raw evidence")
	}
	value.AlgorithmProfile = "different-profile"
	if analysisFeaturesMatchWork(value, source, work) {
		t.Fatal("native execution reused a different extraction identity")
	}
	value.AlgorithmProfile = "p"
	work.Execution.DetectorVersion = introdetect.Version
	if analysisFeaturesMatchWork(value, source, work) || !analysisFeaturesMatchWork(legacy, source, work) {
		t.Fatal("historical execution confused raw and legacy evidence")
	}
}
