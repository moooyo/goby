//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/color"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	adminassets "github.com/moooyo/goby/web/admin"
)

var mediaAnalysisPhase1Phases = []string{"authentication", "user-saved", "user-query", "specials-before", "metadata-saved", "specials-after",
	"sorting-saved", "sorting-consumed", "imports-disabled", "imports-retained", "imports-enabled", "imports-consumed",
	"embedded-disabled", "embedded-absent", "embedded-enabled", "embedded-present", "fields", "restart", "persisted", "cleanup"}

type mediaAnalysisPhase1User struct {
	ID         string `json:"Id"`
	Name       string
	IsHidden   bool
	IsDisabled bool
}

// Credentials are exported only to a private, short-lived browser context.
// Tokens, authentication IDs and database connection strings are never exported.
type mediaAnalysisPhase1Context struct {
	Marker, RunId, BaseURL                             string
	AdminId, AdminName, AdminPassword                  string
	ViewerId, ViewerName, ViewerPassword               string
	EditedUserId, EditedUserName                       string
	UserDirectory                                      []mediaAnalysisPhase1User
	TelevisionLibraryId, TelevisionLibraryName         string
	MovieLibraryId, MovieLibraryName                   string
	MusicLibraryId, MusicLibraryName                   string
	ExistingEmbeddedAudioId, ExistingEmbeddedAudioName string
	SidecarAudioId, SidecarAudioName                   string
	NewEmbeddedAudioName                               string
	SeriesId, SeriesName                               string
	StandaloneSpecialId, StandaloneSpecialName         string
	PlacedSpecialId, PlacedSpecialName                 string
	RegularEpisodeId, RegularEpisodeName               string
	MovieAId, MovieAName, MovieBId, MovieBName         string
	ImportedMovieName, SearchTerm                      string
	ArtifactsDir, ResultPath                           string
}

type mediaAnalysisPhase1UserQuery struct {
	Key              string
	IDs              []string `json:"Ids"`
	TotalRecordCount int
}

type mediaAnalysisPhase1Request struct {
	RunId, Phase, SeriesRevision, PlacementRevision string
	SettingsRevision, LibraryRevision, ScanJobId    string
	MusicLibraryRevision, NewAudioId                string
	Images                                          []mediaAnalysisPhase1Image
	QueryIDs                                        []string `json:"QueryIds"`
	UserQueries                                     []mediaAnalysisPhase1UserQuery
	Fields                                          struct {
		ID                         string `json:"Id"`
		ListMediaStreams           int
		ExcludedDetailMediaStreams bool
		ExcludedNestedMediaStreams bool
		SearchHintID               string `json:"SearchHintId"`
		SearchDetailID             string `json:"SearchDetailId"`
		Language                   string
	}
}

type mediaAnalysisPhase1Image struct {
	ItemID                string `json:"ItemId"`
	SHA256                string
	Status, Width, Height int
	Bytes                 int64
}

type mediaAnalysisPhase1Runtime struct {
	*phase3BrowserRuntime
	active atomic.Int64
}

