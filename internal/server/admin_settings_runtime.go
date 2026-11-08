package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

var adminRuntimeFields = []string{"Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping",
	"Transcoding", "TranscodingTempPath", "SoftwareToneMapOptions", "CodecConfigurations", "EnableThrottling", "ExtractionThreadCount", "CodecProfileLimits", "SubtitleOptions"}

func adminSettingsNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func adminRuntimeObject(raw json.RawMessage, fields, required []string, path string, invalid map[string]string) map[string]json.RawMessage {
	values, errors := adminTaskObject(raw, fields, required, path)
	for field, message := range errors {
		invalid[field] = message
	}
	return values
}

func adminRuntimeInteger(raw json.RawMessage, field string, minimum, maximum int, invalid map[string]string) *int {
	var value *int
	if json.Unmarshal(raw, &value) != nil || value == nil || *value < minimum || *value > maximum {
		invalid[field] = fmt.Sprintf("Supply an integer JSON number between %d and %d.", minimum, maximum)
		return nil
	}
	return value
}

func adminRuntimeString(raw json.RawMessage, field string, invalid map[string]string) string {
	var value *string
	if json.Unmarshal(raw, &value) != nil || value == nil {
		invalid[field] = "Supply a string value."
		return ""
	}
	return *value
}

func adminRuntimeQuality(raw json.RawMessage, field string, invalid map[string]string) *transcode.CPUQuality {
	if adminSettingsNull(raw) {
		return nil
	}
	fields := []string{"Preset", "RateControl", "CRF", "ThreadCount", "X264Options"}
	values := adminRuntimeObject(raw, fields, fields[:3], field, invalid)
	if values == nil {
		return nil
	}
	quality := transcode.CPUQuality{
		Preset:      adminRuntimeString(values["Preset"], field+".Preset", invalid),
		RateControl: adminRuntimeString(values["RateControl"], field+".RateControl", invalid),
	}
	switch quality.Preset {
	case "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow":
	case "ultrafast", "placebo":
		if field != "Runtime.HEVC" {
			invalid[field+".Preset"] = "Select a supported x264 preset."
		}
	default:
		invalid[field+".Preset"] = "Select a supported software encoder preset."
	}
	if quality.RateControl != "bitrate" && quality.RateControl != "capped_crf" {
		invalid[field+".RateControl"] = "Select bitrate or capped_crf."
	}
	if value := adminRuntimeInteger(values["CRF"], field+".CRF", 1, 51, invalid); value != nil {
		quality.CRF = *value
	}
	if raw, present := values["ThreadCount"]; present && !adminSettingsNull(raw) {
		if value := adminRuntimeInteger(raw, field+".ThreadCount", 0, 16, invalid); value != nil {
			quality.ThreadCount, quality.ThreadCountSet = *value, true
		}
	}
	if raw, present := values["X264Options"]; present {
		value := adminRuntimeString(raw, field+".X264Options", invalid)
		canonical, err := transcode.CanonicalX264Options(value)
		if err != nil || field == "Runtime.HEVC" && value != "" {
			invalid[field+".X264Options"] = "Use supported x264 options only for H.264 software encoding."
		} else {
			quality.X264Options = canonical
		}
	}
	return &quality
}

func adminRuntimeTranscoding(raw json.RawMessage, invalid map[string]string) *settings.TranscodingPolicy {
	if adminSettingsNull(raw) {
		return nil
	}
	fields := []string{"EnableHevcEncoding", "EnableHardwareEncoding", "DownMixAudioBoost", "EnableSubtitleExtraction", "EnableOnTheFlyAttachmentExtraction"}
	values := adminRuntimeObject(raw, fields, fields, "Runtime.Transcoding", invalid)
	if values == nil {
		return nil
	}
	value := &settings.TranscodingPolicy{}
	for _, field := range []struct {
		name   string
		target *bool
	}{{"EnableHevcEncoding", &value.EnableHevcEncoding}, {"EnableHardwareEncoding", &value.EnableHardwareEncoding},
		{"EnableSubtitleExtraction", &value.EnableSubtitleExtraction}, {"EnableOnTheFlyAttachmentExtraction", &value.EnableOnTheFlyAttachmentExtraction}} {
		var enabled *bool
		if json.Unmarshal(values[field.name], &enabled) != nil || enabled == nil {
			invalid["Runtime.Transcoding."+field.name] = "Supply a boolean value."
		} else {
			*field.target = *enabled
		}
	}
	var boost *float64
	if json.Unmarshal(values["DownMixAudioBoost"], &boost) != nil || boost == nil || *boost < 0.1 || *boost > 10 {
		invalid["Runtime.Transcoding.DownMixAudioBoost"] = "Supply a finite JSON number between 0.1 and 10."
	} else {
		value.DownMixAudioBoost = *boost
	}
	return value
}

