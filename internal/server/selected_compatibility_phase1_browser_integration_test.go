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
	"hash"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	adminassets "github.com/moooyo/goby/web/admin"
	"golang.org/x/sys/unix"
)

const selectedPhase1OriginalWebDirectory = "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/dashboard-ui"

const selectedPhase1OriginalHostExecutable = "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/EmbyServer"

type selectedPhase1ClientHostConfig struct {
	Marker            string
	Origin            string
	PID               int
	StartTicks        uint64
	BootID            string `json:"BootId"`
	NetworkNamespace  string
	Executable        string
	ExecutableSHA256  string
	ServerID          string `json:"ServerId"`
	Version           string
}

type selectedPhase1ExecutableStamp struct {
	Device, Inode     uint64
	Size              int64
	Modified, Changed int64
	Mode              uint32
}

type selectedPhase1AssetRecord struct {
	Method, Path, MIME, SHA256 string
	Status                   int
	Bytes                    int64
	Complete                 bool
	ErrorCode                string `json:",omitempty"`
}

// This transport never starts, stops, or mutates the retained host. Only the
// original static client routes reach it; Goby owns all browser API traffic.
type selectedPhase1ClientHost struct {
	config     selectedPhase1ClientHostConfig
	origin     *url.URL
	stamp      selectedPhase1ExecutableStamp
	transport  *http.Transport
	proxy      *httputil.ReverseProxy
	output     string
	runID      string
	mu         sync.Mutex
	records    []selectedPhase1AssetRecord
	bytes      int64
	reserved   int64
	budgetChanged chan struct{}
	failure    string
	publicRead bool
}

const (
	selectedPhase1AssetLimit = int64(32 << 20)
	selectedPhase1TotalAssetLimit = int64(512 << 20)
	selectedPhase1AssetCountLimit = 4096
)

func (host *selectedPhase1ClientHost) verify(fullHash bool) error {
	process, alive := refreshBrowserProcessAt(host.config.PID)
	if !alive || process.Start != host.config.StartTicks {
		return errors.New("original_client_host_process_changed")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != host.config.BootID {
		return errors.New("original_client_host_boot_changed")
	}
	base := fmt.Sprintf("/proc/%d", host.config.PID)
	namespace, err := os.Readlink(base + "/ns/net")
	if err != nil || namespace != host.config.NetworkNamespace {
		return errors.New("original_client_host_namespace_changed")
	}
	executable, err := os.Readlink(base + "/exe")
	if err != nil || executable != host.config.Executable {
		return errors.New("original_client_host_executable_changed")
	}
	info, err := os.Stat(base + "/exe")
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128<<20 {
		return errors.New("original_client_host_executable_metadata_changed")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("original_client_host_executable_ownership_changed")
	}
	stamp := selectedPhase1ExecutableStamp{Device: uint64(stat.Dev), Inode: stat.Ino, Size: info.Size(),
		Modified: info.ModTime().UnixNano(), Changed: stat.Ctim.Nano(), Mode: uint32(info.Mode().Perm())}
	disk, err := os.Stat(host.config.Executable)
	if err != nil || !os.SameFile(info, disk) {
		return errors.New("original_client_host_executable_path_changed")
	}
	if host.stamp != (selectedPhase1ExecutableStamp{}) && stamp != host.stamp {
		return errors.New("original_client_host_executable_stamp_changed")
	}
	if fullHash {
		file, err := os.Open(base + "/exe")
		if err != nil {
			return errors.New("original_client_host_executable_unreadable")
		}
		digest := sha256.New()
		count, copyErr := io.Copy(digest, io.LimitReader(file, info.Size()+1))
		after, statErr := file.Stat()
		closeErr := file.Close()
		if copyErr != nil || statErr != nil || closeErr != nil || count != info.Size() || !os.SameFile(info, after) ||
			after.ModTime() != info.ModTime() || hex.EncodeToString(digest.Sum(nil)) != host.config.ExecutableSHA256 {
			return errors.New("original_client_host_executable_hash_changed")
		}
		if host.stamp == (selectedPhase1ExecutableStamp{}) {
			host.stamp = stamp
		}
	}
	return nil
}

// Numeric IPv4 dialing keeps socket creation on this dedicated locked thread.
// A restoration failure leaves the thread locked; Go destroys that OS thread
// when the goroutine exits rather than admitting its namespace to the pool.
func (host *selectedPhase1ClientHost) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if (network != "tcp" && network != "tcp4") || address != host.origin.Host {
		return nil, errors.New("original_client_host_dial_scope_invalid")
	}
	type outcome struct {
		connection net.Conn
		err        error
	}
	done := make(chan outcome, 1)
	go func() {
		goruntime.LockOSThread()
		restored := true
		defer func() {
			if restored {
				goruntime.UnlockOSThread()
			}
		}()
		original, err := os.Open(fmt.Sprintf("/proc/self/task/%d/ns/net", unix.Gettid()))
		if err != nil {
			done <- outcome{err: errors.New("original_client_host_current_namespace_unavailable")}
			return
		}
		defer original.Close()
		if err := host.verify(false); err != nil {
			done <- outcome{err: err}
			return
		}
		target, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", host.config.PID))
		if err != nil {
			done <- outcome{err: errors.New("original_client_host_namespace_unavailable")}
			return
		}
		defer target.Close()
		name, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", target.Fd()))
		if err != nil || name != host.config.NetworkNamespace || host.verify(false) != nil {
			done <- outcome{err: errors.New("original_client_host_namespace_descriptor_changed")}
			return
		}
		if unix.Setns(int(target.Fd()), unix.CLONE_NEWNET) != nil {
			done <- outcome{err: errors.New("original_client_host_namespace_entry_failed")}
			return
		}
		restored = false
		dialer := net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1, FallbackDelay: -1}
		connection, dialErr := dialer.DialContext(ctx, "tcp4", address)
		if unix.Setns(int(original.Fd()), unix.CLONE_NEWNET) != nil {
			if connection != nil {
				connection.Close()
			}
			done <- outcome{err: errors.New("original_client_host_namespace_restore_failed")}
			return
		}
		restored = true
		done <- outcome{connection: connection, err: dialErr}
	}()
	// Always receive the bounded dial result, even if ctx was cancelled, so an
	// established descriptor cannot be abandoned in a buffered result channel.
	result := <-done
	return result.connection, result.err
}

