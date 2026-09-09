package server

import (
	"crypto/subtle"
	"net/http"
	"runtime"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
)

func nativeUser(user identity.User) map[string]any {
	return map[string]any{"Id": user.ID, "Name": user.Name, "IsAdministrator": user.IsAdministrator, "IsDisabled": user.IsDisabled, "HasPassword": user.HasPassword, "CreatedAt": user.CreatedAt}
}

func (s *Server) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.identity.Initialized(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"Initialized": initialized})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) || !s.allowLogin(w, r) {
		return
	}
	var body struct{ SetupToken, Name, Password string }
	if !decodeBody(w, r, &body) {
		return
	}
	if s.cfg.SetupToken == "" || subtle.ConstantTimeCompare([]byte(body.SetupToken), []byte(s.cfg.SetupToken)) != 1 {
		apiError(w, r, 403, "setup_token_invalid", "The setup token is invalid.")
		return
	}
	user, err := s.identity.Bootstrap(r.Context(), body.Name, body.Password)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	s.log.Info("administrator setup completed", "user_id", user.ID)
	jsonResponse(w, 201, map[string]any{"User": nativeUser(user)})
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) || !s.allowLogin(w, r) {
		return
	}
	var body struct{ Name, Password string }
	if !decodeBody(w, r, &body) {
		return
	}
	client := identity.Client{Name: "Goby Dashboard", DeviceID: "goby-dashboard", Device: "Web browser", Version: s.version}
	credentials, err := s.identity.Authenticate(r.Context(), body.Name, body.Password, client, "admin")
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: credentials.Token, Path: "/admin", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, Expires: credentials.ExpiresAt, MaxAge: int(time.Until(credentials.ExpiresAt).Seconds())})
	jsonResponse(w, 200, map[string]any{"User": nativeUser(credentials.User), "CSRFToken": csrfToken(credentials.Token)})
}

func (s *Server) adminSession(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	cookie, _ := r.Cookie(sessionCookie)
	jsonResponse(w, 200, map[string]any{"User": nativeUser(principal.User), "CSRFToken": csrfToken(cookie.Value)})
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookie)
	if err := s.identity.Revoke(r.Context(), cookie.Value); err != nil {
		s.identityError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/admin", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func implementedFeatures() map[string]bool {
	return map[string]bool{"UserManagement": true, "LibraryManagement": true, "Playback": true, "Transcoding": false, "HardwareDecoding": false, "HardwareEncoding": false}
}

func (s *Server) runtimeFeatures() map[string]bool {
	features := implementedFeatures()
	if s.hls != nil {
		features["Transcoding"] = true
		hardware := s.cfg.Transcoding.Hardware
		features["HardwareDecoding"] = hardware.Decode != "" && hardware.Decode != "software"
		features["HardwareEncoding"] = hardware.Encode != "" && hardware.Encode != "software"
	}
	return features
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	var userCount, sessionCount, libraryCount, itemCount int
	err := s.db.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM users),
		(SELECT count(*) FROM sessions JOIN users ON sessions.user_id=users.id
		WHERE revoked_at IS NULL AND expires_at>now() AND NOT users.is_disabled
		AND (sessions.kind='emby' OR users.is_administrator)),
		(SELECT count(*) FROM libraries), (SELECT count(*) FROM items WHERE NOT is_folder)`).Scan(&userCount, &sessionCount, &libraryCount, &itemCount)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{
		"Server":   map[string]string{"Id": s.serverID, "Name": s.cfg.ServerName, "Version": s.version},
		"Database": map[string]string{"Status": "connected", "Engine": "PostgreSQL"},
		"Counts":   map[string]int{"Users": userCount, "Libraries": libraryCount, "Items": itemCount, "ActiveSessions": sessionCount},
		"Runtime":  map[string]string{"GoVersion": runtime.Version()}, "Features": s.runtimeFeatures(),
	})
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	decode, encode := []string{}, []string{}
	hardware := s.cfg.Transcoding.Hardware
	if s.hls != nil && hardware.Decode != "" && hardware.Decode != "software" {
		decode = append(decode, hardware.Decode)
	}
	if s.hls != nil && hardware.Encode != "" && hardware.Encode != "software" {
		encode = append(encode, hardware.Encode)
	}
	jsonResponse(w, 200, map[string]any{
		"ServerVersion": s.version, "Features": s.runtimeFeatures(),
		"Toolchain": map[string]string{"Go": config.GoVersion, "FFmpeg": config.FFmpegVersion},
		"Hardware":  map[string]any{"Verified": false, "Configured": len(decode)+len(encode) > 0, "Decode": decode, "Encode": encode},
	})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	users, err := s.identity.ListUsers(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(users))
	for _, user := range users {
		items = append(items, nativeUser(user))
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Password  string
		IsAdministrator bool
	}
	if !decodeBody(w, r, &body) {
		return
	}
	user, err := s.identity.CreateUser(r.Context(), body.Name, body.Password, body.IsAdministrator)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	s.log.Info("user created", "actor_id", principal.User.ID, "user_id", user.ID, "administrator", body.IsAdministrator)
	jsonResponse(w, 201, map[string]any{"User": nativeUser(user)})
}
