package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/notifications"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/tasks"
	"github.com/moooyo/goby/internal/transcode"
)

type Server struct {
	cfg                  config.Config
	db                   *pgxpool.Pool
	identity             *identity.Store
	log                  *slog.Logger
	version              string
	serverID             string
	limiter              *loginLimiter
	library              *library.Store
	images               *imageCache
	streamSlots          chan struct{}
	originals            *originalStreamRuntime
	subtitleSlots        chan struct{}
	eventHub             *events.Hub
	sockets              *socketRuntime
	notifier             *userDataNotifier
	catalogNotifier      *libraryNotifier
	hls                  *hlsRuntime
	dynamicSources       *dynamicsource.Manager
	dynamicStreams       *dynamicStreamRuntime
	mediaPolicyOnce      sync.Once
	mediaPolicy          *mediaPolicyRuntime
	hardwareEncodingOnce sync.Once
	hardwareEncoding     *hardwareEncodingRuntime
	managedHardware      *managedHardwareInventory
	httpBinding          runtimeHTTPBinding
	taskStore            *tasks.Store
	taskManager          *tasks.Manager
	settings             *settings.Store
	diagnostics          *diagnostics.Store
	mediaDiagnostics     *mediaDiagnosticRuntime
	mediaOperations      *mediaOperationsRuntime
	mediaAnalysis        *mediaAnalysisRuntime
	notificationStore    *notifications.Store
	notificationRuntime  *notifications.Runtime
	notificationOptions  notifications.RuntimeOptions
	dashboardFiles       fs.FS
	recovery             adminRecoveryManager
	activityCancel       context.CancelFunc
	activityDone         chan struct{}
}

// Option attaches dependencies whose lifetime is owned by the process entry
// point. Direct repository fixtures need not open a diagnostic directory.
type Option func(*Server)

func WithDiagnostics(store *diagnostics.Store) Option {
	return func(server *Server) { server.diagnostics = store }
}

func New(ctx context.Context, cfg config.Config, db *pgxpool.Pool, users *identity.Store, logger *slog.Logger, version string, options ...Option) (*Server, error) {
	id, err := users.ServerID(ctx)
	if err != nil {
		return nil, err
	}
	// Optional restart analysis belongs to scanning, independently of whether
	// conversion is currently enabled. Unsupported analysis retains the normal
	// probe facts; playback requests only read cached evidence.
	catalog, err := library.New(db, media.Prober{FFprobePath: cfg.FFprobePath, FFmpegPath: cfg.FFmpegPath,
		AnalyzeVideoSeek: true, Timeout: 30 * time.Second}, cfg.MediaRoots)
	if err != nil {
		return nil, err
	}
	hub, err := events.New(events.Options{})
	if err != nil {
		_ = catalog.Close(ctx)
		return nil, err
	}
	app := &Server{cfg: cfg, db: db, identity: users, log: logger, version: version, serverID: id, limiter: newLoginLimiter(), library: catalog, images: newImageCache(), streamSlots: make(chan struct{}, 64), subtitleSlots: make(chan struct{}, 4), eventHub: hub, sockets: newSocketRuntime()}
	app.managedHardware = newManagedHardwareInventory(cfg.Transcoding)
	for _, option := range options {
		if option != nil {
			option(app)
		}
	}
	app.originals = newOriginalStreamRuntime()
	app.hls, err = newHLSRuntime(ctx, app)
	if err != nil {
		app.originals.stop()
		app.sockets.cancel()
		_ = hub.Close()
		_ = catalog.Close(ctx)
		return nil, err
	}
	app.notifier = newUserDataNotifier(catalog, hub)
	app.catalogNotifier = newLibraryNotifier(catalog, hub)
	if err := app.initializeSettings(ctx); err != nil {
		_ = app.Close(context.Background())
		return nil, err
	}
	app.mediaOperations, err = newMediaOperationsRuntime(ctx, app)
	if err != nil {
		_ = app.Close(context.Background())
		return nil, err
	}
	app.mediaAnalysis, err = newMediaAnalysisRuntime(ctx, app)
	if err != nil {
		_ = app.Close(context.Background())
		return nil, err
	}
	if err := app.initializeDynamicSources(ctx); err != nil {
		_ = app.Close(context.Background())
		return nil, err
	}
	if err := app.initializeTasks(ctx); err != nil {
		_ = app.Close(context.Background())
		return nil, err
	}
	app.mediaDiagnostics, err = newMediaDiagnosticRuntime(app)
	if err != nil {
		_ = app.Close(context.Background())
		return nil, err
	}
	app.startActivityRetention()
	app.initializeNotifications()
	return app, nil
}

