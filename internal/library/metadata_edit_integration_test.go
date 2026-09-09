package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func metadataEditTestActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) identity.Principal {
	t.Helper()
	libraryIntegrationUser(t, ctx, pool, userID, false, true, nil)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = $1", userID); err != nil {
		t.Fatalf("promote metadata editor fixture: %v", err)
	}
	var digest [32]byte
	if _, err := rand.Read(digest[:]); err != nil {
		t.Fatalf("generate metadata authentication fixture: %v", err)
	}
	actor := identity.Principal{
		User:      identity.User{ID: userID, Name: userID, IsAdministrator: true},
		SessionID: "metadata_auth_" + hex.EncodeToString(digest[:12]), Kind: "admin",
		Client: identity.Client{DeviceID: "metadata-editor-device"},
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sessions (id, user_id, token_hash, kind, device_id, expires_at)
		VALUES ($1, $2, $3, 'admin', $4, now() + interval '1 day') RETURNING expires_at`,
		actor.SessionID, actor.User.ID, digest[:], actor.Client.DeviceID).Scan(&actor.ExpiresAt); err != nil {
		t.Fatalf("create metadata editor authentication: %v", err)
	}
	return actor
}

func metadataEditTestRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode metadata edit fixture: %v", err)
	}
	return encoded
}

func metadataEditTestCopy(values map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}

func metadataEditTestRevision(t *testing.T, value string) int64 {
	t.Helper()
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != value {
		t.Fatalf("metadata revision is not canonical positive decimal text: %q", value)
	}
	return revision
}

func metadataEditTestDetail(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, itemID string) ItemMetadataDetail {
	t.Helper()
	detail, err := store.GetItemMetadata(ctx, actor, itemID)
	if err != nil {
		t.Fatalf("read administrator metadata detail: %v", err)
	}
	metadataEditTestRevision(t, detail.Revision)
	encoded := metadataEditTestRaw(t, detail)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode administrator metadata detail: %v", err)
	}
	for _, field := range []string{"Overrides", "LockedValues", "LockedFields", "EditableFields"} {
		if len(object[field]) == 0 || string(object[field]) == "null" {
			t.Errorf("administrator metadata collection %s is null or absent", field)
		}
	}
	for _, layer := range []string{"Automatic", "Effective"} {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(object[layer], &values); err != nil {
			t.Fatalf("decode metadata %s values: %v", layer, err)
		}
		for _, field := range []string{"ProviderIds", "Genres", "Tags", "Studios", "People"} {
			if len(values[field]) == 0 || string(values[field]) == "null" {
				t.Errorf("metadata %s.%s collection is null or absent", layer, field)
			}
		}
	}
	return detail
}

func metadataEditTestUpdate(t *testing.T, ctx context.Context, store *Store, actor identity.Principal, before ItemMetadataDetail, values map[string]json.RawMessage, locks []string) ItemMetadataDetail {
	t.Helper()
	if values == nil {
		values = map[string]json.RawMessage{}
	}
	if locks == nil {
		locks = []string{}
	}
	detail, err := store.UpdateItemMetadata(ctx, actor, before.ItemID, MetadataEdit{Revision: before.Revision, Overrides: values, LockedFields: locks})
	if err != nil {
		t.Fatalf("update administrator metadata: %v", err)
	}
	if metadataEditTestRevision(t, detail.Revision) <= metadataEditTestRevision(t, before.Revision) {
		t.Error("accepted metadata change did not advance its revision")
	}
	if detail.LastEditedBy != actor.User.ID || detail.LastEditedAt == nil {
		t.Errorf("metadata change did not record the editor: %+v", detail)
	}
	return detail
}

func metadataEditTestSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'item', (SELECT to_jsonb(i) FROM items i WHERE id = $1),
		'state', (SELECT to_jsonb(s) FROM item_metadata_state s WHERE item_id = $1),
		'entities', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY entity_id, position)
			FROM item_entities e WHERE item_id = $1), '[]'::jsonb))::text`, itemID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot metadata state and catalog associations: %v", err)
	}
	return snapshot
}

