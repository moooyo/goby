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

func TestGeneratedAVPCMOverlapDifferenceVerifiesWholeCapturesAndBorrowedOffsets(t *testing.T) {
	directory := t.TempDir()
	create := func(name string, data []byte) *os.File {
		t.Helper()
		path := filepath.Join(directory, name)
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
	referenceData := []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 7, 0, 8, 0}
	queryData := []byte{10, 0, 20, 0, 3, 0, 4, 0, 5, 0, 6, 0, 30, 0, 40, 0}
	reference, query := create("reference.pcm", referenceData), create("query.pcm", queryData)
	if _, err := reference.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := query.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	options := generatedAVPCMOverlapTestOptions(referenceData, queryData)
	options.ReferenceFirstSample, options.QueryFirstSample, options.ComparedSamples = 1, 1, 2
	result, err := CompareGeneratedAVPCMOverlapDifferenceDiagnostic(context.Background(), reference, query, options)
	if err != nil || !result.Complete || !result.CapturedBytesVerified || !result.ComparedParsed || !result.BytesEqual || result.WholeQueryCompared ||
		result.UncomparedQueryPrefixSamples != 1 || result.UncomparedQuerySuffixSamples != 1 || result.NativeClockKnown || result.ContentBound || result.DecodedOriginComplete || result.Qualified {
		t.Fatal("captured hypothesis changed complete hashes, explicit scope or native/content claims")
	}
	for index, file := range []*os.File{reference, query} {
		if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != []int64{7, 3}[index] {
			t.Fatal("comparison changed or closed a borrowed capture descriptor")
		}
	}
	changedWholeCapture := options
	changedWholeCapture.QuerySHA256 = sha256.Sum256(queryData[4:12])
	if _, err := CompareGeneratedAVPCMOverlapDifferenceDiagnostic(context.Background(), reference, query, changedWholeCapture); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("hash of only the matching window replaced complete capture identity")
	}
	if _, err := CompareGeneratedAVPCMOverlapDifferenceDiagnostic(context.Background(), reference, reference, options); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("same-inode reference/query manufactured an independent byte comparison")
	}
	if err := os.Chmod(filepath.Join(directory, "query.pcm"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareGeneratedAVPCMOverlapDifferenceDiagnostic(context.Background(), reference, query, options); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nonprivate PCM capture accepted")
	}
	if err := os.Chmod(filepath.Join(directory, "query.pcm"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(directory, "query.pcm"), filepath.Join(directory, "query-link.pcm")); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareGeneratedAVPCMOverlapDifferenceDiagnostic(context.Background(), reference, query, options); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("multiply-linked PCM capture accepted")
	}
}
