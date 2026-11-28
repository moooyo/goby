//go:build linux

package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestHTTPFastIngestWarmNFORetainsRestrictedCatalogAndOriginals(t *testing.T) {
	f := newFastIngestWarmNFOHTTPFixture(t)
	setHTTPUserPolicy(t, f.serverFixture, f.accounts.other.userID, `{"MaxParentalRating":1}`)
	const movieBytes, bareBytes, episodeBytes, flatBytes = "preferred movie source", "unrated movie source", "episode source", "flat episode source"
	moviePath := f.write(t, "movies/Preferred/Film.mp4", movieBytes)
	f.write(t, "movies/Preferred/Film.nfo", `<movie><title>Preferred Film</title><mpaa>PG</mpaa></movie>`)
	f.write(t, "movies/Preferred/movie.nfo", `<movie><title>Ignored Fallback</title><mpaa>R</mpaa></movie>`)
	barePath := f.write(t, "movies/Bare/Bare.mp4", bareBytes)
	poster := filepath.Join(filepath.Dir(moviePath), "Film-poster.jpg")
	writeAPIJPEG(t, poster, 8, 12, 1)

	episodePath := f.write(t, "tvshows/Show/Season 1/Show.S01E02.mp4", episodeBytes)
	seasonPath := filepath.Dir(episodePath)
	seriesPath := filepath.Dir(seasonPath)
	f.write(t, "tvshows/Show/tvshow.nfo", `<tvshow><title>Accepted Series</title><mpaa>TV-PG</mpaa></tvshow>`)
	f.write(t, "tvshows/Show/Season 1/season.nfo", `<season><title>Accepted Season</title><season>1</season></season>`)
	f.write(t, "tvshows/Show/Season 1/Show.S01E02.nfo", `<episodedetails><title>Accepted Episode</title><season>1</season><episode>2</episode></episodedetails>`)
	flatPath := f.write(t, "tvshows/FlatShow.S01E01.mp4", flatBytes)
	f.write(t, "tvshows/FlatShow.S01E01.nfo", `<episodedetails><title>Accepted Flat Episode</title><season>1</season><episode>1</episode><mpaa>PG</mpaa></episodedetails>`)

	movies, tv := f.createAndScan(t, "movies"), f.createAndScan(t, "tvshows")
	f.stopImporter(t)
	// Neither an edited fallback nor a changed poster changes the winning NFO.
	f.write(t, "movies/Preferred/movie.nfo", `<movie><title>Changed Ignored Fallback</title><mpaa>R</mpaa></movie>`)
	writeAPIJPEG(t, poster, 8, 12, 2)

	type shelfItem struct{ path, kind, title, data string }
	for _, shelf := range []struct {
		name, id, types string
		items           []shelfItem
	}{
		{"movies", movies, "Movie", []shelfItem{
			{moviePath, "Movie", "Preferred Film", movieBytes},
			{barePath, "Movie", "Bare", bareBytes},
		}},
		{"tv", tv, "Series,Season,Episode", []shelfItem{
			{seriesPath, "Series", "Accepted Series", ""},
			{seasonPath, "Season", "Accepted Season", ""},
			{episodePath, "Episode", "Accepted Episode", episodeBytes},
		}},
	} {
		t.Run(shelf.name, func(t *testing.T) {
			viewer := f.accounts.viewer
			const position = media.TicksPerSecond
			var ids, lowerCeilingIDs []string
			before := make([]library.Item, len(shelf.items))
			etags := make([]string, len(shelf.items))
			var flat library.Item
			var flatETag string
			if shelf.name == "tv" {
				flat = fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, shelf.id, flatPath)
				if flat.Name != "Accepted Flat Episode" || flat.Season == nil || flat.Series == nil {
					t.Fatalf("expected a flat episode with virtual parents: %+v", flat)
				}
				ids = append(ids, flat.Series.ID, flat.Season.ID, flat.ID)
				// The virtual parents contain no rating of their own.
				lowerCeilingIDs = append(lowerCeilingIDs, flat.Series.ID, flat.Season.ID)
				flatETag = f.originalRange(t, viewer, flat.ID, flatBytes, "", http.StatusPartialContent)
			}
			for i, spec := range shelf.items {
				item := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, shelf.id, spec.path)
				if item.Type != spec.kind || item.Name != spec.title {
					t.Fatalf("initial import for %q = %s %q, want %s %q", spec.path, item.Type, item.Name, spec.kind, spec.title)
				}
				before[i] = item
				ids = append(ids, item.ID)
				if spec.path == barePath {
					lowerCeilingIDs = append(lowerCeilingIDs, item.ID)
				}
				if spec.data != "" {
					etags[i] = f.originalRange(t, viewer, item.ID, spec.data, "", http.StatusPartialContent)
					response := f.request(t, http.MethodPost, warmNFOItemURL(viewer, item.ID)+"/UserData",
						map[string]any{"PlaybackPositionTicks": position, "PlayCount": 1, "IsFavorite": true}, viewer.headers)
					expectStatus(t, response, http.StatusOK)
					assertPlaybackHTTPData(t, jsonObject(t, response), position, 1, false, true)
				}
			}
			listURL := warmNFOListURL(shelf.id, shelf.types)
			f.assertList(t, viewer, listURL, ids...)
			f.assertList(t, f.accounts.other, listURL, lowerCeilingIDs...)
			job := f.warmScan(t, shelf.id)
			if flat.ID != "" {
				f.assertNFOJob(t, flat, "")
				f.assertDetail(t, viewer, flat, http.StatusOK)
				f.originalRange(t, viewer, flat.ID, flatBytes, flatETag, http.StatusPartialContent)
				f.assertDetail(t, f.accounts.other, flat, http.StatusNotFound)
			}
			for i, item := range before {
				t.Run(item.Name, func(t *testing.T) {
					imagesJob := f.assertNFOJob(t, item, "")
					if item.Path == moviePath && imagesJob != job {
						t.Errorf("warm scan %q did not leave independent poster work: import_images job = %q", job, imagesJob)
					}
					detail := f.assertDetail(t, viewer, item, http.StatusOK)
					if item.Path != barePath {
						f.assertDetail(t, f.accounts.other, item, http.StatusNotFound)
					}
					if data := shelf.items[i].data; data != "" {
						f.originalRange(t, viewer, item.ID, data, etags[i], http.StatusPartialContent)
						if item.Path != barePath {
							f.originalRange(t, f.accounts.other, item.ID, data, "", http.StatusNotFound)
						}
						if detail != nil {
							assertPlaybackHTTPData(t, objectValue(t, detail, "UserData"), position, 1, false, true)
						}
					}
				})
			}
			t.Run("allowed-list", func(t *testing.T) { f.assertList(t, viewer, listURL, ids...) })
			t.Run("lower-ceiling-list", func(t *testing.T) { f.assertList(t, f.accounts.other, listURL, lowerCeilingIDs...) })
		})
	}
}

