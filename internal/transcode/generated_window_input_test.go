package transcode

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func generatedInputTestPlan() Plan {
	p := generatedWindowTestPlan()
	p.FrameRate = 30
	return p
}

func generatedInputRecord(rendition int, number, inputFrame, encoderPTS, inputPTS int64) string {
	return fmt.Sprintf("GOBY_INPUT %d 0 %d %d 1/30 %d 1/90000 %d\n", rendition, number, inputFrame, encoderPTS, inputPTS)
}

func TestGeneratedInputWriterRetainsExactClocksAcrossChunks(t *testing.T) {
	p := generatedInputTestPlan()
	var got []GeneratedInputEvidence
	writer, err := newGeneratedInputWriter(p, 1, func(evidence GeneratedInputEvidence) { got = append(got, evidence) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := generatedInputRecord(1, 0, 44, -2, 1931) + generatedInputRecord(1, 1, 44, -1, 1931) + generatedInputRecord(1, 2, 46, 0, 7931)
	for index := range len(data) {
		if n, err := writer.Write([]byte(data[index : index+1])); err != nil || n != 1 {
			t.Fatalf("split record %d = %d, %v", index, n, err)
		}
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	want := GeneratedInputEvidence{Rendition: 1, Frames: 3, FirstInputFrame: 44, LastInputFrame: 46,
		InputTimeBaseNumerator: 1, InputTimeBaseDenominator: 90000, FirstInputPTS: 1931, LastInputPTS: 7931,
		EncoderTimeBaseNumerator: 1, EncoderTimeBaseDenominator: 30, FirstEncoderPTS: -2, LastEncoderPTS: 0}
	if len(got) != 3 || got[0].Frames != 1 || got[1].Frames != 2 || !reflect.DeepEqual(got[2], want) || writer.evidence != want {
		t.Fatalf("input clocks or duplicate/drop evidence changed: %+v", got)
	}
	if err := ValidateGeneratedInputEvidence(p, want); err != nil {
		t.Fatalf("complete observer snapshot rejected: %v", err)
	}
}

func TestGeneratedInputWriterNormalizesEquivalentTimebases(t *testing.T) {
	writer, err := newGeneratedInputWriter(generatedInputTestPlan(), 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := "GOBY_INPUT 0 0 0 7 3/90 0 2/180000 99\r\n" + generatedInputRecord(0, 1, 8, 1, 3099)
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.finish(); err != nil || writer.evidence.EncoderTimeBaseDenominator != 30 || writer.evidence.InputTimeBaseDenominator != 90000 {
		t.Fatalf("equivalent time bases changed their exact representation: %+v, %v", writer.evidence, err)
	}
}

func TestGeneratedInputWriterKeepsIntermediateSourceMismatchFacts(t *testing.T) {
	for name, data := range map[string]string{
		"duplicate then compensating skip": generatedInputRecord(0, 0, 0, 0, 0) + generatedInputRecord(0, 1, 0, 1, 0) +
			generatedInputRecord(0, 2, 2, 2, 6000) + generatedInputRecord(0, 3, 3, 3, 9000),
		"middle cadence diverges then recovers": generatedInputRecord(0, 0, 0, 0, 0) + generatedInputRecord(0, 1, 1, 1, 1500) +
			generatedInputRecord(0, 2, 2, 2, 6000) + generatedInputRecord(0, 3, 3, 3, 9000),
	} {
		t.Run(name, func(t *testing.T) {
			p := generatedInputTestPlan()
			writer, err := newGeneratedInputWriter(p, 0, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte(data)); err != nil || writer.finish() != nil {
				t.Fatalf("valid observed conversion was rejected: %v", err)
			}
			evidence := writer.evidence
			if evidence.LastInputFrame-evidence.FirstInputFrame != evidence.Frames-1 || evidence.LastInputPTS != 9000 || evidence.LastEncoderPTS != 3 {
				t.Fatalf("fixture does not retain apparently matching endpoints: %+v", evidence)
			}
			wantSequential := name == "middle cadence diverges then recovers"
			if evidence.SourceSequential != wantSequential || evidence.InputCadenceAligned {
				t.Fatalf("matching endpoints erased an intermediate mismatch: %+v", evidence)
			}
			if err := ValidateGeneratedInputEvidence(p, evidence); err != nil {
				t.Fatalf("fallback diagnostic snapshot was rejected: %v", err)
			}
		})
	}
}

func TestGeneratedInputWriterAcceptsQuantizedSourceCadenceWithoutAccumulatedDrift(t *testing.T) {
	for _, rate := range []int64{24, 30} {
		t.Run(fmt.Sprintf("%d_fps", rate), func(t *testing.T) {
			p := generatedInputTestPlan()
			p.FrameRate = float64(rate)
			writer, err := newGeneratedInputWriter(p, 0, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			for frame := int64(0); frame < 5*rate; frame++ {
				line := fmt.Sprintf("GOBY_INPUT 0 0 %d %d 1/%d %d 1/1000 %d\n", frame, frame+91, rate, frame+3, 273+frame*1000/rate)
				if _, err := writer.Write([]byte(line)); err != nil {
					t.Fatalf("actual quantized source input %d rejected: %v", frame, err)
				}
			}
			if err := writer.finish(); err != nil || !writer.evidence.SourceSequential || !writer.evidence.InputCadenceAligned || writer.evidence.InputCadenceExact {
				t.Fatalf("41/42 ms quantization lost aligned source cadence: %+v, %v", writer.evidence, err)
			}
			if err := ValidateGeneratedInputEvidence(p, writer.evidence); err != nil {
				t.Fatal(err)
			}
		})
	}
	p := generatedInputTestPlan()
	p.FrameRate = 24
	writer, err := newGeneratedInputWriter(p, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for frame := int64(0); frame < 5; frame++ {
		// Each source interval differs by less than one millisecond, but its
		// repeated extra quantum creates more than one millisecond of total
		// source drift from the first frame. That must disable strict proof.
		line := fmt.Sprintf("GOBY_INPUT 0 0 %d %d 1/24 %d 1/1000 %d\n", frame, frame, frame, frame*42)
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if !writer.evidence.SourceSequential || writer.evidence.InputCadenceAligned {
		t.Fatalf("per-step tolerance accumulated into an aligned-clock claim: %+v", writer.evidence)
	}
}

func TestGeneratedInputWriterDoesNotUpgradeOneQuantumDriftToExactCoverage(t *testing.T) {
	plan := generatedInputTestPlan()
	writer, err := newGeneratedInputWriter(plan, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := generatedInputRecord(0, 0, 0, 0, 0) + generatedInputRecord(0, 1, 1, 1, 3001) + generatedInputRecord(0, 2, 2, 2, 6000)
	if _, err := writer.Write([]byte(data)); err != nil || writer.finish() != nil {
		t.Fatalf("diagnostic quantized cadence was rejected: %v", err)
	}
	evidence := writer.evidence
	if !evidence.SourceSequential || !evidence.InputCadenceAligned || evidence.InputCadenceExact || evidence.LastInputPTS != 6000 {
		t.Fatalf("matching endpoints erased the middle one-quantum shift: %+v", evidence)
	}
	if err := ValidateGeneratedInputEvidence(plan, evidence); err != nil {
		t.Fatalf("diagnostic representation was rejected: %v", err)
	}
}

func TestGeneratedInputWriterRejectsInvalidInitialEvidence(t *testing.T) {
	for name, data := range map[string]string{
		"prefix":              "GOBY 0 0 0 0 1/30 0 1/90000 0\n",
		"missing field":       "GOBY_INPUT 0 0 0 0 1/30 0 1/90000\n",
		"extra field":         "GOBY_INPUT 0 0 0 0 1/30 0 1/90000 0 extra\n",
		"wrong output":        generatedInputRecord(1, 0, 0, 0, 0),
		"wrong stream":        "GOBY_INPUT 0 1 0 0 1/30 0 1/90000 0\n",
		"reordered frame":     generatedInputRecord(0, 1, 0, 0, 0),
		"negative frame":      generatedInputRecord(0, -1, 0, 0, 0),
		"missing input index": generatedInputRecord(0, 0, -1, 0, 0),
		"input index bound":   generatedInputRecord(0, 0, generatedInputMaxIndex+1, 0, 0),
		"missing input pts":   generatedInputRecord(0, 0, 0, 0, math.MaxInt64),
		"input pts bound":     generatedInputRecord(0, 0, 0, 0, 233280000001),
		"encoder pts bound":   generatedInputRecord(0, 0, 0, 77760001, 0),
		"unavailable pts":     "GOBY_INPUT 0 0 0 0 1/30 0 1/90000 N/A\n",
		"float pts":           "GOBY_INPUT 0 0 0 0 1/30 0.0 1/90000 0\n",
		"zero numerator":      "GOBY_INPUT 0 0 0 0 0/30 0 1/90000 0\n",
		"zero denominator":    "GOBY_INPUT 0 0 0 0 1/30 0 1/0 0\n",
		"oversized base":      "GOBY_INPUT 0 0 0 0 1/30 0 1/1000000001 0\n",
		"negative base":       "GOBY_INPUT 0 0 0 0 1/30 0 -1/90000 0\n",
		"nonintegral cadence": "GOBY_INPUT 0 0 0 0 1/25 0 1/90000 0\n",
		"dynamic float base":  "GOBY_INPUT 0 0 0 0 1/29.97 0 1/90000 0\n",
		"embedded carriage":   "GOBY_INPUT 0 0 0 0 1/30 0 1/90000\r 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			cancelled, updates := 0, 0
			writer, err := newGeneratedInputWriter(generatedInputTestPlan(), 0, func(GeneratedInputEvidence) { updates++ }, func() { cancelled++ })
			if err != nil {
				t.Fatal(err)
			}
			_, firstErr := writer.Write([]byte(data))
			if !errors.Is(firstErr, ErrProgress) || cancelled != 1 || updates != 0 || writer.evidence.Frames != 0 {
				t.Fatalf("invalid evidence changed progress: %v, cancel=%d, updates=%d, %+v", firstErr, cancelled, updates, writer.evidence)
			}
			if n, err := writer.Write([]byte(generatedInputRecord(0, 0, 0, 0, 0))); n != 0 || err != firstErr || writer.finish() != firstErr || cancelled != 1 {
				t.Fatal("failed evidence writer resumed or cancelled repeatedly")
			}
		})
	}
}

func TestGeneratedInputWriterRejectsBrokenSequenceAndDynamicClocks(t *testing.T) {
	for name, data := range map[string]string{
		"encoder duplicate":      generatedInputRecord(0, 1, 1, 0, 3000),
		"encoder gap":            generatedInputRecord(0, 1, 1, 2, 3000),
		"encoder reversal":       generatedInputRecord(0, 1, 1, -1, 3000),
		"sequence duplicate":     generatedInputRecord(0, 0, 1, 1, 3000),
		"sequence dropped":       generatedInputRecord(0, 2, 1, 1, 3000),
		"input index reversal":   generatedInputRecord(0, 1, 2, 1, 3000),
		"input pts reversal":     generatedInputRecord(0, 1, 4, 1, 2999),
		"same index changed pts": generatedInputRecord(0, 1, 3, 1, 6000),
		"encoder base changed":   "GOBY_INPUT 0 0 1 4 1/60 2 1/90000 6000\n",
		"input base changed":     "GOBY_INPUT 0 0 1 4 1/30 1 1/1000 6000\n",
		"foreign output late":    generatedInputRecord(1, 1, 4, 1, 6000),
		"unavailable index late": generatedInputRecord(0, 1, -1, 1, 6000),
		"unavailable pts late":   generatedInputRecord(0, 1, 4, 1, math.MaxInt64),
	} {
		t.Run(name, func(t *testing.T) {
			writer, err := newGeneratedInputWriter(generatedInputTestPlan(), 0, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte(generatedInputRecord(0, 0, 3, 0, 3000))); err != nil {
				t.Fatal(err)
			}
			before := writer.evidence
			if _, err := writer.Write([]byte(data)); !errors.Is(err, ErrProgress) || writer.evidence != before {
				t.Fatalf("broken evidence accepted or partially updated: %+v, %v", writer.evidence, err)
			}
		})
	}
}

func TestGeneratedInputWriterBoundsFrameCountAndLines(t *testing.T) {
	writer, err := newGeneratedInputWriter(generatedInputTestPlan(), 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := int64(0); index < writer.maxFrames; index++ {
		if _, err := writer.Write([]byte(generatedInputRecord(0, index, index, index, index*3000))); err != nil {
			t.Fatalf("bounded frame %d failed: %v", index, err)
		}
	}
	if _, err := writer.Write([]byte(generatedInputRecord(0, writer.maxFrames, writer.maxFrames, writer.maxFrames, writer.maxFrames*3000))); !errors.Is(err, ErrProgress) {
		t.Fatal("frame count exceeded the finite observer budget")
	}
	for _, data := range []string{strings.Repeat("x", generatedInputLineBytes+1), generatedInputRecord(0, 0, 0, 0, 0)[:len(generatedInputRecord(0, 0, 0, 0, 0))-1], ""} {
		cancelled := false
		writer, err := newGeneratedInputWriter(generatedInputTestPlan(), 0, nil, func() { cancelled = true })
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := writer.Write([]byte(data))
		if writeErr == nil {
			writeErr = writer.finish()
		}
		if !errors.Is(writeErr, ErrProgress) || !cancelled {
			t.Fatalf("unbounded, incomplete or absent evidence accepted: %d bytes, %v", len(data), writeErr)
		}
	}
}

func TestGeneratedInputWriterRejectsUnsupportedPlans(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"no window":       func(p *Plan) { p.HLS.Window = HLSWindow{} },
		"fractional rate": func(p *Plan) { p.FrameRate = 29.97 },
		"missing rate":    func(p *Plan) { p.FrameRate = 0 },
		"NaN rate":        func(p *Plan) { p.FrameRate = math.NaN() },
		"video copy":      func(p *Plan) { p.VideoCodec = "copy" },
		"no video":        func(p *Plan) { p.VideoStreamIndex = -1 },
		"huge observer": func(p *Plan) {
			p.FrameRate = 240
			p.DurationTicks, p.StartTicks, p.HLS.Window.EndTicks = 6000*ticksPerSecond, 0, 6000*ticksPerSecond
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := generatedInputTestPlan()
			mutate(&p)
			if _, err := newGeneratedInputWriter(p, 0, nil, nil); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unsupported observer plan accepted: %v", err)
			}
		})
	}
	if _, err := newGeneratedInputWriter(generatedInputTestPlan(), MaxHLSRenditions, nil, nil); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("unassigned rendition was accepted")
	}
}

func TestValidateGeneratedInputEvidenceRejectsMutatedSnapshots(t *testing.T) {
	p := generatedInputTestPlan()
	writer, err := newGeneratedInputWriter(p, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(generatedInputRecord(0, 0, 0, 0, 0) + generatedInputRecord(0, 1, 1, 1, 3000))); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*GeneratedInputEvidence){
		"absent":                func(e *GeneratedInputEvidence) { *e = GeneratedInputEvidence{} },
		"foreign output":        func(e *GeneratedInputEvidence) { e.Rendition = 3 },
		"count beyond limit":    func(e *GeneratedInputEvidence) { e.Frames = generatedInputMaxFrames + 1 },
		"negative input":        func(e *GeneratedInputEvidence) { e.FirstInputFrame = -1 },
		"input index reversal":  func(e *GeneratedInputEvidence) { e.LastInputFrame = -1 },
		"input pts reversal":    func(e *GeneratedInputEvidence) { e.LastInputPTS = -1 },
		"encoder end mismatch":  func(e *GeneratedInputEvidence) { e.LastEncoderPTS++ },
		"zero input base":       func(e *GeneratedInputEvidence) { e.InputTimeBaseNumerator = 0 },
		"unavailable input pts": func(e *GeneratedInputEvidence) { e.LastInputPTS = math.MaxInt64 },
		"unnormalized base":     func(e *GeneratedInputEvidence) { e.InputTimeBaseNumerator, e.InputTimeBaseDenominator = 2, 180000 },
		"same frame new pts":    func(e *GeneratedInputEvidence) { e.LastInputFrame = e.FirstInputFrame },
		"single inconsistent":   func(e *GeneratedInputEvidence) { e.Frames, e.LastEncoderPTS = 1, e.FirstEncoderPTS },
		"true sequential mismatch": func(e *GeneratedInputEvidence) {
			e.LastInputFrame++
			e.SourceSequential = true
		},
		"true cadence mismatch": func(e *GeneratedInputEvidence) {
			e.LastInputPTS += 1500
			e.InputCadenceAligned = true
		},
		"true exact cadence mismatch": func(e *GeneratedInputEvidence) {
			e.LastInputPTS++
			e.InputCadenceExact = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			evidence := writer.evidence
			mutate(&evidence)
			if err := ValidateGeneratedInputEvidence(p, evidence); err == nil {
				t.Fatalf("mutated snapshot accepted: %+v", evidence)
			}
		})
	}
}
