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
const imageTransferBytes = 128 << 20
const imageTransferCount = 8

type cachedImage struct {
	key, contentType, etag string
	data                   []byte
}

// Cache entries contain immutable validated bytes. Source files are opened and
// checked before every lookup, including a conditional request or cache hit.
type imageCache struct {
	mu              sync.Mutex
	entries         map[string]*list.Element
	order           *list.List
	bytes           int
	slots           chan struct{}
	activeTransfers int
	activeBytes     int
}

// Rendering and transmission have separate budgets. A slow transfer does not
// retain a decode/processing slot or admit unbounded in-memory response bodies.
func (c *imageCache) beginTransfer(size int) (func(), bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if size < 0 || c.activeTransfers >= imageTransferCount || size > imageTransferBytes-c.activeBytes {
		return nil, false
	}
	c.activeTransfers++
	c.activeBytes += size
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.activeTransfers--
			c.activeBytes -= size
			c.mu.Unlock()
		})
	}, true
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
	// Artwork carries the same library and parental visibility as its item.
	// Authorize before evaluating cache validators, including a potential 304.
	mux.HandleFunc("GET /emby/Items/{Id}/Images/{Type}", s.requireEmby(s.embyImage))
	mux.HandleFunc("GET /emby/Items/{Id}/Images/{Type}/{Index}", s.requireEmby(s.embyImage))
}

func (s *Server) embyImages(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	images, err := s.library.ListImagesFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"))
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
	if strings.EqualFold(r.PathValue("Type"), "ClearArt") {
		request.typeName = "Art"
	}
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
	// Logos and clear art normally remove their transparent/white margins. An
	// explicit false must survive parsing instead of being confused with absence.
	request.options.CropWhitespace = request.typeName == "Logo" || request.typeName == "Art"
	for name, target := range map[string]*bool{
		"cropwhitespace": &request.options.CropWhitespace, "autoorient": &request.options.AutoOrient,
		"disableanimation": &request.options.DisableAnimation, "addplayedindicator": &request.options.AddPlayedIndicator,
	} {
		if raw, exists := values[name]; exists {
			value, parseErr := strconv.ParseBool(raw)
			if parseErr != nil {
				return request, fmt.Errorf("invalid image transformation switch")
			}
			*target = value
		}
	}
	if raw, exists := values["keepanimation"]; exists {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return request, fmt.Errorf("invalid animation switch")
		}
		if _, explicit := values["disableanimation"]; explicit && request.options.DisableAnimation == value {
			return request, fmt.Errorf("conflicting animation switches")
		}
		request.options.DisableAnimation = !value
	}
	// No separately installed enhancer plugins exist; both values select the
	// same empty plugin set. Built-in, explicitly requested effects remain active.
	if raw, exists := values["enableimageenhancers"]; exists {
		if _, err := strconv.ParseBool(raw); err != nil {
			return request, fmt.Errorf("invalid enhancer switch")
		}
	}
	request.options.BackgroundColor = values["backgroundcolor"]
	request.options.ForegroundLayer = values["foregroundlayer"]
	if raw, exists := values["percentplayed"]; exists {
		request.options.PercentPlayed, err = strconv.ParseFloat(raw, 64)
		if err != nil {
			return request, fmt.Errorf("invalid played percentage")
		}
	}
	if raw, exists := values["unplayedcount"]; exists {
		request.options.UnplayedCount, err = strconv.Atoi(raw)
		if err != nil {
			return request, fmt.Errorf("invalid unplayed count")
		}
	}
	var cropParts [4]int
	cropNames := []string{"cropx", "cropy", "cropwidth", "cropheight"}
	coordinates := 0
	for index, name := range cropNames {
		if raw, exists := values[name]; exists {
			cropParts[index], err = strconv.Atoi(raw)
			if err != nil {
				return request, fmt.Errorf("invalid crop coordinate")
			}
			coordinates++
		}
	}
	if coordinates != 0 && coordinates != len(cropParts) {
		return request, fmt.Errorf("all four crop coordinates are required")
	}
	if raw, exists := values["crop"]; exists {
		parts := strings.Split(raw, ",")
		if len(parts) != len(cropParts) {
			return request, fmt.Errorf("crop requires x,y,width,height")
		}
		for index, part := range parts {
			value, parseErr := strconv.Atoi(part)
			if parseErr != nil || coordinates != 0 && value != cropParts[index] {
				return request, fmt.Errorf("invalid or conflicting crop coordinates")
			}
			cropParts[index] = value
		}
		coordinates = len(cropParts)
	}
	if coordinates != 0 {
		if cropParts[2] <= 0 || cropParts[3] <= 0 {
			return request, fmt.Errorf("crop dimensions must be positive")
		}
		request.options.Crop = artwork.CropRect{X: cropParts[0], Y: cropParts[1], Width: cropParts[2], Height: cropParts[3]}
	}
	request.options, err = artwork.CanonicalOptions(request.options)
	if err != nil {
		return request, err
	}
	request.tag = values["tag"]
	return request, nil
}

