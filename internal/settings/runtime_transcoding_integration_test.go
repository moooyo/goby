package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/transcode"
)

func TestRuntimeTranscodingOverridesPersistReloadResetAndRetainLegacyRows(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	options := runtimeTestOptions()
	options.TranscodingTempPath = "/srv/deployment-transcodes"
	options.TranscodingTempPathExclusions = []string{"/srv/media", "/srv/scratch"}
	store, err := New(ctx, pool, owner, settingsTestDefaults(), "transcoding-host", options)
	if err != nil {
		t.Fatal(err)
	}
	var legacy []byte
	if err := pool.QueryRow(ctx, "SELECT runtime_overrides FROM managed_settings WHERE id=1").Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	var legacyShape map[string]json.RawMessage
	if json.Unmarshal(legacy, &legacyShape) != nil || len(legacyShape) != 7 {
		t.Fatal("the new migration rewrote an existing legacy runtime row")
	}
	initial := store.Snapshot()
	value := transcodingRuntimeTestOverrides()
	update := RuntimeUpdate{
		H264:                   Change[transcode.CPUQuality]{Present: true, Value: value.H264},
		HEVC:                   Change[transcode.CPUQuality]{Present: true, Value: value.HEVC},
		Transcoding:            Change[TranscodingPolicy]{Present: true, Value: value.Transcoding},
		TranscodingTempPath:    Change[string]{Present: true, Value: value.TranscodingTempPath},
		SoftwareToneMapOptions: Change[transcode.SoftwareToneMapOptions]{Present: true, Value: value.SoftwareToneMapOptions},
		CodecConfigurations:    Change[transcode.CodecConfigurations]{Present: true, Value: value.CodecConfigurations},
	}
	saved, err := store.Update(ctx, actor, runtimeTestRequest(initial, update))
	if err != nil || saved.Revision != initial.Revision+1 || saved.Runtime.Execution.DownMixAudioBoost != 2 ||
		saved.Runtime.Execution.H264.CRF != 1 || saved.Runtime.Execution.HEVC.CRF != 51 ||
		!saved.Runtime.Execution.H264.ThreadCountSet || saved.Runtime.Execution.H264.ThreadCount != 0 ||
		saved.Runtime.Execution.HEVC.ThreadCount != 16 || saved.Runtime.Transcoding.EnableHevcEncoding ||
		saved.Runtime.DesiredTranscodingTempPath != "/srv/private-transcodes" {
		t.Fatalf("advanced settings did not publish one complete execution snapshot: %v", err)
	}
	coherent := store.ValueSnapshot()
	if coherent.Transcoding != saved.Runtime.Transcoding || coherent.Execution != saved.Runtime.Execution ||
		coherent.CodecConfigurations != saved.Runtime.CodecConfigurations || coherent.DesiredTranscodingTempPath != saved.Runtime.DesiredTranscodingTempPath {
		t.Fatal("the ordinary playback projection mixed advanced runtime revisions")
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT runtime_overrides FROM managed_settings WHERE id=1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	persisted, err := decodeStoredRuntime(raw)
	if err != nil || persisted.Transcoding == nil || persisted.Transcoding.DownMixAudioBoost != 2 ||
		persisted.CodecConfigurations == nil || persisted.SoftwareToneMapOptions == nil || persisted.TranscodingTempPath == nil {
		t.Fatalf("advanced persistence lost a whole group: %v", err)
	}
	var currentShape map[string]json.RawMessage
	if json.Unmarshal(raw, &currentShape) != nil || len(currentShape) != 15 {
		t.Fatal("a new settings write did not use the fifteen-field contract")
	}
	row := settingsRowSnapshot(t, ctx, pool)
	noChange, err := store.Update(ctx, actor, runtimeTestRequest(saved, RuntimeUpdate{}))
	if err != nil || !reflect.DeepEqual(noChange, saved) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("an omitted advanced update changed persisted provenance: %v", err)
	}
	reloadedDefaults := runtimeTestOptions()
	reloadedDefaults.TranscodingTempPath = "/srv/restarted-default"
	reloadedDefaults.Transcoding.DownMixAudioBoost = 1.5
	reloadedDefaults.Execution.DownMixAudioBoost = 1.5
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "reloaded-host", reloadedDefaults)
	if err != nil || !equalRuntimeOverrides(reloaded.Snapshot().Runtime.Overrides, saved.Runtime.Overrides) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("reloading advanced settings rewrote their source facts: %v", err)
	}
	cleared, err := reloaded.Reset(ctx, actor, ResetRequest{Revision: saved.Revision, Fields: []Field{
		FieldTranscoding, FieldTranscodingTempPath, FieldSoftwareToneMapOptions, FieldCodecConfigurations,
	}})
	if err != nil || cleared.Runtime.Transcoding != reloadedDefaults.Transcoding || cleared.Runtime.Execution.DownMixAudioBoost != 1.5 ||
		cleared.Runtime.DesiredTranscodingTempPath != "/srv/restarted-default" ||
		cleared.Runtime.CodecConfigurations != transcode.DefaultCodecConfigurations() ||
		cleared.Runtime.Execution.SoftwareToneMapOptions != transcode.DefaultSoftwareToneMapOptions() ||
		cleared.Runtime.Execution.H264 != saved.Runtime.Execution.H264 || cleared.Runtime.Execution.HEVC != saved.Runtime.Execution.HEVC {
		t.Fatalf("selected advanced reset did not resume frozen defaults independently: %v", err)
	}
}

