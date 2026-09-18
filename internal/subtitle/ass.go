package subtitle

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
)

const ticksPerCentisecond = TicksPerSecond / 100

var assTextEscapes = strings.NewReplacer("\\", `\\`, "{", `\{`, "}", `\}`, "\n", `\N`, "\u00a0", `\h`)

type assDocument struct {
	events []assEvent
	styles map[string]assStyle
}

type assEvent struct {
	line  lineSpan
	start lineSpan
	end   lineSpan
	text  lineSpan
	style string
}

type assStyle struct {
	bold      bool
	italic    bool
	underline bool
	color     string
}

func parseASS(doc Document, source string, lines []string) (Document, error) {
	doc.ass = &assDocument{styles: make(map[string]assStyle)}
	spans := sourceLineSpans(source)
	bomBytes := len(doc.source) - len(source)
	var section string
	var eventFormat, styleFormat []string
	var sawScript, sawEvents bool
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			eventFormat = nil
			styleFormat = nil
			sawScript = sawScript || section == "script info"
			sawEvents = sawEvents || section == "events"
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found || strings.HasPrefix(trimmed, ";") {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if section == "v4 styles" || section == "v4+ styles" {
			if key == "format" {
				styleFormat = assFormatFields(value)
			} else if key == "style" && len(styleFormat) > 0 {
				readASSStyle(doc.ass.styles, styleFormat, value)
			}
			continue
		}
		if section != "events" {
			continue
		}
		if key == "format" {
			eventFormat = assFormatFields(value)
			if !validASSEventFormat(eventFormat) {
				return Document{}, fmt.Errorf("%w: invalid ASS event format at line %d", ErrInvalidDocument, index+1)
			}
			continue
		}
		if key != "dialogue" {
			continue
		}
		if !validASSEventFormat(eventFormat) {
			return Document{}, fmt.Errorf("%w: ASS dialogue requires an event format at line %d", ErrInvalidDocument, index+1)
		}
		textIndex := assFieldIndex(eventFormat, "text")
		fields, ok := assFieldSpans(value, len(eventFormat), textIndex)
		if !ok {
			return Document{}, fmt.Errorf("%w: incomplete ASS dialogue at line %d", ErrInvalidDocument, index+1)
		}
		startField := fields[assFieldIndex(eventFormat, "start")]
		endField := fields[assFieldIndex(eventFormat, "end")]
		start, err := parseASSTimestamp(strings.TrimSpace(value[startField.start:startField.end]))
		if err != nil {
			return Document{}, fmt.Errorf("line %d: %w", index+1, err)
		}
		end, err := parseASSTimestamp(strings.TrimSpace(value[endField.start:endField.end]))
		if err != nil {
			return Document{}, fmt.Errorf("line %d: %w", index+1, err)
		}
		if end < start {
			return Document{}, fmt.Errorf("%w: ASS dialogue ends before it starts at line %d", ErrInvalidDocument, index+1)
		}
		textField := fields[textIndex]
		cueText := value[textField.start:textField.end]
		if len(cueText) > MaxCueTextBytes || len(doc.Cues) >= MaxCueCount {
			return Document{}, fmt.Errorf("%w: ASS dialogue limit at line %d", ErrLimitExceeded, index+1)
		}
		style := ""
		if styleIndex := assFieldIndex(eventFormat, "style"); styleIndex >= 0 {
			field := fields[styleIndex]
			style = strings.TrimSpace(value[field.start:field.end])
		}
		base := spans[index].start + strings.IndexByte(line, ':') + 1 + bomBytes
		absolute := func(field lineSpan) lineSpan {
			return lineSpan{start: base + field.start, end: base + field.end}
		}
		lineEnd := len(doc.source)
		if index+1 < len(spans) {
			lineEnd = spans[index+1].start + bomBytes
		}
		doc.ass.events = append(doc.ass.events, assEvent{
			line:  lineSpan{start: spans[index].start + bomBytes, end: lineEnd},
			start: absolute(startField), end: absolute(endField), text: absolute(textField), style: style,
		})
		doc.Cues = append(doc.Cues, Cue{StartTicks: start, EndTicks: end, Text: cueText})
	}
	if !sawScript || !sawEvents {
		return Document{}, fmt.Errorf("%w: ASS requires Script Info and Events sections", ErrInvalidDocument)
	}
	doc.sourceCues = append([]Cue(nil), doc.Cues...)
	return doc, nil
}

func assFormatFields(value string) []string {
	fields := strings.Split(value, ",")
	for index := range fields {
		fields[index] = strings.ToLower(strings.TrimSpace(fields[index]))
	}
	return fields
}

