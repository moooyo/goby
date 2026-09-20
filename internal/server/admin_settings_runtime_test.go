package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestDecodeAdminRuntimeSettingsPreservesAbsentNullAndExplicitValues(t *testing.T) {
	decode := func(runtime string) settings.UpdateRequest {
		t.Helper()
		body := adminSettingsNullUpdateForTest
		if runtime != "" {
			body = adminSettingsExtendedUpdateBodyForTest(`"Runtime":` + runtime)
		}
		response := httptest.NewRecorder()
		request, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json"))
		if !ok || response.Body.Len() != 0 {
			t.Fatal("valid runtime input was rejected")
		}
		return request
	}
	if decode("").Runtime != nil {
		t.Fatal("an older native writer unexpectedly replaced runtime settings")
	}
	if value := decode(`{}`).Runtime; value == nil || !reflect.DeepEqual(*value, settings.RuntimeUpdate{}) {
		t.Fatal("an empty runtime update did not preserve every field")
	}
	wantReset := settings.ResetRuntimeUpdate()
	if value := decode(`null`).Runtime; value == nil || !reflect.DeepEqual(*value, wantReset) {
		t.Fatal("explicit runtime null did not reset all seven groups")
	}
	if value := decode(`{"Network":null,"Hardware":null,"Threads":null,"H264":null,"HEVC":null,"SoftwareToneMapping":null,"VulkanToneMapping":null}`).Runtime; !reflect.DeepEqual(*value, wantReset) {
		t.Fatal("independent null groups did not retain their clear markers")
	}
	value := decode(`{"Network":{"BindHost":null,"HttpPort":65535},"Hardware":{"Decode":"software","Encode":"software","DeviceId":""},"Threads":64,"H264":{"Preset":"slow","RateControl":"capped_crf","CRF":18},"HEVC":{"Preset":"medium","RateControl":"bitrate","CRF":35},"SoftwareToneMapping":false,"VulkanToneMapping":true}`).Runtime
	if value == nil || !value.Network.Present || value.Network.Value == nil || value.Network.Value.BindHost != nil || *value.Network.Value.HttpPort != 65535 ||
		!value.Hardware.Present || value.Hardware.Value == nil || value.Hardware.Value.Decode != "software" || value.Hardware.Value.DeviceID != "" ||
		!value.Threads.Present || *value.Threads.Value != 64 || !value.H264.Present || value.H264.Value.CRF != 18 || value.H264.Value.Preset != "slow" ||
		!value.HEVC.Present || value.HEVC.Value.CRF != 35 || value.HEVC.Value.RateControl != "bitrate" ||
		!value.SoftwareToneMapping.Present || *value.SoftwareToneMapping.Value || !value.VulkanToneMapping.Present || !*value.VulkanToneMapping.Value {
		t.Fatal("runtime decoding lost explicit false, null, boundary numbers, or a complete group")
	}
}

