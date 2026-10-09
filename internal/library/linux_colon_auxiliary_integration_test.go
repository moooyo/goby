//go:build linux

package library

import (
	"path/filepath"
	"testing"
)

func TestLinuxColonAuxiliaryPathsStayReservedAcrossScans(t *testing.T) {
	for _, owner := range []string{"C:Movie", "C:"} {
		t.Run(owner, func(t *testing.T) {
			ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			moviePath := libraryIntegrationFile(t, root, "movies/"+owner+"/Main.mp4", "video:main")
			songPath := libraryIntegrationFile(t, root, "movies/"+owner+"/theme.mp3", "audio:theme")
			clipPath := libraryIntegrationFile(t, root, "movies/"+owner+"/featurettes/Clip.mp4", "video:extra")
			libraryIntegrationFile(t, root, "movies/"+owner+"/featurettes/nested/Hidden.mp4", "video:hidden")
			lib := libraryIntegrationCreate(t, ctx, store, "Linux colon auxiliaries", "movies", filepath.Join(root, "movies"))
			for scan := 0; scan < 2; scan++ {
				if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
					t.Fatalf("colon auxiliary scan warned: %+v", job)
				}
				ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: lib.ID, Recursive: true})
				movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
				for _, item := range ordinary.Items {
					if !item.IsFolder && item.ID != movie.ID {
						t.Fatalf("colon auxiliary escaped into the ordinary catalog: %+v", item)
					}
				}
				themes := themeScanTestResources(t, ctx, pool, lib.ID)
				extras := extraScanTestResources(t, ctx, pool, lib.ID)
				if len(themes) != 1 || themes[0].path != songPath || !themes[0].active || themes[0].ownerID != movie.ID ||
					len(extras) != 1 || extras[0].path != clipPath || !extras[0].active || extras[0].ownerID != movie.ID {
					t.Fatalf("colon auxiliary identities or ownership changed: themes=%+v extras=%+v", themes, extras)
				}
				for _, resource := range append(themes, extras...) {
					item, err := store.GetItem(ctx, userID, resource.id)
					if err != nil || item.ParentID != movie.ID {
						t.Fatalf("direct colon resource lost its authorized owner: %+v, %v", item, err)
					}
				}
				features, err := store.QuerySpecialFeatures(ctx, movie.ID, Subject{UserID: userID})
				if err != nil || len(features) != 1 || features[0].ID != extras[0].id {
					t.Fatalf("colon extra is absent from SpecialFeatures: %+v, %v", features, err)
				}
				if themes := themeScanTestQuery(t, ctx, store, userID, movie.ID); themes.ThemeSongsResult.TotalRecordCount != 1 {
					t.Fatalf("colon theme is absent from its owner: %+v", themes)
				}
			}
		})
	}
}
