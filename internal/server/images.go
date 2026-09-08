package server

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/library"
)

const imageCacheBytes = 64 << 20
const imageCacheEntries = 256

type cachedImage struct {
	key, contentType, etag string
	data                   []byte
}

// Cache entries contain immutable validated bytes. Source files are opened and
// checked before every lookup, including a conditional request or cache hit.
type imageCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	bytes   int
	slots   chan struct{}
}

func newImageCache() *imageCache {
	return &imageCache{entries: make(map[string]*list.Element), order: list.New(), slots: make(chan struct{}, 4)}
}

func (c *imageCache) get(key string) (cachedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return cachedImage{}, false
	}
	c.order.MoveToFront(entry)
	return entry.Value.(cachedImage), true
}

func (c *imageCache) put(value cachedImage) {
	// Count backing capacity rather than length: an encoder may return a slice
	// backed by a larger allocation. Large representations bypass the cache.
	cost := cap(value.data)
	if cost > 8<<20 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if previous, ok := c.entries[value.key]; ok {
		c.bytes -= cap(previous.Value.(cachedImage).data)
		c.order.Remove(previous)
	}
	c.entries[value.key] = c.order.PushFront(value)
	c.bytes += cost
	for c.bytes > imageCacheBytes || len(c.entries) > imageCacheEntries {
		oldest := c.order.Back()
		old := oldest.Value.(cachedImage)
		delete(c.entries, old.key)
		c.bytes -= cap(old.data)
		c.order.Remove(oldest)
	}
}

func (s *Server) registerImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Items/{Id}/Images", s.requireEmby(s.embyImages))
	// The reference serves indexed item artwork without authenticating its
	// binary endpoint. Metadata enumeration and every mutation remain separate.
	mux.HandleFunc("GET /emby/Items/{Id}/Images/{Type}", s.embyImage)
	mux.HandleFunc("GET /emby/Items/{Id}/Images/{Type}/{Index}", s.embyImage)
}