func (host *selectedPhase1ClientHost) flushLocked() error {
	file := filepath.Join(host.output, "asset-hashes.json")
	temporary := file + ".pending"
	defer os.Remove(temporary)
	if err := refreshBrowserWriteJSON(temporary, map[string]any{"Marker": "goby-selected-phase1-original-assets-v1",
		"RunId": host.runID, "Host": host.config, "PublicIdentityPinned": host.publicRead,
		"CompleteMeaning": "upstream response reached EOF with stable pinned host identity",
		"Bytes": host.bytes, "FailureCode": host.failure, "Assets": host.records}); err != nil {
		return err
	}
	return os.Rename(temporary, file)
}

func (host *selectedPhase1ClientHost) fail(code string) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.failure == "" {
		host.failure = code
	}
}

func (host *selectedPhase1ClientHost) finish(index int, record selectedPhase1AssetRecord) {
	if host.verify(false) != nil {
		record.Complete, record.ErrorCode = false, "original_client_host_identity_changed_after_asset"
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	host.records[index] = record
	if record.ErrorCode != "" && host.failure == "" {
		host.failure = record.ErrorCode
	}
	if err := host.flushLocked(); err != nil && host.failure == "" {
		host.failure = "original_client_asset_evidence_write_failed"
	}
}

func (host *selectedPhase1ClientHost) reserve(ctx context.Context, wanted int64) (int64, error) {
	if wanted <= 0 {
		return 0, nil
	}
	for {
		host.mu.Lock()
		available := selectedPhase1TotalAssetLimit - host.bytes - host.reserved
		if available > 0 {
			count := min(wanted, available)
			host.reserved += count
			host.mu.Unlock()
			return count, nil
		}
		if host.bytes >= selectedPhase1TotalAssetLimit {
			host.mu.Unlock()
			return 0, nil
		}
		changed := host.budgetChanged
		host.mu.Unlock()
		// An in-flight read may release unused capacity after a short read or
		// EOF; temporary reservations are not proof of an actual byte overrun.
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-changed:
		}
	}
}

func (host *selectedPhase1ClientHost) release(reserved, consumed int64) {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.reserved -= reserved
	host.bytes += consumed
	close(host.budgetChanged)
	host.budgetChanged = make(chan struct{})
}

