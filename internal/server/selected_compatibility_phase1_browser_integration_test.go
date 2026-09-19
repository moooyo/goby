//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	adminassets "github.com/moooyo/goby/web/admin"
)

const selectedPhase1OriginalWebDirectory = "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/dashboard-ui"

var selectedPhase1BrowserPhases = []string{
	"admin-credentials", "admin-preferences", "admin-intro", "original-local-login", "profile-pin",
	"next-enabled", "next-disabled", "intro-show-button", "intro-none", "intro-auto-skip",
	"restart", "persisted", "credentials-cleared", "cleanup",
}

// Credentials exist only in this private temporary context. The browser child
// receives its pathname, never a database URL, password, or session token.
type selectedPhase1BrowserContext struct {
	Marker           string
	RunID            string `json:"RunId"`
	BaseURL          string
	ServerID         string `json:"ServerId"`
	AdminID          string `json:"AdminId"`
	AdminName        string
	AdminPassword    string
	UserID           string `json:"UserId"`
	UserName         string
	UserPassword     string
	LocalPassword    string
	ProfilePin       string
	LibraryID        string `json:"LibraryId"`
	LibraryName      string
	MovieLibraryID   string `json:"MovieLibraryId"`
	MovieLibraryName string
	MovieID          string `json:"MovieId"`
	MovieName        string
	EpisodeOneID     string `json:"EpisodeOneId"`
	EpisodeOneName   string
	EpisodeTwoID     string `json:"EpisodeTwoId"`
	EpisodeTwoName   string
	SeriesID         string `json:"SeriesId"`
	ArtifactsDir     string
	ResultPath       string
}

type selectedPhase1BrowserResult struct {
	Marker          string
	RunID           string `json:"RunId"`
	Complete        bool
	Checks          map[string]bool
	PageErrors      *int
	ForeignRequests *int
	Stages          []struct{ Phase, State string }
}

func selectedPhase1SourceFacts(paths []string) ([]map[string]any, error) {
	facts := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 2<<20 {
			return nil, errors.New("selected phase 1 source is not a bounded regular file")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Nlink != 1 {
			return nil, errors.New("selected phase 1 source ownership or link identity changed")
		}
		raw, err := os.ReadFile(path)
		if err != nil || int64(len(raw)) != info.Size() {
			return nil, errors.New("selected phase 1 source could not be read within its bound")
		}
		hash := sha256.Sum256(raw)
		facts = append(facts, map[string]any{"Path": path, "Device": uint64(stat.Dev), "Inode": stat.Ino,
			"UID": stat.Uid, "GID": stat.Gid, "Mode": uint32(info.Mode().Perm()), "Bytes": info.Size(),
			"ModifiedNs": info.ModTime().UnixNano(), "ChangedNs": stat.Ctim.Nano(), "SHA256": hex.EncodeToString(hash[:])})
	}
	return facts, nil
}

