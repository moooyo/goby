package library

import (
	"fmt"
	"os"
	"testing"
)

var (
	scanDirectoryBenchmarkImages    *imageDirectoryIndex
	scanDirectoryBenchmarkSubtitles *subtitleDirectoryIndex
)

func scanDirectoryBenchmarkNames(total, media, sidecarOwners int, images bool) []string {
	names := make([]string, 0, total)
	for index := 0; index < media; index++ {
		names = append(names, fmt.Sprintf("Movie%04d.mkv", index))
	}
	if images && sidecarOwners != 0 {
		names = append(names, "poster.png", "backdrop.png", "backdrop2.png", "backdrop10.png")
	}
	for index := 0; index < sidecarOwners; index++ {
		if images {
			names = append(names, fmt.Sprintf("Movie%04d-poster.png", index), fmt.Sprintf("Movie%04d-backdrop.png", index))
		} else {
			names = append(names, fmt.Sprintf("Movie%04d.en.srt", index), fmt.Sprintf("Movie%04d.fr.forced.ass", index))
		}
	}
	for len(names) < total {
		names = append(names, fmt.Sprintf("unrelated-note-%04d.txt", len(names)))
	}
	return names
}

// Each iteration builds one directory index. Input creation and retaining the
// final result are outside b.Loop's timer; no filesystem or SQL work is measured.
func BenchmarkScanDirectoryIndexes(b *testing.B) {
	for _, workload := range []struct {
		name                          string
		entries, media, sidecarOwners int
	}{
		{"small", 32, 8, 4},
		{"4096_no_sidecars", 4096, 128, 0},
		{"4096_sparse_sidecars", 4096, 128, 8},
	} {
		b.Run("images/"+workload.name, func(b *testing.B) {
			names := scanDirectoryBenchmarkNames(workload.entries, workload.media, workload.sidecarOwners, true)
			var result *imageDirectoryIndex
			b.ReportAllocs()
			for b.Loop() {
				result = newImageDirectoryIndex(names, nil)
			}
			scanDirectoryBenchmarkImages = result
			b.ReportMetric(float64(len(names)), "entries/op")
			b.ReportMetric(float64(workload.media), "media_stems/op")
		})
		b.Run("subtitles/"+workload.name, func(b *testing.B) {
			names := scanDirectoryBenchmarkNames(workload.entries, workload.media, workload.sidecarOwners, false)
			entries := make([]os.DirEntry, len(names))
			for index, name := range names {
				entries[index] = subtitleDirectoryTestEntry{name: name}
			}
			var result *subtitleDirectoryIndex
			b.ReportAllocs()
			for b.Loop() {
				result = newSubtitleDirectoryIndex(entries, "movies", nil)
			}
			scanDirectoryBenchmarkSubtitles = result
			b.ReportMetric(float64(len(entries)), "entries/op")
			b.ReportMetric(float64(workload.media), "media_stems/op")
		})
	}
}
