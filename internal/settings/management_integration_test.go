package settings

import (
	"errors"
	"reflect"
	"testing"
)

func TestManagementSettingsPersistCloneResetAndRejectStaleUpdates(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	initial := store.Snapshot()
	if !reflect.DeepEqual(initial.Management, DefaultManagement()) {
		t.Fatal("management migration did not supply explicit defaults")
	}
	management := DefaultManagement()
	management.Metadata.EnableInternetProviders = true
	management.Metadata.PreferredMetadataLanguage = "zh-CN"
	management.Subtitles.DownloadLanguages = []string{"zh-CN", "en"}
	management.Tasks.MaxConcurrent = 4
	committed, err := store.Update(ctx, actor, UpdateRequest{Revision: initial.Revision, Overrides: initial.Overrides, Management: &management})
	if err != nil {
		t.Fatal(err)
	}
	management.Subtitles.DownloadLanguages[0] = "changed-input"
	committed.Management.Subtitles.DownloadLanguages[0] = "changed-response"
	if store.Snapshot().Management.Subtitles.DownloadLanguages[0] != "zh-CN" {
		t.Fatal("management publication leaked a caller-owned language slice")
	}
	if _, err := store.Update(ctx, actor, UpdateRequest{Revision: initial.Revision, Overrides: initial.Overrides}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("management update did not enforce the shared revision")
	}
	reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Snapshot(), store.Snapshot()) {
		t.Fatal("management state did not survive store reload")
	}
	reset, err := store.Reset(ctx, actor, ResetRequest{Revision: committed.Revision, Fields: []Field{FieldSubtitles}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reset.Management.Subtitles, DefaultManagement().Subtitles) || reset.Management.Metadata.PreferredMetadataLanguage != "zh-CN" || reset.Management.Tasks.MaxConcurrent != 4 {
		t.Fatal("section reset changed unrelated settings")
	}
	var fields []string
	if err := pool.QueryRow(ctx, `SELECT changed_fields FROM activity_entries WHERE action='settings.updated' ORDER BY id DESC LIMIT 1`).Scan(&fields); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fields, []string{"Management"}) {
		t.Fatal("management audit did not retain a closed field-only payload")
	}
}

func TestMetadataConfigurationUpdatesShareRuntimeSettingsAndRespectSectionReset(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	initial := store.Snapshot()
	management := initial.Management
	management.Metadata.EnableInternetProviders = true
	management.Tasks.CacheRetentionDays = 7
	if _, err := store.Update(ctx, native, UpdateRequest{Revision: initial.Revision, Overrides: initial.Overrides, Management: &management}); err != nil {
		t.Fatal(err)
	}
	language, country := "fr", "FR"
	updated, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, PreferredMetadataLanguage: &language, MetadataCountryCode: &country})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Management.Metadata.PreferredMetadataLanguage != "fr" || updated.Management.Metadata.MetadataCountryCode != "FR" {
		t.Fatal("compatibility fields did not update runtime metadata preferences")
	}
	unchanged, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial})
	if err != nil || !reflect.DeepEqual(unchanged, updated) {
		t.Fatal("partial omission reset metadata preferences")
	}
	reset, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationFull})
	if err != nil {
		t.Fatal(err)
	}
	if reset.Management.Metadata.PreferredMetadataLanguage != "en" || reset.Management.Metadata.MetadataCountryCode != "US" || reset.Management.Metadata.EnableInternetProviders || reset.Management.Tasks.CacheRetentionDays != 7 {
		t.Fatal("full compatibility reset changed an unrelated native management setting")
	}
}
