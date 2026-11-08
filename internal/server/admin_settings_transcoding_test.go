package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func TestAdminRuntimeCodecThreadsDistinguishInheritedAndAutomatic(t *testing.T) {
	for _, test := range []struct {
		suffix  string
		present bool
		count   int
	}{
		{"", false, 0}, {`,"ThreadCount":null`, false, 0},
		{`,"ThreadCount":0`, true, 0}, {`,"ThreadCount":16`, true, 16},
	} {
		raw := json.RawMessage(`{"H264":{"Preset":"slower","RateControl":"capped_crf","CRF":1` + test.suffix + `}}`)
		invalid := make(map[string]string)
		update := decodeAdminRuntimeSettings(raw, invalid)
		if len(invalid) != 0 || update == nil || update.H264.Value == nil ||
			update.H264.Value.ThreadCountSet != test.present || update.H264.Value.ThreadCount != test.count {
			t.Fatalf("inherited and explicit encoder threads were conflated: %v", invalid)
		}
		dto := settingsCPUQualityDTO(*update.H264.Value)
		if test.present && dto["ThreadCount"] != test.count || !test.present && dto["ThreadCount"] != nil {
			t.Fatal("codec thread presence was not preserved by the public projection")
		}
	}
	invalid := make(map[string]string)
	update := decodeAdminRuntimeSettings(json.RawMessage(`{"H264":{"Preset":"fast","RateControl":"capped_crf","CRF":23,"X264Options":"subme=8:aq_strength=1.250"},"HEVC":{"Preset":"placebo","RateControl":"capped_crf","CRF":51,"ThreadCount":16}}`), invalid)
	if len(invalid) != 0 || update.H264.Value.X264Options != "aq-strength=1.25:subme=8" || update.HEVC.Value.Preset != "placebo" {
		t.Fatalf("supported native codec controls did not enter the closed request: %v", invalid)
	}
}

func TestAdminRuntimeTranscodingRejectsUnsupportedOrAmbiguousWholeGroups(t *testing.T) {
	for _, test := range []struct{ raw, field string }{
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCountSet":true}}`, "Runtime.H264"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCount":17}}`, "Runtime.H264.ThreadCount"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":23,"ThreadCount":0.0}}`, "Runtime.H264.ThreadCount"},
		{`{"H264":{"Preset":"fast","RateControl":"bitrate","CRF":23,"X264Options":"stats=/private-marker"}}`, "Runtime.H264.X264Options"},
		{`{"HEVC":{"Preset":"fast","RateControl":"bitrate","CRF":23,"X264Options":"subme=8"}}`, "Runtime.HEVC.X264Options"},
		{`{"Transcoding":{"EnableHevcEncoding":false}}`, "Runtime.Transcoding.EnableHardwareEncoding"},
		{`{"Transcoding":{"EnableHevcEncoding":null,"EnableHardwareEncoding":false,"DownMixAudioBoost":2,"EnableSubtitleExtraction":false,"EnableOnTheFlyAttachmentExtraction":false}}`, "Runtime.Transcoding.EnableHevcEncoding"},
		{`{"Transcoding":{"EnableHevcEncoding":false,"EnableHardwareEncoding":false,"DownMixAudioBoost":0,"EnableSubtitleExtraction":false,"EnableOnTheFlyAttachmentExtraction":false}}`, "Runtime.Transcoding.DownMixAudioBoost"},
		{`{"TranscodingTempPath":"/srv/media/../cache"}`, "Runtime.TranscodingTempPath"},
		{`{"SoftwareToneMapOptions":{"ToneMapSizeLimit":"Limit720","SoftwareToneMapAlgorithm":"Bt2390"}}`, "Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm"},
		{`{"CodecConfigurations":[{"CodecId":null,"IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]}`, "Runtime.CodecConfigurations"},
		{`{"CodecConfigurations":[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0}]}`, "Runtime.CodecConfigurations"},
		{`{"EnableThrottling":"true"}`, "Runtime.EnableThrottling"},
	} {
		response := httptest.NewRecorder()
		body := adminSettingsExtendedUpdateBodyForTest(`"Runtime":` + test.raw)
		if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json")); ok {
			t.Fatal("a managed update accepted an unsupported or ambiguous execution field")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, test.field)
		if strings.Contains(response.Body.String(), "private-marker") {
			t.Fatal("a rejected execution fragment was echoed to the caller")
		}
	}
}

