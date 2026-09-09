// Package subtitle parses and renders bounded SRT and WebVTT text subtitles.
// It accepts UTF-8 and BOM-marked UTF-16. Legacy encodings must be converted
// explicitly by the caller rather than guessed from potentially ambiguous bytes.
package subtitle

import (
	"errors"
	"fmt"
	"strings"
)

type Format string

const (
	FormatSRT    Format = "srt"
	FormatWebVTT Format = "vtt"

	TicksPerSecond      = int64(10_000_000)
	TicksPerMillisecond = TicksPerSecond / 1000
	MaxInputBytes       = 8 << 20
	MaxOutputBytes      = 16 << 20
	MaxCueCount         = 50_000
	MaxLineCount        = 250_000
	MaxLineBytes        = 64 << 10
	MaxCueTextBytes     = 256 << 10
)

var (
	ErrUnsupportedFormat = errors.New("unsupported subtitle format")
	ErrInvalidEncoding   = errors.New("invalid or unsupported subtitle encoding")
	ErrInvalidDocument   = errors.New("invalid subtitle document")
	ErrLimitExceeded     = errors.New("subtitle limit exceeded")
	ErrInvalidRange      = errors.New("invalid subtitle time range")
)

// Cue uses 100-nanosecond ticks. Text preserves caption line breaks and markup.
// Identifier and Settings are WebVTT fields; SRT indices become identifiers.
type Cue struct {
	StartTicks int64
	EndTicks   int64
	Text       string
	Identifier string
	Settings   string
}

type Document struct {
	Format         Format
	Cues           []Cue
	header         []string
	blocks         []metadataBlock
	source         string
	sourceFormat   Format
	sourceCues     []Cue
	sourcePayloads []string
}

type metadataBlock struct {
	beforeCue int
	text      string
}

type Options struct {
	Format Format
	// StartTicks filters original cue starts before any timestamp offset.
	StartTicks int64
	// EndTicks is an exclusive cue-start limit after the timestamp offset.
	EndTicks       *int64
	CopyTimestamps bool
	// PreserveSource retains source layout for a complete, same-format response.
	// It preserves a UTF-8 BOM when present. BOM-marked UTF-16 becomes UTF-8.
	PreserveSource bool
}

type Result struct {
	Data        []byte
	ContentType string
}

func NormalizeFormat(value string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "srt", "subrip":
		return FormatSRT, nil
	case "vtt", "webvtt":
		return FormatWebVTT, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedFormat, value)
	}
}

func contentType(format Format) string {
	if format == FormatWebVTT {
		return "text/vtt"
	}
	return "text/plain"
}
