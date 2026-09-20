package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

func runtimeTestOptions() RuntimeOptions {
	options := DefaultRuntimeOptions()
	options.Network = NetworkValues{BindHost: "127.0.0.1", HttpPort: 8097}
	options.Execution = transcode.DefaultExecutionOptions(3)
	options.AuthorizedDeviceIDs = []string{"device-alpha", "device-beta"}
	return options
}

func runtimeTestRequest(value Snapshot, change RuntimeUpdate) UpdateRequest {
	mode := value.ServerNameMode
	return UpdateRequest{Revision: value.Revision, Overrides: value.Overrides, NameMode: &mode, Runtime: &change}
}

func TestRuntimeOverridesPersistReloadAndResetAgainstCurrentDefaults(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	options := runtimeTestOptions()
	store, err := New(ctx, pool, owner, settingsTestDefaults(), "runtime-host-alpha", options)
	if err != nil {
		t.Fatal(err)
	}
	initial := store.Snapshot()
	if initial.Runtime.DesiredNetwork != options.Network || initial.Runtime.Execution != options.Execution || initial.Runtime.Overrides != (RuntimeOverrides{}) {
		t.Fatal("startup runtime defaults did not remain distinct from absent overrides")
	}
	host, port, threads, disabled := "::1", 9096, 7, false
	h264 := transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 20}
	change := RuntimeUpdate{
		Network:             Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{BindHost: &host, HttpPort: &port}},
		Threads:             Change[int]{Present: true, Value: &threads},
		H264:                Change[transcode.CPUQuality]{Present: true, Value: &h264},
		SoftwareToneMapping: Change[bool]{Present: true, Value: &disabled},
	}
	saved, err := store.Update(ctx, actor, runtimeTestRequest(initial, change))
	if err != nil || saved.Revision != initial.Revision+1 || saved.Runtime.DesiredNetwork != (NetworkValues{BindHost: "::1", HttpPort: 9096}) ||
		saved.Runtime.Execution.Threads != 7 || saved.Runtime.Execution.H264 != h264 || saved.Runtime.Execution.SoftwareToneMapping {
		t.Fatalf("runtime override batch did not publish one committed revision: %v", err)
	}
	host, port, threads, disabled, h264.CRF = "192.0.2.8", 1, 1, true, 35
	*saved.Runtime.Overrides.Network.HttpPort = 2
	*saved.Runtime.Overrides.Threads = 2
	saved.Runtime.Overrides.H264.CRF = 34
	*saved.Runtime.Overrides.SoftwareToneMapping = true
	published := store.Snapshot()
	if published.Runtime.DesiredNetwork.HttpPort != 9096 || *published.Runtime.Overrides.Network.HttpPort != 9096 ||
		published.Runtime.Execution.Threads != 7 || *published.Runtime.Overrides.Threads != 7 ||
		published.Runtime.Execution.H264.CRF != 20 || published.Runtime.Overrides.H264.CRF != 20 ||
		published.Runtime.Execution.SoftwareToneMapping || *published.Runtime.Overrides.SoftwareToneMapping {
		t.Fatal("runtime update or result retained caller-owned pointers")
	}
	for _, omitted := range []*RuntimeUpdate{nil, {}} {
		noop, err := store.Update(ctx, actor, UpdateRequest{Revision: published.Revision, Overrides: published.Overrides, Runtime: omitted})
		if err != nil || !reflect.DeepEqual(noop, published) {
			t.Fatalf("an omitted runtime update or group changed existing overrides: %v", err)
		}
	}
	var encoded []byte
	if err := pool.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 7 || string(persisted["Hardware"]) != "null" || string(persisted["HEVC"]) != "null" || string(persisted["VulkanToneMapping"]) != "null" {
		t.Fatal("runtime persistence lost its complete nullable group shape")
	}
	row := settingsRowSnapshot(t, ctx, pool)
	updatedDefaults := runtimeTestOptions()
	updatedDefaults.Network = NetworkValues{BindHost: "192.0.2.10", HttpPort: 10096}
	updatedDefaults.Execution = transcode.DefaultExecutionOptions(11)
	updatedDefaults.Execution.HEVC.Preset = "medium"
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "runtime-host-beta", updatedDefaults)
	if err != nil {
		t.Fatal(err)
	}
	after := reloaded.Snapshot()
	if settingsRowSnapshot(t, ctx, pool) != row || after.Revision != published.Revision || !after.UpdatedAt.Equal(published.UpdatedAt) ||
		!equalRuntimeOverrides(after.Runtime.Overrides, published.Runtime.Overrides) || after.Runtime.Defaults.Network != updatedDefaults.Network ||
		after.Runtime.DesiredNetwork != published.Runtime.DesiredNetwork || after.Runtime.Execution.Threads != 7 ||
		after.Runtime.Execution.H264 != published.Runtime.Execution.H264 || after.Runtime.Execution.HEVC != updatedDefaults.Execution.HEVC {
		t.Fatal("reload rewrote overrides or failed to apply new defaults only to absent groups")
	}
	selected, err := reloaded.Reset(ctx, actor, ResetRequest{Revision: after.Revision, Fields: []Field{FieldNetwork, FieldThreads}})
	if err != nil || selected.Runtime.Overrides.Network != nil || selected.Runtime.Overrides.Threads != nil ||
		selected.Runtime.DesiredNetwork != updatedDefaults.Network || selected.Runtime.Execution.Threads != 11 ||
		selected.Runtime.Execution.H264 != published.Runtime.Execution.H264 || selected.Runtime.Execution.SoftwareToneMapping {
		t.Fatalf("selective runtime reset changed an unrelated group: %v", err)
	}
	cleared, err := reloaded.Reset(ctx, actor, ResetRequest{Revision: selected.Revision, Fields: []Field{FieldRuntime}})
	if err != nil || cleared.Runtime.Overrides != (RuntimeOverrides{}) || cleared.Runtime.Execution != updatedDefaults.Execution ||
		cleared.Runtime.DesiredNetwork != updatedDefaults.Network {
		t.Fatalf("runtime reset did not resume current deployment defaults: %v", err)
	}
	row = settingsRowSnapshot(t, ctx, pool)
	noop, err := reloaded.Update(ctx, actor, runtimeTestRequest(cleared, ResetRuntimeUpdate()))
	if err != nil || !reflect.DeepEqual(noop, cleared) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("clearing absent runtime overrides changed the revision or timestamp: %v", err)
	}
}

