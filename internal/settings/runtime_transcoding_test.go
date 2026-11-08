package settings

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func transcodingRuntimeTestOverrides() RuntimeOverrides {
	policy := DefaultTranscodingPolicy()
	policy.EnableHevcEncoding, policy.EnableHardwareEncoding = false, false
	policy.DownMixAudioBoost = 2
	policy.EnableSubtitleExtraction, policy.EnableOnTheFlyAttachmentExtraction = false, false
	codecs := transcode.DefaultCodecConfigurations()
	codecs[0].Priority, codecs[1].Priority = -1000, 1000
	return RuntimeOverrides{
		H264:        &transcode.CPUQuality{Preset: "slower", RateControl: "capped_crf", CRF: 1, ThreadCountSet: true, X264Options: "aq-strength=1.25:subme=8"},
		HEVC:        &transcode.CPUQuality{Preset: "placebo", RateControl: "capped_crf", CRF: 51, ThreadCount: 16, ThreadCountSet: true},
		Transcoding: &policy, TranscodingTempPath: settingsTestPointer("/srv/private-transcodes"),
		SoftwareToneMapping: settingsTestPointer(true), VulkanToneMapping: settingsTestPointer(true),
		SoftwareToneMapOptions: &transcode.SoftwareToneMapOptions{ToneMapSizeLimit: "Limit720", SoftwareToneMapAlgorithm: "Reinhard"},
		CodecConfigurations:    &codecs,
	}
}

func TestRuntimeTranscodingCapturesResolvedChoicesAndOwnsOverrideGroups(t *testing.T) {
	options := DefaultRuntimeOptions()
	options.TranscodingTempPath = "/srv/deployment-transcodes"
	defaults, _, _, _, err := prepareRuntimeOptions([]RuntimeOptions{options})
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{runtimeDefaults: defaults}
	initial := store.runtimeSnapshot(RuntimeOverrides{})
	if initial.Transcoding != DefaultTranscodingPolicy() || initial.Execution.DownMixAudioBoost != 1 ||
		initial.DesiredTranscodingTempPath != options.TranscodingTempPath || initial.CodecConfigurations != transcode.DefaultCodecConfigurations() {
		t.Fatal("legacy defaults changed without an explicit new policy")
	}
	overrides := transcodingRuntimeTestOverrides()
	captured := store.runtimeSnapshot(overrides)
	if captured.Transcoding.EnableHevcEncoding || captured.Transcoding.EnableHardwareEncoding ||
		captured.Transcoding.EnableSubtitleExtraction || captured.Transcoding.EnableOnTheFlyAttachmentExtraction ||
		captured.Execution.DownMixAudioBoost != 2 || captured.Execution.H264.ThreadCount != 0 || !captured.Execution.H264.ThreadCountSet ||
		captured.Execution.HEVC.ThreadCount != 16 || captured.Execution.SoftwareToneMapOptions != *overrides.SoftwareToneMapOptions ||
		captured.CodecConfigurations != *overrides.CodecConfigurations || captured.DesiredTranscodingTempPath != "/srv/private-transcodes" {
		t.Fatal("a runtime snapshot lost explicit false, automatic threads, or an active execution preference")
	}
	overrides.Transcoding.DownMixAudioBoost = 3
	*overrides.TranscodingTempPath = "/srv/changed"
	overrides.SoftwareToneMapOptions.SoftwareToneMapAlgorithm = "Mobius"
	overrides.CodecConfigurations[0].Priority = 7
	if captured.Transcoding.DownMixAudioBoost != 2 || captured.Overrides.Transcoding.DownMixAudioBoost != 2 ||
		captured.DesiredTranscodingTempPath != "/srv/private-transcodes" || *captured.Overrides.TranscodingTempPath != "/srv/private-transcodes" ||
		captured.Overrides.SoftwareToneMapOptions.SoftwareToneMapAlgorithm != "Reinhard" || captured.CodecConfigurations[0].Priority != -1000 {
		t.Fatal("a captured value or override retained caller-owned storage")
	}
	empty := ""
	inherited := store.runtimeSnapshot(RuntimeOverrides{TranscodingTempPath: &empty})
	if inherited.DesiredTranscodingTempPath != options.TranscodingTempPath || inherited.Overrides.TranscodingTempPath == nil {
		t.Fatal("an explicit empty path lost its provenance or did not select the deployment default")
	}
	cleared := clearRuntimeFields(captured.Overrides, []Field{FieldTranscoding, FieldTranscodingTempPath, FieldSoftwareToneMapOptions, FieldCodecConfigurations})
	if cleared.Transcoding != nil || cleared.TranscodingTempPath != nil || cleared.SoftwareToneMapOptions != nil || cleared.CodecConfigurations != nil ||
		!reflect.DeepEqual(cleared.H264, captured.Overrides.H264) || !reflect.DeepEqual(cleared.HEVC, captured.Overrides.HEVC) {
		t.Fatal("a selected advanced reset changed unrelated codec preferences")
	}
}

