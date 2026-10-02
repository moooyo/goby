package commanddomain

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestLauncherConfigWireContractAndRoundTrip(t *testing.T) {
	if LauncherConfigFD != 3 || LauncherWorkspaceFD != 4 || LauncherToolFD != 5 || LauncherPayloadFDBase != 6 ||
		LauncherConfigVersion != 1 || MaxLauncherConfigBytes != 16<<10 {
		t.Fatal("the launcher wire descriptor or version contract changed")
	}
	config := launcherTestConfig()
	config.Args = []string{"-i", "media with spaces.mkv", "\u5b57\u5e55 \U0001f600", ""}
	encoded, err := EncodeLauncherConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tool_sha256", "mount_id", "hardware_devices", "cgroup_path", "broker_pid", "broker_boot_id", "mount_namespace"} {
		if !bytes.Contains(encoded, []byte("\""+key+"\":")) {
			t.Fatalf("the encoded schema omitted the canonical %s key", key)
		}
	}
	decoded, err := DecodeLauncherConfig(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, config) {
		t.Fatalf("launcher configuration did not survive the wire round trip: got %+v want %+v", decoded, config)
	}
	escapedKey := strings.Replace(string(encoded), "\"uid\":65534", "\"u\\u0069d\":65534", 1)
	if decoded, err := DecodeLauncherConfig([]byte(escapedKey)); err != nil || decoded.UID != config.UID {
		t.Fatalf("an escaped canonical schema key failed to decode: config=%+v error=%v", decoded, err)
	}
	config.Groups = nil
	config.Payload = nil
	config.HardwareDevices = nil
	encoded, err = EncodeLauncherConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeLauncherConfig(encoded); err != nil || !reflect.DeepEqual(decoded, config) {
		t.Fatalf("optional nil lists did not survive their canonical null encoding: error=%v", err)
	}
}

func TestLauncherConfigRejectsInvalidIdentityAndCredentialFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*LauncherConfig)
	}{
		{name: "missing version", mutate: func(c *LauncherConfig) { c.Version = 0 }},
		{name: "future version", mutate: func(c *LauncherConfig) { c.Version++ }},
		{name: "negative version", mutate: func(c *LauncherConfig) { c.Version = -1 }},
		{name: "workspace device", mutate: func(c *LauncherConfig) { c.Workspace.Device = 0 }},
		{name: "workspace inode", mutate: func(c *LauncherConfig) { c.Workspace.Inode = 0 }},
		{name: "workspace subdirectory", mutate: func(c *LauncherConfig) { c.Workspace.Inode = 3 }},
		{name: "workspace mount", mutate: func(c *LauncherConfig) { c.Workspace.MountID = 0 }},
		{name: "root uid", mutate: func(c *LauncherConfig) { c.UID = 0 }},
		{name: "root gid", mutate: func(c *LauncherConfig) { c.GID = 0 }},
		{name: "invalid uid sentinel", mutate: func(c *LauncherConfig) { c.UID = ^uint32(0) }},
		{name: "invalid gid sentinel", mutate: func(c *LauncherConfig) { c.GID = ^uint32(0) }},
		{name: "zero broker pid", mutate: func(c *LauncherConfig) { c.BrokerPID = 0 }},
		{name: "broker pid uint32 sentinel", mutate: func(c *LauncherConfig) { c.BrokerPID = ^uint32(0) }},
		{name: "broker pid beyond Linux pid_t", mutate: func(c *LauncherConfig) { c.BrokerPID = 1 << 31 }},
		{name: "root supplementary group", mutate: func(c *LauncherConfig) { c.Groups = []uint32{100, 0} }},
		{name: "invalid supplementary group sentinel", mutate: func(c *LauncherConfig) { c.Groups = []uint32{^uint32(0)} }},
		{name: "duplicate supplementary group", mutate: func(c *LauncherConfig) { c.Groups = []uint32{100, 100} }},
		{name: "too many supplementary groups", mutate: func(c *LauncherConfig) { c.Groups = launcherTestGroups(33) }},
		{name: "mount namespace device", mutate: func(c *LauncherConfig) { c.MountNamespace.Device = 0 }},
		{name: "mount namespace inode", mutate: func(c *LauncherConfig) { c.MountNamespace.Inode = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := launcherTestConfig()
			test.mutate(&config)
			launcherTestInvalid(t, config)
		})
	}
	for _, groups := range [][]uint32{nil, {}, launcherTestGroups(32)} {
		config := launcherTestConfig()
		config.Groups = groups
		if err := ValidateLauncherConfig(config); err != nil {
			t.Fatalf("valid supplementary group boundary was rejected: groups=%v error=%v", groups, err)
		}
	}
	for _, brokerPID := range []uint32{1, (1 << 31) - 1} {
		config := launcherTestConfig()
		config.BrokerPID = brokerPID
		encoded, err := EncodeLauncherConfig(config)
		if err != nil {
			t.Fatalf("a valid Linux broker pid boundary was rejected: pid=%d error=%v", brokerPID, err)
		}
		decoded, err := DecodeLauncherConfig(encoded)
		if err != nil || decoded.BrokerPID != brokerPID {
			t.Fatalf("the broker pid boundary failed the wire round trip: pid=%d config=%+v error=%v", brokerPID, decoded, err)
		}
	}
}

