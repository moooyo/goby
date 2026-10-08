//go:build linux

package server

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/notificationjournal"
	"github.com/moooyo/goby/internal/notifications"
)

func closeNotificationFixtureRuntime(t *testing.T, f *serverFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.app.notificationRuntime.Close(ctx); err != nil {
		t.Fatalf("close notification runtime: %v", err)
	}
}

func TestNotificationCapacityCommitsPrefixAndDrainsAcrossRestart(t *testing.T) {
	for _, test := range []struct {
		name       string
		references int
		longIDs    bool
		restart    bool
	}{
		// Coalescing must remain over capacity even after a concurrent sender
		// owns the first row and only seven pending rows can be combined.
		{name: "reference count", references: 585, restart: true},
		{name: "serialized bytes after coalescing", references: 128, longIDs: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
			started := make(chan struct{})
			firstEnded := make(chan struct{})
			release := make(chan struct{})
			received := make(chan notifications.Payload, 16)
			var first sync.Once
			receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload notifications.Payload
				if json.NewDecoder(r.Body).Decode(&payload) != nil {
					http.Error(w, "Invalid notification", http.StatusBadRequest)
					return
				}
				initial := false
				first.Do(func() { initial = true; close(started) })
				if initial {
					defer close(firstEnded)
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				received <- payload
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"EventId": payload.EventID, "Accepted": true})
			}))
			defer receiver.Close()
			defer closeNotificationFixtureRuntime(t, f)
			expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notifications", map[string]any{
				"Revision": "1", "Enabled": true, "Endpoint": receiver.URL + "/events",
				"AllowedNetworks": []string{"127.0.0.1/32"}, "ReceiverCredential": "receiver-secret-sentinel-48",
			}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie), http.StatusOK)
			registered := registerNotificationFixture(t, f, headers)
			registration := registered["Id"].(string)
			libraryID := "notification-prefix-library"
			idPrefix := "notification-prefix-item-"
			if test.longIDs {
				libraryID = strings.Repeat("l", 256)
				idPrefix = strings.Repeat("i", 252)
			}
			tx, err := f.pool.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err = tx.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES($1,'Notification prefix','movies')`, libraryID); err != nil {
				t.Fatal(err)
			}
			var before int64
			if err = tx.QueryRow(f.ctx, `SELECT source_cursor FROM notification_registrations WHERE id=$1`, registration).Scan(&before); err != nil {
				t.Fatal(err)
			}
			combined := []notificationjournal.Reference{}
			for batch := 0; batch < 9; batch++ {
				refs := make([]notificationjournal.Reference, test.references)
				ids := make([]string, test.references)
				for index := range refs {
					ids[index] = fmt.Sprintf("%s%04d", idPrefix, batch*test.references+index)
					refs[index] = notificationjournal.Reference{Kind: "Item", ID: ids[index], LibraryID: libraryID}
				}
				if _, err = tx.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type) SELECT id,$2,id,id,'Movie' FROM unnest($1::text[]) id`, ids, libraryID); err != nil {
					t.Fatal(err)
				}
				if err = notificationjournal.RecordCatalog(f.ctx, tx, notificationjournal.NewID(), refs, false); err != nil {
					t.Fatalf("legal source %d was rejected: %v", batch, err)
				}
				combined = append(combined, refs...)
			}
			if test.longIDs {
				raw, err := json.Marshal(combined)
				if err != nil || len(raw) <= 524288 || len(combined) > 4096 {
					t.Fatal("fixture does not reach the post-coalescing byte limit")
				}
			}
			if err = tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			roots := x509.NewCertPool()
			roots.AddCert(receiver.Certificate())
			start := func() {
				f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore, notifications.RuntimeOptions{RootCAs: roots})
			}
			start()
			select {
			case <-started:
			case <-time.After(15 * time.Second):
				t.Fatal("a legal source page blocked delivery instead of committing a fitting prefix")
			}
			var cursor int64
			var queued, cancelled, retainedReferences int
			if err = f.pool.QueryRow(f.ctx, `SELECT source_cursor,
				(SELECT count(*) FROM notification_deliveries WHERE registration_id=$1 AND state IN ('pending','sending')),
				(SELECT count(*) FROM notification_deliveries WHERE registration_id=$1 AND state='cancelled'),
				(SELECT COALESCE(sum(jsonb_array_length(refs)),0) FROM notification_deliveries WHERE registration_id=$1)
				FROM notification_registrations WHERE id=$1`, registration).Scan(&cursor, &queued, &cancelled, &retainedReferences); err != nil {
				t.Fatal(err)
			}
			if cursor != before+8 || queued != 8 || cancelled != 0 || retainedReferences != 8*test.references {
				t.Fatalf("failed coalescing changed the committed prefix: cursor=%d queued=%d cancelled=%d refs=%d", cursor, queued, cancelled, retainedReferences)
			}
			if test.restart {
				closeNotificationFixtureRuntime(t, f)
				select {
				case <-firstEnded:
				case <-time.After(3 * time.Second):
					t.Fatal("receiver did not observe retirement before restarting the worker")
				}
			}
			close(release)
			if test.restart {
				start()
			}
			for index := 1; index <= 9; index++ {
				var payload notifications.Payload
				select {
				case payload = <-received:
				case <-time.After(15 * time.Second):
					t.Fatalf("delivery %d did not drain after backpressure", index)
				}
				var sequence int64
				if err = f.pool.QueryRow(f.ctx, `SELECT source_sequence FROM notification_deliveries WHERE id=$1`, payload.EventID).Scan(&sequence); err != nil || sequence != before+int64(index) {
					t.Fatalf("delivery order changed at %d: sequence=%d error=%v", index, sequence, err)
				}
			}
			finished := time.After(5 * time.Second)
			for {
				var delivered int
				if err = f.pool.QueryRow(f.ctx, `SELECT source_cursor,
					(SELECT count(*) FROM notification_deliveries WHERE registration_id=$1 AND state='delivered')
					FROM notification_registrations WHERE id=$1`, registration).Scan(&cursor, &delivered); err != nil {
					t.Fatal(err)
				}
				if cursor == before+9 && delivered == 9 {
					break
				}
				select {
				case <-finished:
					var outcomes string
					readErr := f.pool.QueryRow(f.ctx, `SELECT COALESCE(string_agg(state||'/'||outcome||':'||total::text,', ' ORDER BY state,outcome),'')
						FROM (SELECT state,outcome,count(*) AS total FROM notification_deliveries WHERE registration_id=$1 GROUP BY state,outcome) summary`, registration).Scan(&outcomes)
					t.Fatalf("remaining source did not finish draining: cursor=%d delivered=%d outcomes=%s diagnostic_error=%v", cursor, delivered, outcomes, readErr)
				case <-time.After(25 * time.Millisecond):
				}
			}
		})
	}
}

