package library

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func TestOnlineMetadataPreservesAdministratorControls(t *testing.T) {
	base := []byte(`{"Name":"Scanner title","SortName":"scanner title","Overview":"Local summary","ProviderIDs":{"Imdb":"tt123"}}`)
	online, err := normalizeOnlineMetadata(providers.Metadata{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, Fields: map[string]json.RawMessage{"Name": json.RawMessage(`"Provider title"`), "Overview": json.RawMessage(`"Provider summary"`), "ProviderIds": json.RawMessage(`{"Tmdb":"42"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := mergeOnlineSource(base, online)
	if err != nil {
		t.Fatal(err)
	}
	effective, _, err := composeMetadataValues(automatic, map[string]json.RawMessage{"Name": json.RawMessage(`"Administrator title"`)}, map[string]json.RawMessage{"Overview": json.RawMessage(`"Locked summary"`)})
	if err != nil {
		t.Fatal(err)
	}
	if effective.Name != "Administrator title" || effective.Overview != "Locked summary" || effective.ProviderIDs["Tmdb"] != "42" {
		t.Fatalf("controls did not survive provider merge: %#v", effective)
	}
}

func TestOnlineMetadataRejectsUnknownFieldsAndStructuralParent(t *testing.T) {
	result := providers.Metadata{Selection: providers.Selection{Provider: "tmdb", ID: "42:1:2", Type: "Episode"}, Fields: map[string]json.RawMessage{"Name": json.RawMessage(`"Episode"`), "ParentIndexNumber": json.RawMessage(`99`)}}
	data, err := normalizeOnlineMetadata(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["ParentIndexNumber"]; exists {
		t.Fatal("provider changed structural season membership")
	}
	result.Fields["Path"] = json.RawMessage(`"/arbitrary/path"`)
	if _, err := normalizeOnlineMetadata(result); err == nil {
		t.Fatal("accepted unsupported provider field")
	}
}