func TestLauncherConfigRejectsNoncanonicalToolHashAndBootID(t *testing.T) {
	for _, hash := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64),
		strings.Repeat("g", 64), strings.Repeat("0", 63) + "\n"} {
		config := launcherTestConfig()
		config.ToolSHA256 = hash
		launcherTestInvalid(t, config)
	}
	for _, bootID := range []string{"", "0123456789abcdef0123456789abcdef", "01234567-89AB-cdef-0123-456789abcdef",
		"01234567_89ab-cdef-0123-456789abcdef", "01234567-89ab-cdef-0123-456789abcdeg",
		"01234567-89ab-cdef-0123-456789abcdef\n", "01234567-89ab-cdef-0123-456789abcde"} {
		config := launcherTestConfig()
		config.BrokerBootID = bootID
		launcherTestInvalid(t, config)
	}
	config := launcherTestConfig()
	config.ToolSHA256 = strings.Repeat("0123456789abcdef", 4)
	config.BrokerBootID = "00000000-0000-0000-0000-000000000000"
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatalf("canonical lowercase identity syntax was rejected: %v", err)
	}
}

func TestLauncherConfigArgumentCountAndTerminatedByteBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "nil argument list"},
		{name: "empty argument list", args: []string{}},
		{name: "too many arguments", args: launcherTestArguments(513)},
		{name: "single terminating byte overflow", args: []string{strings.Repeat("x", 12<<10)}},
		{name: "aggregate terminating byte overflow", args: []string{strings.Repeat("x", 6143), strings.Repeat("y", 6144)}},
		{name: "UTF-8 byte overflow", args: []string{strings.Repeat("\u754c", 4096)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := launcherTestConfig()
			config.Args = test.args
			launcherTestInvalid(t, config)
		})
	}
	for _, args := range [][]string{
		{""},
		launcherTestArguments(512),
		{strings.Repeat("x", (12<<10)-1)},
		{strings.Repeat("x", 6143), strings.Repeat("y", 6143)},
		{strings.Repeat("\u754c", 4095)},
	} {
		config := launcherTestConfig()
		config.Args = args
		if _, err := EncodeLauncherConfig(config); err != nil {
			t.Fatalf("a valid argument boundary was rejected: count=%d error=%v", len(args), err)
		}
	}
}

