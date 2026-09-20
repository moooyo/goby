package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func runtimeTestOverrides() RuntimeOverrides {
	return RuntimeOverrides{
		Network:             &NetworkOverrides{BindHost: settingsTestPointer("127.0.0.1"), HttpPort: settingsTestPointer(9096)},
		Hardware:            &HardwareSelection{Decode: "vaapi", Encode: "software", DeviceID: "device-alpha"},
		Threads:             settingsTestPointer(6),
		H264:                &transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 20},
		HEVC:                &transcode.CPUQuality{Preset: "medium", RateControl: "bitrate", CRF: 30},
		SoftwareToneMapping: settingsTestPointer(false),
		VulkanToneMapping:   settingsTestPointer(true),
	}
}

func TestRuntimeUpdatePresencePreservesClearsAndReplacesWholeGroups(t *testing.T) {
	previous := runtimeTestOverrides()
	preserved := applyRuntimeUpdate(previous, RuntimeUpdate{})
	if !equalRuntimeOverrides(preserved, previous) {
		t.Fatal("omitted runtime groups did not preserve their previous overrides")
	}
	change := RuntimeUpdate{
		Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{HttpPort: settingsTestPointer(10096)}},
		Threads: Change[int]{Present: true},
		H264:    Change[transcode.CPUQuality]{Present: true, Value: &transcode.CPUQuality{Preset: "fast", RateControl: "bitrate", CRF: 31}},
	}
	next := applyRuntimeUpdate(previous, change)
	if next.Network == nil || next.Network.BindHost != nil || next.Network.HttpPort == nil || *next.Network.HttpPort != 10096 || next.Threads != nil ||
		next.H264 == nil || *next.H264 != *change.H264.Value || !equalPointer(next.Hardware, previous.Hardware) ||
		!equalPointer(next.HEVC, previous.HEVC) || !equalPointer(next.SoftwareToneMapping, previous.SoftwareToneMapping) ||
		!equalPointer(next.VulkanToneMapping, previous.VulkanToneMapping) {
		t.Fatal("runtime update merged a whole group or changed an omitted group")
	}
	*change.Network.Value.HttpPort = 1
	change.H264.Value.CRF = 35
	*next.HEVC = transcode.CPUQuality{}
	if *next.Network.HttpPort != 10096 || next.H264.CRF != 31 || previous.HEVC.CRF != 30 || previous.Threads == nil || *previous.Threads != 6 {
		t.Fatal("runtime changes shared mutable input or previous-state pointers")
	}
	if cleared := applyRuntimeUpdate(previous, ResetRuntimeUpdate()); cleared != (RuntimeOverrides{}) {
		t.Fatal("the explicit runtime reset did not clear every override group")
	}
	groupOnly := applyRuntimeUpdate(RuntimeOverrides{}, RuntimeUpdate{
		Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{}},
	})
	if groupOnly.Network == nil || equalRuntimeOverrides(groupOnly, RuntimeOverrides{}) {
		t.Fatal("an explicit empty network group was collapsed into absent provenance")
	}
}

func TestRuntimePublicationOwnsEveryNestedOverridePointer(t *testing.T) {
	store := &Store{}
	input := runtimeTestOverrides()
	want := runtimeTestOverrides()
	published := store.publish(Snapshot{Revision: 2, Runtime: RuntimeSnapshot{Overrides: input}})
	*input.Network.BindHost = "::1"
	*input.Network.HttpPort = 1
	input.Hardware.DeviceID = "device-beta"
	*input.Threads = 1
	input.H264.Preset = "fast"
	input.HEVC.CRF = 35
	*input.SoftwareToneMapping = true
	*input.VulkanToneMapping = false
	if !equalRuntimeOverrides(store.Snapshot().Runtime.Overrides, want) {
		t.Fatal("runtime publication retained caller-owned nested override pointers")
	}
	*published.Runtime.Overrides.Network.BindHost = "192.0.2.1"
	*published.Runtime.Overrides.Network.HttpPort = 2
	published.Runtime.Overrides.Hardware.Encode = "vaapi"
	*published.Runtime.Overrides.Threads = 2
	published.Runtime.Overrides.H264.CRF = 35
	published.Runtime.Overrides.HEVC.Preset = "fast"
	*published.Runtime.Overrides.SoftwareToneMapping = true
	*published.Runtime.Overrides.VulkanToneMapping = false
	current := store.Snapshot()
	if !equalRuntimeOverrides(current.Runtime.Overrides, want) {
		t.Fatal("the returned publication exposed stored runtime pointers")
	}
	current.Runtime.Overrides.Hardware.DeviceID = "device-gamma"
	older := store.publish(Snapshot{Revision: 1, Runtime: RuntimeSnapshot{Overrides: input}})
	if older.Revision != 2 || !equalRuntimeOverrides(older.Runtime.Overrides, want) ||
		!equalRuntimeOverrides(store.Snapshot().Runtime.Overrides, want) {
		t.Fatal("a snapshot reader or delayed publication changed current runtime settings")
	}
}

