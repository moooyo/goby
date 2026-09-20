//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
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
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/transcode"
	adminassets "github.com/moooyo/goby/web/admin"
)

var selectedPhase2Phases = []string{"authentication", "removal-ready", "removal-applied", "ocr-ready", "ocr-reviewed",
	"ocr-applied", "subtitle-selected", "subtitle-off", "subtitle-reselected", "subtitle-stopped",
	"cancel-ready", "cancelled", "artwork", "restart", "persisted", "cleanup"}

type selectedPhase2Execution struct {
	Marker                         string
	FFmpegPath, FFmpegSHA256       string
	FFprobePath, FFprobeSHA256     string
	PythonPath, PythonSHA256       string
	FontPath, FontSHA256           string
	HlsBundlePath, HlsBundleSHA256 string
	OCR                            config.MediaOperationsOCRConfig
}

type selectedPhase2FileFact struct {
	Path                     string
	Device, Inode            uint64
	Bytes, Modified, Changed int64
	Mode                     uint32
	SHA256                   string
}

type selectedPhase2ExpectedCue struct {
	StartTicks int64  `json:"start_ticks"`
	EndTicks   int64  `json:"end_ticks"`
	Text       string `json:"text"`
	Forced     bool   `json:"forced"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RGBAHash   string `json:"rgba_sha256"`
}

type selectedPhase2BitmapManifest struct {
	Format        string `json:"format"`
	DurationTicks int64  `json:"duration_ticks"`
	Cases         []struct {
		Name      string                      `json:"name"`
		File      string                      `json:"pgs_matroska_file"`
		Origin    int64                       `json:"pgs_container_origin_ticks"`
		Models    []string                    `json:"ocr_models"`
		Intervals []selectedPhase2ExpectedCue `json:"pgs_intervals"`
	} `json:"cases"`
	Files map[string]struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// Only this private temporary context contains passwords. No token or database
// URL is passed to the browser child or written into evidence summaries.
type selectedPhase2Context struct {
	Marker                                             string
	RunID                                              string `json:"RunId"`
	BaseURL                                            string
	AdminID                                            string `json:"AdminId"`
	AdminName, AdminPassword                           string
	ViewerID                                           string `json:"ViewerId"`
	ViewerName, ViewerPassword                         string
	LibraryID                                          string `json:"LibraryId"`
	LibraryName                                        string
	AudioLibraryID                                     string `json:"AudioLibraryId"`
	AudioLibraryName                                   string
	RemoveItemID                                       string `json:"RemoveItemId"`
	RemoveItemName                                     string
	OCRItemID                                          string `json:"OCRItemId"`
	OCRItemName                                        string
	AudioItemID                                        string `json:"AudioItemId"`
	AudioItemName                                      string
	SecondAudioItemID                                  string `json:"SecondAudioItemId"`
	RemoveStreamIndex, KeepStreamIndex, OCRStreamIndex int
	RemoveSubtitleTitle, KeepSubtitleTitle             string
	OCRModelIDs                                        []string `json:"ModelIds"`
	OCRLanguage, OCRTitle, ExpectedOCRPhrase           string
	OCRExpectedCues                                    []selectedPhase2ExpectedCue
	ReviewEdits                                        []library.MediaOperationCueEdit
	HlsBundlePath, HlsBundleSHA256                     string
	ArtifactsDir, ResultPath                           string
}

type selectedPhase2Request struct {
	RunID                                    string `json:"RunId"`
	Phase                                    string
	OperationID                              string `json:"OperationId"`
	Revision                                 string
	ResultHash                               string
	CueEdits                                 []library.MediaOperationCueEdit
	HistoryIDs                               []string `json:"HistoryIds"`
	PlaySessionID                            string   `json:"PlaySessionId"`
	DeviceID                                 string   `json:"DeviceId"`
	MediaSourceID                            string   `json:"MediaSourceId"`
	StreamIndex                              int
	HlsID                                    string `json:"HlsId"`
	EngineSHA256, EngineVersion              string
	AuthenticationStatus, PlaybackInfoStatus int
}

type selectedPhase2Result struct {
	Marker                      string
	RunID                       string `json:"RunId"`
	Complete                    bool
	Checks                      map[string]bool
	PageErrors, ForeignRequests *int
	Stages                      []struct{ Phase, State string }
}

// Classify failures without serializing error messages, connection strings,
// source paths, rejected values, or authentication material.
func selectedPhase2SafeError(err error) string {
	if err == nil {
		return `{"Category":"none"}`
	}
	value := map[string]any{}
	value["Type"] = reflect.TypeOf(err).String()
	value["Category"] = "unclassified_error"
	for _, candidate := range []struct {
		name string
		err  error
	}{
		{"context_deadline", context.DeadlineExceeded}, {"context_cancelled", context.Canceled},
		{"not_found", os.ErrNotExist}, {"permission_denied", os.ErrPermission}, {"already_exists", os.ErrExist},
		{"library_invalid_input", library.ErrInvalidInput}, {"library_forbidden", library.ErrForbidden},
		{"library_not_found", library.ErrNotFound}, {"metadata_revision_conflict", library.ErrRevisionConflict},
		{"identity_invalid_input", identity.ErrInvalidInput}, {"identity_unauthorized", identity.ErrUnauthorized},
		{"identity_invalid_credentials", identity.ErrInvalidCredentials}, {"already_initialized", identity.ErrAlreadyInitialized},
		{"subtitle_removal_unsupported", media.ErrSubtitleRemovalUnsupported}, {"subtitle_removal_budget", media.ErrSubtitleRemovalBudget},
		{"source_changed", library.ErrSourceChanged}, {"media_operation_conflict", library.ErrMediaOperationConflict},
		{"media_operation_recovery", library.ErrMediaOperationRecovery},
	} {
		if errors.Is(err, candidate.err) {
			value["Category"] = candidate.name
			break
		}
	}
	var validation *library.MetadataValidationError
	if errors.As(err, &validation) {
		value["Category"] = "metadata_validation"
		known := []string{"Revision", "Overrides", "LockedFields", "Name", "SortName", "Overview", "OriginalTitle", "OfficialRating", "ProductionYear", "PremiereDate", "CommunityRating", "ProviderIds", "Genres", "Tags", "Studios", "People", "IndexNumber", "ParentIndexNumber", "Album", "Artists", "AlbumArtists"}
		fields, unknown := []string{}, 0
		for field := range validation.Fields {
			if slices.Contains(known, field) {
				fields = append(fields, field)
			} else {
				unknown++
			}
		}
		slices.Sort(fields)
		value["Fields"], value["UnknownFieldCount"] = fields, unknown
	}
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		value["Category"] = "postgres_error"
		if regexp.MustCompile(`^[0-9A-Z]{5}$`).MatchString(postgres.Code) {
			value["SQLState"] = postgres.Code
		}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		value["ExitCode"] = exit.ExitCode()
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		value["Category"], value["Offset"] = "json_syntax", syntax.Offset
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		value["Category"], value["Offset"] = "json_type", typeError.Offset
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func selectedPhase2Fatal(t *testing.T, operation string, err error) {
	t.Helper()
	t.Fatalf("%s: %s", operation, selectedPhase2SafeError(err))
}

type selectedPhase2ExecutorDiagnostics struct {
	mu                          sync.Mutex
	directory, mediaRoot, runID string
	sources                     map[string]string
	secrets                     []string
	calls, failures, active     int
}

type selectedPhase2ObservedExecutor struct {
	delegate    mediaOperationExecutor
	diagnostics *selectedPhase2ExecutorDiagnostics
}

type selectedPhase2ExecutorCall struct {
	diagnostics                 *selectedPhase2ExecutorDiagnostics
	work                        library.MediaOperationWork
	phase, sourcePath           string
	source, candidate           *os.File
	sourceError, candidateError error
	progress                    []library.MediaOperationProgress
	progressErrors              []json.RawMessage
	chainTruncated              bool
	number                      int
}

// Only this run's authored media can be retained. A read descriptor does not
// alter the source's bytes, file offset, ownership, link count, or publication.
func (diagnostics *selectedPhase2ExecutorDiagnostics) open(path string) (*os.File, error) {
	relative, err := filepath.Rel(diagnostics.mediaRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("diagnostic sample is outside owned media")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if resolved != path {
		return nil, errors.New("diagnostic sample alias is not admitted")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || info.Mode().Perm()&0o022 != 0 || info.Size() < 0 || info.Size() > 32<<20 {
		file.Close()
		return nil, errors.New("diagnostic sample identity or size is not admitted")
	}
	return file, nil
}

func (diagnostics *selectedPhase2ExecutorDiagnostics) begin(work library.MediaOperationWork, phase string) *selectedPhase2ExecutorCall {
	diagnostics.mu.Lock()
	diagnostics.calls++
	diagnostics.active++
	number := diagnostics.calls
	diagnostics.mu.Unlock()
	call := &selectedPhase2ExecutorCall{diagnostics: diagnostics, work: work, phase: phase, number: number, sourcePath: diagnostics.sources[work.Operation.ItemID]}
	if call.sourcePath != "" {
		call.source, call.sourceError = diagnostics.open(call.sourcePath)
	}
	if phase != "execute" {
		call.openCandidate()
	}
	return call
}

func (call *selectedPhase2ExecutorCall) openCandidate() {
	if call.candidate != nil || call.sourcePath == "" || call.work.Operation.Kind != library.MediaOperationRemoveSubtitle ||
		!regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(call.work.Operation.ID) {
		return
	}
	call.candidate, call.candidateError = call.diagnostics.open(filepath.Join(filepath.Dir(call.sourcePath), ".goby-edit-"+call.work.Operation.ID, "payload"))
}

func (call *selectedPhase2ExecutorCall) observe(progress func(library.MediaOperationProgress) error) func(library.MediaOperationProgress) error {
	return func(value library.MediaOperationProgress) error {
		// Forward the exact progress value and return the exact callback error.
		err := progress(value)
		if len(call.progress) < 16 {
			call.progress = append(call.progress, value)
			call.progressErrors = append(call.progressErrors, json.RawMessage(selectedPhase2SafeError(err)))
		}
		// The real executor creates its candidate before reporting this stage.
		// Hold only a read descriptor until it returns, so its own failure cleanup
		// can unlink normally without destroying the diagnostic inode evidence.
		if value.Stage == "remuxing" || value.Stage == "copying" {
			call.openCandidate()
		}
		return err
	}
}

func (call *selectedPhase2ExecutorCall) close() {
	failed := false
	if call.source != nil {
		failed = call.source.Close() != nil
	}
	if call.candidate != nil {
		failed = call.candidate.Close() != nil || failed
	}
	call.diagnostics.mu.Lock()
	call.diagnostics.active--
	if failed {
		call.diagnostics.failures++
	}
	call.diagnostics.mu.Unlock()
}

func (diagnostics *selectedPhase2ExecutorDiagnostics) redact(message string, work library.MediaOperationWork) string {
	secrets := append(append([]string(nil), diagnostics.secrets...), work.Token, work.Operation.WorkerToken)
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, encoded := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			message = strings.ReplaceAll(message, encoded, "[credential redacted]")
		}
	}
	connection := regexp.MustCompile(`(?i)(postgres(?:ql)?://|\b(?:database_url|dsn|host|dbname|sslmode|user)\s*=)`)
	for _, line := range strings.Split(message, "\n") {
		if connection.MatchString(line) {
			message = strings.ReplaceAll(message, line, "[connection diagnostic redacted]")
		}
	}
	message = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`).ReplaceAllString(message, "[URI redacted]")
	credential := regexp.MustCompile(`(?i)(?:["']?)(?:x-emby-token|api_key|access_?token|refresh_?token|authorization|cookie|password|passwd|pwd|secret|token|profilepin|pin)(?:["']?)\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	return credential.ReplaceAllString(message, "[credential field redacted]")
}

func (call *selectedPhase2ExecutorCall) errorChain(err error) []map[string]any {
	result := []map[string]any{}
	var visit func(error, int)
	visit = func(current error, depth int) {
		if current == nil {
			return
		}
		if len(result) >= 32 || depth > 12 {
			call.chainTruncated = true
			return
		}
		message := call.diagnostics.redact(current.Error(), call.work)
		truncated := len(message) > 64<<10
		if truncated {
			message = message[:64<<10]
		}
		result = append(result, map[string]any{"Depth": depth, "Classification": json.RawMessage(selectedPhase2SafeError(current)), "Message": message, "MessageTruncated": truncated})
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				visit(child, depth+1)
			}
		} else {
			visit(errors.Unwrap(current), depth+1)
		}
	}
	visit(err, 0)
	return result
}

func (call *selectedPhase2ExecutorCall) sample(directory, role string, file *os.File, openErr error) map[string]any {
	value := map[string]any{"Role": role, "Saved": false}
	if file == nil {
		value["OpenError"] = json.RawMessage(selectedPhase2SafeError(openErr))
		return value
	}
	before, err := file.Stat()
	if err != nil {
		value["ReadError"] = json.RawMessage(selectedPhase2SafeError(err))
		return value
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > 32<<20 {
		value["Error"] = "sample_size_or_type_changed"
		return value
	}
	value["Bytes"], value["Device"], value["Inode"], value["Links"] = before.Size(), uint64(stat.Dev), stat.Ino, stat.Nlink
	value["Modified"], value["Changed"], value["UnlinkedAtCapture"] = before.ModTime().UnixNano(), stat.Ctim.Nano(), stat.Nlink == 0
	filename := role + ".bin"
	output, err := os.OpenFile(filepath.Join(directory, filename), os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		value["WriteError"] = json.RawMessage(selectedPhase2SafeError(err))
		return value
	}
	digest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(output, digest), io.NewSectionReader(file, 0, before.Size()))
	syncErr, closeErr := output.Sync(), output.Close()
	after, statErr := file.Stat()
	stable := statErr == nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && media.FileChangeTime(before) == media.FileChangeTime(after)
	value["File"], value["CopiedBytes"], value["SHA256"], value["StableDuringCopy"] = filename, n, hex.EncodeToString(digest.Sum(nil)), stable
	value["Saved"] = copyErr == nil && syncErr == nil && closeErr == nil && n == before.Size() && stable
	if err := errors.Join(copyErr, syncErr, closeErr, statErr); err != nil {
		value["CopyError"] = json.RawMessage(selectedPhase2SafeError(err))
	}
	return value
}

func (call *selectedPhase2ExecutorCall) finish(result library.MediaOperationResult, executionErr error) {
	directoryName := "executor-" + strconv.Itoa(call.number) + "-" + call.phase
	directory := filepath.Join(call.diagnostics.directory, directoryName)
	failed := func() { call.diagnostics.mu.Lock(); call.diagnostics.failures++; call.diagnostics.mu.Unlock() }
	if err := os.Mkdir(directory, 0o700); err != nil {
		failed()
		return
	}
	chain := call.errorChain(executionErr)
	classifications := make([]json.RawMessage, 0, len(chain))
	for _, entry := range chain {
		classifications = append(classifications, entry["Classification"].(json.RawMessage))
	}
	evidence := map[string]any{"Marker": "goby-selected-phase2-executor-diagnostic-v1", "RunId": call.diagnostics.runID,
		"OperationId": call.work.Operation.ID, "Kind": call.work.Operation.Kind, "Phase": call.phase, "StreamIndex": call.work.Operation.StreamIndex,
		"SourceRevision": call.work.Operation.SourceRevision, "ResultHash": result.ResultHash, "Progress": call.progress, "ProgressCallbackErrors": call.progressErrors, "Succeeded": executionErr == nil,
		"OriginalResultAndErrorReturned": true, "ErrorClassifications": classifications}
	if snapshot, err := decodeMediaOperationExecution(call.work.Operation.ExecutionSnapshot); err == nil {
		evidence["ExecutionBudget"] = map[string]any{"MaxRuntimeSeconds": snapshot.Configuration.MaxRuntimeSeconds, "MaxScratchBytes": snapshot.Configuration.MaxScratchBytes, "Profile": call.work.Operation.Parameters.Profile}
	}
	if executionErr != nil {
		evidence["Samples"] = []map[string]any{call.sample(directory, "source-at-entry", call.source, call.sourceError), call.sample(directory, "candidate-held-descriptor", call.candidate, call.candidateError)}
		private := map[string]any{"Marker": "goby-selected-phase2-private-executor-error-v1", "RunId": call.diagnostics.runID,
			"OperationId": call.work.Operation.ID, "Phase": call.phase, "CredentialRedactionApplied": true, "Chain": chain, "ChainTruncated": call.chainTruncated, "NodeLimit": 32, "DepthLimit": 12, "MessageByteLimit": 64 << 10}
		if err := refreshBrowserWriteJSON(filepath.Join(directory, "private-error.json"), private); err != nil {
			failed()
		}
	}
	if err := refreshBrowserWriteJSON(filepath.Join(directory, "receipt.json"), evidence); err != nil {
		failed()
	}
}

func (executor selectedPhase2ObservedExecutor) Execute(ctx context.Context, work library.MediaOperationWork, progress func(library.MediaOperationProgress) error) (library.MediaOperationResult, error) {
	call := executor.diagnostics.begin(work, "execute")
	defer call.close()
	result, err := executor.delegate.Execute(ctx, work, call.observe(progress))
	call.finish(result, err)
	return result, err
}
func (executor selectedPhase2ObservedExecutor) Apply(ctx context.Context, work library.MediaOperationWork, progress func(library.MediaOperationProgress) error) error {
	call := executor.diagnostics.begin(work, "apply")
	defer call.close()
	err := executor.delegate.Apply(ctx, work, call.observe(progress))
	call.finish(library.MediaOperationResult{}, err)
	return err
}
func (executor selectedPhase2ObservedExecutor) Discard(ctx context.Context, work library.MediaOperationWork) error {
	call := executor.diagnostics.begin(work, "discard")
	defer call.close()
	err := executor.delegate.Discard(ctx, work)
	call.finish(library.MediaOperationResult{}, err)
	return err
}

// These two private fixture routes host only a fixed test document and the
// unchanged pinned open-source HLS engine. Every business route remains Goby.
type selectedPhase2Runtime struct {
	*phase3BrowserRuntime
	bundle      []byte
	diagnostics *selectedPhase2ExecutorDiagnostics
}

func (runtime *selectedPhase2Runtime) listen() error {
	if runtime.diagnostics != nil {
		var pending int
		if err := runtime.f.pool.QueryRow(runtime.f.ctx, `SELECT count(*) FROM media_operations WHERE state IN ('queued','running','applying') OR worker_token<>''`).Scan(&pending); err != nil || pending != 0 {
			return errors.New("phase 2 diagnostics require a quiescent executor before listener admission")
		}
		// Join before replacing the executor map: the product's execute reader
		// intentionally does not share its coordinator mutex. Reuse the already
		// admitted real inventory; do not invoke another probe or recovery pass.
		previous := runtime.f.app.mediaOperations
		ctx, cancel := context.WithTimeout(runtime.f.ctx, 20*time.Second)
		closeErr := previous.Close(ctx)
		cancel()
		if closeErr != nil || len(previous.workers) != 0 {
			return errors.New("phase 2 executor instrumentation could not join the idle coordinator")
		}
		lifetime, stop := context.WithCancel(context.Background())
		current := &mediaOperationsRuntime{server: previous.server, store: previous.store, configuration: previous.configuration,
			inventory: previous.inventory, toolsReady: previous.toolsReady, ocrReady: previous.ocrReady, processingEnabled: previous.processingEnabled,
			ctx: lifetime, cancel: stop, wake: make(chan struct{}, 1), done: make(chan struct{}), workers: map[string]*mediaOperationExecution{},
			executors: map[string]mediaOperationExecutor{}, loopStarted: true}
		for _, kind := range []string{library.MediaOperationRemoveSubtitle, library.MediaOperationOCR} {
			delegate, ok := previous.executors[kind].(mediaOperationBuiltinExecutor)
			if !ok {
				stop()
				return errors.New("phase 2 instrumentation requires the unchanged real builtin executor")
			}
			delegate.runtime = current
			current.executors[kind] = selectedPhase2ObservedExecutor{delegate: delegate, diagnostics: runtime.diagnostics}
		}
		runtime.f.app.mediaOperations = current
		go current.loop()
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
		if r.URL.Path != "/__selected-phase2-media" && r.URL.Path != "/__selected-phase2-hls.js" {
			application.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Use GET for the owned media fixture.", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/__selected-phase2-hls.js" {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(runtime.bundle)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; media-src 'self' blob:; connect-src 'self'; img-src 'self' data:")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>Owned subtitle consumption</title></head><body><video id="phase2-media" muted playsinline controls width="640"></video><canvas id="phase2-pixel" width="1" height="1"></canvas></body></html>`)
	})
	actual := httptest.NewUnstartedServer(runtime.f.handler)
	actual.Listener.Close()
	actual.Listener = listener
	actual.Start()
	runtime.server = actual
	return nil
}

func (runtime *selectedPhase2Runtime) restart(ctx context.Context) error {
	if err := runtime.close(ctx); err != nil {
		return err
	}
	app, err := New(runtime.f.ctx, runtime.f.cfg, runtime.f.pool, runtime.f.users, runtime.f.log, "selected-phase2-browser-integration", WithDashboardAssets(runtime.assets))
	if err != nil {
		return err
	}
	runtime.f.app = app
	return runtime.listen()
}

func selectedPhase2Fact(path string, maximum int64) (selectedPhase2FileFact, error) {
	var result selectedPhase2FileFact
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return result, errors.New("phase 2 inventory paths must be canonical absolute paths")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return result, errors.New("phase 2 file is not a bounded regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	resolved, resolveErr := filepath.EvalSymlinks(path)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 || resolveErr != nil || resolved != path || info.Mode().Perm()&0o022 != 0 {
		return result, errors.New("phase 2 file identity or ownership is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, errors.New("phase 2 file could not be opened")
	}
	defer file.Close()
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maximum+1))
	after, statErr := file.Stat()
	if err != nil || statErr != nil || n != info.Size() || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) || media.FileChangeTime(info) != media.FileChangeTime(after) {
		return result, errors.New("phase 2 file changed during its bounded hash observation")
	}
	return selectedPhase2FileFact{Path: path, Device: uint64(stat.Dev), Inode: stat.Ino, Bytes: n,
		Modified: info.ModTime().UnixNano(), Changed: stat.Ctim.Nano(), Mode: uint32(info.Mode().Perm()), SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func selectedPhase2ReadExecution(t *testing.T) (selectedPhase2Execution, []selectedPhase2FileFact) {
	t.Helper()
	path := refreshBrowserPath(t, "GOBY_SELECTED_PHASE2_EXECUTION_CONFIG", false)
	var raw json.RawMessage
	if err := featureWavePrivateJSON(path, 64<<10, &raw); err != nil {
		selectedPhase2Fatal(t, "read private phase 2 execution inventory", err)
	}
	var execution selectedPhase2Execution
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&execution); err != nil {
		selectedPhase2Fatal(t, "decode phase 2 execution inventory", err)
	}
	if execution.Marker != "goby-selected-phase2-execution-v1" {
		t.Fatal("phase 2 execution inventory has an invalid schema")
	}
	configuration := config.MediaOperationsConfig{Enabled: true, MaxConcurrent: 1, MaxQueued: 4, MaxRuntimeSeconds: 90,
		MaxScratchBytes: 512 << 20, ScratchDirectory: "/owned/phase2-scratch", WritableProfiles: []string{"matroska-v1"}, OCR: execution.OCR}
	if err := configuration.Validate(); err != nil {
		selectedPhase2Fatal(t, "validate phase 2 OCR configuration", err)
	}
	if execution.HlsBundleSHA256 != phase2BrowserBundleSHA256 {
		t.Fatal("phase 2 HLS bundle digest does not identify the reviewed engine")
	}
	checks := []struct{ role, path, digest string }{{"ffmpeg", execution.FFmpegPath, execution.FFmpegSHA256}, {"ffprobe", execution.FFprobePath, execution.FFprobeSHA256},
		{"python", execution.PythonPath, execution.PythonSHA256}, {"font", execution.FontPath, execution.FontSHA256}, {"tesseract", execution.OCR.Executable, execution.OCR.ToolSHA256},
		{"hls-bundle", execution.HlsBundlePath, execution.HlsBundleSHA256}}
	models := map[string]bool{}
	for _, model := range execution.OCR.Models {
		models[model.ID] = true
		checks = append(checks, struct{ role, path, digest string }{"model-" + model.ID, filepath.Join(execution.OCR.TessdataDirectory, model.Filename), model.SHA256})
	}
	if !models["eng"] || !models["chi_sim"] {
		t.Fatal("phase 2 overlap OCR requires the explicit eng and chi_sim models")
	}
	facts := make([]selectedPhase2FileFact, 0, len(checks))
	for _, input := range checks {
		fact, err := selectedPhase2Fact(input.path, 256<<20)
		if err != nil {
			t.Fatalf("phase 2 inventory %s: %v", input.role, err)
		}
		if len(input.digest) != 64 || fact.SHA256 != input.digest {
			t.Fatalf("phase 2 inventory %s: pinned SHA-256 mismatch", input.role)
		}
		facts = append(facts, fact)
	}
	return execution, facts
}

func selectedPhase2PNG(t *testing.T, path string, shade color.NRGBA) []byte {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			canvas.SetNRGBA(x, y, shade)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		selectedPhase2Fatal(t, "encode owned phase 2 cover pixels", err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		selectedPhase2Fatal(t, "write owned phase 2 cover pixels", err)
	}
	return encoded.Bytes()
}

func selectedPhase2RGBAHash(data []byte) (string, int, int, error) {
	picture, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, errors.New("phase 2 image did not decode")
	}
	bounds := picture.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 || bounds.Dx() > 4096 || bounds.Dy() > 4096 {
		return "", 0, 0, errors.New("phase 2 decoded image dimensions are invalid")
	}
	digest := sha256.New()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(picture.At(x, y)).(color.NRGBA)
			_, _ = digest.Write([]byte{pixel.R, pixel.G, pixel.B, pixel.A})
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), bounds.Dx(), bounds.Dy(), nil
}

