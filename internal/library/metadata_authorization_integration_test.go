package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

// The observer delays the real post-edit auxiliary query until PostgreSQL says
// the independently stored credential has expired. It neither changes business
// data nor substitutes a transaction, authorization result, or notification.
type metadataNotificationExpiryTracer struct {
	observer  *pgxpool.Pool
	sessionID string
	armed     atomic.Bool
	snapshots atomic.Int32
	waited    bool
	err       error
}

func (trace *metadataNotificationExpiryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.armed.Load() || !strings.HasPrefix(data.SQL, "WITH seeds AS (SELECT unnest($1::text[]) id), resources AS (") {
		return ctx
	}
	if trace.snapshots.Add(1) != 2 {
		return ctx
	}
	active := func() (bool, error) {
		var current bool
		err := trace.observer.QueryRow(ctx, "SELECT expires_at > clock_timestamp() FROM sessions WHERE id = $1", trace.sessionID).Scan(&current)
		return current, err
	}
	current, err := active()
	if err != nil || !current {
		trace.err = fmt.Errorf("the notification query did not begin with a live credential: active=%t, error=%v", current, err)
		return ctx
	}
	trace.waited = true
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			trace.err = ctx.Err()
			return ctx
		case <-ticker.C:
			current, err = active()
			if err != nil {
				trace.err = err
				return ctx
			}
			if !current {
				return ctx
			}
		}
	}
}

func (*metadataNotificationExpiryTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestStoreMetadataFinalAuthorizationFollowsNotificationQueries(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	trace := &metadataNotificationExpiryTracer{observer: fixture.pool}
	config := fixture.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create traced metadata owner pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	root := t.TempDir()
	store, err := New(pool, &libraryFixtureProber{}, []string{root})
	if err != nil {
		t.Fatalf("create traced metadata store: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close traced metadata store: %v", err)
		}
	})
	actor := metadataEditTestActor(t, ctx, fixture.pool, "metadata-notification-expiry-editor")
	detail := catalogAuditMetadataFixture(t, ctx, fixture.pool, store, root, actor)
	notifications := catalogChangesTestListener(t, store)
	before := catalogAuditSnapshot(t, ctx, fixture.pool)
	if _, err := fixture.pool.Exec(ctx, `UPDATE sessions SET expires_at = clock_timestamp() + interval '5 seconds'
		WHERE id = $1`, actor.SessionID); err != nil {
		t.Fatalf("bound the stored metadata credential lifetime: %v", err)
	}
	trace.sessionID = actor.SessionID
	trace.armed.Store(true)
	_, err = store.UpdateItemMetadata(ctx, actor, detail.ItemID, MetadataEdit{
		Revision: detail.Revision,
		Overrides: map[string]json.RawMessage{
			"Name":   json.RawMessage(`"Uncommitted after expiry"`),
			"Genres": json.RawMessage(`["Uncommitted expiry genre"]`),
		},
		LockedFields: []string{},
	})
	trace.armed.Store(false)
	if trace.err != nil || !trace.waited || trace.snapshots.Load() != 2 {
		t.Fatalf("the real post-edit notification query did not cross credential expiry: snapshots=%d, waited=%t, error=%v",
			trace.snapshots.Load(), trace.waited, trace.err)
	}
	if !errors.Is(err, ErrForbidden) || !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("metadata committed after expiry during notification collection: %v", err)
	}
	if after := catalogAuditSnapshot(t, ctx, fixture.pool); after != before {
		t.Error("late authorization failure retained catalog values, metadata revision, entities, or activity")
	}
	assertNoCatalogTestNotification(t, notifications)
	// A fresh current credential can retry the unchanged revision after rollback.
	if _, err := fixture.pool.Exec(ctx, `UPDATE sessions SET expires_at = clock_timestamp() + interval '1 day'
		WHERE id = $1`, actor.SessionID); err != nil {
		t.Fatalf("renew the fixture credential: %v", err)
	}
	updated := metadataEditTestUpdate(t, ctx, store, actor, detail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Committed retry"`)}, nil)
	if updated.Effective.Name != "Committed retry" {
		t.Fatal("retry after rollback did not expose its committed effective name")
	}
	nextCatalogTestNotification(t, notifications)
	assertNoCatalogTestNotification(t, notifications)
}