type selectedPhase1AssetBody struct {
	io.ReadCloser
	host      *selectedPhase1ClientHost
	ctx       context.Context
	index     int
	record    selectedPhase1AssetRecord
	digest    hash.Hash
	once      sync.Once
	closeOnce sync.Once
	closeErr  error
	empty     bool
	finished  bool
}

func (body *selectedPhase1AssetBody) finish(complete bool, code string) {
	body.once.Do(func() {
		if err := body.host.verify(false); err != nil {
			complete, code = false, "original_client_host_identity_changed_after_asset"
		}
		body.record.Complete, body.record.ErrorCode = complete, code
		body.record.SHA256 = hex.EncodeToString(body.digest.Sum(nil))
		body.finished = true
		body.host.finish(body.index, body.record)
	})
}

func (body *selectedPhase1AssetBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	wanted := min(int64(len(buffer)), selectedPhase1AssetLimit-body.record.Bytes)
	reserved, reserveErr := body.host.reserve(body.ctx, max(wanted, 0))
	if reserveErr != nil {
		body.finish(false, "original_client_asset_budget_wait_cancelled")
		return 0, reserveErr
	}
	if reserved == 0 {
		// One unforwarded byte distinguishes a real EOF at the exact bound from
		// an overrun. It never contributes to a successful response digest.
		var probe [1]byte
		n, err := body.ReadCloser.Read(probe[:])
		if n == 0 && errors.Is(err, io.EOF) {
			body.finish(true, "")
			return 0, io.EOF
		}
		body.finish(false, "original_client_asset_byte_bound_exceeded")
		return 0, errors.New("original_client_asset_byte_bound_exceeded")
	}
	n, err := body.ReadCloser.Read(buffer[:int(reserved)])
	body.host.release(reserved, int64(n))
	if n > 0 {
		body.record.Bytes += int64(n)
		_, _ = body.digest.Write(buffer[:n])
	}
	if errors.Is(err, io.EOF) {
		body.finish(true, "")
	} else if err != nil {
		body.finish(false, "original_client_asset_read_failed")
	}
	return n, err
}

func (body *selectedPhase1AssetBody) Close() error {
	body.closeOnce.Do(func() {
		body.closeErr = body.ReadCloser.Close()
		if !body.finished {
			if body.empty && body.closeErr == nil {
				body.finish(true, "")
			} else {
				body.finish(false, "original_client_asset_response_incomplete")
			}
		}
		if body.closeErr != nil {
			body.record.Complete, body.record.ErrorCode = false, "original_client_asset_close_failed"
			body.host.finish(body.index, body.record)
		}
	})
	return body.closeErr
}

type selectedPhase1AssetContextKey struct{}

type selectedPhase1AssetTransport struct{ host *selectedPhase1ClientHost }

func (transport selectedPhase1AssetTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	host := transport.host
	index, ok := request.Context().Value(selectedPhase1AssetContextKey{}).(int)
	if !ok || host.verify(false) != nil {
		host.fail("original_client_host_identity_changed_before_asset")
		return nil, errors.New("original_client_host_identity_changed_before_asset")
	}
	response, err := host.transport.RoundTrip(request)
	if err != nil {
		host.finish(index, selectedPhase1AssetRecord{Method: request.Method, Path: request.URL.Path, ErrorCode: "original_client_asset_request_failed"})
		return nil, errors.New("original_client_asset_request_failed")
	}
	record := selectedPhase1AssetRecord{Method: request.Method, Path: request.URL.Path,
		Status: response.StatusCode, MIME: response.Header.Get("Content-Type")}
	empty := request.Method == http.MethodHead || response.StatusCode == http.StatusNotModified || response.StatusCode == http.StatusNoContent
	if response.StatusCode == http.StatusSwitchingProtocols || !empty && response.ContentLength > selectedPhase1AssetLimit || len(record.MIME) > 256 {
		response.Body.Close()
		record.ErrorCode = "original_client_asset_response_invalid"
		host.finish(index, record)
		return nil, errors.New(record.ErrorCode)
	}
	response.Body = &selectedPhase1AssetBody{ReadCloser: response.Body, host: host, ctx: request.Context(), index: index,
		record: record, digest: sha256.New(), empty: empty}
	return response, nil
}

