package library

import (
	"fmt"
	"slices"
	"testing"
)

func TestImageCandidateNamesMoviePrimaryPriority(t *testing.T) {
	names := []string{
		"movie.jpg", "default.jpg", "cover.jpg", "folder.jpg", "poster.jpg",
		"Film-movie.jpg", "Film-default.gif", "Film.jpg", "Film-cover.jpeg", "Film-poster.png",
		"Film.mkv", "Film.nfo", "unrelated.jpg",
	}
	want := []string{
		"Film-poster.png", "Film-cover.jpeg", "Film.jpg", "Film-default.gif", "Film-movie.jpg",
		"poster.jpg", "folder.jpg", "cover.jpg", "default.jpg", "movie.jpg",
	}
	for _, itemType := range []string{"Movie", "Video"} {
		t.Run(itemType, func(t *testing.T) {
			got := imageCandidateNames(names, itemType, "Movies/Film/Film.mkv", false)
			assertImageCandidates(t, got, map[string][]string{"Primary": want})
		})
	}
}

func TestImageCandidateNamesFolderPrimaryPriority(t *testing.T) {
	names := []string{"show.png", "default.jpg", "cover.jpg", "folder.jpg", "poster.jpg", "Unrelated-poster.jpg"}
	for _, test := range []struct {
		itemType string
		want     []string
	}{
		{"Folder", []string{"poster.jpg", "folder.jpg", "cover.jpg", "default.jpg"}},
		{"Series", []string{"poster.jpg", "folder.jpg", "cover.jpg", "default.jpg", "show.png"}},
	} {
		t.Run(test.itemType, func(t *testing.T) {
			got := imageCandidateNames(names, test.itemType, "Shows/Unrelated", true)
			assertImageCandidates(t, got, map[string][]string{"Primary": test.want})
		})
	}
}

func TestImageCandidateNamesMovieSecondaryPriority(t *testing.T) {
	names := []string{
		"landscape.jpg", "thumb.png", "Film-landscape.gif", "Film-thumb.jpg",
		"logo.jpg", "clearlogo.png", "Film-logo.gif", "Film-clearlogo.png",
		"clearart.png", "Film-clearart.gif", "banner.png", "Film-banner.jpg",
	}
	assertImageCandidates(t, imageCandidateNames(names, "Movie", "Movies/Film.mkv", false), map[string][]string{
		"Thumb":  {"Film-thumb.jpg", "Film-landscape.gif", "thumb.png", "landscape.jpg"},
		"Logo":   {"Film-clearlogo.png", "Film-logo.gif", "clearlogo.png", "logo.jpg"},
		"Art":    {"Film-clearart.gif", "clearart.png"},
		"Banner": {"Film-banner.jpg", "banner.png"},
	})
}

func TestImageCandidateNamesEpisodeUsesOwnPrimary(t *testing.T) {
	names := []string{
		"poster.jpg", "folder.jpg", "cover.jpg", "default.jpg", "show.jpg", "movie.jpg",
		"Episode-poster.jpg", "Episode-cover.jpg", "Episode.png", "Episode-thumb.jpg",
	}
	got := imageCandidateNames(names, "Episode", "Shows/Series/Season 01/Episode.mkv", false)
	if !slices.Equal(got["Primary"], []string{"Episode-thumb.jpg", "Episode.png"}) {
		t.Fatalf("episode primary candidates = %v, want only its own thumbnail and basename", got["Primary"])
	}
}

func TestImageCandidateNamesEpisodeDoesNotInheritFolderImages(t *testing.T) {
	names := []string{
		"poster.jpg", "folder.jpg", "cover.jpg", "default.jpg", "show.jpg", "movie.jpg",
		"thumb.jpg", "landscape.jpg", "clearlogo.png", "logo.png", "clearart.png", "banner.jpg",
		"backdrop.jpg", "backdrop1.jpg", "fanart.jpg", "background.jpg", "art.jpg",
	}
	assertImageCandidates(t, imageCandidateNames(names, "Episode", "Shows/Series/Season 01/Episode.mkv", false), nil)
}

func TestImageCandidateNamesEpisodeKeepsPrefixedSecondaryImages(t *testing.T) {
	names := []string{
		"Episode-landscape.jpg", "Episode-clearlogo.png", "Episode-logo.jpg",
		"Episode-clearart.png", "Episode-banner.jpg", "Episode-backdrop.jpg",
	}
	assertImageCandidates(t, imageCandidateNames(names, "Episode", "Season 01/Episode.mkv", false), map[string][]string{
		"Thumb":    {"Episode-landscape.jpg"},
		"Logo":     {"Episode-clearlogo.png", "Episode-logo.jpg"},
		"Art":      {"Episode-clearart.png"},
		"Banner":   {"Episode-banner.jpg"},
		"Backdrop": {"Episode-backdrop.jpg"},
	})
}

