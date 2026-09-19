//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgreSQLSelectedPhase1ArchivePreservesCredentialsAndIntroState(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedThemeSnapshotWitness(t, ctx, source)
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-local-password"), 10)
	if err != nil {
		t.Fatal("prepare a valid non-production local-password hash")
	}
	// This is an opaque storage witness, not a decryptable PIN. Identity tests
	// own authenticated-encryption and master-key compatibility assertions.
	ciphertext := append([]byte{'G', 'P', 'P', 1}, bytes.Repeat([]byte{0x91}, 32)...)
	if _, err := source.Exec(ctx, `UPDATE users SET local_password_hash=$1,
		profile_pin_ciphertext=$2,local_credentials_revision=9007199254740993,
		local_password_failures=4,local_password_blocked_until='2030-01-01T00:00:00Z',
		configuration=configuration||'{"EnableLocalPassword":true,"IntroSkipMode":"ShowButton","EnableNextEpisodeAutoPlay":false}'::jsonb
		WHERE id='backup-admin'`, string(hash), ciphertext); err != nil {
		t.Fatalf("seed encrypted/hash account storage witness: %v", err)
	}
	if _, err := source.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at,local_auth)
		VALUES('selected-phase1-local','backup-admin',decode(repeat('b6',32),'hex'),'emby',
		'2020-01-01T00:00:00Z','2030-01-01T00:00:00Z',true);
		INSERT INTO item_intro_state(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by,last_edited_at)
		VALUES('theme-owner',9007199254740995,'retained-source-revision',30000000,80000000,'Import','backup-admin','2020-01-03T00:00:00.123456Z'),
		('theme-other-owner',9007199254740997,'',NULL,NULL,'',NULL,'2020-01-04T00:00:00.234567Z')`); err != nil {
		t.Fatalf("seed local session, intro interval and reset tombstone: %v", err)
	}
	want := selectedPhase1DurableWitness(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) {
		t.Fatalf("restore current account/playback archive: %v", err)
	}
	if got := selectedPhase1DurableWitness(t, ctx, target); got != want {
		t.Fatal("restoration changed encrypted credentials, login restrictions, exact revisions or intro state")
	}
	targetOptions := options
	targetOptions.SourceURL = os.Getenv("GOBY_TEST_BACKUP_TARGET_DATABASE_URL")
	got, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(got.Tables, facts.Tables) || !reflect.DeepEqual(targetSequences, sequences) {
		t.Fatal("the round trip changed durable rows or independent sequence state")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLSelectedPhase1Schema41UpgradeHasNoInferredCredentialsOrIntros(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 41)
	seedThemeSnapshotWitness(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 41 || result.CurrentVersion != currentRecoveryVersion(t) {
		t.Fatalf("upgrade the published schema41 archive: %v", err)
	}
	var empty bool
	if err := target.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM users WHERE local_password_hash IS NOT NULL OR profile_pin_ciphertext IS NOT NULL
		 OR local_credentials_revision<>1 OR local_password_failures<>0 OR local_password_blocked_until IS NOT NULL)
		AND NOT EXISTS(SELECT 1 FROM sessions WHERE local_auth)
		AND NOT EXISTS(SELECT 1 FROM item_intro_state)`).Scan(&empty); err != nil || !empty {
		t.Fatalf("historical upgrade fabricated credentials, local grants or intro intervals: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = os.Getenv("GOBY_TEST_BACKUP_TARGET_DATABASE_URL")
	got, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, got, sequences, targetSequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func selectedPhase1DurableWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'accounts',(SELECT jsonb_agg(jsonb_build_object('id',id,'local_hash',local_password_hash,
		 'pin_ciphertext',encode(profile_pin_ciphertext,'hex'),'revision',local_credentials_revision,
		 'failures',local_password_failures,'blocked_until',local_password_blocked_until,
		 'configuration',configuration) ORDER BY id) FROM users),
		'local_sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s WHERE local_auth),
		'intros',(SELECT jsonb_agg(to_jsonb(i) ORDER BY item_id) FROM item_intro_state i))::text`).Scan(&value); err != nil {
		t.Fatalf("capture nonempty account and intro preservation witness: %v", err)
	}
	return value
}
