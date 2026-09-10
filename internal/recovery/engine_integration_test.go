//go:build linux

package recovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type engineRecoveryFixture struct {
	ctx           context.Context
	source        *pgxpool.Pool
	target        *pgxpool.Pool
	schema        string
	configuration config.Config
	targetConfig  config.Config
	objects       *backupstore.Store
	engine        *Engine
	vault         *identity.ApplicationKeyVault
	identities    *identity.Store
	actor         identity.Principal
	adminLogin    identity.Credentials
	embyLogin     identity.Credentials
	activeKey     identity.ApplicationKey
	revokedKey    identity.ApplicationKey
	libraryID     string
	playID        string
	encodingID    string
	scanID        string
	runID         string
}

// The external operator owns these two disposable databases and their roles.
// Tests create and remove only an independently owned random schema in each.
// They run serially because restore admission requires a wholly empty target.
func newEngineRecoveryFixture(t *testing.T) *engineRecoveryFixture {
	t.Helper()
	sourceURL := os.Getenv("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL")
	targetURL := os.Getenv("GOBY_TEST_BACKUP_TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("two disposable backup test databases are required")
	}
	if os.Getenv("GOBY_TEST_BACKUP_DISPOSABLE_DATABASES") != "1" {
		t.Fatal("explicit disposable database marker is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	sourceConfig, err := pgxpool.ParseConfig(sourceURL)
	if err != nil {
		t.Fatal("parse source fixture database configuration")
	}
	targetConfig, err := pgxpool.ParseConfig(targetURL)
	if err != nil {
		t.Fatal("parse target fixture database configuration")
	}
	if sourceConfig.ConnConfig.Database == targetConfig.ConnConfig.Database ||
		sourceConfig.ConnConfig.User == targetConfig.ConnConfig.User ||
		!strings.HasPrefix(sourceConfig.ConnConfig.Database, "goby_backup_") ||
		!strings.HasPrefix(targetConfig.ConnConfig.Database, "goby_backup_") {
		t.Fatal("test databases must have independent goby_backup_ names and roles")
	}
	schema := "goby_recovery_test_" + recoveryEngineTestID(t)
	fixture := &engineRecoveryFixture{ctx: ctx, schema: schema}
	for index, poolConfig := range []*pgxpool.Config{sourceConfig, targetConfig} {
		poolConfig.MaxConns = 6
		if poolConfig.ConnConfig.RuntimeParams == nil {
			poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
		}
		poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
		poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
		pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			t.Fatal("open dedicated fixture database")
		}
		t.Cleanup(pool.Close)
		var unsafe bool
		if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls
			FROM pg_catalog.pg_roles WHERE rolname=current_user`).Scan(&unsafe); err != nil || unsafe {
			t.Fatal("fixture requires an explicitly unprivileged database role")
		}
		if _, err := pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
			t.Fatal("create independently owned fixture schema")
		}
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := pool.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
				t.Error("remove only the independently owned fixture schema")
			}
		})
		if index == 0 {
			fixture.source = pool
		} else {
			fixture.target = pool
		}
	}
	if err := database.Migrate(ctx, fixture.source); err != nil {
		t.Fatal("apply compiled source migrations")
	}
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	if err := os.Mkdir(mediaRoot, 0700); err != nil {
		t.Fatal("create approved fixture media root")
	}
	objectConfig := backupstore.Config{
		Directory: filepath.Join(root, "backups"), MaxObjectBytes: 8 << 20,
		MaxTotalBytes: 64 << 20, MaxObjects: 8, MinFreeBytes: 1 << 20,
	}
	fixture.configuration = config.Config{
		DatabaseURL: sourceURL, ServerName: "Source deployment default",
		APIKeyMasterKeyFile: filepath.Join(root, "application-master.key"),
		MediaRoots:          []string{mediaRoot}, FFprobePath: "/unused-recovery-test-ffprobe", FFmpegPath: "/unused-recovery-test-ffmpeg",
		Transcoding: config.TranscodingConfig{MaxBitrate: 8_000_000, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 2},
		Recovery: config.RecoveryConfig{
			Directory: filepath.Join(root, "recovery"), Backups: objectConfig, DatabaseURL: targetURL,
			PGDumpPath: os.Getenv("GOBY_TEST_PG_DUMP"), PGRestorePath: os.Getenv("GOBY_TEST_PG_RESTORE"),
			OperationTimeout: 3 * time.Minute,
		},
	}
	if fixture.configuration.Recovery.PGDumpPath == "" || fixture.configuration.Recovery.PGRestorePath == "" {
		t.Fatal("explicit PostgreSQL 17 executable paths are required")
	}
	fixture.targetConfig = fixture.configuration
	fixture.targetConfig.DatabaseURL = targetURL
	fixture.targetConfig.Recovery.DatabaseURL = sourceURL
	fixture.targetConfig.ServerName = "Target operator default"
	fixture.targetConfig.APIKeyMasterKeyFile = filepath.Join(root, "unavailable-old-target-master.key")
	fixture.objects, err = backupstore.Open(objectConfig)
	if err != nil {
		t.Fatal("open owned encrypted backup store")
	}
	t.Cleanup(func() {
		if err := fixture.objects.Close(); err != nil {
			t.Error("close owned encrypted backup store")
		}
	})
	fixture.vault = identity.NewApplicationKeyVault(fixture.configuration.APIKeyMasterKeyFile)
	fixture.identities = identity.NewWithApplicationKeyVault(fixture.source, fixture.vault)
	fixture.seed(t)
	fixture.engine, err = NewEngine(fixture.configuration, fixture.source, fixture.vault, fixture.objects, "recovery-integration")
	if err != nil {
		t.Fatal("create source backup engine")
	}
	fixture.engine.options.Schema = schema
	return fixture
}

func (f *engineRecoveryFixture) seed(t *testing.T) {
	t.Helper()
	ctx := f.ctx
	admin, err := f.identities.Bootstrap(ctx, "Recovery administrator", "recovery-administrator-password")
	if err != nil {
		t.Fatal("bootstrap source administrator")
	}
	f.adminLogin, err = f.identities.Authenticate(ctx, admin.Name, "recovery-administrator-password", identity.Client{Name: "Native admin"}, "admin")
	if err != nil {
		t.Fatal("issue native source login")
	}
	f.actor, err = f.identities.Resolve(ctx, f.adminLogin.Token, "admin")
	if err != nil {
		t.Fatal("resolve source administrator")
	}
	f.embyLogin, err = f.identities.Authenticate(ctx, admin.Name, "recovery-administrator-password",
		identity.Client{Name: "Player", DeviceID: "retained-player-device", Device: "Player device", Version: "1"}, "emby")
	if err != nil {
		t.Fatal("issue source player login")
	}
	serverID, err := f.identities.ServerID(ctx)
	if err != nil {
		t.Fatal("create persistent source server identity")
	}
	client := identity.Client{DeviceID: serverID, Device: "Source server", Version: "1"}
	f.activeKey, err = f.identities.CreateApplicationKey(ctx, f.actor, "Active recovery key", "192.0.2.44", client)
	if err != nil {
		t.Fatal("issue active source application key")
	}
	f.revokedKey, err = f.identities.CreateApplicationKey(ctx, f.actor, "Revoked recovery key", "192.0.2.44", client)
	if err != nil {
		t.Fatal("issue source application key history")
	}
	if _, err := f.identities.RevokeApplicationKey(ctx, f.actor, f.revokedKey.ID); err != nil {
		t.Fatal("revoke source application key history")
	}
	mediaDirectory := filepath.Join(f.configuration.MediaRoots[0], "library")
	if err := os.Mkdir(mediaDirectory, 0700); err != nil {
		t.Fatal("create registered fixture media directory")
	}
	catalog, err := library.New(f.source, media.Prober{
		FFprobePath: f.configuration.FFprobePath, FFmpegPath: f.configuration.FFmpegPath, Timeout: time.Second,
	}, f.configuration.MediaRoots)
	if err != nil {
		t.Fatal("open source catalog owner")
	}
	closeCatalog := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(closeCtx); err != nil {
			t.Error("close source catalog owner")
		}
	}
	t.Cleanup(closeCatalog)
	registered, err := catalog.CreateLibraryAsAdministrator(ctx, f.actor, identity.AdministratorNative,
		"Retained library", "movies", []string{mediaDirectory})
	if err != nil {
		t.Fatal("register source library through its owner")
	}
	f.libraryID = registered.ID
	taskStore, err := tasks.New(f.source, catalog)
	if err != nil || taskStore.Reconcile(ctx) != nil {
		t.Fatal("register source task definitions")
	}
	definition, err := taskStore.GetByKey(ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatal("read source task definition")
	}
	taskActor := tasks.Actor{Principal: f.actor, Audience: identity.AdministratorNative}
	definition, err = taskStore.ReplaceTriggers(ctx, taskActor, tasks.ReplaceTriggersRequest{
		TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC",
		Triggers: []tasks.ScheduleRule{{Kind: tasks.ScheduleStartup}},
	})
	if err != nil {
		t.Fatal("register retained startup trigger")
	}
	admission, err := taskStore.Start(ctx, taskActor, tasks.StartRequest{TaskID: definition.ID, RequestID: "recovery-pending-run"})
	if err != nil || !admission.Admitted {
		t.Fatal("admit source task without starting a manager")
	}
	f.runID = admission.Run.ID
	closeCatalog()
	// These minimal rows represent work abandoned by a previous process. No
	// scanner, media probe, playback controller, or encoder is run to seed it.
	f.playID, f.encodingID, f.scanID = recoveryEngineTestID(t), recoveryEngineTestID(t), recoveryEngineTestID(t)
	itemID := recoveryEngineTestID(t)
	if _, err := f.source.Exec(ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
		VALUES($1,$2,$2,'Recovery fixture movie','recovery fixture movie','Movie')`, itemID, f.libraryID); err != nil {
		t.Fatal("seed retained media item fixture")
	}
	if _, err := f.source.Exec(ctx, `INSERT INTO play_sessions
		(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
		VALUES($1,$2,$3,'retained-player-device',$4,'recovery-source','Playing',600000000,clock_timestamp()+interval '1 hour')`,
		f.playID, admin.ID, f.embyLogin.SessionID, itemID); err != nil {
		t.Fatal("seed abandoned playback fixture")
	}
	if _, err := f.source.Exec(ctx, `INSERT INTO encoding_jobs
		(id,user_id,auth_session_id,device_id,play_session_id,item_id,media_source_id,source_stamp,plan,state,created_at,updated_at,last_access_at)
		VALUES($1,$2,$3,'retained-player-device',$4,$5,'recovery-source','fixture-stamp','{}','running',now(),now(),now())`,
		f.encodingID, admin.ID, f.embyLogin.SessionID, f.playID, itemID); err != nil {
		t.Fatal("seed abandoned encoding fixture")
	}
	if _, err := f.source.Exec(ctx, `INSERT INTO scan_jobs(id,library_id,status,started_at)
		VALUES($1,$2,'Running',clock_timestamp())`, f.scanID, f.libraryID); err != nil {
		t.Fatal("seed abandoned scan fixture")
	}
	if _, err := f.source.Exec(ctx, `UPDATE managed_settings SET revision=9007199254740993,
		server_name='Retained managed server name',server_name_mode='custom'`); err != nil {
		t.Fatal("seed exact managed settings fixture")
	}
}

func TestEngineBackupRestoreRoundTrip(t *testing.T) {
	for _, offline := range []bool{false, true} {
		name := "online"
		if offline {
			name = "offline_unreachable_original"
		}
		t.Run(name, func(t *testing.T) {
			defer releaseRecoveryEngineTestMemory()
			f := newEngineRecoveryFixture(t)
			beforeFacts := recoveryEngineTestFacts(t, f.ctx, f.source, f.engine.options)
			preserved := recoveryEngineRetainedState(t, f.ctx, f.source)
			manifest, metadata := f.create(t)
			if !reflect.DeepEqual(manifest.Source, beforeFacts) {
				t.Fatal("encrypted archive did not describe the original source snapshot")
			}
			reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
			if err != nil {
				t.Fatal("open completed encrypted backup")
			}
			defer reader.Close()
			recoveryEngine := f.engine
			if offline {
				unavailable := f.configuration
				original, err := url.Parse(unavailable.DatabaseURL)
				if err != nil || original.Host == "" {
					t.Fatal("parse explicit offline source identity")
				}
				original.Host = "127.0.0.1:1"
				unavailable.DatabaseURL = original.String()
				recoveryEngine, err = NewOfflineEngine(unavailable, f.objects, "recovery-integration")
				if err != nil || recoveryEngine.pool != nil || recoveryEngine.vault != nil {
					t.Fatal("construct offline recovery without the original server")
				}
				recoveryEngine.options.Schema = f.schema
			}
			passphrase := []byte("recovery-integration-passphrase")
			defer clear(passphrase)
			archive, err := recoveryEngine.OpenArchive(f.ctx, reader, passphrase)
			if err != nil {
				t.Fatalf("decrypt generated archive: %v", err)
			}
			defer archive.Close()
			releaseRecoveryEngineTestMemory()
			if archive.Context().Err() != nil {
				t.Fatal("archive context was cancelled when extraction returned")
			}
			databaseFile := archive.Database()
			if info, err := databaseFile.Stat(); err != nil || info.Size() < 1 {
				t.Fatal("decrypted database closed when extraction returned")
			}
			if _, err := recoveryEngine.OpenArchive(f.ctx, reader, passphrase); !errors.Is(err, ErrBusy) {
				t.Fatal("open archive did not retain the engine operation slot")
			}
			lease, err := database.AcquireLease(f.ctx, f.target)
			if err != nil {
				t.Fatal("acquire exclusive target deployment lease")
			}
			defer lease.Close()
			result, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
			if err != nil {
				t.Fatalf("restore real archive into exclusively owned target: %v", err)
			}
			if result.SourceVersion != manifest.Source.SchemaVersion || result.CurrentVersion != manifest.Source.SchemaVersion ||
				result.RevokedCredentials != 3 || result.ExpiredPlayback != 1 || result.InterruptedEncodings != 1 ||
				result.InterruptedScans != 1 || result.InterruptedTasks != 1 || result.RegisteredRoots != 1 || result.Administrators != 1 {
				t.Fatal("restore did not report the complete credential and runtime normalization")
			}
			if after := recoveryEngineTestFacts(t, f.ctx, f.source, f.engine.options); !reflect.DeepEqual(after, beforeFacts) {
				t.Fatal("backup or recovery changed original source data")
			}
			if after := recoveryEngineRetainedState(t, f.ctx, f.target); after != preserved {
				t.Fatal("restore changed retained users, policies, device, key, catalog, or schedule history")
			}
			f.assertRecoveredState(t, archive)
			if _, err := databaseFile.Seek(0, io.SeekStart); err != nil {
				t.Fatal("restore closed the caller-owned decrypted archive")
			}
			var prefix [5]byte
			if _, err := io.ReadFull(databaseFile, prefix[:]); err != nil || string(prefix[:]) != "PGDMP" {
				t.Fatal("decrypted dump was not retained until archive close")
			}
			masterAlias := archive.Master
			if err := archive.Close(); err != nil {
				t.Fatal("close authenticated archive")
			}
			if len(masterAlias) != 32 || !bytes.Equal(masterAlias, make([]byte, 32)) {
				t.Fatal("archive close did not erase its caller-visible master buffer")
			}
			if _, err := databaseFile.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("archive close did not close the anonymous plaintext dump")
			}
			if current, err := f.objects.Get(f.ctx, metadata.ID); err != nil || current.State != backupstore.StateReady || current.Digest != metadata.Digest {
				t.Fatal("archive close removed or changed the encrypted backup object")
			}
		})
	}
}

