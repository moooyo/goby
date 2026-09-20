//go:build linux

package server

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestHTTPConfigurationRuntimeFullPartialAndNativeOnlyIsolation(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	seed := adminSettingsHTTPUpdate("1", nil, nil)
	seed["Encoding"] = map[string]any{"TranscodingMaxWidth": 640}
	seed["Runtime"] = map[string]any{
		"Network":             map[string]any{"BindHost": "127.0.0.1", "HttpPort": 9090},
		"Hardware":            map[string]any{"Decode": "software", "Encode": "software", "DeviceId": ""},
		"Threads":             3,
		"H264":                map[string]any{"Preset": "medium", "RateControl": "bitrate", "CRF": 31},
		"HEVC":                map[string]any{"Preset": "slow", "RateControl": "capped_crf", "CRF": 30},
		"SoftwareToneMapping": false, "VulkanToneMapping": false,
	}
	adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings", seed), http.StatusOK)
	seeded := f.app.settings.Snapshot()
	assertNativeOnly := func(snapshot settings.Snapshot) {
		t.Helper()
		if !reflect.DeepEqual(snapshot.Runtime.Overrides.Hardware, seeded.Runtime.Overrides.Hardware) ||
			!reflect.DeepEqual(snapshot.Runtime.Overrides.Threads, seeded.Runtime.Overrides.Threads) ||
			!reflect.DeepEqual(snapshot.Runtime.Overrides.HEVC, seeded.Runtime.Overrides.HEVC) ||
			snapshot.Runtime.Overrides.Network == nil || snapshot.Runtime.Overrides.Network.BindHost == nil || *snapshot.Runtime.Overrides.Network.BindHost != "127.0.0.1" {
			t.Fatal("a legacy writer replaced a native-only runtime group")
		}
	}
	before := adminSettingsHTTPRow(t, f.serverFixture)
	// An omitted CRF is a strict no-op when bitrate mode was already selected,
	// including its deliberately inactive, nondefault CRF preference.
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/encoding", map[string]any{
		"TranscodingMaxWidth": 640, "EnableSoftwareToneMapping": false, "EnableHardwareToneMapping": false,
	}, f.admin.headers), http.StatusNoContent)
	configurationHTTPUnchanged(t, f.serverFixture, before, seeded)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", map[string]any{}, f.admin.headers), http.StatusNoContent)
	configurationHTTPUnchanged(t, f.serverFixture, before, seeded)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", map[string]any{"H264Crf": 24}, f.admin.headers), http.StatusNoContent)
	selected := f.app.settings.Snapshot()
	if selected.Runtime.Execution.H264.Preset != "medium" || selected.Runtime.Execution.H264.RateControl != "capped_crf" || selected.Runtime.Execution.H264.CRF != 24 ||
		selected.Encoding.TranscodingMaxWidth != 640 || selected.Runtime.Execution.SoftwareToneMapping || selected.Runtime.Execution.VulkanToneMapping {
		t.Fatal("explicit compatibility CRF did not activate capped CRF while preserving the preset and omitted settings")
	}
	assertNativeOnly(selected)
	encoding := configurationHTTPObject(t, f.request(t, http.MethodGet, "/System/Configuration/encoding", nil, f.admin.headers))
	before = adminSettingsHTTPRow(t, f.serverFixture)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/encoding", encoding, f.admin.headers), http.StatusNoContent)
	configurationHTTPUnchanged(t, f.serverFixture, before, selected)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/encoding", map[string]any{}, f.admin.headers), http.StatusNoContent)
	reset := f.app.settings.Snapshot()
	if reset.Runtime.Execution.H264.Preset != "medium" || reset.Runtime.Execution.H264.RateControl != "bitrate" || reset.Runtime.Execution.H264.CRF != 23 ||
		reset.Runtime.Overrides.SoftwareToneMapping != nil || reset.Runtime.Overrides.VulkanToneMapping != nil ||
		reset.Runtime.Execution.SoftwareToneMapping != reset.Runtime.Defaults.Execution.SoftwareToneMapping ||
		reset.Runtime.Execution.VulkanToneMapping != reset.Runtime.Defaults.Execution.VulkanToneMapping || reset.Encoding.TranscodingMaxWidth != 0 {
		t.Fatal("complete named encoding omission did not reset only its public active CRF, tone-map flags, and independent width")
	}
	assertNativeOnly(reset)
	if reset.Runtime.DesiredNetwork != seeded.Runtime.DesiredNetwork {
		t.Fatal("encoding replacement changed desired listener settings")
	}
	if _, present := configurationHTTPObject(t, f.request(t, http.MethodGet, "/System/Configuration/encoding", nil, f.admin.headers))["H264Crf"]; present {
		t.Fatal("bitrate-mode compatibility GET exposed an inactive CRF")
	}
	before = adminSettingsHTTPRow(t, f.serverFixture)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/encoding", map[string]any{}, f.admin.headers), http.StatusNoContent)
	configurationHTTPUnchanged(t, f.serverFixture, before, reset)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", map[string]any{"HttpServerPortNumber": 9091}, f.admin.headers), http.StatusNoContent)
	portChanged := f.app.settings.Snapshot()
	if portChanged.Runtime.DesiredNetwork.HttpPort != 9091 || portChanged.Runtime.DesiredNetwork.BindHost != "127.0.0.1" {
		t.Fatal("partial desired-port mutation changed the native bind host")
	}
	if configurationHTTPObject(t, f.request(t, http.MethodGet, "/System/Configuration", nil, f.admin.headers))["HttpServerPortNumber"] != float64(9091) {
		t.Fatal("configuration GET did not expose the committed desired port")
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration", map[string]any{}, f.admin.headers), http.StatusNoContent)
	full := f.app.settings.Snapshot()
	if full.Runtime.DesiredNetwork.HttpPort != full.Runtime.Defaults.Network.HttpPort || full.Runtime.Overrides.Network.HttpPort != nil ||
		full.ServerNameMode != settings.ServerNameUnset || full.Runtime.Execution != portChanged.Runtime.Execution {
		t.Fatal("full server omission did not reset desired port independently of native bind and encoding policy")
	}
	assertNativeOnly(full)
	stale := adminSettingsHTTPUpdate(strconv.FormatInt(seeded.Revision, 10), nil, nil)
	stale["Runtime"] = nil
	before = adminSettingsHTTPRow(t, f.serverFixture)
	adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings", stale), http.StatusConflict)
	configurationHTTPUnchanged(t, f.serverFixture, before, full)
}

