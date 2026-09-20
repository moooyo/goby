package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaEditMP4StructuralCopyPreservesUnselectedBytes(t *testing.T) {
	for _, test := range []struct {
		name     string
		prefix   int
		body     int
		suffix   int
		extended bool
	}{
		{"ordinary header", 29, 73, 41, false},
		{"extended header", 29, 73, 41, true},
		{"track at start", 0, 31, 37, false},
		{"track at end", 23, 31, 0, true},
		{"kind crosses chunk", (128 << 10) - 6, 79, 47, false},
		{"extended size crosses chunk", (128 << 10) - 12, 79, 47, true},
		{"body spans chunks", 53, (3 << 17) + 19, (1 << 17) + 11, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData(test.prefix, test.body, test.suffix, test.extended)
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			digest, err := runMediaEditMP4StructuralCopy(context.Background(), input, candidate, plan, plan.SourceBytes)
			if err != nil {
				t.Fatal(err)
			}
			expected := bytes.Clone(data)
			copy(expected[plan.TrackTypeOffset:plan.TrackTypeOffset+4], "free")
			clear(expected[plan.TrackBodyOffset:plan.TrackEnd])
			actual := mediaEditMP4CopyTestRead(t, candidate)
			if !bytes.Equal(actual, expected) {
				t.Fatal("candidate changed bytes outside the selected type and body, or failed to erase the selected body")
			}
			if !bytes.Equal(mediaEditMP4CopyTestRead(t, input), data) {
				t.Fatal("source bytes changed")
			}
			sourceDigest := mediaEditMP4CopyTestDigest(data, plan)
			candidateDigest := mediaEditMP4CopyTestDigest(actual, plan)
			if digest != sourceDigest || digest != candidateDigest {
				t.Fatalf("retained digest = %q, source = %q, candidate = %q", digest, sourceDigest, candidateDigest)
			}
			mediaEditMP4CopyTestOffset(t, input, 11)
			mediaEditMP4CopyTestOffset(t, candidate, 17)
		})
	}
}

func TestMediaEditMP4StructuralFinalProofRejectsPostCopyChanges(t *testing.T) {
	for _, name := range []string{"unchanged", "appended free box", "unreferenced bytes", "removed body", "free type"} {
		t.Run(name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData(29, 73, 41, true)
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			digest, err := runMediaEditMP4StructuralCopy(context.Background(), input, candidate, plan, plan.SourceBytes)
			if err != nil {
				t.Fatal(err)
			}
			var mutation []byte
			var offset int64
			switch name {
			case "appended free box":
				mutation, offset = []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}, plan.SourceBytes
			case "unreferenced bytes":
				mutation, offset = []byte{data[len(data)-1] ^ 0xff}, plan.SourceBytes-1
			case "removed body":
				mutation, offset = []byte{1}, plan.TrackBodyOffset
			case "free type":
				mutation, offset = []byte("trak"), plan.TrackTypeOffset
			}
			if mutation != nil {
				if _, err := candidate.WriteAt(mutation, offset); err != nil {
					t.Fatal(err)
				}
			}
			err = verifyMediaEditMP4StructuralCandidate(context.Background(), candidate, plan, digest)
			if name == "unchanged" && err != nil || name != "unchanged" && !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("post-copy final evidence binding = %v", err)
			}
			mediaEditMP4CopyTestOffset(t, candidate, 17)
		})
	}
}

func TestMediaEditMP4StructuralCopyDigestCoversOnlyRetainedRanges(t *testing.T) {
	data, plan := mediaEditMP4CopyTestData(29, 73, 41, true)
	var original string
	for _, test := range []struct {
		name   string
		change func([]byte)
		same   bool
	}{
		{"original", func([]byte) {}, true},
		{"removed body", func(data []byte) { data[plan.TrackBodyOffset+7] ^= 0xff }, true},
		{"retained prefix", func(data []byte) { data[3] ^= 0xff }, false},
		{"retained suffix", func(data []byte) { data[len(data)-3] ^= 0xff }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := bytes.Clone(data)
			test.change(changed)
			input, candidate := mediaEditMP4CopyTestFiles(t, changed)
			digest, err := runMediaEditMP4StructuralCopy(context.Background(), input, candidate, plan, plan.SourceBytes+100)
			if err != nil {
				t.Fatal(err)
			}
			if original == "" {
				original = digest
			}
			if (digest == original) != test.same {
				t.Fatalf("digest equality = %v, want %v", digest == original, test.same)
			}
		})
	}
}

