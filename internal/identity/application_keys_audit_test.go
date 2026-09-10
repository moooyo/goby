package identity_test

import (
	"context"
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
)

type applicationKeyActivityFact struct {
	action       string
	source       string
	actorKind    string
	actorID      string
	credentialID string
	resourceID   string
	count        int64
}

func applicationKeyActivityFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []applicationKeyActivityFact {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT action, source, actor_kind, actor_id,
		actor_credential_id, resource_id, affected_count FROM activity_entries
		WHERE resource_kind = 'application_key' ORDER BY id`)
	if err != nil {
		t.Fatalf("read application key activity: %v", err)
	}
	defer rows.Close()
	var facts []applicationKeyActivityFact
	for rows.Next() {
		var fact applicationKeyActivityFact
		if err := rows.Scan(&fact.action, &fact.source, &fact.actorKind, &fact.actorID,
			&fact.credentialID, &fact.resourceID, &fact.count); err != nil {
			t.Fatalf("read application key activity fact: %v", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read application key activity rows: %v", err)
	}
	return facts
}

func applicationKeyAuditSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id), '[]'::jsonb)::text
		FROM activity_entries a WHERE resource_kind = 'application_key'`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot application key activity: %v", err)
	}
	return snapshot
}

func applicationKeyAuditBusinessSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var devices string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY id), '[]'::jsonb)::text
		FROM application_key_devices d`).Scan(&devices); err != nil {
		t.Fatalf("snapshot application key device registry: %v", err)
	}
	return applicationKeySnapshot(t, ctx, pool) + devices
}

func TestStoreApplicationKeyActivityRecordsOnlyCommittedFacts(t *testing.T) {
	ctx, pool, store, admin, vaultPath := applicationKeyTestStore(t)
	const firstName = "sensitive-key-name-canary"
	const secondName = "sensitive-second-key-name-canary"
	first := issueApplicationKey(t, ctx, store, admin, firstName)
	keyActor := applicationKeyPrincipal(t, ctx, store, first)
	second := issueApplicationKey(t, ctx, store, keyActor, secondName)
	_, embyAdmin := managedLogin(t, ctx, store, admin.User, "administrator-password", "emby")
	if metadata, err := store.GetApplicationKey(ctx, admin, first.ID, false); err != nil || metadata.Token != "" {
		t.Fatalf("read safe key metadata: %v", err)
	}
	if _, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{}); err != nil {
		t.Fatalf("list safe key metadata: %v", err)
	}
	if empty, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{StartIndex: 99, RevealTokens: true}); err != nil || len(empty.Items) != 0 {
		t.Fatalf("reveal empty key page: %v", err)
	}
	if revealed, err := store.GetApplicationKey(ctx, embyAdmin, first.ID, true); err != nil || revealed.Token != first.Token {
		t.Fatalf("prepare authorized key reveal: %v", err)
	}
	page, err := store.ListApplicationKeys(ctx, keyActor, identity.ApplicationKeyFilter{Limit: 1, RevealTokens: true})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != second.ID || page.Items[0].Token != second.Token {
		t.Fatalf("prepare one page of key reveals: %v", err)
	}
	revoked, err := store.RevokeApplicationKey(ctx, admin, first.ID)
	if err != nil {
		t.Fatalf("revoke audited application key: %v", err)
	}
	if again, err := store.RevokeApplicationKey(ctx, admin, first.ID); err != nil || !again.RevokedAt.Equal(revoked.RevokedAt) {
		t.Fatalf("repeat audited key revocation: %v", err)
	}
	for _, token := range []string{first.Token, "invalid-sensitive-token-canary", strings.Repeat("A", 43)} {
		if _, err := store.RevokeApplicationKeyToken(ctx, admin, token); err != nil {
			t.Fatalf("perform key revocation no-op: %v", err)
		}
	}
	if history, err := store.GetApplicationKey(ctx, admin, first.ID, true); err != nil || history.Token != "" {
		t.Fatalf("read revoked key without reveal: %v", err)
	}
	history, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{
		IncludeRevoked: true, RevealTokens: true, SearchTerm: firstName,
	})
	if err != nil || len(history.Items) != 1 || history.Items[0].Token != "" {
		t.Fatalf("list revoked key without reveal: %v", err)
	}
	firstID, secondID := strconv.FormatInt(first.ID, 10), strconv.FormatInt(second.ID, 10)
	want := []applicationKeyActivityFact{
		{string(activity.ActionApplicationKeyCreated), "native", "user", admin.User.ID, admin.SessionID, firstID, 1},
		{string(activity.ActionApplicationKeyCreated), "emby", "application_key", firstID, first.CredentialID, secondID, 1},
		{string(activity.ActionApplicationKeyRevealed), "emby", "user", admin.User.ID, embyAdmin.SessionID, firstID, 1},
		{string(activity.ActionApplicationKeyRevealed), "emby", "application_key", firstID, first.CredentialID, secondID, 1},
		{string(activity.ActionApplicationKeyRevoked), "native", "user", admin.User.ID, admin.SessionID, firstID, 1},
	}
	if facts := applicationKeyActivityFacts(t, ctx, pool); !reflect.DeepEqual(facts, want) {
		t.Fatalf("application key activity facts = %#v, want %#v", facts, want)
	}
	var digest, ciphertext string
	if err := pool.QueryRow(ctx, `SELECT encode(s.token_hash, 'hex'), encode(k.secret_ciphertext, 'hex')
		FROM sessions s JOIN application_keys k ON k.credential_id = s.id WHERE k.id = $1`, first.ID).
		Scan(&digest, &ciphertext); err != nil {
		t.Fatalf("read sensitive test fixtures: %v", err)
	}
	stored := applicationKeyAuditSnapshot(t, ctx, pool)
	for _, sensitive := range []string{firstName, secondName, first.Token, second.Token, digest, ciphertext, vaultPath,
		admin.User.Name, "administrator-password", "192.0.2.14", "persistent-server-id", "Test Server", "invalid-sensitive-token-canary"} {
		if strings.Contains(stored, sensitive) {
			t.Fatal("application key activity persisted a sensitive value")
		}
	}
	var unsafeFacts int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE resource_kind = 'application_key'
		AND (severity <> 'Info' OR request_id <> '' OR revision <> 0 OR state <> '' OR cardinality(changed_fields) <> 0)`).
		Scan(&unsafeFacts); err != nil || unsafeFacts != 0 {
		t.Fatalf("application key activity contains unexpected facts: %v", err)
	}
}