func (runtime *mediaAnalysisPhase1Runtime) listen() error {
	listener, err := net.Listen("tcp4", runtime.addr)
	if err != nil {
		return err
	}
	runtime.addr = listener.Addr().String()
	origin := "http://" + runtime.addr
	runtime.f.cfg.PublicURL, runtime.f.cfg.CookieSecure = origin, false
	runtime.f.app.cfg.PublicURL, runtime.f.app.cfg.CookieSecure = origin, false
	application := runtime.f.app.Handler()
	runtime.f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtime.active.Add(1)
		defer runtime.active.Add(-1)
		if r.URL.Path != "/__media-analysis-phase1-consumer" {
			application.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Use GET for the owned client.", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; img-src 'self' blob:")
		_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Owned media analysis client</title></head><body><main><h1>Owned media analysis client</h1><div id="choices"></div><pre id="detail"></pre><p id="status"></p></main></body></html>`)
	})
	actual := httptest.NewUnstartedServer(runtime.f.handler)
	actual.Listener.Close()
	actual.Listener = listener
	actual.Start()
	runtime.server = actual
	return nil
}

func (runtime *mediaAnalysisPhase1Runtime) restart(ctx context.Context) error {
	if err := runtime.close(ctx); err != nil {
		return err
	}
	if runtime.active.Load() != 0 {
		return errors.New("media analysis phase 1 restart retained active HTTP requests")
	}
	app, err := New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log,
		"media-analysis-phase1-browser-integration", WithDashboardAssets(runtime.assets))
	if err != nil {
		return err
	}
	runtime.f.app = app
	return runtime.listen()
}

func mediaAnalysisPhase1ReadExecution(t *testing.T) (selectedPhase3Execution, []selectedPhase2FileFact) {
	t.Helper()
	path := refreshBrowserPath(t, "GOBY_MEDIA_ANALYSIS_PHASE1_EXECUTION_CONFIG", false)
	var raw json.RawMessage
	if err := featureWavePrivateJSON(path, 16<<10, &raw); err != nil {
		selectedPhase2Fatal(t, "read media analysis execution inventory", err)
	}
	var execution selectedPhase3Execution
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&execution); err != nil || execution.Marker != "goby-media-analysis-phase1-execution-v1" {
		t.Fatal("media analysis phase 1 execution inventory is invalid")
	}
	var facts []selectedPhase2FileFact
	for _, input := range []struct{ path, digest string }{{execution.FFmpegPath, execution.FFmpegSHA256}, {execution.FFprobePath, execution.FFprobeSHA256}} {
		fact, err := selectedPhase2Fact(input.path, 256<<20)
		if err != nil || len(input.digest) != 64 || fact.SHA256 != input.digest {
			t.Fatal("media analysis phase 1 media tool identity differs")
		}
		facts = append(facts, fact)
	}
	return execution, facts
}

func mediaAnalysisPhase1Author(t *testing.T, execution selectedPhase3Execution, root string) []string {
	t.Helper()
	for _, path := range []string{"authoring", "movies", "tv/Media Analysis Show/Season 00", "tv/Media Analysis Show/Season 01"} {
		if os.MkdirAll(filepath.Join(root, filepath.FromSlash(path)), 0o700) != nil {
			t.Fatal("create media analysis authored directories")
		}
	}
	source := filepath.Join(root, "authoring", "source.mp4")
	hlsHTTPMediaCommand(t, execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=navy:size=160x90:rate=24:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=8",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "24", "-bf", "0",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "8", "-movflags", "+faststart", source)
	if os.Chmod(source, 0o600) != nil {
		t.Fatal("protect media analysis authored source")
	}
	paths := []string{}
	for _, relative := range []string{"movies/The Amber.mp4", "movies/Bravo.mp4",
		"tv/Media Analysis Show/Season 00/Media.Analysis.Show.S00E01.mp4", "tv/Media Analysis Show/Season 00/Media.Analysis.Show.S00E02.mp4",
		"tv/Media Analysis Show/Season 01/Media.Analysis.Show.S01E01.mp4"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := selectedPhase3Copy(source, path); err != nil {
			selectedPhase2Fatal(t, "copy actual media analysis source", err)
		}
		paths = append(paths, path)
	}
	nfos := map[string]string{
		strings.TrimSuffix(paths[0], ".mp4") + ".nfo":                  `<movie><title>The Amber</title><plot>Initial local import</plot></movie>`,
		strings.TrimSuffix(paths[1], ".mp4") + ".nfo":                  `<movie><title>Bravo</title></movie>`,
		filepath.Join(root, "tv", "Media Analysis Show", "tvshow.nfo"): `<tvshow><title>Media Analysis Show</title><status>Continuing</status></tvshow>`,
		strings.TrimSuffix(paths[2], ".mp4") + ".nfo":                  `<episodedetails><title>Standalone special</title><season>0</season><episode>1</episode></episodedetails>`,
		strings.TrimSuffix(paths[3], ".mp4") + ".nfo":                  `<episodedetails><title>Placed special</title><season>0</season><episode>2</episode><airsbefore_season>1</airsbefore_season><airsbefore_episode>2</airsbefore_episode></episodedetails>`,
		strings.TrimSuffix(paths[4], ".mp4") + ".nfo":                  `<episodedetails><title>Regular episode</title><season>1</season><episode>1</episode></episodedetails>`,
	}
	for path, contents := range nfos {
		if os.WriteFile(path, []byte(contents), 0o600) != nil {
			t.Fatal("write media analysis authored NFO")
		}
	}
	return paths
}

type mediaAnalysisPhase1MusicFiles struct {
	ExistingPath, SidecarPath, NewSource, NewPath string
	ExistingPixels, SidecarPixels, NewPixels      string
}

func mediaAnalysisPhase1AuthorMusic(t *testing.T, execution selectedPhase3Execution, root string) mediaAnalysisPhase1MusicFiles {
	t.Helper()
	result := mediaAnalysisPhase1MusicFiles{
		ExistingPath: filepath.Join(root, "music", "Existing Album", "Existing embedded track.mp3"),
		SidecarPath:  filepath.Join(root, "music", "Sidecar Album", "Sidecar track.mp3"),
		NewSource:    filepath.Join(root, "authoring", "Arriving embedded track.mp3"),
		NewPath:      filepath.Join(root, "music", "Arrival Album", "Arriving embedded track.mp3"),
	}
	for _, path := range []string{result.ExistingPath, result.SidecarPath} {
		if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
			t.Fatal("create owned audio artwork directories")
		}
	}
	oldCover := filepath.Join(root, "authoring", "existing-cover.png")
	newCover := filepath.Join(root, "authoring", "arriving-cover.png")
	sidecarCover := strings.TrimSuffix(result.SidecarPath, ".mp3") + "-cover.png"
	for _, cover := range []struct {
		path   string
		shade  color.NRGBA
		target *string
	}{
		{oldCover, color.NRGBA{R: 36, G: 144, B: 224, A: 255}, &result.ExistingPixels},
		{newCover, color.NRGBA{R: 224, G: 128, B: 36, A: 255}, &result.NewPixels},
		{sidecarCover, color.NRGBA{R: 48, G: 192, B: 96, A: 255}, &result.SidecarPixels},
	} {
		data := selectedPhase2PNG(t, cover.path, cover.shade)
		pixels, width, height, err := selectedPhase2RGBAHash(data)
		if err != nil || width != 64 || height != 64 {
			t.Fatal("capture authored artwork pixels")
		}
		*cover.target = pixels
	}
	for _, audio := range []struct{ path, title, album, cover string }{
		{result.ExistingPath, "Existing embedded track", "Existing Album", oldCover},
		{result.SidecarPath, "Sidecar track", "Sidecar Album", ""},
		{result.NewSource, "Arriving embedded track", "Arrival Album", newCover},
	} {
		args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
			"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=8"}
		if audio.cover != "" {
			args = append(args, "-i", audio.cover, "-map", "0:a:0", "-map", "1:v:0", "-c:v", "copy", "-disposition:v:0", "attached_pic",
				"-metadata:s:v:0", "title=Actual embedded cover", "-metadata:s:v:0", "comment=Cover (front)")
		} else {
			args = append(args, "-map", "0:a:0")
		}
		args = append(args, "-c:a", "libmp3lame", "-threads:a", "1", "-b:a", "128k", "-metadata", "title="+audio.title,
			"-metadata", "album="+audio.album, "-metadata", "artist=Media Analysis Artist", "-metadata", "album_artist=Media Analysis Artist",
			"-metadata", "track=1", "-id3v2_version", "3", "-t", "8", audio.path)
		hlsHTTPMediaCommand(t, execution.FFmpegPath, args...)
		if os.Chmod(audio.path, 0o600) != nil {
			t.Fatal("protect authored media analysis audio")
		}
	}
	return result
}

type mediaAnalysisPhase1Observer struct {
	runtime                                                              *mediaAnalysisPhase1Runtime
	fixture                                                              mediaAnalysisPhase1Context
	actor                                                                identity.Principal
	actorToken                                                           string
	files                                                                map[string]selectedPhase2FileFact
	movieNFO                                                             string
	mediaRoot                                                            string
	seriesRevision, placementRevision, settingsRevision, libraryRevision string
	musicRevision, newAudioID, existingArtworkHash, sidecarArtworkHash   string
	musicFiles                                                           mediaAnalysisPhase1MusicFiles
	acceptedImages                                                       map[string]mediaAnalysisPhase1Image
	usedScans                                                            map[string]bool
	initialScans                                                         []string
	durable                                                              json.RawMessage
	completed                                                            int
}

func mediaAnalysisPhase1Failure(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "media analysis phase 1 ") {
		return err.Error()
	}
	return selectedPhase2SafeError(err)
}

func (observer *mediaAnalysisPhase1Observer) filesVerified() ([]selectedPhase2FileFact, error) {
	facts := []selectedPhase2FileFact{}
	currentFiles, err := selectedPhase3Files(observer.mediaRoot)
	if err != nil || len(currentFiles) != len(observer.files) {
		return nil, errors.New("media analysis phase 1 authored tree gained or lost an undeclared source")
	}
	for path, expected := range observer.files {
		current, present := currentFiles[path]
		if !present || current != expected {
			return nil, errors.New("media analysis phase 1 source changed outside its declared fixture changes")
		}
		facts = append(facts, current)
	}
	slices.SortFunc(facts, func(a, b selectedPhase2FileFact) int { return strings.Compare(a.Path, b.Path) })
	return facts, nil
}

func (observer *mediaAnalysisPhase1Observer) snapshot(ctx context.Context, phase, suffix string) (map[string]any, error) {
	f := observer.runtime.f
	var database json.RawMessage
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Users',(SELECT jsonb_agg(jsonb_build_object('Id',id,'Name',name,'IsDisabled',is_disabled,'IsHidden',COALESCE((policy->>'IsHidden')::boolean,false)) ORDER BY normalized_name COLLATE "C",id) FROM users),
		'Items',(SELECT jsonb_agg(jsonb_build_object('Id',i.id,'Name',i.name,'Type',i.type,'ParentId',i.parent_id,'Season',i.parent_index_number,'SortName',i.sort_name,'LocalMetadata',i.local_metadata,'Revision',m.revision::text,'Overrides',m.overrides) ORDER BY i.id) FROM items i LEFT JOIN item_metadata_state m ON m.item_id=i.id),
		'Libraries',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
		'Sorting',(SELECT jsonb_build_object('Revision',revision::text,'SortRemoveWords',sort_remove_words) FROM managed_settings WHERE id=1),
		'EmbeddedArtwork',(SELECT COALESCE(jsonb_agg((to_jsonb(e)-'content')||jsonb_build_object('ContentSHA256',encode(sha256(e.content),'hex')) ORDER BY e.item_id),'[]') FROM item_embedded_artwork e),
		'SidecarImages',(SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.item_id,i.image_type,i.image_index),'[]') FROM item_images i),
		'SettingsSHA256',(SELECT encode(sha256(to_jsonb(s)::text::bytea),'hex') FROM managed_settings s WHERE id=1),
		'Scans',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'LibraryId',library_id,'Status',status,'Scanned',scanned,'ForceProbe',force_probe,'Error',error) ORDER BY created_at,id),'[]') FROM scan_jobs),
		'ActiveAuthentication',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'ActiveScans',(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
		'ActiveTasks',(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
		'PlaybackSessions',(SELECT count(*) FROM play_sessions),
		'EncodingJobs',(SELECT count(*) FROM encoding_jobs))`).Scan(&database)
	if err != nil {
		return nil, errors.New("media analysis phase 1 database evidence could not be captured")
	}
	files, err := observer.filesVerified()
	value := map[string]any{"Marker": "goby-media-analysis-phase1-stage-database-v1", "RunId": observer.fixture.RunId,
		"Phase": phase, "Complete": false, "Observed": false, "Database": database, "Sources": files,
		"Runtime": selectedPhase1RuntimeFacts(f), "ActiveHTTPRequests": observer.runtime.active.Load()}
	if err != nil {
		return value, err
	}
	if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-"+suffix+".json"), value); err != nil {
		return value, errors.New("media analysis phase 1 database evidence could not be retained")
	}
	return value, nil
}