func TestStoreMetadataOverridesLocksAndAutomaticSourcesPersistAcrossScans(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	actor := metadataEditTestActor(t, ctx, pool, "metadata-editor")
	path := libraryIntegrationFile(t, allowedRoot, "movies/Film.mp4", "video:metadata-edit")
	nfoPath := libraryIntegrationFile(t, allowedRoot, "movies/Film.nfo", `<movie><title>Automatic One</title><sorttitle>automatic sort one</sorttitle><plot>Automatic plot one.</plot><year>2001</year><imdbid>tt1001</imdbid><genre>Automatic Drama</genre><actor><name>Automatic Actor</name><role>Original</role></actor></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Editable movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	initial := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if initial.Automatic.Name != "Automatic One" || initial.Effective.Name != initial.Automatic.Name || len(initial.Overrides) != 0 || len(initial.LockedValues) != 0 {
		t.Fatalf("initial administrator values do not reflect the automatic source: %+v", initial)
	}
	beforeNoop := metadataEditTestSnapshot(t, ctx, pool, item.ID)
	unchanged, err := store.UpdateItemMetadata(ctx, actor, item.ID, MetadataEdit{Revision: initial.Revision, Overrides: map[string]json.RawMessage{}, LockedFields: []string{}})
	if err != nil || unchanged.Revision != initial.Revision {
		t.Fatalf("empty metadata edit changed revision: detail = %+v, error = %v", unchanged, err)
	}
	if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != beforeNoop {
		t.Error("empty metadata edit changed persisted state")
	}
	nfoBytes, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("read unchanged NFO values: %v", err)
	}
	if err := os.WriteFile(nfoPath, append(nfoBytes, '\n'), 0600); err != nil {
		t.Fatalf("change only the NFO content hash: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	hashOnly := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if metadataEditTestRevision(t, hashOnly.Revision) <= metadataEditTestRevision(t, initial.Revision) || !reflect.DeepEqual(hashOnly.Automatic, initial.Automatic) || !reflect.DeepEqual(hashOnly.Effective, initial.Effective) {
		t.Errorf("hash-only source change did not preserve values while advancing revision: before = %+v, after = %+v", initial, hashOnly)
	}
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, MetadataEdit{Revision: initial.Revision, Overrides: map[string]json.RawMessage{}, LockedFields: []string{}}); !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("hash-only source change accepted a stale revision: %v", err)
	}
	initial = hashOnly
	values := map[string]json.RawMessage{"Name": metadataEditTestRaw(t, "Manual Movie")}
	named := metadataEditTestUpdate(t, ctx, store, actor, initial, values, nil)
	if named.Effective.Name != "Manual Movie" || named.Effective.SortName != initial.Automatic.SortName {
		t.Errorf("editing Name implicitly changed independent SortName: %+v", named.Effective)
	}
	values["SortName"] = metadataEditTestRaw(t, "manual sort")
	values["Overview"] = metadataEditTestRaw(t, "Pinned manual plot.")
	values["ProductionYear"] = metadataEditTestRaw(t, 1999)
	values["ProviderIds"] = json.RawMessage(`{"Tmdb":"42"}`)
	values["Genres"] = json.RawMessage(`["Pinned Genre"]`)
	values["People"] = json.RawMessage(`[{"Name":"Manual Actor","Role":"Lead","Type":"Actor","SortOrder":0}]`)
	pinned := metadataEditTestUpdate(t, ctx, store, actor, named, values, []string{"Overview", "Genres"})
	if string(pinned.LockedValues["Overview"]) != string(values["Overview"]) || string(pinned.LockedValues["Genres"]) != string(values["Genres"]) {
		t.Errorf("new locks did not capture the prospective manual values: %+v", pinned.LockedValues)
	}
	if pinned.Effective.ProductionYear == nil || *pinned.Effective.ProductionYear != 1999 || pinned.Effective.ProviderIDs["Tmdb"] != "42" || len(pinned.Effective.People) != 1 || pinned.Effective.People[0].Role != "Lead" {
		t.Errorf("manual scalar, provider, or credit metadata was not applied: %+v", pinned.Effective)
	}
	genreResult := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true, Genres: []string{"Pinned Genre"}})
	if genreResult.TotalRecordCount != 1 || len(genreResult.Items) != 1 || genreResult.Items[0].ID != item.ID || len(genreResult.Items[0].Entities.People) != 1 {
		t.Errorf("metadata edit did not update entity navigation immediately: %+v", genreResult)
	}
	libraryIntegrationFile(t, allowedRoot, "movies/Film.nfo", `<movie><title>Automatic Two</title><sorttitle>automatic sort two</sorttitle><plot>Automatic plot two.</plot><year>2022</year><imdbid>tt2002</imdbid><genre>Automatic Comedy</genre><actor><name>Replacement Automatic Actor</name></actor></movie>`)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	rescanned := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if rescanned.Automatic.Name != "Automatic Two" || rescanned.Automatic.ProductionYear == nil || *rescanned.Automatic.ProductionYear != 2022 || !reflect.DeepEqual(rescanned.Effective, pinned.Effective) {
		t.Errorf("automatic refresh lost manual or locked values: before = %+v, after = %+v", pinned, rescanned)
	}
	if metadataEditTestRevision(t, rescanned.Revision) <= metadataEditTestRevision(t, pinned.Revision) {
		t.Error("invisible automatic source change did not advance the revision")
	}
	if _, err := store.UpdateItemMetadata(ctx, actor, item.ID, MetadataEdit{Revision: pinned.Revision, Overrides: metadataEditTestCopy(values), LockedFields: []string{"Overview", "Genres"}}); !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("stale edit after an invisible source change: got %v, want ErrRevisionConflict", err)
	}
	secondValues := metadataEditTestCopy(values)
	secondValues["Overview"] = metadataEditTestRaw(t, "Temporary manual plot.")
	secondValues["Genres"] = json.RawMessage(`["Temporary Genre"]`)
	manualAboveLock := metadataEditTestUpdate(t, ctx, store, actor, rescanned, secondValues, []string{"Overview", "Genres"})
	if manualAboveLock.Effective.Overview != "Temporary manual plot." || !reflect.DeepEqual(manualAboveLock.LockedValues, pinned.LockedValues) {
		t.Errorf("retained locks were recaptured or overrode manual values: %+v", manualAboveLock)
	}
	withoutManual := metadataEditTestCopy(secondValues)
	delete(withoutManual, "Overview")
	delete(withoutManual, "Genres")
	lockRestored := metadataEditTestUpdate(t, ctx, store, actor, manualAboveLock, withoutManual, []string{"Overview", "Genres"})
	if lockRestored.Effective.Overview != "Pinned manual plot." || !reflect.DeepEqual(lockRestored.Effective.Genres, []string{"Pinned Genre"}) {
		t.Errorf("removing manual values did not restore retained lock snapshots: %+v", lockRestored.Effective)
	}
	manualAgain := metadataEditTestUpdate(t, ctx, store, actor, lockRestored, secondValues, []string{"Overview", "Genres"})
	automaticAgain := metadataEditTestUpdate(t, ctx, store, actor, manualAgain, withoutManual, []string{})
	if automaticAgain.Effective.Overview != "Automatic plot two." || !reflect.DeepEqual(automaticAgain.Effective.Genres, []string{"Automatic Comedy"}) || len(automaticAgain.LockedValues) != 0 {
		t.Errorf("use-automatic did not clear both manual and locked layers: %+v", automaticAgain)
	}
	clearValues := metadataEditTestCopy(withoutManual)
	clearValues["ProductionYear"] = json.RawMessage(`null`)
	clearValues["Overview"] = json.RawMessage(`""`)
	clearValues["ProviderIds"] = json.RawMessage(`{}`)
	clearValues["Genres"] = json.RawMessage(`[]`)
	clearValues["People"] = json.RawMessage(`[]`)
	cleared := metadataEditTestUpdate(t, ctx, store, actor, automaticAgain, clearValues, nil)
	if cleared.Effective.ProductionYear != nil || cleared.Effective.Overview != "" || len(cleared.Effective.ProviderIDs) != 0 || len(cleared.Effective.Genres) != 0 || len(cleared.Effective.People) != 0 {
		t.Errorf("explicit empty and null overrides did not clear values: %+v", cleared.Effective)
	}
	if result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true, Genres: []string{"Pinned Genre", "Temporary Genre", "Automatic Comedy"}}); result.TotalRecordCount != 0 {
		t.Errorf("cleared collection retained navigable entity associations: %+v", result)
	}
	if err := os.Remove(nfoPath); err != nil {
		t.Fatalf("remove automatic NFO source: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	removed := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if removed.Automatic.Name != "Film" || removed.Automatic.ProductionYear != nil || !reflect.DeepEqual(removed.Effective, cleared.Effective) || !reflect.DeepEqual(removed.Overrides, cleared.Overrides) {
		t.Errorf("NFO deletion lost manual values or did not refresh automatic values: %+v", removed)
	}
	if metadataEditTestRevision(t, removed.Revision) <= metadataEditTestRevision(t, cleared.Revision) {
		t.Error("automatic NFO deletion did not advance revision")
	}
	if calls := len(prober.calls()); calls != 1 {
		t.Errorf("metadata-only changes repeated media probing: calls = %d, want 1", calls)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("close metadata store before reopening: %v", err)
	}
	reopened, err := New(pool, prober, []string{allowedRoot})
	if err != nil {
		t.Fatalf("reopen persisted metadata state: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := reopened.Close(cleanupCtx); err != nil {
			t.Errorf("close reopened metadata store: %v", err)
		}
	})
	persisted := metadataEditTestDetail(t, ctx, reopened, actor, item.ID)
	if persisted.Revision != removed.Revision || !reflect.DeepEqual(persisted.Effective, removed.Effective) || !reflect.DeepEqual(persisted.Overrides, removed.Overrides) {
		t.Errorf("restart changed metadata state: before = %+v, after = %+v", removed, persisted)
	}
	if err := reopened.DeleteLibrary(ctx, library.ID); err != nil {
		t.Fatalf("delete library with administrator metadata: %v", err)
	}
	var stateExists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM item_metadata_state WHERE item_id = $1)", item.ID).Scan(&stateExists); err != nil || stateExists {
		t.Errorf("library deletion retained item metadata state: exists = %v, error = %v", stateExists, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "video:metadata-edit" {
		t.Errorf("library deletion changed the original media file: error = %v", err)
	}
}

func TestStoreMetadataEditingRechecksCurrentAdministratorAndSession(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "metadata-auth-editor")
	other := metadataEditTestActor(t, ctx, pool, "metadata-other-editor")
	path := libraryIntegrationFile(t, allowedRoot, "movies/Auth.mp4", "video:metadata-auth")
	library := libraryIntegrationCreate(t, ctx, store, "Metadata authorization", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	before := metadataEditTestSnapshot(t, ctx, pool, item.ID)
	assertRejected := func(t *testing.T, rejected identity.Principal) {
		t.Helper()
		if _, err := store.GetItemMetadata(ctx, rejected, item.ID); !errors.Is(err, ErrForbidden) {
			t.Errorf("unauthorized metadata read: got %v, want ErrForbidden", err)
		}
		if _, err := store.UpdateItemMetadata(ctx, rejected, item.ID, MetadataEdit{Revision: detail.Revision, Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Unauthorized"`)}, LockedFields: []string{}}); !errors.Is(err, ErrForbidden) {
			t.Errorf("unauthorized metadata update: got %v, want ErrForbidden", err)
		}
		if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != before {
			t.Error("unauthorized metadata operation changed catalog state")
		}
	}
	wrongKind := actor
	wrongKind.Kind = "emby"
	assertRejected(t, wrongKind)
	crossUser := actor
	crossUser.User = other.User
	assertRejected(t, crossUser)
	for _, change := range []struct{ name, apply, restore, id string }{
		{"RevokedSession", "UPDATE sessions SET revoked_at = now() WHERE id = $1", "UPDATE sessions SET revoked_at = NULL WHERE id = $1", actor.SessionID},
		{"DisabledAccount", "UPDATE users SET is_disabled = true WHERE id = $1", "UPDATE users SET is_disabled = false WHERE id = $1", actor.User.ID},
		{"DemotedAccount", "UPDATE users SET is_administrator = false WHERE id = $1", "UPDATE users SET is_administrator = true WHERE id = $1", actor.User.ID},
		{"ChangedSessionKind", "UPDATE sessions SET kind = 'emby' WHERE id = $1", "UPDATE sessions SET kind = 'admin' WHERE id = $1", actor.SessionID},
	} {
		t.Run(change.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, change.apply, change.id); err != nil {
				t.Fatalf("change current metadata authorization: %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if _, err := pool.Exec(cleanupCtx, change.restore, change.id); err != nil {
					t.Errorf("restore metadata authorization fixture: %v", err)
				}
			})
			assertRejected(t, actor)
		})
	}
}

