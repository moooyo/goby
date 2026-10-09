package notifications

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/notificationjournal"
)

const fanoutTransactionRegistration = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fanoutTargetTraceKey struct{}
type fanoutCursorTraceKey struct{}

type fanoutTransactionTrace struct {
	afterCursor    func(context.Context) error
	afterTarget    func(context.Context) error
	disableStarted chan int32
	cursorFired    bool
	fired          bool
	err            error
}

func (trace *fanoutTransactionTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT source_cursor,event_ids") {
		return context.WithValue(ctx, fanoutCursorTraceKey{}, true)
	}
	if strings.HasPrefix(data.SQL, "SELECT r.id,r.session_id") {
		return context.WithValue(ctx, fanoutTargetTraceKey{}, true)
	}
	if trace.disableStarted != nil && strings.HasPrefix(data.SQL, "UPDATE notification_registrations SET enabled=false") {
		trace.disableStarted <- int32(conn.PgConn().PID())
		trace.disableStarted = nil
	}
	return ctx
}

func (trace *fanoutTransactionTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(fanoutCursorTraceKey{}) == true && data.Err == nil && !trace.cursorFired {
		trace.cursorFired = true
		if trace.afterCursor != nil {
			trace.err = errors.Join(trace.err, trace.afterCursor(ctx))
		}
	}
	if ctx.Value(fanoutTargetTraceKey{}) == true && data.Err == nil && !trace.fired {
		trace.fired = true
		if trace.afterTarget != nil {
			trace.err = errors.Join(trace.err, trace.afterTarget(ctx))
		}
	}
}

type fanoutTransactionFixture struct {
	ctx      context.Context
	observer *pgxpool.Pool
	pool     *pgxpool.Pool
	store    *Store
	trace    *fanoutTransactionTrace
}

func newFanoutTransactionFixture(t *testing.T) fanoutTransactionFixture {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create notification fixture database connection")
	}
	t.Cleanup(admin.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "goby_notification_fanout_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned notification fixture schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse notification fixture database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "read committed"
	// Two observer connections allow a fixture-owned row lock and a separate
	// committed account mutation. The application pool below has only one.
	config.MaxConns = 2
	config.MinConns = 0
	observer, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	if err := database.Migrate(ctx, observer); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash)
		VALUES('notification-user','Notification user','notification user','fixture');
		INSERT INTO sessions(id,user_id,token_hash,kind,device_id,created_at,expires_at)
		VALUES('notification-session','notification-user',decode(repeat('ab',32),'hex'),'emby','notification-device',
			clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 hour');
		UPDATE notification_transport SET enabled=true,endpoint='https://receiver.invalid/events',
			credential_ciphertext=decode(repeat('00',48),'hex') WHERE id=1;
		INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext)
		VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','notification-session','notification-user','notification-device',
			'192.0.2.20',ARRAY['CatalogInvalidated'],decode(repeat('00',48),'hex'))`); err != nil {
		t.Fatal(err)
	}
	trace := new(fanoutTransactionTrace)
	config = config.Copy()
	config.MaxConns = 1
	config.ConnConfig.Tracer = trace
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return fanoutTransactionFixture{ctx: ctx, observer: observer, pool: pool,
		store: NewStore(pool, identity.New(pool), nil), trace: trace}
}

func (fixture fanoutTransactionFixture) enabled(t *testing.T) bool {
	t.Helper()
	var enabled bool
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT enabled FROM notification_registrations WHERE id=$1`, fanoutTransactionRegistration).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	return enabled
}

func (fixture fanoutTransactionFixture) attachCatalog(t *testing.T) {
	t.Helper()
	// The reserved owner consumes one observer connection; the second remains
	// available for the retry's current catalog-reference authorization.
	catalog, err := library.New(fixture.observer, media.Prober{}, []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := catalog.Close(cleanup); err != nil {
			t.Errorf("close notification retry catalog: %v", err)
		}
	})
	fixture.store.catalog = catalog
}