func selectedPhase2GenerateVideo(t *testing.T, execution selectedPhase2Execution, output, bitmap, remove, keep, chapters string) {
	t.Helper()
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=navy:size=720x576:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=12"}
	if bitmap != "" {
		args = append(args, "-i", bitmap, "-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0", "-c:s", "copy", "-metadata:s:s:0", "language=eng", "-metadata:s:s:0", "title=PGS English Chinese")
	} else {
		args = append(args, "-i", remove, "-i", keep, "-f", "ffmetadata", "-i", chapters,
			"-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0", "-map", "3:s:0", "-map_metadata", "4", "-map_chapters", "4", "-c:s", "srt",
			"-metadata:s:s:0", "language=eng", "-metadata:s:s:0", "title=Remove English", "-disposition:s:0", "default",
			"-metadata:s:s:1", "language=fra", "-metadata:s:s:1", "title=Keep French", "-disposition:s:1", "0")
	}
	// Offset input audio by the AAC encoder's 1024-sample priming so muxing
	// leaves the independently authored PGS timeline at its actual zero origin.
	args = append(args, "-af", "asetpts=PTS+1024/SR/TB", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1", "-bf", "0", "-g", "24",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "64000", "-t", "12", "-avoid_negative_ts", "disabled", "-f", "matroska", output)
	hlsHTTPMediaCommand(t, execution.FFmpegPath, args...)
	if err := os.Chmod(output, 0o600); err != nil {
		selectedPhase2Fatal(t, "protect owned phase 2 video", err)
	}
}

func selectedPhase2Preservation(ctx context.Context, f *serverFixture, viewer string, items []string) (json.RawMessage, error) {
	var raw string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Identity',(SELECT jsonb_agg(jsonb_build_object('Id',id,'LibraryId',library_id,'RootId',root_id,'ParentId',parent_id,'Name',name,'Type',type,'Path',path) ORDER BY id) FROM items WHERE id=ANY($2::text[])),
		'Metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m WHERE item_id=ANY($2::text[])),
		'UserData',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('RowVersion',d.xmin::text) ORDER BY item_id) FROM user_item_data d WHERE user_id=$1 AND item_id=ANY($2::text[]))
	)::text`, viewer, items).Scan(&raw)
	return json.RawMessage(raw), err
}

func selectedPhase2Database(ctx context.Context, f *serverFixture) (json.RawMessage, error) {
	var raw string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object('Schema',current_schema(),
		'Operations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Kind',kind,'State',state,'Revision',revision::text,
			'ItemId',source_item_id,'StreamIndex',stream_index,'SourceRevision',source_revision,'Parameters',parameters,
			'ProgressStage',progress_stage,'Processed',processed,'Total',total,'ResultHash',result_hash,'ResultSummary',result_summary,
			'PublicationPhase',publication_phase,'WorkerPresent',worker_token<>'','CancelRequested',cancel_requested_at IS NOT NULL,
			'ErrorCode',error_code,'Started',started_at IS NOT NULL,'Finished',finished_at IS NOT NULL) ORDER BY created_at,id),'[]'::jsonb) FROM media_operations),
		'Cues',(SELECT COALESCE(jsonb_agg(jsonb_build_object('OperationId',operation_id,'Ordinal',ordinal,'OriginalStartTicks',original_start_ticks,
			'OriginalEndTicks',original_end_ticks,'OriginalText',original_text,'StartTicks',start_ticks,'EndTicks',end_ticks,'Text',text,
			'Included',included,'Confidence',confidence,'Warnings',warnings,'ImageSHA256',image_sha256,'ImageBytes',octet_length(image_png)) ORDER BY operation_id,ordinal),'[]'::jsonb) FROM media_operation_cues),
		'OwnedSubtitles',(SELECT COALESCE(jsonb_agg(jsonb_build_object('OperationId',operation_id,'ItemId',item_id,'StreamIndex',stream_index,
			'Codec',codec,'Language',language,'Title',title,'Active',active,'ContentSHA256',content_sha256,'Bytes',octet_length(content)) ORDER BY item_id,stream_index),'[]'::jsonb) FROM item_owned_subtitles),
		'EmbeddedArtwork',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',item_id,'Status',status,'SourceHash',source_hash,'Width',width,'Height',height) ORDER BY item_id),'[]'::jsonb) FROM item_embedded_artwork),
		'PlaybackHistory',(SELECT COALESCE(jsonb_agg(jsonb_build_object('PlaySessionId',p.id,'ItemId',p.item_id,'State',p.state,'Started',p.started_at IS NOT NULL,'Counted',p.counted,'AuthenticationRevoked',a.revoked_at IS NOT NULL) ORDER BY p.id),'[]'::jsonb) FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id),
		'EncodingHistory',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'PlaySessionId',play_session_id,'State',state,'OutputBytes',output_bytes,'ErrorCode',error_code) ORDER BY id),'[]'::jsonb) FROM encoding_jobs),
		'ActiveSessions',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'ActiveOperations',(SELECT count(*) FROM media_operations WHERE state IN ('queued','running','applying') OR worker_token<>'' OR publication_phase IN ('prepared','catalog_committed') OR state='recovery_required')
	)::text`).Scan(&raw)
	return json.RawMessage(raw), err
}

