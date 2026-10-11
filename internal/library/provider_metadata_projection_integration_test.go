package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/providers"
)

func providerMetadataTargetFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES ('provider-target-library','Provider targets','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES ('provider-target-root','provider-target-library','/provider-target','/provider-target','');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path)
		VALUES ('provider-target-parent','provider-target-library','provider-target-root','Parent','parent','Folder',true,'Film');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path)
		VALUES ('provider-target-item','provider-target-library','provider-target-root','provider-target-parent','Movie','movie','Movie','Film/Movie.mp4')`); err != nil {
		t.Fatal(err)
	}
}

func assertProviderTargetProjection(t *testing.T, trace *itemCountTracer, locked bool) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	count := 0
	for _, statement := range trace.statements {
		normalized := strings.Join(strings.Fields(statement), " ")
		if !strings.HasPrefix(normalized, "SELECT i.library_id, COALESCE(i.parent_id, ''), i.type, i.is_folder, ms.revision,") {
			continue
		}
		count++
		for _, omitted := range []string{"ms.automatic", "ms.overrides", "ms.locked_values", "ms.last_edited", "parent.name", "i.name", "i.path"} {
			if strings.Contains(statement, omitted) {
				t.Fatalf("provider target loaded unrelated metadata %q", omitted)
			}
		}
		if strings.HasSuffix(normalized, " FOR UPDATE OF i, ms") != locked {
			t.Fatalf("provider target changed its locking mode: %s", normalized)
		}
	}
	if count != 1 {
		t.Fatalf("provider operation loaded %d metadata targets, want one", count)
	}
}

func TestProviderSourcesProjectsNamesAndPreservesEmptySources(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	providerMetadataTargetFixture(t, ctx, fixture.pool)
	actor := metadataEditTestActor(t, ctx, fixture.pool, "provider-source-editor")
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO item_provider_sources
		(item_id,provider,provider_id,source_url,language,fields,fetched_at) VALUES
		('provider-target-item','musicbrainz','empty','https://musicbrainz.invalid/empty','', '{}','2026-01-02T03:04:05Z'),
		('provider-target-item','tmdb','42','https://tmdb.invalid/42','en',
		jsonb_build_object('Overview',repeat('x',131072),'ProviderIDs',jsonb_build_object('Tmdb','42'),
		'ProviderIds',jsonb_build_object('Custom','retained'),'Empty',NULL,
		'Future',jsonb_build_object('Count',9007199254740993::bigint)),'2026-01-02T03:04:05Z')`); err != nil {
		t.Fatal(err)
	}
	store, trace := countItemsTestStore(t, ctx, fixture)
	trace.reset()
	sources, err := store.ProviderSources(ctx, actor, "provider-target-item")
	if err != nil {
		t.Fatal(err)
	}
	fetched := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	want := []ProviderSource{
		{Provider: "musicbrainz", ProviderID: "empty", SourceURL: "https://musicbrainz.invalid/empty", FetchedAt: fetched, Fields: []string{}},
		{Provider: "tmdb", ProviderID: "42", SourceURL: "https://tmdb.invalid/42", Language: "en", FetchedAt: fetched,
			Fields: []string{"Empty", "Future", "Overview", "ProviderIds", "ProviderIds"}},
	}
	for index := range sources {
		sources[index].FetchedAt = sources[index].FetchedAt.UTC()
	}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("provider field projection changed response facts: got %#v, want %#v", sources, want)
	}
	encoded, err := json.Marshal(sources[0])
	if err != nil || !strings.Contains(string(encoded), `"Fields":[]`) {
		t.Fatalf("empty provider fields stopped being a non-null array: %s, %v", encoded, err)
	}
	assertProviderTargetProjection(t, trace, false)
	if count := catalogBatchStatementCount(trace, "ARRAY(SELECT jsonb_object_keys(sources.fields))"); count != 1 {
		t.Fatalf("provider sources used %d field-name projections, want one", count)
	}
	if count := catalogBatchStatementCount(trace, "FROM item_provider_sources"); count != 1 {
		t.Fatalf("provider sources queried provenance %d times, want one", count)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	trace.reset()
	if _, err := store.ProviderSources(ctx, actor, " provider-target-item "); !errors.Is(err, ErrForbidden) {
		t.Fatalf("source projection changed authorization before target lookup: %v", err)
	}
	if count := catalogBatchStatementCount(trace, "FROM item_provider_sources"); count != 0 {
		t.Fatal("a revoked administrator reached provider source data")
	}
}

