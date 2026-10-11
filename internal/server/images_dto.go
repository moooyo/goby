package server

import (
	"net/http"
	"strings"

	"github.com/moooyo/goby/internal/library"
)

// applyIndexedImages batches the authorized catalog reads after item projection.
// It does not expose image paths or make filesystem requests while listing items.
func (s *Server) applyIndexedImages(w http.ResponseWriter, r *http.Request, userID string, items []map[string]any, detail bool, presentation itemPresentation) bool {
	if !s.applyItemCapabilities(w, r, items, detail, presentation) {
		return false
	}
	if !presentation.enableImages {
		for _, item := range items {
			presentation.applyFieldExclusions(item)
		}
		return true
	}
	if presentation.invalidImageTypeLimit {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "ImageTypeLimit must be a non-negative integer.")
		return false
	}
	byID := make(map[string][]library.Image)
	subject := requestLibrarySubject(r, userID)
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
		batch, err := s.library.ImagesForItemsFor(r.Context(), subject, ids)
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
		presentation.applyIndexedImageTags(item, byID[id], detail)
	}
	return true
}

func (presentation itemPresentation) applyIndexedImageTags(item map[string]any, images []library.Image, detail bool) {
	tags := make(map[string]string)
	backdrops := make([]string, 0)
	for _, source := range images {
		if presentation.imageTypeLimit == 0 || (len(presentation.enabledImageTypes) > 0 && !presentation.enabledImageTypes[strings.ToLower(source.ImageType)]) {
			continue
		}
		if source.ImageType == "Backdrop" {
			if len(backdrops) < presentation.imageTypeLimit {
				backdrops = append(backdrops, source.Tag)
			}
		} else if _, found := tags[source.ImageType]; !found {
			tags[source.ImageType] = source.Tag
			if source.ImageType == "Primary" && source.Height > 0 &&
				(detail || hasField(presentation.fields, "PrimaryImageAspectRatio")) {
				item["PrimaryImageAspectRatio"] = float64(source.Width) / float64(source.Height)
			}
		}
	}
	item["ImageTags"] = tags
	item["BackdropImageTags"] = backdrops
	presentation.applyFieldExclusions(item)
}
