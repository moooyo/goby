package library

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestLibraryPreviewPolicyRequiresExplicitVideoLibraryOptIn(t *testing.T) {
	if DefaultLibraryOptions().EnablePreviewGeneration || EffectiveLibraryOptions(Library{}).EnablePreviewGeneration {
		t.Fatal("historical callers must not acquire automatic preview work")
	}
	options := DefaultLibraryOptions()
	options.EnablePreviewGeneration = true
	for _, kind := range []string{"movies", "tvshows", "mixed"} {
		if err := validateLibraryOptions(kind, options); err != nil {
			t.Errorf("supported preview library rejected: %s: %v", kind, err)
		}
	}
	for _, kind := range []string{"music", "collections", "homevideos", ""} {
		if err := validateLibraryOptions(kind, options); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("unsupported preview library accepted: %s: %v", kind, err)
		}
	}
	options.EnableIntroDetection = true
	if err := validateLibraryOptions("movies", options); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("preview support broadened the independent intro policy")
	}
}

func TestLibraryPreviewOptionInputPreservesIndependentFlags(t *testing.T) {
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnablePreviewGeneration":true}`), &update); err != nil {
		t.Fatal(err)
	}
	previous := DefaultLibraryOptions()
	previous.EnableIntroDetection = true
	options := applyLibraryOptions(previous, &update)
	if !options.EnablePreviewGeneration || !options.EnableIntroDetection || !options.EnableLocalImages || !options.EnableLocalMetadata || !options.EnableEmbeddedArtwork {
		t.Fatalf("preview edit changed unrelated options: %+v", options)
	}
	if err := json.Unmarshal([]byte(`{"EnableLocalMetadata":false}`), &update); err != nil || update.EnablePreviewGeneration != nil {
		t.Fatalf("omitted preview policy became an explicit change: %+v %v", update, err)
	}
	if next := applyLibraryOptions(options, &update); !next.EnablePreviewGeneration || !next.EnableIntroDetection || next.EnableLocalMetadata {
		t.Fatalf("importer edit reset generation policy: %+v", next)
	}
	for _, raw := range []string{
		`{"EnablePreviewGeneration":null}`, `{"EnablePreviewGeneration":"true"}`,
		`{"EnablePreviewGeneration":1}`, `{"EnablePreviewGeneration":true,"EnablePreviewGeneration":false}`,
		`{"EnablePreviewGeneration":true,"enablepreviewgeneration":false}`, `{"PreviewInterval":10}`,
	} {
		if err := json.Unmarshal([]byte(raw), &update); err == nil {
			t.Errorf("ambiguous or unsupported preview option accepted: %s", raw)
		}
	}
}
