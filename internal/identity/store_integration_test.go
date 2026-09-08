package identity_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

// Each integration test owns a new schema in the explicitly configured database.
// The suite never drops or truncates pre-existing schemas or tables.
func identityTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *identity.Store) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create integration database connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "goby_identity_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create isolated test schema: %v", err)
	}
	// This cleanup is registered only after this test successfully creates its schema.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated integration database pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	return ctx, pool, identity.New(pool)
}

func bootstrapTestAdmin(t *testing.T, ctx context.Context, store *identity.Store) identity.User {
	t.Helper()
	user, err := store.Bootstrap(ctx, "Administrator", "administrator-password")
	if err != nil {
		t.Fatalf("bootstrap administrator: %v", err)
	}
	return user
}

func TestStoreBootstrapConcurrent(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	initialized, err := store.Initialized(ctx)
	if err != nil || initialized {
		t.Fatalf("fresh store initialized = %v, error = %v", initialized, err)
	}
	type result struct {
		user identity.User
		err  error
	}
	const attempts = 8
	start := make(chan struct{})
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		go func(index int) {
			<-start
			user, err := store.Bootstrap(ctx, fmt.Sprintf("Admin %d", index), "administrator-password")
			results <- result{user: user, err: err}
		}(i)
	}
	close(start)
	successes := 0
	for i := 0; i < attempts; i++ {
		outcome := <-results
		if outcome.err == nil {
			successes++
			if outcome.user.ID == "" || !outcome.user.IsAdministrator || !outcome.user.HasPassword {
				t.Errorf("bootstrap returned invalid administrator: %+v", outcome.user)
			}
		} else if !errors.Is(outcome.err, identity.ErrAlreadyInitialized) {
			t.Errorf("concurrent bootstrap returned unexpected error: %v", outcome.err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful bootstrap attempts = %d, want 1", successes)
	}
	users, err := store.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("users after concurrent bootstrap = %d, error = %v", len(users), err)
	}
	initialized, err = store.Initialized(ctx)
	if err != nil || !initialized {
		t.Fatalf("bootstrapped store initialized = %v, error = %v", initialized, err)
	}
}

