package settings

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func settingsActivityEntries(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []activity.Entry {
	t.Helper()
	page, err := activity.QueryOwned(func(statement string, args ...any) activity.Row {
		return pool.QueryRow(ctx, statement, args...)
	}, activity.QueryOptions{Action: activity.ActionSettingsUpdated, Limit: activity.MaxPageLimit})
	if err != nil {
		t.Fatalf("query settings activity: %v", err)
	}
	if page.TotalRecordCount != int64(len(page.Items)) {
		t.Fatal("settings activity fixture exceeded its complete query page")
	}
	return page.Items
}

func settingsActivityRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var rows string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb)::text
		FROM activity_entries a WHERE action = 'settings.updated'`).Scan(&rows); err != nil {
		t.Fatalf("snapshot persisted settings activity: %v", err)
	}
	return rows
}

func assertSettingsActivityUnchanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, snapshot Snapshot, row, events string) {
	t.Helper()
	if !reflect.DeepEqual(store.Snapshot(), snapshot) {
		t.Error("rejected or unchanged operation altered the published settings snapshot")
	}
	if settingsRowSnapshot(t, ctx, pool) != row {
		t.Error("rejected or unchanged operation altered persisted settings")
	}
	if settingsActivityRows(t, ctx, pool) != events {
		t.Error("rejected or unchanged operation altered committed activity")
	}
}

func TestSettingsActivitySharesNativeAndCompatibilityRevisionsWithoutValues(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	emby := configurationTestActor(t, ctx, pool, native, false)
	initial := store.Snapshot()
	if rows := settingsActivityRows(t, ctx, pool); rows != "[]" {
		t.Fatal("settings initialization fabricated activity")
	}
	nativeName := "private-native-name-token-marker /private/media/native"
	embyName := "private-emby-name-token-marker /private/media/emby"
	extra := "private-unrelated-setting-token-marker /private/credentials"
	if _, err := pool.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES('activity-test-extra',$1)`, extra); err != nil {
		t.Fatal(err)
	}
	// An explicit default is a stored choice even when effective width stays
	// unchanged. Its field name belongs in activity; its value does not.
	width := initial.Defaults.MaxWidth
	first, err := store.Update(ctx, native, UpdateRequest{Revision: initial.Revision,
		Overrides: Overrides{ServerName: &nativeName, MaxWidth: &width}})
	if err != nil || first.Revision != 2 || first.Effective.MaxWidth != initial.Effective.MaxWidth || first.Overrides.MaxWidth == nil {
		t.Fatalf("native explicit-default settings update failed: %v", err)
	}
	initialized := true
	second, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationPartial,
		ServerNamePresent: true, ServerName: &embyName, StartupWizardCompleted: &initialized})
	if err != nil || second.Revision != 3 || second.Overrides.MaxWidth == nil || *second.Overrides.MaxWidth != width {
		t.Fatalf("compatibility settings update lost the shared native revision or choice: %v", err)
	}
	third, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationEncoding,
		TranscodingMaxWidthPresent: true, TranscodingMaxWidth: 1555})
	if err != nil || third.Revision != 4 || third.Encoding.TranscodingMaxWidth != 1555 {
		t.Fatalf("compatibility encoding update failed: %v", err)
	}
	fourth, err := store.Reset(ctx, native, ResetRequest{Revision: third.Revision,
		Fields: []Field{FieldServerName, FieldMaxWidth}})
	if err != nil || fourth.Revision != 5 || fourth.Encoding != third.Encoding || fourth.Overrides.MaxWidth != nil {
		t.Fatalf("native reset failed to preserve the independent compatibility choice: %v", err)
	}
	entries := settingsActivityEntries(t, ctx, pool)
	if len(entries) != 4 {
		t.Fatalf("settings activity count = %d, want 4 committed changes", len(entries))
	}
	want := []struct {
		revision   int64
		source     activity.Source
		credential string
		fields     []activity.Field
	}{
		{5, activity.SourceNative, native.Principal.SessionID, []activity.Field{activity.FieldMaxWidth, activity.FieldServerName, activity.FieldServerNameMode}},
		{4, activity.SourceEmby, emby.Principal.SessionID, []activity.Field{activity.FieldTranscodingMaxWidth}},
		{3, activity.SourceEmby, emby.Principal.SessionID, []activity.Field{activity.FieldServerName}},
		{2, activity.SourceNative, native.Principal.SessionID, []activity.Field{activity.FieldMaxWidth, activity.FieldServerName, activity.FieldServerNameMode}},
	}
	for index, expected := range want {
		entry := entries[index]
		if entry.Revision != expected.revision || entry.Source != expected.source || entry.Actor.Kind != activity.ActorUser ||
			entry.Actor.ID != native.Principal.User.ID || entry.Actor.CredentialID != expected.credential ||
			entry.Resource.Kind != activity.ResourceSettings || entry.Resource.ID != "1" || entry.Count != 1 ||
			!reflect.DeepEqual(entry.ChangedFields, expected.fields) {
			t.Errorf("settings activity %d did not preserve its actor, source, resource, revision, or field names", index)
		}
		if entry.Name != "Server settings updated" || entry.Overview != "Supported server settings were updated." {
			t.Error("settings activity display text was not the fixed application template")
		}
	}
	events := settingsActivityRows(t, ctx, pool)
	for _, value := range []string{nativeName, embyName, extra, "private-native-name-token-marker", "private-emby-name-token-marker", "private-unrelated-setting-token-marker"} {
		if strings.Contains(events, value) {
			t.Error("persisted activity contains a raw setting or unrelated value")
		}
	}
	row := settingsRowSnapshot(t, ctx, pool)
	mode, encoding := fourth.ServerNameMode, fourth.Encoding
	noop, err := store.Update(ctx, native, UpdateRequest{Revision: fourth.Revision,
		Overrides: fourth.Overrides, NameMode: &mode, Encoding: &encoding})
	if err != nil || !reflect.DeepEqual(noop, fourth) {
		t.Fatalf("native no-op unexpectedly changed settings: %v", err)
	}
	if _, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationPartial,
		StartupWizardCompleted: &initialized}); err != nil {
		t.Fatalf("compatibility no-op failed: %v", err)
	}
	if _, err := store.Update(ctx, native, UpdateRequest{Revision: third.Revision, Overrides: fourth.Overrides}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision returned %v, want ErrRevisionConflict", err)
	}
	wrongInitialization := false
	if _, err := store.ApplyConfiguration(ctx, emby, ConfigurationMutation{Section: ConfigurationPartial,
		ServerNamePresent: true, ServerName: &embyName, StartupWizardCompleted: &wrongInitialization}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid read-only compatibility field returned %v, want ErrInvalidInput", err)
	}
	assertSettingsActivityUnchanged(t, ctx, pool, store, fourth, row, events)
}

func settingsActivityApplicationActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, native Actor) Actor {
	t.Helper()
	credential, client := settingsTestID(t), settingsTestID(t)
	digest := sha256.Sum256([]byte(credential))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,expires_at)
		VALUES($1,NULL,$2,'application_key',NULL)`, credential, digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_devices(id,reported_device_id)
		VALUES(1,'settings-activity-server')`); err != nil {
		t.Fatal(err)
	}
	// A bigint beyond JavaScript's exact integer range proves the audit identity
	// uses its decimal database ID instead of a float or credential identifier.
	if _, err := pool.Exec(ctx, `SELECT setval(pg_get_serial_sequence('application_keys','id'),9007199254740992)`); err != nil {
		t.Fatal(err)
	}
	var keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO application_keys(credential_id,secret_ciphertext,created_by)
		VALUES($1,decode('01020304','hex'),$2) RETURNING id`, credential, native.Principal.User.ID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients
		(id,credential_id,client_name,device_id,device_name,client_version)
		VALUES($1,$2,'Private application display marker','settings-activity-server','Private device display marker','1')`, client, credential); err != nil {
		t.Fatal(err)
	}
	return Actor{Audience: identity.AdministratorEmby, Principal: identity.Principal{
		Kind: identity.ApplicationKeyKind, SessionID: credential, ClientSessionID: client, ApplicationKeyID: keyID}}
}

func TestSettingsActivityRetainsUserlessApplicationIdentity(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := settingsActivityApplicationActor(t, ctx, pool, native)
	name := "Private application settings name marker"
	result, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial,
		ServerNamePresent: true, ServerName: &name})
	if err != nil || result.Revision != 2 {
		t.Fatalf("application settings update failed: %v", err)
	}
	entries := settingsActivityEntries(t, ctx, pool)
	if len(entries) != 1 {
		t.Fatalf("application settings activity count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Actor.Kind != activity.ActorApplicationKey || entry.Actor.ID != strconv.FormatInt(actor.Principal.ApplicationKeyID, 10) ||
		entry.Actor.ID != "9007199254740993" || entry.Actor.CredentialID != actor.Principal.SessionID ||
		entry.ActorName != "" || entry.Source != activity.SourceEmby || entry.Revision != result.Revision {
		t.Error("application settings activity borrowed a user, lost bigint precision, or confused key and credential identities")
	}
	for _, value := range []string{name, "Private application display marker", "Private device display marker"} {
		if strings.Contains(settingsActivityRows(t, ctx, pool), value) {
			t.Error("application settings activity persisted request or display values")
		}
	}
}

func TestSettingsActivityInsertFailureRollsBackPersistenceAndPublication(t *testing.T) {
	for _, compatibility := range []bool{false, true} {
		name := "native"
		if compatibility {
			name = "compatibility"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, _, store, actor := settingsRepository(t)
			if compatibility {
				actor = configurationTestActor(t, ctx, pool, actor, false)
			}
			initial, row, events := store.Snapshot(), settingsRowSnapshot(t, ctx, pool), settingsActivityRows(t, ctx, pool)
			if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries ADD CONSTRAINT settings_test_activity_rejection
				CHECK (action <> 'settings.updated') NOT VALID`); err != nil {
				t.Fatal(err)
			}
			value := "Must not survive failed audit insertion"
			var err error
			if compatibility {
				_, err = store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial,
					ServerNamePresent: true, ServerName: &value})
			} else {
				_, err = store.Update(ctx, actor, UpdateRequest{Revision: initial.Revision, Overrides: Overrides{ServerName: &value}})
			}
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
				t.Fatalf("rejected audit INSERT returned %v, want a PostgreSQL check violation", err)
			}
			assertSettingsActivityUnchanged(t, ctx, pool, store, initial, row, events)
			if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries DROP CONSTRAINT settings_test_activity_rejection`); err != nil {
				t.Fatal(err)
			}
			var recovered Snapshot
			if compatibility {
				recovered, err = store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial,
					ServerNamePresent: true, ServerName: &value})
			} else {
				recovered, err = store.Update(ctx, actor, UpdateRequest{Revision: initial.Revision, Overrides: Overrides{ServerName: &value}})
			}
			if err != nil || recovered.Revision != initial.Revision+1 || len(settingsActivityEntries(t, ctx, pool)) != 1 {
				t.Fatalf("settings owner did not recover after audit insertion failure: %v", err)
			}
		})
	}
}

