package settings

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func configurationTestActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, native Actor, viewer bool) Actor {
	t.Helper()
	actor := native
	if viewer {
		actor.Principal.User.ID = settingsTestID(t)
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
			VALUES ($1,'Configuration Viewer','configuration viewer','')`, actor.Principal.User.ID); err != nil {
			t.Fatal(err)
		}
	}
	id := settingsTestID(t)
	digest := sha256.Sum256([]byte(id))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,expires_at)
		VALUES ($1,$2,$3,'emby',clock_timestamp()+interval '1 hour')`, id, actor.Principal.User.ID, digest[:]); err != nil {
		t.Fatal(err)
	}
	actor.Principal.Kind, actor.Principal.SessionID = "emby", id
	actor.Audience = identity.AdministratorEmby
	return actor
}

func TestConfigurationNamesKeepFourModesAndNativeValuesIndependent(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	width, bitrate := 1700, int64(12_000_000)
	first, err := store.Update(ctx, native, UpdateRequest{Revision: 1,
		Overrides: Overrides{MaxWidth: &width, MaxBitrate: &bitrate}})
	if err != nil {
		t.Fatal(err)
	}
	name, empty := "Configuration name", ""
	for _, test := range []struct {
		name      string
		mutation  ConfigurationMutation
		mode      ServerNameMode
		raw       *string
		effective string
	}{
		{"custom", ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name}, ServerNameCustom, &name, name},
		{"empty", ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &empty}, ServerNameEmpty, &empty, "settings-host-alpha"},
		{"unset", ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true}, ServerNameUnset, nil, "settings-host-alpha"},
		{"explicit-default-is-custom", ConfigurationMutation{Section: ConfigurationFull, ServerNamePresent: true, ServerName: &first.Defaults.ServerName}, ServerNameCustom, &first.Defaults.ServerName, first.Defaults.ServerName},
		{"full-omission-is-unset", ConfigurationMutation{Section: ConfigurationFull}, ServerNameUnset, nil, "settings-host-alpha"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := store.Snapshot()
			result, err := store.ApplyConfiguration(ctx, actor, test.mutation)
			if err != nil || result.Revision != before.Revision+1 || result.ServerNameMode != test.mode ||
				!equalPointer(result.Overrides.ServerName, test.raw) || result.Effective.ServerName != test.effective ||
				result.Effective.MaxWidth != width || result.Effective.MaxBitrate != bitrate {
				t.Fatalf("configuration name state was collapsed or unrelated settings changed: %v", err)
			}
			view, err := store.GetConfiguration(ctx, actor)
			if err != nil || !view.CanManage || !view.StartupWizardCompleted || !reflect.DeepEqual(view.Snapshot, result) {
				t.Fatalf("configuration view differs from committed name state: %v", err)
			}
			noop, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial})
			if err != nil || !reflect.DeepEqual(noop, result) {
				t.Fatalf("absent Partial name changed a configuration: %v", err)
			}
		})
	}
	before := store.Snapshot()
	reset, err := store.Reset(ctx, native, ResetRequest{Revision: before.Revision, Fields: []Field{FieldServerName}})
	if err != nil || reset.ServerNameMode != ServerNameDeployment || reset.Overrides.ServerName != nil ||
		reset.Effective.ServerName != reset.Defaults.ServerName || reset.Effective.MaxWidth != width {
		t.Fatalf("native name reset did not restore deployment mode alone: %v", err)
	}
}