// Projections enumerate safe fields. Password hashes, PIN ciphertext, plaintext
// profile PINs, authentication tokens, and authentication session IDs are absent.
func selectedPhase1DatabaseSnapshot(ctx context.Context, f *serverFixture, fixture selectedPhase1BrowserContext) (json.RawMessage, error) {
	var snapshot string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Schema',current_schema(), 'Durable',jsonb_build_object(
		'Credentials',(SELECT jsonb_build_object('UserId',id,'HasLocalPassword',local_password_hash IS NOT NULL,
			'HasProfilePin',profile_pin_ciphertext IS NOT NULL,'EnableLocalPassword',COALESCE(configuration->'EnableLocalPassword','false'::jsonb)) FROM users WHERE id=$1),
		'Preferences',(SELECT jsonb_build_object('Revision',configuration_revision::text,'Configuration',configuration-'ProfilePin') FROM users WHERE id=$1),
		'Intro',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',item_id,'Revision',revision::text,
			'SourceRevision',source_revision,'StartTicks',start_ticks,'EndTicks',end_ticks,'Provenance',provenance) ORDER BY item_id),'[]'::jsonb)
			FROM item_intro_state WHERE item_id=ANY($2::text[]))),
		'Catalog',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Type',type,'Name',name,'ParentId',parent_id,
			'IndexNumber',index_number,'ParentIndexNumber',parent_index_number,'DurationTicks',media->'DurationTicks',
			'ProbeVersion',media->'ProbeVersion','Chapters',media->'Chapters') ORDER BY id),'[]'::jsonb) FROM items WHERE id=ANY($2::text[])),
		'UserData',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',item_id,'PositionTicks',playback_position_ticks,
			'PlayCount',play_count,'Played',played,'LastPlayedAt',last_played_at) ORDER BY item_id),'[]'::jsonb) FROM user_item_data WHERE user_id=$1 AND item_id=ANY($2::text[])),
		'Playback',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',item_id,'State',state,'PositionTicks',position_ticks,
			'DurationTicks',duration_ticks,'Counted',counted,'Started',started_at IS NOT NULL,'Stopped',stopped_at IS NOT NULL) ORDER BY created_at,id),'[]'::jsonb)
			FROM play_sessions WHERE user_id=$1),
		'ActiveSessions',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'ActivePlayback',(SELECT count(*) FROM play_sessions WHERE state IN ('Prepared','Playing','Paused')),
		'ActiveEncodings',(SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running')),
		'ActiveTasks',(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
		'ActiveScans',(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running'))
	)::text`, fixture.UserID, []string{fixture.MovieID, fixture.EpisodeOneID, fixture.EpisodeTwoID}).Scan(&snapshot)
	return json.RawMessage(snapshot), err
}

type selectedPhase1BrowserObserver struct {
	runtime          *phase3BrowserRuntime
	fixture          selectedPhase1BrowserContext
	paths            []string
	sources          []map[string]any
	durable          json.RawMessage
	vaultPath        string
	movieStarted     int
	episodeOneStarts int
	episodeTwoStarts int
	startedSessions  map[string][]string
	playbackObserved bool
	blocked          []string
	completed        int
}

func (observer *selectedPhase1BrowserObserver) credentials(ctx context.Context, enabled bool) error {
	var local, pin, selected bool
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT local_password_hash IS NOT NULL,profile_pin_ciphertext IS NOT NULL,
		COALESCE(configuration->>'EnableLocalPassword','false')='true' FROM users WHERE id=$1`, observer.fixture.UserID).Scan(&local, &pin, &selected)
	if err != nil || local != enabled || pin != enabled || selected != enabled {
		return errors.New("selected phase 1 local credential state did not persist")
	}
	return nil
}

func (observer *selectedPhase1BrowserObserver) preferences(ctx context.Context, mode string, next bool) error {
	var saved bool
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT configuration_revision>1 AND COALESCE(configuration->>'IntroSkipMode','None')=$2
		AND COALESCE((configuration->>'EnableNextEpisodeAutoPlay')::boolean,true)=$3 FROM users WHERE id=$1`, observer.fixture.UserID, mode, next).Scan(&saved)
	if err != nil || !saved {
		return errors.New("selected phase 1 playback preferences did not persist")
	}
	return nil
}

func (observer *selectedPhase1BrowserObserver) intro(ctx context.Context) error {
	var saved bool
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT revision>=1 AND source_revision<>'' AND start_ticks=30000000
		AND end_ticks=80000000 AND provenance='Manual' FROM item_intro_state WHERE item_id=$1`, observer.fixture.MovieID).Scan(&saved)
	if err != nil || !saved {
		return errors.New("selected phase 1 manual movie intro did not persist")
	}
	return nil
}