func TestNotificationFanoutReusesSingleConnection(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	var isolation string
	if err := fixture.pool.QueryRow(fixture.ctx, "SHOW default_transaction_isolation").Scan(&isolation); err != nil || isolation != "repeatable read" {
		t.Fatalf("fixture default isolation = %q, error %v", isolation, err)
	}
	ctx, cancel := context.WithTimeout(fixture.ctx, 3*time.Second)
	defer cancel()
	if err := fixture.store.fanoutRegistration(ctx, fanoutTransactionRegistration); err != nil {
		t.Fatalf("fanout required another application connection: %v", err)
	}
	if !fixture.trace.fired || !fixture.enabled(t) || fixture.pool.Stat().AcquiredConns() != 0 {
		t.Fatal("empty fanout changed registration authority or retained its connection")
	}
	var cursor int64
	var deliveries int
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT source_cursor,(SELECT count(*) FROM notification_deliveries)
		FROM notification_registrations WHERE id=$1`, fanoutTransactionRegistration).Scan(&cursor, &deliveries); err != nil || cursor != 0 || deliveries != 0 {
		t.Fatalf("empty fanout changed cursor or deliveries: %d/%d, %v", cursor, deliveries, err)
	}
}

func TestNotificationFanoutRevalidationUsesFreshReadCommittedSnapshot(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	for _, test := range []struct {
		name      string
		statement string
	}{
		{"revoked", `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='notification-session'`},
		{"expired", `UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id='notification-session'`},
		{"disabled", `UPDATE users SET is_disabled=true WHERE id='notification-user'`},
		{"remote access", `UPDATE users SET policy='{"EnableRemoteAccess":false}' WHERE id='notification-user'`},
		{"device access", `UPDATE users SET policy='{"EnableAllDevices":false,"EnabledDevices":["other-device"]}' WHERE id='notification-user'`},
		{"local login", `UPDATE sessions SET local_auth=true WHERE id='notification-session'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := fixture.observer.Exec(fixture.ctx, `UPDATE users SET is_disabled=false,policy='{}' WHERE id='notification-user';
				UPDATE sessions SET revoked_at=NULL,expires_at=clock_timestamp()+interval '1 hour',local_auth=false WHERE id='notification-session';
				UPDATE notification_registrations SET enabled=true`); err != nil {
				t.Fatal(err)
			}
			fixture.trace.fired, fixture.trace.err = false, nil
			fixture.trace.afterTarget = func(ctx context.Context) error {
				_, err := fixture.observer.Exec(ctx, test.statement)
				return err
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 3*time.Second)
			defer cancel()
			if err := fixture.store.fanoutRegistration(ctx, fanoutTransactionRegistration); err != nil {
				t.Fatal(err)
			}
			if !fixture.trace.fired || fixture.trace.err != nil || fixture.enabled(t) {
				t.Fatalf("fanout reused authority observed before the committed change: fired=%t, error=%v", fixture.trace.fired, fixture.trace.err)
			}
		})
	}
}

