package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMediaEditMatroskaCRCKnownIEEEVectors(t *testing.T) {
	for _, vector := range []struct {
		text string
		sum  uint32
	}{
		{"", 0x00000000},
		{"a", 0xe8b7be43},
		{"ab", 0x9e83486d},
		{"abc", 0x352441c2},
		{"123456789", 0xcbf43926},
	} {
		for _, width := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("%q/size-width-%d", vector.text, width), func(t *testing.T) {
				if mediaEditMatroskaCRCTestReference([]byte(vector.text)) != vector.sum {
					t.Fatal("independent bitwise reference disagreed with the fixed IEEE vector")
				}
				data, scope := mediaEditMatroskaCRCTestEnvelope([]byte(vector.text), width, 4, []byte{0x99, 0xaa}, []byte{0xdd, 0xee})
				binary.LittleEndian.PutUint32(data[scope.ValueOffset:scope.ElementEnd], vector.sum)
				file := mediaEditMatroskaCRCTestFile(t, data)
				if _, err := file.Seek(1, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				if err := verifyMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); err != nil {
					t.Fatal(err)
				}
				mediaEditMatroskaCRCTestOffset(t, file, 1)
				if err := rewriteMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); err != nil {
					t.Fatal(err)
				}
				mediaEditMatroskaCRCTestOffset(t, file, 1)
				if !bytes.Equal(data, mediaEditMatroskaCRCTestBytes(t, file)) {
					t.Fatal("valid CRC rewrite changed bytes")
				}
			})
		}
	}
}

func TestMediaEditMatroskaCRCExcludesHeaderAndOtherParents(t *testing.T) {
	data, scope := mediaEditMatroskaCRCTestEnvelope([]byte("abc"), 2, 1, []byte("before"), []byte("after"))
	file := mediaEditMatroskaCRCTestFile(t, data)
	data[0] ^= 0xff
	data[len(data)-1] ^= 0xff
	if _, err := file.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); err != nil {
		t.Fatalf("bytes outside the parent affected its checksum: %v", err)
	}
	// The wide size VINT is excluded along with the CRC ID and value. A
	// correctly encoded big-endian checksum must not be accepted as storage.
	binary.BigEndian.PutUint32(data[scope.ValueOffset:scope.ElementEnd], 0x352441c2)
	if _, err := file.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("big-endian checksum was admitted: %v", err)
	}
}

func TestMediaEditMatroskaCRCPayloadAndValueTampering(t *testing.T) {
	for _, changeValue := range []bool{false, true} {
		t.Run(fmt.Sprint(changeValue), func(t *testing.T) {
			data, scope := mediaEditMatroskaCRCTestEnvelope([]byte("123456789"), 1, 1, nil, nil)
			if changeValue {
				data[scope.ValueOffset] ^= 1
			} else {
				data[scope.ElementEnd+3] ^= 1
			}
			file := mediaEditMatroskaCRCTestFile(t, data)
			if err := verifyMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("tampered checksum was admitted: %v", err)
			}
			// An already edited candidate necessarily has a stale old CRC.
			if err := rewriteMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); err != nil {
				t.Fatalf("repair incorrectly required an unchanged old checksum: %v", err)
			}
			want := append([]byte(nil), data...)
			binary.LittleEndian.PutUint32(want[scope.ValueOffset:scope.ElementEnd], mediaEditMatroskaCRCTestReference(want[scope.ElementEnd:scope.ParentEnd]))
			if !bytes.Equal(want, mediaEditMatroskaCRCTestBytes(t, file)) {
				t.Fatal("repair changed bytes outside the four-byte CRC value")
			}
		})
	}
}

