//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
	adminassets "github.com/moooyo/goby/web/admin"
)

const phase3BrowserTitle = "phase 3 administration workflows persist through conflicts and restart"

var phase3BrowserPhases = []string{
	"authentication", "library-conflict", "library", "preferences", "artwork", "music",
	"schedule", "restart", "persistence", "permission-revoked", "permission-denied", "cleanup",
}

// Only this private, temporary context contains browser credentials. The child
// environment receives the context pathname, never passwords or session tokens.
type phase3BrowserContext struct {
	Marker                string
	RunID                 string `json:"RunId"`
	BaseURL               string
	AdminID               string `json:"AdminId"`
	AdminName             string
	AdminPassword         string
	UserID                string `json:"UserId"`
	UserName              string
	LibraryID             string `json:"LibraryId"`
	LibraryName           string
	LibraryPath           string
	AudioItemID           string `json:"AudioItemId"`
	AudioItemName         string
	ArtworkEntityID       string `json:"ArtworkEntityId"`
	ArtworkEntityName     string
	CacheTaskID           string `json:"CacheTaskId"`
	CacheTaskName         string
	WritableDirectory     string
	ImageUploadPath       string
	ImageUploadPathSecond string
	ArtifactsDir          string
	ResultPath            string
}

type phase3BrowserResult struct {
	Marker          string
	RunID           string `json:"RunId"`
	Complete        bool
	StartupRunID    string `json:"StartupRunId"`
	Checks          map[string]bool
	PageErrors      *int
	ForeignRequests *int
	Stages          []struct{ Phase, State string }
}

// The fixture supplies catalog facts only. It does not claim to decode these
// synthetic bytes or to validate local music-tag extraction or media playback.
type phase3BrowserProber struct{}

func (phase3BrowserProber) CacheVersion() int { return media.CurrentProbeVersion }

func (phase3BrowserProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (streamHTTPProber{}).ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	info.EmbeddedMusic = &media.MusicMetadata{
		Version: media.CurrentMusicMetadataVersion, Title: "Phase 3 Original Track", Album: "Original Album",
		Artists: []string{"Original Artist"}, AlbumArtists: []string{"Original Album Artist"},
		Composers: []string{"Original Composer"}, Genres: []string{"Phase 3 Genre"}, TrackNumber: 1, DiscNumber: 1,
	}
	return info, nil
}

