package library

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestThemeProbeRequiresActualCorrespondingMedia(t *testing.T) {
	cases := []struct {
		name    string
		kind    themePathKind
		streams []media.Stream
		want    bool
	}{
		{"Audio", themePathKindSong, []media.Stream{{CodecType: "audio"}}, true},
		{"AudioWithCover", themePathKindSong, []media.Stream{{CodecType: "audio"}, {CodecType: "video", IsAttachedPicture: true}}, true},
		{"RealVideoIsNotSong", themePathKindSong, []media.Stream{{CodecType: "audio"}, {CodecType: "video"}}, false},
		{"CoverOnlyIsNotSong", themePathKindSong, []media.Stream{{CodecType: "video", IsAttachedPicture: true}}, false},
		{"CoverOnlyIsNotVideo", themePathKindVideo, []media.Stream{{CodecType: "video", IsAttachedPicture: true}}, false},
		{"Video", themePathKindVideo, []media.Stream{{CodecType: "video"}}, true},
		{"VideoWithAudio", themePathKindVideo, []media.Stream{{CodecType: "video"}, {CodecType: "audio"}}, true},
		{"AudioIsNotVideo", themePathKindVideo, []media.Stream{{CodecType: "audio"}}, false},
		{"Case", themePathKindSong, []media.Stream{{CodecType: "AUDIO"}}, true},
		{"Empty", themePathKindSong, nil, false},
		{"NoCandidate", themePathKindNone, []media.Stream{{CodecType: "audio"}}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if actual := themeProbeMatches(test.kind, &media.Info{Streams: test.streams}); actual != test.want {
				t.Errorf("theme stream classification = %v; want %v", actual, test.want)
			}
		})
	}
	if themeProbeMatches(themePathKindSong, nil) || themeProbeMatches(themePathKindVideo, nil) {
		t.Error("a missing probe became a theme resource")
	}
}

func TestThemeOwnerLayoutsDoNotChooseUnprovedSongPrecedence(t *testing.T) {
	songFile := themeCandidate{kind: themePathKindSong, layout: themePathLayoutFile}
	songDirectory := themeCandidate{kind: themePathKindSong, layout: themePathLayoutMusic}
	video := themeCandidate{kind: themePathKindVideo, layout: themePathLayoutBackdrops}
	cases := []struct {
		name       string
		candidates []themeCandidate
		valid      bool
	}{
		{"Empty", nil, true},
		{"IndependentKinds", []themeCandidate{songFile, video}, true},
		{"MultipleDirectorySongs", []themeCandidate{songDirectory, songDirectory, video}, true},
		{"MultipleDirectoryVideos", []themeCandidate{video, video}, true},
		{"CompetingSongLayouts", []themeCandidate{songFile, songDirectory}, false},
		{"CompetingDirectFiles", []themeCandidate{songFile, songFile}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := validateThemeLayouts(test.candidates); (err == nil) != test.valid {
				t.Errorf("theme layout validation = %v; valid = %v", err, test.valid)
			}
		})
	}
}

func TestThemeRetirementAbsenceRequiresAnchoredMissingComponents(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "Film", "theme-music"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "Film", "theme-music", "song.mp3")
	if err := os.WriteFile(file, []byte("present"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if absent, err := themePathAbsent(root, "Film/theme-music/song.mp3"); err != nil || absent {
		t.Fatalf("present theme source counted as absent: %v, %v", absent, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"Film/theme-music/song.mp3", "Missing/theme-music/song.mp3"} {
		if absent, err := themePathAbsent(root, relative); err != nil || !absent {
			t.Errorf("missing component %q was not witnessed: %v, %v", relative, absent, err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "NotDirectory"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"../outside.mp3", "/outside.mp3", "NotDirectory/theme.mp3"} {
		if absent, err := themePathAbsent(root, relative); err == nil || absent {
			t.Errorf("unsafe or unproved path %q authorized retirement: %v, %v", relative, absent, err)
		}
	}
	if runtime.GOOS == "linux" {
		if err := os.Symlink("Film", filepath.Join(directory, "Alias")); err != nil {
			t.Fatal(err)
		}
		if absent, err := themePathAbsent(root, "Alias/theme-music/song.mp3"); err == nil || absent {
			t.Errorf("a symlink ancestor authorized missing-resource retirement: %v, %v", absent, err)
		}
	}
}