type selectedPhase2Observer struct {
	runtime                                                     *selectedPhase2Runtime
	fixture                                                     selectedPhase2Context
	execution                                                   selectedPhase2Execution
	actor                                                       identity.Principal
	actorToken                                                  string
	files                                                       map[string]selectedPhase2FileFact
	removePath, ocrPath, scratch                                string
	originalRemove                                              selectedPhase2FileFact
	removeMedia                                                 media.Info
	coverBytes                                                  [][]byte
	preservation                                                json.RawMessage
	removal, ocr, cancelled                                     string
	removeReady                                                 library.MediaOperation
	originalCues, reviewedCues                                  []library.MediaOperationCue
	reviewHash                                                  string
	reviewRevision                                              int64
	durable                                                     json.RawMessage
	imageObservations                                           []map[string]any
	completed                                                   int
	hlsPlayID, hlsAuthID, hlsDeviceID, hlsSourceID, hlsRevision string
	hlsStreamIndex                                              int
	hlsSessions                                                 []*hlsSession
	hlsProducerIDs                                              []string
	hlsObservation                                              map[string]any
}

type selectedPhase2CommandOutput struct {
	buffer  bytes.Buffer
	maximum int
}

func (output *selectedPhase2CommandOutput) Len() int       { return output.buffer.Len() }
func (output *selectedPhase2CommandOutput) String() string { return output.buffer.String() }

func (output *selectedPhase2CommandOutput) Write(data []byte) (int, error) {
	if output.Len()+len(data) > output.maximum {
		return 0, errors.New("phase 2 media command output exceeded its bound")
	}
	return output.buffer.Write(data)
}

func (observer *selectedPhase2Observer) decodePublishedRemoval(ctx context.Context, source selectedPhase2FileFact) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tool, toolErr := selectedPhase2Fact(observer.execution.FFmpegPath, 256<<20)
	if toolErr != nil || tool.SHA256 != observer.execution.FFmpegSHA256 {
		return errors.New("phase 2 published decode FFmpeg identity differs")
	}
	stdout, stderr := &selectedPhase2CommandOutput{maximum: 64 << 10}, &selectedPhase2CommandOutput{maximum: 64 << 10}
	command := exec.CommandContext(ctx, observer.execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode",
		"-threads", "1", "-filter_threads", "1", "-i", observer.removePath, "-map", "0:v:0", "-map", "0:a:0", "-sn", "-dn",
		"-c:v", "wrapped_avframe", "-c:a", "pcm_s16le", "-threads:v", "1", "-threads:a", "1", "-fps_mode", "passthrough",
		"-progress", "pipe:1", "-nostats", "-f", "null", "-")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC"}
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = 3 * time.Second
	runErr := command.Run()
	progress := map[string]string{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if key, value, found := strings.Cut(strings.TrimSpace(line), "="); found {
			progress[key] = value
		}
	}
	frames, frameErr := strconv.Atoi(progress["frame"])
	micros, timeErr := strconv.ParseInt(progress["out_time_us"], 10, 64)
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	after, fileErr := selectedPhase2Fact(observer.removePath, 32<<20)
	complete := runErr == nil && exitCode == 0 && stderr.Len() == 0 && frameErr == nil && frames == 288 && timeErr == nil && micros >= 11_900_000 && micros <= 12_200_000 && progress["progress"] == "end" && fileErr == nil && after == source
	evidence := map[string]any{"RunId": observer.fixture.RunID, "Complete": complete, "SourceSHA256": source.SHA256, "FFmpegSHA256": observer.execution.FFmpegSHA256,
		"ExitCode": exitCode, "DecodedVideoFrames": frames, "OutputTimeMicroseconds": micros, "ExpectedVideoFrames": 288, "ExpectedSeconds": 12,
		"MappedAudioStreams": 1, "MappedVideoStreams": 1, "ProgressEnded": progress["progress"] == "end", "StderrBytes": stderr.Len(), "SourceUnchanged": fileErr == nil && after == source}
	if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "published-removal-decode.json"), evidence); err != nil {
		return errors.New("phase 2 full decode evidence could not be preserved")
	}
	if !complete {
		return errors.New("phase 2 published removal did not fully decode its actual twelve-second audio and video")
	}
	return observer.viewerHTTP(ctx, func(token string, client *http.Client) error {
		path := "/emby/Videos/" + observer.fixture.RemoveItemID + "/" + media.SourceID(observer.fixture.RemoveItemID) + "/Subtitles/" + strconv.Itoa(observer.fixture.KeepStreamIndex) + "/0/Stream.vtt"
		data, header, err := selectedPhase2HTTP(ctx, client, observer.fixture.BaseURL, path, token)
		if err != nil || !strings.HasPrefix(header.Get("Content-Type"), "text/vtt") {
			return errors.New("phase 2 retained French subtitle HTTP read failed")
		}
		document, err := subtitle.Parse(data, subtitle.FormatWebVTT)
		if err != nil || len(document.Cues) != 1 || document.Cues[0].Text != "Conserver cette piste" || document.Cues[0].StartTicks != 20_000_000 || document.Cues[0].EndTicks != 40_000_000 {
			return errors.New("phase 2 retained French subtitle lost its authored text or timing")
		}
		digest := sha256.Sum256(data)
		return featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "retained-subtitle-http.json"), map[string]any{"RunId": observer.fixture.RunID, "Complete": true, "Status": 200, "SHA256": hex.EncodeToString(digest[:]), "Bytes": len(data), "CueCount": 1, "StreamIndex": observer.fixture.KeepStreamIndex, "TokenInURL": false})
	})
}