func TestHTTPFastIngestWarmNFOLeavesChangedOrRejectedCandidatesPending(t *testing.T) {
	f := newFastIngestWarmNFOHTTPFixture(t)
	setHTTPUserPolicy(t, f.serverFixture, f.accounts.other.userID, `{"BlockedTags":["Blocked Fixture"]}`)
	const accepted = `<movie><title>Accepted Fallback</title><mpaa>PG</mpaa></movie>`
	const data = "unchanged movie source"
	cases := []string{"changed-content", "changed-tag", "removed", "malformed", "changed-selection", "unprovable-preferred"}
	for _, name := range cases {
		f.write(t, "movies/"+name+"/Film.mp4", data)
		f.write(t, "movies/"+name+"/movie.nfo", accepted)
	}
	movies := f.createAndScan(t, "movies")
	f.stopImporter(t)

	var before []library.Item
	var ids []string
	for _, name := range cases {
		item := fastIngestItem(t, f.serverFixture, f.accounts.admin.userID, movies, filepath.Join(f.root, "movies", name, "Film.mp4"))
		if item.Name != "Accepted Fallback" {
			t.Fatalf("initial import for %q did not use the fallback: %q", name, item.Name)
		}
		before = append(before, item)
		ids = append(ids, item.ID)
		f.assertDetail(t, f.accounts.viewer, item, http.StatusOK)
	}
	listURL := warmNFOListURL(movies, "Movie")
	f.assertList(t, f.accounts.viewer, listURL, ids...)
	f.assertList(t, f.accounts.other, listURL, ids...)
	f.write(t, "movies/changed-content/movie.nfo", `<movie><title>Changed Fallback</title><mpaa>R</mpaa></movie>`)
	f.write(t, "movies/changed-tag/movie.nfo", `<movie><title>Accepted Fallback</title><mpaa>PG</mpaa><tag>Blocked Fixture</tag></movie>`)
	if err := os.Remove(filepath.Join(f.root, "movies", "removed", "movie.nfo")); err != nil {
		t.Fatal(err)
	}
	f.write(t, "movies/malformed/movie.nfo", `<movie>unfinished`)
	// Byte equality alone cannot verify a different winning candidate.
	f.write(t, "movies/changed-selection/Film.nfo", accepted)
	// A present but unsafe preferred candidate must not fall through to the
	// previously accepted fallback or be treated as absence.
	if err := os.Symlink("movie.nfo", filepath.Join(f.root, "movies", "unprovable-preferred", "Film.nfo")); err != nil {
		t.Fatal(err)
	}
	job := f.warmScan(t, movies)
	f.assertList(t, f.accounts.viewer, listURL)
	f.assertList(t, f.accounts.other, listURL)
	f.assertList(t, f.accounts.admin, listURL, ids...)
	for i, item := range before {
		t.Run(cases[i], func(t *testing.T) {
			f.assertNFOJob(t, item, job)
			f.assertDetail(t, f.accounts.admin, item, http.StatusOK)
			f.originalRange(t, f.accounts.admin, item.ID, data, "", http.StatusPartialContent)
			f.assertDetail(t, f.accounts.viewer, item, http.StatusNotFound)
			f.originalRange(t, f.accounts.viewer, item.ID, data, "", http.StatusNotFound)
			if cases[i] == "changed-tag" {
				f.assertDetail(t, f.accounts.other, item, http.StatusNotFound)
				f.originalRange(t, f.accounts.other, item.ID, data, "", http.StatusNotFound)
			}
		})
	}
}