func (observer *selectedPhase1BrowserObserver) playback(ctx context.Context, itemID string, previous int, minimumPosition int64, requireEnded bool) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	previousIDs := observer.startedSessions[itemID]
	if previousIDs == nil {
		previousIDs = []string{}
	}
	for {
		var ids []string
		var started, stopped, advanced, ended, active int
		err := observer.runtime.f.pool.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY id) FILTER(WHERE started_at IS NOT NULL),'{}'::text[]),
			count(*) FILTER(WHERE started_at IS NOT NULL AND NOT(id=ANY($3::text[]))),
			count(*) FILTER(WHERE started_at IS NOT NULL AND NOT(id=ANY($3::text[])) AND stopped_at IS NOT NULL AND state='Stopped'),
			count(*) FILTER(WHERE started_at IS NOT NULL AND NOT(id=ANY($3::text[])) AND position_ticks>=$4),
			count(*) FILTER(WHERE started_at IS NOT NULL AND NOT(id=ANY($3::text[])) AND position_ticks>=150000000 AND state='Stopped'),
			count(*) FILTER(WHERE state IN ('Prepared','Playing','Paused')) FROM play_sessions WHERE user_id=$1 AND item_id=$2`,
			observer.fixture.UserID, itemID, previousIDs, minimumPosition).Scan(&ids, &started, &stopped, &advanced, &ended, &active)
		if err != nil {
			return 0, errors.New("selected phase 1 playback observation failed")
		}
		if len(ids) > previous && started > 0 && stopped == started && active == 0 && advanced == started && (!requireEnded || ended == started) {
			var counted bool
			if observer.runtime.f.pool.QueryRow(ctx, `SELECT play_count>0 AND last_played_at IS NOT NULL FROM user_item_data
				WHERE user_id=$1 AND item_id=$2`, observer.fixture.UserID, itemID).Scan(&counted) != nil || !counted {
				return 0, errors.New("selected phase 1 playback did not persist real user data")
			}
			if observer.startedSessions == nil {
				observer.startedSessions = make(map[string][]string)
			}
			observer.startedSessions[itemID] = ids
			observer.playbackObserved = true
			return len(ids), nil
		}
		select {
		case <-ctx.Done():
			return 0, errors.New("selected phase 1 playback did not reach its expected terminal state")
		case <-ticker.C:
		}
	}
}

func (observer *selectedPhase1BrowserObserver) stage(ctx context.Context, phase string, blocked bool) error {
	f, fixture := observer.runtime.f, observer.fixture
	switch phase {
	case "admin-credentials", "profile-pin":
		if err := observer.credentials(ctx, true); err != nil {
			return err
		}
	case "admin-preferences":
		if err := observer.preferences(ctx, "ShowButton", true); err != nil {
			return err
		}
	case "admin-intro":
		if err := observer.intro(ctx); err != nil {
			return err
		}
	case "original-local-login":
		var loggedIn bool
		if f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND kind='emby' AND local_auth AND revoked_at IS NULL)`, fixture.UserID).Scan(&loggedIn) != nil || !loggedIn {
			return errors.New("selected phase 1 original client did not create an authenticated user session")
		}
	case "intro-show-button", "intro-none", "intro-auto-skip":
		mode := map[string]string{"intro-show-button": "ShowButton", "intro-none": "None", "intro-auto-skip": "AutoSkip"}[phase]
		if err := observer.preferences(ctx, mode, false); err != nil {
			return err
		}
		minimum := int64(80_000_000)
		if phase == "intro-none" {
			minimum = 30_000_000
		}
		if blocked {
			minimum = 1
		}
		started, err := observer.playback(ctx, fixture.MovieID, observer.movieStarted, minimum, false)
		if err != nil {
			return err
		}
		observer.movieStarted = started
	case "next-enabled":
		if err := observer.preferences(ctx, "None", true); err != nil {
			return err
		}
		first, err := observer.playback(ctx, fixture.EpisodeOneID, 0, 150_000_000, true)
		if err != nil {
			return err
		}
		second, err := observer.playback(ctx, fixture.EpisodeTwoID, 0, 1, false)
		if err != nil {
			return err
		}
		observer.episodeOneStarts, observer.episodeTwoStarts = first, second
	case "next-disabled":
		if err := observer.preferences(ctx, "None", false); err != nil {
			return err
		}
		first, err := observer.playback(ctx, fixture.EpisodeOneID, observer.episodeOneStarts, 150_000_000, true)
		if err != nil {
			return err
		}
		var second int
		if f.pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE user_id=$1 AND item_id=$2 AND started_at IS NOT NULL`, fixture.UserID, fixture.EpisodeTwoID).Scan(&second) != nil || second != observer.episodeTwoStarts {
			return errors.New("selected phase 1 disabled next episode unexpectedly started the second episode")
		}
		observer.episodeOneStarts = first
	case "restart":
		if len(observer.durable) == 0 {
			return errors.New("selected phase 1 restart has no preceding durable state")
		}
		// Construct a new identity store and vault reader as well, so the saved
		// profile PIN must decrypt using the owned master file after restart.
		f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(observer.vaultPath))
		if err := observer.runtime.restart(ctx); err != nil {
			return errors.New("selected phase 1 complete server restart failed")
		}
	case "persisted":
		if err := observer.credentials(ctx, true); err != nil {
			return err
		}
		if err := observer.preferences(ctx, "AutoSkip", false); err != nil {
			return err
		}
		if err := observer.intro(ctx); err != nil {
			return err
		}
	case "credentials-cleared":
		if err := observer.credentials(ctx, false); err != nil {
			return err
		}
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL", fixture.UserID).Scan(&active) != nil || active != 0 {
			return errors.New("selected phase 1 credential reset retained an active user session")
		}
	case "cleanup":
		var active, playback, encodings, tasks, scans int
		err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
			(SELECT count(*) FROM play_sessions WHERE state IN ('Prepared','Playing','Paused')),
			(SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running')),
			(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
			(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running'))`).Scan(&active, &playback, &encodings, &tasks, &scans)
		if err != nil || active != 0 || playback != 0 || encodings != 0 || tasks != 0 || scans != 0 {
			return errors.New("selected phase 1 browser did not retire its sessions and active work")
		}
	}
	sources, err := selectedPhase1SourceFacts(observer.paths)
	if err != nil || !reflect.DeepEqual(sources, observer.sources) {
		return errors.New("selected phase 1 browser changed an owned media or metadata source")
	}
	database, err := selectedPhase1DatabaseSnapshot(ctx, f, fixture)
	if err != nil {
		return errors.New("selected phase 1 database snapshot failed")
	}
	var snapshot struct{ Durable json.RawMessage }
	if json.Unmarshal(database, &snapshot) != nil || len(snapshot.Durable) == 0 {
		return errors.New("selected phase 1 database observation lacks durable state")
	}
	if phase == "intro-auto-skip" {
		observer.durable = append(json.RawMessage(nil), snapshot.Durable...)
	}
	if (phase == "restart" || phase == "persisted") && !bytes.Equal(observer.durable, snapshot.Durable) {
		return errors.New("selected phase 1 restart changed saved credentials, preferences, or intro markers")
	}
	return featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "stage-"+phase+"-database.json"), map[string]any{
		"Marker": "goby-selected-phase1-stage-database-v1", "RunId": fixture.RunID, "Phase": phase,
		"Complete": !blocked, "Observed": true, "Blocked": blocked,
		"SourcesUnchanged": true, "Sources": sources, "Database": database, "BaseURL": "http://" + observer.runtime.addr,
	})
}

