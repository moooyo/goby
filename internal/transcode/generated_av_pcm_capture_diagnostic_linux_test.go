//go:build linux

package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedAVPCMCaptureCopyFromFDEnforcesByteLimitAndCancels(t *testing.T) {
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "pcm.raw"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcm, err := newGeneratedAVPCMWriter(2, 8)
	if err != nil {
		t.Fatal(err)
	}
	pcm.cancel = cancel
	writer := &generatedAVPCMCaptureWriter{pcm: pcm, file: file, limit: 8, cancel: cancel}
	data := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	// A real pipe FD exercises io.Copy/os.File.WriteTo optional paths, rather
	// than mirroring the destination's direct Write implementation.
	if count, err := generatedAVDiagnosticCopyFromFD(t, writer, data); err != nil || count != int64(len(data)) {
		t.Fatal("within-budget actual FD copy did not finish")
	}
	if _, err := generatedAVDiagnosticCopyFromFD(t, writer, []byte{5, 0, 6, 0}); !errors.Is(err, ErrTimelineLimit) {
		t.Fatal("actual FD copy bypassed the capture limit")
	}
	if ctx.Err() == nil || writer.written != 8 {
		t.Fatal("over-budget stream was not canceled or enlarged the captured prefix")
	}
	stored := make([]byte, 8)
	info, err := file.Stat()
	if err != nil || info.Size() != 8 {
		t.Fatal("actual capture file exceeded its byte cap")
	}
	if _, err := file.ReadAt(stored, 0); err != nil || !bytes.Equal(stored, data) {
		t.Fatal("over-budget stream altered the admitted captured prefix")
	}
	if _, ok := any(writer).(io.ReaderFrom); ok {
		t.Fatal("capture exposes a bypassing ReaderFrom fast path")
	}
	if _, ok := any(writer).(io.WriterTo); ok {
		t.Fatal("capture exposes its private output descriptor")
	}
}

func TestGeneratedAVPCMCaptureSourceAndOrderedPartsFailuresConsumeArtifactAndRelease(t *testing.T) {
	for _, role := range []GeneratedAVPCMCaptureRole{GeneratedAVPCMCaptureSource, GeneratedAVPCMCaptureCut, GeneratedAVPCMCaptureGroup} {
		for _, admissionFailure := range []bool{false, true} {
			t.Run(string(role)+map[bool]string{false: "_start", true: "_admission"}[admissionFailure], func(t *testing.T) {
				directory := t.TempDir()
				open := func(name string, data []byte, offset int64) *os.File {
					t.Helper()
					file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = file.Close() })
					if _, err := file.Write(data); err != nil {
						t.Fatal(err)
					}
					if _, err := file.Seek(offset, io.SeekStart); err != nil {
						t.Fatal(err)
					}
					return file
				}
				sourceBytes := []byte("opaque-encoded-source-not-a-decoder-receipt")
				source := open("source.mp4", sourceBytes, 3)
				_, identity, err := generatedAVAssociationInputInfo(source)
				if err != nil {
					t.Fatal(err)
				}
				files, offsets := []*os.File{source}, []int64{3}
				if role != GeneratedAVPCMCaptureSource {
					files, offsets = []*os.File{open("part-0.ts", []byte("part-zero"), 1)}, []int64{1}
				}
				if role == GeneratedAVPCMCaptureGroup {
					files, offsets = append(files, open("part-1.ts", []byte("part-one"), 2)), append(offsets, 2)
				}
				capture, err := os.OpenFile(filepath.Join(directory, "capture.raw"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				acquires, releases := 0, 0
				admissionErr := errors.New("owned admission error")
				result, err := MeasureGeneratedAVPCMCaptureDiagnostic(context.Background(), source, files, GeneratedAVPCMCaptureOptions{
					Role: role, Channels: 2, FFmpegPath: filepath.Join(directory, "absent-ffmpeg"), Capture: capture,
					ExpectedSourceSHA256: sha256.Sum256(sourceBytes), SourceCertificate: GeneratedAVSourceCertificate{
						SourceIdentity: identity, Audio: GeneratedAVAudioTrackCertificate{SampleRate: 48000, Channels: 2},
					},
					AcquireProbe: func(context.Context) (func(), error) {
						acquires++
						release := func() { releases++ }
						if admissionFailure {
							return release, admissionErr
						}
						return release, nil
					},
				})
				if err == nil || acquires != 1 || releases != 1 || result.Complete || result.PCM.Complete || result.CaptureWritten || result.NativeClockComplete || result.ContentBound || result.DecodedOriginComplete || result.Qualified {
					t.Fatal("pre-start failure leaked a capability or minted PCM evidence")
				}
				if admissionFailure && !errors.Is(err, admissionErr) || !admissionFailure && !errors.Is(err, ErrStart) {
					t.Fatal("pre-start failure lost its original cause")
				}
				if _, err := capture.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("failure did not consume its transferred private capture")
				}
				for index, file := range files {
					offset, err := file.Seek(0, io.SeekCurrent)
					if err != nil || offset != offsets[index] {
						t.Fatal("failure closed or moved a borrowed input")
					}
				}
			})
		}
	}
}

func TestGeneratedAVPCMCaptureRejectsAliasesDuplicatePartsAndWrongEncodedSource(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.mp4")
	if err := os.WriteFile(sourcePath, []byte("encoded-source"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_, identity, err := generatedAVAssociationInputInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wrong_source", "source_as_cut", "duplicate_group", "capture_alias"} {
		t.Run(mode, func(t *testing.T) {
			capture, err := os.OpenFile(filepath.Join(directory, mode+".raw"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			options := GeneratedAVPCMCaptureOptions{Role: GeneratedAVPCMCaptureSource, Channels: 2, Capture: capture, ExpectedSourceSHA256: sha256.Sum256([]byte("encoded-source")),
				SourceCertificate: GeneratedAVSourceCertificate{SourceIdentity: identity, Audio: GeneratedAVAudioTrackCertificate{SampleRate: 48000, Channels: 2}}}
			calls := 0
			options.AcquireProbe = func(context.Context) (func(), error) { calls++; return func() {}, nil }
			files := []*os.File{source}
			switch mode {
			case "wrong_source":
				options.ExpectedSourceSHA256[0] ^= 1
			case "source_as_cut":
				options.Role = GeneratedAVPCMCaptureCut
			case "duplicate_group":
				partPath := filepath.Join(directory, "duplicate-part.ts")
				if err := os.WriteFile(partPath, []byte("distinct-output-part"), 0600); err != nil {
					t.Fatal(err)
				}
				part, err := os.Open(partPath)
				if err != nil {
					t.Fatal(err)
				}
				defer part.Close()
				options.Role, files = GeneratedAVPCMCaptureGroup, []*os.File{part, part}
			case "capture_alias":
				_ = capture.Close()
				options.Capture, err = os.Open(sourcePath)
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := MeasureGeneratedAVPCMCaptureDiagnostic(context.Background(), source, files, options)
			if err == nil || calls != 0 || result.Complete || result.Qualified {
				t.Fatal("unsupported source/input/capture identity reached process admission")
			}
			if _, err := options.Capture.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("invalid preflight retained artifact ownership")
			}
			if _, err := source.Stat(); err != nil {
				t.Fatal("invalid preflight closed its borrowed source")
			}
		})
	}
}
