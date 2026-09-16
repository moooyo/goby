package media

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func diagnosticContentSample(t *testing.T, kind DiagnosticSampleKind) DiagnosticRawSample {
	t.Helper()
	sample, err := GenerateDiagnosticSample(kind)
	if err != nil {
		t.Fatal(err)
	}
	return sample
}

func diagnosticContentCode(t *testing.T, err error, code string) {
	t.Helper()
	var detail *DiagnosticContentError
	if !errors.Is(err, ErrDiagnosticContent) || !errors.As(err, &detail) || detail.Code != code || err.Error() != code {
		t.Fatalf("content error = %v, want fixed code %s", err, code)
	}
}

func TestDiagnosticVideoContentAcceptsTheIndependentPatternAndBoundedLoss(t *testing.T) {
	for _, lossy := range []bool{false, true} {
		reference := diagnosticContentSample(t, DiagnosticRawVideo)
		decoded := bytes.Clone(reference.Data)
		if lossy {
			for index := range decoded {
				decoded[index] = byte(int(decoded[index]) + index%7 - 3)
			}
		}
		before := bytes.Clone(decoded)
		result, err := ValidateDiagnosticVideoContent(decoded, 320, 192, "yuv420p")
		if err != nil || result.Frames != 8 || result.Width != 320 || result.Height != 192 || result.PixelFormat != "yuv420p" ||
			result.ReferenceSHA256 != reference.SHA256 || result.PolicyVersion != DiagnosticContentPolicyVersion {
			t.Fatalf("valid fixed video was rejected: %v", err)
		}
		for index, frame := range result.FrameMetrics {
			if frame.Frame != index || frame.BestReferenceFrame != index {
				t.Fatal("content validation did not retain the exact frame order")
			}
			for _, plane := range frame.Planes {
				if plane.MeanAbsoluteError > 3 || plane.RootMeanSquareError > 3 || plane.WorstTileMeanAbsError > 3 {
					t.Fatal("fixed loss metrics exceeded the applied perturbation")
				}
			}
		}
		if !bytes.Equal(decoded, before) {
			t.Fatal("content validation changed its decoded input")
		}
	}
}

func TestDiagnosticVideoContentRejectsReorderedOrRepeatedFrames(t *testing.T) {
	reference := diagnosticContentSample(t, DiagnosticRawVideo)
	for _, order := range [][8]int{{1, 0, 2, 3, 4, 5, 6, 7}, {7, 6, 5, 4, 3, 2, 1, 0}, {0, 0, 2, 3, 4, 5, 6, 7}} {
		decoded := make([]byte, len(reference.Data))
		for destination, source := range order {
			copy(decoded[destination*DiagnosticVideoFrameBytes:], reference.Data[source*DiagnosticVideoFrameBytes:(source+1)*DiagnosticVideoFrameBytes])
		}
		result, err := ValidateDiagnosticVideoContent(decoded, 320, 192, "yuv420p")
		diagnosticContentCode(t, err, "diagnostic_video_frame_order")
		for index, expected := range order {
			if result.FrameMetrics[index].BestReferenceFrame != expected {
				t.Fatal("reordered-frame diagnostics did not identify the best original frame")
			}
		}
	}
}

func TestDiagnosticVideoContentRejectsBlankPlanesAndLocalMarkerDamage(t *testing.T) {
	for _, value := range []byte{0, 16, 128, 235, 255} {
		_, err := ValidateDiagnosticVideoContent(bytes.Repeat([]byte{value}, DiagnosticVideoBytes), 320, 192, "yuv420p")
		diagnosticContentCode(t, err, "diagnostic_video_content")
	}
	decoded := diagnosticContentSample(t, DiagnosticRawVideo).Data
	// One destroyed marker tile fits the global MAE/RMSE budgets. Its local
	// error must still reject the frame rather than being averaged away.
	for y := 80; y < 96; y++ {
		for x := 0; x < 16; x++ {
			decoded[y*320+x] = 0
		}
	}
	result, err := ValidateDiagnosticVideoContent(decoded, 320, 192, "yuv420p")
	diagnosticContentCode(t, err, "diagnostic_video_content")
	luma := result.FrameMetrics[0].Planes[0]
	if luma.MeanAbsoluteError > DiagnosticVideoMAELimit || luma.RootMeanSquareError > DiagnosticVideoRMSELimit || luma.WorstTileMeanAbsError <= DiagnosticVideoTileMAELimit {
		t.Fatal("the marker counterexample did not isolate the local error guard")
	}
	decoded = diagnosticContentSample(t, DiagnosticRawVideo).Data
	const yBytes, chromaBytes = 320 * 192, 160 * 96
	for frame := 0; frame < 8; frame++ {
		start := frame*DiagnosticVideoFrameBytes + yBytes
		first := bytes.Clone(decoded[start : start+chromaBytes])
		copy(decoded[start:start+chromaBytes], decoded[start+chromaBytes:start+2*chromaBytes])
		copy(decoded[start+chromaBytes:start+2*chromaBytes], first)
	}
	_, err = ValidateDiagnosticVideoContent(decoded, 320, 192, "yuv420p")
	diagnosticContentCode(t, err, "diagnostic_video_content")
}