func (observer *selectedPhase2Observer) idle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		var active int
		err := observer.runtime.f.pool.QueryRow(ctx, `SELECT count(*) FROM media_operations WHERE state IN ('queued','running','applying') OR worker_token<>'' OR publication_phase IN ('prepared','catalog_committed') OR state='recovery_required'`).Scan(&active)
		if err != nil {
			return errors.New("phase 2 execution closure observation failed")
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 2 execution workers did not close")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (observer *selectedPhase2Observer) hlsScope(request selectedPhase2Request) bool {
	return regexp.MustCompile(`^play_[0-9a-f]{32}$`).MatchString(request.PlaySessionID) &&
		request.DeviceID == "phase2-subtitles-"+observer.fixture.RunID && request.MediaSourceID == media.SourceID(observer.fixture.OCRItemID) &&
		request.EngineSHA256 == phase2BrowserBundleSHA256 && request.EngineVersion == "1.6.0-beta.2" && request.AuthenticationStatus == 200 && request.PlaybackInfoStatus == 200 &&
		(observer.hlsPlayID == "" || request.PlaySessionID == observer.hlsPlayID && request.DeviceID == observer.hlsDeviceID && request.MediaSourceID == observer.hlsSourceID && request.StreamIndex == observer.hlsStreamIndex && request.HlsID == observer.hlsRevision)
}

func (observer *selectedPhase2Observer) hlsLive(ctx context.Context, request selectedPhase2Request) error {
	if !observer.hlsScope(request) {
		return errors.New("phase 2 browser HLS scope or pinned engine differs")
	}
	f := observer.runtime.f
	var authID, state, subtitleHash string
	var started, counted, revoked, authUnexpired, playUnexpired bool
	err := f.pool.QueryRow(ctx, `SELECT p.auth_session_id,p.state,p.started_at IS NOT NULL,p.counted,a.revoked_at IS NOT NULL,a.expires_at>clock_timestamp(),p.expires_at>clock_timestamp()
		FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$1 AND p.user_id=$2 AND p.device_id=$3 AND p.item_id=$4 AND p.media_source_id=$5
		AND a.user_id=$2 AND a.device_id=$3 AND a.kind='emby'`, request.PlaySessionID, observer.fixture.ViewerID, request.DeviceID, observer.fixture.OCRItemID, request.MediaSourceID).
		Scan(&authID, &state, &started, &counted, &revoked, &authUnexpired, &playUnexpired)
	if err != nil || state != "Prepared" || started || counted || revoked || !authUnexpired || !playUnexpired {
		return errors.New("phase 2 HLS playback is not the live authorized unreported owned preparation")
	}
	owner := library.PlaybackOwner{UserID: observer.fixture.ViewerID, SessionID: authID, DeviceID: request.DeviceID, PeerIP: "127.0.0.1"}
	play, err := f.app.library.GetPlaybackSession(ctx, owner, request.PlaySessionID)
	if err != nil || play.ItemID != observer.fixture.OCRItemID || play.MediaSourceID != request.MediaSourceID {
		return errors.New("phase 2 product playback authorization rejected the browser HLS scope")
	}
	if f.pool.QueryRow(ctx, `SELECT content_sha256 FROM item_owned_subtitles WHERE item_id=$1 AND operation_id=$2 AND stream_index=$3 AND active`, observer.fixture.OCRItemID, observer.ocr, request.StreamIndex).Scan(&subtitleHash) != nil {
		return errors.New("phase 2 HLS does not select the actually published OCR subtitle")
	}
	if f.app.hls == nil {
		return errors.New("phase 2 actual HLS runtime is unavailable")
	}
	f.app.hls.mu.Lock()
	sessions := []*hlsSession{}
	for _, session := range f.app.hls.sessions {
		if session.key.scope.AuthSessionID == authID && session.key.scope.PlaySessionID == request.PlaySessionID {
			sessions = append(sessions, session)
		}
	}
	f.app.hls.mu.Unlock()
	if len(sessions) != 1 {
		return errors.New("phase 2 browser did not retain exactly one actual HLS revision")
	}
	session := sessions[0]
	if session.id != request.HlsID || session.key.scope.UserID != observer.fixture.ViewerID || session.key.scope.DeviceID != request.DeviceID || session.key.scope.ItemID != observer.fixture.OCRItemID || session.key.scope.SourceID != request.MediaSourceID || session.key.plan.StartTicks != 0 || session.key.plan.OutputMode == "progressive" {
		return errors.New("phase 2 HLS runtime revision has a different source or owner")
	}
	tracks := transcode.PlanHLSSubtitles(session.key.plan)
	bound := false
	for slot := 0; slot < tracks.Count; slot++ {
		track := tracks.Tracks[slot]
		bound = bound || track.StreamIndex == request.StreamIndex && track.ExternalTag == subtitleHash
	}
	if !bound {
		return errors.New("phase 2 HLS producer does not bind the published subtitle content digest")
	}
	session.mu.Lock()
	closed := session.closed
	producers := append([]hlsProducer(nil), session.producers...)
	readers := session.progressiveReaders
	session.mu.Unlock()
	if closed || session.ctx.Err() != nil || len(producers) != 1 {
		return errors.New("phase 2 HLS producer is absent, retired, or duplicated")
	}
	ids := []string{}
	states := []string{}
	bytesWritten := int64(0)
	for _, producer := range producers {
		record, err := f.app.hls.manager.Snapshot(session.key.scope, producer.id)
		cache, cacheErr := os.Lstat(filepath.Join(f.cfg.Transcoding.CacheDirectory, producer.id))
		if err != nil || (record.State != "running" && record.State != "completed") || record.OutputBytes <= 0 || record.ErrorCode != "" || cacheErr != nil || !cache.IsDir() {
			return errors.New("phase 2 actual HLS job or retained output is not usable")
		}
		ids = append(ids, record.ID)
		states = append(states, record.State)
		bytesWritten += record.OutputBytes
	}
	var jobCount int
	if f.pool.QueryRow(ctx, `SELECT count(*) FROM encoding_jobs WHERE play_session_id=$1 AND auth_session_id=$2 AND user_id=$3 AND device_id=$4 AND item_id=$5`, request.PlaySessionID, authID, observer.fixture.ViewerID, request.DeviceID, observer.fixture.OCRItemID).Scan(&jobCount) != nil || jobCount != 1 {
		return errors.New("phase 2 HLS created an unexpected durable producer count")
	}
	if observer.hlsPlayID != "" && !reflect.DeepEqual(ids, observer.hlsProducerIDs) {
		return errors.New("phase 2 subtitle selection replaced its audio/video producer")
	}
	observer.hlsPlayID, observer.hlsAuthID, observer.hlsDeviceID, observer.hlsSourceID, observer.hlsRevision = request.PlaySessionID, authID, request.DeviceID, request.MediaSourceID, request.HlsID
	observer.hlsStreamIndex, observer.hlsSessions, observer.hlsProducerIDs = request.StreamIndex, sessions, ids
	observer.hlsObservation = map[string]any{"Complete": true, "Phase": request.Phase, "ProducerIds": ids, "ProducerStates": states, "HlsId": request.HlsID,
		"OutputBytes": bytesWritten, "Authorized": true, "HlsSessions": 1, "ProgressiveReaders": readers, "CachePresent": true, "PlaybackReportsSent": false, "SubtitleContentSHA256": subtitleHash}
	return nil
}

