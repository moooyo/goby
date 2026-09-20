// Package bif reads and writes the Roku BIF version-zero archive format.
// It admits strictly increasing frame timestamps and complete JPEG images.
// Codec timestamps are unsigned units, not milliseconds or media ticks;
// multiplying by MultiplierMillis converts them to unsigned milliseconds.
package bif

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"math"
	"sort"
)

const (
	headerSize               = 64
	indexEntrySize           = 8
	minimumSize              = headerSize + indexEntrySize
	ioChunkSize              = 32 << 10
	hardMaxFrames     uint32 = 65536
	hardMaxFrameBytes uint32 = 8 << 20
	hardMaxTotalBytes uint64 = 512 << 20
	hardMaxDimension  uint32 = 4096
	hardMaxPixels     uint64 = 16 << 20
)

var magic = [8]byte{0x89, 'B', 'I', 'F', 0x0d, 0x0a, 0x1a, 0x0a}

var (
	ErrFormat             = errors.New("invalid BIF archive")
	ErrUnsupportedVersion = errors.New("unsupported BIF version")
	ErrLimit              = errors.New("BIF resource limit exceeded")
	ErrInvalidJPEG        = errors.New("invalid BIF JPEG image")
	ErrIndex              = errors.New("BIF frame index out of range")
	ErrInvalidLimits      = errors.New("invalid BIF resource limits")
)

// Limits are independent ceilings. A zero field selects that field's default;
// callers may tighten or raise it only within the documented hard ceiling.
// MaxTotalBytes includes the header, index, optional input padding and images.
type Limits struct {
	MaxFrames     uint32
	MaxFrameBytes uint32
	MaxTotalBytes uint64
	MaxDimension  uint32
	MaxPixels     uint64
}

func DefaultLimits() Limits {
	return Limits{MaxFrames: 4096, MaxFrameBytes: 2 << 20, MaxTotalBytes: 128 << 20, MaxDimension: 2048, MaxPixels: 4 << 20}
}