func (observer *mediaAnalysisPhase1Observer) metadata(ctx context.Context, edited bool) error {
	f, fixture := observer.runtime.f, observer.fixture
	series, err := f.app.library.GetItemMetadata(ctx, observer.actor, fixture.SeriesId)
	if err != nil || series.Effective.Status == nil {
		return errors.New("media analysis phase 1 scanned series metadata is absent")
	}
	placed, err := f.app.library.GetItemMetadata(ctx, observer.actor, fixture.PlacedSpecialId)
	if err != nil || placed.Effective.AirsBeforeEpisodeNumber == nil || *placed.Effective.AirsBeforeEpisodeNumber != 2 {
		return errors.New("media analysis phase 1 special lost its accepted episode placement")
	}
	var season int
	if f.pool.QueryRow(ctx, "SELECT parent_index_number FROM items WHERE id=$1 AND type='Episode'", fixture.PlacedSpecialId).Scan(&season) != nil || season != 0 {
		return errors.New("media analysis phase 1 metadata edit changed structural special-season membership")
	}
	if !edited {
		if *series.Effective.Status != "Continuing" || series.Effective.EndDate != nil || placed.Effective.AirsBeforeSeasonNumber == nil || *placed.Effective.AirsBeforeSeasonNumber != 1 || len(series.Overrides) != 0 || len(placed.Overrides) != 0 {
			return errors.New("media analysis phase 1 initial metadata did not come from its real NFO sources")
		}
		return nil
	}
	if *series.Effective.Status != "Ended" || series.Effective.EndDate == nil || series.Effective.EndDate.UTC().Format("2006-01-02") != "2025-12-31" ||
		placed.Effective.AirsBeforeSeasonNumber == nil || *placed.Effective.AirsBeforeSeasonNumber != 0 ||
		placed.Effective.AirsAfterSeasonNumber == nil || *placed.Effective.AirsAfterSeasonNumber != 0 ||
		series.LastEditedBy != fixture.AdminId || placed.LastEditedBy != fixture.AdminId {
		return errors.New("media analysis phase 1 native metadata overrides did not persist their exact values")
	}
	var overrides bool
	if f.pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM item_metadata_state WHERE item_id=$1 AND overrides->>'Status'='Ended' AND (overrides->>'EndDate')::timestamptz='2025-12-31T00:00:00Z' AND last_edited_by=$3)
		AND EXISTS(SELECT 1 FROM item_metadata_state WHERE item_id=$2 AND overrides->>'AirsBeforeSeasonNumber'='0' AND overrides->>'AirsAfterSeasonNumber'='0' AND last_edited_by=$3)`,
		fixture.SeriesId, fixture.PlacedSpecialId, fixture.AdminId).Scan(&overrides) != nil || !overrides {
		return errors.New("media analysis phase 1 metadata database rows do not prove the native saves")
	}
	return nil
}

func (observer *mediaAnalysisPhase1Observer) completedScan(ctx context.Context, id, libraryID string, expected int) error {
	if id == "" || observer.usedScans[id] || slices.Contains(observer.initialScans, id) {
		return errors.New("media analysis phase 1 browser reused or omitted its actual scan job")
	}
	job, err := observer.runtime.f.app.library.GetJob(ctx, id)
	if err != nil || job.LibraryID != libraryID || job.Status != "Completed" || job.Error != "" || job.Scanned != expected || job.FinishedAt == nil || job.TaskChildID != "" {
		return errors.New("media analysis phase 1 browser scan did not finish its exact source population")
	}
	observer.usedScans[id] = true
	return nil
}

func (observer *mediaAnalysisPhase1Observer) userQueries(ctx context.Context, actual []mediaAnalysisPhase1UserQuery) error {
	if len(actual) != 5 {
		return errors.New("media analysis phase 1 directory cases are incomplete")
	}
	cases := map[string]struct {
		predicate, order string
		offset, limit    int
	}{
		"combined":        {`NOT is_disabled AND NOT COALESCE((policy->>'IsHidden')::boolean,false) AND normalized_name COLLATE "C">=(SELECT normalized_name FROM users WHERE name='Alpha') COLLATE "C"`, `normalized_name COLLATE "C" DESC,id DESC`, 1, 2},
		"hidden-disabled": {`is_disabled AND COALESCE((policy->>'IsHidden')::boolean,false)`, `normalized_name COLLATE "C",id`, 0, 100},
		"lower-bound":     {`normalized_name COLLATE "C">=(SELECT normalized_name FROM users WHERE name='Bravo') COLLATE "C"`, `normalized_name COLLATE "C",id`, 0, 100},
		"count-only":      {`true`, `normalized_name COLLATE "C",id`, 0, 0},
		"beyond-end":      {`true`, `normalized_name COLLATE "C",id`, 999, 100},
	}
	seen := map[string]bool{}
	for _, result := range actual {
		spec, exists := cases[result.Key]
		if !exists || seen[result.Key] || result.IDs == nil {
			return errors.New("media analysis phase 1 directory evidence has an unknown or repeated case")
		}
		seen[result.Key] = true
		var count int
		var ids []string
		statement := `SELECT (SELECT count(*) FROM users WHERE ` + spec.predicate + `),COALESCE((SELECT array_agg(id) FROM (SELECT id FROM users WHERE ` + spec.predicate + ` ORDER BY ` + spec.order + ` OFFSET $1 LIMIT $2) page),'{}'::text[])`
		if observer.runtime.f.pool.QueryRow(ctx, statement, spec.offset, spec.limit).Scan(&count, &ids) != nil || count != result.TotalRecordCount || !slices.Equal(ids, result.IDs) {
			return errors.New("media analysis phase 1 directory page differs from independent filtered ordered rows")
		}
	}
	return nil
}

func (observer *mediaAnalysisPhase1Observer) movieState(ctx context.Context, imported bool, ids []string) error {
	f, fixture := observer.runtime.f, observer.fixture
	var name, localName, overview, localOverview string
	if f.pool.QueryRow(ctx, "SELECT name,local_metadata->>'Name',overview,local_metadata->>'Overview' FROM items WHERE id=$1", fixture.MovieAId).Scan(&name, &localName, &overview, &localOverview) != nil {
		return errors.New("media analysis phase 1 movie source metadata could not be observed")
	}
	expected := fixture.MovieAName
	expectedOverview := "Initial local import"
	if imported {
		expected = fixture.ImportedMovieName
		expectedOverview = "Changed local import"
	}
	if name != expected || localName != expected || overview != expectedOverview || localOverview != expectedOverview {
		return errors.New("media analysis phase 1 local metadata import did not respect the saved option")
	}
	wantOrder := []string{fixture.MovieAId, fixture.MovieBId}
	if imported {
		wantOrder = []string{fixture.MovieBId, fixture.MovieAId}
	}
	var storedOrder []string
	if f.pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY sort_name COLLATE "C",id) FROM items WHERE id=ANY($1::text[])`, wantOrder).Scan(&storedOrder) != nil || !slices.Equal(storedOrder, wantOrder) {
		return errors.New("media analysis phase 1 catalog sort names do not use the committed remove-word setting")
	}
	if ids != nil && !slices.Equal(ids, wantOrder) {
		return errors.New("media analysis phase 1 browser sort order does not use its committed remove-word setting")
	}
	return nil
}