func TestStoreSessionKindsAndAdministratorRole(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	client := identity.Client{Name: "Integration Client", DeviceID: "device-1", Device: "Linux", Version: "1.0"}
	var tokens []string
	for _, kind := range []string{"admin", "emby"} {
		credentials, err := store.Authenticate(ctx, admin.Name, "administrator-password", client, kind)
		if err != nil {
			t.Fatalf("authenticate %s session: %v", kind, err)
		}
		if credentials.Token == "" || credentials.SessionID == "" || credentials.User.ID != admin.ID || !credentials.ExpiresAt.After(time.Now()) {
			t.Fatalf("authenticate %s returned incomplete credentials", kind)
		}
		principal, err := store.Resolve(ctx, credentials.Token, kind)
		if err != nil {
			t.Fatalf("resolve %s session: %v", kind, err)
		}
		if principal.User.ID != admin.ID || principal.SessionID != credentials.SessionID || principal.Kind != kind || principal.Client != client {
			t.Errorf("resolved %s principal does not match authentication: %+v", kind, principal)
		}
		var lifetimeSeconds int64
		if err := pool.QueryRow(ctx, "SELECT EXTRACT(EPOCH FROM (expires_at - created_at))::bigint FROM sessions WHERE id = $1", credentials.SessionID).Scan(&lifetimeSeconds); err != nil {
			t.Fatalf("read %s session lifetime: %v", kind, err)
		}
		expectedSeconds := int64(24 * 60 * 60)
		if kind == "emby" {
			expectedSeconds *= 30
		}
		if lifetimeSeconds != expectedSeconds {
			t.Errorf("%s session lifetime = %d seconds, want %d", kind, lifetimeSeconds, expectedSeconds)
		}
		otherKind := "admin"
		if kind == "admin" {
			otherKind = "emby"
		}
		if _, err := store.Resolve(ctx, credentials.Token, otherKind); !errors.Is(err, identity.ErrUnauthorized) {
			t.Errorf("resolve %s token as %s: got %v, want ErrUnauthorized", kind, otherKind, err)
		}
		tokens = append(tokens, credentials.Token)
	}
	if tokens[0] == tokens[1] {
		t.Error("separate authentications returned the same token")
	}
	viewer, err := store.CreateUser(ctx, "Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if _, err := store.Authenticate(ctx, viewer.Name, "viewer-password", client, "admin"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("viewer administrator login: got %v, want ErrInvalidCredentials", err)
	}
	if _, err := store.Authenticate(ctx, viewer.Name, "viewer-password", client, "emby"); err != nil {
		t.Errorf("viewer Emby login: %v", err)
	}
	for _, attempt := range []struct{ name, password string }{
		{name: "missing-user", password: "administrator-password"},
		{name: admin.Name, password: "wrong-password"},
	} {
		if _, err := store.Authenticate(ctx, attempt.name, attempt.password, client, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Errorf("invalid login for %q: got %v, want ErrInvalidCredentials", attempt.name, err)
		}
	}
	if _, err := store.Resolve(ctx, "unknown-token", "emby"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("unknown token: got %v, want ErrUnauthorized", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = false WHERE id = $1", admin.ID); err != nil {
		t.Fatalf("demote test administrator: %v", err)
	}
	if _, err := store.Resolve(ctx, tokens[0], "admin"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("demoted administrator session: got %v, want ErrUnauthorized", err)
	}
	if _, err := store.Resolve(ctx, tokens[1], "emby"); err != nil {
		t.Errorf("demoted user's Emby session must remain valid: %v", err)
	}
}

func TestStorePersistenceAndRevocation(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	serverID, err := store.ServerID(ctx)
	if err != nil || serverID == "" {
		t.Fatalf("server ID = %q, error = %v", serverID, err)
	}
	credentials, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "emby")
	if err != nil {
		t.Fatalf("authenticate before recreating store: %v", err)
	}
	reopened := identity.New(pool)
	if initialized, err := reopened.Initialized(ctx); err != nil || !initialized {
		t.Fatalf("reopened store initialized = %v, error = %v", initialized, err)
	}
	if reopenedID, err := reopened.ServerID(ctx); err != nil || reopenedID != serverID {
		t.Errorf("reopened server ID = %q, want %q, error = %v", reopenedID, serverID, err)
	}
	if user, err := reopened.GetUser(ctx, admin.ID); err != nil || user.ID != admin.ID || user.Name != admin.Name {
		t.Errorf("reopened user = %+v, error = %v", user, err)
	}
	if _, err := reopened.Resolve(ctx, credentials.Token, "emby"); err != nil {
		t.Fatalf("resolve persisted token: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := reopened.Revoke(ctx, credentials.Token); err != nil {
			t.Fatalf("revoke attempt %d: %v", i+1, err)
		}
	}
	if _, err := store.Resolve(ctx, credentials.Token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("revoked token resolved by original store: got %v, want ErrUnauthorized", err)
	}
	if err := reopened.Revoke(ctx, "unknown-token"); err != nil {
		t.Errorf("revoke unknown token: %v", err)
	}
	if _, err := reopened.GetUser(ctx, "unknown-user"); !errors.Is(err, identity.ErrNotFound) {
		t.Errorf("get unknown user: got %v, want ErrNotFound", err)
	}
}

func TestStoreRejectsDisabledAndExpiredSessions(t *testing.T) {
	for _, state := range []string{"disabled", "expired"} {
		t.Run(state, func(t *testing.T) {
			ctx, pool, store := identityTestStore(t)
			admin := bootstrapTestAdmin(t, ctx, store)
			credentials, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "emby")
			if err != nil {
				t.Fatalf("authenticate: %v", err)
			}
			if state == "disabled" {
				if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = $1", admin.ID); err != nil {
					t.Fatalf("disable test user: %v", err)
				}
				if _, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "emby"); !errors.Is(err, identity.ErrInvalidCredentials) {
					t.Errorf("disabled user authentication: got %v, want ErrInvalidCredentials", err)
				}
			} else {
				if _, err := pool.Exec(ctx, "UPDATE sessions SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = $1", credentials.SessionID); err != nil {
					t.Fatalf("expire test session: %v", err)
				}
			}
			if _, err := store.Resolve(ctx, credentials.Token, "emby"); !errors.Is(err, identity.ErrUnauthorized) {
				t.Errorf("resolve %s session: got %v, want ErrUnauthorized", state, err)
			}
		})
	}
}

