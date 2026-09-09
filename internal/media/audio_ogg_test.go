package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestReadOggAudioEvidenceCodecs(t *testing.T) {
	for _, codec := range []string{"opus", "vorbis", "flac"} {
		t.Run(codec, func(t *testing.T) {
			pages, stream := oggAudioTestFixture(codec, 19, 7)
			evidence, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(pages, nil)), []Stream{stream})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(evidence.PacketCounts, map[int]int64{7: 2}) {
				t.Fatalf("packet counts = %v", evidence.PacketCounts)
			}
			switch codec {
			case "opus":
				if !reflect.DeepEqual(evidence.OpusSamples[7], []uint16{960, 1920}) || evidence.OpusPreSkip[7] != 312 {
					t.Fatalf("Opus evidence = %+v", evidence)
				}
			case "vorbis":
				if evidence.VorbisMaxPacketSamples[7] != 1024 {
					t.Fatalf("Vorbis maximum = %d", evidence.VorbisMaxPacketSamples[7])
				}
			}
		})
	}
}

func TestReadOggAudioEvidenceMapsPhysicalStreamsAndIgnoresArtwork(t *testing.T) {
	var pages [][]byte
	var streams []Stream
	var fixtures [][][]byte
	for index := range maxOggAudioStreams {
		fixture, stream := oggAudioTestFixture("opus", uint32(index+1), index*2)
		fixtures = append(fixtures, fixture)
		streams = append(streams, stream)
		pages = append(pages, fixture[0])
	}
	for _, fixture := range fixtures {
		pages = append(pages, fixture[1:]...)
	}
	streams = append(streams, Stream{Index: 1, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true})
	// Physical AVStream order is its index, even if the supplied slice is shuffled.
	streams[0], streams[3] = streams[3], streams[0]
	evidence, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(pages, nil)), streams)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.PacketCounts) != maxOggAudioStreams {
		t.Fatalf("stream counts = %v", evidence.PacketCounts)
	}
	for index := range maxOggAudioStreams {
		if evidence.PacketCounts[index*2] != 2 || evidence.OpusPreSkip[index*2] != 312 {
			t.Fatalf("missing evidence for stream %d", index*2)
		}
	}
}

func TestReadOggAudioEvidenceContinuedPacketAndBoundedPrefix(t *testing.T) {
	fixture, stream := oggAudioTestFixture("opus", 5, 0)
	packet := make([]byte, 300)
	packet[0] = 0xf8
	pages := [][]byte{
		fixture[0], fixture[1],
		oggAudioTestPage(5, 2, 0, []byte{255}, packet[:255]),
		oggAudioTestPage(5, 3, 5, []byte{45}, packet[255:]),
	}
	evidence, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(pages, nil)), []Stream{stream})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.PacketCounts[0] != 1 || !reflect.DeepEqual(evidence.OpusSamples[0], []uint16{960}) {
		t.Fatalf("continued packet evidence = %+v", evidence)
	}
}

func TestReadOggAudioEvidenceRejectsBrokenTopology(t *testing.T) {
	fixture, stream := oggAudioTestFixture("opus", 5, 0)
	other, _ := oggAudioTestFixture("opus", 6, 0)
	full := bytes.Join(fixture, nil)
	corrupt := append([]byte(nil), full...)
	corrupt[len(corrupt)-1] ^= 1
	badVersion := append([]byte(nil), fixture[0]...)
	badVersion[4] = 1
	oggAudioTestSetCRC(badVersion)
	tests := []struct {
		name string
		data []byte
	}{
		{"crc", corrupt},
		{"version", append(badVersion, bytes.Join(fixture[1:], nil)...)},
		{"missing_eos", bytes.Join(fixture[:2], nil)},
		{"partial_header", append(append([]byte(nil), full...), 'O', 'g')},
		{"partial_payload", full[:len(full)-1]},
		{"chain", append(append([]byte(nil), full...), bytes.Join(other, nil)...)},
		{"same_serial_chain", append(append([]byte(nil), full...), full...)},
		{"duplicate_bos", bytes.Join([][]byte{fixture[0], oggAudioTestPackets(5, 1, 2, oggAudioTestOpusHead(1)), fixture[2]}, nil)},
		{"missing_bos", bytes.Join([][]byte{oggAudioTestPackets(5, 0, 0, oggAudioTestOpusHead(1)), fixture[1], fixture[2]}, nil)},
		{"sequence_gap", bytes.Join([][]byte{fixture[0], fixture[1], oggAudioTestPackets(5, 3, 4, []byte{0xf8})}, nil)},
		{"unexpected_continuation", bytes.Join([][]byte{fixture[0], fixture[1], oggAudioTestPackets(5, 2, 5, []byte{0xf8})}, nil)},
		{"reserved_flag", bytes.Join([][]byte{fixture[0], fixture[1], oggAudioTestPackets(5, 2, 12, []byte{0xf8})}, nil)},
		{"empty_packet", bytes.Join([][]byte{fixture[0], fixture[1], oggAudioTestPackets(5, 2, 4, nil)}, nil)},
		{"missing_continuation", bytes.Join([][]byte{fixture[0], fixture[1], oggAudioTestPage(5, 2, 0, []byte{255}, make([]byte, 255)), oggAudioTestPackets(5, 3, 4, []byte{0xf8})}, nil)},
		{"unfinished_packet_at_eos", bytes.Join([][]byte{fixture[0], fixture[1], oggAudioTestPage(5, 2, 4, []byte{255}, make([]byte, 255))}, nil)},
		{"bos_after_headers", bytes.Join([][]byte{fixture[0], fixture[1], other[0], fixture[2]}, nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(test.data), []Stream{stream})
			oggAudioTestUnproven(t, err)
		})
	}
}

