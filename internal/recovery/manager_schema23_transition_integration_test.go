//go:build linux

package recovery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
)

// The current primary retains schema25 music and preferences while a historical
// encrypted archive replaces the other slot. Runtime reopening and rollback
// must continue to distinguish the imported and original identities and keys.
func TestRecoveryManagerSchema23EncryptedArchiveApplyRestartAndRollback(t *testing.T) {
	testRecoveryManagerHistoricalEncryptedArchiveApplyRestartAndRollback(t, 23)
}

func TestRecoveryManagerSchema24EncryptedArchiveApplyRestartAndRollback(t *testing.T) {
	testRecoveryManagerHistoricalEncryptedArchiveApplyRestartAndRollback(t, 24)
}

type historicalManagerArchiveState struct {
	schemaVersion int64
	users         string
	preferences   string
	catalog       string
}

func testRecoveryManagerHistoricalEncryptedArchiveApplyRestartAndRollback(t *testing.T, sourceVersion int64) {
	t.Helper()
	defer releaseRecoveryEngineTestMemory()
	var verificationPool *pgxpool.Pool
	t.Cleanup(func() {
		if verificationPool != nil {
			verificationPool.Close()
		}
	})
	f := newManagerIntegrationFixture(t)
	ctx := f.seed.ctx
	if err := f.runtime.BindDatabase(ctx, f.seed.configuration, f.seed.source, f.lease); err != nil {
		t.Fatal("bind the current primary before importing a historical archive")
	}
	if version, err := database.SchemaVersion(ctx, f.seed.source); err != nil || version != 25 {
		t.Fatalf("original primary schema = %d, want 25: %v", version, err)
	}
	var preferenceOwners int
	if err := f.seed.source.QueryRow(ctx, "SELECT count(*) FROM user_settings WHERE settings<>'{}'::jsonb").Scan(&preferenceOwners); err != nil || preferenceOwners != 2 {
		t.Fatal("the current primary lacks two nonempty independent preference witnesses")
	}
	originalHistory := recoveryEngineRetainedState(t, ctx, f.seed.source)
	originalPreferences := recoveryEnginePreferenceState(t, ctx, f.seed.source)
	originalMaster, err := os.ReadFile(f.seed.configuration.APIKeyMasterKeyFile)
	if err != nil || len(originalMaster) != 32 {
		t.Fatal("read the original owned master witness")
	}
	defer clear(originalMaster)

	legacy, metadata, legacyState := createHistoricalManagerArchive(t, f, sourceVersion)
	legacyMaster, err := os.ReadFile(legacy.configuration.APIKeyMasterKeyFile)
	if err != nil || len(legacyMaster) != 32 || bytes.Equal(legacyMaster, originalMaster) {
		t.Fatal("the historical archive did not retain a distinct real master")
	}
	defer clear(legacyMaster)
	reader, err := legacy.objects.Snapshot(ctx, metadata.ID)
	if err != nil {
		t.Fatal("open the published historical encrypted artifact")
	}
	var prefix [len("age-encryption.org/v1\n")]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil || string(prefix[:]) != "age-encryption.org/v1\n" {
		_ = reader.Close()
		t.Fatal("the historical fixture is not a real encrypted native archive")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		_ = reader.Close()
		t.Fatal("rewind the exact encrypted historical artifact")
	}
	imported, importErr := f.manager.Import(ctx, f.seed.actor, recoveryEngineTestID(t), reader)
	closeErr := reader.Close()
	if importErr != nil || closeErr != nil {
		t.Fatalf("import the real schema%d encrypted archive: %v", sourceVersion, importErr)
	}
	imported = f.waitOperation(t, imported.Id, "completed")
	backup, err := f.manager.Backup(ctx, f.seed.actor, imported.BackupId)
	if err != nil || backup.Verified || backup.Source != nil || backup.SHA256 != metadata.Digest {
		t.Fatal("import changed the ciphertext digest or bypassed authenticated archive verification")
	}
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	plan, err := f.manager.Plan(ctx, f.seed.actor, PlanRequest{RequestId: recoveryEngineTestID(t), BackupId: backup.Id,
		SHA256: backup.SHA256, Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: "0"})
	if err != nil {
		t.Fatalf("admit the old encrypted archive through the current native manager: %v", err)
	}
	plan = f.waitOperation(t, plan.Id, "ready")
	releaseRecoveryEngineTestMemory()
	verified, err := f.manager.Backup(ctx, f.seed.actor, backup.Id)
	if err != nil || !verified.Verified || verified.Source == nil || verified.Source.SchemaVersion != strconv.FormatInt(sourceVersion, 10) ||
		verified.Source.ServerId != metadata.Summary.ServerID || len(verified.Source.Tables) != historicalManagerTableCount(sourceVersion) {
		t.Fatal("native staging did not authenticate and retain the historical archive source identity")
	}
	var preferencesArchived bool
	for _, table := range verified.Source.Tables {
		if table.Name == "user_settings" {
			preferencesArchived = true
		}
	}
	if preferencesArchived != (sourceVersion >= 24) {
		t.Fatal("native staging changed the historical archive preference table inventory")
	}
	operation, err := f.manager.operationCopy(plan.Id)
	if err != nil || operation.Manifest == nil || operation.Manifest.Source.SchemaVersion != sourceVersion ||
		operation.Target == nil || operation.Target.Facts.SchemaVersion != 25 {
		t.Fatal("the ready manager operation conflated the historical archive schema with its migrated schema25 target")
	}
	assertHistoricalManagerTarget(t, ctx, f.seed.target, legacyState)
	assertHistoricalManagerGeneration(t, ctx, f.runtime, operation.GenerationID, f.seed.target, legacy, legacyMaster, originalMaster)
	if actual := recoveryEngineRetainedState(t, ctx, f.seed.source); actual != originalHistory {
		t.Fatal("historical archive preparation or staging changed the original primary")
	}
	if _, err := f.manager.Apply(ctx, f.seed.actor, plan.Id, ApplyRequest{Revision: plan.Revision, GenerationRevision: "0"}); err != nil {
		t.Fatal("authorize application of the verified historical archive")
	}
	candidate, err := f.manager.PrepareSwitch(ctx, plan.Id)
	if err != nil || candidate.State.Revision != 1 || candidate.State.DatabaseSlot != lifecycle.DatabaseRecovery {
		t.Fatalf("activate the migrated historical archive: %v", err)
	}
	registerTransitionCandidateCleanup(t, candidate)
	closeTransitionManager(t, f.manager)
	if err := f.lease.Close(); err != nil {
		t.Fatal("release the retained original primary lease")
	}
	verificationPool = retireTransitionSourcePool(t, f)
	f.manager = managerForTransitionCandidate(t, f, candidate)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("reconcile and validate the activated historical archive")
	}
	stopStartup := initializeTransitionApplication(t, ctx, candidate.Pool, f.manager.cfg)
	commitTransitionAcceptanceReceipt(t, f.manager, plan.Id)
	stopStartup()
	closeTransitionManager(t, f.manager)
	if err := candidate.Lease.Close(); err != nil {
		t.Fatal("close the migrated target lease before runtime reopening")
	}
	candidate.Pool.Close()
	if err := f.runtime.Close(); err != nil {
		t.Fatal("close the runtime at the historical target acceptance boundary")
	}
	f.runtime, err = Open(ctx, f.seed.configuration)
	if err != nil {
		t.Fatal("reopen the original deployment policy after historical archive activation")
	}
	if id, activated, err := f.runtime.RecoverStartupTransition(ctx); err != nil || id != plan.Id || !activated {
		t.Fatal("runtime reopening did not recover the exact historical target transition")
	}
	resumed := openCurrentTransitionCandidate(t, f, plan.Id)
	f.manager = managerForTransitionCandidate(t, f, resumed)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("reconcile the historical target after runtime reopening")
	}
	stopStartup = initializeTransitionApplication(t, ctx, resumed.Pool, f.manager.cfg)
	if err := f.manager.AcceptSwitch(ctx); err != nil {
		t.Fatalf("finish historical target acceptance after reopening: %v", err)
	}
	assertTransitionAppliedReceipt(t, ctx, resumed.Pool, plan.Id)
	assertHistoricalManagerTarget(t, ctx, resumed.Pool, legacyState)
	assertHistoricalManagerGeneration(t, ctx, f.runtime, resumed.State.GenerationID, resumed.Pool, legacy, legacyMaster, originalMaster)
	activeIdentities := identity.NewWithApplicationKeyVault(resumed.Pool, identity.NewApplicationKeyVault(f.manager.cfg.APIKeyMasterKeyFile))
	login, err := activeIdentities.Authenticate(ctx, legacy.actor.User.Name, historicalManagerAdministratorPassword(sourceVersion), identity.Client{Name: "Historical target administrator"}, "admin")
	if err != nil {
		t.Fatal("the historical administrator password did not survive encrypted restoration and runtime reopening")
	}
	actor, err := activeIdentities.Resolve(ctx, login.Token, "admin")
	if err != nil || actor.User.ID != legacy.actor.User.ID {
		t.Fatal("the accepted target selected the original primary administrator instead of the historical account")
	}
	acceptedBackup, err := f.manager.Backup(ctx, actor, backup.Id)
	if err != nil || !acceptedBackup.Verified || acceptedBackup.Source == nil || acceptedBackup.SHA256 != metadata.Digest ||
		acceptedBackup.Source.SchemaVersion != strconv.FormatInt(sourceVersion, 10) || len(acceptedBackup.Source.Tables) != historicalManagerTableCount(sourceVersion) {
		t.Fatal("runtime reopening changed the authenticated historical archive source facts")
	}
	if _, err := resumed.Pool.Exec(ctx, `INSERT INTO user_settings(user_id,settings)
		VALUES($1,'{"HistoricalTargetOnly":"discard-on-rollback"}'::jsonb)
		ON CONFLICT(user_id) DO UPDATE SET settings=user_settings.settings || EXCLUDED.settings,
		updated_at=clock_timestamp()`, actor.User.ID); err != nil {
		t.Fatal("write a new preference on the accepted historical target")
	}
	if actual := recoveryEnginePreferenceState(t, ctx, f.seed.source); actual != originalPreferences {
		t.Fatal("the accepted historical target changed original primary preferences")
	}
	status, err := f.manager.Status(ctx, actor)
	if err != nil || !status.Rollback.Available || status.GenerationRevision != "1" {
		t.Fatal("the historical target did not retain the original schema25 rollback image")
	}
	rollback, err := f.manager.Rollback(ctx, actor, RollbackRequest{RequestId: recoveryEngineTestID(t), GenerationRevision: "1"})
	if err != nil {
		t.Fatalf("authorize rollback from the historical archive to the original primary: %v", err)
	}
	stopStartup()
	returned, err := f.manager.PrepareSwitch(ctx, rollback.Id)
	if err != nil || returned.State.Revision != 2 || returned.State.DatabaseSlot != lifecycle.DatabasePrimary {
		t.Fatalf("reactivate the original primary as a fresh rollback generation: %v", err)
	}
	registerTransitionCandidateCleanup(t, returned)
	closeTransitionManager(t, f.manager)
	if err := resumed.Lease.Close(); err != nil {
		t.Fatal("release the retired historical target lease")
	}
	resumed.Pool.Close()
	f.manager = managerForTransitionCandidate(t, f, returned)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("reconcile the original primary rollback generation")
	}
	stopPrimary := initializeTransitionApplication(t, ctx, returned.Pool, f.manager.cfg)
	defer stopPrimary()
	if err := f.manager.AcceptSwitch(ctx); err != nil {
		t.Fatalf("accept rollback after the historical encrypted archive transition: %v", err)
	}
	assertTransitionAppliedReceipt(t, ctx, returned.Pool, rollback.Id)
	if version, err := database.SchemaVersion(ctx, returned.Pool); err != nil || version != 25 {
		t.Fatalf("rollback schema = %d, want original schema25: %v", version, err)
	}
	if actual := recoveryEngineRetainedState(t, ctx, returned.Pool); actual != originalHistory ||
		recoveryEnginePreferenceState(t, ctx, returned.Pool) != originalPreferences {
		t.Fatal("rollback failed to restore original users, preference ownership, values, timestamps, or retained history")
	}
	f.seed.assertMusicState(t, returned.Pool)
	assertHistoricalManagerGeneration(t, ctx, f.runtime, returned.State.GenerationID, returned.Pool, f.seed, originalMaster, legacyMaster)
	if _, err := identity.New(returned.Pool).Authenticate(ctx, f.seed.actor.User.Name, "recovery-administrator-password", identity.Client{Name: "Original rollback administrator"}, "admin"); err != nil {
		t.Fatal("rollback did not restore the original administrator password")
	}
	if retained, err := legacy.objects.Get(ctx, metadata.ID); err != nil || retained.State != backupstore.StateReady || retained.Digest != metadata.Digest ||
		retained.Summary == nil || retained.Summary.SchemaVersion != int(sourceVersion) {
		t.Fatal("native recovery or rollback changed the retained historical encrypted artifact")
	}
}

