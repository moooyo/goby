package media

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func videoCopySeekAV1TestOBU(kind byte, payload []byte) []byte {
	data := []byte{kind<<3 | 2}
	size := len(payload)
	for size >= 128 {
		data = append(data, byte(size&127)|128)
		size >>= 7
	}
	data = append(data, byte(size))
	return append(data, payload...)
}

func videoCopySeekAV1TestSequence(t *testing.T) []byte {
	t.Helper()
	// This complete sequence header is from FFmpeg's FATE film_grain.ivf.
	data, err := hex.DecodeString("000000043cffbc6af940c0")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func videoCopySeekAV1TestPacket(sequence, frame []byte) []byte {
	data := videoCopySeekAV1TestOBU(1, sequence)
	return append(data, videoCopySeekAV1TestOBU(6, frame)...)
}

func TestVideoCopySeekAV1RestartPacketRequiresInBandDisplayedKeyFrame(t *testing.T) {
	sequence := videoCopySeekAV1TestSequence(t)
	// Frame prefixes isolate classification; decoder proof must validate tiles.
	for name, prefix := range map[string]byte{
		"show existing frame": 0x80,
		"hidden key frame":    0x00,
		"inter frame":         0x30,
		"intra only frame":    0x50,
		"switch frame":        0x70,
	} {
		t.Run(name, func(t *testing.T) {
			if videoCopySeekAV1RestartPacket(videoCopySeekAV1TestPacket(sequence, []byte{prefix, 0x80})) {
				t.Fatal("a frame requiring an unproven restart was accepted")
			}
		})
	}
	frame := videoCopySeekAV1TestOBU(6, []byte{0x10, 0x80})
	if videoCopySeekAV1RestartPacket(frame) {
		t.Fatal("a packet without an in-band sequence header was accepted")
	}
	valid := videoCopySeekAV1TestPacket(sequence, []byte{0x10, 0x80})
	if !videoCopySeekAV1RestartPacket(valid) || !videoCopySeekAV1RestartPacket(append([]byte{0x12, 0}, valid...)) {
		t.Fatal("displayed key-frame restart syntax was rejected")
	}
	separate := append(videoCopySeekAV1TestOBU(1, sequence), videoCopySeekAV1TestOBU(3, []byte{0x10, 0x80})...)
	if videoCopySeekAV1RestartPacket(separate) {
		t.Fatal("a frame header without any tile payload was accepted")
	}
	if !videoCopySeekAV1RestartPacket(append(separate, videoCopySeekAV1TestOBU(4, []byte{0})...)) {
		t.Fatal("separate displayed key-frame and tile-group syntax was rejected")
	}
}

func TestVideoCopySeekAV1RestartPacketAcceptsFinalImplicitSizeSampleOBU(t *testing.T) {
	sequence := videoCopySeekAV1TestOBU(1, videoCopySeekAV1TestSequence(t))
	header := videoCopySeekAV1TestOBU(3, []byte{0x10, 0x80})
	for name, sample := range map[string][]byte{
		"frame": append(bytes.Clone(sequence), 0x30, 0x10, 0x80),
		"reduced frame": append(videoCopySeekAV1TestOBU(1,
			videoCopySeekAV1TestReducedSequence("000", "0100")), 0x30, 0, 0),
		"tile group": append(append(bytes.Clone(sequence), header...), 0x20, 0),
		"last of several tile groups": append(append(append(bytes.Clone(sequence), header...),
			videoCopySeekAV1TestOBU(4, []byte{0})...), 0x20, 0),
	} {
		t.Run(name, func(t *testing.T) {
			if !videoCopySeekAV1RestartPacket(sample) {
				t.Fatal("a final sample frame or tile group with implicit size was rejected")
			}
		})
	}
	for name, suffix := range map[string][]byte{
		"empty frame":            {0x30},
		"frame prefix only":      {0x30, 0x10},
		"show existing frame":    {0x30, 0x80, 0},
		"hidden key frame":       {0x30, 0, 0},
		"inter frame":            {0x30, 0x30, 0},
		"forbidden frame bit":    {0xb0, 0x10, 0x80},
		"reserved frame bit":     {0x31, 0x10, 0x80},
		"frame extension":        {0x34, 0, 0x10, 0x80},
		"frame header":           {0x18, 0x10, 0x80},
		"tile before header":     {0x20, 0},
		"empty final tile group": append(bytes.Clone(header), 0x20),
		"metadata":               {0x28, 1, 0, 0, 0, 0, 0x80},
		"padding":                {0x78, 0x80},
		"padding after frame":    {0x32, 2, 0x10, 0x80, 0x78, 0x80},
		"multiple frames":        {0x32, 2, 0x10, 0x80, 0x30, 0x10, 0x80},
		"tile after frame":       {0x32, 2, 0x10, 0x80, 0x20, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if videoCopySeekAV1RestartPacket(append(bytes.Clone(sequence), suffix...)) {
				t.Fatal("incomplete or unsupported implicit-size restart syntax was accepted")
			}
		})
	}
	if videoCopySeekAV1RestartPacket([]byte{0x30, 0x10, 0x80}) {
		t.Fatal("an implicit-size frame without an in-band sequence header was accepted")
	}
}