func TestNotificationDisabledTransportDoesNotWaitForJournalLock(t *testing.T) {
	f, _, _, _, _ := notificationHTTPFixture(t)
	locked, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(f.ctx)
	if _, err = locked.Exec(f.ctx, `SELECT id FROM notification_journal_state WHERE id=1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	if err = notificationjournal.RecordCatalog(ctx, f.pool, notificationjournal.NewID(), []notificationjournal.Reference{{Kind: "Item", ID: "disabled-item", LibraryID: "disabled-library"}}, false); err != nil {
		t.Fatalf("disabled transport waited for unrelated journal ownership: %v", err)
	}
	var events int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM notification_source_events`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("disabled transport retained source work: count=%d error=%v", events, err)
	}
}

func TestNotificationFilteredSourceAdvancesOnceAndIdlePagesDoNotWrite(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	configureNotificationFixture(t, f, cookie, csrf)
	registration := registerNotificationFixture(t, f, headers)["Id"].(string)
	if _, err := f.pool.Exec(f.ctx, `CREATE TABLE notification_cursor_writes(count integer NOT NULL);
		INSERT INTO notification_cursor_writes VALUES(0);
		CREATE FUNCTION notification_count_cursor_write() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN UPDATE notification_cursor_writes SET count=count+1; RETURN NEW; END $$;
		CREATE TRIGGER notification_cursor_write AFTER UPDATE OF source_cursor ON notification_registrations
		FOR EACH ROW EXECUTE FUNCTION notification_count_cursor_write()`); err != nil {
		t.Fatal(err)
	}
	if err := notificationjournal.RecordCatalog(f.ctx, f.pool, notificationjournal.NewID(), []notificationjournal.Reference{{Kind: "Item", ID: "missing-item", LibraryID: "missing-library"}}, false); err != nil {
		t.Fatal(err)
	}
	var sequence int64
	if err := f.pool.QueryRow(f.ctx, `SELECT max(sequence) FROM notification_source_events`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore)
	defer closeNotificationFixtureRuntime(t, f)
	deadline := time.After(10 * time.Second)
	for {
		var cursor int64
		if err := f.pool.QueryRow(f.ctx, `SELECT source_cursor FROM notification_registrations WHERE id=$1`, registration).Scan(&cursor); err != nil {
			t.Fatal(err)
		}
		if cursor == sequence {
			break
		}
		select {
		case <-deadline:
			t.Fatal("filtered source did not advance the private cursor")
		case <-time.After(25 * time.Millisecond):
		}
	}
	select {
	case <-time.After(1100 * time.Millisecond):
	case <-f.ctx.Done():
		t.Fatal("fixture ended before idle fanout observation")
	}
	closeNotificationFixtureRuntime(t, f)
	var writes, deliveries int
	if err := f.pool.QueryRow(f.ctx, `SELECT count,(SELECT count(*) FROM notification_deliveries) FROM notification_cursor_writes`).Scan(&writes, &deliveries); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || deliveries != 0 {
		t.Fatalf("filtered or idle page wrote unexpected state: cursor writes=%d deliveries=%d", writes, deliveries)
	}
}