func phase3BrowserSourceFacts(paths []string) ([]map[string]any, error) {
	facts := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		fact, err := refreshBrowserMediaFacts(path)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

// This projection deliberately excludes password hashes, session identifiers,
// tokens, provider credentials, filesystem contents, and image payloads.
func phase3BrowserDatabaseSnapshot(ctx context.Context, f *serverFixture, fixture phase3BrowserContext) (json.RawMessage, error) {
	entityID, err := strconv.ParseInt(fixture.ArtworkEntityID, 10, 64)
	if err != nil || entityID <= 0 {
		return nil, errors.New("phase 3 artwork entity identity is invalid")
	}
	var snapshot string
	err = f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Schema', current_schema(), 'Durable', jsonb_build_object(
		'Library', (SELECT jsonb_build_object('Id',id,'Name',name,'CollectionType',collection_type,'Revision',revision::text,'Options',options) FROM libraries WHERE id=$1),
		'Roots', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Path',path,'BindingRevision',binding_revision::text) ORDER BY path),'[]'::jsonb) FROM library_roots WHERE library_id=$1),
		'Preferences', (SELECT jsonb_build_object('UserId',id,'Revision',configuration_revision::text,'Configuration',configuration) FROM users WHERE id=$2),
		'Metadata', (SELECT jsonb_build_object('ItemId',item_id,'Revision',revision::text,'Automatic',automatic,'Overrides',overrides,'LockedValues',locked_values,'Effective',effective) FROM item_metadata_state WHERE item_id=$3),
		'Entities', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',e.id::text,'Type',e.kind,'Name',e.name,'CreditType',a.credit_type) ORDER BY e.id,a.position),'[]'::jsonb) FROM catalog_entities e JOIN item_entities a ON a.entity_id=e.id WHERE a.item_id=$3),
		'Artwork', (SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',s.item_id,'EntityId',s.entity_id::text,'UserId',s.user_id,
			'Revision',s.revision::text,'ManagedTypes',s.managed_types,'Images',
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('Type',i.image_type,'Index',i.image_index,'Bytes',octet_length(i.content),
				'MimeType',i.mime_type,'Width',i.width,'Height',i.height,'SHA256',i.source_hash) ORDER BY i.image_type,i.image_index),'[]'::jsonb)
			FROM artwork_images i WHERE i.state_id=s.id)) ORDER BY s.id),'[]'::jsonb)
			FROM artwork_state s WHERE s.item_id=$3 OR s.entity_id=$5 OR s.user_id=$2),
		'Triggers', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Kind',kind,'SystemEvent',system_event,'Revision',schedule_revision::text,'Retired',retired_at IS NOT NULL) ORDER BY id),'[]'::jsonb) FROM task_triggers WHERE task_id=$4)),
		'Runs', (SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Source',source,'State',state,'CompletedChildren',completed_children) ORDER BY id),'[]'::jsonb) FROM task_runs WHERE task_id=$4),
		'ActiveSessions', (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'ActiveTasks', (SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
		'PlaybackSessions', (SELECT count(*) FROM play_sessions),
		'EncodingJobs', (SELECT count(*) FROM encoding_jobs)
	)::text`, fixture.LibraryID, fixture.UserID, fixture.AudioItemID, fixture.CacheTaskID, entityID).Scan(&snapshot)
	return json.RawMessage(snapshot), err
}

// Restarts construct a complete new Server after the previous generation has
// retired. No notifier, event hub, manager, or catalog is copied between them.
type phase3BrowserRuntime struct {
	f      *serverFixture
	assets fs.FS
	server *httptest.Server
	addr   string
}

func (runtime *phase3BrowserRuntime) listen() error {
	listener, err := net.Listen("tcp4", runtime.addr)
	if err != nil {
		return err
	}
	runtime.addr = listener.Addr().String()
	origin := "http://" + runtime.addr
	runtime.f.cfg.PublicURL, runtime.f.cfg.CookieSecure = origin, false
	runtime.f.app.cfg.PublicURL, runtime.f.app.cfg.CookieSecure = origin, false
	WithDashboardAssets(runtime.assets)(runtime.f.app)
	runtime.f.handler = runtime.f.app.Handler()
	actual := httptest.NewUnstartedServer(runtime.f.handler)
	actual.Listener.Close()
	actual.Listener = listener
	actual.Start()
	runtime.server = actual
	return nil
}

func (runtime *phase3BrowserRuntime) close(ctx context.Context) error {
	if runtime.server != nil {
		runtime.server.CloseClientConnections()
	}
	err := runtime.f.app.Close(ctx)
	if runtime.server != nil {
		runtime.server.Close()
		runtime.server = nil
	}
	return err
}

func (runtime *phase3BrowserRuntime) restart(ctx context.Context) error {
	if err := runtime.close(ctx); err != nil {
		return err
	}
	app, err := New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log,
		"phase3-browser-integration", WithDashboardAssets(runtime.assets))
	if err != nil {
		return err
	}
	runtime.f.app = app
	return runtime.listen()
}

func phase3BrowserAdminRole(ctx context.Context, f *serverFixture, actor identity.Principal, id string, enabled bool) error {
	current, err := f.users.GetManagedUser(ctx, id)
	if err != nil {
		return err
	}
	update := managedUpdateFrom(current)
	update.IsAdministrator = enabled
	_, err = f.users.UpdateManagedUser(ctx, actor, id, update)
	return err
}

func phase3BrowserWaitRequest(ctx context.Context, fixture phase3BrowserContext, phase string) error {
	path := filepath.Join(fixture.ArtifactsDir, "stage-"+phase+"-request.json")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var request struct {
			RunID string `json:"RunId"`
			Phase string
		}
		err := featureWavePrivateJSON(path, 4096, &request)
		if !errors.Is(err, os.ErrNotExist) {
			if err != nil || request.RunID != fixture.RunID || request.Phase != phase {
				return errors.New("phase 3 request does not bind the expected stage")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type phase3BrowserObserver struct {
	runtime         *phase3BrowserRuntime
	fixture         phase3BrowserContext
	actor           identity.Principal
	actorToken      string
	paths           []string
	sources         []map[string]any
	durable         json.RawMessage
	startupSequence int64
	startupRunID    string
	completed       int
}

func (observer *phase3BrowserObserver) verifyLibrary(ctx context.Context) error {
	editing, err := observer.runtime.f.app.library.GetLibraryEditing(ctx, observer.fixture.LibraryID)
	if err != nil {
		return err
	}
	options := library.EffectiveLibraryOptions(editing.Library)
	paths := editing.Library.Paths
	if editing.Library.Name != "Phase 3 Music Edited" || options.EnableLocalMetadata || options.EnableLocalImages || len(paths) != 2 ||
		!((paths[0] == observer.fixture.LibraryPath && paths[1] == observer.fixture.WritableDirectory) ||
			(paths[1] == observer.fixture.LibraryPath && paths[0] == observer.fixture.WritableDirectory)) {
		return errors.New("phase 3 library edit did not persist its exact name, roots, and options")
	}
	return nil
}

func (observer *phase3BrowserObserver) verifyPreferences(ctx context.Context) error {
	var saved bool
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT configuration_revision > 1
		AND configuration->>'AudioLanguagePreference'='eng'
		AND configuration->>'SubtitleLanguagePreference'='zho'
		AND configuration->>'SubtitleMode'='Always'
		AND configuration->>'ResumeRewindSeconds'='10'
		FROM users WHERE id=$1`, observer.fixture.UserID).Scan(&saved)
	if err != nil || !saved {
		return errors.New("phase 3 playback preferences did not persist")
	}
	return nil
}

