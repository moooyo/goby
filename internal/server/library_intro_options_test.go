package server

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestEmbyIntroOptionsUseOneExplicitAutomaticMode(t *testing.T) {
	for _, raw := range []string{
		`{"EnableMarkerDetection":true}`,
		`{"EnableMarkerDetection":true,"EnableMarkerDetectionDuringLibraryScan":true,"IntroDetectionFingerprintLength":10}`,
		`{"enablemarkerdetection":true,"enablemarkerdetectionduringlibraryscan":true}`,
	} {
		var wire embyLibraryOptionsUpdate
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		options, err := wire.native()
		if err != nil || options.EnableIntroDetection == nil || !*options.EnableIntroDetection || options.EnableLocalMetadata != nil || options.EnableLocalImages != nil || options.EnableEmbeddedArtwork != nil {
			t.Fatalf("intro setting changed an unrelated option: %s %+v %v", raw, options, err)
		}
		if !nativeLibraryCreationOptions(options).EnableIntroDetection {
			t.Fatal("native creation discarded the selected intro policy")
		}
	}
	for _, enabled := range []bool{false, true} {
		options := library.DefaultLibraryOptions()
		options.EnableIntroDetection = enabled
		projection := embyEditableLibraryOptions(library.Library{CollectionType: "tvshows", Options: &options})
		if projection["EnableMarkerDetection"] != enabled || projection["EnableMarkerDetectionDuringLibraryScan"] != enabled || projection["IntroDetectionFingerprintLength"] != 10 {
			t.Fatalf("projection misrepresented the supported mode: %+v", projection)
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
		if err != nil || update.EnableIntroDetection == nil || *update.EnableIntroDetection != enabled {
			t.Fatalf("the advertised library options cannot roundtrip: %v", err)
		}
	}
}

func TestEmbyIntroOptionsRejectUnsupportedModesAndFingerprintLengths(t *testing.T) {
	for _, raw := range []string{
		`{"EnableMarkerDetection":null}`, `{"EnableMarkerDetection":"true"}`,
		`{"EnableMarkerDetectionDuringLibraryScan":true}`,
		`{"EnableMarkerDetection":true,"EnableMarkerDetectionDuringLibraryScan":false}`,
		`{"EnableMarkerDetection":false,"EnableMarkerDetectionDuringLibraryScan":true}`,
		`{"EnableMarkerDetection":false,"EnableMarkerDetectionDuringLibraryScan":null}`,
		`{"IntroDetectionFingerprintLength":null}`, `{"IntroDetectionFingerprintLength":0}`,
		`{"IntroDetectionFingerprintLength":5}`, `{"IntroDetectionFingerprintLength":10.5}`,
		`{"IntroDetectionFingerprintLength":"10"}`,
		`{"EnableMarkerDetection":true,"enablemarkerdetection":false}`,
	} {
		var wire embyLibraryOptionsUpdate
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			continue
		}
		if _, err := wire.native(); err == nil {
			t.Errorf("unsupported setting was silently acknowledged: %s", raw)
		}
	}
	var fixed embyLibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"IntroDetectionFingerprintLength":10}`), &fixed); err != nil {
		t.Fatal(err)
	}
	if options, err := fixed.native(); err != nil || options.EnableIntroDetection != nil {
		t.Fatalf("fixed fingerprint echo changed the intro switch: %+v %v", options, err)
	}
}