func adminRuntimeSoftwareToneMapOptions(raw json.RawMessage, invalid map[string]string) *transcode.SoftwareToneMapOptions {
	if adminSettingsNull(raw) {
		return nil
	}
	fields := []string{"ToneMapSizeLimit", "SoftwareToneMapAlgorithm"}
	values := adminRuntimeObject(raw, fields, fields, "Runtime.SoftwareToneMapOptions", invalid)
	if values == nil {
		return nil
	}
	value := &transcode.SoftwareToneMapOptions{
		ToneMapSizeLimit:         adminRuntimeString(values["ToneMapSizeLimit"], "Runtime.SoftwareToneMapOptions.ToneMapSizeLimit", invalid),
		SoftwareToneMapAlgorithm: adminRuntimeString(values["SoftwareToneMapAlgorithm"], "Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm", invalid),
	}
	switch value.ToneMapSizeLimit {
	case "None", "Limit420", "Limit720", "Limit1080":
	default:
		invalid["Runtime.SoftwareToneMapOptions.ToneMapSizeLimit"] = "Select None, Limit420, Limit720, or Limit1080."
	}
	switch value.SoftwareToneMapAlgorithm {
	case "Hable", "Reinhard", "Mobius":
	default:
		invalid["Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm"] = "Select Hable, Reinhard, or Mobius."
	}
	return value
}

func adminRuntimeCodecConfigurations(raw json.RawMessage, invalid map[string]string) *transcode.CodecConfigurations {
	if adminSettingsNull(raw) {
		return nil
	}
	var value transcode.CodecConfigurations
	if json.Unmarshal(raw, &value) != nil || transcode.ValidateCodecConfigurations(value) != nil {
		invalid["Runtime.CodecConfigurations"] = "Supply both supported software encoder identities once with boolean enabled states and integer priorities between -1000 and 1000."
		return nil
	}
	return &value
}

func adminRuntimeCodecProfileLimits(raw json.RawMessage, invalid map[string]string) *transcode.CodecProfileLimits {
	if adminSettingsNull(raw) {
		return nil
	}
	var value transcode.CodecProfileLimits
	if json.Unmarshal(raw, &value) != nil || transcode.ValidateCodecProfileLimits(value) != nil {
		invalid["Runtime.CodecProfileLimits"] = "Supply a complete profile limit group with H.264 baseline, main, or high and HEVC main or main10."
		return nil
	}
	return &value
}

func adminRuntimeSubtitleOptions(raw json.RawMessage, invalid map[string]string) *settings.RuntimeSubtitleOptions {
	if adminSettingsNull(raw) {
		return nil
	}
	fields := []string{"AllowAssToText"}
	values := adminRuntimeObject(raw, fields, fields, "Runtime.SubtitleOptions", invalid)
	if values == nil {
		return nil
	}
	var enabled *bool
	if json.Unmarshal(values["AllowAssToText"], &enabled) != nil || enabled == nil {
		invalid["Runtime.SubtitleOptions.AllowAssToText"] = "Supply a boolean value."
		return nil
	}
	return &settings.RuntimeSubtitleOptions{AllowAssToText: *enabled}
}