func (observer *selectedPhase1BrowserObserver) run(ctx context.Context) error {
	for _, phase := range selectedPhase1BrowserPhases {
		request := phase3BrowserContext{RunID: observer.fixture.RunID, ArtifactsDir: observer.fixture.ArtifactsDir}
		if err := phase3BrowserWaitRequest(ctx, request, phase); err != nil {
			return errors.New("selected phase 1 browser stage request failed")
		}
		var state struct {
			RunID  string `json:"RunId"`
			Phase  string
			State  string
			Reason string
		}
		if featureWavePrivateJSON(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-request.json"), 4096, &state) != nil ||
			state.RunID != observer.fixture.RunID || state.Phase != phase {
			return errors.New("selected phase 1 stage state does not bind the expected request")
		}
		blocked := state.State == "blocked"
		if blocked {
			if (phase != "intro-show-button" && phase != "intro-auto-skip") || state.Reason != "original_client_entitlement" {
				return errors.New("selected phase 1 stage cannot use the declared blocked disposition")
			}
		} else if state.State != "complete" || state.Reason != "" {
			return errors.New("selected phase 1 stage state is invalid")
		}
		if err := observer.stage(ctx, phase, blocked); err != nil {
			return err
		}
		if blocked {
			observer.blocked = append(observer.blocked, phase)
		}
		observer.completed++
	}
	return nil
}

func selectedPhase1Scan(t *testing.T, f *serverFixture, libraryID string, count int) {
	t.Helper()
	job, err := f.app.library.StartScan(f.ctx, libraryID)
	if err != nil {
		t.Fatal("start selected phase 1 real media scan")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := f.app.library.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatal("read selected phase 1 real media scan")
		}
		if current.Status == "Completed" {
			if current.Error != "" || current.ForceProbe || current.Scanned != count {
				t.Fatal("selected phase 1 scan did not index the exact owned media files")
			}
			return
		}
		if current.Status != "Queued" && current.Status != "Running" {
			t.Fatal("selected phase 1 media scan failed")
		}
		select {
		case <-ctx.Done():
			t.Fatal("selected phase 1 media scan exceeded its deadline")
		case <-ticker.C:
		}
	}
}

