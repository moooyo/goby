package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerMediaDownloadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Items/{Id}/Download", s.requireEmby(s.downloadMedia))
	mux.HandleFunc("GET /emby/Items/{Id}/File", s.requireEmby(s.downloadMedia))
}

// The pinned LibraryService download contract only takes the route item ID.
// Credential carriers remain valid; no query value can select a path, source,
// target user, filename, conversion, or playback context.
func validateMediaDownloadRequest(r *http.Request) error {
	id := r.PathValue("Id")
	if id == "" || len(id) > 256 || strings.TrimSpace(id) != id || !utf8.ValidString(id) ||
		strings.IndexFunc(id, unicode.IsControl) >= 0 || strings.ContainsAny(id, `/\`) ||
		r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return library.ErrInvalidInput
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return library.ErrInvalidInput
	}
	for name := range values {
		switch strings.ToLower(name) {
		case "api_key", "x-emby-token", "x-emby-client", "x-emby-client-version", "x-emby-device-id", "x-emby-device-name":
		default:
			return library.ErrInvalidInput
		}
	}
	return nil
}

func (s *Server) downloadMedia(w http.ResponseWriter, r *http.Request) {
	if err := validateMediaDownloadRequest(r); err != nil {
		s.libraryError(w, r, err)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	select {
	case s.streamSlots <- struct{}{}:
		defer func() { <-s.streamSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The active media response limit has been reached.")
		return
	}
	prepare, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	source, err := s.library.PlanDownloadFor(prepare, librarySubject(principal, principal.User.ID), r.PathValue("Id"), "")
	if err != nil {
		s.downloadMediaError(w, r, err)
		return
	}
	s.servePreparedOriginalDownloadSnapshot(w, r, prepare, source)
}

func (s *Server) downloadMediaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	case errors.Is(err, library.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The authenticated owner has reached its active media response limit.")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		if r.Context().Err() == nil {
			apiError(w, r, http.StatusServiceUnavailable, "media_cancelled", "The media download response was cancelled.")
		}
	case errors.Is(err, library.ErrSourceChanged):
		s.libraryError(w, r, library.ErrUnavailable)
	default:
		s.libraryError(w, r, err)
	}
}

func (s *Server) authorizeDownload(ctx context.Context, principal identity.Principal, source library.MediaFile) error {
	fresh, err := s.identity.RevalidateSession(ctx, principal)
	if err != nil {
		return err
	}
	if fresh.IsApplicationKey() != principal.IsApplicationKey() || fresh.User.ID != principal.User.ID ||
		fresh.ClientSessionID != principal.ClientSessionID || fresh.SessionID != principal.SessionID ||
		fresh.Client.DeviceID != principal.Client.DeviceID {
		return library.ErrNotFound
	}
	verified, current, err := s.library.OpenDownloadFor(ctx, librarySubject(fresh, fresh.User.ID), source.Item.ID, source.SourceID)
	if err != nil {
		return err
	}
	_ = verified.Close()
	if current.ETag != source.ETag {
		return library.ErrSourceChanged
	}
	return nil
}

// Downloads share shutdown and resource bounds with original responses, while
// their authorization stays independent from playback, bitrate and play leases.
// Every failed revalidation cancels blocked writes and closes the descriptor;
// a retriable GET must not retain an older policy during catalog unavailability.
func (s *Server) guardDownloadMedia(_ http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile) (context.Context, func(), error) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	lifetime, leave, err := s.originals.enterSource(principal, source.Item.ID, source.SourceID)
	if err != nil {
		return nil, nil, err
	}
	work, cancel := context.WithCancel(r.Context())
	stopLifetime := context.AfterFunc(lifetime, cancel)
	stopExpiry := func() {}
	if !principal.IsApplicationKey() && !principal.ExpiresAt.IsZero() {
		timer := time.AfterFunc(time.Until(principal.ExpiresAt), cancel)
		stopExpiry = func() { timer.Stop() }
	}
	check, stopCheck := context.WithTimeout(work, 10*time.Second)
	err = s.authorizeDownload(check, principal, source)
	stopCheck()
	if err != nil {
		stopExpiry()
		stopLifetime()
		cancel()
		leave()
		return nil, nil, err
	}
	finished, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		abort := func() {
			cancel()
			_ = file.Close()
		}
		for {
			select {
			case <-finished:
				return
			case <-work.Done():
				abort()
				return
			case <-ticker.C:
				check, stopCheck := context.WithTimeout(work, 750*time.Millisecond)
				err := s.authorizeDownload(check, principal, source)
				stopCheck()
				if err != nil {
					abort()
					return
				}
			}
		}
	}()
	var once sync.Once
	finish := func() {
		once.Do(func() {
			close(finished)
			stopExpiry()
			stopLifetime()
			cancel()
			<-watched
			leave()
		})
	}
	return work, finish, nil
}
