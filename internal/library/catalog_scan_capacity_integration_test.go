//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

const (
	catalogCapacityMediaBytes     = int64(98_304_000)
	catalogCapacityAllocatedBytes = int64(128 << 20)
	catalogCapacityItems          = 10_688
	catalogCapacityArtist         = "Shared Capacity Artist"
	// Execution limits are not latency or throughput acceptance targets. The
	// 1500-second test-binary limit leaves time for independently bounded cleanup.
	catalogCapacityDeadline = 22 * time.Minute
	catalogCapacityScanWait = 20 * time.Minute
)

type catalogCapacitySource struct {
	kind  string
	bytes int64
	hash  [sha256.Size]byte
}

type catalogCapacityFileStamp struct {
	device, inode, links         uint64
	bytes, modifiedNS, changedNS int64
	mode                         os.FileMode
	hash                         [sha256.Size]byte
}

type catalogCapacityRecord struct {
	id, libraryID, rootID, parentID, kind, path, relative, name, sortName string
	folder                                                                bool
	index, parentIndex                                                    int
}

func TestCatalogRealMediaCapacityScanAndCachedRescan(t *testing.T) {
	started := time.Now()
	defer func() { t.Logf("capacity profile total elapsed=%s", time.Since(started)) }()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for real-media catalog capacity integration")
	}
	for _, tool := range []string{ffmpeg, ffprobe} {
		info, err := os.Stat(tool)
		if !filepath.IsAbs(tool) || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("real-media tool must be an absolute executable file: %q, error = %v", tool, err)
		}
	}
	prober := &catalogRescanRealProber{Prober: media.Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 8 * time.Second}}
	ctx, pool, store, root, unrestricted := libraryIntegrationStoreWithTimeout(t, prober, catalogCapacityDeadline)
	// The runner places test temporary files on its owned ext4 filesystem. This
	// test neither mounts storage nor switches process UID nor reads old media.
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	observation, observeErr := ObserveRootStorageIdentity(directory)
	closeErr := directory.Close()
	if observeErr != nil || closeErr != nil || observation.Identity.Validate() != nil {
		t.Fatalf("capacity fixture requires a verified storage identity: observation=%v close=%v", observeErr, closeErr)
	}

	generationStarted := time.Now()
	videoPath := filepath.Join(root, "A", "Movies", "A Movie0001.mp4")
	audioPath := filepath.Join(root, "A", "Music", "A Album001", "01 A Album001 Track01.flac")
	for _, path := range []string{videoPath, audioPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Templates are two of the corpus members. The generator's larger emergency
	// file limit cannot make an oversized template acceptable: no profile output
	// is trimmed to the 8/16 KiB acceptance limits below.
	catalogRescanGenerateMedia(t, ctx, ffmpeg, videoPath,
		"-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-threads", "1", "-movflags", "+faststart")
	catalogRescanGenerateMedia(t, ctx, ffmpeg, audioPath,
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "0.25", "-ac", "1",
		"-c:a", "flac", "-threads", "1", "-metadata", "artist="+catalogCapacityArtist,
		"-metadata", "album_artist="+catalogCapacityArtist, "-metadata", "title=", "-metadata", "album=")
	readTemplate := func(path string, limit int) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 || len(data) > limit {
			t.Fatalf("generated template exceeds its nonempty profile limit: bytes=%d limit=%d error=%v", len(data), limit, err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		return data
	}
	video, audio := readTemplate(videoPath, 8<<10), readTemplate(audioPath, 16<<10)
	sources := make(map[string]catalogCapacitySource, 10_200)
	add := func(relative, kind string, data []byte) {
		t.Helper()
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		if _, exists := sources[path]; exists {
			t.Fatal("duplicate capacity source pathname")
		}
		if path != videoPath && path != audioPath {
			catalogCapacityWriteExclusive(t, path, data)
		}
		sources[path] = catalogCapacitySource{kind: kind, bytes: int64(len(data)), hash: sha256.Sum256(data)}
	}
	for _, label := range []string{"A", "B"} {
		for movie := 1; movie <= 2000; movie++ {
			add(fmt.Sprintf("%s/Movies/%s Movie%04d.mp4", label, label, movie), "Movie", video)
		}
		for series := 1; series <= 20; series++ {
			for season := 1; season <= 5; season++ {
				for episode := 1; episode <= 20; episode++ {
					add(fmt.Sprintf("%s/Television/%s Show%02d/Season%02d/%s Show%02d.S%02dE%02d.mp4",
						label, label, series, season, label, series, season, episode), "Episode", video)
				}
			}
		}
		for album := 1; album <= 100; album++ {
			name := fmt.Sprintf("%s Album%03d", label, album)
			// Audio has no per-track NFO contract. Filenames supply track titles;
			// album.nfo describes the existing physical MusicAlbum boundary.
			add(fmt.Sprintf("%s/Music/%s/album.nfo", label, name), "", []byte("<album><title>"+name+"</title></album>"))
			for track := 1; track <= 10; track++ {
				add(fmt.Sprintf("%s/Music/%s/%02d %s Track%02d.flac", label, name, track, name, track), "Audio", audio)
			}
		}
	}
	beforeFiles, mediaBytes, allocatedBytes := catalogCapacityTree(t, ctx, root, sources, 10_000)
	t.Logf("generation elapsed=%s media_files=10000 video_template_bytes=%d audio_template_bytes=%d media_bytes=%d corpus_allocated_bytes=%d",
		time.Since(generationStarted), len(video), len(audio), mediaBytes, allocatedBytes)
	libraries := make(map[string]Library, 2)
	for _, label := range []string{"A", "B"} {
		libraries[label] = libraryIntegrationCreate(t, ctx, store, "Capacity "+label, "mixed", filepath.Join(root, label))
	}
	users := []string{"capacity-visible-a", "capacity-visible-b", unrestricted}
	for index, label := range []string{"A", "B"} {
		libraryIntegrationUser(t, ctx, pool, users[index], false, false, []string{libraries[label].ID})
	}
	actor := metadataEditTestActor(t, ctx, pool, "capacity-binding-observer")
	bindings := make(map[string]RootBindingInfo, 2)
	rootIDs := make(map[string]string, 2)
	assertBindings := func() {
		t.Helper()
		for _, label := range []string{"A", "B"} {
			library := libraries[label]
			roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
			if err != nil || len(roots) != 1 {
				t.Fatalf("expected one capacity root: count=%d error=%v", len(roots), err)
			}
			binding, err := store.GetRootBinding(ctx, actor, library.ID, roots[0].RootID)
			if err != nil || binding.Status != RootBindingVerified || binding.Revision != "1" || binding.BoundAt == nil || binding.BoundBy != "system" ||
				binding.Path != filepath.Join(root, label) || binding.ApprovedFingerprint == "" || binding.ApprovedFingerprint != binding.ObservedFingerprint ||
				binding.Approved == nil || binding.Observed == nil {
				t.Fatalf("capacity root %s is not independently verified: status=%s error=%v", label, binding.Status, err)
			}
			if before, exists := bindings[label]; exists && (binding.RegisteredRootInfo != before.RegisteredRootInfo ||
				binding.ApprovedFingerprint != before.ApprovedFingerprint || before.BoundAt == nil || !binding.BoundAt.Equal(*before.BoundAt) ||
				!reflect.DeepEqual(binding.Approved, before.Approved)) {
				t.Fatal("capacity scan or Store reopen changed the storage approval")
			}
			bindings[label] = binding
			rootIDs[library.ID] = binding.RootID
		}
	}
	assertBindings()
	scan := func(label string, wantAddedA, wantAddedB int) {
		t.Helper()
		phaseStarted, probesBefore := time.Now(), prober.calls.Load()
		var jobs [2]Job
		for index, libraryLabel := range []string{"A", "B"} {
			var err error
			jobs[index], err = store.StartScan(ctx, libraries[libraryLabel].ID)
			if err != nil {
				t.Fatalf("start owned capacity scan %s: %v", libraryLabel, err)
			}
		}
		for index, wantAdded := range []int{wantAddedA, wantAddedB} {
			job := libraryIntegrationWaitJobWithTimeout(t, ctx, store, jobs[index].ID, "Completed", catalogCapacityScanWait)
			// Job.Error includes inspection warnings and retained-evidence notices.
			if job.Error != "" || job.Scanned != 5000 || job.Added != wantAdded || job.Updated != 0 || job.ForceProbe || job.CancelRequested || job.StartedAt == nil || job.FinishedAt == nil {
				t.Fatalf("capacity scan %s/%d did not complete the exact corpus: %+v", label, index, job)
			}
			t.Logf("%s library=%d job_elapsed=%s scanned=%d added=%d updated=%d", label, index,
				job.FinishedAt.Sub(*job.StartedAt), job.Scanned, job.Added, job.Updated)
		}
		assertBindings()
		t.Logf("%s elapsed=%s probe_calls=%d total_probe_calls=%d", label, time.Since(phaseStarted), prober.calls.Load()-probesBefore, prober.calls.Load())
	}
	scan("cold_scan", 5000, 5000)
	if prober.calls.Load() != 10_000 {
		t.Fatalf("cold real-prober calls=%d, want 10000", prober.calls.Load())
	}
	initial := catalogCapacityReadCatalog(t, ctx, pool)
	catalogCapacityAssertCorpus(t, initial, sources, libraries, rootIDs)
	removed := catalogCapacityFindPath(t, initial, videoPath)
	for index, label := range []string{"A", "B"} {
		lastPlayed := time.Date(2024, 2, 3, 4, 5, 6, 7000, time.UTC)
		for _, seed := range []struct {
			relative         string
			played, favorite bool
			position         int64
			count            int
		}{
			{fmt.Sprintf("Movies/%s Movie0002.mp4", label), false, true, 12345, 3},
			{fmt.Sprintf("Television/%s Show01/Season01/%s Show01.S01E01.mp4", label, label), true, false, 0, 2},
			{fmt.Sprintf("Music/%s Album001/01 %s Album001 Track01.flac", label, label), true, true, 0, 4},
			{fmt.Sprintf("Music/%s Album001", label), true, true, 0, 5},
		} {
			item := catalogCapacityFindPath(t, initial, filepath.Join(root, label, filepath.FromSlash(seed.relative)))
			if item.id == removed.id {
				t.Fatal("replacement target must have no seeded user history")
			}
			userDataSeed(t, ctx, pool, users[index], UserData{ItemID: item.id, Played: seed.played, IsFavorite: seed.favorite,
				PlaybackPositionTicks: seed.position, PlayCount: seed.count, LastPlayedDate: &lastPlayed})
		}
	}
	userDataBefore := catalogCapacityUserData(t, ctx, pool)
	assertCatalog := func(label string, records []catalogCapacityRecord) {
		t.Helper()
		phaseStarted := time.Now()
		catalogCapacityAssertCorpus(t, records, sources, libraries, rootIDs)
		catalogCapacityAssertViews(t, ctx, store, root, records, libraries, users)
		if catalogCapacityUserData(t, ctx, pool) != userDataBefore {
			t.Fatal("capacity work rewrote existing UserData columns, timestamps or xmin")
		}
		t.Logf("%s read_assertions_elapsed=%s catalog_items=%d leaf_items=10000 userdata_rows=8", label, time.Since(phaseStarted), len(records))
	}
	assertCatalog("after_cold_scan", initial)
	filesAfterCold, _, _ := catalogCapacityTree(t, ctx, root, sources, 10_000)
	if !reflect.DeepEqual(beforeFiles, filesAfterCold) {
		t.Fatal("cold scans modified a corpus file")
	}

	reopenStarted := time.Now()
	catalogCapacityCloseStore(t, store)
	reopened, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := reopened.Close(cleanupCtx); err != nil {
			t.Errorf("close reopened capacity Store: %v", err)
		}
	})
	store = reopened
	// This is an in-process Store reopen over the same pool, not a native Goby
	// service/PG restart, mount failure, or host durability acceptance test.
	afterReopen := catalogCapacityReadCatalog(t, ctx, pool)
	if !reflect.DeepEqual(initial, afterReopen) || prober.calls.Load() != 10_000 {
		t.Fatal("Store reopen changed catalog identities or probed media")
	}
	assertBindings()
	assertCatalog("after_reopen_before_scan", afterReopen)
	t.Logf("reopen elapsed=%s additional_probe_calls=%d", time.Since(reopenStarted), prober.calls.Load()-10_000)

	// Keep the old inode allocated until the new file exists. Inode recycling
	// could otherwise legitimately satisfy the scanner's same-library move rule.
	replacementPath := filepath.Join(root, "A", "Movies", "A Replacement Movie.mp4")
	if mediaBytes+int64(len(video)) > catalogCapacityMediaBytes {
		t.Fatal("temporary replacement would exceed the media budget")
	}
	catalogCapacityWriteExclusive(t, replacementPath, video)
	sources[replacementPath] = catalogCapacitySource{kind: "Movie", bytes: int64(len(video)), hash: sha256.Sum256(video)}
	withReplacement, _, _ := catalogCapacityTree(t, ctx, root, sources, 10_001)
	for path, before := range beforeFiles {
		if withReplacement[path] != before {
			t.Fatal("replacement creation modified an existing file")
		}
	}
	newStamp, oldStamp := withReplacement[replacementPath], beforeFiles[videoPath]
	if newStamp.device == oldStamp.device && newStamp.inode == oldStamp.inode {
		t.Fatal("replacement reused the still-existing inode")
	}
	if err := os.Remove(videoPath); err != nil {
		t.Fatal(err)
	}
	delete(sources, videoPath)
	delete(withReplacement, videoPath)
	if _, err := os.Lstat(videoPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned old pathname did not disappear: %v", err)
	}
	assertBindings()
	scan("cached_rescan", 1, 0)
	if prober.calls.Load() != 10_001 {
		t.Fatalf("persisted probe cache was not reused: calls=%d", prober.calls.Load())
	}
	after := catalogCapacityReadCatalog(t, ctx, pool)
	replacement := catalogCapacityFindPath(t, after, replacementPath)
	if replacement.id == removed.id || replacement.libraryID != libraries["A"].ID || replacement.kind != "Movie" {
		t.Fatal("replacement did not receive its own catalog identity")
	}
	beforeIDs := make(map[string]catalogCapacityRecord, len(initial))
	for _, record := range initial {
		beforeIDs[record.id] = record
	}
	for _, record := range after {
		if record.id == removed.id {
			t.Fatal("completed scan retained the missing item instead of reconciling it")
		}
		if record.id == replacement.id {
			continue
		}
		if before, exists := beforeIDs[record.id]; !exists || before != record {
			t.Fatal("rescan changed an unrelated identity or hierarchy")
		}
		delete(beforeIDs, record.id)
	}
	if len(beforeIDs) != 1 || beforeIDs[removed.id] != removed {
		t.Fatal("rescan removed something other than the missing movie")
	}
	var missingRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE id=$1 OR path=$2`, removed.id, videoPath).Scan(&missingRows); err != nil || missingRows != 0 {
		t.Fatalf("missing item deletion was not committed: rows=%d error=%v", missingRows, err)
	}
	if _, err := store.GetItem(ctx, unrestricted, removed.id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted movie still resolves: %v", err)
	}
	assertCatalog("after_cached_rescan", after)
	finalFiles, finalMediaBytes, finalAllocatedBytes := catalogCapacityTree(t, ctx, root, sources, 10_000)
	if !reflect.DeepEqual(withReplacement, finalFiles) {
		t.Fatal("cached scans modified the corpus")
	}
	var jobs, completed int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status='Completed' AND error='' AND finished_at IS NOT NULL) FROM scan_jobs`).Scan(&jobs, &completed); err != nil || jobs != 4 || completed != 4 {
		t.Fatalf("capacity jobs did not close exactly once: jobs=%d completed=%d error=%v", jobs, completed, err)
	}
	catalogCapacityCloseStore(t, store)
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("closed capacity Store retained a PostgreSQL connection")
	}
	t.Logf("capacity profile complete media_bytes=%d corpus_allocated_bytes=%d real_probe_calls=%d completed_jobs=4 store_closed=true",
		finalMediaBytes, finalAllocatedBytes, prober.calls.Load())
}

