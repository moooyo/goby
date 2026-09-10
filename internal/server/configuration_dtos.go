package server

import "github.com/moooyo/goby/internal/settings"

// These fields have real backing behavior. Deployment paths, credentials,
// upstream migration markers, and unsupported encoding options are omitted.
func serverConfigurationDTO(view settings.Configuration) map[string]any {
	result := map[string]any{"IsStartupWizardCompleted": view.StartupWizardCompleted}
	snapshot := view.Snapshot
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
	return map[string]any{"TranscodingMaxWidth": snapshot.Encoding.TranscodingMaxWidth}
}
