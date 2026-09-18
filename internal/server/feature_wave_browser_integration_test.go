//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/tasks"
	adminassets "github.com/moooyo/goby/web/admin"
)

const featureWaveBrowserTitle = "live feature wave preserves media and enforces saved administrator controls"

var featureWaveBrowserPhases = []string{"authentication", "playlist", "collection", "user-policy", "management", "cache", "cleanup"}

// The only credential-bearing artifact is this temporary, private manifest.
// The child environment receives its pathname, never a password or token.
type featureWaveBrowserFixture struct {
	Marker         string
	RunID          string `json:"RunId"`
	BaseURL        string
	AdminID        string `json:"AdminId"`
	AdminName      string
	AdminPassword  string
	MemberID       string `json:"MemberId"`
	MemberName     string
	MemberPassword string
	LibraryID      string   `json:"LibraryId"`
	MovieIDs       []string `json:"MovieIds"`
	ArtifactsDir   string
	ResultPath     string
}

type featureWaveBrowserResult struct {
	Marker       string
	RunID        string `json:"RunId"`
	Complete     bool
	PlaylistID   string `json:"PlaylistId"`
	CollectionID string `json:"CollectionId"`
	CacheRunID   string `json:"CacheRunId"`
	Checks       map[string]bool
	Stages       []json.RawMessage
}

type featureWaveSourceFact struct {
	Name   string
	Bytes  int64
	SHA256 string
}

func featureWaveSourceFacts(paths []string) ([]featureWaveSourceFact, error) {
	facts := make([]featureWaveSourceFact, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, errors.New("owned feature-wave source metadata changed")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return nil, errors.New("owned feature-wave source link identity changed")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("owned feature-wave source could not be read")
		}
		hash := sha256.Sum256(data)
		facts = append(facts, featureWaveSourceFact{filepath.Base(path), int64(len(data)), hex.EncodeToString(hash[:])})
	}
	return facts, nil
}

