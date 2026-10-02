package transcode

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
)

const (
	generatedAVPCMOverlapBinFrames = 1024
	generatedAVPCMOverlapMaxBins   = 8192
)

// These coordinates are explicit hypotheses on captured PCM byte grids. They
// are not native timestamps, selected-source origins, trims or audible endpoints.
// EncodedSourceSHA256 is caller context, not independently proved by PCM reads.
type GeneratedAVPCMOverlapDifferenceOptions struct {
	Channels                                          int
	SampleRate                                        int64
	ReferenceSHA256, QuerySHA256, EncodedSourceSHA256 [32]byte
	ReferenceFirstSample, QueryFirstSample            int64
	ComparedSamples                                   int64
}

type GeneratedAVPCMOverlapDifferenceBin struct {
	ReferenceFirstSample, QueryFirstSample int64
	Frames, MismatchFrames                 int64
	MismatchValues                         [2]int64
	ErrorSumSquares                        [2]uint64
	MaximumAbsoluteDifference              [2]int32
}

// First/last mismatches retain actual signed PCM values. A no-mismatch result
// has Known=false and sample/channel sentinel -1, never an invented native clock.
type GeneratedAVPCMOverlapMismatch struct {
	Known                        bool
	ReferenceSample, QuerySample int64
	Channel                      int
	ReferenceValue, QueryValue   int16
}

// Byte equality describes only the explicitly compared captured-byte range.
// Every unexamined prefix/suffix stays visible; it is neither cropped nor called
// padding/trim. Complete proves full hashes, range reads and held FD fences only.
// Error results describe only the already read prefix. ComparedParsed/Complete
// are required before treating LastMismatch or bins as the entire requested range.
type GeneratedAVPCMOverlapDifferenceDiagnostic struct {
	Qualified, Complete, CapturedBytesVerified, ComparedParsed         bool
	BytesEqual, WholeQueryCompared, DerivedHypothesis                  bool
	NativeClockKnown, DecodedOriginComplete, ContentBound              bool
	Options                                                            GeneratedAVPCMOverlapDifferenceOptions
	ReferenceBytes, QueryBytes                                         int64
	ReferenceSamples, QuerySamples                                     int64
	UncomparedQueryPrefixSamples, UncomparedQuerySuffixSamples         int64
	UncomparedReferencePrefixSamples, UncomparedReferenceSuffixSamples int64
	MismatchFrames                                                     int64
	MismatchValues                                                     [2]int64
	ErrorSumSquares                                                    [2]uint64
	MaximumAbsoluteDifference                                          [2]int32
	FirstMismatch, LastMismatch                                        GeneratedAVPCMOverlapMismatch
	Bins                                                               []GeneratedAVPCMOverlapDifferenceBin
}

func generatedAVPCMOverlapOptionsValid(options GeneratedAVPCMOverlapDifferenceOptions) bool {
	return (options.Channels == 1 || options.Channels == 2) && options.SampleRate == 48000 &&
		options.ReferenceSHA256 != ([32]byte{}) && options.QuerySHA256 != ([32]byte{}) && options.EncodedSourceSHA256 != ([32]byte{}) &&
		options.ReferenceFirstSample >= 0 && options.QueryFirstSample >= 0 && options.ComparedSamples > 0 &&
		options.ComparedSamples <= (8<<20)/int64(options.Channels*2)
}