func selectedPhase1AssetRequest(request *http.Request) bool {
	if (request.Method != http.MethodGet && request.Method != http.MethodHead) ||
		request.ContentLength != 0 || len(request.TransferEncoding) != 0 || request.Header.Get("Upgrade") != "" ||
		request.URL.User != nil || !strings.HasPrefix(request.URL.Path, "/web/") || len(request.URL.Path) > 1024 ||
		strings.ContainsAny(request.URL.Path, "\\%\x00") || request.URL.Path != "/web/" && path.Clean(request.URL.Path) != request.URL.Path {
		return false
	}
	escaped := strings.ToLower(request.URL.RawPath)
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") || strings.Contains(escaped, "%2e") || strings.Contains(escaped, "%25") {
		return false
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(request.URL.RawQuery) > 4096 {
		return false
	}
	for key := range query {
		lower := strings.ToLower(key)
		if lower == "pw" || lower == "pin" || lower == "profilepin" || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") ||
			strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.HasPrefix(lower, "x-emby-") || strings.HasPrefix(lower, "x-mediabrowser-") {
			return false
		}
	}
	return true
}

func (host *selectedPhase1ClientHost) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if !selectedPhase1AssetRequest(request) {
		host.fail("original_client_asset_request_scope_invalid")
		http.Error(w, "Original client asset request is outside the fixture scope.", http.StatusForbidden)
		return
	}
	host.mu.Lock()
	if host.failure != "" || len(host.records) >= selectedPhase1AssetCountLimit {
		if host.failure == "" {
			host.failure = "original_client_asset_request_bound_exceeded"
		}
		host.mu.Unlock()
		http.Error(w, "Original client asset proxy is unavailable.", http.StatusBadGateway)
		return
	}
	index := len(host.records)
	host.records = append(host.records, selectedPhase1AssetRecord{Method: request.Method, Path: request.URL.Path})
	host.mu.Unlock()
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, selectedPhase1AssetContextKey{}, index)
	host.proxy.ServeHTTP(w, request.WithContext(ctx))
}

func selectedPhase1OpenClientHost(configPath, output, runID string) (*selectedPhase1ClientHost, error) {
	var raw json.RawMessage
	if err := featureWavePrivateJSON(configPath, 16<<10, &raw); err != nil {
		return nil, errors.New("original_client_host_private_config_required")
	}
	var config selectedPhase1ClientHostConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || config.Marker != "goby-selected-phase1-client-host-v1" ||
		config.Origin != "http://127.0.0.1:28497" || config.PID != 366598 || config.StartTicks != 506485 ||
		config.BootID != "4de83999-7586-4716-83d1-0d81c9343126" || config.NetworkNamespace != "net:[4026532544]" ||
		config.Executable != selectedPhase1OriginalHostExecutable ||
		config.ExecutableSHA256 != "c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2" ||
		config.ServerID != "9b45875a4abd417fb19ef7f71ad81e7a" || config.Version != "4.9.5.0" {
		return nil, errors.New("original_client_host_config_identity_mismatch")
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Host != "127.0.0.1:28497" || origin.Path != "" || origin.RawQuery != "" || origin.User != nil {
		return nil, errors.New("original_client_host_origin_invalid")
	}
	host := &selectedPhase1ClientHost{config: config, origin: origin, output: output, runID: runID, budgetChanged: make(chan struct{}),
		records: []selectedPhase1AssetRecord{}}
	if err := host.verify(true); err != nil {
		return nil, err
	}
	host.transport = &http.Transport{DialContext: host.dial, DisableCompression: true, DisableKeepAlives: true,
		MaxConnsPerHost: 16, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	client := &http.Client{Transport: host.transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequest(http.MethodGet, config.Origin+"/emby/System/Info/Public", nil)
	if err != nil {
		return nil, errors.New("original_client_host_public_identity_request_invalid")
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		host.transport.CloseIdleConnections()
		return nil, errors.New("original_client_host_public_identity_unavailable")
	}
	public, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	closeErr := response.Body.Close()
	identityErr := host.verify(false)
	var identity struct { ID string `json:"Id"`; Version string }
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || len(public) > 64<<10 ||
		json.Unmarshal(public, &identity) != nil || identity.ID != config.ServerID || identity.Version != config.Version || identityErr != nil {
		host.transport.CloseIdleConnections()
		return nil, errors.New("original_client_host_public_identity_mismatch")
	}
	host.publicRead = true
	host.proxy = &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(host.origin)
			request.Out.URL.User = nil
			request.Out.Header = make(http.Header)
			for _, name := range []string{"Accept", "Accept-Language", "Cache-Control", "If-Modified-Since", "If-None-Match", "If-Range", "Range", "User-Agent"} {
				for _, value := range request.In.Header.Values(name) {
					request.Out.Header.Add(name, value)
				}
			}
			request.Out.Header.Set("Accept-Encoding", "identity")
		},
		Transport: selectedPhase1AssetTransport{host: host}, ErrorLog: log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, request *http.Request, err error) {
			host.fail("original_client_asset_proxy_failed")
			http.Error(w, "Original client asset proxy failed.", http.StatusBadGateway)
		},
	}
	host.mu.Lock()
	err = host.flushLocked()
	host.mu.Unlock()
	if err != nil {
		return nil, errors.New("original_client_asset_evidence_write_failed")
	}
	return host, nil
}