func assFieldIndex(fields []string, name string) int {
	for index, field := range fields {
		if field == name {
			return index
		}
	}
	return -1
}

func validASSEventFormat(fields []string) bool {
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if field == "" || seen[field] {
			return false
		}
		seen[field] = true
	}
	return seen["start"] && seen["end"] && seen["text"]
}

// Text is the only event field allowed to contain commas. Splitting from both
// ends also supports files that place Text before the last event column.
func assFieldSpans(value string, count, textIndex int) ([]lineSpan, bool) {
	fields := make([]lineSpan, count)
	left, right := 0, len(value)
	for index := 0; index < textIndex; index++ {
		comma := strings.IndexByte(value[left:right], ',')
		if comma < 0 {
			return nil, false
		}
		fields[index] = lineSpan{start: left, end: left + comma}
		left += comma + 1
	}
	for index := count - 1; index > textIndex; index-- {
		comma := strings.LastIndexByte(value[left:right], ',')
		if comma < 0 {
			return nil, false
		}
		comma += left
		fields[index] = lineSpan{start: comma + 1, end: right}
		right = comma
	}
	fields[textIndex] = lineSpan{start: left, end: right}
	return fields, true
}

func parseASSTimestamp(value string) (int64, error) {
	invalid := func() (int64, error) {
		return 0, fmt.Errorf("%w: invalid ASS timestamp %q", ErrInvalidDocument, value)
	}
	clock := strings.Split(value, ":")
	if len(clock) != 3 || !decimal(clock[0]) || len(clock[1]) != 2 || !decimal(clock[1]) ||
		len(clock[2]) != 5 || clock[2][2] != '.' || !decimal(clock[2][:2]) || !decimal(clock[2][3:]) {
		return invalid()
	}
	hours, err := strconv.ParseInt(clock[0], 10, 64)
	if err != nil {
		return invalid()
	}
	minutes, _ := strconv.ParseInt(clock[1], 10, 64)
	seconds, _ := strconv.ParseInt(clock[2][:2], 10, 64)
	centiseconds, _ := strconv.ParseInt(clock[2][3:], 10, 64)
	if minutes > 59 || seconds > 59 {
		return invalid()
	}
	remainder := (minutes*60+seconds)*TicksPerSecond + centiseconds*ticksPerCentisecond
	if hours > (math.MaxInt64-remainder)/(3600*TicksPerSecond) {
		return invalid()
	}
	return hours*3600*TicksPerSecond + remainder, nil
}

func formatASSTimestamp(ticks int64) string {
	centiseconds := ticks / ticksPerCentisecond
	return fmt.Sprintf("%d:%02d:%02d.%02d", centiseconds/360000, (centiseconds/6000)%60, (centiseconds/100)%60, centiseconds%100)
}

func readASSStyle(styles map[string]assStyle, format []string, value string) {
	fields := strings.Split(value, ",")
	if len(fields) != len(format) {
		return
	}
	get := func(name string) string {
		if index := assFieldIndex(format, name); index >= 0 {
			return strings.TrimSpace(fields[index])
		}
		return ""
	}
	enabled := func(name string) bool {
		number, err := strconv.ParseInt(get(name), 10, 32)
		return err == nil && number != 0
	}
	if name := get("name"); name != "" {
		styles[name] = assStyle{bold: enabled("bold"), italic: enabled("italic"), underline: enabled("underline"), color: assColor(get("primarycolour"))}
	}
}

func assColor(value string) string {
	value = strings.TrimSpace(value)
	var color uint64
	var err error
	if len(value) > 2 && strings.EqualFold(value[:2], "&h") {
		color, err = strconv.ParseUint(strings.TrimSuffix(value[2:], "&"), 16, 32)
	} else {
		var signed int64
		signed, err = strconv.ParseInt(value, 10, 32)
		color = uint64(uint32(signed))
	}
	if err != nil {
		return ""
	}
	return fmt.Sprintf("#%02X%02X%02X", color&255, (color>>8)&255, (color>>16)&255)
}