func (observer *selectedPhase2Observer) hlsStopped(ctx context.Context, request selectedPhase2Request) error {
	if observer.hlsPlayID == "" || !observer.hlsScope(request) {
		return errors.New("phase 2 HLS stop does not bind the observed producer")
	}
	f := observer.runtime.f
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		f.app.hls.mu.Lock()
		registered := 0
		for _, session := range f.app.hls.sessions {
			if session.key.scope.AuthSessionID == observer.hlsAuthID && session.key.scope.PlaySessionID == observer.hlsPlayID {
				registered++
			}
		}
		f.app.hls.mu.Unlock()
		closed := true
		readers := 0
		for _, session := range observer.hlsSessions {
			session.mu.Lock()
			closed = closed && session.closed && session.ctx.Err() != nil
			readers += session.progressiveReaders
			session.mu.Unlock()
		}
		cacheRetired := true
		for _, id := range observer.hlsProducerIDs {
			if _, err := os.Lstat(filepath.Join(f.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
				cacheRetired = false
			}
		}
		var activeAuth, plays, jobs, activeJobs, invalidJobs int
		err := f.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL),
			(SELECT count(*) FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id=$2 AND p.user_id=$1 AND p.auth_session_id=$3 AND p.device_id=$4 AND p.item_id=$5 AND p.media_source_id=$6 AND p.state='Prepared' AND p.started_at IS NULL AND NOT p.counted AND a.revoked_at IS NOT NULL),
			(SELECT count(*) FROM encoding_jobs WHERE play_session_id=$2 AND auth_session_id=$3),
			(SELECT count(*) FROM encoding_jobs WHERE play_session_id=$2 AND state IN ('queued','running')),
			(SELECT count(*) FROM encoding_jobs WHERE play_session_id=$2 AND state NOT IN ('completed','cancelled'))`, observer.fixture.ViewerID, observer.hlsPlayID, observer.hlsAuthID, observer.hlsDeviceID, observer.fixture.OCRItemID, observer.hlsSourceID).
			Scan(&activeAuth, &plays, &jobs, &activeJobs, &invalidJobs)
		if err != nil {
			return errors.New("phase 2 HLS retirement database observation failed")
		}
		resources := selectedPhase1RuntimeFacts(f)
		runtimeClosed := true
		for _, count := range resources {
			runtimeClosed = runtimeClosed && count == 0
		}
		observer.hlsObservation = map[string]any{"Complete": false, "Phase": request.Phase, "ProducerIds": observer.hlsProducerIDs, "HlsId": observer.hlsRevision,
			"HlsSessions": registered, "SessionContextsClosed": closed, "ProgressiveReaders": readers, "CacheRetired": cacheRetired,
			"ActiveViewerSessions": activeAuth, "PreservedPreparedPlays": plays, "PreservedJobRows": jobs, "ActiveJobs": activeJobs, "InvalidTerminalJobs": invalidJobs,
			"Runtime": resources, "ReaderClosureEvidence": "retired producer caches are removed only after processes and owned readers close"}
		if registered == 0 && closed && readers == 0 && cacheRetired && activeAuth == 0 && plays == 1 && jobs == len(observer.hlsProducerIDs) && activeJobs == 0 && invalidJobs == 0 && runtimeClosed {
			observer.hlsObservation["Complete"] = true
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 2 browser stop did not retire its actual HLS resources before fixture shutdown")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (observer *selectedPhase2Observer) filesVerified() ([]selectedPhase2FileFact, error) {
	paths := make([]string, 0, len(observer.files))
	for path := range observer.files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	facts := make([]selectedPhase2FileFact, 0, len(paths))
	for _, path := range paths {
		current, err := selectedPhase2Fact(path, 32<<20)
		if err != nil || current != observer.files[path] {
			return facts, errors.New("phase 2 source differs from its explicitly authorized file state")
		}
		facts = append(facts, current)
	}
	return facts, nil
}

func (observer *selectedPhase2Observer) snapshot(ctx context.Context, phase, suffix string) (map[string]any, error) {
	database, dbErr := selectedPhase2Database(ctx, observer.runtime.f)
	facts, fileErr := observer.filesVerified()
	value := map[string]any{"Marker": "goby-selected-phase2-stage-database-v1", "RunId": observer.fixture.RunID, "Phase": phase,
		"Complete": false, "Observed": dbErr == nil, "ExpectedFilesVerified": fileErr == nil, "Files": facts}
	if strings.HasPrefix(phase, "subtitle-") && observer.hlsObservation != nil {
		value["HLSObservation"] = observer.hlsObservation
	}
	if dbErr == nil {
		value["Database"] = database
	}
	if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-"+suffix+".json"), value); err != nil {
		return value, errors.New("phase 2 safe database observation could not be preserved")
	}
	if dbErr != nil {
		return value, errors.New("phase 2 safe database observation failed")
	}
	if fileErr != nil && suffix != "before-observation" {
		return value, fileErr
	}
	return value, nil
}

func (observer *selectedPhase2Observer) operation(ctx context.Context, id, expectedID, item, kind, state string) (library.MediaOperation, error) {
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) || expectedID != "" && id != expectedID {
		return library.MediaOperation{}, errors.New("phase 2 stage does not bind its owned operation")
	}
	op, err := observer.runtime.f.app.library.GetMediaOperation(ctx, observer.actor, id)
	wantError := ""
	if state == "cancelled" {
		wantError = "cancelled"
	}
	if err != nil || op.ItemID != item || op.Kind != kind || op.State != state || op.WorkerToken != "" || op.ErrorCode != wantError || op.Revision < 1 {
		return op, errors.New("phase 2 operation did not reach its exact expected durable state")
	}
	if err := observer.idle(ctx); err != nil {
		return op, err
	}
	return op, nil
}

func (observer *selectedPhase2Observer) removalApplied(ctx context.Context, op library.MediaOperation) error {
	var journal struct {
		StageName, SourceSHA256 string
		Candidate               struct {
			SHA256 string
			Size   int64
		}
		Published *struct {
			SHA256 string
			Size   int64
		}
	}
	var summary struct {
		BackupRetained       bool
		RemovedStreamIndex   int
		PreservedStreamCount int
	}
	if op.PublicationPhase != "done" || !op.Applied || json.Unmarshal(op.Journal, &journal) != nil || json.Unmarshal(op.ResultSummary, &summary) != nil ||
		!summary.BackupRetained || summary.RemovedStreamIndex != observer.fixture.RemoveStreamIndex || journal.StageName != ".goby-edit-"+op.ID ||
		journal.SourceSHA256 != observer.originalRemove.SHA256 || journal.Published == nil || journal.Published.SHA256 != journal.Candidate.SHA256 {
		return errors.New("phase 2 removal lacks the committed candidate and retained original witness")
	}
	backup := filepath.Join(filepath.Dir(observer.removePath), journal.StageName, "payload")
	original, err := selectedPhase2Fact(backup, 32<<20)
	if err != nil || original.SHA256 != observer.originalRemove.SHA256 || original.Bytes != observer.originalRemove.Bytes || original.Device != observer.originalRemove.Device || original.Inode != observer.originalRemove.Inode {
		return errors.New("phase 2 retained backup differs from the original media bytes and file identity")
	}
	current, err := selectedPhase2Fact(observer.removePath, 32<<20)
	if err != nil || current.SHA256 != journal.Candidate.SHA256 || current.SHA256 == observer.originalRemove.SHA256 {
		return errors.New("phase 2 source was not replaced by the confirmed candidate")
	}
	info, err := (media.Prober{FFprobePath: observer.execution.FFprobePath, FFmpegPath: observer.execution.FFmpegPath, Timeout: 20 * time.Second}).Probe(ctx, observer.removePath)
	if err != nil || len(info.Streams) != len(observer.removeMedia.Streams)-1 || len(info.Streams) != summary.PreservedStreamCount || !reflect.DeepEqual(info.Chapters, observer.removeMedia.Chapters) {
		return errors.New("phase 2 actual remux did not preserve chapters and the nonselected streams")
	}
	video, audio, kept, removed := 0, 0, 0, 0
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && stream.Codec == "h264" {
			video++
		}
		if stream.CodecType == "audio" && stream.Codec == "aac" {
			audio++
		}
		if stream.CodecType == "subtitle" && stream.Title == observer.fixture.KeepSubtitleTitle && stream.Language == "fra" && stream.Index == observer.fixture.KeepStreamIndex {
			kept++
		}
		if stream.CodecType == "subtitle" && stream.Title == observer.fixture.RemoveSubtitleTitle {
			removed++
		}
	}
	if video != 1 || audio != 1 || kept != 1 || removed != 0 {
		return errors.New("phase 2 removed or retained the wrong actual media stream")
	}
	observer.files[observer.removePath], observer.files[backup] = current, original
	return observer.decodePublishedRemoval(ctx, current)
}

func (observer *selectedPhase2Observer) ocrReady(ctx context.Context, op library.MediaOperation) error {
	if op.PublicationPhase != "none" || op.Applied || op.StreamIndex != observer.fixture.OCRStreamIndex ||
		!reflect.DeepEqual(op.Parameters.ModelIDs, observer.fixture.OCRModelIDs) || op.Parameters.OutputFormat != "srt" || op.Parameters.Language != observer.fixture.OCRLanguage || op.Parameters.Title != observer.fixture.OCRTitle ||
		op.Parameters.IsDefault || op.Parameters.IsForced || op.Parameters.IsHearingImpaired {
		return errors.New("phase 2 OCR did not use the selected source, models, and output settings")
	}
	page, err := observer.runtime.f.app.library.GetMediaOperationReview(ctx, observer.actor, op.ID, library.MediaOperationPageOptions{Limit: 200})
	if err != nil || int(page.TotalRecordCount) != len(observer.fixture.OCRExpectedCues) || len(page.Items) != len(observer.fixture.OCRExpectedCues) {
		return errors.New("phase 2 OCR cue count differs from the independently authored display intervals")
	}
	for index := range page.Items {
		cue := &page.Items[index]
		cue.ImagePNG, err = observer.runtime.f.app.library.GetMediaOperationCueImage(ctx, observer.actor, op.ID, cue.Ordinal)
		if err != nil {
			return errors.New("phase 2 original OCR bitmap could not be read under administrator authority")
		}
		expected := observer.fixture.OCRExpectedCues[index]
		digest := sha256.Sum256(cue.ImagePNG)
		rgba, width, height, decodeErr := selectedPhase2RGBAHash(cue.ImagePNG)
		if cue.Ordinal != index || cue.OriginalStartTicks != expected.StartTicks || cue.OriginalEndTicks != expected.EndTicks ||
			cue.StartTicks != expected.StartTicks || cue.EndTicks != expected.EndTicks || cue.OriginalText != cue.Text || cue.IsForced != expected.Forced ||
			cue.Confidence == nil || math.IsNaN(*cue.Confidence) || *cue.Confidence <= 0 || *cue.Confidence > 100 ||
			strings.Join(strings.Fields(cue.OriginalText), "") != strings.Join(strings.Fields(expected.Text), "") ||
			decodeErr != nil || width != expected.Width || height != expected.Height || rgba != expected.RGBAHash || hex.EncodeToString(digest[:]) != cue.ImageSHA256 {
			return errors.New("phase 2 OCR did not preserve real decoded pixels, authored timing, or recognition confidence")
		}
	}
	if strings.Join(strings.Fields(page.Items[0].OriginalText), " ") != observer.fixture.ExpectedOCRPhrase {
		return errors.New("phase 2 OCR did not recognize the authored English phrase")
	}
	var summary struct {
		EngineSHA256 string
		Models       []struct {
			ID     string `json:"Id"`
			SHA256 string
		}
		CueCount int
	}
	if json.Unmarshal(op.ResultSummary, &summary) != nil || summary.EngineSHA256 != observer.execution.OCR.ToolSHA256 || summary.CueCount != len(page.Items) || len(summary.Models) != 2 {
		return errors.New("phase 2 OCR result omitted the actual engine and model identity")
	}
	for _, used := range summary.Models {
		matched := false
		for _, model := range observer.execution.OCR.Models {
			matched = matched || model.ID == used.ID && model.SHA256 == used.SHA256
		}
		if !matched {
			return errors.New("phase 2 OCR used an undeclared trained model")
		}
	}
	observer.originalCues, observer.reviewHash, observer.reviewRevision = page.Items, op.ResultHash, op.Revision
	return nil
}

func (observer *selectedPhase2Observer) ocrReviewed(ctx context.Context, op library.MediaOperation, request selectedPhase2Request) error {
	page, err := observer.runtime.f.app.library.GetMediaOperationReview(ctx, observer.actor, op.ID, library.MediaOperationPageOptions{Limit: 200})
	if err != nil || len(page.Items) != len(observer.originalCues) || len(request.CueEdits) != 2 || request.Revision != strconv.FormatInt(op.Revision, 10) ||
		request.ResultHash != op.ResultHash || op.Revision <= observer.reviewRevision || op.ResultHash == observer.reviewHash {
		return errors.New("phase 2 reviewed OCR does not bind a new persisted CAS result")
	}
	first := observer.fixture.ReviewEdits[0]
	second := observer.originalCues[1]
	wantEdits := []library.MediaOperationCueEdit{first, {Ordinal: 1, StartTicks: second.StartTicks, EndTicks: second.EndTicks, Text: second.Text, Included: false}}
	if !reflect.DeepEqual(request.CueEdits, wantEdits) {
		return errors.New("phase 2 browser did not submit the exact reviewed cue edits")
	}
	for index := range page.Items {
		cue := &page.Items[index]
		cue.ImagePNG, err = observer.runtime.f.app.library.GetMediaOperationCueImage(ctx, observer.actor, op.ID, cue.Ordinal)
		if err != nil {
			return errors.New("phase 2 reviewed OCR bitmap could not be read under administrator authority")
		}
		before := observer.originalCues[index]
		if cue.OriginalStartTicks != before.OriginalStartTicks || cue.OriginalEndTicks != before.OriginalEndTicks || cue.OriginalText != before.OriginalText ||
			cue.ImageSHA256 != before.ImageSHA256 || !bytes.Equal(cue.ImagePNG, before.ImagePNG) || !reflect.DeepEqual(cue.Confidence, before.Confidence) {
			return errors.New("phase 2 review overwrote original OCR evidence")
		}
		if index == 0 {
			before.StartTicks, before.EndTicks, before.Text, before.Included = first.StartTicks, first.EndTicks, first.Text, true
		}
		if index == 1 {
			before.Included = false
		}
		if cue.StartTicks != before.StartTicks || cue.EndTicks != before.EndTicks || cue.Text != before.Text || cue.Included != before.Included {
			return errors.New("phase 2 reviewed cue text, timing, or inclusion differs")
		}
	}
	observer.reviewedCues, observer.reviewHash, observer.reviewRevision = page.Items, op.ResultHash, op.Revision
	return nil
}

func (observer *selectedPhase2Observer) viewerHTTP(ctx context.Context, action func(string, *http.Client) error) error {
	f := observer.runtime.f
	credentials, err := f.users.Authenticate(ctx, observer.fixture.ViewerName, observer.fixture.ViewerPassword,
		identity.Client{Name: "Phase 2 independent HTTP observer", DeviceID: "phase2-http-observer", Device: "Linux", Version: "1"}, "emby")
	if err != nil {
		return errors.New("phase 2 observer authentication failed")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.users.Revoke(cleanupCtx, credentials.Token)
	}()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	actionErr := action(credentials.Token, client)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, observer.fixture.BaseURL+"/emby/Sessions/Logout", nil)
	if err != nil {
		return errors.New("phase 2 observer logout request failed")
	}
	request.Header.Set("X-Emby-Token", credentials.Token)
	response, err := client.Do(request)
	if err != nil {
		return errors.Join(actionErr, errors.New("phase 2 observer HTTP logout failed"))
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.Join(actionErr, errors.New("phase 2 observer HTTP logout was rejected"))
	}
	return actionErr
}

func selectedPhase2HTTP(ctx context.Context, client *http.Client, base, path, token string) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("X-Emby-Token", token)
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, errors.New("phase 2 owned HTTP read failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || response.StatusCode != http.StatusOK || len(data) > 8<<20 {
		return nil, nil, errors.New("phase 2 owned HTTP response is invalid")
	}
	return data, response.Header.Clone(), nil
}

func (observer *selectedPhase2Observer) ocrApplied(ctx context.Context, op library.MediaOperation) error {
	if op.PublicationPhase != "done" || !op.Applied || op.ResultHash != observer.reviewHash {
		return errors.New("phase 2 OCR publication did not bind the saved review")
	}
	var index int
	var content []byte
	var digest, codec, language, title string
	var active bool
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT stream_index,content,content_sha256,codec,language,title,active FROM item_owned_subtitles WHERE operation_id=$1 AND item_id=$2`, op.ID, observer.fixture.OCRItemID).
		Scan(&index, &content, &digest, &codec, &language, &title, &active)
	actual := sha256.Sum256(content)
	if err != nil || !active || codec != "srt" || language != observer.fixture.OCRLanguage || title != observer.fixture.OCRTitle || hex.EncodeToString(actual[:]) != digest {
		return errors.New("phase 2 OCR publication did not own its exact reviewed subtitle bytes")
	}
	document, err := subtitle.Parse(content, subtitle.FormatSRT)
	if err != nil {
		return errors.New("phase 2 published SRT did not parse")
	}
	want := []library.MediaOperationCue{}
	for _, cue := range observer.reviewedCues {
		if cue.Included {
			want = append(want, cue)
		}
	}
	if len(document.Cues) != len(want) {
		return errors.New("phase 2 excluded OCR cue was published")
	}
	for ordinal, cue := range document.Cues {
		if cue.StartTicks != want[ordinal].StartTicks || cue.EndTicks != want[ordinal].EndTicks || cue.Text != want[ordinal].Text {
			return errors.New("phase 2 published OCR text or timing differs from the reviewed document")
		}
	}
	return observer.viewerHTTP(ctx, func(token string, client *http.Client) error {
		path := "/emby/Videos/" + observer.fixture.OCRItemID + "/" + media.SourceID(observer.fixture.OCRItemID) + "/Subtitles/" + strconv.Itoa(index) + "/0/Stream.srt?GobySubtitleTag=" + url.QueryEscape(digest)
		data, header, err := selectedPhase2HTTP(ctx, client, observer.fixture.BaseURL, path, token)
		if err != nil || !bytes.Equal(data, content) || !strings.HasPrefix(header.Get("Content-Type"), "text/plain") {
			return errors.New("phase 2 viewer HTTP did not deliver the exact applied SRT")
		}
		return featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "published-subtitle-http.json"), map[string]any{"RunId": observer.fixture.RunID, "Status": 200, "ContentSHA256": digest, "Bytes": len(data), "CueCount": len(want), "TokenInURL": false})
	})
}

