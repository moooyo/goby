package transcode

import (
	"context"
	"encoding/binary"
	"math"
)

const (
	generatedAVPCMContentMaxCandidates = 8193
	generatedAVPCMContentMaxWindow     = 4096
)

// These sample indices belong to captured PCM byte streams. They are content
// hypotheses and never become a native PTS, decoder origin, edit or audible end.
// EncodedSourceSHA256 is caller context checked against capture receipts; reading
// two PCM files cannot independently re-prove their encoded-source association.
type GeneratedAVPCMContentOptions struct {
	Channels                      int
	ReferenceSHA256, QuerySHA256  [32]byte
	EncodedSourceSHA256           [32]byte
	ReferenceFirstCandidateSample int64
	QueryFirstSample              int64
	CandidateCount, WindowSamples int
}

// Thresholds are fixed before actual capture: positive centered correlation on
// every channel, and a unique winner outside a 32-sample exclusion neighborhood.
// Sparse correlation is a diagnostic landmark, not complete sample coverage.
type GeneratedAVPCMContentCandidate struct {
	Qualified, Complete, CapturedBytesVerified               bool
	NativeClockKnown, DecodedOriginComplete, ContentBound    bool
	Status                                                   GeneratedAVAssociationCandidateStatus
	EncodedSourceSHA256, ReferenceSHA256, QuerySHA256        [32]byte
	ReferenceBytes, QueryBytes                               int64
	ReferenceSampleHypothesis, QueryFirstSample              int64
	ReferenceFirstCandidateSample                            int64
	DisplacementSamples                                      int64
	Channels, CandidateCount, WindowSamples, SampleStride    int
	ExclusionRadiusSamples                                   int
	ComparedValuesPerChannel                                 int
	MinimumCorrelation, MinimumMargin                        float64
	BestChannelCorrelation                                   [2]float64
	BestMinimumCorrelation, SecondMinimumCorrelation, Margin float64
	SecondCandidateKnown                                     bool
}

func generatedAVPCMContentOptionsValid(options GeneratedAVPCMContentOptions) bool {
	return (options.Channels == 1 || options.Channels == 2) &&
		options.ReferenceSHA256 != ([32]byte{}) && options.QuerySHA256 != ([32]byte{}) && options.EncodedSourceSHA256 != ([32]byte{}) &&
		options.ReferenceFirstCandidateSample >= 0 && options.QueryFirstSample >= 0 &&
		options.ReferenceFirstCandidateSample <= generatedAVPCMBytes/int64(options.Channels*2) && options.QueryFirstSample <= (8<<20)/int64(options.Channels*2) &&
		options.CandidateCount >= 1 && options.CandidateCount <= generatedAVPCMContentMaxCandidates &&
		options.WindowSamples >= 256 && options.WindowSamples <= generatedAVPCMContentMaxWindow && options.WindowSamples%4 == 0
}

// The caller provides only the bounded candidate span and query window. Pure
// comparison cannot mark these bytes as captured, joined or source-bound.
func compareGeneratedAVPCMContentWindows(ctx context.Context, reference, query []byte, options GeneratedAVPCMContentOptions) (GeneratedAVPCMContentCandidate, error) {
	result := GeneratedAVPCMContentCandidate{Status: GeneratedAVAssociationMissing, ReferenceSampleHypothesis: -1,
		EncodedSourceSHA256: options.EncodedSourceSHA256, ReferenceSHA256: options.ReferenceSHA256, QuerySHA256: options.QuerySHA256,
		QueryFirstSample: options.QueryFirstSample, CandidateCount: options.CandidateCount, WindowSamples: options.WindowSamples,
		ReferenceFirstCandidateSample: options.ReferenceFirstCandidateSample, Channels: options.Channels,
		SampleStride: 4, ExclusionRadiusSamples: 32, MinimumCorrelation: 0.90, MinimumMargin: 0.01,
		BestMinimumCorrelation: -2, SecondMinimumCorrelation: -2}
	if ctx == nil || !generatedAVPCMContentOptionsValid(options) {
		return result, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	frameBytes := options.Channels * 2
	if len(reference) != (options.CandidateCount-1+options.WindowSamples)*frameBytes || len(query) != options.WindowSamples*frameBytes {
		return result, ErrInvalidInput
	}
	n := options.WindowSamples / result.SampleStride
	result.ComparedValuesPerChannel = n
	var querySum, querySquares, queryVariance [2]float64
	for frame := 0; frame < options.WindowSamples; frame += result.SampleStride {
		for channel := 0; channel < options.Channels; channel++ {
			value := float64(int16(binary.LittleEndian.Uint16(query[frame*frameBytes+channel*2:])))
			querySum[channel] += value
			querySquares[channel] += value * value
		}
	}
	for channel := 0; channel < options.Channels; channel++ {
		queryVariance[channel] = querySquares[channel] - querySum[channel]*querySum[channel]/float64(n)
		if queryVariance[channel] <= float64(n) {
			return result, nil
		}
	}
	type score struct {
		channels [2]float64
		minimum  float64
	}
	scores := make([]score, options.CandidateCount)
	best := -1
	for candidate := range scores {
		if candidate%64 == 0 {
			if err := ctx.Err(); err != nil {
				return result, err
			}
		}
		var sum, squares, product [2]float64
		for frame := 0; frame < options.WindowSamples; frame += result.SampleStride {
			for channel := 0; channel < options.Channels; channel++ {
				r := float64(int16(binary.LittleEndian.Uint16(reference[(candidate+frame)*frameBytes+channel*2:])))
				q := float64(int16(binary.LittleEndian.Uint16(query[frame*frameBytes+channel*2:])))
				sum[channel] += r
				squares[channel] += r * r
				product[channel] += r * q
			}
		}
		value := score{minimum: 1}
		for channel := 0; channel < options.Channels; channel++ {
			variance := squares[channel] - sum[channel]*sum[channel]/float64(n)
			correlation := -2.0
			if variance > float64(n) {
				correlation = (product[channel] - sum[channel]*querySum[channel]/float64(n)) / math.Sqrt(variance*queryVariance[channel])
				correlation = max(-1, min(1, correlation))
			}
			value.channels[channel] = correlation
			value.minimum = min(value.minimum, correlation)
		}
		scores[candidate] = value
		if best == -1 || value.minimum > scores[best].minimum {
			best = candidate
		}
	}
	if best < 0 || scores[best].minimum == -2 {
		return result, nil
	}
	result.ReferenceSampleHypothesis = options.ReferenceFirstCandidateSample + int64(best)
	result.DisplacementSamples = result.ReferenceSampleHypothesis - options.QueryFirstSample
	result.BestChannelCorrelation, result.BestMinimumCorrelation = scores[best].channels, scores[best].minimum
	for candidate, value := range scores {
		if candidate < best-result.ExclusionRadiusSamples || candidate > best+result.ExclusionRadiusSamples {
			if value.minimum > -2 {
				result.SecondCandidateKnown = true
				result.SecondMinimumCorrelation = max(result.SecondMinimumCorrelation, value.minimum)
			}
		}
	}
	if result.SecondCandidateKnown {
		result.Margin = result.BestMinimumCorrelation - result.SecondMinimumCorrelation
	}
	if result.BestMinimumCorrelation >= result.MinimumCorrelation {
		result.Status = GeneratedAVAssociationAmbiguous
		if result.SecondCandidateKnown && result.Margin >= result.MinimumMargin {
			result.Status = GeneratedAVAssociationUnique
		}
	}
	return result, ctx.Err()
}
