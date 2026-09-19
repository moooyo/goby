package backuppg

import (
	"math"
	"testing"
)

func TestPhase3RetainsPublishedSchema35RecoveryCatalog(t *testing.T) {
	catalog, migrations, err := loadCatalog(35, "phase3_historical_catalog")
	if err != nil {
		t.Fatalf("load the published source catalog after appending phase 3 migrations: %v", err)
	}
	if len(migrations) != 35 || migrations[len(migrations)-1].Version != 35 ||
		len(catalog.Tables) != 42 || len(catalog.Sequences) != 6 || len(catalog.Constraints) != 64 {
		t.Fatal("phase 3 changed the published schema35 recovery inventory")
	}
}

func TestPhase3CurrentRecoveryCatalogContainsDurableState(t *testing.T) {
	catalog, _ := currentRecoveryCatalog(t, "phase3_current_catalog")
	tables := make(map[string]TableSpec, len(catalog.Tables))
	for _, table := range catalog.Tables {
		tables[table.Name] = table
	}
	for _, expected := range []TableSpec{
		{Name: "display_preferences", Columns: []string{"user_id", "client", "preferences_id", "preferences", "revision", "updated_at"}, PrimaryKey: []string{"user_id", "client", "preferences_id"}},
		{Name: "artwork_state", Columns: []string{"id", "item_id", "entity_id", "user_id", "revision", "managed_types", "updated_at"}, PrimaryKey: []string{"id"}},
		{Name: "artwork_images", Columns: []string{"state_id", "image_type", "image_index", "content", "mime_type", "width", "height", "source_hash", "modified_at"}, PrimaryKey: []string{"state_id", "image_type", "image_index"}},
		{Name: "entity_user_data", Columns: []string{"user_id", "entity_id", "playback_position_ticks", "play_count", "is_favorite", "played", "last_played_at", "rating", "likes", "updated_at"}, PrimaryKey: []string{"user_id", "entity_id"}},
		{Name: "task_system_events", Columns: []string{"name", "sequence", "occurred_at", "lifecycle_key"}, PrimaryKey: []string{"name"}},
		{Name: "task_system_event_receipts", Columns: []string{"trigger_id", "task_id", "schedule_revision", "system_event", "first_sequence", "last_sequence", "occurred_at", "run_id", "disposition"}, PrimaryKey: []string{"trigger_id", "schedule_revision", "last_sequence"}},
	} {
		expected.SortKey = expected.PrimaryKey
		if actual, exists := tables[expected.Name]; !exists || !equalJSON(actual, expected) {
			t.Errorf("the current recovery catalog omitted phase 3 copy fields or stable identity for %s", expected.Name)
		}
	}
	for name, additions := range map[string][]string{
		"libraries":      {"revision", "options"},
		"users":          {"configuration_revision"},
		"user_item_data": {"hide_from_resume", "rating", "likes", "remembered_media_source_id", "remembered_media_stamp", "remembered_audio_stream_index", "remembered_subtitle_stream_index"},
		"task_triggers":  {"system_event", "last_event_sequence"},
	} {
		columns := make(map[string]bool)
		for _, column := range tables[name].Columns {
			columns[column] = true
		}
		for _, column := range additions {
			if !columns[column] {
				t.Errorf("the current recovery catalog omitted %s.%s", name, column)
			}
		}
	}
	wantSequence := SequenceSpec{
		Name: "artwork_state_id_seq", Table: "artwork_state", Column: "id",
		MinValue: 1, MaxValue: math.MaxInt64, Increment: 1,
		Consumers: []SequenceColumn{{Table: "artwork_state", Column: "id"}},
	}
	found := false
	for _, sequence := range catalog.Sequences {
		if sequence.Name == wantSequence.Name {
			found = true
			if !equalJSON(sequence, wantSequence) {
				t.Error("managed artwork lost its independent identity sequence ownership or bounds")
			}
		}
	}
	if !found {
		t.Error("the current recovery catalog omitted the managed artwork identity sequence")
	}
}
