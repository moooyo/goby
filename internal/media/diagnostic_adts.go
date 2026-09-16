package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

var ErrDiagnosticADTS = errors.New("diagnostic_adts_invalid")

// DiagnosticADTS records physical packet boundaries and header declarations.
// It neither validates AAC payloads nor proves decoder selection, decoded sample
// counts, priming, padding, or content. Those require actual decode observations.
type DiagnosticADTS struct {
	PacketCount int
	Bytes       int
	Codec       string
	Profile     string
	SampleRate  int
	Channels    int
	SHA256      string
}

// ValidateDiagnosticADTS accepts only the fixed MPEG-4 AAC-LC, 48 kHz, stereo
// ADTS profile emitted by the diagnostic's default FFmpeg ADTS muxer. CRC,
// program-config channel layouts, metadata tags, multiple raw data blocks,
// non-default flags, and other buffer-fullness modes are outside this profile.
// Every packet must be complete; no resynchronization or suffix is accepted.
//
// Header layout and fixed muxer fields were checked against FFmpeg's sources:
// https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/adts_header.c
// https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/adtsenc.c
// https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/mpeg4audio_sample_rates.h
func ValidateDiagnosticADTS(data []byte) (DiagnosticADTS, error) {
	if len(data) == 0 || len(data) >= DiagnosticCompressedBytesLimit {
		return DiagnosticADTS{}, ErrDiagnosticADTS
	}
	const headerBytes = 7
	packets := 0
	for offset := 0; offset < len(data); {
		if len(data)-offset < headerBytes {
			return DiagnosticADTS{}, ErrDiagnosticADTS
		}
		header := data[offset : offset+headerBytes]
		// Twelve sync bits, MPEG-4 ID, layer zero, and protection_absent=1.
		if header[0] != 0xff || header[1] != 0xf1 {
			return DiagnosticADTS{}, ErrDiagnosticADTS
		}
		objectType := int(header[2]>>6) + 1
		samplingIndex := int((header[2] >> 2) & 0x0f)
		channelConfiguration := int(header[2]&1)<<2 | int(header[3]>>6)
		if objectType != 2 || samplingIndex != 3 || channelConfiguration != 2 {
			return DiagnosticADTS{}, ErrDiagnosticADTS
		}
		// Default private/original/home/copyright flags, VBR fullness 0x7ff,
		// and exactly one raw_data_block per physical ADTS frame.
		if header[2]&0x02 != 0 || header[3]&0x3c != 0 ||
			header[5]&0x1f != 0x1f || header[6] != 0xfc {
			return DiagnosticADTS{}, ErrDiagnosticADTS
		}
		frameBytes := int(header[3]&0x03)<<11 | int(header[4])<<3 | int(header[5]>>5)
		if frameBytes <= headerBytes || frameBytes > len(data)-offset {
			return DiagnosticADTS{}, ErrDiagnosticADTS
		}
		packets++
		if packets >= DiagnosticAudioPacketsLimit {
			return DiagnosticADTS{}, ErrDiagnosticADTS
		}
		offset += frameBytes
	}
	checksum := sha256.Sum256(data)
	return DiagnosticADTS{
		PacketCount: packets, Bytes: len(data), Codec: "aac", Profile: "LC",
		SampleRate: DiagnosticSampleRate, Channels: DiagnosticAudioChannels,
		SHA256: hex.EncodeToString(checksum[:]),
	}, nil
}