type metadataEditGatedProber struct {
	fixture libraryFixtureProber
	entered chan struct{}
	release chan struct{}
}

func (prober *metadataEditGatedProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := prober.fixture.ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	if len(prober.fixture.calls()) == 2 {
		prober.entered <- struct{}{}
		select {
		case <-prober.release:
		case <-ctx.Done():
			return media.Info{}, ctx.Err()
		}
	}
	return info, nil
}

func TestStoreMetadataEditCommittedDuringProbeSurvivesScanPersistence(t *testing.T) {
	prober := &metadataEditGatedProber{entered: make(chan struct{}, 1), release: make(chan struct{})}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(prober.release) }) }
	t.Cleanup(release)
	actor := metadataEditTestActor(t, ctx, pool, "metadata-race-editor")
	path := libraryIntegrationFile(t, allowedRoot, "movies/Race.mp4", "video:before-edit")
	libraryIntegrationFile(t, allowedRoot, "movies/Race.nfo", `<movie><title>Automatic Before</title></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Concurrent metadata", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	before := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	libraryIntegrationFile(t, allowedRoot, "movies/Race.mp4", "video:after-edit-with-new-size")
	libraryIntegrationFile(t, allowedRoot, "movies/Race.nfo", `<movie><title>Automatic After</title></movie>`)
	job, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatalf("start scan for concurrent metadata edit: %v", err)
	}
	select {
	case <-prober.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("rescan did not reach the controlled probe")
	}
	type result struct {
		detail ItemMetadataDetail
		err    error
	}
	updated := make(chan result, 1)
	editCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	go func() {
		detail, err := store.UpdateItemMetadata(editCtx, actor, item.ID, MetadataEdit{Revision: before.Revision, Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Committed Manual Name"`)}, LockedFields: []string{}})
		updated <- result{detail: detail, err: err}
	}()
	var committed ItemMetadataDetail
	select {
	case result := <-updated:
		if result.err != nil {
			t.Fatalf("commit metadata while media probing is blocked: %v", result.err)
		}
		committed = result.detail
	case <-editCtx.Done():
		release()
		t.Fatal("metadata editing waited for a blocked media probe")
	}
	release()
	libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	after := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if after.Automatic.Name != "Automatic After" || after.Effective.Name != "Committed Manual Name" || string(after.Overrides["Name"]) != `"Committed Manual Name"` || metadataEditTestRevision(t, after.Revision) <= metadataEditTestRevision(t, committed.Revision) {
		t.Errorf("scan persisted stale metadata over a committed edit: %+v", after)
	}
}