func TestProviderMetadataTargetPreservesCompositionAndExistence(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	providerMetadataTargetFixture(t, ctx, fixture.pool)
	for _, test := range []struct {
		name, local, music, online, onlineType, itemType, wantError string
	}{
		{name: "absent sources", local: `null`, music: `{}`, online: `{}`},
		{name: "all source layers", local: `{"Name":"Local name","Unknown":9007199254740993}`,
			music:  `{"Version":1,"Name":"Embedded name","Artists":["Artist"],"AlbumArtists":[]}`,
			online: `{"Name":"Online name","ProviderIDs":{"Tmdb":"42"}}`, onlineType: "Movie"},
		{name: "invalid accepted version", local: `{}`, music: `{"Version":2}`, online: `{}`, wantError: "must have Version 1"},
		{name: "invalid local object with music", local: `[]`, music: `{"Version":1}`, online: `{}`, wantError: "must be a JSON object"},
		{name: "invalid local name with music", local: `{"Name":7}`, music: `{"Version":1}`, online: `{}`, wantError: "read accepted local name"},
		{name: "invalid local object with online", local: `[]`, music: `{}`, online: `{"Name":"Online"}`, onlineType: "Movie", wantError: "must be a JSON object"},
		{name: "online source for former type stays ignored", local: `[]`, music: `{}`, online: `{"Name":"Online"}`, onlineType: "Episode"},
		{name: "empty online does not add local validation", local: `[]`, music: `{}`, online: `{}`, onlineType: "Movie"},
		{name: "unsupported type precedes source validation", local: `{}`, music: `{"Version":2}`, online: `{}`, itemType: "CollectionFolder", wantError: ErrNotFound.Error()},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			itemType := test.itemType
			if itemType == "" {
				itemType = "Movie"
			}
			if _, err := tx.Exec(ctx, `UPDATE items SET local_metadata=$2::jsonb,type=$3 WHERE id=$1`, "provider-target-item", test.local, itemType); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE item_metadata_state SET music_source=$2::jsonb,online_source=$3::jsonb,online_type=$4
				WHERE item_id=$1`, "provider-target-item", test.music, test.online, test.onlineType); err != nil {
				t.Fatal(err)
			}
			for _, lock := range []bool{false, true} {
				full, fullErr := readMetadataRecord(ctx, tx, "provider-target-item", lock)
				target, targetErr := readProviderMetadataTarget(ctx, tx, "provider-target-item", lock)
				if test.wantError != "" {
					if fullErr == nil || targetErr == nil || fullErr.Error() != targetErr.Error() || !strings.Contains(targetErr.Error(), test.wantError) {
						t.Fatalf("target changed source/type failure: full=%v, target=%v, want %q", fullErr, targetErr, test.wantError)
					}
					continue
				}
				if fullErr != nil || targetErr != nil {
					t.Fatalf("target changed accepted source composition: full=%v, target=%v", fullErr, targetErr)
				}
				want := providerMetadataTarget{libraryID: full.libraryID, parentID: full.parentID,
					itemType: full.itemType, isFolder: full.isFolder, revision: full.revision}
				if target != want {
					t.Fatalf("target lost catalog or revision facts: got %+v, want %+v", target, want)
				}
			}
		})
	}
	for _, itemID := range []string{"missing-provider-item", " provider-target-item "} {
		tx, err := fixture.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = readProviderMetadataTarget(ctx, tx, itemID, false)
		rollback(tx)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing target %q changed its lookup error: %v", itemID, err)
		}
	}
	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `DELETE FROM item_metadata_state WHERE item_id='provider-target-item'`); err != nil {
		t.Fatal(err)
	}
	if _, err := readProviderMetadataTarget(ctx, tx, "provider-target-item", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("target accepted an item without metadata state: %v", err)
	}
}

func TestProviderMetadataTargetRetainsOnlyRequiredRowLocks(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	providerMetadataTargetFixture(t, ctx, fixture.pool)
	for _, lock := range []bool{false, true} {
		t.Run(map[bool]string{false: "provenance read", true: "image mutation"}[lock], func(t *testing.T) {
			tx, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if _, err := readProviderMetadataTarget(ctx, tx, "provider-target-item", lock); err != nil {
				t.Fatal(err)
			}
			for _, candidate := range []struct {
				statement string
				locked    bool
			}{
				{`SELECT id FROM items WHERE id='provider-target-item' FOR UPDATE NOWAIT`, lock},
				{`SELECT item_id FROM item_metadata_state WHERE item_id='provider-target-item' FOR UPDATE NOWAIT`, lock},
				{`SELECT id FROM items WHERE id='provider-target-parent' FOR UPDATE NOWAIT`, false},
			} {
				contender, err := fixture.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var id string
				err = contender.QueryRow(ctx, candidate.statement).Scan(&id)
				rollback(contender)
				var conflict *pgconn.PgError
				if candidate.locked && (!errors.As(err, &conflict) || conflict.Code != "55P03") || !candidate.locked && err != nil {
					t.Fatalf("provider target changed row lock scope: expectedLocked=%t, statement=%s, error=%v", candidate.locked, candidate.statement, err)
				}
			}
		})
	}
}

func TestSelectProviderImageUsesNarrowTargetAndRollsBackFinalDenial(t *testing.T) {
	ctx, pool, store, _, trace := catalogBatchTestStore(t)
	providerMetadataTargetFixture(t, ctx, pool)
	actor := metadataEditTestActor(t, ctx, pool, "provider-image-editor")
	if _, err := pool.Exec(ctx, `UPDATE item_metadata_state SET last_edited_at='infinity' WHERE item_id='provider-target-item'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItemMetadata(ctx, actor, "provider-target-item"); err == nil {
		t.Fatal("full metadata fixture no longer exercises an omitted timestamp scan failure")
	}
	trace.reset()
	if sources, err := store.ProviderSources(ctx, actor, "provider-target-item"); err != nil || sources == nil || len(sources) != 0 {
		t.Fatalf("provenance target retained the unrelated editor timestamp: sources=%+v, error=%v", sources, err)
	}
	assertProviderTargetProjection(t, trace, false)
	selected := providers.RemoteImage{Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: "Movie"}, ImageID: "cover", ImageType: "Primary"}
	data := imageStoreTestPNG(t)
	notifications := catalogChangesTestListener(t, store)
	trace.reset()
	result, err := store.SelectProviderImage(ctx, actor, "provider-target-item", "1", selected, 0, data)
	if err != nil || result.ImageID != selected.ImageID || result.Width != 2 || result.Height != 2 || result.Tag == "" {
		t.Fatalf("provider image selection failed with narrow target: %+v, %v", result, err)
	}
	assertProviderTargetProjection(t, trace, true)
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{
		{Kind: CatalogUpdated, ItemID: "provider-target-item", LibraryID: "provider-target-library", ParentID: "provider-target-parent"},
	})
	if _, err := store.SelectProviderImage(ctx, actor, "provider-target-item", "2", selected, 0, data); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("image target stopped checking its revision: %v", err)
	}
	wrongType := selected
	wrongType.Type = "Episode"
	if _, err := store.SelectProviderImage(ctx, actor, "provider-target-item", "1", wrongType, 0, data); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("image target stopped checking its type: %v", err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(im)::text FROM item_provider_images im WHERE item_id='provider-target-item'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE SEQUENCE provider_image_expiry_hits;
		CREATE FUNCTION expire_provider_image_actor() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM nextval('provider_image_expiry_hits');
			UPDATE sessions SET created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 day'
			WHERE user_id='provider-image-editor';
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER expire_provider_image_actor BEFORE INSERT ON item_provider_images
		FOR EACH ROW EXECUTE FUNCTION expire_provider_image_actor()`); err != nil {
		t.Fatal(err)
	}
	selected.ImageID = "denied-replacement"
	if _, err := store.SelectProviderImage(ctx, actor, "provider-target-item", "1", selected, 0, data); !errors.Is(err, ErrForbidden) {
		t.Fatalf("provider image committed after its final authorization failed: %v", err)
	}
	var reached, active bool
	var after string
	if err := pool.QueryRow(ctx, `SELECT is_called FROM provider_image_expiry_hits`).Scan(&reached); err != nil || !reached {
		t.Fatalf("image mutation did not reach the expiry fault: reached=%t, error=%v", reached, err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(im)::text FROM item_provider_images im WHERE item_id='provider-target-item'`).Scan(&after); err != nil || after != before {
		t.Fatalf("failed final authorization retained a provider image update: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT expires_at>clock_timestamp() FROM sessions WHERE id=$1`, actor.SessionID).Scan(&active); err != nil || !active {
		t.Fatalf("failed image transaction did not roll back its expiry mutation: active=%t, error=%v", active, err)
	}
	assertNoCatalogTestNotification(t, notifications)
}
