package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestConfigurationRuntimeDecoderUsesClosedSectionsAndPresence(t *testing.T) {
	for _, section := range []settings.ConfigurationSection{settings.ConfigurationFull, settings.ConfigurationPartial} {
		for _, raw := range []string{`{"HttpServerPortNumber":1}`, `{"HTTPSERVERPORTNUMBER":65535}`} {
			r := httptest.NewRequest(http.MethodPost, "/System/Configuration", strings.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			value, ok := decodeConfiguration(httptest.NewRecorder(), r, section)
			if !ok || !value.HttpServerPortNumberPresent || value.HttpServerPortNumber < 1 {
				t.Fatal("compatibility decoder lost a desired port or its presence")
			}
		}
	}
	for _, section := range []settings.ConfigurationSection{settings.ConfigurationEncoding, settings.ConfigurationPartial} {
		r := httptest.NewRequest(http.MethodPost, "/System/Configuration/encoding", strings.NewReader(`{"h264CRF":35,"ENABLESOFTWARETONEMAPPING":false,"EnableHardwareToneMapping":true}`))
		r.Header.Set("Content-Type", "application/json")
		value, ok := decodeConfiguration(httptest.NewRecorder(), r, section)
		if !ok || !value.H264CrfPresent || value.H264Crf != 35 || value.EnableSoftwareToneMapping == nil || *value.EnableSoftwareToneMapping ||
			value.EnableHardwareToneMapping == nil || !*value.EnableHardwareToneMapping {
			t.Fatal("compatibility decoder lost the presence or false value of a supported encoding setting")
		}
	}
	for _, test := range []struct {
		section settings.ConfigurationSection
		raw     string
	}{
		{settings.ConfigurationFull, `{"H264Crf":23}`},
		{settings.ConfigurationFull, `{"EnableSoftwareToneMapping":true}`},
		{settings.ConfigurationEncoding, `{"HttpServerPortNumber":8096}`},
		{settings.ConfigurationPartial, `{"HttpServerPortNumber":null}`},
		{settings.ConfigurationPartial, `{"HttpServerPortNumber":0}`},
		{settings.ConfigurationPartial, `{"HttpServerPortNumber":65536}`},
		{settings.ConfigurationPartial, `{"HttpServerPortNumber":8e3}`},
		{settings.ConfigurationPartial, `{"HttpServerPortNumber":8096,"httpserverportnumber":8097}`},
		{settings.ConfigurationEncoding, `{"H264Crf":17}`},
		{settings.ConfigurationEncoding, `{"H264Crf":36}`},
		{settings.ConfigurationEncoding, `{"H264Crf":null}`},
		{settings.ConfigurationEncoding, `{"H264Crf":23.0}`},
		{settings.ConfigurationEncoding, `{"H264Crf":23,"h264crf":24}`},
		{settings.ConfigurationEncoding, `{"EnableSoftwareToneMapping":null}`},
		{settings.ConfigurationEncoding, `{"EnableHardwareToneMapping":0}`},
		{settings.ConfigurationEncoding, `{"EnableHardwareEncoding":true}`},
		{settings.ConfigurationEncoding, `{"EncodingThreadCount":4}`},
		{settings.ConfigurationEncoding, `{"DeviceId":"private-marker"}`},
	} {
		r := httptest.NewRequest(http.MethodPost, "/System/Configuration", strings.NewReader(test.raw))
		r.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		if _, ok := decodeConfiguration(response, r, test.section); ok || response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-marker") {
			t.Fatal("compatibility runtime parser accepted unsupported, null, duplicated, or out-of-range data")
		}
	}
}

func TestConfigurationRuntimeProjectionDistinguishesDesiredPortAndActiveCRF(t *testing.T) {
	snapshot := settings.Snapshot{Runtime: settings.RuntimeSnapshot{
		DesiredNetwork: settings.NetworkValues{HttpPort: 9090}, Execution: transcode.DefaultExecutionOptions(2),
	}}
	snapshot.Runtime.Execution.H264.CRF = 31
	if serverConfigurationDTO(settings.Configuration{Snapshot: snapshot})["HttpServerPortNumber"] != 9090 {
		t.Fatal("configuration port was not projected from committed desired state")
	}
	value := encodingConfigurationDTO(snapshot)
	if _, exists := value["H264Crf"]; exists {
		t.Fatal("inactive bitrate-mode CRF was advertised as an active compatibility option")
	}
	if value["EnableSoftwareToneMapping"] != true || value["EnableHardwareToneMapping"] != true {
		t.Fatal("compatibility tone-map flags did not use the managed filter policy")
	}
	snapshot.Runtime.Execution.H264.RateControl = "capped_crf"
	if encodingConfigurationDTO(snapshot)["H264Crf"] != 31 {
		t.Fatal("active capped CRF was omitted or replaced with a synthetic default")
	}
}
