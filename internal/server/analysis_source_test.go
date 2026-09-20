package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestAnalysisSourceDigestIncludesTheTailAndPreservesOffset(t *testing.T) {
	prefix := make([]byte, 700<<10)
	var previous string
	for index, ending := range []byte{'a', 'b'} {
		data := append(append([]byte{}, prefix...), ending)
		name := filepath.Join(t.TempDir(), "source.bin")
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		if _, err := file.Seek(123, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		actual, err := analysisSourceDigest(context.Background(), file, int64(len(data)), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256(data)
		if actual != hex.EncodeToString(want[:]) || index != 0 && actual == previous {
			t.Fatal("a complete source hash was replaced by a shared-prefix identity")
		}
		position, err := file.Seek(0, io.SeekCurrent)
		if err != nil || position != 123 {
			t.Fatal("analysis hashing changed the borrowed descriptor offset")
		}
		previous = actual
	}
}

func TestAnalysisSourceDigestRejectsCancellationBudgetAndChangedSize(t *testing.T) {
	name := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(name, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		ctx         context.Context
		size, limit int64
		want        error
	}{
		{canceled, 6, 10, context.Canceled},
		{context.Background(), 6, 5, media.ErrAnalysisBudget},
		{context.Background(), 5, 10, library.ErrAnalysisSourceChanged},
		{context.Background(), 7, 10, library.ErrAnalysisSourceChanged},
	} {
		if digest, err := analysisSourceDigest(test.ctx, file, test.size, test.limit); digest != "" || !errors.Is(err, test.want) {
			t.Fatalf("invalid source produced content identity %q or wrong failure: %v", digest, err)
		}
	}
}

func TestAnalysisStreamSelectionUsesOriginalIndexesAndDefaultPreference(t *testing.T) {
	streams := []media.Stream{
		{Index: 7, CodecType: "audio"}, {Index: 3, CodecType: "audio", IsDefault: true},
		{Index: 1, CodecType: "audio", IsExternal: true, IsDefault: true},
		{Index: 100000, CodecType: "subtitle", IsExternal: true},
		{Index: 0, CodecType: "video", IsAttachedPicture: true}, {Index: 4, CodecType: "video"},
		{Index: 9, CodecType: "video", IsDefault: true},
	}
	for _, reverse := range []bool{false, true} {
		if reverse {
			for left, right := 0, len(streams)-1; left < right; left, right = left+1, right-1 {
				streams[left], streams[right] = streams[right], streams[left]
			}
		}
		for kind, want := range map[string]int{"audio": 3, "video": 9} {
			if actual, ok := analysisStream(media.Info{Streams: streams}, kind); !ok || actual != want {
				t.Fatalf("%s selected a list position or unsupported stream: %d", kind, actual)
			}
		}
	}
	streams = append(streams, media.Stream{Index: 3, CodecType: "audio"})
	if _, ok := analysisStream(media.Info{Streams: streams}, "audio"); ok {
		t.Fatal("ambiguous original stream indexes became extraction authority")
	}
}
