package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

type catalogAuditFact struct {
	source, actorKind, actorID, credentialID string
	resourceKind, resourceID, state, raw     string
	revision                                 int64
	fields                                   []string
}

func catalogAuditFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action, resourceID string) []catalogAuditFact {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT source, actor_kind, actor_id, actor_credential_id,
		resource_kind, resource_id, state, revision, changed_fields, to_jsonb(entry)::text
		FROM activity_entries entry WHERE action = $1 AND resource_id = $2 ORDER BY id`, string(action), resourceID)
	if err != nil {
		t.Fatalf("query catalog activity facts: %v", err)
	}
	defer rows.Close()
	result := make([]catalogAuditFact, 0)
	for rows.Next() {
		var fact catalogAuditFact
		if err := rows.Scan(&fact.source, &fact.actorKind, &fact.actorID, &fact.credentialID,
			&fact.resourceKind, &fact.resourceID, &fact.state, &fact.revision, &fact.fields, &fact.raw); err != nil {
			t.Fatalf("read catalog activity fact: %v", err)
		}
		result = append(result, fact)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("finish catalog activity query: %v", err)
	}
	return result
}

func catalogAuditAssertActor(t *testing.T, fact catalogAuditFact, actor identity.Principal, source activity.Source, resource activity.ResourceKind) {
	t.Helper()
	kind, actorID := string(activity.ActorUser), actor.User.ID
	if source == activity.SourceSystem {
		kind, actorID = string(activity.ActorSystem), ""
	} else if actor.IsApplicationKey() {
		kind, actorID = string(activity.ActorApplicationKey), strconv.FormatInt(actor.ApplicationKeyID, 10)
	}
	if fact.source != string(source) || fact.actorKind != kind || fact.actorID != actorID ||
		fact.credentialID != actor.SessionID || fact.resourceKind != string(resource) {
		t.Errorf("catalog activity attribution = %+v; want source %s, actor %s/%s, credential %s, resource %s",
			fact, source, kind, actorID, actor.SessionID, resource)
	}
}

func catalogAuditOnlyFact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action, resourceID string) catalogAuditFact {
	t.Helper()
	facts := catalogAuditFacts(t, ctx, pool, action, resourceID)
	if len(facts) != 1 {
		t.Fatalf("%s facts for %s = %d, want exactly one", action, resourceID, len(facts))
	}
	return facts[0]
}

func catalogAuditActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, audience identity.AdministratorAudience) identity.Principal {
	t.Helper()
	actor := metadataEditTestActor(t, ctx, pool, "catalog-audit-administrator")
	if audience == identity.AdministratorEmby {
		actor.Kind = "emby"
		if _, err := pool.Exec(ctx, "UPDATE sessions SET kind = 'emby' WHERE id = $1", actor.SessionID); err != nil {
			t.Fatalf("prepare Emby administrator credential: %v", err)
		}
	}
	return actor
}

func catalogAuditApplicationActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool) identity.Principal {
	t.Helper()
	subject := seedCatalogApplicationKey(t, ctx, pool, "catalog-audit-application-credential", true)
	actor := identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: subject.ApplicationCredentialID,
		ClientSessionID: "catalog-audit-application-client"}
	if err := pool.QueryRow(ctx, "SELECT id FROM application_keys WHERE credential_id = $1", actor.SessionID).Scan(&actor.ApplicationKeyID); err != nil {
		t.Fatalf("read application audit actor identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients
		(id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ($1, $2, 'Private audit client', 'private-reported-device', 'Private device name', '1.0')`,
		actor.ClientSessionID, actor.SessionID); err != nil {
		t.Fatalf("seed application audit actor client context: %v", err)
	}
	return actor
}