func TestVideoCopySeekAV1RestartPacketRejectsAmbiguousAndTruncatedOBUs(t *testing.T) {
	sequence := videoCopySeekAV1TestSequence(t)
	valid := videoCopySeekAV1TestPacket(sequence, []byte{0x10, 0x80})
	for name, data := range map[string][]byte{
		"empty":                   nil,
		"forbidden bit":           append([]byte{valid[0] | 0x80}, valid[1:]...),
		"reserved bit":            append([]byte{valid[0] | 1}, valid[1:]...),
		"extension":               append([]byte{valid[0] | 4, 0}, valid[1:]...),
		"implicit sequence size":  append([]byte{valid[0] &^ 2}, valid[1:]...),
		"truncated size":          {0x0a, 0x80},
		"unterminated size":       append([]byte{0x0a}, bytes.Repeat([]byte{0x80}, 8)...),
		"size above uint32":       {0x0a, 0x80, 0x80, 0x80, 0x80, 0x10},
		"truncated payload":       valid[:len(valid)-1],
		"truncated trailing OBU":  append(bytes.Clone(valid), 0x7a),
		"multiple frames":         append(bytes.Clone(valid), videoCopySeekAV1TestOBU(6, []byte{0x10, 0x80})...),
		"late sequence":           append(bytes.Clone(valid), videoCopySeekAV1TestOBU(1, sequence)...),
		"late temporal delimiter": append(bytes.Clone(valid), 0x12, 0),
		"nonempty delimiter":      append([]byte{0x12, 1, 0}, valid...),
		"reserved OBU":            append(bytes.Clone(valid), 0x4a, 0),
		"redundant frame header":  append(bytes.Clone(valid), 0x3a, 1, 0x10),
		"tile list":               append(bytes.Clone(valid), 0x42, 0),
		"unexpected tile group":   append(bytes.Clone(valid), 0x22, 1, 0),
		"scalability metadata":    append([]byte{0x2a, 2, 3, 0x80}, valid...),
		"empty frame":             videoCopySeekAV1TestPacket(sequence, nil),
		"prefix without tile":     videoCopySeekAV1TestPacket(sequence, []byte{0x10}),
		"oversized packet":        make([]byte, maxVideoCopySeekPacketBytes+1),
		"too many OBUs":           append(bytes.Repeat([]byte{0x7a, 0}, maxVideoCopySeekAV1OBUs), valid...),
	} {
		t.Run(name, func(t *testing.T) {
			if videoCopySeekAV1RestartPacket(data) {
				t.Fatal("ambiguous or truncated AV1 restart evidence was accepted")
			}
		})
	}
	for length := 0; length < len(sequence); length++ {
		if videoCopySeekAV1RestartPacket(videoCopySeekAV1TestPacket(sequence[:length], []byte{0x10, 0x80})) {
			t.Fatalf("a sequence header truncated to %d bytes was accepted", length)
		}
	}
}

func videoCopySeekAV1TestReducedSequence(profile, color string) []byte {
	// One-pixel reduced still picture, level zero, with coding tools disabled.
	bits := profile + "11" + "00000" + "00000000" + "00" + "000" + "000" + color + "01"
	data := make([]byte, (len(bits)+7)/8)
	for i, value := range bits {
		if value == '1' {
			data[i/8] |= 1 << (7 - i%8)
		}
	}
	return data
}

