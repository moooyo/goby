package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func runtimeInvalid(field, message string) error {
	return &ValidationError{Fields: map[string]string{"Runtime." + field: message}}
}

func validDeviceID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char < 'a' || char > 'z' {
			if char < 'A' || char > 'Z' {
				if char < '0' || char > '9' {
					if char != '-' && char != '_' {
						return false
					}
				}
			}
		}
	}
	return true
}

func validateNetworkOverrides(value *NetworkOverrides) error {
	if value == nil {
		return nil
	}
	if value.BindHost != nil && *value.BindHost != "" {
		address, err := netip.ParseAddr(*value.BindHost)
		if err != nil || address.Zone() != "" || address.String() != *value.BindHost {
			return runtimeInvalid("Network.BindHost", "use an empty wildcard host or one canonical IP address without a zone or port")
		}
	}
	if value.HttpPort != nil && (*value.HttpPort < 1 || *value.HttpPort > 65535) {
		return runtimeInvalid("Network.HttpPort", "use an integer port between 1 and 65535")
	}
	return nil
}

func validateHardwareSelection(value HardwareSelection) error {
	if value.Decode != "software" && value.Decode != "vaapi" || value.Encode != "software" && value.Encode != "vaapi" {
		return runtimeInvalid("Hardware", "select software or the approved AMD VAAPI backend for each axis")
	}
	if value.DeviceID != "" && !validDeviceID(value.DeviceID) || (value.Decode == "vaapi" || value.Encode == "vaapi") && value.DeviceID == "" {
		return runtimeInvalid("Hardware.DeviceId", "select a deployment-authorized device identity for hardware processing")
	}
	return nil
}

// The boost interval is Goby's bounded execution policy. Native source material
// establishes a reference default of two, but does not establish its range.
func ValidateTranscodingPolicy(value TranscodingPolicy) error {
	if math.IsNaN(value.DownMixAudioBoost) || math.IsInf(value.DownMixAudioBoost, 0) ||
		value.DownMixAudioBoost < 0.1 || value.DownMixAudioBoost > 10 {
		return runtimeInvalid("Transcoding.DownMixAudioBoost", "use a finite boost between 0.1 and 10")
	}
	return nil
}

func validateRuntimeOverrides(value RuntimeOverrides) error {
	if err := validateNetworkOverrides(value.Network); err != nil {
		return err
	}
	if value.Hardware != nil {
		if err := validateHardwareSelection(*value.Hardware); err != nil {
			return err
		}
	}
	if value.Threads != nil && (*value.Threads < 1 || *value.Threads > 64) {
		return runtimeInvalid("Threads", "use an integer thread setting between 1 and 64")
	}
	if value.ExtractionThreadCount != nil && media.ValidateImageExtractionThreadCount(*value.ExtractionThreadCount) != nil {
		return runtimeInvalid("ExtractionThreadCount", "use an integer image extraction thread setting between 0 and 16")
	}
	for _, quality := range []struct {
		codec, field string
		value        *transcode.CPUQuality
	}{{"h264", "H264", value.H264}, {"hevc", "HEVC", value.HEVC}} {
		if quality.value != nil && transcode.ValidateCPUQuality(quality.codec, *quality.value) != nil {
			return runtimeInvalid(quality.field, "use a complete supported CPU preset, rate-control mode, CRF between 1 and 51, and supported codec parameters")
		}
	}
	if value.Transcoding != nil {
		if err := ValidateTranscodingPolicy(*value.Transcoding); err != nil {
			return err
		}
	}
	if value.TranscodingTempPath != nil {
		if err := config.ValidateTranscodingTempPath(*value.TranscodingTempPath, nil); err != nil {
			return runtimeInvalid("TranscodingTempPath", err.Error())
		}
	}
	if value.SoftwareToneMapOptions != nil && transcode.ValidateSoftwareToneMapOptions(*value.SoftwareToneMapOptions) != nil {
		return runtimeInvalid("SoftwareToneMapOptions", "select a supported software tone-map algorithm and size limit")
	}
	if value.CodecConfigurations != nil && transcode.ValidateCodecConfigurations(*value.CodecConfigurations) != nil {
		return runtimeInvalid("CodecConfigurations", "configure both supported software encoders with distinct native identities and priorities between -1000 and 1000")
	}
	if value.CodecProfileLimits != nil && transcode.ValidateCodecProfileLimits(*value.CodecProfileLimits) != nil {
		return runtimeInvalid("CodecProfileLimits", "select H.264 baseline, main, or high and HEVC main or main10 as the supported profile ceilings")
	}
	return nil
}