// Snapshots deliberately include persisted audit facts but exclude sequences:
// PostgreSQL sequence advancement survives transaction rollback by design.
func catalogAuditSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'libraries', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM libraries r),
		'roots', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM library_roots r),
		'items', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM items r),
		'metadata', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY item_id), '[]') FROM item_metadata_state r),
		'catalog_entities', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM catalog_entities r),
		'entities', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY item_id, entity_id, position), '[]') FROM item_entities r),
		'jobs', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM scan_jobs r),
		'activity', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM activity_entries r))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot catalog mutations and audit facts: %v", err)
	}
	return snapshot
}

// The trigger targets one action so scanner cleanup can still commit terminal
// events. Its sequence proves that a rollback actually reached the audit write.
func catalogAuditInstallTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action activity.Action, expireActor bool) {
	t.Helper()
	body := `RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'Injected catalog audit failure';`
	if expireActor {
		body = `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours',
			expires_at = clock_timestamp() - interval '1 hour' WHERE id = NEW.actor_credential_id;`
	}
	statement := fmt.Sprintf(`CREATE SEQUENCE catalog_audit_trigger_hits;
		CREATE FUNCTION catalog_audit_test_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.action = TG_ARGV[0] THEN
				PERFORM nextval('catalog_audit_trigger_hits');
				%s
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER catalog_audit_test_trigger BEFORE INSERT ON activity_entries
		FOR EACH ROW EXECUTE FUNCTION catalog_audit_test_trigger('%s')`, body, action)
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("install catalog audit trigger fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS catalog_audit_test_trigger ON activity_entries;
			DROP FUNCTION IF EXISTS catalog_audit_test_trigger(); DROP SEQUENCE IF EXISTS catalog_audit_trigger_hits`); err != nil {
			t.Errorf("remove catalog audit trigger fixture: %v", err)
		}
	})
}

func catalogAuditAssertTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var called bool
	if err := pool.QueryRow(ctx, "SELECT is_called FROM catalog_audit_trigger_hits").Scan(&called); err != nil || !called {
		t.Fatalf("catalog audit write did not reach its trigger: called = %v, error = %v", called, err)
	}
}

func catalogAuditSeedJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID, status string, cancelRequested bool) string {
	t.Helper()
	id := "catalog-audit-job"
	if _, err := pool.Exec(ctx, `INSERT INTO scan_jobs (id, library_id, status, cancel_requested, started_at, finished_at)
		VALUES ($1, $2, $3, $4, CASE WHEN $3 <> 'Queued' THEN clock_timestamp() END,
		CASE WHEN $3 NOT IN ('Queued', 'Running') THEN clock_timestamp() END)`, id, libraryID, status, cancelRequested); err != nil {
		t.Fatalf("seed catalog scan audit fixture: %v", err)
	}
	return id
}

func catalogAuditMetadataFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, root string, actor identity.Principal) ItemMetadataDetail {
	t.Helper()
	library := libraryIntegrationCreate(t, ctx, store, "Catalog audit metadata", "movies", root)
	const itemID = "catalog-audit-metadata-item"
	source := []byte(`{"Kind":"movie","Name":"Automatic Film","SortName":"automatic film","Overview":"Private automatic plot","Genres":["Original Genre"],"InternalExtension":{"PrivateValue":"opaque-private-value"}}`)
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, parent_id, name, sort_name, overview, type, path, local_metadata)
		VALUES ($1, $2, $2, 'Automatic Film', 'automatic film', 'Private automatic plot', 'Movie', $3, $4)`,
		itemID, library.ID, filepath.Join(root, "Private Film.mp4"), source); err != nil {
		t.Fatalf("seed metadata audit item: %v", err)
	}
	if _, err := pool.Exec(ctx, "SELECT sync_catalog_item_entities($1, $2::jsonb)", itemID, source); err != nil {
		t.Fatalf("seed metadata audit associations: %v", err)
	}
	return metadataEditTestDetail(t, ctx, store, actor, itemID)
}

