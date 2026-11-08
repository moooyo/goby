package backuppg

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestManagedTranscodingRecoveryCatalogChangesOnlyRuntimeDefaultAndConstraint(t *testing.T) {
	historical, historicalPrefix, err := loadCatalog(47, "managed_transcoding_catalog")
	if err != nil {
		t.Fatal("load the published schema47 runtime catalog", err)
	}
	previous, prefix, err := loadCatalog(76, "managed_transcoding_catalog")
	if err != nil {
		t.Fatal("load the published schema76 runtime catalog", err)
	}
	current, migrations, err := loadCatalog(77, "managed_transcoding_catalog")
	if err != nil {
		t.Fatal("load the observed schema77 runtime catalog", err)
	}
	if len(historicalPrefix) != 47 || len(prefix) != 76 || len(migrations) != 77 ||
		!equalJSON(historicalPrefix, prefix[:47]) || !equalJSON(prefix, migrations[:76]) ||
		migrations[76].Name != "0077_managed_transcoding_options.sql" ||
		len(historical.Tables) != 56 || len(previous.Tables) != 94 || previous.SHA256 == current.SHA256 ||
		!equalJSON(previous.Tables, current.Tables) || !equalJSON(previous.Sequences, current.Sequences) ||
		!equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("managed transcoding changed the historical migration prefix, row shapes, or object ownership")
	}
	legacyGroups := []string{"Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping"}
	transcodingGroups := []string{"Transcoding", "TranscodingTempPath", "SoftwareToneMapOptions", "CodecConfigurations"}
	legacyObjects := make(map[string]backendReviewCatalogObject)
	for _, object := range backendReviewCatalogObjects(t, 47) {
		if object.Kind == "column" && object.Name == "managed_settings.00013" ||
			object.Kind == "constraint" && object.Name == "managed_settings.managed_runtime_overrides_shape" {
			legacyObjects[object.Name] = object
		}
	}
	if len(legacyObjects) != 2 {
		t.Fatal("schema47 omitted its original runtime default or constraint")
	}
	before := backendReviewCatalogObjects(t, 76)
	after := backendReviewCatalogObjects(t, 77)
	if len(before) != len(after) {
		t.Fatal("managed transcoding changed the schema object inventory")
	}
	changed := make(map[string]int)
	for index, object := range after {
		prior := before[index]
		if object.Kind != prior.Kind || object.Name != prior.Name {
			t.Fatalf("managed transcoding omitted or reordered an object: %s %s", object.Kind, object.Name)
		}
		if legacy, exists := legacyObjects[object.Name]; exists && !equalJSON(legacy, prior) {
			t.Fatalf("schema76 changed the original schema47 runtime object: %s", object.Name)
		}
		if equalJSON(object, prior) {
			continue
		}
		var previousValue, currentValue map[string]json.RawMessage
		if json.Unmarshal(prior.Value, &previousValue) != nil || json.Unmarshal(object.Value, &currentValue) != nil {
			t.Fatal("decode the managed runtime catalog object")
		}
		switch {
		case object.Kind == "column" && object.Name == "managed_settings.00013":
			assertManagedTranscodingCatalogDefault(t, previousValue["default"], legacyGroups)
			assertManagedTranscodingCatalogDefault(t, currentValue["default"], append(legacyGroups, transcodingGroups...))
			currentValue["default"] = previousValue["default"]
		case object.Kind == "constraint" && object.Name == "managed_settings.managed_runtime_overrides_shape":
			var legacy, extended string
			if json.Unmarshal(previousValue["definition"], &legacy) != nil ||
				json.Unmarshal(currentValue["definition"], &extended) != nil ||
				!strings.HasPrefix(extended, strings.TrimSuffix(legacy, ")")+" OR ") ||
				!strings.Contains(extended, "octet_length(runtime_overrides::text) <= 16384") {
				t.Fatal("schema77 lost the exact legacy branch or the bounded new runtime branch")
			}
			for _, group := range transcodingGroups {
				if !strings.Contains(extended, "'"+group+"'::text") {
					t.Fatalf("schema77 omitted the managed runtime group %s", group)
				}
			}
			currentValue["definition"] = previousValue["definition"]
		default:
			t.Fatalf("managed transcoding changed an unrelated object: %s %s", object.Kind, object.Name)
		}
		if !equalJSON(previousValue, currentValue) {
			t.Fatalf("managed transcoding changed runtime object metadata beyond its default or check: %s", object.Name)
		}
		changed[object.Name]++
	}
	if len(changed) != 2 || changed["managed_settings.00013"] != 1 ||
		changed["managed_settings.managed_runtime_overrides_shape"] != 1 {
		t.Fatal("schema77 omitted or duplicated its runtime default and constraint changes")
	}
	previousPlan, err := compiledRecoveryDropPlan(76)
	if err != nil {
		t.Fatal("load the historical authenticated runtime recovery plan", err)
	}
	currentPlan, err := compiledRecoveryDropPlan(77)
	if err != nil || !reflect.DeepEqual(previousPlan, currentPlan) {
		t.Fatal("managed transcoding changed the authenticated recovery object plan", err)
	}
}

func assertManagedTranscodingCatalogDefault(t *testing.T, raw json.RawMessage, groups []string) {
	t.Helper()
	var expression string
	if json.Unmarshal(raw, &expression) != nil || !strings.HasPrefix(expression, "'") || !strings.HasSuffix(expression, "'::jsonb") {
		t.Fatal("runtime default is not an explicit JSONB object")
	}
	var payload map[string]any
	if json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(expression, "'"), "'::jsonb")), &payload) != nil || len(payload) != len(groups) {
		t.Fatal("runtime default changed its closed group inventory")
	}
	for _, group := range groups {
		if value, exists := payload[group]; !exists || value != nil {
			t.Fatalf("runtime default omitted or inferred a value for %s", group)
		}
	}
}
