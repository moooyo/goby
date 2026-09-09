package media

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"
)

const (
	MaxVideoSeekCandidateBytes   = 64 * 1024
	maxVideoSeekCandidateEntries = 64
)

// VideoSeekCandidate retains the proposed input argument and a bounded window of
// restart evidence. It is private preparation data, not playback authorization.
type VideoSeekCandidate struct {
	Version             int            `json:"version"`
	RequestedStartTicks int64          `json:"requested_start_ticks"`
	InputSeekTicks      int64          `json:"input_seek_ticks"`
	Index               VideoSeekIndex `json:"index"`
}

// VideoSeekPointTime returns an exact source-clock PTS for a validated index.
func VideoSeekPointTime(index VideoSeekIndex, point VideoSeekPoint) *big.Rat {
	return new(big.Rat).Mul(new(big.Rat).SetInt64(point.PTS),
		videoSeekTimeBase(index.TimeBaseNumerator, index.TimeBaseDenominator))
}

// VideoSeekRequestedTime returns the absolute source position without an
// overflowing intermediate addition or a floating-point conversion.
func VideoSeekRequestedTime(index VideoSeekIndex, requestedTicks int64) *big.Rat {
	ticks := new(big.Int).Add(big.NewInt(index.FormatStartTicks), big.NewInt(requestedTicks))
	return new(big.Rat).SetFrac(ticks, big.NewInt(TicksPerSecond))
}

// SelectVideoSeekCandidate selects the final native PTS at or before the
// requested position and preserves at most 64 entries ending at that point.
func SelectVideoSeekCandidate(index VideoSeekIndex, requestedTicks int64) (string, error) {
	if err := ValidateVideoSeekIndex(index); err != nil {
		return "", err
	}
	if requestedTicks <= 0 || requestedTicks >= index.DurationTicks {
		return "", fmt.Errorf("video seek request is outside the source duration")
	}
	requested := VideoSeekRequestedTime(index, requestedTicks)
	last := -1
	for position, point := range index.Entries {
		if VideoSeekPointTime(index, point).Cmp(requested) > 0 {
			break
		}
		last = position
	}
	if last < 0 {
		return "", fmt.Errorf("video seek request has no preceding IDR evidence")
	}
	inputTicks, err := videoSeekInputTicks(index, index.Entries[last])
	if err != nil {
		return "", err
	}
	first := max(0, last-maxVideoSeekCandidateEntries+1)
	index.Entries = append([]VideoSeekPoint(nil), index.Entries[first:last+1]...)
	candidate := VideoSeekCandidate{
		Version: VideoSeekIndexVersion, RequestedStartTicks: requestedTicks,
		InputSeekTicks: inputTicks, Index: index,
	}
	if err := validateVideoSeekCandidate(candidate); err != nil {
		return "", err
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return "", fmt.Errorf("encode video seek candidate: %w", err)
	}
	if len(data) > MaxVideoSeekCandidateBytes {
		return "", fmt.Errorf("video seek candidate exceeds its byte budget")
	}
	return string(data), nil
}

// ValidateVideoSeekCandidate rejects unknown fields, trailing input, and
// candidate arguments that do not follow from their final indexed native PTS.
func ValidateVideoSeekCandidate(encoded string) (VideoSeekCandidate, error) {
	if len(encoded) == 0 || len(encoded) > MaxVideoSeekCandidateBytes {
		return VideoSeekCandidate{}, fmt.Errorf("video seek candidate exceeds its byte budget")
	}
	var candidate VideoSeekCandidate
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return VideoSeekCandidate{}, fmt.Errorf("decode video seek candidate: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return VideoSeekCandidate{}, fmt.Errorf("video seek candidate has trailing data")
	}
	if err := validateVideoSeekCandidate(candidate); err != nil {
		return VideoSeekCandidate{}, err
	}
	return candidate, nil
}

func validateVideoSeekCandidate(candidate VideoSeekCandidate) error {
	index := candidate.Index
	if candidate.Version != VideoSeekIndexVersion || candidate.RequestedStartTicks <= 0 ||
		candidate.RequestedStartTicks >= index.DurationTicks || candidate.InputSeekTicks <= 0 ||
		candidate.InputSeekTicks > candidate.RequestedStartTicks || len(index.Entries) > maxVideoSeekCandidateEntries {
		return fmt.Errorf("invalid video seek candidate metadata")
	}
	if err := ValidateVideoSeekIndex(index); err != nil {
		return err
	}
	last := index.Entries[len(index.Entries)-1]
	if VideoSeekPointTime(index, last).Cmp(VideoSeekRequestedTime(index, candidate.RequestedStartTicks)) > 0 {
		return fmt.Errorf("video seek candidate follows the requested position")
	}
	inputTicks, err := videoSeekInputTicks(index, last)
	if err != nil {
		return err
	}
	if candidate.InputSeekTicks != inputTicks {
		return fmt.Errorf("video seek candidate input argument does not match its evidence")
	}
	return nil
}

func videoSeekInputTicks(index VideoSeekIndex, point VideoSeekPoint) (int64, error) {
	ticks := VideoSeekPointTime(index, point)
	ticks.Mul(ticks, new(big.Rat).SetInt64(TicksPerSecond))
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(ticks.Num(), ticks.Denom(), remainder)
	if remainder.Sign() < 0 {
		whole.Sub(whole, big.NewInt(1))
	}
	whole.Sub(whole, big.NewInt(index.FormatStartTicks))
	if !whole.IsInt64() || whole.Sign() <= 0 {
		return 0, fmt.Errorf("video seek candidate has no positive representable input argument")
	}
	return whole.Int64(), nil
}
