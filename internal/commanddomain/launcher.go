package commanddomain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	LauncherConfigFD            = 3
	LauncherWorkspaceFD         = 4
	LauncherToolFD              = 5
	LauncherPayloadFDBase       = 6
	LauncherConfigVersion       = 1
	MaxLauncherConfigBytes      = 16 << 10
	RequiredLauncherLandlockABI = 6

	maxLauncherArgs            = 512
	maxLauncherArgsBytes       = 12 << 10
	maxLauncherPayloads        = 32
	maxLauncherGroups          = 32
	maxLauncherHardwareDevices = 8
	maxLauncherCgroupPathBytes = 4096
	maxLauncherJSONDepth       = 64
)

var ErrLauncherConfig = errors.New("invalid native launcher configuration")

type LauncherWorkspaceIdentity struct {
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
}

type LauncherObjectIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type LauncherPayload struct {
	Role     string `json:"role"`
	Writable bool   `json:"writable"`
}

type LauncherHardwareDevice struct {
	Path    string `json:"path"`
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	MountID uint64 `json:"mount_id"`
	Rdev    uint64 `json:"rdev"`
}

// LauncherConfig describes the borrowed objects supplied to one native helper.
// Schema validation is not authority for executable, descriptor, mount, cgroup,
// device or broker identity. The native helper must verify the actual objects.
// Args excludes argv[0]; the verified executable establishes that value.
type LauncherConfig struct {
	Version    int                       `json:"version"`
	ToolSHA256 string                    `json:"tool_sha256"`
	Workspace  LauncherWorkspaceIdentity `json:"workspace"`
	UID        uint32                    `json:"uid"`
	GID        uint32                    `json:"gid"`
	Groups     []uint32                  `json:"groups"`
	Args       []string                  `json:"args"`
	Payload    []LauncherPayload         `json:"payload"`
	// Hardware selects a kernel device-policy class, not a verified codec
	// backend. "vaapi" grants approved DRM render nodes shared by VAAPI/QSV
	// and explicitly selected DRM-based Vulkan paths. Runtime support requires
	// separate positive evidence for the concrete driver and command path.
	Hardware        string                   `json:"hardware"`
	HardwareDevices []LauncherHardwareDevice `json:"hardware_devices"`
	CgroupPath      string                   `json:"cgroup_path"`
	BrokerPID       uint32                   `json:"broker_pid"`
	BrokerBootID    string                   `json:"broker_boot_id"`
	MountNamespace  LauncherObjectIdentity   `json:"mount_namespace"`
	Limits          *LauncherLimits          `json:"limits,omitempty"`
}