func TestRuntimeTranscodingCanonicalizesOnlyChangedInputs(t *testing.T) {
	quality := transcode.CPUQuality{Preset: "fast", RateControl: "capped_crf", CRF: 17, X264Options: "subme=8:aq_strength=1.250"}
	codecs := transcode.DefaultCodecConfigurations()
	codecs[0], codecs[1] = codecs[1], codecs[0]
	update := RuntimeUpdate{
		H264:                Change[transcode.CPUQuality]{Present: true, Value: &quality},
		CodecConfigurations: Change[transcode.CodecConfigurations]{Present: true, Value: &codecs},
	}
	result := applyRuntimeUpdate(RuntimeOverrides{}, update)
	if result.H264.X264Options != "aq-strength=1.25:subme=8" || result.CodecConfigurations[0].CodecId != transcode.H264SoftwareCodecID ||
		quality.X264Options != "subme=8:aq_strength=1.250" || codecs[0].CodecId != transcode.HEVCSoftwareCodecID {
		t.Fatal("canonicalization changed its caller's input or retained unrelated ordering")
	}
	previous := RuntimeOverrides{H264: &quality}
	unchanged := applyRuntimeUpdate(previous, RuntimeUpdate{})
	if !equalRuntimeOverrides(previous, unchanged) {
		t.Fatal("an omitted group normalized stored provenance")
	}
	for _, boost := range []float64{0, 0.09, 10.01, math.NaN(), math.Inf(1)} {
		policy := DefaultTranscodingPolicy()
		policy.DownMixAudioBoost = boost
		if err := ValidateRuntimeUpdate(RuntimeUpdate{Transcoding: Change[TranscodingPolicy]{Present: true, Value: &policy}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("an unbounded or non-finite audio boost entered managed settings")
		}
	}
	for _, invalid := range []RuntimeUpdate{
		{Transcoding: Change[TranscodingPolicy]{Value: settingsTestPointer(DefaultTranscodingPolicy())}},
		{TranscodingTempPath: Change[string]{Value: settingsTestPointer("/srv/cache")}},
		{SoftwareToneMapOptions: Change[transcode.SoftwareToneMapOptions]{Value: settingsTestPointer(transcode.DefaultSoftwareToneMapOptions())}},
		{CodecConfigurations: Change[transcode.CodecConfigurations]{Value: settingsTestPointer(transcode.DefaultCodecConfigurations())}},
	} {
		if err := ValidateRuntimeUpdate(invalid); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("an advanced value without its explicit change marker was accepted")
		}
	}
}

func TestStoredRuntimeTranscodingSeparatesLegacyAndCurrentExactShapes(t *testing.T) {
	const legacy = `{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}`
	if value, err := decodeStoredRuntime([]byte(legacy)); err != nil || value != (RuntimeOverrides{}) {
		t.Fatal("the historical seven-field empty object was no longer readable")
	}
	current, err := json.Marshal(transcodingRuntimeTestOverrides())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(current, &fields) != nil || len(fields) != 15 {
		t.Fatal("new persistence did not emit a complete fifteen-group object")
	}
	decoded, err := decodeStoredRuntime(current)
	if err != nil || !reflect.DeepEqual(decoded, transcodingRuntimeTestOverrides()) {
		t.Fatalf("new persistence lost automatic threads or complete advanced groups: %v", err)
	}
	replaceQuality := func(value string) string {
		raw, _ := json.Marshal(RuntimeOverrides{})
		return strings.Replace(string(raw), `"H264":null`, `"H264":`+value, 1)
	}
	for _, raw := range []string{
		strings.Replace(legacy, `"H264":null`, `"H264":{"Preset":"fast","RateControl":"bitrate","CRF":1}`, 1),
		strings.Replace(legacy, `"H264":null`, `"H264":{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCountSet":true}`, 1),
		strings.Replace(legacy, `"Network":null`, `"Network":null,"Transcoding":null`, 1),
		strings.Replace(string(current), `,"CodecConfigurations":`+string(fields["CodecConfigurations"]), "", 1),
		replaceQuality(`{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCount":0}`),
		replaceQuality(`{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCountSet":false}`),
		replaceQuality(`{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCountSet":null}`),
		replaceQuality(`{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCountSet":true,"ThreadCount":null}`),
		replaceQuality(`{"Preset":"fast","RateControl":"bitrate","CRF":23,"X264Options":"stats=/private-marker"}`),
		strings.Replace(string(current), `"CodecId":"V-E-libx264"`, `"CodecId":null`, 1),
		strings.Replace(string(current), `"CodecId":"V-E-libx264"`, `"CodecId":"V-E-libx264","CodecId":"V-E-libx264"`, 1),
		strings.Replace(string(current), `"SoftwareToneMapAlgorithm":"Reinhard"`, `"SoftwareToneMapAlgorithm":"Bt2390"`, 1),
	} {
		if _, err := decodeStoredRuntime([]byte(raw)); !errors.Is(err, ErrStoredSettings) || errors.Is(err, ErrInvalidInput) {
			t.Fatalf("an ambiguous advanced object passed the archive gate: %v", err)
		}
	}
}