func TestStoreUserValidationAndCaseFolding(t *testing.T) {
	ctx, _, store := identityTestStore(t)
	for _, password := range []string{"", strings.Repeat("a", 73), strings.Repeat("é", 37)} {
		if _, err := store.Bootstrap(ctx, "Administrator", password); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("bootstrap with %d-byte password: got %v, want ErrInvalidInput", len(password), err)
		}
	}
	bootstrapTestAdmin(t, ctx, store)
	for _, input := range []struct {
		name, password string
		administrator  bool
	}{
		{name: "   ", password: "password"},
		{name: "Empty Admin", administrator: true},
		{name: "Long Password", password: strings.Repeat("a", 73)},
	} {
		if _, err := store.CreateUser(ctx, input.name, input.password, input.administrator); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("invalid user %q: got %v, want ErrInvalidInput", input.name, err)
		}
	}
	user, err := store.CreateUser(ctx, "  MediaΣ  ", "viewer-password", false)
	if err != nil {
		t.Fatalf("create Unicode user: %v", err)
	}
	if user.Name != "MediaΣ" {
		t.Errorf("trimmed name = %q, want MediaΣ", user.Name)
	}
	// Greek sigma and final sigma share a Unicode simple-fold equivalence class.
	if _, err := store.CreateUser(ctx, "mEDIAς", "other-password", false); !errors.Is(err, identity.ErrInvalidInput) {
		t.Errorf("duplicate Unicode-folded user: got %v, want ErrInvalidInput", err)
	}
	if _, err := store.CreateUser(ctx, "ADMINISTRATOR", "other-password", false); !errors.Is(err, identity.ErrInvalidInput) {
		t.Errorf("duplicate ASCII-folded user: got %v, want ErrInvalidInput", err)
	}
	credentials, err := store.Authenticate(ctx, "  mediaς  ", "viewer-password", identity.Client{}, "emby")
	if err != nil || credentials.User.ID != user.ID {
		t.Errorf("authenticate trimmed Unicode-folded name: error = %v", err)
	}
	guest, err := store.CreateUser(ctx, "Guest", "", false)
	if err != nil || guest.HasPassword {
		t.Fatalf("create passwordless non-administrator: has password = %v, error = %v", guest.HasPassword, err)
	}
	if _, err := store.Authenticate(ctx, guest.Name, "", identity.Client{}, "emby"); err != nil {
		t.Errorf("authenticate passwordless non-administrator: %v", err)
	}
}

func TestStoreDoesNotPersistRawSecrets(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	credentials, err := store.Authenticate(ctx, admin.Name, "administrator-password", identity.Client{}, "emby")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	var tokenHash []byte
	var sessionJSON, userJSON string
	if err := pool.QueryRow(ctx, "SELECT token_hash, row_to_json(s)::text FROM sessions s WHERE id = $1", credentials.SessionID).Scan(&tokenHash, &sessionJSON); err != nil {
		t.Fatalf("read stored session: %v", err)
	}
	if len(tokenHash) == 0 || bytes.Equal(tokenHash, []byte(credentials.Token)) {
		t.Error("session must store a nonempty token digest instead of the raw token")
	}
	if strings.Contains(sessionJSON, credentials.Token) || strings.Contains(sessionJSON, hex.EncodeToString([]byte(credentials.Token))) {
		t.Error("stored session contains the raw token")
	}
	if err := pool.QueryRow(ctx, "SELECT row_to_json(u)::text FROM users u WHERE id = $1", admin.ID).Scan(&userJSON); err != nil {
		t.Fatalf("read stored user: %v", err)
	}
	if strings.Contains(userJSON, "administrator-password") {
		t.Error("stored user contains the raw password")
	}
}