func (observer *phase3BrowserObserver) verifyMusic(ctx context.Context) error {
	detail, err := observer.runtime.f.app.library.GetItemMetadata(ctx, observer.actor, observer.fixture.AudioItemID)
	if err != nil {
		return err
	}
	values := detail.Effective
	composer := false
	for _, person := range values.People {
		composer = composer || person.Name == "Phase 3 Composer" && person.Type == "Composer"
	}
	if values.Album != "Phase 3 Album" || !reflect.DeepEqual(values.Artists, []string{"Phase 3 Artist", "Guest Artist"}) ||
		!reflect.DeepEqual(values.AlbumArtists, []string{"Phase 3 Album Artist"}) || values.IndexNumber == nil || *values.IndexNumber != 7 ||
		values.ParentIndexNumber == nil || *values.ParentIndexNumber != 2 || !composer {
		return errors.New("phase 3 music metadata did not persist its complete typed values")
	}
	return nil
}

func (observer *phase3BrowserObserver) verifyArtwork(ctx context.Context, includeItem bool) error {
	entityID, err := strconv.ParseInt(observer.fixture.ArtworkEntityID, 10, 64)
	if err != nil || entityID <= 0 {
		return errors.New("phase 3 artwork entity identity is invalid")
	}
	var entityPrimary, entityBackdrop, avatar, itemPrimary, invalid int
	err = observer.runtime.f.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE s.entity_id=$1 AND i.image_type='Primary' AND i.image_index=0),
		count(*) FILTER (WHERE s.entity_id=$1 AND i.image_type='Backdrop'),
		count(*) FILTER (WHERE s.user_id=$2 AND i.image_type='Primary' AND i.image_index=0),
		count(*) FILTER (WHERE s.item_id=$3 AND i.image_type='Primary' AND i.image_index=0),
		count(*) FILTER (WHERE s.revision<1 OR i.mime_type<>'image/png' OR i.width<>16 OR i.height<>16
			OR octet_length(i.content)=0 OR i.source_hash NOT IN ($4,$5))
		FROM artwork_state s JOIN artwork_images i ON i.state_id=s.id
		WHERE s.entity_id=$1 OR s.user_id=$2 OR s.item_id=$3`, entityID,
		observer.fixture.UserID, observer.fixture.AudioItemID, observer.sources[1]["SHA256"], observer.sources[2]["SHA256"]).
		Scan(&entityPrimary, &entityBackdrop, &avatar, &itemPrimary, &invalid)
	if err != nil || entityPrimary != 1 || entityBackdrop != 0 || avatar != 1 || invalid != 0 || includeItem && itemPrimary != 1 {
		return errors.New("phase 3 artwork did not persist its owned PNG images and cleared backdrop list")
	}
	return nil
}

func (observer *phase3BrowserObserver) verifySchedule(ctx context.Context) error {
	definition, err := observer.runtime.f.app.taskStore.Get(ctx, observer.fixture.CacheTaskID)
	if err != nil {
		return err
	}
	if len(definition.Triggers) != 1 || definition.Triggers[0].Kind != "system_event" ||
		definition.Triggers[0].SystemEvent == nil || *definition.Triggers[0].SystemEvent != "ServerStarted" {
		return errors.New("phase 3 startup schedule did not persist its typed trigger")
	}
	return nil
}

func (observer *phase3BrowserObserver) waitStartup(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var sequence int64
		var runs, completed, receipts int
		var runID string
		err := observer.runtime.f.pool.QueryRow(ctx, `SELECT
			(SELECT sequence FROM task_system_events WHERE name='ServerStarted'),
			(SELECT count(*) FROM task_runs WHERE task_id=$1),
			(SELECT count(*) FROM task_runs WHERE task_id=$1 AND source='system_event' AND state='completed' AND total_children=1 AND completed_children=1),
			(SELECT count(*) FROM task_system_event_receipts WHERE task_id=$1 AND system_event='ServerStarted' AND disposition='admitted' AND first_sequence=$2 AND last_sequence=$2),
			COALESCE((SELECT id FROM task_runs WHERE task_id=$1 ORDER BY created_at LIMIT 1),'')`,
			observer.fixture.CacheTaskID, observer.startupSequence+1).Scan(&sequence, &runs, &completed, &receipts, &runID)
		if err != nil {
			return err
		}
		if sequence < observer.startupSequence || sequence > observer.startupSequence+1 || runs > 1 || receipts > 1 {
			return errors.New("phase 3 restart duplicated its startup signal or task admission")
		}
		if sequence == observer.startupSequence+1 && runs == 1 && completed == 1 && receipts == 1 {
			observer.startupRunID = runID
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 3 startup task did not complete within its deadline")
		case <-ticker.C:
		}
	}
}

func (observer *phase3BrowserObserver) stage(ctx context.Context, phase string) error {
	f, fixture := observer.runtime.f, observer.fixture
	switch phase {
	case "library-conflict":
		current, err := f.app.library.GetLibraryEditing(ctx, fixture.LibraryID)
		if err != nil {
			return err
		}
		name := "Concurrent library edit"
		_, err = f.app.library.UpdateLibraryAsAdministrator(ctx, observer.actor, identity.AdministratorNative,
			fixture.LibraryID, library.LibraryUpdate{Revision: current.Library.Revision, Name: &name})
		if err != nil {
			return err
		}
	case "library":
		if err := observer.verifyLibrary(ctx); err != nil {
			return err
		}
	case "preferences":
		if err := observer.verifyPreferences(ctx); err != nil {
			return err
		}
	case "artwork":
		if err := observer.verifyArtwork(ctx, false); err != nil {
			return err
		}
	case "music":
		if err := observer.verifyMusic(ctx); err != nil {
			return err
		}
		if err := observer.verifyArtwork(ctx, true); err != nil {
			return err
		}
	case "schedule":
		if err := observer.verifySchedule(ctx); err != nil {
			return err
		}
		var runs int
		if err := f.pool.QueryRow(ctx, `SELECT (SELECT sequence FROM task_system_events WHERE name='ServerStarted'),
			(SELECT count(*) FROM task_runs WHERE task_id=$1)`, fixture.CacheTaskID).Scan(&observer.startupSequence, &runs); err != nil || runs != 0 {
			return errors.New("phase 3 startup trigger ran before a new server lifecycle")
		}
	case "restart":
		if len(observer.durable) == 0 {
			return errors.New("phase 3 restart has no preceding persisted state")
		}
		if err := observer.runtime.restart(ctx); err != nil {
			return err
		}
		if err := observer.waitStartup(ctx); err != nil {
			return err
		}
	case "persistence":
		if err := observer.verifyLibrary(ctx); err != nil {
			return err
		}
		if err := observer.verifyPreferences(ctx); err != nil {
			return err
		}
		if err := observer.verifyMusic(ctx); err != nil {
			return err
		}
		if err := observer.verifyArtwork(ctx, true); err != nil {
			return err
		}
		if err := observer.verifySchedule(ctx); err != nil {
			return err
		}
		if err := observer.waitStartup(ctx); err != nil {
			return err
		}
	case "permission-revoked":
		if err := phase3BrowserAdminRole(ctx, f, observer.actor, fixture.AdminID, false); err != nil {
			return err
		}
	case "permission-denied":
		if err := phase3BrowserAdminRole(ctx, f, observer.actor, fixture.AdminID, true); err != nil {
			return err
		}
	case "cleanup":
		if err := observer.waitStartup(ctx); err != nil {
			return err
		}
		if err := f.users.Revoke(ctx, observer.actorToken); err != nil {
			return err
		}
		var active, tasks, playback, encodings, scans int
		err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
			(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
			(SELECT count(*) FROM play_sessions),(SELECT count(*) FROM encoding_jobs),
			(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running'))`).Scan(&active, &tasks, &playback, &encodings, &scans)
		if err != nil || active != 0 || tasks != 0 || playback != 0 || encodings != 0 || scans != 0 {
			return errors.New("phase 3 browser did not retire its sessions and work")
		}
	}
	sources, err := phase3BrowserSourceFacts(observer.paths)
	if err != nil || !reflect.DeepEqual(sources, observer.sources) {
		return errors.New("phase 3 administration changed owned source files")
	}
	database, err := phase3BrowserDatabaseSnapshot(ctx, f, fixture)
	if err != nil {
		return err
	}
	var snapshot struct{ Durable json.RawMessage }
	if json.Unmarshal(database, &snapshot) != nil || len(snapshot.Durable) == 0 {
		return errors.New("phase 3 database observation lacks durable state")
	}
	if phase == "schedule" {
		observer.durable = append(json.RawMessage(nil), snapshot.Durable...)
	}
	if len(observer.durable) != 0 && !bytes.Equal(observer.durable, snapshot.Durable) {
		return errors.New("phase 3 restart or rejected write changed persisted administrator state")
	}
	return featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "stage-"+phase+"-database.json"), map[string]any{
		"Marker": "goby-phase3-stage-database-v1", "RunId": fixture.RunID, "Phase": phase, "Complete": true,
		"SourcesUnchanged": true, "Sources": sources, "Database": database,
		"BaseURL": "http://" + observer.runtime.addr, "StartupRunId": observer.startupRunID,
	})
}