func TestRuntimeTranscodingPathValidationUsesFrozenExcludedRoots(t *testing.T) {
	store := &Store{
		runtimeDefaults:               RuntimeValues{TranscodingTempPath: "/srv/transcodes"},
		transcodingTempPathExclusions: []string{"/srv/media", "/srv/scratch", "/var/log/goby"},
	}
	for _, path := range []string{"/srv/media", "/srv/media/child", "/srv", "/var/log/goby/child"} {
		if err := store.validateTranscodingTempPath(RuntimeOverrides{TranscodingTempPath: &path}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("a requested cache could overlap a reserved deployment root")
		}
	}
	for _, path := range []string{"", "/srv/media-copy", "/srv/private-cache"} {
		if err := store.validateTranscodingTempPath(RuntimeOverrides{TranscodingTempPath: &path}); err != nil {
			t.Fatalf("a distinct or inherited cache path was rejected: %v", err)
		}
	}
}

func TestConfigurationTranscodingPreservesOmittedGroupsAndRoundTripProvenance(t *testing.T) {
	options := DefaultRuntimeOptions()
	options.TranscodingTempPath = "/srv/default-cache"
	defaults, _, _, _, err := prepareRuntimeOptions([]RuntimeOptions{options})
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{runtimeDefaults: defaults}
	previous := transcodingRuntimeTestOverrides()
	if actual := store.applyRuntimeConfiguration(previous, ConfigurationMutation{Section: ConfigurationPartial}); !equalRuntimeOverrides(previous, actual) {
		t.Fatal("an older empty compatibility writer changed advanced controls")
	}
	policy := previous.Transcoding
	current := store.runtimeSnapshot(previous)
	quality := current.Execution.H264
	roundTrip := ConfigurationMutation{
		Section: ConfigurationEncoding, H264CrfPresent: true, H264Crf: quality.CRF, H264Preset: &quality.Preset,
		EnableSoftwareToneMapping: &current.Execution.SoftwareToneMapping, EnableHardwareToneMapping: &current.Execution.VulkanToneMapping,
		EnableHevcEncoding: &policy.EnableHevcEncoding, EnableHardwareEncoding: &policy.EnableHardwareEncoding,
		DownMixAudioBoost: &policy.DownMixAudioBoost, EnableSubtitleExtraction: &policy.EnableSubtitleExtraction,
		EnableOnTheFlyAttachmentExtraction: &policy.EnableOnTheFlyAttachmentExtraction,
		TranscodingTempPath:                &current.DesiredTranscodingTempPath, CodecConfigurations: &current.CodecConfigurations,
	}
	if actual := store.applyRuntimeConfiguration(previous, roundTrip); !equalRuntimeOverrides(previous, actual) {
		t.Fatal("a native configuration round trip changed an advanced override's provenance")
	}
	empty := RuntimeOverrides{}
	inherited := store.runtimeSnapshot(empty)
	unchanged := store.applyRuntimeConfiguration(empty, ConfigurationMutation{
		Section: ConfigurationPartial, H264Preset: &inherited.Execution.H264.Preset,
		EnableHevcEncoding: &inherited.Transcoding.EnableHevcEncoding, EnableHardwareEncoding: &inherited.Transcoding.EnableHardwareEncoding,
		DownMixAudioBoost: &inherited.Transcoding.DownMixAudioBoost, EnableSubtitleExtraction: &inherited.Transcoding.EnableSubtitleExtraction,
		EnableOnTheFlyAttachmentExtraction: &inherited.Transcoding.EnableOnTheFlyAttachmentExtraction,
		TranscodingTempPath:                &inherited.DesiredTranscodingTempPath, CodecConfigurations: &inherited.CodecConfigurations,
	})
	if !equalRuntimeOverrides(empty, unchanged) {
		t.Fatal("equal compatibility values turned inherited defaults into database overrides")
	}
}
