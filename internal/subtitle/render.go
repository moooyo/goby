package subtitle

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Render emits UTF-8 subtitles, canonicalizing line endings, cue numbering, and
// timestamp spelling unless complete source layout preservation was requested.
// StartTicks selects source cue starts. EndTicks limits cue starts after any
// offset, matching the observed subtitle delivery behavior of Emby 4.9.5.0.
func Render(doc Document, options Options) (Result, error) {
	sourceFormat, err := NormalizeFormat(string(doc.Format))
	if err != nil {
		return Result{}, err
	}
	if options.Format == "" {
		options.Format = sourceFormat
	}
	format, err := NormalizeFormat(string(options.Format))
	if err != nil {
		return Result{}, err
	}
	if options.StartTicks < 0 || (options.EndTicks != nil && *options.EndTicks < 0) {
		return Result{}, fmt.Errorf("%w: expected nonnegative start and end", ErrInvalidRange)
	}
	if err := validateCues(doc.Cues); err != nil {
		return Result{}, err
	}
	if options.PreserveSource && format == doc.sourceFormat && options.StartTicks == 0 && options.EndTicks == nil && unchangedCues(doc) {
		return Result{Data: []byte(doc.source), ContentType: contentType(format)}, nil
	}
	var out strings.Builder
	write := func(value string) error {
		if len(value) > MaxOutputBytes-out.Len() {
			return fmt.Errorf("%w: output exceeds %d bytes", ErrLimitExceeded, MaxOutputBytes)
		}
		out.WriteString(value)
		return nil
	}
	if format == FormatWebVTT {
		header := "WEBVTT"
		if sourceFormat == FormatWebVTT && len(doc.header) > 0 {
			header = strings.Join(doc.header, "\n")
		}
		if err := write(header + "\n\n"); err != nil {
			return Result{}, err
		}
	}
	metadataIndex := 0
	outputIndex := 0
	for index := 0; index <= len(doc.Cues); index++ {
		if sourceFormat == FormatWebVTT && format == FormatWebVTT {
			for metadataIndex < len(doc.blocks) && doc.blocks[metadataIndex].beforeCue <= index {
				if err := write(doc.blocks[metadataIndex].text + "\n\n"); err != nil {
					return Result{}, err
				}
				metadataIndex++
			}
		}
		if index == len(doc.Cues) {
			break
		}
		cue, offset, include := transformCue(doc.Cues[index], options)
		if !include {
			continue
		}
		outputIndex++
		var block strings.Builder
		if format == FormatSRT {
			block.WriteString(strconv.Itoa(outputIndex))
			block.WriteByte('\n')
		} else if sourceFormat == FormatWebVTT && cue.Identifier != "" {
			block.WriteString(cue.Identifier)
			block.WriteByte('\n')
		}
		block.WriteString(formatTimestamp(cue.StartTicks, format))
		block.WriteString(" --> ")
		block.WriteString(formatTimestamp(cue.EndTicks, format))
		if format == FormatWebVTT && cue.Settings != "" {
			block.WriteByte(' ')
			block.WriteString(cue.Settings)
		}
		block.WriteByte('\n')
		if sourceFormat == FormatSRT && format == FormatSRT && index < len(doc.sourcePayloads) && index < len(doc.sourceCues) && cue.Text == doc.sourceCues[index].Text {
			cue.Text = doc.sourcePayloads[index]
		}
		block.WriteString(renderCueText(cue, sourceFormat, format, offset))
		block.WriteString("\n\n")
		if err := write(block.String()); err != nil {
			return Result{}, err
		}
	}
	resultText := out.String()
	if format == FormatWebVTT && outputIndex > 0 {
		resultText = strings.TrimSuffix(resultText, "\n")
	}
	return Result{Data: []byte(resultText), ContentType: contentType(format)}, nil
}

func unchangedCues(doc Document) bool {
	if doc.sourceFormat == "" || len(doc.Cues) != len(doc.sourceCues) {
		return false
	}
	for index := range doc.Cues {
		if doc.Cues[index] != doc.sourceCues[index] {
			return false
		}
	}
	return true
}

