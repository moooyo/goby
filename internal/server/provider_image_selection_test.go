package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderArtworkRevisionDistinguishesOmittedAndInvalidValues(t *testing.T) {
	if revision, managed, err := providerArtworkRevision(nil); err != nil || revision != "" || managed {
		t.Fatalf("an omitted revision stopped selecting legacy behavior: revision=%q managed=%t error=%v", revision, managed, err)
	}
	for _, value := range []string{"0", "42", strings.Repeat("9", 78)} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if revision, managed, err := providerArtworkRevision(raw); err != nil || revision != value || !managed {
			t.Errorf("valid artwork revision %q: revision=%q managed=%t error=%v", value, revision, managed, err)
		}
	}
	for _, raw := range []string{`null`, `0`, `true`, `{}`, `[]`, `""`, `"01"`, `"-1"`, `"+1"`, `" 1"`, `"1 "`, `"\u0661"`, `"` + strings.Repeat("9", 79) + `"`} {
		if _, managed, err := providerArtworkRevision(json.RawMessage(raw)); err == nil || !managed {
			t.Errorf("a supplied invalid revision selected legacy behavior: raw=%s managed=%t error=%v", raw, managed, err)
		}
	}
}
