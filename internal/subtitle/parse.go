package subtitle

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parse accepts conventional SRT with optional decimal cue indices and WebVTT
// with a mandatory WEBVTT signature. Caption order and overlapping cues survive.
func Parse(data []byte, format Format) (Document, error) {
	normalized, err := NormalizeFormat(string(format))
	if err != nil {
		return Document{}, err
	}
	source, err := decode(data)
	if err != nil {
		return Document{}, err
	}
	lines, err := linesOf(source)
	if err != nil {
		return Document{}, err
	}
	doc := Document{Format: normalized, source: source, sourceFormat: normalized}
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		doc.source = string(data)
	}
	var spans []lineSpan
	if normalized == FormatSRT {
		spans = sourceLineSpans(source)
	}
	start := 0
	if normalized == FormatWebVTT {
		if len(lines) == 0 || !validSignature(lines[0]) {
			return Document{}, fmt.Errorf("%w: missing WEBVTT signature", ErrInvalidDocument)
		}
		doc.header = append(doc.header, lines[0])
		start = 1
		for start < len(lines) && !blank(lines[start]) {
			if strings.Contains(lines[start], "-->") {
				return Document{}, fmt.Errorf("%w: WEBVTT header requires a blank separator", ErrInvalidDocument)
			}
			doc.header = append(doc.header, lines[start])
			start++
		}
	}
	for start < len(lines) {
		if blank(lines[start]) {
			start++
			continue
		}
		end := start + 1
		for end < len(lines) && !blank(lines[end]) {
			end++
		}
		block := lines[start:end]
		lineNumber := start + 1
		start = end
		if normalized == FormatWebVTT && isMetadata(block[0]) {
			kind := strings.TrimRight(block[0], " \t")
			if (kind == "STYLE" || kind == "REGION") && len(doc.Cues) > 0 {
				return Document{}, fmt.Errorf("%w: %s block after a cue at line %d", ErrInvalidDocument, kind, lineNumber)
			}
			if kind == "STYLE" || kind == "REGION" {
				for _, line := range block[1:] {
					if strings.Contains(line, "-->") {
						return Document{}, fmt.Errorf("%w: timing delimiter in %s block at line %d", ErrInvalidDocument, kind, lineNumber)
					}
				}
			}
			doc.blocks = append(doc.blocks, metadataBlock{beforeCue: len(doc.Cues), text: strings.Join(block, "\n")})
			continue
		}
		cue, err := parseCue(block, normalized)
		if err != nil {
			return Document{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if len(doc.Cues) >= MaxCueCount {
			return Document{}, fmt.Errorf("%w: more than %d cues", ErrLimitExceeded, MaxCueCount)
		}
		doc.Cues = append(doc.Cues, cue)
		if normalized == FormatSRT {
			payloadLine := lineNumber
			if !strings.Contains(block[0], "-->") {
				payloadLine++
			}
			payload := ""
			if payloadLine < end {
				payload = source[spans[payloadLine].start:spans[end-1].end]
			}
			doc.sourcePayloads = append(doc.sourcePayloads, payload)
		}
	}
	doc.sourceCues = append([]Cue(nil), doc.Cues...)
	return doc, nil
}

func validSignature(line string) bool {
	return (line == "WEBVTT" || strings.HasPrefix(line, "WEBVTT ") || strings.HasPrefix(line, "WEBVTT\t")) && !strings.Contains(line, "-->")
}

func blank(line string) bool { return strings.Trim(line, " \t") == "" }

func isMetadata(line string) bool {
	kind := strings.TrimRight(line, " \t")
	return kind == "STYLE" || kind == "REGION" || kind == "NOTE" || strings.HasPrefix(line, "NOTE ") || strings.HasPrefix(line, "NOTE\t")
}

func parseCue(lines []string, format Format) (Cue, error) {
	var cue Cue
	timing := 0
	if !strings.Contains(lines[0], "-->") {
		if format == FormatSRT && !decimal(strings.TrimSpace(lines[0])) {
			return Cue{}, fmt.Errorf("%w: expected an SRT index or timing line", ErrInvalidDocument)
		}
		cue.Identifier = lines[0]
		timing = 1
	}
	if timing >= len(lines) {
		return Cue{}, fmt.Errorf("%w: missing timing line", ErrInvalidDocument)
	}
	parts := strings.Split(lines[timing], "-->")
	if len(parts) != 2 {
		return Cue{}, fmt.Errorf("%w: expected exactly one timing delimiter", ErrInvalidDocument)
	}
	start := strings.TrimSpace(parts[0])
	endFields := strings.Fields(parts[1])
	if len(endFields) == 0 {
		return Cue{}, fmt.Errorf("%w: missing end timestamp", ErrInvalidDocument)
	}
	var err error
	if cue.StartTicks, err = parseTimestamp(start, format); err != nil {
		return Cue{}, err
	}
	if cue.EndTicks, err = parseTimestamp(endFields[0], format); err != nil {
		return Cue{}, err
	}
	if cue.EndTicks < cue.StartTicks {
		return Cue{}, fmt.Errorf("%w: cue ends before it starts", ErrInvalidDocument)
	}
	if len(endFields) > 1 {
		if format == FormatSRT {
			return Cue{}, fmt.Errorf("%w: unsupported SRT timing suffix", ErrInvalidDocument)
		}
		for _, field := range endFields[1:] {
			if !validSetting(field) {
				return Cue{}, fmt.Errorf("%w: invalid WebVTT cue setting", ErrInvalidDocument)
			}
		}
		cue.Settings = strings.Join(endFields[1:], " ")
	}
	cue.Text = strings.Join(lines[timing+1:], "\n")
	if len(cue.Text) > MaxCueTextBytes {
		return Cue{}, fmt.Errorf("%w: cue text exceeds %d bytes", ErrLimitExceeded, MaxCueTextBytes)
	}
	return cue, nil
}

func validSetting(field string) bool {
	key, value, found := strings.Cut(field, ":")
	return found && key != "" && value != "" && !strings.ContainsAny(field, "<>\x00\r\n")
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func parseTimestamp(value string, format Format) (int64, error) {
	invalid := func() (int64, error) {
		return 0, fmt.Errorf("%w: invalid %s timestamp %q", ErrInvalidDocument, format, value)
	}
	separator := byte('.')
	if format == FormatSRT {
		separator = ','
	}
	if len(value) < 9 || value[len(value)-4] != separator || !decimal(value[len(value)-3:]) {
		return invalid()
	}
	clock := strings.Split(value[:len(value)-4], ":")
	if len(clock) != 3 && !(format == FormatWebVTT && len(clock) == 2) {
		return invalid()
	}
	var hours int64
	if len(clock) == 3 {
		if !decimal(clock[0]) || (format == FormatWebVTT && len(clock[0]) < 2) {
			return invalid()
		}
		var err error
		hours, err = strconv.ParseInt(clock[0], 10, 64)
		if err != nil {
			return invalid()
		}
		clock = clock[1:]
	}
	if len(clock[0]) != 2 || len(clock[1]) != 2 || !decimal(clock[0]) || !decimal(clock[1]) {
		return invalid()
	}
	minutes, _ := strconv.ParseInt(clock[0], 10, 64)
	seconds, _ := strconv.ParseInt(clock[1], 10, 64)
	milliseconds, _ := strconv.ParseInt(value[len(value)-3:], 10, 64)
	if minutes > 59 || seconds > 59 {
		return invalid()
	}
	remaining := (minutes*60+seconds)*TicksPerSecond + milliseconds*TicksPerMillisecond
	if hours > (math.MaxInt64-remaining)/(3600*TicksPerSecond) {
		return invalid()
	}
	return hours*3600*TicksPerSecond + remaining, nil
}