func TestMediaEditMatroskaCRCNestedAndDisjointRepairs(t *testing.T) {
	innerData, inner := mediaEditMatroskaCRCTestEnvelope([]byte("abc"), 2, 7, nil, nil)
	siblingData, sibling := mediaEditMatroskaCRCTestEnvelope([]byte("123456789"), 1, 5, nil, nil)
	payload := append([]byte("left"), innerData...)
	payload = append(payload, []byte("middle")...)
	siblingStart := len(payload)
	payload = append(payload, siblingData...)
	payload = append(payload, []byte("right")...)
	data, outer := mediaEditMatroskaCRCTestEnvelope(payload, 1, 3, []byte{0x11, 0x22}, []byte{0x33, 0x44})
	inner = mediaEditMatroskaCRCTestShift(inner, outer.ElementEnd+4)
	sibling = mediaEditMatroskaCRCTestShift(sibling, outer.ElementEnd+int64(siblingStart))
	scopes := []mediaEditMatroskaCRC{sibling, outer, inner}
	originalScopes := append([]mediaEditMatroskaCRC(nil), scopes...)
	file := mediaEditMatroskaCRCTestFile(t, data)
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, scopes); err != nil {
		t.Fatal(err)
	}
	data[inner.ElementEnd] = 'z'
	data[sibling.ElementEnd] = '9'
	data[outer.ElementEnd] = 'L'
	if _, err := file.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, scopes); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("stale ancestor CRC was admitted: %v", err)
	}
	if _, err := file.Seek(9, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := rewriteMediaEditMatroskaCRCs(context.Background(), file, scopes); err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), data...)
	for _, scope := range []mediaEditMatroskaCRC{inner, sibling, outer} {
		binary.LittleEndian.PutUint32(want[scope.ValueOffset:scope.ElementEnd], mediaEditMatroskaCRCTestReference(want[scope.ElementEnd:scope.ParentEnd]))
	}
	if !bytes.Equal(want, mediaEditMatroskaCRCTestBytes(t, file)) {
		t.Fatal("nested repair did not include repaired child values or changed other bytes")
	}
	if !reflect.DeepEqual(scopes, originalScopes) {
		t.Fatal("CRC processing reordered the caller's scope slice")
	}
	mediaEditMatroskaCRCTestOffset(t, file, 9)
}

func TestMediaEditMatroskaCRCSharedEndsAndAdjacentParents(t *testing.T) {
	data := make([]byte, 48)
	scopes := []mediaEditMatroskaCRC{
		{ParentStart: 0, ParentEnd: 24, ElementStart: 0, ElementEnd: 6, ValueOffset: 2, Depth: 0},
		{ParentStart: 10, ParentEnd: 24, ElementStart: 10, ElementEnd: 16, ValueOffset: 12, Depth: 16},
		{ParentStart: 24, ParentEnd: 48, ElementStart: 24, ElementEnd: 30, ValueOffset: 26, Depth: 0},
	}
	for _, scope := range scopes {
		data[scope.ElementStart], data[scope.ElementStart+1] = 0xbf, 0x84
	}
	file := mediaEditMatroskaCRCTestFile(t, data)
	if err := rewriteMediaEditMatroskaCRCs(context.Background(), file, scopes); err != nil {
		t.Fatalf("legal shared ends, skipped depths or adjacent parents were rejected: %v", err)
	}
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, scopes); err != nil {
		t.Fatal(err)
	}
}