func selectedPhase1Media(t *testing.T, ffmpeg, path, metadata, color string) {
	t.Helper()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		t.Fatal("create selected phase 1 owned media directory")
	}
	arguments := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=" + color + ":size=160x90:rate=24:duration=18",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=18"}
	if metadata != "" {
		arguments = append(arguments, "-f", "ffmetadata", "-i", metadata, "-map_metadata", "2", "-map_chapters", "2")
	}
	arguments = append(arguments, "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1",
		"-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "18", "-movflags", "+faststart", path)
	hlsHTTPMediaCommand(t, ffmpeg, arguments...)
	if os.Chmod(path, 0o600) != nil {
		t.Fatal("protect selected phase 1 owned media")
	}
}

func selectedPhase1Item(t *testing.T, f *serverFixture, userID, libraryID, path, kind string, chapters bool) library.Item {
	t.Helper()
	var itemID string
	if f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND path=$2 AND type=$3", libraryID, path, kind).Scan(&itemID) != nil {
		t.Fatal("read selected phase 1 scanned item identity")
	}
	item, err := f.app.library.GetItem(f.ctx, userID, itemID)
	if err != nil || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion ||
		item.Media.DurationTicks < 179_000_000 || item.Media.DurationTicks > 181_000_000 {
		t.Fatal("selected phase 1 catalog lacks actual eighteen-second probe facts")
	}
	video, audio, start, end := false, false, false, false
	for _, stream := range item.Media.Streams {
		video = video || stream.CodecType == "video" && stream.Codec == "h264"
		audio = audio || stream.CodecType == "audio" && stream.Codec == "aac"
	}
	for _, chapter := range item.Media.Chapters {
		start = start || chapter.Title == "IntroStart" && chapter.StartTicks == 30_000_000
		end = end || chapter.Title == "IntroEnd" && chapter.StartTicks == 80_000_000
	}
	if !video || !audio || chapters && (!start || !end) || !chapters && len(item.Media.Chapters) != 0 {
		t.Fatal("selected phase 1 source codecs or explicit intro chapter boundaries differ")
	}
	if chapters && (item.Intro == nil || item.Intro.StartTicks != 30_000_000 || item.Intro.EndTicks != 80_000_000 || item.Intro.Provenance != "Chapter") ||
		!chapters && item.Intro != nil {
		t.Fatal("selected phase 1 scanned source intro projection differs from its explicit chapters")
	}
	return item
}

