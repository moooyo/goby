package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/moooyo/goby/internal/library"
)

const (
	MetadataRefreshKey  = "metadata.refresh"
	SubtitleDownloadKey = "subtitle.download"
	CacheMaintainKey    = "cache.maintain"
)

// CompatibilityKey publishes only implemented executors. Goby-prefixed keys
// distinguish narrower native work from unrelated upstream maintenance jobs.
func CompatibilityKey(key string) string {
	switch key {
	case LibraryScanKey:
		return LibraryScanEmbyKey
	case LibraryRefreshMediaKey:
		return "GobyRefreshMediaDetails"
	case MetadataRefreshKey:
		return "GobyRefreshOnlineMetadata"
	case SubtitleDownloadKey:
		return "DownloadSubtitles"
	case CacheMaintainKey:
		return "GobyMaintainProviderCache"
	case library.TaskIntroAnalysisKey:
		return "GobyAnalyzeIntroductions"
	case library.TaskPreviewGenerationKey:
		return "GobyGenerateSeekPreviews"
	default:
		return ""
	}
}

// Work is an immutable child snapshot. A global task has an empty LibraryID.
// Executors must honor cancellation and must not start background work after
// Execute returns. Progress contains absolute counters, not increments.
type Work struct {
	RunID                     string
	ChildID                   string
	LibraryID                 string
	TaskKey                   string
	AnalysisInput             *library.AnalysisSelection
	AnalysisScopeKey          string
	AnalysisConfigFingerprint string
	fence                     func(library.OwnedTx, Work) error
	publicationContexts       []context.Context
}

// Fence must precede business locks and be called again after all publication
// writes/events as the final database operation. Its captured capability is
// process-local; marshaling or reconstructing Work never transfers authority.
func (work Work) Fence(tx library.OwnedTx) error {
	if work.fence == nil || tx == nil {
		return ErrUnavailable
	}
	if err := work.publicationContextError(); err != nil {
		return err
	}
	if err := work.fence(tx, work); err != nil {
		return err
	}
	return work.publicationContextError()
}

// WithContext adds a publication deadline/cancellation boundary. It never
// replaces an earlier context or the original sealed manager authority. A
// background context cannot reopen cancelled work; nil closes the capability.
func (work Work) WithContext(ctx context.Context) Work {
	work.AnalysisInput = cloneAnalysisSelection(work.AnalysisInput)
	work.publicationContexts = append(append([]context.Context{}, work.publicationContexts...), ctx)
	return work
}

func (work Work) publicationContextError() error {
	for _, ctx := range work.publicationContexts {
		if ctx == nil {
			return context.Canceled
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

type Progress struct {
	Processed int64
	Added     int64
	Updated   int64
}

type Executor interface {
	Available() bool
	Execute(context.Context, Work, func(Progress) error) error
}

type ExecutorRegistration struct {
	Key               string
	Name              string
	Description       string
	Category          string
	Global            bool
	Executor          Executor
	AnalysisAdmission func(library.OwnedTx, AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error)
}

// ExecutorRegistry is frozen during construction. Registration is explicit;
// an absent or unavailable provider never becomes a successful empty task.
type ExecutorRegistry struct {
	entries map[string]ExecutorRegistration
}

func NewExecutorRegistry(entries ...ExecutorRegistration) (*ExecutorRegistry, error) {
	registry := &ExecutorRegistry{entries: make(map[string]ExecutorRegistration, len(entries))}
	for _, entry := range entries {
		if entry.Executor == nil || entry.Key == "" || len(entry.Key) > 128 || entry.Key == LibraryScanKey || entry.Key == LibraryRefreshMediaKey ||
			strings.TrimSpace(entry.Name) == "" || len(entry.Name) > 256 || len(entry.Description) > 2048 || len(entry.Category) > 128 {
			return nil, fmt.Errorf("%w: invalid executor registration", ErrInvalidInput)
		}
		if isAnalysisTask(entry.Key) && (entry.Global || entry.AnalysisAdmission == nil) || !isAnalysisTask(entry.Key) && entry.AnalysisAdmission != nil {
			return nil, fmt.Errorf("%w: analysis registration requires a scoped admission binding", ErrInvalidInput)
		}
		for _, character := range entry.Key {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' || character == '_') {
				return nil, fmt.Errorf("%w: executor keys must use lowercase identifiers", ErrInvalidInput)
			}
		}
		if _, exists := registry.entries[entry.Key]; exists {
			return nil, fmt.Errorf("%w: duplicate executor registration", ErrInvalidInput)
		}
		registry.entries[entry.Key] = entry
	}
	return registry, nil
}

func (registry *ExecutorRegistry) lookup(key string) (ExecutorRegistration, bool) {
	if registry == nil {
		return ExecutorRegistration{}, false
	}
	entry, found := registry.entries[key]
	return entry, found
}

func (s *Store) executorKeys() []string {
	keys := []string{LibraryScanKey, LibraryRefreshMediaKey}
	if s.executors != nil {
		for key := range s.executors.entries {
			keys = append(keys, key)
		}
	}
	// Keep scan keys first to preserve the original definition lock order.
	sort.Strings(keys[2:])
	return keys
}

func validProgress(value Progress) bool {
	// One processed media item can add several subtitle languages. Counters
	// have independent units and are constrained to monotonic nonnegative
	// values by the durable progress update.
	return value.Processed >= 0 && value.Added >= 0 && value.Updated >= 0
}
