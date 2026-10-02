package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func generatedAVPCMOverlapTestOptions(reference, query []byte) GeneratedAVPCMOverlapDifferenceOptions {
	return GeneratedAVPCMOverlapDifferenceOptions{Channels: 2, SampleRate: 48000, ReferenceSHA256: sha256.Sum256(reference), QuerySHA256: sha256.Sum256(query), EncodedSourceSHA256: [32]byte{3}}
}

func TestGeneratedAVPCMOverlapDifferenceStreamsActualValuesAndBinBoundaries(t *testing.T) {
	const frames = 17000
	reference, query := make([]byte, frames*4), make([]byte, frames*4)
	set := func(data []byte, frame, channel int, value int16) {
		binary.LittleEndian.PutUint16(data[frame*4+channel*2:], uint16(value))
	}
	set(reference, 0, 0, -32768)
	set(query, 0, 0, 32767)
	set(reference, 1023, 1, -9)
	set(query, 1023, 1, 10)
	set(query, 1024, 0, -3)
	set(query, 1024, 1, 4)
	set(reference, frames-1, 1, 6)
	set(query, frames-1, 1, -6)
	options := generatedAVPCMOverlapTestOptions(reference, query)
	options.ComparedSamples = frames
	result, err := compareGeneratedAVPCMOverlapDifference(context.Background(), bytes.NewReader(reference), bytes.NewReader(query), int64(len(reference)), int64(len(query)), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ComparedParsed || result.BytesEqual || !result.WholeQueryCompared || result.MismatchFrames != 4 || result.MismatchValues != ([2]int64{2, 3}) ||
		result.ErrorSumSquares != ([2]uint64{4294836234, 521}) || result.MaximumAbsoluteDifference != ([2]int32{65535, 19}) {
		t.Fatal("streamed signed values, full-range counts or error energy differ")
	}
	if !result.FirstMismatch.Known || result.FirstMismatch.ReferenceSample != 0 || result.FirstMismatch.QuerySample != 0 || result.FirstMismatch.Channel != 0 ||
		result.FirstMismatch.ReferenceValue != -32768 || result.FirstMismatch.QueryValue != 32767 || !result.LastMismatch.Known ||
		result.LastMismatch.ReferenceSample != frames-1 || result.LastMismatch.Channel != 1 || result.LastMismatch.ReferenceValue != 6 || result.LastMismatch.QueryValue != -6 {
		t.Fatal("first/last actual mismatched signed values were lost")
	}
	if len(result.Bins) != 17 || result.Bins[0].Frames != 1024 || result.Bins[0].MismatchFrames != 2 || result.Bins[1].MismatchFrames != 1 ||
		result.Bins[16].Frames != 616 || result.Bins[16].QueryFirstSample != 16384 || result.Bins[16].MismatchFrames != 1 || result.Bins[16].ErrorSumSquares[1] != 144 {
		t.Fatal("1024-frame bins or 64 KiB buffer transition lost actual differences")
	}
	if result.Complete || result.CapturedBytesVerified || result.NativeClockKnown || result.DecodedOriginComplete || result.ContentBound || result.Qualified {
		t.Fatal("pure range statistics acquired file, timeline or content proof")
	}
}

func TestGeneratedAVPCMOverlapDifferenceRetainsUncomparedPrefixAndSuffix(t *testing.T) {
	reference, query := make([]byte, 30*4), make([]byte, 20*4)
	for index := range reference {
		reference[index] = byte(index * 11)
	}
	copy(query[3*4:10*4], reference[5*4:12*4])
	beforeQuery := bytes.Clone(query)
	options := generatedAVPCMOverlapTestOptions(reference, query)
	options.ReferenceFirstSample, options.QueryFirstSample, options.ComparedSamples = 5, 3, 7
	result, err := compareGeneratedAVPCMOverlapDifference(context.Background(), bytes.NewReader(reference), bytes.NewReader(query), int64(len(reference)), int64(len(query)), options)
	if err != nil || !result.BytesEqual || result.WholeQueryCompared || result.UncomparedQueryPrefixSamples != 3 || result.UncomparedQuerySuffixSamples != 10 ||
		result.UncomparedReferencePrefixSamples != 5 || result.UncomparedReferenceSuffixSamples != 18 || !result.DerivedHypothesis || !bytes.Equal(query, beforeQuery) {
		t.Fatal("hypothesis interval cropped or hid unexamined captured samples")
	}
	for _, item := range []GeneratedAVPCMOverlapMismatch{result.FirstMismatch, result.LastMismatch} {
		if item.Known || item.ReferenceSample != -1 || item.QuerySample != -1 || item.Channel != -1 {
			t.Fatal("equal bytes invented a first/last mismatch or native coordinate")
		}
	}
	if result.ContentBound || result.DecodedOriginComplete || result.NativeClockKnown || result.Qualified {
		t.Fatal("a byte-equal hypothesis acquired an origin/trim/timeline claim")
	}
}

func TestGeneratedAVPCMOverlapDifferenceRejectsUnboundedPartialAndInvalidRanges(t *testing.T) {
	reference, query := make([]byte, 16), make([]byte, 16)
	base := generatedAVPCMOverlapTestOptions(reference, query)
	base.ComparedSamples = 4
	cases := map[string]GeneratedAVPCMOverlapDifferenceOptions{}
	for _, name := range []string{"zero_count", "negative_reference", "negative_query", "past_extent", "integer_overflow", "sample_cap", "wrong_rate", "wrong_channels"} {
		changed := base
		switch name {
		case "zero_count":
			changed.ComparedSamples = 0
		case "negative_reference":
			changed.ReferenceFirstSample = -1
		case "negative_query":
			changed.QueryFirstSample = -1
		case "past_extent":
			changed.QueryFirstSample = 1
		case "integer_overflow":
			changed.ReferenceFirstSample = math.MaxInt64
		case "sample_cap":
			changed.ComparedSamples = (8<<20)/4 + 1
		case "wrong_rate":
			changed.SampleRate = 44100
		case "wrong_channels":
			changed.Channels = 3
		}
		cases[name] = changed
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := compareGeneratedAVPCMOverlapDifference(context.Background(), bytes.NewReader(reference), bytes.NewReader(query), 16, 16, options)
			if err == nil || result.ComparedParsed || result.Complete || result.Qualified {
				t.Fatal("invalid hypothesis or byte/sample bound acquired evidence")
			}
		})
	}
	for _, sizes := range [][2]int64{{15, 16}, {16, 15}, {24<<20 + 4, 16}, {16, 8<<20 + 4}} {
		if _, err := compareGeneratedAVPCMOverlapDifference(context.Background(), bytes.NewReader(reference), bytes.NewReader(query), sizes[0], sizes[1], base); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("partial stereo frame or capture extent bound accepted")
		}
	}
	if result, err := compareGeneratedAVPCMOverlapDifference(context.Background(), bytes.NewReader(reference[:12]), bytes.NewReader(query), 16, 16, base); !errors.Is(err, ErrInvalidInput) || result.ComparedParsed {
		t.Fatal("short ReadAt silently compared a nominal complete range")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compareGeneratedAVPCMOverlapDifference(ctx, bytes.NewReader(reference), bytes.NewReader(query), 16, 16, base); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled scan entered streamed range reads")
	}
}