func (s *Server) embyImages(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	images, err := s.library.ListImages(r.Context(), userID, r.PathValue("Id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(images))
	for _, source := range images {
		item := map[string]any{"ImageType": source.ImageType, "Path": source.Path, "Filename": source.Filename,
			"Width": source.Width, "Height": source.Height, "Size": source.Size}
		if source.ImageType == "Backdrop" || source.ImageType == "Screenshot" || source.ImageIndex != 0 {
			item["ImageIndex"] = source.ImageIndex
		}
		items = append(items, item)
	}
	jsonResponse(w, http.StatusOK, items)
}

type imageRequest struct {
	typeName string
	index    int
	tag      string
	options  artwork.Options
}

func imageQuery(values url.Values) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for name, entries := range values {
		name = strings.ToLower(name)
		for _, value := range entries {
			if previous, ok := result[name]; ok && previous != value {
				return nil, fmt.Errorf("conflicting image query values")
			}
			result[name] = value
		}
	}
	return result, nil
}

func parseImageRequest(r *http.Request) (imageRequest, error) {
	var request imageRequest
	for _, name := range []string{"Primary", "Backdrop", "Thumb", "Banner", "Logo", "Art", "Disc", "Box", "BoxRear", "Menu", "Screenshot"} {
		if strings.EqualFold(name, r.PathValue("Type")) {
			request.typeName = name
			break
		}
	}
	if request.typeName == "" {
		return request, fmt.Errorf("unknown image type")
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return request, fmt.Errorf("invalid image query encoding")
	}
	values, err := imageQuery(query)
	if err != nil {
		return request, err
	}
	for name, target := range map[string]*int{"width": &request.options.Width, "height": &request.options.Height,
		"maxwidth": &request.options.MaxWidth, "maxheight": &request.options.MaxHeight, "quality": &request.options.Quality} {
		if raw, exists := values[name]; exists {
			value, parseErr := strconv.Atoi(raw)
			maximum := 4096
			if name == "quality" {
				maximum = 100
			}
			if parseErr != nil || value < 0 || value > maximum {
				return request, fmt.Errorf("invalid image size or quality")
			}
			*target = value
		}
	}
	index, indexExists := values["index"]
	if pathIndex := r.PathValue("Index"); pathIndex != "" {
		if indexExists && index != pathIndex {
			return request, fmt.Errorf("image indices must agree")
		}
		index, indexExists = pathIndex, true
	}
	if indexExists {
		value, parseErr := strconv.Atoi(index)
		if parseErr != nil || value < 0 || value >= 32 {
			return request, fmt.Errorf("invalid image index")
		}
		request.index = value
	}
	request.options.Format = strings.ToLower(values["format"])
	switch request.options.Format {
	case "", "original":
		request.options.Format = ""
	case "jpg", "jpeg":
		request.options.Format = "jpeg"
	case "png", "gif":
	default:
		return request, fmt.Errorf("unsupported image format")
	}
	// Enhancers are an empty set in this implementation. Explicit transforms
	// that the renderer cannot honor are rejected instead of returning a false
	// approximation of requested crop, orientation, animation, or overlays.
	for _, name := range []string{"cropwhitespace", "autoorient", "keepanimation", "addplayedindicator", "enableimageenhancers"} {
		if raw, exists := values[name]; exists {
			value, parseErr := strconv.ParseBool(raw)
			if parseErr != nil || (value && name != "enableimageenhancers") {
				return request, fmt.Errorf("unsupported image transformation")
			}
		}
	}
	for _, name := range []string{"backgroundcolor", "foregroundlayer"} {
		if values[name] != "" {
			return request, fmt.Errorf("unsupported image transformation")
		}
	}
	for _, name := range []string{"percentplayed", "unplayedcount"} {
		if raw, exists := values[name]; exists {
			value, parseErr := strconv.ParseFloat(raw, 64)
			if parseErr != nil || value != 0 {
				return request, fmt.Errorf("unsupported image transformation")
			}
		}
	}
	request.tag = values["tag"]
	return request, nil
}

func imageVariantKey(tag string, options artwork.Options) string {
	return fmt.Sprintf("%s/%s/%d/%d/%d/%d/%d", tag, options.Format, options.Width, options.Height,
		options.MaxWidth, options.MaxHeight, options.Quality)
}

func imageETag(tag string, options artwork.Options) string {
	if options == (artwork.Options{}) {
		return `"` + tag + `"`
	}
	sum := sha256.Sum256([]byte(imageVariantKey(tag, options)))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func matchesImageETag(header, tag string) bool {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || strings.TrimPrefix(value, "W/") == tag {
			return true
		}
	}
	return false
}

func (s *Server) embyImage(w http.ResponseWriter, r *http.Request) {
	request, err := parseImageRequest(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_image_request", "Check the image type, index, format, dimensions, and supported transformations.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	select {
	case s.images.slots <- struct{}{}:
		defer func() { <-s.images.slots }()
	case <-ctx.Done():
		s.imageError(w, r, ctx.Err())
		return
	}
	file, source, err := s.library.OpenPublicImage(ctx, r.PathValue("Id"), request.typeName, request.index)
	if err != nil {
		s.imageError(w, r, err)
		return
	}
	defer file.Close()
	key := imageVariantKey(source.Tag, request.options)
	result, found := s.images.get(key)
	if !found {
		rendered, renderErr := artwork.Render(ctx, file, request.options)
		if renderErr != nil {
			s.imageError(w, r, renderErr)
			return
		}
		// The open inode can still be modified in place. Only content matching
		// the indexed source tag may populate the cache or reach the response.
		if rendered.Source.Tag != source.Tag {
			s.imageError(w, r, library.ErrUnavailable)
			return
		}
		result = cachedImage{key: key, contentType: rendered.MIMEType, etag: imageETag(source.Tag, request.options), data: rendered.Bytes}
		s.images.put(result)
	}
	if err := ctx.Err(); err != nil {
		s.imageError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", result.contentType)
	w.Header().Set("ETag", result.etag)
	if matchesImageETag(strings.Join(r.Header.Values("If-None-Match"), ","), result.etag) {
		// These omissions match the recorded reference GET and HEAD 304s.
		w.Header().Del("Content-Length")
		w.Header().Del("Cache-Control")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Cache-Control", "public")
	if request.tag != "" && request.tag == source.Tag {
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		w.Header().Set("Expires", time.Now().UTC().Add(365*24*time.Hour).Format(http.TimeFormat))
		if !source.ModifiedAt.IsZero() {
			w.Header().Set("Last-Modified", source.ModifiedAt.UTC().Format(http.TimeFormat))
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(result.data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(result.data)
	}
}

func (s *Server) imageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "image_timeout", "The image could not be processed within the time limit.")
	case errors.Is(err, artwork.ErrInvalidImage), errors.Is(err, artwork.ErrLimitExceeded), errors.Is(err, artwork.ErrUnsupportedFormat):
		apiError(w, r, http.StatusUnsupportedMediaType, "invalid_image", "The indexed image is unavailable or exceeds the supported image limits.")
	case errors.Is(err, artwork.ErrInvalidOptions):
		apiError(w, r, http.StatusBadRequest, "invalid_image_request", "The image transformation is not supported.")
	default:
		s.libraryError(w, r, err)
	}
}