func TestNotificationFanoutCanceledDisableRollsBack(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	blocker, err := fixture.observer.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var id string
	var blockerPID int32
	if err := blocker.QueryRow(fixture.ctx, `SELECT id,pg_backend_pid() FROM notification_registrations WHERE id=$1 FOR UPDATE`, fanoutTransactionRegistration).Scan(&id, &blockerPID); err != nil {
		t.Fatal(err)
	}
	fixture.trace.afterTarget = func(ctx context.Context) error {
		_, err := fixture.observer.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id='notification-session'`)
		return err
	}
	started := make(chan int32, 1)
	fixture.trace.disableStarted = started
	ctx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- fixture.store.fanoutRegistration(ctx, fanoutTransactionRegistration) }()
	var applicationPID int32
	select {
	case applicationPID = <-started:
	case err := <-finished:
		t.Fatalf("fanout did not reach the blocked registration update: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("fanout did not reach the registration update")
	}
	// A query-start callback precedes execution. Observe the actual PostgreSQL
	// lock wait before canceling, rather than canceling an unsent UPDATE.
	wait, stopWait := context.WithTimeout(fixture.ctx, 5*time.Second)
	defer stopWait()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var waiting bool
		if err := fixture.observer.QueryRow(wait, `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, blockerPID, applicationPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-poll.C:
		case <-wait.Done():
			t.Fatal("registration update did not wait for the fixture row lock")
		}
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("canceled registration update = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled fanout retained its application connection")
	}
	if err := blocker.Rollback(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	if fixture.trace.err != nil || !fixture.enabled(t) {
		t.Fatalf("failed fanout committed registration disablement: %v", fixture.trace.err)
	}
	fixture.trace.afterTarget = nil
	if _, err := fixture.observer.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=NULL WHERE id='notification-session'`); err != nil {
		t.Fatal(err)
	}
	check, stop := context.WithTimeout(fixture.ctx, 3*time.Second)
	defer stop()
	if err := fixture.store.fanoutRegistration(check, fanoutTransactionRegistration); err != nil {
		t.Fatalf("rolled-back fanout left the single connection unusable: %v", err)
	}
}

func TestNotificationFanoutRetainsRegistrationAndTransportRevisionFences(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	// A filtered source still requires an atomic cursor advance. It avoids a
	// catalog dependency while exercising the second transaction's version fence.
	if _, err := fixture.observer.Exec(fixture.ctx, `INSERT INTO notification_source_events(id,sequence,kind,user_id,refs)
		VALUES('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'UserDataInvalidated','notification-user','[]')`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		statement string
	}{
		{"registration", `UPDATE notification_registrations SET revision=revision+1`},
		{"transport", `UPDATE notification_transport SET revision=revision+1 WHERE id=1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture.trace.fired, fixture.trace.err = false, nil
			fixture.trace.afterTarget = func(ctx context.Context) error {
				_, err := fixture.observer.Exec(ctx, test.statement)
				return err
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 3*time.Second)
			defer cancel()
			if err := fixture.store.fanoutRegistration(ctx, fanoutTransactionRegistration); err != nil {
				t.Fatal(err)
			}
			var cursor int64
			var deliveries int
			if err := fixture.observer.QueryRow(fixture.ctx, `SELECT source_cursor,(SELECT count(*) FROM notification_deliveries)
				FROM notification_registrations WHERE id=$1`, fanoutTransactionRegistration).Scan(&cursor, &deliveries); err != nil {
				t.Fatal(err)
			}
			if !fixture.trace.fired || fixture.trace.err != nil || cursor != 0 || deliveries != 0 {
				t.Fatalf("changed generation advanced stale fanout: cursor=%d, deliveries=%d, error=%v", cursor, deliveries, fixture.trace.err)
			}
		})
	}
	fixture.trace.afterTarget = nil
	if err := fixture.store.fanoutRegistration(fixture.ctx, fanoutTransactionRegistration); err != nil {
		t.Fatal(err)
	}
	var cursor int64
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT source_cursor FROM notification_registrations WHERE id=$1`, fanoutTransactionRegistration).Scan(&cursor); err != nil || cursor != 1 {
		t.Fatalf("stable filtered fanout did not advance its cursor: %d, %v", cursor, err)
	}
}

func TestNotificationFanoutRetriesSubscriptionChangeBetweenCursorAndTarget(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	writer := NewStore(fixture.observer, identity.New(fixture.observer), nil)
	actor := identity.Principal{Kind: "emby", SessionID: "notification-session",
		User: identity.User{ID: "notification-user"}, PeerIP: "192.0.2.20"}
	fixture.trace.afterCursor = func(ctx context.Context) error {
		// PutRegistration advances the revision but leaves the already-caught-up
		// cursor at zero. The first fanout read still holds the old event list.
		_, err := writer.PutRegistration(ctx, actor, RegistrationUpdate{Revision: "1", Transport: Transport,
			EventIds: []string{"UserDataInvalidated"}})
		return err
	}
	fixture.trace.afterTarget = func(ctx context.Context) error {
		// Publish only after readTarget has observed the new registration. RC can
		// see this source, while an old event list would incorrectly discard it.
		return notificationjournal.RecordUserResync(ctx, fixture.observer, actor.User.ID)
	}
	ctx, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
	defer cancel()
	if err := fixture.store.fanoutRegistration(ctx, fanoutTransactionRegistration); err != nil {
		t.Fatal(err)
	}
	if !fixture.trace.cursorFired || !fixture.trace.fired || fixture.trace.err != nil {
		t.Fatalf("subscription/source interleaving did not complete: cursor=%t, target=%t, error=%v",
			fixture.trace.cursorFired, fixture.trace.fired, fixture.trace.err)
	}
	var cursor, revision, sequence int64
	var deliveries int
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT source_cursor,revision,
		(SELECT sequence FROM notification_journal_state WHERE id=1),(SELECT count(*) FROM notification_deliveries)
		FROM notification_registrations WHERE id=$1`, fanoutTransactionRegistration).Scan(&cursor, &revision, &sequence, &deliveries); err != nil {
		t.Fatal(err)
	}
	if cursor != 0 || revision != 2 || sequence != 1 || deliveries != 0 {
		t.Fatalf("mixed subscription snapshots consumed the new event: cursor=%d, revision=%d, sequence=%d, deliveries=%d",
			cursor, revision, sequence, deliveries)
	}
	fixture.trace.afterCursor, fixture.trace.afterTarget = nil, nil
	fixture.attachCatalog(t)
	if err := fixture.store.fanoutRegistration(fixture.ctx, fanoutTransactionRegistration); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT r.source_cursor,d.registration_revision,d.source_sequence,d.kind
		FROM notification_registrations r JOIN notification_deliveries d ON d.registration_id=r.id WHERE r.id=$1`,
		fanoutTransactionRegistration).Scan(&cursor, &revision, &sequence, &kind); err != nil {
		t.Fatal(err)
	}
	if cursor != 1 || revision != 2 || sequence != 1 || kind != "ResyncRequired" {
		t.Fatalf("retry did not deliver the newly subscribed event: cursor=%d, revision=%d, sequence=%d, kind=%q",
			cursor, revision, sequence, kind)
	}
}