func TestDecodeAdminRuntimeSettingsRejectsAtomicGroupAmbiguityAtFixedFields(t *testing.T) {
	for _, test := range []struct{ raw, field string }{
		{`[]`, "Runtime"}, {`{"private-marker":true}`, "Runtime"}, {`{"threads":4}`, "Runtime"},
		{`{"Threads":1,"Threads":2}`, "Runtime.Threads"}, {`{"Threads":1,"\u0054hreads":2}`, "Runtime.Threads"},
		{`{"Network":{"BindHost":""}}`, "Runtime.Network.HttpPort"},
		{`{"Network":{"BindHost":"","HttpPort":8096,"HttpPort":8097}}`, "Runtime.Network.HttpPort"},
		{`{"Network":{"BindHost":true,"HttpPort":8096}}`, "Runtime.Network.BindHost"},
		{`{"Network":{"BindHost":"","HttpPort":0}}`, "Runtime.Network.HttpPort"},
		{`{"Network":{"BindHost":"","HttpPort":65536}}`, "Runtime.Network.HttpPort"},
		{`{"Network":{"BindHost":"","HttpPort":1e3}}`, "Runtime.Network.HttpPort"},
		{`{"Hardware":{"Decode":"software","Encode":"software"}}`, "Runtime.Hardware.DeviceId"},
		{`{"Hardware":{"Decode":null,"Encode":"software","DeviceId":""}}`, "Runtime.Hardware.Decode"},
		{`{"Hardware":{"Decode":"qsv","Encode":"software","DeviceId":""}}`, "Runtime.Hardware.Decode"},
		{`{"Hardware":{"Decode":"software","Encode":"qsv","DeviceId":""}}`, "Runtime.Hardware.Encode"},
		{`{"Hardware":{"Decode":"software","Encode":"software","DeviceId":null}}`, "Runtime.Hardware.DeviceId"},
		{`{"Hardware":{"Decode":"software","Encode":"software","DeviceID":"private-marker"}}`, "Runtime.Hardware"},
		{`{"Threads":0}`, "Runtime.Threads"}, {`{"Threads":65}`, "Runtime.Threads"}, {`{"Threads":4.0}`, "Runtime.Threads"},
		{`{"Threads":"4"}`, "Runtime.Threads"}, {`{"Threads":9223372036854775808}`, "Runtime.Threads"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate"}}`, "Runtime.H264.CRF"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":null}}`, "Runtime.H264.CRF"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":17}}`, "Runtime.H264.CRF"},
		{`{"H264":{"Preset":"private-marker","RateControl":"bitrate","CRF":23}}`, "Runtime.H264.Preset"},
		{`{"HEVC":{"Preset":"fast","RateControl":"bitrate","CRF":36}}`, "Runtime.HEVC.CRF"},
		{`{"HEVC":{"Preset":"fast","RateControl":"private-marker","CRF":28}}`, "Runtime.HEVC.RateControl"},
		{`{"HEVC":{"Preset":"fast","RateControl":"bitrate","CRF":28,"CRF":29}}`, "Runtime.HEVC.CRF"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":23,"private-marker":true}}`, "Runtime.H264"},
		{`{"SoftwareToneMapping":0}`, "Runtime.SoftwareToneMapping"}, {`{"VulkanToneMapping":"false"}`, "Runtime.VulkanToneMapping"},
	} {
		t.Run(test.raw, func(t *testing.T) {
			response := httptest.NewRecorder()
			body := adminSettingsExtendedUpdateBodyForTest(`"Runtime":` + test.raw)
			if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json")); ok {
				t.Fatal("runtime update accepted an ambiguous or invalid atomic value")
			}
			assertAdminSettingsInputError(t, response, http.StatusBadRequest, test.field)
		})
	}
}

func TestDecodeAdminRuntimeResetAcceptsOnlyExactGroupNames(t *testing.T) {
	fields := []string{"Runtime", "Runtime.Network", "Runtime.Hardware", "Runtime.Threads", "Runtime.H264", "Runtime.HEVC", "Runtime.SoftwareToneMapping", "Runtime.VulkanToneMapping"}
	for _, field := range fields {
		body, _ := json.Marshal(map[string]any{"Revision": "9007199254740993", "Fields": []string{field}})
		response := httptest.NewRecorder()
		request, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(string(body), "application/json"))
		if !ok || request.Revision != 9007199254740993 || len(request.Fields) != 1 || string(request.Fields[0]) != field {
			t.Fatal("runtime reset lost an exact group or large decimal revision")
		}
	}
	for _, raw := range []string{`["Runtime.Threads","Runtime.Threads"]`, `["Runtime.H264.CRF"]`, `["Runtime.Network.HttpPort"]`, `["runtime"]`} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(`{"Revision":"1","Fields":`+raw+`}`, "application/json")); ok {
			t.Fatal("runtime reset admitted an unsupported leaf or duplicate selection")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Fields")
	}
}

func TestSettingsRuntimeDTOUsesFrozenDefaultsSourcesAndIndependentValues(t *testing.T) {
	defaults := settings.DefaultRuntimeOptions()
	defaults.Network = settings.NetworkValues{BindHost: "::1", HttpPort: 8097}
	defaults.Execution.Threads = 7
	port, threads, off := 9090, 3, false
	quality := transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 19}
	snapshot := settings.RuntimeSnapshot{
		Defaults:       settings.RuntimeValues{Network: defaults.Network, Hardware: defaults.Hardware, Execution: defaults.Execution},
		Overrides:      settings.RuntimeOverrides{Network: &settings.NetworkOverrides{HttpPort: &port}, Threads: &threads, H264: &quality, SoftwareToneMapping: &off},
		DesiredNetwork: settings.NetworkValues{BindHost: "::1", HttpPort: 9090}, Hardware: defaults.Hardware, Execution: defaults.Execution,
	}
	snapshot.Execution.Threads, snapshot.Execution.H264, snapshot.Execution.SoftwareToneMapping = threads, quality, off
	value := settingsRuntimeDTO(snapshot)
	if value["Defaults"].(map[string]any)["Threads"] != 7 || value["Effective"].(map[string]any)["Threads"] != 3 ||
		value["Overrides"].(map[string]any)["SoftwareToneMapping"] != false || value["Effective"].(map[string]any)["H264"] != quality {
		t.Fatal("runtime projection synthesized defaults or lost a configured false or codec group")
	}
	if _, exists := value["Effective"].(map[string]any)["Network"]; exists {
		t.Fatal("desired network configuration was mislabeled as an effective listener")
	}
	sources := value["Sources"].(map[string]any)
	if sources["Threads"] != "database" || sources["HEVC"] != "deployment" || sources["Network"].(map[string]any)["BindHost"] != "deployment" || sources["Network"].(map[string]any)["HttpPort"] != "database" {
		t.Fatal("runtime sources inferred provenance from equality instead of override presence")
	}
	port, threads, off, quality.CRF = 1, 1, true, 35
	if value["Overrides"].(map[string]any)["Network"].(map[string]any)["HttpPort"] != 9090 || value["Overrides"].(map[string]any)["Threads"] != 3 ||
		value["Overrides"].(map[string]any)["SoftwareToneMapping"] != false || value["Overrides"].(map[string]any)["H264"].(transcode.CPUQuality).CRF != 19 {
		t.Fatal("runtime DTO retained mutable pointers from its snapshot")
	}
}

