package settings

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func configurationPolicyRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var result string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(p)::text FROM server_policy_settings p WHERE id=1").Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func configurationPolicyEventSequence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var result int64
	if err := pool.QueryRow(ctx, "SELECT sequence FROM task_system_events WHERE name='ConfigurationChanged'").Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestConfigurationPolicyMixedWritesKeepNativeCASIndependent(t *testing.T) {
	ctx, pool, owner, store, native := settingsRepository(t)
	policy, err := NewServerPolicy(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actor := configurationTestActor(t, ctx, pool, native, false)
	initial, policyInitial := store.Snapshot(), policy.Snapshot()
	name, language, country, culture, remote := "Combined configuration", "fr", "FR", "en-US", false
	subnets := []string{}
	mutation := ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name,
		HttpServerPortNumberPresent: true, HttpServerPortNumber: 9096, PreferredMetadataLanguage: &language,
		MetadataCountryCode: &country, EnableRemoteAccess: &remote, LocalNetworkSubnets: &subnets, UICulture: &culture}
	committed, err := store.ApplyConfigurationWithPolicy(ctx, actor, mutation, policy, "127.0.0.1")
	if err != nil || committed.Revision != initial.Revision+1 || committed.Effective.ServerName != name ||
		committed.Runtime.DesiredNetwork.HttpPort != 9096 || committed.Management.Metadata.PreferredMetadataLanguage != language ||
		committed.Management.Metadata.MetadataCountryCode != country {
		t.Fatalf("mixed configuration failed to commit all managed fields: %v", err)
	}
	policyCommitted := policy.Snapshot()
	if policyCommitted.Revision != policyInitial.Revision+1 || policyCommitted.Values.EnableRemoteAccess ||
		policyCommitted.Values.LocalNetworkSubnets == nil || len(policyCommitted.Values.LocalNetworkSubnets) != 0 || policyCommitted.Values.UICulture != culture {
		t.Fatal("mixed configuration failed to publish the separately revisioned policy")
	}
	view, err := store.GetConfigurationWithPolicy(ctx, actor, policy)
	if err != nil || !view.CanManage || view.ServerPolicy == nil || !reflect.DeepEqual(view.Snapshot, committed) ||
		!reflect.DeepEqual(*view.ServerPolicy, policyCommitted) {
		t.Fatalf("combined configuration read did not return both committed snapshots: %v", err)
	}
	viewer := configurationTestActor(t, ctx, pool, native, true)
	viewerView, err := store.GetConfigurationWithPolicy(ctx, viewer, policy)
	if err != nil || !reflect.DeepEqual(viewerView, Configuration{}) {
		t.Fatalf("viewer configuration exposed policy or managed values: %v", err)
	}
	if _, err := store.Update(ctx, native, UpdateRequest{Revision: initial.Revision, Overrides: initial.Overrides}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("combined configuration bypassed the native managed-settings CAS: %v", err)
	}
	if _, err := policy.Update(ctx, native, ServerPolicyUpdate{Revision: policyInitial.Revision, Values: policyInitial.Values,
		ClientAddress: "127.0.0.1"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("combined configuration bypassed the independent native policy CAS: %v", err)
	}
	culture = "zh-CN"
	unchanged, err := store.ApplyConfigurationWithPolicy(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, UICulture: &culture}, policy, "127.0.0.1")
	if err != nil || !reflect.DeepEqual(unchanged, committed) || policy.Snapshot().Revision != policyCommitted.Revision+1 {
		t.Fatalf("policy-only mutation advanced the unrelated managed-settings revision: %v", err)
	}
	policyOnly := policy.Snapshot()
	for _, section := range []ConfigurationSection{ConfigurationPartial, ConfigurationFull} {
		if _, err := store.ApplyConfigurationWithPolicy(ctx, actor, ConfigurationMutation{Section: section}, policy, "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(policy.Snapshot(), policyOnly) {
			t.Fatal("an omitted policy field reset the access boundary or interface culture")
		}
	}
	beforeNoop := store.Snapshot()
	if _, err := store.ApplyConfigurationWithPolicy(ctx, actor, ConfigurationMutation{Section: ConfigurationFull,
		EnableRemoteAccess: &remote, LocalNetworkSubnets: &subnets, UICulture: &culture}, policy, "127.0.0.1"); err != nil ||
		!reflect.DeepEqual(store.Snapshot(), beforeNoop) || !reflect.DeepEqual(policy.Snapshot(), policyOnly) {
		t.Fatalf("explicit policy no-op advanced a revision or changed another setting: %v", err)
	}
	reloaded, err := NewServerPolicy(ctx, owner)
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot(), policyOnly) {
		t.Fatalf("reload lost the policy written through core configuration: %v", err)
	}
}

