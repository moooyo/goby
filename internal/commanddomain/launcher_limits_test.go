package commanddomain

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// This explicit pre-extension shape preserves the complete version-1 wire
// order and tags. Comparing it with the new encoder is a compatibility check,
// rather than comparing the encoder against its own extended struct.
type launcherLimitsTestLegacyConfig struct {
	Version         int                       `json:"version"`
	ToolSHA256      string                    `json:"tool_sha256"`
	Workspace       LauncherWorkspaceIdentity `json:"workspace"`
	UID             uint32                    `json:"uid"`
	GID             uint32                    `json:"gid"`
	Groups          []uint32                  `json:"groups"`
	Args            []string                  `json:"args"`
	Payload         []LauncherPayload         `json:"payload"`
	Hardware        string                    `json:"hardware"`
	HardwareDevices []LauncherHardwareDevice  `json:"hardware_devices"`
	CgroupPath      string                    `json:"cgroup_path"`
	BrokerPID       uint32                    `json:"broker_pid"`
	BrokerBootID    string                    `json:"broker_boot_id"`
	MountNamespace  LauncherObjectIdentity    `json:"mount_namespace"`
}

func TestLauncherLimitsVersionOnePreservesExactLegacyWireBytes(t *testing.T) {
	config := launcherTestConfig()
	config.Version, config.Limits = 1, nil
	legacy := launcherLimitsTestLegacyConfig{
		Version: config.Version, ToolSHA256: config.ToolSHA256, Workspace: config.Workspace, UID: config.UID, GID: config.GID,
		Groups: config.Groups, Args: config.Args, Payload: config.Payload, Hardware: config.Hardware, HardwareDevices: config.HardwareDevices,
		CgroupPath: config.CgroupPath, BrokerPID: config.BrokerPID, BrokerBootID: config.BrokerBootID, MountNamespace: config.MountNamespace,
	}
	expected, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeLauncherConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, expected) || bytes.Contains(encoded, []byte("\"limits\"")) {
		t.Fatalf("the optional extension changed version-1 wire bytes: got=%s want=%s", encoded, expected)
	}
	decoded, err := DecodeLauncherConfig(expected)
	if err != nil || !reflect.DeepEqual(decoded, config) {
		t.Fatalf("the exact legacy configuration stopped decoding: config=%+v error=%v", decoded, err)
	}
}

func TestLauncherLimitsVersionTwoAnalysisRoundTrip(t *testing.T) {
	config := launcherLimitsTestAnalysisConfig()
	encoded, err := EncodeLauncherConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("\"limits\":{\"version\":1,\"class\":\"analysis-v1\"}")) {
		t.Fatalf("the fixed analysis class did not use its canonical bounded object: %s", encoded)
	}
	decoded, err := DecodeLauncherConfig(encoded)
	if err != nil || !reflect.DeepEqual(decoded, config) {
		t.Fatalf("version-2 analysis limits failed the wire round trip: config=%+v error=%v", decoded, err)
	}
}

func TestLauncherLimitsRejectsVersionAndClassMismatches(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*LauncherConfig)
	}{
		{name: "legacy version with limits", mutate: func(c *LauncherConfig) { c.Version = 1 }},
		{name: "analysis version without limits", mutate: func(c *LauncherConfig) { c.Limits = nil }},
		{name: "unknown wire version", mutate: func(c *LauncherConfig) { c.Version = 3 }},
		{name: "missing limits version", mutate: func(c *LauncherConfig) { c.Limits.Version = 0 }},
		{name: "future limits version", mutate: func(c *LauncherConfig) { c.Limits.Version = 2 }},
		{name: "negative limits version", mutate: func(c *LauncherConfig) { c.Limits.Version = -1 }},
		{name: "empty class", mutate: func(c *LauncherConfig) { c.Limits.Class = "" }},
		{name: "default class", mutate: func(c *LauncherConfig) { c.Limits.Class = "default" }},
		{name: "unknown analysis class", mutate: func(c *LauncherConfig) { c.Limits.Class = "analysis-v2" }},
		{name: "uppercase class", mutate: func(c *LauncherConfig) { c.Limits.Class = "ANALYSIS-V1" }},
		{name: "class control", mutate: func(c *LauncherConfig) { c.Limits.Class += "\n" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := launcherLimitsTestAnalysisConfig()
			test.mutate(&config)
			if err := ValidateLauncherConfig(config); !errors.Is(err, ErrLauncherConfig) {
				t.Fatalf("invalid limit-class policy passed schema validation: %v", err)
			}
			if encoded, err := EncodeLauncherConfig(config); encoded != nil || !errors.Is(err, ErrLauncherConfig) {
				t.Fatalf("invalid limit-class policy was encoded: bytes=%d error=%v", len(encoded), err)
			}
		})
	}
}

