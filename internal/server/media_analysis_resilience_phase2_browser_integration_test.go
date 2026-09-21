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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	adminassets "github.com/moooyo/goby/web/admin"
)

// This opt-in test never creates a positive corpus. Its only media copies are
// byte-identical copies of operator-provided, independently labeled sources.
type mediaAnalysisPhase2Tool struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type mediaAnalysisPhase2Case struct {
	ID             string `json:"case_id"`
	Split          string `json:"split"`
	EvaluationRole string `json:"evaluation_role"`
	Series         string `json:"series_id"`
	Season         string `json:"season_id"`
	Episode        string `json:"episode_id"`
	Source         struct {
		Path     string `json:"path"`
		SHA256   string `json:"sha256"`
		Bytes    int64  `json:"size_bytes"`
		Duration int64  `json:"duration_ticks"`
	} `json:"source"`
	Expected struct {
		Kind string `json:"kind"`
	} `json:"expected"`
	Preview struct {
		Required bool  `json:"required"`
		Width    int   `json:"width"`
		Interval int64 `json:"interval_ticks"`
	} `json:"preview"`
}
type mediaAnalysisPhase2Manifest struct {
	Version             int             `json:"schema_version"`
	SplitUnit           string          `json:"split_unit"`
	Artifacts           string          `json:"artifacts_root"`
	ConsumerCaseID      string          `json:"consumer_case_id"`
	OriginalAttemptRefs json.RawMessage `json:"original_attempt_refs"`
	Tools               struct {
		FFmpeg  mediaAnalysisPhase2Tool `json:"ffmpeg"`
		FFprobe mediaAnalysisPhase2Tool `json:"ffprobe"`
	} `json:"tools"`
	Thresholds struct {
		MaxRGBMAE float64 `json:"max_rgb_mae"`
		MaxRGBP95 float64 `json:"max_rgb_p95_error"`
	} `json:"thresholds"`
	Cases []mediaAnalysisPhase2Case `json:"cases"`
}
type mediaAnalysisPhase2Execution struct {
	Marker, PythonPath, PythonSHA256, FingerprintPath, FingerprintSHA256                              string
	VideoJSPath, VideoCSSPath, PluginModulePath, PluginParserPath, PluginComponentPath, PluginDOMPath string
	PluginLicensePath, VideoLicensePath                                                               string
}
type mediaAnalysisPhase2ClientCase struct {
	CaseId, Split, ItemId, Name, LibraryId, MediaSourceId, SourceRevision string
	EvaluationRole                                                        string `json:"EvaluationRole,omitempty"`
	PreviewRequired                                                       bool
	Width                                                                 int
	DurationTicks                                                         int64
}
type mediaAnalysisPhase2BrowserContext struct {
	Marker, RunId, BaseURL, AdminId, AdminName, AdminPassword, ViewerId, ViewerName, ViewerPassword string
	ArtifactsDir, ResultPath, ConsumerCaseId                                                        string
	PreviewIntervalSeconds                                                                          int
	ManifestVersion                                                                                 int
	MaxRGBMAE, MaxRGBP95                                                                            float64
	Cases                                                                                           []mediaAnalysisPhase2ClientCase
	Libraries                                                                                       []map[string]string
}
type mediaAnalysisPhase2Asset struct {
	path, digest, contentType string
	bytes                     []byte
}

func mediaAnalysisPhase2Assets(execution mediaAnalysisPhase2Execution, consumerPath string) (map[string]mediaAnalysisPhase2Asset, error) {
	assets := map[string]mediaAnalysisPhase2Asset{
		"/video.js":              {path: execution.VideoJSPath, digest: "59a717e69bec72ad009181785a1a65b674d1c01e77e04bdc718deb02a9b97671", contentType: "text/javascript"},
		"/video-js.css":          {path: execution.VideoCSSPath, digest: "e4444f0ec2ddd0aa024154b22470afa5d065650e9c07cd4593ba3047c1480f1f", contentType: "text/css"},
		"/plugin/videojs-bif.js": {path: execution.PluginModulePath, digest: "d61c5708988931ffab5eefe02c0bb78c653767d7cb5ed9d8cd6e4cc1c6fa1b06", contentType: "text/javascript"},
		"/plugin/parser":         {path: execution.PluginParserPath, digest: "49dd250b3cd5bc86782217821d01f1dc72bbb65593e70ae8009b8966e7329d15", contentType: "text/javascript"},
		"/plugin/component/bif-mouse-time-display": {path: execution.PluginComponentPath, digest: "e49e97ca494377300ada90356fcf9a6be7709d5b3c7b2d53cfd8bb24fe67527c", contentType: "text/javascript"},
		"/plugin/util/dom":                         {path: execution.PluginDOMPath, digest: "b9bf9700baa7543442c537a13687f040cc10cb431e580045fc20c4befd519667", contentType: "text/javascript"},
		"/license-plugin":                          {path: execution.PluginLicensePath, digest: "426f2636e9b4c8cccb067ea34d71a8fd04eb2cde8227a129d76274962b3f2919", contentType: "text/plain"},
		"/license-videojs":                         {path: execution.VideoLicensePath, digest: "c0d958248a3de7537966f6659e0176fa6a35f23afbbe37a7b23fcc096a5b3b5d", contentType: "text/plain"},
		"/consumer.mjs":                            {path: consumerPath, contentType: "text/javascript"},
	}
	for route, asset := range assets {
		fact, err := selectedPhase2Fact(asset.path, 8<<20)
		if err != nil || asset.digest != "" && fact.SHA256 != asset.digest {
			return nil, errors.New("phase 2 consumer dependency does not match its pinned source")
		}
		asset.bytes, err = os.ReadFile(asset.path)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(asset.bytes)
		if hex.EncodeToString(digest[:]) != fact.SHA256 {
			return nil, errors.New("phase 2 dependency changed during source loading")
		}
		asset.digest = fact.SHA256
		assets[route] = asset
	}
	return assets, nil
}

