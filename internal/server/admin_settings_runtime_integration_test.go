//go:build linux

package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestHTTPAdminRuntimeSettingsAtomicUpdatesOmissionsResetsAndCAS(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	initial := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	initialRuntime := objectValue(t, initial, "Runtime")
	defaults := objectValue(t, initialRuntime, "Defaults")
	if objectValue(t, initialRuntime, "Network")["Active"] != nil {
		t.Fatal("an in-process handler fixture invented a published HTTP listener")
	}
	seed := adminSettingsHTTPUpdate("1", nil, nil)
	seed["Runtime"] = map[string]any{
		"Network": map[string]any{"BindHost": "127.0.0.1", "HttpPort": 8097},
		"Threads": 4, "H264": map[string]any{"Preset": "fast", "RateControl": "capped_crf", "CRF": 25},
		"SoftwareToneMapping": false,
	}
	saved := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", seed), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, saved, "2")
	runtime := objectValue(t, saved, "Runtime")
	effective, overrides := objectValue(t, runtime, "Effective"), objectValue(t, runtime, "Overrides")
	if effective["Threads"] != float64(4) || effective["SoftwareToneMapping"] != false || objectValue(t, effective, "H264")["CRF"] != float64(25) ||
		overrides["HEVC"] != nil || !reflect.DeepEqual(runtime["Defaults"], defaults) ||
		objectValue(t, objectValue(t, runtime, "Network"), "Desired")["HttpPort"] != float64(8097) {
		t.Fatal("native runtime write did not expose its exact committed groups and unchanged defaults")
	}
	before, snapshot := adminSettingsHTTPRow(t, f), f.app.settings.Snapshot()
	for _, includeEmptyRuntime := range []bool{false, true} {
		legacy := adminSettingsHTTPUpdate("2", nil, nil)
		if includeEmptyRuntime {
			legacy["Runtime"] = map[string]any{}
		}
		unchanged := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", legacy), http.StatusOK)
		if !reflect.DeepEqual(unchanged, saved) {
			t.Fatal("an omitted runtime group changed the native response or revision")
		}
		configurationHTTPUnchanged(t, f, before, snapshot)
	}
	clearQuality := adminSettingsHTTPUpdate("2", nil, nil)
	clearQuality["Runtime"] = map[string]any{"H264": nil}
	cleared := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", clearQuality), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, cleared, "3")
	runtime = objectValue(t, cleared, "Runtime")
	if !reflect.DeepEqual(objectValue(t, runtime, "Effective")["H264"], defaults["H264"]) || objectValue(t, runtime, "Effective")["Threads"] != float64(4) ||
		objectValue(t, runtime, "Overrides")["H264"] != nil || objectValue(t, runtime, "Sources")["H264"] != "deployment" {
		t.Fatal("group null failed to restore its deployment value or changed another group")
	}
	before, snapshot = adminSettingsHTTPRow(t, f), f.app.settings.Snapshot()
	stale := adminSettingsHTTPUpdate("2", nil, nil)
	stale["Runtime"] = nil
	response := adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", stale)
	value := adminSettingsHTTPObject(t, response, http.StatusConflict)
	if objectValue(t, value, "Error")["Code"] != "revision_conflict" {
		t.Fatal("runtime reset did not retain native CAS conflicts")
	}
	configurationHTTPUnchanged(t, f, before, snapshot)
	reset := map[string]any{"Revision": "3", "Fields": []string{"Runtime.Network", "Runtime.Threads"}}
	partial := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, partial, "4")
	runtime = objectValue(t, partial, "Runtime")
	if !reflect.DeepEqual(objectValue(t, runtime, "Network")["Desired"], defaults["Network"]) ||
		objectValue(t, runtime, "Effective")["Threads"] != defaults["Threads"] || objectValue(t, runtime, "Effective")["SoftwareToneMapping"] != false {
		t.Fatal("selective runtime reset changed an unselected tone-mapping group")
	}
	stale["Revision"] = "4"
	restored := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", stale), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, restored, "5")
	if !reflect.DeepEqual(objectValue(t, restored, "Runtime"), initialRuntime) {
		t.Fatal("top-level runtime null did not restore all original groups and sources")
	}
}

func TestHTTPAdminRuntimeInvalidGroupsRemainAtomicAndDoNotReflectInput(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	before, snapshot := adminSettingsHTTPRow(t, f), f.app.settings.Snapshot()
	for _, test := range []struct{ runtime, field string }{
		{`{"Threads":4,"H264":{"Preset":"fast","RateControl":"capped_crf"}}`, "Runtime.H264.CRF"},
		{`{"Threads":4,"H264":{"Preset":"fast","RateControl":"capped_crf","CRF":25,"CRF":26}}`, "Runtime.H264.CRF"},
		{`{"Threads":4,"Network":{"BindHost":"private-marker.example","HttpPort":8097}}`, "Runtime.Network.BindHost"},
		{`{"Threads":4,"Network":{"BindHost":"127.0.0.1","HttpPort":0}}`, "Runtime.Network.HttpPort"},
		{`{"Threads":4,"Hardware":{"Decode":"vaapi","Encode":"vaapi","DeviceId":"private-marker"}}`, "Runtime.Hardware.DeviceId"},
		{`{"Threads":4,"VulkanToneMapping":0}`, "Runtime.VulkanToneMapping"},
	} {
		body := adminSettingsExtendedUpdateBodyForTest(`"Runtime":` + test.runtime)
		response := configurationHTTPRaw(f, http.MethodPut, "/admin/v1/settings", body, "application/json",
			http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie)
		value := adminSettingsHTTPObject(t, response, http.StatusBadRequest)
		fields := objectValue(t, objectValue(t, value, "Error"), "Fields")
		if fields[test.field] == nil || strings.Contains(response.Body.String(), "private-marker") {
			t.Fatal("runtime validation omitted its exact field or exposed submitted data")
		}
		configurationHTTPUnchanged(t, f, before, snapshot)
	}
}
