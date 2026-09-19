package settings

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNamedManagementConfigurationPersistsResetsAndSignalsOnlyChanges(t *testing.T) {
	ctx, pool, owner, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	sequence := func() int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='ConfigurationChanged'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := sequence()
	subtitles := SubtitleOptions{DownloadLanguages: []string{"fr", "zh-CN"}, DownloadEpisodeSubtitles: true}
	first, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationSubtitles, Subtitles: &subtitles})
	if err != nil {
		t.Fatal(err)
	}
	subtitles.DownloadLanguages[0] = "caller"
	if store.Snapshot().Management.Subtitles.DownloadLanguages[0] != "fr" || sequence() != before+1 {
		t.Fatal("named subtitle publication aliased input or omitted its signal")
	}
	options := TaskOptions{MaxConcurrent: 3, CacheRetentionDays: 7, CacheMaxEntries: 120}
	second, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationTasks, Tasks: &options})
	if err != nil || !reflect.DeepEqual(second.Management.Subtitles, first.Management.Subtitles) || second.Management.Tasks != options {
		t.Fatalf("named task update crossed section boundaries: %v", err)
	}
	if _, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationTasks, Tasks: &options}); err != nil || sequence() != before+2 {
		t.Fatal("configuration no-op emitted another task signal")
	}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha")
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot(), store.Snapshot()) {
		t.Fatal("named management settings did not reload exactly")
	}
	reset, err := store.Reset(ctx, native, ResetRequest{Revision: second.Revision, Fields: []Field{FieldSubtitles, FieldTasks}})
	if err != nil || !reflect.DeepEqual(reset.Management, DefaultManagement()) || sequence() != before+3 {
		t.Fatalf("native reset did not share named defaults: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationTasks, Tasks: &options}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked caller changed management state: %v", err)
	}
	if sequence() != before+3 {
		t.Fatal("rejected configuration emitted a system event")
	}
}
