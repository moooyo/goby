package library

import "sort"

const (
	// Reserve the reusable path, its visiting map, and the memo map header.
	// Per-entry charges also cover unused map capacity while buckets grow.
	scanReconciliationAncestorScratchBytes = 512 + scanReconciliationMaxDepth*128
	scanReconciliationAncestorMemoBytes    = 128
	// Include the closure map, slice growth, and the encoded text-array argument.
	scanReconciliationClosureBaseBytes = 512
	scanReconciliationClosureIDBytes   = 256
)

type scanReconciliationAncestorSuffix struct {
	length  int
	albumID string
}

type scanReconciliationAncestorStep struct {
	id      string
	albumID string
}

// The locked relation is immutable. Reuse only complete, validated suffixes;
// each origin still has its own depth limit and Audio-only album contribution.
func scanReconciliationAlbumsWithBudget(members, known map[string]scanReconciliationItem, budget *scanReconciliationBudgetState) ([]string, error) {
	ids, complete, err := scanReconciliationMemoizedAlbums(members, known, budget)
	if !complete {
		return scanReconciliationAlbums(members, known)
	}
	return ids, err
}

func scanReconciliationMemoizedAlbums(members, known map[string]scanReconciliationItem, budget *scanReconciliationBudgetState) ([]string, bool, error) {
	if budget == nil || len(members) == 0 || budget.charge(scanReconciliationAncestorScratchBytes) != nil {
		return nil, false, nil
	}
	reserved := scanReconciliationAncestorScratchBytes
	// This scratch is no longer retained when album discovery returns. It must
	// not consume the allowance for the later mandatory music/physical proofs.
	defer func() { budget.bytes -= reserved }()
	memo := make(map[string]scanReconciliationAncestorSuffix)
	visiting := make(map[string]bool, scanReconciliationMaxDepth)
	var path [scanReconciliationMaxDepth]scanReconciliationAncestorStep
	albums := make(map[string]bool)
	for _, origin := range members {
		current := origin
		length := 0
		suffix := scanReconciliationAncestorSuffix{}
		for {
			if length >= scanReconciliationMaxDepth {
				return nil, true, scanReconciliationBudget()
			}
			if completed, exists := memo[current.id]; exists {
				if completed.length > scanReconciliationMaxDepth-length {
					return nil, true, scanReconciliationBudget()
				}
				suffix = completed
				break
			}
			if visiting[current.id] {
				return nil, true, scanReconciliationUnavailable("deletion hierarchy contains a cycle")
			}
			visiting[current.id] = true
			step := scanReconciliationAncestorStep{id: current.id}
			if current.typeName == "MusicAlbum" && current.isFolder && current.ordinary {
				if _, removed := members[current.id]; !removed {
					step.albumID = current.id
				}
			}
			path[length] = step
			length++
			if current.parentID == "" {
				break
			}
			parent, exists := known[current.parentID]
			if !exists {
				return nil, true, scanReconciliationUnavailable("deletion hierarchy is incomplete")
			}
			current = parent
		}
		for index := length - 1; index >= 0; index-- {
			step := path[index]
			suffix.length++
			if step.albumID != "" {
				suffix.albumID = step.albumID
			}
			if budget.charge(scanReconciliationAncestorMemoBytes) != nil {
				// Optional caching cannot turn an otherwise valid deletion into
				// a capacity rejection. Discard it and use the original walk.
				return nil, false, nil
			}
			reserved += scanReconciliationAncestorMemoBytes
			memo[step.id] = suffix
			delete(visiting, step.id)
		}
		if origin.typeName == "Audio" && suffix.albumID != "" {
			albums[suffix.albumID] = true
		}
	}
	ids := make([]string, 0, len(albums))
	for id := range albums {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, true, nil
}

// Return the exact SQL ancestor closure for the visible removal facts. Parent
// links come from the locked relation, including for auxiliary facts whose
// notification ParentID is intentionally empty. Owner links are not ancestors.
// A nil result asks the caller to retain the ordinary recursive SQL path.
func scanReconciliationCollectionAncestors(changes []CatalogChange, known map[string]scanReconciliationItem, budget *scanReconciliationBudgetState) (ids []string, reserved int) {
	if budget == nil || len(known) == 0 || budget.charge(scanReconciliationClosureBaseBytes) != nil {
		return nil, 0
	}
	charged := scanReconciliationClosureBaseBytes
	defer func() {
		if ids == nil {
			budget.bytes -= charged
		}
	}()
	seen := make(map[string]bool)
	for _, change := range changes {
		if change.Kind != CatalogRemoved {
			continue
		}
		// Scan deletion never includes a CollectionFolder. A different caller
		// needs the ordinary query's separate library-membership branch.
		if change.IsCollectionFolder {
			return nil, 0
		}
		current, exists := known[change.ItemID]
		if !exists || current.id != change.ItemID || current.libraryID != change.LibraryID {
			return nil, 0
		}
		for {
			if seen[current.id] {
				break
			}
			// Four copies of the text length conservatively cover parameter
			// encoding and buffer growth; the map and slice share source text.
			needed := scanReconciliationClosureIDBytes + 4*len(current.id)
			if budget.charge(needed) != nil {
				return nil, 0
			}
			charged += needed
			seen[current.id] = true
			ids = append(ids, current.id)
			if current.parentID == "" {
				break
			}
			parent, exists := known[current.parentID]
			if !exists {
				return nil, 0
			}
			if parent.libraryID != current.libraryID {
				break
			}
			current = parent
		}
	}
	if len(ids) == 0 {
		return nil, 0
	}
	return ids, charged
}