func TestRuntimeUpdateCloneOwnsInputGroups(t *testing.T) {
	values := runtimeTestOverrides()
	input := RuntimeUpdate{
		Network:             Change[NetworkOverrides]{Present: true, Value: values.Network},
		Hardware:            Change[HardwareSelection]{Present: true, Value: values.Hardware},
		Threads:             Change[int]{Present: true, Value: values.Threads},
		H264:                Change[transcode.CPUQuality]{Present: true, Value: values.H264},
		HEVC:                Change[transcode.CPUQuality]{Present: true, Value: values.HEVC},
		SoftwareToneMapping: Change[bool]{Present: true, Value: values.SoftwareToneMapping},
		VulkanToneMapping:   Change[bool]{Present: true, Value: values.VulkanToneMapping},
	}
	copy := cloneRuntimeUpdate(input)
	*input.Network.Value.BindHost = "::1"
	*input.Network.Value.HttpPort = 1
	input.Hardware.Value.DeviceID = "device-beta"
	*input.Threads.Value = 1
	input.H264.Value.CRF = 35
	input.HEVC.Value.CRF = 35
	*input.SoftwareToneMapping.Value = true
	*input.VulkanToneMapping.Value = false
	if !equalRuntimeOverrides(applyRuntimeUpdate(RuntimeOverrides{}, copy), runtimeTestOverrides()) {
		t.Fatal("runtime request cloning retained caller-owned pointers")
	}
}