func TestNotificationFanoutRejectedPeerCannotDisableNewRegistration(t *testing.T) {
	fixture := newFanoutTransactionFixture(t)
	writer := NewStore(fixture.observer, identity.New(fixture.observer), nil)
	actor := identity.Principal{Kind: "emby", SessionID: "notification-session",
		User: identity.User{ID: "notification-user"}, PeerIP: "127.0.0.1"}
	fixture.trace.afterTarget = func(ctx context.Context) error {
		if _, err := fixture.observer.Exec(ctx, `UPDATE users SET policy='{"EnableRemoteAccess":false}' WHERE id=$1`, actor.User.ID); err != nil {
			return err
		}
		// The old target retained its remote peer. A legitimate local request
		// replaces that registration before the old principal is revalidated.
		if _, err := writer.PutRegistration(ctx, actor, RegistrationUpdate{Revision: "1", Transport: Transport,
			EventIds: []string{"UserDataInvalidated"}}); err != nil {
			return err
		}
		return notificationjournal.RecordUserResync(ctx, fixture.observer, actor.User.ID)
	}
	ctx, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
	defer cancel()
	if err := fixture.store.fanoutRegistration(ctx, fanoutTransactionRegistration); err != nil {
		t.Fatal(err)
	}
	if !fixture.trace.fired || fixture.trace.err != nil || !fixture.enabled(t) {
		t.Fatalf("rejected old peer disabled the new local registration: fired=%t, error=%v", fixture.trace.fired, fixture.trace.err)
	}
	var cursor, revision int64
	var peer string
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT source_cursor,revision,peer_ip FROM notification_registrations WHERE id=$1`,
		fanoutTransactionRegistration).Scan(&cursor, &revision, &peer); err != nil {
		t.Fatal(err)
	}
	if cursor != 0 || revision != 2 || peer != actor.PeerIP {
		t.Fatalf("fanout changed the replacement registration: cursor=%d, revision=%d, peer=%q", cursor, revision, peer)
	}
	fixture.trace.afterTarget = nil
	fixture.attachCatalog(t)
	if err := fixture.store.fanoutRegistration(fixture.ctx, fanoutTransactionRegistration); err != nil {
		t.Fatal(err)
	}
	var sequence int64
	var kind string
	if err := fixture.observer.QueryRow(fixture.ctx, `SELECT r.source_cursor,d.registration_revision,d.source_sequence,d.kind
		FROM notification_registrations r JOIN notification_deliveries d ON d.registration_id=r.id WHERE r.id=$1`,
		fanoutTransactionRegistration).Scan(&cursor, &revision, &sequence, &kind); err != nil {
		t.Fatal(err)
	}
	if !fixture.enabled(t) || cursor != 1 || revision != 2 || sequence != 1 || kind != "ResyncRequired" {
		t.Fatalf("new local registration could not resume fanout: cursor=%d, revision=%d, sequence=%d, kind=%q",
			cursor, revision, sequence, kind)
	}
}
