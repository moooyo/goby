package identity_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreCopyUserSelectsFacetsAndNeverCopiesCredentialsOrAccountState(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	source, err := store.CreateUser(ctx, "Copy Source", "source-password", true)
	if err != nil {
		t.Fatal(err)
	}
	managedLogin(t, ctx, store, source, "source-password", "emby")
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('copy-library','Copy Library','movies');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES('copy-item','copy-library','Copy Movie','Copy Movie','Movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=true,
		policy='{"IsAdministrator":true,"IsDisabled":true,"EnableAllFolders":false,"EnabledFolders":["copy-library"],
		"IsHidden":true,"BlockedTags":["adult"],"LockedOutDate":638000000000000000,"InvalidLoginAttemptCount":9,"OpaqueSecret":"private-source-policy"}'::jsonb,
		configuration='{"SubtitleMode":"Always","ResumeRewindSeconds":5,"OrderedViews":["copy-library"],"Pin":"private-source-pin"}'::jsonb WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at)
		VALUES($1,'copy-item',123456,7,true,false,'2026-09-01T12:34:56Z')`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_settings(user_id,settings)
		VALUES($1,'{"opaque-client-setting":"private-source-setting"}'::jsonb)`, source.ID); err != nil {
		t.Fatal(err)
	}
	before := managedSnapshot(t, ctx, pool, source.ID)
	var sourceDataBefore string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id='copy-item'`, source.ID).Scan(&sourceDataBefore); err != nil {
		t.Fatal(err)
	}
	for bits := 0; bits < 8; bits++ {
		options := []string{}
		for bit, option := range []string{"UserPolicy", "UserConfiguration", "UserData"} {
			if bits&(1<<bit) != 0 {
				options = append(options, option)
			}
		}
		copied, err := store.CreateManagedUserCopy(ctx, actor, fmt.Sprintf("Copied User %d", bits), source.ID, options)
		if err != nil {
			t.Fatalf("copy selection %v: %v", options, err)
		}
		if copied.ID == source.ID || copied.IsAdministrator || copied.IsDisabled || copied.HasPassword {
			t.Fatalf("copy selection %v inherited source identity or authentication state", options)
		}
		var sameHash bool
		var sessions, settings int
		if err := pool.QueryRow(ctx, `SELECT password_hash=(SELECT password_hash FROM users WHERE id=$2),
			(SELECT count(*) FROM sessions WHERE user_id=$1), (SELECT count(*) FROM user_settings WHERE user_id=$1)
			FROM users WHERE id=$1`, copied.ID, source.ID).Scan(&sameHash, &sessions, &settings); err != nil {
			t.Fatal(err)
		}
		if sameHash || sessions != 0 || settings != 0 {
			t.Fatal("copy transferred credentials, sessions, or separate opaque user settings")
		}
		policy, err := identity.ParseManagedPolicy(copied.Policy)
		if err != nil {
			t.Fatal(err)
		}
		if bits&1 != 0 {
			if policy.EnableAllFolders || !policy.IsHidden || !reflect.DeepEqual(policy.EnabledFolders, []string{"copy-library"}) || !reflect.DeepEqual(policy.BlockedTags, []string{"adult"}) {
				t.Fatalf("selected policy facts were not copied: %#v", policy)
			}
		} else if !policy.EnableAllFolders || policy.IsHidden || len(policy.BlockedTags) != 0 {
			t.Fatal("unselected policy facts were copied")
		}
		var configuration map[string]any
		if err := json.Unmarshal(copied.Configuration, &configuration); err != nil {
			t.Fatal(err)
		}
		if bits&2 != 0 {
			if configuration["SubtitleMode"] != "Always" || configuration["ResumeRewindSeconds"] != float64(5) || len(configuration) != 3 {
				t.Fatalf("selected configuration facts were not copied: %#v", configuration)
			}
		} else if len(configuration) != 0 {
			t.Fatal("unselected configuration facts were copied")
		}
		if strings.Contains(string(copied.Policy), "private-") || strings.Contains(string(copied.Configuration), "private-") ||
			strings.Contains(string(copied.Policy), "LockedOutDate") || strings.Contains(string(copied.Policy), "IsAdministrator") {
			t.Fatal("copy transferred opaque secrets or independent account state")
		}
		var stateMatches bool
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_item_data WHERE user_id=$1`, copied.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if bits&4 != 0 {
			if err := pool.QueryRow(ctx, `SELECT playback_position_ticks=123456 AND play_count=7 AND is_favorite AND NOT played
				AND last_played_at='2026-09-01T12:34:56Z'::timestamptz FROM user_item_data WHERE user_id=$1 AND item_id='copy-item'`, copied.ID).Scan(&stateMatches); err != nil {
				t.Fatal(err)
			}
			if count != 1 || !stateMatches {
				t.Fatal("selected persistent media state was not copied exactly")
			}
		} else if count != 0 {
			t.Fatal("unselected media state was copied")
		}
	}
	assertManagedUnchanged(t, ctx, pool, source.ID, before)
	var sourceDataAfter string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id='copy-item'`, source.ID).Scan(&sourceDataAfter); err != nil || sourceDataAfter != sourceDataBefore {
		t.Fatalf("copy modified source media state: %v", err)
	}
}

func TestStoreCopyUserRejectsStaleAuthorityAndRollsBackBusinessFailure(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	source, err := store.CreateUser(ctx, "Atomic Copy Source", "source-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Missing Copy", "missing-user", []string{"UserData"}); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("missing source = %v, want not found", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('atomic-copy-library','Atomic Copy Library','movies');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES('atomic-copy-item','atomic-copy-library','Copy Movie','Copy Movie','Movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,play_count) VALUES($1,'atomic-copy-item',7)`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_copied_data_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.item_id='atomic-copy-item' THEN RAISE EXCEPTION 'rejected copied data'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER reject_copied_data_for_test BEFORE INSERT ON user_item_data FOR EACH ROW EXECUTE FUNCTION reject_copied_data_for_test()`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Failed Copy", source.ID, []string{"UserData"}); err == nil {
		t.Fatal("injected copy failure succeeded")
	}
	var created bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE normalized_name IN ('failed copy','missing copy'))`).Scan(&created); err != nil || created {
		t.Fatalf("failed copy left a user: exists=%v, error=%v", created, err)
	}
	if err := store.Revoke(ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Revoked Copy", source.ID, []string{"UserConfiguration"}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked administrator copy = %v, want unauthorized", err)
	}
}
