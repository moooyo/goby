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

func settingsExecutionDTO(value transcode.ExecutionOptions) map[string]any {
	return map[string]any{
		"Threads": value.Threads, "H264": value.H264, "HEVC": value.HEVC,
		"SoftwareToneMapping": value.SoftwareToneMapping, "VulkanToneMapping": value.VulkanToneMapping,
	}
}

// The shared snapshot owns every default and new-admission choice. The HTTP
// adapter adds only independently observed listener and inventory state.
func settingsRuntimeDTO(snapshot settings.RuntimeSnapshot) map[string]any {
	defaults := settingsExecutionDTO(snapshot.Defaults.Execution)
	defaults["Network"] = settingsNetworkDTO(snapshot.Defaults.Network)
	defaults["Hardware"] = settingsHardwareDTO(snapshot.Defaults.Hardware)
	effective := settingsExecutionDTO(snapshot.Execution)
	effective["Hardware"] = settingsHardwareDTO(snapshot.Hardware)
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
			"H264": settingsOverrideValue(snapshot.Overrides.H264), "HEVC": settingsOverrideValue(snapshot.Overrides.HEVC),
			"SoftwareToneMapping": settingsOverrideValue(snapshot.Overrides.SoftwareToneMapping),
			"VulkanToneMapping":   settingsOverrideValue(snapshot.Overrides.VulkanToneMapping),
		},
		"Effective": effective,
		"Sources": map[string]any{
			"Network":  map[string]any{"BindHost": bindSource, "HttpPort": portSource},
			"Hardware": settingsSource(snapshot.Overrides.Hardware != nil), "Threads": settingsSource(snapshot.Overrides.Threads != nil),
			"H264": settingsSource(snapshot.Overrides.H264 != nil), "HEVC": settingsSource(snapshot.Overrides.HEVC != nil),
			"SoftwareToneMapping": settingsSource(snapshot.Overrides.SoftwareToneMapping != nil),
			"VulkanToneMapping":   settingsSource(snapshot.Overrides.VulkanToneMapping != nil),
		},
		"Effects": map[string]any{
			"Network": "restart", "Hardware": "next_admission", "Threads": "next_admission",
			"H264": "next_admission", "HEVC": "next_admission", "SoftwareToneMapping": "next_admission", "VulkanToneMapping": "next_admission",
		},
		"Applicability": map[string]any{
			"H264": "software_h264_output", "HEVC": "software_hevc_output",
			"SoftwareToneMapping": "software_filter", "VulkanToneMapping": "vulkan_filter",
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
	if s.managedHardware != nil {
		for _, device := range s.managedHardware.devices() {
			devices = append(devices, map[string]any{"DeviceId": device.DeviceID, "Label": device.Label, "Available": device.Available, "Code": device.Code})
		}
	}
	_, available, code := s.resolveManagedHardware(snapshot.Runtime.Hardware)
	runtime["Hardware"] = map[string]any{"Available": available, "Code": code, "Devices": devices}
	result["Runtime"] = runtime
	return result
}
