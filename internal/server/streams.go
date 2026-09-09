package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"

	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Shared media aliases dispatch to original or negotiated conversion delivery.
// A filename here is a delivery alias, never a filesystem lookup argument.
func (s *Server) registerStreamRoutes(mux *http.ServeMux) {
	for _, resource := range []string{"Videos", "Audio"} {
		handler := s.videoStream
		if resource == "Audio" {
			handler = s.audioStream
		}
		for _, base := range []string{"/emby/" + resource, "/emby/" + strings.ToLower(resource), "/" + resource, "/" + strings.ToLower(resource)} {
			mux.HandleFunc("GET "+base+"/{Id}/stream", s.requireEmby(handler))
			mux.HandleFunc("GET "+base+"/{Id}/{StreamFileName}", s.requireEmby(handler))
		}
	}
}

func streamValues(r *http.Request) (map[string]string, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return imageQuery(values)
}

func streamAlias(name string) (container string, original bool, valid bool) {
	if name == "" || strings.EqualFold(name, "stream") {
		return "", false, true
	}
	prefix, extension, found := strings.Cut(strings.ToLower(name), ".")
	if !found || (prefix != "stream" && prefix != "original") || extension == "" || len(extension) > 16 {
		return "", false, false
	}
	for _, character := range extension {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9') {
			return "", false, false
		}
	}
	return extension, prefix == "original", true
}

// serveOriginalMedia only writes a source already opened and authorized by its
// caller. Sharing the delivery code keeps Universal and legacy original reads
// on the same snapshot without reopening a potentially changed source.
func (s *Server) serveOriginalMedia(w http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile) {
	work, finish, err := s.guardOriginalMedia(w, r, file, source)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			s.identityError(w, r, err)
		} else if errors.Is(err, context.Canceled) {
			if r.Context().Err() == nil {
				apiError(w, r, http.StatusServiceUnavailable, "media_cancelled", "The original media response was cancelled.")
			}
		} else {
			s.libraryError(w, r, err)
		}
		return
	}
	defer finish()
	r = r.WithContext(work)
	w.Header().Set("Content-Type", source.MIMEType)
	w.Header().Set("ETag", source.ETag)
	w.Header().Set("Cache-Control", "private, no-transform")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag, Last-Modified")
	modified := source.ModifiedAt
	if source.Item.Media != nil && source.Item.Media.FileChangeTimeNs > 0 {
		changed := time.Unix(0, source.Item.Media.FileChangeTimeNs).UTC()
		if changed.After(modified) {
			modified = changed
		}
	}
	// net/http provides byte/suffix/multipart ranges, HEAD, If-Range and
	// conditional responses. Ticks never become a guessed source-byte offset.
	// Linux ctime invalidates date validators when a writer restores mtime;
	// ETags remain the precise validator because HTTP dates have second precision.
	http.ServeContent(w, r, "original."+source.Container, modified, file)
	if work.Err() != nil {
		panic(http.ErrAbortHandler)
	}
}
