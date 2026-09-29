package server

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestEmbyPreviewOptionsUseOneExplicitAutomaticMode(t *testing.T) {
	for _, raw := range []string{
		`{"EnableChapterImageExtraction":true}`,
		`{"EnableChapterImageExtraction":true,"ExtractChapterImagesDuringLibraryScan":true}`,
		`{"enablechapterimageextraction":true,"extractchapterimagesduringlibraryscan":true}`,
	} {
		var wire embyLibraryOptionsUpdate
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		options, err := wire.native()
		if err != nil || options.EnablePreviewGeneration == nil || !*options.EnablePreviewGeneration || options.EnableIntroDetection != nil || options.EnableLocalMetadata != nil || options.EnableLocalImages != nil || options.EnableEmbeddedArtwork != nil {
			t.Fatalf("preview setting changed an unrelated option: %s %+v %v", raw, options, err)
		}
		if !nativeLibraryCreationOptions(options).EnablePreviewGeneration {
			t.Fatal("native creation discarded the preview policy")
		}
	}
	for _, enabled := range []bool{false, true} {
		options := library.DefaultLibraryOptions()
		options.EnablePreviewGeneration = enabled
		projection := embyEditableLibraryOptions(library.Library{CollectionType: "movies", Options: &options})
		if projection["EnableChapterImageExtraction"] != enabled || projection["ExtractChapterImagesDuringLibraryScan"] != enabled {
			t.Fatalf("projection misrepresented the supported preview mode: %+v", projection)
		}
		encoded, err := json.Marshal(projection)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip embyLibraryOptionsUpdate
		if err := json.Unmarshal(encoded, &roundtrip); err != nil {
			t.Fatal(err)
		}
		update, err := roundtrip.native()
		if err != nil || update.EnablePreviewGeneration == nil || *update.EnablePreviewGeneration != enabled {
			t.Fatalf("the advertised preview options cannot roundtrip: %v", err)
		}
	}
}

func TestEmbyPreviewOptionsRejectUnsupportedModesAndUnconsumedFields(t *testing.T) {
	for _, raw := range []string{
		`{"EnableChapterImageExtraction":null}`, `{"EnableChapterImageExtraction":"true"}`,
		`{"ExtractChapterImagesDuringLibraryScan":true}`, `{"ExtractChapterImagesDuringLibraryScan":false}`,
		`{"EnableChapterImageExtraction":true,"ExtractChapterImagesDuringLibraryScan":false}`,
		`{"EnableChapterImageExtraction":false,"ExtractChapterImagesDuringLibraryScan":true}`,
		`{"EnableChapterImageExtraction":true,"ExtractChapterImagesDuringLibraryScan":null}`,
		`{"EnableChapterImageExtraction":true,"enablechapterimageextraction":false}`,
		`{"EnableChapterImageExtraction":true,"ChapterImageInterval":10}`,
	} {
		var wire embyLibraryOptionsUpdate
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			continue
		}
		if _, err := wire.native(); err == nil {
			t.Errorf("unsupported preview setting was silently acknowledged: %s", raw)
		}
	}
}
