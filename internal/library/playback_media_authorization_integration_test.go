//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func playbackMediaTestPrincipal(t *testing.T, fixture mediaSourceFixture, application bool) (identity.Principal, PlaySession) {
	t.Helper()
	owner := playSessionOwnerFixture(t, fixture.ctx, fixture.pool, fixture.userID, "combined-media-device")
	previous := identity.Principal{Kind: "emby", SessionID: owner.SessionID, User: identity.User{ID: owner.UserID},
		Client: identity.Client{DeviceID: owner.DeviceID}, PeerIP: "127.0.0.1"}
	if application {
		owner = applicationPlaybackOwnerFixture(t, fixture.ctx, fixture.pool, "combined-media")
		previous = identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: owner.SessionID,
			ClientSessionID: owner.ApplicationClientID, Client: identity.Client{DeviceID: owner.DeviceID}, PeerIP: "127.0.0.1"}
		if err := fixture.pool.QueryRow(fixture.ctx, "SELECT id FROM application_keys WHERE credential_id=$1", owner.SessionID).Scan(&previous.ApplicationKeyID); err != nil {
			t.Fatal(err)
		}
	}
	principal, err := identity.New(fixture.pool).RevalidateSession(fixture.ctx, previous)
	if err != nil {
		t.Fatal(err)
	}
	principal.PeerIP = previous.PeerIP
	return principal, playSessionPrepare(t, fixture.ctx, fixture.store, owner, fixture.item.ID, "")
}

type playbackMediaTestResult struct {
	file   *os.File
	result PlaybackMediaAuthorization
	err    error
}

func awaitPlaybackMediaTest(t *testing.T, ctx context.Context, results <-chan playbackMediaTestResult) playbackMediaTestResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("combined authorization did not complete")
		return playbackMediaTestResult{}
	}
}

func TestPlaybackMediaAuthorizationPreservesPrincipalSourceAndSharedReads(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			original, source, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, Subject{UserID: principal.User.ID, ApplicationCredentialID: func() string {
				if application {
					return principal.SessionID
				}
				return ""
			}()}, fixture.item.ID, play.MediaSourceID, false)
			if err != nil {
				t.Fatal(err)
			}
			_ = original.Close()
			barrier, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(barrier)
			var id string
			if err := barrier.QueryRow(fixture.ctx, "SELECT id FROM play_sessions WHERE id=$1 FOR SHARE", play.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			results := make(chan playbackMediaTestResult, 8)
			for range 8 {
				go func() {
					file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
					results <- playbackMediaTestResult{file, result, err}
				}()
			}
			for range 8 {
				result := awaitPlaybackMediaTest(t, ctx, results)
				if result.err != nil || result.file == nil {
					t.Fatalf("compatible shared reader did not complete: %v", result.err)
				}
				data, err := io.ReadAll(result.file)
				_ = result.file.Close()
				if err != nil || string(data) != fixture.contents || !reflect.DeepEqual(result.result.Source, source) ||
					!reflect.DeepEqual(result.result.Principal, principal) || result.result.Play.ID != play.ID {
					t.Fatalf("combined authorization changed current principal/source facts: %v", err)
				}
			}
		})
	}
}

func TestPlaybackMediaAuthorizationRejectsChangesCommittedDuringLockWait(t *testing.T) {
	for _, change := range []string{"disable", "permission", "folders", "login-revoke", "login-device", "key-revoke", "key-client-delete", "key-client-device", "stop", "item-delete"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, strings.HasPrefix(change, "key-"))
			statement, id, want := "UPDATE users SET is_disabled=true WHERE id=$1", principal.User.ID, error(identity.ErrUnauthorized)
			switch change {
			case "permission":
				statement, want = `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, ErrForbidden
			case "folders":
				statement, want = `UPDATE users SET policy=policy || '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, ErrNotFound
			case "login-revoke", "key-revoke":
				statement, id = "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID
			case "login-device":
				statement, id, want = "UPDATE sessions SET device_id='rebound-device' WHERE id=$1", principal.SessionID, ErrForbidden
			case "key-client-delete":
				statement, id = "DELETE FROM application_key_clients WHERE id=$1", principal.ClientSessionID
			case "key-client-device":
				statement, id = "UPDATE application_key_clients SET device_id='rebound-device' WHERE id=$1", principal.ClientSessionID
			case "stop":
				statement, id, want = "UPDATE play_sessions SET state='Stopped',stopped_at=clock_timestamp() WHERE id=$1", play.ID, ErrNotFound
			case "item-delete":
				statement, id, want = "DELETE FROM items WHERE id=$1", fixture.item.ID, ErrNotFound
			}
			barrier, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(barrier)
			if _, err := barrier.Exec(fixture.ctx, statement, id); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
				results <- playbackMediaTestResult{file, result, err}
			}()
			waitPlaybackValidationBlock(t, ctx, fixture.pool, barrier.Conn().PgConn().PID())
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.file != nil {
				_ = result.file.Close()
			}
			if result.file != nil || !errors.Is(result.err, want) {
				t.Fatalf("combined authorization accepted changed %s authority: %v, want %v", change, result.err, want)
			}
		})
	}
}