func TestRuntimeUpdatesAreAtomicAndRecheckRevisionAndCurrentAuthority(t *testing.T) {
	ctx, pool, _, store, actor := settingsRepository(t)
	initial, row := store.Snapshot(), settingsRowSnapshot(t, ctx, pool)
	name := "Must not partially persist"
	invalid := RuntimeUpdate{Threads: Change[int]{Present: true, Value: settingsTestPointer(65)}}
	request := runtimeTestRequest(initial, invalid)
	request.Overrides.ServerName = &name
	if _, err := store.Update(ctx, actor, request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mixed native and invalid runtime update returned %v", err)
	}
	if settingsRowSnapshot(t, ctx, pool) != row || !reflect.DeepEqual(store.Snapshot(), initial) {
		t.Fatal("invalid runtime settings partially committed native settings")
	}
	threads := initial.Runtime.Execution.Threads
	valid := RuntimeUpdate{Threads: Change[int]{Present: true, Value: &threads}}
	saved, err := store.Update(ctx, actor, runtimeTestRequest(initial, valid))
	if err != nil || saved.Revision != initial.Revision+1 || saved.Runtime.Overrides.Threads == nil || saved.Runtime.Execution != initial.Runtime.Execution {
		t.Fatalf("an explicit runtime default was collapsed into absence: %v", err)
	}
	row = settingsRowSnapshot(t, ctx, pool)
	activity := settingsActivityRows(t, ctx, pool)
	noop, err := store.Update(ctx, actor, runtimeTestRequest(saved, valid))
	if err != nil || !reflect.DeepEqual(noop, saved) {
		t.Fatalf("an unchanged runtime override created another revision: %v", err)
	}
	if _, err := store.Update(ctx, actor, runtimeTestRequest(initial, valid)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("a stale runtime no-op bypassed CAS: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, actor, runtimeTestRequest(saved, valid)); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("a runtime no-op trusted a revoked actor snapshot: %v", err)
	}
	assertSettingsActivityUnchanged(t, ctx, pool, store, saved, row, activity)
}

func TestRuntimeConcurrentCASPublishesOneCompleteWinner(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	const count = 4
	type outcome struct {
		value Snapshot
		err   error
	}
	results := make(chan outcome, count)
	var workers sync.WaitGroup
	for index := range count {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			threads := index + 4
			crf := 20 + index
			quality := transcode.CPUQuality{Preset: "medium", RateControl: "capped_crf", CRF: crf}
			change := RuntimeUpdate{Threads: Change[int]{Present: true, Value: &threads}, H264: Change[transcode.CPUQuality]{Present: true, Value: &quality}}
			value, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Runtime: &change})
			results <- outcome{value: value, err: err}
		}(index)
	}
	workers.Wait()
	close(results)
	winners, conflicts := 0, 0
	var winner Snapshot
	for result := range results {
		switch {
		case result.err == nil:
			winners++
			winner = result.value
		case errors.Is(result.err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent runtime update returned %v", result.err)
		}
	}
	if winners != 1 || conflicts != count-1 || winner.Revision != 2 || winner.Runtime.Execution.H264.CRF != winner.Runtime.Execution.Threads+16 ||
		!reflect.DeepEqual(store.Snapshot(), winner) {
		t.Fatalf("runtime CAS did not preserve one complete winner: winners=%d conflicts=%d", winners, conflicts)
	}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha")
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot(), winner) {
		t.Fatalf("published runtime winner differs from persisted state: %v", err)
	}
}

