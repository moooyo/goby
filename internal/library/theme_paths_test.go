package library

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
)

func TestClassifyThemePathDirectCandidatesAndDirectoryOwners(t *testing.T) {
	cases := []struct {
		name     string
		relative string
		kind     themePathKind
		owner    string
		layout   themePathLayout
	}{
		{"RootThemeFile", "theme.mp3", themePathKindSong, ".", themePathLayoutFile},
		{"MovieThemeFile", "Movies/Seed/theme.flac", themePathKindSong, "Movies/Seed", themePathLayoutFile},
		{"ThemeFileCase", "Movies/Seed/ThEmE.MP3", themePathKindSong, "Movies/Seed", themePathLayoutFile},
		{"RootMusicChild", "theme-music/ambient.mp3", themePathKindSong, ".", themePathLayoutMusic},
		{"AlbumMusicChild", "Music/Album/theme-music/ambient.flac", themePathKindSong, "Music/Album", themePathLayoutMusic},
		{"MusicLayoutCase", "Series/Season 01/THEME-MUSIC/Opening.WAV", themePathKindSong, "Series/Season 01", themePathLayoutMusic},
		{"RootVideoChild", "backdrops/scene.mp4", themePathKindVideo, ".", themePathLayoutBackdrops},
		{"VideoLayoutCase", "Movies/Seed/BaCkDrOpS/Scene.WEBM", themePathKindVideo, "Movies/Seed", themePathLayoutBackdrops},
		{"UnicodeOwnerPreserved", "Movies/Caf\u00e9/theme-music/song.ogg", themePathKindSong, "Movies/Caf\u00e9", themePathLayoutMusic},
		{"ReservedMusicOwnsThemeNamedChild", "Movie/theme-music/theme.mp3", themePathKindSong, "Movie", themePathLayoutMusic},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual, err := classifyThemePath(test.relative, 0644)
			want := themePathClassification{Reserved: true, Kind: test.kind, OwnerDirectory: test.owner, Layout: test.layout}
			if err != nil || actual != want {
				t.Fatalf("classification = %+v, error = %v; want %+v", actual, err, want)
			}
		})
	}
}

func TestClassifyThemePathUsesSupportedAudioExtensions(t *testing.T) {
	for _, extension := range []string{"mp3", "flac", "m4a", "aac", "ogg", "opus", "wav", "wma", "aiff", "aif", "alac", "ape", "mka"} {
		t.Run(extension, func(t *testing.T) {
			for _, relative := range []string{"theme." + extension, "Owner/THEME." + strings.ToUpper(extension), "Owner/theme-music/track." + extension} {
				actual, err := classifyThemePath(relative, 0)
				if err != nil || !actual.Reserved || actual.Kind != themePathKindSong {
					t.Errorf("supported audio candidate %q = %+v, error = %v", relative, actual, err)
				}
			}
		})
	}
}

func TestClassifyThemePathReservesUnknownAndNestedContentsWithoutCandidates(t *testing.T) {
	cases := []struct {
		name     string
		relative string
		mode     fs.FileMode
		owner    string
		layout   themePathLayout
	}{
		{"MusicDirectory", "theme-music", fs.ModeDir, ".", themePathLayoutMusic},
		{"VideoDirectory", "Movie/BACKDROPS", fs.ModeDir, "Movie", themePathLayoutBackdrops},
		{"UnknownMusicFile", "Movie/theme-music/notes.txt", 0, "Movie", themePathLayoutMusic},
		{"UnknownVideoFile", "Movie/backdrops/notes.txt", 0, "Movie", themePathLayoutBackdrops},
		{"WrongMusicMediaKind", "Movie/theme-music/clip.mp4", 0, "Movie", themePathLayoutMusic},
		{"WrongVideoMediaKind", "Movie/backdrops/song.mp3", 0, "Movie", themePathLayoutBackdrops},
		{"NoInnerThemePromotion", "Movie/backdrops/theme.mp3", 0, "Movie", themePathLayoutBackdrops},
		{"NestedDirectory", "Movie/theme-music/extras", fs.ModeDir, "Movie", themePathLayoutMusic},
		{"AudioNamedDirectory", "Movie/theme-music/extras.mp3", fs.ModeDir, "Movie", themePathLayoutMusic},
		{"NestedAudio", "Movie/theme-music/extras/song.mp3", 0, "Movie", themePathLayoutMusic},
		{"NestedThemeName", "Movie/theme-music/extras/theme.mp3", 0, "Movie", themePathLayoutMusic},
		{"NestedVideo", "Movie/backdrops/extras/clip.mp4", 0, "Movie", themePathLayoutBackdrops},
		{"NestedVideoLayout", "Movie/theme-music/backdrops/clip.mp4", 0, "Movie", themePathLayoutMusic},
		{"NestedMusicLayout", "Movie/backdrops/theme-music/song.mp3", 0, "Movie", themePathLayoutBackdrops},
		{"HiddenMusic", "Movie/theme-music/.ambient.mp3", 0, "Movie", themePathLayoutMusic},
		{"TemporaryMusic", "Movie/theme-music/~ambient.mp3", 0, "Movie", themePathLayoutMusic},
		{"IncompleteVideo", "Movie/backdrops/clip.mp4.partial", 0, "Movie", themePathLayoutBackdrops},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual, err := classifyThemePath(test.relative, test.mode)
			want := themePathClassification{Reserved: true, Kind: themePathKindNone, OwnerDirectory: test.owner, Layout: test.layout}
			if err != nil || actual != want {
				t.Fatalf("reserved classification = %+v, error = %v; want %+v", actual, err, want)
			}
		})
	}
}

