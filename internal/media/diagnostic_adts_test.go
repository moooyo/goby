package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// These synthetic packets exercise framing only. Their opaque payloads are
// deliberately not a codec fixture or evidence of successful AAC decoding.
func diagnosticADTSTestPacket(t *testing.T, payload []byte) []byte {
	t.Helper()
	length := 7 + len(payload)
	if len(payload) == 0 || length > 0x1fff {
		t.Fatal("invalid synthetic ADTS packet size")
	}
	packet := append([]byte{0xff, 0xf1, 0x4c, 0x80, 0, 0x1f, 0xfc}, payload...)
	packet[3] |= byte(length >> 11)
	packet[4] = byte(length >> 3)
	packet[5] |= byte(length&7) << 5
	return packet
}

func diagnosticADTSRequireRejected(t *testing.T, data []byte) {
	t.Helper()
	result, err := ValidateDiagnosticADTS(data)
	if !errors.Is(err, ErrDiagnosticADTS) || result != (DiagnosticADTS{}) {
		t.Fatalf("invalid ADTS returned accepted or partial facts: %+v, %v", result, err)
	}
}

func TestDiagnosticADTSReportsCompleteHeaderAndPacketFacts(t *testing.T) {
	// Independent eight- and nine-byte header witnesses exercise the low
	// frame-length bits without deriving the expected headers from the parser.
	first := []byte{0xff, 0xf1, 0x4c, 0x80, 0x01, 0x1f, 0xfc, 0xe0}
	second := []byte{0xff, 0xf1, 0x4c, 0x80, 0x01, 0x3f, 0xfc, 0x11, 0x22}
	data := append(append([]byte(nil), first...), second...)
	before := append([]byte(nil), data...)
	checksum := sha256.Sum256(data)
	result, err := ValidateDiagnosticADTS(data)
	want := DiagnosticADTS{PacketCount: 2, Bytes: 17, Codec: "aac", Profile: "LC",
		SampleRate: 48000, Channels: 2, SHA256: hex.EncodeToString(checksum[:])}
	if err != nil || result != want || !bytes.Equal(data, before) {
		t.Fatalf("complete packets lost their exact facts or changed input: %+v, %v", result, err)
	}
}

func TestDiagnosticADTSCountsPhysicalPacketsWithoutScanningPayload(t *testing.T) {
	inner := diagnosticADTSTestPacket(t, []byte{0, 0, 0})
	outer := diagnosticADTSTestPacket(t, append([]byte{0xff, 0xf1}, inner...))
	result, err := ValidateDiagnosticADTS(outer)
	if err != nil || result.PacketCount != 1 || result.Bytes != len(outer) {
		t.Fatal("payload sync patterns were counted as physical ADTS packets")
	}
	changed := append([]byte(nil), outer...)
	changed[len(changed)-1] ^= 0xff
	other, err := ValidateDiagnosticADTS(changed)
	if err != nil || other.PacketCount != 1 || other.SHA256 == result.SHA256 {
		t.Fatal("opaque payload bytes were omitted from the physical stream identity")
	}
}

func TestDiagnosticADTSRejectsTruncatedAndMismatchedFrames(t *testing.T) {
	packet := diagnosticADTSTestPacket(t, []byte{0x11, 0x22, 0x33, 0x44})
	for cut := 1; cut < len(packet); cut++ {
		diagnosticADTSRequireRejected(t, packet[:cut])
		partialSecond := append(append([]byte(nil), packet...), packet[:cut]...)
		diagnosticADTSRequireRejected(t, partialSecond)
	}
	for _, length := range []int{0, 1, 6, 7, len(packet) - 1, len(packet) + 1, 0x1fff} {
		changed := append([]byte(nil), packet...)
		changed[3] = changed[3]&0xfc | byte(length>>11)
		changed[4] = byte(length >> 3)
		changed[5] = changed[5]&0x1f | byte(length&7)<<5
		diagnosticADTSRequireRejected(t, changed)
	}
	for _, outside := range [][]byte{{0}, {'I', 'D', '3'}, {'A', 'P', 'E', 'T', 'A', 'G', 'E', 'X'}} {
		diagnosticADTSRequireRejected(t, append(append([]byte(nil), packet...), outside...))
		diagnosticADTSRequireRejected(t, append(append([]byte(nil), outside...), packet...))
	}
}

