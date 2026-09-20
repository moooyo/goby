package server

import "github.com/moooyo/goby/internal/settings"

// These fields have real backing behavior. Deployment paths, credentials,
// upstream migration markers, and unsupported encoding options are omitted.
func serverConfigurationDTO(view settings.Configuration) map[string]any {
	result := map[string]any{"IsStartupWizardCompleted": view.StartupWizardCompleted}
	snapshot := view.Snapshot
	result["PreferredMetadataLanguage"] = snapshot.Management.Metadata.PreferredMetadataLanguage
	result["MetadataCountryCode"] = snapshot.Management.Metadata.MetadataCountryCode
	result["EnableInternetProviders"] = snapshot.Management.Metadata.EnableInternetProviders
	result["HttpServerPortNumber"] = snapshot.Runtime.DesiredNetwork.HttpPort
	switch snapshot.ServerNameMode {
	case settings.ServerNameDeployment:
		result["ServerName"] = snapshot.Defaults.ServerName
	case settings.ServerNameCustom:
		result["ServerName"] = *snapshot.Overrides.ServerName
	case settings.ServerNameEmpty:
		result["ServerName"] = ""
	case settings.ServerNameUnset:
		// A configured null is omitted, even though public system information
		// and the native effective value continue to use the actual host name.
	}
	return result
}

func encodingConfigurationDTO(snapshot settings.Snapshot) map[string]any {
	result := map[string]any{
		"TranscodingMaxWidth":       snapshot.Encoding.TranscodingMaxWidth,
		"EnableSoftwareToneMapping": snapshot.Runtime.Execution.SoftwareToneMapping,
		"EnableHardwareToneMapping": snapshot.Runtime.Execution.VulkanToneMapping,
	}
	if snapshot.Runtime.Execution.H264.RateControl == "capped_crf" {
		result["H264Crf"] = snapshot.Runtime.Execution.H264.CRF
	}
	return result
}