func TestLauncherConfigRejectsArgumentControlsAndInvalidUTF8(t *testing.T) {
	for _, argument := range []string{"nul\x00suffix", "tab\tsuffix", "line\nsuffix", "return\rsuffix",
		"escape\x1bsuffix", "delete\x7fsuffix", "next-line\u0085suffix", "unit\x1fsuffix",
		string([]byte{'a', 0xff}), string([]byte{0xc0, 0xaf})} {
		config := launcherTestConfig()
		config.Args = []string{argument}
		launcherTestInvalid(t, config)
	}
	config := launcherTestConfig()
	config.Args = []string{"spaces are allowed", "caption \U0001f600", "zero-width\u200btext"}
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatalf("valid non-control Unicode arguments were rejected: %v", err)
	}
}

func TestLauncherConfigPayloadRoleAccessAndMultiplicity(t *testing.T) {
	for _, role := range []string{"source", "subtitle-input", "bitmap-input", "progress-output", "workspace-output"} {
		t.Run(role, func(t *testing.T) {
			config := launcherTestConfig()
			writable := role == "progress-output" || role == "workspace-output"
			config.Payload = []LauncherPayload{{Role: role, Writable: writable}, {Role: role, Writable: writable}}
			if err := ValidateLauncherConfig(config); err != nil {
				t.Fatalf("repeated valid payload role was rejected: %v", err)
			}
			config.Payload[0].Writable = !writable
			launcherTestInvalid(t, config)
		})
	}
	for _, role := range []string{"", "Source", "subtitle", "output", "source\n"} {
		config := launcherTestConfig()
		config.Payload = []LauncherPayload{{Role: role}}
		launcherTestInvalid(t, config)
	}
	config := launcherTestConfig()
	config.Payload = nil
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatalf("a command without payload descriptors was rejected: %v", err)
	}
	config.Payload = make([]LauncherPayload, 32)
	for index := range config.Payload {
		config.Payload[index] = LauncherPayload{Role: "progress-output", Writable: true}
	}
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatalf("the maximum bounded payload set was rejected: %v", err)
	}
	config.Payload = append(config.Payload, LauncherPayload{Role: "progress-output", Writable: true})
	launcherTestInvalid(t, config)
}

func TestLauncherConfigHardwareModeAndDeviceAllowlist(t *testing.T) {
	for _, test := range []struct {
		hardware string
		paths    []string
	}{
		{hardware: "none"},
		{hardware: "vaapi", paths: []string{"/dev/dri/renderD128", "/dev/dri/renderD255"}},
		{hardware: "nvidia", paths: []string{"/dev/nvidia0", "/dev/nvidia15", "/dev/nvidia16", "/dev/nvidia31", "/dev/nvidia4294967295", "/dev/nvidiactl", "/dev/nvidia-uvm", "/dev/nvidia-uvm-tools"}},
	} {
		config := launcherTestConfig()
		config.Hardware = test.hardware
		config.HardwareDevices = launcherTestDevices(test.paths...)
		encoded, err := EncodeLauncherConfig(config)
		if err != nil {
			t.Fatalf("allowlisted hardware configuration was rejected: mode=%s error=%v", test.hardware, err)
		}
		decoded, err := DecodeLauncherConfig(encoded)
		if err != nil || !reflect.DeepEqual(decoded, config) {
			t.Fatalf("hardware configuration failed the wire round trip: mode=%s error=%v", test.hardware, err)
		}
	}
	for _, hardware := range []string{"", "None", "cuda", "vulkan", "vaapi", "nvidia"} {
		config := launcherTestConfig()
		config.Hardware = hardware
		launcherTestInvalid(t, config)
	}
	config := launcherTestConfig()
	config.HardwareDevices = launcherTestDevices("/dev/dri/renderD128")
	launcherTestInvalid(t, config)
	for _, test := range []struct {
		hardware string
		path     string
	}{
		{hardware: "vaapi", path: "/dev/dri/renderD127"},
		{hardware: "vaapi", path: "/dev/dri/renderD256"},
		{hardware: "vaapi", path: "/dev/dri/renderD0128"},
		{hardware: "vaapi", path: "/dev/dri/renderD+128"},
		{hardware: "vaapi", path: "/dev/dri/renderD128/"},
		{hardware: "vaapi", path: "/dev/dri/card0"},
		{hardware: "vaapi", path: "/dev/nvidia0"},
		{hardware: "nvidia", path: "/dev/nvidia4294967296"},
		{hardware: "nvidia", path: "/dev/nvidia-1"},
		{hardware: "nvidia", path: "/dev/nvidia00"},
		{hardware: "nvidia", path: "/dev/nvidia016"},
		{hardware: "nvidia", path: "/dev/nvidia04294967295"},
		{hardware: "nvidia", path: "/dev/nvidia+0"},
		{hardware: "nvidia", path: "/dev/nvidiactl/"},
		{hardware: "nvidia", path: "/dev/nvidia-uvm-tools-extra"},
		{hardware: "nvidia", path: "/dev/dri/renderD128"},
		{hardware: "nvidia", path: "/dev//nvidia0"},
		{hardware: "nvidia", path: "/dev/nvidia0\x00"},
	} {
		config := launcherTestConfig()
		config.Hardware = test.hardware
		config.HardwareDevices = launcherTestDevices(test.path)
		launcherTestInvalid(t, config)
	}
}