func (observer *mediaAnalysisPhase1Observer) musicOptions(ctx context.Context, enabled bool, revision string, changed bool) error {
	var storedRevision string
	var localImages, embedded bool
	if observer.runtime.f.pool.QueryRow(ctx, `SELECT revision::text,
		COALESCE((options->>'EnableLocalImages')::boolean,true),COALESCE((options->>'EnableEmbeddedArtwork')::boolean,true)
		FROM libraries WHERE id=$1`, observer.fixture.MusicLibraryId).Scan(&storedRevision, &localImages, &embedded) != nil ||
		!localImages || embedded != enabled || revision == "" || revision != storedRevision || changed && revision == observer.musicRevision {
		return errors.New("media analysis phase 1 embedded artwork option did not preserve its master switch and shared revision")
	}
	observer.musicRevision = storedRevision
	return nil
}

func (observer *mediaAnalysisPhase1Observer) artworkState(ctx context.Context, arrived, extracted bool) error {
	f, fixture := observer.runtime.f, observer.fixture
	var retained, sidecar bool
	if f.pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM item_embedded_artwork e JOIN items i ON i.id=e.item_id
		 WHERE e.item_id=$1 AND e.status='ready' AND e.failure_code='' AND e.source_hash=$2 AND encode(sha256(e.content),'hex')=$2
		 AND e.width=64 AND e.height=64 AND e.extraction_version=1 AND e.probe_version=(i.media->>'ProbeVersion')::integer),
		EXISTS(SELECT 1 FROM item_images WHERE item_id=$3 AND image_type='Primary' AND image_index=0 AND source_hash=$4 AND width=64 AND height=64)`,
		fixture.ExistingEmbeddedAudioId, observer.existingArtworkHash, fixture.SidecarAudioId, observer.sidecarArtworkHash).Scan(&retained, &sidecar) != nil || !retained || !sidecar {
		return errors.New("media analysis phase 1 artwork setting changed a previously accepted embedded or sidecar cover")
	}
	if !arrived {
		return nil
	}
	var id, name string
	if f.pool.QueryRow(ctx, `SELECT id,name FROM items WHERE path=$1 AND library_id=$2 AND type='Audio' AND media IS NOT NULL`, observer.musicFiles.NewPath, fixture.MusicLibraryId).Scan(&id, &name) != nil || name != fixture.NewEmbeddedAudioName ||
		observer.newAudioID != "" && observer.newAudioID != id {
		return errors.New("media analysis phase 1 arriving audio was not independently scanned under its owned source path")
	}
	observer.newAudioID = id
	var total, ready int
	if f.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='ready' AND failure_code='' AND extraction_version=1 AND width=64 AND height=64 AND source_hash=encode(sha256(content),'hex'))
		FROM item_embedded_artwork WHERE item_id=$1`, id).Scan(&total, &ready) != nil || !extracted && total != 0 || extracted && (total != 1 || ready != 1) {
		return errors.New("media analysis phase 1 actual scan did not consume the embedded artwork enablement")
	}
	return nil
}

func (observer *mediaAnalysisPhase1Observer) images(ctx context.Context, actual []mediaAnalysisPhase1Image, extracted bool) (resultErr error) {
	expected := map[string]string{observer.fixture.ExistingEmbeddedAudioId: observer.musicFiles.ExistingPixels, observer.fixture.SidecarAudioId: observer.musicFiles.SidecarPixels}
	if extracted {
		expected[observer.newAudioID] = observer.musicFiles.NewPixels
	}
	if len(actual) != len(expected) {
		return errors.New("media analysis phase 1 browser omitted an actual artwork owner")
	}
	f, fixture := observer.runtime.f, observer.fixture
	credentials, err := f.users.Authenticate(ctx, fixture.ViewerName, fixture.ViewerPassword,
		identity.Client{Name: "Media analysis image observer", DeviceID: "media-analysis-phase1-image-observer", Device: "Linux", Version: "1"}, "emby")
	if err != nil {
		return errors.New("media analysis phase 1 independent image observer could not authenticate")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(cleanup, http.MethodPost, fixture.BaseURL+"/emby/Sessions/Logout", nil)
		loggedOut := false
		if err == nil {
			request.Header.Set("X-Emby-Token", credentials.Token)
			response, err := client.Do(request)
			if response != nil {
				closeErr := response.Body.Close()
				loggedOut = err == nil && response.StatusCode == http.StatusNoContent && closeErr == nil
			}
		}
		if !loggedOut {
			resultErr = errors.Join(resultErr, errors.New("media analysis phase 1 image observer HTTP logout failed"))
			if f.users.Revoke(cleanup, credentials.Token) != nil {
				resultErr = errors.Join(resultErr, errors.New("media analysis phase 1 image observer fallback authentication cleanup failed"))
			}
		}
	}()
	seen := map[string]bool{}
	for _, reported := range actual {
		pixels, exists := expected[reported.ItemID]
		if !exists || seen[reported.ItemID] || reported.Status != http.StatusOK || reported.Width != 64 || reported.Height != 64 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(reported.SHA256) {
			return errors.New("media analysis phase 1 browser artwork evidence does not bind unique decoded owners")
		}
		seen[reported.ItemID] = true
		body, _, err := selectedPhase2HTTP(ctx, client, fixture.BaseURL, "/emby/Items/"+reported.ItemID+"/Images/Primary/0", credentials.Token)
		if err != nil {
			return errors.New("media analysis phase 1 independent real image request failed")
		}
		digest := sha256.Sum256(body)
		actualPixels, width, height, err := selectedPhase2RGBAHash(body)
		if err != nil || actualPixels != pixels || width != reported.Width || height != reported.Height || int64(len(body)) != reported.Bytes || hex.EncodeToString(digest[:]) != reported.SHA256 {
			return errors.New("media analysis phase 1 browser and observer artwork differ from the authored owner pixels")
		}
	}
	if !extracted {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.BaseURL+"/emby/Items/"+observer.newAudioID+"/Images/Primary/0", nil)
		if err != nil {
			return errors.New("media analysis phase 1 missing artwork request could not be built")
		}
		request.Header.Set("X-Emby-Token", credentials.Token)
		response, err := client.Do(request)
		if err != nil {
			return errors.New("media analysis phase 1 missing artwork request did not complete")
		}
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusNotFound || closeErr != nil {
			return errors.New("media analysis phase 1 disabled new audio unexpectedly served an image")
		}
	}
	return nil
}

