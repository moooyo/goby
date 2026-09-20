//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/color"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/notifications"
	"github.com/moooyo/goby/internal/transcode"
	adminassets "github.com/moooyo/goby/web/admin"
)

var selectedPhase4Phases = []string{"account-pin", "runtime-saved", "client-capabilities", "audio-playing", "remote-paused", "session-reconnected", "audio-stopped",
	"notifications-configured", "notification-retried", "notification-offline", "notification-hidden", "notification-rotated", "notification-new-target",
	"notification-revoked", "notifications-disabled", "listener-restarted", "restarted-csrf", "cleanup"}

type selectedPhase4Execution struct {
	Marker                                                                          string
	FFmpegPath, FFmpegSHA256, FFprobePath, FFprobeSHA256                            string
	ReceiverBinaryPath, ReceiverBinarySHA256                                        string
	CACertPath, CACertSHA256, ReceiverCertPath, ReceiverCertSHA256, ReceiverKeyPath string
}

type selectedPhase4Context struct {
	Marker, RunId, BaseURL, RestartBaseURL                                              string
	NextPort                                                                            int
	AdminId, AdminName, AdminPassword, ViewerId, ViewerName, ViewerPassword, ProfilePin string
	LibraryId, VisibleMovieId, HiddenMovieId, HiddenMovieName                           string
	AudioItemId, AudioName, AlbumId, AlbumName, ArtistId, ArtistName, SearchTerm        string
	NotificationEndpoint, ReceiverCredential, TargetTokenA, TargetTokenB                string
	ArtifactsDir, ResultPath                                                            string
}

type selectedPhase4Request struct {
	RunId, Phase, SettingsRevision, NotificationSettingsRevision                                      string
	NextPort                                                                                          int
	TargetSessionId, DeviceId, AudioItemId, PlaybackReference, NegotiatedPlaySessionId, MediaSourceId string
	PositionTicks                                                                                     int64
	IsPaused                                                                                          bool
	HiddenItemId, MetadataRevision, RegistrationId, RegistrationRevision                              string
}

type selectedPhase4Runtime struct {
	f        *serverFixture
	assets   fs.FS
	roots    *x509.CertPool
	server   *http.Server
	listener net.Listener
	done     chan error
	active   atomic.Int64
	origin   string
}

func (runtime *selectedPhase4Runtime) listen(ctx context.Context) error {
	app := runtime.f.app
	startup, err := app.StartupHTTPBinding()
	if err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", startup.Address())
	if err != nil {
		return err
	}
	if err = app.PublishHTTPBinding(listener.Addr(), startup); err != nil {
		_ = listener.Close()
		return err
	}
	runtime.listener = listener
	runtime.origin = "http://" + listener.Addr().String()
	application := app.Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtime.active.Add(1)
		defer runtime.active.Add(-1)
		if r.URL.Path != "/__selected-phase4-consumer" {
			application.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Use GET for the owned client.", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self' "+strings.Replace(runtime.origin, "http://", "ws://", 1)+"; media-src 'self' blob:; img-src 'self' blob: data:")
		_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Owned Phase 4 reference client</title></head><body><main><h1>Owned Phase 4 reference client</h1><input id="profile-pin" type="password" autocomplete="off"><button id="unlock-profile">Unlock profile</button><p id="pin-status"></p><div id="choices"></div><pre id="detail"></pre><button id="play-audio">Play audio</button><button id="pause-audio">Pause audio</button><button id="resume-audio">Resume audio</button><button id="stop-audio">Stop audio</button><audio id="audio" controls></audio><p id="status"></p></main></body></html>`)
	})
	runtime.server = &http.Server{Handler: handler, ConnContext: app.HTTPConnectionContext, BaseContext: func(net.Listener) context.Context { return runtime.f.ctx },
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 1 << 20}
	runtime.done = make(chan error, 1)
	go func() { runtime.done <- runtime.server.Serve(listener) }()
	return nil
}

func (runtime *selectedPhase4Runtime) close(ctx context.Context) error {
	var result error
	if runtime.server != nil {
		if err := runtime.server.Shutdown(ctx); err != nil {
			result = err
			_ = runtime.server.Close()
		}
		select {
		case err := <-runtime.done:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				result = errors.Join(result, err)
			}
		case <-ctx.Done():
			return errors.Join(result, ctx.Err())
		}
		runtime.server, runtime.listener = nil, nil
		runtime.f.app.WithdrawHTTPBinding()
	}
	return errors.Join(result, runtime.f.app.Close(ctx))
}

func (runtime *selectedPhase4Runtime) restart(ctx context.Context) error {
	if err := runtime.close(ctx); err != nil {
		return err
	}
	app, err := New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log, "selected-phase4-browser-integration",
		WithDashboardAssets(runtime.assets), WithNotificationTrustRoots(runtime.roots))
	if err != nil {
		return err
	}
	runtime.f.app = app
	return runtime.listen(ctx)
}

type selectedPhase4Process struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	result map[string]any
	err    error
	output string
}

func selectedPhase4StartProcess(parent context.Context, executable string, args, environment []string, work, output string) *selectedPhase4Process {
	ctx, cancel := context.WithCancel(parent)
	process := &selectedPhase4Process{cancel: cancel, done: make(chan struct{}), output: output}
	go func() {
		result, err := refreshBrowserCommand(ctx, executable, args, environment, work, output)
		process.mu.Lock()
		process.result, process.err = result, err
		process.mu.Unlock()
		close(process.done)
	}()
	return process
}

func (process *selectedPhase4Process) stop(ctx context.Context) (map[string]any, error) {
	process.cancel()
	select {
	case <-process.done:
	case <-ctx.Done():
		return nil, errors.New("phase 4 independent process did not join")
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	result := process.result
	if result == nil || result["ExitCode"] != 0 || result["ObservedDescendantsClosed"] != true || result["ProcessGroupClosed"] != true || result["FailureCleanupSignals"] != 0 {
		return result, errors.New("phase 4 independent process required abnormal cleanup")
	}
	if process.err != nil && !errors.Is(process.err, context.Canceled) {
		return result, errors.New("phase 4 independent process failed")
	}
	return result, nil
}

func selectedPhase4ReadExecution(t *testing.T) (selectedPhase4Execution, []selectedPhase2FileFact, *x509.CertPool) {
	t.Helper()
	var execution selectedPhase4Execution
	path := refreshBrowserPath(t, "GOBY_SELECTED_PHASE4_EXECUTION_CONFIG", false)
	var raw json.RawMessage
	if err := featureWavePrivateJSON(path, 32<<10, &raw); err != nil {
		selectedPhase2Fatal(t, "read phase 4 private execution inventory", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&execution); err != nil {
		selectedPhase2Fatal(t, "decode phase 4 execution inventory", err)
	}
	if execution.Marker != "goby-selected-phase4-execution-v1" {
		t.Fatal("phase 4 execution inventory marker differs")
	}
	inventory := []selectedPhase2FileFact{}
	for _, input := range []struct{ path, digest string }{{execution.FFmpegPath, execution.FFmpegSHA256}, {execution.FFprobePath, execution.FFprobeSHA256},
		{execution.ReceiverBinaryPath, execution.ReceiverBinarySHA256}, {execution.CACertPath, execution.CACertSHA256}, {execution.ReceiverCertPath, execution.ReceiverCertSHA256}} {
		fact, err := selectedPhase2Fact(input.path, 256<<20)
		if err != nil || len(input.digest) != 64 || fact.SHA256 != input.digest {
			t.Fatal("phase 4 pinned execution input identity differs")
		}
		inventory = append(inventory, fact)
	}
	key, err := selectedPhase2Fact(execution.ReceiverKeyPath, 64<<10)
	if err != nil || key.Mode != 0o600 {
		t.Fatal("phase 4 receiver private key is not privately owned")
	}
	inventory = append(inventory, key)
	ca, err := os.ReadFile(execution.CACertPath)
	if err != nil {
		selectedPhase2Fatal(t, "read admitted phase 4 authority certificate", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("phase 4 authority certificate is invalid")
	}
	pair, err := tls.LoadX509KeyPair(execution.ReceiverCertPath, execution.ReceiverKeyPath)
	if err != nil || len(pair.Certificate) == 0 {
		t.Fatal("phase 4 receiver TLS identity is invalid")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal("phase 4 receiver certificate could not be parsed")
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "127.0.0.1", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatal("phase 4 receiver certificate does not validate its actual loopback endpoint")
	}
	return execution, inventory, roots
}

func selectedPhase4JSON(ctx context.Context, client *http.Client, base, path, token, method string, input any) ([]byte, int, error) {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		request.Header.Set("X-Emby-Token", token)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, errors.New("phase 4 actual HTTP request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
	if err != nil || len(raw) > 16<<20 {
		return nil, response.StatusCode, errors.New("phase 4 actual HTTP response exceeds its bound")
	}
	return raw, response.StatusCode, nil
}

func selectedPhase4PrivateReplace(path, value string) error {
	temporary := path + ".new"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, value)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("phase 4 receiver secret replacement failed")
	}
	return os.Rename(temporary, path)
}

type selectedPhase4Observer struct {
	runtime                                                                      *selectedPhase4Runtime
	fixture                                                                      selectedPhase4Context
	execution                                                                    selectedPhase4Execution
	actor                                                                        identity.Principal
	actorToken, vaultPath, targetFile, receipts                                  string
	receiver, consumer                                                           *selectedPhase4Process
	guard                                                                        net.Listener
	files                                                                        map[string]selectedPhase2FileFact
	adminBaseline                                                                json.RawMessage
	settingsRevision, notificationRevision, registrationId, registrationRevision string
	targetSession, playId, negotiatedPlay, videoPlay, videoAuth                  string
	producerIds, videoProducerIds                                                []string
	audioSessions                                                                []*hlsSession
	audioPosition                                                                int64
	notificationGeneration                                                       int64
	deliveryIds, receiptIds                                                      []string
	statusBaseline                                                               json.RawMessage
	cursorBaseline                                                               int64
	hiddenMetadataRevision                                                       string
	videoEvidence, audioEvidence, notificationEvidence                           map[string]any
	processEvidence                                                              map[string]any
	durable                                                                      json.RawMessage
	completed                                                                    int
}

func (observer *selectedPhase4Observer) filesVerified() ([]selectedPhase2FileFact, error) {
	paths := make([]string, 0, len(observer.files))
	for path := range observer.files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	result := []selectedPhase2FileFact{}
	for _, path := range paths {
		fact, err := selectedPhase2Fact(path, 32<<20)
		if err != nil || fact != observer.files[path] {
			return result, errors.New("phase 4 owned media source identity changed")
		}
		result = append(result, fact)
	}
	return result, nil
}

func (observer *selectedPhase4Observer) snapshot(ctx context.Context, phase, suffix string) (map[string]any, error) {
	f := observer.runtime.f
	var raw string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Settings',(SELECT jsonb_build_object('Revision',revision::text,'Runtime',runtime_overrides) FROM managed_settings WHERE id=1),
		'Transport',(SELECT jsonb_build_object('Revision',revision::text,'Enabled',enabled,'HasCredential',credential_ciphertext IS NOT NULL) FROM notification_transport WHERE id=1),
		'Registrations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Revision',revision::text,'Enabled',enabled,'LastOutcome',last_outcome,'Cursor',source_cursor) ORDER BY id),'[]'::jsonb) FROM notification_registrations),
		'Deliveries',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'RegistrationId',registration_id,'Revision',registration_revision::text,'Kind',kind,'State',state,'Attempts',attempts,'Outcome',outcome) ORDER BY id),'[]'::jsonb) FROM notification_deliveries),
		'Playback',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'ItemId',item_id,'State',state,'Counted',counted,'Started',started_at IS NOT NULL,'Stopped',stopped_at IS NOT NULL,'ClientCorrelated',client_correlated,'PositionTicks',position_ticks) ORDER BY id),'[]'::jsonb) FROM play_sessions),
		'ActiveSessions',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'ActiveEncodings',(SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running')),
		'ActiveDelivery',(SELECT count(*) FROM notification_deliveries WHERE state IN ('pending','sending'))
	)::text`).Scan(&raw)
	files, fileErr := observer.filesVerified()
	value := map[string]any{"Marker": "goby-selected-phase4-stage-database-v1", "RunId": observer.fixture.RunId, "Phase": phase,
		"Observed": err == nil, "Complete": false, "ExpectedFilesVerified": fileErr == nil, "Files": files, "Runtime": selectedPhase1RuntimeFacts(f),
		"ActiveHTTPRequests": observer.runtime.active.Load(), "TargetWebSockets": f.app.eventHub.CountForSession(observer.targetSession)}
	if err == nil {
		value["Database"] = json.RawMessage(raw)
	}
	if observer.videoEvidence != nil {
		value["ManagedVideoEvidence"] = observer.videoEvidence
	}
	if observer.audioEvidence != nil {
		value["AudioEvidence"] = observer.audioEvidence
	}
	if strings.Contains(phase, "notif") && observer.notificationEvidence != nil {
		value["NotificationEvidence"] = observer.notificationEvidence
	}
	if phase == "listener-restarted" {
		value["BaseURL"] = observer.runtime.origin
	}
	if observer.processEvidence != nil {
		value["IndependentProcesses"] = observer.processEvidence
	}
	if writeErr := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-"+suffix+".json"), value); writeErr != nil {
		return value, errors.New("phase 4 independent observation could not be retained")
	}
	if err != nil {
		return value, errors.New("phase 4 independent database observation failed")
	}
	return value, fileErr
}

