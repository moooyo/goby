package transcode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestProgressiveAudioReadyRequiresFirstMediaPayload(t *testing.T) {
	for _, test := range []struct {
		container string
		data      []byte
	}{
		{"mp3", progressiveReadyMP3([4]byte{0xff, 0xfb, 0x90, 0}, 417)},
		{"aac", progressiveReadyADTS(false, 0, []byte{1, 2, 3, 4})},
		{"flac", progressiveReadyFLAC([]byte{0xff, 0xf8, 0xca, 0x18, 0}, []byte{0, 1, 2})},
		{"ogg", progressiveReadyOgg()},
		{"wav", progressiveReadyWAV(false, false, []byte{0, 0, 0, 0})},
		{"m4a", progressiveReadyM4A()},
	} {
		t.Run(test.container, func(t *testing.T) {
			original := bytes.Clone(test.data)
			for size := 0; size < len(test.data); size++ {
				ready, err := ProgressiveAudioReady(test.container, test.data[:size])
				if err != nil || ready {
					t.Fatalf("incomplete prefix %d/%d: ready=%t, err=%v", size, len(test.data), ready, err)
				}
			}
			ready, err := ProgressiveAudioReady(test.container, test.data)
			if err != nil || !ready {
				t.Fatalf("complete payload: ready=%t, err=%v", ready, err)
			}
			if !bytes.Equal(original, test.data) {
				t.Fatal("inspection mutated the prefix")
			}
		})
	}
}

func TestProgressiveAudioReadyClosedContainersAndPrefixLimit(t *testing.T) {
	for _, container := range []string{"", "MP3", "adts", "mp4", "ts", "webm"} {
		if _, err := ProgressiveAudioReady(container, nil); !errors.Is(err, ErrInvalidProgressiveStream) {
			t.Errorf("container %q: %v", container, err)
		}
	}
	for _, container := range []string{"mp3", "aac", "flac", "ogg", "wav", "m4a"} {
		if ready, err := ProgressiveAudioReady(container, make([]byte, MaxProgressivePrefixBytes+1)); ready || !errors.Is(err, ErrInvalidProgressiveStream) {
			t.Errorf("oversized %s: ready=%t, err=%v", container, ready, err)
		}
	}
	if MaxProgressivePrefixBytes > 4*1024*1024 {
		t.Fatal("prefix memory bound increased")
	}
}