type fastIngestWarmNFOHTTPFixture struct {
	*serverFixture
	accounts clientSessionHTTPAccounts
	root     string
}

func newFastIngestWarmNFOHTTPFixture(t *testing.T) *fastIngestWarmNFOHTTPFixture {
	t.Helper()
	f, accounts := newClientSessionHTTPAccounts(t)
	root := t.TempDir()
	probe, _, _, _ := fastIngestFixtureTools(t, t.TempDir())
	cfg := f.cfg
	cfg.MediaRoots, cfg.FFprobePath, cfg.FastLibraryIngest = []string{root}, probe, true
	fastIngestReopen(t, f, cfg)
	setHTTPUserPolicy(t, f, accounts.viewer.userID, `{"MaxParentalRating":5}`)
	return &fastIngestWarmNFOHTTPFixture{f, accounts, root}
}

func (f *fastIngestWarmNFOHTTPFixture) write(t *testing.T, relative, data string) string {
	t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(relative))
	writeFastIngestFile(t, path, []byte(data), 0o600)
	return path
}

func (f *fastIngestWarmNFOHTTPFixture) createAndScan(t *testing.T, kind string) string {
	t.Helper()
	collection, err := f.app.library.CreateLibrary(f.ctx, "Warm NFO "+kind, kind, []string{filepath.Join(f.root, kind)})
	if err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: f.serverFixture}).rescan(t, collection.ID)
	return collection.ID
}

func (f *fastIngestWarmNFOHTTPFixture) stopImporter(t *testing.T) {
	t.Helper()
	waitFastIngestDrain(t, f.serverFixture)
	// Close joins the coordinator without stopping the scanner or HTTP server.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func (f *fastIngestWarmNFOHTTPFixture) warmScan(t *testing.T, libraryID string) string {
	t.Helper()
	cookie := f.accounts.cookie
	response := f.request(t, http.MethodPost, "/admin/v1/libraries/"+libraryID+"/scan", nil,
		http.Header{"X-CSRF-Token": {csrfToken(cookie.Value)}}, cookie)
	expectStatus(t, response, http.StatusAccepted)
	id := stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f.serverFixture}, "ordinary warm scan", func() bool {
		job, err := f.app.library.GetJob(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		switch job.Status {
		case "Completed":
			// A rejected NFO can legitimately leave a scan warning.
			if job.ForceProbe || job.Scanned == 0 {
				t.Fatalf("warm scan did not visit its sources in ordinary mode: %+v", job)
			}
			return true
		case "Queued", "Running":
			return false
		default:
			t.Fatalf("warm scan did not complete: %+v", job)
			return false
		}
	})
	return id
}