func TestStoreApplicationKeyRevealActivityRollsBackOnDecryptionFailure(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	corrupt := issueApplicationKey(t, ctx, store, admin, "Older corrupted key")
	issueApplicationKey(t, ctx, store, admin, "Newer valid key")
	if _, err := pool.Exec(ctx, `UPDATE application_keys SET secret_ciphertext = set_byte(secret_ciphertext,
		length(secret_ciphertext) - 1, get_byte(secret_ciphertext, length(secret_ciphertext) - 1) # 1) WHERE id = $1`, corrupt.ID); err != nil {
		t.Fatalf("corrupt owned ciphertext authentication tag: %v", err)
	}
	before := applicationKeyAuditSnapshot(t, ctx, pool)
	businessBefore := applicationKeyAuditBusinessSnapshot(t, ctx, pool)
	if key, err := store.GetApplicationKey(ctx, admin, corrupt.ID, true); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) || key != (identity.ApplicationKey{}) {
		t.Fatalf("failed decryption returned a key or the wrong error: %v", err)
	}
	// The valid newer row is decrypted and recorded first. A later corrupt row
	// must roll back that pending fact instead of recording a partial response.
	if page, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{RevealTokens: true}); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) || page.Items != nil || page.TotalRecordCount != 0 {
		t.Fatalf("partial failed key list escaped its transaction: %v", err)
	}
	if metadata, err := store.GetApplicationKey(ctx, admin, corrupt.ID, false); err != nil || metadata.Token != "" {
		t.Fatalf("safe metadata depended on secret decryption: %v", err)
	}
	if applicationKeyAuditSnapshot(t, ctx, pool) != before || applicationKeyAuditBusinessSnapshot(t, ctx, pool) != businessBefore {
		t.Fatal("failed key decryption changed activity or business state")
	}
}