func (observer *selectedPhase2Observer) artwork(ctx context.Context) error {
	return observer.viewerHTTP(ctx, func(token string, client *http.Client) error {
		for index, itemID := range []string{observer.fixture.AudioItemID, observer.fixture.SecondAudioItemID, observer.fixture.AudioLibraryID} {
			path := "/emby/Items/" + itemID + "/Images/Primary"
			data, header, err := selectedPhase2HTTP(ctx, client, observer.fixture.BaseURL, path, token)
			if err != nil {
				return err
			}
			decoded, format, decodeErr := image.Decode(bytes.NewReader(data))
			if decodeErr != nil || format != "png" || header.Get("ETag") == "" {
				return errors.New("phase 2 artwork HTTP did not deliver a real PNG representation")
			}
			listed, err := observer.runtime.f.app.library.ListImagesFor(ctx, library.Subject{UserID: observer.fixture.ViewerID}, itemID)
			if err != nil || len(listed) != 1 || listed[0].Tag != strings.Trim(header.Get("ETag"), `"`) {
				return errors.New("phase 2 artwork HTTP differs from its actual source manifest")
			}
			if index < 2 {
				if listed[0].Source != "embedded" || !bytes.Equal(data, observer.coverBytes[index]) {
					return errors.New("phase 2 audio artwork is not the embedded owned cover bytes")
				}
			} else {
				if listed[0].Source != "generated" || decoded.Bounds().Dx() != 512 || decoded.Bounds().Dy() != 512 {
					return errors.New("phase 2 library artwork is not an automatic collage")
				}
				colors := map[color.NRGBA]bool{}
				for _, at := range [][2]int{{128, 128}, {384, 128}, {128, 384}, {384, 384}} {
					colors[color.NRGBAModel.Convert(decoded.At(at[0], at[1])).(color.NRGBA)] = true
				}
				if !colors[color.NRGBA{R: 224, G: 32, B: 48, A: 255}] || !colors[color.NRGBA{R: 32, G: 80, B: 224, A: 255}] || len(colors) != 2 {
					return errors.New("phase 2 collage did not contain the two distinct owned embedded covers")
				}
			}
			digest := sha256.Sum256(data)
			observer.imageObservations = append(observer.imageObservations, map[string]any{"Path": path, "Status": 200, "Source": listed[0].Source,
				"Width": decoded.Bounds().Dx(), "Height": decoded.Bounds().Dy(), "ETag": header.Get("ETag"), "ContentSHA256": hex.EncodeToString(digest[:]), "SHA256": hex.EncodeToString(digest[:]), "Complete": true, "Decoded": true, "Observation": "independent authorized HTTP"})
		}
		return nil
	})
}

func (observer *selectedPhase2Observer) stage(ctx context.Context, request selectedPhase2Request) error {
	f, fixture := observer.runtime.f, observer.fixture
	switch request.Phase {
	case "authentication":
		var operations int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM media_operations").Scan(&operations) != nil || operations != 0 {
			return errors.New("phase 2 did not start from an empty operation history")
		}
	case "removal-ready":
		op, err := observer.operation(ctx, request.OperationID, "", fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "ready")
		if err != nil {
			return err
		}
		if op.StreamIndex != fixture.RemoveStreamIndex || op.PublicationPhase != "none" || op.Applied || len(op.ResultHash) != 64 {
			return errors.New("phase 2 removal preparation selected the wrong original stream")
		}
		observer.removal, observer.removeReady = op.ID, op
	case "removal-applied":
		op, err := observer.operation(ctx, request.OperationID, observer.removal, fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "completed")
		if err != nil {
			return err
		}
		if err := observer.removalApplied(ctx, op); err != nil {
			return err
		}
	case "ocr-ready":
		op, err := observer.operation(ctx, request.OperationID, "", fixture.OCRItemID, library.MediaOperationOCR, "ready")
		if err != nil {
			return err
		}
		observer.ocr = op.ID
		if err := observer.ocrReady(ctx, op); err != nil {
			return err
		}
	case "ocr-reviewed":
		op, err := observer.operation(ctx, request.OperationID, observer.ocr, fixture.OCRItemID, library.MediaOperationOCR, "ready")
		if err != nil {
			return err
		}
		if err := observer.ocrReviewed(ctx, op, request); err != nil {
			return err
		}
	case "ocr-applied":
		op, err := observer.operation(ctx, request.OperationID, observer.ocr, fixture.OCRItemID, library.MediaOperationOCR, "completed")
		if err != nil {
			return err
		}
		if err := observer.ocrApplied(ctx, op); err != nil {
			return err
		}
	case "subtitle-selected", "subtitle-off", "subtitle-reselected":
		if err := observer.hlsLive(ctx, request); err != nil {
			return err
		}
	case "subtitle-stopped":
		if err := observer.hlsStopped(ctx, request); err != nil {
			return err
		}
	case "cancel-ready":
		op, err := observer.operation(ctx, request.OperationID, "", fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "ready")
		if err != nil {
			return err
		}
		if op.ID == observer.removal || op.StreamIndex != fixture.KeepStreamIndex || op.PublicationPhase != "none" || op.Applied {
			return errors.New("phase 2 cancellation preparation did not target the retained stream")
		}
		observer.cancelled = op.ID
	case "cancelled":
		op, err := observer.operation(ctx, request.OperationID, observer.cancelled, fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "cancelled")
		if err != nil {
			return err
		}
		if op.Applied || op.PublicationPhase != "none" {
			return errors.New("phase 2 cancelled operation published media")
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(observer.removePath), ".goby-edit-"+op.ID, "payload")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("phase 2 cancellation retained its unpublished candidate bytes")
		}
	case "artwork":
		if err := observer.artwork(ctx); err != nil {
			return err
		}
	case "restart":
		if err := observer.idle(ctx); err != nil {
			return err
		}
		if err := observer.runtime.restart(ctx); err != nil {
			return errors.New("phase 2 complete application restart failed")
		}
	case "persisted":
		expected := []string{observer.removal, observer.ocr, observer.cancelled}
		actual := append([]string(nil), request.HistoryIDs...)
		slices.Sort(expected)
		slices.Sort(actual)
		if !reflect.DeepEqual(expected, actual) {
			return errors.New("phase 2 browser history does not bind all three real operations")
		}
	case "cleanup":
		if err := observer.idle(ctx); err != nil {
			return err
		}
		// The coordinator owns its worker map. Only inspect it after Close has
		// joined the real executor loop, processes, and pending storage work.
		if err := f.app.mediaOperations.Close(ctx); err != nil || len(f.app.mediaOperations.workers) != 0 {
			return errors.New("phase 2 actual media operation workers did not close")
		}
		if err := f.users.Revoke(ctx, observer.actorToken); err != nil {
			return errors.New("phase 2 fixture observer session could not be retired")
		}
		var sessions, playback, encodings, scans, tasks, unsafePlays, activeJobs int
		if f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM play_sessions),
			(SELECT count(*) FROM encoding_jobs),(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
			(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
			(SELECT count(*) FROM play_sessions p JOIN sessions a ON a.id=p.auth_session_id WHERE p.id<>$1 OR p.started_at IS NOT NULL OR p.counted OR p.state NOT IN ('Prepared','Expired') OR a.revoked_at IS NULL),
			(SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running'))`, observer.hlsPlayID).
			Scan(&sessions, &playback, &encodings, &scans, &tasks, &unsafePlays, &activeJobs) != nil || sessions != 0 || playback != 1 || encodings != len(observer.hlsProducerIDs) || scans != 0 || tasks != 0 || unsafePlays != 0 || activeJobs != 0 {
			return errors.New("phase 2 left authentication or runtime work after cleanup")
		}
		for _, count := range selectedPhase1RuntimeFacts(f) {
			if count != 0 {
				return errors.New("phase 2 left actual playback resources after cleanup")
			}
		}
	}
	if _, err := observer.filesVerified(); err != nil {
		return err
	}
	preserved, err := selectedPhase2Preservation(ctx, f, fixture.ViewerID, []string{fixture.RemoveItemID, fixture.OCRItemID})
	if err != nil || !bytes.Equal(preserved, observer.preservation) {
		return errors.New("phase 2 changed catalog identity, metadata, or complete user data")
	}
	if request.Phase == "artwork" {
		observer.durable, err = selectedPhase2Database(ctx, f)
		if err != nil {
			return err
		}
	}
	if request.Phase == "restart" || request.Phase == "persisted" {
		current, err := selectedPhase2Database(ctx, f)
		if err != nil || !bytes.Equal(current, observer.durable) {
			return errors.New("phase 2 restart changed completed operation history or replayed publication")
		}
	}
	return nil
}

func (observer *selectedPhase2Observer) run(ctx context.Context) error {
	for _, phase := range selectedPhase2Phases {
		if err := phase3BrowserWaitRequest(ctx, phase3BrowserContext{RunID: observer.fixture.RunID, ArtifactsDir: observer.fixture.ArtifactsDir}, phase); err != nil {
			return errors.New("phase 2 browser stage request failed")
		}
		var request selectedPhase2Request
		if featureWavePrivateJSON(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-request.json"), 64<<10, &request) != nil || request.RunID != observer.fixture.RunID || request.Phase != phase {
			return errors.New("phase 2 request does not bind the expected stage")
		}
		_, stageErr := observer.snapshot(ctx, phase, "before-observation")
		if stageErr == nil {
			stageErr = observer.stage(ctx, request)
		}
		value, snapshotErr := observer.snapshot(ctx, phase, "after-observation")
		if stageErr == nil {
			stageErr = snapshotErr
		}
		if stageErr != nil {
			value["Failed"], value["ErrorCode"], value["FailureDetail"] = true, "database_stage_verification_failed", stageErr.Error()
			_ = featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json"), value)
			return stageErr
		}
		value["Complete"], value["ExpectedFilesVerified"] = true, true
		if phase == "artwork" {
			value["ImageObservations"] = observer.imageObservations
		}
		if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json"), value); err != nil {
			return errors.New("phase 2 stage acknowledgement could not be preserved")
		}
		observer.completed++
	}
	return nil
}

