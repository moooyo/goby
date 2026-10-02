package transcode

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func generatedAVPCMContentTestNoise(frames int, seed uint32) []byte {
	data := make([]byte, frames*4)
	for frame := 0; frame < frames; frame++ {
		for channel := 0; channel < 2; channel++ {
			seed ^= seed << 13
			seed ^= seed >> 17
			seed ^= seed << 5
			value := int16(int32(seed%16001) - 8000)
			binary.LittleEndian.PutUint16(data[frame*4+channel*2:], uint16(value))
		}
	}
	return data
}

func generatedAVPCMContentTestOptions() GeneratedAVPCMContentOptions {
	return GeneratedAVPCMContentOptions{Channels: 2, ReferenceSHA256: [32]byte{1}, QuerySHA256: [32]byte{2}, EncodedSourceSHA256: [32]byte{3},
		ReferenceFirstCandidateSample: 9000, QueryFirstSample: 10000, CandidateCount: 513, WindowSamples: 512}
}

func TestGeneratedAVPCMContentCandidateUsesBothChannelsAndExplicitRange(t *testing.T) {
	options := generatedAVPCMContentTestOptions()
	reference := generatedAVPCMContentTestNoise(options.CandidateCount-1+options.WindowSamples, 0x189ec5)
	query := append([]byte(nil), reference[211*4:(211+options.WindowSamples)*4]...)
	for offset := 0; offset < len(query); offset += 2 {
		value := int16(binary.LittleEndian.Uint16(query[offset:]))
		binary.LittleEndian.PutUint16(query[offset:], uint16(value/2))
	}
	result, err := compareGeneratedAVPCMContentWindows(context.Background(), reference, query, options)
	if err != nil || result.Status != GeneratedAVAssociationUnique || result.ReferenceSampleHypothesis != 9211 || result.DisplacementSamples != -789 ||
		result.SampleStride != 4 || result.ComparedValuesPerChannel != 128 || result.ReferenceFirstCandidateSample != 9000 || result.CandidateCount != 513 ||
		result.BestMinimumCorrelation < 0.99 || result.Margin < result.MinimumMargin {
		t.Fatalf("explicit-range content candidate differs: result=%+v error=%v", result, err)
	}
	if result.Qualified || result.Complete || result.CapturedBytesVerified || result.NativeClockKnown || result.DecodedOriginComplete || result.ContentBound {
		t.Fatal("pure sparse waveform comparison acquired capture, origin or playback qualification")
	}
	for _, name := range []string{"wrong_position", "swapped_channels", "one_wrong_channel"} {
		t.Run(name, func(t *testing.T) {
			negative := generatedAVPCMContentTestNoise(options.WindowSamples, 0x73cd)
			if name != "wrong_position" {
				negative = append([]byte(nil), query...)
				for offset := 0; offset < len(negative); offset += 4 {
					if name == "swapped_channels" {
						negative[offset], negative[offset+2] = negative[offset+2], negative[offset]
						negative[offset+1], negative[offset+3] = negative[offset+3], negative[offset+1]
					} else {
						binary.LittleEndian.PutUint16(negative[offset+2:], uint16(int16(offset%193-96)))
					}
				}
			}
			result, err := compareGeneratedAVPCMContentWindows(context.Background(), reference, negative, options)
			if err != nil || result.Status != GeneratedAVAssociationMissing || result.BestMinimumCorrelation >= result.MinimumCorrelation {
				t.Fatalf("wrong content admitted: status=%s error=%v", result.Status, err)
			}
		})
	}
}

func TestGeneratedAVPCMContentRepeatedPatternRemainsAmbiguous(t *testing.T) {
	options := generatedAVPCMContentTestOptions()
	pattern := generatedAVPCMContentTestNoise(64, 0x339ed)
	reference := make([]byte, (options.CandidateCount-1+options.WindowSamples)*4)
	for offset := range reference {
		reference[offset] = pattern[offset%len(pattern)]
	}
	query := append([]byte(nil), reference[:options.WindowSamples*4]...)
	result, err := compareGeneratedAVPCMContentWindows(context.Background(), reference, query, options)
	if err != nil || result.Status != GeneratedAVAssociationAmbiguous || result.Margin != 0 || result.BestMinimumCorrelation < 0.99 {
		t.Fatalf("repeated landmark selected a convenient origin: result=%+v error=%v", result, err)
	}
	result, err = compareGeneratedAVPCMContentWindows(context.Background(), reference, make([]byte, len(query)), options)
	if err != nil || result.Status != GeneratedAVAssociationMissing || result.ReferenceSampleHypothesis != -1 {
		t.Fatal("silence repaired a source origin")
	}
}

func TestGeneratedAVPCMContentCannotInventUnobservedCompetitorMargin(t *testing.T) {
	for _, count := range []int{1, 65} {
		options := generatedAVPCMContentTestOptions()
		options.CandidateCount = count
		reference := generatedAVPCMContentTestNoise(count-1+options.WindowSamples, 0x189e5)
		first := (count - 1) / 2
		query := reference[first*4 : (first+options.WindowSamples)*4]
		result, err := compareGeneratedAVPCMContentWindows(context.Background(), reference, query, options)
		if err != nil || result.Status != GeneratedAVAssociationAmbiguous || result.SecondCandidateKnown || result.Margin != 0 || result.BestMinimumCorrelation < 0.99 {
			t.Fatalf("absent competitor produced a unique margin: count=%d result=%+v error=%v", count, result, err)
		}
	}
}

func TestGeneratedAVPCMContentRejectsUnboundedOrPartialInputs(t *testing.T) {
	options := generatedAVPCMContentTestOptions()
	reference := generatedAVPCMContentTestNoise(options.CandidateCount-1+options.WindowSamples, 9)
	query := reference[:options.WindowSamples*4]
	if _, err := compareGeneratedAVPCMContentWindows(context.Background(), reference[:len(reference)-1], query, options); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("partial stereo PCM frame accepted")
	}
	oversized := options
	oversized.CandidateCount = generatedAVPCMContentMaxCandidates + 1
	if _, err := compareGeneratedAVPCMContentWindows(context.Background(), reference, query, oversized); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal("candidate budget increased")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compareGeneratedAVPCMContentWindows(canceled, reference, query, options); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled comparison entered its candidate scan")
	}
}