func TestCatalogAuditAdministratorSourcesAndCancellationReplay(t *testing.T) {
	for _, fixture := range []struct {
		name        string
		audience    identity.AdministratorAudience
		source      activity.Source
		application bool
	}{
		{"native", identity.AdministratorNative, activity.SourceNative, false},
		{"emby", identity.AdministratorEmby, activity.SourceEmby, false},
		{"emby-application", identity.AdministratorEmby, activity.SourceEmby, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			prober := taskScanBlockingProber()
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			var actor identity.Principal
			if fixture.application {
				actor = catalogAuditApplicationActor(t, ctx, pool)
			} else {
				actor = catalogAuditActor(t, ctx, pool, fixture.audience)
			}
			path := libraryIntegrationFile(t, root, "private-catalog/Private Film.mp4", "video:catalog-audit")
			library, err := store.CreateLibraryAsAdministrator(ctx, actor, fixture.audience, "Private library display name", "movies", []string{filepath.Dir(path)})
			if err != nil {
				t.Fatalf("create audited administrator library: %v", err)
			}
			created := catalogAuditOnlyFact(t, ctx, pool, activity.ActionLibraryCreated, library.ID)
			catalogAuditAssertActor(t, created, actor, fixture.source, activity.ResourceLibrary)
			for _, forbidden := range []string{"Private library display name", "Private Film.mp4", "private-catalog"} {
				if strings.Contains(created.raw, forbidden) {
					t.Errorf("library activity retained private input %q", forbidden)
				}
			}
			job, err := store.StartScanAsAdministrator(ctx, actor, fixture.audience, library.ID, ScanOptions{ForceProbe: true})
			if err != nil || !job.ForceProbe {
				t.Fatalf("request audited forced scan: job = %+v, error = %v", job, err)
			}
			taskScanAwaitSignal(t, ctx, prober.entered, "audited manual scan did not enter the probe")
			requested := catalogAuditOnlyFact(t, ctx, pool, activity.ActionScanRequested, job.ID)
			catalogAuditAssertActor(t, requested, actor, fixture.source, activity.ResourceScan)
			if _, err := store.StartScanAsAdministrator(ctx, actor, fixture.audience, library.ID, ScanOptions{}); !errors.Is(err, ErrScanAlreadyActive) {
				t.Errorf("duplicate audited scan admission = %v, want ErrScanAlreadyActive", err)
			}
			catalogAuditOnlyFact(t, ctx, pool, activity.ActionScanRequested, job.ID)
			if err := store.CancelJobAsAdministrator(ctx, actor, fixture.audience, job.ID); err != nil {
				t.Fatalf("cancel audited manual scan: %v", err)
			}
			taskScanAwaitSignal(t, ctx, prober.cancelled, "audited cancellation did not stop the probe")
			libraryIntegrationWaitJob(t, ctx, store, job.ID, "Cancelled")
			cancelled := catalogAuditOnlyFact(t, ctx, pool, activity.ActionScanCancelRequested, job.ID)
			catalogAuditAssertActor(t, cancelled, actor, fixture.source, activity.ResourceScan)
			beforeReplay := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", job.ID)
			if err := store.CancelJobAsAdministrator(ctx, actor, fixture.audience, job.ID); err != nil {
				t.Fatalf("replay completed cancellation: %v", err)
			}
			catalogAuditOnlyFact(t, ctx, pool, activity.ActionScanCancelRequested, job.ID)
			if after := taskScanRowSnapshot(t, ctx, pool, "scan_jobs", job.ID); after != beforeReplay {
				t.Error("terminal cancellation replay changed the persisted scan")
			}
			if err := store.DeleteLibraryAsAdministrator(ctx, actor, fixture.audience, library.ID); err != nil {
				t.Fatalf("remove audited administrator library: %v", err)
			}
			removed := catalogAuditOnlyFact(t, ctx, pool, activity.ActionLibraryRemoved, library.ID)
			catalogAuditAssertActor(t, removed, actor, fixture.source, activity.ResourceLibrary)
			catalogAuditOnlyFact(t, ctx, pool, activity.ActionLibraryCreated, library.ID)
			catalogAuditOnlyFact(t, ctx, pool, activity.ActionScanRequested, job.ID)
			if err := store.DeleteLibraryAsAdministrator(ctx, actor, fixture.audience, library.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("repeat removal = %v, want ErrNotFound", err)
			}
			catalogAuditOnlyFact(t, ctx, pool, activity.ActionLibraryRemoved, library.ID)
		})
	}
}