func TestProgressiveMP3HeadersAndBoundedID3(t *testing.T) {
	for _, frame := range []struct {
		header [4]byte
		size   int
	}{
		{[4]byte{0xff, 0xfb, 0x58, 0}, 288}, // MPEG-1 Layer III, 64 kb/s, 32 kHz.
		{[4]byte{0xff, 0xfd, 0x58, 0}, 360}, // MPEG-1 Layer II, 80 kb/s, 32 kHz.
		{[4]byte{0xff, 0xff, 0x58, 0}, 240}, // MPEG-1 Layer I, 160 kb/s, 32 kHz.
		{[4]byte{0xff, 0xf3, 0x58, 0}, 180}, // MPEG-2 Layer III, 40 kb/s, 16 kHz.
		{[4]byte{0xff, 0xf5, 0x58, 0}, 360}, // MPEG-2 Layer II, 40 kb/s, 16 kHz.
		{[4]byte{0xff, 0xf7, 0x58, 0}, 240}, // MPEG-2 Layer I, 80 kb/s, 16 kHz.
		{[4]byte{0xff, 0xe3, 0x58, 0}, 360}, // MPEG-2.5 Layer III, 40 kb/s, 8 kHz.
		{[4]byte{0xff, 0xe5, 0x58, 0}, 720}, // MPEG-2.5 Layer II, 40 kb/s, 8 kHz.
		{[4]byte{0xff, 0xe7, 0x58, 0}, 480}, // MPEG-2.5 Layer I, 80 kb/s, 8 kHz.
		{[4]byte{0xff, 0xfb, 0x92, 0}, 418}, // Layer III padding adds one byte.
		{[4]byte{0xff, 0xff, 0x5a, 0}, 244}, // Layer I padding adds four bytes.
	} {
		for _, crc := range []bool{false, true} {
			data := progressiveReadyMP3(frame.header, frame.size)
			if crc {
				data[1] &^= 1
			}
			if ready, err := ProgressiveAudioReady("mp3", data[:len(data)-1]); ready || err != nil {
				t.Fatalf("partial MPEG frame header=%x crc=%t: %t, %v", frame.header, crc, ready, err)
			}
			if ready, err := ProgressiveAudioReady("mp3", data); !ready || err != nil {
				t.Fatalf("MPEG frame header=%x crc=%t: %t, %v", frame.header, crc, ready, err)
			}
		}
	}
	frame := progressiveReadyMP3([4]byte{0xff, 0xfb, 0x90, 0}, 417)
	for _, version := range []byte{2, 3, 4} {
		tag := []byte{'I', 'D', '3', version, 0, 0, 0, 0, 0, 3, 1, 2, 3}
		if ready, err := ProgressiveAudioReady("mp3", tag); ready || err != nil {
			t.Fatalf("ID3-only version %d: %t, %v", version, ready, err)
		}
		if ready, err := ProgressiveAudioReady("mp3", append(tag, frame...)); !ready || err != nil {
			t.Fatalf("ID3 version %d: %t, %v", version, ready, err)
		}
	}
	tag := []byte{'I', 'D', '3', 4, 0, 0x10, 0, 0, 0, 1, 0, '3', 'D', 'I', 4, 0, 0x10, 0, 0, 0, 1}
	if ready, err := ProgressiveAudioReady("mp3", append(bytes.Clone(tag), frame...)); !ready || err != nil {
		t.Fatalf("ID3 footer: %t, %v", ready, err)
	}
	tag[13] = 'X'
	progressiveReadyWantInvalid(t, "mp3", tag)
	for _, mutation := range []struct {
		index int
		value byte
	}{{0, 0}, {1, 0xeb}, {1, 0xf9}, {2, 0}, {2, 0xf0}, {2, 0x9c}, {3, 2}} {
		data := bytes.Clone(frame)
		data[mutation.index] = mutation.value
		progressiveReadyWantInvalid(t, "mp3", data)
	}
	progressiveReadyWantInvalid(t, "mp3", []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0})
	progressiveReadyWantInvalid(t, "mp3", []byte{'I', 'D', '3', 4, 0, 0, 2, 0, 0, 0})
	progressiveReadyWantInvalid(t, "mp3", []byte{'I', 'D', '3', 3, 0, 0x10, 0, 0, 0, 0})
}

func TestProgressiveADTSProtectedFramesAndInvalidSizes(t *testing.T) {
	for blocks := byte(0); blocks < 4; blocks++ {
		data := progressiveReadyADTS(true, blocks, []byte{1, 2, 3, 4, 5, 6, 7, 8})
		if ready, err := ProgressiveAudioReady("aac", data[:len(data)-1]); ready || err != nil {
			t.Fatalf("partial protected ADTS blocks=%d: %t, %v", blocks, ready, err)
		}
		if ready, err := ProgressiveAudioReady("aac", data); !ready || err != nil {
			t.Fatalf("protected ADTS blocks=%d: %t, %v", blocks, ready, err)
		}
	}
	frame := progressiveReadyADTS(false, 0, []byte{1, 2})
	for _, mutation := range []struct {
		index int
		value byte
	}{{0, 0}, {1, 0xf3}, {2, 0x7c}} {
		data := bytes.Clone(frame)
		data[mutation.index] = mutation.value
		progressiveReadyWantInvalid(t, "aac", data)
	}
	frame[4], frame[5] = 0, 0
	progressiveReadyWantInvalid(t, "aac", frame)
	progressiveReadyWantInvalid(t, "aac", progressiveReadyADTS(false, 0, nil))
}

