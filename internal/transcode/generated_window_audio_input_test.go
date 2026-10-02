package transcode

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func generatedAudioInputTestOptions(rendition int) RawGeneratedAudioInputOptions {
	return RawGeneratedAudioInputOptions{Rendition: rendition, OutputStreamIndex: 1, SampleRate: 48_000, MaxFrames: 2400, MaxSamples: 1_200_000}
}

func generatedAudioInputRecord(rendition int, number, samplesBefore, samples, encoderPTS, inputFrame, inputPTS int64) string {
	return fmt.Sprintf("GOBY_AUDIO %d 1 %d %d %d 1/48000 %d %d 1/48000 %d\n", rendition, number, samplesBefore, samples, encoderPTS, inputFrame, inputPTS)
}

func TestGeneratedAudioInputRetainsVariableLastFrameAndExactClocks(t *testing.T) {
	var reports []RawGeneratedAudioInputEvidence
	writer, err := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(1), func(evidence RawGeneratedAudioInputEvidence) {
		reports = append(reports, evidence)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := generatedAudioInputRecord(1, 0, 0, 1024, -1024, 71, 192_000) +
		generatedAudioInputRecord(1, 1, 1024, 1024, 0, 72, 193_024) +
		generatedAudioInputRecord(1, 2, 2048, 256, 1024, 73, 194_048)
	for index := range len(data) {
		if n, err := writer.Write([]byte(data[index : index+1])); n != 1 || err != nil {
			t.Fatalf("split record %d = %d, %v", index, n, err)
		}
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	got := writer.evidence
	if len(reports) != 3 || reports[0].TotalSamples != 1024 || reports[1].LastSampleNumber != 1024 ||
		got.Frames != 3 || got.TotalSamples != 2304 || got.FirstSampleNumber != 0 || got.LastSampleNumber != 2048 ||
		got.FirstFrameSamples != 1024 || got.LastFrameSamples != 256 || got.MinFrameSamples != 256 || got.MaxFrameSamples != 1024 ||
		got.FirstEncoder.PTS != -1024 || got.LastEncoder.PTS != 1024 || got.FirstInput.FrameIndex != 71 || got.LastInput.Clock.PTS != 194_048 ||
		!got.SamplesContiguous || !got.EncoderSampleClockExact || !got.EncoderTimeBaseStable || !got.InputAssociationKnown ||
		!got.SourceSequential || !got.InputCadenceExact || !got.InputClockNondecreasing || !got.InputTimeBaseStable {
		t.Fatalf("raw variable-frame evidence changed: %+v, reports=%+v", got, reports)
	}
}

func TestGeneratedAudioInputUnknownAssociationCannotRecover(t *testing.T) {
	writer, err := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := generatedAudioInputRecord(0, 0, 0, 1024, 0, 5, 0) +
		fmt.Sprintf("GOBY_AUDIO 0 1 1 1024 1024 1/48000 1024 -1 0/1 %d\n", int64(math.MaxInt64)) +
		generatedAudioInputRecord(0, 2, 2048, 1024, 2048, 7, 2048)
	if _, err := writer.Write([]byte(data)); err != nil || writer.finish() != nil {
		t.Fatalf("documented unknown association rejected: %v", err)
	}
	got := writer.evidence
	if !got.EncoderSampleClockExact || !got.LastInput.Available || got.InputAssociationKnown || got.InputTimeBaseStable ||
		got.SourceSequential || got.InputClockNondecreasing || got.InputCadenceExact {
		t.Fatalf("later matching input restored an unknown middle association: %+v", got)
	}
}

func TestGeneratedAudioInputKeepsEveryIntermediateClockMismatch(t *testing.T) {
	for name, middle := range map[string]string{
		"encoder clock diverges": generatedAudioInputRecord(0, 1, 1024, 1024, 1025, 1, 1024),
		"input clock diverges":   generatedAudioInputRecord(0, 1, 1024, 1024, 1024, 1, 1025),
		"input frame repeats":    generatedAudioInputRecord(0, 1, 1024, 1024, 1024, 0, 0),
		"input clock reverses":   generatedAudioInputRecord(0, 1, 1024, 1024, 1024, 1, -1),
	} {
		t.Run(name, func(t *testing.T) {
			writer, err := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			data := generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0) + middle + generatedAudioInputRecord(0, 2, 2048, 1024, 2048, 2, 2048)
			if _, err := writer.Write([]byte(data)); err != nil || writer.finish() != nil {
				t.Fatalf("raw diagnostic mismatch was rejected: %v", err)
			}
			got := writer.evidence
			if got.TotalSamples != 3072 || got.LastEncoder.PTS != 2048 || got.LastInput.Clock.PTS != 2048 {
				t.Fatal("fixture endpoints do not apparently recover")
			}
			if name == "encoder clock diverges" && got.EncoderSampleClockExact || name != "encoder clock diverges" && got.InputCadenceExact ||
				name == "input frame repeats" && got.SourceSequential || name == "input clock reverses" && got.InputClockNondecreasing {
				t.Fatalf("matching endpoints erased an intermediate mismatch: %+v", got)
			}
		})
	}
}

func TestGeneratedAudioInputRetainsExactChangedTimeBases(t *testing.T) {
	writer, err := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := "GOBY_AUDIO 0 1 0 0 1024 2/96000 0 0 2/96000 0\r\n" +
		"GOBY_AUDIO 0 1 1 1024 1024 1/96000 2048 1 1/96000 2048\n"
	if _, err := writer.Write([]byte(data)); err != nil || writer.finish() != nil {
		t.Fatalf("exact diagnostic time-base change was rejected: %v", err)
	}
	got := writer.evidence
	if got.FirstEncoder.TimeBase != (GeneratedRational{Num: 1, Den: 48000}) ||
		got.EncoderTimeBaseStable || got.InputTimeBaseStable || !got.EncoderSampleClockExact || !got.InputCadenceExact {
		t.Fatalf("normalized or changed clocks were misrepresented: %+v", got)
	}
}

func TestGeneratedAudioInputRejectsMalformedCountersAndForeignRecords(t *testing.T) {
	for name, record := range map[string]string{
		"wrong prefix":         "GOBY 0 1 0 0 1024 1/48000 0 0 1/48000 0\n",
		"wrong rendition":      generatedAudioInputRecord(1, 0, 0, 1024, 0, 0, 0),
		"wrong stream":         "GOBY_AUDIO 0 0 0 0 1024 1/48000 0 0 1/48000 0\n",
		"frame sequence":       generatedAudioInputRecord(0, 1, 0, 1024, 0, 0, 0),
		"post-frame sn":        generatedAudioInputRecord(0, 0, 1024, 1024, 0, 0, 0),
		"negative sample sn":   generatedAudioInputRecord(0, 0, -1, 1024, 0, 0, 0),
		"empty frame":          generatedAudioInputRecord(0, 0, 0, 0, 0, 0, 0),
		"oversized frame":      generatedAudioInputRecord(0, 0, 0, generatedAudioInputMaxFrameSamples+1, 0, 0, 0),
		"missing input index":  generatedAudioInputRecord(0, 0, 0, 1024, 0, -2, 0),
		"oversized index":      generatedAudioInputRecord(0, 0, 0, 1024, 0, generatedInputMaxIndex+1, 0),
		"unavailable encoder":  generatedAudioInputRecord(0, 0, 0, 1024, math.MaxInt64, 0, 0),
		"unknown source shape": "GOBY_AUDIO 0 1 0 0 1024 1/48000 0 -1 0/1 0\n",
		"missing field":        "GOBY_AUDIO 0 1 0 0 1024 1/48000 0 0 1/48000\n",
		"extra field":          generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0)[:len(generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0))-1] + " extra\n",
		"zero encoder base":    "GOBY_AUDIO 0 1 0 0 1024 0/1 0 0 1/48000 0\n",
		"float samples":        "GOBY_AUDIO 0 1 0 0 1024.0 1/48000 0 0 1/48000 0\n",
		"uint64 overflow":      "GOBY_AUDIO 0 1 0 18446744073709551615 1024 1/48000 0 0 1/48000 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			cancelled, reports := 0, 0
			writer, err := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), func(RawGeneratedAudioInputEvidence) { reports++ }, func() { cancelled++ })
			if err != nil {
				t.Fatal(err)
			}
			_, firstErr := writer.Write([]byte(record))
			if !errors.Is(firstErr, ErrProgress) || cancelled != 1 || reports != 0 || writer.evidence.Frames != 0 {
				t.Fatalf("invalid record changed evidence: %v, cancel=%d, reports=%d, %+v", firstErr, cancelled, reports, writer.evidence)
			}
			if n, err := writer.Write([]byte(generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0))); n != 0 || err != firstErr || writer.finish() != firstErr || cancelled != 1 {
				t.Fatal("faulted observer resumed or cancelled repeatedly")
			}
		})
	}
}

