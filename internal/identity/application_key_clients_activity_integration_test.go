package identity_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func applicationClientActivitySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'credentials', (SELECT jsonb_agg(to_jsonb(a) || jsonb_build_object('xmin', a.xmin::text) ORDER BY a.id) FROM sessions a),
		'keys', (SELECT jsonb_agg(to_jsonb(k) || jsonb_build_object('xmin', k.xmin::text) ORDER BY k.id) FROM application_keys k),
		'clients', (SELECT jsonb_agg(to_jsonb(c) || jsonb_build_object('xmin', c.xmin::text) ORDER BY c.id) FROM application_key_clients c),
		'devices', (SELECT jsonb_agg(to_jsonb(d) || jsonb_build_object('xmin', d.xmin::text) ORDER BY d.id) FROM application_key_devices d))::text`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestStoreApplicationKeySteadyClientsUseSharedAuthorityWithoutWrites(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Shared readers")
	clients := make([]identity.Principal, 0, 4)
	for index := range 4 {
		clients = append(clients, bindApplicationClient(t, ctx, store, key, identity.Client{
			Name: fmt.Sprintf("Client %d", index), DeviceID: fmt.Sprintf("device-%d", index), Device: "Shared metadata", Version: "1.0"}))
	}
	before := applicationClientActivitySnapshot(t, ctx, pool)
	blocker, _ := managedSessionBlocker(t, ctx, pool)
	for _, statement := range []string{
		"SELECT id FROM sessions WHERE id = $1 FOR SHARE",
		"SELECT id FROM application_keys WHERE credential_id = $1 FOR SHARE",
		"SELECT id FROM application_key_clients WHERE credential_id = $1 FOR SHARE",
		"SELECT id FROM application_key_devices WHERE id = (SELECT reported_device_numeric_id FROM application_keys WHERE credential_id = $1) FOR SHARE",
	} {
		if _, err := blocker.Exec(ctx, statement, key.CredentialID); err != nil {
			t.Fatal(err)
		}
	}
	// These requests must finish while another reader pins every authority row.
	// The former FOR UPDATE authentication and duplicate touch both blocked here.
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for index := range 16 {
		workers.Add(1)
		go func(principal identity.Principal) {
			defer workers.Done()
			resolved, err := store.ResolveEmbyForClientWithPeer(readCtx, key.Token, principal.Client, "192.0.2.31")
			if err == nil && (resolved.ClientSessionID != principal.ClientSessionID || resolved.Client != principal.Client) {
				err = errors.New("steady authentication changed client context")
			}
			if err == nil {
				// A forged projected display is never written by the touch helper.
				resolved.Client.Device, resolved.Client.Version = "Untrusted", "forged"
				err = store.TouchClientSessionFromAddress(readCtx, resolved, "192.0.2.31")
			}
			results <- err
		}(clients[index%len(clients)])
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("steady request required an exclusive authority lock: %v", err)
		}
	}
	if after := applicationClientActivitySnapshot(t, ctx, pool); after != before {
		t.Fatal("steady requests wrote activity or replaced stored client metadata")
	}
}

func TestStoreApplicationKeyActivityPredicatesRestartExclusiveWithoutLockUpgrades(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Activity predicates")
	principal := bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Bound", DeviceID: "bound-device", Device: "Bound Device", Version: "1"})
	for _, mutation := range []struct {
		name, statement, check string
	}{
		{"client_due", "UPDATE application_key_clients SET last_seen_at = clock_timestamp() - interval '1 minute' WHERE id = $1", "SELECT last_seen_at > clock_timestamp() - interval '10 seconds' FROM application_key_clients WHERE id = $1"},
		{"credential_due", "UPDATE sessions SET last_seen_at = clock_timestamp() - interval '1 minute' WHERE id = $2", "SELECT last_seen_at > clock_timestamp() - interval '10 seconds' FROM sessions WHERE id = $2"},
		{"key_due", "UPDATE application_keys SET last_used_at = NULL WHERE credential_id = $2", "SELECT last_used_at > clock_timestamp() - interval '10 seconds' FROM application_keys WHERE credential_id = $2"},
		{"device_due", "UPDATE application_key_devices SET last_seen_at = clock_timestamp() - interval '1 minute' WHERE id = $3", "SELECT last_seen_at > clock_timestamp() - interval '10 seconds' FROM application_key_devices WHERE id = $3"},
		{"credential_metadata", "UPDATE sessions SET device_name = 'Other client', client_version = 'other' WHERE id = $2", "SELECT device_name = 'Bound Device' AND client_version = '1' FROM sessions WHERE id = $2"},
		{"device_metadata", "UPDATE application_key_devices SET reported_name = 'Other client', app_name = 'Other app', app_version = 'other' WHERE id = $3", "SELECT reported_name = 'Bound Device' AND app_name = 'Activity predicates' AND app_version = '1' FROM application_key_devices WHERE id = $3"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			// Placeholder use is intentionally uniform across the matrix.
			statement := "WITH args AS (SELECT $1::text AS client_id, $2::text AS credential_id, $3::bigint AS device_id) " + mutation.statement
			if _, err := pool.Exec(ctx, statement, principal.ClientSessionID, principal.SessionID, key.ReportedDeviceNumericID); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 8)
			for range 8 {
				go func() {
					<-start
					_, err := store.ResolveEmbyForClient(ctx, key.Token, principal.Client)
					results <- err
				}()
			}
			close(start)
			for range 8 {
				select {
				case err := <-results:
					if err != nil {
						t.Fatalf("competing due readers failed exclusive restart: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("competing due readers did not finish")
				}
			}
			var updated bool
			check := "WITH args AS (SELECT $1::text AS client_id, $2::text AS credential_id, $3::bigint AS device_id) " + mutation.check
			if err := pool.QueryRow(ctx, check, principal.ClientSessionID, principal.SessionID, key.ReportedDeviceNumericID).Scan(&updated); err != nil || !updated {
				t.Fatalf("due predicate was not persisted: updated=%t error=%v", updated, err)
			}
		})
	}
	client := principal.Client
	client.Device, client.Version = "Renamed Bound Device", "2"
	updated := bindApplicationClient(t, ctx, store, key, client)
	if updated.ClientSessionID != principal.ClientSessionID {
		t.Fatal("metadata restart created a replacement client context")
	}
}

func TestStoreApplicationKeySteadyTouchRechecksRevocationAfterCredentialWait(t *testing.T) {
	ctx, pool, store, admin, _ := applicationKeyTestStore(t)
	key := issueApplicationKey(t, ctx, store, admin, "Touch revocation")
	principal := bindApplicationClient(t, ctx, store, key, identity.Client{Name: "Bound", DeviceID: "bound-device", Device: "Bound", Version: "1"})
	blocker, blockerPID := managedSessionBlocker(t, ctx, pool)
	if _, err := blocker.Exec(ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", key.CredentialID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		results <- store.TouchClientSession(ctx, principal)
	}()
	waitManagedBlockedQuery(t, ctx, pool, blockerPID, "SELECT id FROM sessions WHERE id = $1 FOR SHARE", done)
	if _, err := blocker.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", key.CredentialID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("steady activity accepted revocation committed during its lock wait: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("steady activity did not finish after revocation")
	}
}