func TestDiagnosticVideoContentRequiresExactFixedShape(t *testing.T) {
	reference := diagnosticContentSample(t, DiagnosticRawVideo).Data
	for _, decoded := range [][]byte{nil, reference[:len(reference)-1], reference[:DiagnosticVideoFrameBytes], append(bytes.Clone(reference), reference[:DiagnosticVideoFrameBytes]...)} {
		_, err := ValidateDiagnosticVideoContent(decoded, 320, 192, "yuv420p")
		diagnosticContentCode(t, err, "diagnostic_video_shape")
	}
	for _, shape := range []struct {
		width, height int
		format        string
	}{{192, 320, "yuv420p"}, {320, 191, "yuv420p"}, {320, 192, "nv12"}, {320, 192, "private-format-marker"}} {
		_, err := ValidateDiagnosticVideoContent(reference, shape.width, shape.height, shape.format)
		diagnosticContentCode(t, err, "diagnostic_video_shape")
		if strings.Contains(err.Error(), "private-format-marker") {
			t.Fatal("content error echoed supplied metadata")
		}
	}
}

func diagnosticPaddedPCM(t *testing.T, leading, trailing int, lossy bool) []byte {
	t.Helper()
	reference := diagnosticContentSample(t, DiagnosticRawAudio).Data
	data := make([]byte, (leading+DiagnosticAudioSampleFrames+trailing)*4)
	copy(data[leading*4:], reference)
	if lossy {
		for frame := 0; frame < DiagnosticAudioSampleFrames; frame++ {
			for channel := 0; channel < 2; channel++ {
				position := (leading+frame)*4 + channel*2
				value := int32(int16(binary.LittleEndian.Uint16(data[position:])))
				value = value*98/100 + int32((frame*11+channel)%41-20)
				binary.LittleEndian.PutUint16(data[position:], uint16(int16(value)))
			}
		}
	}
	return data
}

func TestDiagnosticAudioContentFindsVariablePrimingAndPadding(t *testing.T) {
	for _, shape := range [][2]int{{0, 0}, {17, 13}, {511, 512}, {1024, 0}, {2048, 1024}, {4096, 4095}} {
		for _, lossy := range []bool{false, true} {
			decoded := diagnosticPaddedPCM(t, shape[0], shape[1], lossy)
			before := bytes.Clone(decoded)
			result, err := ValidateDiagnosticAudioContent(decoded, 48000, 2, "s16le")
			if err != nil || result.OffsetFrames != shape[0] || result.TrailingFrames != shape[1] || result.SampleFrames != len(decoded)/4 ||
				result.ReferenceFrames != 8192 || result.SampleRate != 48000 || result.Channels != 2 ||
				result.CandidateOffsets < 1 || result.CandidateOffsets > DiagnosticAudioAlignmentLimit+1 || len(result.ReferenceSHA256) != 64 {
				t.Fatalf("valid PCM with variable alignment was rejected or misaligned: %v", err)
			}
			for channel := 0; channel < 2; channel++ {
				if result.NormalizedRMSE[channel] > 0.15 || result.WorstBlockNRMSE[channel] > 0.35 || result.SignalRMS[channel] <= 0 ||
					result.PaddingPeak[channel] != 0 {
					t.Fatal("valid PCM did not retain bounded channel metrics")
				}
			}
			if !bytes.Equal(decoded, before) {
				t.Fatal("PCM validation mutated its input")
			}
		}
	}
}

func TestDiagnosticAudioContentRejectsSilenceAndWrongChannelContent(t *testing.T) {
	_, err := ValidateDiagnosticAudioContent(make([]byte, DiagnosticAudioBytes), 48000, 2, "s16le")
	diagnosticContentCode(t, err, "diagnostic_audio_silence")
	for _, mode := range []string{"silent_left", "swapped", "duplicated", "inverted", "attenuated", "noise"} {
		decoded := diagnosticContentSample(t, DiagnosticRawAudio).Data
		for frame := 0; frame < DiagnosticAudioSampleFrames; frame++ {
			position := frame * 4
			left := int16(binary.LittleEndian.Uint16(decoded[position:]))
			right := int16(binary.LittleEndian.Uint16(decoded[position+2:]))
			switch mode {
			case "silent_left":
				left = 0
			case "swapped":
				left, right = right, left
			case "duplicated":
				right = left
			case "inverted":
				left, right = -left, -right
			case "attenuated":
				left, right = left/2, right/2
			case "noise":
				left, right = int16((frame%2)*60000-30000), int16(((frame+1)%2)*60000-30000)
			}
			binary.LittleEndian.PutUint16(decoded[position:], uint16(left))
			binary.LittleEndian.PutUint16(decoded[position+2:], uint16(right))
		}
		_, err := ValidateDiagnosticAudioContent(decoded, 48000, 2, "s16le")
		if !errors.Is(err, ErrDiagnosticContent) {
			t.Fatalf("incorrect channel content %s was accepted", mode)
		}
	}
}