func TestPlaybackMediaAuthorizationRefreshesDeadlinesAfterPlayLockWait(t *testing.T) {
	for _, authentication := range []bool{false, true} {
		t.Run(map[bool]string{false: "play", true: "login"}[authentication], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			table, id, want := "play_sessions", play.ID, error(ErrNotFound)
			if authentication {
				table, id, want = "sessions", principal.SessionID, identity.ErrUnauthorized
			}
			if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE "+table+" SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			barrier, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(barrier)
			var locked string
			if err := barrier.QueryRow(fixture.ctx, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE", play.ID).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
				results <- playbackMediaTestResult{file, result, err}
			}()
			waitPlaybackValidationBlock(t, ctx, fixture.pool, barrier.Conn().PgConn().PID())
			if _, err := barrier.Exec(ctx, "SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))+0.05) FROM "+table+" WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.file != nil {
				_ = result.file.Close()
			}
			if result.file != nil || !errors.Is(result.err, want) {
				t.Fatalf("authorization accepted elapsed deadline: %v", result.err)
			}
		})
	}
}

type playbackMediaRootChangeTrace struct {
	observer *pgxpool.Pool
	rootID   string
	once     sync.Once
	err      error
}

func (trace *playbackMediaRootChangeTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "read-playback-source-test-marker") {
		return ctx
	}
	if strings.Contains(data.SQL, "publication.source_item_id=i.id") && strings.Contains(data.SQL, "AND i.type IN") {
		return context.WithValue(ctx, playbackMediaRootChangeContextKey{}, true)
	}
	return ctx
}

type playbackMediaRootChangeContextKey struct{}

func (trace *playbackMediaRootChangeTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(playbackMediaRootChangeContextKey{}).(bool); matched {
		trace.once.Do(func() {
			_, trace.err = trace.observer.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", trace.rootID)
		})
	}
}

func (*playbackMediaRootChangeTrace) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

func (trace *playbackMediaRootChangeTrace) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	if data.Err == nil && strings.Contains(data.SQL, "publication.source_item_id=i.id") && strings.Contains(data.SQL, "AND i.type IN") {
		trace.once.Do(func() {
			_, trace.err = trace.observer.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", trace.rootID)
		})
	}
}

func (*playbackMediaRootChangeTrace) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {
}

func TestPlaybackMediaAuthorizationCapturesSourceAndPublicationInOneSnapshot(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	trace := &playbackMediaRootChangeTrace{observer: fixture.pool}
	if err := fixture.pool.QueryRow(fixture.ctx, "SELECT root_id FROM items WHERE id=$1", fixture.item.ID).Scan(&trace.rootID); err != nil {
		t.Fatal(err)
	}
	configuration := fixture.pool.Config()
	configuration.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, traced)
	fixture.store.pool = traced
	t.Cleanup(func() { fixture.store.pool = fixture.pool })
	snapshot, _, err := fixture.store.readPlaybackMediaAuthorization(fixture.ctx, principal, playbackMediaOwner(principal), play.ID, fixture.item.ID, media.SourceID(fixture.item.ID), false)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fixture.store.openPublicMediaSource(fixture.ctx, snapshot)
	if file != nil {
		_ = file.Close()
	}
	if trace.err != nil {
		t.Fatal(trace.err)
	}
	if file != nil || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("mixed old source facts with a new root publication revision: %v", err)
	}
}

