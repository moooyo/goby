package library

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSubtitleTimelinePolicyDefaultsAndStrictUpdates(t *testing.T) {
	if DefaultLibraryOptions().EnableSubtitleTimelineGeneration || EffectiveLibraryOptions(Library{}).EnableSubtitleTimelineGeneration {
		t.Fatal("subtitle timelines must default off")
	}
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnableSubtitleTimelineGeneration":true}`), &update); err != nil {
		t.Fatal(err)
	}
	options := applyLibraryOptions(DefaultLibraryOptions(), &update)
	if !options.EnableSubtitleTimelineGeneration || options.EnableAudioWaveformGeneration || options.EnablePreviewGeneration || options.EnableBackgroundPreviewGeneration || options.EnableIntroDetection {
		t.Fatalf("independent subtitle timeline policy was coupled: %+v", options)
	}
	for _, collection := range []string{"movies", "tvshows", "mixed"} {
		if err := validateLibraryOptions(collection, options); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateLibraryOptions("music", options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported library policy: %v", err)
	}
	for _, input := range []string{
		`{"EnableSubtitleTimelineGeneration":null}`,
		`{"EnableSubtitleTimelineGeneration":true,"enablesubtitletimelinegeneration":false}`,
		`{"EnableSubtitleTimelineGeneration":"true"}`,
	} {
		if json.Unmarshal([]byte(input), &update) == nil {
			t.Fatalf("ambiguous subtitle timeline option accepted: %s", input)
		}
	}
}