func TestRuntimeValidationAcceptsOnlyManagedCanonicalValues(t *testing.T) {
	for _, host := range []string{"", "0.0.0.0", "127.0.0.1", "::", "::1", "2001:db8::1"} {
		change := RuntimeUpdate{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{BindHost: settingsTestPointer(host)}}}
		if err := ValidateRuntimeUpdate(change); err != nil {
			t.Errorf("canonical managed bind host %q was rejected: %v", host, err)
		}
	}
	for _, mode := range []string{"bitrate", "capped_crf"} {
		for _, preset := range []string{"veryfast", "fast", "medium", "slow"} {
			for _, crf := range []int{18, 35} {
				quality := transcode.CPUQuality{Preset: preset, RateControl: mode, CRF: crf}
				change := RuntimeUpdate{H264: Change[transcode.CPUQuality]{Present: true, Value: &quality}, HEVC: Change[transcode.CPUQuality]{Present: true, Value: &quality}}
				if err := ValidateRuntimeUpdate(change); err != nil {
					t.Errorf("supported codec quality %+v was rejected: %v", quality, err)
				}
			}
		}
	}
	for _, decode := range []string{"software", "vaapi"} {
		for _, encode := range []string{"software", "vaapi"} {
			hardware := HardwareSelection{Decode: decode, Encode: encode, DeviceID: "opaque-device_1"}
			if err := ValidateRuntimeUpdate(RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &hardware}}); err != nil {
				t.Errorf("valid two-axis hardware shape %+v was rejected: %v", hardware, err)
			}
		}
	}
	for _, change := range []RuntimeUpdate{
		{}, ResetRuntimeUpdate(),
		{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{}}},
		{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{HttpPort: settingsTestPointer(1)}}},
		{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{HttpPort: settingsTestPointer(65535)}}},
		{Threads: Change[int]{Present: true, Value: settingsTestPointer(1)}},
		{Threads: Change[int]{Present: true, Value: settingsTestPointer(64)}},
		{SoftwareToneMapping: Change[bool]{Present: true, Value: settingsTestPointer(false)}},
		{VulkanToneMapping: Change[bool]{Present: true, Value: settingsTestPointer(false)}},
	} {
		if err := ValidateRuntimeUpdate(change); err != nil {
			t.Errorf("valid presence or boundary value was rejected: %v", err)
		}
	}
	invalid := []struct {
		name   string
		change RuntimeUpdate
	}{
		{"zero-port", RuntimeUpdate{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{HttpPort: settingsTestPointer(0)}}}},
		{"large-port", RuntimeUpdate{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{HttpPort: settingsTestPointer(65536)}}}},
		{"zero-threads", RuntimeUpdate{Threads: Change[int]{Present: true, Value: settingsTestPointer(0)}}},
		{"large-threads", RuntimeUpdate{Threads: Change[int]{Present: true, Value: settingsTestPointer(65)}}},
		{"unknown-decode", RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &HardwareSelection{Decode: "cuda", Encode: "software"}}}},
		{"unknown-encode", RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &HardwareSelection{Decode: "software", Encode: "qsv"}}}},
		{"missing-device", RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &HardwareSelection{Decode: "vaapi", Encode: "software"}}}},
		{"device-path", RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &HardwareSelection{Decode: "vaapi", Encode: "software", DeviceID: "/dev/dri/renderD128"}}}},
		{"device-environment", RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &HardwareSelection{Decode: "vaapi", Encode: "software", DeviceID: "LIBVA_DRIVER_NAME=radeonsi"}}}},
		{"h264-preset", RuntimeUpdate{H264: Change[transcode.CPUQuality]{Present: true, Value: &transcode.CPUQuality{Preset: "ultrafast", RateControl: "bitrate", CRF: 23}}}},
		{"hevc-rate-control", RuntimeUpdate{HEVC: Change[transcode.CPUQuality]{Present: true, Value: &transcode.CPUQuality{Preset: "fast", RateControl: "crf", CRF: 28}}}},
		{"h264-low-crf", RuntimeUpdate{H264: Change[transcode.CPUQuality]{Present: true, Value: &transcode.CPUQuality{Preset: "fast", RateControl: "bitrate", CRF: 17}}}},
		{"hevc-high-crf", RuntimeUpdate{HEVC: Change[transcode.CPUQuality]{Present: true, Value: &transcode.CPUQuality{Preset: "fast", RateControl: "bitrate", CRF: 36}}}},
		{"unmarked-network", RuntimeUpdate{Network: Change[NetworkOverrides]{Value: &NetworkOverrides{}}}},
		{"unmarked-threads", RuntimeUpdate{Threads: Change[int]{Value: settingsTestPointer(4)}}},
		{"unmarked-false", RuntimeUpdate{SoftwareToneMapping: Change[bool]{Value: settingsTestPointer(false)}}},
	}
	for _, host := range []string{"localhost", " 127.0.0.1", "127.000.0.1", "127.0.0.1:8096", "[::1]", "2001:0db8::1", "FE80::1", "fe80::1%eth0"} {
		invalid = append(invalid, struct {
			name   string
			change RuntimeUpdate
		}{"host-" + host, RuntimeUpdate{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{BindHost: settingsTestPointer(host)}}}})
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRuntimeUpdate(test.change); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("unsupported managed runtime value returned %v", err)
			}
		})
	}
}

