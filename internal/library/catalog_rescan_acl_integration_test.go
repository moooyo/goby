//go:build linux

package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// Preserve the real prober's CacheVersion and MusicMetadataVersion methods.
// Counting calls must not turn this into the unversioned fixture prober.
type catalogRescanRealProber struct {
	media.Prober
	calls atomic.Int64
}

func (prober *catalogRescanRealProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	prober.calls.Add(1)
	return prober.Prober.ProbeFile(ctx, file)
}

func TestCatalogRescanRealMediaMovePreservesACLAndDerivedUserDataAcrossStoreReopen(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real-media catalog integration")
	}
	for _, tool := range []string{ffmpeg, ffprobe} {
		info, err := os.Stat(tool)
		if !filepath.IsAbs(tool) || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("real-media tool must be an absolute executable file: %q, error = %v", tool, err)
		}
	}
	prober := &catalogRescanRealProber{Prober: media.Prober{
		FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 8 * time.Second,
	}}
	ctx, pool, store, root, unrestricted := libraryIntegrationStore(t, prober)
	mixedRoot, hiddenRoot := filepath.Join(root, "mixed"), filepath.Join(root, "hidden")
	paths := map[string]string{
		"movie":   filepath.Join(mixedRoot, "Film.mp4"),
		"episode": filepath.Join(mixedRoot, "Show.S01E01.mp4"),
		"t1":      filepath.Join(mixedRoot, "A", "01 Played.flac"),
		"t2":      filepath.Join(mixedRoot, "A", "02 Moving.flac"),
		"t3":      filepath.Join(mixedRoot, "B", "03 Played.flac"),
		"h1":      filepath.Join(hiddenRoot, "Hidden Album", "01 Hidden.flac"),
		"h2":      filepath.Join(hiddenRoot, "Hidden Album", "02 Hidden.flac"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	catalogRescanGenerateMedia(t, ctx, ffmpeg, paths["movie"],
		"-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-threads", "1", "-movflags", "+faststart")
	video, err := os.ReadFile(paths["movie"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths["episode"], video, 0o600); err != nil {
		t.Fatal(err)
	}
	const artistName = "Shared Rescan Artist"
	for _, track := range []struct{ key, title, album string }{
		{"t1", "Track One", "Album A"}, {"t2", "Moving Track", "Album A"},
		{"t3", "Track Three", "Album B"}, {"h1", "Hidden One", "Hidden Album"},
		{"h2", "Hidden Two", "Hidden Album"},
	} {
		catalogRescanGenerateMedia(t, ctx, ffmpeg, paths[track.key],
			"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "0.25", "-ac", "1",
			"-c:a", "flac", "-threads", "1", "-metadata", "title="+track.title,
			"-metadata", "album="+track.album, "-metadata", "artist="+artistName,
			"-metadata", "album_artist="+artistName)
	}
	type fileStamp struct {
		info os.FileInfo
		hash [sha256.Size]byte
	}
	stamps := make(map[string]fileStamp)
	for key, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 || len(data) > 1<<20 {
			t.Fatalf("fixture %s is not a bounded nonempty media file: %v", key, err)
		}
		stamps[key] = fileStamp{info: info, hash: sha256.Sum256(data)}
	}
	mixed := libraryIntegrationCreate(t, ctx, store, "Mixed rescan", "mixed", mixedRoot)
	hidden := libraryIntegrationCreate(t, ctx, store, "Hidden music", "music", hiddenRoot)
	for _, fixture := range []struct {
		libraryID string
		files     int
	}{{mixed.ID, 5}, {hidden.ID, 2}} {
		job := libraryIntegrationScan(t, ctx, store, fixture.libraryID, "Completed")
		if job.Error != "" || job.Scanned != fixture.files || job.Added != fixture.files {
			t.Fatalf("initial real-media scan did not import its complete fixture: %+v", job)
		}
	}
	if prober.calls.Load() != 7 {
		t.Fatalf("initial scans must invoke the real prober once per media file: calls = %d", prober.calls.Load())
	}
	const viewer, other = "rescan-visible-viewer", "rescan-hidden-viewer"
	libraryIntegrationUser(t, ctx, pool, viewer, false, false, []string{mixed.ID})
	libraryIntegrationUser(t, ctx, pool, other, false, false, []string{hidden.ID})
	initial := make(map[string]Item)
	for key, path := range paths {
		libraryID := mixed.ID
		if key == "h1" || key == "h2" {
			libraryID = hidden.ID
		}
		initial[key] = nfoCatalogItem(t, ctx, store, unrestricted, libraryID, path)
	}
	initial["a"] = nfoCatalogItem(t, ctx, store, unrestricted, mixed.ID, filepath.Join(mixedRoot, "A"))
	initial["b"] = nfoCatalogItem(t, ctx, store, unrestricted, mixed.ID, filepath.Join(mixedRoot, "B"))
	if initial["episode"].Series == nil || initial["episode"].Season == nil {
		t.Fatal("real mixed-library episode has no persisted television hierarchy")
	}
	for key, id := range map[string]string{
		"series": initial["episode"].Series.ID, "season": initial["episode"].Season.ID,
	} {
		item, err := store.GetItem(ctx, unrestricted, id)
		if err != nil {
			t.Fatal(err)
		}
		initial[key] = item
	}
	if initial["movie"].Type != "Movie" || initial["episode"].Type != "Episode" ||
		initial["series"].Type != "Series" || initial["season"].Type != "Season" {
		t.Fatal("real mixed scan did not distinguish movie and television identities")
	}
	if len(initial["t1"].Entities.Artists) != 1 {
		t.Fatal("real FLAC metadata did not publish its artist")
	}
	artistID := initial["t1"].Entities.Artists[0].ID
	for _, key := range []string{"t1", "t2", "t3", "h1", "h2"} {
		item := initial[key]
		if item.Type != "Audio" || item.Media == nil || item.Media.EmbeddedMusic == nil ||
			item.Media.ProbeVersion != media.CurrentProbeVersion || item.Media.EmbeddedMusic.Version != media.CurrentMusicMetadataVersion ||
			len(item.Entities.Artists) != 1 || len(item.Entities.AlbumArtists) != 1 ||
			item.Entities.Artists[0].ID != artistID || item.Entities.AlbumArtists[0].ID != artistID {
			t.Fatalf("real audio %s did not retain the shared artist in both indexed credit roles: %+v", key, item)
		}
	}
	lastPlayed := time.Date(2024, 2, 3, 4, 5, 6, 7000, time.UTC)
	for _, value := range []struct {
		key              string
		played, favorite bool
	}{
		{"movie", false, true}, {"episode", false, true}, {"t1", true, false},
		{"t2", false, true}, {"t3", true, false}, {"a", false, true}, {"b", true, true},
		{"h1", true, true},
	} {
		userDataSeed(t, ctx, pool, viewer, UserData{ItemID: initial[value.key].ID, Played: value.played,
			IsFavorite: value.favorite, PlayCount: 4, PlaybackPositionTicks: 12345, LastPlayedDate: &lastPlayed})
	}
	userDataSeed(t, ctx, pool, other, UserData{ItemID: initial["h1"].ID, Played: true, IsFavorite: true, PlayCount: 2})
	userDataSeed(t, ctx, pool, other, UserData{ItemID: initial["h2"].ID, Played: false, PlaybackPositionTicks: 4567})
	userDataSnapshot := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(d) || jsonb_build_object(
			'rowVersion',d.xmin::text) ORDER BY d.user_id,d.item_id),'[]'::jsonb)::text FROM user_item_data d`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	beforeUserData := userDataSnapshot()
	assertIDs := func(result ItemResult, want []string) {
		t.Helper()
		got := make([]string, 0, len(result.Items))
		for _, item := range result.Items {
			got = append(got, item.ID)
		}
		expected := append([]string{}, want...)
		sort.Strings(got)
		sort.Strings(expected)
		if result.TotalRecordCount != len(expected) || !reflect.DeepEqual(got, expected) {
			t.Fatalf("authorized result IDs/count = %v/%d, want %v/%d", got, result.TotalRecordCount, expected, len(expected))
		}
	}
	assertCatalog := func(label string, moved bool) {
		t.Helper()
		t.Log(label)
		for kind, keys := range map[string][]string{"Movie": {"movie"}, "Episode": {"episode"}, "Audio": {"t1", "t2", "t3"}} {
			want := make([]string, 0, len(keys))
			for _, key := range keys {
				want = append(want, initial[key].ID)
			}
			assertIDs(libraryIntegrationQuery(t, ctx, store, Query{UserID: viewer, Recursive: true, IncludeItemTypes: []string{kind}}), want)
		}
		assertIDs(libraryIntegrationQuery(t, ctx, store, Query{UserID: viewer, Recursive: true,
			IncludeItemTypes: []string{"Movie", "Episode", "Audio"}}),
			[]string{initial["movie"].ID, initial["episode"].ID, initial["t1"].ID, initial["t2"].ID, initial["t3"].ID})
		page := libraryIntegrationQuery(t, ctx, store, Query{UserID: viewer, Recursive: true,
			IncludeItemTypes: []string{"Audio"}, SortBy: "Name", StartIndex: 1, Limit: 1})
		if page.TotalRecordCount != 3 || len(page.Items) != 1 || page.Items[0].LibraryID != mixed.ID {
			t.Fatal("paging exposed a hidden source or changed the visible audio total")
		}
		for _, subject := range []struct {
			user        string
			keys        []string
			entityCount int
		}{{viewer, []string{"t1", "t2", "t3"}, 5}, {other, []string{"h1", "h2"}, 3},
			{unrestricted, []string{"t1", "t2", "t3", "h1", "h2"}, 8}} {
			entity, err := store.GetEntityByID(ctx, subject.user, artistID)
			if err != nil || entity.ID != artistID || entity.Name != artistName ||
				entity.Type != "MusicArtist" || entity.Count != subject.entityCount {
				t.Fatalf("shared artist detail counted hidden sources or duplicate credit roles for %s: %+v, error = %v", subject.user, entity, err)
			}
			want := make([]string, 0, len(subject.keys))
			for _, key := range subject.keys {
				want = append(want, initial[key].ID)
			}
			// Artist detail counts all visible associated items, including albums.
			// Audio-only counts use the public artist/album-artist item filters.
			for _, query := range []Query{
				{ArtistIds: []int64{artistID}}, {AlbumArtistIds: []int64{artistID}},
				{ArtistIds: []int64{artistID}, AlbumArtistIds: []int64{artistID}},
			} {
				query.UserID, query.Recursive = subject.user, true
				query.IncludeItemTypes = []string{"Audio"}
				assertIDs(libraryIntegrationQuery(t, ctx, store, query), want)
			}
		}
		for _, pair := range []struct{ user, forbidden string }{{viewer, initial["h1"].ID}, {other, initial["t2"].ID}} {
			assertIDs(libraryIntegrationQuery(t, ctx, store, Query{UserID: pair.user, Ids: []string{pair.forbidden}}), nil)
			if _, err := store.GetItem(ctx, pair.user, pair.forbidden); !errors.Is(err, ErrNotFound) {
				t.Fatalf("hidden item detail error = %v, want ErrNotFound", err)
			}
		}
		if _, err := store.QueryItems(ctx, Query{UserID: other, ParentID: initial["a"].ID}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden album parent query error = %v, want ErrNotFound", err)
		}
		for _, key := range []string{"movie", "episode", "series", "season", "t1", "t2", "t3"} {
			current, err := store.GetItem(ctx, viewer, initial[key].ID)
			if err != nil || current.ID != initial[key].ID || current.LibraryID != mixed.ID || current.Type != initial[key].Type {
				t.Fatalf("mixed item %s lost its stable identity or kind: %+v, error = %v", key, current, err)
			}
			wantParent, wantPath := initial[key].ParentID, initial[key].Path
			if key == "t2" && moved {
				wantParent, wantPath = initial["b"].ID, paths["t2"]
			}
			if current.ParentID != wantParent || current.Path != wantPath {
				t.Fatalf("item %s has the wrong physical/television parent or path: %+v", key, current)
			}
			if key == "movie" || key == "episode" {
				if !reflect.DeepEqual(current.Media, initial[key].Media) || !reflect.DeepEqual(current.Series, initial[key].Series) ||
					!reflect.DeepEqual(current.Season, initial[key].Season) {
					t.Fatalf("unrelated %s probe or television parent facts changed", key)
				}
			}
		}
		for _, album := range []struct {
			key      string
			children []string
			unplayed int
		}{
			{"a", []string{"t1", "t2"}, 1}, {"b", []string{"t3"}, 0},
		} {
			if moved {
				if album.key == "a" {
					album.children, album.unplayed = []string{"t1"}, 0
				} else {
					album.children, album.unplayed = []string{"t2", "t3"}, 1
				}
			}
			item, err := store.GetItem(ctx, viewer, initial[album.key].ID)
			if err != nil {
				t.Fatal(err)
			}
			assertMusicAlbumChildCount(t, item, len(album.children))
			if item.ParentID != mixed.ID || item.Path != initial[album.key].Path || item.UserData == nil ||
				item.UserData.UnplayedItemCount == nil || *item.UserData.UnplayedItemCount != album.unplayed ||
				item.UserData.Played != (album.unplayed == 0) || !item.UserData.IsFavorite {
				t.Fatalf("album %s did not derive current membership while retaining its favorite: %+v", album.key, item)
			}
			wantName := "Album A"
			if album.key == "b" {
				wantName = "Album B"
				if moved {
					wantName = "B"
				}
			}
			if item.Name != wantName {
				t.Fatalf("album %s name = %q, want %q from current member agreement", album.key, item.Name, wantName)
			}
			ids := make([]string, 0, len(album.children))
			for _, key := range album.children {
				ids = append(ids, initial[key].ID)
			}
			assertIDs(libraryIntegrationQuery(t, ctx, store, Query{UserID: viewer, ParentID: item.ID}), ids)
		}
		played := true
		wantPlayed := initial["b"].ID
		if moved {
			wantPlayed = initial["a"].ID
		}
		assertIDs(libraryIntegrationQuery(t, ctx, store, Query{UserID: viewer,
			Ids: []string{initial["a"].ID, initial["b"].ID}, IsPlayed: &played}), []string{wantPlayed})
		latest, err := store.QueryLatest(ctx, Query{UserID: viewer, IncludeItemTypes: []string{"Audio"},
			Ids: []string{initial["t1"].ID, initial["t2"].ID}}, true)
		if err != nil {
			t.Fatal(err)
		}
		wantLatest := map[string][2]int{initial["a"].ID: {2, 2}}
		if moved {
			wantLatest = map[string][2]int{initial["a"].ID: {1, 1}, initial["b"].ID: {1, 2}}
		}
		if len(latest) != len(wantLatest) {
			t.Fatalf("moved track did not change filtered latest groups: %+v", latest)
		}
		for _, entry := range latest {
			want, ok := wantLatest[entry.Item.ID]
			if !ok || entry.ChildCount != want[0] || entry.Item.ChildCount == nil || *entry.Item.ChildCount != want[1] {
				t.Fatalf("filtered Latest count was confused with the album's full direct-child count: %+v", entry)
			}
		}
		moving, err := store.GetItem(ctx, viewer, initial["t2"].ID)
		if err != nil || moving.Media == nil || moving.Media.EmbeddedMusic == nil || moving.Media.EmbeddedMusic.Album != "Album A" || moving.Album == nil {
			t.Fatal("moving track lost its accepted embedded metadata or physical album reference")
		}
		wantAlbum := initial["a"].ID
		if moved {
			wantAlbum = initial["b"].ID
		}
		if moving.Album.ID != wantAlbum {
			t.Fatal("embedded Album text replaced the moved track's physical album identity")
		}
		if userDataSnapshot() != beforeUserData {
			t.Fatal("scan, query or Store reopen rewrote existing user data, timestamps or row versions")
		}
		for key, path := range paths {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			stamp := stamps[key]
			if err != nil || !os.SameFile(info, stamp.info) || info.Size() != stamp.info.Size() ||
				!info.ModTime().Equal(stamp.info.ModTime()) || sha256.Sum256(data) != stamp.hash {
				t.Fatalf("catalog work modified source media %s: %v", key, err)
			}
		}
	}
	assertCatalog("before the physical move", false)
	oldPath := paths["t2"]
	newPath := filepath.Join(mixedRoot, "B", filepath.Base(oldPath))
	if _, err := os.Lstat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("move destination must start absent: %v", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	paths["t2"] = newPath
	afterRename, err := os.Stat(newPath)
	if err != nil || !os.SameFile(stamps["t2"].info, afterRename) || afterRename.Size() != stamps["t2"].info.Size() ||
		!catalogModifiedTime(afterRename).Equal(catalogModifiedTime(stamps["t2"].info)) {
		t.Fatalf("fixture rename did not retain the scanner's dev/inode/size/mtime match: %v", err)
	}
	if _, err := os.Lstat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old path must disappear before it is a rename candidate: %v", err)
	}
	// findStoredFileForRole reuses this same-library identity only after the old
	// path disappears. A real prober may refresh T2 because rename changes ctime;
	// that does not grant permission to replace its catalog ID or user history.
	job := libraryIntegrationScan(t, ctx, store, mixed.ID, "Completed")
	if job.Error != "" || job.Scanned != 5 || job.Added != 0 || job.Updated != 1 {
		t.Fatalf("the one physical move did not produce one retained-identity update: %+v", job)
	}
	assertCatalog("after the physical move and rescan", true)
	callsAfterMove := prober.calls.Load()
	closeCtx, cancelClose := context.WithTimeout(ctx, 10*time.Second)
	err = store.Close(closeCtx)
	cancelClose()
	if err != nil {
		t.Fatal(err)
	}
	// This closes/reopens the catalog Store in the same test process. It is not
	// an OS service restart, PostgreSQL restart, mount test or user-ID transition.
	reopened, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := reopened.Close(cleanupCtx); err != nil {
			t.Errorf("close reopened catalog Store: %v", err)
		}
	})
	store = reopened
	assertCatalog("after Store.Close/New without another scan", true)
	if prober.calls.Load() != callsAfterMove {
		t.Fatal("Store reopen or read-only catalog queries unexpectedly probed media")
	}
	job = libraryIntegrationScan(t, ctx, store, mixed.ID, "Completed")
	if job.Error != "" || job.Scanned != 5 || job.Added != 0 || job.Updated != 0 || prober.calls.Load() != callsAfterMove {
		t.Fatalf("reopened Store did not reuse the unchanged persisted probe cache: %+v, calls = %d/%d", job, prober.calls.Load(), callsAfterMove)
	}
	assertCatalog("after the reopened Store's cache-aware rescan", true)
}

func catalogRescanGenerateMedia(t *testing.T, ctx context.Context, ffmpeg, path string, encoding ...string) {
	t.Helper()
	childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	arguments := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-nostats", "-n", "-filter_threads", "1"}
	arguments = append(arguments, encoding...)
	arguments = append(arguments, "-fs", "1048576", path)
	command := exec.CommandContext(childCtx, ffmpeg, arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TZ=UTC"}
	command.WaitDelay = 2 * time.Second
	command.Stdout = io.Discard
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := stderr.Bytes()
		if len(message) > 4096 {
			message = message[:4096]
		}
		t.Fatalf("generate local fixture %s: %v, context = %v, stderr = %s", filepath.Base(path), err, childCtx.Err(), message)
	}
}