func TestStoreMetadataEpisodeNumberIsEditableButStructuralNumbersAreReadOnly(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "metadata-number-editor")
	path := libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.mp4", "video:metadata-numbers")
	library := libraryIntegrationCreate(t, ctx, store, "Numbered metadata", "tvshows", filepath.Join(allowedRoot, "television"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	episode := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	season := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(path))
	episodeDetail := metadataEditTestDetail(t, ctx, store, actor, episode.ID)
	episodeDetail = metadataEditTestUpdate(t, ctx, store, actor, episodeDetail, map[string]json.RawMessage{"IndexNumber": json.RawMessage(`9`)}, nil)
	if episodeDetail.Effective.IndexNumber == nil || *episodeDetail.Effective.IndexNumber != 9 || episodeDetail.ParentID != season.ID || episodeDetail.Effective.ParentIndexNumber == nil || *episodeDetail.Effective.ParentIndexNumber != 1 {
		t.Errorf("episode numbering edit changed its structural season: %+v", episodeDetail)
	}
	for _, target := range []struct{ itemID, field string }{{episode.ID, "ParentIndexNumber"}, {season.ID, "IndexNumber"}} {
		detail := metadataEditTestDetail(t, ctx, store, actor, target.itemID)
		for _, field := range detail.EditableFields {
			if field == target.field {
				t.Errorf("structural field %s is advertised as editable", target.field)
			}
		}
		before := metadataEditTestSnapshot(t, ctx, pool, target.itemID)
		for _, edit := range []MetadataEdit{
			{Revision: detail.Revision, Overrides: map[string]json.RawMessage{target.field: json.RawMessage(`1`)}, LockedFields: []string{}},
			{Revision: detail.Revision, Overrides: map[string]json.RawMessage{target.field: json.RawMessage(`2`)}, LockedFields: []string{}},
			{Revision: detail.Revision, Overrides: map[string]json.RawMessage{}, LockedFields: []string{target.field}},
		} {
			_, err := store.UpdateItemMetadata(ctx, actor, target.itemID, edit)
			var validation *MetadataValidationError
			if !errors.Is(err, ErrInvalidInput) || !errors.As(err, &validation) {
				t.Errorf("structural number edit did not return metadata validation: %v", err)
			}
			if after := metadataEditTestSnapshot(t, ctx, pool, target.itemID); after != before {
				t.Errorf("rejected structural %s change modified metadata state", target.field)
			}
		}
	}
	t.Run("FilenameRenamePreservesEpisodeOverride", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("stable filesystem rename identity is implemented on Linux")
		}
		newPath := filepath.Join(filepath.Dir(path), "Show.S01E03.mp4")
		if err := os.Rename(path, newPath); err != nil {
			t.Fatalf("rename the episode within its existing season: %v", err)
		}
		libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		rescanned := metadataEditTestDetail(t, ctx, store, actor, episode.ID)
		if rescanned.Automatic.IndexNumber == nil || *rescanned.Automatic.IndexNumber != 3 || rescanned.Effective.IndexNumber == nil || *rescanned.Effective.IndexNumber != 9 || rescanned.ParentID != season.ID || string(rescanned.Overrides["IndexNumber"]) != "9" {
			t.Errorf("filename renumbering lost the independent manual episode number: %+v", rescanned)
		}
	})
}

