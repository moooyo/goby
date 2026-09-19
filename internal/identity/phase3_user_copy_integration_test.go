package identity_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestPhase3UserCopyTransfersPublicItemAndEntityStateOnlyWhenSelected(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	source, err := store.CreateUser(ctx, "Phase Three Copy Source", "source-password", false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "Phase Three Other State", "other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	managedLogin(t, ctx, store, source, "source-password", "emby")
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('phase3-copy-library','Copy Library','movies');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES
		('phase3-copy-item','phase3-copy-library','Feature','Feature','Movie'),
		('phase3-copy-empty','phase3-copy-library','Unrated','Unrated','Movie');
		INSERT INTO catalog_entities(kind,name) VALUES('Person','Copy Person'),('Genre','Copy Genre')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data
		(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at,rating,likes,hide_from_resume,
		remembered_media_source_id,remembered_media_stamp,remembered_audio_stream_index,remembered_subtitle_stream_index)
		VALUES($1,'phase3-copy-item',123456,7,true,false,'2026-09-01T12:34:56Z',8.5,false,true,
		'private-source',repeat('a',32),5,6),($1,'phase3-copy-empty',0,0,false,false,NULL,NULL,NULL,false,'','',NULL,NULL)`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO entity_user_data(user_id,entity_id,play_count,is_favorite,played,last_played_at,rating,likes)
		SELECT $1,id,CASE kind WHEN 'Person' THEN 13 ELSE 0 END,kind='Person',kind='Person',
		CASE kind WHEN 'Person' THEN '2026-08-01T10:20:30Z'::timestamptz END,
		CASE kind WHEN 'Person' THEN 4.25::double precision END,CASE kind WHEN 'Person' THEN true END FROM catalog_entities`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO entity_user_data(user_id,entity_id,play_count,rating,likes)
		SELECT $1,id,21,9,false FROM catalog_entities WHERE kind='Person'`, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO display_preferences(user_id,client,preferences_id,preferences)
		VALUES($1,'web','phase3-copy-library','{"SortBy":"SortName","SortOrder":"Descending","CustomPrefs":{"layout":"poster"}}'::jsonb)`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO artwork_state(user_id,revision,managed_types) VALUES($1,1,ARRAY['Primary'])`, source.ID); err != nil {
		t.Fatal(err)
	}
	configuration := `{"AudioLanguagePreference":"eng","SubtitleLanguagePreference":"zh-Hans","SubtitleMode":"Always",
		"HidePlayedInLatest":false,"HidePlayedInMoreLikeThis":true,"OrderedViews":["phase3-copy-library"],
		"LatestItemsExcludes":[],"MyMediaExcludes":[],"PlayDefaultAudioTrack":false,
		"RememberAudioSelections":false,"RememberSubtitleSelections":false,"ResumeRewindSeconds":15,"UnknownPreference":"private"}`
	if _, err := pool.Exec(ctx, "UPDATE users SET configuration=$2::jsonb WHERE id=$1", source.ID, configuration); err != nil {
		t.Fatal(err)
	}
	stateSnapshot := func(userID string, publicOnly bool) string {
		t.Helper()
		itemProjection, entityProjection := "to_jsonb(d)", "to_jsonb(d)"
		if publicOnly {
			itemProjection += `-ARRAY['user_id','updated_at','remembered_media_source_id','remembered_media_stamp','remembered_audio_stream_index','remembered_subtitle_stream_index']::text[]`
			entityProjection += `-ARRAY['user_id','updated_at']::text[]`
		}
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
			'items',COALESCE((SELECT jsonb_agg(`+itemProjection+` ORDER BY d.item_id) FROM user_item_data d WHERE user_id=$1),'[]'::jsonb),
			'entities',COALESCE((SELECT jsonb_agg(`+entityProjection+` ORDER BY d.entity_id) FROM entity_user_data d WHERE user_id=$1),'[]'::jsonb))::text`, userID).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	accountBefore := managedSnapshot(t, ctx, pool, source.ID)
	stateBefore, publicBefore, otherBefore := stateSnapshot(source.ID, false), stateSnapshot(source.ID, true), stateSnapshot(other.ID, false)
	for bits := 0; bits < 4; bits++ {
		options := []string{}
		if bits&1 != 0 {
			options = append(options, "UserData")
		}
		if bits&2 != 0 {
			options = append(options, "UserConfiguration")
		}
		copied, err := store.CreateManagedUserCopy(ctx, actor, fmt.Sprintf("Phase Three Copy %d", bits), source.ID, options)
		if err != nil {
			t.Fatalf("copy selected public facets: %v", err)
		}
		if bits&1 != 0 {
			if stateSnapshot(copied.ID, true) != publicBefore {
				t.Fatal("public item/entity state was not copied exactly, including nullable ratings and independent entity values")
			}
		} else {
			var count int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_item_data WHERE user_id=$1)+(SELECT count(*) FROM entity_user_data WHERE user_id=$1)`, copied.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("unselected user state was copied")
			}
		}
		var privateRows int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM user_item_data WHERE user_id=$1 AND (remembered_media_source_id<>'' OR remembered_media_stamp<>'' OR remembered_audio_stream_index IS NOT NULL OR remembered_subtitle_stream_index IS NOT NULL))+
			(SELECT count(*) FROM display_preferences WHERE user_id=$1)+(SELECT count(*) FROM artwork_state WHERE user_id=$1)+
			(SELECT count(*) FROM user_settings WHERE user_id=$1)+(SELECT count(*) FROM sessions WHERE user_id=$1)`, copied.ID).Scan(&privateRows); err != nil || privateRows != 0 {
			t.Fatal("user copy transferred private selection, client, artwork, or session state")
		}
		var values map[string]any
		if json.Unmarshal(copied.Configuration, &values) != nil {
			t.Fatal("copied configuration is invalid")
		}
		if bits&2 != 0 {
			if len(values) != 12 || values["AudioLanguagePreference"] != "eng" || values["SubtitleLanguagePreference"] != "zh-Hans" || values["ResumeRewindSeconds"] != float64(15) {
				t.Fatal("supported language preferences were omitted or opaque fields copied")
			}
		} else if len(values) != 0 {
			t.Fatal("unselected configuration was copied")
		}
	}
	assertManagedUnchanged(t, ctx, pool, source.ID, accountBefore)
	if stateSnapshot(source.ID, false) != stateBefore || stateSnapshot(other.ID, false) != otherBefore {
		t.Fatal("copy mutated source or unrelated user state")
	}
}