func TestCatalogAuditLegacyMutationsUseSystemActor(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	library := taskScanCreateLibrary(t, ctx, store, root, "legacy-audit")
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if err := store.CancelJob(ctx, job.ID); err != nil {
		t.Fatalf("replay terminal system cancellation: %v", err)
	}
	if facts := catalogAuditFacts(t, ctx, pool, activity.ActionScanCancelRequested, job.ID); len(facts) != 0 {
		t.Errorf("terminal system cancellation emitted %d facts", len(facts))
	}
	if err := store.DeleteLibrary(ctx, library.ID); err != nil {
		t.Fatalf("remove library through trusted system API: %v", err)
	}
	for _, expected := range []struct {
		action   activity.Action
		resource activity.ResourceKind
		id       string
	}{
		{activity.ActionLibraryCreated, activity.ResourceLibrary, library.ID},
		{activity.ActionLibraryRemoved, activity.ResourceLibrary, library.ID},
		{activity.ActionScanRequested, activity.ResourceScan, job.ID},
		{activity.ActionScanFinished, activity.ResourceScan, job.ID},
	} {
		fact := catalogAuditOnlyFact(t, ctx, pool, expected.action, expected.id)
		catalogAuditAssertActor(t, fact, identity.Principal{}, activity.SourceSystem, expected.resource)
		if expected.action == activity.ActionScanFinished && fact.state != string(activity.StateCompleted) {
			t.Errorf("completed system scan activity state = %q", fact.state)
		}
	}
}

func TestCatalogAuditFailureRollsBackBusinessMutation(t *testing.T) {
	for _, action := range []activity.Action{
		activity.ActionLibraryCreated, activity.ActionLibraryRemoved,
		activity.ActionScanRequested, activity.ActionScanCancelRequested,
	} {
		t.Run(string(action), func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
			var library Library
			var jobID string
			if action != activity.ActionLibraryCreated {
				library = libraryIntegrationCreate(t, ctx, store, "Rollback catalog", "movies", root)
			}
			if action == activity.ActionScanCancelRequested {
				jobID = catalogAuditSeedJob(t, ctx, pool, library.ID, "Queued", false)
			}
			before := catalogAuditSnapshot(t, ctx, pool)
			catalogAuditInstallTrigger(t, ctx, pool, action, false)
			var err error
			switch action {
			case activity.ActionLibraryCreated:
				_, err = store.CreateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, "Uncommitted library", "movies", []string{root})
			case activity.ActionLibraryRemoved:
				err = store.DeleteLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID)
			case activity.ActionScanRequested:
				_, err = store.StartScanAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, ScanOptions{ForceProbe: true})
			case activity.ActionScanCancelRequested:
				err = store.CancelJobAsAdministrator(ctx, actor, identity.AdministratorNative, jobID)
			}
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
				t.Fatalf("audited mutation did not return the injected database failure: %v", err)
			}
			catalogAuditAssertTrigger(t, ctx, pool)
			if after := catalogAuditSnapshot(t, ctx, pool); after != before {
				t.Error("failed audit insert partially committed business state or activity")
			}
			if action == activity.ActionScanRequested {
				store.mu.Lock()
				queued, active := len(store.queue), len(store.active)
				store.mu.Unlock()
				if queued != 0 || active != 0 {
					t.Errorf("failed scan audit published uncommitted work: queued = %d, active = %d", queued, active)
				}
			}
		})
	}
}

