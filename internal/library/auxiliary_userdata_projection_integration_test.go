//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func auxiliaryItemsWithoutUserData(t *testing.T, items []Item) []Item {
	t.Helper()
	if items == nil {
		return nil
	}
	expected := make([]Item, len(items))
	copy(expected, items)
	for index, item := range items {
		if supportsUserData(item.Type) && item.UserData == nil {
			t.Fatalf("the default auxiliary query omitted user data for %s", item.ID)
		}
		expected[index].UserData = nil
	}
	return expected
}

func TestAuxiliaryUserDataProjectionPreservesAncestors(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	userDataSeed(t, ctx, fixture.pool, "restricted", UserData{ItemID: "season-b", IsFavorite: true})
	userDataSeed(t, ctx, fixture.pool, "restricted", UserData{ItemID: "episode-b1", Played: true})
	store, trace := tracedDirectItemStore(t, ctx, fixture)
	subject := Subject{UserID: "restricted"}
	complete, err := store.Ancestors(ctx, subject, "episode-b1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(queryItemIDs(complete), []string{"season-b", "series-b", "library-b"}) ||
		complete[0].UserData == nil || !complete[0].UserData.IsFavorite ||
		complete[0].UserData.UnplayedItemCount == nil || trace.count("folder_descendants") == 0 {
		t.Fatal("the default ancestor query lost its ordered parents or folder user data")
	}
	trace.reset()
	omitted, err := store.Ancestors(ctx, subject, "episode-b1", QueryProjection{UserDataDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(auxiliaryItemsWithoutUserData(t, complete), omitted) || trace.count("user_item_data") != 0 {
		t.Fatal("ancestor projection changed visible parents or queried omitted user data")
	}
	if _, err := store.Ancestors(ctx, Subject{UserID: "none"}, "episode-b1", QueryProjection{UserDataDisabled: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ancestor projection bypassed seed authorization: %v", err)
	}
}

func TestAuxiliaryUserDataProjectionPreservesExtraResources(t *testing.T) {
	ctx, fixture := extraTestFixture(t)
	for _, id := range []string{"extra-alpha", "extra-trailer"} {
		userDataSeed(t, ctx, fixture.pool, "restricted", UserData{ItemID: id, IsFavorite: true, PlayCount: 3})
	}
	store, trace := tracedDirectItemStore(t, ctx, fixture)
	subject := Subject{UserID: "restricted"}
	for _, test := range []struct {
		name  string
		query func(context.Context, string, Subject, ...QueryProjection) ([]Item, error)
		ids   []string
	}{
		{"special features", store.QuerySpecialFeatures, []string{"extra-alpha", "extra-middle", "extra-zeta"}},
		{"local trailers", store.QueryLocalTrailers, []string{"extra-trailer"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.reset()
			complete, err := test.query(ctx, "theme-seed", subject)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(queryItemIDs(complete), test.ids) || complete[0].UserData == nil ||
				!complete[0].UserData.IsFavorite || complete[0].UserData.PlayCount != 3 || trace.count("user_item_data") == 0 {
				t.Fatal("the default extra query lost ordered resources or their saved user data")
			}
			trace.reset()
			omitted, err := test.query(ctx, "theme-seed", subject, QueryProjection{UserDataDisabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(auxiliaryItemsWithoutUserData(t, complete), omitted) || trace.count("user_item_data") != 0 {
				t.Fatal("extra projection changed resource fields or queried omitted user data")
			}
			if _, err := test.query(ctx, "theme-hidden", subject, QueryProjection{UserDataDisabled: true}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("extra projection bypassed owner authorization: %v", err)
			}
		})
	}
}

func TestAuxiliaryUserDataProjectionPreservesThemeMedia(t *testing.T) {
	ctx, fixture := themeTestFixture(t)
	for _, id := range []string{"theme-song", "theme-video", "theme-series-song"} {
		userDataSeed(t, ctx, fixture.pool, "restricted", UserData{ItemID: id, IsFavorite: true, PlayCount: 4})
	}
	store, trace := tracedDirectItemStore(t, ctx, fixture)
	for _, test := range []struct {
		name, seed        string
		inherit           bool
		songIDs, videoIDs []string
	}{
		{"songs", "theme-seed", false, []string{"theme-song"}, []string{}},
		{"videos", "theme-video-owner", false, []string{}, []string{"theme-video"}},
		{"inherited songs", "theme-episode1", true, []string{"theme-series-song"}, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := ThemeQuery{Subject: Subject{UserID: "restricted"}, InheritFromParent: test.inherit,
				EnableThemeSongs: true, EnableThemeVideos: true}
			trace.reset()
			complete := themeTestQuery(t, ctx, store, test.seed, query)
			if !reflect.DeepEqual(queryItemIDs(complete.ThemeSongsResult.Items), test.songIDs) ||
				!reflect.DeepEqual(queryItemIDs(complete.ThemeVideosResult.Items), test.videoIDs) || trace.count("user_item_data") == 0 {
				t.Fatal("the default theme query lost its selected resource population or user data")
			}
			for _, group := range []ThemeResult{complete.ThemeSongsResult, complete.ThemeVideosResult} {
				for _, item := range group.Items {
					if item.UserData == nil || !item.UserData.IsFavorite || item.UserData.PlayCount != 4 {
						t.Fatalf("the default theme query lost saved user data for %s", item.ID)
					}
				}
			}
			trace.reset()
			query.Projection.UserDataDisabled = true
			omitted := themeTestQuery(t, ctx, store, test.seed, query)
			complete.ThemeSongsResult.Items = auxiliaryItemsWithoutUserData(t, complete.ThemeSongsResult.Items)
			complete.ThemeVideosResult.Items = auxiliaryItemsWithoutUserData(t, complete.ThemeVideosResult.Items)
			complete.SoundtrackSongsResult.Items = auxiliaryItemsWithoutUserData(t, complete.SoundtrackSongsResult.Items)
			if !reflect.DeepEqual(complete, omitted) || trace.count("user_item_data") != 0 {
				t.Fatal("theme projection changed owners, counts, or resources, or queried omitted user data")
			}
		})
	}
	if _, err := store.QueryThemeMedia(ctx, "theme-hidden", ThemeQuery{Subject: Subject{UserID: "restricted"},
		EnableThemeSongs: true, Projection: QueryProjection{UserDataDisabled: true}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("theme projection bypassed seed authorization: %v", err)
	}
}

func TestAuxiliaryUserDataProjectionPreservesAdditionalParts(t *testing.T) {
	ctx, pool, store, root, user := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	firstPath := libraryIntegrationFile(t, root, "parts/Movie - part1.mkv", "video:first")
	secondPath := libraryIntegrationFile(t, root, "parts/Movie - part2.mkv", "video:second")
	collection := libraryIntegrationCreate(t, ctx, store, "Projected parts", "movies", filepath.Join(root, "parts"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: collection.ID, Recursive: true, Limit: 100}).Items
	first := libraryIntegrationItemByPath(t, items, firstPath)
	second := libraryIntegrationItemByPath(t, items, secondPath)
	userDataSeed(t, ctx, pool, user, UserData{ItemID: second.ID, IsFavorite: true, PlayCount: 4})
	trace := &directItemProjectionTrace{}
	config := pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, reader)
	store.pool = reader
	t.Cleanup(func() { store.pool = pool })
	subject := Subject{UserID: user}
	complete, err := store.AdditionalParts(ctx, subject, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if complete.TotalRecordCount != 1 || !reflect.DeepEqual(queryItemIDs(complete.Items), []string{second.ID}) ||
		complete.Items[0].UserData == nil || !complete.Items[0].UserData.IsFavorite ||
		complete.Items[0].UserData.PlayCount != 4 || trace.count("user_item_data") == 0 {
		t.Fatal("the default additional-parts query lost a contiguous source or its saved user data")
	}
	trace.reset()
	omitted, err := store.AdditionalParts(ctx, subject, first.ID, QueryProjection{UserDataDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	complete.Items = auxiliaryItemsWithoutUserData(t, complete.Items)
	if !reflect.DeepEqual(complete, omitted) || trace.count("user_item_data") != 0 {
		t.Fatal("additional-parts projection changed sources or counts, or queried omitted user data")
	}
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdditionalParts(ctx, subject, first.ID, QueryProjection{UserDataDisabled: true}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("additional-parts projection bypassed source validation: %v", err)
	}
}
