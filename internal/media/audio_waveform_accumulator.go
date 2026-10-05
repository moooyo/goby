package media

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"math/bits"
)

type audioWaveformAccumulator struct {
	duration, origin              int64
	stream                        Stream
	peak                          [audioWaveformFinest]float64
	energy                        [audioWaveformFinest]float64
	count                         [audioWaveformFinest]int64
	previousEnd                   int64
	seen                          bool
	startTicks, endTicks, samples int64
	quantizationSamples           int64
}

func newAudioWaveformAccumulator(duration, origin int64, stream Stream) *audioWaveformAccumulator {
	result := &audioWaveformAccumulator{duration: duration, origin: origin, stream: stream, startTicks: duration}
	// Matroska commonly stores millisecond PTS for 48 kHz audio. Adjacent frame
	// timestamps may consequently differ from sample continuity by one source
	// time-base unit. Only that declared quantization is snapped; real gaps are
	// retained and larger backward/overlapping jumps are rejected.
	if base, err := analysisTimeBase(stream.TimeBase); err == nil {
		value := new(big.Rat).Mul(base, big.NewRat(int64(stream.SampleRate), 1))
		quotient, remainder := new(big.Int).QuoRem(value.Num(), value.Denom(), new(big.Int))
		if remainder.Sign() != 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		if quotient.IsInt64() {
			result.quantizationSamples = quotient.Int64()
		}
	}
	// Coarse source clocks cannot justify inventing a long continuous stretch.
	result.quantizationSamples = min(result.quantizationSamples, int64(stream.SampleRate)/1000+1)
	return result
}

func (a *audioWaveformAccumulator) add(pts int64, pcm []byte) error {
	if a.stream.Channels < 1 || a.stream.SampleRate < 1 || len(pcm) == 0 || len(pcm)%(a.stream.Channels*4) != 0 || a.duration <= 0 {
		return ErrAnalysisUnproven
	}
	frames := int64(len(pcm) / (a.stream.Channels * 4))
	rate := int64(a.stream.SampleRate)
	if frames > audioWaveformFrameSamples || pts < -2*MaxAnalysisDurationTicks*rate/TicksPerSecond || pts > 2*MaxAnalysisDurationTicks*rate/TicksPerSecond {
		return ErrAnalysisUnproven
	}
	if a.seen {
		difference := pts - a.previousEnd
		if difference < -a.quantizationSamples {
			return fmt.Errorf("%w: overlapping waveform timestamps", ErrAnalysisUnproven)
		}
		if difference >= -a.quantizationSamples && difference <= a.quantizationSamples {
			pts = a.previousEnd
		}
	}
	a.seen, a.previousEnd = true, pts+frames
	stride := a.stream.Channels * 4
	for index := int64(0); index < frames; index++ {
		sample := pts + index
		numerator := sample*TicksPerSecond - a.origin*rate
		// Negative presentation samples and decoder tail samples outside the
		// item are deliberately not shifted or included as visible content.
		inside := numerator >= 0 && numerator < a.duration*rate
		bucket := 0
		if inside {
			// Preserve exact rational bucket boundaries without overflowing the
			// 64-bit product on long, high-rate titles or rounding twice.
			hi, lo := bits.Mul64(uint64(numerator), audioWaveformFinest)
			value, _ := bits.Div64(hi, lo, uint64(a.duration*rate))
			bucket = int(value)
		}
		var energy, peak float64
		start := int(index) * stride
		for channel := range a.stream.Channels {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[start+channel*4:])))
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("%w: non-finite waveform sample", ErrAnalysisUnproven)
			}
			energy += value * value
			peak = max(peak, math.Abs(value))
		}
		if !inside {
			continue
		}
		a.peak[bucket] = max(a.peak[bucket], peak)
		a.energy[bucket] += energy
		a.count[bucket]++
		a.samples++
		a.startTicks = min(a.startTicks, numerator/rate)
		a.endTicks = max(a.endTicks, min(a.duration, (numerator+TicksPerSecond+rate-1)/rate))
	}
	return nil
}

func audioWaveformQuantize(value float64) uint16 {
	return uint16(math.Round(min(1, max(0, value)) * 65535))
}

func (a *audioWaveformAccumulator) finish() (AudioWaveformTrack, error) {
	if !a.seen || a.samples == 0 || a.endTicks <= a.startTicks {
		return AudioWaveformTrack{}, fmt.Errorf("%w: no waveform presentation samples", ErrAnalysisUnproven)
	}
	track := AudioWaveformTrack{AudioWaveformTrackSummary: AudioWaveformTrackSummary{StreamIndex: a.stream.Index, Channels: a.stream.Channels, SampleRate: a.stream.SampleRate, ChannelLayout: a.stream.ChannelLayout, SampleCount: a.samples, CoverageStartTicks: a.startTicks, CoverageEndTicks: a.endTicks}}
	for _, buckets := range audioWaveformBucketCounts {
		level := AudioWaveformLevel{BucketCount: buckets, Peaks: make([]uint16, buckets), RMS: make([]uint16, buckets), Validity: make([]byte, buckets/8)}
		factor := audioWaveformFinest / buckets
		for index := range buckets {
			var peak, energy float64
			var count int64
			for fine := index * factor; fine < (index+1)*factor; fine++ {
				peak = max(peak, a.peak[fine])
				energy += a.energy[fine]
				count += a.count[fine]
			}
			if count == 0 {
				continue
			}
			level.Peaks[index] = audioWaveformQuantize(peak)
			level.RMS[index] = audioWaveformQuantize(math.Sqrt(energy / (float64(count) * float64(a.stream.Channels))))
			level.Validity[index/8] |= 1 << uint(index%8)
		}
		track.Levels = append(track.Levels, level)
	}
	return track, nil
}