func (host *selectedPhase1ClientHost) close() error {
	host.transport.CloseIdleConnections()
	if err := host.verify(true); err != nil {
		host.fail("original_client_host_identity_changed_after_browser")
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, record := range host.records {
		if !record.Complete && host.failure == "" {
			host.failure = "original_client_asset_response_incomplete"
		}
	}
	if err := host.flushLocked(); err != nil {
		return errors.New("original_client_asset_evidence_write_failed")
	}
	if host.failure != "" {
		return errors.New(host.failure)
	}
	return nil
}

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

// The product serves its native administration at /admin/. This owned fixture
// transparently proxies the pinned original host's consumer assets at /web/ on
// the same private origin; every API request reaches the actual application.
// No client files, host-generated modules, or API responses are substituted.
type selectedPhase1BrowserRuntime struct {
	f      *serverFixture
	assets fs.FS
	client *selectedPhase1ClientHost
	server *httptest.Server
	addr   string
}

func (runtime *selectedPhase1BrowserRuntime) listen() error {
	if runtime.client == nil {
		return errors.New("selected phase 1 requires its pinned original client host")
	}
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
		if strings.HasPrefix(r.URL.Path, "/web/") {
			runtime.client.ServeHTTP(w, r)
			return
		}
		application.ServeHTTP(w, r)
	})
	actual := httptest.NewUnstartedServer(runtime.f.handler)
	actual.Listener.Close()
	actual.Listener = listener
	actual.Start()
	runtime.server = actual
	return nil
}

func (runtime *selectedPhase1BrowserRuntime) close(ctx context.Context) error {
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

func (runtime *selectedPhase1BrowserRuntime) restart(ctx context.Context) error {
	if err := runtime.close(ctx); err != nil {
		return err
	}
	app, err := New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log,
		"selected-phase1-browser-integration", WithDashboardAssets(runtime.assets))
	if err != nil {
		return err
	}
	runtime.f.app = app
	return runtime.listen()
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
	runtime          *selectedPhase1BrowserRuntime
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
	clientHostConfig := refreshBrowserPath(t, "GOBY_SELECTED_PHASE1_CLIENT_HOST_CONFIG", false)
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("selected phase 1 artifact parent must be private")
	}
	runID := os.Getenv("GOBY_SELECTED_PHASE1_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("an explicit selected phase 1 browser run ID is required")
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
	clientHost, err := selectedPhase1OpenClientHost(clientHostConfig, output, runID)
	if err != nil {
		t.Fatal("admit the explicitly pinned original client host: " + err.Error())
	}
	driver["OriginalClientHostIdentityPinned"] = true
	driver["OriginalClientHostLifecycleMutations"] = 0
	t.Cleanup(func() {
		if err := clientHost.close(); err != nil {
			driver["OriginalClientHostFailure"] = err.Error()
			t.Error("close selected phase 1 original client evidence: " + err.Error())
		} else {
			driver["OriginalClientHostIdentityPreserved"] = true
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
	runtime := &selectedPhase1BrowserRuntime{f: f, assets: assets, client: clientHost, addr: "127.0.0.1:0"}
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
