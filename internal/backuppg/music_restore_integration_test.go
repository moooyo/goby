//go:build linux

package backuppg

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedMusicSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('music-snapshot-library','Music snapshot','music');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('music-snapshot-album','music-snapshot-library','Accepted album','accepted album','MusicAlbum',true);
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type) VALUES
		('music-snapshot-track','music-snapshot-library','music-snapshot-album','Accepted title','accepted title','Audio');
		UPDATE item_metadata_state SET music_source=jsonb_build_object('Version',1,'Name',
		CASE item_id WHEN 'music-snapshot-track' THEN 'Accepted title' ELSE 'Accepted album' END,
		'Album','Accepted album','Artists',jsonb_build_array('Snapshot artist','Featured artist'),
		'AlbumArtists',jsonb_build_array('Snapshot artist')),
		overrides='{"Overview":"Retained music override"}'::jsonb,locked_values='{"Tags":["Retained tag"]}'::jsonb,
		revision=9007199254740993,last_edited_by='backup-admin',last_edited_at='2020-01-04T00:00:00Z',
		updated_at='2020-01-04T00:00:00Z' WHERE item_id IN ('music-snapshot-track','music-snapshot-album');
		UPDATE item_metadata_state SET automatic=(music_source - 'Version') || '{"Genres":["Music genre"],"Tags":["Retained tag"],"Studios":[],"People":[{"Name":"Snapshot artist","Type":"Composer"}]}'::jsonb,
		source_key=source_key || jsonb_build_object('MusicSourceHash',encode(sha256(convert_to(music_source::text,'UTF8')),'hex'))
		WHERE item_id IN ('music-snapshot-track','music-snapshot-album');
		UPDATE item_metadata_state SET effective=automatic || overrides || locked_values
		WHERE item_id IN ('music-snapshot-track','music-snapshot-album');
		SELECT sync_catalog_item_entities(item_id,effective) FROM item_metadata_state
		WHERE item_id IN ('music-snapshot-track','music-snapshot-album') ORDER BY item_id`); err != nil {
		t.Fatalf("seed accepted music source and durable catalog associations: %v", err)
	}
	assertMusicSnapshotIdentity(t, ctx, pool)
	return musicSnapshotState(t, ctx, pool)
}

func musicSnapshotState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i WHERE library_id='music-snapshot-library'),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m WHERE item_id IN ('music-snapshot-track','music-snapshot-album')),
		'associations',(SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id,entity_id,credit_group,position) FROM item_entities a WHERE item_id IN ('music-snapshot-track','music-snapshot-album')),
		'entities',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e WHERE id IN
			(SELECT entity_id FROM item_entities WHERE item_id IN ('music-snapshot-track','music-snapshot-album'))))::text`).Scan(&state); err != nil {
		t.Fatalf("read complete music snapshot rows: %v", err)
	}
	return state
}

func assertMusicSnapshotIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_entities a JOIN catalog_entities e ON e.id=a.entity_id
		 WHERE a.item_id='music-snapshot-track' AND e.kind='MusicArtist' AND e.normalized_name='snapshot artist'
		 AND a.position=1 AND ((a.credit_group=1 AND a.credit_type='Artist') OR (a.credit_group=2 AND a.credit_type='AlbumArtist')))=2
		AND (SELECT count(DISTINCT a.entity_id) FROM item_entities a JOIN catalog_entities e ON e.id=a.entity_id
		 WHERE a.item_id IN ('music-snapshot-track','music-snapshot-album') AND e.kind='MusicArtist' AND e.normalized_name='snapshot artist')=1
		AND (SELECT count(*) FROM catalog_entities WHERE normalized_name='snapshot artist' AND kind IN ('Person','MusicArtist'))=2
		AND NOT EXISTS(SELECT 1 FROM item_entities a LEFT JOIN items i ON i.id=a.item_id
		 LEFT JOIN catalog_entities e ON e.id=a.entity_id WHERE i.id IS NULL OR e.id IS NULL)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("music snapshot lost shared artist identity, distinct Person identity, dual-role position, or foreign keys: %v", err)
	}
}
