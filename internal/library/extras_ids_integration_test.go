package library

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreExplicitExtraIDsUseDirectValidityAndPublicTypeWithoutThemeVisibility(t *testing.T) {
	ctx, store := extraTestFixture(t)
	extraTestResource(t, ctx, store, "extra-invalid", "theme-seed", "library-b", "Seed/featurettes/Invalid.mp4", ExtraKindClip)
	extraTestResource(t, ctx, store, "extra-private", "theme-hidden", "library-a", "Hidden/featurettes/Private.mp4", ExtraKindClip)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_id='theme-all' WHERE id='extra-invalid';
		UPDATE item_extra_resources SET active=false WHERE resource_item_id='extra-zeta'`); err != nil {
		t.Fatal(err)
	}
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "extra-alpha", PlayCount: 4, IsFavorite: true})
	before := extraTestState(t, ctx, store)
	ids := []string{"extra-alpha", "extra-middle", "extra-trailer", "extra-zeta", "extra-invalid", "extra-private", "theme-song", "movie-b", "missing", "extra-alpha"}
	for _, test := range []struct {
		name  string
		types []string
		want  []string
	}{
		{"mixed", nil, []string{"extra-alpha", "movie-b", "extra-trailer", "extra-middle"}},
		{"trailer", []string{"Trailer"}, []string{"extra-trailer"}},
		{"video", []string{"Video"}, []string{"extra-alpha", "extra-middle"}},
		{"movie and trailer", []string{"Movie", "Trailer"}, []string{"movie-b", "extra-trailer"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids, IncludeItemTypes: test.types, Limit: 100})
			if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
				t.Fatalf("known-ID selection ignored public type, validity, or current scope: %v, items=%v", err, queryItemIDs(result.Items))
			}
			for _, item := range result.Items {
				direct, err := store.GetItem(ctx, "restricted", item.ID)
				if err != nil || !reflect.DeepEqual(item, direct) {
					t.Fatalf("known-ID extra projection escaped the direct contract: %v", err)
				}
			}
		})
	}
	page, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids, StartIndex: 1, Limit: 2})
	if err != nil || page.TotalRecordCount != 4 || !reflect.DeepEqual(queryItemIDs(page.Items), []string{"movie-b", "extra-trailer"}) {
		t.Fatalf("known-ID totals or paging included hidden resources: %v", err)
	}
	if extraTestState(t, ctx, store) != before {
		t.Fatal("known-ID selection wrote resource, metadata, credential, or user state")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	denied, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids})
	if err != nil || denied.TotalRecordCount != 0 || len(denied.Items) != 0 {
		t.Fatalf("explicit attachment IDs bypassed the next request's policy revocation: %v", err)
	}
}

func TestStoreExplicitExtraIDsIntersectOnlyOrdinaryAncestryAndAuthorizedParent(t *testing.T) {
	ctx, store := extraTestFixture(t)
	extraTestResource(t, ctx, store, "extra-private", "theme-hidden", "library-a", "Hidden/featurettes/Private.mp4", ExtraKindClip)
	// A malformed catalog hierarchy must not make a resource into a traversal
	// bridge. This ordinary owner is reachable only through an extra resource.
	themeTestItem(t, ctx, store, "extra-bridged-owner", "Movie", "extra-alpha", "library-b", "Outside/movie.mp4")
	extraTestResource(t, ctx, store, "extra-bridged", "extra-bridged-owner", "library-b", "Outside/featurettes/Hidden.mp4", ExtraKindClip)
	ids := []string{"extra-alpha", "extra-trailer", "extra-private", "extra-bridged", "theme-seed"}
	for _, test := range []struct {
		parent, user string
		recursive    bool
		want         []string
	}{
		{"library-b", "restricted", true, []string{"extra-alpha", "extra-trailer", "theme-seed"}},
		{"library-b", "restricted", false, []string{"theme-seed"}},
		{"theme-seed", "restricted", true, []string{"extra-alpha", "extra-trailer"}},
		{"theme-seed", "restricted", false, []string{"extra-alpha", "extra-trailer"}},
		{"theme-all", "restricted", true, []string{}},
		{"library-b", "default", true, []string{"extra-alpha", "extra-trailer", "theme-seed"}},
		{"library-a", "default", true, []string{"extra-private"}},
	} {
		t.Run(test.parent+"/"+test.user+"/"+map[bool]string{true: "recursive", false: "direct"}[test.recursive], func(t *testing.T) {
			result, err := store.QueryItems(ctx, Query{UserID: test.user, Ids: ids, ParentID: test.parent, Recursive: test.recursive})
			if err != nil || result.TotalRecordCount != len(test.want) || !reflect.DeepEqual(queryItemIDs(result.Items), test.want) {
				t.Fatalf("explicit-ID ancestry escaped its parent or traversed an extra: %v, items=%v", err, queryItemIDs(result.Items))
			}
		})
	}
	for _, parent := range []string{"library-a", "missing", "extra-alpha"} {
		if _, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids, ParentID: parent, Recursive: true}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("explicit-ID selection bypassed the ordinary parent boundary: %v", err)
		}
	}
}

func TestStoreExplicitExtraIDsDoNotChangeLatestResumeEntitiesOrOrdinaryBrowse(t *testing.T) {
	ctx, store := extraTestFixture(t)
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media=jsonb_set(media,'{DurationTicks}','3000000000') WHERE id='extra-alpha';
		SELECT sync_catalog_item_entities('extra-alpha','{"Genres":["OnlyExtraGenre"]}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "extra-alpha", PlaybackPositionTicks: 300000000})
	ids := []string{"extra-alpha", "extra-trailer"}
	direct, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: ids, Resumable: true})
	if err != nil || !reflect.DeepEqual(queryItemIDs(direct.Items), []string{"extra-alpha"}) {
		t.Fatalf("the fixture does not qualify for a direct resumable extra selection: %v", err)
	}
	resume, err := store.QueryResume(ctx, Query{UserID: "restricted", Ids: ids})
	if err != nil || resume.TotalRecordCount != 0 || len(resume.Items) != 0 {
		t.Fatalf("Items direct selection changed the separate Resume endpoint: %v", err)
	}
	for _, group := range []bool{true, false} {
		latest, err := store.QueryLatest(ctx, Query{UserID: "restricted", Ids: ids}, group)
		if err != nil || len(latest) != 0 {
			t.Fatalf("Items direct selection changed Latest's ordinary scope: %v", err)
		}
	}
	entities, err := store.ListEntities(ctx, "Genre", Query{UserID: "restricted", Ids: ids})
	if err != nil || entities.TotalRecordCount != 0 || len(entities.Items) != 0 {
		t.Fatalf("Items direct selection exposed auxiliary-only entities: %v", err)
	}
	ordinary, err := store.QueryItems(ctx, Query{UserID: "restricted", ParentID: "library-b", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range ordinary.Items {
		if item.ID == "extra-alpha" || item.ID == "extra-trailer" {
			t.Fatal("the absence of explicit IDs did not retain ordinary browsing")
		}
	}
}

func TestStoreExplicitExtraIDsProjectAuthorityAndAttributesInOneReadSnapshot(t *testing.T) {
	ctx, store := extraTestFixture(t)
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "extra-alpha", PlayCount: 3})
	trace := &extraSnapshotTracer{writer: store.pool, afterPrefix: "SELECT count(*) FROM items i WHERE"}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result, err := (&Store{pool: reader}).QueryItems(ctx, Query{UserID: "restricted", Ids: []string{"extra-alpha"}})
	if err != nil || trace.err != nil || result.TotalRecordCount != 1 || len(result.Items) != 1 ||
		result.Items[0].ExtraKind != ExtraKindClip || result.Items[0].ExtraOwnerName != "Feature Movie" ||
		result.Items[0].UserData == nil || result.Items[0].UserData.PlayCount != 3 {
		t.Fatalf("explicit extra attributes escaped the authorized count snapshot: query=%v, writer=%v", err, trace.err)
	}
	after, err := store.QueryItems(ctx, Query{UserID: "restricted", Ids: []string{"extra-alpha"}})
	if err != nil || after.TotalRecordCount != 0 || len(after.Items) != 0 {
		t.Fatalf("next known-ID request ignored committed policy/relationship changes: %v", err)
	}
}