func TestStoredRuntimeRequiresExactNullableGroupsAndCompleteValues(t *testing.T) {
	const empty = `{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}`
	for _, want := range []RuntimeOverrides{{}, {Network: &NetworkOverrides{}}, runtimeTestOverrides()} {
		encoded, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		value, err := decodeStoredRuntime(encoded)
		if err != nil || !reflect.DeepEqual(value, want) {
			t.Fatalf("valid persisted runtime overrides lost null, false, or inactive preferences: %v", err)
		}
	}
	replace := func(field, value string) string {
		return strings.Replace(empty, `"`+field+`":null`, `"`+field+`":`+value, 1)
	}
	invalid := map[string]string{
		"empty":                "",
		"root-null":            "null",
		"root-array":           "[]",
		"missing-all":          "{}",
		"trailing-object":      empty + "{}",
		"missing-null-group":   strings.Replace(empty, `,"VulkanToneMapping":null`, "", 1),
		"unknown-group":        strings.Replace(empty, `"Network":null`, `"Network":null,"Unsupported":null`, 1),
		"duplicate-group":      strings.Replace(empty, `"Network":null`, `"Network":null,"Network":null`, 1),
		"lowercase-group":      strings.Replace(empty, `"Network":null`, `"network":null`, 1),
		"network-missing-port": replace("Network", `{"BindHost":null}`),
		"network-unknown":      replace("Network", `{"BindHost":null,"HttpPort":null,"Listener":true}`),
		"network-duplicate":    replace("Network", `{"BindHost":null,"HttpPort":null,"HttpPort":8096}`),
		"network-type":         replace("Network", `{"BindHost":true,"HttpPort":null}`),
		"network-hostname":     replace("Network", `{"BindHost":"localhost","HttpPort":null}`),
		"network-zero-port":    replace("Network", `{"BindHost":null,"HttpPort":0}`),
		"hardware-missing-id":  replace("Hardware", `{"Decode":"software","Encode":"software"}`),
		"hardware-null-axis":   replace("Hardware", `{"Decode":null,"Encode":"software","DeviceID":""}`),
		"hardware-unknown":     replace("Hardware", `{"Decode":"cuda","Encode":"software","DeviceID":"gpu"}`),
		"thread-fraction":      replace("Threads", "1.5"),
		"thread-string":        replace("Threads", `"4"`),
		"thread-limit":         replace("Threads", "65"),
		"h264-missing-crf":     replace("H264", `{"Preset":"fast","RateControl":"bitrate"}`),
		"h264-null-crf":        replace("H264", `{"Preset":"fast","RateControl":"bitrate","CRF":null}`),
		"h264-unknown-field":   replace("H264", `{"Preset":"fast","RateControl":"bitrate","CRF":23,"Flags":"-x264-params"}`),
		"h264-duplicate-crf":   replace("H264", `{"Preset":"fast","RateControl":"bitrate","CRF":23,"CRF":24}`),
		"hevc-missing-preset":  replace("HEVC", `{"RateControl":"bitrate","CRF":28}`),
		"hevc-invalid-crf":     replace("HEVC", `{"Preset":"fast","RateControl":"bitrate","CRF":36}`),
		"boolean-string":       replace("SoftwareToneMapping", `"false"`),
		"boolean-number":       replace("VulkanToneMapping", "0"),
	}
	for name, document := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeStoredRuntime([]byte(document)); !errors.Is(err, ErrStoredSettings) || errors.Is(err, ErrInvalidInput) {
				t.Fatalf("malformed persisted runtime settings were not an operational error: %v", err)
			}
		})
	}
}

func TestRuntimeStartupDefaultsKeepDeploymentAuthoritySeparateFromManagedChoices(t *testing.T) {
	options := DefaultRuntimeOptions()
	options.Network = NetworkValues{BindHost: "localhost", HttpPort: 0}
	options.Hardware = HardwareSelection{Decode: "cuda", Encode: "nvenc"}
	options.LegacyHardwareDefault = true
	options.AuthorizedDeviceIDs = []string{"amd-alpha"}
	options.AvailableDeviceIDs = []string{}
	defaults, authorized, available, legacy, err := prepareRuntimeOptions([]RuntimeOptions{options})
	if err != nil || !legacy || defaults.Network != options.Network || defaults.Hardware != options.Hardware || !authorized["amd-alpha"] || len(available) != 0 {
		t.Fatalf("trusted legacy defaults or explicit empty availability were rewritten: %v", err)
	}
	options.AuthorizedDeviceIDs[0] = "changed"
	if !authorized["amd-alpha"] || authorized["changed"] {
		t.Fatal("startup authorization retained a caller-owned slice")
	}
	store := &Store{runtimeDefaults: defaults, authorizedDevices: authorized, availableDevices: available, legacyHardwareDefault: legacy}
	snapshot := store.runtimeSnapshot(RuntimeOverrides{})
	if !snapshot.HardwareAvailable || snapshot.Hardware != defaults.Hardware {
		t.Fatal("legacy deployment hardware could not remain effective without a managed override")
	}
	managed := HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: "amd-alpha"}
	snapshot = store.runtimeSnapshot(RuntimeOverrides{Hardware: &managed})
	if snapshot.HardwareAvailable {
		t.Fatal("legacy availability bypassed the managed AMD device check")
	}
	for _, invalid := range []RuntimeOptions{
		{Network: defaults.Network, Hardware: managed, Execution: defaults.Execution, LegacyHardwareDefault: true},
		{Network: defaults.Network, Hardware: HardwareSelection{Decode: "software", Encode: "software"}, Execution: defaults.Execution, AuthorizedDeviceIDs: []string{"same", "same"}},
		{Network: defaults.Network, Hardware: HardwareSelection{Decode: "software", Encode: "software"}, Execution: defaults.Execution, AvailableDeviceIDs: []string{"not-authorized"}},
	} {
		if _, _, _, _, err := prepareRuntimeOptions([]RuntimeOptions{invalid}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid startup inventory or legacy bypass was accepted")
		}
	}
}
