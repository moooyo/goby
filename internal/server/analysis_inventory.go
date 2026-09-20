package server

import (
	"strconv"

	"github.com/moooyo/goby/internal/library"
)

// A valid persisted reference may outlive LRU eviction or a disabled cache.
// Do not advertise it as resident merely because its database revision matches.
// This cheap inventory snapshot is not an authorization or filesystem proof;
// preview delivery always acquires and revalidates the actual source and bytes.
func (s *Server) residentMediaAnalysisItemDTO(item library.AnalysisItem) adminMediaAnalysisItem {
	item.Previews = append([]library.AnalysisPreviewStatus{}, item.Previews...)
	for index := range item.Previews {
		preview := &item.Previews[index]
		if preview.Status != "ready" {
			continue
		}
		if s.mediaAnalysis == nil || s.mediaAnalysis.cache == nil {
			preview.Status, preview.FailureCode = "unavailable", "cache_not_configured"
			continue
		}
		runtime := s.mediaAnalysis
		runtime.mu.Lock()
		closing := runtime.closing
		unsafe := runtime.failures[library.TaskPreviewGenerationKey] == "cache_unavailable"
		runtime.mu.Unlock()
		if closing || unsafe {
			preview.Status, preview.FailureCode = "unavailable", "cache_unavailable"
			continue
		}
		_, manifestPresent := s.mediaAnalysis.cache.ArtifactSnapshot(preview.CacheKey, preview.CacheSeal, "manifest.json")
		if !manifestPresent || !s.mediaAnalysis.cache.HasArtifact(preview.CacheKey, preview.CacheSeal,
			strconv.Itoa(preview.Width)+".bif", preview.CacheSHA256, preview.Size) {
			preview.Status, preview.FailureCode = "missing", "cache_not_resident"
		}
	}
	return adminMediaAnalysisItemDTO(item)
}