func TestGeneratedAudioInputRejectsCountersThatHideMissingMiddleSamples(t *testing.T) {
	for name, record := range map[string]string{
		"sample counter repeats": generatedAudioInputRecord(0, 1, 0, 1024, 1024, 1, 1024),
		"sample counter skips":   generatedAudioInputRecord(0, 1, 2048, 1024, 1024, 1, 1024),
		"frame counter repeats":  generatedAudioInputRecord(0, 0, 1024, 1024, 1024, 1, 1024),
	} {
		t.Run(name, func(t *testing.T) {
			writer, _ := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), nil, nil)
			if _, err := writer.Write([]byte(generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0))); err != nil {
				t.Fatal(err)
			}
			before := writer.evidence
			if _, err := writer.Write([]byte(record)); !errors.Is(err, ErrProgress) || writer.evidence != before {
				t.Fatalf("missing samples were hidden by endpoint arithmetic: %+v, %v", writer.evidence, err)
			}
		})
	}
}

func TestGeneratedAudioInputBoundsRecordsSamplesAndIncompleteFinish(t *testing.T) {
	for name, options := range map[string]RawGeneratedAudioInputOptions{
		"frame budget":  {Rendition: 0, OutputStreamIndex: 1, SampleRate: 48000, MaxFrames: 1, MaxSamples: 4096},
		"sample budget": {Rendition: 0, OutputStreamIndex: 1, SampleRate: 48000, MaxFrames: 8, MaxSamples: 1024},
	} {
		t.Run(name, func(t *testing.T) {
			writer, _ := newGeneratedAudioInputWriter(options, nil, nil)
			if _, err := writer.Write([]byte(generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0))); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte(generatedAudioInputRecord(0, 1, 1024, 1, 1024, 1, 1024))); !errors.Is(err, ErrProgress) {
				t.Fatal("observer exceeded its explicit budget")
			}
		})
	}
	for _, data := range []string{"", strings.Repeat("x", generatedAudioInputLineBytes+1), "GOBY_AUDIO 0 1 0 0 1024 1/48000 0 0 1/48000 0"} {
		cancelled := false
		writer, _ := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), nil, func() { cancelled = true })
		_, err := writer.Write([]byte(data))
		if err == nil {
			err = writer.finish()
		}
		if !errors.Is(err, ErrProgress) || !cancelled {
			t.Fatalf("empty, truncated or oversized evidence finished: bytes=%d, %v", len(data), err)
		}
	}
}

