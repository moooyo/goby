//go:build linux

package library

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type subtitleProviderSessionTrace struct {
	reads  atomic.Int64
	locks  atomic.Int64
	before func(context.Context, string)
}

func (trace *subtitleProviderSessionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	if strings.HasPrefix(statement, "SELECT ") && strings.Contains(statement, "FROM sessions ") {
		trace.reads.Add(1)
		if strings.HasSuffix(statement, " FOR SHARE") {
			trace.locks.Add(1)
		}
	}
	if trace.before != nil {
		trace.before(ctx, statement)
	}
	return ctx
}

func (*subtitleProviderSessionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func subtitleProviderReadFixture(t *testing.T, fixture mediaSourceFixture) (*Store, *subtitleProviderSessionTrace, identity.Principal) {
	t.Helper()
	owner := playSessionOwnerFixture(t, fixture.ctx, fixture.pool, fixture.userID, "provider-read-device")
	actor := identity.Principal{Kind: "emby", User: identity.User{ID: owner.UserID}, SessionID: owner.SessionID,
		Client: identity.Client{DeviceID: owner.DeviceID}, PeerIP: "127.0.0.1"}
	trace := &subtitleProviderSessionTrace{}
	configuration := fixture.pool.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	// These two read-only entry points require only the database pool.
	return &Store{pool: pool}, trace, actor
}

func TestSubtitleProviderReadEndpointsKeepBothLiveSessionBoundaries(t *testing.T) {
	fixture, subtitle, _ := subtitleTestCatalog(t)
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_subtitle_provider_sources
		(item_id, stream_index, provider, provider_id) VALUES ($1, $2, 'opensubtitles', 'provider-read-fixture')`,
		fixture.item.ID, subtitle.Index); err != nil {
		t.Fatal(err)
	}
	reader, trace, actor := subtitleProviderReadFixture(t, fixture)
	if _, tag, err := reader.SubtitleProviderTarget(fixture.ctx, actor, fixture.item.ID, media.SourceID(fixture.item.ID)); err != nil || tag == "" {
		t.Fatalf("subtitle provider target read failed: %v", err)
	}
	if reads, locks := trace.reads.Load(), trace.locks.Load(); reads != 4 || locks != 0 {
		t.Fatalf("target authorization used %d session reads and %d locks, want 4 and 0", reads, locks)
	}
	trace.reads.Store(0)
	if index, err := reader.DownloadedSubtitleIndex(fixture.ctx, actor, fixture.item.ID, "provider-read-fixture"); err != nil || index != subtitle.Index {
		t.Fatalf("downloaded subtitle index changed: index=%d error=%v", index, err)
	}
	if reads, locks := trace.reads.Load(), trace.locks.Load(); reads != 4 || locks != 0 {
		t.Fatalf("index authorization used %d session reads and %d locks, want 4 and 0", reads, locks)
	}
}

func TestSubtitleProviderAuthorizationRetainsMutationLocks(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	reader, trace, actor := subtitleProviderReadFixture(t, fixture)
	for _, lock := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "mutation"}[lock], func(t *testing.T) {
			options := pgx.TxOptions{AccessMode: pgx.ReadOnly}
			if lock {
				options.AccessMode = pgx.ReadWrite
			}
			tx, err := reader.pool.BeginTx(fixture.ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			trace.reads.Store(0)
			trace.locks.Store(0)
			if err := checkSubtitleProviderActor(fixture.ctx, tx, &actor, fixture.item.ID, lock); err != nil {
				t.Fatal(err)
			}
			wantReads, wantLocks := int64(2), int64(0)
			if lock {
				wantReads, wantLocks = 3, 1
			}
			if trace.reads.Load() != wantReads || trace.locks.Load() != wantLocks {
				t.Fatalf("session reads/locks = %d/%d, want %d/%d", trace.reads.Load(), trace.locks.Load(), wantReads, wantLocks)
			}
			for _, target := range []struct{ statement, id string }{
				{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", actor.User.ID},
				{"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", actor.SessionID},
			} {
				contender, err := fixture.pool.Begin(fixture.ctx)
				if err != nil {
					t.Fatal(err)
				}
				var id string
				err = contender.QueryRow(fixture.ctx, target.statement, target.id).Scan(&id)
				rollback(contender)
				var conflict *pgconn.PgError
				if lock && (!errors.As(err, &conflict) || conflict.Code != "55P03") || !lock && err != nil {
					t.Fatalf("account/credential locking changed: lock=%t error=%v", lock, err)
				}
			}
		})
	}
}

func TestSubtitleProviderReadAuthorizationKeepsScopeAndPolicyErrors(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	reader, _, actor := subtitleProviderReadFixture(t, fixture)
	libraryIntegrationUser(t, fixture.ctx, fixture.pool, "subtitle-other-owner", false, true, nil)
	for _, test := range []struct {
		name, statement string
		want            error
	}{
		{"missing_session", "DELETE FROM sessions WHERE id=$1", ErrForbidden},
		{"wrong_kind", "UPDATE sessions SET kind='admin' WHERE id=$1", ErrForbidden},
		{"wrong_owner", "UPDATE sessions SET user_id='subtitle-other-owner' WHERE id=$1", ErrForbidden},
		{"revoked", "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", ErrForbidden},
		{"expired", "UPDATE sessions SET created_at=clock_timestamp()-interval '2 days', expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", ErrForbidden},
		{"device_policy", `UPDATE users SET policy='{"EnableAllDevices":false,"EnabledDevices":["claimed-device"]}'::jsonb WHERE id=$1`, ErrForbidden},
		{"subtitle_policy", `UPDATE users SET policy='{"EnableSubtitleDownloading":false}'::jsonb WHERE id=$1`, ErrForbidden},
		{"locked_out", `UPDATE users SET policy='{"LockedOutDate":1}'::jsonb WHERE id=$1`, ErrForbidden},
		{"library_acl", `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := reader.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			id := actor.SessionID
			if strings.HasPrefix(test.statement, "UPDATE users ") {
				id = actor.User.ID
			}
			if _, err := tx.Exec(fixture.ctx, test.statement, id); err != nil {
				t.Fatal(err)
			}
			candidate := actor
			candidate.Client.DeviceID = "claimed-device"
			if err := checkSubtitleProviderActor(fixture.ctx, tx, &candidate, fixture.item.ID, false); !errors.Is(err, test.want) {
				t.Fatalf("subtitle authorization changed %s error: %v", test.name, err)
			}
		})
	}
}