func TestProgressiveFLACMetadataAndExtendedFrameNumbers(t *testing.T) {
	frame := []byte{0xff, 0xf8, 0xca, 0x18, 0}
	data := progressiveReadyFLAC(frame, []byte{0, 1, 2})
	// The STREAMINFO sample count and MD5 remain zero in this streaming header.
	metadata := append(bytes.Clone(data[:42]), []byte{0x81, 0, 0, 4, 0, 0, 0, 0}...)
	metadata[4] = 0
	if ready, err := ProgressiveAudioReady("flac", metadata); ready || err != nil {
		t.Fatalf("metadata without frame: %t, %v", ready, err)
	}
	if ready, err := ProgressiveAudioReady("flac", append(metadata, data[42:]...)); !ready || err != nil {
		t.Fatalf("multiple metadata blocks: %t, %v", ready, err)
	}
	for _, header := range [][]byte{
		{0xff, 0xf9, 0xca, 0x1e, 0xfe, 0x82, 0x80, 0x80, 0x80, 0x80, 0x80},
		{0xff, 0xf8, 0x6c, 0x18, 0, 31, 48},
		{0xff, 0xf8, 0x7d, 0x18, 0, 0, 31, 0xbb, 0x80},
		{0xff, 0xf8, 0x7e, 0x18, 0, 0, 31, 0x12, 0xc0},
	} {
		if ready, err := ProgressiveAudioReady("flac", progressiveReadyFLAC(header, []byte{0, 1, 2})); !ready || err != nil {
			t.Fatalf("valid FLAC extended header %x: %t, %v", header, ready, err)
		}
	}
	for _, header := range [][]byte{
		{0xff, 0xf8, 0x89, 0x18, 0xc0, 0x80},
		{0xff, 0xf8, 0x89, 0x18, 0xc2, 0x01},
		{0xff, 0xf8, 0x89, 0x18, 0xfe, 0x82, 0x80, 0x80, 0x80, 0x80, 0x80},
		{0xff, 0xf8, 0x89, 0x16, 0},
		{0xff, 0xf8, 0x09, 0x18, 0},
		{0xff, 0xf8, 0x6c, 0x18, 0, 31, 0},
	} {
		progressiveReadyWantInvalid(t, "flac", progressiveReadyFLAC(header, []byte{0, 1, 2}))
	}
	badCRC := bytes.Clone(data)
	badCRC[47] ^= 1
	progressiveReadyWantInvalid(t, "flac", badCRC)
	badSubframe := bytes.Clone(data)
	badSubframe[48] = 0x80
	progressiveReadyWantInvalid(t, "flac", badSubframe)
	for _, mutation := range []struct {
		index int
		value byte
	}{{0, 0}, {4, 0x81}, {7, 33}, {8, 0}, {9, 1}, {18, 0}, {19, 0}, {20, 0}} {
		bad := bytes.Clone(data)
		bad[mutation.index] = mutation.value
		// STREAMINFO's sample rate occupies three bytes; clear it as a field.
		if mutation.index >= 18 {
			bad[18], bad[19], bad[20] = 0, 0, bad[20]&0x0f
		}
		progressiveReadyWantInvalid(t, "flac", bad)
	}
	large := append(bytes.Clone(data[:42]), 0x81, 0x40, 0, 0)
	large[4] = 0
	progressiveReadyWantInvalid(t, "flac", large)
	blocks := bytes.Clone(data[:42])
	blocks[4] = 0
	for range maxProgressiveHeaderElements {
		blocks = append(blocks, 1, 0, 0, 0)
	}
	progressiveReadyWantInvalid(t, "flac", blocks)
}

func TestProgressiveOggPageGranulesAndContinuation(t *testing.T) {
	initial := progressiveReadyOggPage(2, 0, 0, []byte{8}, []byte("OpusHead"))
	partial := progressiveReadyOggPage(0, 1, -1, []byte{255}, make([]byte, 255))
	data := append(bytes.Clone(initial), partial...)
	if ready, err := ProgressiveAudioReady("ogg", data); ready || err != nil {
		t.Fatalf("granule -1 must remain pending: %t, %v", ready, err)
	}
	data = append(data, progressiveReadyOggPage(1, 2, 960, []byte{3}, []byte{1, 2, 3})...)
	if ready, err := ProgressiveAudioReady("ogg", data); !ready || err != nil {
		t.Fatalf("continued packet: %t, %v", ready, err)
	}
	// A page can contain a completed packet followed by an incomplete packet.
	data = append(bytes.Clone(initial), progressiveReadyOggPage(0, 1, 960, []byte{1, 255}, make([]byte, 256))...)
	if ready, err := ProgressiveAudioReady("ogg", data); !ready || err != nil {
		t.Fatalf("completed packet before continued packet: %t, %v", ready, err)
	}
	for _, page := range [][]byte{
		progressiveReadyOggPage(0, 1, -2, []byte{1}, []byte{1}),
		progressiveReadyOggPage(0, 1, 960, []byte{255}, make([]byte, 255)),
		progressiveReadyOggPage(1, 1, 960, []byte{1}, []byte{1}),
		progressiveReadyOggPage(0, 3, 960, []byte{1}, []byte{1}),
		progressiveReadyOggPage(2, 1, 960, []byte{1}, []byte{1}),
	} {
		progressiveReadyWantInvalid(t, "ogg", append(bytes.Clone(initial), page...))
	}
	badSerial := progressiveReadyOggPage(0, 1, 960, []byte{1}, []byte{1})
	badSerial[14] ^= 1
	progressiveReadyWantInvalid(t, "ogg", append(bytes.Clone(initial), badSerial...))
	ended := progressiveReadyOggPage(6, 0, 0, []byte{0}, nil)
	progressiveReadyWantInvalid(t, "ogg", append(ended, initial...))
	var pages []byte
	for sequence := 0; sequence < maxProgressiveHeaderElements; sequence++ {
		flags := byte(0)
		if sequence == 0 {
			flags = 2
		}
		pages = append(pages, progressiveReadyOggPage(flags, uint32(sequence), 0, []byte{0}, nil)...)
	}
	progressiveReadyWantInvalid(t, "ogg", pages)
}