func TestSelectedCompatibilityPhase2BrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("phase 2 browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	execution, inventory := selectedPhase2ReadExecution(t)
	hlsBundle, bundleErr := os.ReadFile(execution.HlsBundlePath)
	bundleDigest := sha256.Sum256(hlsBundle)
	if bundleErr != nil {
		selectedPhase2Fatal(t, "read pinned phase 2 HLS bundle", bundleErr)
	}
	if len(hlsBundle) > 8<<20 || hex.EncodeToString(bundleDigest[:]) != phase2BrowserBundleSHA256 {
		t.Fatal("phase 2 HLS bundle changed after pinned inventory admission")
	}
	runID := os.Getenv("GOBY_SELECTED_PHASE2_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("an explicit phase 2 browser run identity is required")
	}
	if info, err := os.Stat(artifacts); err != nil {
		selectedPhase2Fatal(t, "read phase 2 artifact parent identity", err)
	} else if info.Mode().Perm() != 0o700 {
		t.Fatalf("phase 2 artifact parent must be private: mode=%04o", info.Mode().Perm())
	}
	cwd, err := os.Getwd()
	if err != nil {
		selectedPhase2Fatal(t, "read phase 2 source directory", err)
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate phase 2 source root")
	}
	for _, input := range []struct{ role, path string }{{"node", node}, {"playwright", playwright},
		{"bitmap-author", filepath.Join(sourceRoot, "scripts", "test-env", "bitmap-subtitle-fixtures.py")},
		{"browser-driver", filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase2-browser.mjs")}} {
		fact, err := selectedPhase2Fact(input.path, 256<<20)
		if err != nil {
			// selectedPhase2Fact returns only fixed path-free diagnostic text.
			t.Fatalf("capture phase 2 %s identity: %v", input.role, err)
		}
		inventory = append(inventory, fact)
	}
	output, err := os.MkdirTemp(artifacts, "selected-phase2-browser-")
	if err != nil {
		selectedPhase2Fatal(t, "create phase 2 private artifact directory", err)
	}
	if err := os.Chmod(output, 0o700); err != nil {
		selectedPhase2Fatal(t, "protect phase 2 private artifact directory", err)
	}
	driver := map[string]any{"Marker": "goby-selected-phase2-browser-driver-v1", "RunId": runID, "Complete": false, "ArtifactDirectory": output,
		"CredentialsWrittenToSummary": false, "RealProber": true, "OCRRecognitionExercised": false, "OriginalClientUsed": false}
	var schema, mediaRoot string
	t.Cleanup(func() {
		if schema != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe phase 2 schema cleanup")
			} else {
				defer pool.Close()
				var remains bool
				if pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains) != nil || remains {
					t.Error("phase 2 owned schema was not removed")
				} else {
					driver["OwnedSchemaRemoved"] = true
				}
			}
		}
		if mediaRoot != "" {
			if _, err := os.Lstat(mediaRoot); !errors.Is(err, os.ErrNotExist) {
				t.Error("phase 2 owned media root was not removed")
			} else {
				driver["OwnedMediaRootRemoved"] = true
			}
		}
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve phase 2 driver result")
		}
	})
	if err := refreshBrowserWriteJSON(filepath.Join(output, "execution-inventory.json"), inventory); err != nil {
		selectedPhase2Fatal(t, "preserve pinned phase 2 execution identities", err)
	}
	f := newServerFixtureWithTimeout(t, 12*time.Minute)
	if err := f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		selectedPhase2Fatal(t, "identify phase 2 owned schema", err)
	}
	mediaRoot = t.TempDir()
	inputDir := filepath.Join(mediaRoot, "authoring")
	movieDir := filepath.Join(mediaRoot, "movies")
	musicDir := filepath.Join(mediaRoot, "music")
	for _, dir := range []string{inputDir, movieDir, filepath.Join(musicDir, "First Album"), filepath.Join(musicDir, "Second Album")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			selectedPhase2Fatal(t, "create phase 2 owned source directories", err)
		}
	}
	bitmapDir := filepath.Join(inputDir, "bitmap")
	generator := filepath.Join(sourceRoot, "scripts", "test-env", "bitmap-subtitle-fixtures.py")
	hlsHTTPMediaCommand(t, execution.PythonPath, "-I", generator, "--font-file", execution.FontPath, "--font-sha256", execution.FontSHA256, "--output-dir", bitmapDir)
	manifestPath := filepath.Join(bitmapDir, "manifest.json")
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		selectedPhase2Fatal(t, "read bounded phase 2 authored bitmap manifest", err)
	}
	if len(manifestRaw) > 1<<20 {
		t.Fatalf("phase 2 authored bitmap manifest exceeds size limit: bytes=%d", len(manifestRaw))
	}
	var manifest selectedPhase2BitmapManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		selectedPhase2Fatal(t, "decode phase 2 bitmap authoring manifest", err)
	}
	if manifest.Format != "goby-bitmap-subtitle-fixtures-v2" || manifest.DurationTicks != 97_280_000 {
		t.Fatal("phase 2 bitmap authoring manifest identity differs")
	}
	var expected []selectedPhase2ExpectedCue
	bitmapPath := ""
	for _, item := range manifest.Cases {
		if item.Name == "overlap" {
			expected = item.Intervals
			bitmapPath = filepath.Join(bitmapDir, item.File)
			if item.Origin != 0 || !reflect.DeepEqual(item.Models, []string{"eng", "chi_sim"}) {
				t.Fatal("phase 2 authored overlap origin or models differ")
			}
		}
	}
	if len(expected) != 4 || bitmapPath != filepath.Join(bitmapDir, "overlap-pgs.mks") {
		t.Fatal("phase 2 authored PGS overlap display intervals are missing")
	}
	starts := []int64{10_240_000, 25_600_000, 40_960_000, 66_560_000}
	ends := []int64{25_600_000, 40_960_000, 56_320_000, 87_040_000}
	texts := []string{"Hello world", "Hello world\n\u4e2d\u6587\u6d4b\u8bd5", "\u4e2d\u6587\u6d4b\u8bd5", "Hello world"}
	for index, cue := range expected {
		if cue.StartTicks != starts[index] || cue.EndTicks != ends[index] || cue.Text != texts[index] || cue.Forced || cue.Width < 1 || cue.Height < 1 || len(cue.RGBAHash) != 64 {
			t.Fatal("phase 2 bitmap generator changed the fixed independent overlap ground truth")
		}
	}
	for name, recorded := range manifest.Files {
		if filepath.Base(name) != name {
			t.Fatal("phase 2 authored fixture file escaped its directory")
		}
		fact, err := selectedPhase2Fact(filepath.Join(bitmapDir, name), 32<<20)
		if err != nil {
			t.Fatalf("phase 2 authored bitmap %s: %v", name, err)
		}
		if fact.Bytes != recorded.Bytes || fact.SHA256 != recorded.SHA256 {
			t.Fatalf("phase 2 authored bitmap %s: manifest size or SHA-256 mismatch", name)
		}
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "bitmap-authoring-manifest.json"), json.RawMessage(manifestRaw)); err != nil {
		selectedPhase2Fatal(t, "preserve independent phase 2 authored bitmap evidence", err)
	}
	removeText := filepath.Join(inputDir, "remove.srt")
	keepText := filepath.Join(inputDir, "keep.srt")
	chapterPath := filepath.Join(inputDir, "chapters.ffmetadata")
	for path, contents := range map[string]string{removeText: "1\n00:00:01,000 --> 00:00:03,000\nRemove this English track\n", keepText: "1\n00:00:02,000 --> 00:00:04,000\nConserver cette piste\n",
		chapterPath: ";FFMETADATA1\ntitle=Selected Phase Two Source\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=3000\ntitle=Opening\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=3000\nEND=12000\ntitle=Main\n"} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			selectedPhase2Fatal(t, "write owned phase 2 subtitle and chapter sources", err)
		}
	}
	removePath := filepath.Join(movieDir, "Phase Two Remove.mkv")
	ocrPath := filepath.Join(movieDir, "Phase Two OCR.mkv")
	selectedPhase2GenerateVideo(t, execution, removePath, "", removeText, keepText, chapterPath)
	selectedPhase2GenerateVideo(t, execution, ocrPath, bitmapPath, "", "", "")
	coverPaths := []string{filepath.Join(inputDir, "cover-red.png"), filepath.Join(inputDir, "cover-blue.png")}
	coverBytes := [][]byte{selectedPhase2PNG(t, coverPaths[0], color.NRGBA{R: 224, G: 32, B: 48, A: 255}), selectedPhase2PNG(t, coverPaths[1], color.NRGBA{R: 32, G: 80, B: 224, A: 255})}
	audioPaths := []string{filepath.Join(musicDir, "First Album", "First Track.flac"), filepath.Join(musicDir, "Second Album", "Second Track.flac")}
	for index, path := range audioPaths {
		hlsHTTPMediaCommand(t, execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=800:sample_rate=48000", "-i", coverPaths[index],
			"-map", "0:a:0", "-map", "1:v:0", "-af", "atrim=end_sample=9600,asetpts=PTS-STARTPTS", "-c:a", "flac", "-c:v", "copy", "-disposition:v:0", "attached_pic",
			"-metadata:s:v:0", "comment=Cover (front)", "-metadata:s:v:0", "title=Embedded source cover", path)
		if err := os.Chmod(path, 0o600); err != nil {
			selectedPhase2Fatal(t, "protect phase 2 real audio source", err)
		}
	}
	files := map[string]selectedPhase2FileFact{}
	for _, path := range append([]string{removePath, ocrPath, removeText, keepText, chapterPath, bitmapPath, manifestPath, coverPaths[0], coverPaths[1]}, audioPaths...) {
		fact, err := selectedPhase2Fact(path, 32<<20)
		if err != nil {
			t.Fatalf("capture phase 2 owned source fingerprints: %v", err)
		}
		files[path] = fact
	}
	if err := f.app.Close(f.ctx); err != nil {
		selectedPhase2Fatal(t, "close initial phase 2 application", err)
	}
	scratch := t.TempDir()
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = execution.FFmpegPath, execution.FFprobePath, []string{mediaRoot}
	f.cfg.Transcoding = config.TranscodingConfig{Enabled: true, CacheDirectory: t.TempDir(), Threads: 1, MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
		MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20, MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}
	f.cfg.MediaOperations = config.MediaOperationsConfig{Enabled: true, MaxConcurrent: 1, MaxQueued: 4, MaxRuntimeSeconds: 90, MaxScratchBytes: 512 << 20, ScratchDirectory: scratch, WritableProfiles: []string{"matroska-v1"}, OCR: execution.OCR}
	assets, err := adminassets.Files()
	if err != nil {
		selectedPhase2Fatal(t, "open phase 2 embedded native administrator assets", err)
	}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "selected-phase2-browser-integration", WithDashboardAssets(assets))
	if err != nil {
		selectedPhase2Fatal(t, "construct phase 2 actual media operation application", err)
	}
	f.app, f.handler = app, app.Handler()
	runtime := &selectedPhase2Runtime{phase3BrowserRuntime: &phase3BrowserRuntime{f: f, assets: assets, addr: "127.0.0.1:0"}, bundle: hlsBundle}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe phase 2 residual sessions")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("retire phase 2 residual owned sessions")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
			if driver["Complete"] == true && result.RowsAffected() != 0 {
				t.Error("successful phase 2 browser required fallback session revocation")
			}
		}
		if runtime.close(ctx) != nil {
			t.Error("close phase 2 actual workers")
		} else {
			driver["ServerWorkersClosed"], driver["HTTPListenerClosed"] = true, true
		}
		if runtime.diagnostics != nil {
			runtime.diagnostics.mu.Lock()
			calls, failures, active := runtime.diagnostics.calls, runtime.diagnostics.failures, runtime.diagnostics.active
			runtime.diagnostics.mu.Unlock()
			driver["ExecutorDiagnostics"] = map[string]any{"Calls": calls, "WriteOrCloseFailures": failures, "ActiveCallsAfterRuntimeClose": active, "PrivateErrorMessagesExcluded": true}
			if failures != 0 || active != 0 {
				t.Error("phase 2 executor diagnostic persistence or descriptor closure failed")
			}
		}
	})
	if !app.mediaOperations.Available() || !app.mediaOperations.ocrReady {
		t.Fatal("phase 2 pinned real media and OCR execution inventory is unavailable")
	}
	adminPassword, viewerPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Selected phase 2 administrator", adminPassword)
	if err != nil {
		selectedPhase2Fatal(t, "bootstrap phase 2 native administrator", err)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Selected phase 2 viewer", viewerPassword, false)
	if err != nil {
		selectedPhase2Fatal(t, "create phase 2 independent HTTP viewer", err)
	}
	credentials, err := f.users.Authenticate(f.ctx, admin.Name, adminPassword, identity.Client{Name: "Phase 2 fixture observer", DeviceID: "phase2-fixture-observer", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "authenticate phase 2 fixture observer", err)
	}
	actor, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		selectedPhase2Fatal(t, "resolve phase 2 fixture observer", err)
	}
	movies, err := app.library.CreateLibrary(f.ctx, "Selected Phase Two Movies", "movies", []string{movieDir})
	if err != nil {
		selectedPhase2Fatal(t, "create phase 2 real movie library", err)
	}
	music, err := app.library.CreateLibrary(f.ctx, "Selected Phase Two Music", "music", []string{musicDir})
	if err != nil {
		selectedPhase2Fatal(t, "create phase 2 real music library", err)
	}
	policy, _ := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{movies.ID, music.ID}, "EnableMediaPlayback": true,
		"EnableVideoPlaybackTranscoding": true, "EnableAudioPlaybackTranscoding": true, "EnablePlaybackRemuxing": true})
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
		selectedPhase2Fatal(t, "set phase 2 owned viewer media execution policy", err)
	}
	selectedPhase1Scan(t, f, movies.ID, 2)
	selectedPhase1Scan(t, f, music.ID, 2)
	readItem := func(path string) library.Item {
		var id string
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE path=$1", path).Scan(&id); err != nil {
			selectedPhase2Fatal(t, "read phase 2 scanned item identity", err)
		}
		item, err := app.library.GetItem(f.ctx, viewer.ID, id)
		if err != nil {
			selectedPhase2Fatal(t, "read phase 2 scanned item", err)
		}
		if item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion {
			t.Fatal("phase 2 item lacks real current media probe facts")
		}
		return item
	}
	removeItem, ocrItem, firstAudio, secondAudio := readItem(removePath), readItem(ocrPath), readItem(audioPaths[0]), readItem(audioPaths[1])
	if !ocrItem.Media.FormatStartKnown || ocrItem.Media.FormatStartTicks != 0 || ocrItem.Media.DurationTicks < 110_000_000 || len(removeItem.Media.Chapters) != 2 {
		t.Fatal("phase 2 actual media does not preserve authored timing and chapters")
	}
	removeIndex, keepIndex, ocrIndex := -1, -1, -1
	for _, stream := range removeItem.Media.Streams {
		if stream.CodecType == "subtitle" && stream.Title == "Remove English" {
			removeIndex = stream.Index
		}
		if stream.CodecType == "subtitle" && stream.Title == "Keep French" {
			keepIndex = stream.Index
		}
	}
	for _, stream := range ocrItem.Media.Streams {
		if stream.CodecType == "subtitle" && stream.Codec == "hdmv_pgs_subtitle" {
			ocrIndex = stream.Index
		}
	}
	if removeIndex < 0 || keepIndex < 0 || ocrIndex < 0 {
		t.Fatal("phase 2 actual scans did not expose the authored subtitle streams")
	}
	if keepIndex > removeIndex {
		keepIndex--
	}
	for _, id := range []string{removeItem.ID, ocrItem.ID} {
		detail, err := app.library.GetItemMetadata(f.ctx, actor, id)
		if err != nil {
			selectedPhase2Fatal(t, "read phase 2 metadata baseline", err)
		}
		value, _ := json.Marshal("Phase 2 preserved manual overview")
		if _, err := app.library.UpdateItemMetadata(f.ctx, actor, id, library.MetadataEdit{Revision: detail.Revision, Overrides: map[string]json.RawMessage{"Overview": value}, LockedFields: []string{}}); err != nil {
			selectedPhase2Fatal(t, "seed phase 2 preserved manual metadata", err)
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at) VALUES($1,$2,50000000,3,true,false,'2026-01-02T03:04:05Z')`, viewer.ID, id); err != nil {
			selectedPhase2Fatal(t, "seed phase 2 persistent owned user data", err)
		}
	}
	preservation, err := selectedPhase2Preservation(f.ctx, f, viewer.ID, []string{removeItem.ID, ocrItem.ID})
	if err != nil {
		selectedPhase2Fatal(t, "capture phase 2 user data and metadata preservation baseline", err)
	}
	embedded, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		selectedPhase2Fatal(t, "inventory phase 2 native assets", err)
	}
	sourceAssets, err := refreshBrowserAssetInventory(os.DirFS(filepath.Join(sourceRoot, "web", "admin", "dist")))
	if err != nil {
		selectedPhase2Fatal(t, "inventory phase 2 frozen source assets", err)
	}
	if !reflect.DeepEqual(embedded, sourceAssets) {
		t.Fatal("phase 2 embedded administrator assets differ from source")
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embedded); err != nil {
		selectedPhase2Fatal(t, "preserve phase 2 embedded asset inventory", err)
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	runtime.diagnostics = &selectedPhase2ExecutorDiagnostics{directory: output, mediaRoot: mediaRoot, runID: runID,
		sources: map[string]string{removeItem.ID: removePath, ocrItem.ID: ocrPath},
		secrets: []string{adminPassword, viewerPassword, credentials.Token, os.Getenv("GOBY_TEST_DATABASE_URL")}}
	if connection, err := url.Parse(os.Getenv("GOBY_TEST_DATABASE_URL")); err == nil && connection.User != nil {
		if password, found := connection.User.Password(); found {
			runtime.diagnostics.secrets = append(runtime.diagnostics.secrets, password)
		}
	}
	if err := runtime.listen(); err != nil {
		selectedPhase2Fatal(t, "start phase 2 owned TCP4 listener", err)
	}
	fixture := selectedPhase2Context{Marker: "goby-selected-phase2-browser-fixture-v1", RunID: runID, BaseURL: "http://" + runtime.addr,
		AdminID: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword, ViewerID: viewer.ID, ViewerName: viewer.Name, ViewerPassword: viewerPassword,
		LibraryID: movies.ID, LibraryName: movies.Name, AudioLibraryID: music.ID, AudioLibraryName: music.Name, RemoveItemID: removeItem.ID, RemoveItemName: removeItem.Name,
		OCRItemID: ocrItem.ID, OCRItemName: ocrItem.Name, AudioItemID: firstAudio.ID, AudioItemName: firstAudio.Name, SecondAudioItemID: secondAudio.ID,
		RemoveStreamIndex: removeIndex, KeepStreamIndex: keepIndex, OCRStreamIndex: ocrIndex, RemoveSubtitleTitle: "Remove English", KeepSubtitleTitle: "Keep French",
		OCRModelIDs: []string{"eng", "chi_sim"}, OCRLanguage: "eng", OCRTitle: "Phase 2 reviewed subtitles", ExpectedOCRPhrase: "Hello world", OCRExpectedCues: expected,
		ReviewEdits:   []library.MediaOperationCueEdit{{Ordinal: 0, StartTicks: 12_500_000, EndTicks: 24_000_000, Text: "Phase 2 reviewed subtitle", Included: true}},
		HlsBundlePath: execution.HlsBundlePath, HlsBundleSHA256: execution.HlsBundleSHA256,
		ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	contextPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove phase 2 private browser credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if err := refreshBrowserWriteJSON(contextPath, fixture); err != nil {
		selectedPhase2Fatal(t, "write private phase 2 browser context", err)
	}
	observer := &selectedPhase2Observer{runtime: runtime, fixture: fixture, execution: execution, actor: actor, actorToken: credentials.Token, files: files,
		removePath: removePath, ocrPath: ocrPath, scratch: scratch, originalRemove: files[removePath], removeMedia: *removeItem.Media, coverBytes: coverBytes, preservation: preservation}
	if _, err := observer.snapshot(f.ctx, "seeded", "database"); err != nil {
		selectedPhase2Fatal(t, "preserve phase 2 real source seed evidence", err)
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if err := os.Mkdir(filepath.Join(output, name), 0o700); err != nil {
			selectedPhase2Fatal(t, "create phase 2 private browser runtime directories", err)
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1", "HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright, "GOBY_SELECTED_PHASE2_RUN_ID=" + runID, "GOBY_SELECTED_PHASE2_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Minute)
	defer cancel()
	observerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase2-browser.mjs")}, environment, sourceRoot, output)
	stop()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	driver["OCRRecognitionExercised"] = len(observer.originalCues) > 0
	if observerErr != nil && observer.completed < len(selectedPhase2Phases) {
		driver["FailedDatabaseStage"], driver["DatabaseObservationError"] = selectedPhase2Phases[observer.completed], observerErr.Error()
	}
	var result selectedPhase2Result
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(selectedPhase2Phases) {
		t.Fatalf("phase 2 browser or independent observer failed: command=%s observer=%s result=%s completed_stages=%d; inspect retained private artifacts",
			selectedPhase2SafeError(commandErr), selectedPhase2SafeError(observerErr), selectedPhase2SafeError(readErr), observer.completed)
	}
	checks := []string{"Authentication", "RemovalPrepared", "RemovalApplied", "OCRRecognized", "OCRReviewed", "OCRApplied", "SubtitleSelected", "SubtitleOff", "SubtitleReselected", "SubtitleStopped", "CancelPrepared", "Cancelled", "ArtworkObserved", "RestartPersisted", "HistoryPersisted", "Cleanup"}
	if result.Marker != "goby-selected-phase2-browser-result-v1" || result.RunID != runID || !result.Complete || len(result.Checks) != len(checks) || len(result.Stages) != len(selectedPhase2Phases) || result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("phase 2 result does not bind the complete real scenario")
	}
	for index, phase := range selectedPhase2Phases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" {
			t.Fatal("phase 2 browser stages differ from independently observed states")
		}
	}
	for _, name := range checks {
		if !result.Checks[name] {
			t.Fatal("phase 2 browser check did not complete")
		}
	}
	driver["Complete"], driver["ExpectedFilesVerified"], driver["OwnedSessionsRetired"] = true, true, true
	driver["BrowserChecks"], driver["ImageObservations"], driver["ServerRestarts"] = result.Checks, observer.imageObservations, 1
	t.Log("selected_phase2_browser_verified=true stages=16 real_media_operations=3 real_ocr=true actual_hls_subtitle_consumption=true server_restarts=1")
}