func renderASS(doc Document, sourceFormat, format Format, options Options) (Result, error) {
	var out strings.Builder
	write := func(value string) error {
		if len(value) > MaxOutputBytes-out.Len() {
			return fmt.Errorf("%w: output exceeds %d bytes", ErrLimitExceeded, MaxOutputBytes)
		}
		out.WriteString(value)
		return nil
	}
	if doc.ass != nil && sourceFormat == format && len(doc.Cues) == len(doc.ass.events) {
		cursor := 0
		for index, event := range doc.ass.events {
			if err := write(doc.source[cursor:event.line.start]); err != nil {
				return Result{}, err
			}
			cursor = event.line.end
			cue, include, err := transformCue(doc.Cues[index], options)
			if err != nil {
				return Result{}, err
			}
			if !include {
				continue
			}
			replacements := []assReplacement{
				{span: event.start, value: formatASSTimestamp(cue.StartTicks), preserveSpace: true},
				{span: event.end, value: formatASSTimestamp(cue.EndTicks), preserveSpace: true},
				{span: event.text, value: strings.ReplaceAll(cue.Text, "\n", `\N`)},
			}
			// Keep untouched timestamp spelling as well as surrounding whitespace.
			if index < len(doc.sourceCues) {
				if cue.StartTicks == doc.sourceCues[index].StartTicks {
					replacements[0].value = doc.source[event.start.start:event.start.end]
					replacements[0].preserveSpace = false
				}
				if cue.EndTicks == doc.sourceCues[index].EndTicks {
					replacements[1].value = doc.source[event.end.start:event.end.end]
					replacements[1].preserveSpace = false
				}
			}
			if err := write(replaceASSFields(doc.source, event.line, replacements)); err != nil {
				return Result{}, err
			}
		}
		if err := write(doc.source[cursor:]); err != nil {
			return Result{}, err
		}
		return Result{Data: []byte(out.String()), ContentType: contentType(format)}, nil
	}
	if err := write(assHeader(format)); err != nil {
		return Result{}, err
	}
	for index, sourceCue := range doc.Cues {
		cue, include, err := transformCue(sourceCue, options)
		if err != nil {
			return Result{}, err
		}
		if !include {
			continue
		}
		text := cue.Text
		if isASS(sourceFormat) {
			text = assPlainText(doc, index, text, FormatSRT)
		} else {
			text = renderCueText(cue, sourceFormat, FormatSRT, options)
		}
		prefix := "Dialogue: 0,"
		if format == FormatSSA {
			prefix = "Dialogue: Marked=0,"
		}
		line := prefix + formatASSTimestamp(cue.StartTicks) + "," + formatASSTimestamp(cue.EndTicks) + ",Default,,0,0,0,," + textToASS(text) + "\n"
		if err := write(line); err != nil {
			return Result{}, err
		}
	}
	return Result{Data: []byte(out.String()), ContentType: contentType(format)}, nil
}

type assReplacement struct {
	span          lineSpan
	value         string
	preserveSpace bool
}

func replaceASSFields(source string, line lineSpan, replacements []assReplacement) string {
	// There are only three replacements, so a small insertion sort avoids a
	// dependency on the order of columns in the event Format declaration.
	for i := 1; i < len(replacements); i++ {
		for j := i; j > 0 && replacements[j].span.start < replacements[j-1].span.start; j-- {
			replacements[j], replacements[j-1] = replacements[j-1], replacements[j]
		}
	}
	var out strings.Builder
	cursor := line.start
	for _, replacement := range replacements {
		out.WriteString(source[cursor:replacement.span.start])
		if replacement.preserveSpace {
			original := source[replacement.span.start:replacement.span.end]
			left := len(original) - len(strings.TrimLeft(original, " \t"))
			right := len(strings.TrimRight(original, " \t"))
			out.WriteString(original[:left])
			out.WriteString(replacement.value)
			out.WriteString(original[right:])
		} else {
			out.WriteString(replacement.value)
		}
		cursor = replacement.span.end
	}
	out.WriteString(source[cursor:line.end])
	return out.String()
}