func TestProgressiveWAVUnknownSizesAndPCM16Extensible(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		for _, extensible := range []bool{false, true} {
			data := progressiveReadyWAV(unknown, extensible, []byte{0, 0, 0, 0})
			if ready, err := ProgressiveAudioReady("wav", data[:len(data)-1]); ready || err != nil {
				t.Fatalf("partial PCM sample unknown=%t extensible=%t: %t, %v", unknown, extensible, ready, err)
			}
			if ready, err := ProgressiveAudioReady("wav", data); !ready || err != nil {
				t.Fatalf("PCM sample unknown=%t extensible=%t: %t, %v", unknown, extensible, ready, err)
			}
		}
	}
	data := progressiveReadyWAV(true, false, []byte{0, 0, 0, 0})
	for _, mutation := range []struct {
		index int
		value byte
	}{{0, 0}, {8, 0}, {20, 3}, {22, 0}, {28, 1}, {32, 2}, {34, 24}} {
		bad := bytes.Clone(data)
		bad[mutation.index] = mutation.value
		progressiveReadyWantInvalid(t, "wav", bad)
	}
	bad := progressiveReadyWAV(false, false, []byte{0, 0, 0, 0})
	binary.LittleEndian.PutUint32(bad[40:44], 3)
	progressiveReadyWantInvalid(t, "wav", bad)
	bad = progressiveReadyWAV(true, true, []byte{0, 0, 0, 0})
	bad[44] = 3
	progressiveReadyWantInvalid(t, "wav", bad)
	for _, size := range []uint32{0xffffffff, MaxProgressivePrefixBytes} {
		bad = append(bytes.Clone(data[:12]), []byte("JUNK\x00\x00\x00\x00")...)
		binary.LittleEndian.PutUint32(bad[16:20], size)
		progressiveReadyWantInvalid(t, "wav", bad)
	}
	bad = append(bytes.Clone(data[:12]), data[36:]...)
	progressiveReadyWantInvalid(t, "wav", bad)
	bad = append(bytes.Clone(data[:36]), data[12:]...)
	progressiveReadyWantInvalid(t, "wav", bad)
	bad = bytes.Clone(data[:12])
	for range maxProgressiveHeaderElements {
		bad = append(bad, []byte("JUNK\x00\x00\x00\x00")...)
	}
	progressiveReadyWantInvalid(t, "wav", bad)
}