func decodeAdminRuntimeSettings(raw json.RawMessage, invalid map[string]string) *settings.RuntimeUpdate {
	if adminSettingsNull(raw) {
		value := settings.ResetRuntimeUpdate()
		return &value
	}
	values := adminRuntimeObject(raw, adminRuntimeFields, nil, "Runtime", invalid)
	if values == nil {
		return nil
	}
	result := &settings.RuntimeUpdate{}
	if raw, present := values["Network"]; present {
		result.Network.Present = true
		if !adminSettingsNull(raw) {
			fields := []string{"BindHost", "HttpPort"}
			network := adminRuntimeObject(raw, fields, fields, "Runtime.Network", invalid)
			if network != nil {
				result.Network.Value = &settings.NetworkOverrides{}
				if !adminSettingsNull(network["BindHost"]) {
					value := adminRuntimeString(network["BindHost"], "Runtime.Network.BindHost", invalid)
					result.Network.Value.BindHost = &value
				}
				if !adminSettingsNull(network["HttpPort"]) {
					result.Network.Value.HttpPort = adminRuntimeInteger(network["HttpPort"], "Runtime.Network.HttpPort", 1, 65535, invalid)
				}
			}
		}
	}
	if raw, present := values["Hardware"]; present {
		result.Hardware.Present = true
		if !adminSettingsNull(raw) {
			fields := []string{"Decode", "Encode", "DeviceId"}
			hardware := adminRuntimeObject(raw, fields, fields, "Runtime.Hardware", invalid)
			if hardware != nil {
				value := &settings.HardwareSelection{
					Decode:   adminRuntimeString(hardware["Decode"], "Runtime.Hardware.Decode", invalid),
					Encode:   adminRuntimeString(hardware["Encode"], "Runtime.Hardware.Encode", invalid),
					DeviceID: adminRuntimeString(hardware["DeviceId"], "Runtime.Hardware.DeviceId", invalid),
				}
				for field, mode := range map[string]string{"Decode": value.Decode, "Encode": value.Encode} {
					if mode != "software" && mode != "vaapi" {
						invalid["Runtime.Hardware."+field] = "Select software or vaapi."
					}
				}
				result.Hardware.Value = value
			}
		}
	}
	if raw, present := values["Threads"]; present {
		result.Threads.Present = true
		if !adminSettingsNull(raw) {
			result.Threads.Value = adminRuntimeInteger(raw, "Runtime.Threads", 1, 64, invalid)
		}
	}
	if raw, present := values["ExtractionThreadCount"]; present {
		result.ExtractionThreadCount.Present = true
		if !adminSettingsNull(raw) {
			result.ExtractionThreadCount.Value = adminRuntimeInteger(raw, "Runtime.ExtractionThreadCount", 0, 16, invalid)
		}
	}
	if raw, present := values["H264"]; present {
		result.H264 = settings.Change[transcode.CPUQuality]{Present: true, Value: adminRuntimeQuality(raw, "Runtime.H264", invalid)}
	}
	if raw, present := values["HEVC"]; present {
		result.HEVC = settings.Change[transcode.CPUQuality]{Present: true, Value: adminRuntimeQuality(raw, "Runtime.HEVC", invalid)}
	}
	if raw, present := values["Transcoding"]; present {
		result.Transcoding = settings.Change[settings.TranscodingPolicy]{Present: true, Value: adminRuntimeTranscoding(raw, invalid)}
	}
	if raw, present := values["TranscodingTempPath"]; present {
		result.TranscodingTempPath.Present = true
		if !adminSettingsNull(raw) {
			value := adminRuntimeString(raw, "Runtime.TranscodingTempPath", invalid)
			if config.ValidateTranscodingTempPath(value, nil) != nil {
				invalid["Runtime.TranscodingTempPath"] = "Use an empty deployment default or a canonical absolute dedicated Linux directory."
			} else {
				result.TranscodingTempPath.Value = &value
			}
		}
	}
	if raw, present := values["SoftwareToneMapOptions"]; present {
		result.SoftwareToneMapOptions = settings.Change[transcode.SoftwareToneMapOptions]{Present: true, Value: adminRuntimeSoftwareToneMapOptions(raw, invalid)}
	}
	if raw, present := values["CodecConfigurations"]; present {
		result.CodecConfigurations = settings.Change[transcode.CodecConfigurations]{Present: true, Value: adminRuntimeCodecConfigurations(raw, invalid)}
	}
	if raw, present := values["CodecProfileLimits"]; present {
		result.CodecProfileLimits = settings.Change[transcode.CodecProfileLimits]{Present: true, Value: adminRuntimeCodecProfileLimits(raw, invalid)}
	}
	if raw, present := values["SubtitleOptions"]; present {
		result.SubtitleOptions = settings.Change[settings.RuntimeSubtitleOptions]{Present: true, Value: adminRuntimeSubtitleOptions(raw, invalid)}
	}
	for _, field := range []struct {
		name   string
		target *settings.Change[bool]
	}{{"SoftwareToneMapping", &result.SoftwareToneMapping}, {"VulkanToneMapping", &result.VulkanToneMapping}, {"EnableThrottling", &result.EnableThrottling}} {
		if raw, present := values[field.name]; present {
			field.target.Present = true
			if !adminSettingsNull(raw) {
				var value bool
				if json.Unmarshal(raw, &value) != nil {
					invalid["Runtime."+field.name] = "Supply a boolean value or null."
				} else {
					field.target.Value = &value
				}
			}
		}
	}
	return result
}

// Domain validation uses these fixed keys. Never echo an arbitrary field path
// from an error into the public response.
func adminRuntimeErrorField(field string) bool {
	switch field {
	case "Runtime", "Runtime.Network", "Runtime.Network.BindHost", "Runtime.Network.HttpPort",
		"Runtime.Hardware", "Runtime.Hardware.Decode", "Runtime.Hardware.Encode", "Runtime.Hardware.DeviceId",
		"Runtime.Threads", "Runtime.H264", "Runtime.H264.Preset", "Runtime.H264.RateControl", "Runtime.H264.CRF",
		"Runtime.H264.ThreadCount", "Runtime.H264.X264Options",
		"Runtime.HEVC", "Runtime.HEVC.Preset", "Runtime.HEVC.RateControl", "Runtime.HEVC.CRF",
		"Runtime.HEVC.ThreadCount", "Runtime.HEVC.X264Options",
		"Runtime.SoftwareToneMapping", "Runtime.VulkanToneMapping", "Runtime.Transcoding", "Runtime.TranscodingTempPath",
		"Runtime.Transcoding.EnableHevcEncoding", "Runtime.Transcoding.EnableHardwareEncoding", "Runtime.Transcoding.DownMixAudioBoost",
		"Runtime.Transcoding.EnableSubtitleExtraction", "Runtime.Transcoding.EnableOnTheFlyAttachmentExtraction",
		"Runtime.SoftwareToneMapOptions", "Runtime.SoftwareToneMapOptions.ToneMapSizeLimit", "Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm",
		"Runtime.CodecConfigurations", "Runtime.CodecProfileLimits", "Runtime.SubtitleOptions", "Runtime.SubtitleOptions.AllowAssToText",
		"Runtime.EnableThrottling", "Runtime.ExtractionThreadCount":
		return true
	default:
		return false
	}
}
