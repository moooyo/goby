package library

import (
	"context"
	"strings"
	"sync"
)

const maxAutomaticDiscoveryHints = 512

type automaticDiscoveryKey struct {
	libraryID string
	taskKey   string
}

// These bounded hints skip only repeated candidate enumeration. Every child
// still enters its owned transaction and checks its fence. Catalog commits
// invalidate them independently of task signals or external event listeners,
// including a scan that publishes some items before failing or being cancelled.
// A new Store or an evicted entry always performs discovery again.
type automaticDiscoveryHints struct {
	mu         sync.Mutex
	generation uint64
	completed  map[automaticDiscoveryKey]uint64
	order      [maxAutomaticDiscoveryHints]automaticDiscoveryKey
	next       int
}

func (hints *automaticDiscoveryHints) lookup(key automaticDiscoveryKey) (uint64, bool) {
	hints.mu.Lock()
	defer hints.mu.Unlock()
	revision, exists := hints.completed[key]
	return hints.generation, exists && revision == hints.generation
}

func (hints *automaticDiscoveryHints) remember(key automaticDiscoveryKey, generation uint64) {
	hints.mu.Lock()
	defer hints.mu.Unlock()
	// Commit releases catalog ownership before this call. A newer catalog
	// commit must not be hidden by a late publication of an older hint.
	if hints.generation != generation {
		return
	}
	if hints.completed == nil {
		hints.completed = make(map[automaticDiscoveryKey]uint64)
	}
	if _, exists := hints.completed[key]; !exists {
		delete(hints.completed, hints.order[hints.next])
		key.libraryID = strings.Clone(key.libraryID)
		hints.order[hints.next] = key
		hints.next = (hints.next + 1) % maxAutomaticDiscoveryHints
	}
	hints.completed[key] = generation
}

func (hints *automaticDiscoveryHints) invalidate() {
	hints.mu.Lock()
	defer hints.mu.Unlock()
	hints.generation++
	if hints.generation == 0 {
		clear(hints.completed)
		hints.order = [maxAutomaticDiscoveryHints]automaticDiscoveryKey{}
		hints.next = 0
	}
}

func (s *Store) prepareAutomaticSidecars(ctx context.Context, fence AnalysisFence, libraryID, taskKey, statement string) error {
	if !metadataIdentifier(libraryID) || fence == nil {
		return ErrInvalidInput
	}
	if err := analysisContext(ctx); err != nil {
		return err
	}
	key := automaticDiscoveryKey{libraryID: libraryID, taskKey: taskKey}
	var generation uint64
	prepared := false
	err := s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		var known bool
		generation, known = s.automaticDiscovery.lookup(key)
		if !known {
			if _, err := tx.Exec(statement, libraryID); err != nil {
				return err
			}
			prepared = true
		}
		if err := fence(tx); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
	if err == nil && prepared {
		s.automaticDiscovery.remember(key, generation)
	}
	return err
}