func TestHTTPConfigurationRuntimeAuthorityAndAtomicInvalidFields(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	key := f.create(t, "Runtime configuration writer")
	headers := http.Header{"X-Emby-Token": {key.token}}
	body := map[string]any{"HttpServerPortNumber": 9090, "H264Crf": 18, "EnableSoftwareToneMapping": false}
	before, snapshot := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", body, f.viewer.headers), http.StatusForbidden)
	configurationHTTPUnchanged(t, f.serverFixture, before, snapshot)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", body, headers), http.StatusNoContent)
	snapshot = f.app.settings.Snapshot()
	if snapshot.Runtime.DesiredNetwork.HttpPort != 9090 || snapshot.Runtime.Execution.H264.RateControl != "capped_crf" || snapshot.Runtime.Execution.H264.CRF != 18 ||
		snapshot.Runtime.Execution.SoftwareToneMapping || snapshot.Runtime.Execution.VulkanToneMapping != snapshot.Runtime.Defaults.Execution.VulkanToneMapping {
		t.Fatal("authorized application key did not change exactly the supplied runtime fields")
	}
	before = adminSettingsHTTPRow(t, f.serverFixture)
	for _, raw := range []string{
		`{"HttpServerPortNumber":9091,"H264Crf":36}`,
		`{"HttpServerPortNumber":9091,"EnableSoftwareToneMapping":null}`,
		`{"HttpServerPortNumber":9091,"httpserverportnumber":9092}`,
		`{"HttpServerPortNumber":9091,"H264Crf":24,"h264crf":25}`,
		`{"HttpServerPortNumber":9091,"EnableHardwareEncoding":true}`,
	} {
		configurationHTTPStatus(t, configurationHTTPRaw(f.serverFixture, http.MethodPost, "/System/Configuration/Partial", raw, "application/json", headers), http.StatusBadRequest)
		configurationHTTPUnchanged(t, f.serverFixture, before, snapshot)
	}
	expectStatus(t, f.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/revoke", map[string]any{}), http.StatusOK)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", body, headers), http.StatusUnauthorized)
	configurationHTTPUnchanged(t, f.serverFixture, before, snapshot)
}