func TestVideoCopySeekAV1SequenceHeaderConsumesReducedAndColorSyntax(t *testing.T) {
	for name, sequence := range map[string][]byte{
		"monochrome":            videoCopySeekAV1TestReducedSequence("000", "0100"),
		"profile zero 420":      videoCopySeekAV1TestReducedSequence("000", "0000000"),
		"profile one 444":       videoCopySeekAV1TestReducedSequence("001", "0000"),
		"profile two 422":       videoCopySeekAV1TestReducedSequence("010", "00000"),
		"profile two 12bit 420": videoCopySeekAV1TestReducedSequence("010", "1100011000"),
		"RGB":                   videoCopySeekAV1TestReducedSequence("001", "010000000100001101000000000"),
	} {
		t.Run(name, func(t *testing.T) {
			if !videoCopySeekAV1RestartPacket(videoCopySeekAV1TestPacket(sequence, []byte{0, 0})) {
				t.Fatal("valid reduced sequence syntax was rejected")
			}
		})
	}
	minimal := videoCopySeekAV1TestReducedSequence("000", "0100")
	for name, sequence := range map[string][]byte{
		"reserved profile":         videoCopySeekAV1TestReducedSequence("011", "0100"),
		"reduced without still":    append([]byte{minimal[0] &^ 0x10}, minimal[1:]...),
		"reserved level":           append([]byte{0x1e, minimal[1] & 0x3f}, minimal[2:]...),
		"missing trailing one":     {0x18, 0, 0, 0x10},
		"nonzero trailing bit":     append(bytes.Clone(minimal), 1),
		"reserved chroma position": videoCopySeekAV1TestReducedSequence("000", "0000110"),
		"invalid RGB profile":      videoCopySeekAV1TestReducedSequence("000", "0010000000100001101000000000"),
	} {
		t.Run(name, func(t *testing.T) {
			if videoCopySeekAV1RestartPacket(videoCopySeekAV1TestPacket(sequence, []byte{0, 0})) {
				t.Fatal("invalid sequence syntax was accepted")
			}
		})
	}
	reader := videoCopySeekAV1Bits{data: make([]byte, 4)}
	reader.uvlc()
	if !reader.failed {
		t.Fatal("reserved picture interval was accepted")
	}
}

