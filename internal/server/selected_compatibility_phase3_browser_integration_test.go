//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	adminassets "github.com/moooyo/goby/web/admin"
)

var selectedPhase3Phases = []string{"authentication", "preferences", "roster-reviewed", "roster-saved",
	"missing-queries", "suggestions-played", "music-navigation", "music-mixes", "search-navigation",
	"audio-playing", "audio-stopped", "physical-arrival", "arrival-queries", "restart", "persisted", "cleanup"}

type selectedPhase3Execution struct {
	Marker                                               string
	FFmpegPath, FFmpegSHA256, FFprobePath, FFprobeSHA256 string
}

type selectedPhase3Music struct {
	TrackID         string `json:"TrackId"`
	TrackName       string
	AlbumID         string `json:"AlbumId"`
	AlbumName       string
	ArtistID        string `json:"ArtistId"`
	ArtistName      string
	AlbumArtistID   string `json:"AlbumArtistId"`
	AlbumArtistName string
	GenreID         string `json:"GenreId"`
	GenreName       string
}

// Passwords occur only in the private context removed by fixture cleanup.
// Tokens, database connection strings and authentication IDs are never exported.
type selectedPhase3Context struct {
	Marker                     string
	RunID                      string `json:"RunId"`
	BaseURL                    string
	AdminID                    string `json:"AdminId"`
	AdminName, AdminPassword   string
	ViewerID                   string `json:"ViewerId"`
	ViewerName, ViewerPassword string
	TelevisionLibraryID        string `json:"TelevisionLibraryId"`
	MovieLibraryID             string `json:"MovieLibraryId"`
	MusicLibraryID             string `json:"MusicLibraryId"`
	SeriesID                   string `json:"SeriesId"`
	SeriesName                 string
	InitialEpisodeID           string `json:"InitialEpisodeId"`
	UnknownSeriesID            string `json:"UnknownSeriesId"`
	UnknownSeriesName          string
	SuggestionAID              string   `json:"SuggestionAId"`
	SuggestionBID              string   `json:"SuggestionBId"`
	HiddenItemIDs              []string `json:"HiddenItemIds"`
	HiddenEntityIDs            []string `json:"HiddenEntityIds"`
	Music                      []selectedPhase3Music
	PlaylistID                 string `json:"PlaylistId"`
	Roster                     struct {
		Source  library.EpisodeRosterSourceInput
		Entries []library.EpisodeRosterEntryInput
	}
	SearchTerm, ArtifactsDir, ResultPath string
}

type selectedPhase3Request struct {
	RunID                                                      string `json:"RunId"`
	Phase, PreferencesRevision, LoadedRevision, RosterRevision string
	SeriesID                                                   string   `json:"SeriesId"`
	MissingIDs                                                 []string `json:"MissingIds"`
	QueryItemIDs                                               []string `json:"QueryItemIds"`
	RemainingMissingIDs                                        []string `json:"RemainingMissingIds"`
	MixTrackIDs                                                []string `json:"MixTrackIds"`
	PlayedItemID                                               string   `json:"PlayedItemId"`
	UnplayedItemID                                             string   `json:"UnplayedItemId"`
	ArtistID                                                   string   `json:"ArtistId"`
	AlbumArtistID                                              string   `json:"AlbumArtistId"`
	AlbumID                                                    string   `json:"AlbumId"`
	TrackID                                                    string   `json:"TrackId"`
	PhysicalDetailID                                           string   `json:"PhysicalDetailId"`
	EntityDetailID                                             string   `json:"EntityDetailId"`
	ImageSHA256                                                string
	PlaySessionID                                              string `json:"PlaySessionId"`
	DeviceID                                                   string `json:"DeviceId"`
	MediaSourceID                                              string `json:"MediaSourceId"`
	AudioItemID                                                string `json:"AudioItemId"`
	PositionTicks                                              int64
	ArrivedItemID                                              string `json:"ArrivedItemId"`
}

type selectedPhase3Runtime struct {
	*phase3BrowserRuntime
	active atomic.Int64
}

