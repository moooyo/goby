package bif

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"testing"
)

func TestEncodeMatchesIndependentVersionZeroLayout(t *testing.T) {
	frames := []Frame{
		{Timestamp: 3, JPEG: bifTestJPEG(t, 12, 8)},
		{Timestamp: 12, JPEG: bifTestJPEG(t, 9, 5)},
	}
	got, err := Encode(context.Background(), frames, 250, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := bifTestArchive(frames, 250, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded archive differs from the independent version-zero layout: got %d bytes, want %d", len(got), len(want))
	}
	if !bytes.Equal(got[:20], []byte{0x89, 'B', 'I', 'F', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0, 2, 0, 0, 0, 250, 0, 0, 0}) {
		t.Fatalf("magic, version, count, or multiplier is not in its specified little-endian position: %x", got[:20])
	}
	if !bytes.Equal(got[20:64], make([]byte, 44)) {
		t.Fatal("version-zero reserved header bytes must be zero")
	}
	for index, frame := range frames {
		offset := 88
		if index == 1 {
			offset += len(frames[0].JPEG)
		}
		if binary.LittleEndian.Uint32(got[64+8*index:]) != frame.Timestamp || binary.LittleEndian.Uint32(got[68+8*index:]) != uint32(offset) {
			t.Fatalf("frame %d does not have its raw timestamp and absolute offset", index)
		}
	}
	if binary.LittleEndian.Uint32(got[80:]) != math.MaxUint32 || binary.LittleEndian.Uint32(got[84:]) != uint32(len(got)) {
		t.Fatal("the final index entry must contain the sentinel and the byte after the archive")
	}
}

func TestEmptyArchiveHasExactGoldenSentinel(t *testing.T) {
	for _, multiplier := range []uint32{0, 250, math.MaxUint32} {
		t.Run(fmt.Sprint(multiplier), func(t *testing.T) {
			got, err := Encode(context.Background(), nil, multiplier, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			want := make([]byte, 72)
			copy(want, []byte{0x89, 'B', 'I', 'F', 0x0d, 0x0a, 0x1a, 0x0a})
			binary.LittleEndian.PutUint32(want[16:20], multiplier)
			copy(want[64:72], []byte{0xff, 0xff, 0xff, 0xff, 72, 0, 0, 0})
			if !bytes.Equal(got, want) {
				t.Fatalf("empty archive must match the 72-byte golden: %x", got)
			}
			file, err := Open(bytes.NewReader(want), int64(len(want)), Limits{})
			if err != nil || file.Len() != 0 || file.Multiplier() != multiplier {
				t.Fatalf("open independent empty archive: file=%v, error=%v", file, err)
			}
			if _, ok := file.AtMillis(math.MaxUint64); ok {
				t.Fatal("an empty archive cannot select a frame")
			}
			if _, err := file.Entry(0); !errors.Is(err, ErrIndex) {
				t.Fatalf("empty entry lookup returned %v", err)
			}
			if data, err := file.JPEG(context.Background(), 0); !errors.Is(err, ErrIndex) || len(data) != 0 {
				t.Fatalf("empty image lookup returned %d bytes, %v", len(data), err)
			}
		})
	}
}

func TestOpenIndependentPaddedArchiveReadsOnlyMetadata(t *testing.T) {
	frames := []Frame{
		{Timestamp: 2, JPEG: bifTestJPEG(t, 12, 8)},
		{Timestamp: 7, JPEG: bifTestJPEG(t, 9, 5)},
		{Timestamp: 11, JPEG: bifTestJPEG(t, 7, 6)},
	}
	padding := []byte{0x7f, 0x00, 0xff, 0x12, 0x34, 0x89, 0x45, 0x00, 0x31}
	data := bifTestArchive(frames, 250, padding)
	reader := &bifTestReaderAt{data: data, maxEnd: 96}
	file, err := Open(reader, int64(len(data)), Limits{})
	if err != nil {
		t.Fatalf("opening metadata must not read initial padding or any image: %v", err)
	}
	if file.Len() != 3 || file.Multiplier() != 250 || file.MultiplierMillis() != 250 {
		t.Fatalf("wrong independent archive metadata: count=%d, raw=%d, effective=%d", file.Len(), file.Multiplier(), file.MultiplierMillis())
	}
	offset := uint32(96 + len(padding))
	for index, frame := range frames {
		entry, err := file.Entry(index)
		want := Entry{Timestamp: frame.Timestamp, TimestampMillis: uint64(frame.Timestamp) * 250, Offset: offset, Size: uint32(len(frame.JPEG))}
		if err != nil || entry != want {
			t.Fatalf("entry %d = %+v, %v; want %+v", index, entry, err, want)
		}
		offset += uint32(len(frame.JPEG))
	}
	reader.maxEnd, reader.reads = -1, nil
	got, err := file.JPEG(context.Background(), 1)
	if err != nil || !bytes.Equal(got, frames[1].JPEG) {
		t.Fatalf("read selected original JPEG: %d bytes, %v", len(got), err)
	}
	entry, _ := file.Entry(1)
	if len(reader.reads) == 0 {
		t.Fatal("image lookup did not read its selected bytes")
	}
	for _, read := range reader.reads {
		if read.offset < int64(entry.Offset) || read.offset+int64(read.size) > int64(entry.Offset)+int64(entry.Size) {
			t.Fatalf("selected image lookup read outside its indexed span: %+v, entry=%+v", read, entry)
		}
	}
}

func TestZeroMultiplierPreservesRawValueAndUsesOneSecondUnits(t *testing.T) {
	frames := []Frame{{Timestamp: 1, JPEG: bifTestJPEG(t, 8, 6)}, {Timestamp: 7, JPEG: bifTestJPEG(t, 7, 5)}}
	data := bifTestArchive(frames, 0, nil)
	file, err := Open(bytes.NewReader(data), int64(len(data)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if file.Multiplier() != 0 || file.MultiplierMillis() != 1000 {
		t.Fatalf("zero multiplier must preserve its stored value and expose 1000 ms units: raw=%d, effective=%d", file.Multiplier(), file.MultiplierMillis())
	}
	for index, want := range []uint64{1000, 7000} {
		entry, err := file.Entry(index)
		if err != nil || entry.TimestampMillis != want {
			t.Fatalf("entry %d timestamp = %d, %v; want %d ms", index, entry.TimestampMillis, err, want)
		}
	}
	if index, ok := file.AtMillis(6999); !ok || index != 0 {
		t.Fatalf("zero-multiplier lookup selected %d, %v; want frame zero", index, ok)
	}
	encoded, err := Encode(context.Background(), frames, 0, Limits{})
	if err != nil || binary.LittleEndian.Uint32(encoded[16:20]) != 0 {
		t.Fatalf("encoding must retain an explicitly zero multiplier: %v", err)
	}
}

func TestAtMillisSelectsLastTimestampNotAfterQuery(t *testing.T) {
	jpegData := bifTestJPEG(t, 8, 6)
	data := bifTestArchive([]Frame{{Timestamp: 2, JPEG: jpegData}, {Timestamp: 5, JPEG: jpegData}, {Timestamp: 11, JPEG: jpegData}}, 250, nil)
	file, err := Open(bytes.NewReader(data), int64(len(data)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query uint64
		index int
		ok    bool
	}{
		{0, 0, false}, {499, 0, false}, {500, 0, true}, {501, 0, true},
		{1249, 0, true}, {1250, 1, true}, {2749, 1, true}, {2750, 2, true},
		{100000, 2, true}, {math.MaxUint64, 2, true},
	} {
		if index, ok := file.AtMillis(test.query); ok != test.ok || ok && index != test.index {
			t.Errorf("AtMillis(%d) = %d, %v; want %d, %v", test.query, index, ok, test.index, test.ok)
		}
	}
}

func TestTimestampMultiplicationKeepsFullUint64Range(t *testing.T) {
	const timestamp, multiplier = uint32(math.MaxUint32 - 1), uint32(math.MaxUint32)
	wantMillis := uint64(timestamp) * uint64(multiplier)
	frames := []Frame{{Timestamp: timestamp, JPEG: bifTestJPEG(t, 8, 6)}}
	data := bifTestArchive(frames, multiplier, nil)
	file, err := Open(bytes.NewReader(data), int64(len(data)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := file.Entry(0)
	if err != nil || entry.TimestampMillis != wantMillis || entry.TimestampMillis <= math.MaxInt64 {
		t.Fatalf("timestamp multiplication narrowed an unsigned value: %+v, %v; want %d", entry, err, wantMillis)
	}
	if _, ok := file.AtMillis(wantMillis - 1); ok {
		t.Fatal("the large timestamp was selected one millisecond too early")
	}
	for _, query := range []uint64{wantMillis, math.MaxUint64} {
		if index, ok := file.AtMillis(query); !ok || index != 0 {
			t.Fatalf("large timestamp lookup at %d returned %d, %v", query, index, ok)
		}
	}
	encoded, err := Encode(context.Background(), frames, multiplier, Limits{})
	if err != nil || !bytes.Equal(encoded, data) {
		t.Fatalf("encoding rejected or narrowed valid uint32 timestamps and multiplier: %v", err)
	}
}

func TestOpenRejectsMalformedHeaderAndIndex(t *testing.T) {
	jpegData := bifTestJPEG(t, 8, 6)
	original := bifTestArchive([]Frame{{Timestamp: 2, JPEG: jpegData}, {Timestamp: 9, JPEG: jpegData}}, 1000, nil)
	for _, test := range []struct {
		name   string
		mutate func([]byte)
		want   error
	}{
		{"bad_magic", func(data []byte) { data[0] ^= 1 }, ErrFormat},
		{"unsupported_version", func(data []byte) { bifTestPut32(data, 8, 1) }, ErrUnsupportedVersion},
		{"reserved_start", func(data []byte) { data[20] = 1 }, ErrFormat},
		{"reserved_end", func(data []byte) { data[63] = 1 }, ErrFormat},
		{"missing_sentinel_timestamp", func(data []byte) { bifTestPut32(data, 80, 0) }, ErrFormat},
		{"sentinel_before_eof", func(data []byte) { bifTestPut32(data, 84, uint32(len(data)-1)) }, ErrFormat},
		{"sentinel_after_eof", func(data []byte) { bifTestPut32(data, 84, uint32(len(data)+1)) }, ErrFormat},
		{"first_offset_in_header", func(data []byte) { bifTestPut32(data, 68, 63) }, ErrFormat},
		{"first_offset_in_index", func(data []byte) { bifTestPut32(data, 68, 87) }, ErrFormat},
		{"first_offset_after_eof", func(data []byte) { bifTestPut32(data, 68, uint32(len(data)+1)) }, ErrFormat},
		{"zero_length_first_image", func(data []byte) { bifTestPut32(data, 76, 88) }, ErrFormat},
		{"descending_image_offsets", func(data []byte) { bifTestPut32(data, 76, 87) }, ErrFormat},
		{"second_image_at_eof", func(data []byte) { bifTestPut32(data, 76, uint32(len(data))) }, ErrFormat},
		{"offset_subtraction_underflow", func(data []byte) { bifTestPut32(data, 68, math.MaxUint32-8) }, ErrFormat},
		{"duplicate_timestamps", func(data []byte) { bifTestPut32(data, 72, 2) }, ErrFormat},
		{"descending_timestamps", func(data []byte) { bifTestPut32(data, 72, 1) }, ErrFormat},
		{"sentinel_used_as_frame_timestamp", func(data []byte) { bifTestPut32(data, 64, math.MaxUint32) }, ErrFormat},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := bytes.Clone(original)
			test.mutate(data)
			file, err := Open(bytes.NewReader(data), int64(len(data)), Limits{})
			if !errors.Is(err, test.want) || file != nil {
				t.Fatalf("malformed metadata returned file=%v, error=%v; want %v", file, err, test.want)
			}
		})
	}
}

func TestOpenRejectsSizeAndCountOverflowBeforeLargeReads(t *testing.T) {
	empty := bifTestArchive(nil, 0, nil)
	for _, size := range []int64{-1, 0, 63, 64, 71} {
		t.Run("short_size_"+fmt.Sprint(size), func(t *testing.T) {
			if file, err := Open(bytes.NewReader(empty), size, Limits{}); !errors.Is(err, ErrFormat) || file != nil {
				t.Fatalf("invalid size %d returned %v, %v", size, file, err)
			}
		})
	}
	for _, size := range []int64{(128 << 20) + 1, math.MaxUint32, int64(math.MaxUint32) + 1, math.MaxInt64} {
		t.Run("large_size_"+fmt.Sprint(size), func(t *testing.T) {
			reader := &bifTestReaderAt{data: empty, maxEnd: 72}
			if file, err := Open(reader, size, Limits{}); !errors.Is(err, ErrLimit) || file != nil {
				t.Fatalf("unrepresentable or over-budget size %d returned %v, %v", size, file, err)
			}
		})
	}
	for _, count := range []uint32{4097, 65537, math.MaxUint32} {
		t.Run("large_count_"+fmt.Sprint(count), func(t *testing.T) {
			data := bytes.Clone(empty)
			bifTestPut32(data, 12, count)
			reader := &bifTestReaderAt{data: data, maxEnd: 72}
			if file, err := Open(reader, int64(len(data)), Limits{}); !errors.Is(err, ErrLimit) || file != nil {
				t.Fatalf("oversized count %d returned %v, %v", count, file, err)
			}
		})
	}
	data := bytes.Clone(empty)
	bifTestPut32(data, 12, 1)
	if file, err := Open(bytes.NewReader(data), int64(len(data)), Limits{}); !errors.Is(err, ErrFormat) || file != nil {
		t.Fatalf("an index larger than the archive returned %v, %v", file, err)
	}
}

func TestOpenRejectsTruncatedReaderAndMetadataReadFaults(t *testing.T) {
	data := bifTestArchive([]Frame{{Timestamp: 2, JPEG: bifTestJPEG(t, 8, 6)}}, 1000, nil)
	for _, length := range []int{0, 7, 63, 64, 79} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			if file, err := Open(bytes.NewReader(data[:length]), int64(len(data)), Limits{}); err == nil || file != nil {
				t.Fatalf("truncated ReaderAt with advertised full size returned %v, %v", file, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		reader *bifTestReaderAt
	}{
		{"header_error", &bifTestReaderAt{data: data, maxEnd: -1, readErr: errors.New("controlled header failure")}},
		{"index_error", &bifTestReaderAt{data: data, maxEnd: 64}},
		{"short_read_without_error", &bifTestReaderAt{data: data, maxEnd: -1, shortRead: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if file, err := Open(test.reader, int64(len(data)), Limits{}); err == nil || file != nil {
				t.Fatalf("metadata ReaderAt failure returned %v, %v", file, err)
			}
		})
	}
}

func TestOpenAcceptsHardFrameCountWithoutReadingPayloads(t *testing.T) {
	frames := make([]Frame, 65536)
	for index := range frames {
		frames[index] = Frame{Timestamp: uint32(index), JPEG: []byte{0xff}}
	}
	data := bifTestArchive(frames, 1, nil)
	reader := &bifTestReaderAt{data: data, maxEnd: 64 + 8*(65536+1)}
	file, err := Open(reader, int64(len(data)), Limits{MaxFrames: 65536, MaxFrameBytes: 1})
	if err != nil || file.Len() != 65536 {
		t.Fatalf("the inclusive hard frame-count boundary was rejected: file=%v, error=%v", file, err)
	}
	entry, err := file.Entry(65535)
	if err != nil || entry.Timestamp != 65535 || entry.Size != 1 || uint64(entry.Offset)+uint64(entry.Size) != uint64(len(data)) {
		t.Fatalf("the final entry at the hard count boundary was truncated: %+v, %v", entry, err)
	}
}

func TestReaderAtMayReturnFullReadWithEOF(t *testing.T) {
	jpegData := bifTestJPEG(t, 8, 6)
	data := bifTestArchive([]Frame{{Timestamp: 2, JPEG: jpegData}}, 1000, nil)
	reader := &bifTestReaderAt{data: data, maxEnd: -1, fullReadEOF: true}
	file, err := Open(reader, int64(len(data)), Limits{})
	if err != nil {
		t.Fatalf("ReaderAt permits a complete read with EOF: %v", err)
	}
	got, err := file.JPEG(context.Background(), 0)
	if err != nil || !bytes.Equal(got, jpegData) {
		t.Fatalf("a complete image read accompanied by EOF was rejected: %d bytes, %v", len(got), err)
	}
}

func TestJPEGValidationIsDeferredAndRequiresOneCompleteImage(t *testing.T) {
	valid := bifTestJPEG(t, 12, 8)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"not_jpeg", []byte("not a JPEG image")},
		{"missing_soi", append([]byte{0, 0}, valid[2:]...)},
		{"missing_eoi", bytes.Clone(valid[:len(valid)-2])},
		{"trailing_byte", append(bytes.Clone(valid), 0)},
		{"concatenated_images", append(bytes.Clone(valid), valid...)},
		{"header_without_entropy", bifTestJPEGWithoutEntropy(t, valid)},
	} {
		t.Run(test.name, func(t *testing.T) {
			frames := []Frame{{Timestamp: 0, JPEG: test.data}}
			archive := bifTestArchive(frames, 1000, nil)
			file, err := Open(bytes.NewReader(archive), int64(len(archive)), Limits{})
			if err != nil {
				t.Fatalf("Open must not decode an indexed image: %v", err)
			}
			if data, err := file.JPEG(context.Background(), 0); !errors.Is(err, ErrInvalidJPEG) || len(data) != 0 {
				t.Fatalf("invalid indexed JPEG returned %d bytes, %v", len(data), err)
			}
			if data, err := Encode(context.Background(), frames, 1000, Limits{}); !errors.Is(err, ErrInvalidJPEG) || len(data) != 0 {
				t.Fatalf("invalid input JPEG produced %d archive bytes, %v", len(data), err)
			}
		})
	}
}

func TestJPEGFramingPreservesMarkerBytesInsideApplicationSegments(t *testing.T) {
	valid := bifTestJPEG(t, 12, 8)
	// APP15 is an opaque length-prefixed segment. Marker-looking bytes inside
	// its payload are not the actual end of this JPEG or a concatenated image.
	withSegment := append(bytes.Clone(valid[:2]), 0xff, 0xef, 0x00, 0x0a, 1, 2, 0xff, 0xd9, 0xff, 0xd8, 3, 4)
	withSegment = append(withSegment, valid[2:]...)
	frames := []Frame{{Timestamp: 0, JPEG: withSegment}}
	want := bifTestArchive(frames, 1000, nil)
	encoded, err := Encode(context.Background(), frames, 1000, Limits{})
	if err != nil || !bytes.Equal(encoded, want) {
		t.Fatalf("length-prefixed JPEG metadata was mistaken for framing: %v", err)
	}
	file, err := Open(bytes.NewReader(want), int64(len(want)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := file.JPEG(context.Background(), 0); err != nil || !bytes.Equal(got, withSegment) {
		t.Fatalf("selected JPEG metadata bytes were not retained: %d bytes, %v", len(got), err)
	}
}

func TestJPEGReadsCurrentBytesAndDoesNotTrustEarlierValidation(t *testing.T) {
	jpegData := bifTestJPEG(t, 12, 8)
	archive := bifTestArchive([]Frame{{Timestamp: 0, JPEG: jpegData}}, 1000, nil)
	reader := &bifTestReaderAt{data: archive, maxEnd: -1}
	file, err := Open(reader, int64(len(archive)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := file.JPEG(context.Background(), 0)
	if err != nil || !bytes.Equal(first, jpegData) {
		t.Fatalf("initial image read: %d bytes, %v", len(first), err)
	}
	first[0] = 0
	again, err := file.JPEG(context.Background(), 0)
	if err != nil || !bytes.Equal(again, jpegData) {
		t.Fatalf("caller mutation changed subsequent original reads: %d bytes, %v", len(again), err)
	}
	entry, _ := file.Entry(0)
	reader.data[int(entry.Offset)+int(entry.Size)-1] = 0
	if data, err := file.JPEG(context.Background(), 0); !errors.Is(err, ErrInvalidJPEG) || len(data) != 0 {
		t.Fatalf("a mutable ReaderAt bypassed repeated image validation: %d bytes, %v", len(data), err)
	}
}

func TestJPEGRejectsReadFaultsAndTruncationAfterOpen(t *testing.T) {
	for _, fault := range []string{"error", "short_nil", "truncated"} {
		t.Run(fault, func(t *testing.T) {
			archive := bifTestArchive([]Frame{{Timestamp: 0, JPEG: bifTestJPEG(t, 12, 8)}}, 1000, nil)
			reader := &bifTestReaderAt{data: archive, maxEnd: -1}
			file, err := Open(reader, int64(len(archive)), Limits{})
			if err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "error":
				reader.readErr = errors.New("controlled image read failure")
			case "short_nil":
				reader.shortRead = true
			case "truncated":
				reader.data = reader.data[:len(reader.data)-1]
			}
			if data, err := file.JPEG(context.Background(), 0); err == nil || len(data) != 0 {
				t.Fatalf("image read failure returned %d bytes, %v", len(data), err)
			}
		})
	}
}

func TestIndexesAndCancellationDoNotExposeImages(t *testing.T) {
	archive := bifTestArchive([]Frame{{Timestamp: 0, JPEG: bifTestJPEG(t, 12, 8)}}, 1000, nil)
	reader := &bifTestReaderAt{data: archive, maxEnd: -1}
	file, err := Open(reader, int64(len(archive)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	reader.reads = nil
	for _, index := range []int{-1, 1, math.MaxInt} {
		if _, err := file.Entry(index); !errors.Is(err, ErrIndex) {
			t.Errorf("Entry(%d) returned %v", index, err)
		}
		if data, err := file.JPEG(context.Background(), index); !errors.Is(err, ErrIndex) || len(data) != 0 {
			t.Errorf("JPEG(%d) returned %d bytes, %v", index, len(data), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, err := file.JPEG(ctx, 0); !errors.Is(err, context.Canceled) || len(data) != 0 {
		t.Fatalf("pre-canceled image request returned %d bytes, %v", len(data), err)
	}
	if len(reader.reads) != 0 {
		t.Fatalf("invalid indexes or an already canceled request performed reads: %+v", reader.reads)
	}
	if data, err := Encode(ctx, []Frame{{Timestamp: 0, JPEG: bifTestJPEG(t, 12, 8)}}, 1000, Limits{}); !errors.Is(err, context.Canceled) || len(data) != 0 {
		t.Fatalf("pre-canceled encoding returned %d bytes, %v", len(data), err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	reader.onRead = cancel
	if data, err := file.JPEG(ctx, 0); !errors.Is(err, context.Canceled) || len(data) != 0 {
		t.Fatalf("cancellation during a cooperative read returned %d bytes, %v", len(data), err)
	}
}

func TestDefaultAndPartialLimits(t *testing.T) {
	want := Limits{MaxFrames: 4096, MaxFrameBytes: 2 << 20, MaxTotalBytes: 128 << 20, MaxDimension: 2048, MaxPixels: 4 << 20}
	if got := DefaultLimits(); got != want {
		t.Fatalf("default budgets = %+v; want %+v", got, want)
	}
	frames := []Frame{{Timestamp: 0, JPEG: bifTestJPEG(t, 12, 8)}}
	for _, limits := range []Limits{{}, {MaxFrames: 1}, {MaxFrameBytes: 1 << 20}, {MaxTotalBytes: 1 << 20}, {MaxDimension: 12}, {MaxPixels: 96}} {
		data, err := Encode(context.Background(), frames, 1000, limits)
		if err != nil {
			t.Fatalf("zero fields must independently retain their defaults: limits=%+v, error=%v", limits, err)
		}
		file, err := Open(bytes.NewReader(data), int64(len(data)), limits)
		if err != nil {
			t.Fatalf("open with independently defaulted budgets %+v: %v", limits, err)
		}
		if _, err := file.JPEG(context.Background(), 0); err != nil {
			t.Fatalf("read with independently defaulted budgets %+v: %v", limits, err)
		}
	}
	// Independent budgets need not be ordered relative to one another when
	// the actual archive fits all of them.
	if data, err := Encode(context.Background(), nil, 1000, Limits{MaxFrameBytes: 1 << 20, MaxTotalBytes: 72}); err != nil || len(data) != 72 {
		t.Fatalf("independent limits rejected a fitting empty archive: %d bytes, %v", len(data), err)
	}
}

func TestInvalidLimitsAreRejectedForEncodingAndOpening(t *testing.T) {
	for _, limits := range []Limits{
		{MaxFrames: 65537}, {MaxFrameBytes: (8 << 20) + 1}, {MaxTotalBytes: (512 << 20) + 1},
		{MaxTotalBytes: 71}, {MaxDimension: 4097}, {MaxPixels: (16 << 20) + 1},
	} {
		empty := bifTestArchive(nil, 0, nil)
		if data, err := Encode(context.Background(), nil, 0, limits); !errors.Is(err, ErrInvalidLimits) || len(data) != 0 {
			t.Errorf("invalid encoding limits %+v returned %d bytes, %v", limits, len(data), err)
		}
		if file, err := Open(bytes.NewReader(empty), int64(len(empty)), limits); !errors.Is(err, ErrInvalidLimits) || file != nil {
			t.Errorf("invalid opening limits %+v returned %v, %v", limits, file, err)
		}
	}
}

func TestFrameAndArchiveBudgetsIncludeMetadataAndPadding(t *testing.T) {
	jpegData := bifTestJPEG(t, 12, 8)
	frames := []Frame{{Timestamp: 0, JPEG: jpegData}}
	archive := bifTestArchive(frames, 1000, nil)
	boundary := Limits{MaxFrames: 1, MaxFrameBytes: uint32(len(jpegData)), MaxTotalBytes: uint64(len(archive)), MaxDimension: 12, MaxPixels: 96}
	encoded, err := Encode(context.Background(), frames, 1000, boundary)
	if err != nil || !bytes.Equal(encoded, archive) {
		t.Fatalf("the exact configured boundaries must be accepted: %v", err)
	}
	file, err := Open(bytes.NewReader(archive), int64(len(archive)), boundary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.JPEG(context.Background(), 0); err != nil {
		t.Fatalf("image exactly at its dimension and pixel budgets was rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Limits)
	}{
		{"frame_bytes", func(limits *Limits) { limits.MaxFrameBytes-- }},
		{"archive_bytes", func(limits *Limits) { limits.MaxTotalBytes-- }},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := boundary
			test.mutate(&limits)
			if data, err := Encode(context.Background(), frames, 1000, limits); !errors.Is(err, ErrLimit) || len(data) != 0 {
				t.Fatalf("over-budget encoding returned %d bytes, %v", len(data), err)
			}
			if file, err := Open(bytes.NewReader(archive), int64(len(archive)), limits); !errors.Is(err, ErrLimit) || file != nil {
				t.Fatalf("over-budget index returned %v, %v", file, err)
			}
		})
	}
	padded := bifTestArchive(frames, 1000, []byte{1, 2, 3, 4})
	if file, err := Open(bytes.NewReader(padded), int64(len(padded)), boundary); !errors.Is(err, ErrLimit) || file != nil {
		t.Fatalf("initial padding escaped the archive byte budget: %v, %v", file, err)
	}
	two := append(append([]Frame(nil), frames...), Frame{Timestamp: 1, JPEG: jpegData})
	if data, err := Encode(context.Background(), two, 1000, Limits{MaxFrames: 1}); !errors.Is(err, ErrLimit) || len(data) != 0 {
		t.Fatalf("frame count exceeded its configured budget: %d bytes, %v", len(data), err)
	}
}

func TestDimensionAndPixelBudgetsAreCheckedWhenJPEGIsDecoded(t *testing.T) {
	frames := []Frame{{Timestamp: 0, JPEG: bifTestJPEG(t, 12, 8)}}
	archive := bifTestArchive(frames, 1000, nil)
	for _, limits := range []Limits{{MaxDimension: 11}, {MaxPixels: 95}} {
		file, err := Open(bytes.NewReader(archive), int64(len(archive)), limits)
		if err != nil {
			t.Fatalf("Open must defer JPEG dimensions to selected-image decoding: %v", err)
		}
		if data, err := file.JPEG(context.Background(), 0); !errors.Is(err, ErrLimit) || len(data) != 0 {
			t.Fatalf("oversized decoded image returned %d bytes, %v", len(data), err)
		}
		if data, err := Encode(context.Background(), frames, 1000, limits); !errors.Is(err, ErrLimit) || len(data) != 0 {
			t.Fatalf("oversized input image produced %d archive bytes, %v", len(data), err)
		}
	}
	// The declared dimensions already exceed the budget. Even with damaged
	// entropy data, the configuration limit must win before a full decode.
	damaged := []Frame{{Timestamp: 0, JPEG: bifTestJPEGWithoutEntropy(t, frames[0].JPEG)}}
	data := bifTestArchive(damaged, 1000, nil)
	limits := Limits{MaxDimension: 11}
	file, err := Open(bytes.NewReader(data), int64(len(data)), limits)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := file.JPEG(context.Background(), 0); !errors.Is(err, ErrLimit) || len(data) != 0 {
		t.Fatalf("full JPEG decoding ran before the configuration budget check: %d bytes, %v", len(data), err)
	}
	if data, err := Encode(context.Background(), damaged, 1000, limits); !errors.Is(err, ErrLimit) || len(data) != 0 {
		t.Fatalf("input dimensions were not checked before full decoding: %d bytes, %v", len(data), err)
	}
}

func TestEncodeRejectsTimestampViolationsAndLaterInvalidImagesAtomically(t *testing.T) {
	jpegData := bifTestJPEG(t, 8, 6)
	for _, test := range []struct {
		name   string
		frames []Frame
		want   error
	}{
		{"duplicate_timestamps", []Frame{{Timestamp: 7, JPEG: jpegData}, {Timestamp: 7, JPEG: jpegData}}, ErrFormat},
		{"descending_timestamps", []Frame{{Timestamp: 7, JPEG: jpegData}, {Timestamp: 6, JPEG: jpegData}}, ErrFormat},
		{"reserved_timestamp", []Frame{{Timestamp: math.MaxUint32, JPEG: jpegData}}, ErrFormat},
		{"later_invalid_image", []Frame{{Timestamp: 0, JPEG: jpegData}, {Timestamp: 7, JPEG: []byte("not JPEG")}}, ErrInvalidJPEG},
	} {
		t.Run(test.name, func(t *testing.T) {
			if data, err := Encode(context.Background(), test.frames, 1000, Limits{}); !errors.Is(err, test.want) || len(data) != 0 {
				t.Fatalf("invalid encoding returned partial bytes or the wrong error: %d bytes, %v; want %v", len(data), err, test.want)
			}
		})
	}
}

func bifTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			imageData.SetRGBA(x, y, color.RGBA{R: uint8(17 * x), G: uint8(23 * y), B: uint8(7 * (x + y)), A: 255})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, imageData, &jpeg.Options{Quality: 87}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// bifTestArchive writes the pinned Roku v0 wire layout independently of the
// package encoder. It deliberately permits malformed JPEG payload fixtures.
func bifTestArchive(frames []Frame, multiplier uint32, padding []byte) []byte {
	indexEnd := 64 + 8*(len(frames)+1)
	size := indexEnd + len(padding)
	for _, frame := range frames {
		size += len(frame.JPEG)
	}
	data := make([]byte, size)
	copy(data, []byte{0x89, 'B', 'I', 'F', 0x0d, 0x0a, 0x1a, 0x0a})
	bifTestPut32(data, 12, uint32(len(frames)))
	bifTestPut32(data, 16, multiplier)
	copy(data[indexEnd:], padding)
	offset := indexEnd + len(padding)
	for index, frame := range frames {
		bifTestPut32(data, 64+8*index, frame.Timestamp)
		bifTestPut32(data, 68+8*index, uint32(offset))
		copy(data[offset:], frame.JPEG)
		offset += len(frame.JPEG)
	}
	bifTestPut32(data, 64+8*len(frames), math.MaxUint32)
	bifTestPut32(data, 68+8*len(frames), uint32(size))
	return data
}

func bifTestPut32(data []byte, offset int, value uint32) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], value)
}

func bifTestJPEGWithoutEntropy(t *testing.T, valid []byte) []byte {
	t.Helper()
	scan := bytes.Index(valid, []byte{0xff, 0xda})
	if scan < 0 || scan+4 > len(valid) {
		t.Fatal("the generated JPEG has no complete start-of-scan marker")
	}
	end := scan + 2 + int(binary.BigEndian.Uint16(valid[scan+2:scan+4]))
	if end >= len(valid)-2 {
		t.Fatal("the generated JPEG has no entropy-coded image data")
	}
	damaged := append(bytes.Clone(valid[:end]), 0xff, 0xd9)
	if _, err := jpeg.DecodeConfig(bytes.NewReader(damaged)); err != nil {
		t.Fatalf("the controlled damaged image must retain its readable dimensions: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(damaged)); err == nil {
		t.Fatal("the controlled damaged image is unexpectedly decodable")
	}
	return damaged
}

type bifTestRead struct {
	offset int64
	size   int
}

type bifTestReaderAt struct {
	data        []byte
	reads       []bifTestRead
	maxEnd      int64
	readErr     error
	shortRead   bool
	fullReadEOF bool
	onRead      func()
}

func (reader *bifTestReaderAt) ReadAt(data []byte, offset int64) (int, error) {
	reader.reads = append(reader.reads, bifTestRead{offset: offset, size: len(data)})
	if reader.onRead != nil {
		reader.onRead()
	}
	if reader.readErr != nil {
		return 0, reader.readErr
	}
	if reader.maxEnd >= 0 && offset+int64(len(data)) > reader.maxEnd {
		return 0, fmt.Errorf("controlled reader refuses data beyond %d", reader.maxEnd)
	}
	n, err := bytes.NewReader(reader.data).ReadAt(data, offset)
	if reader.shortRead && n > 0 {
		return n - 1, nil
	}
	if reader.fullReadEOF && n == len(data) {
		return n, io.EOF
	}
	return n, err
}