func TestConfigurationPolicyFailureRollsBackBothSettingsAndPublication(t *testing.T) {
	for _, failure := range []string{"policy-row", "policy-activity", "policy-system-event"} {
		t.Run(failure, func(t *testing.T) {
			ctx, pool, owner, store, native := settingsRepository(t)
			policy, err := NewServerPolicy(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			actor := configurationTestActor(t, ctx, pool, native, false)
			before, policyBefore := store.Snapshot(), policy.Snapshot()
			rowBefore, policyRowBefore := settingsRowSnapshot(t, ctx, pool), configurationPolicyRow(t, ctx, pool)
			activityBefore, sequenceBefore := settingsActivityRows(t, ctx, pool), configurationPolicyEventSequence(t, ctx, pool)
			table, constraint := "server_policy_settings", "CHECK (ui_culture <> 'en-US')"
			switch failure {
			case "policy-activity":
				table, constraint = "activity_entries", "CHECK (resource_id <> 'server_policy')"
			case "policy-system-event":
				table, constraint = "task_system_events", fmt.Sprintf("CHECK (name <> 'ConfigurationChanged' OR sequence <= %d)", sequenceBefore+1)
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT configuration_policy_test_rejection "+constraint+" NOT VALID"); err != nil {
				t.Fatal(err)
			}
			remote, release, allowed := policy.Watch(ctx, "203.0.113.17")
			if !allowed {
				t.Fatal("initial remote operation was rejected")
			}
			defer release()
			name, culture, enable := "Must roll back both records", "en-US", false
			mutation := ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name,
				HttpServerPortNumberPresent: true, HttpServerPortNumber: 9096, UICulture: &culture, EnableRemoteAccess: &enable}
			_, err = store.ApplyConfigurationWithPolicy(ctx, actor, mutation, policy, "127.0.0.1")
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
				t.Fatalf("combined write did not reach the injected second-record failure: %v", err)
			}
			if settingsRowSnapshot(t, ctx, pool) != rowBefore || configurationPolicyRow(t, ctx, pool) != policyRowBefore ||
				settingsActivityRows(t, ctx, pool) != activityBefore || configurationPolicyEventSequence(t, ctx, pool) != sequenceBefore ||
				!reflect.DeepEqual(store.Snapshot(), before) || !reflect.DeepEqual(policy.Snapshot(), policyBefore) || remote.Err() != nil {
				t.Fatal("failed mixed write changed a record, event, cache, or admitted operation")
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE "+table+" DROP CONSTRAINT configuration_policy_test_rejection"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ApplyConfigurationWithPolicy(ctx, actor, mutation, policy, "127.0.0.1"); err != nil ||
				store.Snapshot().Revision != before.Revision+1 || policy.Snapshot().Revision != policyBefore.Revision+1 ||
				!errors.Is(context.Cause(remote), ErrServerPolicyDenied) {
				t.Fatalf("identical combined write could not recover after rollback: %v", err)
			}
		})
	}
}

type configurationPolicyHookOwner struct {
	owner       library.OwnedTransactions
	afterWrite  func(library.OwnedTx) error
	afterCommit func() error
}

func (owner configurationPolicyHookOwner) WithOwnedTx(ctx context.Context, callback func(library.OwnedTx) error) error {
	wrote := false
	err := owner.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return callback(configurationPolicyHookTx{OwnedTx: tx, after: func() error {
			wrote = true
			if owner.afterWrite != nil {
				return owner.afterWrite(tx)
			}
			return nil
		}})
	})
	if err == nil && wrote && owner.afterCommit != nil {
		return owner.afterCommit()
	}
	return err
}

type configurationPolicyHookTx struct {
	library.OwnedTx
	after func() error
}

func (tx configurationPolicyHookTx) QueryRow(statement string, args ...any) library.OwnedRow {
	row := tx.OwnedTx.QueryRow(statement, args...)
	if strings.HasPrefix(strings.TrimSpace(statement), "UPDATE server_policy_settings SET") {
		return settingsHookRow{row: row, after: tx.after}
	}
	return row
}