func TestClassifyThemePathDoesNotReserveOrdinarySimilarNames(t *testing.T) {
	cases := []struct {
		relative string
		mode     fs.FileMode
	}{
		{"Movie/theme.mp4", 0},
		{"Movie/theme.json", 0},
		{"Movie/theme", 0},
		{"Movie/mytheme.mp3", 0},
		{"Movie/theme-song.mp3", 0},
		{"Movie/theme.en.mp3", 0},
		{"Movie/theme-music.mp3", 0},
		{"Movie/backdrops.flac", 0},
		{"Movie/theme-music", 0},
		{"Movie/backdrops", 0},
		{"Movie/theme.mp3", fs.ModeDir},
		{"Movie/theme.mp3/ordinary.mp3", 0},
		{"Movie/theme-music-old/song.mp3", 0},
		{"Movie/mybackdrops/clip.mp4", 0},
		{"Movie/theme-music /song.mp3", 0},
		{"Movie/them\u00e9.mp3", 0},
		{"Movie/theme-mu\u017fic/song.mp3", 0},
		{"Movie/bac\u212adrops/clip.mp4", 0},
	}
	for _, test := range cases {
		actual, err := classifyThemePath(test.relative, test.mode)
		want := themePathClassification{Kind: themePathKindNone, Layout: themePathLayoutNone}
		if err != nil || actual != want {
			t.Errorf("ordinary path %q = %+v, error = %v; want %+v", test.relative, actual, err, want)
		}
	}
}

func TestClassifyThemePathRejectsUnsafePathsAndObservedNonregularTypes(t *testing.T) {
	for _, relative := range []string{"", " ", ".", "..", "../theme.mp3", "Movie/../theme.mp3", "./theme.mp3",
		"Movie/./theme.mp3", "Movie//theme.mp3", "Movie/theme-music/", "/theme.mp3", "//server/theme.mp3",
		"C:/Movie/theme.mp3", "C:theme.mp3", `Movie\theme.mp3`, `C:\Movie\theme.mp3`, "Movie/theme\x00.mp3"} {
		for _, mode := range []fs.FileMode{0, fs.ModeDir} {
			actual, err := classifyThemePath(relative, mode)
			if !errors.Is(err, ErrInvalidInput) || actual.Reserved || actual.Kind != themePathKindNone {
				t.Errorf("unsafe relative path %q mode %v = %+v, error = %v", relative, mode, actual, err)
			}
		}
	}
	for _, mode := range []fs.FileMode{fs.ModeSymlink, fs.ModeSymlink | fs.ModeDir, fs.ModeNamedPipe,
		fs.ModeSocket, fs.ModeDevice, fs.ModeDevice | fs.ModeCharDevice, fs.ModeIrregular} {
		for _, relative := range []string{"theme.mp3", "Movie/theme-music", "Movie/theme-music/song.mp3", "Movie/ordinary.mp3"} {
			actual, err := classifyThemePath(relative, mode)
			if !errors.Is(err, ErrInvalidInput) || actual.Reserved || actual.Kind != themePathKindNone {
				t.Errorf("nonregular entry %q mode %v = %+v, error = %v", relative, mode, actual, err)
			}
		}
	}
}

func TestClassifyThemePathKeepsReservedAudioOutOfAlbumEvidence(t *testing.T) {
	// Album detection consumes the entries left by root-relative theme
	// classification, without applying a second basename-only path policy.
	cases := []struct {
		name  string
		paths []string
		want  bool
	}{
		{"ThemeFileOnly", []string{"Movie/theme.mp3"}, false},
		{"ReservedSubtreeAudio", []string{"Movie/theme-music/song.mp3", "Movie/theme-music/nested/song.flac", "Movie/backdrops/audio.mp3"}, false},
		{"OrdinarySameNamePrefixes", []string{"Album/theme-music.mp3", "Album/backdrops.flac"}, true},
		{"OrdinaryTrackBesideTheme", []string{"Album/theme.mp3", "Album/01.flac"}, true},
		{"OrdinaryDriveLikeBasename", []string{"Album/C:Track.mp3"}, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var ordinary []os.DirEntry
			for _, relative := range test.paths {
				classification, err := classifyThemePath(relative, 0)
				if err != nil {
					t.Fatalf("classify album evidence: %v", err)
				}
				if !classification.Reserved {
					ordinary = append(ordinary, themePathTestEntry{name: path.Base(relative)})
				}
			}
			if actual := containsAudio(ordinary); actual != test.want {
				t.Errorf("ordinary album audio evidence = %v; want %v", actual, test.want)
			}
		})
	}
}

func TestClassifyThemePathDoesNotChooseBetweenIndependentLayoutsOrMovieOwners(t *testing.T) {
	first, firstErr := classifyThemePath("Movies/Shared/theme.mp3", 0)
	second, secondErr := classifyThemePath("Movies/Shared/theme-music/alternate.mp3", 0)
	if firstErr != nil || secondErr != nil || first.Kind != themePathKindSong || second.Kind != themePathKindSong ||
		first.OwnerDirectory != "Movies/Shared" || second.OwnerDirectory != first.OwnerDirectory || first.Layout == second.Layout {
		t.Fatalf("independent layout candidates = %+v / %+v; errors = %v / %v", first, second, firstErr, secondErr)
	}
	// The identical directory output intentionally supplies no catalog item
	// identity, even if multiple primary movies later exist in that directory.
}

type themePathTestEntry struct {
	name string
}

func (entry themePathTestEntry) Name() string               { return entry.name }
func (entry themePathTestEntry) IsDir() bool                { return false }
func (entry themePathTestEntry) Type() fs.FileMode          { return 0 }
func (entry themePathTestEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrInvalid }
