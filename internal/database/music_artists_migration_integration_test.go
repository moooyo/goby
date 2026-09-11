package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

// Apply the published schema 24 before introducing any music storage. Removing
// the latest migration row from a current schema would hide upgrade failures.
func musicArtistsVersion24Baseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	userSettingsVersion23Baseline(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin schema 24 music baseline: %v", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()
	if err := database.RecoveryMigrateTo(ctx, tx, 24); err != nil {
		t.Fatalf("apply published migrations through schema 24: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit schema 24 music baseline: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 24 {
		t.Fatalf("music baseline schema = %d, want 24: %v", version, err)
	}
	var musicColumns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_attribute
		WHERE ((attrelid='item_metadata_state'::regclass AND attname='music_source')
		OR (attrelid='item_entities'::regclass AND attname='credit_group'))
		AND NOT attisdropped`).Scan(&musicColumns); err != nil || musicColumns != 0 {
		t.Fatalf("schema 24 already contains music columns: count=%d error=%v", musicColumns, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_settings(user_id,settings,updated_at)
		VALUES('device-user-a','{"SubtitleMode":"Smart","ExactInteger":9007199254740993,"Extension":[null,true,"preserve"]}',
		'2025-09-05T00:00:00Z')`); err != nil {
		t.Fatalf("seed schema 24 preference witness: %v", err)
	}
}

func seedMusicArtistsLegacyMetadata(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	const metadata = `{
		"Genres":[" Rock ","rock",null," Jazz ",17,""],
		"Tags":[" Live ","live"," Studio Session "],
		"Studios":[" Legacy Studio ","legacy studio"," Second Studio "],
		"People":[
			{"Name":"Shared Name","Role":"Voice","Type":"Actor","SortOrder":9},
			{"Name":" shared name ","Role":"Direction","Type":"Director","SortOrder":0},
			{"Name":"No Sort","Role":12,"Type":false,"SortOrder":2147483648},
			{"Name":"Sort Shape","SortOrder":"7"},
			{"Name":"Negative Sort","SortOrder":-1},
			{"Name":"Fractional Sort","SortOrder":1.5},
			{"Name":"Maximum Sort","SortOrder":2147483647},
			{"Name":"","Type":"Ignored"},null,17
		]
	}`
	// High-entropy historical credit text must remain legal. Repeated characters
	// would compress and could miss an accidental unbounded B-tree index key.
	var enriched string
	if err := pool.QueryRow(ctx, `SELECT ($1::jsonb || jsonb_build_object('People',
		($1::jsonb->'People') || jsonb_build_array(jsonb_build_object(
			'Name','Long Legacy Type','Role','Original long type role','SortOrder',17,
			'Type',(SELECT string_agg(md5(value::text),'' ORDER BY value)
				FROM generate_series(1,1024) AS source(value))))))::text`, metadata).Scan(&enriched); err != nil {
		t.Fatalf("construct long historical credit fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET local_metadata=$1::jsonb,
		local_metadata_hash=repeat('d',64),local_metadata_path='preserved.nfo'
		WHERE id='device-migration-item'`, enriched); err != nil {
		t.Fatalf("persist historical raw metadata fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE item_metadata_state SET automatic=automatic || $1::jsonb,
		effective=$1::jsonb,source_key=source_key || '{"LegacyExtension":{"ExactInteger":9007199254740993}}'::jsonb
		WHERE item_id='device-migration-item'`, enriched); err != nil {
		t.Fatalf("persist historical metadata state fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "SELECT sync_catalog_item_entities('device-migration-item',$1::jsonb)", enriched); err != nil {
		t.Fatalf("synchronize historical catalog entities: %v", err)
	}
	var longBytes, kinds, personCredits int
	if err := pool.QueryRow(ctx, `SELECT max(octet_length(association.credit_type)),
		count(DISTINCT entity.kind),count(*) FILTER (WHERE entity.kind='Person')
		FROM item_entities association JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE association.item_id='device-migration-item'`).Scan(&longBytes, &kinds, &personCredits); err != nil || longBytes != 32768 || kinds != 4 || personCredits != 8 {
		t.Fatalf("historical entity witnesses: type_bytes=%d kinds=%d person_credits=%d error=%v", longBytes, kinds, personCredits, err)
	}
	return enriched
}

// Project the captured historical columns, including all 24 history rows.
// Never print these snapshots: synthetic secret fields are part of the proof.
func musicArtistsLegacySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table deviceLegacyTable) string {
	t.Helper()
	columns := make([]string, len(table.columns))
	for index, column := range table.columns {
		columns[index] = pgx.Identifier{column}.Sanitize()
	}
	statement := "SELECT " + strings.Join(columns, ", ") + " FROM " + pgx.Identifier{table.name}.Sanitize()
	if table.name == "schema_migrations" {
		statement += " WHERE version <= 24"
	}
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(original)
		ORDER BY to_jsonb(original)::text),'[]'::jsonb)::text FROM (`+statement+`) original`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot schema 24 columns in %s: %v", table.name, err)
	}
	return snapshot
}

func TestMigrateMusicArtistsPreservesSchema24RowsAndUnboundedLegacyCredits(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	musicArtistsVersion24Baseline(t, ctx, pool)
	seedMusicArtistsLegacyMetadata(t, ctx, pool)
	legacy := captureDeviceLegacyTables(t, ctx, pool)
	if len(legacy) != 30 {
		t.Fatalf("schema 24 preservation inventory = %d tables, want 30", len(legacy))
	}
	for index := range legacy {
		legacy[index].snapshot = musicArtistsLegacySnapshot(t, ctx, pool, legacy[index])
	}
	var firstHistory string
	for attempt := 1; attempt <= 2; attempt++ {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatalf("music migration attempt %d with long historical credit: %v", attempt, err)
		}
		if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 25 {
			t.Fatalf("music migration schema = %d, want 25: %v", version, err)
		}
		var migrationName string
		var historyCount, tableCount, inferredSources, changedGroups, musicEntities int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT name FROM schema_migrations WHERE version=25),
			(SELECT count(*) FROM schema_migrations),
			(SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()),
			(SELECT count(*) FROM item_metadata_state WHERE music_source IS DISTINCT FROM '{}'::jsonb),
			(SELECT count(*) FROM item_entities WHERE credit_group IS DISTINCT FROM 0),
			(SELECT count(*) FROM catalog_entities WHERE kind='MusicArtist')`).Scan(
			&migrationName, &historyCount, &tableCount, &inferredSources, &changedGroups, &musicEntities); err != nil || migrationName != "0025_music_artists.sql" || historyCount != 25 || tableCount != 30 || inferredSources != 0 || changedGroups != 0 || musicEntities != 0 {
			t.Fatalf("music migration defaults/history: name=%q history=%d tables=%d sources=%d groups=%d artists=%d error=%v", migrationName, historyCount, tableCount, inferredSources, changedGroups, musicEntities, err)
		}
		for _, table := range legacy {
			if after := musicArtistsLegacySnapshot(t, ctx, pool, table); after != table.snapshot {
				t.Errorf("music migration attempt %d changed historical rows or columns in %s", attempt, table.name)
			}
		}
		history := migrationHistory(t, ctx, pool)
		if attempt == 1 {
			firstHistory = history
		} else if history != firstHistory {
			t.Error("repeated music migration changed complete migration history")
		}
	}
}

func musicArtistsLegacyEntityState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Entities',(SELECT jsonb_agg(to_jsonb(entity) ORDER BY entity.id)
			FROM catalog_entities entity WHERE entity.kind<>'MusicArtist'),
		'Credits',(SELECT jsonb_agg(to_jsonb(association)-'credit_group'
			ORDER BY association.item_id,association.entity_id,association.position)
			FROM item_entities association JOIN catalog_entities entity ON entity.id=association.entity_id
			WHERE entity.kind<>'MusicArtist'))::text`).Scan(&state); err != nil {
		t.Fatalf("snapshot legacy entity identities and credit fields: %v", err)
	}
	return state
}

func TestMusicArtistSynchronizationPreservesLegacyCreditsAndStableRoleIdentities(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	musicArtistsVersion24Baseline(t, ctx, pool)
	legacyMetadata := seedMusicArtistsLegacyMetadata(t, ctx, pool)
	legacyBefore := musicArtistsLegacyEntityState(t, ctx, pool)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate catalog synchronization fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "SELECT sync_catalog_item_entities('device-migration-item',$1::jsonb)", legacyMetadata); err != nil {
		t.Fatalf("synchronize all historical kinds with schema 25: %v", err)
	}
	if after := musicArtistsLegacyEntityState(t, ctx, pool); after != legacyBefore {
		t.Fatal("schema 25 synchronization changed legacy identities, names, positions, roles, types, or sort orders")
	}
	var misplacedLegacyCredits int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_entities WHERE credit_group<>0").Scan(&misplacedLegacyCredits); err != nil || misplacedLegacyCredits != 0 {
		t.Fatalf("historical synchronization assigned music credit groups: count=%d error=%v", misplacedLegacyCredits, err)
	}
	// The guest appears earlier in the album list, so source priority must take
	// precedence over position when choosing its first canonical spelling.
	const music = `{"Artists":[" Shared Name ","shared name",null," Guest Artist ","","Guest Artist",13,{}],
		"AlbumArtists":["SHARED NAME","GUEST ARTIST","shared name",null," Album Only ","album only"]}`
	const expected = `[
		["Shared Name","Shared Name","Artist",1,1,"",null],
		["Guest Artist","Guest Artist","Artist",1,4,"",null],
		["Shared Name","SHARED NAME","AlbumArtist",2,1,"",null],
		["Guest Artist","GUEST ARTIST","AlbumArtist",2,2,"",null],
		["Album Only","Album Only","AlbumArtist",2,5,"",null]
	]`
	var stableMusicID int64
	var firstEntities string
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := pool.Exec(ctx, "SELECT sync_catalog_item_entities('device-migration-item',$1::jsonb || $2::jsonb)", legacyMetadata, music); err != nil {
			t.Fatalf("synchronize ordered music credits attempt %d: %v", attempt, err)
		}
		var matches bool
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(entity.name,association.display_name,
			association.credit_type,association.credit_group,association.position,association.role,association.sort_order)
			ORDER BY association.credit_group,association.position,entity.id)=$1::jsonb
			FROM item_entities association JOIN catalog_entities entity ON entity.id=association.entity_id
			WHERE association.item_id='device-migration-item' AND entity.kind='MusicArtist'`, expected).Scan(&matches); err != nil || !matches {
			t.Fatalf("music credits lost ordered deduplication, source spelling, or separate roles: matches=%v error=%v", matches, err)
		}
		var musicID, personID int64
		var musicCredits, musicEntities int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND normalized_name='shared name'),
			(SELECT id FROM catalog_entities WHERE kind='Person' AND normalized_name='shared name'),
			(SELECT count(*) FROM item_entities association JOIN catalog_entities entity ON entity.id=association.entity_id
				WHERE association.item_id='device-migration-item' AND entity.kind='MusicArtist'
				AND entity.normalized_name='shared name' AND association.position=1),
			(SELECT count(*) FROM catalog_entities WHERE kind='MusicArtist')`).Scan(&musicID, &personID, &musicCredits, &musicEntities); err != nil || musicID == personID || musicCredits != 2 || musicEntities != 3 {
			t.Fatalf("music/person identity separation or shared role identity failed: music=%d person=%d credits=%d entities=%d error=%v", musicID, personID, musicCredits, musicEntities, err)
		}
		var entities string
		if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(entity) ORDER BY id)::text FROM catalog_entities entity WHERE kind='MusicArtist'").Scan(&entities); err != nil {
			t.Fatalf("snapshot persisted music identities: %v", err)
		}
		if attempt == 1 {
			stableMusicID, firstEntities = musicID, entities
		} else if musicID != stableMusicID || entities != firstEntities {
			t.Error("repeated synchronization changed persisted music IDs, canonical names, or creation times")
		}
		if after := musicArtistsLegacyEntityState(t, ctx, pool); after != legacyBefore {
			t.Errorf("adding music credits changed historical entity state on attempt %d", attempt)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type)
		VALUES('music-migration-peer','device-migration-library','Peer Track','peer track','Audio');
		SELECT sync_catalog_item_entities('music-migration-peer','{"Artists":["shared NAME"],"AlbumArtists":["Shared Name"]}');
		SELECT sync_catalog_item_entities('music-migration-peer','{"Artists":[],"AlbumArtists":[]}')`); err != nil {
		t.Fatalf("add and clear an independent item using existing music identities: %v", err)
	}
	var peerCredits int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_entities WHERE item_id='music-migration-peer'").Scan(&peerCredits); err != nil || peerCredits != 0 {
		t.Fatalf("replacement synchronization retained removed music credits: count=%d error=%v", peerCredits, err)
	}
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('music-migration-peer',
		'{"Artists":["shared NAME"],"AlbumArtists":["Shared Name"]}')`); err != nil {
		t.Fatalf("restore independent item music credits: %v", err)
	}
	var reused bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(entity_id=$1)
		FROM item_entities WHERE item_id='music-migration-peer'`, stableMusicID).Scan(&reused); err != nil || !reused {
		t.Fatalf("music IDs were not reused across items and removal/recreation: reused=%v error=%v", reused, err)
	}
	var canonical string
	if err := pool.QueryRow(ctx, "SELECT name FROM catalog_entities WHERE id=$1", stableMusicID).Scan(&canonical); err != nil || canonical != "Shared Name" {
		t.Fatalf("later source spelling changed the persisted canonical name: name=%q error=%v", canonical, err)
	}
}

func TestMusicArtistMigrationEnforcesSourceShapeBoundedKeysAndOwnership(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("initialize music storage constraint schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('music-constraint-library','Music Constraint Library','music');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES
		('music-constraint-item','music-constraint-library','First Track','first track','Audio'),
		('music-constraint-peer','music-constraint-library','Second Track','second track','Audio');
		INSERT INTO catalog_entities(kind,name) VALUES('MusicArtist','Constraint Artist'),('Person','Constraint Person')`); err != nil {
		t.Fatalf("seed music storage constraint owners: %v", err)
	}
	var defaultsValid bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(music_source='{}'::jsonb)
		FROM item_metadata_state`).Scan(&defaultsValid); err != nil || !defaultsValid {
		t.Fatalf("new item music source defaults = %v: %v", defaultsValid, err)
	}
	var keyColumns []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(attribute.attname ORDER BY key.ordinality)
		FROM pg_constraint constraint_entry
		CROSS JOIN LATERAL unnest(constraint_entry.conkey) WITH ORDINALITY AS key(attnum,ordinality)
		JOIN pg_attribute attribute ON attribute.attrelid=constraint_entry.conrelid AND attribute.attnum=key.attnum
		WHERE constraint_entry.conrelid='item_entities'::regclass AND constraint_entry.contype='p'`).Scan(&keyColumns); err != nil || strings.Join(keyColumns, ",") != "item_id,entity_id,credit_group,position" {
		t.Fatalf("music association primary key includes unbounded credit text or lost role separation: columns=%v error=%v", keyColumns, err)
	}
	var boundedGroup bool
	if err := pool.QueryRow(ctx, `SELECT atttypid='smallint'::regtype AND attnotnull
		FROM pg_attribute WHERE attrelid='item_entities'::regclass AND attname='credit_group' AND NOT attisdropped`).Scan(&boundedGroup); err != nil || !boundedGroup {
		t.Fatalf("music credit group is not a non-null bounded integer: bounded=%v error=%v", boundedGroup, err)
	}
	const artist = "(SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND name='Constraint Artist')"
	for _, test := range []struct {
		name, statement, code string
	}{
		{"null music source", `UPDATE item_metadata_state SET music_source=NULL WHERE item_id='music-constraint-item'`, "23502"},
		{"json null source", `UPDATE item_metadata_state SET music_source='null' WHERE item_id='music-constraint-item'`, "23514"},
		{"array source", `UPDATE item_metadata_state SET music_source='[]' WHERE item_id='music-constraint-item'`, "23514"},
		{"string source", `UPDATE item_metadata_state SET music_source='"artist"' WHERE item_id='music-constraint-item'`, "23514"},
		{"boolean source", `UPDATE item_metadata_state SET music_source='true' WHERE item_id='music-constraint-item'`, "23514"},
		{"number source", `UPDATE item_metadata_state SET music_source='42' WHERE item_id='music-constraint-item'`, "23514"},
		{"unsupported entity kind", `INSERT INTO catalog_entities(kind,name) VALUES('AlbumArtist','Unsupported Separate Kind')`, "23514"},
		{"null credit group", "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group) VALUES('music-constraint-item'," + artist + ",1,'Artist',NULL)", "23502"},
		{"negative credit group", "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group) VALUES('music-constraint-item'," + artist + ",1,'Artist',-1)", "23514"},
		{"unknown credit group", "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group) VALUES('music-constraint-item'," + artist + ",1,'Artist',3)", "23514"},
		{"missing item", "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group) VALUES('missing-music-item'," + artist + ",1,'Artist',1)", "23503"},
		{"missing entity", `INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group) VALUES('music-constraint-item',9223372036854775807,1,'Artist',1)`, "23503"},
		{"zero position", "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group) VALUES('music-constraint-item'," + artist + ",0,'Artist',1)", "23514"},
		{"negative sort order", "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_group,sort_order) VALUES('music-constraint-item'," + artist + ",1,'Artist',1,-1)", "23514"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, test.statement)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != test.code {
				t.Fatalf("music storage constraint error = %v, want SQLSTATE %s", err, test.code)
			}
		})
	}
	var objectPreserved bool
	if err := pool.QueryRow(ctx, `UPDATE item_metadata_state SET music_source=
		'{"Artists":["Constraint Artist"],"ExactInteger":9007199254740993,"Extension":null}'::jsonb
		WHERE item_id='music-constraint-item'
		RETURNING music_source->>'ExactInteger'='9007199254740993' AND music_source->'Extension'='null'::jsonb`).Scan(&objectPreserved); err != nil || !objectPreserved {
		t.Fatalf("accepted music source object was not preserved exactly: valid=%v error=%v", objectPreserved, err)
	}
	var legacyDefault bool
	if err := pool.QueryRow(ctx, `INSERT INTO item_entities(item_id,entity_id,position,display_name)
		SELECT 'music-constraint-item',id,1,'Constraint Person' FROM catalog_entities WHERE kind='Person'
		RETURNING credit_group=0 AND credit_type=''`).Scan(&legacyDefault); err != nil || !legacyDefault {
		t.Fatalf("omitted legacy association group did not default to zero: valid=%v error=%v", legacyDefault, err)
	}
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('music-constraint-item',
		'{"Artists":["Constraint Artist"],"AlbumArtists":["Constraint Artist"]}');
		SELECT sync_catalog_item_entities('music-constraint-peer','{"Artists":["Constraint Artist"]}')`); err != nil {
		t.Fatalf("persist same-position independent music roles and owner: %v", err)
	}
	_, err := pool.Exec(ctx, "INSERT INTO item_entities(item_id,entity_id,position,display_name,credit_type,credit_group) VALUES('music-constraint-item',"+artist+",1,'Different Display','Different Original Credit',1)")
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("duplicate bounded role key error = %v, want SQLSTATE 23505", err)
	}
	_, err = pool.Exec(ctx, "DELETE FROM catalog_entities WHERE id="+artist)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("deleting a referenced music identity error = %v, want SQLSTATE 23503", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM items WHERE id='music-constraint-item'"); err != nil {
		t.Fatalf("delete one music association owner: %v", err)
	}
	var removedCredits, removedSource, peerCredits, artists int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_entities WHERE item_id='music-constraint-item'),
		(SELECT count(*) FROM item_metadata_state WHERE item_id='music-constraint-item'),
		(SELECT count(*) FROM item_entities WHERE item_id='music-constraint-peer'),
		(SELECT count(*) FROM catalog_entities WHERE kind='MusicArtist')`).Scan(&removedCredits, &removedSource, &peerCredits, &artists); err != nil || removedCredits != 0 || removedSource != 0 || peerCredits != 1 || artists != 1 {
		t.Fatalf("music owner cascade changed another item or durable identity: removed_credits=%d removed_source=%d peer_credits=%d artists=%d error=%v", removedCredits, removedSource, peerCredits, artists, err)
	}
}