func TestProgressiveM4ABoxBoundsAndSampleRuns(t *testing.T) {
	data := progressiveReadyM4A()
	for _, run := range [][]byte{
		// Two samples carry explicit durations and byte sizes.
		{0, 0, 3, 1, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 2, 0, 0, 4, 0, 0, 0, 0, 2},
		// Version one allows a negative composition-time offset.
		{1, 0, 8, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0xff, 0xff, 0xfc, 0},
	} {
		if ready, err := ProgressiveAudioReady("m4a", progressiveReadyM4AWithRun(run)); !ready || err != nil {
			t.Fatalf("MP4 explicit sample fields %x: %t, %v", run, ready, err)
		}
	}
	mdat := bytes.Index(data, []byte("mdat")) - 4
	if ready, err := ProgressiveAudioReady("m4a", data[:mdat]); ready || err != nil {
		t.Fatalf("fragment headers alone: %t, %v", ready, err)
	}
	// Extended lengths are legal when they fit the prefix and contain payload.
	extended := make([]byte, 16)
	binary.BigEndian.PutUint32(extended[:4], 1)
	copy(extended[4:8], "mdat")
	binary.BigEndian.PutUint64(extended[8:16], 20)
	extended = append(extended, 1, 2, 3, 4)
	if ready, err := ProgressiveAudioReady("m4a", append(bytes.Clone(data[:mdat]), extended...)); !ready || err != nil {
		t.Fatalf("extended media box: %t, %v", ready, err)
	}
	for _, size := range []uint64{0, 7, MaxProgressivePrefixBytes + 1, math.MaxUint64} {
		bad := bytes.Clone(extended)
		binary.BigEndian.PutUint64(bad[8:16], size)
		progressiveReadyWantInvalid(t, "m4a", append(bytes.Clone(data[:mdat]), bad...))
	}
	for _, kind := range []string{"ftyp", "moov", "moof", "mdat", "trak", "trun"} {
		bad := bytes.Clone(data)
		offset := bytes.Index(bad, []byte(kind)) - 4
		binary.BigEndian.PutUint32(bad[offset:offset+4], 0)
		progressiveReadyWantInvalid(t, "m4a", bad)
	}
	for _, kind := range []string{"soun", "mvex", "mfhd", "tfhd"} {
		bad := bytes.Clone(data)
		offset := bytes.Index(bad, []byte(kind))
		copy(bad[offset:offset+4], "none")
		progressiveReadyWantInvalid(t, "m4a", bad)
	}
	for _, count := range []uint32{0, 5, math.MaxUint32} {
		bad := bytes.Clone(data)
		offset := bytes.Index(bad, []byte("trun"))
		binary.BigEndian.PutUint32(bad[offset+8:offset+12], count)
		progressiveReadyWantInvalid(t, "m4a", bad)
	}
	bad := bytes.Clone(data)
	offset := bytes.Index(bad, []byte("trun"))
	bad[offset+6] = 2 // Advertise a sample-size field that is absent.
	progressiveReadyWantInvalid(t, "m4a", bad)
	progressiveReadyWantInvalid(t, "m4a", progressiveReadyBox("mdat", []byte{1}))
	progressiveReadyWantInvalid(t, "m4a", append(bytes.Clone(data[:mdat]), progressiveReadyBox("mdat", nil)...))
	// Sibling track fragments cannot supply each other's mandatory fields.
	moof := bytes.Index(data, []byte("moof")) - 4
	header := progressiveReadyBox("tfhd", []byte{0, 2, 0, 0, 0, 0, 0, 1})
	run := progressiveReadyBox("trun", []byte{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0})
	fragmentHeader := progressiveReadyBox("mfhd", []byte{0, 0, 0, 0, 0, 0, 0, 1})
	for _, tracks := range [][]byte{
		append(progressiveReadyBox("traf", header), progressiveReadyBox("traf", run)...),
		progressiveReadyBox("traf", append(append(bytes.Clone(header), header...), run...)),
		progressiveReadyBox("mdia", progressiveReadyBox("traf", append(bytes.Clone(header), run...))),
	} {
		bad = append(bytes.Clone(data[:moof]), progressiveReadyBox("moof", append(bytes.Clone(fragmentHeader), tracks...))...)
		bad = append(bad, data[mdat:]...)
		progressiveReadyWantInvalid(t, "m4a", bad)
	}
	bad = bytes.Clone(data)
	offset = bytes.Index(bad, []byte("hdlr")) - 4
	binary.BigEndian.PutUint32(bad[offset:offset+4], uint32(len(data)))
	progressiveReadyWantInvalid(t, "m4a", bad)
	var boxes []byte
	for range maxProgressiveHeaderElements + 1 {
		boxes = append(boxes, progressiveReadyBox("free", nil)...)
	}
	progressiveReadyWantInvalid(t, "m4a", boxes)
	boxes = progressiveReadyBox("free", make([]byte, MaxProgressivePrefixBytes-8))
	progressiveReadyWantInvalid(t, "m4a", boxes)
}

func progressiveReadyWantInvalid(t *testing.T, container string, data []byte) {
	t.Helper()
	if ready, err := ProgressiveAudioReady(container, data); ready || !errors.Is(err, ErrInvalidProgressiveStream) {
		t.Fatalf("invalid %s prefix: ready=%t, err=%v", container, ready, err)
	}
}

func progressiveReadyMP3(header [4]byte, size int) []byte {
	data := make([]byte, size)
	copy(data, header[:])
	return data
}

func progressiveReadyADTS(protected bool, blocks byte, payload []byte) []byte {
	header := 7
	if protected {
		header += 2 + int(blocks)*2
	}
	size := header + len(payload)
	data := make([]byte, size)
	copy(data, []byte{0xff, 0xf1, 0x50, 0x80, 0, 0x1f, 0xfc | blocks})
	if protected {
		data[1] &^= 1
	}
	data[3] |= byte(size >> 11)
	data[4] = byte(size >> 3)
	data[5] |= byte(size << 5)
	copy(data[header:], payload)
	return data
}