func TestImageCandidateNamesPrioritizesSupportedExtensions(t *testing.T) {
	names := []string{"poster.webp", "poster.svg", "poster.gif", "poster.png", "poster.jpeg", "poster.jpg"}
	got := imageCandidateNames(names, "Folder", "Movies", true)["Primary"]
	if len(got) != 6 || !slices.Equal(got[:4], []string{"poster.jpg", "poster.jpeg", "poster.png", "poster.gif"}) {
		t.Fatalf("primary extension priority = %v", got)
	}
	if !slices.Contains(got[4:], "poster.svg") || !slices.Contains(got[4:], "poster.webp") {
		t.Fatalf("known unsupported formats must remain candidates after supported formats: %v", got)
	}
	for _, name := range []string{"poster.svg", "poster.webp"} {
		assertImageCandidates(t, imageCandidateNames([]string{name}, "Folder", "Movies", true), map[string][]string{"Primary": {name}})
	}
}

func TestImageCandidateNamesBackdropExtensionDeduplication(t *testing.T) {
	names := []string{
		"backdrop.png", "backdrop.gif", "backdrop.jpeg", "backdrop.jpg", "backdrop.svg",
		"backdrop2.png", "backdrop2.gif", "backdrop10.webp",
	}
	assertImageCandidates(t, imageCandidateNames(names, "Movie", "Film.mkv", false), map[string][]string{
		"Backdrop": {"backdrop.jpg", "backdrop2.png", "backdrop10.webp"},
	})
}

func TestImageCandidateNamesBackdropNumericOrder(t *testing.T) {
	for _, prefix := range []string{"backdrop", "fanart"} {
		t.Run(prefix, func(t *testing.T) {
			names := []string{prefix + "10.jpg", prefix + "2.jpg", prefix + "1.jpg", prefix + ".jpg"}
			want := []string{prefix + ".jpg", prefix + "1.jpg", prefix + "2.jpg", prefix + "10.jpg"}
			assertImageCandidates(t, imageCandidateNames(names, "Folder", "Movies", true), map[string][]string{"Backdrop": want})
		})
	}
	for _, prefix := range []string{"backdrop", "fanart", "background", "art"} {
		t.Run(prefix+" with separator", func(t *testing.T) {
			names := []string{prefix + "-10.jpg", prefix + "-2.jpg", prefix + ".jpg"}
			want := []string{prefix + ".jpg", prefix + "-2.jpg", prefix + "-10.jpg"}
			assertImageCandidates(t, imageCandidateNames(names, "Folder", "Movies", true), map[string][]string{"Backdrop": want})
		})
	}
}

func TestImageCandidateNamesBackdropLimitAfterOrderingAndDeduplication(t *testing.T) {
	names := []string{"backdrop.png", "backdrop.jpg", "backdrop1.png"}
	for index := 40; index >= 1; index-- {
		names = append(names, fmt.Sprintf("backdrop%d.jpg", index))
	}
	want := []string{"backdrop.jpg"}
	for index := 1; index < 32; index++ {
		want = append(want, fmt.Sprintf("backdrop%d.jpg", index))
	}
	assertImageCandidates(t, imageCandidateNames(names, "Movie", "Film.mkv", false), map[string][]string{"Backdrop": want})
}

func TestImageCandidateNamesCaseInsensitiveAndStable(t *testing.T) {
	names := []string{"poster.jpg", "Poster.JPG", "POSTER.JPG", "backdrop.jpg", "Backdrop.JPG", "BACKDROP.JPG"}
	before := slices.Clone(names)
	got := imageCandidateNames(names, "Folder", "Movies", true)
	if !slices.Equal(names, before) {
		t.Fatal("candidate selection mutated the caller's filename list")
	}
	if len(got["Primary"]) == 0 || got["Primary"][0] != "POSTER.JPG" {
		t.Fatalf("case-insensitive primary tie must use stable raw filename order: %v", got["Primary"])
	}
	if !slices.Equal(got["Backdrop"], []string{"BACKDROP.JPG"}) {
		t.Fatalf("case-insensitive backdrop stems were not deduplicated deterministically: %v", got["Backdrop"])
	}
	slices.Reverse(names)
	assertImageCandidates(t, imageCandidateNames(names, "Folder", "Movies", true), got)
	assertImageCandidates(t, imageCandidateNames([]string{"fIlM-PoStEr.JpG"}, "Movie", "Movies/Film.MKV", false), map[string][]string{
		"Primary": {"fIlM-PoStEr.JpG"},
	})
}

func TestImageCandidateNamesRejectsUnrelatedNames(t *testing.T) {
	names := []string{
		"holiday.jpg", "Film Behind The Scenes.png", "Other-poster.jpg", "poster.txt", "folder.nfo",
		"cover.mp4", "backdropnotes.jpg", "backdrop-abc.jpg", "backdrop1extra.jpg", "fanart-final.jpg",
		"background-other.jpg", "clearartwork.png", "mylogo.png", "banner-old.jpg",
	}
	assertImageCandidates(t, imageCandidateNames(names, "Movie", "Film.mkv", false), nil)
	assertImageCandidates(t, imageCandidateNames(nil, "Movie", "Film.mkv", false), nil)
}

func assertImageCandidates(t *testing.T, got, want map[string][]string) {
	t.Helper()
	allowed := []string{"Primary", "Backdrop", "Thumb", "Banner", "Logo", "Art"}
	for _, imageType := range allowed {
		if !slices.Equal(got[imageType], want[imageType]) {
			t.Errorf("%s candidates = %v, want %v", imageType, got[imageType], want[imageType])
		}
	}
	for imageType := range got {
		if !slices.Contains(allowed, imageType) {
			t.Errorf("unexpected image type %q", imageType)
		}
	}
}