func TestReadOggAudioEvidenceRejectsHeaderUpdatesAndMismatches(t *testing.T) {
	for _, codec := range []string{"opus", "vorbis", "flac"} {
		t.Run(codec, func(t *testing.T) {
			fixture, stream := oggAudioTestFixture(codec, 5, 0)
			for _, changed := range []Stream{
				{Index: 0, CodecType: "audio", Codec: codec, Channels: 2, SampleRate: 48000},
				{Index: 0, CodecType: "audio", Codec: codec, Channels: 1, SampleRate: 44100},
			} {
				_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(fixture, nil)), []Stream{changed})
				oggAudioTestUnproven(t, err)
			}
			var update []byte
			switch codec {
			case "opus":
				update = oggAudioTestOpusHead(1)
			case "vorbis":
				update = oggAudioTestVorbisHead()
			case "flac":
				update = oggAudioTestFLACHead(1)
			}
			fixture[2] = oggAudioTestPackets(5, 2, 4, update)
			_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(fixture, nil)), []Stream{stream})
			oggAudioTestUnproven(t, err)
		})
	}
}

func TestReadOggAudioEvidenceFLACRequiresCommentAndConsistentHeaderCount(t *testing.T) {
	fixture, stream := oggAudioTestFixture("flac", 5, 0)
	tests := []struct {
		name    string
		head    []byte
		comment []byte
	}{
		{"missing_comment", oggAudioTestFLACHead(1), []byte{0xff, 0xf8, 1}},
		{"wrong_first_metadata", oggAudioTestFLACHead(1), []byte{0x81, 0, 0, 0}},
		{"count_too_high", oggAudioTestFLACHead(2), []byte{0x84, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"missing_last_flag", oggAudioTestFLACHead(1), []byte{4, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"metadata_length", oggAudioTestFLACHead(1), []byte{0x84, 0, 0, 9, 0, 0, 0, 0, 0, 0, 0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pages := [][]byte{oggAudioTestPackets(5, 0, 2, test.head), oggAudioTestPackets(5, 1, 0, test.comment), fixture[2]}
			_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(pages, nil)), []Stream{stream})
			oggAudioTestUnproven(t, err)
		})
	}
	fixture[0] = oggAudioTestPackets(5, 0, 2, oggAudioTestFLACHead(0))
	if _, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(fixture, nil)), []Stream{stream}); err != nil {
		t.Fatalf("unknown header count with a valid last flag: %v", err)
	}
}

func TestReadOggAudioEvidenceOpusLayoutAndVorbisBlockSizes(t *testing.T) {
	opus, stream := oggAudioTestFixture("opus", 5, 0)
	head := append(oggAudioTestOpusHead(6), 4, 2, 0, 4, 1, 2, 3, 5)
	head[18] = 1
	stream.Channels = 6
	opus[0] = oggAudioTestPackets(5, 0, 2, head)
	if _, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(opus, nil)), []Stream{stream}); err != nil {
		t.Fatalf("standard Opus layout: %v", err)
	}
	head[23] = 7
	opus[0] = oggAudioTestPackets(5, 0, 2, head)
	_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(opus, nil)), []Stream{stream})
	oggAudioTestUnproven(t, err)
	vorbis, stream := oggAudioTestFixture("vorbis", 5, 0)
	for _, sizes := range []byte{0xb5, 0xe6, 0x6b} {
		head = oggAudioTestVorbisHead()
		head[28] = sizes
		vorbis[0] = oggAudioTestPackets(5, 0, 2, head)
		_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(vorbis, nil)), []Stream{stream})
		oggAudioTestUnproven(t, err)
	}
}