func progressiveReadyFLAC(frameHeader, payload []byte) []byte {
	data := make([]byte, 42)
	copy(data, []byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 34})
	binary.BigEndian.PutUint16(data[8:10], 4096)
	binary.BigEndian.PutUint16(data[10:12], 4096)
	// STREAMINFO packs sample rate, channels minus one, and bits minus one.
	bits := uint64(15)
	if frameHeader[3]>>1&7 == 7 {
		bits = 31
	}
	packed := uint64(48000)<<44 | uint64(1)<<41 | bits<<36
	binary.BigEndian.PutUint64(data[18:26], packed)
	data = append(data, frameHeader...)
	data = append(data, progressiveFLACHeaderCRC(frameHeader))
	return append(data, payload...)
}

func progressiveReadyOggPage(flags byte, sequence uint32, granule int64, laces, payload []byte) []byte {
	data := make([]byte, 27)
	copy(data, "OggS")
	data[5], data[26] = flags, byte(len(laces))
	binary.LittleEndian.PutUint64(data[6:14], uint64(granule))
	binary.LittleEndian.PutUint32(data[14:18], 123)
	binary.LittleEndian.PutUint32(data[18:22], sequence)
	data = append(data, laces...)
	return append(data, payload...)
}

func progressiveReadyOgg() []byte {
	data := progressiveReadyOggPage(2, 0, 0, []byte{8}, []byte("OpusHead"))
	data = append(data, progressiveReadyOggPage(0, 1, 0, []byte{8}, []byte("OpusTags"))...)
	return append(data, progressiveReadyOggPage(4, 2, 960, []byte{4}, []byte{1, 2, 3, 4})...)
}

func progressiveReadyWAV(unknown, extensible bool, payload []byte) []byte {
	format := make([]byte, 16)
	binary.LittleEndian.PutUint16(format[:2], 1)
	binary.LittleEndian.PutUint16(format[2:4], 2)
	binary.LittleEndian.PutUint32(format[4:8], 48000)
	binary.LittleEndian.PutUint32(format[8:12], 192000)
	binary.LittleEndian.PutUint16(format[12:14], 4)
	binary.LittleEndian.PutUint16(format[14:16], 16)
	if extensible {
		format[0], format[1] = 0xfe, 0xff
		format = append(format, 22, 0, 16, 0, 3, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71)
	}
	data := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(data[16:20], uint32(len(format)))
	data = append(data, format...)
	data = append(data, []byte("data\x00\x00\x00\x00")...)
	sizeOffset := len(data) - 4
	data = append(data, payload...)
	if unknown {
		binary.LittleEndian.PutUint32(data[4:8], math.MaxUint32)
		binary.LittleEndian.PutUint32(data[sizeOffset:sizeOffset+4], math.MaxUint32)
	} else {
		binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
		binary.LittleEndian.PutUint32(data[sizeOffset:sizeOffset+4], uint32(len(payload)))
	}
	return data
}

func progressiveReadyBox(kind string, payload []byte) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[:4], uint32(len(payload)+8))
	copy(data[4:8], kind)
	return append(data, payload...)
}

func progressiveReadyM4A() []byte {
	return progressiveReadyM4AWithRun([]byte{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0})
}

func progressiveReadyM4AWithRun(run []byte) []byte {
	data := progressiveReadyBox("ftyp", []byte("iso5\x00\x00\x00\x01iso5"))
	handler := make([]byte, 24)
	copy(handler[8:12], "soun")
	track := progressiveReadyBox("trak", progressiveReadyBox("mdia", progressiveReadyBox("hdlr", handler)))
	movie := append(track, progressiveReadyBox("mvex", progressiveReadyBox("trex", make([]byte, 24)))...)
	data = append(data, progressiveReadyBox("moov", movie)...)
	fragment := progressiveReadyBox("mfhd", []byte{0, 0, 0, 0, 0, 0, 0, 1})
	trackFragment := progressiveReadyBox("tfhd", []byte{0, 2, 0, 0, 0, 0, 0, 1})
	trackFragment = append(trackFragment, progressiveReadyBox("tfdt", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})...)
	trackFragment = append(trackFragment, progressiveReadyBox("trun", run)...)
	fragment = append(fragment, progressiveReadyBox("traf", trackFragment)...)
	data = append(data, progressiveReadyBox("moof", fragment)...)
	return append(data, progressiveReadyBox("mdat", []byte{1, 2, 3, 4})...)
}