// ValidateRuntimeUpdate is pure and does not authorize inventory identities.
// Store validates a changed selection against its immutable deployment scope.
func ValidateRuntimeUpdate(value RuntimeUpdate) error {
	if !value.Network.Present && value.Network.Value != nil || !value.Hardware.Present && value.Hardware.Value != nil ||
		!value.Threads.Present && value.Threads.Value != nil || !value.H264.Present && value.H264.Value != nil ||
		!value.HEVC.Present && value.HEVC.Value != nil || !value.SoftwareToneMapping.Present && value.SoftwareToneMapping.Value != nil ||
		!value.VulkanToneMapping.Present && value.VulkanToneMapping.Value != nil ||
		!value.Transcoding.Present && value.Transcoding.Value != nil || !value.TranscodingTempPath.Present && value.TranscodingTempPath.Value != nil ||
		!value.SoftwareToneMapOptions.Present && value.SoftwareToneMapOptions.Value != nil ||
		!value.CodecConfigurations.Present && value.CodecConfigurations.Value != nil ||
		!value.EnableThrottling.Present && value.EnableThrottling.Value != nil || !value.ExtractionThreadCount.Present && value.ExtractionThreadCount.Value != nil ||
		!value.CodecProfileLimits.Present && value.CodecProfileLimits.Value != nil || !value.SubtitleOptions.Present && value.SubtitleOptions.Value != nil {
		return runtimeInvalid("Input", "a value requires its explicit presence marker")
	}
	return validateRuntimeOverrides(applyRuntimeUpdate(RuntimeOverrides{}, value))
}

