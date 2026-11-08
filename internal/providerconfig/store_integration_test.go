//go:build linux

package providerconfig

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

func providerConfigurationFixture(t *testing.T) (context.Context, *pgxpool.Pool, *identity.Store, *Store, identity.Principal, string) {
	t.Helper()
	connectionURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if connectionURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for provider configuration integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, connectionURL)
	if err != nil {
		t.Fatal("create provider configuration integration connection")
	}
	t.Cleanup(admin.Close)
	identifier := func() string {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(raw[:])
	}
	schemaName := "goby_provider_cfg_" + identifier()
	schema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, finish := context.WithTimeout(context.Background(), 15*time.Second)
		defer finish()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("remove owned provider configuration schema")
		}
	})
	options, err := pgxpool.ParseConfig(connectionURL)
	if err != nil {
		t.Fatal("parse provider configuration integration connection")
	}
	options.ConnConfig.RuntimeParams["search_path"] = schemaName
	options.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, options)
	if err != nil {
		t.Fatal("create isolated provider configuration pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	userID, sessionID := identifier(), identifier()
	digest := sha256.Sum256([]byte(sessionID))
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,normalized_name,password_hash,is_administrator)
		VALUES($1,'Provider administrator','provider administrator','',true)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,expires_at)
		VALUES($1,$2,$3,'admin',clock_timestamp()+interval '1 hour')`, sessionID, userID, digest[:]); err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(t.TempDir(), "provider-master.key")
	users := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	defaults := config.OnlineProvidersConfig{Enabled: true, TMDBToken: "deployment-provider-token", MusicBrainzUserAgent: "Goby integration (contact@example.test)"}
	store, err := New(ctx, pool, users, defaults)
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.Principal{Kind: "admin", SessionID: sessionID, User: identity.User{ID: userID, IsAdministrator: true}}
	return ctx, pool, users, store, actor, masterPath
}

func TestProviderConfigurationEncryptsPublishesRestartsAndUsesCAS(t *testing.T) {
	ctx, pool, _, store, actor, masterPath := providerConfigurationFixture(t)
	initial, err := store.Get(ctx, actor)
	if err != nil || initial.Revision != "1" {
		t.Fatal("initial provider configuration was not readable")
	}
	replacement := "replacement-provider-private-token"
	updated, err := store.Update(ctx, actor, "tmdb", Update{Revision: initial.Revision, Fields: map[string]Change{"Token": {Mode: "replace", Value: &replacement}}})
	if err != nil || updated.Revision != "2" || store.Effective().TMDBToken != replacement {
		t.Fatal("saved credential was not published to new operations")
	}
	var sealed []byte
	if err := pool.QueryRow(ctx, `SELECT configuration_ciphertext FROM provider_configuration WHERE id=1`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if len(sealed) == 0 || bytes.Contains(sealed, []byte(replacement)) {
		t.Fatal("provider credentials were persisted in plaintext")
	}
	encoded, err := json.Marshal(updated)
	if err != nil || strings.Contains(string(encoded), replacement) {
		t.Fatal("provider credentials escaped in the administrator projection")
	}
	_, err = store.Update(ctx, actor, "tmdb", Update{Revision: initial.Revision, Fields: map[string]Change{"Token": {Mode: "clear"}}})
	if !errors.Is(err, ErrConflict) || store.Effective().TMDBToken != replacement {
		t.Fatal("a stale provider configuration changed the runtime credential")
	}
	restartedUsers := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	restarted, err := New(ctx, pool, restartedUsers, store.defaults)
	if err != nil || restarted.Effective().TMDBToken != replacement {
		t.Fatal("provider override did not survive a fresh vault and runtime")
	}
	cleared, err := restarted.Update(ctx, actor, "tmdb", Update{Revision: "2", Fields: map[string]Change{"Token": {Mode: "clear"}}})
	if err != nil || cleared.Revision != "3" || restarted.Effective().TMDBToken != "" {
		t.Fatal("an explicit clear inherited the deployment credential")
	}
	restored, err := restarted.Update(ctx, actor, "tmdb", Update{Revision: "3", Fields: map[string]Change{"Token": {Mode: "default"}}})
	if err != nil || restored.Revision != "4" || restarted.Effective().TMDBToken != store.defaults.TMDBToken {
		t.Fatal("the deployment credential was not restored")
	}
	if err := pool.QueryRow(ctx, `SELECT configuration_ciphertext FROM provider_configuration WHERE id=1`).Scan(&sealed); err != nil || sealed != nil {
		t.Fatal("restoring all defaults retained an unnecessary credential document")
	}
}

func TestProviderConfigurationRevalidatesAuthorityAndRevisionForTests(t *testing.T) {
	ctx, pool, _, store, actor, _ := providerConfigurationFixture(t)
	if _, err := store.Capture(ctx, actor, "9007199254740993"); !errors.Is(err, ErrConflict) {
		t.Fatal("a connection test accepted an untested saved revision")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	replacement := "new-private-token"
	if _, err := store.Update(ctx, actor, "tmdb", Update{Revision: "1", Fields: map[string]Change{"Token": {Mode: "replace", Value: &replacement}}}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked administrator changed provider credentials")
	}
	if _, err := store.Capture(ctx, actor, "1"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked administrator captured credentials for an external connection test")
	}
	if store.Effective().TMDBToken != store.defaults.TMDBToken {
		t.Fatal("a denied mutation changed the published credential")
	}
}

func TestProviderConfigurationParticipatesInTheSharedVaultRecoveryWitness(t *testing.T) {
	ctx, pool, _, store, actor, masterPath := providerConfigurationFixture(t)
	replacement := "provider-witness-private-token"
	if _, err := store.Update(ctx, actor, "tmdb", Update{Revision: "1", Fields: map[string]Change{"Token": {Mode: "replace", Value: &replacement}}}); err != nil {
		t.Fatal(err)
	}
	masterKey, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal("read owned provider test master")
	}
	defer clear(masterKey)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := identity.ValidateApplicationKeyRecovery(ctx, tx, masterKey)
	_ = tx.Rollback(ctx)
	if err != nil || !witness.HasMasterKey {
		t.Fatal("the provider ciphertext did not validate with its archived master")
	}
	if _, err := pool.Exec(ctx, `UPDATE provider_configuration SET configuration_ciphertext=set_byte(configuration_ciphertext,octet_length(configuration_ciphertext)-1,get_byte(configuration_ciphertext,octet_length(configuration_ciphertext)-1)#1) WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = identity.ValidateApplicationKeyRecovery(ctx, tx, masterKey)
	_ = tx.Rollback(ctx)
	if !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("a corrupt provider document passed the archive recovery witness")
	}
}