func (observer *selectedPhase4Observer) runtimeSettings(ctx context.Context, revision string, threads int, restarted bool) error {
	f := observer.runtime.f
	snapshot := f.app.settings.Snapshot()
	if strconv.FormatInt(snapshot.Revision, 10) != revision || snapshot.Runtime.Execution.Threads != threads ||
		snapshot.Runtime.Execution.H264 != (transcode.CPUQuality{Preset: "fast", RateControl: "capped_crf", CRF: 23}) ||
		snapshot.Runtime.Hardware.Decode != "software" || snapshot.Runtime.Hardware.Encode != "software" || snapshot.Runtime.Hardware.DeviceID != "" ||
		snapshot.Runtime.DesiredNetwork.HttpPort != observer.fixture.NextPort || snapshot.Runtime.DesiredNetwork.BindHost != "127.0.0.1" {
		return errors.New("phase 4 native runtime settings did not match the committed execution snapshot")
	}
	var stored bool
	if f.pool.QueryRow(ctx, `SELECT revision::text=$1 AND runtime_overrides->>'Threads'=$2 AND runtime_overrides->'H264' @> '{"Preset":"fast","RateControl":"capped_crf","CRF":23}'::jsonb
		FROM managed_settings WHERE id=1`, revision, strconv.Itoa(threads)).Scan(&stored) != nil || !stored {
		return errors.New("phase 4 persisted settings differ from live admission")
	}
	binding := f.app.ManagedHTTPBindingState(snapshot.Runtime.DesiredNetwork)
	if binding.Active == nil || binding.RestartRequired == restarted || binding.ReconnectURL != observer.fixture.RestartBaseURL {
		return errors.New("phase 4 desired and active listener state was conflated")
	}
	if restarted {
		if binding.Active.HttpPort != observer.fixture.NextPort || binding.Active.Configured != snapshot.Runtime.DesiredNetwork {
			return errors.New("phase 4 new listener did not bind the saved settings")
		}
	} else {
		if observer.guard == nil || binding.Active.HttpPort == observer.fixture.NextPort || observer.runtime.origin != observer.fixture.BaseURL {
			return errors.New("phase 4 settings write moved the running listener")
		}
	}
	return nil
}

