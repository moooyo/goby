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

func TestGeneratedAVPCMContentCaptureHashAndBorrowedOffsets(t *testing.T) {
	options := generatedAVPCMContentTestOptions()
	options.ReferenceFirstCandidateSample, options.QueryFirstSample = 0, 0
	referenceData := generatedAVPCMContentTestNoise(options.CandidateCount-1+options.WindowSamples, 0x198ed)
	queryData := append([]byte(nil), referenceData[211*4:(211+options.WindowSamples)*4]...)
	options.ReferenceSHA256, options.QuerySHA256 = sha256.Sum256(referenceData), sha256.Sum256(queryData)
	parent := t.TempDir()
	create := func(name string, data []byte) *os.File {
		t.Helper()
		path := filepath.Join(parent, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		return file
	}
	reference, query := create("reference.pcm", referenceData), create("query.pcm", queryData)
	if _, err := reference.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	result, err := CompareGeneratedAVPCMContentDiagnostic(context.Background(), reference, query, options)
	if err != nil || !result.Complete || !result.CapturedBytesVerified || result.Status != GeneratedAVAssociationUnique || result.ReferenceSampleHypothesis != 211 ||
		result.Qualified || result.NativeClockKnown || result.DecodedOriginComplete || result.ContentBound {
		t.Fatalf("capture-backed candidate differs: result=%+v error=%v", result, err)
	}
	if offset, err := reference.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatal("comparison changed borrowed reference offset")
	}
	if _, err := reference.Stat(); err != nil {
		t.Fatal("comparison closed borrowed capture")
	}
	badHash := options
	badHash.ReferenceSHA256[0] ^= 1
	if _, err := CompareGeneratedAVPCMContentDiagnostic(context.Background(), reference, query, badHash); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("caller hash mismatch accepted")
	}
	if err := os.Link(filepath.Join(parent, "query.pcm"), filepath.Join(parent, "query-alias.pcm")); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareGeneratedAVPCMContentDiagnostic(context.Background(), reference, query, options); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("multiply linked capture accepted")
	}
}