func TestProviderOnlyCiphertextPreventsCreatingAnotherMaster(t *testing.T) {
	ctx, pool, _, store, actor, masterPath := providerConfigurationFixture(t)
	replacement := "provider-only-private-token"
	if _, err := store.Update(ctx, actor, "tmdb", Update{Revision: "1", Fields: map[string]Change{"Token": {Mode: "replace", Value: &replacement}}}); err != nil {
		t.Fatal(err)
	}
	retainedPath := masterPath + ".retained"
	if err := os.Rename(masterPath, retainedPath); err != nil {
		t.Fatal("retain the owned fixture master before simulating loss")
	}
	t.Cleanup(func() {
		if _, err := os.Stat(masterPath); errors.Is(err, os.ErrNotExist) {
			_ = os.Rename(retainedPath, masterPath)
		}
	})
	restarted := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := identity.LockNotificationMutation(ctx, tx, actor, true); err != nil {
		t.Fatal(err)
	}
	_, err = restarted.SealNotificationSecret(ctx, tx, identity.NotificationReceiverPurpose, identity.NotificationSecretBinding("receiver", "1"), "notification-fixture-secret")
	if !errors.Is(err, identity.ErrApplicationKeyVaultMissing) {
		t.Fatal("provider-only ciphertext allowed the shared master to be replaced")
	}
	if _, err := os.Stat(masterPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a replacement master was created beside retained provider ciphertext")
	}
}