func TestRuntimePostWriteFailureDoesNotPublishOrPersist(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	failure := errors.New("synthetic runtime write failure")
	reached := false
	store, err := New(ctx, pool, settingsHookOwner{owner: owner, afterWrite: func(library.OwnedTx) error {
		reached = true
		return failure
	}}, settingsTestDefaults(), "settings-host-alpha")
	if err != nil {
		t.Fatal(err)
	}
	initial, row, activity := store.Snapshot(), settingsRowSnapshot(t, ctx, pool), settingsActivityRows(t, ctx, pool)
	change := RuntimeUpdate{Threads: Change[int]{Present: true, Value: settingsTestPointer(8)}}
	if _, err := store.Update(ctx, actor, runtimeTestRequest(initial, change)); !reached || !errors.Is(err, failure) {
		t.Fatalf("runtime write did not reach the rollback fixture: %v", err)
	}
	assertSettingsActivityUnchanged(t, ctx, pool, store, initial, row, activity)
}

func TestRuntimeHardwareOrphanRemainsVisibleWithoutBlockingOtherChanges(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	options := runtimeTestOptions()
	store, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha", options)
	if err != nil {
		t.Fatal(err)
	}
	hardware := HardwareSelection{Decode: "vaapi", Encode: "software", DeviceID: "device-alpha"}
	change := RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &hardware}}
	saved, err := store.Update(ctx, actor, runtimeTestRequest(store.Snapshot(), change))
	if err != nil || saved.Runtime.Hardware != hardware || !saved.Runtime.HardwareAvailable {
		t.Fatalf("authorized hardware selection was not accepted: %v", err)
	}
	row := settingsRowSnapshot(t, ctx, pool)
	options.AuthorizedDeviceIDs = []string{"device-beta"}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha", options)
	if err != nil {
		t.Fatalf("a missing persisted device prevented startup: %v", err)
	}
	orphan := reloaded.Snapshot()
	if orphan.Runtime.Hardware != hardware || orphan.Runtime.HardwareAvailable || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatal("startup removed, substituted, or reported availability for an orphan hardware selection")
	}
	change.Threads = Change[int]{Present: true, Value: settingsTestPointer(8)}
	updated, err := reloaded.Update(ctx, actor, runtimeTestRequest(orphan, change))
	if err != nil || updated.Runtime.Hardware != hardware || updated.Runtime.HardwareAvailable || updated.Runtime.Execution.Threads != 8 {
		t.Fatalf("unchanged orphan hardware blocked an unrelated runtime update: %v", err)
	}
	unknown := HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: "device-unknown"}
	change = RuntimeUpdate{Hardware: Change[HardwareSelection]{Present: true, Value: &unknown}}
	row = settingsRowSnapshot(t, ctx, pool)
	if _, err := reloaded.Update(ctx, actor, runtimeTestRequest(updated, change)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a newly selected unauthorized device was accepted: %v", err)
	}
	if settingsRowSnapshot(t, ctx, pool) != row || !reflect.DeepEqual(reloaded.Snapshot(), updated) {
		t.Fatal("rejected hardware selection changed the persisted orphan")
	}
	options.AuthorizedDeviceIDs = []string{"device-alpha", "device-beta"}
	options.AvailableDeviceIDs = []string{}
	unavailable, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha", options)
	if err != nil || unavailable.Snapshot().Runtime.HardwareAvailable {
		t.Fatalf("explicitly empty device availability was treated as all authorized devices: %v", err)
	}
}

