package settings

import (
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func validateRuntimeConfiguration(value ConfigurationMutation) error {
	invalid := func(message string) error {
		return &ValidationError{Fields: map[string]string{"Configuration": message}}
	}
	hasEncoding := value.H264CrfPresent || value.H264Crf != 0 || value.EnableSoftwareToneMapping != nil || value.EnableHardwareToneMapping != nil ||
		value.H264Preset != nil || value.EnableHevcEncoding != nil || value.EnableHardwareEncoding != nil || value.DownMixAudioBoost != nil ||
		value.EnableSubtitleExtraction != nil || value.EnableOnTheFlyAttachmentExtraction != nil || value.TranscodingTempPath != nil || value.CodecConfigurations != nil ||
		value.EnableThrottling != nil || value.ExtractionThreadCount != nil
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
	if value.AllowAssToText != nil && value.Section != ConfigurationSubtitles {
		return invalid("ASS text conversion belongs to named subtitle configuration")
	}
	if value.HttpServerPortNumberPresent && (value.HttpServerPortNumber < 1 || value.HttpServerPortNumber > 65535) {
		return invalid("HTTP port must be an integer between 1 and 65535")
	}
	if value.H264CrfPresent {
		quality := transcode.DefaultExecutionOptions(2).H264
		quality.RateControl, quality.CRF = "capped_crf", value.H264Crf
		if transcode.ValidateCPUQuality("h264", quality) != nil {
			return invalid("H264Crf must be an integer between 1 and 51")
		}
	}
	if value.H264Preset != nil {
		quality := transcode.DefaultExecutionOptions(2).H264
		quality.Preset = *value.H264Preset
		if transcode.ValidateCPUQuality("h264", quality) != nil {
			return invalid("H264Preset must select a supported native x264 preset")
		}
	}
	if value.DownMixAudioBoost != nil {
		policy := DefaultTranscodingPolicy()
		policy.DownMixAudioBoost = *value.DownMixAudioBoost
		if err := ValidateTranscodingPolicy(policy); err != nil {
			return invalid("DownMixAudioBoost must be a finite number between 0.1 and 10")
		}
	}
	if value.TranscodingTempPath != nil && config.ValidateTranscodingTempPath(*value.TranscodingTempPath, nil) != nil {
		return invalid("TranscodingTempPath must select an empty deployment default or a canonical dedicated Linux directory")
	}
	if value.CodecConfigurations != nil && transcode.ValidateCodecConfigurations(*value.CodecConfigurations) != nil {
		return invalid("CodecConfigurations must configure both supported software encoder identities exactly once")
	}
	if value.ExtractionThreadCount != nil && media.ValidateImageExtractionThreadCount(*value.ExtractionThreadCount) != nil {
		return invalid("ExtractionThreadCount must be an integer between 0 and 16 for image extraction")
	}
	return nil
}

func (s *Store) applyRuntimeConfiguration(previous RuntimeOverrides, mutation ConfigurationMutation) RuntimeOverrides {
	next := cloneRuntimeOverrides(previous)
	if mutation.AllowAssToText != nil {
		current := s.runtimeSnapshot(next).SubtitleOptions
		if current.AllowAssToText != *mutation.AllowAssToText {
			next.SubtitleOptions = &RuntimeSubtitleOptions{AllowAssToText: *mutation.AllowAssToText}
		}
	}
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
		if mutation.EnableThrottling != nil && *mutation.EnableThrottling != current.EnableThrottling {
			next.EnableThrottling = clonePointer(mutation.EnableThrottling)
		}
		if mutation.ExtractionThreadCount != nil && *mutation.ExtractionThreadCount != current.ExtractionThreadCount {
			next.ExtractionThreadCount = clonePointer(mutation.ExtractionThreadCount)
		}
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
		if mutation.H264Preset != nil {
			quality := s.runtimeSnapshot(next).Execution.H264
			if quality.Preset != *mutation.H264Preset {
				quality.Preset = *mutation.H264Preset
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
		// New compatibility controls preserve omitted fields, allowing older
		// named-encoding writers to coexist with native whole-group settings.
		policy := s.runtimeSnapshot(next).Transcoding
		previousPolicy := policy
		changedPolicy := false
		for _, field := range []struct {
			input  *bool
			target *bool
		}{{mutation.EnableHevcEncoding, &policy.EnableHevcEncoding}, {mutation.EnableHardwareEncoding, &policy.EnableHardwareEncoding},
			{mutation.EnableSubtitleExtraction, &policy.EnableSubtitleExtraction}, {mutation.EnableOnTheFlyAttachmentExtraction, &policy.EnableOnTheFlyAttachmentExtraction}} {
			if field.input != nil {
				*field.target, changedPolicy = *field.input, true
			}
		}
		if mutation.DownMixAudioBoost != nil {
			policy.DownMixAudioBoost, changedPolicy = *mutation.DownMixAudioBoost, true
		}
		if changedPolicy && policy != previousPolicy {
			next.Transcoding = &policy
		}
		if mutation.TranscodingTempPath != nil {
			desired := *mutation.TranscodingTempPath
			if desired == "" {
				desired = s.runtimeDefaults.TranscodingTempPath
			}
			if desired != s.runtimeSnapshot(next).DesiredTranscodingTempPath {
				next.TranscodingTempPath = clonePointer(mutation.TranscodingTempPath)
			}
		}
		if mutation.CodecConfigurations != nil {
			canonical, _ := transcode.CanonicalCodecConfigurations(*mutation.CodecConfigurations)
			if canonical != s.runtimeSnapshot(next).CodecConfigurations {
				next.CodecConfigurations = &canonical
			}
		}
	}
	return next
}
