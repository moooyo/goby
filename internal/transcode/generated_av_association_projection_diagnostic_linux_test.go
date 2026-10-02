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

func TestGeneratedAVAssociationProjectionCaptureKeepsExactBytesAndBounds(t *testing.T) {
	directory := t.TempDir()
	file, err := os.OpenFile(filepath.Join(directory, "raw.json"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data := []byte(`{"packets_and_frames":[{"type":"frame","nb_samples":1024}],"streams":[]}`)
	if err := generatedAVWriteAssociationCapture(context.Background(), file, data, nil); err != nil {
		t.Fatal(err)
	}
	stored := make([]byte, len(data))
	if _, err := file.ReadAt(stored, 0); err != nil || !bytes.Equal(stored, data) || sha256.Sum256(stored) != sha256.Sum256(data) {
		t.Fatal("capture changed joined raw output bytes")
	}
	if err := generatedAVWriteAssociationCapture(context.Background(), file, data, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("capture appended to a used target")
	}
	bounded, err := os.OpenFile(filepath.Join(directory, "over.json"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	if err := generatedAVWriteAssociationCapture(context.Background(), bounded, bytes.Repeat([]byte{'x'}, (4<<20)+1), nil); !errors.Is(err, ErrTimelineLimit) {
		t.Fatal("capture enlarged the fixed JSON cap")
	}
	info, err := bounded.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("over-budget capture wrote a prefix")
	}
}

func TestGeneratedAVAssociationProjectionOwnsCaptureBeforePreflightAndNeverStartsOnFailure(t *testing.T) {
	directory := t.TempDir()
	for _, cancelled := range []bool{false, true} {
		file, err := os.CreateTemp(directory, "capture-")
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Context(nil)
		if cancelled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		calls := 0
		_, err = MeasureGeneratedAVAssociationProjection(ctx, nil, nil, GeneratedAVAssociationProjectionOptions{
			Capture: file, ExpectedSourceSHA256: [32]byte{1}, AcquireProbe: func(context.Context) (func(), error) { calls++; return func() {}, nil },
		})
		if err == nil || calls != 0 {
			t.Fatal("invalid preflight reached admission or process start")
		}
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("preflight failure retained transferred capture ownership")
		}
	}
}

func TestGeneratedAVAssociationProjectionSourceHashPreservesBorrowedOffsetAndRejectsMismatch(t *testing.T) {
	directory := t.TempDir()
	source, err := os.OpenFile(filepath.Join(directory, "source.mp4"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	data := bytes.Repeat([]byte("encoded-source"), 10000)
	if _, err := source.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	hash, err := generatedAVAssociationSourceHash(context.Background(), source, int64(len(data)))
	offset, offsetErr := source.Seek(0, io.SeekCurrent)
	if err != nil || offsetErr != nil || offset != 7 || hash != sha256.Sum256(data) {
		t.Fatal("whole encoded-source hash changed borrowed offset or byte extent")
	}
	segment, err := os.OpenFile(filepath.Join(directory, "segment.ts"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer segment.Close()
	if _, err := segment.Write([]byte("physical-segment")); err != nil {
		t.Fatal(err)
	}
	_, identity, err := generatedAVAssociationInputInfo(source)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := os.OpenFile(filepath.Join(directory, "mismatch.json"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := MeasureGeneratedAVAssociationProjection(context.Background(), source, segment, GeneratedAVAssociationProjectionOptions{
		Capture: capture, SourceCertificate: GeneratedAVSourceCertificate{SourceIdentity: identity}, ExpectedSourceSHA256: [32]byte{1},
		AcquireProbe: func(context.Context) (func(), error) { calls++; return func() {}, nil },
	})
	if !errors.Is(err, ErrInvalidInput) || calls != 0 || result.Complete || result.EnvelopeParsed || result.Qualified {
		t.Fatal("wrong whole encoded-source hash reached admission or obtained evidence")
	}
	if _, err := capture.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("source mismatch leaked its capture")
	}
	offset, err = source.Seek(0, io.SeekCurrent)
	if err != nil || offset != 7 {
		t.Fatal("failed measurement changed borrowed source offset")
	}
}