func TestLauncherConfigHardwareIdentityUniquenessAndCount(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*LauncherHardwareDevice)
	}{
		{name: "device", mutate: func(d *LauncherHardwareDevice) { d.Device = 0 }},
		{name: "inode", mutate: func(d *LauncherHardwareDevice) { d.Inode = 0 }},
		{name: "mount", mutate: func(d *LauncherHardwareDevice) { d.MountID = 0 }},
		{name: "rdev", mutate: func(d *LauncherHardwareDevice) { d.Rdev = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := launcherTestConfig()
			config.Hardware = "vaapi"
			config.HardwareDevices = launcherTestDevices("/dev/dri/renderD128")
			test.mutate(&config.HardwareDevices[0])
			launcherTestInvalid(t, config)
		})
	}
	config := launcherTestConfig()
	config.Hardware = "nvidia"
	config.HardwareDevices = launcherTestDevices("/dev/nvidia0", "/dev/nvidia0")
	launcherTestInvalid(t, config)
	paths := make([]string, 8)
	for index := range paths {
		paths[index] = "/dev/nvidia" + strconv.Itoa(index)
	}
	config.HardwareDevices = launcherTestDevices(paths...)
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatalf("the maximum allowlisted hardware set was rejected: %v", err)
	}
	config.HardwareDevices = append(config.HardwareDevices, launcherTestDevices("/dev/nvidia8")...)
	launcherTestInvalid(t, config)
}

func TestLauncherConfigCgroupPathCanonicalityAndBoundary(t *testing.T) {
	for _, cgroupPath := range []string{"", "/", "relative/path", "//job", "/job//command", "/job/./command",
		"/job/../command", "/job/command/", "/job\ncommand", "/job\tcommand", "/job\x00command",
		string([]byte{'/', 0xff}), "/" + strings.Repeat("x", 4096)} {
		config := launcherTestConfig()
		config.CgroupPath = cgroupPath
		launcherTestInvalid(t, config)
	}
	for _, cgroupPath := range []string{"/job", "/goby/jobs/command-01", "/goby/\u5b57\u5e55", "/" + strings.Repeat("x", 4095)} {
		config := launcherTestConfig()
		config.CgroupPath = cgroupPath
		if err := ValidateLauncherConfig(config); err != nil {
			t.Fatalf("a canonical bounded cgroup path was rejected: length=%d error=%v", len(cgroupPath), err)
		}
	}
}

