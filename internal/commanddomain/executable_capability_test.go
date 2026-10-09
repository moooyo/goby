package commanddomain

import (
	"encoding/json"
	"errors"
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