func TestOggOpusPacketSamples(t *testing.T) {
	for _, test := range []struct {
		packet []byte
		want   uint16
	}{
		{[]byte{0x80}, 120}, {[]byte{0x88}, 240}, {[]byte{0x90}, 480}, {[]byte{0x98}, 960},
		{[]byte{0x00}, 480}, {[]byte{0x08}, 960}, {[]byte{0x10}, 1920}, {[]byte{0x18}, 2880},
		{[]byte{0x60}, 480}, {[]byte{0x68}, 960}, {[]byte{0x19}, 5760}, {[]byte{0x83, 48}, 5760},
	} {
		got, err := oggOpusPacketSamples(test.packet, int64(len(test.packet)))
		if err != nil || got != test.want {
			t.Fatalf("TOC %x = %d, %v; want %d", test.packet, got, err, test.want)
		}
	}
	for _, packet := range [][]byte{nil, {0x83}, {0x83, 0}, {0x83, 49}, {0xfb, 7}} {
		_, err := oggOpusPacketSamples(packet, int64(len(packet)))
		oggAudioTestUnproven(t, err)
	}
}

func TestOggFLACFrameHeaderRejectsParameterChanges(t *testing.T) {
	state := oggAudioStream{stream: Stream{SampleRate: 48000, Channels: 1}, flacBits: 16}
	for _, packet := range [][]byte{
		{0xff, 0xf8, 0x8a, 0, 0, 0},
		{0xff, 0xf8, 0x8c, 0, 0, 48, 0},
		{0xff, 0xf8, 0x8d, 0, 0, 0xbb, 0x80, 0},
		{0xff, 0xf8, 0x8e, 0, 0, 0x12, 0xc0, 0},
		{0xff, 0xf8, 0x6c, 0, 0xc2, 0x80, 15, 48, 0},
	} {
		if err := state.flacFrameHeader(packet); err != nil {
			t.Fatalf("consistent header %x: %v", packet, err)
		}
	}
	for _, packet := range [][]byte{
		{0xff, 0xf8, 0x89, 0, 0, 0},
		{0xff, 0xf8, 0x8a, 0x10, 0, 0},
		{0xff, 0xf8, 0x8a, 0x0c, 0, 0},
		{0xff, 0xf8, 0x8c, 0, 0, 44, 0},
		{0xff, 0xf8, 0x8d, 0, 0, 0xbb},
		{0xff, 0xf8, 0x6c, 0, 0xc2, 0, 15, 48, 0},
		{0xff, 0xf8, 0x8a, 0, 0xff, 0},
	} {
		oggAudioTestUnproven(t, state.flacFrameHeader(packet))
	}
}

func TestReadOggAudioEvidenceStreamDescriptions(t *testing.T) {
	fixture, stream := oggAudioTestFixture("opus", 5, 0)
	tooMany := make([]Stream, maxOggAudioStreams+1)
	for index := range tooMany {
		tooMany[index] = stream
		tooMany[index].Index = index
	}
	for _, streams := range [][]Stream{
		nil,
		{stream, stream},
		{stream, {Index: 1, CodecType: "video", Codec: "vp9"}},
		{{Index: 0, CodecType: "audio", Codec: "mp3", Channels: 1, SampleRate: 48000}},
		tooMany,
	} {
		_, err := readOggAudioEvidence(context.Background(), bytes.NewReader(bytes.Join(fixture, nil)), streams)
		oggAudioTestUnproven(t, err)
	}
}

