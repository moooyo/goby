package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestDolbyVisionStreamingMatchesCompleteParserAcrossChunkBoundaries(t *testing.T) {
	disabled, enabled := dolbyVisionAccessUnitFixture(true), dolbyVisionAccessUnitFixture(false)
	aud := []byte{0, 0, 0, 1, 70, 1, 16}
	fixtures := [][]byte{
		nil, {0}, {0, 0, 0, 0}, {0, 0, 1}, {0, 0, 1, 70},
		disabled, enabled, aud, disabled[len(aud):],
		append(append([]byte{}, disabled...), enabled...),
		append(append([]byte{}, disabled...), aud...),
		append(append([]byte{}, aud...), disabled...),
		append(append(append([]byte{}, aud...), disabled...), 0, 0, 1, 9),
		append(append([]byte{}, disabled...), disabled[len(aud):]...),
		append(append([]byte{}, disabled...), 0, 0, 1),
		append(append([]byte{}, disabled...), 0, 0, 0),
		append(append([]byte{0, 0, 0, 0, 0}, disabled...), 0, 0, 0),
		append([]byte{9}, disabled...),
	}
	for length := 1; length < len(disabled); length++ {
		fixtures = append(fixtures, disabled[:length])
	}
	for index := range disabled {
		mutated := append([]byte{}, disabled...)
		mutated[index] ^= 0x40
		fixtures = append(fixtures, mutated)
	}
	for _, mode := range []string{"none", "limited", "extended", "unsupported"} {
		for index, data := range fixtures {
			want, wantErr := parseDolbyVisionRPUAccessUnits(context.Background(), data, mode)
			for split := 0; split <= len(data); split++ {
				stream := newDolbyVisionRPUStream(context.Background(), mode)
				for _, part := range [][]byte{data[:split], data[split:]} {
					if n, err := stream.Write(part); err != nil || n != len(part) {
						t.Fatalf("fixture %d split %d mode %s write: %d, %v", index, split, mode, n, err)
					}
				}
				got, gotErr := stream.finish()
				if got != want || !errors.Is(gotErr, wantErr) {
					t.Fatalf("fixture %d split %d mode %s differs: got %+v, %v; want %+v, %v", index, split, mode, got, gotErr, want, wantErr)
				}
			}
			stream := newDolbyVisionRPUStream(context.Background(), mode)
			for offset := range data {
				if _, err := stream.Write(data[offset : offset+1]); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := stream.finish(); got != want || !errors.Is(err, wantErr) {
				t.Fatalf("fixture %d one-byte chunks mode %s differs: %+v, %v; want %+v, %v", index, mode, got, err, want, wantErr)
			}
		}
	}
}

func TestDolbyVisionStreamingHandlesLongAnnexBZeroPadding(t *testing.T) {
	stream := newDolbyVisionRPUStream(context.Background())
	zeroes := make([]byte, 4093)
	for index := 0; index < 512; index++ {
		if _, err := stream.Write(zeroes); err != nil {
			t.Fatal(err)
		}
	}
	fixture := dolbyVisionAccessUnitFixture(true)
	for index := range fixture {
		if _, err := stream.Write(fixture[index : index+1]); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 512; index++ {
		if _, err := stream.Write(zeroes); err != nil {
			t.Fatal(err)
		}
	}
	got, err := stream.finish()
	if err != nil || !got.verified || got.frameCount != 1 || got.profile != 8 || !got.residualDisabled {
		t.Fatalf("long leading or trailing zero padding changed evidence: %+v, %v", got, err)
	}
}

func TestDolbyVisionStreamingPreservesHeaderAndCompressionEvidence(t *testing.T) {
	for _, mode := range []string{"none", "limited", "extended", "unsupported"} {
		for _, change := range []func(*dolbyVisionAccessUnitHeaderFixture){
			func(h *dolbyVisionAccessUnitHeaderFixture) {},
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.rpuProfile, h.fullRange = 0, 1 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled = false },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled, h.vdrDepth = false, 2 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled, h.elSpatial = false, 0 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.coefficientType = 1 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.denominator = 32 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.enhancementDepth = 0xff02 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.sequenceInfo = 0 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.denominator = 33 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.baseDepth = 9 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.enhancementDepth = 0x10002 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.vdrDepth = 9 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.compression = 2 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.usePrevious = 1 },
			func(h *dolbyVisionAccessUnitHeaderFixture) { h.compression, h.metadataPresent = 1, 1 },
		} {
			header := dolbyVisionDefaultAccessUnitHeader()
			change(&header)
			data := dolbyVisionAccessUnitWithHeader(header)
			want, err := parseDolbyVisionRPUAccessUnits(context.Background(), data, mode)
			if err != nil {
				t.Fatal(err)
			}
			stream := newDolbyVisionRPUStream(context.Background(), mode)
			for offset := 0; offset < len(data); offset += 7 {
				if _, err := stream.Write(data[offset:min(offset+7, len(data))]); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := stream.finish(); err != nil || got != want {
				t.Fatalf("header mode %s differs: %+v, %v; want %+v", mode, got, err, want)
			}
		}
	}
}

func TestDolbyVisionStreamingValidatesEntireLargeNAL(t *testing.T) {
	fixture := dolbyVisionAccessUnitFixture(true)
	rbsp, valid, err := dolbyVisionRPUUnescape(context.Background(), fixture[13:])
	if err != nil || !valid {
		t.Fatalf("invalid base fixture: %v, %v", valid, err)
	}
	body := append(append([]byte{}, rbsp[1:len(rbsp)-5]...), bytes.Repeat([]byte{0x55}, 8<<20)...)
	data := dolbyVisionAccessUnitEnvelope(body)
	want, err := parseDolbyVisionRPUAccessUnits(context.Background(), data)
	if err != nil || !want.verified || want.frameCount != 1 {
		t.Fatalf("large complete envelope was not accepted: %+v, %v", want, err)
	}
	for _, corrupt := range []bool{false, true} {
		if corrupt {
			data[len(data)-6] ^= 0x40
		}
		stream := newDolbyVisionRPUStream(context.Background())
		for offset := 0; offset < len(data); offset += 4093 {
			if _, err := stream.Write(data[offset:min(offset+4093, len(data))]); err != nil {
				t.Fatal(err)
			}
		}
		got, err := stream.finish()
		if err != nil || !corrupt && got != want || corrupt && (got.verified || got.reason != dolbyVisionRPUInvalidScan) {
			t.Fatalf("large envelope suffix was not completely checked, corrupt=%v: %+v, %v", corrupt, got, err)
		}
	}
}

func TestDolbyVisionStreamingDefersEvidenceUntilCompleteInput(t *testing.T) {
	stream := newDolbyVisionRPUStream(context.Background())
	if _, err := stream.Write(dolbyVisionAccessUnitFixture(true)); err != nil {
		t.Fatal(err)
	}
	// A later empty NAL must invalidate what appeared to be a complete frame.
	if _, err := stream.Write([]byte{0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	if got, err := stream.finish(); err != nil || got.verified || got.reason != dolbyVisionRPUInvalidScan {
		t.Fatalf("partial evidence survived a malformed suffix: %+v, %v", got, err)
	}
	if n, err := stream.Write(nil); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("finished parser accepted more input: %d, %v", n, err)
	}
}

func TestDolbyVisionStreamingOutputLimitWinsAfterInvalidPrefix(t *testing.T) {
	chunk := make([]byte, 64<<10)
	for _, extra := range []bool{false, true} {
		stream := newDolbyVisionRPUStream(context.Background())
		if _, err := stream.Write([]byte{9}); err != nil {
			t.Fatal(err)
		}
		for remaining := maxDolbyVisionProbeOutput - 1; remaining > 0; {
			part := chunk[:min(remaining, len(chunk))]
			if n, err := stream.Write(part); err != nil || n != len(part) {
				t.Fatalf("invalid scan stopped draining: %d, %v", n, err)
			}
			remaining -= len(part)
		}
		wantReason := dolbyVisionRPUInvalidScan
		if extra {
			if _, err := stream.Write([]byte{0}); err != nil {
				t.Fatal(err)
			}
			wantReason = dolbyVisionRPUOutputLimit
		}
		if got, err := stream.finish(); err != nil || got.reason != wantReason {
			t.Fatalf("output budget extra=%v returned %+v, %v; want %s", extra, got, err, wantReason)
		}
	}
}

func TestDolbyVisionStreamingCancellationDiscardsEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := newDolbyVisionRPUStream(ctx)
	if _, err := stream.Write(dolbyVisionAccessUnitFixture(true)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := stream.Write([]byte{0}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write returned %v", err)
	}
	if got, err := stream.finish(); !errors.Is(err, context.Canceled) || got != (dolbyVisionRPUEvidence{}) {
		t.Fatalf("cancelled stream retained evidence: %+v, %v", got, err)
	}
}
