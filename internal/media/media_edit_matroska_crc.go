package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"
)

const (
	mediaEditMatroskaCRCMaxScopes = 4096
	mediaEditMatroskaCRCChunkSize = 64 << 10
)

// mediaEditMatroskaCRC contains descriptor-bound offsets from the complete
// structural scan. ParentStart and ParentEnd delimit the parent's payload.
// They do not include the parent's element header. The CRC must be its first
// child; ElementEnd is exclusive and ValueOffset begins its four-byte value.
type mediaEditMatroskaCRC struct {
	ParentStart, ParentEnd   int64
	ElementStart, ElementEnd int64
	ValueOffset              int64
	Depth                    int
}

// verifyMediaEditMatroskaCRCs checks RFC 8794 CRC-32 values without changing
// the borrowed descriptor's position. The caller supplies every affected
// ancestor from its complete scan and must verify them before editing bytes.
// The caller retains exclusive control of the descriptor and its contents.
func verifyMediaEditMatroskaCRCs(ctx context.Context, file *os.File, scopes []mediaEditMatroskaCRC) error {
	ordered, size, err := prepareMediaEditMatroskaCRCs(ctx, file, scopes)
	if err != nil {
		return err
	}
	var buffer [mediaEditMatroskaCRCChunkSize]byte
	for _, scope := range ordered {
		got, err := sumMediaEditMatroskaCRC(ctx, file, scope, buffer[:])
		if err != nil {
			return err
		}
		want, err := readMediaEditMatroskaCRCHeader(ctx, file, scope)
		if err != nil {
			return err
		}
		if got != want {
			return mediaEditContainerError("Matroska CRC checksum mismatch")
		}
	}
	return checkMediaEditMatroskaCRCExtent(ctx, file, size)
}

// rewriteMediaEditMatroskaCRCs repairs an exclusively owned candidate after a
// separately verified edit. Old checksum values are intentionally not checked:
// the edit has already invalidated them. All layouts are admitted before the
// first write, and children are repaired before their ancestors. Only four
// bytes per value are written. A failure can leave a partially repaired
// candidate, which the caller must not publish. All values are verified again.
func rewriteMediaEditMatroskaCRCs(ctx context.Context, file *os.File, scopes []mediaEditMatroskaCRC) error {
	ordered, size, err := prepareMediaEditMatroskaCRCs(ctx, file, scopes)
	if err != nil {
		return err
	}
	var buffer [mediaEditMatroskaCRCChunkSize]byte
	var value [4]byte
	// Validated intervals are in parent-before-child order. Reversing this
	// order guarantees that an outer checksum includes repaired inner values.
	for i := len(ordered) - 1; i >= 0; i-- {
		scope := ordered[i]
		sum, err := sumMediaEditMatroskaCRC(ctx, file, scope, buffer[:])
		if err != nil {
			return err
		}
		if _, err := readMediaEditMatroskaCRCHeader(ctx, file, scope); err != nil {
			return err
		}
		if err := checkMediaEditMatroskaCRCExtent(ctx, file, size); err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(value[:], sum)
		n, err := file.WriteAt(value[:], scope.ValueOffset)
		if err != nil {
			return mediaEditMatroskaCRCIOError("write", err)
		}
		if n != len(value) {
			return mediaEditMatroskaCRCIOError("write", io.ErrShortWrite)
		}
	}
	if err := checkMediaEditMatroskaCRCExtent(ctx, file, size); err != nil {
		return err
	}
	if err := verifyMediaEditMatroskaCRCs(ctx, file, ordered); err != nil {
		return err
	}
	return checkMediaEditMatroskaCRCExtent(ctx, file, size)
}