func TestStoreMetadataUnknownSourceFieldsAndEntityChangesCommitAtomically(t *testing.T) {
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "metadata-atomic-editor")
	path := libraryIntegrationFile(t, allowedRoot, "movies/Opaque.mp4", "video:opaque-metadata")
	library := libraryIntegrationCreate(t, ctx, store, "Opaque source metadata", "movies", filepath.Dir(path))
	const itemID = "metadata-opaque-item"
	source := []byte(`{"Kind":"movie","Name":"Opaque Source","SortName":"opaque source","Overview":"Automatic overview.","Genres":["Original Genre"],"InternalExtension":{"LargeInteger":9007199254740993,"Opaque":[true,null,"keep"]}}`)
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, parent_id, name, sort_name, overview, type, path, local_metadata, local_metadata_hash, local_metadata_path)
		VALUES ($1, $2, $2, 'Opaque Source', 'opaque source', 'Automatic overview.', 'Movie', $3, $4, repeat('a',64), 'Opaque.nfo')`, itemID, library.ID, path, source); err != nil {
		t.Fatalf("insert opaque automatic metadata fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "SELECT sync_catalog_item_entities($1, $2::jsonb)", itemID, source); err != nil {
		t.Fatalf("create original opaque metadata associations: %v", err)
	}
	detail := metadataEditTestDetail(t, ctx, store, actor, itemID)
	updated := metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{"Name": json.RawMessage(`"Edited Opaque Source"`), "Genres": json.RawMessage(`["Committed Genre"]`)}, nil)
	var kind, largeInteger, opaque string
	if err := pool.QueryRow(ctx, `SELECT effective->>'Kind', effective #>> '{InternalExtension,LargeInteger}',
		(effective #> '{InternalExtension,Opaque}')::text FROM item_metadata_state WHERE item_id = $1`, itemID).Scan(&kind, &largeInteger, &opaque); err != nil || kind != "movie" || largeInteger != "9007199254740993" || opaque != `[true, null, "keep"]` {
		t.Errorf("administrator edit lost unknown source data: kind = %q, integer = %q, opaque = %q, error = %v", kind, largeInteger, opaque, err)
	}
	if result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true, Genres: []string{"Committed Genre"}}); result.TotalRecordCount != 1 || len(result.Items) != 1 || result.Items[0].ID != itemID {
		t.Errorf("edited entity filter was not immediately consistent: %+v", result)
	}
	if result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, Recursive: true, Genres: []string{"Original Genre"}}); result.TotalRecordCount != 0 {
		t.Errorf("old entity association remained after the edit: %+v", result)
	}
	before := metadataEditTestSnapshot(t, ctx, pool, itemID)
	if _, err := pool.Exec(ctx, `CREATE SEQUENCE metadata_edit_failure_hits;
		CREATE FUNCTION reject_metadata_entities_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM nextval('metadata_edit_failure_hits');
			RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'Injected metadata entity failure';
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_metadata_entities_for_test BEFORE INSERT ON item_entities
		FOR EACH ROW EXECUTE FUNCTION reject_metadata_entities_for_test()`); err != nil {
		t.Fatalf("install metadata transaction failure fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS reject_metadata_entities_for_test ON item_entities;
			DROP FUNCTION IF EXISTS reject_metadata_entities_for_test(); DROP SEQUENCE IF EXISTS metadata_edit_failure_hits`); err != nil {
			t.Errorf("remove metadata transaction failure fixture: %v", err)
		}
	})
	_, err := store.UpdateItemMetadata(ctx, actor, itemID, MetadataEdit{Revision: updated.Revision, Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Must Roll Back"`), "Genres": json.RawMessage(`["Uncommitted Genre"]`)}, LockedFields: []string{}})
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("metadata update did not reach the injected entity failure: %v", err)
	}
	var triggered bool
	if err := pool.QueryRow(ctx, "SELECT is_called FROM metadata_edit_failure_hits").Scan(&triggered); err != nil || !triggered {
		t.Fatalf("metadata failure trigger was not reached: triggered = %v, error = %v", triggered, err)
	}
	if after := metadataEditTestSnapshot(t, ctx, pool, itemID); after != before {
		t.Error("failed entity synchronization partially committed administrator metadata")
	}
}
