package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	default:
		return ""
	}
}

// Work is an immutable child snapshot. A global task has an empty LibraryID.
// Executors must honor cancellation and must not start background work after
// Execute returns. Progress contains absolute counters, not increments.
type Work struct {
	RunID     string
	ChildID   string
	LibraryID string
	TaskKey   string
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
	Key         string
	Name        string
	Description string
	Category    string
	Global      bool
	Executor    Executor
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