func assHeader(format Format) string {
	if format == FormatSSA {
		return "[Script Info]\nScriptType: v4.00\n\n[V4 Styles]\n" +
			"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, TertiaryColour, BackColour, Bold, Italic, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, AlphaLevel, Encoding\n" +
			"Style: Default,Arial,20,16777215,16777215,0,0,0,0,1,1,0,2,10,10,10,0,1\n\n[Events]\n" +
			"Format: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"
	}
	return "[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\n" +
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Default,Arial,20,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1\n\n[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"
}

func assPlainText(doc Document, index int, text string, format Format) string {
	base := assStyle{}
	if doc.ass != nil && index < len(doc.ass.events) {
		base = doc.ass.styles[doc.ass.events[index].style]
	}
	state := base
	active := assStyle{}
	drawing := false
	var out strings.Builder
	writeText := func(value string) {
		if drawing || value == "" {
			return
		}
		if state != active {
			closeASSMarkup(&out, active, format)
			openASSMarkup(&out, state, format)
			active = state
		}
		out.WriteString(html.EscapeString(value))
	}
	for len(text) > 0 {
		if text[0] == '{' {
			if close := strings.IndexByte(text, '}'); close >= 0 {
				applyASSOverrides(text[1:close], base, &state, &drawing, doc.ass)
				text = text[close+1:]
				continue
			}
		}
		if len(text) >= 2 && text[0] == '\\' {
			switch text[1] {
			case 'N', 'n':
				writeText("\n")
				text = text[2:]
				continue
			case 'h':
				writeText("\u00a0")
				text = text[2:]
				continue
			case '{', '}', '\\':
				writeText(text[1:2])
				text = text[2:]
				continue
			}
		}
		length := 1
		for length < len(text) && text[length] != '{' && text[length] != '\\' {
			length++
		}
		writeText(text[:length])
		text = text[length:]
	}
	closeASSMarkup(&out, active, format)
	return out.String()
}

func applyASSOverrides(block string, base assStyle, state *assStyle, drawing *bool, doc *assDocument) {
	for index := 0; index < len(block); {
		if block[index] != '\\' {
			index++
			continue
		}
		start := index + 1
		index = start
		depth := 0
		for index < len(block) {
			if block[index] == '(' {
				depth++
			} else if block[index] == ')' && depth > 0 {
				depth--
			} else if block[index] == '\\' && depth == 0 {
				break
			}
			index++
		}
		tag := strings.TrimSpace(block[start:index])
		if tag == "" {
			continue
		}
		if tag[0] == 'r' {
			*state = base
			if doc != nil && len(tag) > 1 {
				if style, ok := doc.styles[tag[1:]]; ok {
					*state = style
				}
			}
			continue
		}
		if strings.HasPrefix(tag, "1c") || strings.HasPrefix(tag, "c") {
			value := tag[1:]
			if tag[0] == '1' {
				value = tag[2:]
			}
			if value == "" {
				state.color = base.color
			} else if color := assColor(value); color != "" {
				state.color = color
			}
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(tag[1:]))
		if err != nil {
			continue
		}
		switch tag[0] {
		case 'b':
			state.bold = value != 0
		case 'i':
			state.italic = value != 0
		case 'u':
			state.underline = value != 0
		case 'p':
			*drawing = value > 0
		}
	}
}

func openASSMarkup(out *strings.Builder, style assStyle, format Format) {
	if format == FormatSRT && style.color != "" && style.color != "#FFFFFF" {
		out.WriteString(`<font color="` + style.color + `">`)
	}
	if style.bold {
		out.WriteString("<b>")
	}
	if style.italic {
		out.WriteString("<i>")
	}
	if style.underline {
		out.WriteString("<u>")
	}
}

func closeASSMarkup(out *strings.Builder, style assStyle, format Format) {
	if style.underline {
		out.WriteString("</u>")
	}
	if style.italic {
		out.WriteString("</i>")
	}
	if style.bold {
		out.WriteString("</b>")
	}
	if format == FormatSRT && style.color != "" && style.color != "#FFFFFF" {
		out.WriteString("</font>")
	}
}

func textToASS(text string) string {
	var out strings.Builder
	for len(text) > 0 {
		if text[0] == '<' {
			if close := strings.IndexByte(text, '>'); close >= 0 {
				tag := strings.ToLower(strings.TrimSpace(text[1:close]))
				switch tag {
				case "b", "i", "u":
					out.WriteString(`{\` + tag + "1}")
				case "/b", "/i", "/u":
					out.WriteString(`{\` + tag[1:] + "0}")
				case "br", "br/", "br /":
					out.WriteString(`\N`)
				case "/font":
					out.WriteString(`{\c}`)
				default:
					if strings.HasPrefix(tag, "font ") {
						if color := htmlFontColor(tag[5:]); color != "" {
							out.WriteString(`{\c&H` + color[5:7] + color[3:5] + color[1:3] + "&}")
						}
					}
				}
				text = text[close+1:]
				continue
			}
		}
		end := strings.IndexByte(text, '<')
		if end <= 0 {
			end = len(text)
		}
		plain := html.UnescapeString(text[:end])
		plain = assTextEscapes.Replace(plain)
		out.WriteString(plain)
		text = text[end:]
	}
	return out.String()
}

func htmlFontColor(attributes string) string {
	for attributes != "" {
		attributes = strings.TrimSpace(attributes)
		key, rest, found := strings.Cut(attributes, "=")
		if !found {
			return ""
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return ""
		}
		var value string
		if rest[0] == '\'' || rest[0] == '"' {
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				return ""
			}
			value, attributes = rest[1:end+1], rest[end+2:]
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				value, attributes = rest, ""
			} else {
				value, attributes = rest[:end], rest[end:]
			}
		}
		if strings.TrimSpace(key) == "color" && len(value) == 7 && value[0] == '#' {
			if _, err := strconv.ParseUint(value[1:], 16, 24); err == nil {
				return strings.ToUpper(value)
			}
		}
	}
	return ""
}
