package settings

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestRuntimeRestoreReplacesOnlyTargetHostChoicesInCallerTransaction(t *testing.T) {
	ctx, pool, _, _, _ := settingsRepository(t)
	source := RuntimeOverrides{
		Network:  &NetworkOverrides{BindHost: settingsTestPointer("192.0.2.99"), HttpPort: settingsTestPointer(9196)},
		Hardware: &HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: "source-amd"}, Threads: settingsTestPointer(32),
		H264:                &transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 20},
		HEVC:                &transcode.CPUQuality{Preset: "medium", RateControl: "bitrate", CRF: 30},
		SoftwareToneMapping: settingsTestPointer(false), VulkanToneMapping: settingsTestPointer(true),
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE managed_settings SET runtime_overrides=$1,revision=9007199254740993`, encoded); err != nil {
		t.Fatal(err)
	}
	target := TargetHostSettings{Network: &NetworkOverrides{BindHost: settingsTestPointer("127.0.0.1"), HttpPort: settingsTestPointer(10096)},
		Hardware: &HardwareSelection{Decode: "software", Encode: "software"}, Threads: settingsTestPointer(3)}
	before := settingsRowSnapshot(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	readTarget, err := ReadTargetHostSettings(ctx, tx)
	if err != nil || !reflect.DeepEqual(readTarget.Network, source.Network) || !reflect.DeepEqual(readTarget.Hardware, source.Hardware) || !reflect.DeepEqual(readTarget.Threads, source.Threads) {
		t.Fatalf("target transaction capture lost current host choices: %v", err)
	}
	changed, err := NormalizeRestoredHostSettings(ctx, tx, target)
	if err != nil || !changed {
		t.Fatalf("trusted target normalization failed: %v", err)
	}
	var revision int64
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT revision,runtime_overrides FROM managed_settings`).Scan(&revision, &raw); err != nil {
		t.Fatal(err)
	}
	after, err := decodeStoredRuntime(raw)
	if err != nil || revision != 9007199254740994 || !reflect.DeepEqual(after.Network, target.Network) ||
		!reflect.DeepEqual(after.Hardware, target.Hardware) || !reflect.DeepEqual(after.Threads, target.Threads) ||
		!reflect.DeepEqual(after.H264, source.H264) || !reflect.DeepEqual(after.HEVC, source.HEVC) ||
		!reflect.DeepEqual(after.SoftwareToneMapping, source.SoftwareToneMapping) || !reflect.DeepEqual(after.VulkanToneMapping, source.VulkanToneMapping) {
		t.Fatal("restore imported source host choices or changed portable encoding policy")
	}
	changed, err = NormalizeRestoredHostSettings(ctx, tx, target)
	if err != nil || changed {
		t.Fatal("identical target normalization was not a strict no-op")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if settingsRowSnapshot(t, ctx, pool) != before {
		t.Fatal("host normalization escaped its caller transaction")
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	changed, err = NormalizeRestoredHostSettings(ctx, tx, TargetHostSettings{})
	if err != nil || !changed {
		t.Fatalf("offline target defaults normalization failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	after, err = decodeStoredRuntime(raw)
	if err != nil || after.Network != nil || after.Hardware != nil || after.Threads != nil || !reflect.DeepEqual(after.H264, source.H264) {
		t.Fatal("offline restore did not preserve target deployment ownership")
	}
}

func TestRuntimeRestoreRejectsRevisionOverflowAndOwnsTargetCapture(t *testing.T) {
	ctx, pool, _, _, _ := settingsRepository(t)
	if _, err := pool.Exec(ctx, `UPDATE managed_settings SET revision=$1`, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	before := settingsRowSnapshot(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if changed, err := NormalizeRestoredHostSettings(ctx, tx, TargetHostSettings{}); err != nil || changed {
		t.Fatal("unchanged maximum revision was not preserved")
	}
	if _, err := NormalizeRestoredHostSettings(ctx, tx, TargetHostSettings{Threads: settingsTestPointer(4)}); !errors.Is(err, ErrStoredSettings) {
		t.Fatal("host normalization advanced an exhausted revision")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if settingsRowSnapshot(t, ctx, pool) != before {
		t.Fatal("failed host normalization changed stored settings")
	}
	snapshot := Snapshot{Runtime: RuntimeSnapshot{Overrides: runtimeTestOverrides()}}
	capture := CaptureTargetHostSettings(snapshot)
	*snapshot.Runtime.Overrides.Threads = 64
	*snapshot.Runtime.Overrides.Network.HttpPort = 1
	snapshot.Runtime.Overrides.Hardware.DeviceID = "mutated"
	if *capture.Threads != 6 || *capture.Network.HttpPort != 9096 || capture.Hardware.DeviceID != "device-alpha" {
		t.Fatal("target capture retained mutable snapshot pointers")
	}
}