func catalogCapacityAssertViews(t *testing.T, ctx context.Context, store *Store, root string, records []catalogCapacityRecord, libraries map[string]Library, users []string) {
	t.Helper()
	for subject, user := range users {
		allowed := map[string]bool{libraries["A"].ID: subject != 1, libraries["B"].ID: subject != 0}
		var expected []string
		for _, record := range records {
			if allowed[record.libraryID] && !record.folder {
				expected = append(expected, record.id)
			}
		}
		want := 5000
		if subject == 2 {
			want = 10_000
		}
		if len(expected) != want {
			t.Fatal("independent ACL expectation has the wrong leaf cardinality")
		}
		// Store Limit=0 means the default page size, not HTTP count-only. A
		// one-row page and an empty terminal page both retain the full count.
		for _, page := range []struct{ start, limit int }{{0, 1}, {0, 37}, {want / 2, 37}, {want - 37, 37}, {want, 1}} {
			folder := false
			result := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, Recursive: true,
				IncludeItemTypes: []string{"Movie", "Episode", "Audio"}, IsFolder: &folder,
				SortBy: "Name", SortOrder: "ASC", StartIndex: page.start, Limit: page.limit})
			end := min(page.start+page.limit, want)
			if result.TotalRecordCount != want || len(result.Items) != end-page.start {
				t.Fatalf("authorized page count/length is wrong: total=%d items=%d", result.TotalRecordCount, len(result.Items))
			}
			for index, item := range result.Items {
				if item.ID != expected[page.start+index] || !allowed[item.LibraryID] || item.IsFolder {
					t.Fatal("capacity pagination changed expected order or crossed an ACL boundary")
				}
			}
		}
	}
	var artistID int64
	for index, label := range []string{"A", "B"} {
		user, hiddenUser, library := users[index], users[1-index], libraries[label]
		episodeRecord := catalogCapacityFindPath(t, records, filepath.Join(root, label, "Television", label+" Show01", "Season01", label+" Show01.S01E01.mp4"))
		episode, err := store.GetItem(ctx, user, episodeRecord.id)
		if err != nil || episode.Type != "Episode" || episode.Series == nil || episode.Season == nil ||
			episode.IndexNumber != 1 || episode.ParentIndexNumber != 1 || episode.Media == nil || episode.Media.ProbeVersion != media.CurrentProbeVersion {
			t.Fatalf("capacity episode lost its real probe or television identity: %v", err)
		}
		series, err := store.GetItem(ctx, user, episode.Series.ID)
		if err != nil || series.Type != "Series" || !series.IsFolder || series.ParentID != library.ID || series.Path != "" || series.Name != label+" Show01" {
			t.Fatalf("mixed-library synthetic series has the wrong identity: %v", err)
		}
		season, err := store.GetItem(ctx, user, episode.Season.ID)
		if err != nil || season.Type != "Season" || !season.IsFolder || season.ParentID != series.ID || season.IndexNumber != 1 || episode.ParentID != season.ID {
			t.Fatalf("mixed-library synthetic season has the wrong identity: %v", err)
		}
		for _, hierarchy := range []struct {
			parent, kind string
			count        int
			recursive    bool
		}{
			{series.ID, "Season", 5, false}, {season.ID, "Episode", 20, false}, {series.ID, "Episode", 100, true},
		} {
			result := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: hierarchy.parent,
				IncludeItemTypes: []string{hierarchy.kind}, Recursive: hierarchy.recursive, Limit: 1})
			if result.TotalRecordCount != hierarchy.count || len(result.Items) != 1 || result.Items[0].LibraryID != library.ID {
				t.Fatal("representative television parent counted the wrong children")
			}
		}
		physical := catalogCapacityFindPath(t, records, filepath.Join(root, label, "Television", label+" Show01", "Season01"))
		if physical.kind != "Folder" || !physical.folder || physical.id == season.ID {
			t.Fatal("physical directories were confused with synthetic TV parents")
		}
		albumRecord := catalogCapacityFindPath(t, records, filepath.Join(root, label, "Music", label+" Album001"))
		album, err := store.GetItem(ctx, user, albumRecord.id)
		if err != nil {
			t.Fatal(err)
		}
		assertMusicAlbumChildCount(t, album, 10)
		if album.Name != label+" Album001" || album.UserData == nil || !album.UserData.IsFavorite || album.UserData.Played ||
			album.UserData.UnplayedItemCount == nil || *album.UserData.UnplayedItemCount != 9 {
			t.Fatal("album NFO or derived user state disagrees with its ten physical tracks")
		}
		tracks := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: album.ID, IncludeItemTypes: []string{"Audio"}, Limit: 20})
		if tracks.TotalRecordCount != 10 || len(tracks.Items) != 10 {
			t.Fatal("album direct-child query lost tracks")
		}
		for _, track := range tracks.Items {
			if track.LibraryID != library.ID || track.ParentID != album.ID || track.Album == nil || track.Album.ID != album.ID || track.Album.Name != album.Name ||
				track.Media == nil || track.Media.ProbeVersion != media.CurrentProbeVersion || track.Media.EmbeddedMusic == nil ||
				track.Media.EmbeddedMusic.Version != media.CurrentMusicMetadataVersion || track.Media.EmbeddedMusic.Title != "" || track.Media.EmbeddedMusic.Album != "" ||
				track.Media.EmbeddedMusic.Artist != catalogCapacityArtist || track.Media.EmbeddedMusic.AlbumArtist != catalogCapacityArtist ||
				len(track.Entities.Artists) != 1 || len(track.Entities.AlbumArtists) != 1 || track.Entities.Artists[0].ID != track.Entities.AlbumArtists[0].ID {
				t.Fatal("copied FLAC lost its accepted embedded credits or physical album reference")
			}
			if artistID == 0 {
				artistID = track.Entities.Artists[0].ID
			} else if track.Entities.Artists[0].ID != artistID {
				t.Fatal("the libraries did not share the indexed artist")
			}
		}
		for _, forbidden := range []string{episode.ID, series.ID, season.ID, album.ID, tracks.Items[0].ID} {
			if _, err := store.GetItem(ctx, hiddenUser, forbidden); !errors.Is(err, ErrNotFound) {
				t.Fatalf("hidden detail was not denied: %v", err)
			}
		}
		if _, err := store.QueryItems(ctx, Query{UserID: hiddenUser, ParentID: album.ID}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden album query was not denied: %v", err)
		}
		// Synthetic history is a persistence fixture, not playback evidence.
		movieRecord := catalogCapacityFindPath(t, records, filepath.Join(root, label, "Movies", label+" Movie0002.mp4"))
		movie, err := store.GetItem(ctx, user, movieRecord.id)
		lastPlayed := time.Date(2024, 2, 3, 4, 5, 6, 7000, time.UTC)
		if err != nil || movie.UserData == nil || movie.UserData.Played || !movie.UserData.IsFavorite || movie.UserData.PlayCount != 3 ||
			movie.UserData.PlaybackPositionTicks != 12345 || movie.UserData.LastPlayedDate == nil || !movie.UserData.LastPlayedDate.Equal(lastPlayed) {
			t.Fatalf("existing movie UserData projection changed: %v", err)
		}
	}
	for index, user := range users {
		wantSources, wantTracks := 1100, 1000
		if index == 2 {
			wantSources, wantTracks = 2200, 2000
		}
		entity, err := store.GetEntityByID(ctx, user, artistID)
		if err != nil || entity.ID != artistID || entity.Type != "MusicArtist" || entity.Name != catalogCapacityArtist || entity.Count != wantSources {
			t.Fatalf("shared artist counted hidden or duplicate sources: count=%d error=%v", entity.Count, err)
		}
		for _, query := range []Query{{ArtistIds: []int64{artistID}}, {AlbumArtistIds: []int64{artistID}}, {ArtistIds: []int64{artistID}, AlbumArtistIds: []int64{artistID}}} {
			query.UserID, query.Recursive, query.Limit = user, true, 1
			query.IncludeItemTypes = []string{"Audio"}
			result := libraryIntegrationQuery(t, ctx, store, query)
			if result.TotalRecordCount != wantTracks || len(result.Items) != 1 {
				t.Fatal("artist filters changed authorized audio counts")
			}
		}
	}
}

