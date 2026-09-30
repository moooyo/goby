package library

import (
	"errors"
	"testing"
)

func TestAnalysisStrictJSONOnlyExplicitOmitEmptyFieldsAreOptional(t *testing.T) {
	type detail struct {
		Samples int
	}
	type envelope struct {
		Required string
		Optional *detail `json:",omitempty"`
		Named    string  `json:"Renamed,omitempty"`
	}
	for _, raw := range []string{
		`{"Required":"present"}`,
		`{"Required":"present","Optional":null}`,
		`{"Required":"present","Optional":{"Samples":16},"Renamed":"value"}`,
	} {
		var value envelope
		if err := analysisStrictJSON([]byte(raw), &value); err != nil {
			t.Fatalf("valid optional-field shape rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{}`,
		`{"Required":"present","Unknown":null}`,
		`{"required":"present"}`,
		`{"Required":"present","optional":null}`,
		`{"Required":"present","Named":"value"}`,
		`{"Required":"present","Optional":null,"Optional":null}`,
		`{"Required":"present","Required":"duplicate"}`,
		`{"Required":"present","Optional":{}}`,
		`{"Required":"present","Optional":{"Samples":16,"Samples":16}}`,
		`{"Required":"present","Optional":{"samples":16}}`,
		`{"Required":"present","Optional":{"Samples":16,"Future":0}}`,
		`{"Required":"present"} {}`,
	} {
		var value envelope
		if err := analysisStrictJSON([]byte(raw), &value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid optional-field shape accepted: %s: %v", raw, err)
		}
	}
}
