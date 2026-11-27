package metadata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type nfoEncoding string

const (
	nfoUTF8    nfoEncoding = "UTF-8"
	nfoUTF16LE nfoEncoding = "UTF-16LE"
	nfoUTF16BE nfoEncoding = "UTF-16BE"
)

func (enc nfoEncoding) matchesDeclaration(label string) bool {
	return strings.EqualFold(label, string(enc)) || enc != nfoUTF8 && strings.EqualFold(label, "UTF-16")
}

// nfoUTF8Bytes validates the wire bytes and decodes only BOM-marked UTF-16.
// The input is already bounded by maxNFOBytes. Transcoding can expand at most
// 3 UTF-8 bytes per 2 input bytes; the XML field limits still count UTF-8 bytes.
func nfoUTF8Bytes(data []byte) ([]byte, nfoEncoding, error) {
	var order binary.ByteOrder
	var enc nfoEncoding
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		order, enc = binary.LittleEndian, nfoUTF16LE
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		order, enc = binary.BigEndian, nfoUTF16BE
	default:
		if !utf8.Valid(data) {
			return nil, nfoUTF8, fmt.Errorf("NFO input must be valid UTF-8 or BOM-marked UTF-16")
		}
		return bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), nfoUTF8, nil
	}

	data = data[2:]
	if len(data)%2 != 0 {
		return nil, enc, fmt.Errorf("NFO UTF-16 input has a truncated code unit")
	}
	decoded := make([]byte, 0, len(data)/2)
	for len(data) != 0 {
		ch := rune(order.Uint16(data))
		data = data[2:]
		if utf16.IsSurrogate(ch) {
			if len(data) == 0 {
				return nil, enc, fmt.Errorf("NFO UTF-16 input has an unpaired surrogate")
			}
			ch = utf16.DecodeRune(ch, rune(order.Uint16(data)))
			if ch == utf8.RuneError {
				return nil, enc, fmt.Errorf("NFO UTF-16 input has an unpaired surrogate")
			}
			data = data[2:]
		}
		decoded = utf8.AppendRune(decoded, ch)
	}
	return decoded, enc, nil
}