func TestCatalogAuditRejectedAdministratorCannotWriteOrEmitActivity(t *testing.T) {
	for _, audience := range []identity.AdministratorAudience{identity.AdministratorNative, identity.AdministratorEmby} {
		for _, state := range []string{"revoked", "expired", "demoted", "disabled"} {
			t.Run(fmt.Sprintf("audience-%d/%s", audience, state), func(t *testing.T) {
				ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
				actor := catalogAuditActor(t, ctx, pool, audience)
				library := libraryIntegrationCreate(t, ctx, store, "Protected library", "movies", root)
				jobID := catalogAuditSeedJob(t, ctx, pool, library.ID, "Running", false)
				var statement string
				id := actor.SessionID
				switch state {
				case "revoked":
					statement = "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1"
				case "expired":
					statement = `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`
				case "demoted":
					statement, id = "UPDATE users SET is_administrator = false WHERE id = $1", actor.User.ID
				case "disabled":
					statement, id = "UPDATE users SET is_disabled = true WHERE id = $1", actor.User.ID
				}
				if _, err := pool.Exec(ctx, statement, id); err != nil {
					t.Fatalf("invalidate catalog administrator fixture: %v", err)
				}
				before := catalogAuditSnapshot(t, ctx, pool)
				for _, operation := range []struct {
					name string
					call func() error
				}{
					{"create", func() error {
						_, err := store.CreateLibraryAsAdministrator(ctx, actor, audience, "Unauthorized library", "movies", []string{root})
						return err
					}},
					{"delete", func() error { return store.DeleteLibraryAsAdministrator(ctx, actor, audience, library.ID) }},
					{"scan", func() error {
						_, err := store.StartScanAsAdministrator(ctx, actor, audience, library.ID, ScanOptions{})
						return err
					}},
					{"cancel", func() error { return store.CancelJobAsAdministrator(ctx, actor, audience, jobID) }},
				} {
					err := operation.call()
					if !errors.Is(err, identity.ErrUnauthorized) && !errors.Is(err, identity.ErrClientSessionForbidden) && !errors.Is(err, ErrForbidden) {
						t.Errorf("%s with stale administrator authority = %v, want authorization failure", operation.name, err)
					}
					if after := catalogAuditSnapshot(t, ctx, pool); after != before {
						t.Errorf("rejected %s changed catalog state or emitted activity", operation.name)
					}
				}
			})
		}
	}
}

func TestCatalogAuditCancellationNoopAndTerminalReplay(t *testing.T) {
	for _, fixture := range []struct {
		status    string
		requested bool
		want      int
	}{
		{"Queued", false, 1}, {"Running", false, 1}, {"Running", true, 0},
		{"Completed", false, 0}, {"Failed", false, 0}, {"Cancelled", true, 0}, {"Interrupted", false, 0},
	} {
		t.Run(fmt.Sprintf("%s/requested-%t", fixture.status, fixture.requested), func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
			library := libraryIntegrationCreate(t, ctx, store, "Cancellation replay", "movies", root)
			jobID := catalogAuditSeedJob(t, ctx, pool, library.ID, fixture.status, fixture.requested)
			if err := store.CancelJobAsAdministrator(ctx, actor, identity.AdministratorNative, jobID); err != nil {
				t.Fatalf("first catalog cancellation: %v", err)
			}
			beforeReplay := catalogAuditSnapshot(t, ctx, pool)
			if err := store.CancelJobAsAdministrator(ctx, actor, identity.AdministratorNative, jobID); err != nil {
				t.Fatalf("replay catalog cancellation: %v", err)
			}
			if after := catalogAuditSnapshot(t, ctx, pool); after != beforeReplay {
				t.Error("cancellation replay changed the catalog or repeated an audit fact")
			}
			if facts := catalogAuditFacts(t, ctx, pool, activity.ActionScanCancelRequested, jobID); len(facts) != fixture.want {
				t.Errorf("cancellation facts = %d, want %d", len(facts), fixture.want)
			}
		})
	}
}

