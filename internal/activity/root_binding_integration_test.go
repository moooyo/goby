package activity_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/activity"
)

func rootBindingIntegrationEvent() activity.Event {
	return activity.Event{Action: activity.ActionLibraryRootBindingUpdated, Severity: activity.SeverityInfo,
		Source: activity.SourceNative, Actor: activity.Actor{Kind: activity.ActorUser, ID: "activity-user", CredentialID: "activity-auth"},
		Resource: activity.Resource{Kind: activity.ResourceLibraryRoot, ID: "historical-root:1"}, RequestID: "root-binding-request",
		PreviousRevision: math.MaxInt64 - 1, Revision: math.MaxInt64, ObservationFingerprint: strings.Repeat("ab", 32)}
}

func TestRootBindingActivitySharesRollbackAndPreservesQueriedInt64Facts(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	seedActivityUser(t, ctx, pool)
	event := rootBindingIntegrationEvent()
	rolledBack := beginActivityTransaction(t, ctx, pool)
	if _, err := rolledBack.Exec(ctx, `UPDATE users SET name='Rejected business witness', management_revision=2 WHERE id='activity-user'`); err != nil {
		t.Fatal(err)
	}
	if err := activity.Record(ctx, rolledBack, event); err != nil {
		t.Fatal(err)
	}
	var previous, revision int64
	var fingerprint string
	if err := rolledBack.QueryRow(ctx, `SELECT previous_revision,revision,observation_fingerprint FROM activity_entries
		WHERE action='library.root_binding.updated'`).Scan(&previous, &revision, &fingerprint); err != nil ||
		previous != event.PreviousRevision || revision != event.Revision || fingerprint != event.ObservationFingerprint {
		t.Fatalf("the pending transaction lost exact root binding audit columns: previous=%d revision=%d error=%v", previous, revision, err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
	if err := rolledBack.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
	committed := beginActivityTransaction(t, ctx, pool)
	if _, err := committed.Exec(ctx, `UPDATE users SET name='Committed business witness', management_revision=2 WHERE id='activity-user'`); err != nil {
		t.Fatal(err)
	}
	if err := activity.RecordOwned(activityOwnedExecutor{ctx: ctx, tx: committed}, event); err != nil {
		t.Fatal(err)
	}
	if err := committed.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertActivityBusinessState(t, ctx, pool, "Committed business witness", 2, 1)
	page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionLibraryRootBindingUpdated, ActorID: event.Actor.ID})
	if page.TotalRecordCount != 1 || len(page.Items) != 1 {
		t.Fatal("the committed binding event was missing from its authorized action/actor query")
	}
	entry := page.Items[0]
	if entry.Action != event.Action || entry.Source != event.Source || entry.Actor != event.Actor || entry.Resource != event.Resource ||
		entry.RequestID != event.RequestID || entry.PreviousRevision != event.PreviousRevision || entry.Revision != event.Revision ||
		entry.ObservationFingerprint != event.ObservationFingerprint || entry.Count != 0 || entry.State != "" || len(entry.ChangedFields) != 0 {
		t.Fatal("the root binding query did not preserve the exact committed facts")
	}
	want := "A registered media directory binding changed from revision 9223372036854775806 to 9223372036854775807. Observation fingerprint: " + event.ObservationFingerprint + "."
	if entry.Name != "Library root binding updated" || entry.Overview != want || entry.ActorName != "Committed business witness" {
		t.Fatal("the root binding activity summary lost its safe transition facts or current actor enrichment")
	}
	insertActivityEvent(t, ctx, pool, activityTestEvent("legacy-action-after-binding"))
	legacy := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionUserUpdated})
	if legacy.TotalRecordCount != 1 || len(legacy.Items) != 1 || legacy.Items[0].PreviousRevision != 0 || legacy.Items[0].ObservationFingerprint != "" {
		t.Fatal("an unrelated committed action acquired root binding audit facts")
	}
}

func TestRootBindingActivityDeferredCommitFailureRollsBackBusinessAndAudit(t *testing.T) {
	ctx, pool := activityIntegrationPool(t, true)
	seedActivityUser(t, ctx, pool)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_root_binding_audit_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.action='library.root_binding.updated' THEN RAISE EXCEPTION 'binding audit commit rejected'; END IF; RETURN NEW; END; $$;
		CREATE CONSTRAINT TRIGGER reject_root_binding_audit_commit AFTER INSERT ON activity_entries
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_root_binding_audit_commit()`); err != nil {
		t.Fatal(err)
	}
	tx := beginActivityTransaction(t, ctx, pool)
	if _, err := tx.Exec(ctx, `UPDATE users SET name='Must not survive commit', management_revision=2 WHERE id='activity-user'`); err != nil {
		t.Fatal(err)
	}
	if err := activity.RecordOwned(activityOwnedExecutor{ctx: ctx, tx: tx}, rootBindingIntegrationEvent()); err != nil {
		t.Fatal("the deferred failure happened before the owning transaction committed")
	}
	var databaseError *pgconn.PgError
	if err := tx.Commit(ctx); !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("the deferred audit failure did not reject the complete transaction: %v", err)
	}
	assertActivityBusinessState(t, ctx, pool, "Original Administrator", 1, 0)
	page := queryActivityPage(t, ctx, pool, activity.QueryOptions{Action: activity.ActionLibraryRootBindingUpdated})
	if page.TotalRecordCount != 0 || len(page.Items) != 0 {
		t.Fatal("a failed commit retained a root binding activity summary")
	}
}
