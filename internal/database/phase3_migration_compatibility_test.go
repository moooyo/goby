package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Historical snapshots retain every original column. Verify the explicitly
// appended phase 3 values separately instead of treating them as old row data.
func assertPhase3MigrationDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM libraries WHERE revision IS DISTINCT FROM 1
			OR options IS DISTINCT FROM CASE WHEN EXISTS(SELECT 1 FROM schema_migrations WHERE version=49)
				THEN '{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true}'::jsonb
				ELSE '{"EnableLocalMetadata":true,"EnableLocalImages":true}'::jsonb END)
		AND NOT EXISTS(SELECT 1 FROM users WHERE configuration_revision IS DISTINCT FROM 1)
		AND NOT EXISTS(SELECT 1 FROM user_item_data WHERE hide_from_resume IS DISTINCT FROM false
			OR rating IS NOT NULL OR likes IS NOT NULL OR remembered_media_source_id IS DISTINCT FROM ''
			OR remembered_media_stamp IS DISTINCT FROM '' OR remembered_audio_stream_index IS NOT NULL
			OR remembered_subtitle_stream_index IS NOT NULL)
		AND NOT EXISTS(SELECT 1 FROM display_preferences)
		AND NOT EXISTS(SELECT 1 FROM artwork_state)
		AND NOT EXISTS(SELECT 1 FROM artwork_images)
		AND NOT EXISTS(SELECT 1 FROM entity_user_data)
		AND NOT EXISTS(SELECT 1 FROM task_triggers WHERE system_event IS NOT NULL OR last_event_sequence IS DISTINCT FROM 0)
		AND (SELECT count(*) FROM task_system_events)=3
		AND (SELECT count(*) FROM task_system_events WHERE name IN
			('ServerStarted','LibraryChanged','ConfigurationChanged') AND sequence=0
			AND lifecycle_key='' AND occurred_at IS NOT NULL)=3
		AND NOT EXISTS(SELECT 1 FROM task_system_event_receipts)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("migration inferred phase 3 library, preference, artwork, or event state: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT
		pg_get_serial_sequence('artwork_state','id')::regclass='artwork_state_id_seq'::regclass
		AND EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='artwork_state'::regclass
			AND attname='id' AND attidentity='a' AND NOT attisdropped)
		AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid='artwork_state_id_seq'::regclass
			AND seqtypid='bigint'::regtype AND seqstart=1 AND seqincrement=1 AND seqmin=1
			AND seqmax=9223372036854775807 AND seqcache=1 AND NOT seqcycle)
		AND last_value=1 AND log_cnt=0 AND NOT is_called
		FROM artwork_state_id_seq`).Scan(&valid); err != nil || !valid {
		t.Fatalf("schema38 did not initialize the independent unused artwork identity: %v", err)
	}
}