func prepareRuntimeOptions(options []RuntimeOptions) (RuntimeValues, map[string]bool, map[string]bool, bool, error) {
	if len(options) > 1 {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("Defaults", "supply one startup runtime configuration")
	}
	value := DefaultRuntimeOptions()
	if len(options) == 1 {
		value = options[0]
	}
	extractionThreads := 1
	if value.ExtractionThreadCount != nil {
		extractionThreads = *value.ExtractionThreadCount
	}
	if media.ValidateImageExtractionThreadCount(extractionThreads) != nil {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("ExtractionThreadCount", "startup image extraction thread setting is invalid")
	}
	if value.Transcoding == (TranscodingPolicy{}) {
		value.Transcoding = DefaultTranscodingPolicy()
	}
	if err := ValidateTranscodingPolicy(value.Transcoding); err != nil {
		return RuntimeValues{}, nil, nil, false, err
	}
	if err := config.ValidateTranscodingTempPath(value.TranscodingTempPath, value.TranscodingTempPathExclusions); err != nil {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("TranscodingTempPath", err.Error())
	}
	value.Execution.DownMixAudioBoost = value.Transcoding.DownMixAudioBoost
	var codecErr error
	value.CodecConfigurations, codecErr = transcode.ResolveCodecConfigurations(value.CodecConfigurations)
	if codecErr != nil {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("CodecConfigurations", "startup software encoder selection is invalid")
	}
	value.CodecProfileLimits, codecErr = transcode.ResolveCodecProfileLimits(value.CodecProfileLimits)
	if codecErr != nil {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("CodecProfileLimits", "startup video profile limits are invalid")
	}
	subtitles := DefaultRuntimeSubtitleOptions()
	if value.SubtitleOptions != nil {
		subtitles = *value.SubtitleOptions
	}
	if value.Execution.SoftwareToneMapOptions == (transcode.SoftwareToneMapOptions{}) {
		value.Execution.SoftwareToneMapOptions = transcode.DefaultSoftwareToneMapOptions()
	}
	// Deployment hostnames and ephemeral test listeners remain deployment-owned;
	// managed overrides have the narrower literal-IP and nonzero-port contract.
	if value.Network.HttpPort < 0 || value.Network.HttpPort > 65535 || len(value.Network.BindHost) > 253 ||
		!utf8.ValidString(value.Network.BindHost) || strings.IndexFunc(value.Network.BindHost, unicode.IsControl) >= 0 ||
		strings.TrimSpace(value.Network.BindHost) != value.Network.BindHost || strings.ContainsAny(value.Network.BindHost, "/\\[]@") {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("Network", "startup binding is invalid")
	}
	if transcode.ValidateExecutionOptions(value.Execution) != nil {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("Execution", "startup execution options are invalid")
	}
	if !value.LegacyHardwareDefault {
		if err := validateHardwareSelection(value.Hardware); err != nil {
			return RuntimeValues{}, nil, nil, false, err
		}
	} else if (value.Hardware.Decode == "software" || value.Hardware.Decode == "vaapi") && (value.Hardware.Encode == "software" || value.Hardware.Encode == "vaapi") ||
		value.Hardware.DeviceID != "" || len(value.Hardware.Decode) > 32 || len(value.Hardware.Encode) > 32 ||
		strings.IndexFunc(value.Hardware.Decode+value.Hardware.Encode, unicode.IsControl) >= 0 {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("Hardware", "legacy startup hardware identity must remain deployment-owned")
	}
	if len(value.AuthorizedDeviceIDs) > 8 || len(value.AvailableDeviceIDs) > 8 {
		return RuntimeValues{}, nil, nil, false, runtimeInvalid("Hardware", "startup inventory exceeds the device bound")
	}
	authorized, available := make(map[string]bool), make(map[string]bool)
	for _, id := range value.AuthorizedDeviceIDs {
		if !validDeviceID(id) || authorized[id] {
			return RuntimeValues{}, nil, nil, false, runtimeInvalid("Hardware", "startup inventory identities must be distinct and bounded")
		}
		authorized[id] = true
	}
	availableIDs := value.AvailableDeviceIDs
	if availableIDs == nil {
		availableIDs = value.AuthorizedDeviceIDs
	}
	for _, id := range availableIDs {
		if !authorized[id] || available[id] {
			return RuntimeValues{}, nil, nil, false, runtimeInvalid("Hardware", "available devices must be a distinct authorized subset")
		}
		available[id] = true
	}
	return RuntimeValues{Network: value.Network, Hardware: value.Hardware, Execution: value.Execution,
		Transcoding: value.Transcoding, TranscodingTempPath: value.TranscodingTempPath,
		CodecConfigurations: value.CodecConfigurations, CodecProfileLimits: value.CodecProfileLimits,
		SubtitleOptions: subtitles, EnableThrottling: value.EnableThrottling,
		ExtractionThreadCount: extractionThreads}, authorized, available, value.LegacyHardwareDefault, nil
}