func imageVariantKey(tag string, options artwork.Options) string {
	key, err := artwork.OptionsKey(options)
	if err != nil {
		// Only canonical options reach handlers. Keep malformed internal options
		// outside every valid cache namespace if a future caller breaks that rule.
		return tag + "/invalid-options"
	}
	return tag + "/" + key
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
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	request, err := parseImageRequest(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_image_request", "Check the image type, index, format, dimensions, and supported transformations.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	processing := false
	releaseProcessing := func() {
		if processing {
			<-s.images.slots
			processing = false
		}
	}
	select {
	case s.images.slots <- struct{}{}:
		processing = true
		defer releaseProcessing()
	case <-ctx.Done():
		s.imageError(w, r, ctx.Err())
		return
	}
	file, source, err := s.library.OpenImageContentFor(ctx, requestLibrarySubject(r, userID), r.PathValue("Id"), request.typeName, request.index)
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
		if rendered.Source.Tag != source.ContentDigest() {
			s.imageError(w, r, library.ErrUnavailable)
			return
		}
		result = cachedImage{key: key, contentType: rendered.MIMEType, etag: imageETag(source.Tag, request.options), data: rendered.Bytes}
		s.images.put(result)
	}
	// Recompute current source selection and permissions before every response,
	// including a generated-image cache hit or conditional 304.
	fresh, current, err := s.library.OpenImageContentFor(ctx, requestLibrarySubject(r, userID), r.PathValue("Id"), request.typeName, request.index)
	if err != nil {
		s.imageError(w, r, err)
		return
	}
	_ = fresh.Close()
	if source.Tag != current.Tag || source.ContentDigest() != current.ContentDigest() {
		s.imageError(w, r, library.ErrRevisionConflict)
		return
	}
	if err := ctx.Err(); err != nil {
		s.imageError(w, r, err)
		return
	}
	transferSize := cap(result.data)
	if r.Method == http.MethodHead || matchesImageETag(strings.Join(r.Header.Values("If-None-Match"), ","), result.etag) {
		transferSize = 0
	}
	releaseTransfer, ok := s.images.beginTransfer(transferSize)
	if !ok {
		releaseProcessing()
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "image_transfer_limit", "The active image transfer limit has been reached.")
		return
	}
	defer releaseTransfer()
	releaseProcessing()
	writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer writer.finish()
	w.Header().Set("Content-Type", result.contentType)
	w.Header().Set("ETag", result.etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if matchesImageETag(strings.Join(r.Header.Values("If-None-Match"), ","), result.etag) {
		// Revalidation must pass current authorization, even for unchanged bytes.
		w.Header().Del("Content-Length")
		writer.WriteHeader(http.StatusNotModified)
		return
	}
	if request.tag != "" && request.tag == source.Tag {
		if !source.ModifiedAt.IsZero() {
			w.Header().Set("Last-Modified", source.ModifiedAt.UTC().Format(http.TimeFormat))
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(result.data)))
	writer.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = writer.Write(result.data)
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