func (s *Server) initializeSettings(ctx context.Context) error {
	hostName, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("resolve server host name: %w", err)
	}
	limits := s.cfg.Transcoding
	// Direct constructors historically use the zero value to disable the
	// conversion engine. Its display/planning defaults still need valid values.
	if limits == (config.TranscodingConfig{}) {
		limits.MaxBitrate = config.DefaultMaxBitrate
		limits.MaxWidth = config.DefaultMaxWidth
		limits.MaxHeight = config.DefaultMaxHeight
		limits.MaxAudioChannels = config.DefaultMaxAudioChannels
	}
	binding, err := config.HTTPBindingDefaults(s.cfg.ListenAddress)
	if err != nil {
		return err
	}
	if s.managedHardware == nil {
		s.managedHardware = newManagedHardwareInventory(s.cfg.Transcoding)
	}
	execution := limits.Execution
	if execution == (transcode.ExecutionOptions{}) {
		execution = transcode.DefaultExecutionOptions(limits.Threads)
	}
	runtimeDefaults := settings.RuntimeOptions{
		Network:  settings.NetworkValues{BindHost: binding.BindHost, HttpPort: binding.HttpPort},
		Hardware: s.managedHardware.defaultSelection(), Execution: execution,
		AuthorizedDeviceIDs:   s.managedHardware.authorizedDeviceIDs(),
		AvailableDeviceIDs:    s.managedHardware.availableDeviceIDs(),
		LegacyHardwareDefault: s.managedHardware.legacyDefault(),
	}
	store, err := settings.New(ctx, s.db, s.library, settings.Values{
		ServerName: s.cfg.ServerName, MaxBitrate: limits.MaxBitrate,
		MaxWidth: limits.MaxWidth, MaxHeight: limits.MaxHeight,
		MaxAudioChannels: limits.MaxAudioChannels,
	}, hostName, runtimeDefaults)
	if err != nil {
		return err
	}
	s.settings = store
	return nil
}

func (s *Server) initializeTasks(ctx context.Context) error {
	executors, err := s.managementTaskExecutors()
	if err != nil {
		return err
	}
	store, err := tasks.New(s.db, s.library, executors)
	if err != nil {
		return err
	}
	if err := store.Reconcile(ctx); err != nil {
		return err
	}
	if err := store.RecoverRuns(ctx); err != nil {
		return err
	}
	manager, err := tasks.NewManager(store, s.library, tasks.ManagerOptions{Logger: s.log,
		MaxConcurrent: func() int { return s.settings.Snapshot().Management.Tasks.MaxConcurrent }})
	if err != nil {
		return err
	}
	s.taskStore, s.taskManager = store, manager
	return nil
}