func (s *Store) runtimeSnapshot(overrides RuntimeOverrides) RuntimeSnapshot {
	value := RuntimeSnapshot{Defaults: s.runtimeDefaults, Overrides: cloneRuntimeOverrides(overrides),
		DesiredNetwork: s.runtimeDefaults.Network, Hardware: s.runtimeDefaults.Hardware, Execution: s.runtimeDefaults.Execution,
		Transcoding: s.runtimeDefaults.Transcoding, DesiredTranscodingTempPath: s.runtimeDefaults.TranscodingTempPath,
		CodecConfigurations: s.runtimeDefaults.CodecConfigurations, CodecProfileLimits: s.runtimeDefaults.CodecProfileLimits,
		SubtitleOptions: s.runtimeDefaults.SubtitleOptions, EnableThrottling: s.runtimeDefaults.EnableThrottling,
		ExtractionThreadCount: s.runtimeDefaults.ExtractionThreadCount}
	if value.Transcoding == (TranscodingPolicy{}) {
		value.Transcoding = DefaultTranscodingPolicy()
	}
	value.CodecConfigurations, _ = transcode.ResolveCodecConfigurations(value.CodecConfigurations)
	value.CodecProfileLimits, _ = transcode.ResolveCodecProfileLimits(value.CodecProfileLimits)
	if value.Execution.SoftwareToneMapOptions == (transcode.SoftwareToneMapOptions{}) {
		value.Execution.SoftwareToneMapOptions = transcode.DefaultSoftwareToneMapOptions()
	}
	if network := overrides.Network; network != nil {
		if network.BindHost != nil {
			value.DesiredNetwork.BindHost = *network.BindHost
		}
		if network.HttpPort != nil {
			value.DesiredNetwork.HttpPort = *network.HttpPort
		}
	}
	if overrides.Hardware != nil {
		value.Hardware = *overrides.Hardware
	}
	value.HardwareAvailable = value.Hardware.Decode != "vaapi" && value.Hardware.Encode != "vaapi" || s.availableDevices[value.Hardware.DeviceID]
	if overrides.Hardware == nil && s.legacyHardwareDefault {
		value.HardwareAvailable = true
	}
	if overrides.Threads != nil {
		value.Execution.Threads = *overrides.Threads
	}
	if overrides.H264 != nil {
		value.Execution.H264 = *overrides.H264
	}
	if overrides.HEVC != nil {
		value.Execution.HEVC = *overrides.HEVC
	}
	if overrides.SoftwareToneMapping != nil {
		value.Execution.SoftwareToneMapping = *overrides.SoftwareToneMapping
	}
	if overrides.VulkanToneMapping != nil {
		value.Execution.VulkanToneMapping = *overrides.VulkanToneMapping
	}
	if overrides.Transcoding != nil {
		value.Transcoding = *overrides.Transcoding
	}
	value.Execution.DownMixAudioBoost = value.Transcoding.DownMixAudioBoost
	if overrides.TranscodingTempPath != nil && *overrides.TranscodingTempPath != "" {
		value.DesiredTranscodingTempPath = *overrides.TranscodingTempPath
	}
	if overrides.SoftwareToneMapOptions != nil {
		value.Execution.SoftwareToneMapOptions = *overrides.SoftwareToneMapOptions
	}
	if overrides.CodecConfigurations != nil {
		value.CodecConfigurations, _ = transcode.CanonicalCodecConfigurations(*overrides.CodecConfigurations)
	}
	if overrides.EnableThrottling != nil {
		value.EnableThrottling = *overrides.EnableThrottling
	}
	if overrides.ExtractionThreadCount != nil {
		value.ExtractionThreadCount = *overrides.ExtractionThreadCount
	}
	if overrides.CodecProfileLimits != nil {
		value.CodecProfileLimits = *overrides.CodecProfileLimits
	}
	if overrides.SubtitleOptions != nil {
		value.SubtitleOptions = *overrides.SubtitleOptions
	}
	return value
}

func (s *Store) authorizeHardwareChange(previous, next RuntimeOverrides) error {
	if equalPointer(previous.Hardware, next.Hardware) || next.Hardware == nil || next.Hardware.DeviceID == "" {
		return nil
	}
	if !s.authorizedDevices[next.Hardware.DeviceID] {
		return runtimeInvalid("Hardware.DeviceId", "select an identity from the startup-authorized AMD inventory")
	}
	return nil
}

func (s *Store) validateTranscodingTempPath(overrides RuntimeOverrides) error {
	value := s.runtimeDefaults.TranscodingTempPath
	if overrides.TranscodingTempPath != nil && *overrides.TranscodingTempPath != "" {
		value = *overrides.TranscodingTempPath
	}
	if err := config.ValidateTranscodingTempPath(value, s.transcodingTempPathExclusions); err != nil {
		return runtimeInvalid("TranscodingTempPath", err.Error())
	}
	return nil
}

func exactRuntimeObject(data []byte, fields ...string) (map[string]json.RawMessage, error) {
	return exactRuntimeObjectWithOptional(data, fields, nil)
}

func exactRuntimeObjectWithOptional(data []byte, required, optional []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrStoredSettings
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, field := range required {
		allowed[field] = true
	}
	for _, field := range optional {
		allowed[field] = true
	}
	values := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || values[key] != nil {
			return nil, ErrStoredSettings
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, ErrStoredSettings
		}
		values[key] = raw
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrStoredSettings
	}
	for _, field := range required {
		if values[field] == nil {
			return nil, ErrStoredSettings
		}
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrStoredSettings
	}
	return values, nil
}