func (runtime *selectedPhase3Runtime) listen() error {
	listener, err := net.Listen("tcp4", runtime.addr)
	if err != nil {
		return err
	}
	runtime.addr = listener.Addr().String()
	origin := "http://" + runtime.addr
	runtime.f.cfg.PublicURL, runtime.f.cfg.CookieSecure = origin, false
	runtime.f.app.cfg.PublicURL, runtime.f.app.cfg.CookieSecure = origin, false
	WithDashboardAssets(runtime.assets)(runtime.f.app)
	application := runtime.f.app.Handler()
	runtime.f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtime.active.Add(1)
		defer runtime.active.Add(-1)
		if r.URL.Path != "/__selected-phase3-consumer" {
			application.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Use GET for the owned adapter consumer.", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; media-src 'self' blob:; img-src 'self' blob: data:")
		_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Owned adapter consumer</title></head><body><main><h1>Owned adapter consumer</h1><div id="choices"></div><pre id="detail"></pre><img id="artwork" alt="Actual adapter image" hidden><button id="play-audio">Play selected mix track</button><button id="stop-audio">Stop playback</button><audio id="audio" controls></audio><p id="status"></p></main></body></html>`)
	})
	actual := httptest.NewUnstartedServer(runtime.f.handler)
	actual.Listener.Close()
	actual.Listener = listener
	actual.Start()
	runtime.server = actual
	return nil
}

func (runtime *selectedPhase3Runtime) restart(ctx context.Context) error {
	if err := runtime.close(ctx); err != nil {
		return err
	}
	if runtime.active.Load() != 0 {
		return errors.New("phase 3 restart retained active HTTP requests")
	}
	app, err := New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log,
		"selected-phase3-browser-integration", WithDashboardAssets(runtime.assets))
	if err != nil {
		return err
	}
	runtime.f.app = app
	return runtime.listen()
}

func selectedPhase3ReadExecution(t *testing.T) (selectedPhase3Execution, []selectedPhase2FileFact) {
	t.Helper()
	path := refreshBrowserPath(t, "GOBY_SELECTED_PHASE3_EXECUTION_CONFIG", false)
	var raw json.RawMessage
	if err := featureWavePrivateJSON(path, 16<<10, &raw); err != nil {
		selectedPhase2Fatal(t, "read private phase 3 execution inventory", err)
	}
	var execution selectedPhase3Execution
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&execution); err != nil {
		selectedPhase2Fatal(t, "decode phase 3 execution inventory", err)
	}
	if execution.Marker != "goby-selected-phase3-execution-v1" {
		t.Fatal("phase 3 execution inventory marker differs")
	}
	facts := []selectedPhase2FileFact{}
	for _, input := range []struct{ path, digest string }{{execution.FFmpegPath, execution.FFmpegSHA256}, {execution.FFprobePath, execution.FFprobeSHA256}} {
		fact, err := selectedPhase2Fact(input.path, 256<<20)
		if err != nil || len(input.digest) != 64 || fact.SHA256 != input.digest {
			t.Fatal("phase 3 pinned media tool identity differs")
		}
		facts = append(facts, fact)
	}
	return execution, facts
}

func selectedPhase3Copy(source, target string) error {
	before, err := selectedPhase2Fact(source, 32<<20)
	if err != nil {
		return errors.New("phase 3 copy source identity is invalid")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(output, io.LimitReader(input, 32<<20+1))
	syncErr, closeErr := output.Sync(), output.Close()
	after, sourceErr := selectedPhase2Fact(source, 32<<20)
	copied, targetErr := selectedPhase2Fact(target, 32<<20)
	if copyErr != nil || syncErr != nil || closeErr != nil || sourceErr != nil || targetErr != nil ||
		before != after || n != before.Bytes || copied.Bytes != before.Bytes || copied.SHA256 != before.SHA256 {
		return errors.New("phase 3 owned copy did not preserve exact source bytes")
	}
	return nil
}

func selectedPhase3Files(root string) (map[string]selectedPhase2FileFact, error) {
	result := map[string]selectedPhase2FileFact{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		fact, err := selectedPhase2Fact(path, 32<<20)
		if err != nil {
			return err
		}
		result[path] = fact
		return nil
	})
	return result, err
}

func selectedPhase3Scan(ctx context.Context, f *serverFixture, libraryID string, expected int) error {
	job, err := f.app.library.StartScan(ctx, libraryID)
	if err != nil {
		return errors.New("phase 3 actual scan admission failed")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := f.app.library.GetJob(ctx, job.ID)
		if err != nil {
			return errors.New("phase 3 actual scan observation failed")
		}
		if current.Status == "Completed" {
			if current.Error != "" || current.Scanned != expected || current.ForceProbe {
				return errors.New("phase 3 actual scan did not retain the exact authored population")
			}
			return nil
		}
		if current.Status != "Queued" && current.Status != "Running" {
			return errors.New("phase 3 actual scan failed")
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 3 actual scan exceeded its deadline")
		case <-ticker.C:
		}
	}
}

func selectedPhase3Item(t *testing.T, f *serverFixture, userID, path, kind string) library.Item {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE path=$1 AND type=$2", path, kind).Scan(&id); err != nil {
		selectedPhase2Fatal(t, "read phase 3 scanned identity", err)
	}
	item, err := f.app.library.GetItem(f.ctx, userID, id)
	if err != nil || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion ||
		item.Media.DurationTicks < 79_000_000 || item.Media.DurationTicks > 82_000_000 {
		t.Fatal("phase 3 source lacks actual eight-second current media facts")
	}
	return item
}

func selectedPhase3Author(t *testing.T, execution selectedPhase3Execution, root string) (string, []string, []string) {
	t.Helper()
	directories := []string{"authoring", "movies", "hidden-movies", "tv/Phase Three Known Show/Season 01",
		"tv/Phase Three Unknown Show/Season 01", "music/Phase Three First Album", "music/Phase Three Second Album",
		"music/Phase Three Third Album", "hidden-music/Phase Three Shared Hidden Album", "hidden-music/Phase Three Exclusive Hidden Album"}
	for _, name := range directories {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o700); err != nil {
			selectedPhase2Fatal(t, "create phase 3 owned directories", err)
		}
	}
	source := filepath.Join(root, "authoring", "arrival-source.mp4")
	hlsHTTPMediaCommand(t, execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=navy:size=160x90:rate=24:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=8",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "24", "-bf", "0",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "8", "-movflags", "+faststart", source)
	if err := os.Chmod(source, 0o600); err != nil {
		selectedPhase2Fatal(t, "protect phase 3 authored video", err)
	}
	videoPaths := []string{}
	for _, name := range []string{"movies/Phase Three Suggestion A.mp4", "movies/Phase Three Suggestion B.mp4", "hidden-movies/Phase Three Hidden Movie.mp4",
		"tv/Phase Three Known Show/Season 01/Phase.Three.Known.Show.S01E01.mp4",
		"tv/Phase Three Unknown Show/Season 01/Phase.Three.Unknown.Show.S01E01.mp4",
		"tv/Phase Three Unknown Show/Season 01/Phase.Three.Unknown.Show.S01E03.mp4"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := selectedPhase3Copy(source, path); err != nil {
			selectedPhase2Fatal(t, "create phase 3 actual video copy", err)
		}
		videoPaths = append(videoPaths, path)
	}
	cover := filepath.Join(root, "authoring", "cover.png")
	selectedPhase2PNG(t, cover, color.NRGBA{R: 36, G: 144, B: 224, A: 255})
	audioPaths := []string{}
	for index, names := range []struct{ dir, album, title, artist, ensemble, genre string }{
		{"music", "Phase Three First Album", "Phase Three First Track", "Phase Three Shared Artist", "Phase Three Shared Ensemble", "Phase Three Shared Genre"},
		{"music", "Phase Three Second Album", "Phase Three Second Track", "Phase Three Shared Artist", "Phase Three Shared Ensemble", "Phase Three Shared Genre"},
		{"music", "Phase Three Third Album", "Phase Three Third Track", "Phase Three Other Artist", "Phase Three Other Ensemble", "Phase Three Other Genre"},
		{"hidden-music", "Phase Three Shared Hidden Album", "Phase Three Shared Hidden Track", "Phase Three Shared Artist", "Phase Three Shared Ensemble", "Phase Three Shared Genre"},
		{"hidden-music", "Phase Three Exclusive Hidden Album", "Phase Three Exclusive Hidden Track", "Phase Three Exclusive Artist", "Phase Three Exclusive Ensemble", "Phase Three Exclusive Genre"},
	} {
		path := filepath.Join(root, names.dir, names.album, names.title+".mp3")
		args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=48000:duration=8", 500+index*77),
			"-i", cover, "-map", "0:a:0", "-map", "1:v:0", "-c:a", "libmp3lame", "-threads:a", "1", "-b:a", "128k",
			"-c:v", "copy", "-disposition:v:0", "attached_pic", "-metadata:s:v:0", "title=Actual embedded cover",
			"-metadata:s:v:0", "comment=Cover (front)", "-metadata", "title=" + names.title, "-metadata", "album=" + names.album,
			"-metadata", "artist=" + names.artist, "-metadata", "album_artist=" + names.ensemble, "-metadata", "genre=" + names.genre,
			"-metadata", "track=1", "-metadata", "disc=1", "-id3v2_version", "3", "-t", "8", path}
		hlsHTTPMediaCommand(t, execution.FFmpegPath, args...)
		if err := os.Chmod(path, 0o600); err != nil {
			selectedPhase2Fatal(t, "protect phase 3 authored audio", err)
		}
		audioPaths = append(audioPaths, path)
	}
	return source, videoPaths, audioPaths
}

type selectedPhase3Observer struct {
	runtime                                     *selectedPhase3Runtime
	fixture                                     selectedPhase3Context
	actor                                       identity.Principal
	actorToken                                  string
	files                                       map[string]selectedPhase2FileFact
	arrivalSource, arrivalPath, arrivedID       string
	initialPreferences, initialEpisode, durable json.RawMessage
	preferencesRevision, rosterRevision         string
	playID, playAuth, playDevice, playSource    string
	playPosition                                int64
	playSessions                                []*hlsSession
	producerIDs                                 []string
	audioObservation                            map[string]any
	completed                                   int
}

func selectedPhase3Durable(ctx context.Context, f *serverFixture) (json.RawMessage, error) {
	var snapshot string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Items',(SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.id),'[]'::jsonb) FROM items i),
		'Libraries',(SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id),'[]'::jsonb) FROM libraries l),
		'Roots',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.id),'[]'::jsonb) FROM library_roots r),
		'MetadataState',(SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY m.item_id),'[]'::jsonb) FROM item_metadata_state m),
		'Entities',(SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.id),'[]'::jsonb) FROM catalog_entities e),
		'Associations',(SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.item_id,e.entity_id,e.credit_group,e.position),'[]'::jsonb) FROM item_entities e),
		'Rosters',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.series_id),'[]'::jsonb) FROM series_episode_rosters r),
		'Imports',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.series_id,r.revision),'[]'::jsonb) FROM episode_roster_imports r),
		'Expected',(SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.id),'[]'::jsonb) FROM expected_episodes e),
		'Preferences',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Configuration',configuration,'Revision',configuration_revision::text) ORDER BY id),'[]'::jsonb) FROM users),
		'UserData',(SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY d.user_id,d.item_id),'[]'::jsonb) FROM user_item_data d),
		'Collections',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.item_id),'[]'::jsonb) FROM media_collections c),
		'CollectionEntries',(SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.collection_id,e.position,e.id),'[]'::jsonb) FROM media_collection_entries e),
		'CollectionShares',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY s.collection_id,s.user_id),'[]'::jsonb) FROM media_collection_shares s),
		'EmbeddedArtwork',(SELECT COALESCE(jsonb_agg((to_jsonb(a)-'content')||jsonb_build_object('ContentSHA256',encode(sha256(a.content),'hex')) ORDER BY a.item_id),'[]'::jsonb) FROM item_embedded_artwork a)
	)::text`).Scan(&snapshot)
	return json.RawMessage(snapshot), err
}

