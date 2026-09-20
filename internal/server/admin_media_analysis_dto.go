package server

import (
	"time"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
)

// Runtime projections contain availability and bounded aggregate accounting.
// They never expose cache roots, tool paths, cache keys or process identities.
type adminMediaAnalysisRuntimeStatus struct {
	Configured       bool
	IntroAvailable   bool
	PreviewAvailable bool
	Reasons          []string
	Cache            *adminMediaAnalysisCacheStatus
}

type adminMediaAnalysisCacheStatus struct {
	ReadyEntries        int
	BuildingEntries     int
	PendingPublications int
	Readers             int
	ReadyBytes          int64
	ReservedBytes       int64
	ControlBytes        int64
	TotalBytes          int64
	MaxBytes            int64
}

type adminMediaAnalysisPruneResult struct {
	RemovedEntries int
	RemovedBytes   int64
	RemainingBytes int64
	BusyEntries    int
}

type adminMediaAnalysisItem struct {
	ID             string `json:"Id"`
	Name           string
	Type           string
	LibraryID      string `json:"LibraryId"`
	MediaSourceID  string `json:"MediaSourceId"`
	SourceRevision string
	Detection      library.AnalysisDetection
	Previews       []adminMediaAnalysisPreview
}

type adminMediaAnalysisPreview struct {
	Width       int
	Height      int
	Size        int64
	FrameCount  int
	Status      string
	FailureCode string
	UpdatedAt   time.Time
}

func adminMediaAnalysisConfigurationDTO(value library.AnalysisConfiguration) library.AnalysisConfiguration {
	value.UpdatedAt = value.UpdatedAt.UTC()
	return value
}

func adminMediaAnalysisDetectionDTO(value library.AnalysisDetection) library.AnalysisDetection {
	value.Reasons = append([]string{}, value.Reasons...)
	value.UpdatedAt = value.UpdatedAt.UTC()
	if value.Candidate != nil {
		candidate := *value.Candidate
		candidate.Reasons = append([]introdetect.Reason{}, candidate.Reasons...)
		candidate.Support = append([]introdetect.Support{}, candidate.Support...)
		value.Candidate = &candidate
	}
	return value
}

func adminMediaAnalysisItemDTO(value library.AnalysisItem) adminMediaAnalysisItem {
	result := adminMediaAnalysisItem{ID: value.ID, Name: value.Name, Type: value.Type, LibraryID: value.LibraryID,
		MediaSourceID: value.MediaSourceID, SourceRevision: value.SourceRevision, Detection: adminMediaAnalysisDetectionDTO(value.Detection),
		Previews: make([]adminMediaAnalysisPreview, 0, len(value.Previews))}
	for _, preview := range value.Previews {
		result.Previews = append(result.Previews, adminMediaAnalysisPreview{Width: preview.Width, Height: preview.Height,
			Size: preview.Size, FrameCount: preview.FrameCount, Status: preview.Status, FailureCode: preview.FailureCode, UpdatedAt: preview.UpdatedAt.UTC()})
	}
	return result
}

func adminMediaAnalysisRuntimeDTO(value adminMediaAnalysisRuntimeStatus) adminMediaAnalysisRuntimeStatus {
	value.Reasons = append([]string{}, value.Reasons...)
	if value.Cache != nil {
		cache := *value.Cache
		value.Cache = &cache
	}
	return value
}