func historicalManagerTableCount(version int64) int {
	if version == 23 {
		return 29
	}
	return 30
}

func historicalManagerAdministratorPassword(version int64) string {
	return "schema" + strconv.FormatInt(version, 10) + "-administrator-password"
}

// The fixture's independently owned, initially empty secondary is used only to
// manufacture a genuine historical archive. It is cleared by the production
// catalog-checked RESTRICT path using locally captured facts before Plan runs.
func createHistoricalManagerArchive(t *testing.T, f *managerIntegrationFixture, version int64) (*engineRecoveryFixture, backupstore.Metadata, historicalManagerArchiveState) {
	t.Helper()
	if version != 23 && version != 24 {
		t.Fatal("historical manager fixtures support only schema23 and schema24")
	}
	versionText := strconv.FormatInt(version, 10)
	ctx, pool := f.seed.ctx, f.seed.target
	lease, err := database.AcquireLease(ctx, pool)
	if err != nil {
		t.Fatal("exclusively lease the empty secondary for the historical fixture")
	}
	defer lease.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal("begin the historical secondary schema")
	}
	defer rollbackRestore(tx)
	if _, err := backuppg.InspectEmptyRecoveryTransaction(ctx, tx, "public"); err != nil {
		t.Fatal("the historical archive fixture did not start from a strictly empty owned secondary")
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path=public,pg_catalog"); err != nil || database.RecoveryMigrateTo(ctx, tx, version) != nil {
		t.Fatal("apply exactly the published historical migration prefix")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit the historical schema fixture")
	}
	var preferencesExist bool
	if actualVersion, err := database.SchemaVersion(ctx, pool); err != nil || actualVersion != version ||
		pool.QueryRow(ctx, "SELECT to_regclass('public.user_settings') IS NOT NULL").Scan(&preferencesExist) != nil || preferencesExist != (version >= 24) {
		t.Fatal("the historical fixture differs from its published source schema")
	}
	var musicColumns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_attribute
		WHERE (attrelid='item_metadata_state'::regclass AND attname='music_source'
		OR attrelid='item_entities'::regclass AND attname='credit_group') AND NOT attisdropped`).Scan(&musicColumns); err != nil || musicColumns != 0 {
		t.Fatal("the historical fixture contains schema25 music columns")
	}
	root := t.TempDir()
	cfg := f.seed.configuration
	cfg.DatabaseURL, cfg.Recovery.DatabaseURL = f.seed.configuration.Recovery.DatabaseURL, f.seed.configuration.DatabaseURL
	cfg.ServerName = "Historical schema" + versionText + " archive"
	cfg.APIKeyMasterKeyFile = filepath.Join(root, "historical-master.key")
	cfg.Recovery.Directory = filepath.Join(root, "historical-lifecycle")
	cfg.Recovery.OperationsDirectory = filepath.Join(root, "historical-operations")
	cfg.Recovery.Backups.Directory = filepath.Join(root, "historical-backups")
	legacy := &engineRecoveryFixture{ctx: ctx, source: pool, schema: "public", configuration: cfg}
	legacy.objects, err = backupstore.Open(cfg.Recovery.Backups)
	if err != nil {
		t.Fatal("open a separate historical encrypted artifact store")
	}
	t.Cleanup(func() {
		if err := legacy.objects.Close(); err != nil {
			t.Error("close the historical encrypted artifact store")
		}
	})
	legacy.vault = identity.NewApplicationKeyVault(cfg.APIKeyMasterKeyFile)
	legacy.identities = identity.NewWithApplicationKeyVault(pool, legacy.vault)
	admin, err := legacy.identities.Bootstrap(ctx, "Schema"+versionText+" administrator", historicalManagerAdministratorPassword(version))
	if err != nil {
		t.Fatal("bootstrap the real historical administrator")
	}
	viewer, err := legacy.identities.CreateUser(ctx, "Schema"+versionText+" viewer", "schema"+versionText+"-viewer-password", false)
	if err != nil {
		t.Fatal("create a distinct historical viewer")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET configuration=jsonb_build_object('HistoricalUser',id,
		'AudioLanguagePreference','fra','ExactInteger',9007199254740993::bigint),management_revision=9007199254740993
		WHERE id=ANY($1::text[])`, []string{admin.ID, viewer.ID}); err != nil {
		t.Fatal("seed historical account configuration and exact revisions")
	}
	if version >= 24 {
		if _, err := pool.Exec(ctx, `INSERT INTO user_settings(user_id,settings,updated_at)
			VALUES($1,'{"Theme":"historical-dark","MaxStreamingBitrate":"3000000","ExactInteger":9007199254740993,"AdminOnly":{"Keep":[1,true,null]}}'::jsonb,'2020-02-03T00:00:00Z'),
			($2,'{"Theme":"historical-light","SubtitleMode":"Smart","MaxStreamingBitrate":"700000","ViewerOnly":"preserved"}'::jsonb,'2020-02-04T00:00:00Z')`,
			admin.ID, viewer.ID); err != nil {
			t.Fatal("seed two independent historical preference maps and exact timestamps")
		}
	}
	seedHistoricalManagerCatalog(t, ctx, pool, admin.ID)
	legacy.adminLogin, err = legacy.identities.Authenticate(ctx, admin.Name, historicalManagerAdministratorPassword(version), identity.Client{Name: "Historical native login"}, "admin")
	if err != nil {
		t.Fatal("issue the real historical administrator credential")
	}
	legacy.actor, err = legacy.identities.Resolve(ctx, legacy.adminLogin.Token, "admin")
	if err != nil {
		t.Fatal("resolve the historical administrator")
	}
	legacy.embyLogin, err = legacy.identities.Authenticate(ctx, admin.Name, historicalManagerAdministratorPassword(version),
		identity.Client{Name: "Historical player", DeviceID: "schema" + versionText + "-player", Device: "Historical device"}, "emby")
	if err != nil {
		t.Fatal("issue a real historical player credential")
	}
	serverID, err := legacy.identities.ServerID(ctx)
	if err != nil {
		t.Fatal("retain the historical server identity")
	}
	client := identity.Client{DeviceID: serverID, Device: "Historical server", Version: "schema" + versionText}
	legacy.activeKey, err = legacy.identities.CreateApplicationKey(ctx, legacy.actor, "Historical active key", "192.0.2."+versionText, client)
	if err != nil {
		t.Fatal("create a real key sealed by the independent historical master")
	}
	legacy.revokedKey, err = legacy.identities.CreateApplicationKey(ctx, legacy.actor, "Historical revoked key", "192.0.2."+versionText, client)
	if err != nil {
		t.Fatal("create real historical sealed key history")
	}
	if _, err := legacy.identities.RevokeApplicationKey(ctx, legacy.actor, legacy.revokedKey.ID); err != nil {
		t.Fatal("retain an already-revoked historical key")
	}
	legacy.engine, err = NewEngine(cfg, pool, legacy.vault, legacy.objects, "schema"+versionText+"-manager-transition-fixture")
	if err != nil {
		t.Fatal("construct the real historical encryption engine")
	}
	localFacts := recoveryEngineTestFacts(t, ctx, pool, legacy.engine.options)
	state := historicalManagerArchiveState{schemaVersion: version, preferences: "[]"}
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u").Scan(&state.users); err != nil {
		t.Fatal("capture the historical users before encryption")
	}
	if version >= 24 {
		state.preferences = recoveryEnginePreferenceState(t, ctx, pool)
	}
	state.catalog = historicalManagerCatalogState(t, ctx, pool)
	manifest, metadata := legacy.create(t)
	if manifest.Source.SchemaVersion != version || manifest.Source.DatabaseSchema != "public" ||
		len(manifest.Source.Tables) != historicalManagerTableCount(version) || len(manifest.Source.MigrationChecksums) != int(version) ||
		!reflect.DeepEqual(manifest.Source, localFacts) || metadata.Summary == nil || metadata.Summary.SchemaVersion != int(version) {
		t.Fatal("the encrypted engine output is not the locally witnessed historical archive")
	}
	var preferencesArchived bool
	for _, table := range manifest.Source.Tables {
		if table.Name == "user_settings" {
			preferencesArchived = true
		}
	}
	if preferencesArchived != (version >= 24) {
		t.Fatal("the historical encrypted archive changed its preference table inventory")
	}
	cleanup, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal("begin controlled removal of the historical fixture")
	}
	defer rollbackRestore(cleanup)
	inspection, err := backuppg.InspectRecoveryTransaction(ctx, cleanup, "public")
	if err != nil || !lease.Protects(pool) {
		t.Fatal("the historical fixture lost its catalog or exclusive database authority")
	}
	if err := inspection.LockTables(ctx); err != nil {
		t.Fatal("lock only the trusted historical fixture tables")
	}
	if err := inspection.EmptyTrustedSchema(ctx, localFacts); err != nil {
		t.Fatalf("return the exactly witnessed historical fixture to an empty secondary: %v", err)
	}
	if _, err := backuppg.InspectEmptyRecoveryTransaction(ctx, cleanup, "public"); err != nil {
		t.Fatal("controlled historical fixture removal did not leave a strictly empty target")
	}
	if err := cleanup.Commit(ctx); err != nil {
		t.Fatal("commit the empty secondary before native restore planning")
	}
	return legacy, metadata, state
}

func seedHistoricalManagerCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, editorID string) {
	t.Helper()
	// Music-shaped historical NFO fields are preserved as original metadata.
	// Their presence must not infer accepted music provenance or artist rows.
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type,created_at,last_scan_at)
		VALUES('historical-manager-library','Historical metadata library','music','2020-03-01T00:00:00Z','2020-03-02T00:00:00Z');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder,created_at,updated_at)
		VALUES('historical-manager-library','historical-manager-library','Historical metadata library',
		'historical metadata library','CollectionFolder',true,'2020-03-01T00:00:00Z','2020-03-02T00:00:00Z');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,overview,media,local_metadata,local_metadata_path,
			local_metadata_hash,created_at,updated_at)
		SELECT 'historical-manager-song','historical-manager-library','historical-manager-library',
			'Historical curated song','historical curated song','Audio','Historical overview',
			'{"Container":"flac","DurationTicks":1200000000,"OpaqueMedia":{"ExactInteger":9007199254740993}}'::jsonb,
			jsonb_build_object('Name','Historical NFO song','Genres',jsonb_build_array('Historical Genre'),
				'Tags',jsonb_build_array('Historical Tag'),'Studios',jsonb_build_array('Historical Studio'),
				'Artists',jsonb_build_array('Shared historical name'),'AlbumArtists',jsonb_build_array('Shared historical name'),
				'People',jsonb_build_array(jsonb_build_object('Name','Shared historical name','Role','Historical performer role',
					'Type',long_credit_type,'SortOrder',5,'OpaqueCredit',9007199254740993::bigint)),
				'ProviderIDs',jsonb_build_object('Legacy','historical-source-id'),
				'OpaqueNFO',jsonb_build_object('ExactInteger',9007199254740993::bigint,'NullValue',NULL,'Keep',jsonb_build_array(1,true,'same'))),
			'historical-song.nfo',repeat('a',64),'2020-03-03T00:00:00Z','2020-03-04T00:00:00Z'
		FROM (SELECT string_agg(md5(n::text),'' ORDER BY n) AS long_credit_type FROM generate_series(1,128) n) credit`); err != nil {
		t.Fatal("seed complete historical media metadata with a long legal person credit type")
	}
	if _, err := pool.Exec(ctx, `UPDATE item_metadata_state state SET
		automatic=state.automatic || '{"Name":"Historical automatic song","OpaqueAutomatic":{"ExactInteger":9007199254740993}}'::jsonb,
		source_key=state.source_key || '{"OpaqueSource":{"ExactInteger":9007199254740993,"Keep":true}}'::jsonb,
		overrides='{"Name":"Historical curated song","OpaqueOverride":[1,null,"preserved"]}'::jsonb,
		locked_values='{"Genres":["Historical Genre"],"OpaqueLock":{"Keep":true}}'::jsonb,
		effective=item.local_metadata || '{"Name":"Historical curated song","OpaqueEffective":{"ExactInteger":9007199254740993,"Keep":true}}'::jsonb,
		revision=9007199254740993,last_edited_by=$1,last_edited_at='2020-03-06T00:00:00Z',updated_at='2020-03-07T00:00:00Z'
		FROM items item WHERE item.id=state.item_id AND item.id='historical-manager-song'`, editorID); err != nil {
		t.Fatal("retain every historical metadata layer, editor, revision, and timestamp")
	}
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('historical-manager-song',
		(SELECT effective FROM item_metadata_state WHERE item_id='historical-manager-song'));
		UPDATE catalog_entities SET created_at='2020-03-05T00:00:00Z'`); err != nil {
		t.Fatal("persist historical entity identifiers and original person associations")
	}
	var metadataRows, originalCredits, longCredits int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_metadata_state),
		(SELECT count(*) FROM item_entities),
		(SELECT count(*) FROM item_entities WHERE octet_length(credit_type)=4096
			AND role='Historical performer role' AND sort_order=5)`).Scan(&metadataRows, &originalCredits, &longCredits); err != nil || metadataRows != 2 || originalCredits != 4 || longCredits != 1 {
		t.Fatal("the historical catalog lacks complete metadata and long-credit preservation witnesses")
	}
}

// Compare every old catalog field while checking the two schema25 additions
// separately. JSON is kept in PostgreSQL so exact integers remain unchanged.
func historicalManagerCatalogState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'libraries',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
		'roots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'entities',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e),
		'credits',(SELECT jsonb_agg(to_jsonb(c)-'credit_group' ORDER BY item_id,entity_id,position) FROM item_entities c),
		'metadata',(SELECT jsonb_agg(to_jsonb(m)-'music_source' ORDER BY item_id) FROM item_metadata_state m))::text`).Scan(&state); err != nil {
		t.Fatal("capture every original historical catalog and metadata field")
	}
	return state
}

