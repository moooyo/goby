package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

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
	for _, quality := range []struct {
		codec, field string
		value        *transcode.CPUQuality
	}{{"h264", "H264", value.H264}, {"hevc", "HEVC", value.HEVC}} {
		if quality.value != nil && transcode.ValidateCPUQuality(quality.codec, *quality.value) != nil {
			return runtimeInvalid(quality.field, "use a complete supported CPU preset, rate-control mode and CRF between 18 and 35")
		}
	}
	return nil
}

// ValidateRuntimeUpdate is pure and does not authorize inventory identities.
// Store validates a changed selection against its immutable deployment scope.
func ValidateRuntimeUpdate(value RuntimeUpdate) error {
	if !value.Network.Present && value.Network.Value != nil || !value.Hardware.Present && value.Hardware.Value != nil ||
		!value.Threads.Present && value.Threads.Value != nil || !value.H264.Present && value.H264.Value != nil ||
		!value.HEVC.Present && value.HEVC.Value != nil || !value.SoftwareToneMapping.Present && value.SoftwareToneMapping.Value != nil ||
		!value.VulkanToneMapping.Present && value.VulkanToneMapping.Value != nil {
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
	return RuntimeValues{Network: value.Network, Hardware: value.Hardware, Execution: value.Execution}, authorized, available, value.LegacyHardwareDefault, nil
}

func (s *Store) runtimeSnapshot(overrides RuntimeOverrides) RuntimeSnapshot {
	value := RuntimeSnapshot{Defaults: s.runtimeDefaults, Overrides: cloneRuntimeOverrides(overrides),
		DesiredNetwork: s.runtimeDefaults.Network, Hardware: s.runtimeDefaults.Hardware, Execution: s.runtimeDefaults.Execution}
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

func exactRuntimeObject(data []byte, fields ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrStoredSettings
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	values := make(map[string]json.RawMessage, len(fields))
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
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(values) != len(fields) {
		return nil, ErrStoredSettings
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
	if len(data) == 0 || len(data) > 8192 || !utf8.Valid(data) {
		return invalid()
	}
	values, err := exactRuntimeObject(data, "Network", "Hardware", "Threads", "H264", "HEVC", "SoftwareToneMapping", "VulkanToneMapping")
	if err != nil {
		return invalid()
	}
	for field, keys := range map[string][]string{
		"Network": {"BindHost", "HttpPort"}, "Hardware": {"Decode", "Encode", "DeviceID"},
		"H264": {"Preset", "RateControl", "CRF"}, "HEVC": {"Preset", "RateControl", "CRF"},
	} {
		if bytes.Equal(bytes.TrimSpace(values[field]), []byte("null")) {
			continue
		}
		group, err := exactRuntimeObject(values[field], keys...)
		if err != nil {
			return invalid()
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
	return value, nil
}
