package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type Server struct {
	cfg           config.Config
	db            *pgxpool.Pool
	identity      *identity.Store
	log           *slog.Logger
	version       string
	serverID      string
	limiter       *loginLimiter
	library       *library.Store
	images        *imageCache
	streamSlots   chan struct{}
	originals     *originalStreamRuntime
	subtitleSlots chan struct{}
	eventHub      *events.Hub
	sockets       *socketRuntime
	notifier      *userDataNotifier
	hls           *hlsRuntime
}

func New(ctx context.Context, cfg config.Config, db *pgxpool.Pool, users *identity.Store, logger *slog.Logger, version string) (*Server, error) {
	id, err := users.ServerID(ctx)
	if err != nil {
		return nil, err
	}
	catalog, err := library.New(db, media.Prober{FFprobePath: cfg.FFprobePath, Timeout: 30 * time.Second}, cfg.MediaRoots)
	if err != nil {
		return nil, err
	}
	hub, err := events.New(events.Options{})
	if err != nil {
		_ = catalog.Close(ctx)
		return nil, err
	}
	app := &Server{cfg: cfg, db: db, identity: users, log: logger, version: version, serverID: id, limiter: newLoginLimiter(), library: catalog, images: newImageCache(), streamSlots: make(chan struct{}, 64), subtitleSlots: make(chan struct{}, 4), eventHub: hub, sockets: newSocketRuntime()}
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
	return app, nil
}

func (s *Server) Close(ctx context.Context) error {
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
	s.registerAdminMetadataRoutes(mux)
	s.registerLibraryRoutes(mux)
	s.registerEntityRoutes(mux)
	s.registerImageRoutes(mux)
	s.registerStreamRoutes(mux)
	s.registerPlaybackRoutes(mux)
	s.registerClientSessionRoutes(mux)
	s.registerSubtitleRoutes(mux)
	s.registerRemoteCommandRoutes(mux)
	s.registerHLSRoutes(mux)
	mux.HandleFunc("/admin/v1/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, r, 404, "not_found", "The requested administrator API is not available.")
	})
	mux.HandleFunc("GET /emby/System/Info/Public", s.publicSystemInfo)
	mux.HandleFunc("GET /emby/System/Info", s.requireEmby(s.systemInfo))
	mux.HandleFunc("GET /emby/System/Ping", s.ping)
	mux.HandleFunc("POST /emby/System/Ping", s.ping)
	mux.HandleFunc("GET /emby/Users/Public", s.publicUsers)
	mux.HandleFunc("POST /emby/Users/AuthenticateByName", s.embyLogin)
	mux.HandleFunc("POST /emby/Users/{Id}/Authenticate", s.embyLoginByID)
	mux.HandleFunc("GET /emby/Users/{Id}", s.requireEmby(s.embyUser))
	mux.HandleFunc("GET /emby/Users/Query", s.requireEmby(s.embyUsers))
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
	return s.middleware(mux)
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
	jsonResponse(w, 200, map[string]string{"Status": "ready"})
}
