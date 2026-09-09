package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// The original-file endpoint is distinct from future conversion handlers.
// A filename here is a delivery alias, never a filesystem lookup argument.
func (s *Server) registerStreamRoutes(mux *http.ServeMux) {
	for _, resource := range []string{"Videos", "Audio"} {
		handler := s.originalStream(false)
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

func (s *Server) originalStream(audio bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		container, original, valid := streamAlias(r.PathValue("StreamFileName"))
		if !valid {
			apiError(w, r, http.StatusNotFound, "not_found", "The requested media delivery route is not available.")
			return
		}
		values, err := streamValues(r)
		if err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_stream_request", "The stream query contains invalid or conflicting values.")
			return
		}
		static := original
		if raw, supplied := values["static"]; supplied {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				apiError(w, r, http.StatusBadRequest, "invalid_stream_request", "Static must be a boolean.")
				return
			}
			if !value {
				apiError(w, r, http.StatusNotImplemented, "conversion_unavailable", "Media conversion is not yet available. Request original-file delivery.")
				return
			}
			static = true
		}
		if !static {
			// An unconstrained legacy stream request can read the original file.
			// Explicit conversion instructions cannot silently become a raw copy.
			for _, key := range []string{"videocodec", "audiocodec", "videobitrate", "audiobitrate", "width", "height",
				"maxwidth", "maxheight", "audiochannels", "maxaudiochannels", "audiosamplerate", "segmentcontainer"} {
				if values[key] != "" {
					apiError(w, r, http.StatusNotImplemented, "conversion_unavailable", "The requested media transformation is not yet available.")
					return
				}
			}
		}
		if value := values["container"]; value != "" {
			value = strings.ToLower(value)
			if container != "" && container != value {
				apiError(w, r, http.StatusBadRequest, "invalid_stream_request", "Container selectors must agree.")
				return
			}
			container = value
		}
		for _, name := range []string{"starttimeticks", "startpositionticks"} {
			if raw, supplied := values[name]; supplied {
				value, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || value < 0 {
					apiError(w, r, http.StatusBadRequest, "invalid_stream_request", "Playback positions must use non-negative 64-bit ticks.")
					return
				}
			}
		}
		principal := r.Context().Value(principalKey).(identity.Principal)
		if deviceID := values["deviceid"]; deviceID != "" && deviceID != principal.Client.DeviceID {
			apiError(w, r, http.StatusForbidden, "device_mismatch", "The stream request belongs to another authenticated device.")
			return
		}
		select {
		case s.streamSlots <- struct{}{}:
			defer func() { <-s.streamSlots }()
		default:
			w.Header().Set("Retry-After", "2")
			apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The server has reached its active original-stream limit.")
			return
		}
		openContext, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		file, source, err := s.library.OpenMedia(openContext, principal.User.ID, r.PathValue("Id"), values["mediasourceid"])
		cancel()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			if errors.Is(err, context.DeadlineExceeded) {
				apiError(w, r, http.StatusServiceUnavailable, "media_timeout", "The media source could not be opened within the time limit.")
				return
			}
			s.libraryError(w, r, err)
			return
		}
		defer file.Close()
		if (source.Item.Type == "Audio") != audio {
			apiError(w, r, http.StatusNotFound, "not_found", "The item is not available through this media route.")
			return
		}
		if container != "" && container != source.Container {
			apiError(w, r, http.StatusUnsupportedMediaType, "container_conversion_unavailable", "The requested container differs from the original media source.")
			return
		}
		serveOriginalMedia(w, r, file, source)
	}
}

// serveOriginalMedia only writes a source already opened and authorized by its
// caller. Sharing the delivery code keeps Universal and legacy original reads
// on the same snapshot without reopening a potentially changed source.
func serveOriginalMedia(w http.ResponseWriter, r *http.Request, file *os.File, source library.MediaFile) {
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
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-r.Context().Done():
			_ = file.Close()
		case <-finished:
		}
	}()
	// net/http provides byte/suffix/multipart ranges, HEAD, If-Range and
	// conditional responses. Ticks never become a guessed source-byte offset.
	// Linux ctime invalidates date validators when a writer restores mtime;
	// ETags remain the precise validator because HTTP dates have second precision.
	http.ServeContent(w, r, "original."+source.Container, modified, file)
}
