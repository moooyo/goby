package library

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAudioWaveformPolicyDefaultsAndStrictUpdates(t *testing.T) {
	if DefaultLibraryOptions().EnableAudioWaveformGeneration || EffectiveLibraryOptions(Library{}).EnableAudioWaveformGeneration {
		t.Fatal("audio waveforms must default off")
	}
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnableAudioWaveformGeneration":true}`), &update); err != nil {
		t.Fatal(err)
	}
	options := applyLibraryOptions(DefaultLibraryOptions(), &update)
	if !options.EnableAudioWaveformGeneration || options.EnablePreviewGeneration || options.EnableBackgroundPreviewGeneration || options.EnableIntroDetection {
		t.Fatalf("independent waveform policy was coupled: %+v", options)
	}
	for _, collection := range []string{"movies", "tvshows", "mixed"} {
		if err := validateLibraryOptions(collection, options); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateLibraryOptions("music", options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported library policy: %v", err)
	}
	for _, input := range []string{`{"EnableAudioWaveformGeneration":null}`, `{"EnableAudioWaveformGeneration":true,"enableaudiowaveformgeneration":false}`} {
		if json.Unmarshal([]byte(input), &update) == nil {
			t.Fatalf("ambiguous waveform option accepted: %s", input)
		}
	}
}
