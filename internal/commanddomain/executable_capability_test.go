package commanddomain

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestExecutableCapabilityZeroAndNilRejectOpen(t *testing.T) {
	var zero ExecutableCapability
	for _, test := range []struct {
		name       string
		capability *ExecutableCapability
	}{
		{name: "nil"},
		{name: "zero", capability: &zero},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := test.capability.Open()
			if file != nil {
				_ = file.Close()
			}
			if file != nil || !errors.Is(err, ErrUnsafe) {
				t.Fatalf("an unissued capability opened an executable: file=%v error=%v", file, err)
			}
		})
	}
}

func TestExecutableCapabilityRejectsJSONTrustTransfer(t *testing.T) {
	var zero ExecutableCapability
	for _, value := range []any{zero, &zero} {
		if encoded, err := json.Marshal(value); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("an executable capability was serialized as transferable trust: bytes=%s error=%v", encoded, err)
		}
	}
	for _, encoded := range []string{"{}", "null", "\"opaque\"", "{\"path\":\"/usr/bin/tool\",\"approved\":true}"} {
		var decoded ExecutableCapability
		if err := json.Unmarshal([]byte(encoded), &decoded); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("JSON reconstructed executable trust from %s", encoded)
		}
		file, err := decoded.Open()
		if file != nil {
			_ = file.Close()
		}
		if file != nil || !errors.Is(err, ErrUnsafe) {
			t.Fatalf("rejected JSON left a usable capability: file=%v error=%v", file, err)
		}
	}
}

func TestExecutableCapabilityUnavailableOutsideLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("the unsupported-platform contract applies outside Linux")
	}
	capability, err := NewExecutableCapability(nil, "/approved/tool", strings.Repeat("a", 64))
	if capability != nil {
		_ = capability.Close()
	}
	if capability != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("the unsupported platform issued executable trust: capability=%v error=%v", capability, err)
	}
}
