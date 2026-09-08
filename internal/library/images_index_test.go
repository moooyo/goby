package library

import (
	"fmt"
	"reflect"
	"testing"
)

func TestImageDirectoryIndexMatchesCompleteCandidateSelection(t *testing.T) {
	names := []string{"poster.jpg", "Poster.JPG", "folder.png", "cover.webp", "show.gif", "artist-poster.jpg",
		"Film-poster.png", "Film.jpg", "Film-thumb.png", "Film-clearlogo.png", "Film-clearart.svg",
		"thumb.jpg", "landscape.png", "clearlogo.png", "clearart.png", "banner.gif", "Other-poster.jpg"}
	for _, prefix := range []string{"", "Film-", "Other-", "Film.Part.Two-"} {
		for _, family := range []string{"backdrop", "fanart", "background", "art"} {
			for number := 45; number >= 0; number-- {
				names = append(names, fmt.Sprintf("%s%s%d.jpg", prefix, family, number))
				if number%3 == 0 {
					names = append(names, fmt.Sprintf("%s%s%d.png", prefix, family, number))
				}
			}
		}
	}
	for index := 0; index < 10_000; index++ {
		names = append(names, fmt.Sprintf("Unrelated Movie %d.mkv", index))
	}
	index := newImageDirectoryIndex(names, nil)
	for _, test := range []struct {
		typeName, relative string
		folder             bool
	}{
		{"Movie", "Film.mkv", false}, {"Episode", "Film.mkv", false}, {"Audio", "Film.mp3", false},
		{"Movie", "Film.Part.Two.mkv", false}, {"Movie", "Other.mkv", false},
		{"Movie", "No Artwork.mkv", false}, {"Folder", ".", true}, {"Series", "Shows", true},
		{"Season", "Shows/Season 01", true}, {"MusicAlbum", "Albums", true}, {"MusicArtist", "Artist", true},
	} {
		t.Run(test.typeName+" "+test.relative, func(t *testing.T) {
			want := imageCandidateNames(names, test.typeName, test.relative, test.folder)
			got := index.candidateNames(test.typeName, test.relative, test.folder)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("indexed candidate selection changed priority: got=%+v want=%+v", got, want)
			}
		})
	}
	for prefix, group := range index.backdrops {
		if len(group) > 32 {
			t.Fatalf("backdrop group %q was not bounded during indexing: %d", prefix, len(group))
		}
	}
}

func TestImageDirectoryIndexDoesNotRetainCallerFilenameSlice(t *testing.T) {
	names := []string{"Film-poster.jpg", "backdrop1.png"}
	index := newImageDirectoryIndex(names, nil)
	names[0] = "malicious-replacement.svg"
	names[1] = "unrelated.txt"
	got := index.candidateNames("Movie", "Film.mkv", false)
	assertImageCandidates(t, got, map[string][]string{"Primary": {"Film-poster.jpg"}, "Backdrop": {"backdrop1.png"}})
}
