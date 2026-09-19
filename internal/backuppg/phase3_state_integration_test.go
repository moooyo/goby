//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func assertPhase3HistoricalPreferenceDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM users WHERE configuration_revision IS DISTINCT FROM 1)
		AND NOT EXISTS(SELECT 1 FROM display_preferences)
		AND NOT EXISTS(SELECT 1 FROM user_item_data WHERE hide_from_resume IS DISTINCT FROM false
			OR rating IS NOT NULL OR likes IS NOT NULL OR remembered_media_source_id IS DISTINCT FROM ''
			OR remembered_media_stamp IS DISTINCT FROM '' OR remembered_audio_stream_index IS NOT NULL
			OR remembered_subtitle_stream_index IS NOT NULL)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("historical restoration inferred client preferences, ratings, or remembered tracks: %v", err)
	}
}

func seedPhase3PreferenceSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE users SET configuration_revision=9007199254740993,
		configuration='{"AudioLanguagePreference":"fra","SubtitleLanguagePreference":"jpn","SubtitleMode":"Always","ResumeRewindSeconds":17,"ExactInteger":9007199254740993}'::jsonb
		WHERE id='backup-admin';
		INSERT INTO users(id,name,normalized_name,password_hash,configuration,configuration_revision)
		VALUES('phase3-viewer','Phase 3 viewer','phase 3 viewer','test-only-phase3-password-hash',
			'{"AudioLanguagePreference":"eng","SubtitleMode":"None","ResumeRewindSeconds":4}'::jsonb,9007199254740995);
		INSERT INTO display_preferences(user_id,client,preferences_id,preferences,revision,updated_at)
		VALUES('backup-admin','phase3-client-a','music',
			'{"Id":"music","Client":"phase3-client-a","SortBy":"SortName","SortOrder":"Ascending","CustomPrefs":{"Witness":"administrator-a"}}'::jsonb,
			9007199254740993,'2020-01-06T00:00:00.123456Z'),
			('backup-admin','phase3-client-b','music',
			'{"Id":"music","Client":"phase3-client-b","SortBy":"DateCreated","SortOrder":"Descending","CustomPrefs":{"Witness":"administrator-b"}}'::jsonb,
			9007199254740994,'2020-01-06T00:00:00.234567Z'),
			('phase3-viewer','phase3-client-a','music',
			'{"Id":"music","Client":"phase3-client-a","SortBy":"SortName","SortOrder":"Descending","CustomPrefs":{"Witness":"viewer-a"}}'::jsonb,
			9007199254740995,'2020-01-06T00:00:00.345678Z');
		UPDATE user_item_data SET hide_from_resume=true,rating=9.25,likes=true,
		remembered_media_source_id='theme-song',remembered_media_stamp=(SELECT md5(media::text) FROM items WHERE id='theme-song'),
		remembered_audio_stream_index=1,remembered_subtitle_stream_index=-1 WHERE user_id='backup-admin' AND item_id='theme-song';
		INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,rating,likes,updated_at)
		VALUES('phase3-viewer','theme-song',123456789,2,2.5,false,'2020-01-07T00:00:00.456789Z')`); err != nil {
		t.Fatalf("seed independent phase 3 user, client, and item preferences: %v", err)
	}
	assertPhase3PreferenceSnapshotWitness(t, ctx, pool)
}

func assertPhase3PreferenceSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM users WHERE id='backup-admin' AND configuration_revision=9007199254740993
			AND configuration->>'AudioLanguagePreference'='fra' AND configuration->>'ExactInteger'='9007199254740993')
		AND EXISTS(SELECT 1 FROM users WHERE id='phase3-viewer' AND configuration_revision=9007199254740995
			AND configuration->>'SubtitleMode'='None')
		AND (SELECT count(*) FROM display_preferences WHERE preferences_id='music')=3
		AND EXISTS(SELECT 1 FROM display_preferences WHERE user_id='backup-admin' AND client='phase3-client-a'
			AND preferences_id='music' AND revision=9007199254740993 AND preferences#>>'{CustomPrefs,Witness}'='administrator-a')
		AND EXISTS(SELECT 1 FROM display_preferences WHERE user_id='backup-admin' AND client='phase3-client-b'
			AND preferences_id='music' AND revision=9007199254740994 AND preferences#>>'{CustomPrefs,Witness}'='administrator-b')
		AND EXISTS(SELECT 1 FROM display_preferences WHERE user_id='phase3-viewer' AND client='phase3-client-a'
			AND preferences_id='music' AND revision=9007199254740995 AND preferences#>>'{CustomPrefs,Witness}'='viewer-a')
		AND EXISTS(SELECT 1 FROM user_item_data WHERE user_id='backup-admin' AND item_id='theme-song'
			AND hide_from_resume AND rating=9.25 AND likes IS TRUE AND remembered_media_source_id='theme-song'
			AND remembered_media_stamp=(SELECT md5(media::text) FROM items WHERE id='theme-song')
			AND remembered_audio_stream_index=1 AND remembered_subtitle_stream_index=-1)
		AND EXISTS(SELECT 1 FROM user_item_data WHERE user_id='phase3-viewer' AND item_id='theme-song'
			AND NOT hide_from_resume AND rating=2.5 AND likes IS FALSE AND remembered_media_source_id=''
			AND remembered_media_stamp='' AND remembered_audio_stream_index IS NULL
			AND remembered_subtitle_stream_index IS NULL)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("restoration merged preference scopes or changed exact user and item state: %v", err)
	}
}

func assertPhase3HistoricalArtworkDefaults(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM artwork_state)
		AND NOT EXISTS(SELECT 1 FROM artwork_images) AND NOT EXISTS(SELECT 1 FROM entity_user_data)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("historical restoration invented managed artwork or independent entity preferences: %v", err)
	}
}

func phase3ArtworkPayload(t *testing.T, red uint8) ([]byte, string) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.SetNRGBA(0, 0, color.NRGBA{R: red, G: 37, B: 91, A: 255})
	picture.SetNRGBA(1, 1, color.NRGBA{R: 13, G: red, B: 127, A: 128})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("encode the original tiny artwork witness: %v", err)
	}
	content := encoded.Bytes()
	digest := sha256.Sum256(content)
	return content, hex.EncodeToString(digest[:])
}

func seedPhase3ArtworkSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO artwork_state(id,item_id,revision,managed_types,updated_at) OVERRIDING SYSTEM VALUE
		VALUES(9007199254740993,'theme-owner',9007199254740997,ARRAY['Primary','Backdrop','Thumb'],'2020-01-08T00:00:00.123456Z');
		INSERT INTO artwork_state(id,entity_id,revision,managed_types,updated_at) OVERRIDING SYSTEM VALUE
		SELECT 9007199254740994,id,9007199254740998,ARRAY['Primary'],'2020-01-08T00:00:00.234567Z'::timestamptz
		FROM catalog_entities WHERE kind='MusicArtist' AND normalized_name='snapshot artist';
		INSERT INTO artwork_state(id,user_id,revision,managed_types,updated_at) OVERRIDING SYSTEM VALUE
		VALUES(9007199254740995,'backup-admin',9007199254740999,ARRAY['Primary'],'2020-01-08T00:00:00.345678Z');
		SELECT setval('artwork_state_id_seq',9007199254740995,true);
		INSERT INTO entity_user_data(user_id,entity_id,play_count,is_favorite,played,last_played_at,rating,likes,updated_at)
		SELECT 'backup-admin',id,7,true,true,'2020-01-09T00:00:00Z',8.5,true,'2020-01-09T00:00:00.123456Z'
		FROM catalog_entities WHERE kind='MusicArtist' AND normalized_name='snapshot artist';
		INSERT INTO entity_user_data(user_id,entity_id,play_count,is_favorite,played,rating,likes,updated_at)
		SELECT 'phase3-viewer',id,0,false,false,1.25,false,'2020-01-09T00:00:00.234567Z'
		FROM catalog_entities WHERE kind='MusicArtist' AND normalized_name='snapshot artist'`); err != nil {
		t.Fatalf("seed independent item, entity, and avatar artwork owners: %v", err)
	}
	for _, picture := range []struct {
		stateID int64
		kind    string
		index   int
		red     uint8
	}{
		{9007199254740993, "Primary", 0, 37},
		{9007199254740993, "Backdrop", 0, 73},
		{9007199254740993, "Backdrop", 1, 109},
		{9007199254740994, "Primary", 0, 149},
		{9007199254740995, "Primary", 0, 191},
	} {
		content, digest := phase3ArtworkPayload(t, picture.red)
		if _, err := pool.Exec(ctx, `INSERT INTO artwork_images(state_id,image_type,image_index,content,mime_type,width,height,source_hash,modified_at)
			VALUES($1,$2,$3,$4,'image/png',2,2,$5,'2020-01-08T00:01:00.123456Z')`,
			picture.stateID, picture.kind, picture.index, content, digest); err != nil {
			t.Fatalf("seed original managed artwork bytes and ordered image identity: %v", err)
		}
	}
	assertPhase3ArtworkSnapshotWitness(t, ctx, pool)
}