func normalizeLimits(limits Limits) (Limits, error) {
	defaults := DefaultLimits()
	if limits.MaxFrames == 0 {
		limits.MaxFrames = defaults.MaxFrames
	}
	if limits.MaxFrameBytes == 0 {
		limits.MaxFrameBytes = defaults.MaxFrameBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if limits.MaxDimension == 0 {
		limits.MaxDimension = defaults.MaxDimension
	}
	if limits.MaxPixels == 0 {
		limits.MaxPixels = defaults.MaxPixels
	}
	if limits.MaxFrames > hardMaxFrames || limits.MaxFrameBytes > hardMaxFrameBytes || limits.MaxTotalBytes > hardMaxTotalBytes ||
		limits.MaxTotalBytes < minimumSize || limits.MaxDimension > hardMaxDimension || limits.MaxPixels > hardMaxPixels {
		return Limits{}, ErrInvalidLimits
	}
	return limits, nil
}

// Frame is the convenience in-memory encoding input. JPEG is borrowed only
// during Encode. Timestamp is expressed in the header multiplier's units.
type Frame struct {
	Timestamp uint32
	JPEG      []byte
}

// FrameSource allows bounded streaming without retaining all image bytes.
// Size is the exact encoded JPEG size, including its SOI and EOI markers.
// Open is called once, in frame order, only after structural preflight. The
// codec always closes a non-nil returned reader before opening another one.
// Open and its reader must honor their context or provide bounded I/O.
type FrameSource struct {
	Timestamp uint32
	Size      int64
	Open      func(context.Context) (io.ReadCloser, error)
}

// Entry is a validated index record. Offset is absolute within the supplied
// ReaderAt view. Size is the next entry's offset minus this entry's offset.
// TimestampMillis uses uint64: all products of two uint32 values fit exactly.
type Entry struct {
	Timestamp       uint32
	TimestampMillis uint64
	Offset          uint32
	Size            uint32
}

// File owns an immutable copy of the verified index and borrows its ReaderAt.
// Open validates structure only. JPEG validates bytes on every selected read.
// The caller owns the reader's lifetime and must keep its bytes stable during
// an operation. The codec provides no source identity or authorization checks.
type File struct {
	reader     io.ReaderAt
	entries    []Entry
	multiplier uint32
	limits     Limits
}

func (f *File) Len() int {
	if f == nil {
		return 0
	}
	return len(f.entries)
}
func (f *File) Multiplier() uint32 {
	if f == nil {
		return 0
	}
	return f.multiplier
}
func (f *File) MultiplierMillis() uint32 {
	if f == nil || f.multiplier == 0 {
		return 1000
	}
	return f.multiplier
}

func (f *File) Entry(index int) (Entry, error) {
	if f == nil || index < 0 || index >= len(f.entries) {
		return Entry{}, ErrIndex
	}
	return f.entries[index], nil
}

// AtMillis selects the last frame at or before the requested presentation
// time. Before the first frame, or for an empty archive, it returns false.
func (f *File) AtMillis(milliseconds uint64) (int, bool) {
	if f == nil {
		return 0, false
	}
	index := sort.Search(len(f.entries), func(index int) bool { return f.entries[index].TimestampMillis > milliseconds }) - 1
	if index < 0 {
		return 0, false
	}
	return index, true
}

// Open reads only the fixed header and bounded index, without allocating the
// entire archive or decoding any JPEG. size is the exact ReaderAt view length.
// A first-image gap after the index is allowed by the Roku specification.
func Open(reader io.ReaderAt, size int64, limits Limits) (*File, error) {
	limits, err := normalizeLimits(limits)
	if err != nil {
		return nil, err
	}
	if reader == nil || size < minimumSize {
		return nil, ErrFormat
	}
	if uint64(size) > limits.MaxTotalBytes || uint64(size) > math.MaxUint32 {
		return nil, ErrLimit
	}
	var header [headerSize]byte
	if err := readExactAt(reader, header[:], 0); err != nil {
		return nil, fmt.Errorf("%w: read header: %w", ErrFormat, err)
	}
	if !bytes.Equal(header[:8], magic[:]) {
		return nil, fmt.Errorf("%w: magic", ErrFormat)
	}
	if binary.LittleEndian.Uint32(header[8:12]) != 0 {
		return nil, ErrUnsupportedVersion
	}
	for _, reserved := range header[20:] {
		if reserved != 0 {
			return nil, fmt.Errorf("%w: reserved header bytes", ErrFormat)
		}
	}
	count := binary.LittleEndian.Uint32(header[12:16])
	if count > limits.MaxFrames {
		return nil, ErrLimit
	}
	indexEnd := uint64(headerSize) + indexEntrySize*(uint64(count)+1)
	if indexEnd > uint64(size) {
		return nil, fmt.Errorf("%w: index exceeds archive", ErrFormat)
	}
	encoded := make([]byte, int(indexEnd-headerSize))
	if err := readExactAt(reader, encoded, headerSize); err != nil {
		return nil, fmt.Errorf("%w: read index: %w", ErrFormat, err)
	}
	f := &File{reader: reader, entries: make([]Entry, int(count)), multiplier: binary.LittleEndian.Uint32(header[16:20]), limits: limits}
	var previousTimestamp, previousOffset uint32
	for index := 0; index <= int(count); index++ {
		position := index * indexEntrySize
		timestamp := binary.LittleEndian.Uint32(encoded[position : position+4])
		offset := binary.LittleEndian.Uint32(encoded[position+4 : position+8])
		if uint64(offset) < indexEnd || uint64(offset) > uint64(size) || index > 0 && offset <= previousOffset {
			return nil, fmt.Errorf("%w: frame offset %d", ErrFormat, index)
		}
		if index == int(count) {
			if timestamp != math.MaxUint32 || uint64(offset) != uint64(size) {
				return nil, fmt.Errorf("%w: end sentinel", ErrFormat)
			}
		} else {
			if timestamp == math.MaxUint32 || index > 0 && timestamp <= previousTimestamp {
				return nil, fmt.Errorf("%w: frame timestamp %d", ErrFormat, index)
			}
			f.entries[index] = Entry{Timestamp: timestamp, TimestampMillis: uint64(timestamp) * uint64(f.MultiplierMillis()), Offset: offset}
		}
		if index > 0 {
			length := offset - previousOffset
			if length > limits.MaxFrameBytes {
				return nil, ErrLimit
			}
			f.entries[index-1].Size = length
		}
		previousTimestamp, previousOffset = timestamp, offset
	}
	return f, nil
}

// JPEG returns one complete validated JPEG and never reads neighboring frame
// bytes. It revalidates the current selected bytes, not a cached decode result.
// ReaderAt implementations must provide bounded I/O; context is checked between
// reads and before and after the bounded Go JPEG decoder.
func (f *File) JPEG(ctx context.Context, index int) ([]byte, error) {
	entry, err := f.Entry(index)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data := make([]byte, int(entry.Size))
	for position := 0; position < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(position+ioChunkSize, len(data))
		if err := readExactAt(f.reader, data[position:end], int64(entry.Offset)+int64(position)); err != nil {
			return nil, fmt.Errorf("read BIF JPEG: %w", err)
		}
		position = end
	}
	if err := validateJPEG(ctx, data, f.limits); err != nil {
		return nil, err
	}
	return data, nil
}

