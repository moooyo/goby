package library

import (
	"encoding/json"
	"testing"
)

func TestLocalMetadataRefreshRetainsUnchangedUnknownSourceExtensions(t *testing.T) {
	raw := []byte(`{"Kind":"movie","Name":"Old title","People":[{"Name":"Actor","Role":"Lead","Type":"Actor","SortOrder":0,"FutureCredit":{"PreciseInteger":9007199254740993}}],"FutureTopLevel":{"PreciseInteger":9007199254740993}}`)
	var local localMetadata
	if err := decodeLocalMetadata(raw, &local); err != nil {
		t.Fatal(err)
	}
	local.value.Name = "New title"
	encoded, err := encodeLocalMetadata(local)
	if err != nil {
		t.Fatal(err)
	}
	object, err := metadataSourceObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	metadataTestEqualJSON(t, object["People"], metadataTestObject(t, raw)["People"])
	metadataTestEqualJSON(t, object["FutureTopLevel"], json.RawMessage(`{"PreciseInteger":9007199254740993}`))
	if string(object["Name"]) != `"New title"` {
		t.Errorf("refreshed title = %s", object["Name"])
	}
	local.value.People[0].Name = "Different actor"
	encoded, err = encodeLocalMetadata(local)
	if err != nil {
		t.Fatal(err)
	}
	var changed struct {
		People []map[string]json.RawMessage
	}
	if err := json.Unmarshal(encoded, &changed); err != nil {
		t.Fatal(err)
	}
	if _, exists := changed.People[0]["FutureCredit"]; exists {
		t.Fatal("unknown credit extension was reassigned to a different person")
	}
	local.value = nil
	if encoded, err = encodeLocalMetadata(local); err != nil || encoded != nil {
		t.Errorf("deleted source retained metadata: %s, %v", encoded, err)
	}
}