func TestConfigurationEncodingAndNativeWidthNeverResetEachOther(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	width := 1440
	if _, err := store.Update(ctx, native, UpdateRequest{Revision: 1, Overrides: Overrides{MaxWidth: &width}}); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationEncoding, TranscodingMaxWidthPresent: true, TranscodingMaxWidth: 1280})
	if err != nil || result.Encoding.TranscodingMaxWidth != 1280 || result.Effective.MaxWidth != 1440 {
		t.Fatalf("encoding write replaced native width: %v", err)
	}
	width = 1600
	result, err = store.Update(ctx, native, UpdateRequest{Revision: result.Revision, Overrides: Overrides{MaxWidth: &width}})
	if err != nil || result.Encoding.TranscodingMaxWidth != 1280 || result.Effective.MaxWidth != 1600 {
		t.Fatalf("legacy native update reset encoding width: %v", err)
	}
	result, err = store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationFull})
	if err != nil || result.Encoding.TranscodingMaxWidth != 1280 || result.Effective.MaxWidth != 1600 || result.ServerNameMode != ServerNameUnset {
		t.Fatalf("full server update changed encoding/native limits: %v", err)
	}
	result, err = store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationEncoding})
	if err != nil || result.Encoding.TranscodingMaxWidth != 0 || result.Effective.MaxWidth != 1600 || result.ServerNameMode != ServerNameUnset {
		t.Fatalf("full encoding omission changed another section: %v", err)
	}
	encoding := Encoding{TranscodingMaxWidth: 640}
	mode := result.ServerNameMode
	result, err = store.Update(ctx, native, UpdateRequest{Revision: result.Revision, Overrides: result.Overrides, NameMode: &mode, Encoding: &encoding})
	if err != nil || result.ServerNameMode != ServerNameUnset || result.Encoding.TranscodingMaxWidth != 640 {
		t.Fatalf("explicit native update lost name/encoding mode: %v", err)
	}
	encoding.TranscodingMaxWidth = 1
	mode = ServerNameDeployment
	if store.Snapshot().ServerNameMode != ServerNameUnset || store.Snapshot().Encoding.TranscodingMaxWidth != 640 {
		t.Fatal("native optional value pointers escaped into the published snapshot")
	}
	reset, err := store.Reset(ctx, native, ResetRequest{Revision: result.Revision, Fields: []Field{FieldTranscodingMaxWidth}})
	if err != nil || reset.Encoding.TranscodingMaxWidth != 0 || reset.Effective.MaxWidth != 1600 || reset.ServerNameMode != ServerNameUnset {
		t.Fatalf("encoding reset cleared native width or configured name: %v", err)
	}
}

func TestConfigurationHostReloadChangesOnlyEffectiveAutomaticName(t *testing.T) {
	for _, mode := range []ServerNameMode{ServerNameDeployment, ServerNameCustom, ServerNameEmpty, ServerNameUnset} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, pool, owner, store, actor := settingsRepository(t)
			var name *string
			if mode == ServerNameCustom {
				name = settingsTestPointer("Fixed custom name")
			}
			if mode == ServerNameEmpty {
				name = settingsTestPointer("")
			}
			initial, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: name}, NameMode: &mode})
			if err != nil {
				t.Fatal(err)
			}
			row := settingsRowSnapshot(t, ctx, pool)
			newDefaults := settingsTestDefaults()
			newDefaults.ServerName = "Different deployment name"
			reloaded, err := New(ctx, pool, owner, newDefaults, "settings-host-beta")
			if err != nil {
				t.Fatal(err)
			}
			after := reloaded.Snapshot()
			want := "settings-host-beta"
			if mode == ServerNameDeployment {
				want = newDefaults.ServerName
			}
			if mode == ServerNameCustom {
				want = *name
			}
			if settingsRowSnapshot(t, ctx, pool) != row || after.Revision != initial.Revision ||
				!after.UpdatedAt.Equal(initial.UpdatedAt) || after.ServerNameMode != mode ||
				!equalPointer(after.Overrides.ServerName, name) || after.Effective.ServerName != want || after.HostName != "settings-host-beta" {
				t.Fatal("startup hostname/default changes rewrote configuration or collapsed its mode")
			}
		})
	}
}