func validateCues(cues []Cue) error {
	if len(cues) > MaxCueCount {
		return fmt.Errorf("%w: more than %d cues", ErrLimitExceeded, MaxCueCount)
	}
	lineCount := 0
	for index, cue := range cues {
		if cue.StartTicks < 0 || cue.EndTicks < cue.StartTicks {
			return fmt.Errorf("%w: invalid timing in cue %d", ErrInvalidDocument, index+1)
		}
		if len(cue.Text) > MaxCueTextBytes || len(cue.Identifier) > MaxLineBytes || len(cue.Settings) > MaxLineBytes {
			return fmt.Errorf("%w: cue %d is too large", ErrLimitExceeded, index+1)
		}
		for _, value := range []string{cue.Text, cue.Identifier, cue.Settings} {
			if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
				return fmt.Errorf("%w: invalid cue text", ErrInvalidEncoding)
			}
		}
		if strings.ContainsAny(cue.Identifier, "\r\n") || strings.Contains(cue.Identifier, "-->") || isMetadata(cue.Identifier) {
			return fmt.Errorf("%w: invalid cue identifier", ErrInvalidDocument)
		}
		if strings.ContainsAny(cue.Settings, "\r\n<>") {
			return fmt.Errorf("%w: invalid cue settings", ErrInvalidDocument)
		}
		for _, field := range strings.Fields(cue.Settings) {
			if !validSetting(field) {
				return fmt.Errorf("%w: invalid cue settings", ErrInvalidDocument)
			}
		}
		lines, err := linesOf(cue.Text)
		if err != nil {
			return err
		}
		lineCount += len(lines) + 3
		if lineCount > MaxLineCount {
			return fmt.Errorf("%w: too many output lines", ErrLimitExceeded)
		}
		for _, line := range lines {
			if len(lines) > 1 && blank(line) {
				return fmt.Errorf("%w: blank line inside cue text", ErrInvalidDocument)
			}
		}
		if strings.ContainsRune(cue.Text, '\r') {
			return fmt.Errorf("%w: cue text must use LF line endings", ErrInvalidDocument)
		}
	}
	return nil
}

func formatTimestamp(ticks int64, format Format) string {
	milliseconds := ticks / TicksPerMillisecond
	hours := milliseconds / 3_600_000
	minutes := (milliseconds / 60_000) % 60
	seconds := (milliseconds / 1000) % 60
	if format == FormatWebVTT && hours == 0 {
		return fmt.Sprintf("%02d:%02d.%03d", minutes, seconds, milliseconds%1000)
	}
	separator := "."
	if format == FormatSRT {
		separator = ","
	}
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", hours, minutes, seconds, separator, milliseconds%1000)
}

// transformCue is the only place that applies selection and timeline offsets.
func transformCue(cue Cue, options Options) (Cue, int64, bool) {
	if cue.StartTicks < options.StartTicks {
		return Cue{}, 0, false
	}
	var offset int64
	if !options.CopyTimestamps {
		offset = options.StartTicks
		cue.StartTicks -= options.StartTicks
		cue.EndTicks -= options.StartTicks
	}
	if options.EndTicks != nil && cue.StartTicks >= *options.EndTicks {
		return Cue{}, 0, false
	}
	return cue, offset, true
}

// WebVTT inline timestamps share the cue timeline. A shifted cue must not retain
// absolute inline timestamps, and SRT has no representation for these markers.
func renderCueText(cue Cue, sourceFormat, outputFormat Format, offset int64) string {
	if sourceFormat != FormatWebVTT || !strings.ContainsRune(cue.Text, '<') {
		return cue.Text
	}
	var out strings.Builder
	text := cue.Text
	for text != "" {
		open := strings.IndexByte(text, '<')
		if open < 0 {
			out.WriteString(text)
			break
		}
		close := strings.IndexByte(text[open+1:], '>')
		if close < 0 {
			out.WriteString(text)
			break
		}
		close += open + 1
		out.WriteString(text[:open])
		marker := text[open+1 : close]
		var timestamp int64
		var timestampMarker bool
		if len(marker) >= 9 && marker[0] >= '0' && marker[0] <= '9' {
			var err error
			timestamp, err = parseTimestamp(marker, FormatWebVTT)
			timestampMarker = err == nil
		}
		if !timestampMarker {
			out.WriteString(text[open : close+1])
		} else if outputFormat == FormatWebVTT && timestamp >= offset {
			timestamp -= offset
			if timestamp > cue.StartTicks && timestamp < cue.EndTicks {
				out.WriteByte('<')
				out.WriteString(formatTimestamp(timestamp, FormatWebVTT))
				out.WriteByte('>')
			}
		}
		text = text[close+1:]
	}
	return out.String()
}