func mediaAnalysisPhase1Durable(ctx context.Context, f *serverFixture) (json.RawMessage, error) {
	catalog, err := selectedPhase3Durable(ctx, f)
	if err != nil {
		return nil, err
	}
	var settingsHash string
	if err := f.pool.QueryRow(ctx, `SELECT encode(sha256(to_jsonb(s)::text::bytea),'hex') FROM managed_settings s WHERE id=1`).Scan(&settingsHash); err != nil {
		return nil, err
	}
	var users json.RawMessage
	if err := f.pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('Id',id,'Name',name,'IsDisabled',is_disabled,'IsHidden',COALESCE((policy->>'IsHidden')::boolean,false)) ORDER BY id) FROM users`).Scan(&users); err != nil {
		return nil, err
	}
	var imageMetadata json.RawMessage
	if err := f.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.item_id,i.image_type,i.image_index),'[]') FROM item_images i`).Scan(&imageMetadata); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Catalog        json.RawMessage
		SettingsSHA256 string
		Users          json.RawMessage
		Images         json.RawMessage
	}{catalog, settingsHash, users, imageMetadata})
}

func (observer *mediaAnalysisPhase1Observer) stage(ctx context.Context, request mediaAnalysisPhase1Request) error {
	f, fixture := observer.runtime.f, observer.fixture
	switch request.Phase {
	case "authentication":
		var native, administrator, viewer int
		if f.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE user_id=$1 AND kind='admin'),count(*) FILTER(WHERE user_id=$1 AND kind='emby'),count(*) FILTER(WHERE user_id=$2 AND kind='emby') FROM sessions WHERE revoked_at IS NULL AND expires_at>clock_timestamp()`, fixture.AdminId, fixture.ViewerId).Scan(&native, &administrator, &viewer) != nil || native != 2 || administrator != 1 || viewer != 1 {
			return errors.New("media analysis phase 1 browser contexts did not authenticate independently")
		}
	case "user-saved":
		var saved bool
		if f.pool.QueryRow(ctx, `SELECT is_disabled AND (policy->>'IsHidden')::boolean FROM users WHERE id=$1`, fixture.EditedUserId).Scan(&saved) != nil || !saved {
			return errors.New("media analysis phase 1 native user edit was not committed")
		}
	case "user-query":
		return observer.userQueries(ctx, request.UserQueries)
	case "specials-before":
		if !selectedPhase3SameIDs(request.QueryIDs, []string{fixture.StandaloneSpecialId}) {
			return errors.New("media analysis phase 1 initial standalone query lost NFO placement semantics")
		}
		return observer.metadata(ctx, false)
	case "metadata-saved":
		if request.SeriesRevision == "" || request.PlacementRevision == "" || request.SeriesRevision == observer.seriesRevision || request.PlacementRevision == observer.placementRevision {
			return errors.New("media analysis phase 1 native metadata did not advance both saved revisions")
		}
		if err := observer.metadata(ctx, true); err != nil {
			return err
		}
		var series, placed string
		if f.pool.QueryRow(ctx, `SELECT (SELECT revision::text FROM item_metadata_state WHERE item_id=$1),(SELECT revision::text FROM item_metadata_state WHERE item_id=$2)`, fixture.SeriesId, fixture.PlacedSpecialId).Scan(&series, &placed) != nil || series != request.SeriesRevision || placed != request.PlacementRevision {
			return errors.New("media analysis phase 1 browser metadata revisions differ from committed rows")
		}
		observer.seriesRevision, observer.placementRevision = series, placed
	case "specials-after":
		if !selectedPhase3SameIDs(request.QueryIDs, []string{fixture.StandaloneSpecialId, fixture.PlacedSpecialId}) {
			return errors.New("media analysis phase 1 edited special was not returned with the original standalone special")
		}
		if err := observer.completedScan(ctx, request.ScanJobId, fixture.TelevisionLibraryId, 3); err != nil {
			return err
		}
		return observer.metadata(ctx, true)
	case "sorting-saved":
		if request.SettingsRevision == "" || request.SettingsRevision == observer.settingsRevision {
			return errors.New("media analysis phase 1 native sorting save did not advance settings revision")
		}
		var revision string
		var words []string
		if f.pool.QueryRow(ctx, "SELECT revision::text,sort_remove_words FROM managed_settings WHERE id=1").Scan(&revision, &words) != nil || revision != request.SettingsRevision || len(words) != 1 || !strings.EqualFold(words[0], "The") {
			return errors.New("media analysis phase 1 sorting save revision differs from its committed row")
		}
		observer.settingsRevision = revision
		if err := observer.movieState(ctx, false, nil); err != nil {
			return err
		}
	case "sorting-consumed":
		if request.QueryIDs == nil {
			return errors.New("media analysis phase 1 browser omitted its sorted movie page")
		}
		if err := observer.completedScan(ctx, request.ScanJobId, fixture.MovieLibraryId, 2); err != nil {
			return err
		}
		if err := observer.movieState(ctx, false, request.QueryIDs); err != nil {
			return err
		}
		if os.WriteFile(observer.movieNFO, []byte(`<movie><title>`+fixture.ImportedMovieName+`</title><plot>Changed local import</plot></movie>`), 0o600) != nil {
			return errors.New("media analysis phase 1 declared NFO change failed")
		}
		fact, err := selectedPhase2Fact(observer.movieNFO, 32<<20)
		if err != nil {
			return errors.New("media analysis phase 1 changed NFO identity could not be captured")
		}
		observer.files[observer.movieNFO] = fact
	case "imports-disabled", "imports-enabled":
		var enabled bool
		var revision string
		if f.pool.QueryRow(ctx, "SELECT COALESCE((options->>'EnableLocalMetadata')::boolean,true),revision::text FROM libraries WHERE id=$1", fixture.MovieLibraryId).Scan(&enabled, &revision) != nil || enabled != (request.Phase == "imports-enabled") || request.LibraryRevision == "" || revision != request.LibraryRevision || revision == observer.libraryRevision {
			return errors.New("media analysis phase 1 library import option was not saved with its browser revision")
		}
		observer.libraryRevision = revision
	case "imports-retained", "imports-consumed":
		if request.QueryIDs == nil {
			return errors.New("media analysis phase 1 browser omitted its imported movie page")
		}
		if err := observer.completedScan(ctx, request.ScanJobId, fixture.MovieLibraryId, 2); err != nil {
			return err
		}
		return observer.movieState(ctx, request.Phase == "imports-consumed", request.QueryIDs)
	case "embedded-disabled":
		if err := observer.musicOptions(ctx, false, request.MusicLibraryRevision, true); err != nil {
			return err
		}
		if err := observer.artworkState(ctx, false, false); err != nil {
			return err
		}
		if os.MkdirAll(filepath.Dir(observer.musicFiles.NewPath), 0o700) != nil || selectedPhase3Copy(observer.musicFiles.NewSource, observer.musicFiles.NewPath) != nil {
			return errors.New("media analysis phase 1 new artwork source could not arrive after disabling extraction")
		}
		fact, err := selectedPhase2Fact(observer.musicFiles.NewPath, 32<<20)
		if err != nil {
			return errors.New("media analysis phase 1 arriving artwork source identity could not be captured")
		}
		observer.files[observer.musicFiles.NewPath] = fact
	case "embedded-absent", "embedded-present":
		extracted := request.Phase == "embedded-present"
		if err := observer.musicOptions(ctx, extracted, observer.musicRevision, false); err != nil {
			return err
		}
		if err := observer.completedScan(ctx, request.ScanJobId, fixture.MusicLibraryId, 3); err != nil {
			return err
		}
		if err := observer.artworkState(ctx, true, extracted); err != nil {
			return err
		}
		if request.NewAudioId != observer.newAudioID {
			return errors.New("media analysis phase 1 browser discovered a different arriving audio source")
		}
		if err := observer.images(ctx, request.Images, extracted); err != nil {
			return err
		}
		if extracted {
			observer.acceptedImages = map[string]mediaAnalysisPhase1Image{}
			for _, reported := range request.Images {
				observer.acceptedImages[reported.ItemID] = reported
			}
		}
	case "embedded-enabled":
		if request.NewAudioId != observer.newAudioID {
			return errors.New("media analysis phase 1 embedded enablement changed its arriving audio identity")
		}
		if err := observer.musicOptions(ctx, true, request.MusicLibraryRevision, true); err != nil {
			return err
		}
		if err := observer.artworkState(ctx, true, false); err != nil {
			return err
		}
	case "fields":
		if request.Fields.ID != fixture.MovieAId || request.Fields.SearchHintID != fixture.MovieAId || request.Fields.SearchDetailID != fixture.MovieAId ||
			!request.Fields.ExcludedDetailMediaStreams || !request.Fields.ExcludedNestedMediaStreams || request.Fields.Language != "zh-CN" {
			return errors.New("media analysis phase 1 field and search projection did not bind the current physical movie")
		}
		var streams int
		if f.pool.QueryRow(ctx, "SELECT jsonb_array_length(media->'Streams') FROM items WHERE id=$1", fixture.MovieAId).Scan(&streams) != nil || streams < 2 || request.Fields.ListMediaStreams != streams {
			return errors.New("media analysis phase 1 list fields differ from the actual probed media streams")
		}
	case "restart":
		if err := observer.metadata(ctx, true); err != nil {
			return err
		}
		if err := observer.movieState(ctx, true, nil); err != nil {
			return err
		}
		if err := observer.artworkState(ctx, true, true); err != nil {
			return err
		}
		var err error
		observer.durable, err = mediaAnalysisPhase1Durable(ctx, f)
		if err != nil || featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-before.json"), observer.durable) != nil {
			return errors.New("media analysis phase 1 restart baseline could not be retained")
		}
		if err := observer.runtime.restart(ctx); err != nil {
			return errors.New("media analysis phase 1 complete runtime restart failed")
		}
		current, err := mediaAnalysisPhase1Durable(ctx, f)
		if err != nil || !bytes.Equal(current, observer.durable) {
			return errors.New("media analysis phase 1 restart changed its durable accepted state")
		}
	case "persisted":
		if request.QueryIDs == nil {
			return errors.New("media analysis phase 1 post-restart readback omitted its current sorted movie page")
		}
		if request.SettingsRevision != observer.settingsRevision || request.LibraryRevision != observer.libraryRevision || request.SeriesRevision != observer.seriesRevision || request.PlacementRevision != observer.placementRevision {
			return errors.New("media analysis phase 1 post-restart readback lost the saved revisions")
		}
		if request.NewAudioId != observer.newAudioID {
			return errors.New("media analysis phase 1 restarted browser lost its arriving audio identity")
		}
		if err := observer.musicOptions(ctx, true, request.MusicLibraryRevision, false); err != nil {
			return err
		}
		if err := observer.artworkState(ctx, true, true); err != nil {
			return err
		}
		if len(observer.acceptedImages) != 3 || len(request.Images) != len(observer.acceptedImages) {
			return errors.New("media analysis phase 1 restarted browser omitted an accepted artwork owner")
		}
		seenImages := map[string]bool{}
		for _, reported := range request.Images {
			baseline, exists := observer.acceptedImages[reported.ItemID]
			if !exists || seenImages[reported.ItemID] || reported != baseline {
				return errors.New("media analysis phase 1 restarted HTTP images differ from independently verified owner bytes")
			}
			seenImages[reported.ItemID] = true
		}
		if err := observer.metadata(ctx, true); err != nil {
			return err
		}
		if err := observer.movieState(ctx, true, request.QueryIDs); err != nil {
			return err
		}
		current, err := mediaAnalysisPhase1Durable(ctx, f)
		if err != nil || !bytes.Equal(current, observer.durable) || featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-after.json"), current) != nil {
			return errors.New("media analysis phase 1 restart readback changed or lost its durable accepted state")
		}
	case "cleanup":
		if err := f.users.Revoke(ctx, observer.actorToken); err != nil {
			return errors.New("media analysis phase 1 observer authentication could not retire")
		}
		var sessions, scans, tasks int
		if f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping'))`).Scan(&sessions, &scans, &tasks) != nil || sessions != 0 || scans != 0 || tasks != 0 {
			return errors.New("media analysis phase 1 normal cleanup retained credentials or background work")
		}
		if observer.runtime.close(ctx) != nil || observer.runtime.active.Load() != 0 {
			return errors.New("media analysis phase 1 actual runtime did not close its listener and workers")
		}
		for _, count := range selectedPhase1RuntimeFacts(f) {
			if count != 0 {
				return errors.New("media analysis phase 1 closed runtime retained playback resources")
			}
		}
	}
	var plays, jobs int
	if f.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM play_sessions),(SELECT count(*) FROM encoding_jobs)").Scan(&plays, &jobs) != nil || plays != 0 || jobs != 0 {
		return errors.New("media analysis phase 1 catalog-only journeys started unrelated playback work")
	}
	_, err := observer.filesVerified()
	return err
}