func waitSettingsActivityInsert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerPID, blockerPID int32, relationOID uint32) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(waitCtx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid = a.pid
			WHERE a.pid = $1 AND l.relation = $2::oid AND NOT l.granted
			AND l.mode = 'RowExclusiveLock' AND a.wait_event_type = 'Lock'
			AND $3::integer = ANY(pg_blocking_pids(a.pid)))`, ownerPID, relationOID, blockerPID).Scan(&waiting); err != nil {
			t.Fatalf("observe settings audit INSERT table-lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("settings writer never reached the blocked activity INSERT")
		case <-ticker.C:
		}
	}
}

func TestSettingsActivityWaitRechecksNaturalActorExpiryBeforeCommit(t *testing.T) {
	for _, compatibility := range []bool{false, true} {
		name := "native"
		if compatibility {
			name = "compatibility"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, owner, store, actor := settingsRepository(t)
			// Complete every authentication fixture before locking activity. Login
			// and bootstrap now write their own activity in their transactions.
			if compatibility {
				actor = configurationTestActor(t, ctx, pool, actor, false)
			}
			initial, row, events := store.Snapshot(), settingsRowSnapshot(t, ctx, pool), settingsActivityRows(t, ctx, pool)
			var ownerPID int32
			if err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
				return tx.QueryRow("SELECT pg_backend_pid()").Scan(&ownerPID)
			}); err != nil {
				t.Fatal(err)
			}
			blocker, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = blocker.Rollback(cleanup)
			}()
			var blockerPID int32
			var relationOID uint32
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid(),'activity_entries'::regclass::oid").Scan(&blockerPID, &relationOID); err != nil {
				t.Fatal(err)
			}
			if _, err := blocker.Exec(ctx, "LOCK TABLE activity_entries IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.Principal.SessionID); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				value := "Must roll back after audit wait and natural expiry"
				if compatibility {
					_, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial,
						ServerNamePresent: true, ServerName: &value})
					result <- err
					return
				}
				_, err := store.Update(ctx, actor, UpdateRequest{Revision: initial.Revision, Overrides: Overrides{ServerName: &value}})
				result <- err
			}()
			waitSettingsActivityInsert(t, ctx, pool, ownerPID, blockerPID, relationOID)
			if !reflect.DeepEqual(store.Snapshot(), initial) || settingsRowSnapshot(t, ctx, pool) != row {
				t.Fatal("settings became visible while their audit INSERT was still blocked")
			}
			// Observe expiry on a separate connection without changing the actor
			// row locked by the writer. The wait derives from the database clock.
			if _, err := pool.Exec(ctx, `SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
				(expires_at-clock_timestamp())))+0.05)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID); err != nil {
				t.Fatal(err)
			}
			var expired bool
			if err := pool.QueryRow(ctx, "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id=$1", actor.Principal.SessionID).Scan(&expired); err != nil || !expired {
				t.Fatalf("actor did not naturally expire while activity INSERT waited: %v", err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("expired actor after audit INSERT wait returned %v, want ErrUnauthorized", err)
				}
			case <-ctx.Done():
				t.Fatal("settings writer did not finish after the activity table lock was released")
			}
			assertSettingsActivityUnchanged(t, ctx, pool, store, initial, row, events)
			if err := owner.CheckOwnership(ctx); err != nil {
				t.Fatalf("audit wait rejection lost catalog ownership: %v", err)
			}
		})
	}
}
