package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func nativeSelectionRuntimeOverrides() RuntimeOverrides {
	return RuntimeOverrides{
		EnableThrottling:      settingsTestPointer(true),
		ExtractionThreadCount: settingsTestPointer(0),
		CodecProfileLimits:    &transcode.CodecProfileLimits{H264: "baseline", HEVC: "main"},
		SubtitleOptions:       &RuntimeSubtitleOptions{AllowAssToText: false},
	}
}

func TestRuntimeNativeSelectionKeepsLegacyDefaultsAndExplicitFalseIndependent(t *testing.T) {
	legacyOptions := RuntimeOptions{Network: NetworkValues{HttpPort: 8096},
		Hardware: HardwareSelection{Decode: "software", Encode: "software"}, Execution: transcode.DefaultExecutionOptions(3)}
	defaults, _, _, _, err := prepareRuntimeOptions([]RuntimeOptions{legacyOptions})
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{runtimeDefaults: defaults}
	inherited := store.runtimeSnapshot(RuntimeOverrides{})
	if inherited.CodecProfileLimits != transcode.DefaultCodecProfileLimits() || !inherited.SubtitleOptions.AllowAssToText ||
		inherited.EnableThrottling || inherited.ExtractionThreadCount != 1 ||
		inherited.Overrides.CodecProfileLimits != nil || inherited.Overrides.SubtitleOptions != nil ||
		inherited.Overrides.EnableThrottling != nil || inherited.Overrides.ExtractionThreadCount != nil {
		t.Fatal("an omitted new group changed historical output compatibility or fabricated provenance")
	}
	previous := nativeSelectionRuntimeOverrides()
	captured := store.runtimeSnapshot(previous)
	if captured.CodecProfileLimits != *previous.CodecProfileLimits || captured.SubtitleOptions.AllowAssToText || !captured.EnableThrottling || captured.ExtractionThreadCount != 0 ||
		captured.Overrides.SubtitleOptions == nil || captured.Overrides.SubtitleOptions.AllowAssToText {
		t.Fatal("an explicit false subtitle preference was interpreted as an absent default")
	}
	previous.CodecProfileLimits.H264 = "high"
	previous.SubtitleOptions.AllowAssToText = true
	*previous.EnableThrottling, *previous.ExtractionThreadCount = false, 16
	if captured.CodecProfileLimits.H264 != "baseline" || captured.Overrides.CodecProfileLimits.H264 != "baseline" ||
		captured.SubtitleOptions.AllowAssToText || captured.Overrides.SubtitleOptions.AllowAssToText ||
		!captured.EnableThrottling || !*captured.Overrides.EnableThrottling || captured.ExtractionThreadCount != 0 || *captured.Overrides.ExtractionThreadCount != 0 {
		t.Fatal("a committed output policy retained caller-owned storage")
	}
	unchanged := applyRuntimeUpdate(captured.Overrides, RuntimeUpdate{})
	if !equalRuntimeOverrides(captured.Overrides, unchanged) {
		t.Fatal("an older omitted update changed new policy provenance")
	}
	cleared := applyRuntimeUpdate(captured.Overrides, RuntimeUpdate{SubtitleOptions: Change[RuntimeSubtitleOptions]{Present: true}})
	after := store.runtimeSnapshot(cleared)
	if after.Overrides.SubtitleOptions != nil || !after.SubtitleOptions.AllowAssToText ||
		after.CodecProfileLimits != captured.CodecProfileLimits || !reflect.DeepEqual(after.Overrides.CodecProfileLimits, captured.Overrides.CodecProfileLimits) {
		t.Fatal("an explicit null failed to restore only the selected subtitle default")
	}
	options := DefaultRuntimeOptions()
	options.SubtitleOptions.AllowAssToText = false
	explicit, _, _, _, err := prepareRuntimeOptions([]RuntimeOptions{options})
	if err != nil || explicit.SubtitleOptions.AllowAssToText {
		t.Fatal("an explicit startup false was normalized to the legacy default")
	}
	options.SubtitleOptions.AllowAssToText = true
	if explicit.SubtitleOptions.AllowAssToText {
		t.Fatal("resolved startup defaults retained a mutable input pointer")
	}
	zero := 0
	options.ExtractionThreadCount = &zero
	explicitAuto, _, _, _, err := prepareRuntimeOptions([]RuntimeOptions{options})
	if err != nil || explicitAuto.ExtractionThreadCount != 0 {
		t.Fatal("explicit automatic image decoder threads were replaced with legacy single-thread defaults")
	}
	zero = 16
	if explicitAuto.ExtractionThreadCount != 0 {
		t.Fatal("resolved image decoder settings retained a caller-owned pointer")
	}
}

func TestRuntimeNativeSelectionRejectsIncompleteGroupsAndUnmarkedValues(t *testing.T) {
	for _, update := range []RuntimeUpdate{
		{CodecProfileLimits: Change[transcode.CodecProfileLimits]{Value: settingsTestPointer(transcode.DefaultCodecProfileLimits())}},
		{SubtitleOptions: Change[RuntimeSubtitleOptions]{Value: settingsTestPointer(DefaultRuntimeSubtitleOptions())}},
		{EnableThrottling: Change[bool]{Value: settingsTestPointer(true)}},
		{ExtractionThreadCount: Change[int]{Value: settingsTestPointer(0)}},
		{ExtractionThreadCount: Change[int]{Present: true, Value: settingsTestPointer(-1)}},
		{ExtractionThreadCount: Change[int]{Present: true, Value: settingsTestPointer(17)}},
		{CodecProfileLimits: Change[transcode.CodecProfileLimits]{Present: true, Value: &transcode.CodecProfileLimits{H264: "main"}}},
		{CodecProfileLimits: Change[transcode.CodecProfileLimits]{Present: true, Value: &transcode.CodecProfileLimits{H264: "high", HEVC: "rext"}}},
	} {
		if err := ValidateRuntimeUpdate(update); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("an unmarked, incomplete, or unsupported profile policy entered managed settings")
		}
	}
	if err := ValidateRuntimeUpdate(RuntimeUpdate{SubtitleOptions: Change[RuntimeSubtitleOptions]{Present: true, Value: &RuntimeSubtitleOptions{AllowAssToText: false}}}); err != nil {
		t.Fatalf("the complete explicit false subtitle group was rejected: %v", err)
	}
}