func TestRuntimeCompatibilityPortPresenceResetsOnlyTheRequestedSection(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	emby := configurationTestActor(t, ctx, pool, native, false)
	host, port := "127.0.0.2", 9096
	change := RuntimeUpdate{Network: Change[NetworkOverrides]{Present: true, Value: &NetworkOverrides{BindHost: &host, HttpPort: &port}},
		Threads: Change[int]{Present: true, Value: settingsTestPointer(5)}}
	saved, err := store.Update(ctx, native, runtimeTestRequest(store.Snapshot(), change))
	if err != nil {
		t.Fatal(err)
	}
	noop, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationPartial})
	if err != nil || !reflect.DeepEqual(noop, saved) {
		t.Fatalf("missing Partial port reset the network override: %v", err)
	}
	changed, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationPartial,
		HttpServerPortNumberPresent: true, HttpServerPortNumber: 10096})
	if err != nil || changed.Runtime.DesiredNetwork.BindHost != host || changed.Runtime.DesiredNetwork.HttpPort != 10096 || changed.Runtime.Execution.Threads != 5 {
		t.Fatalf("compatibility port update changed unrelated runtime values: %v", err)
	}
	reset, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationFull})
	if err != nil || reset.Runtime.DesiredNetwork.BindHost != host || reset.Runtime.DesiredNetwork.HttpPort != reset.Runtime.Defaults.Network.HttpPort ||
		reset.Runtime.Overrides.Network == nil || reset.Runtime.Overrides.Network.BindHost == nil || reset.Runtime.Overrides.Network.HttpPort != nil || reset.Runtime.Execution.Threads != 5 {
		t.Fatalf("missing Full port did not reset only the network port: %v", err)
	}
}

func TestRuntimeCompatibilityCRFTransitionsPreservePresetAndInactivePreference(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	emby := configurationTestActor(t, ctx, pool, native, false)
	quality := transcode.CPUQuality{Preset: "slow", RateControl: "bitrate", CRF: 31}
	change := RuntimeUpdate{H264: Change[transcode.CPUQuality]{Present: true, Value: &quality},
		Threads: Change[int]{Present: true, Value: settingsTestPointer(6)}}
	saved, err := store.Update(ctx, native, runtimeTestRequest(store.Snapshot(), change))
	if err != nil {
		t.Fatal(err)
	}
	row := settingsRowSnapshot(t, ctx, pool)
	noop, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationEncoding})
	if err != nil || !reflect.DeepEqual(noop, saved) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("absent CRF rewrote an inactive bitrate preference: %v", err)
	}
	capped, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationEncoding,
		H264CrfPresent: true, H264Crf: 21, TranscodingMaxWidthPresent: true, TranscodingMaxWidth: 1280,
		EnableSoftwareToneMapping: settingsTestPointer(false), EnableHardwareToneMapping: settingsTestPointer(false)})
	want := transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 21}
	if err != nil || capped.Runtime.Execution.H264 != want || capped.Runtime.Execution.Threads != 6 ||
		capped.Runtime.Execution.SoftwareToneMapping || capped.Runtime.Execution.VulkanToneMapping || capped.Encoding.TranscodingMaxWidth != 1280 {
		t.Fatalf("present compatibility CRF did not activate capped quality while preserving native settings: %v", err)
	}
	reset, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationEncoding})
	want.RateControl, want.CRF = "bitrate", 23
	if err != nil || reset.Runtime.Execution.H264 != want || reset.Encoding.TranscodingMaxWidth != 0 ||
		reset.Runtime.Execution.Threads != 6 || !reset.Runtime.Execution.SoftwareToneMapping || !reset.Runtime.Execution.VulkanToneMapping {
		t.Fatalf("named encoding omission did not reset its public scalar controls while retaining native settings: %v", err)
	}
	row = settingsRowSnapshot(t, ctx, pool)
	noop, err = store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationEncoding})
	if err != nil || !reflect.DeepEqual(noop, reset) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("repeated named encoding omission created another revision: %v", err)
	}
}