func TestGeneratedAudioInputRejectsInvalidOptionsAndPanickingCallback(t *testing.T) {
	for _, option := range []RawGeneratedAudioInputOptions{{}, {Rendition: -1},
		{Rendition: 4, OutputStreamIndex: 1, SampleRate: 48000, MaxFrames: 1, MaxSamples: 1},
		{Rendition: 0, OutputStreamIndex: 2, SampleRate: 48000, MaxFrames: 1, MaxSamples: 1},
		{Rendition: 0, OutputStreamIndex: 1, SampleRate: 48000, MaxFrames: generatedAudioInputMaxFrames + 1, MaxSamples: 1},
		{Rendition: 0, OutputStreamIndex: 1, SampleRate: 48000, MaxFrames: 1, MaxSamples: generatedAudioInputMaxSamples + 1}} {
		if _, err := newGeneratedAudioInputWriter(option, nil, nil); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("unbounded options accepted: %+v, %v", option, err)
		}
	}
	for _, report := range []func(RawGeneratedAudioInputEvidence){func(RawGeneratedAudioInputEvidence) { panic("private callback detail") }, func(RawGeneratedAudioInputEvidence) { panic(nil) }} {
		writer, _ := newGeneratedAudioInputWriter(generatedAudioInputTestOptions(0), report, func() { panic("private cancellation detail") })
		if _, err := writer.Write([]byte(generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0))); !errors.Is(err, ErrProgress) || !errors.Is(writer.finish(), ErrProgress) {
			t.Fatalf("report panic escaped or yielded complete-looking evidence: %v", err)
		}
	}
}