func (observer *mediaAnalysisPhase1Observer) run(ctx context.Context) error {
	for _, phase := range mediaAnalysisPhase1Phases {
		value := map[string]any{"Marker": "goby-media-analysis-phase1-stage-database-v1", "RunId": observer.fixture.RunId, "Phase": phase, "Complete": false, "Observed": false}
		ack := filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json")
		stageErr := phase3BrowserWaitRequest(ctx, phase3BrowserContext{RunID: observer.fixture.RunId, ArtifactsDir: observer.fixture.ArtifactsDir}, phase)
		var request mediaAnalysisPhase1Request
		if stageErr == nil {
			if featureWavePrivateJSON(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-request.json"), 64<<10, &request) != nil || request.RunId != observer.fixture.RunId || request.Phase != phase {
				stageErr = errors.New("media analysis phase 1 request does not bind its owned run and phase")
			}
		}
		if stageErr == nil {
			_, stageErr = observer.snapshot(ctx, phase, "before-observation")
		}
		if stageErr == nil {
			stageErr = observer.stage(ctx, request)
		}
		after, snapshotErr := observer.snapshot(ctx, phase, "after-observation")
		if after != nil {
			value = after
		}
		if stageErr == nil {
			stageErr = snapshotErr
		}
		if stageErr != nil {
			value["Failed"], value["ErrorCode"], value["FailureDetail"] = true, "database_stage_verification_failed", mediaAnalysisPhase1Failure(stageErr)
			_ = featureWaveWriteCheckpoint(ack, value)
			return stageErr
		}
		value["Complete"], value["Observed"], value["ExpectedFilesVerified"] = true, true, true
		if featureWaveWriteCheckpoint(ack, value) != nil {
			return errors.New("media analysis phase 1 acknowledgement could not be retained")
		}
		observer.completed++
	}
	return nil
}