func TestConfigurationInvalidBatchesReadonlyEchoAndModesAreAtomic(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	name := "Must not partly apply"
	falseValue := false
	baseline, initial := settingsRowSnapshot(t, ctx, pool), store.Snapshot()
	for _, request := range []ConfigurationMutation{
		{Section: ConfigurationFull, ServerNamePresent: true, ServerName: &name, StartupWizardCompleted: &falseValue},
		{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name, TranscodingMaxWidthPresent: true, TranscodingMaxWidth: 1280},
		{Section: ConfigurationEncoding, ServerNamePresent: true, ServerName: &name, TranscodingMaxWidthPresent: true, TranscodingMaxWidth: 1280},
		{Section: ConfigurationEncoding, StartupWizardCompleted: &falseValue},
		{Section: ConfigurationPartial, ServerName: &name},
		{Section: ConfigurationEncoding, TranscodingMaxWidth: 1280},
		{Section: ConfigurationEncoding, TranscodingMaxWidthPresent: true, TranscodingMaxWidth: -1},
		{Section: ConfigurationEncoding, TranscodingMaxWidthPresent: true, TranscodingMaxWidth: 8193},
		{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: settingsTestPointer(" \t ")},
		{Section: "unrecognized"},
	} {
		if _, err := store.ApplyConfiguration(ctx, actor, request); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("contradictory/readonly configuration request returned %v", err)
		}
	}
	for _, request := range []UpdateRequest{
		{Revision: 1, Overrides: Overrides{ServerName: &name}, NameMode: settingsTestPointer(ServerNameDeployment)},
		{Revision: 1, NameMode: settingsTestPointer(ServerNameCustom)},
		{Revision: 1, NameMode: settingsTestPointer(ServerNameEmpty)},
		{Revision: 1, Overrides: Overrides{ServerName: &name}, NameMode: settingsTestPointer(ServerNameUnset)},
		{Revision: 1, Overrides: Overrides{ServerName: settingsTestPointer("")}},
		{Revision: 1, NameMode: settingsTestPointer(ServerNameMode("unknown"))},
		{Revision: 1, Encoding: &Encoding{TranscodingMaxWidth: -1}},
	} {
		if _, err := store.Update(ctx, native, request); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid native mode combination returned %v", err)
		}
	}
	if settingsRowSnapshot(t, ctx, pool) != baseline || !reflect.DeepEqual(store.Snapshot(), initial) {
		t.Fatal("a rejected batch partly changed durable or effective configuration")
	}
	actual := true
	noop, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, StartupWizardCompleted: &actual})
	if err != nil || !reflect.DeepEqual(noop, initial) {
		t.Fatalf("matching readonly echo changed the revision: %v", err)
	}
}

func TestConfigurationViewerIsEmptyAndInvalidCredentialsNeverBecomeViewers(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	viewer := configurationTestActor(t, ctx, pool, native, true)
	// The copied principal still claims administrator status. Database role
	// and credential validity, not that snapshot, determine configuration access.
	view, err := store.GetConfiguration(ctx, viewer)
	if err != nil || !reflect.DeepEqual(view, Configuration{}) {
		t.Fatalf("viewer obtained configuration or initialization data: %v", err)
	}
	name := "Forbidden viewer change"
	if _, err := store.ApplyConfiguration(ctx, viewer, ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("viewer write returned %v", err)
	}
	for _, actor := range []Actor{{}, native} {
		if _, err := store.GetConfiguration(ctx, actor); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("invalid configuration read audience returned %v", err)
		}
		if _, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial}); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("invalid configuration write audience returned %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, viewer.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if value, err := store.GetConfiguration(ctx, viewer); !errors.Is(err, identity.ErrUnauthorized) || !reflect.DeepEqual(value, Configuration{}) {
		t.Fatalf("revoked viewer was downgraded to an allowed empty view: %v", err)
	}
}

func TestConfigurationInitializationUsesPersistedPredicateForApplicationCredentials(t *testing.T) {
	ctx, pool, _, store, native := settingsRepository(t)
	parent, client := settingsTestID(t), settingsTestID(t)
	digest := sha256.Sum256([]byte(parent))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,expires_at)
		VALUES ($1,NULL,$2,'application_key',NULL)`, parent, digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_devices(id,reported_device_id) VALUES(1,'configuration-test-server')`); err != nil {
		t.Fatal(err)
	}
	var keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO application_keys(credential_id,secret_ciphertext,created_by)
		VALUES($1,decode('01020304','hex'),$2) RETURNING id`, parent, native.Principal.User.ID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients(id,credential_id,client_name,device_id,device_name,client_version)
		VALUES($1,$2,'Configuration test','configuration-test-server','Configuration test','1')`, client, parent); err != nil {
		t.Fatal(err)
	}
	actor := Actor{Audience: identity.AdministratorEmby, Principal: identity.Principal{
		Kind: identity.ApplicationKeyKind, SessionID: parent, ClientSessionID: client, ApplicationKeyID: keyID}}
	if _, err := pool.Exec(ctx, `DELETE FROM server_settings WHERE key='setup_completed'; DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	view, err := store.GetConfiguration(ctx, actor)
	if err != nil || !view.CanManage || view.StartupWizardCompleted || view.Snapshot.Revision != 1 {
		t.Fatalf("application configuration hardcoded initialization or borrowed a deleted creator: %v", err)
	}
	initialized := false
	name := "Independent application configuration"
	if _, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name, StartupWizardCompleted: &initialized}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_settings(key,value) VALUES('setup_completed','true')`); err != nil {
		t.Fatal(err)
	}
	view, err = store.GetConfiguration(ctx, actor)
	if err != nil || !view.StartupWizardCompleted {
		t.Fatalf("setup_completed did not control the initialization predicate: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetConfiguration(ctx, actor); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked application credential retained configuration reads: %v", err)
	}
}

