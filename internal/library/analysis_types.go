package library

import (
	"context"
	"errors"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
)

var (
	ErrAnalysisConflict      = errors.New("media analysis revision conflict")
	ErrAnalysisSourceChanged = errors.New("media analysis source or cohort changed")
	ErrAnalysisSuppressed    = errors.New("media analysis result suppressed")
)

type AnalysisFence func(OwnedTx) error

type AnalysisProfile struct {
	AutoPublishIntros      bool
	PreviewIntervalSeconds int
	PreviewQuality         int
	MaxSourceBytes         int64
	MaxItemRuntimeSeconds  int
	FeatureCacheMaxBytes   int64
}

type AnalysisConfiguration struct {
	Revision  string
	Profile   AnalysisProfile
	Defaults  AnalysisProfile
	UpdatedAt time.Time
}

type AnalysisConfigurationUpdate struct {
	Revision string
	Profile  AnalysisProfile
}

// Execution identities are obtained before admission by the runtime. Admission
// never probes tools, reads files or starts a process while owning SQL state.
type AnalysisExecutionProfile struct {
	Version             int
	Available           bool
	UnavailableReason   string
	FFmpegSHA256        string
	FFprobeSHA256       string
	FingerprintSHA256   string
	DetectorVersion     string
	DetectorOptions     introdetect.Options
	VisualIntervalTicks int64
	PreviewProfile      string
	PreviewWidths       []int
	IntroProfile        string
}

type AnalysisAdmissionBinding struct {
	ConfigurationFingerprint string
	Bind                     func(OwnedTx, string) error
	SnapshotChildren         func(OwnedTx, string) (int64, error)
}

type AnalysisSource struct {
	ItemID            string `json:"ItemId"`
	LibraryID         string `json:"LibraryId"`
	RootID            string `json:"RootId"`
	SeriesID          string `json:"SeriesId"`
	SeasonID          string `json:"SeasonId"`
	EpisodeKey        string
	SourceRevision    string
	HierarchyRevision string
	MediaSourceID     string `json:"MediaSourceId"`
	DurationTicks     int64
	Size              int64
	Target            bool
	Position          int
	ItemType          string
	ManualRevision    string
	DecisionRevision  string
	PreviewRevision   string
}

type AnalysisWork struct {
	ChildID                  string `json:"ChildId"`
	RunID                    string `json:"RunId"`
	TaskKey                  string
	LibraryID                string `json:"LibraryId"`
	ScopeKey                 string
	CohortRevision           string
	ConfigurationFingerprint string
	ConfigurationRevision    string
	PublicationEpoch         int64
	Reason                   string
	Profile                  AnalysisProfile
	Execution                AnalysisExecutionProfile
	Force                    bool
	Sources                  []AnalysisSource
}

type AnalysisFeatures struct {
	ContentSHA256                 string
	AlgorithmProfile              string
	AudioBoundaryUncertaintyTicks int64
	Audio                         []introdetect.AudioSample
	Visual                        []introdetect.VisualSample
}

type AnalysisPreview struct {
	ItemID             string `json:"ItemId"`
	Revision           string
	SourceRevision     string
	ProfileFingerprint string
	ProfileRevision    string
	PublicationEpoch   int64
	CacheKey           string
	Seal               string
	Width              int
	Height             int
	SHA256             string
	Bytes              int64
	FrameCount         int
	IntervalTicks      int64
	NominalTicks       []int64
	ActualTicks        []int64
	UpdatedAt          time.Time
}

type AnalysisPreviewPublication struct {
	ItemID        string `json:"ItemId"`
	CacheKey      string
	Seal          string
	Width         int
	Height        int
	SHA256        string
	Bytes         int64
	FrameCount    int
	IntervalTicks int64
	NominalTicks  []int64
	ActualTicks   []int64
}

type AnalysisDetection struct {
	ItemID         string `json:"ItemId"`
	Revision       string
	ManualRevision string
	SourceRevision string
	Status         string
	Reasons        []string
	Candidate      *introdetect.Candidate
	Effective      *IntroInterval
	Suppressed     bool
	UpdatedAt      time.Time
}

type AnalysisDecision struct {
	Revision       string
	SourceRevision string
	ManualRevision string
	Action         string // accept, reject or reset
}

type AnalysisItemQuery struct {
	LibraryID, SearchTerm string
	StartIndex, Limit     int
}
type AnalysisItemPage struct {
	Items            []AnalysisItem
	TotalRecordCount int
}
type AnalysisItem struct {
	ID             string `json:"Id"`
	Name           string
	Type           string
	LibraryID      string `json:"LibraryId"`
	MediaSourceID  string `json:"MediaSourceId"`
	SourceRevision string
	Detection      AnalysisDetection
	Previews       []AnalysisPreviewStatus
}
type AnalysisPreviewStatus struct {
	Width, Height       int
	Size                int64
	FrameCount          int
	Status, FailureCode string
	UpdatedAt           time.Time
	CacheKey            string `json:"-"`
	CacheSeal           string `json:"-"`
	CacheSHA256         string `json:"-"`
}

// Keep context checks separate from the protected SQL driver's context. A task
// cancellation never becomes publication authority merely by entering an owner.
func analysisContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	return ctx.Err()
}
