package transcode

import (
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"strings"
)

const (
	liveJournalLineBytes       = 256
	liveJournalPipeBytes       = 4096
	liveScratchFileLimit       = 1024
	liveMaxSegmentTicks  int64 = 120 * ticksPerSecond
)

type liveJournalRecord struct {
	name       string
	sequence   int64
	start, end int64
}

func liveSegmentName(p Plan, rendition int, sequence int64, temporary bool) string {
	extension := "ts"
	if p.HLS.SegmentType == "fmp4" {
		extension = "m4s"
	}
	name := fmt.Sprintf("%ssegment-%06d.%s", hlsPrefix(rendition, p.HLS.RenditionCount), sequence, extension)
	if temporary {
		name += ".tmp"
	}
	return name
}

func parseLiveJournalRecord(line string, p Plan, rendition int, sequence int64) (liveJournalRecord, error) {
	return parseLiveJournalNamedRecord(line, liveSegmentName(p, rendition, sequence, true), sequence)
}

func parseLiveJournalNamedRecord(line, expectedName string, sequence int64) (liveJournalRecord, error) {
	invalid := func() (liveJournalRecord, error) { return liveJournalRecord{}, ErrInvalidTimeline }
	if len(line) == 0 || len(line) > liveJournalLineBytes || !strings.HasSuffix(line, "\n") || sequence < 0 || sequence > 1<<31-1 {
		return invalid()
	}
	reader := csv.NewReader(strings.NewReader(line))
	reader.FieldsPerRecord = 3
	fields, err := reader.Read()
	if err != nil {
		return invalid()
	}
	if _, err = reader.Read(); err != io.EOF {
		return invalid()
	}
	if fields[0] != expectedName {
		return invalid()
	}
	start, err := liveSecondsTicks(fields[1])
	if err != nil {
		return invalid()
	}
	end, err := liveSecondsTicks(fields[2])
	if err != nil {
		return invalid()
	}
	// FFmpeg initializes the first CSV start to zero; its real first packet is
	// supplied independently by HLSMuxClock. Subsequent starts are key packets.
	if sequence > 0 && (end <= start || end-start > liveMaxSegmentTicks) {
		return invalid()
	}
	return liveJournalRecord{name: fields[0], sequence: sequence, start: start, end: end}, nil
}

func liveSecondsTicks(value string) (int64, error) {
	if len(value) == 0 || len(value) > 32 {
		return 0, ErrInvalidTimeline
	}
	digits, dots := 0, 0
	for position, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
			digits++
		case ch == '.':
			dots++
		case position == 0 && ch == '-':
		default:
			return 0, ErrInvalidTimeline
		}
	}
	if digits == 0 || dots > 1 {
		return 0, ErrInvalidTimeline
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0, ErrInvalidTimeline
	}
	r.Mul(r, new(big.Rat).SetInt64(ticksPerSecond))
	if !r.IsInt() || !r.Num().IsInt64() {
		return 0, ErrInvalidTimeline
	}
	valueTicks := r.Num().Int64()
	if valueTicks < 0 {
		return 0, ErrInvalidTimeline
	}
	return valueTicks, nil
}

func liveTicksClose(a, b int64) bool {
	if a < b {
		a, b = b, a
	}
	return a-b <= LiveAlignmentTicks
}