func selectedPhase4Command(ctx context.Context, tool string, arguments []string) (string, string, map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	stdout, stderr := &selectedPhase2CommandOutput{maximum: 128 << 10}, &selectedPhase2CommandOutput{maximum: 128 << 10}
	command := exec.CommandContext(ctx, tool, arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC"}
	command.Stdout, command.Stderr = stdout, stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	command.WaitDelay = 3 * time.Second
	runErr := command.Run()
	exit := -1
	closed := false
	if command.ProcessState != nil {
		exit = command.ProcessState.ExitCode()
	}
	if command.Process != nil {
		closed = errors.Is(syscall.Kill(-command.Process.Pid, 0), syscall.ESRCH)
	}
	evidence := map[string]any{"ExitCode": exit, "ProcessGroupClosed": closed, "StdoutBytes": stdout.Len(), "StderrBytes": stderr.Len()}
	if runErr != nil || exit != 0 || !closed || stderr.Len() != 0 {
		return stdout.String(), stderr.String(), evidence, errors.New("phase 4 pinned media command failed")
	}
	return stdout.String(), stderr.String(), evidence, nil
}

func (observer *selectedPhase4Observer) managedVideo(ctx context.Context) (resultErr error) {
	f, fixture := observer.runtime.f, observer.fixture
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	// Authentication is a real ordinary login, independent of native authority.
	loginBody := map[string]any{"Username": fixture.ViewerName, "Pw": fixture.ViewerPassword}
	loginRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, observer.runtime.origin+"/emby/Users/AuthenticateByName", nil)
	if err != nil {
		return errors.New("phase 4 video observer request could not be created")
	}
	encoded, _ := json.Marshal(loginBody)
	loginRequest.Body = io.NopCloser(bytes.NewReader(encoded))
	loginRequest.ContentLength = int64(len(encoded))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("X-Emby-Client", "Goby Phase 4 Video Observer")
	loginRequest.Header.Set("X-Emby-Device-Id", "phase4-video-"+fixture.RunId)
	loginRequest.Header.Set("X-Emby-Device-Name", "Owned HTTP")
	loginRequest.Header.Set("X-Emby-Client-Version", "1")
	response, err := client.Do(loginRequest)
	if err != nil {
		return errors.New("phase 4 ordinary video observer login failed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 128<<10))
	_ = response.Body.Close()
	var login struct {
		AccessToken string
		SessionInfo struct{ Id string }
	}
	if readErr != nil || response.StatusCode != 200 || json.Unmarshal(raw, &login) != nil || login.AccessToken == "" || login.SessionInfo.Id == "" {
		return errors.New("phase 4 ordinary video observer login response was invalid")
	}
	observer.videoAuth = login.SessionInfo.Id
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, status, err := selectedPhase4JSON(cleanup, client, observer.runtime.origin, "/emby/Sessions/Logout", login.AccessToken, http.MethodPost, nil)
		if err != nil || status != 204 {
			resultErr = errors.New("phase 4 video observer did not normally log out")
		}
	}()
	body := videoHTTPBody(true, videoHTTPProfile("http", true, true))
	body["StartTimeTicks"] = int64(0)
	body["UserId"] = fixture.ViewerId
	raw, status, err := selectedPhase4JSON(ctx, client, observer.runtime.origin, "/emby/Items/"+fixture.VisibleMovieId+"/PlaybackInfo", login.AccessToken, http.MethodPost, body)
	var info struct {
		PlaySessionId string
		ErrorCode     string
		MediaSources  []struct {
			Id                                           string
			TranscodingUrl                               string
			SupportsTranscoding                          bool
			TranscodingContainer, TranscodingSubProtocol string
		}
	}
	if err != nil || status != 200 || json.Unmarshal(raw, &info) != nil || info.ErrorCode != "" || len(info.MediaSources) != 1 || !strings.HasPrefix(info.PlaySessionId, "play_") {
		return errors.New("phase 4 managed video PlaybackInfo failed")
	}
	source := info.MediaSources[0]
	if source.Id != media.SourceID(fixture.VisibleMovieId) || !source.SupportsTranscoding || source.TranscodingContainer != "mp4" || source.TranscodingSubProtocol != "http" {
		return errors.New("phase 4 video negotiation did not select actual progressive MP4")
	}
	uri, err := url.Parse(source.TranscodingUrl)
	if err != nil || uri.IsAbs() || uri.Host != "" || uri.Fragment != "" || uri.Path != "/emby/Videos/"+fixture.VisibleMovieId+"/stream.mp4" ||
		uri.Query().Get("PlaySessionId") != info.PlaySessionId {
		return errors.New("phase 4 video output URL does not bind the actual preparation")
	}
	observer.videoPlay = info.PlaySessionId
	var jobsBefore int
	if f.pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&jobsBefore) != nil || jobsBefore != 0 {
		return errors.New("phase 4 video negotiation started a producer prematurely")
	}
	output, status, err := selectedPhase4JSON(ctx, client, observer.runtime.origin, uri.String(), login.AccessToken, http.MethodGet, nil)
	if err != nil || status != 200 || len(output) < 1024 {
		return errors.New("phase 4 actual managed video output was not consumed")
	}
	outputPath := filepath.Join(fixture.ArtifactsDir, "managed-video-output.mp4")
	if err = os.WriteFile(outputPath, output, 0o600); err != nil {
		return errors.New("phase 4 actual managed output could not be retained")
	}
	var planRaw []byte
	var jobId, state string
	var bytesWritten int64
	if f.pool.QueryRow(ctx, `SELECT id,state,output_bytes,plan FROM encoding_jobs WHERE play_session_id=$1 AND auth_session_id=$2 AND item_id=$3`,
		info.PlaySessionId, observer.videoAuth, fixture.VisibleMovieId).Scan(&jobId, &state, &bytesWritten, &planRaw) != nil {
		return errors.New("phase 4 actual admitted video job was not persisted")
	}
	var plan transcode.Plan
	if json.Unmarshal(planRaw, &plan) != nil || plan.Hardware.Device != "" || (plan.Hardware.Decode != "" && plan.Hardware.Decode != "software") || (plan.Hardware.Encode != "" && plan.Hardware.Encode != "software") || plan.ExecutionVersion != transcode.ExecutionVersion || plan.Execution.Threads != 2 ||
		plan.Execution.H264 != (transcode.CPUQuality{Preset: "fast", RateControl: "capped_crf", CRF: 23}) ||
		plan.OutputMode != "progressive" || plan.VideoCodec != "h264" || plan.AudioCodec != "aac" || plan.Width != 96 || plan.Height != 54 ||
		plan.StartTicks != 0 || plan.Container != "mp4" || state != "completed" || bytesWritten <= 0 {
		return errors.New("phase 4 actual video admission did not capture the native runtime policy")
	}
	observer.videoProducerIds = []string{jobId}
	probe, _, probeEvidence, probeErr := selectedPhase4Command(ctx, observer.execution.FFprobePath, []string{"-v", "error", "-show_entries", "stream=codec_type,codec_name,width,height,pix_fmt", "-show_entries", "format=duration", "-of", "json", outputPath})
	var facts struct {
		Streams []struct {
			CodecType     string `json:"codec_type"`
			CodecName     string `json:"codec_name"`
			Width, Height int
		}
		Format struct{ Duration string }
	}
	video, audio := 0, 0
	if probeErr == nil && json.Unmarshal([]byte(probe), &facts) == nil {
		for _, stream := range facts.Streams {
			if stream.CodecType == "video" && stream.CodecName == "h264" && stream.Width == 96 && stream.Height == 54 {
				video++
			}
			if stream.CodecType == "audio" && stream.CodecName == "aac" {
				audio++
			}
		}
	}
	progress, _, decodeEvidence, decodeErr := selectedPhase4Command(ctx, observer.execution.FFmpegPath, []string{"-hide_banner", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode", "-threads", "1", "-filter_threads", "1", "-i", outputPath,
		"-map", "0:v:0", "-map", "0:a:0", "-c:v", "wrapped_avframe", "-c:a", "pcm_s16le", "-threads:v", "1", "-threads:a", "1", "-fps_mode", "passthrough", "-progress", "pipe:1", "-nostats", "-f", "null", "-"})
	values := map[string]string{}
	for _, line := range strings.Split(progress, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[key] = value
		}
	}
	frames, frameErr := strconv.Atoi(values["frame"])
	micros, timeErr := strconv.ParseInt(values["out_time_us"], 10, 64)
	digest := sha256.Sum256(output)
	complete := probeErr == nil && decodeErr == nil && video == 1 && audio == 1 && frameErr == nil && frames == 192 && timeErr == nil && micros >= 7_900_000 && micros <= 8_300_000 && values["progress"] == "end"
	observer.videoEvidence = map[string]any{"Complete": complete, "OutputSHA256": hex.EncodeToString(digest[:]), "Bytes": len(output), "DecodedFrames": frames, "OutputMicroseconds": micros,
		"ExpectedFrames": 192, "VideoStreams": video, "AudioStreams": audio, "Plan": plan, "Probe": probeEvidence, "Decode": decodeEvidence, "SettingsRevision": observer.settingsRevision, "JobId": jobId, "PreparedPlayCounted": false}
	if err = featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "managed-video-evidence.json"), observer.videoEvidence); err != nil {
		return errors.New("phase 4 managed video evidence could not be retained")
	}
	if !complete {
		return errors.New("phase 4 managed video did not completely decode its actual audio and video")
	}
	stopPath := "/emby/Videos/ActiveEncodings?" + url.Values{"DeviceId": {"phase4-video-" + fixture.RunId}, "PlaySessionId": {info.PlaySessionId}, "SessionId": {observer.videoAuth}}.Encode()
	_, status, err = selectedPhase4JSON(ctx, client, observer.runtime.origin, stopPath, login.AccessToken, http.MethodDelete, nil)
	if err != nil || status != 204 {
		return errors.New("phase 4 managed video producer could not retire normally")
	}
	if err = observer.waitMediaIdle(ctx, observer.videoProducerIds); err != nil {
		return err
	}
	var prepared bool
	if f.pool.QueryRow(ctx, "SELECT state='Prepared' AND started_at IS NULL AND NOT counted FROM play_sessions WHERE id=$1", info.PlaySessionId).Scan(&prepared) != nil || !prepared {
		return errors.New("phase 4 managed output validation fabricated user playback")
	}
	return nil
}

func (observer *selectedPhase4Observer) waitMediaIdle(ctx context.Context, ids []string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		idle := true
		for _, count := range selectedPhase1RuntimeFacts(observer.runtime.f) {
			idle = idle && count == 0
		}
		for _, id := range ids {
			if _, err := os.Lstat(filepath.Join(observer.runtime.f.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
				idle = false
			}
		}
		var active int
		if observer.runtime.f.pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running')").Scan(&active) != nil {
			return errors.New("phase 4 active producer observation failed")
		}
		if idle && active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 4 actual media resources did not retire")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (observer *selectedPhase4Observer) capabilities(ctx context.Context, request selectedPhase4Request) error {
	f := observer.runtime.f
	if request.DeviceId != "phase4-target-"+observer.fixture.RunId || request.AudioItemId != observer.fixture.AudioItemId || request.TargetSessionId == "" {
		return errors.New("phase 4 client identity differs from the declared reference client")
	}
	var exact bool
	if f.pool.QueryRow(ctx, `SELECT kind='emby' AND user_id=$2 AND device_id=$3 AND client_name='Goby Phase 4 Reference Client'
		AND device_name='Owned Chromium' AND client_version='1' AND revoked_at IS NULL AND expires_at>clock_timestamp()
		AND client_capabilities @> '{"PlayableMediaTypes":["Audio"],"SupportedCommands":["Pause","Unpause","Stop"],"SupportsMediaControl":true}'::jsonb
		AND COALESCE((client_capabilities->>'SupportsSync')::boolean,false)=false FROM sessions WHERE id=$1`,
		request.TargetSessionId, observer.fixture.ViewerId, request.DeviceId).Scan(&exact) != nil || !exact {
		return errors.New("phase 4 actual capabilities were not bound to the current client login")
	}
	if f.app.eventHub.CountForSession(request.TargetSessionId) != 1 {
		return errors.New("phase 4 actual target subscription is missing or duplicated")
	}
	pin, err := f.users.GetOwnProfilePin(ctx, identity.Principal{Kind: "emby", SessionID: request.TargetSessionId, User: identity.User{ID: observer.fixture.ViewerId}, PeerIP: "127.0.0.1"}, observer.fixture.ViewerId)
	if err != nil || pin != observer.fixture.ProfilePin {
		return errors.New("phase 4 current owner profile configuration does not reveal its own saved PIN")
	}
	observer.targetSession = request.TargetSessionId
	return nil
}