func featureWavePrivateJSON(path string, maximum int64, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || info.Mode().Perm() != 0o600 || info.Size() > maximum {
		return errors.New("feature-wave artifact is not a bounded private regular file")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return errors.New("feature-wave artifact path is not canonical")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > maximum {
		return errors.New("feature-wave artifact could not be read within its bound")
	}
	return json.Unmarshal(data, target)
}

func featureWaveWriteCheckpoint(path string, value any) error {
	temporary := path + ".pending"
	defer os.Remove(temporary)
	if err := refreshBrowserWriteJSON(temporary, value); err != nil {
		return err
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("feature-wave checkpoint acknowledgement already exists or cannot be observed")
	}
	return os.Rename(temporary, path)
}

// Snapshots deliberately enumerate safe columns. They never include password
// hashes, authentication tokens, CSRF tokens, provider credentials, or URLs.
func featureWaveDatabaseSnapshot(ctx context.Context, f *serverFixture, memberID string) (json.RawMessage, error) {
	var text string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Schema', current_schema(),
		'Libraries', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Name',name,'CollectionType',collection_type) ORDER BY id),'[]'::jsonb) FROM libraries),
		'MediaItems', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'LibraryId',library_id,'Type',type,'Name',name) ORDER BY id),'[]'::jsonb) FROM items WHERE type='Movie'),
		'Collections', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',item_id,'OwnerId',owner_id,'Kind',kind,'IsPublic',is_public,'IsLocked',is_locked) ORDER BY item_id),'[]'::jsonb) FROM media_collections),
		'Entries', (SELECT COALESCE(jsonb_agg(jsonb_build_object('EntryId',id::text,'CollectionId',collection_id,'ItemId',item_id,'Position',position) ORDER BY collection_id,position),'[]'::jsonb) FROM media_collection_entries),
		'Shares', (SELECT COALESCE(jsonb_agg(jsonb_build_object('CollectionId',collection_id,'UserId',user_id,'CanEdit',can_edit) ORDER BY collection_id,user_id),'[]'::jsonb) FROM media_collection_shares),
		'MemberPolicy', (SELECT policy FROM users WHERE id=$1),
		'Management', (SELECT management FROM managed_settings WHERE id=1),
		'TaskRuns', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'TaskKey',task_key,'State',state,'Source',source,'TotalChildren',total_children,'TerminalChildren',terminal_children,'Scanned',scanned,'Updated',updated) ORDER BY id),'[]'::jsonb) FROM task_runs),
		'TaskChildren', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'RunId',run_id,'LibraryId',library_id,'State',state,'Scanned',scanned,'Updated',updated) ORDER BY run_id,ordinal),'[]'::jsonb) FROM task_run_children),
		'CacheRows', (SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',item_id,'ImageType',image_type,'ImageIndex',image_index,'Bytes',octet_length(content),'SourceHash',source_hash) ORDER BY item_id,image_type,image_index),'[]'::jsonb) FROM item_provider_images),
		'ActiveSessions', (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'RevokedSessions', (SELECT count(*) FROM sessions WHERE revoked_at IS NOT NULL)
	)::text`, memberID).Scan(&text)
	return json.RawMessage(text), err
}

// The browser waits for every acknowledgement before starting its next stage.
// Consequently each SQL/source snapshot reflects the current completed stage,
// rather than reconstructing all intermediate states after the browser exits.
func featureWaveCheckpoints(ctx context.Context, f *serverFixture, fixture featureWaveBrowserFixture, paths []string, baseline []featureWaveSourceFact) (int, error) {
	completed := 0
	for _, phase := range featureWaveBrowserPhases {
		requestPath := filepath.Join(fixture.ArtifactsDir, "stage-"+phase+"-request.json")
		for {
			var request struct {
				RunID string `json:"RunId"`
				Phase string
			}
			err := featureWavePrivateJSON(requestPath, 4096, &request)
			var syntax *json.SyntaxError
			if errors.Is(err, os.ErrNotExist) || errors.As(err, &syntax) && syntax.Error() == "unexpected end of JSON input" {
				select {
				case <-ctx.Done():
					return completed, ctx.Err()
				case <-time.After(50 * time.Millisecond):
					continue
				}
			}
			if err != nil || request.RunID != fixture.RunID || request.Phase != phase {
				return completed, errors.New("feature-wave checkpoint does not bind the expected stage")
			}
			break
		}
		sources, err := featureWaveSourceFacts(paths)
		if err != nil || !reflect.DeepEqual(sources, baseline) {
			return completed, errors.New("feature-wave operation changed source files")
		}
		database, err := featureWaveDatabaseSnapshot(ctx, f, fixture.MemberID)
		if err != nil {
			return completed, errors.New("feature-wave stage database observation failed")
		}
		summary := map[string]any{"Marker": "goby-feature-wave-stage-database-v1", "RunId": fixture.RunID,
			"Phase": phase, "Complete": true, "SourcesUnchanged": true, "Sources": sources, "Database": database}
		if err := featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "stage-"+phase+"-database.json"), summary); err != nil {
			return completed, errors.New("feature-wave stage acknowledgement could not be preserved")
		}
		completed++
	}
	return completed, nil
}

func featureWavePassword(t *testing.T) string {
	t.Helper()
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal("generate a private feature-wave fixture password")
	}
	return hex.EncodeToString(secret[:])
}

func TestFeatureWaveBrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("feature-wave browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	cli := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_CLI", false)
	work := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_WORK", true)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("feature-wave artifact parent must be private")
	}
	runID := os.Getenv("GOBY_FEATURE_WAVE_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("a valid explicit feature-wave run ID is required")
	}
	for _, name := range []string{"playwright.config.ts", "e2e/feature-wave-live.spec.ts"} {
		info, err := os.Lstat(filepath.Join(work, name))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("the owned feature-wave Playwright work copy is incomplete")
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read feature-wave source directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate feature-wave source root")
	}
	relative, err := filepath.Rel(sourceRoot, work)
	if err != nil || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("Playwright work must be outside the frozen Go source")
	}
	output, err := os.MkdirTemp(artifacts, "feature-wave-browser-")
	if err != nil {
		t.Fatal("create feature-wave artifact directory")
	}
	if err := os.Chmod(output, 0o700); err != nil {
		t.Fatal("protect feature-wave artifacts")
	}
	driver := map[string]any{"Marker": "goby-feature-wave-browser-driver-v1", "RunId": runID, "Complete": false,
		"ArtifactDirectory": output, "OnlineProviderIntegrationsExercised": false, "CatalogFixture": "streamHTTPProber with current indexed source snapshots and real owned source files", "CredentialsWrittenToSummary": false}
	t.Cleanup(func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if err := refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver); err != nil {
			t.Error("preserve feature-wave driver summary")
		}
	})
	f := newServerFixtureWithTimeout(t, 8*time.Minute)
	root := t.TempDir()
	paths := []string{writeAPIMediaFile(t, root, "movies/Alpha.Feature.Wave.mp4"), writeAPIMediaFile(t, root, "movies/Beta.Feature.Wave.mp4")}
	baseline, err := featureWaveSourceFacts(paths)
	if err != nil {
		t.Fatal("capture feature-wave source baseline")
	}
	f.app.notifier.Close()
	f.app.catalogNotifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	// Original downloads require a current probe version and the source ctime.
	// The legacy catalog-only prober deliberately does not establish that proof.
	catalog, err := library.New(f.pool, streamHTTPProber{}, []string{root})
	if err != nil {
		t.Fatal("create feature-wave catalog")
	}
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	f.app.catalogNotifier = newLibraryNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() { f.app.notifier.Close(); f.app.catalogNotifier.Close() })
	administratorPassword, memberPassword := featureWavePassword(t), featureWavePassword(t)
	accountSuffix := runID
	if len(accountSuffix) > 64 {
		accountSuffix = accountSuffix[:64]
	}
	administrator, err := f.users.Bootstrap(f.ctx, "Feature wave administrator "+accountSuffix, administratorPassword)
	if err != nil {
		t.Fatal("bootstrap the private feature-wave administrator")
	}
	member, err := f.users.CreateUser(f.ctx, "Feature wave member "+accountSuffix, memberPassword, false)
	if err != nil {
		t.Fatal("create the private feature-wave member")
	}
	collection, err := catalog.CreateLibrary(f.ctx, "Feature wave movies "+runID, "movies", []string{filepath.Join(root, "movies")})
	if err != nil {
		t.Fatal("create the owned feature-wave media library")
	}
	job, err := catalog.StartScan(f.ctx, collection.ID)
	if err != nil {
		t.Fatal("start feature-wave catalog seed scan")
	}
	for {
		state, err := catalog.GetJob(f.ctx, job.ID)
		if err != nil {
			t.Fatal("read feature-wave seed scan")
		}
		if state.Status == "Completed" {
			if state.Error != "" || state.Scanned != 2 {
				t.Fatal("feature-wave seed scan did not catalog both files")
			}
			break
		}
		if state.Status != "Queued" && state.Status != "Running" {
			t.Fatal("feature-wave seed scan failed")
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("feature-wave seed deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	movieIDs := make([]string, 0, 2)
	for _, path := range paths {
		var id string
		if f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie' AND path=$2", collection.ID, path).Scan(&id) != nil {
			t.Fatal("read feature-wave media identity")
		}
		item, itemErr := catalog.GetItemFor(f.ctx, library.Subject{UserID: member.ID}, id)
		info, statErr := os.Stat(path)
		if itemErr != nil || statErr != nil || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion ||
			item.Media.FileChangeTimeNs <= 0 || item.Media.FileChangeTimeNs != media.FileChangeTime(info) || item.Media.Size != info.Size() {
			t.Fatal("feature-wave media lacks a current indexed version, ctime, and size matching its source file")
		}
		file, _, openErr := catalog.OpenDownloadFor(f.ctx, library.Subject{UserID: member.ID}, id, "")
		if openErr != nil {
			t.Fatal("feature-wave member cannot open its indexed original source before browser admission")
		}
		if file.Close() != nil {
			t.Fatal("close feature-wave original source fixture descriptor")
		}
		movieIDs = append(movieIDs, id)
	}
	driver["IndexedSourceProbeVersion"] = media.CurrentProbeVersion
	driver["SeedSourcesAuthorizedForDownload"] = true
	// Seed local cache rows only. No remote provider client is called by this
	// fixture or by the admitted global maintenance task.
	var imageBytes bytes.Buffer
	thumbnail := image.NewRGBA(image.Rect(0, 0, 1, 1))
	thumbnail.Set(0, 0, color.RGBA{R: 40, G: 80, B: 120, A: 255})
	if png.Encode(&imageBytes, thumbnail) != nil {
		t.Fatal("encode the owned cache fixture")
	}
	imageHash := sha256.Sum256(imageBytes.Bytes())
	for index, id := range movieIDs {
		fetched := time.Now().UTC()
		if index == 0 {
			fetched = fetched.Add(-10 * 24 * time.Hour)
		}
		_, err = f.pool.Exec(f.ctx, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash,fetched_at)
			VALUES($1,'Primary',0,'tmdb','feature-wave-local','feature-wave-local.png',$2,'image/png',1,1,$3,$4)`, id, imageBytes.Bytes(), hex.EncodeToString(imageHash[:]), fetched)
		if err != nil {
			t.Fatal("seed the two local cache entries")
		}
	}
	assets, err := adminassets.Files()
	if err != nil {
		t.Fatal("open the embedded feature-wave administrator bundle")
	}
	embeddedRows, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		t.Fatal("inventory embedded feature-wave assets")
	}
	bundlePath := filepath.Join(sourceRoot, "web", "admin", "dist")
	if resolved, err := filepath.EvalSymlinks(bundlePath); err != nil || resolved != bundlePath {
		t.Fatal("feature-wave source bundle must be canonical")
	}
	sourceRows, err := refreshBrowserAssetInventory(os.DirFS(bundlePath))
	if err != nil || !reflect.DeepEqual(embeddedRows, sourceRows) {
		t.Fatal("feature-wave embedded assets differ from the frozen source bundle")
	}
	if refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embeddedRows) != nil {
		t.Fatal("preserve feature-wave asset inventory")
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	WithDashboardAssets(assets)(f.app)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve the private feature-wave HTTP listener")
	}
	actual := httptest.NewUnstartedServer(nil)
	actual.Listener.Close()
	actual.Listener = listener
	origin := "http://" + listener.Addr().String()
	f.cfg.PublicURL, f.cfg.CookieSecure = origin, false
	f.app.cfg.PublicURL, f.app.cfg.CookieSecure = origin, false
	f.handler = f.app.Handler()
	actual.Config.Handler = f.handler
	actual.Start()
	t.Cleanup(actual.Close)
	fixture := featureWaveBrowserFixture{Marker: "goby-feature-wave-browser-fixture-v1", RunID: runID, BaseURL: origin,
		AdminID: administrator.ID, AdminName: administrator.Name, AdminPassword: administratorPassword, MemberID: member.ID, MemberName: member.Name, MemberPassword: memberPassword,
		LibraryID: collection.ID, MovieIDs: movieIDs, ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	manifestPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove private feature-wave credentials")
		}
	})
	if refreshBrowserWriteJSON(manifestPath, fixture) != nil {
		t.Fatal("save the private feature-wave browser context")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe remaining feature-wave sessions")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("revoke residual private feature-wave sessions")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
		}
		if err := f.app.taskManager.Close(ctx); err != nil {
			t.Error("close feature-wave task manager")
		}
	})
	seedDB, err := featureWaveDatabaseSnapshot(f.ctx, f, member.ID)
	if err != nil || refreshBrowserWriteJSON(filepath.Join(output, "stage-seeded-database.json"), map[string]any{"RunId": runID, "Database": seedDB, "Sources": baseline}) != nil {
		t.Fatal("preserve feature-wave seed facts")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create feature-wave runtime directory")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_SMOKE_BASE_URL=" + origin,
		"GOBY_FEATURE_WAVE_RUN_ID=" + runID, "GOBY_FEATURE_WAVE_BROWSER_CONTEXT=" + manifestPath}
	checkpointCtx, checkpointCancel := context.WithCancel(f.ctx)
	type checkpointResult struct {
		Count int
		Err   error
	}
	checkpointDone := make(chan checkpointResult, 1)
	go func() {
		count, err := featureWaveCheckpoints(checkpointCtx, f, fixture, paths, baseline)
		checkpointDone <- checkpointResult{count, err}
	}()
	ctx, cancel := context.WithTimeout(f.ctx, 6*time.Minute)
	command, commandErr := refreshBrowserCommand(ctx, node, []string{cli, "test", "e2e/feature-wave-live.spec.ts", "--project=chromium", "--grep", "(?:^| )" + regexp.QuoteMeta(featureWaveBrowserTitle) + "$", "--workers=1", "--retries=0", "--output", filepath.Join(output, "playwright")}, environment, work, output)
	cancel()
	checkpointCancel()
	checkpoint := <-checkpointDone
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, checkpoint.Count
	var result featureWaveBrowserResult
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || readErr != nil || checkpoint.Err != nil || checkpoint.Count != len(featureWaveBrowserPhases) {
		t.Fatal("feature-wave browser or stage observer failed; inspect private artifacts")
	}
	checks := []string{"PlaylistLifecycle", "CollectionLifecycle", "PolicyRestrictions", "ManagementPersistence", "GlobalCacheRun", "NoPageErrors", "NoOnlineProviderRequests", "LogoutCompleted"}
	if result.Marker != "goby-feature-wave-browser-result-v1" || result.RunID != runID || !result.Complete || len(result.Checks) != len(checks) || len(result.Stages) != len(featureWaveBrowserPhases) {
		t.Fatal("feature-wave browser result does not bind this complete scenario")
	}
	for _, check := range checks {
		if !result.Checks[check] {
			t.Fatal("feature-wave browser check did not complete")
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(result.PlaylistID) || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(result.CollectionID) || result.PlaylistID == result.CollectionID {
		t.Fatal("feature-wave result lacks distinct created collection identities")
	}
	var virtual, entries, shares, mediaCount, expired, fresh, active, totalRuns, activeRuns, activeTriggers int
	err = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM media_collections),(SELECT count(*) FROM media_collection_entries),(SELECT count(*) FROM media_collection_shares),
		(SELECT count(*) FROM items WHERE type='Movie' AND id=ANY($1::text[])),
		(SELECT count(*) FROM item_provider_images WHERE item_id=$2),(SELECT count(*) FROM item_provider_images WHERE item_id=$3),
		(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM task_runs),
		(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),(SELECT count(*) FROM task_triggers WHERE retired_at IS NULL)`, movieIDs, movieIDs[0], movieIDs[1]).Scan(&virtual, &entries, &shares, &mediaCount, &expired, &fresh, &active, &totalRuns, &activeRuns, &activeTriggers)
	if err != nil || virtual != 0 || entries != 0 || shares != 0 || mediaCount != 2 || expired != 0 || fresh != 1 || active != 0 || totalRuns != 1 || activeRuns != 0 || activeTriggers != 0 {
		t.Fatal("feature-wave final database invariants differ from the browser evidence")
	}
	managed, err := f.users.GetManagedUser(f.ctx, member.ID)
	if err != nil || managed.Policy.AutoRemoteQuality != 12000000 || managed.Policy.RemoteClientBitrateLimit != 6000000 || !reflect.DeepEqual(managed.Policy.RestrictedFeatures, []string{identity.FeatureDownloads}) {
		t.Fatal("feature-wave policy did not persist in the database")
	}
	expectedManagement := settings.Management{Metadata: settings.MetadataOptions{EnableInternetProviders: false, PreferredMetadataLanguage: "fr", MetadataCountryCode: "FR"},
		Subtitles: settings.SubtitleOptions{DownloadLanguages: []string{}, DownloadMovieSubtitles: false, DownloadEpisodeSubtitles: false},
		Tasks:     settings.TaskOptions{MaxConcurrent: 3, CacheRetentionDays: 1, CacheMaxEntries: 1}}
	var managementJSON []byte
	if f.pool.QueryRow(f.ctx, "SELECT management FROM managed_settings WHERE id=1").Scan(&managementJSON) != nil {
		t.Fatal("read persisted feature-wave management settings")
	}
	var persisted settings.Management
	if json.Unmarshal(managementJSON, &persisted) != nil || !reflect.DeepEqual(persisted, expectedManagement) || !reflect.DeepEqual(f.app.settings.Snapshot().Management, expectedManagement) {
		t.Fatal("feature-wave settings database and live snapshot do not agree")
	}
	run, err := f.app.taskStore.GetRun(f.ctx, result.CacheRunID)
	if err != nil || run.TaskKey != tasks.CacheMaintainKey || run.State != tasks.RunCompleted || run.Source != "manual" || run.TotalChildren != 1 || run.TerminalChildren != 1 || run.CompletedChildren != 1 || run.Scanned != 1 || run.Updated != 1 {
		t.Fatal("feature-wave cache run was not a real completed global executor run")
	}
	var globalChildren, receipts int
	if f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_run_children WHERE run_id=$1 AND library_id='' AND scan_job_id IS NULL AND state='completed' AND scanned=1 AND updated=1", run.ID).Scan(&globalChildren) != nil || globalChildren != 1 {
		t.Fatal("feature-wave cache execution lacks its completed global child")
	}
	if f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_run_requests WHERE run_id=$1", run.ID).Scan(&receipts) != nil || receipts != 1 {
		t.Fatal("feature-wave cache start lacks a unique durable request receipt")
	}
	finalSources, err := featureWaveSourceFacts(paths)
	if err != nil || !reflect.DeepEqual(finalSources, baseline) {
		t.Fatal("feature-wave workflow changed original media bytes")
	}
	driver["Complete"], driver["SourcesUnchanged"], driver["OwnedSessionsLoggedOut"] = true, true, true
	driver["BrowserChecks"], driver["CacheRunId"], driver["CacheEntriesRemoved"], driver["CacheEntriesRetained"] = result.Checks, run.ID, expired+1, fresh
	t.Log("feature_wave_browser_verified=true stages=7 source_files=2 global_cache_runs=1 online_providers_exercised=false")
}
