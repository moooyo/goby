package settings

import "github.com/moooyo/goby/internal/transcode"

func validateRuntimeConfiguration(value ConfigurationMutation) error {
	invalid := func(message string) error {
		return &ValidationError{Fields: map[string]string{"Configuration": message}}
	}
	hasEncoding := value.H264CrfPresent || value.H264Crf != 0 || value.EnableSoftwareToneMapping != nil || value.EnableHardwareToneMapping != nil
	hasNetwork := value.HttpServerPortNumberPresent || value.HttpServerPortNumber != 0
	if !value.HttpServerPortNumberPresent && value.HttpServerPortNumber != 0 || !value.H264CrfPresent && value.H264Crf != 0 {
		return invalid("a runtime configuration value requires its explicit presence marker")
	}
	if hasNetwork && value.Section != ConfigurationFull && value.Section != ConfigurationPartial {
		return invalid("HTTP port belongs to server configuration")
	}
	if hasEncoding && value.Section != ConfigurationEncoding && value.Section != ConfigurationPartial {
		return invalid("encoding controls require named encoding or partial configuration")
	}
	if value.HttpServerPortNumberPresent && (value.HttpServerPortNumber < 1 || value.HttpServerPortNumber > 65535) {
		return invalid("HTTP port must be an integer between 1 and 65535")
	}
	if value.H264CrfPresent {
		quality := transcode.DefaultExecutionOptions(2).H264
		quality.RateControl, quality.CRF = "capped_crf", value.H264Crf
		if transcode.ValidateCPUQuality("h264", quality) != nil {
			return invalid("H264Crf must be an integer between 18 and 35")
		}
	}
	return nil
}

func (s *Store) applyRuntimeConfiguration(previous RuntimeOverrides, mutation ConfigurationMutation) RuntimeOverrides {
	next := cloneRuntimeOverrides(previous)
	if mutation.HttpServerPortNumberPresent {
		if next.Network == nil {
			next.Network = &NetworkOverrides{}
		}
		next.Network.HttpPort = clonePointer(&mutation.HttpServerPortNumber)
	} else if mutation.Section == ConfigurationFull && next.Network != nil && next.Network.HttpPort != nil {
		next.Network.HttpPort = nil
		if next.Network.BindHost == nil {
			next.Network = nil
		}
	}
	if mutation.Section == ConfigurationEncoding || mutation.Section == ConfigurationPartial {
		current := s.runtimeSnapshot(next)
		if mutation.H264CrfPresent {
			quality := current.Execution.H264
			quality.RateControl, quality.CRF = "capped_crf", mutation.H264Crf
			next.H264 = &quality
		} else if mutation.Section == ConfigurationEncoding && current.Execution.H264.RateControl == "capped_crf" {
			// The exposed scalar activates capped CRF. Its omission disables
			// only that activation, retaining the native-only preset. An
			// already inactive scalar is a strict no-op, including its CRF.
			quality := current.Execution.H264
			defaults := transcode.DefaultExecutionOptions(current.Execution.Threads).H264
			quality.RateControl, quality.CRF = defaults.RateControl, defaults.CRF
			if quality == s.runtimeDefaults.Execution.H264 {
				next.H264 = nil
			} else {
				next.H264 = &quality
			}
		}
		if mutation.EnableSoftwareToneMapping != nil {
			next.SoftwareToneMapping = clonePointer(mutation.EnableSoftwareToneMapping)
		} else if mutation.Section == ConfigurationEncoding {
			next.SoftwareToneMapping = nil
		}
		if mutation.EnableHardwareToneMapping != nil {
			next.VulkanToneMapping = clonePointer(mutation.EnableHardwareToneMapping)
		} else if mutation.Section == ConfigurationEncoding {
			next.VulkanToneMapping = nil
		}
	}
	return next
}
