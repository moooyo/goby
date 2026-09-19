package media

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseProbePreservesDolbyVisionConfiguration(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile any
		el      any
		compat  any
		want    DolbyVisionMetadata
	}{
		{"profile 5 single layer", 5, 0, 0, DolbyVisionMetadata{Profile: 5, Level: 6, RPUPresent: true, BLPresent: true}},
		{"profile 8 HDR10 base", "8", "0", "1", DolbyVisionMetadata{Profile: 8, Level: 6, RPUPresent: true, BLPresent: true, CompatibilityID: 1}},
		{"profile 8 HLG base", 8, 0, 4, DolbyVisionMetadata{Profile: 8, Level: 6, RPUPresent: true, BLPresent: true, CompatibilityID: 4}},
		{"profile 7 dual layer", 7, 1, 6, DolbyVisionMetadata{Profile: 7, Level: 6, RPUPresent: true, ELPresent: true, BLPresent: true, CompatibilityID: 6}},
		{"reserved profile retained", 127, 0, 15, DolbyVisionMetadata{Profile: 127, Level: 6, RPUPresent: true, BLPresent: true, CompatibilityID: 15}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := dolbyVisionRecordFixture()
			record["dv_profile"], record["el_present_flag"], record["dv_bl_signal_compatibility_id"] = test.profile, test.el, test.compat
			stream := parseDolbyVisionFixture(t, "video", record)
			if stream.DolbyVision == nil || *stream.DolbyVision != test.want || stream.VideoRange != "DOVI" || !stream.VideoRangeKnown {
				t.Fatalf("incorrect Dolby Vision facts: %+v, %+v", stream, stream.DolbyVision)
			}
			encoded, err := json.Marshal(stream)
			if err != nil {
				t.Fatal(err)
			}
			var restored Stream
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored, stream) {
				t.Fatalf("cached Dolby Vision facts changed: got %+v, want %+v", restored, stream)
			}
		})
	}
}

func TestParseProbeDolbyVisionPreservesExplicitAbsentLayers(t *testing.T) {
	record := dolbyVisionRecordFixture()
	record["rpu_present_flag"], record["el_present_flag"], record["bl_present_flag"] = "0", 0, "0"
	stream := parseDolbyVisionFixture(t, "video", record)
	if stream.DolbyVision == nil || stream.DolbyVision.RPUPresent || stream.DolbyVision.ELPresent || stream.DolbyVision.BLPresent {
		t.Fatalf("explicit absent layers were not preserved: %+v", stream.DolbyVision)
	}
}

func TestParseProbeDolbyVisionCompressionPreservesUnknownAndReservedFacts(t *testing.T) {
	for _, value := range []string{"none", "limited", "extended", "reserved", "unknown", ""} {
		record := dolbyVisionRecordFixture()
		record["dv_md_compression"] = value
		stream := parseDolbyVisionFixture(t, "video", record)
		want := value
		if want == "unknown" {
			want = ""
		}
		if stream.DolbyVision == nil || stream.DolbyVision.MetadataCompression != want {
			t.Fatalf("compression was guessed or discarded: value=%q metadata=%+v", value, stream.DolbyVision)
		}
	}
}

func TestParseProbeDolbyVisionRequiresEveryConfigurationField(t *testing.T) {
	for _, field := range []string{
		"dv_profile", "dv_level", "rpu_present_flag", "el_present_flag", "bl_present_flag", "dv_bl_signal_compatibility_id",
	} {
		for _, state := range []struct {
			name    string
			value   any
			missing bool
		}{
			{name: "missing", missing: true},
			{name: "null", value: nil},
			{name: "empty", value: ""},
			{name: "unavailable", value: "N/A"},
			{name: "unknown", value: "unknown"},
		} {
			t.Run(field+"/"+state.name, func(t *testing.T) {
				record := dolbyVisionRecordFixture()
				if state.missing {
					delete(record, field)
				} else {
					record[field] = state.value
				}
				stream := parseDolbyVisionFixture(t, "video", record)
				if stream.DolbyVision != nil || stream.VideoRange != "DOVI" || !stream.VideoRangeKnown {
					t.Fatalf("incomplete configuration did not retain only the DOVI range: %+v", stream)
				}
			})
		}
	}
}

func TestParseProbeRejectsMalformedDolbyVisionConfiguration(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		value any
	}{
		{"negative profile", "dv_profile", -1},
		{"profile overflow", "dv_profile", 128},
		{"level overflow", "dv_level", 64},
		{"fractional level", "dv_level", "1/2"},
		{"negative RPU flag", "rpu_present_flag", -1},
		{"invalid EL flag", "el_present_flag", 2},
		{"fractional BL flag", "bl_present_flag", 0.5},
		{"boolean flag", "rpu_present_flag", true},
		{"compatibility overflow", "dv_bl_signal_compatibility_id", 16},
		{"integer overflow", "dv_profile", "9223372036854775808"},
		{"malformed scalar", "dv_level", "invalid"},
		{"object scalar", "dv_level", map[string]any{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := dolbyVisionRecordFixture()
			record[test.field] = test.value
			if _, err := parseProbe(dolbyVisionFixture(t, "video", record)); err == nil {
				t.Fatal("malformed Dolby Vision configuration was accepted")
			}
		})
	}
}

