package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestAdminRuntimeNativeSelectionKeepsOmittedGroupsAndExplicitFalse(t *testing.T) {
	invalid := make(map[string]string)
	update := decodeAdminRuntimeSettings(json.RawMessage(`{"EnableThrottling":true,"ExtractionThreadCount":0,"CodecProfileLimits":{"H264":"baseline","HEVC":"main"},"SubtitleOptions":{"AllowAssToText":false}}`), invalid)
	if len(invalid) != 0 || update == nil || !update.CodecProfileLimits.Present || update.CodecProfileLimits.Value == nil ||
		*update.CodecProfileLimits.Value != (transcode.CodecProfileLimits{H264: "baseline", HEVC: "main"}) ||
		!update.SubtitleOptions.Present || update.SubtitleOptions.Value == nil || update.SubtitleOptions.Value.AllowAssToText ||
		!update.EnableThrottling.Present || update.EnableThrottling.Value == nil || !*update.EnableThrottling.Value ||
		!update.ExtractionThreadCount.Present || update.ExtractionThreadCount.Value == nil || *update.ExtractionThreadCount.Value != 0 {
		t.Fatalf("a complete native selection lost its profile ceiling or explicit false: %v", invalid)
	}
	older := decodeAdminRuntimeSettings(json.RawMessage(`{"Threads":3}`), invalid)
	if older == nil || older.CodecProfileLimits.Present || older.SubtitleOptions.Present || older.EnableThrottling.Present || older.ExtractionThreadCount.Present || len(invalid) != 0 {
		t.Fatal("an older writer implicitly cleared a selected native output policy")
	}
	cleared := decodeAdminRuntimeSettings(json.RawMessage(`{"CodecProfileLimits":null,"SubtitleOptions":null}`), invalid)
	if cleared == nil || !cleared.CodecProfileLimits.Present || cleared.CodecProfileLimits.Value != nil ||
		!cleared.SubtitleOptions.Present || cleared.SubtitleOptions.Value != nil || cleared.Threads.Present || len(invalid) != 0 {
		t.Fatal("independent native group resets changed an omitted execution setting")
	}
}

func TestAdminRuntimeNativeSelectionRejectsAmbiguousAndUnsupportedGroups(t *testing.T) {
	for _, test := range []struct{ raw, field string }{
		{`{"EnableThrottling":1}`, "Runtime.EnableThrottling"},
		{`{"ExtractionThreadCount":-1}`, "Runtime.ExtractionThreadCount"},
		{`{"ExtractionThreadCount":17}`, "Runtime.ExtractionThreadCount"},
		{`{"ExtractionThreadCount":0.0}`, "Runtime.ExtractionThreadCount"},
		{`{"CodecProfileLimits":{"H264":"high"}}`, "Runtime.CodecProfileLimits"},
		{`{"CodecProfileLimits":{"H264":"high","HEVC":"rext"}}`, "Runtime.CodecProfileLimits"},
		{`{"CodecProfileLimits":{"H264":null,"HEVC":"main"}}`, "Runtime.CodecProfileLimits"},
		{`{"CodecProfileLimits":{"H264":"baseline","H264":"high","HEVC":"main"}}`, "Runtime.CodecProfileLimits"},
		{`{"CodecProfileLimits":{"H264":"baseline","HEVC":"main","Profile":"private-marker"}}`, "Runtime.CodecProfileLimits"},
		{`{"SubtitleOptions":{}}`, "Runtime.SubtitleOptions.AllowAssToText"},
		{`{"SubtitleOptions":{"AllowAssToText":null}}`, "Runtime.SubtitleOptions.AllowAssToText"},
		{`{"SubtitleOptions":{"AllowAssToText":"false"}}`, "Runtime.SubtitleOptions.AllowAssToText"},
		{`{"SubtitleOptions":{"AllowAssToText":false,"AllowAssToText":true}}`, "Runtime.SubtitleOptions.AllowAssToText"},
		{`{"SubtitleOptions":{"AllowAssToText":false,"Style":"private-marker"}}`, "Runtime.SubtitleOptions"},
	} {
		response := httptest.NewRecorder()
		body := adminSettingsExtendedUpdateBodyForTest(`"Runtime":` + test.raw)
		if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json")); ok {
			t.Fatal("an ambiguous or unsupported native selection entered the update request")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, test.field)
	}
}