func TestSubtitleProviderReadAuthorizationRechecksExpiryAfterEachACL(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	reader, trace, actor := subtitleProviderReadFixture(t, fixture)
	for _, boundary := range []int{1, 2} {
		t.Run(map[int]string{1: "initial_acl", 2: "final_acl"}[boundary], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(fixture.ctx, 15*time.Second)
			defer cancel()
			if _, err := fixture.pool.Exec(ctx, "UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			trace.reads.Store(0)
			var aclReads int
			var waited bool
			trace.before = func(work context.Context, statement string) {
				if !strings.HasPrefix(statement, "SELECT i.library_id FROM items i ") {
					return
				}
				aclReads++
				if aclReads == boundary {
					waited = true
					waitStateSessionExpiry(t, work, fixture.pool, actor.SessionID)
				}
			}
			t.Cleanup(func() { trace.before = nil })
			_, tag, err := reader.SubtitleProviderTarget(ctx, actor, fixture.item.ID, media.SourceID(fixture.item.ID))
			if !waited || tag != "" || !errors.Is(err, ErrForbidden) {
				t.Fatalf("authorization outlived ACL boundary %d: waited=%t error=%v", boundary, waited, err)
			}
			if reads := trace.reads.Load(); reads != int64(2*boundary) {
				t.Fatalf("boundary %d used %d credential reads, want %d", boundary, reads, 2*boundary)
			}
		})
	}
}

func TestSubtitleProviderLiveReadPreservesDatabaseErrors(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	reader, trace, actor := subtitleProviderReadFixture(t, fixture)
	var renamed bool
	t.Cleanup(func() {
		if renamed {
			if _, err := fixture.pool.Exec(fixture.ctx, "ALTER TABLE unavailable_subtitle_sessions RENAME TO sessions"); err != nil {
				t.Error(err)
			}
		}
	})
	trace.before = func(work context.Context, statement string) {
		if renamed || !strings.HasPrefix(statement, "SELECT COALESCE(revoked_at IS NULL") {
			return
		}
		if _, err := fixture.pool.Exec(work, "ALTER TABLE sessions RENAME TO unavailable_subtitle_sessions"); err != nil {
			t.Fatal(err)
		}
		renamed = true
	}
	tx, err := reader.pool.BeginTx(fixture.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	err = checkSubtitleProviderActor(fixture.ctx, tx, &actor, fixture.item.ID, false)
	rollback(tx)
	var databaseError *pgconn.PgError
	if !renamed || errors.Is(err, ErrForbidden) || !errors.As(err, &databaseError) || databaseError.Code != "42P01" {
		t.Fatalf("subtitle live-session read lost its database error: %v", err)
	}
}
