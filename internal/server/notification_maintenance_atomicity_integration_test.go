//go:build linux

package server

import (
	"context"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/notifications"
)

func TestNotificationMaintenanceRollsBackTerminalChangesWhenHistoryPruningFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		injection string
	}{
		{name: "statement_failure", injection: `RAISE EXCEPTION 'injected notification history prune failure';`},
		{name: "context_deadline", injection: `PERFORM pg_sleep(30);`},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
			configureNotificationFixture(t, f, cookie, csrf)
			registration := registerNotificationFixture(t, f, headers)["Id"].(string)
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,state)
				SELECT md5('notification-atomic-history-'||n::text),$1,1,2,n,'Test','[]'::jsonb,'delivered' FROM generate_series(1,32)n`, registration); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,attempts,due_at)
				VALUES(md5('notification-atomic-exhausted'),$1,1,2,33,'CatalogInvalidated',
				'[{"Kind":"Item","Id":"pending-item","LibraryId":"pending-library"}]'::jsonb,5,clock_timestamp()+interval '1 hour')`, registration); err != nil {
				t.Fatal(err)
			}
			// Sequence advances survive rollback and identify the actual targeted
			// prune, including a canceled statement. The global startup fallback
			// sees the restored pending row and does not trigger this fault.
			if _, err := f.pool.Exec(f.ctx, `CREATE SEQUENCE notification_atomic_prune_attempts;
				CREATE TABLE notification_atomic_prune_fault(enabled boolean NOT NULL);
				INSERT INTO notification_atomic_prune_fault VALUES(true);
				CREATE FUNCTION notification_atomic_prune_failure() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
				    IF (SELECT enabled FROM notification_atomic_prune_fault)
				       AND EXISTS(SELECT 1 FROM notification_deliveries WHERE id=md5('notification-atomic-exhausted') AND state='failed') THEN
				        PERFORM nextval('notification_atomic_prune_attempts');
				        `+test.injection+`
				    END IF;
				    RETURN NULL;
				END $$;
				CREATE TRIGGER notification_atomic_prune_failure BEFORE DELETE ON notification_deliveries
				FOR EACH STATEMENT EXECUTE FUNCTION notification_atomic_prune_failure()`); err != nil {
				t.Fatal(err)
			}
			f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore)
			defer closeNotificationFixtureRuntime(t, f)
			waitNotificationCondition(t, f, "maintenance did not reach the injected history prune failure", func() bool {
				var attempted bool
				if err := f.pool.QueryRow(f.ctx, `SELECT is_called FROM notification_atomic_prune_attempts`).Scan(&attempted); err != nil {
					t.Fatal(err)
				}
				return attempted
			})

			// Wait on the transition's row lock so a still-running prune cannot
			// produce a false pass from an ordinary pre-commit snapshot. The
			// runtime's five-second context must cancel the sleeping variant.
			ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
			defer cancel()
			observation, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer observation.Rollback(ctx)
			var state, outcome string
			var attempts, references, terminal int
			if err := observation.QueryRow(ctx, `SELECT state,outcome,attempts,jsonb_array_length(refs)
				FROM notification_deliveries WHERE id=md5('notification-atomic-exhausted') FOR SHARE`).
				Scan(&state, &outcome, &attempts, &references); err != nil {
				t.Fatalf("history prune did not release its transition after failure: %v", err)
			}
			if err := observation.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE registration_id=$1 AND state NOT IN('pending','sending')`, registration).Scan(&terminal); err != nil {
				t.Fatal(err)
			}
			if state != "pending" || outcome != "" || attempts != 5 || references != 1 || terminal != 32 {
				t.Fatalf("prune failure committed terminal state: state=%s outcome=%s attempts=%d refs=%d terminal=%d", state, outcome, attempts, references, terminal)
			}
			// Disable the fault while retaining the row lock. A queued maintenance
			// pass cannot transition the delivery again until this commit exposes
			// the repaired prune path, so the next retry does not depend on timing.
			if _, err := observation.Exec(ctx, `UPDATE notification_atomic_prune_fault SET enabled=false`); err != nil {
				t.Fatal(err)
			}
			if err := observation.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			waitNotificationCondition(t, f, "maintenance did not retry and atomically bound history after repair", func() bool {
				var repaired bool
				if err := f.pool.QueryRow(f.ctx, `SELECT
					EXISTS(SELECT 1 FROM notification_deliveries WHERE id=md5('notification-atomic-exhausted') AND state='failed' AND outcome='retry_exhausted' AND refs='[]'::jsonb)
					AND (SELECT count(*) FROM notification_deliveries WHERE registration_id=$1 AND state NOT IN('pending','sending'))=32`, registration).Scan(&repaired); err != nil {
					t.Fatal(err)
				}
				return repaired
			})
		})
	}
}