func videoCopySeekTestPacketDump(data []byte) string {
	var output strings.Builder
	output.WriteByte('\n')
	for offset := 0; offset < len(data); offset += 16 {
		row := data[offset:min(offset+16, len(data))]
		fmt.Fprintf(&output, "%08x: ", offset)
		for i, value := range row {
			fmt.Fprintf(&output, "%02x", value)
			if i&1 != 0 {
				output.WriteByte(' ')
			}
		}
		output.WriteString(strings.Repeat(" ", 41-2*len(row)-len(row)/2))
		for _, value := range row {
			if value < 32 || value > 126 {
				value = '.'
			}
			output.WriteByte(value)
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func TestVideoCopySeekPacketDataDecodesOnlyHexColumn(t *testing.T) {
	const fullRow = "\n00000000: 3031 3233 3435 3637 3839 6162 6364 6566  0123456789abcdef\n"
	decoded, err := videoCopySeekPacketData(fullRow)
	if err != nil || string(decoded) != "0123456789abcdef" {
		t.Fatalf("unexpected fixed-width packet decoding: %x, %v", decoded, err)
	}
	for length := 1; length <= 33; length++ {
		data := make([]byte, length)
		for i := range data {
			data[i] = byte(i*19 + 32)
		}
		for _, dump := range []string{videoCopySeekTestPacketDump(data), strings.ReplaceAll(videoCopySeekTestPacketDump(data), "\n", "\r\n")} {
			decoded, err := videoCopySeekPacketData(dump)
			if err != nil || !bytes.Equal(decoded, data) {
				t.Fatalf("packet length %d did not round-trip: %v", length, err)
			}
		}
	}
}

func TestVideoCopySeekPacketDataRejectsMalformedRowsAndBudgetOverflow(t *testing.T) {
	valid := videoCopySeekTestPacketDump([]byte("0123456789abcdefABC"))
	partial := videoCopySeekTestPacketDump([]byte("abc"))
	for name, encoded := range map[string]string{
		"empty":              "",
		"empty dump":         "\n",
		"missing newline":    strings.TrimSuffix(valid, "\n"),
		"duplicate address":  strings.Replace(valid, "00000010:", "00000000:", 1),
		"address gap":        strings.Replace(valid, "00000010:", "00000020:", 1),
		"nonzero start":      strings.Replace(valid, "00000000:", "00000001:", 1),
		"invalid hex":        strings.Replace(valid, "3031", "30x1", 1),
		"missing hex byte":   strings.Replace(valid, "3031", "30  ", 1),
		"joined hex groups":  strings.Replace(valid, "3031 3233", "30313233 ", 1),
		"corrupt ASCII":      strings.Replace(valid, "0123456789abcdef", "fedcba9876543210", 1),
		"extra ASCII":        strings.Replace(valid, "ABC\n", "ABCD\n", 1),
		"extra blank line":   valid + "\n",
		"row after partial":  partial + strings.TrimPrefix(strings.Replace(partial, "00000000:", "00000003:", 1), "\n"),
		"oversized encoding": strings.Repeat(" ", 5*maxVideoCopySeekPacketBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := videoCopySeekPacketData(encoded); err == nil {
				t.Fatal("malformed packet hexdump was accepted")
			}
		})
	}
	data := make([]byte, maxVideoCopySeekPacketBytes+1)
	if _, err := videoCopySeekPacketData(videoCopySeekTestPacketDump(data[:maxVideoCopySeekPacketBytes])); err != nil {
		t.Fatalf("packet at byte limit was rejected: %v", err)
	}
	if _, err := videoCopySeekPacketData(videoCopySeekTestPacketDump(data)); err == nil {
		t.Fatal("packet above the byte limit was accepted")
	}
}

func videoCopySeekAV1TestFrameHash() string {
	var output strings.Builder
	output.WriteString("#format: frame checksums\n#version: 2\n#hash: SHA256\n")
	for number := range 4 {
		codec, denominator := "av1", TicksPerSecond
		if number == 1 {
			codec, denominator = "rawvideo", 1000
		}
		fmt.Fprintf(&output, "#tb %d: 1/%d\n#media_type %d: video\n#codec_id %d: %s\n#dimensions %d: 64x64\n",
			number, denominator, number, number, codec, number)
	}
	for _, record := range []string{
		"2, 0, 0, 1, 64", "3, 0, 0, 1, 10", "0, 0, 0, 1, 64", "1, 0, 0, 1, 6144",
		"2, 1, 1, 1, 64", "2, 20000000, 20000000, 1, 64",
		"3, 20000000, 20000000, 1, 10", "0, 20000000, 20000000, 1, 64", "1, 2000, 2000, 1, 6144",
	} {
		fmt.Fprintf(&output, "%s, %s\n", record, strings.Repeat("a", 64))
	}
	return output.String()
}

func TestVideoCopySeekAV1FrameHashRequiresAllPacketSideDataEvidence(t *testing.T) {
	base := videoSeekCandidateTestIndex()
	base.Codec = "av1"
	valid := videoCopySeekAV1TestFrameHash()
	index, err := parseVideoCopySeekAV1FrameHash(strings.NewReader(valid), base, MaxVideoSeekEntries)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 2 || index.Entries[1].PTS != 20_000_000 ||
		!index.PacketSideDataChecked || index.PacketRestartChecked || index.NALScopeChecked || index.ParameterSetsSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("unexpected preliminary AV1 evidence: %+v", index)
	}
	if err := ValidateVideoSeekIndex(index); err == nil {
		t.Fatal("key flags and frame hashes authorized a restart without packet syntax")
	}
	index.PacketRestartChecked = true
	for position := range index.Entries {
		index.Entries[position].PacketSHA256 = index.Entries[position].CodedSHA256
	}
	if err := ValidateVideoSeekIndex(index); err != nil {
		t.Fatalf("complete AV1 evidence failed its index contract: %v", err)
	}
	for name, output := range map[string]string{
		"non-key extradata": strings.Replace(valid, "2, 1, 1, 1, 64, "+strings.Repeat("a", 64),
			"2, 1, 1, 1, 64, "+strings.Repeat("a", 64)+", S=1, 12, "+strings.Repeat("b", 64), 1),
		"missing all-packet branch": strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(valid,
			"2, 0, 0, 1, 64, "+strings.Repeat("a", 64)+"\n", ""),
			"2, 1, 1, 1, 64, "+strings.Repeat("a", 64)+"\n", ""),
			"2, 20000000, 20000000, 1, 64, "+strings.Repeat("a", 64)+"\n", ""),
		"changed sequence header": strings.Replace(valid, "3, 20000000, 20000000, 1, 10, "+strings.Repeat("a", 64),
			"3, 20000000, 20000000, 1, 10, "+strings.Repeat("b", 64), 1),
		"missing sequence headers": strings.ReplaceAll(strings.ReplaceAll(valid,
			"3, 0, 0, 1, 10, "+strings.Repeat("a", 64)+"\n", ""),
			"3, 20000000, 20000000, 1, 10, "+strings.Repeat("a", 64)+"\n", ""),
		"changed decoded size": strings.Replace(valid, "1, 2000, 2000, 1, 6144", "1, 2000, 2000, 1, 6145", 1),
		"repeated packet DTS":  strings.Replace(valid, "2, 1, 1, 1, 64", "2, 0, 1, 1, 64", 1),
		"wrong codec":          strings.Replace(valid, "#codec_id 0: av1", "#codec_id 0: h264", 1),
		"truncated output":     strings.TrimSuffix(valid, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseVideoCopySeekAV1FrameHash(strings.NewReader(output), base, MaxVideoSeekEntries); err == nil {
				t.Fatal("incomplete AV1 framehash evidence was accepted")
			}
		})
	}
}