func (observer *selectedPhase4Observer) audioState(ctx context.Context, request selectedPhase4Request, stopped bool) error {
	f, fixture := observer.runtime.f, observer.fixture
	if request.PlaybackReference != "phase4-client-"+fixture.RunId || request.TargetSessionId != observer.targetSession ||
		request.DeviceId != "phase4-target-"+fixture.RunId || request.AudioItemId != fixture.AudioItemId ||
		request.MediaSourceId != media.SourceID(fixture.AudioItemId) || request.PositionTicks < 20_000_000 || request.PositionTicks > 210_000_000 {
		return errors.New("phase 4 measured audio does not bind the declared correlated source")
	}
	var internal string
	if f.pool.QueryRow(ctx, `SELECT play_session_id FROM client_playback_references WHERE auth_session_id=$1 AND user_id=$2 AND device_id=$3
		AND client_nonce=$4 AND application_client_id IS NULL`, request.TargetSessionId, fixture.ViewerId, request.DeviceId, request.PlaybackReference).Scan(&internal) != nil ||
		internal == request.NegotiatedPlaySessionId || observer.playId != "" && internal != observer.playId {
		return errors.New("phase 4 client nonce did not resolve to one distinct canonical correlated playback")
	}
	var prepared, valid bool
	if f.pool.QueryRow(ctx, `SELECT NOT client_correlated AND state='Prepared' AND started_at IS NULL AND NOT counted AND user_id=$2 AND auth_session_id=$3
		FROM play_sessions WHERE id=$1`, request.NegotiatedPlaySessionId, fixture.ViewerId, observer.targetSession).Scan(&prepared) != nil || !prepared {
		return errors.New("phase 4 original negotiation was not kept separate from correlated playback")
	}
	state := "Playing"
	if request.IsPaused {
		state = "Paused"
	}
	if stopped {
		state = "Stopped"
	}
	if f.pool.QueryRow(ctx, `SELECT p.client_correlated AND p.state=$7 AND p.started_at IS NOT NULL AND p.counted AND p.position_ticks=$8
		AND (p.stopped_at IS NOT NULL)=$9 AND p.user_id=$2 AND p.auth_session_id=$3 AND p.device_id=$4
		AND p.item_id=$5 AND p.media_source_id=$6 AND NOT p.is_dynamic AND p.application_client_id IS NULL
		AND a.revoked_at IS NULL AND a.expires_at>clock_timestamp()
		FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$1`, internal, fixture.ViewerId, observer.targetSession, request.DeviceId,
		fixture.AudioItemId, request.MediaSourceId, state, request.PositionTicks, stopped).Scan(&valid) != nil || !valid {
		return errors.New("phase 4 client reports did not persist the actual correlated playback state")
	}
	var starts, count int
	if f.pool.QueryRow(ctx, "SELECT count(*) FROM play_sessions WHERE started_at IS NOT NULL").Scan(&starts) != nil || starts != 1 ||
		f.pool.QueryRow(ctx, "SELECT play_count FROM user_item_data WHERE user_id=$1 AND item_id=$2", fixture.ViewerId, fixture.AudioItemId).Scan(&count) != nil || count != 1 {
		return errors.New("phase 4 correlated reports duplicated or fabricated playback counts")
	}
	if request.PositionTicks < observer.audioPosition {
		return errors.New("phase 4 correlated audio clock regressed")
	}
	observer.playId, observer.negotiatedPlay, observer.audioPosition = internal, request.NegotiatedPlaySessionId, request.PositionTicks
	if observer.producerIds == nil {
		f.app.hls.mu.Lock()
		sessions := []*hlsSession{}
		for _, session := range f.app.hls.sessions {
			if session.key.scope.PlaySessionID == internal && session.key.scope.AuthSessionID == observer.targetSession {
				sessions = append(sessions, session)
			}
		}
		f.app.hls.mu.Unlock()
		if len(sessions) != 1 {
			return errors.New("phase 4 correlated audio did not create one actual producer session")
		}
		session := sessions[0]
		if session.key.plan.OutputMode != "progressive" || session.key.plan.Container != "mp3" || session.key.plan.AudioCodec != "mp3" ||
			session.key.plan.VideoStreamIndex != -1 || session.key.plan.StartTicks != 0 || session.key.plan.Execution.Threads != 2 {
			return errors.New("phase 4 correlated audio plan differs from native settings and source")
		}
		session.mu.Lock()
		producers := append([]hlsProducer(nil), session.producers...)
		closed := session.closed
		session.mu.Unlock()
		if len(producers) != 1 || closed || session.ctx.Err() != nil {
			return errors.New("phase 4 actual audio producer is missing or duplicated")
		}
		record, err := f.app.hls.manager.Snapshot(session.key.scope, producers[0].id)
		if err != nil || (record.State != "running" && record.State != "completed") || record.OutputBytes <= 0 || record.ErrorCode != "" {
			return errors.New("phase 4 actual audio producer has no usable output")
		}
		observer.producerIds = []string{record.ID}
		observer.audioSessions = sessions
	}
	if stopped {
		if err := observer.waitMediaIdle(ctx, observer.producerIds); err != nil {
			return err
		}
		for _, session := range observer.audioSessions {
			session.mu.Lock()
			closed := session.closed && session.progressiveReaders == 0 && session.ctx.Err() != nil
			session.mu.Unlock()
			if !closed {
				return errors.New("phase 4 audio session context or reader did not join")
			}
		}
	} else if f.app.eventHub.CountForSession(observer.targetSession) != 1 {
		return errors.New("phase 4 target did not retain its current Sessions subscription")
	}
	observer.audioEvidence = map[string]any{"Complete": true, "ClientCorrelated": true, "CanonicalPlaySessionId": internal, "UnstartedNegotiationId": observer.negotiatedPlay,
		"State": state, "PositionTicks": request.PositionTicks, "PlayCount": count, "ProducerIds": observer.producerIds, "StoppedResourcesClosed": stopped,
		"TargetWebSockets": f.app.eventHub.CountForSession(observer.targetSession)}
	return nil
}

func selectedPhase4LogRows(path string) ([]map[string]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, errors.New("phase 4 independent process output is not bounded")
	}
	scanner := bufio.NewScanner(io.LimitReader(file, 8<<20+1))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	rows := []map[string]json.RawMessage{}
	for scanner.Scan() {
		var row map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &row) == nil {
			rows = append(rows, row)
		}
	}
	return rows, scanner.Err()
}

func selectedPhase4LogEvidence(path, eventId, flag string) (bool, error) {
	rows, err := selectedPhase4LogRows(path)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		var id string
		if json.Unmarshal(row["EventId"], &id) != nil || id != eventId {
			continue
		}
		if flag == "Retry" {
			var status int
			if json.Unmarshal(row["Status"], &status) == nil && status == 503 {
				return true, nil
			}
		} else {
			var value bool
			if json.Unmarshal(row[flag], &value) == nil && value {
				return true, nil
			}
		}
	}
	return false, nil
}

func selectedPhase4Consumed(path string, payload notifications.Payload) (bool, error) {
	rows, err := selectedPhase4LogRows(path)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		var id, kind string
		var consumed, refresh bool
		var count int
		if json.Unmarshal(row["EventId"], &id) != nil || id != payload.EventID {
			continue
		}
		if json.Unmarshal(row["Kind"], &kind) != nil || json.Unmarshal(row["Consumed"], &consumed) != nil ||
			json.Unmarshal(row["RequiresRefresh"], &refresh) != nil || json.Unmarshal(row["ReferenceCount"], &count) != nil ||
			kind != payload.Kind || count != len(payload.References) || !consumed || refresh != (payload.Kind != "Test") {
			return false, errors.New("phase 4 independent consumer reported a different actual payload")
		}
		return true, nil
	}
	return false, nil
}

func (observer *selectedPhase4Observer) favoriteState(ctx context.Context, expected bool) error {
	var actual bool
	if observer.runtime.f.pool.QueryRow(ctx, "SELECT is_favorite FROM user_item_data WHERE user_id=$1 AND item_id=$2",
		observer.fixture.ViewerId, observer.fixture.VisibleMovieId).Scan(&actual) != nil || actual != expected {
		return errors.New("phase 4 visible userdata mutation was not durably observed")
	}
	return nil
}

