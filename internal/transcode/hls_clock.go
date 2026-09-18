package transcode

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// HLSMuxClock retains an exact pre-mux packet clock. The measured PTS of the
// corresponding published packet supplies the container's actual clock shift.
type HLSMuxClock struct {
	Rendition           int
	PTS                 int64
	TimeBaseNumerator   int64
	TimeBaseDenominator int64
}

func (clock HLSMuxClock) Ticks() (int64, error) {
	if clock.Rendition < 0 || clock.Rendition >= MaxHLSRenditions || clock.TimeBaseNumerator <= 0 || clock.TimeBaseNumerator > 1_000_000_000 || clock.TimeBaseDenominator <= 0 || clock.TimeBaseDenominator > 1_000_000_000 {
		return 0, ErrInvalidTimeline
	}
	value := new(big.Int).Mul(big.NewInt(clock.PTS), big.NewInt(clock.TimeBaseNumerator))
	value.Mul(value, big.NewInt(ticksPerSecond))
	denominator := big.NewInt(clock.TimeBaseDenominator)
	// Round only at the API boundary; all observer evidence remains rational.
	if value.Sign() < 0 {
		value.Sub(value, new(big.Int).Quo(denominator, big.NewInt(2)))
	} else {
		value.Add(value, new(big.Int).Quo(denominator, big.NewInt(2)))
	}
	value.Quo(value, denominator)
	if !value.IsInt64() || value.Int64() < -maxDurationTicks || value.Int64() > maxDurationTicks {
		return 0, ErrInvalidTimeline
	}
	return value.Int64(), nil
}

func needsHLSClock(p Plan) bool { return p.OutputMode == "" && p.Subtitle.Mode == "hls" }

func needsHLSCopyClock(p Plan) bool {
	return needsHLSClock(p) && (p.VideoStreamIndex >= 0 && p.VideoCodec == "copy" || p.VideoStreamIndex < 0 && p.AudioCodec == "copy")
}

type hlsClockWriter struct {
	count             int
	callback          func(Progress)
	cancel            func()
	line              [256]byte
	length            int
	next              [MaxHLSRenditions]int64
	seen              [MaxHLSRenditions]bool
	err               error
	copyReference     bool
	copyTimeBase      HLSMuxClock
	copyTimeBaseKnown bool
	copyBytes         int
	expectRendition   bool
	expectedRendition int
}

func (writer *hlsClockWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	if writer.copyReference {
		if len(data) > 8192-writer.copyBytes {
			return 0, writer.fail(ErrProgress)
		}
		writer.copyBytes += len(data)
	}
	for index, char := range data {
		if char == '\n' {
			if err := writer.consume(); err != nil {
				return index + 1, writer.fail(err)
			}
			writer.length = 0
		} else {
			if writer.length == len(writer.line) {
				return index, writer.fail(ErrProgress)
			}
			writer.line[writer.length] = char
			writer.length++
		}
	}
	return len(data), nil
}

func (writer *hlsClockWriter) fail(err error) error {
	writer.err = err
	if writer.cancel != nil {
		writer.cancel()
	}
	return err
}

func (writer *hlsClockWriter) consume() error {
	if writer.copyReference {
		return writer.consumeCopyReference()
	}
	parts := strings.Fields(string(writer.line[:writer.length]))
	if len(parts) != 5 || parts[0] != "GOBY" {
		return ErrProgress
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 0 || index >= writer.count || writer.count > MaxHLSRenditions {
		return ErrProgress
	}
	if writer.expectRendition && index != writer.expectedRendition {
		return ErrProgress
	}
	number, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || number != writer.next[index] || number >= 1<<53 {
		return ErrProgress
	}
	writer.next[index]++
	if writer.seen[index] {
		return nil
	}
	numerator, denominator, found := strings.Cut(parts[3], "/")
	if !found {
		return ErrProgress
	}
	clock := HLSMuxClock{Rendition: index}
	clock.PTS, err = strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return ErrProgress
	}
	clock.TimeBaseNumerator, err = strconv.ParseInt(numerator, 10, 64)
	if err != nil {
		return ErrProgress
	}
	clock.TimeBaseDenominator, err = strconv.ParseInt(denominator, 10, 64)
	if err != nil {
		return ErrProgress
	}
	if _, err := clock.Ticks(); err != nil {
		return ErrProgress
	}
	writer.seen[index] = true
	if writer.callback != nil {
		writer.callback(Progress{HLSClock: &clock})
	}
	return nil
}

// FFmpeg initializes stats_mux_pre only for encoder-backed streams. A copy
// reference therefore uses one packet in a timestamp-preserving framehash
// output from the same demuxer. No second input or decoder is introduced.
func (writer *hlsClockWriter) consumeCopyReference() error {
	line := strings.TrimSpace(string(writer.line[:writer.length]))
	if line == "" {
		return nil
	}
	if strings.HasPrefix(line, "#tb ") {
		if writer.count != 1 || writer.copyTimeBaseKnown || writer.seen[0] {
			return ErrProgress
		}
		stream, value, ok := strings.Cut(strings.TrimPrefix(line, "#tb "), ":")
		if !ok || strings.TrimSpace(stream) != "0" {
			return ErrProgress
		}
		numerator, denominator, ok := strings.Cut(strings.TrimSpace(value), "/")
		if !ok {
			return ErrProgress
		}
		var err error
		writer.copyTimeBase.TimeBaseNumerator, err = strconv.ParseInt(numerator, 10, 64)
		if err != nil {
			return ErrProgress
		}
		writer.copyTimeBase.TimeBaseDenominator, err = strconv.ParseInt(denominator, 10, 64)
		if err != nil {
			return ErrProgress
		}
		if _, err := writer.copyTimeBase.Ticks(); err != nil {
			return ErrProgress
		}
		writer.copyTimeBaseKnown = true
		return nil
	}
	if strings.HasPrefix(line, "#") {
		return nil
	}
	if !writer.copyTimeBaseKnown || writer.seen[0] {
		return ErrProgress
	}
	fields := strings.Split(line, ",")
	if len(fields) != 6 {
		return ErrProgress
	}
	for index := range fields {
		fields[index] = strings.TrimSpace(fields[index])
	}
	if fields[0] != "0" {
		return ErrProgress
	}
	for index := 1; index <= 4; index++ {
		value, err := strconv.ParseInt(fields[index], 10, 64)
		if err != nil || index == 3 && value < 0 || index == 4 && value <= 0 {
			return ErrProgress
		}
	}
	if len(fields[5]) != 64 {
		return ErrProgress
	}
	if _, err := hex.DecodeString(fields[5]); err != nil {
		return ErrProgress
	}
	clock := writer.copyTimeBase
	clock.PTS, _ = strconv.ParseInt(fields[2], 10, 64)
	if _, err := clock.Ticks(); err != nil {
		return ErrProgress
	}
	writer.seen[0] = true
	if writer.callback != nil {
		writer.callback(Progress{HLSClock: &clock})
	}
	return nil
}

func (writer *hlsClockWriter) finish() error {
	if writer.err != nil {
		return writer.err
	}
	if writer.length != 0 {
		return writer.fail(ErrProgress)
	}
	for index := 0; index < writer.count; index++ {
		if writer.expectRendition && index != writer.expectedRendition {
			continue
		}
		if !writer.seen[index] {
			return fmt.Errorf("%w: missing HLS packet clock", ErrProgress)
		}
	}
	return nil
}