func TestReadOggAudioEvidenceCancellationIOAndLimit(t *testing.T) {
	_, stream := oggAudioTestFixture("opus", 5, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readOggAudioEvidence(ctx, bytes.NewReader(nil), []Stream{stream}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	sentinel := errors.New("source read failed")
	if _, err := readOggAudioEvidence(context.Background(), oggAudioTestErrorReader{sentinel}, []Stream{stream}); !errors.Is(err, sentinel) {
		t.Fatalf("read error = %v", err)
	}
	input := oggAudioInput{
		ctx: context.Background(), input: io.LimitedReader{R: bytes.NewReader([]byte{0, 0}), N: 2}, read: maxOggAudioBytes - 1,
	}
	_, err := input.readFull(make([]byte, 2))
	oggAudioTestUnproven(t, err)
}

// These fixtures contain structural codec prefixes, not decodable audio. The
// exhaustive FFprobe integration tests separately validate compressed payloads.
func oggAudioTestFixture(codec string, serial uint32, index int) ([][]byte, Stream) {
	stream := Stream{Index: index, CodecType: "audio", Codec: codec, Channels: 1, SampleRate: 48000}
	var head []byte
	var headers, packets [][]byte
	switch codec {
	case "opus":
		head = oggAudioTestOpusHead(1)
		tags := make([]byte, 16)
		copy(tags, "OpusTags")
		headers = [][]byte{tags}
		packets = [][]byte{{0xf8}, {0xf9}}
	case "vorbis":
		head = oggAudioTestVorbisHead()
		comment := make([]byte, 16)
		copy(comment, "\x03vorbis")
		comment[15] = 1
		headers = [][]byte{comment, []byte("\x05vorbis\x01")}
		packets = [][]byte{{0, 1}, {0, 2}}
	case "flac":
		head = oggAudioTestFLACHead(1)
		headers = [][]byte{{0x84, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0}}
		packets = [][]byte{{0xff, 0xf8, 0x8a, 0, 0, 0}, {0xff, 0xf8, 0x8a, 0, 1, 0}}
	}
	return [][]byte{oggAudioTestPackets(serial, 0, 2, head), oggAudioTestPackets(serial, 1, 0, headers...), oggAudioTestPackets(serial, 2, 4, packets...)}, stream
}

func oggAudioTestOpusHead(channels byte) []byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8], head[9] = 1, channels
	binary.LittleEndian.PutUint16(head[10:12], 312)
	binary.LittleEndian.PutUint32(head[12:16], 48000)
	return head
}

func oggAudioTestVorbisHead() []byte {
	head := make([]byte, 30)
	copy(head, "\x01vorbis")
	head[11], head[28], head[29] = 1, 0xb8, 1
	binary.LittleEndian.PutUint32(head[12:16], 48000)
	return head
}

func oggAudioTestFLACHead(headers uint16) []byte {
	head := make([]byte, 51)
	copy(head, "\x7fFLAC\x01\x00")
	binary.BigEndian.PutUint16(head[7:9], headers)
	copy(head[9:13], "fLaC")
	head[16] = 34
	binary.BigEndian.PutUint16(head[17:19], 16)
	binary.BigEndian.PutUint16(head[19:21], 4096)
	binary.BigEndian.PutUint64(head[27:35], uint64(48000)<<44|uint64(15)<<36)
	return head
}

func oggAudioTestPackets(serial, sequence uint32, flags byte, packets ...[]byte) []byte {
	var laces, body []byte
	for _, packet := range packets {
		length := len(packet)
		for length >= 255 {
			laces = append(laces, 255)
			length -= 255
		}
		laces = append(laces, byte(length))
		body = append(body, packet...)
	}
	return oggAudioTestPage(serial, sequence, flags, laces, body)
}

func oggAudioTestPage(serial, sequence uint32, flags byte, laces, body []byte) []byte {
	page := make([]byte, 27+len(laces)+len(body))
	copy(page, "OggS")
	page[5], page[26] = flags, byte(len(laces))
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], sequence)
	copy(page[27:], laces)
	copy(page[27+len(laces):], body)
	oggAudioTestSetCRC(page)
	return page
}

func oggAudioTestSetCRC(page []byte) {
	clear(page[22:26])
	var value uint32
	for _, part := range page {
		value ^= uint32(part) << 24
		for range 8 {
			if value&0x80000000 != 0 {
				value = value<<1 ^ 0x04c11db7
			} else {
				value <<= 1
			}
		}
	}
	binary.LittleEndian.PutUint32(page[22:26], value)
}

func oggAudioTestUnproven(t *testing.T, err error) {
	t.Helper()
	var unproven *audioTimingUnproven
	if !errors.As(err, &unproven) || unproven.Reason == "" {
		t.Fatalf("expected an unproven scan, got %v", err)
	}
}

type oggAudioTestErrorReader struct{ err error }

func (reader oggAudioTestErrorReader) Read([]byte) (int, error) { return 0, reader.err }
