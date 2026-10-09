//go:build linux

package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

type remoteDeliveryWait struct {
	ctx     context.Context
	cancel  context.CancelFunc
	blocker pgx.Tx
	done    chan struct{}
	allowed bool
	err     error
}

func beginRemoteDeliveryWait(t *testing.T, f *serverFixture, receiver identity.Principal, event events.Event, account bool) *remoteDeliveryWait {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	blocker, err := f.pool.Begin(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	operation := &remoteDeliveryWait{ctx: ctx, cancel: cancel, blocker: blocker, done: make(chan struct{})}
	var id string
	statement, target := "SELECT id FROM sessions WHERE id=$1 FOR UPDATE", receiver.SessionID
	if account {
		statement, target = "SELECT id FROM users WHERE id=$1 FOR UPDATE", receiver.User.ID
	}
	if err := blocker.QueryRow(ctx, statement, target).Scan(&id); err != nil {
		_ = blocker.Rollback(context.Background())
		cancel()
		t.Fatal(err)
	}
	blockerPID := blocker.Conn().PgConn().PID()
	go func() {
		defer close(operation.done)
		operation.allowed, operation.err = f.app.authorizeRemoteSocketEvent(ctx, receiver, event)
	}()
	t.Cleanup(func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = blocker.Rollback(cleanup)
		select {
		case <-operation.done:
		case <-time.After(10 * time.Second):
			t.Error("remote delivery did not retire after releasing its receiver blocker")
		}
	})
	wait, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := f.pool.QueryRow(wait, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock'
			AND (query LIKE 'SELECT id FROM sessions%' OR query LIKE 'SELECT is_administrator, policy FROM users%'))`, blockerPID).Scan(&blocked); err != nil {
			t.Fatal("observe remote delivery's receiver authority wait", err)
		}
		if blocked {
			return operation
		}
		select {
		case <-operation.done:
			t.Fatalf("remote delivery returned before its receiver wait: allowed=%t error=%v", operation.allowed, operation.err)
		case <-wait.Done():
			t.Fatal("remote delivery did not reach its receiver authority wait")
		case <-ticker.C:
		}
	}
}

func (operation *remoteDeliveryWait) release(t *testing.T, want bool) {
	t.Helper()
	if err := operation.blocker.Commit(operation.ctx); err != nil {
		t.Fatal("release receiver authority wait", err)
	}
	select {
	case <-operation.done:
	case <-operation.ctx.Done():
		t.Fatal("remote delivery did not finish after its receiver wait")
	}
	if operation.err != nil || operation.allowed != want {
		t.Fatalf("remote delivery after receiver wait: allowed=%t want=%t error=%v", operation.allowed, want, operation.err)
	}
}

func remoteDeliveryEvent(t *testing.T, controller, receiver identity.Principal, message string, data map[string]any) events.Event {
	t.Helper()
	target := identity.ClientSession{Kind: receiver.Kind, UserID: receiver.User.ID,
		SessionID: clientSessionID(receiver), CredentialID: receiver.SessionID, ApplicationKeyID: receiver.ApplicationKeyID}
	envelope, err := remoteCommandEnvelope(message, data, controller, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if receiver.IsApplicationKey() {
		return applicationQueuedCommand(t, envelope, receiver)
	}
	return remoteCommandQueuedEvent(t, envelope, receiver)
}

func remoteDeliveryPrincipal(t *testing.T, f *serverFixture, login clientSessionHTTPLogin) identity.Principal {
	t.Helper()
	principal, err := f.users.ResolveWithPeer(f.ctx, login.headers.Get("X-Emby-Token"), "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func enableRemoteDeliveryReceiver(t *testing.T, f *serverFixture, receiver identity.Principal) {
	t.Helper()
	if err := f.users.UpdateClientCapabilities(f.ctx, receiver, clientSessionID(receiver), identity.ClientCapabilities{SupportsMediaControl: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCommandDeliveryRevalidatesControllerAfterReceiverWait(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	receiver := remoteDeliveryPrincipal(t, f, accounts.viewer)
	enableRemoteDeliveryReceiver(t, f, receiver)
	for _, message := range []string{"Playstate", "GeneralCommand"} {
		for _, change := range []string{"unchanged", "revoked", "expired", "control-policy"} {
			t.Run(message+"/"+change, func(t *testing.T) {
				user, err := f.users.CreateUser(f.ctx, "Wait controller "+message+change, "controller-password", false)
				if err != nil {
					t.Fatal(err)
				}
				setHTTPUserPolicy(t, f, user.ID, `{"EnableRemoteControlOfOtherUsers":true}`)
				credentials, err := f.users.AuthenticateWithPeer(f.ctx, user.Name, "controller-password", identity.Client{
					Name: "Delivery wait", DeviceID: message + change, Device: "Delivery fixture", Version: "1",
				}, "emby", "127.0.0.1")
				if err != nil {
					t.Fatal(err)
				}
				controller, err := f.users.ResolveWithPeer(f.ctx, credentials.Token, "emby", "127.0.0.1")
				if err != nil {
					t.Fatal(err)
				}
				event := remoteDeliveryEvent(t, controller, receiver, message, map[string]any{"Command": "Pause", "Name": "SetVolume"})
				operation := beginRemoteDeliveryWait(t, f, receiver, event, false)
				switch change {
				case "revoked":
					err = f.users.Revoke(operation.ctx, credentials.Token)
				case "expired":
					_, err = f.pool.Exec(operation.ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',
						expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, controller.SessionID)
				case "control-policy":
					_, err = f.pool.Exec(operation.ctx, `UPDATE users SET policy=policy || '{"EnableRemoteControlOfOtherUsers":false}'::jsonb WHERE id=$1`, controller.User.ID)
				}
				if err != nil {
					t.Fatal("change only the waiting command's controller", err)
				}
				operation.release(t, change == "unchanged")
				if _, err := f.users.RevalidateSession(f.ctx, receiver); err != nil {
					t.Fatal("controller rejection invalidated the independent receiver", err)
				}
			})
		}
	}
}

func TestRemoteCommandDeliveryPreservesSiblingAndSelfChecks(t *testing.T) {
	for _, change := range []string{"sibling-revoked", "self-revoked", "self-expired", "self-control-feature"} {
		t.Run(change, func(t *testing.T) {
			f, accounts := newClientSessionHTTPAccounts(t)
			receiver := remoteDeliveryPrincipal(t, f, accounts.viewer)
			controller := receiver
			if change == "sibling-revoked" {
				controller = remoteDeliveryPrincipal(t, f, accounts.second)
			}
			enableRemoteDeliveryReceiver(t, f, receiver)
			event := remoteDeliveryEvent(t, controller, receiver, "GeneralCommand", map[string]any{"Name": "VolumeUp"})
			operation := beginRemoteDeliveryWait(t, f, receiver, event, change == "self-control-feature")
			var err error
			switch change {
			case "sibling-revoked":
				err = f.users.Revoke(operation.ctx, accounts.second.headers.Get("X-Emby-Token"))
			case "self-revoked":
				_, err = operation.blocker.Exec(operation.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, receiver.SessionID)
			case "self-expired":
				_, err = operation.blocker.Exec(operation.ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',
					expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, receiver.SessionID)
			case "self-control-feature":
				_, err = operation.blocker.Exec(operation.ctx, `UPDATE users SET policy=policy || '{"RestrictedFeatures":["goby_remote_control"]}'::jsonb WHERE id=$1`, receiver.User.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			operation.release(t, false)
		})
	}
}

func TestRemoteCommandDeliveryRevalidatesApplicationControllerAfterReceiverWait(t *testing.T) {
	for _, change := range []string{"key-revoked", "client-removed"} {
		t.Run(change, func(t *testing.T) {
			f, accounts := newClientSessionHTTPAccounts(t)
			keys := applicationMediaIssueKeys(t, f, accounts.admin.headers.Get("X-Emby-Token"))
			controller, receiver := keys[0].principal, keys[1].principal
			enableRemoteDeliveryReceiver(t, f, receiver)
			event := remoteDeliveryEvent(t, controller, receiver, "GeneralCommand", map[string]any{"Name": "VolumeUp"})
			operation := beginRemoteDeliveryWait(t, f, receiver, event, false)
			var err error
			if change == "key-revoked" {
				err = f.users.Revoke(operation.ctx, keys[0].key.Token)
			} else {
				_, err = f.pool.Exec(operation.ctx, "DELETE FROM application_key_clients WHERE id=$1", controller.ClientSessionID)
			}
			if err != nil {
				t.Fatal(err)
			}
			operation.release(t, false)
			if _, err := f.users.RevalidateSession(f.ctx, receiver); err != nil {
				t.Fatal("application controller rejection invalidated the receiver", err)
			}
		})
	}
}

func TestRemoteCommandDeliveryPropagatesReceiverWaitCancellation(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	receiver := remoteDeliveryPrincipal(t, f, accounts.viewer)
	controller := remoteDeliveryPrincipal(t, f, accounts.admin)
	enableRemoteDeliveryReceiver(t, f, receiver)
	event := remoteDeliveryEvent(t, controller, receiver, "GeneralCommand", map[string]any{"Name": "VolumeUp"})
	operation := beginRemoteDeliveryWait(t, f, receiver, event, false)
	operation.cancel()
	select {
	case <-operation.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled remote delivery did not release its receiver wait")
	}
	if operation.allowed || !errors.Is(operation.err, context.Canceled) {
		t.Fatalf("receiver wait cancellation was swallowed: allowed=%t error=%v", operation.allowed, operation.err)
	}
}

func TestRemotePlayDeliveryRevalidatesControllerAfterReceiverWait(t *testing.T) {
	stream := newStreamHTTPFixture(t)
	f := stream.f
	receiver, err := f.users.ResolveWithPeer(f.ctx, stream.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := f.users.AuthenticateWithPeer(f.ctx, "Administrator", "administrator-password", identity.Client{
		Name: "Waiting Play controller", DeviceID: "waiting-play-controller", Device: "Play fixture", Version: "1",
	}, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	controller, err := f.users.ResolveWithPeer(f.ctx, credentials.Token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	enableRemoteDeliveryReceiver(t, f, receiver)
	event := remoteDeliveryEvent(t, controller, receiver, "Play", map[string]any{"ItemIds": []string{stream.video.id}, "PlayCommand": "PlayNow"})
	operation := beginRemoteDeliveryWait(t, f, receiver, event, false)
	if err := f.users.Revoke(operation.ctx, credentials.Token); err != nil {
		t.Fatal(err)
	}
	// The user's catalog visibility remains unchanged. Reject the original
	// credential after both item reads instead of treating visible IDs as login.
	operation.release(t, false)
}

type remoteControllerPolicyTrace struct {
	sessionID string
	calls     int
	err       error
	change    func(context.Context, bool) error
}

type remoteControllerPolicyTraceKey struct{}

func (trace *remoteControllerPolicyTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "FROM sessions authentication JOIN users u") && len(data.Args) > 0 && data.Args[0] == trace.sessionID {
		trace.calls++
		if trace.calls == 1 {
			return context.WithValue(ctx, remoteControllerPolicyTraceKey{}, true)
		}
		if trace.calls == 2 {
			trace.err = trace.change(ctx, false)
		}
	}
	return ctx
}

func (trace *remoteControllerPolicyTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil && ctx.Value(remoteControllerPolicyTraceKey{}) == true {
		trace.err = trace.change(ctx, true)
	}
}

func TestRemotePlayDeliveryRejectsIntermediateControllerPolicy(t *testing.T) {
	stream := newStreamHTTPFixture(t)
	f := stream.f
	receiver, err := f.users.ResolveWithPeer(f.ctx, stream.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.users.CreateUser(f.ctx, "Transient Play Controller", "controller-password", false)
	if err != nil {
		t.Fatal(err)
	}
	const denied = `{"EnableRemoteControlOfOtherUsers":true,"EnableAllFolders":false,"EnabledFolders":[]}`
	const allowed = `{"EnableRemoteControlOfOtherUsers":true,"EnableAllFolders":true}`
	setHTTPUserPolicy(t, f, user.ID, allowed)
	credentials, err := f.users.AuthenticateWithPeer(f.ctx, user.Name, "controller-password", identity.Client{
		Name: "Transient controller", DeviceID: "transient-policy", Device: "Policy fixture", Version: "1",
	}, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	controller, err := f.users.ResolveWithPeer(f.ctx, credentials.Token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	enableRemoteDeliveryReceiver(t, f, receiver)
	event := remoteDeliveryEvent(t, controller, receiver, "Play", map[string]any{"ItemIds": []string{stream.video.id}, "PlayCommand": "PlayNow"})
	if permitted, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event); err != nil || !permitted {
		t.Fatalf("authorized Play fixture was not deliverable: %t, %v", permitted, err)
	}
	setHTTPUserPolicy(t, f, user.ID, denied)
	trace := &remoteControllerPolicyTrace{sessionID: controller.SessionID}
	trace.change = func(ctx context.Context, permit bool) error {
		policy := denied
		if permit {
			policy = allowed
		}
		_, err := f.pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", user.ID, policy)
		return err
	}
	configuration := f.pool.Config().Copy()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(f.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.app.identity = identity.New(pool)
	permitted, err := f.app.authorizeRemoteSocketEvent(f.ctx, receiver, event)
	if trace.err != nil || trace.calls != 2 {
		t.Fatalf("policy fixture did not surround the actual item projection with two matching authority observations: calls=%d error=%v", trace.calls, trace.err)
	}
	if err != nil || permitted {
		t.Fatalf("Play reused an intermediate permission snapshot after policy changed back: allowed=%t error=%v", permitted, err)
	}
	if _, err := f.users.RevalidateSession(f.ctx, receiver); err != nil {
		t.Fatal("policy mismatch invalidated the independent receiver", err)
	}
}