// ValidateStoredRuntimeOverrides applies the exact persisted object contract
// without consulting runtime authority or hardware availability. Archive gates
// can reject malformed source facts before any target normalization occurs.
func ValidateStoredRuntimeOverrides(data []byte) error {
	_, err := decodeStoredRuntime(data)
	return err
}

func decodeStoredRuntime(data []byte) (RuntimeOverrides, error) {
	invalid := func() (RuntimeOverrides, error) {
		return RuntimeOverrides{}, fmt.Errorf("%w: invalid runtime overrides", ErrStoredSettings)
	}
	if len(data) == 0 || len(data) > 16384 || !utf8.Valid(data) {
		return invalid()
	}
	legacyFields := []string{"Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping"}
	values, err := exactRuntimeObject(data, legacyFields...)
	legacy := err == nil
	if legacy && len(data) > 8192 {
		return invalid()
	}
	if !legacy {
		fields := append(append([]string(nil), legacyFields...), "Transcoding", "TranscodingTempPath", "SoftwareToneMapOptions", "CodecConfigurations")
		values, err = exactRuntimeObject(data, fields...)
		if err != nil {
			fields = append(fields, "EnableThrottling", "ExtractionThreadCount", "CodecProfileLimits", "SubtitleOptions")
			values, err = exactRuntimeObject(data, fields...)
		}
	}
	if err != nil {
		return invalid()
	}
	for field, keys := range map[string][]string{
		"Network": {"BindHost", "HttpPort"}, "Hardware": {"Decode", "Encode", "DeviceID"},
		"H264": {"Preset", "RateControl", "CRF"}, "HEVC": {"Preset", "RateControl", "CRF"},
		"Transcoding":            {"EnableHevcEncoding", "EnableHardwareEncoding", "DownMixAudioBoost", "EnableSubtitleExtraction", "EnableOnTheFlyAttachmentExtraction"},
		"SoftwareToneMapOptions": {"ToneMapSizeLimit", "SoftwareToneMapAlgorithm"},
		"CodecProfileLimits":     {"H264", "HEVC"},
		"SubtitleOptions":        {"AllowAssToText"},
	} {
		if _, present := values[field]; !present {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(values[field]), []byte("null")) {
			continue
		}
		var group map[string]json.RawMessage
		var err error
		if !legacy && (field == "H264" || field == "HEVC") {
			group, err = exactRuntimeObjectWithOptional(values[field], keys, []string{"ThreadCount", "ThreadCountSet", "X264Options"})
		} else {
			group, err = exactRuntimeObject(values[field], keys...)
		}
		if err != nil {
			return invalid()
		}
		if !legacy && (field == "H264" || field == "HEVC") {
			setting, threadPresent := group["ThreadCountSet"]
			if threadPresent && !bytes.Equal(bytes.TrimSpace(setting), []byte("true")) || !threadPresent && group["ThreadCount"] != nil {
				return invalid()
			}
		}
		if field != "Network" {
			for _, value := range group {
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return invalid()
				}
			}
		}
	}
	var value RuntimeOverrides
	if json.Unmarshal(data, &value) != nil || validateRuntimeOverrides(value) != nil {
		return invalid()
	}
	if !legacy && value.CodecConfigurations != nil {
		var order []struct{ CodecId string }
		if json.Unmarshal(values["CodecConfigurations"], &order) != nil || len(order) != 2 ||
			order[0].CodecId != transcode.H264SoftwareCodecID || order[1].CodecId != transcode.HEVCSoftwareCodecID {
			return invalid()
		}
	}
	if legacy {
		for _, quality := range []*transcode.CPUQuality{value.H264, value.HEVC} {
			if quality == nil {
				continue
			}
			if quality.CRF < 18 || quality.CRF > 35 {
				return invalid()
			}
			switch quality.Preset {
			case "veryfast", "fast", "medium", "slow":
			default:
				return invalid()
			}
		}
	}
	return value, nil
}