func catalogCapacityWriteExclusive(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || written != len(data) {
		t.Fatalf("write independent fixture file: written=%d expected=%d write=%v close=%v", written, len(data), writeErr, closeErr)
	}
}

func catalogCapacityTree(t *testing.T, ctx context.Context, root string, sources map[string]catalogCapacitySource, wantMedia int) (map[string]catalogCapacityFileStamp, int64, int64) {
	t.Helper()
	files := make(map[string]catalogCapacityFileStamp, len(sources))
	identities := make(map[[2]uint64]bool, len(sources))
	var mediaBytes, allocatedBytes int64
	mediaFiles := 0
	var device uint64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (!info.IsDir() && !info.Mode().IsRegular()) || info.Mode()&os.ModeSymlink != 0 || stat.Blocks < 0 {
			return fmt.Errorf("capacity corpus contains a special or unaccountable file")
		}
		if path == root {
			device = uint64(stat.Dev)
		} else if uint64(stat.Dev) != device {
			return fmt.Errorf("capacity corpus crosses a filesystem device")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || len(path) > 256 || len(relative) > 128 || len(filepath.Base(path)) > 64 {
			return fmt.Errorf("capacity corpus exceeds its bounded pathname profile")
		}
		allocatedBytes += stat.Blocks * 512
		if allocatedBytes > catalogCapacityAllocatedBytes {
			return fmt.Errorf("capacity corpus exceeds its allocated-byte budget")
		}
		if info.IsDir() {
			return nil
		}
		expected, exists := sources[path]
		key := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
		if !exists || identities[key] || stat.Nlink != 1 || stat.Ino == 0 || info.Mode().Perm() != 0o600 || info.Size() != expected.bytes {
			return fmt.Errorf("capacity corpus contains an unexpected, shared, or changed file")
		}
		identities[key] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if hash != expected.hash {
			return fmt.Errorf("capacity corpus bytes changed")
		}
		files[path] = catalogCapacityFileStamp{device: key[0], inode: key[1], links: uint64(stat.Nlink), bytes: info.Size(),
			modifiedNS: info.ModTime().UnixNano(), changedNS: media.FileChangeTime(info), mode: info.Mode(), hash: hash}
		if expected.kind != "" {
			mediaFiles++
			mediaBytes += info.Size()
		}
		if mediaBytes > catalogCapacityMediaBytes {
			return fmt.Errorf("capacity corpus exceeds its logical media-byte budget")
		}
		return nil
	})
	if err != nil || len(files) != len(sources) || mediaFiles != wantMedia {
		t.Fatalf("verify independent corpus: files=%d/%d media=%d/%d error=%v", len(files), len(sources), mediaFiles, wantMedia, err)
	}
	return files, mediaBytes, allocatedBytes
}

func catalogCapacityReadCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []catalogCapacityRecord {
	t.Helper()
	// Folder updated_at/xmin writes are legitimate on scans. UserData has its
	// separate whole-row comparison. SQL order matches the public Name sort.
	rows, err := pool.Query(ctx, `SELECT id, library_id, COALESCE(root_id,''), COALESCE(parent_id,''),
		type, path, relative_path, name, sort_name, is_folder, index_number, parent_index_number
		FROM items ORDER BY lower(name), id LIMIT $1`, catalogCapacityItems+1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []catalogCapacityRecord
	for rows.Next() {
		var item catalogCapacityRecord
		if err := rows.Scan(&item.id, &item.libraryID, &item.rootID, &item.parentID, &item.kind, &item.path,
			&item.relative, &item.name, &item.sortName, &item.folder, &item.index, &item.parentIndex); err != nil {
			t.Fatal(err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil || len(result) != catalogCapacityItems {
		t.Fatalf("capacity catalog has extra or missing rows: rows=%d error=%v", len(result), err)
	}
	return result
}

func catalogCapacityFindPath(t *testing.T, records []catalogCapacityRecord, path string) catalogCapacityRecord {
	t.Helper()
	for _, record := range records {
		if record.path == path {
			return record
		}
	}
	t.Fatalf("expected capacity catalog path is absent: %s", path)
	return catalogCapacityRecord{}
}

func catalogCapacityAssertCorpus(t *testing.T, records []catalogCapacityRecord, sources map[string]catalogCapacitySource, libraries map[string]Library, rootIDs map[string]string) {
	t.Helper()
	counts := make(map[string]map[string]int)
	rootPaths := make(map[string]string, 2)
	mediaPaths := make(map[string]bool, 10_000)
	for _, label := range []string{"A", "B"} {
		library := libraries[label]
		if len(library.Paths) != 1 || len(rootIDs[library.ID]) != 32 {
			t.Fatal("capacity library must keep its one verified physical root")
		}
		counts[library.ID] = make(map[string]int)
		rootPaths[library.ID] = library.Paths[0]
	}
	for _, item := range records {
		kinds, exists := counts[item.libraryID]
		if !exists || item.id == "" {
			t.Fatal("capacity item is outside its two owned libraries")
		}
		kinds[item.kind]++
		if item.folder {
			continue
		}
		source, exists := sources[item.path]
		if !exists || source.kind == "" || source.kind != item.kind || mediaPaths[item.path] {
			t.Fatal("leaf catalog is not a one-to-one media projection")
		}
		if item.rootID != rootIDs[item.libraryID] {
			t.Fatal("a media item references another library's registered root")
		}
		relative, err := filepath.Rel(rootPaths[item.libraryID], item.path)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.ToSlash(relative) != item.relative {
			t.Fatal("a media item was assigned to a foreign library root or relative path")
		}
		if len(item.id) != 32 || len(item.rootID) != 32 || len(item.path) > 256 || len(item.relative) > 128 {
			t.Fatal("media identity exceeds its proof profile")
		}
		mediaPaths[item.path] = true
	}
	want := map[string]int{"CollectionFolder": 1, "Folder": 123, "MusicAlbum": 100, "Series": 20, "Season": 100, "Movie": 2000, "Episode": 2000, "Audio": 1000}
	for _, kinds := range counts {
		if !reflect.DeepEqual(kinds, want) {
			t.Fatalf("wrong physical/synthetic hierarchy: got=%v want=%v", kinds, want)
		}
	}
	if len(mediaPaths) != 10_000 {
		t.Fatal("leaf projection does not contain ten thousand files")
	}
}

func catalogCapacityUserData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var count int
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(jsonb_agg(to_jsonb(d) ||
		jsonb_build_object('rowVersion',d.xmin::text) ORDER BY d.user_id,d.item_id),'[]'::jsonb)::text
		FROM user_item_data d`).Scan(&count, &snapshot); err != nil || count != 8 {
		t.Fatalf("read bounded existing UserData: count=%d error=%v", count, err)
	}
	return snapshot
}

func catalogCapacityCloseStore(t *testing.T, store *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("capacity Store did not close within its cleanup deadline: %v", err)
	}
}
