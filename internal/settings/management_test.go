package settings

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestStoredManagementRejectsMissingBooleansAndUnsupportedFields(t *testing.T) {
	encoded, err := json.Marshal(DefaultManagement())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	delete(document["Metadata"], "EnableInternetProviders")
	malformed, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeStoredManagement(malformed); !errors.Is(err, ErrStoredSettings) {
		t.Fatal("a missing persisted boolean silently became false")
	}
	document["Metadata"]["EnableInternetProviders"] = false
	document["Metadata"]["Unsupported"] = true
	malformed, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeStoredManagement(malformed); !errors.Is(err, ErrStoredSettings) {
		t.Fatal("stored unimplemented configuration was accepted")
	}
}

func TestManagementLimitsRejectProviderIncompatibleLanguages(t *testing.T) {
	value := DefaultManagement()
	value.Subtitles.DownloadLanguages = []string{"eng"}
	if !errors.Is(ValidateManagement(value), ErrInvalidInput) {
		t.Fatal("an OpenSubtitles-incompatible language was accepted")
	}
	value.Subtitles.DownloadLanguages = []string{}
	if err := ValidateManagement(value); err != nil {
		t.Fatal("an explicit disabled subtitle language list was rejected")
	}
	value.Tasks.MaxConcurrent = 0
	if !errors.Is(ValidateManagement(value), ErrInvalidInput) {
		t.Fatal("unbounded worker concurrency was accepted")
	}
}