func TestParseProbeDolbyVisionDoesNotInferConfiguration(t *testing.T) {
	unrelated := map[string]any{
		"side_data_type": "Mastering display metadata",
		"dv_profile":     map[string]any{"unrelated": true},
	}
	wrongType := dolbyVisionRecordFixture()
	wrongType["side_data_type"] = "DOVI metadata"
	for _, test := range []struct {
		name      string
		codecType string
		records   []map[string]any
	}{
		{"no side data", "video", nil},
		{"unrelated side data", "video", []map[string]any{unrelated}},
		{"different DOVI record", "video", []map[string]any{wrongType}},
		{"nonvideo stream", "audio", []map[string]any{dolbyVisionRecordFixture()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := parseDolbyVisionFixture(t, test.codecType, test.records...)
			if stream.DolbyVision != nil || stream.VideoRange != "" || stream.VideoRangeKnown {
				t.Fatalf("Dolby Vision configuration was inferred: %+v", stream)
			}
		})
	}
	stream := parseDolbyVisionFixture(t, "video", unrelated, dolbyVisionRecordFixture())
	if stream.DolbyVision == nil || stream.DolbyVision.Profile != 5 {
		t.Fatalf("unrelated side data obscured the DOVI configuration: %+v", stream)
	}
}

func TestParseProbeDolbyVisionRequiresConsistentCompleteRecords(t *testing.T) {
	conflicting := dolbyVisionRecordFixture()
	conflicting["el_present_flag"] = 1
	incomplete := dolbyVisionRecordFixture()
	delete(incomplete, "el_present_flag")
	for _, test := range []struct {
		name     string
		records  []map[string]any
		complete bool
	}{
		{"identical records", []map[string]any{dolbyVisionRecordFixture(), dolbyVisionRecordFixture()}, true},
		{"conflicting layer flags", []map[string]any{dolbyVisionRecordFixture(), conflicting}, false},
		{"incomplete first", []map[string]any{incomplete, dolbyVisionRecordFixture()}, false},
		{"incomplete last", []map[string]any{dolbyVisionRecordFixture(), incomplete}, false},
		{"conflict followed by matching record", []map[string]any{dolbyVisionRecordFixture(), conflicting, dolbyVisionRecordFixture()}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := parseDolbyVisionFixture(t, "video", test.records...)
			if (stream.DolbyVision != nil) != test.complete || stream.VideoRange != "DOVI" || !stream.VideoRangeKnown {
				t.Fatalf("inconsistent or incomplete records became authoritative: %+v", stream)
			}
		})
	}
}

func TestDolbyVisionLegacyCacheLeavesConfigurationUnknown(t *testing.T) {
	var stream Stream
	if err := json.Unmarshal([]byte(`{"CodecType":"video","Codec":"hevc","VideoRange":"DOVI","VideoRangeKnown":true}`), &stream); err != nil {
		t.Fatal(err)
	}
	if stream.DolbyVision != nil {
		t.Fatalf("legacy snapshot acquired unprobed configuration: %+v", stream.DolbyVision)
	}
}

func TestParseProbeDolbyVisionRejectsMalformedRecordAfterIncompleteRecord(t *testing.T) {
	incomplete := dolbyVisionRecordFixture()
	delete(incomplete, "el_present_flag")
	malformed := dolbyVisionRecordFixture()
	malformed["rpu_present_flag"] = 2
	if _, err := parseProbe(dolbyVisionFixture(t, "video", incomplete, malformed)); err == nil {
		t.Fatal("an incomplete configuration concealed a later malformed record")
	}
}

func dolbyVisionRecordFixture() map[string]any {
	return map[string]any{
		"side_data_type":                "DOVI configuration record",
		"dv_profile":                    5,
		"dv_level":                      "6",
		"rpu_present_flag":              "1",
		"el_present_flag":               0,
		"bl_present_flag":               1,
		"dv_bl_signal_compatibility_id": 0,
	}
}

func parseDolbyVisionFixture(t *testing.T, codecType string, records ...map[string]any) Stream {
	t.Helper()
	info, err := parseProbe(dolbyVisionFixture(t, codecType, records...))
	if err != nil {
		t.Fatal(err)
	}
	return info.Streams[0]
}

func dolbyVisionFixture(t *testing.T, codecType string, records ...map[string]any) []byte {
	t.Helper()
	document := map[string]any{
		"format": map[string]any{"format_name": "matroska,webm"},
		"streams": []map[string]any{{
			"index":            0,
			"codec_type":       codecType,
			"codec_name":       "hevc",
			"codec_tag_string": "dvh1",
			"side_data_list":   records,
		}},
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
