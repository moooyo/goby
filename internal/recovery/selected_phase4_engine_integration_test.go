//go:build linux

package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/notificationjournal"
	"github.com/moooyo/goby/internal/notifications"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

const selectedPhase4RawSQL = `SELECT jsonb_build_object(
	'settings',(SELECT to_jsonb(s) FROM managed_settings s WHERE id=1),
	'transport',(SELECT to_jsonb(t) FROM notification_transport t WHERE id=1),
	'journal',(SELECT to_jsonb(j) FROM notification_journal_state j WHERE id=1),
	'registrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM notification_registrations r),
	'sources',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM notification_source_events e),
	'deliveries',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM notification_deliveries d))::text`

func selectedPhase4RetainedStateSQL(normalizeSource bool) string {
	delivery := "to_jsonb(d)"
	if normalizeSource {
		// Predict the allowed cleanup only in the source expectation. The
		// restored witness below reads actual refs unchanged, so unexpected
		// retained or rewritten delivery references still fail exact equality.
		delivery = `CASE WHEN d.state IN ('pending','sending')
			THEN jsonb_set(to_jsonb(d),'{refs}','[]'::jsonb) ELSE to_jsonb(d) END`
	}
	return `SELECT jsonb_build_object(
	'settings',(SELECT to_jsonb(s)-ARRAY['runtime_overrides','revision','updated_at'] FROM managed_settings s WHERE id=1),
	'transport',(SELECT to_jsonb(t)-ARRAY['enabled','revision'] FROM notification_transport t WHERE id=1),
	'journal',(SELECT to_jsonb(j) FROM notification_journal_state j WHERE id=1),
	'registrations',(SELECT jsonb_agg(to_jsonb(r)-ARRAY['enabled','revision','last_outcome','updated_at'] ORDER BY id) FROM notification_registrations r),
	'sources',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM notification_source_events e),
	'deliveries',(SELECT jsonb_agg(CASE WHEN id=repeat('c',32) THEN to_jsonb(d)
		ELSE (` + delivery + `)-ARRAY['state','lease_id','lease_until','outcome','updated_at'] END ORDER BY id) FROM notification_deliveries d))::text`
}

func phase4Pointer[T any](value T) *T { return &value }