func TestMediaEditMatroskaCRCRejectsMalformedScopesAndHeaders(t *testing.T) {
	mutations := map[string]func([]byte, *mediaEditMatroskaCRC){
		"negative parent":      func(_ []byte, s *mediaEditMatroskaCRC) { s.ParentStart = -1 },
		"empty parent":         func(_ []byte, s *mediaEditMatroskaCRC) { s.ParentEnd = s.ParentStart },
		"parent past file":     func(_ []byte, s *mediaEditMatroskaCRC) { s.ParentEnd += 10 },
		"overflowing parent":   func(_ []byte, s *mediaEditMatroskaCRC) { s.ParentEnd = math.MaxInt64 },
		"not first element":    func(_ []byte, s *mediaEditMatroskaCRC) { s.ParentStart-- },
		"negative element":     func(_ []byte, s *mediaEditMatroskaCRC) { s.ElementStart = -1 },
		"element past parent":  func(_ []byte, s *mediaEditMatroskaCRC) { s.ElementEnd = s.ParentEnd + 1 },
		"value before element": func(_ []byte, s *mediaEditMatroskaCRC) { s.ValueOffset = s.ElementStart - 1 },
		"value past element":   func(_ []byte, s *mediaEditMatroskaCRC) { s.ValueOffset = s.ElementEnd + 1 },
		"overflowing value":    func(_ []byte, s *mediaEditMatroskaCRC) { s.ValueOffset = math.MaxInt64 },
		"wrong value extent":   func(_ []byte, s *mediaEditMatroskaCRC) { s.ElementEnd-- },
		"no size bytes": func(_ []byte, s *mediaEditMatroskaCRC) {
			s.ValueOffset = s.ElementStart + 1
			s.ElementEnd = s.ValueOffset + 4
		},
		"too many size bytes": func(_ []byte, s *mediaEditMatroskaCRC) {
			s.ValueOffset = s.ElementStart + 10
			s.ElementEnd = s.ValueOffset + 4
		},
		"negative depth":        func(_ []byte, s *mediaEditMatroskaCRC) { s.Depth = -1 },
		"excessive depth":       func(_ []byte, s *mediaEditMatroskaCRC) { s.Depth = mediaEditContainerMaxDepth + 1 },
		"wrong element ID":      func(data []byte, s *mediaEditMatroskaCRC) { data[s.ElementStart] = 0xec },
		"zero size marker":      func(data []byte, s *mediaEditMatroskaCRC) { data[s.ElementStart+1] = 0 },
		"unknown element size":  func(data []byte, s *mediaEditMatroskaCRC) { data[s.ElementStart+1] = 0xff },
		"wrong element size":    func(data []byte, s *mediaEditMatroskaCRC) { data[s.ElementStart+1] = 0x85 },
		"mismatched VINT width": func(data []byte, s *mediaEditMatroskaCRC) { data[s.ElementStart+1] = 0x40 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data, scope := mediaEditMatroskaCRCTestEnvelope([]byte("12345678901234567890"), 1, 1, []byte{0x99}, []byte{0xaa})
			mutate(data, &scope)
			file := mediaEditMatroskaCRCTestFile(t, data)
			for _, operation := range []func(context.Context, *os.File, []mediaEditMatroskaCRC) error{verifyMediaEditMatroskaCRCs, rewriteMediaEditMatroskaCRCs} {
				if err := operation(context.Background(), file, []mediaEditMatroskaCRC{scope}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
					t.Fatalf("malformed CRC was admitted: %v", err)
				}
			}
			if !bytes.Equal(data, mediaEditMatroskaCRCTestBytes(t, file)) {
				t.Fatal("malformed scope caused a write")
			}
		})
	}
}

func TestMediaEditMatroskaCRCRejectsDuplicateAndCrossingScopes(t *testing.T) {
	for name, regions := range map[string][][3]int64{
		"duplicate":              {{10, 80, 1}, {10, 80, 1}},
		"same start":             {{10, 80, 1}, {10, 50, 2}},
		"crossing":               {{10, 50, 1}, {30, 80, 2}},
		"inside excluded value":  {{10, 80, 1}, {14, 40, 2}},
		"same nested depth":      {{10, 80, 2}, {30, 50, 2}},
		"shallower nested depth": {{10, 80, 2}, {30, 50, 1}},
	} {
		t.Run(name, func(t *testing.T) {
			data := make([]byte, 100)
			var scopes []mediaEditMatroskaCRC
			for _, region := range regions {
				start := region[0]
				scopes = append(scopes, mediaEditMatroskaCRC{ParentStart: start, ParentEnd: region[1], ElementStart: start,
					ElementEnd: start + 6, ValueOffset: start + 2, Depth: int(region[2])})
				data[start], data[start+1] = 0xbf, 0x84
			}
			file := mediaEditMatroskaCRCTestFile(t, data)
			for _, operation := range []func(context.Context, *os.File, []mediaEditMatroskaCRC) error{verifyMediaEditMatroskaCRCs, rewriteMediaEditMatroskaCRCs} {
				if err := operation(context.Background(), file, scopes); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
					t.Fatalf("invalid scope relationship was admitted: %v", err)
				}
			}
			if !bytes.Equal(data, mediaEditMatroskaCRCTestBytes(t, file)) {
				t.Fatal("invalid scope relationship caused a write")
			}
		})
	}
}

func TestMediaEditMatroskaCRCPreflightsAllHeadersBeforeWriting(t *testing.T) {
	for _, badFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(badFirst), func(t *testing.T) {
			first, a := mediaEditMatroskaCRCTestEnvelope([]byte("abc"), 1, 1, nil, nil)
			second, b := mediaEditMatroskaCRCTestEnvelope([]byte("123456789"), 1, 1, nil, nil)
			b = mediaEditMatroskaCRCTestShift(b, int64(len(first)))
			data := append(first, second...)
			bad, stale := b, a
			if badFirst {
				bad, stale = a, b
			}
			// Test both orders: a lazy reverse-order implementation would
			// otherwise repair b before discovering a's malformed header.
			data[stale.ValueOffset] ^= 1
			data[bad.ElementStart] = 0xec
			file := mediaEditMatroskaCRCTestFile(t, data)
			if err := rewriteMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{a, b}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("malformed header was admitted: %v", err)
			}
			if !bytes.Equal(data, mediaEditMatroskaCRCTestBytes(t, file)) {
				t.Fatal("rewrite changed a CRC before admitting all headers")
			}
		})
	}
}