func (observer *selectedPhase3Observer) preservedEpisode(ctx context.Context) (json.RawMessage, error) {
	var snapshot string
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT jsonb_build_object('Item',jsonb_build_object(
		'Id',i.id,'LibraryId',i.library_id,'ParentId',i.parent_id,'Type',i.type,'Index',i.index_number,'Season',i.parent_index_number,
		'Media',i.media,'FileIdentity',i.file_identity,'FileSize',i.file_size,'Metadata',i.local_metadata),
		'UserData',(SELECT to_jsonb(d) FROM user_item_data d WHERE d.item_id=i.id AND d.user_id=$2))::text
		FROM items i WHERE i.id=$1`, observer.fixture.InitialEpisodeID, observer.fixture.ViewerID).Scan(&snapshot)
	return json.RawMessage(snapshot), err
}

func (observer *selectedPhase3Observer) preferences(ctx context.Context, expected bool, revision string) error {
	var configuration []byte
	var current string
	if err := observer.runtime.f.pool.QueryRow(ctx, "SELECT configuration,configuration_revision::text FROM users WHERE id=$1",
		observer.fixture.ViewerID).Scan(&configuration, &current); err != nil {
		return errors.New("phase 3 preference observation failed")
	}
	if !expected {
		if !bytes.Equal(configuration, observer.initialPreferences) || current != observer.preferencesRevision {
			return errors.New("phase 3 initial preferences changed before native save")
		}
		return nil
	}
	value := identity.ProjectUserConfiguration(configuration)
	if !value.DisplayMissingEpisodes || !value.HidePlayedInSuggestions || revision == "" || current != revision {
		return errors.New("phase 3 native preference save did not persist both flags and revision")
	}
	return nil
}

func (observer *selectedPhase3Observer) expectedIDs(remaining bool) []string {
	ids := []string{}
	for _, entry := range observer.fixture.Roster.Entries {
		if remaining && entry.EpisodeNumber == 2 {
			continue
		}
		ids = append(ids, library.ExpectedEpisodeID(observer.fixture.SeriesID, observer.fixture.Roster.Source.Key, entry.Key))
	}
	slices.Sort(ids)
	return ids
}

func selectedPhase3SameIDs(actual, expected []string) bool {
	first, second := append([]string{}, actual...), append([]string{}, expected...)
	slices.Sort(first)
	slices.Sort(second)
	return reflect.DeepEqual(first, second)
}

func (observer *selectedPhase3Observer) roster(ctx context.Context, saved, arrived bool) error {
	f, fixture := observer.runtime.f, observer.fixture
	var rosters, imports, facts, syntheticItems, unknown int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM series_episode_rosters),(SELECT count(*) FROM episode_roster_imports),
		(SELECT count(*) FROM expected_episodes),(SELECT count(*) FROM items WHERE id LIKE 'missing-%'),
		(SELECT count(*) FROM expected_episodes WHERE series_id=$1)`, fixture.UnknownSeriesID).
		Scan(&rosters, &imports, &facts, &syntheticItems, &unknown); err != nil {
		return errors.New("phase 3 roster table observation failed")
	}
	if syntheticItems != 0 || unknown != 0 {
		return errors.New("phase 3 fabricated physical or unproven episode facts")
	}
	detail, err := f.app.library.GetEpisodeRoster(ctx, observer.actor, fixture.SeriesID)
	if err != nil {
		return errors.New("phase 3 current native roster read failed")
	}
	if !saved {
		if rosters != 0 || imports != 0 || facts != 0 || detail.State != "absent" || detail.Revision != "0" || detail.Source != nil || len(detail.Entries) != 0 {
			return errors.New("phase 3 preview mutated the accepted roster")
		}
		return nil
	}
	if rosters != 1 || imports != 1 || facts != 3 || detail.State != "active" || detail.Revision != observer.rosterRevision ||
		detail.Source == nil || detail.Source.Kind != "admin_import" || detail.Source.Key != fixture.Roster.Source.Key ||
		detail.Source.Label != fixture.Roster.Source.Label || detail.Source.Revision != fixture.Roster.Source.Revision ||
		detail.Source.ParserVersion != 1 || detail.LastEditedBy != fixture.AdminID || detail.RetiredCount != 0 || len(detail.Entries) != 3 {
		return errors.New("phase 3 accepted roster provenance or revision differs")
	}
	var payload []byte
	var action, sourceHash, actor string
	if f.pool.QueryRow(ctx, "SELECT payload,action,payload_sha256,actor_id FROM episode_roster_imports WHERE series_id=$1 AND revision=1",
		fixture.SeriesID).Scan(&payload, &action, &sourceHash, &actor) != nil {
		return errors.New("phase 3 immutable roster receipt is missing")
	}
	parsed, digest, err := library.ParseEpisodeRosterPayload(payload)
	if err != nil || digest != sourceHash || digest != detail.Source.SHA256 || parsed.Source != fixture.Roster.Source ||
		!reflect.DeepEqual(parsed.Entries, fixture.Roster.Entries) || action != "replace" || actor != fixture.AdminID {
		return errors.New("phase 3 retained roster payload does not bind the authored source")
	}
	for index, entry := range detail.Entries {
		want := fixture.Roster.Entries[index]
		airing := []string{"aired", "unknown", "unaired"}[index]
		if entry.EpisodeRosterEntryInput != want || entry.ID != library.ExpectedEpisodeID(fixture.SeriesID, fixture.Roster.Source.Key, want.Key) ||
			entry.Airing != airing {
			return errors.New("phase 3 accepted episode identity or date semantics differ")
		}
		available := arrived && want.EpisodeNumber == 2
		if available {
			if entry.Availability != "available" || entry.AvailableItemCount != 1 || !reflect.DeepEqual(entry.AvailableItemIDs, []string{observer.arrivedID}) {
				return errors.New("phase 3 physical arrival did not bind the existing expected fact")
			}
		} else if entry.Availability != "missing" || entry.AvailableItemCount != 0 || len(entry.AvailableItemIDs) != 0 {
			return errors.New("phase 3 missing fact fabricated physical availability")
		}
		var exact bool
		if f.pool.QueryRow(ctx, `SELECT active AND retired_at IS NULL AND import_revision=1 AND entry_key=$2 AND season_number=$3 AND episode_number=$4
			AND name=$5 AND COALESCE(to_char(premiere_date,'YYYY-MM-DD'),'')=$6 FROM expected_episodes WHERE id=$1`,
			entry.ID, want.Key, want.SeasonNumber, want.EpisodeNumber, want.Name, want.PremiereDate).Scan(&exact) != nil || !exact {
			return errors.New("phase 3 persisted episode fact differs from the authored row")
		}
	}
	return nil
}

func (observer *selectedPhase3Observer) filesVerified() ([]selectedPhase2FileFact, error) {
	paths := make([]string, 0, len(observer.files))
	for path := range observer.files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	result := make([]selectedPhase2FileFact, 0, len(paths))
	for _, path := range paths {
		actual, err := selectedPhase2Fact(path, 32<<20)
		if err != nil || actual != observer.files[path] {
			return result, errors.New("phase 3 authored source bytes or identity changed")
		}
		result = append(result, actual)
	}
	return result, nil
}

func (observer *selectedPhase3Observer) snapshot(ctx context.Context, phase, suffix string) (map[string]any, error) {
	f := observer.runtime.f
	var database string
	dbErr := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Preferences',(SELECT jsonb_build_object('Revision',configuration_revision::text,'Configuration',configuration) FROM users WHERE id=$1),
		'Rosters',(SELECT count(*) FROM series_episode_rosters),'ExpectedFacts',(SELECT count(*) FROM expected_episodes),
		'ActiveAuthentication',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'Playback',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'ItemId',item_id,'State',state,'PositionTicks',position_ticks,'Counted',counted,'Started',started_at IS NOT NULL,'Stopped',stopped_at IS NOT NULL) ORDER BY id),'[]'::jsonb) FROM play_sessions),
		'Encodings',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'State',state,'ItemId',item_id,'PlaySessionId',play_session_id) ORDER BY id),'[]'::jsonb) FROM encoding_jobs),
		'ActiveScans',(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
		'ActiveTasks',(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping'))
	)::text`, observer.fixture.ViewerID).Scan(&database)
	files, fileErr := observer.filesVerified()
	value := map[string]any{"Marker": "goby-selected-phase3-stage-database-v1", "RunId": observer.fixture.RunID, "Phase": phase,
		"Observed": dbErr == nil, "Complete": false, "ExpectedFilesVerified": fileErr == nil, "Files": files,
		"Runtime": selectedPhase1RuntimeFacts(f), "ActiveHTTPRequests": observer.runtime.active.Load()}
	if dbErr == nil {
		value["Database"] = json.RawMessage(database)
	}
	if observer.audioObservation != nil {
		value["AudioObservation"] = observer.audioObservation
	}
	if observer.arrivedID != "" {
		value["ArrivedItemId"] = observer.arrivedID
	}
	if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-"+suffix+".json"), value); err != nil {
		return value, errors.New("phase 3 independent observation could not be retained")
	}
	if dbErr != nil {
		return value, errors.New("phase 3 database observation failed")
	}
	return value, fileErr
}

func (observer *selectedPhase3Observer) noVirtualPlays(ctx context.Context) error {
	var invalid int
	if observer.runtime.f.pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE item_id LIKE 'missing-%' OR item_id=ANY($1::text[])`,
		observer.fixture.HiddenItemIDs).Scan(&invalid) != nil || invalid != 0 {
		return errors.New("phase 3 browsing created virtual or denied playback state")
	}
	return nil
}