func TestConfigurationPolicyFinalAuthorityAndUnknownCommitNeverPublish(t *testing.T) {
	for _, failure := range []string{"actor-expiry", "unknown-commit"} {
		t.Run(failure, func(t *testing.T) {
			ctx, pool, owner, _, native := settingsRepository(t)
			actor := configurationTestActor(t, ctx, pool, native, false)
			uncertain := errors.New("synthetic unknown commit result")
			reached := false
			hooked := configurationPolicyHookOwner{owner: owner, afterWrite: func(tx library.OwnedTx) error {
				reached = true
				if failure == "actor-expiry" {
					_, err := tx.Exec(`SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
						(expires_at-clock_timestamp())))+0.03)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID)
					return err
				}
				return nil
			}}
			if failure == "unknown-commit" {
				hooked.afterCommit = func() error { return uncertain }
			}
			store, err := New(ctx, pool, hooked, settingsTestDefaults(), "settings-host-alpha")
			if err != nil {
				t.Fatal(err)
			}
			policy, err := NewServerPolicy(ctx, hooked)
			if err != nil {
				t.Fatal(err)
			}
			before, policyBefore := store.Snapshot(), policy.Snapshot()
			rowBefore, policyRowBefore := settingsRowSnapshot(t, ctx, pool), configurationPolicyRow(t, ctx, pool)
			if failure == "actor-expiry" {
				if _, err := pool.Exec(ctx, "UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1", actor.Principal.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			name, culture := "Unpublished configuration", "en-US"
			_, err = store.ApplyConfigurationWithPolicy(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial,
				ServerNamePresent: true, ServerName: &name, UICulture: &culture}, policy, "127.0.0.1")
			want := uncertain
			if failure == "actor-expiry" {
				want = identity.ErrUnauthorized
			}
			if !reached || !errors.Is(err, want) || !reflect.DeepEqual(store.Snapshot(), before) || !reflect.DeepEqual(policy.Snapshot(), policyBefore) {
				t.Fatalf("failed or uncertain transaction published one of its settings caches: %v", err)
			}
			if failure == "actor-expiry" {
				if settingsRowSnapshot(t, ctx, pool) != rowBefore || configurationPolicyRow(t, ctx, pool) != policyRowBefore {
					t.Fatal("final actor expiry left one of the two records committed")
				}
			} else {
				// A connection failure can hide a successful COMMIT. The caller
				// receives uncertainty, and fresh stores recover both records.
				reloaded, err := New(ctx, pool, owner, settingsTestDefaults(), "settings-host-alpha")
				if err != nil {
					t.Fatal(err)
				}
				reloadedPolicy, err := NewServerPolicy(ctx, owner)
				if err != nil || reloaded.Snapshot().Effective.ServerName != name || reloadedPolicy.Snapshot().Values.UICulture != culture {
					t.Fatalf("fresh publishers did not recover both records after an uncertain commit: %v", err)
				}
			}
		})
	}
}

func TestConfigurationPolicyRejectsDeniedPeerAndRevokedApplicationKey(t *testing.T) {
	ctx, pool, owner, store, native := settingsRepository(t)
	policy, err := NewServerPolicy(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actor := settingsActivityApplicationActor(t, ctx, pool, native)
	culture := "en-US"
	if _, err := store.ApplyConfigurationWithPolicy(ctx, actor, ConfigurationMutation{Section: ConfigurationPartial, UICulture: &culture}, policy, "203.0.113.17"); err != nil {
		t.Fatalf("complete userless application principal could not update core policy: %v", err)
	}
	values := policy.Snapshot().Values
	values.EnableRemoteAccess = false
	if _, err := policy.Update(ctx, native, ServerPolicyUpdate{Revision: policy.Snapshot().Revision, Values: values,
		ClientAddress: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	before, policyBefore := store.Snapshot(), policy.Snapshot()
	name, remote := "Denied peer must not reopen access", true
	mutation := ConfigurationMutation{Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name, EnableRemoteAccess: &remote}
	for _, candidate := range []ConfigurationMutation{mutation, {Section: ConfigurationPartial, ServerNamePresent: true, ServerName: &name}} {
		if _, err := store.ApplyConfigurationWithPolicy(ctx, actor, candidate, policy, "203.0.113.17"); !errors.Is(err, ErrServerPolicyDenied) {
			t.Fatalf("a denied peer changed configuration after its original admission: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyConfigurationWithPolicy(ctx, actor, mutation, policy, "127.0.0.1"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked application credential retained core configuration authority: %v", err)
	}
	if !reflect.DeepEqual(store.Snapshot(), before) || !reflect.DeepEqual(policy.Snapshot(), policyBefore) {
		t.Fatal("denied peer or revoked credential changed a settings publisher")
	}
}
