package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedAVPCMCaptureBorrowedDescriptorPrecedesInvalidArtifactTransfer(t *testing.T) {
	for _, role := range []GeneratedAVPCMCaptureRole{GeneratedAVPCMCaptureSource, GeneratedAVPCMCaptureCut, GeneratedAVPCMCaptureGroup} {
		for _, contextKind := range []string{"nil", "canceled", "live"} {
			for _, aliasKind := range []string{"same_pointer", "same_descriptor"} {
				for _, target := range []string{"source", "part"} {
					t.Run(string(role)+"_"+contextKind+"_"+aliasKind+"_"+target, func(t *testing.T) {
						directory := t.TempDir()
						create := func(name string, offset int64) *os.File {
							t.Helper()
							file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
							if err != nil {
								t.Fatal(err)
							}
							// Close only the original owner once. The intentional copied
							// os.File alias below shares its internal file object.
							t.Cleanup(func() { _ = file.Close() })
							if _, err := file.Write([]byte("opaque-borrowed-descriptor")); err != nil {
								t.Fatal(err)
							}
							if _, err := file.Seek(offset, io.SeekStart); err != nil {
								t.Fatal(err)
							}
							return file
						}
						source, parts := create("source", 3), []*os.File{}
						offsets := []int64{}
						if role == GeneratedAVPCMCaptureSource {
							parts, offsets = []*os.File{source}, []int64{3}
						} else {
							parts, offsets = []*os.File{create("part0", 5)}, []int64{5}
							if role == GeneratedAVPCMCaptureGroup {
								parts, offsets = append(parts, create("part1", 7)), append(offsets, 7)
							}
						}
						capture := source
						if target == "part" {
							capture = parts[len(parts)-1]
						}
						if aliasKind == "same_descriptor" {
							// This is deliberately invalid capability aliasing, not
							// os.NewFile with an independent finalizer/duplicate Close.
							captureCopy := *capture
							capture = &captureCopy
						}
						var ctx context.Context
						if contextKind != "nil" {
							ctx = context.Background()
							if contextKind == "canceled" {
								var cancel context.CancelFunc
								ctx, cancel = context.WithCancel(ctx)
								cancel()
							}
						}
						acquires, releases := 0, 0
						result, err := MeasureGeneratedAVPCMCaptureDiagnostic(ctx, source, parts, GeneratedAVPCMCaptureOptions{
							Role: role, Capture: capture, AcquireProbe: func(context.Context) (func(), error) { acquires++; return func() { releases++ }, nil },
						})
						if !errors.Is(err, ErrInvalidInput) || acquires != 0 || releases != 0 || result.Complete || result.PCM.Complete || result.CaptureWritten || result.Qualified {
							t.Fatal("invalid descriptor alias acquired ownership, admission or evidence")
						}
						for index, file := range parts {
							if _, err := file.Stat(); err != nil {
								t.Fatal("invalid artifact transfer closed a borrowed part")
							}
							if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != offsets[index] {
								t.Fatal("invalid artifact transfer moved a borrowed part")
							}
						}
						if _, err := source.Stat(); err != nil {
							t.Fatal("invalid artifact transfer closed the borrowed source")
						}
						if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 3 {
							t.Fatal("invalid artifact transfer moved the borrowed source")
						}
					})
				}
			}
		}
	}
}

func TestGeneratedAVPCMCaptureIndependentSameInodeArtifactIsStillConsumed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("opaque-source"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	artifact, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if generatedAVPCMCaptureAliasesBorrowed(source, []*os.File{source}, artifact) {
		t.Fatal("independent Open was mislabeled as the same descriptor")
	}
	if _, err := MeasureGeneratedAVPCMCaptureDiagnostic(nil, source, []*os.File{source}, GeneratedAVPCMCaptureOptions{Capture: artifact}); err == nil {
		t.Fatal("invalid independent artifact was accepted")
	}
	if err := artifact.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("a distinct supplied artifact lost failure-path Close ownership")
	}
	if _, err := source.Stat(); err != nil {
		t.Fatal("closing the independent artifact closed a borrowed source")
	}
}