func TestMediaEditMP4StructuralCopyRejectsInvalidPlansAndHeaders(t *testing.T) {
	for _, test := range []struct {
		name     string
		extended bool
		change   func([]byte, *mediaEditMP4RemovalPlan)
	}{
		{"zero source size", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.SourceBytes = 0 }},
		{"negative source size", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.SourceBytes = -1 }},
		{"source shorter than plan", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.SourceBytes++ }},
		{"source longer than plan", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.SourceBytes-- }},
		{"zero track ID", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.RemovedTrackID = 0 }},
		{"oversized track ID", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.RemovedTrackID = 1 << 32 }},
		{"negative type offset", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackTypeOffset = -1 }},
		{"type lacks size header", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackTypeOffset = 3 }},
		{"type beyond source", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackTypeOffset = p.SourceBytes + 1 }},
		{"body overlaps kind", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackBodyOffset = p.TrackTypeOffset + 3 }},
		{"unknown header length", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackBodyOffset++ }},
		{"body before kind", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackBodyOffset = p.TrackTypeOffset - 1 }},
		{"empty body", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackEnd = p.TrackBodyOffset }},
		{"end before body", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackEnd = p.TrackBodyOffset - 1 }},
		{"end beyond source", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackEnd = p.SourceBytes + 1 }},
		{"extreme type offset", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackTypeOffset = 1<<63 - 1 }},
		{"extreme body offset", false, func(_ []byte, p *mediaEditMP4RemovalPlan) { p.TrackBodyOffset = 1<<63 - 1 }},
		{"wrong kind", false, func(data []byte, p *mediaEditMP4RemovalPlan) { copy(data[p.TrackTypeOffset:], "free") }},
		{"case differs", false, func(data []byte, p *mediaEditMP4RemovalPlan) { copy(data[p.TrackTypeOffset:], "TRAK") }},
		{"ordinary size mismatch", false, func(data []byte, p *mediaEditMP4RemovalPlan) { data[p.TrackTypeOffset-1]++ }},
		{"ordinary header declares extended size", false, func(data []byte, p *mediaEditMP4RemovalPlan) {
			binary.BigEndian.PutUint32(data[p.TrackTypeOffset-4:p.TrackTypeOffset], 1)
		}},
		{"implicit size", false, func(data []byte, p *mediaEditMP4RemovalPlan) {
			clear(data[p.TrackTypeOffset-4 : p.TrackTypeOffset])
		}},
		{"extended size mismatch", true, func(data []byte, p *mediaEditMP4RemovalPlan) { data[p.TrackBodyOffset-1]++ }},
		{"extended header lacks marker", true, func(data []byte, p *mediaEditMP4RemovalPlan) {
			binary.BigEndian.PutUint32(data[p.TrackTypeOffset-4:p.TrackTypeOffset], uint32(p.TrackEnd-p.TrackTypeOffset+4))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData(29, 73, 41, test.extended)
			test.change(data, &plan)
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			digest, err := runMediaEditMP4StructuralCopy(context.Background(), input, candidate, plan, int64(len(data))+1)
			if !errors.Is(err, ErrSubtitleRemovalUnsupported) || digest != "" {
				t.Fatalf("invalid copy = %q, %v", digest, err)
			}
			mediaEditMP4CopyTestUnwritten(t, input, candidate, data)
		})
	}
}

func TestMediaEditMP4StructuralCopyRejectsInsufficientBudget(t *testing.T) {
	for _, test := range []struct {
		name      string
		budget    int64
		oversized bool
	}{
		{"negative budget", -1, false},
		{"zero budget", 0, false},
		{"one byte short", 151, false},
		{"source above maximum", MaxSubtitleRemovalInputBytes + 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData(29, 73, 42, false)
			if test.oversized {
				plan.SourceBytes = MaxSubtitleRemovalInputBytes + 1
			}
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			digest, err := runMediaEditMP4StructuralCopy(context.Background(), input, candidate, plan, test.budget)
			if !errors.Is(err, ErrSubtitleRemovalBudget) || digest != "" {
				t.Fatalf("over-budget copy = %q, %v", digest, err)
			}
			mediaEditMP4CopyTestUnwritten(t, input, candidate, data)
		})
	}
}