func (f *fastIngestWarmNFOHTTPFixture) assertNFOJob(t *testing.T, item library.Item, want string) (imagesJob string) {
	t.Helper()
	var nfoJob string
	if err := f.pool.QueryRow(f.ctx, `SELECT
		COALESCE((SELECT scan_job_id FROM local_metadata_ingest WHERE item_id=$1 AND import_nfo),''),
		COALESCE((SELECT scan_job_id FROM local_metadata_ingest WHERE item_id=$1 AND import_images),'')`, item.ID).
		Scan(&nfoJob, &imagesJob); err != nil {
		t.Fatal(err)
	}
	if nfoJob != want {
		t.Errorf("warm scan %s %q: import_nfo job = %q, want %q (empty means not pending)", item.Type, item.Name, nfoJob, want)
	}
	return imagesJob
}

func warmNFOItemURL(login clientSessionHTTPLogin, id string) string {
	return "/emby/Users/" + login.userID + "/Items/" + id
}

func warmNFOListURL(libraryID, types string) string {
	return "/emby/Items?ParentId=" + libraryID + "&Recursive=true&EnableImages=false&IncludeItemTypes=" + types
}

func (f *fastIngestWarmNFOHTTPFixture) assertList(t *testing.T, login clientSessionHTTPLogin, target string, want ...string) {
	t.Helper()
	assertTVFilterItems(t, f.request(t, http.MethodGet, target, nil, login.headers), want, len(want), false)
}

func (f *fastIngestWarmNFOHTTPFixture) assertDetail(t *testing.T, login clientSessionHTTPLogin, item library.Item, status int) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, warmNFOItemURL(login, item.ID), nil, login.headers)
	if response.Code != status {
		t.Errorf("viewer %s %s %q detail: status = %d, want %d; body = %s", login.userID, item.Type, item.Name, response.Code, status, response.Body)
		return nil
	}
	if status != http.StatusOK {
		return nil
	}
	detail := jsonObject(t, response)
	if detail["Id"] != item.ID || detail["Name"] != item.Name || detail["Type"] != item.Type || detail["ParentId"] != item.ParentID {
		t.Errorf("%s %q detail did not retain the accepted item and parent: ID=%v name=%v type=%v parent=%v",
			item.Type, item.Name, detail["Id"], detail["Name"], detail["Type"], detail["ParentId"])
	}
	return detail
}

func (f *fastIngestWarmNFOHTTPFixture) originalRange(t *testing.T, login clientSessionHTTPLogin, id, data, wantETag string, status int) string {
	t.Helper()
	headers := login.headers.Clone()
	headers.Set("Range", "bytes=2-7")
	response := f.request(t, http.MethodGet, "/emby/Videos/"+id+"/original.mp4", nil, headers)
	etag, contentRange := response.Header().Get("ETag"), response.Header().Get("Content-Range")
	if response.Code != status {
		t.Errorf("viewer %s original range for %s: status = %d, want %d; body = %q", login.userID, id, response.Code, status, response.Body.String())
		return ""
	}
	if status == http.StatusPartialContent {
		if response.Body.String() != data[2:8] || contentRange != fmt.Sprintf("bytes 2-7/%d", len(data)) || etag == "" || wantETag != "" && etag != wantETag {
			t.Errorf("original range for %s changed: body=%q Content-Range=%q ETag=%q (prior ETag=%q)", id, response.Body.String(), contentRange, etag, wantETag)
		}
	} else if etag != "" || contentRange != "" || response.Body.String() == data[2:8] {
		t.Errorf("denied original range for %s exposed media bytes or validators", id)
	}
	return etag
}