func TestPlaybackMediaAuthorizationChecksDeliveryPolicyAfterCommitBeforeIO(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	if err := os.Remove(fixture.path); err != nil {
		t.Fatal(err)
	}
	checks := 0
	file, _, err := fixture.store.AuthorizePlaybackMediaForChecked(fixture.ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false, func(current PlaybackMediaAuthorization) error {
		checks++
		if current.Principal.SessionID != principal.SessionID || current.Play.ID != play.ID || current.Source.Item.ID != fixture.item.ID {
			t.Error("delivery callback did not receive the authorized current facts")
		}
		ctx, cancel := context.WithTimeout(fixture.ctx, time.Second)
		defer cancel()
		if _, err := fixture.pool.Exec(ctx, "UPDATE users SET name=name WHERE id=$1", principal.User.ID); err != nil {
			t.Error("delivery callback retained the account lock or database transaction")
		}
		return ErrForbidden
	})
	if file != nil {
		_ = file.Close()
	}
	if checks != 1 || file != nil || !errors.Is(err, ErrForbidden) {
		t.Fatalf("source I/O preceded a delivery policy refusal: checks=%d error=%v", checks, err)
	}
}

func TestPlaybackMediaAuthorizationRechecksCredentialsAfterRootQueue(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			_, finish, err := fixture.store.beginMediaSourceLifetime(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			hint, err := fixture.store.readMediaSourceRootHint(fixture.ctx, fixture.item.ID)
			if err != nil {
				t.Fatal(err)
			}
			root, domain, err := fixture.store.mediaSourceRootLane(hint)
			if err != nil {
				t.Fatal(err)
			}
			var releases []func()
			for range mediaSourceRootOwnerLimit {
				release, err := mediaSourceAdmission.acquireRoot(fixture.ctx, false, root, domain)
				if err != nil {
					t.Fatal(err)
				}
				releases = append(releases, release)
				t.Cleanup(release)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
				results <- playbackMediaTestResult{file, result, err}
			}()
			for {
				queued := false
				mediaSourceAdmission.mu.Lock()
				for _, waiter := range mediaSourceAdmission.waiters {
					queued = queued || waiter.root == root
				}
				mediaSourceAdmission.mu.Unlock()
				if queued {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("combined authorization did not enter its root lane")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if _, err := fixture.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
				t.Fatal("root queue retained a credential transaction or failed to revoke the fixture")
			}
			for _, release := range releases {
				release()
			}
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.file != nil {
				_ = result.file.Close()
			}
			if result.file != nil || !errors.Is(result.err, identity.ErrUnauthorized) {
				t.Fatalf("root admission reused authority captured before revocation: %v", result.err)
			}
		})
	}
}

func TestPlaybackMediaAuthorizationSchedulingFailuresKeepCredentialErrorOrder(t *testing.T) {
	for _, failure := range []string{"missing-item", "full-root-queue"} {
		t.Run(failure, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			if failure == "missing-item" {
				if _, err := fixture.pool.Exec(fixture.ctx, "DELETE FROM items WHERE id=$1", fixture.item.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				_, finish, err := fixture.store.beginMediaSourceLifetime(fixture.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer finish()
				hint, err := fixture.store.readMediaSourceRootHint(fixture.ctx, fixture.item.ID)
				if err != nil {
					t.Fatal(err)
				}
				root, domain, err := fixture.store.mediaSourceRootLane(hint)
				if err != nil {
					t.Fatal(err)
				}
				for range mediaSourceRootOwnerLimit {
					release, err := mediaSourceAdmission.acquireRoot(fixture.ctx, false, root, domain)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(release)
				}
				for range mediaSourceScopedQueueLimit {
					waiter, err := mediaSourceAdmission.requestRoot(fixture.ctx, false, false, root, domain, mediaSourceScopedQueueLimit)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { mediaSourceAdmission.release(waiter) })
				}
			}
			if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
			defer cancel()
			file, _, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
			if file != nil {
				_ = file.Close()
			}
			if file != nil || !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("scheduling %s replaced the credential error: %v", failure, err)
			}
		})
	}
}
