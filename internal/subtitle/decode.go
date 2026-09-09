package subtitle

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func decode(data []byte) (string, error) {
	if len(data) > MaxInputBytes {
		return "", fmt.Errorf("%w: input exceeds %d bytes", ErrLimitExceeded, MaxInputBytes)
	}
	if len(data) >= 4 && ((data[0] == 0 && data[1] == 0 && data[2] == 0xfe && data[3] == 0xff) ||
		(data[0] == 0xff && data[1] == 0xfe && data[2] == 0 && data[3] == 0)) {
		return "", fmt.Errorf("%w: UTF-32 is not supported", ErrInvalidEncoding)
	}
	if len(data) >= 2 && ((data[0] == 0xff && data[1] == 0xfe) || (data[0] == 0xfe && data[1] == 0xff)) {
		var order binary.ByteOrder = binary.BigEndian
		if data[0] == 0xff {
			order = binary.LittleEndian
		}
		data = data[2:]
		if len(data)%2 != 0 {
			return "", fmt.Errorf("%w: odd UTF-16 byte count", ErrInvalidEncoding)
		}
		var out strings.Builder
		out.Grow(len(data))
		for i := 0; i < len(data); i += 2 {
			u := order.Uint16(data[i : i+2])
			r := rune(u)
			if u >= 0xd800 && u <= 0xdbff {
				if i+3 >= len(data) {
					return "", fmt.Errorf("%w: unpaired UTF-16 high surrogate", ErrInvalidEncoding)
				}
				v := order.Uint16(data[i+2 : i+4])
				if v < 0xdc00 || v > 0xdfff {
					return "", fmt.Errorf("%w: unpaired UTF-16 high surrogate", ErrInvalidEncoding)
				}
				r = utf16.DecodeRune(rune(u), rune(v))
				i += 2
			} else if u >= 0xdc00 && u <= 0xdfff {
				return "", fmt.Errorf("%w: unpaired UTF-16 low surrogate", ErrInvalidEncoding)
			}
			if r == 0 {
				return "", fmt.Errorf("%w: NUL character", ErrInvalidEncoding)
			}
			out.WriteRune(r)
			if out.Len() > MaxInputBytes {
				return "", fmt.Errorf("%w: decoded input exceeds %d bytes", ErrLimitExceeded, MaxInputBytes)
			}
		}
		return out.String(), nil
	}
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		data = data[3:]
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%w: expected UTF-8 or BOM-marked UTF-16; convert legacy encodings explicitly", ErrInvalidEncoding)
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return "", fmt.Errorf("%w: NUL character; UTF-16 requires a BOM", ErrInvalidEncoding)
	}
	return string(data), nil
}

func linesOf(source string) ([]string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	if strings.Count(normalized, "\n") >= MaxLineCount {
		return nil, fmt.Errorf("%w: too many lines", ErrLimitExceeded)
	}
	lines := strings.Split(normalized, "\n")
	for _, line := range lines {
		if len(line) > MaxLineBytes {
			return nil, fmt.Errorf("%w: line exceeds %d bytes", ErrLimitExceeded, MaxLineBytes)
		}
	}
	return lines, nil
}

type lineSpan struct {
	start int
	end   int
}

func sourceLineSpans(source string) []lineSpan {
	var spans []lineSpan
	start := 0
	for index := 0; index < len(source); index++ {
		if source[index] != '\r' && source[index] != '\n' {
			continue
		}
		spans = append(spans, lineSpan{start: start, end: index})
		if source[index] == '\r' && index+1 < len(source) && source[index+1] == '\n' {
			index++
		}
		start = index + 1
	}
	return append(spans, lineSpan{start: start, end: len(source)})
}