func TestLauncherLimitsStrictJSONRejectsClientValuesUnknownFieldsAndDuplicateKeys(t *testing.T) {
	encoded, err := EncodeLauncherConfig(launcherLimitsTestAnalysisConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := string(encoded)
	canonical := "\"limits\":{\"version\":1,\"class\":\"analysis-v1\"}"
	if !strings.Contains(base, canonical) {
		t.Fatal("the version-2 fixture omitted its exact canonical limits object")
	}
	for _, test := range []struct {
		name string
		from string
		to   string
	}{
		{name: "null limits", from: canonical, to: "\"limits\":null"},
		{name: "array limits", from: canonical, to: "\"limits\":[2147483648,64,0]"},
		{name: "unknown limits field", from: canonical, to: "\"limits\":{\"version\":1,\"class\":\"analysis-v1\",\"unknown\":1}"},
		{name: "client limit values", from: canonical, to: "\"limits\":{\"version\":1,\"class\":\"analysis-v1\",\"values\":[2147483648,64,0]}"},
		{name: "client address space", from: canonical, to: "\"limits\":{\"version\":1,\"class\":\"analysis-v1\",\"as\":4294967296}"},
		{name: "duplicate limits field", from: canonical, to: canonical + "," + canonical},
		{name: "duplicate class", from: canonical, to: "\"limits\":{\"version\":1,\"class\":\"analysis-v1\",\"class\":\"analysis-v1\"}"},
		{name: "escaped duplicate class", from: canonical, to: "\"limits\":{\"version\":1,\"class\":\"analysis-v1\",\"cl\\u0061ss\":\"analysis-v1\"}"},
		{name: "duplicate limits version", from: canonical, to: "\"limits\":{\"version\":1,\"version\":1,\"class\":\"analysis-v1\"}"},
		{name: "class field alias", from: canonical, to: "\"limits\":{\"version\":1,\"Class\":\"analysis-v1\"}"},
		{name: "null class", from: canonical, to: "\"limits\":{\"version\":1,\"class\":null}"},
		{name: "null version", from: canonical, to: "\"limits\":{\"version\":null,\"class\":\"analysis-v1\"}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcherTestDecodeInvalid(t, []byte(strings.Replace(base, test.from, test.to, 1)))
		})
	}
	launcherTestDecodeInvalid(t, append(append([]byte(nil), encoded...), []byte("{}")...))
}

func TestLauncherLimitsRetainsExistingEncodedAndArgumentBudgets(t *testing.T) {
	config := launcherLimitsTestAnalysisConfig()
	config.Args = []string{strings.Repeat("x", (12<<10)-1)}
	if _, err := EncodeLauncherConfig(config); err != nil {
		t.Fatalf("the fixed class reduced the existing argument-byte boundary: %v", err)
	}
	config.Args[0] += "x"
	if err := ValidateLauncherConfig(config); !errors.Is(err, ErrLauncherConfig) {
		t.Fatalf("version-2 limits bypassed the existing terminated-argument budget: %v", err)
	}
	config = launcherLimitsTestAnalysisConfig()
	config.Args = []string{strings.Repeat("<", 3000)}
	if err := ValidateLauncherConfig(config); err != nil {
		t.Fatal(err)
	}
	if encoded, err := EncodeLauncherConfig(config); encoded != nil || !errors.Is(err, ErrLauncherConfig) {
		t.Fatalf("version-2 limits bypassed the complete encoded-byte budget: bytes=%d error=%v", len(encoded), err)
	}
}

func TestLauncherLimitsClassSelectsOnlyFixedVersionedPolicies(t *testing.T) {
	if LimitsDefault != 0 || LimitsAnalysis != 1 || launcherAnalysisAddressSpace != 2147483648 ||
		launcherAnalysisOpenFiles != 64 || launcherAnalysisFileSize != 0 {
		t.Fatal("the fixed root-owned analysis policy or class discriminants changed")
	}
	version, limits, err := launcherLimitsForClass(LimitsDefault)
	if err != nil || version != 1 || limits != nil {
		t.Fatalf("default class changed the original wire policy: version=%d limits=%+v error=%v", version, limits, err)
	}
	version, limits, err = launcherLimitsForClass(LimitsAnalysis)
	if err != nil || version != 2 || limits == nil || limits.Version != 1 || limits.Class != "analysis-v1" {
		t.Fatalf("analysis class selected a noncanonical policy: version=%d limits=%+v error=%v", version, limits, err)
	}
	version, limits, err = launcherLimitsForClass(LimitsClass(255))
	if !errors.Is(err, ErrUnsafe) || version != 0 || limits != nil {
		t.Fatalf("unknown class selected executable limits: version=%d limits=%+v error=%v", version, limits, err)
	}
}

func launcherLimitsTestAnalysisConfig() LauncherConfig {
	config := launcherTestConfig()
	config.Version = 2
	config.Limits = &LauncherLimits{Version: 1, Class: "analysis-v1"}
	return config
}
