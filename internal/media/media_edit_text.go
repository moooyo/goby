package media

import (
	"bytes"
	"unicode/utf8"
)

// RFC 8794 sections 7.4, 7.5 and 13 allow NUL termination for EBML text.
// This editing profile accepts only an all-zero remainder: it does not silently
// discard nonzero bytes hidden after a terminator. FFmpeg's fixed-length
// Matroska DURATION tag is a common producer of this neutral trailing padding.
func mediaEditEBMLText(data []byte, id uint64) (string, error) {
	if terminator := bytes.IndexByte(data, 0); terminator >= 0 {
		for _, value := range data[terminator:] {
			if value != 0 {
				return "", mediaEditContainerError("Matroska text has nonzero data after its NUL terminator")
			}
		}
		data = data[:terminator]
	}
	if !utf8.Valid(data) {
		return "", mediaEditContainerError("Matroska text has invalid UTF-8 before its terminator")
	}
	// These admitted Matroska elements have EBML's printable-ASCII String type;
	// the other text fields use its Unicode UTF-8 type.
	switch id {
	case 0x4282, 0x22B59C, 0x86, 0x4660, 0x437C, 0x447A:
		for _, value := range data {
			if value < 0x20 || value > 0x7e {
				return "", mediaEditContainerError("Matroska ASCII string contains a non-printable or non-ASCII byte")
			}
		}
	}
	return string(data), nil
}
