package server

import (
	"strconv"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
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
		"Encoding":  map[string]any{"TranscodingMaxWidth": snapshot.Encoding.TranscodingMaxWidth},
		"UpdatedAt": snapshot.UpdatedAt.UTC(),
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