func TestMediaEditMP4StructuralCopyRejectsInvalidDescriptors(t *testing.T) {
	for _, name := range []string{"nil context", "nil source", "nil candidate", "same descriptor", "same file", "nonempty candidate", "closed source", "closed candidate", "read-only candidate", "write-only candidate", "nonregular source", "nonregular candidate"} {
		t.Run(name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData(29, 73, 41, false)
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			originalInput, originalCandidate := input, candidate
			var ctx context.Context = context.Background()
			var underlying error
			switch name {
			case "nil context":
				ctx = nil
			case "nil source":
				input = nil
			case "nil candidate":
				candidate = nil
			case "same descriptor":
				candidate = input
			case "same file":
				var err error
				candidate, err = os.OpenFile(input.Name(), os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { candidate.Close() })
			case "nonempty candidate":
				if _, err := candidate.WriteAt([]byte("existing candidate"), 0); err != nil {
					t.Fatal(err)
				}
			case "closed source":
				if err := input.Close(); err != nil {
					t.Fatal(err)
				}
				underlying = os.ErrClosed
			case "closed candidate":
				if err := candidate.Close(); err != nil {
					t.Fatal(err)
				}
				underlying = os.ErrClosed
			case "read-only candidate", "write-only candidate":
				mode := os.O_RDONLY
				if name == "write-only candidate" {
					mode = os.O_WRONLY
				}
				var err error
				candidate, err = os.OpenFile(candidate.Name(), mode, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { candidate.Close() })
			case "nonregular source", "nonregular candidate":
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { reader.Close(); writer.Close() })
				if name == "nonregular source" {
					input = reader
				} else {
					candidate = writer
				}
			}
			digest, err := runMediaEditMP4StructuralCopy(ctx, input, candidate, plan, plan.SourceBytes)
			if !errors.Is(err, ErrSubtitleRemovalUnsupported) || digest != "" {
				t.Fatalf("invalid descriptor copy = %q, %v", digest, err)
			}
			mediaEditMP4CopyTestNoPrivatePaths(t, err, originalInput.Name(), originalCandidate.Name())
			if underlying != nil && !errors.Is(err, underlying) {
				t.Fatalf("underlying error was lost: %v", err)
			}
			actualSource, readErr := os.ReadFile(originalInput.Name())
			if readErr != nil || !bytes.Equal(actualSource, data) {
				t.Fatalf("source changed: %v", readErr)
			}
			actualCandidate, readErr := os.ReadFile(originalCandidate.Name())
			if readErr != nil {
				t.Fatal(readErr)
			}
			if name == "nonempty candidate" {
				if string(actualCandidate) != "existing candidate" {
					t.Fatal("existing candidate changed")
				}
			} else if len(actualCandidate) != 0 {
				t.Fatal("rejected candidate was written")
			}
			if name != "closed source" {
				mediaEditMP4CopyTestOffset(t, originalInput, 11)
			}
			if name != "closed candidate" {
				mediaEditMP4CopyTestOffset(t, originalCandidate, 17)
			}
			if name == "same file" || name == "read-only candidate" || name == "write-only candidate" {
				mediaEditMP4CopyTestOffset(t, candidate, 0)
			}
		})
	}
}

func TestMediaEditMP4StructuralCopyIOErrorPreservesCauseWithoutPaths(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "private-source-name.mp4")
	candidatePath := filepath.Join(dir, "private-candidate-name.mp4")
	cause := errors.New("underlying media I/O failure")
	nested := &os.PathError{Op: "read", Path: sourcePath,
		Err: &os.PathError{Op: "write", Path: candidatePath, Err: cause}}
	err := mediaEditMP4CopyIOError("copy read", nested)
	if !errors.Is(err, ErrSubtitleRemovalUnsupported) || !errors.Is(err, cause) {
		t.Fatalf("I/O error classification or underlying cause was lost: %v", err)
	}
	mediaEditMP4CopyTestNoPrivatePaths(t, err, sourcePath, candidatePath)
}