func readExactAt(reader io.ReaderAt, data []byte, offset int64) error {
	n, err := reader.ReadAt(data, offset)
	if n < 0 || n > len(data) {
		return fmt.Errorf("invalid ReaderAt byte count: %w", io.ErrUnexpectedEOF)
	}
	if n != len(data) {
		if err == nil || errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Encode is the convenience wrapper for small in-memory image collections.
// Large producers should use Write, which retains at most one JPEG at a time.
func Encode(ctx context.Context, frames []Frame, multiplier uint32, limits Limits) ([]byte, error) {
	limits, err := normalizeLimits(limits)
	if err != nil {
		return nil, err
	}
	if uint64(len(frames)) > uint64(limits.MaxFrames) {
		return nil, ErrLimit
	}
	sources := make([]FrameSource, len(frames))
	for index, frame := range frames {
		data := frame.JPEG
		sources[index] = FrameSource{Timestamp: frame.Timestamp, Size: int64(len(data)), Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
	}
	var output bytes.Buffer
	if _, err := Write(ctx, &output, sources, multiplier, limits); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// Write emits a BIF archive to a caller-owned writer. It preflights all sizes,
// timestamps and offsets, then reads and closes one source before validating
// and writing its JPEG. It never closes the writer. The byte count includes
// every successfully accepted output byte even when an error follows.
//
// JPEG or I/O failures can leave private partial output, including a header
// whose declared payload is incomplete. The caller must publish atomically only
// after a nil error and must discard partial output after any failure.
func Write(ctx context.Context, writer io.Writer, frames []FrameSource, multiplier uint32, limits Limits) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	limits, err := normalizeLimits(limits)
	if err != nil {
		return 0, err
	}
	if writer == nil {
		return 0, ErrFormat
	}
	if uint64(len(frames)) > uint64(limits.MaxFrames) {
		return 0, ErrLimit
	}
	// Openers are callbacks. Freeze declarations before invoking one so a
	// callback cannot alter a later frame's already-admitted layout or budget.
	frames = append([]FrameSource(nil), frames...)
	header, err := encodeIndex(frames, multiplier, limits)
	if err != nil {
		return 0, err
	}
	output := &boundedWriter{ctx: ctx, writer: writer}
	if err := output.write(header); err != nil {
		return output.count, err
	}
	for index, source := range frames {
		data, err := readFrameSource(ctx, source)
		if err != nil {
			return output.count, fmt.Errorf("read BIF frame %d: %w", index, err)
		}
		if err := validateJPEG(ctx, data, limits); err != nil {
			return output.count, fmt.Errorf("validate BIF frame %d: %w", index, err)
		}
		if err := output.write(data); err != nil {
			return output.count, err
		}
	}
	return output.count, ctx.Err()
}

func encodeIndex(frames []FrameSource, multiplier uint32, limits Limits) ([]byte, error) {
	if uint64(len(frames)) > uint64(limits.MaxFrames) {
		return nil, ErrLimit
	}
	indexEnd := uint64(headerSize) + indexEntrySize*(uint64(len(frames))+1)
	if indexEnd > limits.MaxTotalBytes || indexEnd > math.MaxUint32 {
		return nil, ErrLimit
	}
	total := indexEnd
	var previous uint32
	for index, frame := range frames {
		if frame.Open == nil || frame.Size <= 0 {
			return nil, fmt.Errorf("%w: empty frame source %d", ErrFormat, index)
		}
		if frame.Timestamp == math.MaxUint32 || index > 0 && frame.Timestamp <= previous {
			return nil, fmt.Errorf("%w: frame timestamp %d", ErrFormat, index)
		}
		if uint64(frame.Size) > uint64(limits.MaxFrameBytes) {
			return nil, ErrLimit
		}
		// Compare before addition so even an int64-sized declaration cannot
		// overflow the accumulator or wrap an encoded uint32 file offset.
		if uint64(frame.Size) > limits.MaxTotalBytes-total || uint64(frame.Size) > math.MaxUint32-total {
			return nil, ErrLimit
		}
		total += uint64(frame.Size)
		previous = frame.Timestamp
	}
	encoded := make([]byte, int(indexEnd))
	copy(encoded, magic[:])
	binary.LittleEndian.PutUint32(encoded[12:16], uint32(len(frames)))
	binary.LittleEndian.PutUint32(encoded[16:20], multiplier)
	offset := uint32(indexEnd)
	for index, frame := range frames {
		position := headerSize + index*indexEntrySize
		binary.LittleEndian.PutUint32(encoded[position:position+4], frame.Timestamp)
		binary.LittleEndian.PutUint32(encoded[position+4:position+8], offset)
		offset += uint32(frame.Size)
	}
	sentinel := headerSize + len(frames)*indexEntrySize
	binary.LittleEndian.PutUint32(encoded[sentinel:sentinel+4], math.MaxUint32)
	binary.LittleEndian.PutUint32(encoded[sentinel+4:sentinel+8], uint32(total))
	return encoded, nil
}

func readFrameSource(ctx context.Context, source FrameSource) (data []byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, openErr := source.Open(ctx)
	if reader != nil {
		defer func() {
			if closeErr := reader.Close(); closeErr != nil {
				data = nil
				err = errors.Join(err, fmt.Errorf("close frame source: %w", closeErr))
			}
		}()
	}
	if openErr != nil {
		return nil, openErr
	}
	if reader == nil {
		return nil, fmt.Errorf("%w: nil frame reader", ErrInvalidJPEG)
	}
	buffer := make([]byte, int(source.Size)+1)
	position, emptyReads := 0, 0
	for position < len(buffer) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(position+ioChunkSize, len(buffer))
		n, readErr := reader.Read(buffer[position:end])
		if n < 0 || n > end-position {
			return nil, fmt.Errorf("%w: invalid reader byte count", ErrInvalidJPEG)
		}
		position += n
		if int64(position) > source.Size {
			return nil, fmt.Errorf("%w: source exceeds declared size", ErrInvalidJPEG)
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			if int64(position) != source.Size {
				return nil, fmt.Errorf("%w: %w", ErrInvalidJPEG, io.ErrUnexpectedEOF)
			}
			return buffer[:position], nil
		}
		if n == 0 {
			emptyReads++
			if emptyReads >= 100 {
				return nil, io.ErrNoProgress
			}
		} else {
			emptyReads = 0
		}
	}
	return nil, fmt.Errorf("%w: source exceeds declared size", ErrInvalidJPEG)
}

type boundedWriter struct {
	ctx    context.Context
	writer io.Writer
	count  int64
}

func (writer *boundedWriter) write(data []byte) error {
	for len(data) > 0 {
		if err := writer.ctx.Err(); err != nil {
			return err
		}
		chunk := data[:min(len(data), ioChunkSize)]
		n, err := writer.writer.Write(chunk)
		if n < 0 || n > len(chunk) {
			return io.ErrShortWrite
		}
		writer.count += int64(n)
		data = data[n:]
		if err != nil {
			return err
		}
		if n != len(chunk) {
			return io.ErrShortWrite
		}
	}
	return writer.ctx.Err()
}

func validateJPEG(ctx context.Context, data []byte, limits Limits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 {
		return ErrInvalidJPEG
	}
	if uint64(len(data)) > uint64(limits.MaxFrameBytes) {
		return ErrLimit
	}
	if err := checkJPEGFraming(data); err != nil {
		return err
	}
	configuration, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: JPEG configuration: %w", ErrInvalidJPEG, err)
	}
	if configuration.Width <= 0 || configuration.Height <= 0 {
		return ErrInvalidJPEG
	}
	if uint64(configuration.Width) > uint64(limits.MaxDimension) || uint64(configuration.Height) > uint64(limits.MaxDimension) ||
		uint64(configuration.Width)*uint64(configuration.Height) > limits.MaxPixels {
		return ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: JPEG pixels: %w", ErrInvalidJPEG, err)
	}
	if decoded.Bounds().Dx() != configuration.Width || decoded.Bounds().Dy() != configuration.Height {
		return ErrInvalidJPEG
	}
	return ctx.Err()
}

// checkJPEGFraming finds the actual EOI rather than accepting a byte suffix.
// Segment payloads, entropy byte stuffing, restart markers and progressive
// scans are respected. The decoder separately validates the image semantics.
func checkJPEGFraming(data []byte) error {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return ErrInvalidJPEG
	}
	position := 2
	entropy := false
	for position < len(data) {
		if entropy {
			for position < len(data) && data[position] != 0xff {
				position++
			}
			if position == len(data) {
				return ErrInvalidJPEG
			}
		} else if data[position] != 0xff {
			return ErrInvalidJPEG
		}
		for position < len(data) && data[position] == 0xff {
			position++
		}
		if position == len(data) {
			return ErrInvalidJPEG
		}
		marker := data[position]
		position++
		if entropy && (marker == 0 || marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if marker == 0xd9 {
			if position != len(data) {
				return ErrInvalidJPEG
			}
			return nil
		}
		if marker == 0 || marker == 0xd8 || marker >= 0xd0 && marker <= 0xd7 {
			return ErrInvalidJPEG
		}
		if marker == 0x01 {
			continue
		}
		if position+2 > len(data) {
			return ErrInvalidJPEG
		}
		length := int(binary.BigEndian.Uint16(data[position : position+2]))
		if length < 2 || length > len(data)-position {
			return ErrInvalidJPEG
		}
		position += length
		entropy = marker == 0xda
	}
	return ErrInvalidJPEG
}