func TestPhase3UserCopyRollsBackItemStateWhenEntityCopyFails(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	source, err := store.CreateUser(ctx, "Atomic Entity Copy Source", "source-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('entity-copy-library','Copy Library','movies');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES('entity-copy-item','entity-copy-library','Feature','Feature','Movie');
		INSERT INTO catalog_entities(kind,name) VALUES('Person','Atomic Copy Person')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,rating,likes,hide_from_resume) VALUES($1,'entity-copy-item',7,true,true)`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO entity_user_data(user_id,entity_id,rating) SELECT $1,id,4 FROM catalog_entities`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_phase3_entity_copy() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected entity copy failure'; END $$;
		CREATE TRIGGER reject_phase3_entity_copy BEFORE INSERT ON entity_user_data FOR EACH ROW EXECUTE FUNCTION reject_phase3_entity_copy()`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Failed Entity Copy", source.ID, []string{"UserData"}); err == nil {
		t.Fatal("injected entity state failure was accepted")
	}
	var users, items, entities int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE normalized_name='failed entity copy'),
		(SELECT count(*) FROM user_item_data),(SELECT count(*) FROM entity_user_data)`).Scan(&users, &items, &entities); err != nil {
		t.Fatal(err)
	}
	if users != 0 || items != 1 || entities != 1 {
		t.Fatal("entity copy failure left an account or partial item state")
	}
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Revoked Entity Copy", source.ID, []string{"UserData"}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("public state copying reused revoked administrator authority")
	}
}

func TestPhase3UserCopyBoundsCombinedItemAndEntityRows(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	source, err := store.CreateUser(ctx, "Combined Copy Limit Source", "source-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('copy-limit-library','Copy Limit','movies');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES('copy-limit-item','copy-limit-library','Feature','Feature','Movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id) VALUES($1,'copy-limit-item')`, source.ID); err != nil {
		t.Fatal(err)
	}
	// Neither category exceeds 100000 rows independently; only the combined
	// facet exceeds the shared bound. This catches separate per-table limits.
	if _, err := pool.Exec(ctx, `WITH created AS (
		INSERT INTO catalog_entities(kind,name) SELECT 'Tag','Copy Capacity ' || n::text FROM generate_series(1,100000) n RETURNING id)
		INSERT INTO entity_user_data(user_id,entity_id) SELECT $1,id FROM created`, source.ID); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateManagedUserCopy(ctx, actor, "Over Combined Copy Limit", source.ID, []string{"UserData"})
	var validation *identity.ManagedUserValidationError
	if !errors.As(err, &validation) || validation.Fields["UserCopyOptions"] == "" {
		t.Fatal("combined item/entity state copy limit was not enforced")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE normalized_name='over combined copy limit'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("over-limit state copy left an account")
	}
	// An unselected UserData facet performs no state copy and remains valid.
	if _, err := store.CreateManagedUserCopy(ctx, actor, "No State Copy At Limit", source.ID, nil); err != nil {
		t.Fatalf("unselected state was still subject to its copy budget: %v", err)
	}
}
