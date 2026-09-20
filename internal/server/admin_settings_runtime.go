package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

var adminRuntimeFields = []string{"Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping"}

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
	fields := []string{"Preset", "RateControl", "CRF"}
	values := adminRuntimeObject(raw, fields, fields, field, invalid)
	if values == nil {
		return nil
	}
	quality := transcode.CPUQuality{
		Preset:      adminRuntimeString(values["Preset"], field+".Preset", invalid),
		RateControl: adminRuntimeString(values["RateControl"], field+".RateControl", invalid),
	}
	switch quality.Preset {
	case "veryfast", "fast", "medium", "slow":
	default:
		invalid[field+".Preset"] = "Select veryfast, fast, medium, or slow."
	}
	if quality.RateControl != "bitrate" && quality.RateControl != "capped_crf" {
		invalid[field+".RateControl"] = "Select bitrate or capped_crf."
	}
	if value := adminRuntimeInteger(values["CRF"], field+".CRF", 18, 35, invalid); value != nil {
		quality.CRF = *value
	}
	return &quality
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
	if raw, present := values["H264"]; present {
		result.H264 = settings.Change[transcode.CPUQuality]{Present: true, Value: adminRuntimeQuality(raw, "Runtime.H264", invalid)}
	}
	if raw, present := values["HEVC"]; present {
		result.HEVC = settings.Change[transcode.CPUQuality]{Present: true, Value: adminRuntimeQuality(raw, "Runtime.HEVC", invalid)}
	}
	for _, field := range []struct {
		name   string
		target *settings.Change[bool]
	}{{"SoftwareToneMapping", &result.SoftwareToneMapping}, {"VulkanToneMapping", &result.VulkanToneMapping}} {
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
		"Runtime.HEVC", "Runtime.HEVC.Preset", "Runtime.HEVC.RateControl", "Runtime.HEVC.CRF",
		"Runtime.SoftwareToneMapping", "Runtime.VulkanToneMapping":
		return true
	default:
		return false
	}
}