const mediaAnalysisPhase2ConsumerHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Goby phase 2 protocol consumer</title><link rel="stylesheet" href="/__media-analysis-phase2/video-js.css"><style>body{margin:24px;font-family:system-ui;background:#f5f7fa;color:#17272e}main{max-width:960px}.video-js{width:900px;height:506px}.bif-thumbnail{position:absolute;bottom:42px;z-index:10;background:#111;color:white;pointer-events:none}.bif-image{display:block;max-height:240px;max-width:400px;width:auto}.bif-time{display:block;text-align:center}button{font:inherit;padding:10px;margin:12px 0}#thumbnail-chain{display:block;max-width:400px;max-height:240px;width:auto}#state{white-space:pre-wrap}</style></head><body><main><h1>Goby phase 2 protocol consumer</h1><p>Video.js 7.6.5 with unmodified videojs-bif-updated modules. Intro controls are the explicitly named Goby acceptance protocol adapter.</p><video id="phase2-player" class="video-js" controls muted playsinline preload="metadata"></video><button id="skip-intro" disabled>Skip detected intro</button><p id="state" role="status">Waiting for an authenticated source.</p><img id="thumbnail-chain" alt="ThumbnailSet preview"><p><a href="/__media-analysis-phase2/license-plugin">BIF plugin MIT license</a> · <a href="/__media-analysis-phase2/license-videojs">Video.js Apache license notice</a></p></main><script src="/__media-analysis-phase2/video.js"></script><script type="module" src="/__media-analysis-phase2/consumer.mjs"></script></body></html>`

type mediaAnalysisPhase2Runtime struct {
	f      *serverFixture
	assets map[string]mediaAnalysisPhase2Asset
	server *httptest.Server
	active atomic.Int64
	closed []map[string]any
}

func (runtime *mediaAnalysisPhase2Runtime) listen() error {
	startup, err := runtime.f.app.StartupHTTPBinding()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", startup.Address())
	if err != nil {
		return err
	}
	if err = runtime.f.app.PublishHTTPBinding(listener.Addr(), startup); err != nil {
		listener.Close()
		return err
	}
	actual := runtime.f.app.Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtime.active.Add(1)
		defer runtime.active.Add(-1)
		if strings.HasPrefix(r.URL.Path, "/__media-analysis-phase2") {
			if r.Method != http.MethodGet || r.URL.RawQuery != "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if r.URL.Path == "/__media-analysis-phase2" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; font-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
				_, _ = io.WriteString(w, mediaAnalysisPhase2ConsumerHTML)
				return
			}
			asset, ok := runtime.assets[strings.TrimPrefix(r.URL.Path, "/__media-analysis-phase2")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", asset.contentType)
			_, _ = w.Write(asset.bytes)
			return
		}
		actual.ServeHTTP(w, r)
	})
	server := httptest.NewUnstartedServer(handler)
	_ = server.Listener.Close()
	server.Listener = listener
	server.Config.ConnContext = runtime.f.app.HTTPConnectionContext
	server.Start()
	runtime.server = server
	return nil
}
func (runtime *mediaAnalysisPhase2Runtime) close(ctx context.Context) error {
	if runtime.server != nil {
		runtime.server.CloseClientConnections()
	}
	err := runtime.f.app.Close(ctx)
	if runtime.server != nil {
		runtime.server.Close()
		runtime.server = nil
	}
	runtime.f.app.WithdrawHTTPBinding()
	facts := map[string]any{"CloseSucceeded": err == nil, "ActiveHTTPRequests": runtime.active.Load(), "AnalysisPresent": runtime.f.app.mediaAnalysis != nil}
	if analysis := runtime.f.app.mediaAnalysis; analysis != nil {
		facts["AnalysisJoined"] = mediaAnalysisPhase1Done(analysis.done)
		facts["AnalysisStatusAfterClose"] = analysis.Status()
		if !mediaAnalysisPhase1Done(analysis.done) {
			err = errors.Join(err, errors.New("analysis workers were not joined"))
		}
		if status := analysis.Status(); status.Cache != nil && (status.Cache.Readers != 0 || status.Cache.BuildingEntries != 0 || status.Cache.PendingPublications != 0) {
			err = errors.Join(err, errors.New("analysis resources remain after close"))
		}
	}
	runtime.closed = append(runtime.closed, facts)
	if runtime.active.Load() != 0 {
		return errors.Join(err, errors.New("HTTP requests remain after close"))
	}
	return err
}