func TestMediaEditMatroskaCRCChunkedCoverage(t *testing.T) {
	payload := make([]byte, mediaEditMatroskaCRCChunkSize*3+17)
	for i := range payload {
		payload[i] = byte(i*31 + 7)
	}
	data, scope := mediaEditMatroskaCRCTestEnvelope(payload, 1, 1, []byte("prefix"), []byte("suffix"))
	file := mediaEditMatroskaCRCTestFile(t, data)
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); err != nil {
		t.Fatal(err)
	}
	data[scope.ParentEnd-1] ^= 1
	if _, err := file.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	if err := verifyMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("last partial chunk was not covered: %v", err)
	}
	if err := rewriteMediaEditMatroskaCRCs(context.Background(), file, []mediaEditMatroskaCRC{scope}); err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(want[scope.ValueOffset:scope.ElementEnd], mediaEditMatroskaCRCTestReference(want[scope.ElementEnd:scope.ParentEnd]))
	if !bytes.Equal(want, mediaEditMatroskaCRCTestBytes(t, file)) {
		t.Fatal("chunked repair differed from the independent bitwise CRC")
	}
}

func TestMediaEditMatroskaCRCHonorsCancellation(t *testing.T) {
	for _, duringRead := range []bool{false, true} {
		for _, rewrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("during-read-%t/rewrite-%t", duringRead, rewrite), func(t *testing.T) {
				data, scope := mediaEditMatroskaCRCTestEnvelope(bytes.Repeat([]byte{0xa5}, mediaEditMatroskaCRCChunkSize*12), 1, 1, nil, nil)
				file := mediaEditMatroskaCRCTestFile(t, data)
				ctx := &mediaEditMatroskaCRCTestContext{Context: context.Background()}
				if duringRead {
					ctx.remaining = 8
				}
				operation := verifyMediaEditMatroskaCRCs
				if rewrite {
					operation = rewriteMediaEditMatroskaCRCs
				}
				if err := operation(ctx, file, []mediaEditMatroskaCRC{scope}); !errors.Is(err, context.Canceled) {
					t.Fatalf("CRC processing ignored cancellation: %v", err)
				}
				if !bytes.Equal(data, mediaEditMatroskaCRCTestBytes(t, file)) {
					t.Fatal("single-scope repair wrote after cancellation during hashing")
				}
			})
		}
	}
}

func TestMediaEditMatroskaCRCRejectsInvalidDescriptors(t *testing.T) {
	for _, name := range []string{"nil context", "nil file", "closed file", "directory", "write-only file"} {
		t.Run(name, func(t *testing.T) {
			data, scope := mediaEditMatroskaCRCTestEnvelope([]byte("abc"), 1, 1, nil, nil)
			file := mediaEditMatroskaCRCTestFile(t, data)
			var ctx context.Context = context.Background()
			switch name {
			case "nil context":
				ctx = nil
			case "nil file":
				file = nil
			case "closed file":
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			case "directory":
				var err error
				file, err = os.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = file.Close() })
			case "write-only file":
				var err error
				file, err = os.OpenFile(file.Name(), os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = file.Close() })
			}
			for _, operation := range []func(context.Context, *os.File, []mediaEditMatroskaCRC) error{verifyMediaEditMatroskaCRCs, rewriteMediaEditMatroskaCRCs} {
				err := operation(ctx, file, []mediaEditMatroskaCRC{scope})
				if !errors.Is(err, ErrSubtitleRemovalUnsupported) {
					t.Fatalf("invalid descriptor was admitted: %v", err)
				}
				if file != nil && strings.Contains(err.Error(), file.Name()) {
					t.Fatal("descriptor error exposed a private path")
				}
			}
		})
	}
}