func TestCatalogAuditMetadataTracksControlChangesWithoutValues(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
	detail := catalogAuditMetadataFixture(t, ctx, pool, store, root, actor)
	for index, step := range []struct {
		name      string
		overrides map[string]json.RawMessage
		locks     []string
		fields    []string
	}{
		{"initial-noop", map[string]json.RawMessage{}, []string{}, nil},
		{"same-effective-override", map[string]json.RawMessage{"Name": json.RawMessage(`"Automatic Film"`)}, []string{}, []string{"Name", "Overrides"}},
		{"override-noop", map[string]json.RawMessage{"Name": json.RawMessage(`"Automatic Film"`)}, []string{}, nil},
		{"lock-without-effective-change", map[string]json.RawMessage{"Name": json.RawMessage(`"Automatic Film"`)}, []string{"Overview"}, []string{"LockedFields", "Overview"}},
		{"private-values", map[string]json.RawMessage{"Name": json.RawMessage(`"Private manual title"`), "Genres": json.RawMessage(`["Private Manual Genre"]`)}, []string{"Overview"}, []string{"Genres", "Name", "Overrides"}},
		{"reset-overrides", map[string]json.RawMessage{}, []string{"Overview"}, []string{"Genres", "Name", "Overrides"}},
		{"remove-lock", map[string]json.RawMessage{}, []string{}, []string{"LockedFields", "Overview"}},
		{"reset-noop", map[string]json.RawMessage{}, []string{}, nil},
	} {
		t.Run(step.name, func(t *testing.T) {
			beforeCount := len(catalogAuditFacts(t, ctx, pool, activity.ActionMetadataUpdated, detail.ItemID))
			before := metadataEditTestSnapshot(t, ctx, pool, detail.ItemID)
			updated, err := store.UpdateItemMetadata(ctx, actor, detail.ItemID, MetadataEdit{Revision: detail.Revision, Overrides: step.overrides, LockedFields: step.locks})
			if err != nil {
				t.Fatalf("apply metadata audit step %d: %v", index, err)
			}
			facts := catalogAuditFacts(t, ctx, pool, activity.ActionMetadataUpdated, detail.ItemID)
			if step.fields == nil {
				if len(facts) != beforeCount || updated.Revision != detail.Revision || metadataEditTestSnapshot(t, ctx, pool, detail.ItemID) != before {
					t.Error("metadata no-op changed persisted state, revision, or activity")
				}
			} else {
				if len(facts) != beforeCount+1 {
					t.Fatalf("metadata audit count = %d, want %d", len(facts), beforeCount+1)
				}
				fact := facts[len(facts)-1]
				catalogAuditAssertActor(t, fact, actor, activity.SourceNative, activity.ResourceItem)
				if !reflect.DeepEqual(fact.fields, step.fields) || fact.revision != metadataEditTestRevision(t, updated.Revision) || fact.revision <= metadataEditTestRevision(t, detail.Revision) {
					t.Errorf("metadata activity fields/revision = %v/%d, want %v/%s", fact.fields, fact.revision, step.fields, updated.Revision)
				}
				for _, forbidden := range []string{"Automatic Film", "Private automatic plot", "Private manual title", "Private Manual Genre", "opaque-private-value", "Private Film.mp4", "InternalExtension"} {
					if strings.Contains(fact.raw, forbidden) {
						t.Errorf("metadata activity retained private value %q", forbidden)
					}
				}
			}
			detail = updated
		})
	}
}

func TestCatalogAuditMetadataFailureRollsBackProjectionRevisionAndEntities(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
	detail := catalogAuditMetadataFixture(t, ctx, pool, store, root, actor)
	before := catalogAuditSnapshot(t, ctx, pool)
	catalogAuditInstallTrigger(t, ctx, pool, activity.ActionMetadataUpdated, false)
	_, err := store.UpdateItemMetadata(ctx, actor, detail.ItemID, MetadataEdit{
		Revision: detail.Revision,
		Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Uncommitted title"`),
			"Genres": json.RawMessage(`["Uncommitted Genre"]`), "People": json.RawMessage(`[{"Name":"Uncommitted Person","Type":"Actor"}]`)},
		LockedFields: []string{"Overview"},
	})
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("metadata edit did not return injected audit failure: %v", err)
	}
	catalogAuditAssertTrigger(t, ctx, pool)
	if after := catalogAuditSnapshot(t, ctx, pool); after != before {
		t.Error("audit failure partially committed metadata projection, revision, entity associations, or activity")
	}
}