func assertHistoricalManagerTarget(t *testing.T, ctx context.Context, pool *pgxpool.Pool, state historicalManagerArchiveState) {
	t.Helper()
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 25 {
		t.Fatalf("the historical archive target schema = %d, want 25: %v", version, err)
	}
	var actualUsers string
	if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM users u").Scan(&actualUsers); err != nil || actualUsers != state.users ||
		recoveryEnginePreferenceState(t, ctx, pool) != state.preferences {
		t.Fatalf("native historical restoration changed schema%d accounts or preferences", state.schemaVersion)
	}
	if actual := historicalManagerCatalogState(t, ctx, pool); actual != state.catalog {
		t.Fatal("native historical restoration changed an original metadata, catalog, or credit field")
	}
	var musicSources, creditGroups, artists int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM item_metadata_state WHERE music_source IS DISTINCT FROM '{}'::jsonb),
		(SELECT count(*) FROM item_entities WHERE credit_group IS DISTINCT FROM 0),
		(SELECT count(*) FROM catalog_entities WHERE kind='MusicArtist')`).Scan(&musicSources, &creditGroups, &artists); err != nil || musicSources != 0 || creditGroups != 0 || artists != 0 {
		t.Fatal("historical restoration inferred music provenance, artist identities, or credit groups")
	}
}

func assertHistoricalManagerGeneration(t *testing.T, ctx context.Context, runtime *Runtime, generationID string,
	pool *pgxpool.Pool, seed *engineRecoveryFixture, expectedMaster, wrongMaster []byte) {
	t.Helper()
	var active int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active); err != nil || active != 0 {
		t.Fatal("historical apply or original rollback reactivated imported credentials")
	}
	store := identity.New(pool)
	for _, credential := range []struct{ token, kind string }{{seed.adminLogin.Token, "admin"}, {seed.embyLogin.Token, "emby"}} {
		if _, err := store.Resolve(ctx, credential.token, credential.kind); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("a retained user credential remained authorized in a new generation")
		}
	}
	for _, key := range []identity.ApplicationKey{seed.activeKey, seed.revokedKey} {
		if _, err := store.ResolveEmby(ctx, key.Token); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal("a retained application credential remained authorized in a new generation")
		}
	}
	files, err := runtime.lifecycle.ReadGeneration(ctx, generationID)
	if err != nil || !bytes.Equal(files.Master, expectedMaster) {
		clear(files.Master)
		t.Fatal("the generation selected the other database image's master")
	}
	defer clear(files.Master)
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin a generation master and sealed-history witness")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = transaction.Rollback(cleanupCtx)
	}()
	witness, err := identity.ValidateApplicationKeyRecovery(ctx, transaction, files.Master)
	if err != nil || witness.SealedKeyCount != 2 || !witness.HasMasterKey {
		t.Fatal("the selected generation master does not authenticate both retained application keys")
	}
	if _, err := identity.ValidateApplicationKeyRecovery(ctx, transaction, wrongMaster); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
		t.Fatal("the other generation's master authenticated unrelated sealed key history")
	}
}