func seedSelectedPhase4EngineState(t *testing.T, f *engineRecoveryFixture) settings.RuntimeOverrides {
	t.Helper()
	source := settings.RuntimeOverrides{Network: &settings.NetworkOverrides{BindHost: phase4Pointer("192.0.2.99"), HttpPort: phase4Pointer(9196)},
		Hardware: &settings.HardwareSelection{Decode: "vaapi", Encode: "vaapi", DeviceID: "source-host-amd"}, Threads: phase4Pointer(32),
		H264: &transcode.CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 20}, HEVC: &transcode.CPUQuality{Preset: "medium", RateControl: "bitrate", CRF: 30},
		SoftwareToneMapping: phase4Pointer(false), VulkanToneMapping: phase4Pointer(true)}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Exec(f.ctx, `UPDATE managed_settings SET runtime_overrides=$1,revision=9007199254740993`, raw); err != nil {
		t.Fatal("seed source host choices and portable execution preferences")
	}
	store := notifications.NewStore(f.source, f.identities, nil)
	receiver := "synthetic-receiver-credential-phase4"
	if _, err := store.UpdateConfig(f.ctx, f.actor, notifications.ConfigUpdate{Revision: "1", Enabled: true, Endpoint: "https://receiver.example.invalid/goby", AllowedNetworks: []string{"192.0.2.0/24"}, ReceiverCredential: &receiver}); err != nil {
		t.Fatalf("seal the real source receiver credential: %v", err)
	}
	actor, err := f.identities.Resolve(f.ctx, f.embyLogin.Token, "emby")
	if err != nil {
		t.Fatal("resolve the ordinary source registration owner")
	}
	token := "synthetic-personal-target-token-phase4"
	registration, err := store.PutRegistration(f.ctx, actor, notifications.RegistrationUpdate{Revision: "0", Transport: notifications.Transport, TargetToken: &token, EventIds: []string{"CatalogInvalidated", "UserDataInvalidated"}})
	if err != nil {
		t.Fatalf("seal a registration bound to its real session: %v", err)
	}
	refs, err := json.Marshal([]notificationjournal.Reference{{Kind: "Item", ID: f.musicItemID, LibraryID: f.libraryID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Exec(f.ctx, `UPDATE notification_journal_state SET sequence=9007199254740994 WHERE id=1`); err != nil {
		t.Fatal("seed exact source sequence")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO notification_source_events(id,sequence,kind,refs)
		VALUES(repeat('a',32),9007199254740993,'CatalogInvalidated',$1),(repeat('b',32),9007199254740994,'CatalogInvalidated',$1)`, refs); err != nil {
		t.Fatal("seed durable authorized source evidence")
	}
	if _, err := f.source.Exec(f.ctx, `UPDATE notification_registrations SET source_cursor=9007199254740994 WHERE id=$1`, registration.Id); err != nil {
		t.Fatal("seed the committed fanout cursor")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,state,attempts,lease_id,lease_until,outcome)
		VALUES(repeat('a',32),$1,1,2,9007199254740993,'CatalogInvalidated',$2,'pending',0,'',NULL,''),
		(repeat('b',32),$1,1,2,9007199254740994,'CatalogInvalidated',$2,'sending',1,repeat('d',32),'2099-01-01T00:00:00Z',''),
		(repeat('c',32),$1,1,2,9007199254740992,'CatalogInvalidated','[]','delivered',1,'',NULL,'delivered')`, registration.Id, refs); err != nil {
		t.Fatal("seed pending, leased and immutable terminal delivery checkpoints")
	}
	return source
}

func TestEngineSelectedPhase4RestorePreservesHostOwnershipAndPreventsNotificationReplay(t *testing.T) {
	for _, captured := range []bool{false, true} {
		name := "offline_deployment_defaults"
		if captured {
			name = "trusted_target_capture"
		}
		t.Run(name, func(t *testing.T) {
			defer releaseRecoveryEngineTestMemory()
			f := newEngineRecoveryFixture(t)
			source := seedSelectedPhase4EngineState(t, f)
			before := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase4RawSQL)
			retained := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase4RetainedStateSQL(true))
			manifest, metadata := f.create(t)
			reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
			if err != nil {
				t.Fatal("open the generated phase4 archive")
			}
			defer reader.Close()
			passphrase := []byte("recovery-integration-passphrase")
			defer clear(passphrase)
			recoveryEngine := f.engine
			if !captured {
				unavailable := f.configuration
				original, err := url.Parse(unavailable.DatabaseURL)
				if err != nil || original.Host == "" {
					t.Fatal("parse the private offline source configuration")
				}
				original.Host = "127.0.0.1:1"
				unavailable.DatabaseURL = original.String()
				recoveryEngine, err = NewOfflineEngine(unavailable, f.objects, "phase4-offline-recovery")
				if err != nil || recoveryEngine.pool != nil || recoveryEngine.vault != nil {
					t.Fatal("construct offline phase4 recovery without a source owner")
				}
				recoveryEngine.options.Schema = f.schema
			}
			archive, err := recoveryEngine.OpenArchive(f.ctx, reader, passphrase)
			if err != nil {
				t.Fatalf("authenticate the phase4 archive: %v", err)
			}
			defer archive.Close()
			releaseRecoveryEngineTestMemory()
			lease, err := database.AcquireLease(f.ctx, f.target)
			if err != nil {
				t.Fatal("lease the owned phase4 target")
			}
			defer lease.Close()
			target := settings.TargetHostSettings{}
			if captured {
				target = settings.TargetHostSettings{Network: &settings.NetworkOverrides{BindHost: phase4Pointer("127.0.0.1"), HttpPort: phase4Pointer(10096)}, Hardware: &settings.HardwareSelection{Decode: "software", Encode: "software"}, Threads: phase4Pointer(3)}
			}
			refused := errors.New("phase4 raw fingerprints and ciphertext witnessed")
			called := false
			_, err = backuppg.RestoreFinalized(f.ctx, f.source, f.target, archive.Database(), manifest.Source, f.engine.options, func(ctx context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
				called = true
				if !reflect.DeepEqual(raw.Tables, manifest.Source.Tables) {
					t.Error("raw phase4 table fingerprints changed before normalization")
				}
				var state string
				if err := tx.QueryRow(ctx, selectedPhase4RawSQL).Scan(&state); err != nil || state != before {
					t.Error("source host settings or delivery evidence changed before normalization")
				}
				witness, err := identity.ValidateApplicationKeyRecovery(ctx, tx, archive.Master)
				if err != nil || witness.SealedNotificationCount != 2 {
					t.Errorf("real purpose-bound notification ciphertext was not validated: %v", err)
				}
				if captured {
					// Corruption remains SQL-valid but must fail before any host setting,
					// credential or delivery normalization can obscure the raw evidence.
					if _, err := tx.Exec(ctx, `UPDATE notification_registrations SET token_ciphertext=set_byte(token_ciphertext,20,get_byte(token_ciphertext,20)#1)`); err != nil {
						return err
					}
					if _, err := normalizeRestoredIdentityWithHost(ctx, tx, archive.Master, f.targetConfig, target); !errors.Is(err, identity.ErrApplicationKeyVaultCiphertext) {
						t.Errorf("corrupt sealed target token reached normalization: %v", err)
					}
					var unchanged bool
					if err := tx.QueryRow(ctx, `SELECT (SELECT enabled FROM notification_transport WHERE id=1)
					AND (SELECT revision=9007199254740993 FROM managed_settings WHERE id=1)
					AND (SELECT count(*) FROM notification_deliveries WHERE state IN ('pending','sending'))=2
					AND EXISTS(SELECT 1 FROM sessions WHERE revoked_at IS NULL)`).Scan(&unchanged); err != nil || !unchanged {
						t.Error("failed ciphertext witness changed normalization state")
					}
				}
				return refused
			})
			if !called || !errors.Is(err, refused) {
				t.Fatalf("raw phase4 refusal did not roll back: %v", err)
			}
			if _, err := archive.Database().Seek(0, io.SeekStart); err != nil {
				t.Fatal("rewind the same authenticated phase4 archive")
			}
			var result RestoredDatabase
			if captured {
				result, err = archive.restoreIntoWithHost(f.ctx, f.target, lease, f.targetConfig, target)
			} else {
				result, err = archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
			}
			if err != nil || !result.NormalizedHostSettings || result.RevokedCredentials != 3 ||
				result.DisabledNotificationTransports != 1 || result.DisabledNotificationRegistrations != 1 || result.CancelledNotificationDeliveries != 2 {
				t.Fatalf("restore target choices and revoke imported execution authority: %v", err)
			}
			var raw []byte
			var revision int64
			if err := f.target.QueryRow(f.ctx, `SELECT revision,runtime_overrides FROM managed_settings WHERE id=1`).Scan(&revision, &raw); err != nil {
				t.Fatal("read normalized target choices")
			}
			var actual settings.RuntimeOverrides
			if json.Unmarshal(raw, &actual) != nil {
				t.Fatal("decode normalized target choices")
			}
			if revision != 9007199254740994 || !reflect.DeepEqual(actual.Network, target.Network) || !reflect.DeepEqual(actual.Hardware, target.Hardware) || !reflect.DeepEqual(actual.Threads, target.Threads) ||
				!reflect.DeepEqual(actual.H264, source.H264) || !reflect.DeepEqual(actual.HEVC, source.HEVC) || !reflect.DeepEqual(actual.SoftwareToneMapping, source.SoftwareToneMapping) || !reflect.DeepEqual(actual.VulkanToneMapping, source.VulkanToneMapping) {
				t.Fatal("restore imported host authority, changed portable quality, or advanced CAS imprecisely")
			}
			var safe bool
			if err := f.target.QueryRow(f.ctx, `SELECT (SELECT NOT enabled AND revision=3 FROM notification_transport WHERE id=1)
			AND NOT EXISTS(SELECT 1 FROM notification_registrations WHERE enabled OR revision<>2 OR last_outcome<>'backup_restored')
			AND NOT EXISTS(SELECT 1 FROM notification_deliveries WHERE state IN ('pending','sending') OR lease_id<>'' OR lease_until IS NOT NULL)
			AND NOT EXISTS(SELECT 1 FROM notification_deliveries WHERE id IN (repeat('a',32),repeat('b',32)) AND refs IS DISTINCT FROM '[]'::jsonb)
			AND (SELECT count(*) FROM notification_deliveries WHERE state='cancelled' AND outcome='backup_restored')=2`).Scan(&safe); err != nil || !safe {
				t.Fatalf("restoration retained notification replay authority: %v", err)
			}
			if got := recoveryEngineJSONState(t, f.ctx, f.target, selectedPhase4RetainedStateSQL(false)); got != retained {
				t.Fatal("normalization changed ciphertext, terminal delivery history, source journal or portable unrelated state")
			}
			normalized := recoveryEngineJSONState(t, f.ctx, f.target, selectedPhase4RawSQL)
			tx, err := f.target.Begin(f.ctx)
			if err != nil {
				t.Fatal("begin repeated restore normalization")
			}
			defer rollbackRestore(tx)
			repeated, err := normalizeRestoredIdentityWithHost(f.ctx, tx, archive.Master, f.targetConfig, target)
			if err != nil || repeated.NormalizedHostSettings || repeated.RevokedCredentials != 0 ||
				repeated.DisabledNotificationTransports != 0 || repeated.DisabledNotificationRegistrations != 0 || repeated.CancelledNotificationDeliveries != 0 {
				t.Fatalf("repeated normalization changed retained state: %v", err)
			}
			if err := tx.Commit(f.ctx); err != nil {
				t.Fatal("complete repeated no-op normalization")
			}
			if got := recoveryEngineJSONState(t, f.ctx, f.target, selectedPhase4RawSQL); got != normalized {
				t.Fatal("repeated restore advanced revisions or rewrote delivery timestamps")
			}
			if got := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase4RawSQL); got != before {
				t.Fatal("phase4 archive restore changed its original source")
			}
			if strings.Contains(string(raw), "source-host-amd") || strings.Contains(string(raw), "192.0.2.99") {
				t.Fatal("source deployment choices survived activation normalization")
			}
		})
	}
}