func TestMediaEditMatroskaCRCReadOnlyVerificationAndRepairFailure(t *testing.T) {
	data, scope := mediaEditMatroskaCRCTestEnvelope([]byte("abc"), 1, 1, nil, nil)
	file := mediaEditMatroskaCRCTestFile(t, data)
	readOnly, err := os.Open(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if err := verifyMediaEditMatroskaCRCs(context.Background(), readOnly, []mediaEditMatroskaCRC{scope}); err != nil {
		t.Fatal(err)
	}
	err = rewriteMediaEditMatroskaCRCs(context.Background(), readOnly, []mediaEditMatroskaCRC{scope})
	if !errors.Is(err, ErrSubtitleRemovalUnsupported) || strings.Contains(err.Error(), file.Name()) {
		t.Fatalf("read-only repair did not fail safely: %v", err)
	}
	if !bytes.Equal(data, mediaEditMatroskaCRCTestBytes(t, file)) {
		t.Fatal("read-only repair changed bytes")
	}
}

func TestMediaEditMatroskaCRCScopeBudgetAndEmptyInventory(t *testing.T) {
	file := mediaEditMatroskaCRCTestFile(t, nil)
	for _, operation := range []func(context.Context, *os.File, []mediaEditMatroskaCRC) error{verifyMediaEditMatroskaCRCs, rewriteMediaEditMatroskaCRCs} {
		if err := operation(context.Background(), file, nil); err != nil {
			t.Fatalf("empty CRC inventory failed: %v", err)
		}
		if err := operation(context.Background(), file, make([]mediaEditMatroskaCRC, mediaEditMatroskaCRCMaxScopes+1)); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("excessive inventory was admitted: %v", err)
		}
	}
	if len(mediaEditMatroskaCRCTestBytes(t, file)) != 0 {
		t.Fatal("empty inventory extended the file")
	}
}

func TestMediaEditMatroskaCRCIOErrorPreservesCauseWithoutPaths(t *testing.T) {
	cause := errors.New("underlying checksum I/O failure")
	err := mediaEditMatroskaCRCIOError("read", &os.PathError{Op: "read", Path: "private-source-name.mkv",
		Err: &os.PathError{Op: "write", Path: "private-candidate-name.mkv", Err: cause}})
	if !errors.Is(err, cause) || !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("I/O cause or classification was lost: %v", err)
	}
	if strings.Contains(err.Error(), "private-") {
		t.Fatal("nested path error was not stripped")
	}
}

func mediaEditMatroskaCRCTestEnvelope(payload []byte, width, depth int, prefix, suffix []byte) ([]byte, mediaEditMatroskaCRC) {
	start := int64(len(prefix))
	value := start + 1 + int64(width)
	end := value + 4
	data := append([]byte(nil), prefix...)
	data = append(data, make([]byte, 1+width+4)...)
	data[start] = 0xbf
	data[start+1] = byte(1 << (8 - width))
	data[start+int64(width)] |= 4
	binary.LittleEndian.PutUint32(data[value:end], mediaEditMatroskaCRCTestReference(payload))
	data = append(data, payload...)
	parentEnd := int64(len(data))
	data = append(data, suffix...)
	return data, mediaEditMatroskaCRC{ParentStart: start, ParentEnd: parentEnd, ElementStart: start, ElementEnd: end, ValueOffset: value, Depth: depth}
}

func mediaEditMatroskaCRCTestShift(scope mediaEditMatroskaCRC, offset int64) mediaEditMatroskaCRC {
	scope.ParentStart += offset
	scope.ParentEnd += offset
	scope.ElementStart += offset
	scope.ElementEnd += offset
	scope.ValueOffset += offset
	return scope
}

// This deliberately uses the bitwise IEEE definition instead of the helper's
// standard-library table path. Fixed published vectors independently anchor it.
func mediaEditMatroskaCRCTestReference(data []byte) uint32 {
	crc := ^uint32(0)
	for _, value := range data {
		crc ^= uint32(value)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func mediaEditMatroskaCRCTestFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "private-matroska-crc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	return file
}

func mediaEditMatroskaCRCTestBytes(t *testing.T, file *os.File) []byte {
	t.Helper()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, info.Size())
	if len(data) > 0 {
		if _, err := file.ReadAt(data, 0); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

func mediaEditMatroskaCRCTestOffset(t *testing.T, file *os.File, expected int64) {
	t.Helper()
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != expected {
		t.Fatalf("borrowed descriptor offset changed: got %d, want %d, error=%v", position, expected, err)
	}
}

type mediaEditMatroskaCRCTestContext struct {
	context.Context
	remaining int
}

func (c *mediaEditMatroskaCRCTestContext) Err() error {
	if c.remaining == 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}