func prepareMediaEditMatroskaCRCs(ctx context.Context, file *os.File, scopes []mediaEditMatroskaCRC) ([]mediaEditMatroskaCRC, int64, error) {
	if ctx == nil || file == nil {
		return nil, 0, mediaEditContainerError("invalid Matroska CRC descriptor or context")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if len(scopes) > mediaEditMatroskaCRCMaxScopes {
		return nil, 0, fmt.Errorf("%w: Matroska CRC scope count", ErrSubtitleRemovalBudget)
	}
	size, err := statMediaEditMatroskaCRCFile(file)
	if err != nil {
		return nil, 0, err
	}
	ordered := append([]mediaEditMatroskaCRC(nil), scopes...)
	for _, scope := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if scope.ParentStart < 0 || scope.ParentStart >= scope.ParentEnd || scope.ParentEnd > size ||
			scope.ElementStart != scope.ParentStart || scope.ElementEnd > scope.ParentEnd ||
			scope.ValueOffset < scope.ElementStart || scope.ValueOffset > scope.ElementEnd ||
			scope.ValueOffset-scope.ElementStart < 2 || scope.ValueOffset-scope.ElementStart > 9 ||
			scope.ElementEnd-scope.ValueOffset != 4 || scope.Depth < 0 || scope.Depth > mediaEditContainerMaxDepth {
			return nil, 0, mediaEditContainerError("invalid Matroska CRC scope layout")
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ParentStart == ordered[j].ParentStart {
			return ordered[i].ParentEnd > ordered[j].ParentEnd
		}
		return ordered[i].ParentStart < ordered[j].ParentStart
	})
	stack := make([]mediaEditMatroskaCRC, 0, mediaEditContainerMaxDepth+1)
	for _, scope := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		for len(stack) > 0 && scope.ParentStart >= stack[len(stack)-1].ParentEnd {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			if scope.ParentStart < parent.ElementEnd || scope.ParentEnd > parent.ParentEnd || scope.Depth <= parent.Depth {
				return nil, 0, mediaEditContainerError("duplicate or overlapping Matroska CRC scopes")
			}
		}
		stack = append(stack, scope)
		if _, err := readMediaEditMatroskaCRCHeader(ctx, file, scope); err != nil {
			return nil, 0, err
		}
	}
	return ordered, size, nil
}

func readMediaEditMatroskaCRCHeader(ctx context.Context, file *os.File, scope mediaEditMatroskaCRC) (uint32, error) {
	var header [13]byte // One-byte ID, at most eight size bytes, and four data bytes.
	data := header[:scope.ElementEnd-scope.ElementStart]
	if err := readMediaEditMatroskaCRC(ctx, file, data, scope.ElementStart); err != nil {
		return 0, err
	}
	if data[0] != 0xBF {
		return 0, mediaEditContainerError("invalid Matroska CRC element ID")
	}
	width, marker := 1, byte(0x80)
	for marker != 0 && data[1]&marker == 0 {
		width++
		marker >>= 1
	}
	if marker == 0 || int64(width+1) != scope.ValueOffset-scope.ElementStart {
		return 0, mediaEditContainerError("invalid Matroska CRC size encoding")
	}
	length := uint64(data[1] & (marker - 1))
	for _, value := range data[2 : width+1] {
		length = length<<8 | uint64(value)
	}
	// Four cannot be the all-ones unknown-size encoding for any VINT width.
	// Wider finite encodings of four are legal EBML sizes and stay untouched.
	if length != 4 {
		return 0, mediaEditContainerError("invalid Matroska CRC value size")
	}
	return binary.LittleEndian.Uint32(data[width+1:]), nil
}

func sumMediaEditMatroskaCRC(ctx context.Context, file *os.File, scope mediaEditMatroskaCRC, buffer []byte) (uint32, error) {
	var sum uint32
	// crc32.Update applies the IEEE initial and final all-ones complements.
	// The complete first CRC element is excluded, including its ID and size.
	for offset := scope.ElementEnd; offset < scope.ParentEnd; {
		chunk := buffer[:min(int64(len(buffer)), scope.ParentEnd-offset)]
		if err := readMediaEditMatroskaCRC(ctx, file, chunk, offset); err != nil {
			return 0, err
		}
		sum = crc32.Update(sum, crc32.IEEETable, chunk)
		offset += int64(len(chunk))
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return sum, nil
}

func readMediaEditMatroskaCRC(ctx context.Context, file *os.File, data []byte, offset int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := file.ReadAt(data, offset)
	if err != nil {
		return mediaEditMatroskaCRCIOError("read", err)
	}
	if n != len(data) {
		return mediaEditMatroskaCRCIOError("read", io.ErrUnexpectedEOF)
	}
	return nil
}

func statMediaEditMatroskaCRCFile(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, mediaEditMatroskaCRCIOError("stat", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > MaxSubtitleRemovalInputBytes+mediaEditOutputAllowance {
		return 0, mediaEditContainerError("invalid Matroska CRC descriptor extent")
	}
	return info.Size(), nil
}

func checkMediaEditMatroskaCRCExtent(ctx context.Context, file *os.File, expected int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	size, err := statMediaEditMatroskaCRCFile(file)
	if err != nil {
		return err
	}
	if size != expected {
		return mediaEditContainerError("Matroska CRC descriptor extent changed")
	}
	return nil
}

func mediaEditMatroskaCRCIOError(stage string, err error) error {
	for {
		var pathError *os.PathError
		if !errors.As(err, &pathError) {
			break
		}
		err = pathError.Err
	}
	return fmt.Errorf("%w: Matroska CRC %s: %w", ErrSubtitleRemovalUnsupported, stage, err)
}