func TestMediaEditMP4StructuralCopyHonorsCancellation(t *testing.T) {
	for _, duringCopy := range []bool{false, true} {
		name := "before copy"
		if duringCopy {
			name = "between chunks"
		}
		t.Run(name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData((3<<17)+29, (4<<17)+73, 41, false)
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !duringCopy {
				cancel()
			}
			observed := &mediaEditMP4CopyTestContext{Context: ctx, beforeErr: func() {
				if duringCopy && mediaEditMP4CopyTestSize(t, candidate) >= 128<<10 {
					cancel()
				}
			}}
			digest, err := runMediaEditMP4StructuralCopy(observed, input, candidate, plan, plan.SourceBytes)
			if !errors.Is(err, context.Canceled) || digest != "" {
				t.Fatalf("canceled copy = %q, %v", digest, err)
			}
			size := mediaEditMP4CopyTestSize(t, candidate)
			if duringCopy {
				if size != 128<<10 {
					t.Fatalf("copy did not check cancellation after a bounded chunk: %d bytes", size)
				}
			} else if size != 0 {
				t.Fatalf("pre-canceled copy wrote %d bytes", size)
			}
			if !bytes.Equal(mediaEditMP4CopyTestRead(t, input), data) {
				t.Fatal("source bytes changed")
			}
			mediaEditMP4CopyTestOffset(t, input, 11)
			mediaEditMP4CopyTestOffset(t, candidate, 17)
		})
	}
}

func TestMediaEditMP4StructuralCopyRejectsSourceTruncatedDuringCopy(t *testing.T) {
	data, plan := mediaEditMP4CopyTestData(29, (4<<17)+73, 41, false)
	input, candidate := mediaEditMP4CopyTestFiles(t, data)
	truncated := false
	ctx := &mediaEditMP4CopyTestContext{Context: context.Background(), beforeErr: func() {
		if !truncated && mediaEditMP4CopyTestSize(t, candidate) > 0 {
			truncated = true
			if err := input.Truncate((128 << 10) - 1); err != nil {
				t.Fatal(err)
			}
		}
	}}
	digest, err := runMediaEditMP4StructuralCopy(ctx, input, candidate, plan, plan.SourceBytes)
	if !truncated || !errors.Is(err, ErrSubtitleRemovalUnsupported) || !errors.Is(err, io.EOF) || digest != "" {
		t.Fatalf("truncated source copy = %q, %v; truncation triggered = %v", digest, err, truncated)
	}
	if mediaEditMP4CopyTestSize(t, candidate) >= plan.SourceBytes {
		t.Fatal("truncated source was copied as a complete candidate")
	}
	mediaEditMP4CopyTestOffset(t, input, 11)
	mediaEditMP4CopyTestOffset(t, candidate, 17)
}

func TestMediaEditMP4StructuralCopyRejectsCandidateChangesBeforeProof(t *testing.T) {
	for _, name := range []string{"retained prefix", "retained suffix", "replacement kind", "erased body"} {
		t.Run(name, func(t *testing.T) {
			data, plan := mediaEditMP4CopyTestData(29, (2<<17)+73, 41, true)
			input, candidate := mediaEditMP4CopyTestFiles(t, data)
			changed := false
			ctx := &mediaEditMP4CopyTestContext{Context: context.Background(), beforeErr: func() {
				if changed || mediaEditMP4CopyTestSize(t, candidate) != plan.SourceBytes {
					return
				}
				changed = true
				var offset int64
				var value byte
				switch name {
				case "retained prefix":
					offset, value = 3, data[3]^0xff
				case "retained suffix":
					offset, value = plan.SourceBytes-3, data[plan.SourceBytes-3]^0xff
				case "replacement kind":
					offset, value = plan.TrackTypeOffset, 't'
				case "erased body":
					offset, value = plan.TrackBodyOffset+7, 0xff
				}
				if _, err := candidate.WriteAt([]byte{value}, offset); err != nil {
					t.Fatal(err)
				}
			}}
			digest, err := runMediaEditMP4StructuralCopy(ctx, input, candidate, plan, plan.SourceBytes)
			if !changed || !errors.Is(err, ErrSubtitleRemovalUnsupported) || digest != "" {
				t.Fatalf("changed candidate copy = %q, %v; mutation triggered = %v", digest, err, changed)
			}
			if !bytes.Equal(mediaEditMP4CopyTestRead(t, input), data) {
				t.Fatal("source bytes changed")
			}
			mediaEditMP4CopyTestOffset(t, input, 11)
			mediaEditMP4CopyTestOffset(t, candidate, 17)
		})
	}
}