func mediaAnalysisPhase2FileSnapshot(fact selectedPhase2FileFact) map[string]any {
	return map[string]any{"sha256": fact.SHA256, "size_bytes": fact.Bytes, "device": fact.Device, "inode": fact.Inode, "mtime_ns": fact.Modified, "ctime_ns": fact.Changed}
}
func mediaAnalysisPhase2Copy(source, target string, expected selectedPhase2FileFact) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, expected.Bytes+1))
	syncErr, closeErr := out.Sync(), out.Close()
	after, sourceErr := selectedPhase2Fact(source, 1<<40)
	copied, targetErr := selectedPhase2Fact(target, 1<<40)
	if copyErr != nil || syncErr != nil || closeErr != nil || sourceErr != nil || targetErr != nil || expected != after || n != expected.Bytes || copied.SHA256 != expected.SHA256 {
		return errors.New("real media copy did not preserve the frozen source")
	}
	return nil
}

func mediaAnalysisPhase2Scan(ctx context.Context, f *serverFixture, libraryID string, expected int) error {
	job, err := f.app.library.StartScan(ctx, libraryID)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	for {
		current, err := f.app.library.GetJob(bounded, job.ID)
		if err != nil {
			return err
		}
		if current.Status == "Completed" {
			if current.Error != "" || current.Scanned != expected || current.ForceProbe {
				return errors.New("real corpus scan differs from its declared physical population")
			}
			return nil
		}
		if current.Status != "Queued" && current.Status != "Running" {
			return errors.New("real corpus scan failed")
		}
		select {
		case <-bounded.Done():
			return bounded.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func mediaAnalysisPhase2Durable(ctx context.Context, f *serverFixture) (json.RawMessage, error) {
	var value string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
	'Settings',(SELECT to_jsonb(s) FROM analysis_settings s WHERE id=1),
	'Profiles',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY run_id),'[]'::jsonb) FROM analysis_run_profiles s),
	'Work',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY child_id),'[]'::jsonb) FROM analysis_work s),
	'Detections',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY item_id),'[]'::jsonb) FROM analysis_detections s),
	'Decisions',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY item_id),'[]'::jsonb) FROM analysis_intro_decisions s),
	'Previews',(SELECT COALESCE(jsonb_agg(to_jsonb(s)-'timeline' ORDER BY item_id,width),'[]'::jsonb) FROM analysis_previews s),
	'Runs',(SELECT COALESCE(jsonb_agg(to_jsonb(s)-ARRAY['actor_name','actor_user_id','actor_session_id','actor_application_key_id','actor_client_session_id','actor_peer_ip','request_fingerprint'] ORDER BY id),'[]'::jsonb) FROM task_runs s WHERE task_key IN ('media.intro_analysis','media.preview_generation'))
	)::text`).Scan(&value)
	return json.RawMessage(value), err
}

// The native browser writes a checkpoint only after it has retained actual HTTP
// evidence. This observer checks the library owner and SQL independently. The
// fixture never substitutes successful business responses or media payloads.
func mediaAnalysisPhase2Observe(ctx context.Context, runtime *mediaAnalysisPhase2Runtime, fixture mediaAnalysisPhase2BrowserContext, actor identity.Principal) error {
	var durableBefore json.RawMessage
	for _, stage := range []string{"automatic-observed", "restart", "persisted", "cleanup"} {
		requestPath := filepath.Join(fixture.ArtifactsDir, "stage-"+stage+"-request.json")
		var request struct{ RunId, Phase string }
		for {
			err := featureWavePrivateJSON(requestPath, 4096, &request)
			if !errors.Is(err, os.ErrNotExist) {
				if err != nil || request.RunId != fixture.RunId || request.Phase != stage {
					return errors.New("phase 2 stage identity mismatch")
				}
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		ack := map[string]any{"Marker": "goby-media-analysis-phase2-stage-v1", "RunId": fixture.RunId, "Phase": stage, "Complete": false}
		var failure error
		switch stage {
		case "automatic-observed":
			var browser struct {
				Cases []struct {
					CaseId    string
					Detection library.AnalysisDetection
				}
			}
			failure = featureWavePrivateJSON(filepath.Join(fixture.ArtifactsDir, "automatic-http.json"), 4<<20, &browser)
			if failure == nil && len(browser.Cases) != len(fixture.Cases) {
				failure = errors.New("automatic evidence population differs")
			}
			for _, current := range fixture.Cases {
				if failure != nil {
					break
				}
				actual, err := runtime.f.app.library.GetAnalysisDetection(ctx, actor, current.ItemId)
				if err != nil {
					failure = err
					break
				}
				actual = adminMediaAnalysisDetectionDTO(actual)
				found := false
				for _, reported := range browser.Cases {
					if reported.CaseId == current.CaseId {
						found = true
						if !reflect.DeepEqual(actual, reported.Detection) {
							failure = errors.New("HTTP detection differs from the library owner")
						}
						break
					}
				}
				if !found {
					failure = errors.New("automatic evidence omitted a case")
				}
			}
			if failure == nil {
				var durable string
				failure = runtime.f.pool.QueryRow(ctx, `SELECT jsonb_build_object('Runs',(SELECT COALESCE(jsonb_agg(to_jsonb(r)-ARRAY['actor_name','actor_user_id','actor_session_id','actor_application_key_id','actor_client_session_id','actor_peer_ip','request_fingerprint']), '[]'::jsonb) FROM task_runs r WHERE task_key IN ('media.intro_analysis','media.preview_generation')),'Sources',(SELECT COALESCE(jsonb_agg(to_jsonb(s)), '[]'::jsonb) FROM analysis_work_sources s),'Detections',(SELECT COALESCE(jsonb_agg(to_jsonb(d)), '[]'::jsonb) FROM analysis_detections d),'Previews',(SELECT COALESCE(jsonb_agg(to_jsonb(p)-'timeline'), '[]'::jsonb) FROM analysis_previews p))::text`).Scan(&durable)
				if failure == nil {
					failure = featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "automatic-database.json"), json.RawMessage(durable))
				}
			}
		case "restart":
			durableBefore, failure = mediaAnalysisPhase2Durable(ctx, runtime.f)
			if failure == nil {
				failure = featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-before.json"), durableBefore)
			}
			if failure == nil {
				failure = runtime.close(ctx)
			}
			if failure == nil {
				assets, err := adminassets.Files()
				failure = err
				if failure == nil {
					runtime.f.app, failure = New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log, "media-analysis-phase2-browser-restarted", WithDashboardAssets(assets))
				}
				if failure == nil {
					failure = runtime.listen()
					if failure == nil {
						ack["BaseURL"] = runtime.server.URL
					}
				}
			}
		case "persisted":
			current, err := mediaAnalysisPhase2Durable(ctx, runtime.f)
			failure = err
			if failure == nil && !bytes.Equal(current, durableBefore) {
				failure = errors.New("normal restart changed durable analysis results or replayed task history")
			}
			if failure == nil {
				failure = featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-after.json"), current)
			}
			var replay int
			if failure == nil {
				failure = runtime.f.pool.QueryRow(ctx, `SELECT count(*) FROM task_runs WHERE task_key IN ('media.intro_analysis','media.preview_generation') AND state IN ('pending','running','stopping')`).Scan(&replay)
			}
			if failure == nil && replay != 0 {
				failure = errors.New("restart retained or replayed active analysis authority")
			}
		case "cleanup":
			var active int
			failure = runtime.f.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE revoked_at IS NULL AND id<>$1`, actor.SessionID).Scan(&active)
			if failure == nil && active != 0 {
				failure = errors.New("browser credentials remain live after cleanup")
			}
		}
		ack["Complete"] = failure == nil
		if failure != nil {
			ack["ErrorCode"] = "independent_stage_failed"
		}
		if err := featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "stage-"+stage+"-ack.json"), ack); err != nil {
			return err
		}
		if failure != nil {
			return failure
		}
	}
	return nil
}

