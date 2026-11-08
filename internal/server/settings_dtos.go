package server

import (
	"strconv"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func settingsValuesDTO(values settings.Values) map[string]any {
	return map[string]any{
		"ServerName": values.ServerName, "MaxBitrate": values.MaxBitrate,
		"MaxWidth": values.MaxWidth, "MaxHeight": values.MaxHeight,
		"MaxAudioChannels": values.MaxAudioChannels,
	}
}

func settingsOverrideValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func settingsSource(overridden bool) string {
	if overridden {
		return "database"
	}
	return "deployment"
}

func settingsDTO(snapshot settings.Snapshot, deployment config.TranscodingConfig) map[string]any {
	return map[string]any{
		"Revision":       strconv.FormatInt(snapshot.Revision, 10),
		"ServerNameMode": string(snapshot.ServerNameMode),
		"Defaults":       settingsValuesDTO(snapshot.Defaults),
		"Overrides": map[string]any{
			"ServerName":       settingsOverrideValue(snapshot.Overrides.ServerName),
			"MaxBitrate":       settingsOverrideValue(snapshot.Overrides.MaxBitrate),
			"MaxWidth":         settingsOverrideValue(snapshot.Overrides.MaxWidth),
			"MaxHeight":        settingsOverrideValue(snapshot.Overrides.MaxHeight),
			"MaxAudioChannels": settingsOverrideValue(snapshot.Overrides.MaxAudioChannels),
		},
		"Effective": settingsValuesDTO(snapshot.Effective),
		"Sources": map[string]any{
			"ServerName":       settingsSource(snapshot.ServerNameMode != settings.ServerNameDeployment),
			"MaxBitrate":       settingsSource(snapshot.Overrides.MaxBitrate != nil),
			"MaxWidth":         settingsSource(snapshot.Overrides.MaxWidth != nil),
			"MaxHeight":        settingsSource(snapshot.Overrides.MaxHeight != nil),
			"MaxAudioChannels": settingsSource(snapshot.Overrides.MaxAudioChannels != nil),
		},
		"Encoding":           map[string]any{"TranscodingMaxWidth": snapshot.Encoding.TranscodingMaxWidth},
		"UpdatedAt":          snapshot.UpdatedAt.UTC(),
		"Management":         snapshot.Management,
		"Sorting":            snapshot.Sorting,
		"SortingDefaults":    settings.DefaultSorting(),
		"ManagementDefaults": settings.DefaultManagement(),
		"ManagementEffects":  map[string]any{"Metadata": "next_work_item", "Subtitles": "next_work_item", "Tasks": "next_admission", "RestartRequired": false},
		"Deployment": map[string]any{
			"HostName":           snapshot.HostName,
			"TranscodingEnabled": deployment.Enabled,
			"HardwareDecoder":    deployment.Hardware.Decode,
			"HardwareEncoder":    deployment.Hardware.Encode,
			"Threads":            deployment.Threads, "MaxJobs": deployment.MaxJobs,
			"MaxUserJobs": deployment.MaxUserJobs, "MaxSessionJobs": deployment.MaxSessionJobs,
		},
	}
}

func settingsNetworkDTO(value settings.NetworkValues) map[string]any {
	return map[string]any{"BindHost": value.BindHost, "HttpPort": value.HttpPort}
}

func settingsHardwareDTO(value settings.HardwareSelection) map[string]any {
	return map[string]any{"Decode": value.Decode, "Encode": value.Encode, "DeviceId": value.DeviceID}
}

func settingsCPUQualityDTO(value transcode.CPUQuality) map[string]any {
	var threads any
	if value.ThreadCountSet {
		threads = value.ThreadCount
	}
	return map[string]any{"Preset": value.Preset, "RateControl": value.RateControl, "CRF": value.CRF,
		"ThreadCount": threads, "X264Options": value.X264Options}
}

func settingsCPUQualityOverrideDTO(value *transcode.CPUQuality) any {
	if value == nil {
		return nil
	}
	return settingsCPUQualityDTO(*value)
}

func settingsExecutionDTO(value transcode.ExecutionOptions) map[string]any {
	tone := value.SoftwareToneMapOptions
	if tone == (transcode.SoftwareToneMapOptions{}) {
		tone = transcode.DefaultSoftwareToneMapOptions()
	}
	return map[string]any{
		"Threads": value.Threads, "H264": settingsCPUQualityDTO(value.H264), "HEVC": settingsCPUQualityDTO(value.HEVC),
		"SoftwareToneMapping": value.SoftwareToneMapping, "VulkanToneMapping": value.VulkanToneMapping,
		"SoftwareToneMapOptions": tone,
	}
}

// The shared snapshot owns every default and new-admission choice. The HTTP
// adapter adds only independently observed listener and inventory state.
func settingsRuntimeDTO(snapshot settings.RuntimeSnapshot) map[string]any {
	defaults := settingsExecutionDTO(snapshot.Defaults.Execution)
	defaults["Network"] = settingsNetworkDTO(snapshot.Defaults.Network)
	defaults["Hardware"] = settingsHardwareDTO(snapshot.Defaults.Hardware)
	defaults["Transcoding"] = snapshot.Defaults.Transcoding
	defaults["TranscodingTempPath"] = snapshot.Defaults.TranscodingTempPath
	defaults["CodecConfigurations"] = snapshot.Defaults.CodecConfigurations
	defaults["EnableThrottling"] = snapshot.Defaults.EnableThrottling
	defaults["ExtractionThreadCount"] = snapshot.Defaults.ExtractionThreadCount
	defaults["CodecProfileLimits"] = snapshot.Defaults.CodecProfileLimits
	defaults["SubtitleOptions"] = snapshot.Defaults.SubtitleOptions
	effective := settingsExecutionDTO(snapshot.Execution)
	effective["Hardware"] = settingsHardwareDTO(snapshot.Hardware)
	effective["Transcoding"] = snapshot.Transcoding
	effective["CodecConfigurations"] = snapshot.CodecConfigurations
	effective["EnableThrottling"] = snapshot.EnableThrottling
	effective["ExtractionThreadCount"] = snapshot.ExtractionThreadCount
	effective["CodecProfileLimits"] = snapshot.CodecProfileLimits
	effective["SubtitleOptions"] = snapshot.SubtitleOptions
	var network, hardware any
	bindSource, portSource := "deployment", "deployment"
	if value := snapshot.Overrides.Network; value != nil {
		network = map[string]any{"BindHost": settingsOverrideValue(value.BindHost), "HttpPort": settingsOverrideValue(value.HttpPort)}
		bindSource, portSource = settingsSource(value.BindHost != nil), settingsSource(value.HttpPort != nil)
	}
	if value := snapshot.Overrides.Hardware; value != nil {
		hardware = settingsHardwareDTO(*value)
	}
	return map[string]any{
		"Defaults": defaults,
		"Overrides": map[string]any{
			"Network": network, "Hardware": hardware, "Threads": settingsOverrideValue(snapshot.Overrides.Threads),
			"H264": settingsCPUQualityOverrideDTO(snapshot.Overrides.H264), "HEVC": settingsCPUQualityOverrideDTO(snapshot.Overrides.HEVC),
			"SoftwareToneMapping":    settingsOverrideValue(snapshot.Overrides.SoftwareToneMapping),
			"VulkanToneMapping":      settingsOverrideValue(snapshot.Overrides.VulkanToneMapping),
			"Transcoding":            settingsOverrideValue(snapshot.Overrides.Transcoding),
			"TranscodingTempPath":    settingsOverrideValue(snapshot.Overrides.TranscodingTempPath),
			"SoftwareToneMapOptions": settingsOverrideValue(snapshot.Overrides.SoftwareToneMapOptions),
			"CodecConfigurations":    settingsOverrideValue(snapshot.Overrides.CodecConfigurations),
			"EnableThrottling":       settingsOverrideValue(snapshot.Overrides.EnableThrottling),
			"ExtractionThreadCount":  settingsOverrideValue(snapshot.Overrides.ExtractionThreadCount),
			"CodecProfileLimits":     settingsOverrideValue(snapshot.Overrides.CodecProfileLimits),
			"SubtitleOptions":        settingsOverrideValue(snapshot.Overrides.SubtitleOptions),
		},
		"Effective": effective,
		"Sources": map[string]any{
			"Network":  map[string]any{"BindHost": bindSource, "HttpPort": portSource},
			"Hardware": settingsSource(snapshot.Overrides.Hardware != nil), "Threads": settingsSource(snapshot.Overrides.Threads != nil),
			"H264": settingsSource(snapshot.Overrides.H264 != nil), "HEVC": settingsSource(snapshot.Overrides.HEVC != nil),
			"SoftwareToneMapping":    settingsSource(snapshot.Overrides.SoftwareToneMapping != nil),
			"VulkanToneMapping":      settingsSource(snapshot.Overrides.VulkanToneMapping != nil),
			"Transcoding":            settingsSource(snapshot.Overrides.Transcoding != nil),
			"TranscodingTempPath":    settingsSource(snapshot.Overrides.TranscodingTempPath != nil),
			"SoftwareToneMapOptions": settingsSource(snapshot.Overrides.SoftwareToneMapOptions != nil),
			"CodecConfigurations":    settingsSource(snapshot.Overrides.CodecConfigurations != nil),
			"EnableThrottling":       settingsSource(snapshot.Overrides.EnableThrottling != nil),
			"ExtractionThreadCount":  settingsSource(snapshot.Overrides.ExtractionThreadCount != nil),
			"CodecProfileLimits":     settingsSource(snapshot.Overrides.CodecProfileLimits != nil),
			"SubtitleOptions":        settingsSource(snapshot.Overrides.SubtitleOptions != nil),
		},
		"Effects": map[string]any{
			"Network": "restart", "Hardware": "next_admission", "Threads": "next_admission",
			"H264": "next_admission", "HEVC": "next_admission", "SoftwareToneMapping": "next_admission", "VulkanToneMapping": "next_admission",
			"Transcoding": "next_admission", "TranscodingTempPath": "restart", "SoftwareToneMapOptions": "next_admission", "CodecConfigurations": "next_admission",
			"CodecProfileLimits": "next_admission", "SubtitleOptions": "next_admission",
			"EnableThrottling": "next_hls_playback", "ExtractionThreadCount": "next_image_extraction",
		},
		"Applicability": map[string]any{
			"H264": "software_h264_output", "HEVC": "software_hevc_output",
			"SoftwareToneMapping": "software_filter", "VulkanToneMapping": "vulkan_filter",
			"Transcoding": "encoding_and_extraction", "TranscodingTempPath": "cache_root_after_restart",
			"SoftwareToneMapOptions": "software_tone_mapping", "CodecConfigurations": "software_video_encoding",
			"CodecProfileLimits": "new_video_encoding", "SubtitleOptions": "ass_text_negotiation",
			"EnableThrottling": "file_hls_video_encoding", "ExtractionThreadCount": "image_extraction",
		},
	}
}

func (s *Server) adminSettingsDTO(snapshot settings.Snapshot) map[string]any {
	result := settingsDTO(snapshot, s.cfg.Transcoding)
	runtime := settingsRuntimeDTO(snapshot.Runtime)
	network := s.ManagedHTTPBindingState(snapshot.Runtime.DesiredNetwork)
	var active any
	if network.Active != nil {
		active = map[string]any{
			"Configured": settingsNetworkDTO(network.Active.Configured), "BoundHost": network.Active.BoundHost,
			"HttpPort": network.Active.HttpPort, "Revision": network.Active.Revision,
		}
	}
	runtime["Network"] = map[string]any{
		"Desired": settingsNetworkDTO(snapshot.Runtime.DesiredNetwork), "Active": active,
		"RestartRequired": network.RestartRequired, "ReconnectURL": network.ReconnectURL,
	}
	devices := make([]map[string]any, 0)
	var available bool
	var code string
	if s.managedHardware != nil {
		var observations []managedHardwareDevice
		observations, available, code = s.managedHardware.status(snapshot.Runtime.Hardware)
		for _, device := range observations {
			devices = append(devices, map[string]any{"DeviceId": device.DeviceID, "Label": device.Label, "Available": device.Available, "Code": device.Code})
		}
	} else {
		_, available, code = s.resolveManagedHardware(snapshot.Runtime.Hardware)
	}
	runtime["Hardware"] = map[string]any{"Available": available, "Code": code, "Devices": devices}
	path := s.ManagedTranscodingTempPathState(snapshot.Runtime.DesiredTranscodingTempPath)
	runtime["TranscodingTempPath"] = map[string]any{"Desired": path.Desired,
		"Active": settingsOverrideValue(path.Active), "RestartRequired": path.RestartRequired}
	result["Runtime"] = runtime
	return result
}