func TestDiagnosticAudioContentRejectsTruncationHiddenByPeriodicInterior(t *testing.T) {
	reference := diagnosticContentSample(t, DiagnosticRawAudio).Data
	// Both tones repeat after 192 frames. Removing one whole interior period
	// preserves their phase, but cannot preserve the complete ending envelope.
	shortened := append(bytes.Clone(reference[:2048*4]), reference[(2048+192)*4:]...)
	shortened = append(shortened, make([]byte, 192*4)...)
	result, err := ValidateDiagnosticAudioContent(shortened, 48000, 2, "s16le")
	diagnosticContentCode(t, err, "diagnostic_audio_content")
	if result.WorstBlockNRMSE[0] <= 0.35 && result.WorstBlockNRMSE[1] <= 0.35 {
		t.Fatal("periodic truncation did not reach the local-envelope guard")
	}
	flat := make([]byte, len(reference))
	period := reference[192*4 : 384*4]
	for index := range flat {
		flat[index] = period[index%len(period)]
	}
	_, err = ValidateDiagnosticAudioContent(flat, 48000, 2, "s16le")
	diagnosticContentCode(t, err, "diagnostic_audio_content")
	truncated := diagnosticPaddedPCM(t, 1024, 0, false)
	_, err = ValidateDiagnosticAudioContent(truncated[:len(truncated)-512*4], 48000, 2, "s16le")
	if !errors.Is(err, ErrDiagnosticContent) {
		t.Fatal("priming concealed the loss of required reference frames")
	}
}

func TestDiagnosticAudioContentBoundsAlignmentAndPaddingEnergy(t *testing.T) {
	for _, before := range []bool{false, true} {
		decoded := diagnosticPaddedPCM(t, 1024, 1024, false)
		start := 0
		if !before {
			start = (1024 + DiagnosticAudioSampleFrames) * 4
		}
		for position := start; position < start+1024*4; position += 2 {
			binary.LittleEndian.PutUint16(decoded[position:], uint16(30000))
		}
		_, err := ValidateDiagnosticAudioContent(decoded, 48000, 2, "s16le")
		diagnosticContentCode(t, err, "diagnostic_audio_padding")
	}
	for _, shape := range [][2]int{{DiagnosticAudioAlignmentLimit + 512, 0}, {0, DiagnosticAudioPaddingLimit + 512}} {
		_, err := ValidateDiagnosticAudioContent(diagnosticPaddedPCM(t, shape[0], shape[1], false), 48000, 2, "s16le")
		if !errors.Is(err, ErrDiagnosticContent) {
			t.Fatal("unbounded leading or trailing data was accepted")
		}
	}
}

func TestDiagnosticAudioContentRequiresWholeSamplesAndDeclaredFormat(t *testing.T) {
	reference := diagnosticContentSample(t, DiagnosticRawAudio).Data
	for _, data := range [][]byte{nil, reference[:len(reference)-1], reference[:len(reference)-4], make([]byte, DiagnosticAudioFramesLimit*4)} {
		_, err := ValidateDiagnosticAudioContent(data, 48000, 2, "s16le")
		diagnosticContentCode(t, err, "diagnostic_audio_shape")
	}
	for _, shape := range []struct {
		rate, channels int
		format         string
	}{{44100, 2, "s16le"}, {48000, 1, "s16le"}, {48000, 2, "f32le"}, {48000, 2, "private-pcm-marker"}} {
		_, err := ValidateDiagnosticAudioContent(reference, shape.rate, shape.channels, shape.format)
		diagnosticContentCode(t, err, "diagnostic_audio_shape")
		if strings.Contains(err.Error(), "private-pcm-marker") {
			t.Fatal("PCM errors echoed untrusted metadata")
		}
	}
}

func TestDiagnosticContentMetricsAreDeterministicFiniteAndContainNoMedia(t *testing.T) {
	decoded := diagnosticPaddedPCM(t, 1024, 512, true)
	first, err := ValidateDiagnosticAudioContent(decoded, 48000, 2, "s16le")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidateDiagnosticAudioContent(decoded, 48000, 2, "s16le")
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("bounded content metrics were not deterministic")
	}
	for _, pair := range [][2]float64{first.NormalizedRMSE, first.WorstBlockNRMSE, first.SignalRMS, first.LeadingPaddingRMS, first.TrailingPaddingRMS, first.WorstPaddingBlockRMS} {
		for _, value := range pair {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				t.Fatal("content metrics contain an unbounded or non-finite value")
			}
		}
	}
	encoded, err := json.Marshal(first)
	if err != nil || len(encoded) > 4096 || bytes.Contains(encoded, []byte(`"Data"`)) || bytes.Contains(encoded, []byte(`"Samples"`)) {
		t.Fatal("content metrics retained media bytes or unbounded detail")
	}
}
