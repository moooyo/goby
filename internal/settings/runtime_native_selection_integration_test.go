package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/transcode"
)

func TestRuntimeNativeSelectionPersistsReloadsAndResetsOnlySelectedGroups(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	options := runtimeTestOptions()
	store, err := New(ctx, pool, owner, settingsTestDefaults(), "native-selection-host", options)
	if err != nil {
		t.Fatal(err)
	}
	initial := store.Snapshot()
	selected := nativeSelectionRuntimeOverrides()
	saved, err := store.Update(ctx, actor, runtimeTestRequest(initial, RuntimeUpdate{
		EnableThrottling:      Change[bool]{Present: true, Value: selected.EnableThrottling},
		ExtractionThreadCount: Change[int]{Present: true, Value: selected.ExtractionThreadCount},
		CodecProfileLimits:    Change[transcode.CodecProfileLimits]{Present: true, Value: selected.CodecProfileLimits},
		SubtitleOptions:       Change[RuntimeSubtitleOptions]{Present: true, Value: selected.SubtitleOptions},
	}))
	if err != nil || saved.Revision != initial.Revision+1 || saved.Runtime.CodecProfileLimits != *selected.CodecProfileLimits || saved.Runtime.SubtitleOptions.AllowAssToText ||
		!saved.Runtime.EnableThrottling || saved.Runtime.ExtractionThreadCount != 0 {
		t.Fatalf("a selected output policy was not committed in one revision: %v", err)
	}
	coherent := store.ValueSnapshot()
	if coherent.CodecProfileLimits != saved.Runtime.CodecProfileLimits || coherent.SubtitleOptions != saved.Runtime.SubtitleOptions ||
		coherent.EnableThrottling != saved.Runtime.EnableThrottling || coherent.ExtractionThreadCount != saved.Runtime.ExtractionThreadCount {
		t.Fatal("playback readers combined native policy from different revisions")
	}
	row := settingsRowSnapshot(t, ctx, pool)
	newProfile := transcode.CodecProfileLimits{H264: "high", HEVC: "main10"}
	if _, err := store.Update(ctx, actor, runtimeTestRequest(initial, RuntimeUpdate{CodecProfileLimits: Change[transcode.CodecProfileLimits]{Present: true, Value: &newProfile}})); !errors.Is(err, ErrRevisionConflict) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatal("a stale native policy writer escaped revision checks")
	}
	unchanged, err := store.Update(ctx, actor, runtimeTestRequest(saved, RuntimeUpdate{}))
	if err != nil || !reflect.DeepEqual(unchanged, saved) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatal("an older omitted update changed native policy values or provenance")
	}
	newDefaults := runtimeTestOptions()
	newDefaults.CodecProfileLimits = transcode.CodecProfileLimits{H264: "main", HEVC: "main10"}
	newDefaults.SubtitleOptions = &RuntimeSubtitleOptions{AllowAssToText: true}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "restarted-native-host", newDefaults)
	if err != nil || !equalRuntimeOverrides(reloaded.Snapshot().Runtime.Overrides, saved.Runtime.Overrides) ||
		reloaded.ValueSnapshot().CodecProfileLimits != saved.Runtime.CodecProfileLimits || reloaded.ValueSnapshot().SubtitleOptions != saved.Runtime.SubtitleOptions || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatalf("a restart changed persisted native output choices: %v", err)
	}
	reset, err := reloaded.Reset(ctx, actor, ResetRequest{Revision: saved.Revision, Fields: []Field{FieldRuntimeSubtitleOptions}})
	if err != nil || reset.Runtime.Overrides.SubtitleOptions != nil || !reset.Runtime.SubtitleOptions.AllowAssToText ||
		reset.Runtime.CodecProfileLimits != saved.Runtime.CodecProfileLimits || !reflect.DeepEqual(reset.Runtime.Overrides.CodecProfileLimits, saved.Runtime.Overrides.CodecProfileLimits) {
		t.Fatalf("a selected subtitle reset changed independent encoder profile ceilings: %v", err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredRuntime(raw)
	if err != nil || decoded.SubtitleOptions != nil || decoded.CodecProfileLimits == nil || *decoded.CodecProfileLimits != *selected.CodecProfileLimits {
		t.Fatalf("selected reset provenance did not survive persistence: %v", err)
	}
}

func TestRuntimeNativeSelectionDatabaseRejectsPartialAndTypedInvalidGroups(t *testing.T) {
	ctx, pool, _, store, actor := settingsRepository(t)
	initial := store.Snapshot()
	selected := nativeSelectionRuntimeOverrides()
	if _, err := store.Update(ctx, actor, runtimeTestRequest(initial, RuntimeUpdate{
		EnableThrottling:      Change[bool]{Present: true, Value: selected.EnableThrottling},
		ExtractionThreadCount: Change[int]{Present: true, Value: selected.ExtractionThreadCount},
		CodecProfileLimits:    Change[transcode.CodecProfileLimits]{Present: true, Value: selected.CodecProfileLimits},
		SubtitleOptions:       Change[RuntimeSubtitleOptions]{Present: true, Value: selected.SubtitleOptions},
	})); err != nil {
		t.Fatal(err)
	}
	row := settingsRowSnapshot(t, ctx, pool)
	for _, statement := range []string{
		`UPDATE managed_settings SET runtime_overrides=runtime_overrides-'CodecProfileLimits' WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{CodecProfileLimits}','{"H264":"main"}'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{CodecProfileLimits,HEVC}','"rext"'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{SubtitleOptions,AllowAssToText}','null'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{SubtitleOptions,AllowAssToText}','"false"'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{SubtitleOptions,Style}','"private-marker"'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{Threads}','65'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{EnableThrottling}','"true"'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{ExtractionThreadCount}','17'::jsonb) WHERE id=1`,
		`UPDATE managed_settings SET runtime_overrides=jsonb_set(runtime_overrides,'{ExtractionThreadCount}','"0"'::jsonb) WHERE id=1`,
	} {
		_, err := pool.Exec(ctx, statement)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" || settingsRowSnapshot(t, ctx, pool) != row {
			t.Fatalf("an invalid selected policy bypassed the database contract: %v", err)
		}
	}
	var current []byte
	if err := pool.QueryRow(ctx, `SELECT runtime_overrides FROM managed_settings WHERE id=1`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(current, &object) != nil || object["CodecProfileLimits"] == nil || object["SubtitleOptions"] == nil {
		t.Fatal("current persistence omitted a nullable native policy group")
	}
}

func TestConfigurationASSOnlyMutationPreservesIndependentDownloadPolicyAndOmissions(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	downloads := SubtitleOptions{DownloadLanguages: []string{"fr"}, DownloadMovieSubtitles: true}
	first, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationSubtitles, Subtitles: &downloads})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	selected, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationSubtitles, AllowAssToText: &disabled})
	if err != nil || selected.Runtime.SubtitleOptions.AllowAssToText || !reflect.DeepEqual(selected.Management.Subtitles, first.Management.Subtitles) {
		t.Fatalf("the native ASS-only editor changed independent downloads: %v", err)
	}
	row := settingsRowSnapshot(t, ctx, pool)
	unchanged, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationSubtitles, AllowAssToText: &disabled})
	if err != nil || !reflect.DeepEqual(unchanged, selected) || settingsRowSnapshot(t, ctx, pool) != row {
		t.Fatal("a native ASS round trip created a redundant settings revision")
	}
	defaults := DefaultManagement().Subtitles
	resetDownloads, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationSubtitles, Subtitles: &defaults})
	if err != nil || resetDownloads.Runtime.SubtitleOptions.AllowAssToText || !reflect.DeepEqual(resetDownloads.Runtime.Overrides.SubtitleOptions, selected.Runtime.Overrides.SubtitleOptions) {
		t.Fatal("an older download-only writer cleared the native ASS output policy")
	}
}
