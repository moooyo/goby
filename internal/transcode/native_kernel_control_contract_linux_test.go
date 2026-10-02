//go:build linux

package transcode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeKernelControlEncodedBudgetDoesNotGrow(t *testing.T) {
	record := nativeKernelControlRecord{Version: 1, Token: strings.Repeat("a", 32), Stage: "reserved-native-intent"}
	encoded, err := nativeKernelEncodeControl(record)
	if err != nil || len(encoded) != nativeKernelControlBytes {
		t.Fatalf("bounded control record: bytes=%d error=%v", len(encoded), err)
	}
	var decoded nativeKernelControlRecord
	if err = json.Unmarshal(encoded, &decoded); err != nil || decoded.Stage != record.Stage {
		t.Fatalf("fixed-size control JSON: %v", err)
	}
	record.ParentPath = strings.Repeat("\\", 4096)
	record.Launcher.Path = strings.Repeat("\\", 4096)
	record.Tool.Path = strings.Repeat("\\", 4096)
	if _, err = nativeKernelEncodeControl(record); err == nil {
		t.Fatal("encoded escaping exceeded the control domain's fixed file budget")
	}
}

func TestNativeKernelControlUnknownOwnerCannotClose(t *testing.T) {
	if err := (&nativeKernelControl{unknown: true}).closeAfterWriters(); err == nil {
		t.Fatal("unknown writer became a closed control owner")
	}
	if _, err := newNativeKernelControl(nil, fixedVolumeIdentity{}); err == nil {
		t.Fatal("an ordinary metadata shape minted a fixed control filesystem")
	}
}