func assertPhase3ArtworkSnapshotWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM artwork_state)=3 AND (SELECT count(*) FROM artwork_images)=5
		AND EXISTS(SELECT 1 FROM artwork_state WHERE id=9007199254740993 AND item_id='theme-owner'
			AND entity_id IS NULL AND user_id IS NULL AND revision=9007199254740997
			AND managed_types=ARRAY['Primary','Backdrop','Thumb'])
		AND NOT EXISTS(SELECT 1 FROM artwork_images WHERE state_id=9007199254740993 AND image_type='Thumb')
		AND (SELECT count(DISTINCT source_hash) FROM artwork_images WHERE state_id=9007199254740993 AND image_type='Backdrop')=2
		AND EXISTS(SELECT 1 FROM artwork_state WHERE id=9007199254740994 AND entity_id=
			(SELECT id FROM catalog_entities WHERE kind='MusicArtist' AND normalized_name='snapshot artist')
			AND item_id IS NULL AND user_id IS NULL AND revision=9007199254740998)
		AND EXISTS(SELECT 1 FROM artwork_state WHERE id=9007199254740995 AND user_id='backup-admin'
			AND item_id IS NULL AND entity_id IS NULL AND revision=9007199254740999)
		AND NOT EXISTS(SELECT 1 FROM artwork_images WHERE mime_type<>'image/png' OR width<>2 OR height<>2
			OR source_hash<>encode(sha256(content),'hex'))
		AND (SELECT count(*) FROM entity_user_data)=2
		AND EXISTS(SELECT 1 FROM entity_user_data WHERE user_id='backup-admin' AND play_count=7
			AND is_favorite AND played AND rating=8.5 AND likes IS TRUE)
		AND EXISTS(SELECT 1 FROM entity_user_data WHERE user_id='phase3-viewer' AND play_count=0
			AND NOT is_favorite AND NOT played AND rating=1.25 AND likes IS FALSE)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("restoration changed artwork ownership, tombstones, order, bytes, or independent entity state: %v", err)
	}
}