func (observer *selectedPhase3Observer) music(ctx context.Context) error {
	f, fixture := observer.runtime.f, observer.fixture
	for _, expected := range fixture.Music {
		item, err := f.app.library.GetItem(ctx, fixture.ViewerID, expected.TrackID)
		if err != nil || item.Type != "Audio" || item.Album == nil || item.Album.ID != expected.AlbumID || item.Name != expected.TrackName ||
			item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion || len(item.Entities.Artists) != 1 ||
			strconv.FormatInt(item.Entities.Artists[0].ID, 10) != expected.ArtistID || len(item.Entities.AlbumArtists) != 1 ||
			strconv.FormatInt(item.Entities.AlbumArtists[0].ID, 10) != expected.AlbumArtistID || len(item.Entities.Genres) != 1 ||
			strconv.FormatInt(item.Entities.Genres[0].ID, 10) != expected.GenreID {
			return errors.New("phase 3 music navigation does not bind the scanned associations")
		}
	}
	var members []string
	if f.pool.QueryRow(ctx, "SELECT array_agg(item_id ORDER BY position,id) FROM media_collection_entries WHERE collection_id=$1",
		fixture.PlaylistID).Scan(&members) != nil || len(members) != 3 || members[0] != fixture.Music[0].TrackID ||
		members[1] != fixture.Music[0].TrackID || !slices.Contains(fixture.HiddenItemIDs, members[2]) {
		return errors.New("phase 3 playlist did not retain duplicate and denied source members")
	}
	visible, err := f.app.library.CollectionItems(ctx, library.Subject{UserID: fixture.ViewerID}, fixture.PlaylistID, library.PlaylistKind, 0, 100)
	if err != nil || len(visible.Items) != 2 || visible.TotalRecordCount != 2 || visible.Items[0].ID != fixture.Music[0].TrackID ||
		visible.Items[1].ID != fixture.Music[0].TrackID {
		return errors.New("phase 3 shared private playlist leaked denied members or erased real duplicates")
	}
	return nil
}