func TestLauncherConfigEncodedBudgetIncludesEscapingAndWhitespace(t *testing.T) {
	config := launcherTestConfig()
	config.Args = []string{strings.Repeat("<", 3000)}
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatalf("the argument byte budget was incorrectly charged for JSON escaping: %v", err)
	}
	if encoded, err := EncodeLauncherConfig(config); encoded != nil || !errors.Is(err, ErrLauncherConfig) {
		t.Fatalf("JSON escaping exceeded the encoded budget without rejection: bytes=%d error=%v", len(encoded), err)
	}
	config = launcherTestConfig()
	encoded, err := EncodeLauncherConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	padded := append(append([]byte(nil), encoded...), bytes.Repeat([]byte{' '}, MaxLauncherConfigBytes-len(encoded))...)
	if decoded, err := DecodeLauncherConfig(padded); err != nil || !reflect.DeepEqual(decoded, config) {
		t.Fatalf("the exact encoded-byte boundary was rejected: error=%v", err)
	}
	launcherTestDecodeInvalid(t, append(padded, ' '))
}

func TestLauncherConfigStrictJSONRejectsDuplicatesAliasesUnknownFieldsAndTrailingData(t *testing.T) {
	encoded, err := EncodeLauncherConfig(launcherTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := string(encoded)
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "empty"},
		{name: "null root", data: "null"},
		{name: "array root", data: "[]"},
		{name: "scalar root", data: "true"},
		{name: "truncated object", data: base[:len(base)-1]},
		{name: "trailing object", data: base + "{}"},
		{name: "trailing null", data: base + " null"},
		{name: "trailing invalid token", data: base + " x"},
		{name: "unknown root field", data: strings.Replace(base, "\"version\":1", "\"version\":1,\"unexpected\":true", 1)},
		{name: "unknown workspace field", data: strings.Replace(base, "\"inode\":2", "\"inode\":2,\"unexpected\":true", 1)},
		{name: "unknown payload field", data: strings.Replace(base, "\"role\":\"source\"", "\"role\":\"source\",\"unexpected\":true", 1)},
		{name: "unknown namespace field", data: strings.Replace(base, "\"device\":4", "\"device\":4,\"unexpected\":true", 1)},
		{name: "duplicate root key", data: strings.Replace(base, "\"uid\":65534", "\"uid\":65534,\"uid\":65533", 1)},
		{name: "escaped duplicate key", data: strings.Replace(base, "\"uid\":65534", "\"uid\":65534,\"u\\u0069d\":65533", 1)},
		{name: "duplicate nested key", data: strings.Replace(base, "\"inode\":2", "\"inode\":2,\"inode\":2", 1)},
		{name: "duplicate payload key", data: strings.Replace(base, "\"role\":\"source\"", "\"role\":\"source\",\"role\":\"bitmap-input\"", 1)},
		{name: "duplicate namespace key", data: strings.Replace(base, "\"device\":4", "\"device\":4,\"device\":4", 1)},
		{name: "uppercase field alias", data: strings.Replace(base, "\"uid\":65534", "\"UID\":65534", 1)},
		{name: "case-folded duplicate alias", data: strings.Replace(base, "\"uid\":65534", "\"uid\":65534,\"UID\":65533", 1)},
		{name: "missing version", data: strings.Replace(base, "\"version\":1,", "", 1)},
		{name: "null version", data: strings.Replace(base, "\"version\":1", "\"version\":null", 1)},
		{name: "overflowing uid", data: strings.Replace(base, "\"uid\":65534", "\"uid\":4294967296", 1)},
		{name: "negative uid", data: strings.Replace(base, "\"uid\":65534", "\"uid\":-1", 1)},
		{name: "fractional uid", data: strings.Replace(base, "\"uid\":65534", "\"uid\":1.5", 1)},
		{name: "string uid", data: strings.Replace(base, "\"uid\":65534", "\"uid\":\"65534\"", 1)},
		{name: "overflowing object identity", data: strings.Replace(base, "\"inode\":2", "\"inode\":18446744073709551616", 1)},
		{name: "null args", data: strings.Replace(base, "\"args\":[\"-version\"]", "\"args\":null", 1)},
		{name: "null argument element", data: strings.Replace(base, "\"args\":[\"-version\"]", "\"args\":[null]", 1)},
		{name: "numeric argument element", data: strings.Replace(base, "\"args\":[\"-version\"]", "\"args\":[1]", 1)},
		{name: "boolean argument element", data: strings.Replace(base, "\"args\":[\"-version\"]", "\"args\":[true]", 1)},
		{name: "null payload access mode", data: strings.Replace(base, "\"writable\":false", "\"writable\":null", 1)},
		{name: "escaped argument newline", data: strings.Replace(base, "\"-version\"", "\"line\\nfeed\"", 1)},
		{name: "escaped argument NUL", data: strings.Replace(base, "\"-version\"", "\"nul\\u0000suffix\"", 1)},
		{name: "invalid raw UTF-8", data: strings.Replace(base, "-version", string([]byte{0xff}), 1)},
		{name: "excessive nesting", data: strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)},
	} {
		t.Run(test.name, func(t *testing.T) { launcherTestDecodeInvalid(t, []byte(test.data)) })
	}
	config := launcherTestConfig()
	config.Hardware = "vaapi"
	config.HardwareDevices = launcherTestDevices("/dev/dri/renderD128")
	hardware, err := EncodeLauncherConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"\"rdev\":5,\"unexpected\":1", "\"rdev\":5,\"rdev\":5"} {
		launcherTestDecodeInvalid(t, []byte(strings.Replace(string(hardware), "\"rdev\":5", replacement, 1)))
	}
}