func TestConfigurationPatchMergesAfterNativeCommitAndNativeCASStillConflicts(t *testing.T) {
	ctx, pool, owner, _, native := settingsRepository(t)
	actor := configurationTestActor(t, ctx, pool, native, false)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var first atomic.Bool
	store, err := New(ctx, pool, settingsHookOwner{owner: owner, afterWrite: func(library.OwnedTx) error {
		if first.CompareAndSwap(false, true) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}, settingsTestDefaults(), "settings-host-alpha")
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		value Snapshot
		err   error
	}
	nativeDone, compatDone := make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		width := 960
		value, err := store.Update(ctx, native, UpdateRequest{Revision: 1, Overrides: Overrides{MaxWidth: &width}})
		nativeDone <- outcome{value, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("native write did not reach its barrier")
	}
	go func() {
		name := "Merged compatibility name"
		value, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name})
		compatDone <- outcome{value, err}
	}()
	releaseOnce.Do(func() { close(release) })
	var saved, merged outcome
	select {
	case saved = <-nativeDone:
	case <-ctx.Done():
		t.Fatal("native write did not finish")
	}
	select {
	case merged = <-compatDone:
	case <-ctx.Done():
		t.Fatal("compatibility write did not finish")
	}
	if saved.err != nil || merged.err != nil || saved.value.Revision != 2 || merged.value.Revision != 3 ||
		merged.value.Effective.MaxWidth != 960 || merged.value.Effective.ServerName != "Merged compatibility name" {
		t.Fatalf("compatibility patch lost a preceding native field: native=%v compatibility=%v", saved.err, merged.err)
	}
	if _, err := store.Update(ctx, native, UpdateRequest{Revision: 2, Overrides: saved.value.Overrides}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("compatibility write disabled native CAS: %v", err)
	}
	last := "Last explicit name wins"
	final, err := store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &last})
	if err != nil || final.Effective.ServerName != last || final.Effective.MaxWidth != 960 {
		t.Fatalf("same-field patch did not serialize honestly: %v", err)
	}
}

func TestConfigurationPostWriteFailureAndExpiredActorNeverPublish(t *testing.T) {
	for _, expire := range []bool{false, true} {
		name := "writer-failure"
		if expire {
			name = "actor-expiry"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, owner, _, native := settingsRepository(t)
			actor := configurationTestActor(t, ctx, pool, native, false)
			if expire {
				if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, actor.Principal.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("synthetic configuration write failure")
			var reached atomic.Bool
			store, err := New(ctx, pool, settingsHookOwner{owner: owner, afterWrite: func(tx library.OwnedTx) error {
				reached.Store(true)
				if !expire {
					return failure
				}
				_, err := tx.Exec(`SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
					(expires_at-clock_timestamp())))+0.03)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID)
				return err
			}}, settingsTestDefaults(), "settings-host-alpha")
			if err != nil {
				t.Fatal(err)
			}
			initial, row := store.Snapshot(), settingsRowSnapshot(t, ctx, pool)
			value := "Must roll back"
			_, err = store.ApplyConfiguration(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &value})
			want := failure
			if expire {
				want = identity.ErrUnauthorized
			}
			if !reached.Load() || !errors.Is(err, want) {
				t.Fatalf("post-write rejection returned %v", err)
			}
			if !reflect.DeepEqual(store.Snapshot(), initial) || settingsRowSnapshot(t, ctx, pool) != row {
				t.Fatal("failed compatibility patch changed persistence or published state")
			}
		})
	}
}

func TestConfigurationRejectsInvalidHostFallbackWithoutWriting(t *testing.T) {
	ctx, pool, owner, _, _ := settingsRepository(t)
	before := settingsRowSnapshot(t, ctx, pool)
	for _, host := range []string{"", " \t ", strings.Repeat("h", 129), "bad\x00host"} {
		if _, err := New(ctx, pool, owner, settingsTestDefaults(), host); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid host fallback returned %v", err)
		}
	}
	if settingsRowSnapshot(t, ctx, pool) != before {
		t.Fatal("invalid hostname initialization changed persisted settings")
	}
}