// ValidateLauncherConfig checks the bounded wire schema before any native work.
// Payload role multiplicity is bounded by the total descriptor count; the
// authoritative helper checks each descriptor's actual type and permissions.
func ValidateLauncherConfig(config LauncherConfig) error {
	if err := validateLauncherLimits(config.Version, config.Limits); err != nil {
		return err
	}
	if !launcherLowerHex(config.ToolSHA256, 64) {
		return invalidLauncherConfig("tool_sha256 must contain 64 lowercase hexadecimal digits")
	}
	if config.Workspace.Device == 0 || config.Workspace.Inode != 2 || config.Workspace.MountID == 0 {
		return invalidLauncherConfig("workspace must identify the fixed filesystem root")
	}
	if config.UID == 0 || config.GID == 0 || config.UID == ^uint32(0) || config.GID == ^uint32(0) {
		return invalidLauncherConfig("uid and gid must identify non-root accounts")
	}
	if len(config.Groups) > maxLauncherGroups {
		return invalidLauncherConfig("too many supplementary groups")
	}
	groups := make(map[uint32]struct{}, len(config.Groups))
	for _, group := range config.Groups {
		if group == 0 || group == ^uint32(0) {
			return invalidLauncherConfig("supplementary groups must identify valid non-root groups")
		}
		if _, duplicate := groups[group]; duplicate {
			return invalidLauncherConfig("duplicate supplementary group")
		}
		groups[group] = struct{}{}
	}
	if len(config.Args) == 0 || len(config.Args) > maxLauncherArgs {
		return invalidLauncherConfig("argument count must be between 1 and 512")
	}
	argumentBytes := 0
	for _, argument := range config.Args {
		if !launcherPlainText(argument) {
			return invalidLauncherConfig("arguments must be valid UTF-8 without control characters")
		}
		if len(argument) >= maxLauncherArgsBytes-argumentBytes {
			return invalidLauncherConfig("arguments exceed the 12 KiB terminated-byte budget")
		}
		argumentBytes += len(argument) + 1
	}
	if len(config.Payload) > maxLauncherPayloads {
		return invalidLauncherConfig("too many payload descriptors")
	}
	for _, payload := range config.Payload {
		switch payload.Role {
		case "source", "subtitle-input", "bitmap-input":
			if payload.Writable {
				return invalidLauncherConfig("input payload descriptors must be read-only")
			}
		case "progress-output", "workspace-output":
			if !payload.Writable {
				return invalidLauncherConfig("output payload descriptors must be writable")
			}
		default:
			return invalidLauncherConfig("unknown payload descriptor role")
		}
	}
	if err := validateLauncherHardware(config.Hardware, config.HardwareDevices); err != nil {
		return err
	}
	if len(config.CgroupPath) == 0 || len(config.CgroupPath) > maxLauncherCgroupPathBytes ||
		!launcherPlainText(config.CgroupPath) || !strings.HasPrefix(config.CgroupPath, "/") ||
		config.CgroupPath == "/" || strings.Contains(config.CgroupPath, "//") || path.Clean(config.CgroupPath) != config.CgroupPath {
		return invalidLauncherConfig("cgroup_path must be a clean bounded absolute non-root path")
	}
	if !launcherBootID(config.BrokerBootID) {
		return invalidLauncherConfig("broker_boot_id must be a canonical lowercase UUID")
	}
	if config.BrokerPID == 0 || config.BrokerPID > (1<<31)-1 {
		return invalidLauncherConfig("broker_pid must identify a positive Linux process")
	}
	if config.MountNamespace.Device == 0 || config.MountNamespace.Inode == 0 {
		return invalidLauncherConfig("mount_namespace identity must be positive")
	}
	return nil
}

// EncodeLauncherConfig validates the schema and its complete encoded byte cost.
func EncodeLauncherConfig(config LauncherConfig) ([]byte, error) {
	if err := ValidateLauncherConfig(config); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, invalidLauncherConfig("JSON encoding failed: %v", err)
	}
	if len(encoded) > MaxLauncherConfigBytes {
		return nil, invalidLauncherConfig("encoded configuration exceeds 16 KiB")
	}
	return encoded, nil
}