func (observer *selectedPhase4Observer) notificationStatus(ctx context.Context) (json.RawMessage, int64, error) {
	var raw string
	var cursor int64
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT jsonb_build_object('Id',id,'Revision',revision::text,'Enabled',enabled,'LastOutcome',last_outcome,
		'UpdatedAt',updated_at,'Generation',token_generation)::text,source_cursor FROM notification_registrations WHERE id=$1`, observer.registrationId).Scan(&raw, &cursor)
	return json.RawMessage(raw), cursor, err
}

func (observer *selectedPhase4Observer) notificationIDs(ctx context.Context) ([]string, []string, error) {
	var deliveries []string
	if observer.runtime.f.pool.QueryRow(ctx, "SELECT COALESCE(array_agg(id ORDER BY id),'{}'::text[]) FROM notification_deliveries").Scan(&deliveries) != nil {
		return nil, nil, errors.New("phase 4 delivery history could not be observed")
	}
	entries, err := os.ReadDir(observer.receipts)
	if err != nil {
		return nil, nil, errors.New("phase 4 independent receipts could not be enumerated")
	}
	receipts := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !regexp.MustCompile(`^[0-9a-f]{32}\.json$`).MatchString(entry.Name()) {
			return nil, nil, errors.New("phase 4 receipt directory contains an unexpected entry")
		}
		receipts = append(receipts, strings.TrimSuffix(entry.Name(), ".json"))
	}
	slices.Sort(receipts)
	return deliveries, receipts, nil
}

func (observer *selectedPhase4Observer) notificationDelivered(ctx context.Context, request selectedPhase4Request, kind string, retry, offline bool) error {
	f := observer.runtime.f
	if request.RegistrationId == "" || request.RegistrationRevision == "" {
		return errors.New("phase 4 registration receipt is missing")
	}
	if observer.registrationId != "" && (request.RegistrationId != observer.registrationId || request.RegistrationRevision != observer.registrationRevision) {
		return errors.New("phase 4 notification generation differs from the accepted registration")
	}
	observer.registrationId, observer.registrationRevision = request.RegistrationId, request.RegistrationRevision
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		var eventId, state, generation string
		var attempts int
		var refs []byte
		err := f.pool.QueryRow(ctx, `SELECT id,state,registration_revision::text,attempts,refs FROM notification_deliveries
			WHERE registration_id=$1 AND registration_revision::text=$2 AND kind=$3 AND NOT(id=ANY($4::text[])) ORDER BY created_at,id LIMIT 1`,
			observer.registrationId, observer.registrationRevision, kind, append([]string{}, observer.deliveryIds...)).Scan(&eventId, &state, &generation, &attempts, &refs)
		if err == nil && state == "delivered" {
			var payload notifications.Payload
			receipt := filepath.Join(observer.receipts, eventId+".json")
			if featureWavePrivateJSON(receipt, 32<<10, &payload) != nil {
				return errors.New("phase 4 delivered event has no independent receiver payload")
			}
			accepted, err := selectedPhase4LogEvidence(filepath.Join(observer.receiver.output, "stdout.log"), eventId, "Accepted")
			if err != nil {
				return errors.New("phase 4 receiver acknowledgement cannot be observed")
			}
			consumed, err := selectedPhase4Consumed(filepath.Join(observer.consumer.output, "stdout.log"), payload)
			if err != nil {
				return errors.New("phase 4 independent client output cannot be observed")
			}
			retried := true
			if retry {
				retried, _ = selectedPhase4LogEvidence(filepath.Join(observer.receiver.output, "stdout.log"), eventId, "Retry")
			}
			var retainedRefs []map[string]any
			if json.Unmarshal(refs, &retainedRefs) != nil || len(retainedRefs) != 0 {
				return errors.New("phase 4 terminal delivery retained private source references")
			}
			// Terminal rows deliberately erase refs. Bind the accepted body to
			// the known actual mutation, its delivery identity and generation.
			validReferences := len(payload.References) == 0
			if kind == "UserDataInvalidated" {
				validReferences = len(payload.References) == 1 && payload.References[0].Kind == "Item" &&
					payload.References[0].ID == observer.fixture.VisibleMovieId && payload.References[0].LibraryID == "" &&
					payload.References[0].SourceID == "" && !payload.Recursive
			}
			if payload.Version != 1 || payload.EventID != eventId || payload.RegistrationID != observer.registrationId || payload.Generation != generation ||
				payload.Kind != kind || !validReferences || retry && attempts < 2 || !retried {
				return errors.New("phase 4 sender receiver and client do not bind the same actual event payload")
			}
			for _, ref := range payload.References {
				if ref.ID == observer.fixture.HiddenMovieId || ref.SourceID != "" {
					return errors.New("phase 4 notification exposed a denied or private source reference")
				}
			}
			if kind == "UserDataInvalidated" {
				visible := false
				for _, ref := range payload.References {
					visible = visible || ref.ID == observer.fixture.AudioItemId || ref.ID == observer.fixture.VisibleMovieId
				}
				if !visible {
					return errors.New("phase 4 real userdata notification does not identify its visible source")
				}
			}
			if accepted && consumed {
				if offline && f.app.eventHub.CountForSession(observer.targetSession) != 0 {
					return errors.New("phase 4 offline notification still has a target WebSocket")
				}
				fact, err := selectedPhase2Fact(receipt, 32<<10)
				if err != nil {
					return errors.New("phase 4 accepted receiver receipt identity changed")
				}
				observer.notificationEvidence = map[string]any{"Complete": true, "EventId": eventId, "RegistrationId": observer.registrationId, "Generation": generation, "Kind": kind,
					"ReceiverAccepted": accepted, "ClientConsumed": consumed, "Attempts": attempts, "ReceiverReceiptSHA256": fact.SHA256, "NoWebSocket": offline}
				observer.deliveryIds, observer.receiptIds, err = observer.notificationIDs(ctx)
				if err != nil {
					return err
				}
				if f.pool.QueryRow(ctx, "SELECT COALESCE((SELECT revision::text FROM item_metadata_state WHERE item_id=$1),'0')", observer.fixture.HiddenMovieId).Scan(&observer.hiddenMetadataRevision) != nil {
					return errors.New("phase 4 hidden metadata baseline could not be recorded")
				}
				observer.statusBaseline, observer.cursorBaseline, err = observer.notificationStatus(ctx)
				if err != nil {
					return errors.New("phase 4 notification baseline could not be recorded")
				}
				if f.pool.QueryRow(ctx, "SELECT token_generation FROM notification_registrations WHERE id=$1", observer.registrationId).Scan(&observer.notificationGeneration) != nil {
					return errors.New("phase 4 target generation could not be observed")
				}
				return nil
			}
		} else if err == nil && (state == "failed" || state == "suppressed" || state == "cancelled") {
			return errors.New("phase 4 real notification terminated without delivery")
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 4 matched receiver and independent client did not consume the actual notification")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (observer *selectedPhase4Observer) notificationNoNew(ctx context.Context, hidden bool) error {
	f := observer.runtime.f
	if hidden {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		for {
			var sequence, cursor int64
			err := f.pool.QueryRow(ctx, `SELECT j.sequence,r.source_cursor FROM notification_journal_state j JOIN notification_registrations r ON r.id=$1
				WHERE j.id=1 AND j.sequence>$2 AND EXISTS(SELECT 1 FROM item_metadata_state m WHERE m.item_id=$3 AND m.revision::text<>$4)`,
				observer.registrationId, observer.cursorBaseline, observer.fixture.HiddenMovieId, observer.hiddenMetadataRevision).Scan(&sequence, &cursor)
			if err != nil {
				return errors.New("phase 4 hidden source journal observation failed")
			}
			if sequence > observer.cursorBaseline && cursor >= sequence {
				break
			}
			select {
			case <-ctx.Done():
				return errors.New("phase 4 hidden source was not actually evaluated")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	deliveries, receipts, err := observer.notificationIDs(ctx)
	if err != nil || !reflect.DeepEqual(deliveries, observer.deliveryIds) || !reflect.DeepEqual(receipts, observer.receiptIds) {
		return errors.New("phase 4 non-deliverable event changed outbox or independent receipts")
	}
	var active int
	if f.pool.QueryRow(ctx, "SELECT count(*) FROM notification_deliveries WHERE state IN ('pending','sending')").Scan(&active) != nil || active != 0 {
		return errors.New("phase 4 old notification attempts were not retired")
	}
	observer.notificationEvidence = map[string]any{"Complete": true, "NoNewDeliveries": true, "NoNewReceipts": true}
	if hidden {
		status, _, err := observer.notificationStatus(ctx)
		if err != nil || !bytes.Equal(status, observer.statusBaseline) {
			return errors.New("phase 4 hidden-only source changed client-visible notification status")
		}
		observer.notificationEvidence["HiddenEvaluated"], observer.notificationEvidence["LastOutcomeUnchanged"], observer.notificationEvidence["RegistrationUpdatedAtUnchanged"] = true, true, true
	}
	return nil
}

func (observer *selectedPhase4Observer) durableState(ctx context.Context) (json.RawMessage, error) {
	var raw string
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Settings',(SELECT to_jsonb(m) FROM managed_settings m WHERE id=1),
		'Transport',(SELECT to_jsonb(n)-'credential_ciphertext'||jsonb_build_object('CredentialSHA256',encode(sha256(credential_ciphertext),'hex')) FROM notification_transport n WHERE id=1),
		'Registrations',(SELECT COALESCE(jsonb_agg(to_jsonb(r)-'token_ciphertext'||jsonb_build_object('TokenSHA256',encode(sha256(token_ciphertext),'hex')) ORDER BY id),'[]'::jsonb) FROM notification_registrations r),
		'Items',(SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY id),'[]'::jsonb) FROM items i),
		'UserData',(SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id,item_id),'[]'::jsonb) FROM user_item_data d),
		'Pin',(SELECT jsonb_build_object('CiphertextSHA256',encode(sha256(profile_pin_ciphertext),'hex'),'Configuration',configuration) FROM users WHERE id=$1),
		'Deliveries',(SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY id),'[]'::jsonb) FROM notification_deliveries d)
	)::text`, observer.fixture.ViewerId).Scan(&raw)
	return json.RawMessage(raw), err
}