func (s *Server) Close(ctx context.Context) error {
	s.notificationRuntime.BeginClose()
	s.mediaAnalysis.BeginClose()
	if s.mediaOperations != nil {
		s.mediaOperations.BeginClose()
	}
	s.stopMediaPolicy()
	s.hardwareEncodingRuntime().cancel()
	s.mediaDiagnostics.BeginClose()
	s.catalogNotifier.Close()
	s.cancelActivityRetention()
	return s.closeSockets(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]string{"Status": "ok"}) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /admin/v1/bootstrap", s.bootstrapStatus)
	mux.HandleFunc("POST /admin/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("POST /admin/v1/session", s.adminLogin)
	mux.HandleFunc("GET /admin/v1/session", s.requireAdmin(s.adminSession))
	mux.HandleFunc("DELETE /admin/v1/session", s.requireAdmin(s.adminLogout))
	mux.HandleFunc("GET /admin/v1/overview", s.requireAdmin(s.overview))
	mux.HandleFunc("GET /admin/v1/capabilities", s.requireAdmin(s.capabilities))
	mux.HandleFunc("GET /admin/v1/users", s.requireAdmin(s.users))
	mux.HandleFunc("POST /admin/v1/users", s.requireAdmin(s.createUser))
	s.registerAdminUserRoutes(mux)
	s.registerLocalCredentialRoutes(mux)
	s.registerAdminSessionRoutes(mux)
	s.registerAdminDeviceRoutes(mux)
	s.registerAdminTaskRoutes(mux)
	s.registerAdminMediaDiagnosticRoutes(mux)
	s.registerAdminMediaOperationRoutes(mux)
	s.registerAdminMediaAnalysisRoutes(mux)
	s.registerAdminSettingsRoutes(mux)
	s.registerAdminBackupRoutes(mux)
	s.registerConfigurationRoutes(mux)
	s.registerUserSettingsRoutes(mux)
	s.registerBrandingRoutes(mux)
	s.registerObservabilityRoutes(mux)
	s.registerScheduledTaskRoutes(mux)
	s.registerDeviceRoutes(mux)
	s.registerApplicationKeyRoutes(mux)
	s.registerAdminMetadataRoutes(mux)
	s.registerIntroMarkerRoutes(mux)
	s.registerAnalysisPreviewRoutes(mux, s.mediaAnalysis)
	s.registerLibraryRoutes(mux)
	s.registerSelectedManagementRoutes(mux)
	s.registerEntityRoutes(mux)
	s.registerMusicEntityRoutes(mux)
	s.registerMusicDiscoveryRoutes(mux)
	s.registerSearchHintRoutes(mux)
	s.registerSuggestionsRoutes(mux)
	s.registerExpectedEpisodeRoutes(mux)
	s.registerNavigationRoutes(mux)
	s.registerImageRoutes(mux)
	s.registerArtworkManagementRoutes(mux)
	s.registerAvatarRoutes(mux)
	s.registerStreamRoutes(mux)
	s.registerPlaybackRoutes(mux)
	s.registerClientSessionRoutes(mux)
	s.registerNotificationRoutes(mux)
	s.registerSubtitleRoutes(mux)
	s.registerRemoteCommandRoutes(mux)
	s.registerHLSRoutes(mux)
	s.registerHLSSubtitleRoutes(mux)
	s.registerLiveStreamRoutes(mux)
	s.registerCollectionRoutes(mux)
	s.registerProviderRoutes(mux)
	s.registerUserManagementRoutes(mux)
	s.registerMediaDownloadRoutes(mux)
	s.registerMediaManagementRoutes(mux)
	s.registerFeatureRoutes(mux)
	s.registerFeatureLicensingRoutes(mux)
	mux.HandleFunc("/admin/v1/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, r, 404, "not_found", "The requested administrator API is not available.")
	})
	mux.HandleFunc("GET /emby/System/Info/Public", s.publicSystemInfo)
	mux.HandleFunc("GET /emby/System/Info", s.requireEmby(s.systemInfo))
	mux.HandleFunc("GET /emby/System/Endpoint", s.requireEmby(s.systemEndpoint))
	mux.HandleFunc("GET /emby/System/Ping", s.ping)
	mux.HandleFunc("POST /emby/System/Ping", s.ping)
	mux.HandleFunc("GET /emby/Users/Public", s.publicUsers)
	mux.HandleFunc("POST /emby/Users/AuthenticateByName", s.embyLogin)
	mux.HandleFunc("POST /emby/Users/{Id}/Authenticate", s.embyLoginByID)
	mux.HandleFunc("GET /emby/Users/{Id}", s.requireEmby(s.embyUser))
	mux.HandleFunc("GET /emby/Users", s.requireEmby(s.embyUsersBare))
	mux.HandleFunc("GET /emby/Users/Query", s.requireEmby(s.embyUsers))
	mux.HandleFunc("GET /emby/LiveTv/Programs", s.requireEmby(s.embyLiveTVPrograms))
	mux.HandleFunc("POST /emby/Sessions/Logout", s.requireEmby(s.embyLogout))
	mux.HandleFunc("/emby/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, r, 404, "not_implemented", "This operation has not been implemented.")
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/admin/", s.dashboard)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, r, 404, "not_found", "The requested resource was not found.")
	})
	return s.withSettingsSnapshot(withBoundedRequestBodies(s.middleware(mux)))
}

type contextKey int

const requestIDKey contextKey = 1
const principalKey contextKey = 2

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = compatibilityNamespace(r)
		id := make([]byte, 16)
		_, _ = rand.Read(id)
		requestID := hex.EncodeToString(id)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		r = r.WithContext(ctx)
		response, completeRequest := s.beginRequestLogging(w, r)
		w = response
		defer completeRequest()
		w.Header().Set("X-Request-Id", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if s.embyCORS(w, r) {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				response.markAborted()
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				s.log.Error("request panic", "request_id", requestID)
				apiError(w, r, 500, "internal_error", "The request could not be completed.")
			}
		}()
		if isSocketRequest(r) {
			s.requireEmby(s.clientSocket)(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	// Readiness includes an enabled conversion engine. Temporary capacity
	// pressure does not make the engine unhealthy; sticky cache failure and
	// shutdown do. A deliberately disabled engine does not block readiness.
	if status := s.transcodingStatus(); status.Configured && !status.Available {
		apiError(w, r, http.StatusServiceUnavailable, "transcoding_"+status.Reason, "The conversion engine is unavailable. Check its reported status and restart after repairing the cause.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.db.Ping(ctx) != nil {
		apiError(w, r, 503, "not_ready", "The service is not ready.")
		return
	}
	if s.library.CheckOwnership(ctx) != nil {
		apiError(w, r, 503, "catalog_not_ready", "The media catalog is not ready. Restart the service if its database session was lost.")
		return
	}
	if s.taskManager != nil && !s.taskManager.Available() {
		apiError(w, r, 503, "tasks_not_ready", "The task scheduler is not ready.")
		return
	}
	if s.diagnostics != nil && !s.diagnostics.Status().Healthy {
		apiError(w, r, 503, "diagnostics_not_ready", "Diagnostic storage is unavailable. Check the service logs and configured storage.")
		return
	}
	jsonResponse(w, 200, map[string]string{"Status": "ready"})
}