func TestSelectedCompatibilityPhase1BrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("selected phase 1 browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	ffmpeg := refreshBrowserPath(t, "GOBY_FFMPEG", false)
	ffprobe := refreshBrowserPath(t, "GOBY_FFPROBE", false)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("selected phase 1 artifact parent must be private")
	}
	runID := os.Getenv("GOBY_SELECTED_PHASE1_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("an explicit selected phase 1 browser run ID is required")
	}
	if resolved, err := filepath.EvalSymlinks(selectedPhase1OriginalWebDirectory); err != nil || resolved != selectedPhase1OriginalWebDirectory {
		t.Fatal("the original Emby Web asset directory must exist at its pinned canonical path")
	}
	if info, err := os.Lstat(filepath.Join(selectedPhase1OriginalWebDirectory, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("the original Emby Web entry point is unavailable")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read selected phase 1 source directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate selected phase 1 source root")
	}
	script := filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase1-browser.mjs")
	if info, err := os.Lstat(script); err != nil || !info.Mode().IsRegular() {
		t.Fatal("the selected phase 1 browser driver is unavailable")
	}
	output, err := os.MkdirTemp(artifacts, "selected-phase1-browser-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create a private selected phase 1 artifact directory")
	}
	driver := map[string]any{"Marker": "goby-selected-phase1-browser-driver-v1", "RunId": runID, "Complete": false,
		"ArtifactDirectory": output, "CredentialsWrittenToSummary": false, "OriginalWebDirectory": selectedPhase1OriginalWebDirectory,
		"MediaPlaybackExercised": false, "CatalogFixture": "real eighteen-second H.264/AAC media with explicit intro chapters"}
	var schema, mediaRoot string
	t.Cleanup(func() {
		if schema != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe selected phase 1 schema cleanup")
			} else {
				defer pool.Close()
				var remaining bool
				if pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remaining) != nil || remaining {
					t.Error("selected phase 1 owned schema was not removed")
				} else {
					driver["OwnedSchemaRemoved"] = true
				}
			}
		}
		if mediaRoot != "" {
			if _, err := os.Lstat(mediaRoot); !errors.Is(err, os.ErrNotExist) {
				t.Error("selected phase 1 owned media root was not removed")
			} else {
				driver["OwnedMediaRootRemoved"] = true
			}
		}
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve the selected phase 1 driver result")
		}
	})
	f := newServerFixtureWithTimeout(t, 15*time.Minute)
	if f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema) != nil {
		t.Fatal("identify the owned selected phase 1 schema")
	}
	mediaRoot = t.TempDir()
	metadata := filepath.Join(mediaRoot, "intro.ffmetadata")
	// The ordinary opening chapter preserves the explicit three-second start
	// when MP4 writes its chapter track. Only the exact marker pair is an intro.
	chapterText := ";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=3000\ntitle=Opening\n" +
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=3000\nEND=8000\ntitle=IntroStart\n" +
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=8000\nEND=18000\ntitle=IntroEnd\n"
	if os.WriteFile(metadata, []byte(chapterText), 0o600) != nil {
		t.Fatal("write selected phase 1 explicit chapter metadata")
	}
	seriesPath := filepath.Join(mediaRoot, "television")
	moviePath := filepath.Join(mediaRoot, "movies", "Selected Phase One Movie.mp4")
	firstPath := filepath.Join(seriesPath, "Selected Phase One Series", "Season 01", "Selected.Phase.One.Series.S01E01.mp4")
	secondPath := filepath.Join(seriesPath, "Selected Phase One Series", "Season 01", "Selected.Phase.One.Series.S01E02.mp4")
	selectedPhase1Media(t, ffmpeg, moviePath, "", "red")
	selectedPhase1Media(t, ffmpeg, firstPath, metadata, "green")
	selectedPhase1Media(t, ffmpeg, secondPath, metadata, "blue")
	paths := []string{moviePath, firstPath, secondPath, metadata}
	sources, err := selectedPhase1SourceFacts(paths)
	if err != nil {
		t.Fatal("capture selected phase 1 original media identity and content")
	}
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("close the initial selected phase 1 application")
	}
	vaultPath := filepath.Join(t.TempDir(), "profile-credentials.master")
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(vaultPath))
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = ffmpeg, ffprobe, []string{mediaRoot}
	f.cfg.WebDirectory = selectedPhase1OriginalWebDirectory
	f.cfg.Transcoding = config.TranscodingConfig{Enabled: true, CacheDirectory: t.TempDir(), Threads: 1,
		MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}
	assets, err := adminassets.Files()
	if err != nil {
		t.Fatal("open the selected phase 1 embedded administrator bundle")
	}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "selected-phase1-browser-integration", WithDashboardAssets(assets))
	if err != nil {
		t.Fatal("construct the selected phase 1 real media application")
	}
	f.app, f.handler = app, app.Handler()
	runtime := &phase3BrowserRuntime{f: f, assets: assets, addr: "127.0.0.1:0"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe residual selected phase 1 authentication sessions")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("revoke residual selected phase 1 authentication sessions")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
			if driver["Complete"] == true && result.RowsAffected() != 0 {
				t.Error("successful selected phase 1 browser required fallback session revocation")
			}
		}
		if err := runtime.close(ctx); err != nil {
			t.Error("close the selected phase 1 server generation")
		} else {
			driver["ServerWorkersClosed"], driver["HTTPListenerClosed"] = true, true
		}
	})
	adminPassword, userPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Selected phase 1 administrator", adminPassword)
	if err != nil {
		t.Fatal("bootstrap selected phase 1 administrator")
	}
	member, err := f.users.CreateUser(f.ctx, "Selected phase 1 viewer", userPassword, false)
	if err != nil {
		t.Fatal("create selected phase 1 preference owner")
	}
	seriesLibrary, err := app.library.CreateLibrary(f.ctx, "Selected Phase One Television", "tvshows", []string{seriesPath})
	if err != nil {
		t.Fatal("create selected phase 1 television library")
	}
	movieLibrary, err := app.library.CreateLibrary(f.ctx, "Selected Phase One Movies", "movies", []string{filepath.Dir(moviePath)})
	if err != nil {
		t.Fatal("create selected phase 1 movie library")
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{seriesLibrary.ID, movieLibrary.ID},
		"EnableMediaPlayback": true, "EnablePlaybackRemuxing": true, "EnableVideoPlaybackTranscoding": true, "EnableAudioPlaybackTranscoding": true})
	if err != nil {
		t.Fatal("encode selected phase 1 owned library playback policy")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", member.ID, policy); err != nil {
		t.Fatal("apply selected phase 1 owned library playback policy")
	}
	selectedPhase1Scan(t, f, seriesLibrary.ID, 2)
	selectedPhase1Scan(t, f, movieLibrary.ID, 1)
	movie := selectedPhase1Item(t, f, member.ID, movieLibrary.ID, moviePath, "Movie", false)
	first := selectedPhase1Item(t, f, member.ID, seriesLibrary.ID, firstPath, "Episode", true)
	second := selectedPhase1Item(t, f, member.ID, seriesLibrary.ID, secondPath, "Episode", true)
	if first.Series == nil || second.Series == nil || first.Series.ID != second.Series.ID || first.IndexNumber != 1 || second.IndexNumber != 2 {
		t.Fatal("selected phase 1 episodes do not share a real scanned ordered series")
	}
	embeddedRows, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		t.Fatal("inventory selected phase 1 administrator assets")
	}
	bundlePath := filepath.Join(sourceRoot, "web", "admin", "dist")
	if resolved, err := filepath.EvalSymlinks(bundlePath); err != nil || resolved != bundlePath {
		t.Fatal("the selected phase 1 source administrator bundle must be canonical")
	}
	sourceRows, err := refreshBrowserAssetInventory(os.DirFS(bundlePath))
	if err != nil || !reflect.DeepEqual(embeddedRows, sourceRows) || refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embeddedRows) != nil {
		t.Fatal("selected phase 1 embedded assets differ from the frozen source bundle")
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	if err := runtime.listen(); err != nil {
		t.Fatal("start the owned selected phase 1 TCP4 HTTP listener")
	}
	fixture := selectedPhase1BrowserContext{Marker: "goby-selected-phase1-browser-fixture-v1", RunID: runID,
		BaseURL: "http://" + runtime.addr, ServerID: f.app.serverID, AdminID: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword,
		UserID: member.ID, UserName: member.Name, UserPassword: userPassword, LocalPassword: featureWavePassword(t), ProfilePin: "2468",
		LibraryID: seriesLibrary.ID, LibraryName: seriesLibrary.Name, MovieLibraryID: movieLibrary.ID, MovieLibraryName: movieLibrary.Name,
		MovieID: movie.ID, MovieName: movie.Name, EpisodeOneID: first.ID, EpisodeOneName: first.Name,
		EpisodeTwoID: second.ID, EpisodeTwoName: second.Name, SeriesID: first.Series.ID,
		ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	manifestPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove selected phase 1 private credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if refreshBrowserWriteJSON(manifestPath, fixture) != nil {
		t.Fatal("save selected phase 1 private browser context")
	}
	seed, err := selectedPhase1DatabaseSnapshot(f.ctx, f, fixture)
	if err != nil || refreshBrowserWriteJSON(filepath.Join(output, "stage-seeded-database.json"), map[string]any{
		"RunId": runID, "Database": seed, "Sources": sources, "RealMediaFiles": 3, "ScannedEpisodes": 2}) != nil {
		t.Fatal("save selected phase 1 real media seed evidence")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create selected phase 1 private browser runtime directory")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright,
		"GOBY_SELECTED_PHASE1_RUN_ID=" + runID, "GOBY_SELECTED_PHASE1_CONTEXT=" + manifestPath}
	observer := &selectedPhase1BrowserObserver{runtime: runtime, fixture: fixture, paths: paths, sources: sources, vaultPath: vaultPath}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Minute)
	defer cancel()
	observerCtx, stopObserver := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{script}, environment, sourceRoot, output)
	stopObserver()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	driver["MediaPlaybackExercised"] = observer.playbackObserved
	driver["BlockedDatabaseStages"] = append([]string{}, observer.blocked...)
	if observerErr != nil && observer.completed < len(selectedPhase1BrowserPhases) {
		driver["FailedDatabaseStage"], driver["DatabaseObservationError"] = selectedPhase1BrowserPhases[observer.completed], observerErr.Error()
	}
	var result selectedPhase1BrowserResult
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(selectedPhase1BrowserPhases) || len(observer.blocked) != 0 {
		t.Fatal("selected phase 1 browser or database observer failed; inspect retained private artifacts")
	}
	checks := []string{"AdminCredentials", "AdminPreferences", "AdminIntro", "OriginalLocalLogin", "ProfilePin",
		"IntroShowButton", "IntroNone", "IntroAutoSkip", "NextEnabled", "NextDisabled", "RestartPersisted", "CredentialsCleared", "Cleanup"}
	if result.Marker != "goby-selected-phase1-browser-result-v1" || result.RunID != runID || !result.Complete ||
		len(result.Checks) != len(checks) || len(result.Stages) != len(selectedPhase1BrowserPhases) ||
		result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("selected phase 1 browser result does not bind the complete owned scenario")
	}
	for index, phase := range selectedPhase1BrowserPhases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" {
			t.Fatal("selected phase 1 browser stages differ from the observed database stages")
		}
	}
	for _, check := range checks {
		if !result.Checks[check] {
			t.Fatal("a selected phase 1 browser assertion did not complete")
		}
	}
	driver["Complete"], driver["SourcesUnchanged"], driver["OwnedSessionsRetired"] = true, true, true
	driver["BrowserChecks"], driver["ServerRestarts"] = result.Checks, 1
	t.Log("selected_phase1_browser_verified=true stages=14 server_restarts=1 real_media_files=3 original_client_playback=true")
}