func TestCatalogAuditRejectedMetadataDoesNotRecordAnEdit(t *testing.T) {
	for _, state := range []string{"revoked", "expired", "wrong-audience", "revision-conflict", "invalid-field"} {
		t.Run(state, func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
			detail := catalogAuditMetadataFixture(t, ctx, pool, store, root, actor)
			edit := MetadataEdit{Revision: detail.Revision,
				Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Rejected title"`)}, LockedFields: []string{}}
			want := ErrForbidden
			switch state {
			case "revoked":
				if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", actor.SessionID); err != nil {
					t.Fatalf("revoke metadata audit actor: %v", err)
				}
			case "expired":
				if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '2 hours',
					expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`, actor.SessionID); err != nil {
					t.Fatalf("expire metadata audit actor: %v", err)
				}
			case "wrong-audience":
				actor.Kind = "emby"
				if _, err := pool.Exec(ctx, "UPDATE sessions SET kind = 'emby' WHERE id = $1", actor.SessionID); err != nil {
					t.Fatalf("prepare wrong metadata actor audience: %v", err)
				}
			case "revision-conflict":
				edit.Revision = strconv.FormatInt(metadataEditTestRevision(t, detail.Revision)+1, 10)
				want = ErrRevisionConflict
			case "invalid-field":
				edit.Overrides["FilesystemPath"] = json.RawMessage(`"private/untrusted/path"`)
				want = ErrInvalidInput
			}
			before := catalogAuditSnapshot(t, ctx, pool)
			if _, err := store.UpdateItemMetadata(ctx, actor, detail.ItemID, edit); !errors.Is(err, want) {
				t.Errorf("rejected metadata edit = %v, want %v", err, want)
			}
			if after := catalogAuditSnapshot(t, ctx, pool); after != before {
				t.Error("rejected metadata edit changed catalog state or created activity")
			}
		})
	}
}

func TestCatalogAuditScanFinalizationAndRecoveryCommitExactlyOnce(t *testing.T) {
	for _, fixture := range []struct {
		name, initial, terminal string
		recover                 bool
	}{
		{"completed", "Running", "Completed", false},
		{"failed", "Running", "Failed", false},
		{"cancelled", "Running", "Cancelled", false},
		{"recover-queued", "Queued", "Interrupted", true},
		{"recover-running", "Running", "Interrupted", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			library := libraryIntegrationCreate(t, ctx, store, "Terminal audit", "movies", root)
			jobID := catalogAuditSeedJob(t, ctx, pool, library.ID, fixture.initial, false)
			task := &scanTask{ctx: ctx, job: Job{ID: jobID, LibraryID: library.ID, Scanned: 7, Added: 4, Updated: 2}}
			transition := func() error {
				if fixture.recover {
					return store.recoverTaskScans(ctx)
				}
				return store.finishTask(task, fixture.terminal, "Private scanner diagnostic with a filesystem path")
			}
			before := catalogAuditSnapshot(t, ctx, pool)
			catalogAuditInstallTrigger(t, ctx, pool, activity.ActionScanFinished, false)
			var databaseError *pgconn.PgError
			if err := transition(); !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
				t.Fatalf("terminal transition did not return injected audit failure: %v", err)
			}
			catalogAuditAssertTrigger(t, ctx, pool)
			if after := catalogAuditSnapshot(t, ctx, pool); after != before {
				t.Error("failed terminal audit partially committed scan state, last-scan time, or activity")
			}
			if _, err := pool.Exec(ctx, "DROP TRIGGER catalog_audit_test_trigger ON activity_entries"); err != nil {
				t.Fatalf("release terminal activity failure fixture: %v", err)
			}
			if err := transition(); err != nil {
				t.Fatalf("retry terminal transition: %v", err)
			}
			job, err := store.GetJob(ctx, jobID)
			if err != nil || job.Status != fixture.terminal || job.FinishedAt == nil {
				t.Fatalf("committed terminal scan = %+v, error = %v", job, err)
			}
			fact := catalogAuditOnlyFact(t, ctx, pool, activity.ActionScanFinished, jobID)
			catalogAuditAssertActor(t, fact, identity.Principal{}, activity.SourceSystem, activity.ResourceScan)
			if fact.state != strings.ToLower(fixture.terminal) || strings.Contains(fact.raw, "Private scanner diagnostic") || strings.Contains(fact.raw, "filesystem path") {
				t.Errorf("terminal activity retained the wrong state or raw diagnostics: %+v", fact)
			}
			beforeReplay := catalogAuditSnapshot(t, ctx, pool)
			if err := transition(); err != nil {
				t.Fatalf("replay terminal transition: %v", err)
			}
			if after := catalogAuditSnapshot(t, ctx, pool); after != beforeReplay {
				t.Error("terminal transition replay changed business state or duplicated activity")
			}
		})
	}
}

func TestCatalogAuditFinalAuthorizationRunsAfterAuditInsert(t *testing.T) {
	for _, action := range []activity.Action{
		activity.ActionLibraryCreated, activity.ActionLibraryRemoved, activity.ActionScanRequested,
		activity.ActionScanCancelRequested, activity.ActionMetadataUpdated,
	} {
		t.Run(string(action), func(t *testing.T) {
			ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			actor := catalogAuditActor(t, ctx, pool, identity.AdministratorNative)
			var library Library
			var detail ItemMetadataDetail
			var jobID string
			if action == activity.ActionMetadataUpdated {
				detail = catalogAuditMetadataFixture(t, ctx, pool, store, root, actor)
			} else if action != activity.ActionLibraryCreated {
				library = libraryIntegrationCreate(t, ctx, store, "Fresh authority", "movies", root)
			}
			if action == activity.ActionScanCancelRequested {
				jobID = catalogAuditSeedJob(t, ctx, pool, library.ID, "Queued", false)
			}
			before := catalogAuditSnapshot(t, ctx, pool)
			var sessionBefore string
			if err := pool.QueryRow(ctx, "SELECT to_jsonb(session)::text FROM sessions session WHERE id = $1", actor.SessionID).Scan(&sessionBefore); err != nil {
				t.Fatalf("snapshot authorizing session: %v", err)
			}
			catalogAuditInstallTrigger(t, ctx, pool, action, true)
			var err error
			switch action {
			case activity.ActionLibraryCreated:
				_, err = store.CreateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, "Expired creation", "movies", []string{root})
			case activity.ActionLibraryRemoved:
				err = store.DeleteLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID)
			case activity.ActionScanRequested:
				_, err = store.StartScanAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, ScanOptions{})
			case activity.ActionScanCancelRequested:
				err = store.CancelJobAsAdministrator(ctx, actor, identity.AdministratorNative, jobID)
			case activity.ActionMetadataUpdated:
				_, err = store.UpdateItemMetadata(ctx, actor, detail.ItemID, MetadataEdit{Revision: detail.Revision,
					Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Expired edit"`)}, LockedFields: []string{}})
			}
			if !errors.Is(err, identity.ErrUnauthorized) && !errors.Is(err, ErrForbidden) {
				t.Fatalf("administrator expiring at the audit write = %v, want authorization failure", err)
			}
			catalogAuditAssertTrigger(t, ctx, pool)
			if after := catalogAuditSnapshot(t, ctx, pool); after != before {
				t.Error("final authorization failure retained business changes or its audit event")
			}
			var sessionAfter string
			if err := pool.QueryRow(ctx, "SELECT to_jsonb(session)::text FROM sessions session WHERE id = $1", actor.SessionID).Scan(&sessionAfter); err != nil {
				t.Fatalf("read authorizing session after rollback: %v", err)
			}
			if sessionAfter != sessionBefore {
				t.Error("audit-trigger session expiry escaped the rolled-back transaction")
			}
		})
	}
}
