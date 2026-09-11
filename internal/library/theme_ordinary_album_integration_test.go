package library

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func themeOrdinaryAlbumHierarchy(t *testing.T, ctx context.Context, store *Store, userID, libraryID, trackPath, albumPath string) (Item, Item) {
	t.Helper()
	result := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: libraryID, Recursive: true})
	if result.TotalRecordCount != 2 || len(result.Items) != 2 {
		t.Fatalf("ordinary album hierarchy contains %d items with total %d, want one album and one track", len(result.Items), result.TotalRecordCount)
	}
	track := libraryIntegrationItemByPath(t, result.Items, trackPath)
	if track.Type != "Audio" || track.IsFolder || track.ParentID == libraryID || track.Media == nil {
		t.Fatalf("ordinary audio lost its accepted media or album parent: %+v", track)
	}
	var album Item
	for _, item := range result.Items {
		if item.ID == track.ParentID {
			album = item
		}
	}
	if album.ID == "" || album.Type != "MusicAlbum" || !album.IsFolder || album.ParentID != libraryID || album.Path != albumPath {
		t.Fatalf("ordinary audio parent is not the expected album: album=%+v, want path %q", album, albumPath)
	}
	return album, track
}

func TestThemeScanOrdinaryDriveLikeAudioKeepsAlbumHierarchy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("colon-containing ordinary filenames require the Linux scanner boundary")
	}
	for _, collectionType := range []string{"music", "mixed"} {
		for _, directory := range []string{"", "Album"} {
			location := "Root"
			if directory != "" {
				location = "Subdirectory"
			}
			t.Run(collectionType+"/"+location, func(t *testing.T) {
				prober := &libraryFixtureProber{}
				ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
				root := filepath.Join(allowedRoot, "library")
				// This is the only ordinary audio in the directory. A second
				// conventional name would hide the album-detection regression.
				trackPath := libraryIntegrationFile(t, allowedRoot,
					filepath.ToSlash(filepath.Join("library", directory, "C:Track.mp3")), "audio:ordinary-colon-track")
				library := libraryIntegrationCreate(t, ctx, store, "Ordinary colon audio", collectionType, root)
				albumPath, albumRelative := "", "//album/root"
				if directory != "" {
					albumPath, albumRelative = filepath.Join(root, directory), directory
				}
				var firstAlbum, firstTrack Item
				for scan := 0; scan < 2; scan++ {
					job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
					if job.Error != "" {
						t.Fatalf("ordinary colon filename produced a scan warning: %+v", job)
					}
					album, track := themeOrdinaryAlbumHierarchy(t, ctx, store, userID, library.ID, trackPath, albumPath)
					var relative string
					if err := pool.QueryRow(ctx, "SELECT relative_path FROM items WHERE id=$1", album.ID).Scan(&relative); err != nil || relative != albumRelative {
						t.Fatalf("album source identity = %q, want %q; error=%v", relative, albumRelative, err)
					}
					if scan == 0 {
						firstAlbum, firstTrack = album, track
					} else if album.ID != firstAlbum.ID || track.ID != firstTrack.ID || track.ParentID != firstTrack.ParentID ||
						job.Added != 0 || job.Updated != 0 {
						t.Fatalf("stable rescan changed the ordinary album or track identity: album=%+v, track=%+v, job=%+v", album, track, job)
					}
					if calls := len(prober.calls()); calls != 1 {
						t.Fatalf("ordinary audio was not probed exactly once across cached scans: %d", calls)
					}
				}
			})
		}
	}
}

func TestThemeScanCanonicalThemeBesideOrdinaryAudioStaysReserved(t *testing.T) {
	for _, collectionType := range []string{"music", "mixed"} {
		t.Run(collectionType, func(t *testing.T) {
			prober := &libraryFixtureProber{}
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
			trackPath := libraryIntegrationFile(t, allowedRoot, "library/Album/01 Track.mp3", "audio:ordinary-control-track")
			themePath := libraryIntegrationFile(t, allowedRoot, "library/Album/theme.mp3", "audio:canonical-theme-control")
			library := libraryIntegrationCreate(t, ctx, store, "Canonical theme control", collectionType, filepath.Join(allowedRoot, "library"))
			var firstAlbum, firstTrack Item
			var firstThemeID string
			for scan := 0; scan < 2; scan++ {
				job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
				if job.Error != "" {
					t.Fatalf("canonical theme control produced a scan warning: %+v", job)
				}
				album, track := themeOrdinaryAlbumHierarchy(t, ctx, store, userID, library.ID, trackPath, filepath.Dir(trackPath))
				resources := themeScanTestResources(t, ctx, pool, library.ID)
				if len(resources) != 1 || resources[0].path != themePath || resources[0].ownerID != album.ID ||
					resources[0].kind != "song" || resources[0].itemType != "Audio" || !resources[0].active || resources[0].id == track.ID {
					t.Fatalf("canonical theme did not keep its separate active album-owned role: %+v", resources)
				}
				var reserved bool
				if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM theme_reserved_paths reserved
					JOIN library_roots root ON root.id=reserved.root_id
					WHERE root.library_id=$1 AND reserved.relative_path='Album/theme.mp3' AND NOT reserved.is_directory)`, library.ID).Scan(&reserved); err != nil || !reserved {
					t.Fatalf("canonical theme lost its permanent file classification: reserved=%t, error=%v", reserved, err)
				}
				if scan == 0 {
					firstAlbum, firstTrack, firstThemeID = album, track, resources[0].id
				} else if album.ID != firstAlbum.ID || track.ID != firstTrack.ID || track.ParentID != firstTrack.ParentID ||
					resources[0].id != firstThemeID || job.Added != 0 || job.Updated != 0 {
					t.Fatalf("cached scan changed ordinary or reserved control identities: album=%+v, track=%+v, resources=%+v, job=%+v", album, track, resources, job)
				}
				if calls := len(prober.calls()); calls != 2 {
					t.Fatalf("ordinary and theme audio were not each probed once across cached scans: %d", calls)
				}
			}
		})
	}
}