func TestMediaAnalysisResiliencePhase2BrowserIntegration(t *testing.T) {
	manifestPath := os.Getenv("GOBY_MEDIA_ANALYSIS_PHASE2_MANIFEST")
	if manifestPath == "" {
		t.Skip("unconfigured: independently source-reviewed real Phase 2 corpus manifest is required; this is not a passing accuracy or consumer result")
	}
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("phase 2 requires the owned root Linux test-env database")
	}
	initialManifest, err := selectedPhase2Fact(manifestPath, 2<<20)
	if err != nil {
		t.Fatal("configured corpus manifest identity is invalid")
	}
	var manifest mediaAnalysisPhase2Manifest
	if err := featureWavePrivateJSON(manifestPath, 2<<20, &manifest); err != nil {
		t.Fatal("configured real-media manifest cannot be read")
	}
	if parsedManifest, err := selectedPhase2Fact(manifestPath, 2<<20); err != nil || parsedManifest != initialManifest {
		t.Fatal("manifest changed while decoding the frozen corpus selection")
	}
	if manifest.Version != 1 && manifest.Version != 2 || len(manifest.Cases) < 1 || len(manifest.Cases) > 32 {
		t.Fatal("this browser journey requires one through 32 explicitly labeled real cases")
	}
	var declaredBytes int64
	for _, sample := range manifest.Cases {
		if sample.Source.Bytes < 1 || sample.Source.Bytes > (16<<30)-declaredBytes {
			t.Fatal("real corpus browser scope exceeds its preflight copy budget")
		}
		declaredBytes += sample.Source.Bytes
	}
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	if manifest.Artifacts != artifacts {
		t.Fatal("manifest artifact authority differs from the explicitly selected private root")
	}
	var execution mediaAnalysisPhase2Execution
	if featureWavePrivateJSON(refreshBrowserPath(t, "GOBY_MEDIA_ANALYSIS_PHASE2_EXECUTION_CONFIG", false), 64<<10, &execution) != nil || execution.Marker != "goby-media-analysis-phase2-execution-v1" {
		t.Fatal("invalid Phase 2 execution configuration")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	runID := os.Getenv("GOBY_MEDIA_ANALYSIS_PHASE2_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("Phase 2 requires an explicit run identity")
	}
	sourceRoot, err := os.Getwd()
	if err != nil {
		t.Fatal("read fixture source root")
	}
	for {
		if _, err := os.Stat(filepath.Join(sourceRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(sourceRoot)
		if parent == sourceRoot {
			t.Fatal("fixture repository root is unavailable")
		}
		sourceRoot = parent
	}
	scriptRoot := filepath.Join(sourceRoot, "scripts", "test-env")
	consumer, err := mediaAnalysisPhase2Assets(execution, filepath.Join(scriptRoot, "media-analysis-phase2-consumer.mjs"))
	if err != nil {
		t.Fatal("pinned consumer source admission failed")
	}
	for _, tool := range []mediaAnalysisPhase2Tool{manifest.Tools.FFmpeg, manifest.Tools.FFprobe, {execution.PythonPath, execution.PythonSHA256}, {execution.FingerprintPath, execution.FingerprintSHA256}} {
		fact, err := selectedPhase2Fact(tool.Path, 256<<20)
		if err != nil || fact.SHA256 != tool.SHA256 {
			t.Fatal("Phase 2 tool identity differs from the admitted inventory")
		}
	}
	for _, path := range []string{node, playwright, filepath.Join(scriptRoot, "media-analysis-phase2-browser.mjs"), filepath.Join(scriptRoot, "media-analysis-phase2-evaluate.py")} {
		if _, err := selectedPhase2Fact(path, 256<<20); err != nil {
			t.Fatal("Phase 2 launcher source is not an admitted immutable file")
		}
	}
	output, err := os.MkdirTemp(artifacts, "media-analysis-phase2-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create Phase 2 private artifact directory")
	}
	driver := map[string]any{"Marker": "goby-media-analysis-phase2-driver-v1", "RunId": runID, "Complete": false, "ManifestVersion": manifest.Version, "RealCorpusConfigured": true, "SyntheticCorpusUsed": false, "OriginalEmbyClientUsed": false, "ProductEntitlementModified": false}
	if manifest.Version == 2 {
		driver["OriginalAttemptRefs"] = manifest.OriginalAttemptRefs
	}
	t.Cleanup(func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("retain Phase 2 driver receipt")
		}
	})
	for _, name := range []string{"admission-command", "browser-command", "evaluation-command", "home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create bounded Phase 2 command directories")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	baseEnv := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache")}
	admissionPath := filepath.Join(output, "corpus-admission.json")
	command, admissionErr := refreshBrowserCommand(ctx, execution.PythonPath, []string{filepath.Join(scriptRoot, "media-analysis-phase2-evaluate.py"), "--admit-only", "--manifest", manifestPath, "--output", admissionPath}, baseEnv, sourceRoot, filepath.Join(output, "admission-command"))
	driver["CorpusAdmissionCommand"] = command
	if admissionErr != nil {
		t.Fatal("real corpus admission failed; inspect retained private evidence")
	}
	manifestFact, err := selectedPhase2Fact(manifestPath, 2<<20)
	if err != nil || manifestFact != initialManifest {
		t.Fatal("manifest identity changed after admission")
	}
	var admission struct {
		Admitted       bool   `json:"admitted"`
		ManifestSHA256 string `json:"manifest_sha256"`
	}
	if featureWavePrivateJSON(admissionPath, 2<<20, &admission) != nil || !admission.Admitted || admission.ManifestSHA256 != manifestFact.SHA256 {
		t.Fatal("corpus admission did not bind the exact frozen manifest")
	}
	driver["ManifestSHA256"] = manifestFact.SHA256
	before := make(map[string]selectedPhase2FileFact)
	var total int64
	interval := int64(0)
	consumerCase := ""
	for _, sample := range manifest.Cases {
		fact, err := selectedPhase2Fact(sample.Source.Path, 1<<40)
		if err != nil || fact.Bytes != sample.Source.Bytes || fact.SHA256 != sample.Source.SHA256 {
			t.Fatal("real corpus source differs from its frozen reviewed identity")
		}
		before[sample.ID] = fact
		total += fact.Bytes
		if sample.Preview.Required {
			if !strings.EqualFold(filepath.Ext(sample.Source.Path), ".mp4") {
				t.Fatal("this explicitly scoped Video.js preview consumer requires real browser-playable MP4 cases")
			}
			if interval == 0 {
				interval = sample.Preview.Interval
			}
			if sample.Preview.Interval != interval || interval < 20_000_000 || interval > 1_200_000_000 || interval%10_000_000 != 0 {
				t.Fatal("one browser journey requires a shared supported minimum preview interval")
			}
		}
		eligible := sample.Split == "holdout" && sample.Expected.Kind == "positive" && sample.Preview.Required && strings.EqualFold(filepath.Ext(sample.Source.Path), ".mp4")
		if manifest.Version == 1 && consumerCase == "" && eligible {
			consumerCase = sample.ID
		}
		if manifest.Version == 2 && sample.ID == manifest.ConsumerCaseID {
			if !eligible || sample.EvaluationRole != "fresh_holdout" || consumerCase != "" {
				t.Fatal("the explicit consumer must be one independently labeled fresh-holdout positive MP4")
			}
			consumerCase = sample.ID
		}
	}
	if total > 16<<30 || consumerCase == "" {
		t.Fatal("Phase 2 browser scope requires at most 16 GiB of real sources and a preview-enabled holdout-positive MP4 consumer case")
	}
	driver["ConsumerCaseId"] = consumerCase
	f := newServerFixtureWithTimeout(t, 90*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("close initial fixture generation")
	}
	mediaRoot, cacheRoot := t.TempDir(), t.TempDir()
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = manifest.Tools.FFmpeg.Path, manifest.Tools.FFprobe.Path, []string{mediaRoot}
	f.cfg.ListenAddress, f.cfg.CookieSecure = "127.0.0.1:0", false
	f.cfg.MediaAnalysis = config.MediaAnalysisConfig{Enabled: true, CacheDirectory: cacheRoot, CacheMaxBytes: 2 << 30, CacheMaxEntries: 512, MaxEntryBytes: 512 << 20, MaxFileBytes: 128 << 20, FingerprintPath: execution.FingerprintPath, FingerprintSHA256: execution.FingerprintSHA256}
	assets, err := adminassets.Files()
	if err != nil {
		t.Fatal("load embedded native assets")
	}
	embedded, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		t.Fatal("inventory embedded native assets")
	}
	frozen, err := refreshBrowserAssetInventory(os.DirFS(filepath.Join(sourceRoot, "web", "admin", "dist")))
	if err != nil || !reflect.DeepEqual(embedded, frozen) {
		t.Fatal("embedded native assets differ from frozen source assets")
	}
	f.app, err = New(f.ctx, f.cfg, f.pool, f.users, f.log, "media-analysis-phase2-browser", WithDashboardAssets(assets))
	if err != nil {
		t.Fatal("create real analysis fixture runtime")
	}
	runtime := &mediaAnalysisPhase2Runtime{f: f, assets: consumer}
	driver["RuntimeBeforeBrowser"] = f.app.mediaAnalysis.Status()
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		var active int
		_ = f.pool.QueryRow(cleanup, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active)
		driver["FallbackLiveCredentials"] = active
		if driver["Complete"] == true && active != 0 {
			t.Error("successful Phase 2 run required fallback credential cleanup")
		}
		_, _ = f.pool.Exec(cleanup, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if runtime.close(cleanup) != nil {
			t.Error("close and join Phase 2 runtime")
		}
		driver["ClosedGenerations"] = runtime.closed
	})
	adminPassword, viewerPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Phase 2 administrator", adminPassword)
	if err != nil {
		t.Fatal("bootstrap owned corpus administrator")
	}
	viewer, err := f.users.CreateUser(f.ctx, "Phase 2 viewer", viewerPassword, false)
	if err != nil {
		t.Fatal("create owned corpus viewer")
	}
	credential, err := f.users.Authenticate(f.ctx, admin.Name, adminPassword, identity.Client{Name: "Phase 2 observer", DeviceID: "phase2-observer", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		t.Fatal("create bounded independent observer")
	}
	actor, err := f.users.Resolve(f.ctx, credential.Token, "admin")
	if err != nil {
		t.Fatal("resolve independent observer authority")
	}
	fixture := mediaAnalysisPhase2BrowserContext{Marker: "goby-media-analysis-phase2-browser-v1", RunId: runID, AdminId: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword, ViewerId: viewer.ID, ViewerName: viewer.Name, ViewerPassword: viewerPassword, ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json"), ConsumerCaseId: consumerCase, ManifestVersion: manifest.Version, PreviewIntervalSeconds: int(interval / 10_000_000), MaxRGBMAE: manifest.Thresholds.MaxRGBMAE, MaxRGBP95: manifest.Thresholds.MaxRGBP95}
	groups := make(map[string][]mediaAnalysisPhase2Case)
	catalogMappings := make([]map[string]any, 0, len(manifest.Cases))
	processedPaths := make(map[string]string)
	processedBefore := make(map[string]selectedPhase2FileFact)
	for _, sample := range manifest.Cases {
		key := sample.Split + "\x00" + sample.Series + "\x00" + sample.Season
		if manifest.Version == 2 {
			key = sample.EvaluationRole + "\x00" + key
		}
		groups[key] = append(groups[key], sample)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		group := groups[key]
		root := filepath.Join(mediaRoot, fmt.Sprintf("group-%02d", index))
		seriesName, seasonNumber, groupingBasis := "Corpus Series", 1, "test_indexing_container"
		if originalSeason, err := strconv.Atoi(group[0].Season); err == nil && originalSeason >= 0 && originalSeason <= 9999 && strconv.Itoa(originalSeason) == group[0].Season {
			seasonNumber, groupingBasis = originalSeason, "original_numeric_season"
		}
		if group[0].Season == "unknown" {
			if manifest.SplitUnit != "episode" {
				t.Fatal("unknown original seasons require an explicit episode-level split")
			}
			seriesName = "Unseasoned corpus cohort"
		}
		series := filepath.Join(root, seriesName, fmt.Sprintf("Season %02d", seasonNumber))
		if os.MkdirAll(series, 0o700) != nil {
			t.Fatal("create isolated real corpus hierarchy")
		}
		paths := make(map[string]string)
		episodes := make(map[string]int)
		for _, sample := range group {
			if episodes[sample.Episode] == 0 {
				episodes[sample.Episode] = len(episodes) + 1
			}
			name := fmt.Sprintf("Corpus.S%02dE%02d.%s%s", seasonNumber, episodes[sample.Episode], sample.ID, strings.ToLower(filepath.Ext(sample.Source.Path)))
			target := filepath.Join(series, name)
			if err := mediaAnalysisPhase2Copy(sample.Source.Path, target, before[sample.ID]); err != nil {
				t.Fatal("copy real media into owned library without changing bytes")
			}
			paths[sample.ID] = target
			nfo := fmt.Sprintf("<episodedetails><title>Case %s</title><season>%d</season><episode>%d</episode></episodedetails>", sample.ID, seasonNumber, episodes[sample.Episode])
			if os.WriteFile(strings.TrimSuffix(target, filepath.Ext(target))+".nfo", []byte(nfo), 0o600) != nil {
				t.Fatal("write hierarchy-only corpus metadata")
			}
			mapping := map[string]any{"case_id": sample.ID, "split": sample.Split, "original_series_id": sample.Series, "original_season_id": sample.Season, "original_episode_id": sample.Episode, "actual_catalog_season_number": seasonNumber, "actual_catalog_episode_number": episodes[sample.Episode], "grouping_basis": groupingBasis, "isolated_library_group": index}
			if manifest.Version == 2 {
				mapping["evaluation_role"] = sample.EvaluationRole
			}
			catalogMappings = append(catalogMappings, mapping)
		}
		collection, err := f.app.library.CreateLibrary(f.ctx, fmt.Sprintf("Corpus group %02d", index), "tvshows", []string{root})
		if err != nil {
			t.Fatal("create real corpus library")
		}
		if err := mediaAnalysisPhase2Scan(f.ctx, f, collection.ID, len(group)); err != nil {
			t.Fatal("scan real corpus media with current production prober")
		}
		fixture.Libraries = append(fixture.Libraries, map[string]string{"Id": collection.ID, "Name": collection.Name})
		for _, sample := range group {
			var itemID string
			if f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE path=$1 AND type='Episode'", paths[sample.ID]).Scan(&itemID) != nil {
				t.Fatal("real source did not produce an episode item")
			}
			item, err := f.app.library.GetAnalysisItem(f.ctx, actor, itemID)
			if err != nil {
				t.Fatal("bind real source to native analysis identity")
			}
			if item.Detection.Effective != nil || item.Detection.Candidate != nil || item.Detection.Suppressed {
				t.Fatal("automatic corpus cases must not already have manual, imported, chapter or detected intro markers")
			}
			processed, err := selectedPhase2Fact(paths[sample.ID], 1<<40)
			if err != nil || processed.SHA256 != sample.Source.SHA256 || processed.Bytes != sample.Source.Bytes {
				t.Fatal("indexed corpus copy differs from the frozen labeled source")
			}
			processedPaths[sample.ID], processedBefore[sample.ID] = paths[sample.ID], processed
			fixture.Cases = append(fixture.Cases, mediaAnalysisPhase2ClientCase{CaseId: sample.ID, Split: sample.Split, EvaluationRole: sample.EvaluationRole, ItemId: item.ID, Name: item.Name, LibraryId: item.LibraryID, MediaSourceId: item.MediaSourceID, SourceRevision: item.SourceRevision, PreviewRequired: sample.Preview.Required, Width: sample.Preview.Width, DurationTicks: sample.Source.Duration})
		}
	}
	if featureWaveWriteCheckpoint(filepath.Join(output, "catalog-source-mapping.json"), map[string]any{"schema_version": manifest.Version, "split_unit": manifest.SplitUnit, "cohorts_isolated_by_split": true, "cohorts_isolated_by_evaluation_role": manifest.Version == 2, "historical_season_inferred": false, "cases": catalogMappings}) != nil {
		t.Fatal("retain explicit source-to-test-catalog mapping")
	}
	if err := runtime.listen(); err != nil {
		t.Fatal("publish the real owned browser listener")
	}
	fixture.BaseURL = runtime.server.URL
	contextPath := filepath.Join(output, "private-context.json")
	if refreshBrowserWriteJSON(contextPath, fixture) != nil {
		t.Fatal("write private browser context")
	}
	t.Cleanup(func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove short-lived browser credentials")
		}
	})
	observerCtx, stopObserver := context.WithCancel(ctx)
	observed := make(chan error, 1)
	go func() { observed <- mediaAnalysisPhase2Observe(observerCtx, runtime, fixture, actor) }()
	environment := append(append([]string{}, baseEnv...), "CI=1", "PLAYWRIGHT_BROWSERS_PATH="+browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE="+playwright, "GOBY_MEDIA_ANALYSIS_PHASE2_CONTEXT="+contextPath, "GOBY_MEDIA_ANALYSIS_PHASE2_RUN_ID="+runID)
	command, browserErr := refreshBrowserCommand(ctx, node, []string{filepath.Join(scriptRoot, "media-analysis-phase2-browser.mjs")}, environment, sourceRoot, filepath.Join(output, "browser-command"))
	driver["BrowserCommand"] = command
	stopObserver()
	observerErr := <-observed
	driver["IndependentObserverCompleted"] = observerErr == nil
	var browser struct {
		Complete bool
		Cases    []map[string]any
	}
	readErr := featureWavePrivateJSON(fixture.ResultPath, 8<<20, &browser)
	observations := map[string]any{"schema_version": 1, "manifest_sha256": manifestFact.SHA256, "run_id": runID, "captured_at": time.Now().UTC().Format(time.RFC3339Nano), "origin": "real_http"}
	cases := make([]map[string]any, 0, len(manifest.Cases))
	for _, sample := range manifest.Cases {
		value := map[string]any{"case_id": sample.ID}
		for _, reported := range browser.Cases {
			if reported["case_id"] == sample.ID {
				value = reported
				break
			}
		}
		value["source_before"] = mediaAnalysisPhase2FileSnapshot(before[sample.ID])
		after, err := selectedPhase2Fact(sample.Source.Path, 1<<40)
		if err == nil {
			value["source_after"] = mediaAnalysisPhase2FileSnapshot(after)
		} else {
			driver["SourceObservationFailed"] = true
		}
		var binding mediaAnalysisPhase2ClientCase
		for _, current := range fixture.Cases {
			if current.CaseId == sample.ID {
				binding = current
				break
			}
		}
		processed := map[string]any{"path": processedPaths[sample.ID], "item_id": binding.ItemId, "source_revision": binding.SourceRevision, "source_before": mediaAnalysisPhase2FileSnapshot(processedBefore[sample.ID])}
		if observed, err := selectedPhase2Fact(processedPaths[sample.ID], 1<<40); err == nil {
			processed["source_after"] = mediaAnalysisPhase2FileSnapshot(observed)
			if observed != processedBefore[sample.ID] {
				driver["ProcessedSourceChanged"] = true
			}
		} else {
			driver["ProcessedSourceObservationFailed"] = true
		}
		value["processed_source"] = processed
		if preview, ok := value["preview"].(map[string]any); ok && preview["status"] == "ready" {
			itemID := ""
			for _, current := range fixture.Cases {
				if current.CaseId == sample.ID {
					itemID = current.ItemId
					break
				}
			}
			var hash string
			var timeline []byte
			if f.pool.QueryRow(f.ctx, "SELECT content_sha256,timeline FROM analysis_previews WHERE item_id=$1 AND width=$2", itemID, sample.Preview.Width).Scan(&hash, &timeline) != nil || preview["bif_sha256"] != hash {
				driver["PreviewDatabaseBindingFailed"] = true
				preview["status"] = "failed"
				delete(value, "preview")
				value["preview"] = map[string]any{"status": "failed"}
			} else if nominal, actual, err := library.DecodeAnalysisPreviewTimeline(timeline); err == nil {
				encoded, _ := json.Marshal(nominal)
				var observed []int64
				if raw, err := json.Marshal(preview["nominal_ticks"]); err == nil && json.Unmarshal(raw, &observed) == nil {
					other, _ := json.Marshal(observed)
					if bytes.Equal(encoded, other) {
						preview["actual_ticks"] = actual
					} else {
						value["preview"] = map[string]any{"status": "failed"}
						driver["PreviewTimelineBindingFailed"] = true
					}
				}
			} else {
				value["preview"] = map[string]any{"status": "failed"}
				driver["PreviewTimelineBindingFailed"] = true
			}
		}
		cases = append(cases, value)
	}
	observations["cases"] = cases
	observationPath := filepath.Join(output, "observations.json")
	if refreshBrowserWriteJSON(observationPath, observations) != nil {
		t.Fatal("retain exact source-bound corpus observations")
	}
	command, evaluationErr := refreshBrowserCommand(ctx, execution.PythonPath, []string{filepath.Join(scriptRoot, "media-analysis-phase2-evaluate.py"), "--manifest", manifestPath, "--observations", observationPath, "--output", filepath.Join(output, "corpus-result.json")}, baseEnv, sourceRoot, filepath.Join(output, "evaluation-command"))
	driver["CorpusEvaluationCommand"] = command
	_, revokeErr := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND revoked_at IS NULL", actor.SessionID)
	driver["ObserverRevoked"] = revokeErr == nil
	if browserErr != nil || observerErr != nil || readErr != nil || !browser.Complete || evaluationErr != nil || revokeErr != nil {
		t.Error("real Phase 2 browser/corpus acceptance did not pass; retained evidence distinguishes failures and pending cases")
		return
	}
	driver["Complete"] = true
}