func (observer *phase3BrowserObserver) run(ctx context.Context) error {
	for _, phase := range phase3BrowserPhases {
		if err := phase3BrowserWaitRequest(ctx, observer.fixture, phase); err != nil {
			return err
		}
		if err := observer.stage(ctx, phase); err != nil {
			return err
		}
		observer.completed++
	}
	return nil
}

func TestPhase3AdminBrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("phase 3 browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	cli := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_CLI", false)
	work := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_WORK", true)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("phase 3 artifact parent must be private")
	}
	runID := os.Getenv("GOBY_PHASE3_BROWSER_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("an explicit phase 3 browser run ID is required")
	}
	for _, name := range []string{"playwright.config.ts", "e2e/phase3-admin-workflows.spec.ts"} {
		info, err := os.Lstat(filepath.Join(work, name))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("the owned phase 3 Playwright work copy is incomplete")
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read phase 3 source directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate phase 3 source root")
	}
	relative, err := filepath.Rel(sourceRoot, work)
	if err != nil || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("Playwright work must be outside the frozen Go source")
	}
	output, err := os.MkdirTemp(artifacts, "phase3-browser-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create a private phase 3 artifact directory")
	}
	driver := map[string]any{"Marker": "goby-phase3-browser-driver-v1", "RunId": runID, "Complete": false,
		"ArtifactDirectory": output, "CredentialsWrittenToSummary": false, "OnlineProviderIntegrationsExercised": false,
		"CatalogFixture": "synthetic music probe facts with real owned files, directories, and PNG artwork", "MediaPlaybackExercised": false}
	var schema, mediaRoot string
	// Registered before the shared fixture, this observation runs after its
	// owned-schema and temporary-directory cleanup, using a fresh bounded pool.
	t.Cleanup(func() {
		if schema != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe phase 3 schema cleanup")
			} else {
				defer pool.Close()
				var remaining bool
				if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remaining); err != nil || remaining {
					t.Error("phase 3 owned schema was not removed")
				} else {
					driver["OwnedSchemaRemoved"] = true
				}
			}
		}
		if mediaRoot != "" {
			if _, err := os.Lstat(mediaRoot); !errors.Is(err, os.ErrNotExist) {
				t.Error("phase 3 owned media root was not removed")
			} else {
				driver["OwnedMediaRootRemoved"] = true
			}
		}
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve the phase 3 driver result")
		}
	})
	f := newServerFixtureWithTimeout(t, 10*time.Minute)
	if f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema) != nil {
		t.Fatal("identify the owned phase 3 schema")
	}
	mediaRoot = t.TempDir()
	audioPath := writeAPIMediaFile(t, mediaRoot, "music/Original Artist/Original Album/01 Original Track.wav")
	writable := filepath.Join(mediaRoot, "music-extra")
	if os.Mkdir(writable, 0o700) != nil {
		t.Fatal("create the additional owned library directory")
	}
	var encodedImage bytes.Buffer
	artwork := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			artwork.Set(x, y, color.RGBA{R: uint8(20 + x*10), G: uint8(40 + y*10), B: 120, A: 255})
		}
	}
	imagePath := filepath.Join(output, "artwork-upload.png")
	if png.Encode(&encodedImage, artwork) != nil || os.WriteFile(imagePath, encodedImage.Bytes(), 0o600) != nil {
		t.Fatal("create the original phase 3 PNG artwork")
	}
	artwork.Set(0, 0, color.RGBA{R: 240, G: 180, B: 30, A: 255})
	encodedImage.Reset()
	secondImagePath := filepath.Join(output, "artwork-upload-second.png")
	if png.Encode(&encodedImage, artwork) != nil || os.WriteFile(secondImagePath, encodedImage.Bytes(), 0o600) != nil {
		t.Fatal("create the distinct phase 3 backdrop artwork")
	}
	paths := []string{audioPath, imagePath, secondImagePath}
	sources, err := phase3BrowserSourceFacts(paths)
	if err != nil {
		t.Fatal("capture phase 3 source identity and content")
	}
	f.app.notifier.Close()
	f.app.catalogNotifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	f.cfg.MediaRoots, f.app.cfg.MediaRoots = []string{mediaRoot}, []string{mediaRoot}
	catalog, err := library.New(f.pool, phase3BrowserProber{}, []string{mediaRoot})
	if err != nil {
		t.Fatal("create the phase 3 catalog")
	}
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	f.app.catalogNotifier = newLibraryNotifier(catalog, f.app.eventHub)
	password := featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Phase 3 administrator", password)
	if err != nil {
		t.Fatal("bootstrap the private phase 3 administrator")
	}
	member, err := f.users.CreateUser(f.ctx, "Phase 3 listener", featureWavePassword(t), false)
	if err != nil {
		t.Fatal("create the phase 3 preference owner")
	}
	actorPassword := featureWavePassword(t)
	actorUser, err := f.users.CreateUser(f.ctx, "Phase 3 independent administrator", actorPassword, true)
	if err != nil {
		t.Fatal("create the independent phase 3 actor")
	}
	credentials, err := f.users.Authenticate(f.ctx, actorUser.Name, actorPassword,
		identity.Client{Name: "Phase 3 fixture", DeviceID: "phase3-fixture-actor", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		t.Fatal("authenticate the independent phase 3 actor")
	}
	actor, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		t.Fatal("resolve the independent phase 3 actor")
	}
	libraryPath := filepath.Join(mediaRoot, "music")
	collection, err := catalog.CreateLibrary(f.ctx, "Phase 3 Music", "music", []string{libraryPath})
	if err != nil {
		t.Fatal("create the owned phase 3 music library")
	}
	job, err := catalog.StartScan(f.ctx, collection.ID)
	if err != nil {
		t.Fatal("start the phase 3 catalog seed")
	}
	refreshBrowserWaitJob(t, f, job.ID)
	var audioID, audioName string
	if f.pool.QueryRow(f.ctx, "SELECT id,name FROM items WHERE library_id=$1 AND path=$2 AND type='Audio'", collection.ID, audioPath).Scan(&audioID, &audioName) != nil {
		t.Fatal("read the scanned phase 3 audio identity")
	}
	entity, err := catalog.GetEntity(f.ctx, admin.ID, "Genre", "Phase 3 Genre")
	if err != nil || entity.ID <= 0 {
		t.Fatal("read the automatically indexed artwork entity")
	}
	cacheTask, err := f.app.taskStore.GetByKey(f.ctx, tasks.CacheMaintainKey)
	if err != nil {
		t.Fatal("read the real cache-maintenance task")
	}
	assets, err := adminassets.Files()
	if err != nil {
		t.Fatal("open the embedded phase 3 administrator bundle")
	}
	embeddedRows, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		t.Fatal("inventory the embedded phase 3 assets")
	}
	bundlePath := filepath.Join(sourceRoot, "web", "admin", "dist")
	if resolved, err := filepath.EvalSymlinks(bundlePath); err != nil || resolved != bundlePath {
		t.Fatal("the phase 3 source bundle must be canonical")
	}
	sourceRows, err := refreshBrowserAssetInventory(os.DirFS(bundlePath))
	if err != nil || !reflect.DeepEqual(embeddedRows, sourceRows) || refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embeddedRows) != nil {
		t.Fatal("the embedded phase 3 assets differ from the frozen source bundle")
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	runtime := &phase3BrowserRuntime{f: f, assets: assets, addr: "127.0.0.1:0"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe residual phase 3 sessions")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("revoke residual phase 3 sessions")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
		}
		if err := runtime.close(ctx); err != nil {
			t.Error("close the phase 3 server generation")
		} else {
			driver["ServerWorkersClosed"] = true
			driver["HTTPListenerClosed"] = true
		}
	})
	if err := runtime.listen(); err != nil {
		t.Fatal("start the owned phase 3 TCP4 HTTP listener")
	}
	fixture := phase3BrowserContext{Marker: "goby-phase3-browser-fixture-v1", RunID: runID,
		BaseURL: "http://" + runtime.addr, AdminID: admin.ID, AdminName: admin.Name, AdminPassword: password,
		UserID: member.ID, UserName: member.Name, LibraryID: collection.ID, LibraryName: collection.Name,
		LibraryPath: libraryPath, AudioItemID: audioID, AudioItemName: audioName,
		ArtworkEntityID: strconv.FormatInt(entity.ID, 10), ArtworkEntityName: entity.Name, CacheTaskID: cacheTask.ID, CacheTaskName: cacheTask.Name,
		WritableDirectory: writable, ImageUploadPath: imagePath, ImageUploadPathSecond: secondImagePath,
		ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	manifestPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove the private phase 3 credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if refreshBrowserWriteJSON(manifestPath, fixture) != nil {
		t.Fatal("save the private phase 3 browser context")
	}
	seed, err := phase3BrowserDatabaseSnapshot(f.ctx, f, fixture)
	if err != nil || refreshBrowserWriteJSON(filepath.Join(output, "stage-seeded-database.json"), map[string]any{"RunId": runID, "Database": seed, "Sources": sources}) != nil {
		t.Fatal("preserve the phase 3 seed facts")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create the private phase 3 browser runtime directories")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_SMOKE_BASE_URL=" + fixture.BaseURL,
		"GOBY_PHASE3_BROWSER_RUN_ID=" + runID, "GOBY_PHASE3_BROWSER_CONTEXT=" + manifestPath}
	observer := &phase3BrowserObserver{runtime: runtime, fixture: fixture, actor: actor, actorToken: credentials.Token, paths: paths, sources: sources}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Minute)
	defer cancel()
	observerCtx, stopObserver := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{cli, "test", "e2e/phase3-admin-workflows.spec.ts", "--project=chromium",
		"--grep", "(?:^| )" + regexp.QuoteMeta(phase3BrowserTitle) + "$", "--workers=1", "--retries=0", "--output", filepath.Join(output, "playwright")}, environment, work, output)
	stopObserver()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	if observerErr != nil && observer.completed < len(phase3BrowserPhases) {
		driver["FailedDatabaseStage"] = phase3BrowserPhases[observer.completed]
	}
	var result phase3BrowserResult
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(phase3BrowserPhases) {
		t.Fatal("phase 3 browser or database observer failed; inspect the private artifacts")
	}
	checks := []string{"LibrarySaved", "LibraryConflict", "PreferencesSaved", "ArtworkSaved", "MusicSaved",
		"TriggersSaved", "RestartPersisted", "PermissionRevocation", "Completed"}
	if result.Marker != "goby-phase3-browser-result-v1" || result.RunID != runID || !result.Complete ||
		result.StartupRunID == "" || result.StartupRunID != observer.startupRunID || len(result.Checks) != len(checks) || len(result.Stages) != len(phase3BrowserPhases) ||
		result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("the phase 3 browser result does not bind this complete scenario")
	}
	for index, phase := range phase3BrowserPhases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" {
			t.Fatal("the phase 3 browser stages do not match the observed database stages")
		}
	}
	for _, check := range checks {
		if !result.Checks[check] {
			t.Fatal("a phase 3 browser assertion did not complete")
		}
	}
	driver["Complete"], driver["SourcesUnchanged"], driver["OwnedSessionsRetired"] = true, true, true
	driver["BrowserChecks"], driver["StartupRunId"], driver["ServerRestarts"] = result.Checks, observer.startupRunID, 1
	t.Log("phase3_browser_verified=true stages=12 server_restarts=1 startup_runs=1 playback_exercised=false")
}
