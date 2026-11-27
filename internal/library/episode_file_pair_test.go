package library

import (
	"path/filepath"
	"testing"
)

func TestEpisodeEndNumberUsesOnlyAnAdjacentFilenameRange(t *testing.T) {
	for _, test := range []struct {
		name, kind         string
		first, season, end int
	}{
		{"Show.S01E03E04.mkv", "Episode", 3, 1, 4},
		{"S01E03-E04.mp4", "Episode", 3, 1, 4},
		{"show_s00e00e01.MKV", "Episode", 0, 0, 1},
		{"Season 01/Show.S001E0099-E0100.mp4", "Episode", 99, 1, 100},
		{"Show.S01E03.mkv", "Episode", 3, 1, 0},
		{"Show.S01E03E03.mkv", "Episode", 3, 1, 0},
		{"Show.S01E03-E05.mkv", "Episode", 3, 1, 0},
		{"Show.S01E03-S02E04.mkv", "Episode", 3, 1, 0},
		{"Show.S01E03E04E05.mkv", "Episode", 3, 1, 0},
		{"Show.S01E03E04.1080p.mkv", "Episode", 3, 1, 0},
		{"Show.S01E01.cut.S01E03E04.mkv", "Episode", 3, 1, 0},
		{"Show.S01E03E04.mkv", "Episode", 4, 1, 0},
		{"Show.S01E03E04.mkv", "Episode", 3, 2, 0},
		{"Show.S01E03E04.mkv", "Movie", 3, 1, 0},
		{"Show.S01E03E04.nfo", "Episode", 3, 1, 0},
		{"", "Episode", 3, 1, 0},
	} {
		item := Item{Type: test.kind, Path: filepath.FromSlash(test.name), IndexNumber: test.first, ParentIndexNumber: test.season}
		got, ok := item.EpisodeEndNumber()
		if got != test.end || ok != (test.end != 0) {
			t.Errorf("%+v: end=(%d,%v), want %d", test, got, ok, test.end)
		}
	}
}