func (observer *selectedPhase3Observer) image(ctx context.Context, expectedHash string) (resultErr error) {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(expectedHash) {
		return errors.New("phase 3 browser did not report a real image digest")
	}
	f, fixture := observer.runtime.f, observer.fixture
	credentials, err := f.users.Authenticate(ctx, fixture.ViewerName, fixture.ViewerPassword,
		identity.Client{Name: "Phase 3 image observer", DeviceID: "phase3-image-observer", Device: "Linux", Version: "1"}, "emby")
	if err != nil {
		return errors.New("phase 3 independent image observer could not authenticate")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := f.users.Revoke(cleanupCtx, credentials.Token); err != nil {
			resultErr = errors.New("phase 3 image observer authentication did not retire")
		}
	}()
	client := &http.Client{Timeout: 10 * time.Second}
	body, _, err := selectedPhase2HTTP(ctx, client, fixture.BaseURL, "/emby/Items/"+fixture.Music[0].TrackID+"/Images/Primary/0",
		credentials.Token)
	client.CloseIdleConnections()
	if err != nil {
		return errors.New("phase 3 independent actual image HTTP read failed")
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != expectedHash {
		return errors.New("phase 3 browser image bytes differ from the actual owner image")
	}
	return nil
}

func (observer *selectedPhase3Observer) audioScope(request selectedPhase3Request) bool {
	return regexp.MustCompile(`^play_[0-9a-f]{32}$`).MatchString(request.PlaySessionID) &&
		request.DeviceID == "phase3-adapter-"+observer.fixture.RunID && request.AudioItemID == observer.fixture.Music[0].TrackID &&
		request.MediaSourceID == media.SourceID(observer.fixture.Music[0].TrackID) && request.PositionTicks >= 20_000_000 && request.PositionTicks <= 82_000_000 &&
		(observer.playID == "" || observer.playID == request.PlaySessionID && observer.playDevice == request.DeviceID && observer.playSource == request.MediaSourceID)
}

func (observer *selectedPhase3Observer) audioPlaying(ctx context.Context, request selectedPhase3Request) error {
	if !observer.audioScope(request) {
		return errors.New("phase 3 audio scope or real progress is invalid")
	}
	f, fixture := observer.runtime.f, observer.fixture
	var authID, state string
	var started, stopped, counted, authValid, current, correlated bool
	var position int64
	if f.pool.QueryRow(ctx, `SELECT p.auth_session_id,p.state,p.started_at IS NOT NULL,p.stopped_at IS NOT NULL,p.counted,
		a.revoked_at IS NULL AND a.expires_at>clock_timestamp(),p.expires_at>clock_timestamp(),p.client_correlated,p.position_ticks
		FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$1 AND p.user_id=$2 AND p.device_id=$3
		AND p.item_id=$4 AND p.media_source_id=$5 AND NOT p.is_dynamic AND p.application_client_id IS NULL
		AND a.user_id=$2 AND a.device_id=$3 AND a.kind='emby'`, request.PlaySessionID, fixture.ViewerID, request.DeviceID,
		request.AudioItemID, request.MediaSourceID).Scan(&authID, &state, &started, &stopped, &counted, &authValid, &current, &correlated, &position) != nil ||
		state != "Playing" || !started || stopped || !counted || !authValid || !current || position != request.PositionTicks {
		return errors.New("phase 3 actual audio did not persist its scoped start and measured progress")
	}
	var playCount, totalPlays int
	if f.pool.QueryRow(ctx, "SELECT play_count FROM user_item_data WHERE user_id=$1 AND item_id=$2", fixture.ViewerID, request.AudioItemID).Scan(&playCount) != nil ||
		f.pool.QueryRow(ctx, "SELECT count(*) FROM play_sessions").Scan(&totalPlays) != nil || playCount != 1 || totalPlays != 1 {
		return errors.New("phase 3 audio browsing or reports duplicated actual playback state")
	}
	f.app.hls.mu.Lock()
	sessions := []*hlsSession{}
	for _, session := range f.app.hls.sessions {
		if session.key.scope.PlaySessionID == request.PlaySessionID && session.key.scope.AuthSessionID == authID {
			sessions = append(sessions, session)
		}
	}
	f.app.hls.mu.Unlock()
	if len(sessions) != 1 {
		return errors.New("phase 3 audio did not create exactly one real progressive session")
	}
	session := sessions[0]
	if session.key.scope.ItemID != request.AudioItemID || session.key.scope.SourceID != request.MediaSourceID ||
		session.key.scope.UserID != fixture.ViewerID || session.key.scope.DeviceID != request.DeviceID ||
		session.key.plan.OutputMode != "progressive" || session.key.plan.Container != "mp3" ||
		session.key.plan.AudioCodec != "mp3" || session.key.plan.VideoStreamIndex != -1 || session.key.plan.StartTicks != 0 {
		return errors.New("phase 3 actual audio producer does not bind the negotiated source and encode plan")
	}
	session.mu.Lock()
	closed, readers := session.closed, session.progressiveReaders
	producers := append([]hlsProducer(nil), session.producers...)
	session.mu.Unlock()
	if closed || session.ctx.Err() != nil || len(producers) != 1 {
		return errors.New("phase 3 audio producer is absent or duplicated")
	}
	ids, states := []string{}, []string{}
	for _, producer := range producers {
		job, err := f.app.hls.manager.Snapshot(session.key.scope, producer.id)
		cache, cacheErr := os.Lstat(filepath.Join(f.cfg.Transcoding.CacheDirectory, producer.id))
		if err != nil || (job.State != "running" && job.State != "completed") || job.OutputBytes <= 0 || job.ErrorCode != "" ||
			cacheErr != nil || !cache.IsDir() {
			return errors.New("phase 3 progressive producer has no real usable output")
		}
		ids, states = append(ids, job.ID), append(states, job.State)
	}
	var jobs int
	if f.pool.QueryRow(ctx, `SELECT count(*) FROM encoding_jobs WHERE play_session_id=$1 AND auth_session_id=$2 AND item_id=$3 AND user_id=$4 AND device_id=$5`,
		request.PlaySessionID, authID, request.AudioItemID, fixture.ViewerID, request.DeviceID).Scan(&jobs) != nil || jobs != 1 {
		return errors.New("phase 3 audio producer history differs from the actual runtime")
	}
	observer.playID, observer.playAuth, observer.playDevice, observer.playSource, observer.playPosition =
		request.PlaySessionID, authID, request.DeviceID, request.MediaSourceID, position
	observer.playSessions, observer.producerIDs = sessions, ids
	observer.audioObservation = map[string]any{"Complete": true, "Phase": request.Phase, "PlaySessionId": request.PlaySessionID,
		"ProducerIds": ids, "ProducerStates": states, "State": state, "PositionTicks": position, "Counted": counted,
		"ClientCorrelated": correlated, "ProgressiveReaders": readers, "ActualSourcePlanVerified": true}
	return nil
}

func (observer *selectedPhase3Observer) audioStopped(ctx context.Context, request selectedPhase3Request) error {
	if observer.playID == "" || !observer.audioScope(request) || request.PositionTicks < observer.playPosition {
		return errors.New("phase 3 stop does not bind the observed live playback")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	f := observer.runtime.f
	for {
		var terminal bool
		var playCount, jobs, activeJobs, viewerSessions int
		err := f.pool.QueryRow(ctx, `SELECT p.state='Stopped' AND p.started_at IS NOT NULL AND p.stopped_at IS NOT NULL AND p.counted
			AND p.position_ticks=$7 AND p.expires_at<=clock_timestamp() AND a.revoked_at IS NULL AND a.expires_at>clock_timestamp()
			FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$1 AND p.auth_session_id=$2 AND p.user_id=$3 AND p.device_id=$4
			AND p.item_id=$5 AND p.media_source_id=$6`, observer.playID, observer.playAuth, observer.fixture.ViewerID, observer.playDevice,
			request.AudioItemID, observer.playSource, request.PositionTicks).Scan(&terminal)
		if err != nil {
			return errors.New("phase 3 terminal playback observation failed")
		}
		if f.pool.QueryRow(ctx, "SELECT play_count FROM user_item_data WHERE user_id=$1 AND item_id=$2", observer.fixture.ViewerID, request.AudioItemID).Scan(&playCount) != nil ||
			f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM encoding_jobs),(SELECT count(*) FROM encoding_jobs WHERE state NOT IN ('completed','cancelled')),
				(SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL)`, observer.fixture.ViewerID).
				Scan(&jobs, &activeJobs, &viewerSessions) != nil {
			return errors.New("phase 3 terminal audio history observation failed")
		}
		resources := selectedPhase1RuntimeFacts(f)
		closed := true
		for _, count := range resources {
			closed = closed && count == 0
		}
		for _, session := range observer.playSessions {
			session.mu.Lock()
			closed = closed && session.closed && session.progressiveReaders == 0 && session.ctx.Err() != nil
			session.mu.Unlock()
		}
		cacheRetired := true
		for _, id := range observer.producerIDs {
			if _, err := os.Lstat(filepath.Join(f.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
				cacheRetired = false
			}
		}
		observer.audioObservation = map[string]any{"Complete": false, "Phase": request.Phase, "PlaySessionId": observer.playID, "TerminalPlayback": terminal,
			"PlayCount": playCount, "PreservedProducerRows": jobs, "InvalidOrActiveJobs": activeJobs, "ActiveViewerSessions": viewerSessions,
			"Runtime": resources, "ProducerContextsClosed": closed, "CacheRetired": cacheRetired, "PositionTicks": request.PositionTicks}
		if terminal && playCount == 1 && jobs == 1 && activeJobs == 0 && viewerSessions == 1 && closed && cacheRetired {
			observer.playPosition = request.PositionTicks
			observer.audioObservation["Complete"] = true
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 3 real audio resources did not retire before fixture shutdown")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (observer *selectedPhase3Observer) stage(ctx context.Context, request selectedPhase3Request) error {
	f, fixture := observer.runtime.f, observer.fixture
	switch request.Phase {
	case "authentication":
		var native int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1 AND kind='admin' AND revoked_at IS NULL", fixture.AdminID).Scan(&native) != nil || native != 2 {
			return errors.New("phase 3 native browser did not authenticate independently from its observer")
		}
		if err := observer.preferences(ctx, false, ""); err != nil {
			return err
		}
		if err := observer.roster(ctx, false, false); err != nil {
			return err
		}
	case "preferences":
		old, err := strconv.ParseInt(observer.preferencesRevision, 10, 64)
		if err != nil || request.PreferencesRevision != strconv.FormatInt(old+1, 10) {
			return errors.New("phase 3 native preferences did not use one durable CAS update")
		}
		if err := observer.preferences(ctx, true, request.PreferencesRevision); err != nil {
			return err
		}
		observer.preferencesRevision = request.PreferencesRevision
	case "roster-reviewed":
		if request.SeriesID != fixture.SeriesID || request.LoadedRevision != "0" {
			return errors.New("phase 3 roster preview does not bind the initial native load")
		}
		if err := observer.roster(ctx, false, false); err != nil {
			return err
		}
	case "roster-saved":
		if request.SeriesID != fixture.SeriesID || request.RosterRevision != "1" || !selectedPhase3SameIDs(request.MissingIDs, observer.expectedIDs(false)) {
			return errors.New("phase 3 native roster save did not return the exact authored facts")
		}
		observer.rosterRevision = request.RosterRevision
		if err := observer.roster(ctx, true, false); err != nil {
			return err
		}
	case "missing-queries":
		if !selectedPhase3SameIDs(request.MissingIDs, observer.expectedIDs(false)) {
			return errors.New("phase 3 missing query did not observe all three explicit facts")
		}
		for _, id := range request.QueryItemIDs {
			if slices.Contains(fixture.HiddenItemIDs, id) {
				return errors.New("phase 3 missing query returned a denied item")
			}
		}
		if err := observer.roster(ctx, true, false); err != nil {
			return err
		}
	case "suggestions-played":
		if request.PlayedItemID != fixture.SuggestionAID || request.UnplayedItemID != fixture.SuggestionBID {
			return errors.New("phase 3 suggestions state selected another source")
		}
		var played, unplayed bool
		if f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_item_data WHERE user_id=$1 AND item_id=$2 AND played),
			NOT EXISTS(SELECT 1 FROM user_item_data WHERE user_id=$1 AND item_id=$3 AND played)`,
			fixture.ViewerID, fixture.SuggestionAID, fixture.SuggestionBID).Scan(&played, &unplayed) != nil || !played || !unplayed {
			return errors.New("phase 3 actual played mutations did not control Suggestions")
		}
	case "music-navigation":
		first := fixture.Music[0]
		if request.ArtistID != first.ArtistID || request.AlbumArtistID != first.AlbumArtistID || request.AlbumID != first.AlbumID || request.TrackID != first.TrackID {
			return errors.New("phase 3 clicked music navigation selected another relationship")
		}
		if err := observer.music(ctx); err != nil {
			return err
		}
	case "music-mixes":
		ids := []string{}
		for _, item := range fixture.Music {
			ids = append(ids, item.TrackID)
		}
		if request.AudioItemID != fixture.Music[0].TrackID || !selectedPhase3SameIDs(request.MixTrackIDs, ids) {
			return errors.New("phase 3 actual mix did not retain the exact finite authorized track population")
		}
		if err := observer.music(ctx); err != nil {
			return err
		}
		var plays, jobs int
		if f.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM play_sessions),(SELECT count(*) FROM encoding_jobs)").Scan(&plays, &jobs) != nil || plays != 0 || jobs != 0 {
			return errors.New("phase 3 discovery created playback or encoding state")
		}
	case "search-navigation":
		if request.PhysicalDetailID != fixture.Music[0].TrackID || request.EntityDetailID != fixture.Music[0].ArtistID {
			return errors.New("phase 3 search click-through reached another owner")
		}
		if err := observer.image(ctx, request.ImageSHA256); err != nil {
			return err
		}
	case "audio-playing":
		if err := observer.audioPlaying(ctx, request); err != nil {
			return err
		}
	case "audio-stopped":
		if err := observer.audioStopped(ctx, request); err != nil {
			return err
		}
	case "physical-arrival":
		if observer.arrivedID != "" {
			return errors.New("phase 3 attempted to replay a physical source arrival")
		}
		if err := selectedPhase3Copy(observer.arrivalSource, observer.arrivalPath); err != nil {
			return err
		}
		fact, err := selectedPhase2Fact(observer.arrivalPath, 32<<20)
		if err != nil {
			return errors.New("phase 3 arrived source fingerprint could not be observed")
		}
		observer.files[observer.arrivalPath] = fact
		if err := selectedPhase3Scan(ctx, f, fixture.TelevisionLibraryID, 4); err != nil {
			return err
		}
		if f.pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode' AND NOT is_folder AND media IS NOT NULL
			AND index_number=2 AND parent_index_number=1`, observer.arrivalPath).Scan(&observer.arrivedID) != nil || observer.arrivedID == fixture.InitialEpisodeID {
			return errors.New("phase 3 physical arrival did not create the actual second episode")
		}
		if err := observer.roster(ctx, true, true); err != nil {
			return err
		}
	case "arrival-queries":
		if request.ArrivedItemID != observer.arrivedID || !selectedPhase3SameIDs(request.RemainingMissingIDs, observer.expectedIDs(true)) {
			return errors.New("phase 3 post-arrival queries did not separate physical availability from remaining facts")
		}
		if err := observer.roster(ctx, true, true); err != nil {
			return err
		}
	case "restart":
		if err := observer.preferences(ctx, true, observer.preferencesRevision); err != nil {
			return err
		}
		if err := observer.roster(ctx, true, true); err != nil {
			return err
		}
		var err error
		observer.durable, err = selectedPhase3Durable(ctx, f)
		if err != nil {
			return errors.New("phase 3 restart baseline could not be captured")
		}
		if err := featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-before.json"), observer.durable); err != nil {
			return errors.New("phase 3 restart baseline could not be retained")
		}
		if err := observer.runtime.restart(ctx); err != nil {
			return errors.New("phase 3 complete runtime restart failed")
		}
		current, err := selectedPhase3Durable(ctx, f)
		if err != nil || !bytes.Equal(current, observer.durable) {
			return errors.New("phase 3 restart changed accepted facts or replayed source preparation")
		}
	case "persisted":
		if request.RosterRevision != observer.rosterRevision || !selectedPhase3SameIDs(request.RemainingMissingIDs, observer.expectedIDs(true)) {
			return errors.New("phase 3 native restart review differs from the saved source")
		}
		if err := observer.preferences(ctx, true, observer.preferencesRevision); err != nil {
			return err
		}
		if err := observer.roster(ctx, true, true); err != nil {
			return err
		}
		if err := observer.music(ctx); err != nil {
			return err
		}
		current, err := selectedPhase3Durable(ctx, f)
		if err != nil || !bytes.Equal(current, observer.durable) {
			return errors.New("phase 3 post-restart reads changed the exact durable snapshot")
		}
		if err := featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-after.json"), current); err != nil {
			return errors.New("phase 3 restart readback could not be retained")
		}
	case "cleanup":
		if err := f.users.Revoke(ctx, observer.actorToken); err != nil {
			return errors.New("phase 3 observer authentication could not retire")
		}
		var sessions, plays, terminal, jobs, activeJobs, scans, tasks int
		if f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM play_sessions),
			(SELECT count(*) FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$1 AND p.state='Stopped' AND p.counted AND p.stopped_at IS NOT NULL AND a.revoked_at IS NOT NULL),
			(SELECT count(*) FROM encoding_jobs),(SELECT count(*) FROM encoding_jobs WHERE state NOT IN ('completed','cancelled')),
			(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping'))`,
			observer.playID).Scan(&sessions, &plays, &terminal, &jobs, &activeJobs, &scans, &tasks) != nil ||
			sessions != 0 || plays != 1 || terminal != 1 || jobs != 1 || activeJobs != 0 || scans != 0 || tasks != 0 {
			return errors.New("phase 3 normal logout did not close authentication and preserve exact terminal history")
		}
		if err := observer.runtime.close(ctx); err != nil {
			return errors.New("phase 3 final runtime workers did not join")
		}
		for _, count := range selectedPhase1RuntimeFacts(f) {
			if count != 0 {
				return errors.New("phase 3 final playback resources remain")
			}
		}
		if observer.runtime.active.Load() != 0 {
			return errors.New("phase 3 final HTTP requests remain")
		}
	}
	if err := observer.noVirtualPlays(ctx); err != nil {
		return err
	}
	preserved, err := observer.preservedEpisode(ctx)
	if err != nil || !bytes.Equal(preserved, observer.initialEpisode) {
		return errors.New("phase 3 changed the initial physical episode or its complete user data")
	}
	if _, err := observer.filesVerified(); err != nil {
		return err
	}
	return nil
}

