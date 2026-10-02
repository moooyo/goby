//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Both cases terminate before command Start. Real descriptors test ownership,
// admission release and held offsets without substituting a fake decoder receipt.
func TestGeneratedAVPacketFrameMeasurementFailedAdmissionOrStartConsumesCapture(t *testing.T) {
	for _, admissionFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent_executable", true: "admission_failure"}[admissionFailure], func(t *testing.T) {
			directory := t.TempDir()
			source, err := os.OpenFile(filepath.Join(directory, "source.mp4"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			sourceBytes := []byte("opaque-encoded-source-with-no-decoder-proof")
			if _, err := source.Write(sourceBytes); err != nil {
				t.Fatal(err)
			}
			if _, err := source.Seek(3, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			_, identity, err := generatedAVAssociationInputInfo(source)
			if err != nil {
				t.Fatal(err)
			}
			segment, err := os.OpenFile(filepath.Join(directory, "segment.ts"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer segment.Close()
			if _, err := segment.Write(generatedAVTransportTestFixture()); err != nil {
				t.Fatal(err)
			}
			if _, err := segment.Seek(7, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			capture, err := os.OpenFile(filepath.Join(directory, "raw.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			admissionErr := errors.New("owned admission failure")
			acquires, releases := 0, 0
			result, err := MeasureGeneratedAVPacketFrameTransportAssociation(context.Background(), source, segment, GeneratedAVAssociationProjectionOptions{
				FFprobePath: filepath.Join(directory, "absent-ffprobe"), Capture: capture,
				SourceCertificate: GeneratedAVSourceCertificate{SourceIdentity: identity}, ExpectedSourceSHA256: sha256.Sum256(sourceBytes),
				AcquireProbe: func(context.Context) (func(), error) {
					acquires++
					release := func() { releases++ }
					if admissionFailure {
						return release, admissionErr
					}
					return release, nil
				},
			})
			if err == nil || acquires != 1 || releases != 1 || result.Complete || result.Qualified || result.RawProjection.Complete || result.RawProjection.CaptureWritten || result.Projection.measurement != nil || result.Projection.MeasuredInputKnown || result.Association.SameHeldInput || result.Association.PacketUnitBindingComplete {
				t.Fatal("pre-start failure leaked a capability or minted joined evidence")
			}
			if admissionFailure && !errors.Is(err, admissionErr) || !admissionFailure && !errors.Is(err, ErrStart) {
				t.Fatal("failure lost its original cause")
			}
			if _, err := capture.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("pre-start failure did not consume capture")
			}
			for index, file := range []*os.File{source, segment} {
				offset, err := file.Seek(0, io.SeekCurrent)
				if err != nil || offset != []int64{3, 7}[index] {
					t.Fatal("pre-start failure closed or moved a borrowed descriptor")
				}
			}
		})
	}
}