func TestStoreApplicationKeyAuditFailureRollsBackAndPreservesNoOps(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	active := issueApplicationKey(t, ctx, store, admin, "Active failure fixture")
	revoked := issueApplicationKey(t, ctx, store, admin, "Revoked no-op fixture")
	if _, err := store.RevokeApplicationKey(ctx, admin, revoked.ID); err != nil {
		t.Fatalf("prepare revoked key fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE activity_entries ADD CONSTRAINT application_key_audit_error_canary
		CHECK (resource_kind <> 'application_key') NOT VALID`); err != nil {
		t.Fatalf("install application key activity failure: %v", err)
	}
	before := applicationKeyAuditSnapshot(t, ctx, pool)
	businessBefore := applicationKeyAuditBusinessSnapshot(t, ctx, pool)
	assertRejected := func(err error) {
		t.Helper()
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("activity failure = %v, want PostgreSQL check violation", err)
		}
		if applicationKeyAuditSnapshot(t, ctx, pool) != before || applicationKeyAuditBusinessSnapshot(t, ctx, pool) != businessBefore {
			t.Fatal("failed activity insertion changed application key state")
		}
	}
	created, err := store.CreateApplicationKey(ctx, admin, "Creation must roll back", "192.0.2.99", identity.Client{
		DeviceID: "persistent-server-id", Device: "Must not update the registry", Version: "2.0",
	})
	if created != (identity.ApplicationKey{}) {
		t.Fatal("failed audited creation returned a credential")
	}
	assertRejected(err)
	revealed, err := store.GetApplicationKey(ctx, admin, active.ID, true)
	if revealed != (identity.ApplicationKey{}) {
		t.Fatal("failed audited reveal returned a credential")
	}
	assertRejected(err)
	page, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{RevealTokens: true})
	if page.Items != nil || page.TotalRecordCount != 0 {
		t.Fatal("failed audited list returned credentials")
	}
	assertRejected(err)
	revocation, err := store.RevokeApplicationKey(ctx, admin, active.ID)
	if revocation != (identity.ApplicationKeyRevocation{}) {
		t.Fatal("failed audited revocation returned committed state")
	}
	assertRejected(err)
	if _, err := store.RevokeApplicationKey(ctx, admin, revoked.ID); err != nil {
		t.Fatalf("repeated revocation unnecessarily required activity insertion: %v", err)
	}
	for _, token := range []string{revoked.Token, "invalid", strings.Repeat("A", 43)} {
		if _, err := store.RevokeApplicationKeyToken(ctx, admin, token); err != nil {
			t.Fatalf("revocation no-op unnecessarily required activity insertion: %v", err)
		}
	}
	if _, err := store.GetApplicationKey(ctx, admin, active.ID, false); err != nil {
		t.Fatalf("safe key read unnecessarily required activity insertion: %v", err)
	}
	if _, err := store.ListApplicationKeys(ctx, admin, identity.ApplicationKeyFilter{}); err != nil {
		t.Fatalf("safe key list unnecessarily required activity insertion: %v", err)
	}
	if applicationKeyAuditSnapshot(t, ctx, pool) != before || applicationKeyAuditBusinessSnapshot(t, ctx, pool) != businessBefore {
		t.Fatal("no-op operations changed activity or application key state")
	}
}

func pauseApplicationKeyActivityInsert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action) (pgx.Tx, int32) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE application_key_activity_gate (
		action text PRIMARY KEY,
		lock_class integer NOT NULL,
		lock_key integer NOT NULL,
		insert_count integer NOT NULL DEFAULT 0
	);
	CREATE FUNCTION pause_application_key_activity_insert() RETURNS trigger LANGUAGE plpgsql AS $function$
	DECLARE
		gate application_key_activity_gate%ROWTYPE;
	BEGIN
		UPDATE application_key_activity_gate SET insert_count = insert_count + 1
			WHERE action = NEW.action RETURNING * INTO gate;
		IF FOUND THEN
			PERFORM pg_advisory_xact_lock(gate.lock_class, gate.lock_key);
		END IF;
		RETURN NEW;
	END;
	$function$;
	CREATE TRIGGER application_key_activity_barrier AFTER INSERT ON activity_entries
		FOR EACH ROW WHEN (NEW.resource_kind = 'application_key')
		EXECUTE FUNCTION pause_application_key_activity_insert()`); err != nil {
		t.Fatalf("install application key activity barrier: %v", err)
	}
	blocker, pid := managedSessionBlocker(t, ctx, pool)
	const lockClass int32 = 1196446298
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1::integer, $2::integer)", lockClass, pid); err != nil {
		t.Fatalf("hold application key activity barrier: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_activity_gate (action, lock_class, lock_key)
		VALUES ($1, $2, $3)`, string(action), lockClass, pid); err != nil {
		t.Fatalf("configure application key activity barrier: %v", err)
	}
	return blocker, pid
}

func TestStoreApplicationKeyExpiryDuringActivityInsertRollsBack(t *testing.T) {
	cases := []struct {
		name   string
		action activity.Action
		run    func(context.Context, *identity.Store, identity.Principal, identity.ApplicationKey) (bool, error)
	}{
		{"create", activity.ActionApplicationKeyCreated, func(ctx context.Context, store *identity.Store, actor identity.Principal, _ identity.ApplicationKey) (bool, error) {
			key, err := store.CreateApplicationKey(ctx, actor, "Expired creation", "192.0.2.88", identity.Client{DeviceID: "persistent-server-id", Device: "Uncommitted device"})
			return key != (identity.ApplicationKey{}), err
		}},
		{"get-reveal", activity.ActionApplicationKeyRevealed, func(ctx context.Context, store *identity.Store, actor identity.Principal, key identity.ApplicationKey) (bool, error) {
			result, err := store.GetApplicationKey(ctx, actor, key.ID, true)
			return result != (identity.ApplicationKey{}), err
		}},
		{"list-reveal", activity.ActionApplicationKeyRevealed, func(ctx context.Context, store *identity.Store, actor identity.Principal, _ identity.ApplicationKey) (bool, error) {
			result, err := store.ListApplicationKeys(ctx, actor, identity.ApplicationKeyFilter{RevealTokens: true})
			return result.Items != nil || result.TotalRecordCount != 0, err
		}},
		{"revoke", activity.ActionApplicationKeyRevoked, func(ctx context.Context, store *identity.Store, actor identity.Principal, key identity.ApplicationKey) (bool, error) {
			result, err := store.RevokeApplicationKey(ctx, actor, key.ID)
			return result != (identity.ApplicationKeyRevocation{}), err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			testCtx, pool, store, admin, _ := applicationKeyTestStore(t)
			key := issueApplicationKey(t, testCtx, store, admin, "Expiry activity target")
			ctx, cancel := context.WithTimeout(testCtx, 20*time.Second)
			defer cancel()
			blocker, blockerPID := pauseApplicationKeyActivityInsert(t, ctx, pool, test.action)
			if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
				expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, admin.SessionID); err != nil {
				t.Fatalf("set actor expiry for activity barrier: %v", err)
			}
			before := applicationKeyAuditSnapshot(t, ctx, pool)
			businessBefore := applicationKeyAuditBusinessSnapshot(t, ctx, pool)
			type outcome struct {
				returned bool
				err      error
			}
			results := make(chan outcome, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				returned, err := test.run(ctx, store, admin, key)
				results <- outcome{returned, err}
			}()
			waitManagedBlockedQuery(t, ctx, pool, blockerPID, "INSERT INTO activity_entries", done)
			waitManagedSessionDatabaseExpiry(t, ctx, pool, admin.SessionID, done)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatalf("release application key activity barrier: %v", err)
			}
			select {
			case result := <-results:
				if !errors.Is(result.err, identity.ErrUnauthorized) || result.returned {
					t.Fatalf("expired actor committed application key activity or returned a result: %v", result.err)
				}
			case <-ctx.Done():
				t.Fatal("application key activity operation did not finish")
			}
			if applicationKeyAuditSnapshot(t, testCtx, pool) != before || applicationKeyAuditBusinessSnapshot(t, testCtx, pool) != businessBefore {
				t.Fatal("expired actor changed application key activity or business state")
			}
			var insertCount int64
			if err := pool.QueryRow(testCtx, "SELECT insert_count FROM application_key_activity_gate").Scan(&insertCount); err != nil || insertCount != 0 {
				t.Fatalf("rejected activity insert retained transactional side effects: %v", err)
			}
		})
	}
}