func mediaEditMP4CopyTestData(prefix, body, suffix int, extended bool) ([]byte, mediaEditMP4RemovalPlan) {
	header := 8
	if extended {
		header = 16
	}
	data := make([]byte, prefix+header+body+suffix)
	for index := range data {
		data[index] = byte(index%251 + 1)
	}
	plan := mediaEditMP4RemovalPlan{
		SourceBytes:     int64(len(data)),
		RemovedTrackID:  42,
		TrackTypeOffset: int64(prefix + 4),
		TrackBodyOffset: int64(prefix + header),
		TrackEnd:        int64(prefix + header + body),
	}
	if extended {
		binary.BigEndian.PutUint32(data[prefix:prefix+4], 1)
		binary.BigEndian.PutUint64(data[prefix+8:prefix+16], uint64(header+body))
	} else {
		binary.BigEndian.PutUint32(data[prefix:prefix+4], uint32(header+body))
	}
	copy(data[plan.TrackTypeOffset:plan.TrackTypeOffset+4], "trak")
	return data, plan
}

func mediaEditMP4CopyTestFiles(t *testing.T, data []byte) (*os.File, *os.File) {
	t.Helper()
	dir := t.TempDir()
	input, err := os.CreateTemp(dir, "source-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close() })
	if _, err := input.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(11, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	candidate, err := os.CreateTemp(dir, "candidate-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { candidate.Close() })
	if _, err := candidate.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return input, candidate
}

func mediaEditMP4CopyTestRead(t *testing.T, file *os.File) []byte {
	t.Helper()
	data := make([]byte, mediaEditMP4CopyTestSize(t, file))
	if _, err := file.ReadAt(data, 0); err != nil {
		t.Fatal(err)
	}
	return data
}

func mediaEditMP4CopyTestSize(t *testing.T, file *os.File) int64 {
	t.Helper()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func mediaEditMP4CopyTestOffset(t *testing.T, file *os.File, expected int64) {
	t.Helper()
	actual, err := file.Seek(0, io.SeekCurrent)
	if err != nil || actual != expected {
		t.Fatalf("borrowed descriptor offset = %d, want %d: %v", actual, expected, err)
	}
}

func mediaEditMP4CopyTestNoPrivatePaths(t *testing.T, err error, paths ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error to inspect for private paths")
	}
	for _, path := range paths {
		if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), filepath.Base(path)) {
			t.Fatal("I/O error disclosed a descriptor path or basename")
		}
	}
}

func mediaEditMP4CopyTestUnwritten(t *testing.T, input, candidate *os.File, data []byte) {
	t.Helper()
	if !bytes.Equal(mediaEditMP4CopyTestRead(t, input), data) {
		t.Fatal("source bytes changed")
	}
	if mediaEditMP4CopyTestSize(t, candidate) != 0 {
		t.Fatal("rejected candidate was written")
	}
	mediaEditMP4CopyTestOffset(t, input, 11)
	mediaEditMP4CopyTestOffset(t, candidate, 17)
}

func mediaEditMP4CopyTestDigest(data []byte, plan mediaEditMP4RemovalPlan) string {
	framed := bytes.NewBufferString("goby-media-edit-mp4-structural-copy-v1\x00")
	appendUint64 := func(value uint64) {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], value)
		framed.Write(encoded[:])
	}
	appendUint64(uint64(plan.SourceBytes))
	appendUint64(plan.RemovedTrackID)
	appendUint64(3)
	for _, span := range [][2]int64{{0, plan.TrackTypeOffset}, {plan.TrackTypeOffset + 4, plan.TrackBodyOffset}, {plan.TrackEnd, plan.SourceBytes}} {
		appendUint64(uint64(span[0]))
		appendUint64(uint64(span[1]))
		framed.Write(data[span[0]:span[1]])
	}
	digest := sha256.Sum256(framed.Bytes())
	return hex.EncodeToString(digest[:])
}

type mediaEditMP4CopyTestContext struct {
	context.Context
	beforeErr func()
}

func (ctx *mediaEditMP4CopyTestContext) Err() error {
	ctx.beforeErr()
	return ctx.Context.Err()
}
