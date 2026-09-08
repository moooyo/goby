package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/library"
)

// applyIndexedImages batches the authorized catalog reads after item projection.
// It does not expose image paths or make filesystem requests while listing items.
func (s *Server) applyIndexedImages(w http.ResponseWriter, r *http.Request, userID string, items []map[string]any, detail bool) bool {
	if raw := r.URL.Query().Get("EnableImages"); raw != "" {
		if enabled, _ := strconv.ParseBool(raw); !enabled {
			return true
		}
	}
	limit := 32
	if raw := r.URL.Query().Get("ImageTypeLimit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "ImageTypeLimit must be a non-negative integer.")
			return false
		}
		limit = min(value, 32)
	}
	enabledTypes := make(map[string]bool)
	for _, name := range queryValues(r.URL.Query()["EnableImageTypes"]) {
		enabledTypes[strings.ToLower(name)] = true
	}
	byID := make(map[string][]library.Image)
	for start := 0; start < len(items); start += 256 {
		ids := make([]string, 0, min(256, len(items)-start))
		for _, item := range items[start:min(start+256, len(items))] {
			if id, ok := item["Id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		batch, err := s.library.ImagesForItems(r.Context(), userID, ids)
		if err != nil {
			s.libraryError(w, r, err)
			return false
		}
		for id, images := range batch {
			byID[id] = images
		}
	}
	for _, item := range items {
		id, _ := item["Id"].(string)
		tags := make(map[string]string)
		backdrops := make([]string, 0)
		for _, source := range byID[id] {
			if limit == 0 || (len(enabledTypes) > 0 && !enabledTypes[strings.ToLower(source.ImageType)]) {
				continue
			}
			if source.ImageType == "Backdrop" {
				if len(backdrops) < limit {
					backdrops = append(backdrops, source.Tag)
				}
			} else if _, found := tags[source.ImageType]; !found {
				tags[source.ImageType] = source.Tag
				if source.ImageType == "Primary" && source.Height > 0 &&
					(detail || hasField(queryValues(r.URL.Query()["Fields"]), "PrimaryImageAspectRatio")) {
					item["PrimaryImageAspectRatio"] = float64(source.Width) / float64(source.Height)
				}
			}
		}
		item["ImageTags"] = tags
		item["BackdropImageTags"] = backdrops
	}
	return true
}