func TestAdminRuntimeAdvancedDTOSeparatesRequestedCacheAndInternalThreadPresence(t *testing.T) {
	options := settings.DefaultRuntimeOptions()
	options.TranscodingTempPath = "/srv/default-transcodes"
	quality := options.Execution.H264
	quality.ThreadCountSet = true
	execution := options.Execution
	execution.H264 = quality
	snapshot := settings.Snapshot{Runtime: settings.RuntimeSnapshot{
		Defaults: settings.RuntimeValues{Network: options.Network, Hardware: options.Hardware, Execution: options.Execution,
			Transcoding: options.Transcoding, TranscodingTempPath: options.TranscodingTempPath, CodecConfigurations: options.CodecConfigurations},
		Overrides: settings.RuntimeOverrides{H264: &quality},
		Hardware:  options.Hardware, Execution: execution, Transcoding: options.Transcoding,
		CodecConfigurations: options.CodecConfigurations, DesiredTranscodingTempPath: "/srv/next-transcodes",
	}}
	server := &Server{cfg: config.Config{}, activeTranscodingTempPath: "/srv/active-transcodes"}
	value := server.adminSettingsDTO(snapshot)["Runtime"].(map[string]any)
	path := value["TranscodingTempPath"].(map[string]any)
	if path["Desired"] != "/srv/next-transcodes" || path["Active"] != "/srv/active-transcodes" || path["RestartRequired"] != true {
		t.Fatal("requested cache settings were relabeled as the active manager location")
	}
	effective := value["Effective"].(map[string]any)
	if _, exists := effective["TranscodingTempPath"]; exists || effective["H264"].(map[string]any)["ThreadCount"] != 0 {
		t.Fatal("the effective projection fabricated a current cache location or lost automatic threads")
	}
	raw, err := json.Marshal(value)
	if err != nil || strings.Contains(string(raw), "ThreadCountSet") {
		t.Fatal("the codec thread compatibility presence bit entered the product contract")
	}
}

func TestConfigurationTranscodingDecoderAcceptsOnlyImplementedNativeFields(t *testing.T) {
	const raw = `{"H264Preset":"faster","H264Crf":1,"EnableHevcEncoding":false,"EnableHardwareEncoding":false,"DownMixAudioBoost":2,"EnableSubtitleExtraction":false,"EnableOnTheFlyAttachmentExtraction":false,"TranscodingTempPath":"","CodecConfigurations":[{"CodecId":"V-E-libx265","IsEnabled":false,"Priority":1000},{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":-1000}]}`
	request := httptest.NewRequest(http.MethodPost, "/System/Configuration/encoding", strings.NewReader(raw))
	request.Header.Set("Content-Type", "application/octet-stream")
	decoded, ok := decodeConfiguration(httptest.NewRecorder(), request, settings.ConfigurationEncoding)
	if !ok || decoded.H264Preset == nil || *decoded.H264Preset != "faster" || decoded.H264Crf != 1 ||
		decoded.EnableHevcEncoding == nil || *decoded.EnableHevcEncoding || decoded.EnableHardwareEncoding == nil || *decoded.EnableHardwareEncoding ||
		decoded.DownMixAudioBoost == nil || *decoded.DownMixAudioBoost != 2 || decoded.TranscodingTempPath == nil || *decoded.TranscodingTempPath != "" ||
		decoded.CodecConfigurations == nil || decoded.CodecConfigurations[0].CodecId != transcode.H264SoftwareCodecID {
		t.Fatal("a native encoding write lost explicit false, an empty default, or canonical encoder identities")
	}
	for _, raw := range []string{
		`{"TranscodingTempPath":"relative/private-marker"}`, `{"EnableHevcEncoding":null}`, `{"DownMixAudioBoost":11}`,
		`{"CodecConfigurations":[]}`, `{"EncodingThreadCount":4}`, `{"ThrottleBufferSize":120}`, `{"DeinterlaceMethod":"auto"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/System/Configuration/encoding", strings.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		if _, ok := decodeConfiguration(response, request, settings.ConfigurationEncoding); ok || response.Code != http.StatusBadRequest ||
			strings.Contains(response.Body.String(), "private-marker") {
			t.Fatal("an unsupported, unsafe, or inert native control entered configuration storage")
		}
	}
}