func selectedPhase3Failure(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "phase 3 ") {
		// These fixture-owned messages are fixed text and contain no input data.
		return err.Error()
	}
	return selectedPhase2SafeError(err)
}

func (observer *selectedPhase3Observer) run(ctx context.Context) error {
	for _, phase := range selectedPhase3Phases {
		value := map[string]any{"Marker": "goby-selected-phase3-stage-database-v1", "RunId": observer.fixture.RunID, "Phase": phase, "Observed": false, "Complete": false}
		ack := filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json")
		stageErr := phase3BrowserWaitRequest(ctx, phase3BrowserContext{RunID: observer.fixture.RunID, ArtifactsDir: observer.fixture.ArtifactsDir}, phase)
		var request selectedPhase3Request
		if stageErr == nil {
			if err := featureWavePrivateJSON(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-request.json"), 64<<10, &request); err != nil ||
				request.RunID != observer.fixture.RunID || request.Phase != phase {
				stageErr = errors.New("phase 3 stage request does not bind its run and phase")
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
			value["Failed"], value["ErrorCode"], value["FailureDetail"] = true, "database_stage_verification_failed", selectedPhase3Failure(stageErr)
			_ = featureWaveWriteCheckpoint(ack, value)
			return stageErr
		}
		value["Complete"], value["Observed"], value["ExpectedFilesVerified"] = true, true, true
		if err := featureWaveWriteCheckpoint(ack, value); err != nil {
			return errors.New("phase 3 stage acknowledgement could not be retained")
		}
		observer.completed++
	}
	return nil
}

func TestSelectedCompatibilityPhase3BrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("phase 3 browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	execution, inventory := selectedPhase3ReadExecution(t)
	runID := os.Getenv("GOBY_SELECTED_PHASE3_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("phase 3 requires an explicit owned browser run identity")
	}
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("phase 3 artifact parent must be private")
	}
	cwd, err := os.Getwd()
	if err != nil {
		selectedPhase2Fatal(t, "read phase 3 source directory", err)
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("phase 3 source root was not found")
	}
	driverPath := filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase3-browser.mjs")
	for _, path := range []string{node, playwright, driverPath} {
		fact, err := selectedPhase2Fact(path, 256<<20)
		if err != nil {
			t.Fatal("phase 3 browser input identity or ownership is invalid")
		}
		inventory = append(inventory, fact)
	}
	output, err := os.MkdirTemp(artifacts, "selected-phase3-browser-")
	if err != nil {
		selectedPhase2Fatal(t, "create phase 3 private artifact directory", err)
	}
	if err := os.Chmod(output, 0o700); err != nil {
		selectedPhase2Fatal(t, "protect phase 3 artifacts", err)
	}
	driver := map[string]any{"Marker": "goby-selected-phase3-browser-driver-v1", "RunId": runID, "Complete": false,
		"ArtifactDirectory": output, "CredentialsWrittenToSummary": false, "RealProber": true, "OriginalClientUsed": false}
	var schema, mediaRoot string
	t.Cleanup(func() {
		if schema != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe phase 3 owned schema cleanup")
			} else {
				defer pool.Close()
				var remains bool
				if pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains) != nil || remains {
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
			t.Error("preserve phase 3 independent driver result")
		}
	})
	if err := refreshBrowserWriteJSON(filepath.Join(output, "execution-inventory.json"), inventory); err != nil {
		selectedPhase2Fatal(t, "retain phase 3 tool inventory", err)
	}
	f := newServerFixtureWithTimeout(t, 15*time.Minute)
	if err := f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		selectedPhase2Fatal(t, "identify phase 3 owned schema", err)
	}
	mediaRoot = t.TempDir()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		// Retain only the bounded authored media tree, never private contexts,
		// configuration, session tokens, or connection strings.
		facts, err := selectedPhase3Files(mediaRoot)
		if err != nil {
			driver["FailedSourceCapture"] = false
			return
		}
		total := int64(0)
		for _, fact := range facts {
			total += fact.Bytes
		}
		if total > 128<<20 {
			driver["FailedSourceCapture"] = false
			return
		}
		destination := filepath.Join(output, "failed-sources")
		for path := range facts {
			relative, err := filepath.Rel(mediaRoot, path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				driver["FailedSourceCapture"] = false
				return
			}
			target := filepath.Join(destination, relative)
			if os.MkdirAll(filepath.Dir(target), 0o700) != nil || selectedPhase3Copy(path, target) != nil {
				driver["FailedSourceCapture"] = false
				return
			}
		}
		driver["FailedSourceCapture"], driver["FailedSourceBytes"] = true, total
	})
	arrivalSource, videoPaths, audioPaths := selectedPhase3Author(t, execution, mediaRoot)
	files, err := selectedPhase3Files(mediaRoot)
	if err != nil {
		selectedPhase2Fatal(t, "capture phase 3 owned source identities", err)
	}
	if err := f.app.Close(f.ctx); err != nil {
		selectedPhase2Fatal(t, "close initial phase 3 application", err)
	}
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = execution.FFmpegPath, execution.FFprobePath, []string{mediaRoot}
	f.cfg.Transcoding = config.TranscodingConfig{Enabled: true, CacheDirectory: t.TempDir(), Threads: 1, MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2,
		MaxQueueJobs: 8, MaxRetainedJobs: 32, MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
		MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}
	assets, err := adminassets.Files()
	if err != nil {
		selectedPhase2Fatal(t, "open phase 3 embedded native assets", err)
	}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "selected-phase3-browser-integration", WithDashboardAssets(assets))
	if err != nil {
		selectedPhase2Fatal(t, "create actual phase 3 application", err)
	}
	f.app, f.handler = app, app.Handler()
	runtime := &selectedPhase3Runtime{phase3BrowserRuntime: &phase3BrowserRuntime{f: f, assets: assets, addr: "127.0.0.1:0"}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe phase 3 residual authentication")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("retire phase 3 residual owned authentication")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
			if driver["Complete"] == true && result.RowsAffected() != 0 {
				t.Error("successful phase 3 browser required fallback authentication cleanup")
			}
		}
		if runtime.close(ctx) != nil {
			t.Error("close phase 3 actual runtime workers")
		} else {
			driver["ServerWorkersClosed"], driver["HTTPListenerClosed"] = true, true
		}
		driver["ActiveHTTPRequestsAfterClose"] = runtime.active.Load()
		if runtime.active.Load() != 0 {
			t.Error("phase 3 HTTP requests remain after worker shutdown")
		}
	})
	adminPassword, viewerPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Selected phase 3 administrator", adminPassword)
	if err != nil {
		selectedPhase2Fatal(t, "bootstrap phase 3 native administrator", err)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Selected phase 3 viewer", viewerPassword, false)
	if err != nil {
		selectedPhase2Fatal(t, "create phase 3 viewer", err)
	}
	credentials, err := f.users.Authenticate(f.ctx, admin.Name, adminPassword,
		identity.Client{Name: "Phase 3 fixture observer", DeviceID: "phase3-fixture-observer", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "authenticate phase 3 observer", err)
	}
	actor, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "resolve phase 3 observer", err)
	}
	libraries := []library.Library{}
	for _, spec := range []struct{ name, kind, dir string }{
		{"Phase Three Television", "tvshows", "tv"}, {"Phase Three Movies", "movies", "movies"}, {"Phase Three Music", "music", "music"},
		{"Phase Three Denied Movies", "movies", "hidden-movies"}, {"Phase Three Denied Music", "music", "hidden-music"},
	} {
		created, err := app.library.CreateLibrary(f.ctx, spec.name, spec.kind, []string{filepath.Join(mediaRoot, spec.dir)})
		if err != nil {
			selectedPhase2Fatal(t, "create phase 3 owned library", err)
		}
		libraries = append(libraries, created)
	}
	for index, count := range []int{3, 2, 3, 1, 2} {
		if err := selectedPhase3Scan(f.ctx, f, libraries[index].ID, count); err != nil {
			selectedPhase2Fatal(t, "scan phase 3 actual authored sources", err)
		}
	}
	firstEpisode := selectedPhase3Item(t, f, admin.ID, videoPaths[3], "Episode")
	unknownEpisode := selectedPhase3Item(t, f, admin.ID, videoPaths[4], "Episode")
	if firstEpisode.Series == nil || unknownEpisode.Series == nil || firstEpisode.Series.ID == unknownEpisode.Series.ID {
		t.Fatal("phase 3 television parents were not scanned")
	}
	firstMovie, secondMovie, hiddenMovie := selectedPhase3Item(t, f, admin.ID, videoPaths[0], "Movie"),
		selectedPhase3Item(t, f, admin.ID, videoPaths[1], "Movie"), selectedPhase3Item(t, f, admin.ID, videoPaths[2], "Movie")
	tracks := []library.Item{}
	for _, path := range audioPaths {
		tracks = append(tracks, selectedPhase3Item(t, f, admin.ID, path, "Audio"))
	}
	owner := library.Subject{UserID: admin.ID, Actor: &actor}
	collectionCtx := library.WithCollectionActor(f.ctx, actor)
	playlist, err := app.library.CreateCollection(collectionCtx, owner, library.PlaylistKind, library.CollectionInput{
		Name: "Phase Three private repeated playlist", MediaType: "Audio", IsPublic: false, ItemIDs: []string{tracks[0].ID, tracks[0].ID, tracks[3].ID}})
	if err != nil {
		selectedPhase2Fatal(t, "create phase 3 real repeated private playlist", err)
	}
	shares := []library.CollectionShare{{UserID: viewer.ID, CanEdit: false}}
	if _, err := app.library.UpdateCollection(collectionCtx, owner, playlist.ID, library.PlaylistKind, library.CollectionPatch{Shares: &shares}); err != nil {
		selectedPhase2Fatal(t, "share phase 3 private playlist for authorized discovery", err)
	}
	visibleLibraries := []string{libraries[0].ID, libraries[1].ID, libraries[2].ID}
	policy, _ := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": visibleLibraries, "EnableMediaPlayback": true,
		"EnableAudioPlaybackTranscoding": true, "EnableVideoPlaybackTranscoding": true, "EnablePlaybackRemuxing": true})
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
		selectedPhase2Fatal(t, "set phase 3 viewer source scope", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at)
		VALUES($1,$2,30000000,3,true,false,'2026-01-02T03:04:05Z')`, viewer.ID, firstEpisode.ID); err != nil {
		selectedPhase2Fatal(t, "seed phase 3 preserved initial episode state", err)
	}
	fixture := selectedPhase3Context{Marker: "goby-selected-phase3-browser-fixture-v1", RunID: runID,
		AdminID: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword, ViewerID: viewer.ID, ViewerName: viewer.Name, ViewerPassword: viewerPassword,
		TelevisionLibraryID: libraries[0].ID, MovieLibraryID: libraries[1].ID, MusicLibraryID: libraries[2].ID,
		SeriesID: firstEpisode.Series.ID, SeriesName: firstEpisode.Series.Name, InitialEpisodeID: firstEpisode.ID,
		UnknownSeriesID: unknownEpisode.Series.ID, UnknownSeriesName: unknownEpisode.Series.Name, SuggestionAID: firstMovie.ID, SuggestionBID: secondMovie.ID,
		HiddenItemIDs: []string{hiddenMovie.ID}, HiddenEntityIDs: []string{}, Music: []selectedPhase3Music{}, PlaylistID: playlist.ID,
		SearchTerm: "Phase Three", ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	for _, track := range tracks[:3] {
		if track.Album == nil || len(track.Entities.Artists) != 1 || len(track.Entities.AlbumArtists) != 1 || len(track.Entities.Genres) != 1 {
			t.Fatal("phase 3 music tags did not create all required real relationships")
		}
		fixture.Music = append(fixture.Music, selectedPhase3Music{TrackID: track.ID, TrackName: track.Name, AlbumID: track.Album.ID, AlbumName: track.Album.Name,
			ArtistID: strconv.FormatInt(track.Entities.Artists[0].ID, 10), ArtistName: track.Entities.Artists[0].Name,
			AlbumArtistID: strconv.FormatInt(track.Entities.AlbumArtists[0].ID, 10), AlbumArtistName: track.Entities.AlbumArtists[0].Name,
			GenreID: strconv.FormatInt(track.Entities.Genres[0].ID, 10), GenreName: track.Entities.Genres[0].Name})
	}
	if fixture.Music[0].ArtistID != fixture.Music[1].ArtistID || fixture.Music[0].AlbumArtistID != fixture.Music[1].AlbumArtistID ||
		fixture.Music[0].GenreID != fixture.Music[1].GenreID || fixture.Music[0].ArtistID == fixture.Music[2].ArtistID ||
		fixture.Music[0].ArtistID == fixture.Music[0].AlbumArtistID {
		t.Fatal("phase 3 music seed population lost shared and distinct credit identities")
	}
	var hidden []string
	if f.pool.QueryRow(f.ctx, "SELECT COALESCE(array_agg(id ORDER BY id),'{}'::text[]) FROM items WHERE library_id=ANY($1::text[])",
		[]string{libraries[3].ID, libraries[4].ID}).Scan(&hidden) != nil {
		t.Fatal("phase 3 denied physical source identities could not be captured")
	}
	for _, id := range hidden {
		if id != hiddenMovie.ID {
			fixture.HiddenItemIDs = append(fixture.HiddenItemIDs, id)
		}
	}
	if f.pool.QueryRow(f.ctx, `SELECT COALESCE(array_agg(e.id::text ORDER BY e.id),'{}'::text[]) FROM catalog_entities e
		WHERE EXISTS(SELECT 1 FROM item_entities a JOIN items i ON i.id=a.item_id WHERE a.entity_id=e.id AND i.library_id=ANY($1::text[]))
		AND NOT EXISTS(SELECT 1 FROM item_entities a JOIN items i ON i.id=a.item_id WHERE a.entity_id=e.id AND i.library_id=ANY($2::text[]))`,
		[]string{libraries[3].ID, libraries[4].ID}, visibleLibraries).Scan(&fixture.HiddenEntityIDs) != nil || len(fixture.HiddenEntityIDs) < 3 {
		t.Fatal("phase 3 exclusively denied entity population could not be captured")
	}
	fixture.Roster.Source = library.EpisodeRosterSourceInput{Key: "phase3-owned-manifest", Label: "Owned phase 3 expected episodes", Revision: "v1"}
	fixture.Roster.Entries = []library.EpisodeRosterEntryInput{
		{Key: "owned-s01e02", SeasonNumber: 1, EpisodeNumber: 2, Name: "Phase Three roster past episode", PremiereDate: "2020-01-01"},
		{Key: "owned-s01e03", SeasonNumber: 1, EpisodeNumber: 3, Name: "Phase Three roster unknown episode"},
		{Key: "owned-s01e04", SeasonNumber: 1, EpisodeNumber: 4, Name: "Phase Three roster future episode", PremiereDate: "2099-01-01"},
	}
	var embeddedCover int
	if f.pool.QueryRow(f.ctx, "SELECT count(*) FROM item_embedded_artwork WHERE item_id=$1", tracks[0].ID).Scan(&embeddedCover) != nil || embeddedCover != 1 {
		t.Fatal("phase 3 first playable track has no real extracted embedded cover")
	}
	embedded, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		selectedPhase2Fatal(t, "inventory phase 3 embedded assets", err)
	}
	frozen, err := refreshBrowserAssetInventory(os.DirFS(filepath.Join(sourceRoot, "web", "admin", "dist")))
	if err != nil || !reflect.DeepEqual(embedded, frozen) {
		t.Fatal("phase 3 embedded native assets differ from frozen source assets")
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embedded); err != nil {
		selectedPhase2Fatal(t, "retain phase 3 frozen asset inventory", err)
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	if err := runtime.listen(); err != nil {
		selectedPhase2Fatal(t, "start phase 3 owned TCP4 listener", err)
	}
	fixture.BaseURL = "http://" + runtime.addr
	observer := &selectedPhase3Observer{runtime: runtime, fixture: fixture, actor: actor, actorToken: credentials.Token, files: files,
		arrivalSource: arrivalSource, arrivalPath: filepath.Join(filepath.Dir(videoPaths[3]), "Phase.Three.Known.Show.S01E02.mp4")}
	if f.pool.QueryRow(f.ctx, "SELECT configuration,configuration_revision::text FROM users WHERE id=$1", viewer.ID).
		Scan(&observer.initialPreferences, &observer.preferencesRevision) != nil {
		t.Fatal("phase 3 initial preference baseline could not be captured")
	}
	observer.initialEpisode, err = observer.preservedEpisode(f.ctx)
	if err != nil {
		selectedPhase2Fatal(t, "capture phase 3 initial episode preservation baseline", err)
	}
	if _, err := observer.snapshot(f.ctx, "seeded", "database"); err != nil {
		selectedPhase2Fatal(t, "retain phase 3 actual seed evidence", err)
	}
	contextPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove phase 3 private browser credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if err := refreshBrowserWriteJSON(contextPath, fixture); err != nil {
		selectedPhase2Fatal(t, "write private phase 3 browser context", err)
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if err := os.Mkdir(filepath.Join(output, name), 0o700); err != nil {
			selectedPhase2Fatal(t, "create phase 3 private browser directories", err)
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright,
		"GOBY_SELECTED_PHASE3_RUN_ID=" + runID, "GOBY_SELECTED_PHASE3_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Minute)
	defer cancel()
	observerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{driverPath}, environment, sourceRoot, output)
	stop()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	if observerErr != nil && observer.completed < len(selectedPhase3Phases) {
		driver["FailedDatabaseStage"], driver["DatabaseObservationError"] = selectedPhase3Phases[observer.completed], selectedPhase3Failure(observerErr)
	}
	var result selectedPhase2Result
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(selectedPhase3Phases) {
		t.Fatalf("phase 3 browser or observer failed: command=%s observer=%s result=%s completed_stages=%d; inspect retained private artifacts",
			selectedPhase2SafeError(commandErr), selectedPhase2SafeError(observerErr), selectedPhase2SafeError(readErr), observer.completed)
	}
	checks := []string{"Authentication", "Preferences", "RosterReviewed", "RosterSaved", "MissingQueries", "SuggestionsPlayed", "MusicNavigation",
		"MusicMixes", "SearchNavigation", "AudioPlaying", "AudioStopped", "PhysicalArrival", "ArrivalQueries", "Restart", "Persisted", "Cleanup"}
	if result.Marker != "goby-selected-phase3-browser-result-v1" || result.RunID != runID || !result.Complete || len(result.Checks) != len(checks) ||
		len(result.Stages) != len(selectedPhase3Phases) || result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("phase 3 browser result does not bind the complete actual scenario")
	}
	for index, phase := range selectedPhase3Phases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" || !result.Checks[checks[index]] {
			t.Fatal("phase 3 browser and database stages do not agree")
		}
	}
	if command["FailureCleanupSignals"] != 0 || command["ObservedDescendantsClosed"] != true || command["ProcessGroupClosed"] != true {
		t.Fatal("phase 3 successful browser required process fallback cleanup")
	}
	for _, expected := range inventory {
		current, err := selectedPhase2Fact(expected.Path, 256<<20)
		if err != nil || current != expected {
			t.Fatal("phase 3 execution inputs changed during the admitted browser run")
		}
	}
	driver["ExecutionInputsUnchanged"] = true
	driver["Complete"], driver["ExpectedFilesVerified"], driver["OwnedSessionsRetired"] = true, true, true
	driver["BrowserChecks"], driver["AudioObservation"], driver["ServerRestarts"] = result.Checks, observer.audioObservation, 1
	t.Log("selected_phase3_browser_verified=true stages=16 actual_audio=true imported_episode_facts=3 physical_arrivals=1 server_restarts=1")
}