// DecodeLauncherConfig rejects duplicate object keys before typed decoding.
// All failures return an empty configuration, so no partial identity is usable.
func DecodeLauncherConfig(encoded []byte) (LauncherConfig, error) {
	if len(encoded) == 0 || len(encoded) > MaxLauncherConfigBytes || !utf8.Valid(encoded) {
		return LauncherConfig{}, invalidLauncherConfig("JSON must be valid UTF-8 within the 16 KiB budget")
	}
	tokens := json.NewDecoder(bytes.NewReader(encoded))
	tokens.UseNumber()
	if err := scanLauncherJSON(tokens, 0, false); err != nil {
		return LauncherConfig{}, err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return LauncherConfig{}, invalidLauncherConfig("JSON contains trailing data")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var config LauncherConfig
	if err := decoder.Decode(&config); err != nil {
		return LauncherConfig{}, invalidLauncherConfig("JSON decoding failed: %v", err)
	}
	if config.Version == LauncherConfigVersion {
		var fields map[string]json.RawMessage
		if json.Unmarshal(encoded, &fields) != nil {
			return LauncherConfig{}, invalidLauncherConfig("invalid version one object")
		}
		if _, present := fields["limits"]; present {
			return LauncherConfig{}, invalidLauncherConfig("version one cannot contain limits")
		}
	}
	if err := ValidateLauncherConfig(config); err != nil {
		return LauncherConfig{}, err
	}
	return config, nil
}

func validateLauncherHardware(hardware string, devices []LauncherHardwareDevice) error {
	switch hardware {
	case "none":
		if len(devices) != 0 {
			return invalidLauncherConfig("hardware none cannot contain device descriptors")
		}
		return nil
	case "vaapi", "nvidia":
		if len(devices) == 0 || len(devices) > maxLauncherHardwareDevices {
			return invalidLauncherConfig("hardware requires between 1 and 8 device descriptors")
		}
	default:
		return invalidLauncherConfig("unknown hardware mode")
	}
	seen := make(map[string]struct{}, len(devices))
	for _, device := range devices {
		if !launcherHardwarePath(hardware, device.Path) {
			return invalidLauncherConfig("hardware device path is outside the mode allowlist")
		}
		if device.Device == 0 || device.Inode == 0 || device.MountID == 0 || device.Rdev == 0 {
			return invalidLauncherConfig("hardware device identity must be positive")
		}
		if _, duplicate := seen[device.Path]; duplicate {
			return invalidLauncherConfig("duplicate hardware device path")
		}
		seen[device.Path] = struct{}{}
	}
	return nil
}

func launcherHardwarePath(hardware, devicePath string) bool {
	if hardware == "vaapi" {
		return launcherDecimalPath(devicePath, "/dev/dri/renderD", 128, 255)
	}
	if hardware == "nvidia" {
		switch devicePath {
		case "/dev/nvidiactl", "/dev/nvidia-uvm", "/dev/nvidia-uvm-tools":
			return true
		}
		return launcherNvidiaNumberedPath(devicePath)
	}
	return false
}

func launcherDecimalPath(value, prefix string, minimum, maximum int) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(value, prefix)
	number, err := strconv.Atoi(suffix)
	return err == nil && number >= minimum && number <= maximum && strconv.Itoa(number) == suffix
}

func launcherNvidiaNumberedPath(value string) bool {
	const prefix = "/dev/nvidia"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(value, prefix)
	number, err := strconv.ParseUint(suffix, 10, 32)
	return err == nil && strconv.FormatUint(number, 10) == suffix
}

func launcherPlainText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func launcherLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func launcherBootID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func scanLauncherJSON(decoder *json.Decoder, depth int, allowNull bool) error {
	if depth > maxLauncherJSONDepth {
		return invalidLauncherConfig("JSON nesting exceeds the schema limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return invalidLauncherConfig("invalid JSON token: %v", err)
	}
	if token == nil {
		if !allowNull {
			return invalidLauncherConfig("JSON null is allowed only for optional descriptor or group lists")
		}
		return nil
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return invalidLauncherConfig("invalid JSON object key: %v", err)
			}
			key, ok := keyToken.(string)
			if !ok || !launcherJSONKey(key) {
				return invalidLauncherConfig("JSON object keys must use lowercase schema names")
			}
			if _, duplicate := seen[key]; duplicate {
				return invalidLauncherConfig("duplicate JSON object key")
			}
			seen[key] = struct{}{}
			nullableList := key == "groups" || key == "payload" || key == "hardware_devices"
			if err := scanLauncherJSON(decoder, depth+1, nullableList); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanLauncherJSON(decoder, depth+1, false); err != nil {
				return err
			}
		}
	default:
		return invalidLauncherConfig("unexpected JSON delimiter")
	}
	closing, err := decoder.Token()
	if err != nil || delimiter == '{' && closing != json.Delim('}') || delimiter == '[' && closing != json.Delim(']') {
		return invalidLauncherConfig("invalid JSON closing delimiter")
	}
	return nil
}

func launcherJSONKey(key string) bool {
	if key == "" {
		return false
	}
	for _, character := range key {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func invalidLauncherConfig(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrLauncherConfig, fmt.Sprintf(format, values...))
}