func TestRuntimeTranscodingReservedPathIsAtomicAndExclusionsAreFrozen(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	options := runtimeTestOptions()
	options.TranscodingTempPath = "/srv/transcodes"
	options.TranscodingTempPathExclusions = []string{"/srv/media"}
	store, err := New(ctx, pool, owner, settingsTestDefaults(), "reserved-path-host", options)
	if err != nil {
		t.Fatal(err)
	}
	options.TranscodingTempPathExclusions[0] = "/srv/changed"
	initial, row := store.Snapshot(), settingsRowSnapshot(t, ctx, pool)
	path := "/srv/media/child"
	request := runtimeTestRequest(initial, RuntimeUpdate{TranscodingTempPath: Change[string]{Present: true, Value: &path}})
	request.Overrides.ServerName = settingsTestPointer("Must not partially commit")
	mode := ServerNameCustom
	request.NameMode = &mode
	_, err = store.Update(ctx, actor, request)
	var invalid *ValidationError
	if !errors.Is(err, ErrInvalidInput) || !errors.As(err, &invalid) || invalid.Fields["Runtime.TranscodingTempPath"] == "" ||
		!reflect.DeepEqual(store.Snapshot(), initial) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("a reserved path or aliased exclusion allowed a partial update: %v", err)
	}
	empty := ""
	saved, err := store.Update(ctx, actor, runtimeTestRequest(initial, RuntimeUpdate{TranscodingTempPath: Change[string]{Present: true, Value: &empty}}))
	if err != nil || saved.Runtime.Overrides.TranscodingTempPath == nil || *saved.Runtime.Overrides.TranscodingTempPath != "" ||
		saved.Runtime.DesiredTranscodingTempPath != "/srv/transcodes" {
		t.Fatalf("explicit empty path did not persist separately from its resolved deployment default: %v", err)
	}
}

func TestRuntimeTranscodingDatabaseRejectsMalformedNewGroups(t *testing.T) {
	ctx, pool, _, store, actor := settingsRepository(t)
	value := transcodingRuntimeTestOverrides()
	initial := store.Snapshot()
	update := RuntimeUpdate{
		H264:                Change[transcode.CPUQuality]{Present: true, Value: value.H264},
		Transcoding:         Change[TranscodingPolicy]{Present: true, Value: value.Transcoding},
		CodecConfigurations: Change[transcode.CodecConfigurations]{Present: true, Value: value.CodecConfigurations},
	}
	if _, err := store.Update(ctx, actor, runtimeTestRequest(initial, update)); err != nil {
		t.Fatal(err)
	}
	row := settingsRowSnapshot(t, ctx, pool)
	for _, statement := range []string{
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{CodecConfigurations,0,CodecId}','null'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{CodecConfigurations,1,CodecId}','null'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{CodecConfigurations,1,CodecId}','"V-E-libx264"'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{H264,ThreadCountSet}','false'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{H264,ThreadCount}','null'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=runtime_overrides-'SoftwareToneMapOptions' WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{Transcoding,DownMixAudioBoost}','null'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{TranscodingTempPath}','"/srv/media/../cache"'::jsonb) WHERE id=1`,
	} {
		_, err := pool.Exec(ctx, statement)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" || settingsRowSnapshot(t, ctx, pool) != row {
			t.Fatalf("a malformed current runtime object bypassed its database shape check: %v", err)
		}
	}
}