func TestAdminSettingsRuntimeDTOSeparatesObservedBindingAndSafeInventory(t *testing.T) {
	defaults := settings.DefaultRuntimeOptions()
	selected := settings.HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: "amd-fixture"}
	snapshot := settings.Snapshot{Revision: 9007199254740994, Management: settings.DefaultManagement(), Sorting: settings.DefaultSorting(), Runtime: settings.RuntimeSnapshot{
		Defaults:       settings.RuntimeValues{Network: defaults.Network, Hardware: defaults.Hardware, Execution: defaults.Execution},
		DesiredNetwork: settings.NetworkValues{BindHost: "127.0.0.1", HttpPort: 9090},
		Hardware:       selected, HardwareAvailable: true, Execution: defaults.Execution,
	}}
	server := &Server{
		cfg: config.Config{Transcoding: config.TranscodingConfig{Hardware: transcode.Hardware{Device: "/private-marker/deployment"}}},
		httpBinding: runtimeHTTPBinding{active: &HTTPBindingActive{
			Configured: settings.NetworkValues{BindHost: "127.0.0.1", HttpPort: 8096},
			BoundHost:  "127.0.0.1", HttpPort: 8096, Revision: "9007199254740993",
		}},
		managedHardware: &managedHardwareInventory{entries: []managedHardwareEntry{{
			id: "amd-fixture", label: "AMD device 1", path: "/private-marker/renderD128", code: managedHardwareMissing,
		}}},
	}
	data, err := json.Marshal(server.adminSettingsDTO(snapshot))
	if err != nil || strings.Contains(string(data), "private-marker") || strings.Contains(string(data), `"DeviceID"`) {
		t.Fatal("runtime settings exposed a private path or an internal device field spelling")
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil {
		t.Fatal("runtime settings were not valid JSON")
	}
	assertAdminSettingsDTOFields(t, value, true)
	assertAdminSettingsSortingDTO(t, value, []string{})
	runtime := value["Runtime"].(map[string]any)
	if len(runtime) != 8 || len(runtime["Defaults"].(map[string]any)) != 7 ||
		len(runtime["Overrides"].(map[string]any)) != 7 || len(runtime["Effective"].(map[string]any)) != 6 || len(runtime["Sources"].(map[string]any)) != 7 {
		t.Fatal("runtime settings changed their closed projection shape")
	}
	network := runtime["Network"].(map[string]any)
	active := network["Active"].(map[string]any)
	if active["HttpPort"] != float64(8096) || active["Revision"] != "9007199254740993" ||
		network["Desired"].(map[string]any)["HttpPort"] != float64(9090) || network["RestartRequired"] != true || network["ReconnectURL"] != "http://127.0.0.1:9090" {
		t.Fatal("runtime settings relabeled desired state as observed or lost restart and revision semantics")
	}
	hardware := runtime["Hardware"].(map[string]any)
	devices := hardware["Devices"].([]any)
	if hardware["Available"] != false || hardware["Code"] != managedHardwareMissing || len(devices) != 1 ||
		devices[0].(map[string]any)["DeviceId"] != "amd-fixture" || devices[0].(map[string]any)["Available"] != false || len(devices[0].(map[string]any)) != 4 {
		t.Fatal("runtime settings trusted stale hardware availability or exposed an unrestricted inventory object")
	}
}
