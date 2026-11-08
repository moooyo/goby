package backuppg

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSelectedTranscodingControlsRecoveryCatalogPreservesSchema77AndChangesOnlyRuntimeDefaultAndConstraint(t *testing.T) {
	previous, prefix, err := loadCatalog(77, "selected_transcoding_controls_catalog")
	if err != nil {
		t.Fatal("load the published schema77 runtime catalog", err)
	}
	current, migrations, err := loadCatalog(78, "selected_transcoding_controls_catalog")
	if err != nil {
		t.Fatal("load the observed schema78 selected-control catalog", err)
	}
	if len(prefix) != 77 || len(migrations) != 78 ||
		prefix[76].Name != "0077_managed_transcoding_options.sql" ||
		prefix[76].SHA256 != "6bc704aed4d6385da9dd821425fbc96c2660f7d312ce03f5d1421b560e6fbaa6" ||
		previous.SHA256 != "ef564e93ed28cbc26c5df0e2eeb8b3587777d44807b92a0a2bdd3a6b6534c027" ||
		!equalJSON(prefix, migrations[:77]) || migrations[77].Name != "0078_managed_transcoding_controls.sql" ||
		previous.SHA256 == current.SHA256 || !equalJSON(previous.Tables, current.Tables) ||
		!equalJSON(previous.Sequences, current.Sequences) || !equalJSON(previous.Constraints, current.Constraints) {
		t.Fatal("selected controls changed the published schema77 witness, migration prefix, row shapes, or sequence/foreign-key ownership")
	}
	legacyGroups := []string{"Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping", "Transcoding", "TranscodingTempPath", "SoftwareToneMapOptions", "CodecConfigurations"}
	selectedGroups := []string{"EnableThrottling", "ExtractionThreadCount", "CodecProfileLimits", "SubtitleOptions"}
	allGroups := append(append([]string{}, legacyGroups...), selectedGroups...)
	before := backendReviewCatalogObjects(t, 77)
	after := backendReviewCatalogObjects(t, 78)
	if len(before) != len(after) {
		t.Fatal("selected controls changed the schema object inventory")
	}
	changed := make(map[string]int)
	for index, object := range after {
		prior := before[index]
		if object.Kind != prior.Kind || object.Name != prior.Name {
			t.Fatalf("selected controls omitted or reordered an object: %s %s", object.Kind, object.Name)
		}
		if equalJSON(object, prior) {
			continue
		}
		var previousValue, currentValue map[string]json.RawMessage
		if json.Unmarshal(prior.Value, &previousValue) != nil || json.Unmarshal(object.Value, &currentValue) != nil {
			t.Fatal("decode the selected-control catalog object")
		}
		switch {
		case object.Kind == "column" && object.Name == "managed_settings.00013":
			assertManagedTranscodingCatalogDefault(t, previousValue["default"], legacyGroups)
			assertManagedTranscodingCatalogDefault(t, currentValue["default"], allGroups)
			currentValue["default"] = previousValue["default"]
		case object.Kind == "constraint" && object.Name == "managed_settings.managed_runtime_overrides_shape":
			var legacy, extended string
			if json.Unmarshal(previousValue["definition"], &legacy) != nil ||
				json.Unmarshal(currentValue["definition"], &extended) != nil ||
				!strings.HasPrefix(legacy, "CHECK (") || !strings.HasSuffix(legacy, ")") ||
				!strings.Contains(extended, strings.TrimSuffix(strings.TrimPrefix(legacy, "CHECK ("), ")")) {
				t.Fatal("schema78 lost the exact published seven- and eleven-group runtime branches")
			}
			for _, group := range selectedGroups {
				if strings.Contains(legacy, "'"+group+"'::text") {
					t.Fatalf("schema77 acquired the new runtime group %s", group)
				}
			}
			inventory := "ARRAY['" + strings.Join(allGroups, "'::text, '") + "'::text]"
			const sizeBound = "octet_length(runtime_overrides::text) <= 16384"
			if !strings.Contains(extended, "runtime_overrides ?& "+inventory) ||
				!strings.Contains(extended, "(runtime_overrides - "+inventory+") = '{}'::jsonb") ||
				strings.Count(extended, sizeBound) != strings.Count(legacy, sizeBound)+1 ||
				!strings.Contains(extended, "'AllowAssToText'::text") {
				t.Fatal("schema78 omitted its complete bounded fifteen-group contract or subtitle choice")
			}
			currentValue["definition"] = previousValue["definition"]
		default:
			t.Fatalf("selected controls changed an unrelated object: %s %s", object.Kind, object.Name)
		}
		if !equalJSON(previousValue, currentValue) {
			t.Fatalf("selected controls changed runtime object metadata beyond its default or check: %s", object.Name)
		}
		changed[object.Name]++
	}
	if len(changed) != 2 || changed["managed_settings.00013"] != 1 ||
		changed["managed_settings.managed_runtime_overrides_shape"] != 1 {
		t.Fatal("schema78 omitted or duplicated its runtime default and constraint changes")
	}
	previousPlan, err := compiledRecoveryDropPlan(77)
	if err != nil {
		t.Fatal("load the published schema77 authenticated recovery plan", err)
	}
	currentPlan, err := compiledRecoveryDropPlan(78)
	if err != nil || !reflect.DeepEqual(previousPlan, currentPlan) {
		t.Fatal("selected controls changed the authenticated recovery object plan", err)
	}
}