func TestAdminRuntimeNativeSelectionReportsResolvedPolicyAndProvenance(t *testing.T) {
	defaults := settings.DefaultRuntimeOptions()
	profile := transcode.CodecProfileLimits{H264: "main", HEVC: "main"}
	subtitles := settings.RuntimeSubtitleOptions{AllowAssToText: false}
	value := settings.RuntimeSnapshot{
		Defaults: settings.RuntimeValues{Execution: defaults.Execution, CodecConfigurations: defaults.CodecConfigurations,
			CodecProfileLimits: defaults.CodecProfileLimits, SubtitleOptions: *defaults.SubtitleOptions,
			EnableThrottling: defaults.EnableThrottling, ExtractionThreadCount: *defaults.ExtractionThreadCount},
		Overrides: settings.RuntimeOverrides{CodecProfileLimits: &profile, SubtitleOptions: &subtitles},
		Execution: defaults.Execution, CodecConfigurations: defaults.CodecConfigurations, CodecProfileLimits: profile, SubtitleOptions: subtitles,
	}
	dto := settingsRuntimeDTO(value)
	for _, test := range []struct{ field, applicability string }{{"CodecProfileLimits", "new_video_encoding"}, {"SubtitleOptions", "ass_text_negotiation"}} {
		if dto["Sources"].(map[string]any)[test.field] != "database" || dto["Effects"].(map[string]any)[test.field] != "next_admission" ||
			dto["Applicability"].(map[string]any)[test.field] != test.applicability {
			t.Fatal("native output policy has inaccurate provenance or applicability")
		}
	}
	if !reflect.DeepEqual(dto["Effective"].(map[string]any)["CodecProfileLimits"], profile) ||
		!reflect.DeepEqual(dto["Effective"].(map[string]any)["SubtitleOptions"], subtitles) ||
		!reflect.DeepEqual(dto["Defaults"].(map[string]any)["SubtitleOptions"], *defaults.SubtitleOptions) {
		t.Fatal("an explicit false or profile ceiling was relabeled as a deployment default")
	}
}

func TestAdminRuntimeNativeSelectionResetAllowsAllSupportedGroups(t *testing.T) {
	fields := []settings.Field{settings.FieldServerName, settings.FieldMaxBitrate, settings.FieldMaxWidth, settings.FieldMaxHeight, settings.FieldMaxAudioChannels,
		settings.FieldTranscodingMaxWidth, settings.FieldManagement, settings.FieldMetadata, settings.FieldSubtitles, settings.FieldTasks, settings.FieldSorting,
		settings.FieldRuntime, settings.FieldNetwork, settings.FieldHardware, settings.FieldThreads, settings.FieldH264, settings.FieldHEVC,
		settings.FieldSoftwareToneMapping, settings.FieldVulkanToneMapping, settings.FieldTranscoding, settings.FieldTranscodingTempPath,
		settings.FieldSoftwareToneMapOptions, settings.FieldCodecConfigurations, settings.FieldEnableThrottling, settings.FieldExtractionThreadCount,
		settings.FieldCodecProfileLimits, settings.FieldRuntimeSubtitleOptions}
	if len(fields) != 27 {
		t.Fatal("the reset fixture is not the complete closed registry")
	}
	body, err := json.Marshal(map[string]any{"Revision": "9007199254740993", "Fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(string(body), "application/json"))
	if !ok || request.Revision != 9007199254740993 || !reflect.DeepEqual(request.Fields, fields) {
		t.Fatal("a legal complete reset was rejected by an obsolete request bound")
	}
	body, _ = json.Marshal(map[string]any{"Revision": "1", "Fields": append(fields, settings.FieldRuntime)})
	response = httptest.NewRecorder()
	if _, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(string(body), "application/json")); ok {
		t.Fatal("an oversized duplicate reset escaped the closed request bound")
	}
}