// compareGeneratedAVPCMOverlapDifference reads every byte in the explicit range
// with two fixed 64 KiB buffers. Pure range statistics grant no full-hash/identity
// or joined capture claim. The Linux wrapper independently checks full extents.
func compareGeneratedAVPCMOverlapDifference(ctx context.Context, reference, query io.ReaderAt, referenceBytes, queryBytes int64, options GeneratedAVPCMOverlapDifferenceOptions) (GeneratedAVPCMOverlapDifferenceDiagnostic, error) {
	var empty GeneratedAVPCMOverlapDifferenceDiagnostic
	if ctx == nil || reference == nil || query == nil || !generatedAVPCMOverlapOptionsValid(options) {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	frameBytes := int64(options.Channels * 2)
	if referenceBytes < frameBytes || referenceBytes > 24<<20 || queryBytes < frameBytes || queryBytes > 8<<20 || referenceBytes%frameBytes != 0 || queryBytes%frameBytes != 0 {
		return empty, ErrInvalidInput
	}
	referenceSamples, querySamples := referenceBytes/frameBytes, queryBytes/frameBytes
	if options.ReferenceFirstSample > referenceSamples-options.ComparedSamples || options.QueryFirstSample > querySamples-options.ComparedSamples {
		return empty, ErrInvalidInput
	}
	sentinel := GeneratedAVPCMOverlapMismatch{ReferenceSample: -1, QuerySample: -1, Channel: -1}
	result := GeneratedAVPCMOverlapDifferenceDiagnostic{Options: options, DerivedHypothesis: true, BytesEqual: true,
		ReferenceBytes: referenceBytes, QueryBytes: queryBytes, ReferenceSamples: referenceSamples, QuerySamples: querySamples,
		UncomparedReferencePrefixSamples: options.ReferenceFirstSample, UncomparedReferenceSuffixSamples: referenceSamples - options.ReferenceFirstSample - options.ComparedSamples,
		UncomparedQueryPrefixSamples: options.QueryFirstSample, UncomparedQuerySuffixSamples: querySamples - options.QueryFirstSample - options.ComparedSamples,
		WholeQueryCompared: options.QueryFirstSample == 0 && options.ComparedSamples == querySamples, FirstMismatch: sentinel, LastMismatch: sentinel}
	var referenceBuffer, queryBuffer [64 << 10]byte
	for compared := int64(0); compared < options.ComparedSamples; {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		frames := min(int64(len(referenceBuffer))/frameBytes, options.ComparedSamples-compared)
		bytes := int(frames * frameBytes)
		if n, err := reference.ReadAt(referenceBuffer[:bytes], (options.ReferenceFirstSample+compared)*frameBytes); err != nil || n != bytes {
			return result, errors.Join(ErrInvalidInput, err)
		}
		if n, err := query.ReadAt(queryBuffer[:bytes], (options.QueryFirstSample+compared)*frameBytes); err != nil || n != bytes {
			return result, errors.Join(ErrInvalidInput, err)
		}
		for frame := int64(0); frame < frames; frame++ {
			ordinal := compared + frame
			if ordinal%generatedAVPCMOverlapBinFrames == 0 {
				if len(result.Bins) >= generatedAVPCMOverlapMaxBins {
					return result, ErrTimelineLimit
				}
				result.Bins = append(result.Bins, GeneratedAVPCMOverlapDifferenceBin{ReferenceFirstSample: options.ReferenceFirstSample + ordinal, QueryFirstSample: options.QueryFirstSample + ordinal})
			}
			bin := &result.Bins[len(result.Bins)-1]
			mismatched := false
			for channel := 0; channel < options.Channels; channel++ {
				byteOffset := int(frame*frameBytes) + channel*2
				r := int16(binary.LittleEndian.Uint16(referenceBuffer[byteOffset:]))
				q := int16(binary.LittleEndian.Uint16(queryBuffer[byteOffset:]))
				difference := int64(q) - int64(r)
				if difference == 0 {
					continue
				}
				mismatched = true
				result.BytesEqual = false
				result.MismatchValues[channel]++
				bin.MismatchValues[channel]++
				absolute := difference
				if absolute < 0 {
					absolute = -absolute
				}
				result.MaximumAbsoluteDifference[channel] = max(result.MaximumAbsoluteDifference[channel], int32(absolute))
				bin.MaximumAbsoluteDifference[channel] = max(bin.MaximumAbsoluteDifference[channel], int32(absolute))
				squared := uint64(difference * difference)
				// Query <=8 MiB gives <=2^22 mono frames and each square
				// is <2^32: each total is <2^54. Keep an explicit guard too.
				if squared > ^uint64(0)-result.ErrorSumSquares[channel] || squared > ^uint64(0)-bin.ErrorSumSquares[channel] {
					return result, ErrTimelineLimit
				}
				result.ErrorSumSquares[channel] += squared
				bin.ErrorSumSquares[channel] += squared
				item := GeneratedAVPCMOverlapMismatch{Known: true, ReferenceSample: options.ReferenceFirstSample + ordinal, QuerySample: options.QueryFirstSample + ordinal, Channel: channel, ReferenceValue: r, QueryValue: q}
				if !result.FirstMismatch.Known {
					result.FirstMismatch = item
				}
				result.LastMismatch = item
			}
			bin.Frames++
			if mismatched {
				result.MismatchFrames++
				bin.MismatchFrames++
			}
		}
		compared += frames
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.ComparedParsed = true
	return result, nil
}
