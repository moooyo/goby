package library

import (
	"io/fs"
	"testing"
)

func TestExtraPathsUseTheFirstAuxiliaryDirectoryBoundary(t *testing.T) {
	cases := []struct {
		path, kind, owner, boundary string
		mode                        fs.FileMode
		reserved                    bool
	}{
		{"Film/featurettes/Alpha.mp4", ExtraKindClip, "Film", "Film/featurettes", 0, true},
		{"Film/DELETED SCENES/Middle.MKV", ExtraKindDeletedScene, "Film", "Film/DELETED SCENES", 0, true},
		{"trailers/Delta.mp4", ExtraKindTrailer, ".", "trailers", 0, true},
		{"Film/featurettes", "", "Film", "Film/featurettes", fs.ModeDir, true},
		{"Film/featurettes/nested/Hidden.mp4", "", "Film", "Film/featurettes", 0, true},
		{"Film/featurettes/trailers/Hidden.mp4", "", "Film", "Film/featurettes", 0, true},
		{"Film/featurettes/backdrops/Hidden.mp4", "", "Film", "Film/featurettes", 0, true},
		{"Film/featurettes/theme.mp3", "", "Film", "Film/featurettes", 0, true},
		{"Film/featurettes/notes.txt", "", "Film", "Film/featurettes", 0, true},
		{"Film/featurettes/.hidden.mp4", "", "Film", "Film/featurettes", 0, true},
		{"Film/backdrops/featurettes/Hidden.mp4", "", "", "", 0, false},
		{"Film/theme-music/trailers/Hidden.mp4", "", "", "", 0, false},
		{"Film/featurettes.mp4", "", "", "", 0, false},
		{"Film/extras/Unproved.mp4", "", "", "", 0, false},
		{"Film/deleted ſcenes/Unproved.mp4", "", "", "", 0, false},
	}
	for _, test := range cases {
		t.Run(test.path, func(t *testing.T) {
			actual, err := classifyExtraPath(test.path, test.mode)
			if err != nil || actual.Reserved != test.reserved || actual.Kind != test.kind || actual.OwnerDirectory != test.owner || actual.Boundary != test.boundary {
				t.Fatalf("extra classification = %+v, %v; want %+v", actual, err, test)
			}
		})
	}
	for _, test := range []struct {
		path string
		mode fs.FileMode
	}{
		{"../featurettes/a.mp4", 0}, {"/featurettes/a.mp4", 0}, {"Film//featurettes/a.mp4", 0},
		{"C:/featurettes/a.mp4", 0}, {"Film/./featurettes/a.mp4", 0}, {`Film\featurettes\a.mp4`, 0},
		{"Film/featurettes/a.mp4", fs.ModeSymlink}, {"Film/featurettes/a.mp4", fs.ModeNamedPipe},
	} {
		if _, err := classifyExtraPath(test.path, test.mode); err == nil {
			t.Errorf("unsafe extra path or entry type was accepted: %+v", test)
		}
	}
}

func TestExtraBoundarySuppressesNestedThemesOnlyInMoviesCollections(t *testing.T) {
	for _, collection := range []string{"movies", "mixed", "music", "tvshows"} {
		state := &scanState{library: Library{CollectionType: collection}}
		actual, err := state.classifyScannedThemePath("Film/featurettes/backdrops/hidden.mp4", 0)
		if err != nil {
			t.Fatal(err)
		}
		if collection == "movies" {
			if actual.Reserved || actual.Kind != themePathKindNone {
				t.Errorf("a nested theme escaped its outer extra reservation: %+v", actual)
			}
		} else if !actual.Reserved || actual.Kind != themePathKindVideo {
			t.Errorf("Movie-only extras changed the %s theme classifier: %+v", collection, actual)
		}
	}
}