func TestStoredRuntimeNativeSelectionReadsHistoricalShapesWithoutRewritingFacts(t *testing.T) {
	const legacySeven = `{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}`
	const legacyEleven = `{"Network":null,"Hardware":null,"Threads":null,"H264":{"Preset":"slower","RateControl":"bitrate","CRF":1,"ThreadCountSet":true},"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null,"Transcoding":null,"TranscodingTempPath":null,"SoftwareToneMapOptions":null,"CodecConfigurations":null}`
	for _, raw := range []string{legacySeven, legacyEleven} {
		decoded, err := decodeStoredRuntime([]byte(raw))
		if err != nil || decoded.CodecProfileLimits != nil || decoded.SubtitleOptions != nil || decoded.EnableThrottling != nil || decoded.ExtractionThreadCount != nil {
			t.Fatalf("historical source facts were rejected or backfilled with new overrides: %v", err)
		}
	}
	current, err := json.Marshal(nativeSelectionRuntimeOverrides())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredRuntime(current)
	if err != nil || !equalRuntimeOverrides(decoded, nativeSelectionRuntimeOverrides()) {
		t.Fatalf("current persistence did not retain a complete selected policy: %v", err)
	}
	var groups map[string]json.RawMessage
	if json.Unmarshal(current, &groups) != nil || len(groups) != 15 {
		t.Fatal("new persistence did not use the complete fifteen-group contract")
	}
	for _, raw := range []string{
		strings.Replace(string(current), `"CodecProfileLimits":{"H264":"baseline","HEVC":"main"}`, `"CodecProfileLimits":{"H264":"baseline"}`, 1),
		strings.Replace(string(current), `"H264":"baseline"`, `"H264":null`, 1),
		strings.Replace(string(current), `"H264":"baseline"`, `"H264":"baseline","H264":"main"`, 1),
		strings.Replace(string(current), `"AllowAssToText":false`, `"AllowAssToText":null`, 1),
		strings.Replace(string(current), `"AllowAssToText":false`, `"AllowAssToText":"false"`, 1),
		strings.Replace(string(current), `"AllowAssToText":false`, `"AllowAssToText":false,"Style":"private-marker"`, 1),
		strings.Replace(string(current), `"EnableThrottling":true`, `"EnableThrottling":"true"`, 1),
		strings.Replace(string(current), `"ExtractionThreadCount":0`, `"ExtractionThreadCount":17`, 1),
		strings.Replace(string(current), `"ExtractionThreadCount":0`, `"ExtractionThreadCount":0.0`, 1),
		strings.Replace(string(current), `,"SubtitleOptions":{"AllowAssToText":false}`, "", 1),
	} {
		if err := ValidateStoredRuntimeOverrides([]byte(raw)); !errors.Is(err, ErrStoredSettings) {
			t.Fatal("an ambiguous or incomplete selected policy passed the raw archive gate")
		}
	}
}

func nativeSelectionAllResetFields() []Field {
	return []Field{FieldServerName, FieldMaxBitrate, FieldMaxWidth, FieldMaxHeight, FieldMaxAudioChannels, FieldTranscodingMaxWidth,
		FieldManagement, FieldMetadata, FieldSubtitles, FieldTasks, FieldSorting, FieldRuntime, FieldNetwork, FieldHardware, FieldThreads,
		FieldH264, FieldHEVC, FieldSoftwareToneMapping, FieldVulkanToneMapping, FieldTranscoding, FieldTranscodingTempPath,
		FieldSoftwareToneMapOptions, FieldCodecConfigurations, FieldEnableThrottling, FieldExtractionThreadCount, FieldCodecProfileLimits, FieldRuntimeSubtitleOptions}
}

func TestRuntimeNativeSelectionResetAdmitsTheCompleteClosedRegistry(t *testing.T) {
	fields := nativeSelectionAllResetFields()
	if len(fields) != 27 {
		t.Fatal("the reset fixture does not enumerate all twenty-seven supported settings")
	}
	if MaxResetFields != len(fields) {
		t.Fatal("the reset request bound does not match the complete supported registry")
	}
	if err := validateReset(fields); err != nil {
		t.Fatalf("a valid reset across all supported groups was rejected: %v", err)
	}
	if err := validateReset(append(fields, FieldRuntime)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("an oversized duplicate reset escaped the complete registry bound")
	}
	cleared := clearRuntimeFields(nativeSelectionRuntimeOverrides(), []Field{FieldEnableThrottling, FieldExtractionThreadCount})
	if cleared.EnableThrottling != nil || cleared.ExtractionThreadCount != nil || cleared.CodecProfileLimits == nil || cleared.SubtitleOptions == nil {
		t.Fatal("a selected control reset crossed independent profile and subtitle groups")
	}
}