func TestMediaAnalysisResiliencePhase1BrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("media analysis browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	execution, inventory := mediaAnalysisPhase1ReadExecution(t)
	runID := os.Getenv("GOBY_MEDIA_ANALYSIS_PHASE1_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("media analysis phase 1 requires an explicit owned run identity")
	}
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("media analysis phase 1 artifact parent must be private")
	}
	cwd, err := os.Getwd()
	if err != nil {
		selectedPhase2Fatal(t, "read media analysis source directory", err)
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("media analysis source root was not found")
	}
	driverPath := filepath.Join(sourceRoot, "scripts", "test-env", "media-analysis-resilience-phase1-browser.mjs")
	for _, path := range []string{node, playwright, driverPath, filepath.Join(sourceRoot, "internal", "server", "media_analysis_resilience_phase1_browser_integration_test.go")} {
		fact, err := selectedPhase2Fact(path, 256<<20)
		if err != nil {
			t.Fatal("media analysis browser input identity or ownership is invalid")
		}
		inventory = append(inventory, fact)
	}
	output, err := os.MkdirTemp(artifacts, "media-analysis-phase1-browser-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create private media analysis browser artifacts")
	}
	driver := map[string]any{"Marker": "goby-media-analysis-phase1-browser-driver-v1", "RunId": runID, "Complete": false,
		"ArtifactDirectory": output, "CredentialsWrittenToSummary": false, "RealProber": true, "OriginalClientUsed": false}
	var schema, mediaRoot string
	t.Cleanup(func() {
		if schema != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe media analysis owned schema cleanup")
			} else {
				defer pool.Close()
				var remains bool
				if pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains) != nil || remains {
					t.Error("media analysis owned schema was not removed")
				} else {
					driver["OwnedSchemaRemoved"] = true
				}
			}
		}
		if mediaRoot != "" {
			if _, err := os.Lstat(mediaRoot); !errors.Is(err, os.ErrNotExist) {
				t.Error("media analysis owned media root was not removed")
			} else {
				driver["OwnedMediaRootRemoved"] = true
			}
		}
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("retain media analysis driver result")
		}
	})
	if refreshBrowserWriteJSON(filepath.Join(output, "execution-inventory.json"), inventory) != nil {
		t.Fatal("retain media analysis execution inventory")
	}
	f := newServerFixtureWithTimeout(t, 15*time.Minute)
	if f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema) != nil {
		t.Fatal("identify media analysis owned schema")
	}
	mediaRoot = t.TempDir()
	paths := mediaAnalysisPhase1Author(t, execution, mediaRoot)
	musicFiles := mediaAnalysisPhase1AuthorMusic(t, execution, mediaRoot)
	files, err := selectedPhase3Files(mediaRoot)
	if err != nil {
		selectedPhase2Fatal(t, "capture media analysis source identities", err)
	}
	if err := f.app.Close(f.ctx); err != nil {
		selectedPhase2Fatal(t, "close initial media analysis application", err)
	}
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = execution.FFmpegPath, execution.FFprobePath, []string{mediaRoot}
	assets, err := adminassets.Files()
	if err != nil {
		selectedPhase2Fatal(t, "open media analysis native assets", err)
	}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "media-analysis-phase1-browser-integration", WithDashboardAssets(assets))
	if err != nil {
		selectedPhase2Fatal(t, "create actual media analysis application", err)
	}
	f.app, f.handler = app, app.Handler()
	runtime := &mediaAnalysisPhase1Runtime{phase3BrowserRuntime: &phase3BrowserRuntime{f: f, assets: assets, addr: "127.0.0.1:0"}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe residual media analysis authentication")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("retire residual media analysis authentication")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
			if driver["Complete"] == true && result.RowsAffected() != 0 {
				t.Error("successful media analysis browser required fallback authentication cleanup")
			}
		}
		if runtime.close(ctx) != nil {
			t.Error("close actual media analysis runtime workers")
		} else {
			driver["ServerWorkersClosed"], driver["HTTPListenerClosed"] = true, true
		}
		driver["ActiveHTTPRequestsAfterClose"] = runtime.active.Load()
		if runtime.active.Load() != 0 {
			t.Error("media analysis HTTP requests remain after shutdown")
		}
	})
	adminPassword, viewerPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Administrator", adminPassword)
	if err != nil {
		selectedPhase2Fatal(t, "bootstrap media analysis administrator", err)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Viewer", viewerPassword, false)
	if err != nil {
		selectedPhase2Fatal(t, "create media analysis viewer", err)
	}
	credentials, err := f.users.Authenticate(f.ctx, admin.Name, adminPassword,
		identity.Client{Name: "Media analysis observer", DeviceID: "media-analysis-phase1-observer", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "authenticate media analysis observer", err)
	}
	actor, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "resolve media analysis observer", err)
	}
	fixture := mediaAnalysisPhase1Context{Marker: "goby-media-analysis-phase1-browser-fixture-v1", RunId: runID,
		AdminId: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword, ViewerId: viewer.ID, ViewerName: viewer.Name, ViewerPassword: viewerPassword,
		UserDirectory:         []mediaAnalysisPhase1User{{ID: admin.ID, Name: admin.Name}, {ID: viewer.ID, Name: viewer.Name}},
		TelevisionLibraryName: "Media Analysis Television", MovieLibraryName: "Media Analysis Movies", MusicLibraryName: "Media Analysis Music",
		NewEmbeddedAudioName: "Arriving embedded track", ImportedMovieName: "Zulu imported", SearchTerm: "special",
		ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	for _, spec := range []struct {
		name             string
		hidden, disabled bool
	}{{"Alpha", false, false}, {"Bravo", false, false}, {"Charlie", true, false}, {"Delta", false, true}, {"中国账户", false, false}} {
		user, err := f.users.CreateUser(f.ctx, spec.name, featureWavePassword(t), false)
		if err != nil {
			selectedPhase2Fatal(t, "create media analysis directory population", err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_disabled=$2,policy=jsonb_set(policy,'{IsHidden}',to_jsonb($3::boolean),true) WHERE id=$1`, user.ID, spec.disabled, spec.hidden); err != nil {
			selectedPhase2Fatal(t, "configure media analysis directory population", err)
		}
		fixture.UserDirectory = append(fixture.UserDirectory, mediaAnalysisPhase1User{ID: user.ID, Name: user.Name, IsHidden: spec.hidden, IsDisabled: spec.disabled})
		if user.Name == "Bravo" {
			fixture.EditedUserId, fixture.EditedUserName = user.ID, user.Name
		}
	}
	television, err := app.library.CreateLibrary(f.ctx, fixture.TelevisionLibraryName, "tvshows", []string{filepath.Join(mediaRoot, "tv")})
	if err != nil {
		selectedPhase2Fatal(t, "create media analysis television library", err)
	}
	movies, err := app.library.CreateLibrary(f.ctx, fixture.MovieLibraryName, "movies", []string{filepath.Join(mediaRoot, "movies")})
	if err != nil {
		selectedPhase2Fatal(t, "create media analysis movie library", err)
	}
	music, err := app.library.CreateLibrary(f.ctx, fixture.MusicLibraryName, "music", []string{filepath.Join(mediaRoot, "music")})
	if err != nil {
		selectedPhase2Fatal(t, "create media analysis music library", err)
	}
	for _, scan := range []struct {
		id       string
		expected int
	}{{television.ID, 3}, {movies.ID, 2}, {music.ID, 2}} {
		if err := selectedPhase3Scan(f.ctx, f, scan.id, scan.expected); err != nil {
			selectedPhase2Fatal(t, "scan actual media analysis sources", err)
		}
	}
	first, second := selectedPhase3Item(t, f, admin.ID, paths[0], "Movie"), selectedPhase3Item(t, f, admin.ID, paths[1], "Movie")
	standalone, placed, regular := selectedPhase3Item(t, f, admin.ID, paths[2], "Episode"), selectedPhase3Item(t, f, admin.ID, paths[3], "Episode"), selectedPhase3Item(t, f, admin.ID, paths[4], "Episode")
	if standalone.Series == nil || placed.Series == nil || regular.Series == nil || standalone.Series.ID != placed.Series.ID || standalone.Series.ID != regular.Series.ID {
		t.Fatal("media analysis authored episodes did not share their actual series")
	}
	fixture.TelevisionLibraryId, fixture.MovieLibraryId = television.ID, movies.ID
	fixture.MusicLibraryId = music.ID
	existingAudio, sidecarAudio := selectedPhase3Item(t, f, admin.ID, musicFiles.ExistingPath, "Audio"), selectedPhase3Item(t, f, admin.ID, musicFiles.SidecarPath, "Audio")
	fixture.ExistingEmbeddedAudioId, fixture.ExistingEmbeddedAudioName = existingAudio.ID, existingAudio.Name
	fixture.SidecarAudioId, fixture.SidecarAudioName = sidecarAudio.ID, sidecarAudio.Name
	fixture.MovieAId, fixture.MovieAName, fixture.MovieBId, fixture.MovieBName = first.ID, first.Name, second.ID, second.Name
	fixture.StandaloneSpecialId, fixture.StandaloneSpecialName = standalone.ID, standalone.Name
	fixture.PlacedSpecialId, fixture.PlacedSpecialName = placed.ID, placed.Name
	fixture.RegularEpisodeId, fixture.RegularEpisodeName = regular.ID, regular.Name
	fixture.SeriesId, fixture.SeriesName = standalone.Series.ID, standalone.Series.Name
	policy, _ := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{television.ID, movies.ID, music.ID}, "EnableMediaPlayback": true})
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
		selectedPhase2Fatal(t, "set media analysis viewer scope", err)
	}
	embedded, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		selectedPhase2Fatal(t, "inventory media analysis native assets", err)
	}
	frozen, err := refreshBrowserAssetInventory(os.DirFS(filepath.Join(sourceRoot, "web", "admin", "dist")))
	if err != nil || !reflect.DeepEqual(embedded, frozen) {
		t.Fatal("media analysis embedded assets differ from frozen source assets")
	}
	if refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embedded) != nil {
		t.Fatal("retain media analysis frozen assets")
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	if err := runtime.listen(); err != nil {
		selectedPhase2Fatal(t, "start owned media analysis listener", err)
	}
	fixture.BaseURL = "http://" + runtime.addr
	observer := &mediaAnalysisPhase1Observer{runtime: runtime, fixture: fixture, actor: actor, actorToken: credentials.Token, files: files,
		movieNFO: strings.TrimSuffix(paths[0], ".mp4") + ".nfo", mediaRoot: mediaRoot, usedScans: map[string]bool{}, libraryRevision: movies.Revision, musicRevision: music.Revision, musicFiles: musicFiles}
	if f.pool.QueryRow(f.ctx, `SELECT (SELECT source_hash FROM item_embedded_artwork WHERE item_id=$1 AND status='ready'),
		(SELECT source_hash FROM item_images WHERE item_id=$2 AND image_type='Primary' AND image_index=0)`, fixture.ExistingEmbeddedAudioId, fixture.SidecarAudioId).
		Scan(&observer.existingArtworkHash, &observer.sidecarArtworkHash) != nil {
		t.Fatal("capture accepted embedded and sidecar artwork identities")
	}
	if err := observer.artworkState(f.ctx, false, false); err != nil {
		selectedPhase2Fatal(t, "verify actual initial audio artwork", err)
	}
	if f.pool.QueryRow(f.ctx, "SELECT COALESCE(array_agg(id),'{}'::text[]) FROM scan_jobs").Scan(&observer.initialScans) != nil || len(observer.initialScans) != 3 {
		t.Fatal("capture media analysis initial scan identities")
	}
	if f.pool.QueryRow(f.ctx, `SELECT (SELECT revision::text FROM item_metadata_state WHERE item_id=$1),(SELECT revision::text FROM item_metadata_state WHERE item_id=$2),(SELECT revision::text FROM managed_settings WHERE id=1)`, fixture.SeriesId, fixture.PlacedSpecialId).
		Scan(&observer.seriesRevision, &observer.placementRevision, &observer.settingsRevision) != nil {
		t.Fatal("capture media analysis initial revisions")
	}
	if err := observer.metadata(f.ctx, false); err != nil {
		selectedPhase2Fatal(t, "verify actual media analysis NFO import", err)
	}
	if _, err := observer.snapshot(f.ctx, "seeded", "database"); err != nil {
		selectedPhase2Fatal(t, "retain media analysis seeded facts", err)
	}
	contextPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove media analysis browser credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if refreshBrowserWriteJSON(contextPath, fixture) != nil {
		t.Fatal("write private media analysis browser context")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create private media analysis browser directories")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright,
		"GOBY_MEDIA_ANALYSIS_PHASE1_RUN_ID=" + runID, "GOBY_MEDIA_ANALYSIS_PHASE1_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Minute)
	defer cancel()
	observerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{driverPath}, environment, sourceRoot, output)
	stop()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	if observerErr != nil && observer.completed < len(mediaAnalysisPhase1Phases) {
		driver["FailedDatabaseStage"], driver["DatabaseObservationError"] = mediaAnalysisPhase1Phases[observer.completed], mediaAnalysisPhase1Failure(observerErr)
	}
	var result selectedPhase2Result
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(mediaAnalysisPhase1Phases) {
		t.Fatalf("media analysis browser or observer failed: command=%s observer=%s result=%s completed_stages=%d; inspect retained private artifacts",
			selectedPhase2SafeError(commandErr), mediaAnalysisPhase1Failure(observerErr), selectedPhase2SafeError(readErr), observer.completed)
	}
	checks := mediaAnalysisPhase1Phases
	if result.Marker != "goby-media-analysis-phase1-browser-result-v1" || result.RunID != runID || !result.Complete || len(result.Checks) != len(checks) ||
		len(result.Stages) != len(mediaAnalysisPhase1Phases) || result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("media analysis browser result does not bind its complete actual scenario")
	}
	for index, phase := range mediaAnalysisPhase1Phases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" || !result.Checks[checks[index]] {
			t.Fatal("media analysis browser and database stages do not agree")
		}
	}
	if command["FailureCleanupSignals"] != 0 || command["ObservedDescendantsClosed"] != true || command["ProcessGroupClosed"] != true {
		t.Fatal("media analysis successful browser required abnormal process cleanup")
	}
	for _, expected := range inventory {
		current, err := selectedPhase2Fact(expected.Path, 256<<20)
		if err != nil || current != expected {
			t.Fatal("media analysis execution inputs changed during the owned run")
		}
	}
	driver["ExecutionInputsUnchanged"], driver["ExpectedFilesVerified"], driver["OwnedSessionsRetired"] = true, true, true
	driver["Complete"], driver["BrowserChecks"], driver["ServerRestarts"] = true, result.Checks, 1
	driver["ObservedIndependentScans"] = len(observer.usedScans)
	t.Log("media_analysis_phase1_browser_verified=true stages=20 source_nfo_changes=1 actual_audio_arrivals=1 server_restarts=1")
}