func launcherTestConfig() LauncherConfig {
	return LauncherConfig{
		Version:    LauncherConfigVersion,
		ToolSHA256: strings.Repeat("a", 64),
		Workspace:  LauncherWorkspaceIdentity{Device: 1, Inode: 2, MountID: 3},
		UID:        65534,
		GID:        65534,
		Groups:     []uint32{100},
		Args:       []string{"-version"},
		Payload: []LauncherPayload{
			{Role: "source"},
			{Role: "progress-output", Writable: true},
			{Role: "workspace-output", Writable: true},
		},
		Hardware:       "none",
		CgroupPath:     "/goby/job/command",
		BrokerPID:      1,
		BrokerBootID:   "01234567-89ab-cdef-0123-456789abcdef",
		MountNamespace: LauncherObjectIdentity{Device: 4, Inode: 5},
	}
}

func launcherTestDevices(paths ...string) []LauncherHardwareDevice {
	if paths == nil {
		return nil
	}
	devices := make([]LauncherHardwareDevice, len(paths))
	for index, devicePath := range paths {
		devices[index] = LauncherHardwareDevice{Path: devicePath, Device: 1, Inode: uint64(index + 1), MountID: 3, Rdev: 5}
	}
	return devices
}

func launcherTestGroups(count int) []uint32 {
	groups := make([]uint32, count)
	for index := range groups {
		groups[index] = uint32(index + 1)
	}
	return groups
}

func launcherTestArguments(count int) []string {
	arguments := make([]string, count)
	for index := range arguments {
		arguments[index] = "x"
	}
	return arguments
}

func launcherTestInvalid(t *testing.T, config LauncherConfig) {
	t.Helper()
	if err := ValidateLauncherConfig(config); !errors.Is(err, ErrLauncherConfig) {
		t.Fatalf("invalid launcher configuration passed schema validation: %v", err)
	}
	if encoded, err := EncodeLauncherConfig(config); encoded != nil || !errors.Is(err, ErrLauncherConfig) {
		t.Fatalf("invalid launcher configuration was encoded: bytes=%d error=%v", len(encoded), err)
	}
}

func launcherTestDecodeInvalid(t *testing.T, encoded []byte) {
	t.Helper()
	decoded, err := DecodeLauncherConfig(encoded)
	if !errors.Is(err, ErrLauncherConfig) || !reflect.DeepEqual(decoded, LauncherConfig{}) {
		t.Fatalf("invalid JSON returned usable configuration: config=%+v error=%v", decoded, err)
	}
}