func TestDiagnosticADTSRejectsUnsupportedHeadersAndParameterDrift(t *testing.T) {
	packet := diagnosticADTSTestPacket(t, []byte{0x11, 0x22})
	cases := []struct {
		name   string
		change func([]byte)
	}{
		{"sync_first", func(p []byte) { p[0] ^= 1 }},
		{"sync_second", func(p []byte) { p[1] ^= 0x10 }},
		{"mpeg2", func(p []byte) { p[1] |= 8 }},
		{"layer", func(p []byte) { p[1] |= 2 }},
		{"crc", func(p []byte) { p[1] &^= 1 }},
		{"main_profile", func(p []byte) { p[2] &^= 0xc0 }},
		{"ssr_profile", func(p []byte) { p[2] = p[2]&0x3f | 0x80 }},
		{"ltp_profile", func(p []byte) { p[2] |= 0xc0 }},
		{"private", func(p []byte) { p[2] |= 2 }},
		{"program_config", func(p []byte) { p[3] &^= 0xc0 }},
		{"mono", func(p []byte) { p[3] = p[3]&0x3f | 0x40 }},
		{"channel_high_bit", func(p []byte) { p[2] |= 1 }},
		{"original", func(p []byte) { p[3] |= 0x20 }},
		{"home", func(p []byte) { p[3] |= 0x10 }},
		{"copyright_bit", func(p []byte) { p[3] |= 8 }},
		{"copyright_start", func(p []byte) { p[3] |= 4 }},
		{"fullness_high", func(p []byte) { p[5] &^= 1 }},
		{"fullness_low", func(p []byte) { p[6] &^= 4 }},
		{"two_raw_blocks", func(p []byte) { p[6] |= 1 }},
		{"three_raw_blocks", func(p []byte) { p[6] |= 2 }},
		{"four_raw_blocks", func(p []byte) { p[6] |= 3 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := append([]byte(nil), packet...)
			test.change(changed)
			diagnosticADTSRequireRejected(t, changed)
			diagnosticADTSRequireRejected(t, append(append([]byte(nil), packet...), changed...))
		})
	}
	for index := byte(0); index < 16; index++ {
		if index == 3 {
			continue
		}
		changed := append([]byte(nil), packet...)
		changed[2] = changed[2]&0xc3 | index<<2
		diagnosticADTSRequireRejected(t, changed)
		diagnosticADTSRequireRejected(t, append(append([]byte(nil), packet...), changed...))
	}
}

func TestDiagnosticADTSRequiresStrictPacketAndByteBudgets(t *testing.T) {
	packet := diagnosticADTSTestPacket(t, []byte{0})
	for _, count := range []int{1, DiagnosticAudioPacketsLimit - 1} {
		data := bytes.Repeat(packet, count)
		result, err := ValidateDiagnosticADTS(data)
		if err != nil || result.PacketCount != count || result.Bytes != len(data) {
			t.Fatal("a complete stream below the physical packet cap was rejected")
		}
	}
	for _, count := range []int{DiagnosticAudioPacketsLimit, DiagnosticAudioPacketsLimit + 1} {
		diagnosticADTSRequireRejected(t, bytes.Repeat(packet, count))
	}
	diagnosticADTSRequireRejected(t, nil)
	diagnosticADTSRequireRejected(t, []byte{})
	for _, size := range []int{DiagnosticCompressedBytesLimit, DiagnosticCompressedBytesLimit + 1} {
		diagnosticADTSRequireRejected(t, make([]byte, size))
	}
}

func TestDiagnosticADTSReadsAllThirteenFrameLengthBits(t *testing.T) {
	for _, length := range []int{8, 255, 256, 2047, 2048, 4096, 8191} {
		packet := diagnosticADTSTestPacket(t, bytes.Repeat([]byte{0xa5}, length-7))
		result, err := ValidateDiagnosticADTS(packet)
		if err != nil || result.PacketCount != 1 || result.Bytes != length {
			t.Fatalf("valid physical frame length %d was lost: %+v, %v", length, result, err)
		}
	}
}