func (observer *selectedPhase4Observer) stage(ctx context.Context, request selectedPhase4Request) error {
	f, fixture := observer.runtime.f, observer.fixture
	observer.notificationEvidence = nil
	switch request.Phase {
	case "account-pin":
		var sealed []byte
		var inline, local bool
		if f.pool.QueryRow(ctx, "SELECT profile_pin_ciphertext,configuration ? 'ProfilePin',local_password_hash IS NOT NULL FROM users WHERE id=$1", fixture.ViewerId).
			Scan(&sealed, &inline, &local) != nil || len(sealed) < 32 || bytes.Contains(sealed, []byte(fixture.ProfilePin)) || inline || local {
			return errors.New("phase 4 native profile lock was not encrypted separately from password login")
		}
	case "runtime-saved":
		if request.NextPort != fixture.NextPort || request.SettingsRevision == "" {
			return errors.New("phase 4 desired port did not bind the reserved restart target")
		}
		observer.settingsRevision = request.SettingsRevision
		if err := observer.runtimeSettings(ctx, request.SettingsRevision, 2, false); err != nil {
			return err
		}
		if err := observer.managedVideo(ctx); err != nil {
			return err
		}
	case "client-capabilities":
		if err := observer.capabilities(ctx, request); err != nil {
			return err
		}
	case "audio-playing", "remote-paused", "session-reconnected":
		if request.Phase == "remote-paused" && !request.IsPaused {
			return errors.New("phase 4 remote Pause did not produce a measured paused state")
		}
		if err := observer.audioState(ctx, request, false); err != nil {
			return err
		}
	case "audio-stopped":
		if err := observer.audioState(ctx, request, true); err != nil {
			return err
		}
	case "notifications-configured":
		var exact bool
		var credential []byte
		if f.pool.QueryRow(ctx, "SELECT revision::text=$1 AND enabled AND endpoint=$2 AND allowed_networks=ARRAY['127.0.0.1/32']::text[],credential_ciphertext FROM notification_transport WHERE id=1",
			request.NotificationSettingsRevision, fixture.NotificationEndpoint).Scan(&exact, &credential) != nil || !exact || len(credential) < 48 || bytes.Contains(credential, []byte(fixture.ReceiverCredential)) {
			return errors.New("phase 4 native notification configuration did not persist encrypted transport authority")
		}
		observer.notificationRevision = request.NotificationSettingsRevision
	case "notification-retried":
		if err := observer.notificationDelivered(ctx, request, "Test", true, false); err != nil {
			return err
		}
	case "notification-offline":
		if err := observer.favoriteState(ctx, true); err != nil {
			return err
		}
		if err := observer.notificationDelivered(ctx, request, "UserDataInvalidated", false, true); err != nil {
			return err
		}
	case "notification-hidden":
		var changed bool
		if request.HiddenItemId != fixture.HiddenMovieId || request.MetadataRevision == "" ||
			f.pool.QueryRow(ctx, "SELECT revision::text=$2 AND revision::text<>$3 FROM item_metadata_state WHERE item_id=$1", fixture.HiddenMovieId,
				request.MetadataRevision, observer.hiddenMetadataRevision).Scan(&changed) != nil || !changed {
			return errors.New("phase 4 hidden source metadata mutation was not durably observed")
		}
		if _, err := f.app.library.GetItem(ctx, fixture.ViewerId, fixture.HiddenMovieId); !errors.Is(err, library.ErrNotFound) {
			return errors.New("phase 4 edited hidden source is no longer denied")
		}
		if err := observer.notificationNoNew(ctx, true); err != nil {
			return err
		}
	case "notification-rotated":
		if request.RegistrationId != observer.registrationId {
			return errors.New("phase 4 target rotation changed registration identity")
		}
		old, _ := strconv.ParseInt(observer.registrationRevision, 10, 64)
		next, _ := strconv.ParseInt(request.RegistrationRevision, 10, 64)
		var generation int64
		var enabled bool
		var outcome string
		var sealed []byte
		if f.pool.QueryRow(ctx, "SELECT token_generation,enabled,last_outcome,token_ciphertext FROM notification_registrations WHERE id=$1 AND revision=$2",
			observer.registrationId, next).Scan(&generation, &enabled, &outcome, &sealed) != nil || next != old+1 || generation <= observer.notificationGeneration || !enabled || outcome != "" ||
			bytes.Contains(sealed, []byte(fixture.TargetTokenA)) || bytes.Contains(sealed, []byte(fixture.TargetTokenB)) {
			return errors.New("phase 4 target token rotation did not advance its private generation")
		}
		if err := observer.notificationNoNew(ctx, false); err != nil {
			return err
		}
		if err := selectedPhase4PrivateReplace(observer.targetFile, fixture.TargetTokenB); err != nil {
			return errors.New("phase 4 independent receiver target token could not rotate")
		}
		observer.registrationRevision, observer.notificationGeneration = request.RegistrationRevision, generation
		observer.notificationEvidence = map[string]any{"Complete": true, "ReceiverTargetRotated": true, "OldDeliveriesRetired": true, "PrivateGenerationAdvanced": true}
	case "notification-new-target":
		if err := observer.favoriteState(ctx, false); err != nil {
			return err
		}
		if err := observer.notificationDelivered(ctx, request, "UserDataInvalidated", false, true); err != nil {
			return err
		}
	case "notification-revoked":
		if err := observer.favoriteState(ctx, true); err != nil {
			return err
		}
		var revoked bool
		if request.RegistrationId != observer.registrationId || f.pool.QueryRow(ctx, "SELECT NOT enabled AND last_outcome='revoked' AND revision::text=$2 FROM notification_registrations WHERE id=$1",
			observer.registrationId, request.RegistrationRevision).Scan(&revoked) != nil || !revoked {
			return errors.New("phase 4 personal revocation was not durable")
		}
		if err := observer.notificationNoNew(ctx, false); err != nil {
			return err
		}
		observer.registrationRevision = request.RegistrationRevision
		observer.notificationEvidence["RegistrationRevoked"] = true
	case "notifications-disabled":
		if err := observer.favoriteState(ctx, false); err != nil {
			return err
		}
		var disabled, configured bool
		if f.pool.QueryRow(ctx, "SELECT NOT enabled FROM notification_transport WHERE id=1").Scan(&disabled) != nil || !disabled ||
			f.pool.QueryRow(ctx, "SELECT enabled AND revision::text=$2 FROM notification_registrations WHERE id=$1", observer.registrationId, request.RegistrationRevision).Scan(&configured) != nil || !configured {
			return errors.New("phase 4 global disable changed or lost the personal registration")
		}
		if err := observer.notificationNoNew(ctx, false); err != nil {
			return err
		}
		observer.registrationRevision = request.RegistrationRevision
		observer.notificationEvidence["TransportDisabled"], observer.notificationEvidence["RegistrationConfigured"] = true, true
	case "listener-restarted":
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL AND id<>$1", observer.actor.SessionID).Scan(&active) != nil || active != 0 || f.app.eventHub.CountForSession(observer.targetSession) != 0 {
			return errors.New("phase 4 listener restart did not normally log out its clients")
		}
		retireCtx, retireCancel := context.WithTimeout(ctx, 10*time.Second)
		for {
			var remaining int
			if f.pool.QueryRow(retireCtx, "SELECT count(*) FROM notification_registrations WHERE enabled").Scan(&remaining) != nil {
				retireCancel()
				return errors.New("phase 4 inactive registration retirement could not be observed")
			}
			if remaining == 0 {
				break
			}
			select {
			case <-retireCtx.Done():
				retireCancel()
				return errors.New("phase 4 normal logout did not retire its configured registration")
			case <-time.After(100 * time.Millisecond):
			}
		}
		retireCancel()
		if err := observer.runtime.close(ctx); err != nil {
			return errors.New("phase 4 old listener and runtime did not join")
		}
		var err error
		observer.durable, err = observer.durableState(ctx)
		if err != nil {
			return errors.New("phase 4 restart persistence baseline could not be captured")
		}
		if err = featureWaveWriteCheckpoint(filepath.Join(fixture.ArtifactsDir, "restart-before.json"), observer.durable); err != nil {
			return errors.New("phase 4 restart persistence baseline could not be retained")
		}
		if observer.guard == nil {
			return errors.New("phase 4 next listener reservation was lost")
		}
		if err = observer.guard.Close(); err != nil {
			return errors.New("phase 4 next listener guard could not retire")
		}
		observer.guard = nil
		f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(observer.vaultPath))
		if err = observer.runtime.restart(ctx); err != nil {
			return errors.New("phase 4 fresh configured listener could not start")
		}
		if observer.runtime.origin != fixture.RestartBaseURL {
			return errors.New("phase 4 startup ignored the saved exact port")
		}
		if err = observer.runtimeSettings(ctx, observer.settingsRevision, 2, true); err != nil {
			return err
		}
		current, err := observer.durableState(ctx)
		if err != nil || !bytes.Equal(current, observer.durable) {
			return errors.New("phase 4 restart changed accepted state or replayed notifications")
		}
		if err = observer.notificationNoNew(ctx, false); err != nil {
			return err
		}
	case "restarted-csrf":
		old, _ := strconv.ParseInt(observer.settingsRevision, 10, 64)
		next, _ := strconv.ParseInt(request.SettingsRevision, 10, 64)
		if next != old+1 {
			return errors.New("phase 4 new-origin form did not perform exactly one fresh CAS write")
		}
		if err := observer.runtimeSettings(ctx, request.SettingsRevision, 3, true); err != nil {
			return err
		}
		observer.settingsRevision = request.SettingsRevision
		if err := observer.notificationNoNew(ctx, false); err != nil {
			return err
		}
		var disabled bool
		if f.pool.QueryRow(ctx, "SELECT NOT enabled FROM notification_transport WHERE id=1").Scan(&disabled) != nil || !disabled {
			return errors.New("phase 4 restart re-enabled notification transport")
		}
	case "cleanup":
		if err := f.users.Revoke(ctx, observer.actorToken); err != nil {
			return errors.New("phase 4 observer did not retire normally")
		}
		var sessions, plays, started, terminal, jobs, pending int
		if f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
			(SELECT count(*) FROM play_sessions),(SELECT count(*) FROM play_sessions WHERE started_at IS NOT NULL),
			(SELECT count(*) FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$1 AND p.client_correlated AND p.state='Stopped' AND p.counted AND p.stopped_at IS NOT NULL AND a.revoked_at IS NOT NULL),
			(SELECT count(*) FROM encoding_jobs),(SELECT count(*) FROM notification_deliveries WHERE state IN ('pending','sending'))`, observer.playId).
			Scan(&sessions, &plays, &started, &terminal, &jobs, &pending) != nil || sessions != 0 || plays != 3 || started != 1 || terminal != 1 || jobs != 2 || pending != 0 {
			return errors.New("phase 4 normal cleanup did not preserve exact correlated and prepared histories")
		}
		if err := observer.runtime.close(ctx); err != nil {
			return errors.New("phase 4 final listener and worker closure failed")
		}
		if err := observer.waitMediaIdle(ctx, append(append([]string{}, observer.producerIds...), observer.videoProducerIds...)); err != nil {
			return err
		}
		observer.processEvidence = map[string]any{}
		for _, entry := range []struct {
			name    string
			process *selectedPhase4Process
		}{{"Receiver", observer.receiver}, {"Consumer", observer.consumer}} {
			result, err := entry.process.stop(ctx)
			observer.processEvidence[entry.name] = result
			if err != nil {
				return err
			}
		}
		if observer.runtime.active.Load() != 0 || f.app.eventHub.CountForUser(fixture.ViewerId) != 0 || f.app.eventHub.CountForUser(fixture.AdminId) != 0 {
			return errors.New("phase 4 final HTTP or WebSocket scopes remain")
		}
	}
	var unchanged []byte
	if f.pool.QueryRow(ctx, "SELECT jsonb_build_object('Configuration',configuration,'Policy',policy,'Disabled',is_disabled,'Administrator',is_administrator)::text FROM users WHERE id=$1", fixture.AdminId).Scan(&unchanged) != nil ||
		!bytes.Equal(unchanged, observer.adminBaseline) {
		return errors.New("phase 4 unrelated administrator account state changed")
	}
	_, err := observer.filesVerified()
	return err
}

func selectedPhase4Failure(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "phase 4 ") {
		return err.Error()
	}
	return selectedPhase2SafeError(err)
}

func (observer *selectedPhase4Observer) run(ctx context.Context) error {
	for _, phase := range selectedPhase4Phases {
		value := map[string]any{"Marker": "goby-selected-phase4-stage-database-v1", "RunId": observer.fixture.RunId, "Phase": phase, "Observed": false, "Complete": false}
		err := phase3BrowserWaitRequest(ctx, phase3BrowserContext{RunID: observer.fixture.RunId, ArtifactsDir: observer.fixture.ArtifactsDir}, phase)
		var request selectedPhase4Request
		if err == nil {
			if featureWavePrivateJSON(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-request.json"), 64<<10, &request) != nil || request.RunId != observer.fixture.RunId || request.Phase != phase {
				err = errors.New("phase 4 request does not bind its run and stage")
			}
		}
		if err == nil {
			_, err = observer.snapshot(ctx, phase, "before-observation")
		}
		if err == nil {
			err = observer.stage(ctx, request)
		}
		after, snapshotErr := observer.snapshot(ctx, phase, "after-observation")
		if after != nil {
			value = after
		}
		if err == nil {
			err = snapshotErr
		}
		if err != nil {
			value["Failed"], value["FailureDetail"] = true, selectedPhase4Failure(err)
			_ = featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json"), value)
			return err
		}
		value["Complete"], value["Observed"], value["ExpectedFilesVerified"] = true, true, true
		if err = featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json"), value); err != nil {
			return errors.New("phase 4 stage acknowledgement could not be retained")
		}
		observer.completed++
	}
	return nil
}

func selectedPhase4Author(t *testing.T, execution selectedPhase4Execution, root string) (string, string, string) {
	t.Helper()
	movies := filepath.Join(root, "movies")
	album := filepath.Join(root, "music", "Phase Four Album")
	authoring := filepath.Join(root, "authoring")
	for _, directory := range []string{movies, album, authoring} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			selectedPhase2Fatal(t, "create phase 4 owned media directories", err)
		}
	}
	visible := filepath.Join(movies, "Phase Four Visible Movie.mp4")
	hidden := filepath.Join(movies, "Phase Four Hidden Movie.mp4")
	hlsHTTPMediaCommand(t, execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1", "-f", "lavfi", "-i", "color=c=navy:size=160x90:rate=24:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=8", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "24", "-bf", "0",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "8", "-movflags", "+faststart", visible)
	if err := os.Chmod(visible, 0o600); err != nil {
		selectedPhase2Fatal(t, "protect phase 4 actual movie", err)
	}
	if err := selectedPhase3Copy(visible, hidden); err != nil {
		selectedPhase2Fatal(t, "copy phase 4 denied source", err)
	}
	if err := os.WriteFile(strings.TrimSuffix(hidden, ".mp4")+".nfo", []byte("<movie><title>Phase Four Hidden Movie</title><tag>PhaseFourDenied</tag><overview>Owned hidden source baseline</overview></movie>"), 0o600); err != nil {
		selectedPhase2Fatal(t, "author phase 4 same-library source policy facts", err)
	}
	cover := filepath.Join(authoring, "cover.png")
	selectedPhase2PNG(t, cover, color.NRGBA{R: 58, G: 160, B: 96, A: 255})
	audio := filepath.Join(album, "Phase Four Track.mp3")
	hlsHTTPMediaCommand(t, execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1", "-f", "lavfi", "-i", "sine=frequency=773:sample_rate=48000:duration=20",
		"-i", cover, "-map", "0:a:0", "-map", "1:v:0", "-c:a", "libmp3lame", "-threads:a", "1", "-b:a", "128k", "-c:v", "copy", "-disposition:v:0", "attached_pic",
		"-metadata:s:v:0", "title=Actual embedded cover", "-metadata:s:v:0", "comment=Cover (front)", "-metadata", "title=Phase Four Track",
		"-metadata", "album=Phase Four Album", "-metadata", "artist=Phase Four Artist", "-metadata", "album_artist=Phase Four Ensemble", "-metadata", "genre=Phase Four Genre",
		"-metadata", "track=1", "-id3v2_version", "3", "-t", "20", audio)
	if err := os.Chmod(audio, 0o600); err != nil {
		selectedPhase2Fatal(t, "protect phase 4 actual music", err)
	}
	return visible, hidden, audio
}

func selectedPhase4Readiness(ctx context.Context, endpoint string, roots *x509.CertPool, process *selectedPhase4Process) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots.Clone()}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusNotFound && response.TLS != nil && len(response.TLS.VerifiedChains) > 0 {
				return nil
			}
		}
		select {
		case <-process.done:
			return errors.New("phase 4 independent HTTPS receiver exited before admission")
		case <-ctx.Done():
			return errors.New("phase 4 independent HTTPS receiver did not admit its pinned certificate")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestSelectedCompatibilityPhase4BrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("phase 4 actual browser admission requires root and an owned database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	execution, inventory, roots := selectedPhase4ReadExecution(t)
	runId := os.Getenv("GOBY_SELECTED_PHASE4_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runId) {
		t.Fatal("phase 4 requires an explicit run identity")
	}
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("phase 4 artifacts must be privately owned")
	}
	cwd, err := os.Getwd()
	if err != nil {
		selectedPhase2Fatal(t, "read phase 4 source path", err)
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("phase 4 source root is missing")
	}
	driverPath := filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase4-browser.mjs")
	for _, path := range []string{node, playwright, driverPath} {
		fact, err := selectedPhase2Fact(path, 256<<20)
		if err != nil {
			t.Fatal("phase 4 browser input identity is invalid")
		}
		inventory = append(inventory, fact)
	}
	output, err := os.MkdirTemp(artifacts, "selected-phase4-browser-")
	if err != nil {
		selectedPhase2Fatal(t, "create phase 4 artifact directory", err)
	}
	if err = os.Chmod(output, 0o700); err != nil {
		selectedPhase2Fatal(t, "protect phase 4 artifact directory", err)
	}
	driver := map[string]any{"Marker": "goby-selected-phase4-browser-driver-v1", "RunId": runId, "Complete": false, "ArtifactDirectory": output,
		"CredentialsWrittenToSummary": false, "OriginalClientUsed": false, "TLSVerificationBypassed": false, "RealProber": true, "ReceiverConsumerIndependentProcesses": true}
	var schema, mediaRoot string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if schema != "" {
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe phase 4 owned schema cleanup")
			} else {
				defer pool.Close()
				var exists bool
				if pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&exists) != nil || exists {
					t.Error("phase 4 owned schema remains")
				} else {
					driver["OwnedSchemaRemoved"] = true
				}
			}
		}
		if mediaRoot != "" {
			if _, err := os.Lstat(mediaRoot); !errors.Is(err, os.ErrNotExist) {
				t.Error("phase 4 owned source tree remains")
			} else {
				driver["OwnedMediaRootRemoved"] = true
			}
		}
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("retain phase 4 driver closure evidence")
		}
	})
	if err = refreshBrowserWriteJSON(filepath.Join(output, "execution-inventory.json"), inventory); err != nil {
		selectedPhase2Fatal(t, "retain phase 4 pinned execution inputs", err)
	}
	f := newServerFixtureWithTimeout(t, 18*time.Minute)
	if f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema) != nil {
		t.Fatal("identify phase 4 owned schema")
	}
	mediaRoot = t.TempDir()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
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
		for path := range facts {
			relative, err := filepath.Rel(mediaRoot, path)
			if err != nil || strings.HasPrefix(relative, "..") {
				driver["FailedSourceCapture"] = false
				return
			}
			target := filepath.Join(output, "failed-sources", relative)
			if os.MkdirAll(filepath.Dir(target), 0o700) != nil || selectedPhase3Copy(path, target) != nil {
				driver["FailedSourceCapture"] = false
				return
			}
		}
		driver["FailedSourceCapture"] = true
	})
	visiblePath, hiddenPath, audioPath := selectedPhase4Author(t, execution, mediaRoot)
	files, err := selectedPhase3Files(mediaRoot)
	if err != nil {
		selectedPhase2Fatal(t, "capture phase 4 actual media identities", err)
	}
	if err = f.app.Close(f.ctx); err != nil {
		selectedPhase2Fatal(t, "close initial phase 4 application", err)
	}
	vaultPath := filepath.Join(t.TempDir(), "phase4-master.key")
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(vaultPath))
	f.cfg.ListenAddress, f.cfg.CookieSecure = "127.0.0.1:0", false
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = execution.FFmpegPath, execution.FFprobePath, []string{mediaRoot}
	f.cfg.Transcoding = config.TranscodingConfig{Enabled: true, CacheDirectory: t.TempDir(), Threads: 1, MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20, MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}
	assets, err := adminassets.Files()
	if err != nil {
		selectedPhase2Fatal(t, "read phase 4 native assets", err)
	}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "selected-phase4-browser-integration", WithDashboardAssets(assets), WithNotificationTrustRoots(roots))
	if err != nil {
		selectedPhase2Fatal(t, "construct phase 4 actual application", err)
	}
	f.app = app
	runtime := &selectedPhase4Runtime{f: f, assets: assets, roots: roots}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe phase 4 residual authentication")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("retire phase 4 residual authentication")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
			if driver["Complete"] == true && result.RowsAffected() != 0 {
				t.Error("successful phase 4 run required fallback revocation")
			}
		}
		if runtime.close(ctx) != nil {
			t.Error("join phase 4 owned application")
		} else {
			driver["ServerWorkersClosed"], driver["HTTPListenersClosed"] = true, true
		}
		if runtime.active.Load() != 0 {
			t.Error("phase 4 active HTTP requests remain")
		}
	})
	adminPassword, viewerPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Selected phase 4 administrator", adminPassword)
	if err != nil {
		selectedPhase2Fatal(t, "bootstrap phase 4 administrator", err)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Selected phase 4 viewer", viewerPassword, false)
	if err != nil {
		selectedPhase2Fatal(t, "create phase 4 reference client owner", err)
	}
	credentials, err := f.users.Authenticate(f.ctx, admin.Name, adminPassword, identity.Client{Name: "Phase 4 fixture observer", DeviceID: "phase4-fixture-observer", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "authenticate phase 4 independent observer", err)
	}
	actor, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "resolve phase 4 independent observer", err)
	}
	movies, err := app.library.CreateLibrary(f.ctx, "Phase Four Movies", "movies", []string{filepath.Dir(visiblePath)})
	if err != nil {
		selectedPhase2Fatal(t, "create phase 4 movie library", err)
	}
	music, err := app.library.CreateLibrary(f.ctx, "Phase Four Music", "music", []string{filepath.Join(mediaRoot, "music")})
	if err != nil {
		selectedPhase2Fatal(t, "create phase 4 music library", err)
	}
	if err = selectedPhase3Scan(f.ctx, f, movies.ID, 2); err != nil {
		selectedPhase2Fatal(t, "scan phase 4 actual movies", err)
	}
	if err = selectedPhase3Scan(f.ctx, f, music.ID, 1); err != nil {
		selectedPhase2Fatal(t, "scan phase 4 actual music", err)
	}
	visible := selectedPhase3Item(t, f, admin.ID, visiblePath, "Movie")
	hidden := selectedPhase3Item(t, f, admin.ID, hiddenPath, "Movie")
	var audioId string
	if f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE path=$1 AND type='Audio'", audioPath).Scan(&audioId) != nil {
		t.Fatal("phase 4 actual music was not indexed")
	}
	audio, err := app.library.GetItem(f.ctx, admin.ID, audioId)
	if err != nil || audio.Media == nil || audio.Media.ProbeVersion != media.CurrentProbeVersion || audio.Media.DurationTicks < 199_000_000 || audio.Media.DurationTicks > 202_000_000 ||
		audio.Album == nil || len(audio.Entities.Artists) != 1 {
		t.Fatal("phase 4 music lacks actual tagged twenty-second source facts")
	}
	policy := []byte(`{"EnableAllFolders":true,"BlockedTags":["PhaseFourDenied"],"EnableMediaPlayback":true,"EnableAudioPlaybackTranscoding":true,"EnableVideoPlaybackTranscoding":true,"EnablePlaybackRemuxing":true}`)
	if _, err = f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
		selectedPhase2Fatal(t, "bind phase 4 same-library source policy", err)
	}
	if _, err = app.library.GetItem(f.ctx, viewer.ID, hidden.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("phase 4 source policy did not deny the same-library hidden movie")
	}
	if _, err = app.library.GetItem(f.ctx, viewer.ID, visible.ID); err != nil {
		t.Fatal("phase 4 source policy hid the visible movie")
	}
	embedded, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		selectedPhase2Fatal(t, "inventory phase 4 embedded assets", err)
	}
	frozen, err := refreshBrowserAssetInventory(os.DirFS(filepath.Join(sourceRoot, "web", "admin", "dist")))
	if err != nil || !reflect.DeepEqual(embedded, frozen) {
		t.Fatal("phase 4 native assets differ from the frozen distribution")
	}
	if err = refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embedded); err != nil {
		selectedPhase2Fatal(t, "retain phase 4 asset inventory", err)
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	guard, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		selectedPhase2Fatal(t, "reserve phase 4 desired listener", err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	nextPort := guard.Addr().(*net.TCPAddr).Port
	if err = runtime.listen(f.ctx); err != nil {
		selectedPhase2Fatal(t, "admit phase 4 actual startup listener", err)
	}
	receiverGuard, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		selectedPhase2Fatal(t, "reserve phase 4 independent receiver port", err)
	}
	receiverAddress := receiverGuard.Addr().String()
	secretDirectory := t.TempDir()
	credentialPath, targetPath := filepath.Join(secretDirectory, "receiver.secret"), filepath.Join(secretDirectory, "target.secret")
	receiverCredential, targetA, targetB := featureWavePassword(t), featureWavePassword(t), featureWavePassword(t)
	if os.WriteFile(credentialPath, []byte(receiverCredential), 0o600) != nil || os.WriteFile(targetPath, []byte(targetA), 0o600) != nil {
		t.Fatal("write private phase 4 receiver credentials")
	}
	for _, name := range []string{"receiver", "consumer", "receipts", "home", "tmp", "cache"} {
		if err = os.Mkdir(filepath.Join(output, name), 0o700); err != nil {
			selectedPhase2Fatal(t, "create phase 4 owned execution directories", err)
		}
	}
	receipts := filepath.Join(output, "receipts")
	processEnv := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp")}
	if err = receiverGuard.Close(); err != nil {
		selectedPhase2Fatal(t, "release phase 4 receiver reservation for its owned process", err)
	}
	receiver := selectedPhase4StartProcess(f.ctx, execution.ReceiverBinaryPath, []string{"--mode", "receive", "--listen", receiverAddress, "--tls-cert", execution.ReceiverCertPath, "--tls-key", execution.ReceiverKeyPath,
		"--credential-file", credentialPath, "--target-token-file", targetPath, "--receipts", receipts, "--transient-failures", "1"}, processEnv, sourceRoot, filepath.Join(output, "receiver"))
	consumer := selectedPhase4StartProcess(f.ctx, execution.ReceiverBinaryPath, []string{"--mode", "consume", "--receipts", receipts}, processEnv, sourceRoot, filepath.Join(output, "consumer"))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, entry := range []struct {
			name    string
			process *selectedPhase4Process
		}{{"Receiver", receiver}, {"Consumer", consumer}} {
			result, err := entry.process.stop(ctx)
			driver[entry.name+"Process"] = result
			if err != nil {
				t.Error("phase 4 independent receiver or consumer failed to close normally")
			}
		}
	})
	endpoint := "https://" + receiverAddress + "/events"
	if err = selectedPhase4Readiness(f.ctx, endpoint, roots, receiver); err != nil {
		selectedPhase2Fatal(t, "admit phase 4 real private-CA HTTPS receiver", err)
	}
	fixture := selectedPhase4Context{Marker: "goby-selected-phase4-browser-fixture-v1", RunId: runId, BaseURL: runtime.origin, RestartBaseURL: "http://127.0.0.1:" + strconv.Itoa(nextPort), NextPort: nextPort,
		AdminId: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword, ViewerId: viewer.ID, ViewerName: viewer.Name, ViewerPassword: viewerPassword, ProfilePin: "2468",
		LibraryId: movies.ID, VisibleMovieId: visible.ID, HiddenMovieId: hidden.ID, HiddenMovieName: hidden.Name,
		AudioItemId: audio.ID, AudioName: audio.Name, AlbumId: audio.Album.ID, AlbumName: audio.Album.Name,
		ArtistId: strconv.FormatInt(audio.Entities.Artists[0].ID, 10), ArtistName: audio.Entities.Artists[0].Name, SearchTerm: "Phase Four",
		NotificationEndpoint: endpoint, ReceiverCredential: receiverCredential, TargetTokenA: targetA, TargetTokenB: targetB, ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	observer := &selectedPhase4Observer{runtime: runtime, fixture: fixture, execution: execution, actor: actor, actorToken: credentials.Token, vaultPath: vaultPath,
		targetFile: targetPath, receipts: receipts, receiver: receiver, consumer: consumer, guard: guard, files: files, deliveryIds: []string{}, receiptIds: []string{}}
	if f.pool.QueryRow(f.ctx, "SELECT jsonb_build_object('Configuration',configuration,'Policy',policy,'Disabled',is_disabled,'Administrator',is_administrator)::text FROM users WHERE id=$1", admin.ID).Scan(&observer.adminBaseline) != nil {
		t.Fatal("phase 4 unrelated account baseline is unavailable")
	}
	if _, err = observer.snapshot(f.ctx, "seeded", "database"); err != nil {
		selectedPhase2Fatal(t, "retain phase 4 actual seed facts", err)
	}
	contextPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove phase 4 private browser context")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if err = refreshBrowserWriteJSON(contextPath, fixture); err != nil {
		selectedPhase2Fatal(t, "write phase 4 private browser context", err)
	}
	browserEnvironment := append(append([]string{}, processEnv...), "CI=1", "XDG_CACHE_HOME="+filepath.Join(output, "cache"), "PLAYWRIGHT_BROWSERS_PATH="+browserCache,
		"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE="+playwright, "GOBY_SELECTED_PHASE4_RUN_ID="+runId, "GOBY_SELECTED_PHASE4_CONTEXT="+contextPath)
	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Minute)
	defer cancel()
	observerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{driverPath}, browserEnvironment, sourceRoot, output)
	stop()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	if observerErr != nil && observer.completed < len(selectedPhase4Phases) {
		driver["FailedDatabaseStage"], driver["DatabaseObservationError"] = selectedPhase4Phases[observer.completed], selectedPhase4Failure(observerErr)
	}
	var result selectedPhase2Result
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(selectedPhase4Phases) {
		t.Fatalf("phase 4 browser or independent observer failed: command=%s observer=%s result=%s completed_stages=%d; inspect retained private evidence",
			selectedPhase2SafeError(commandErr), selectedPhase2SafeError(observerErr), selectedPhase2SafeError(readErr), observer.completed)
	}
	checks := []string{"AccountPin", "RuntimeSaved", "ClientCapabilities", "AudioPlaying", "RemotePaused", "SessionReconnected", "AudioStopped", "NotificationsConfigured", "NotificationRetried",
		"NotificationOffline", "NotificationHidden", "NotificationRotated", "NotificationNewTarget", "NotificationRevoked", "NotificationsDisabled", "ListenerRestarted", "RestartedCSRF", "Cleanup"}
	if result.Marker != "goby-selected-phase4-browser-result-v1" || result.RunID != runId || !result.Complete || len(result.Checks) != len(checks) || len(result.Stages) != len(checks) ||
		result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("phase 4 browser result does not bind the declared complete journey")
	}
	for index, phase := range selectedPhase4Phases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" || !result.Checks[checks[index]] {
			t.Fatal("phase 4 browser and independent database stages disagree")
		}
	}
	if command["FailureCleanupSignals"] != 0 || command["ObservedDescendantsClosed"] != true || command["ProcessGroupClosed"] != true {
		t.Fatal("phase 4 browser required abnormal process cleanup")
	}
	for _, expected := range inventory {
		actual, err := selectedPhase2Fact(expected.Path, 256<<20)
		if err != nil || actual != expected {
			t.Fatal("phase 4 pinned execution inputs changed during the actual journey")
		}
	}
	driver["Complete"], driver["ExecutionInputsUnchanged"], driver["OwnedSessionsRetired"], driver["ExpectedFilesVerified"] = true, true, true, true
	driver["BrowserChecks"], driver["ManagedVideoEvidence"], driver["AudioEvidence"], driver["IndependentProcesses"] = result.Checks, observer.videoEvidence, observer.audioEvidence, observer.processEvidence
	t.Log("selected_phase4_browser_verified=true stages=18 client_correlated=true managed_video_decoded=true independent_https_receiver=true independent_console_consumer=true listener_restarts=1")
}