func TestEngineRestoreRejectsWrongMasterBeforeCredentialRevocation(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	beforeFacts := recoveryEngineTestFacts(t, f.ctx, f.source, f.engine.options)
	_, metadata := f.create(t)
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open completed encrypted backup")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatal("decrypt generated archive")
	}
	defer archive.Close()
	releaseRecoveryEngineTestMemory()
	archive.Master[0] ^= 0xff
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("acquire exclusive target deployment lease")
	}
	defer lease.Close()
	if _, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatalf("recovery accepted an unrelated extracted master: %v", err)
	}
	var objects int
	if err := f.target.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM pg_catalog.pg_class WHERE relnamespace=current_schema()::regnamespace)+
		(SELECT count(*) FROM pg_catalog.pg_proc WHERE pronamespace=current_schema()::regnamespace)+
		(SELECT count(*) FROM pg_catalog.pg_type WHERE typnamespace=current_schema()::regnamespace)`).Scan(&objects); err != nil {
		t.Fatal("inspect target after rejected master validation")
	}
	if objects != 0 {
		t.Fatal("master validation failure committed part of the restored schema or data")
	}
	if after := recoveryEngineTestFacts(t, f.ctx, f.source, f.engine.options); !reflect.DeepEqual(after, beforeFacts) {
		t.Fatal("rejected restore changed original source data")
	}
}

func (f *engineRecoveryFixture) create(t *testing.T) (backupformat.Manifest, backupstore.Metadata) {
	t.Helper()
	writer, err := f.objects.Begin(f.ctx, backupstore.BeginOptions{
		Kind: backupstore.KindGenerated, CreatorID: f.actor.User.ID, SessionID: f.actor.SessionID,
	})
	if err != nil {
		t.Fatal("begin unpublished encrypted backup")
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = writer.Abort(cleanupCtx, backupstore.CodeCancelled)
	})
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	manifest, err := f.engine.Create(f.ctx, writer, passphrase)
	if err != nil {
		t.Fatalf("create real encrypted PostgreSQL backup: %v", err)
	}
	releaseRecoveryEngineTestMemory()
	proof, err := writer.Prepare(f.ctx)
	if err != nil {
		t.Fatal("durably prepare encrypted backup")
	}
	summary := Summary(manifest)
	metadata, err := writer.Publish(f.ctx, proof, &summary)
	if err != nil || metadata.State != backupstore.StateReady || !metadata.Verified {
		t.Fatal("publish completed encrypted backup")
	}
	return manifest, metadata
}

func (f *engineRecoveryFixture) assertRecoveredState(t *testing.T, archive *Archive) {
	t.Helper()
	tx, err := f.target.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin recovered vault witness")
	}
	witness, witnessErr := identity.ValidateApplicationKeyRecovery(f.ctx, tx, archive.Master)
	_ = tx.Rollback(f.ctx)
	if witnessErr != nil || witness.SealedKeyCount != 2 || !witness.HasMasterKey {
		t.Fatal("restored revoked key history no longer matches the extracted master")
	}
	var unrevoked, activePlays, activeEncodings, activeScans, activeRuns, activeChildren, runCount, occurrenceCount int
	if err := f.target.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		(SELECT count(*) FROM play_sessions WHERE state IN ('Prepared','Playing','Paused')),
		(SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running')),
		(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
		(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
		(SELECT count(*) FROM task_run_children WHERE state IN ('waiting','queued','running')),
		(SELECT count(*) FROM task_runs), (SELECT count(*) FROM task_occurrences)`).
		Scan(&unrevoked, &activePlays, &activeEncodings, &activeScans, &activeRuns, &activeChildren, &runCount, &occurrenceCount); err != nil {
		t.Fatal("read recovered credential and runtime state")
	}
	if unrevoked != 0 || activePlays != 0 || activeEncodings != 0 || activeScans != 0 || activeRuns != 0 || activeChildren != 0 || runCount != 1 || occurrenceCount != 0 {
		t.Fatal("restore left active work or executed a retained schedule")
	}
	var playState, encodingState, encodingError, scanState, runState string
	if err := f.target.QueryRow(f.ctx, `SELECT
		(SELECT state FROM play_sessions WHERE id=$1),
		(SELECT state FROM encoding_jobs WHERE id=$2),
		(SELECT error_code FROM encoding_jobs WHERE id=$2),
		(SELECT status FROM scan_jobs WHERE id=$3),
		(SELECT state FROM task_runs WHERE id=$4)`, f.playID, f.encodingID, f.scanID, f.runID).
		Scan(&playState, &encodingState, &encodingError, &scanState, &runState); err != nil ||
		playState != "Expired" || encodingState != "interrupted" || encodingError != "backup_restored" || scanState != "Interrupted" || runState != "interrupted" {
		t.Fatal("restored work lacks its required terminal status")
	}
	targetIdentities := identity.New(f.target)
	for _, login := range []struct {
		token string
		kind  string
	}{{f.adminLogin.Token, "admin"}, {f.embyLogin.Token, "emby"}} {
		if _, err := targetIdentities.Resolve(f.ctx, login.token, login.kind); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("an imported user credential remains authorized")
		}
	}
	for _, key := range []identity.ApplicationKey{f.activeKey, f.revokedKey} {
		if _, err := targetIdentities.ResolveEmby(f.ctx, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("an imported application credential remains authorized")
		}
	}
	if _, err := targetIdentities.Authenticate(f.ctx, f.actor.User.Name, "recovery-administrator-password", identity.Client{Name: "New target login"}, "admin"); err != nil {
		t.Fatal("restored administrator password cannot issue a new login")
	}
	expectedDefaults, err := config.BackupDefaultsFromConfig(f.configuration)
	if err != nil || archive.Defaults != expectedDefaults {
		t.Fatal("archive did not retain allowlisted deployment defaults")
	}
	applied, err := archive.Defaults.Apply(f.targetConfig)
	if err != nil || applied.DatabaseURL != f.targetConfig.DatabaseURL || applied.APIKeyMasterKeyFile != f.targetConfig.APIKeyMasterKeyFile ||
		!reflect.DeepEqual(applied.MediaRoots, f.targetConfig.MediaRoots) || applied.ServerName != f.configuration.ServerName ||
		applied.Transcoding.MaxBitrate != f.configuration.Transcoding.MaxBitrate {
		t.Fatal("applying recovered defaults changed target-owned deployment identity")
	}
}

func recoveryEngineTestFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, options backuppg.Options) backupformat.SourceFacts {
	t.Helper()
	snapshot, err := backuppg.OpenSnapshot(ctx, pool, options)
	if err != nil {
		t.Fatalf("open source fingerprint snapshot: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		t.Fatal("read source snapshot fingerprints")
	}
	return facts
}

func recoveryEngineRetainedState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'sessions',(SELECT jsonb_agg(to_jsonb(s)-'revoked_at' ORDER BY id) FROM sessions s),
		'keys',(SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k),
		'clients',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c),
		'devices',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM devices d),
		'key_devices',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM application_key_devices d),
		'libraries',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
		'roots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'definitions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM task_definitions d),
		'triggers',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM task_triggers t),
		'settings',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM managed_settings s))::text`).Scan(&state); err != nil {
		t.Fatal("read retained identity and catalog history")
	}
	return state
}

func recoveryEngineTestID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal("generate isolated recovery fixture identifier")
	}
	return hex.EncodeToString(value[:])
}

// Production scrypt work factor remains unchanged. Explicit collection keeps
// sequential archive operations inside the remote test cgroup's memory budget.
func releaseRecoveryEngineTestMemory() {
	runtime.GC()
	debug.FreeOSMemory()
}
