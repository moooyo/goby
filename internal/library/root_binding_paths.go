package library

import "os"

// A root binding changes only this registered root's approved anchor. The
// configured anchor remains shared by every root without an explicit override.
type rootBindingAnchor struct {
	root     libraryRoot
	approved *os.Root
}

// Store.mu protects these counters. A borrow pins the exact published
// descriptor while its filesystem open or clone runs without Store.mu.
type rootAnchorReference struct {
	approved *os.Root
	borrowed int
	retired  bool
}

func (s *Store) borrowRootAnchorLocked(approved *os.Root) *rootAnchorReference {
	if s.rootAnchorReferences == nil {
		s.rootAnchorReferences = make(map[*os.Root]*rootAnchorReference)
	}
	reference := s.rootAnchorReferences[approved]
	if reference == nil {
		reference = &rootAnchorReference{approved: approved}
		s.rootAnchorReferences[approved] = reference
	}
	reference.borrowed++
	return reference
}

// Retirement removes admission authority immediately. The descriptor closes
// only after its last admitted open finishes; even Close itself runs unlocked.
func (s *Store) retireRootAnchorLocked(approved *os.Root) {
	if approved == nil {
		return
	}
	if s.rootAnchorReferences == nil {
		s.rootAnchorReferences = make(map[*os.Root]*rootAnchorReference)
	}
	reference := s.rootAnchorReferences[approved]
	if reference == nil {
		reference = &rootAnchorReference{approved: approved}
		s.rootAnchorReferences[approved] = reference
	}
	if reference.retired {
		return
	}
	reference.retired = true
	s.rootClosures.Add(1)
	if reference.borrowed == 0 {
		go s.closeRetiredRootAnchor(reference)
	}
}

func (s *Store) releaseRootAnchor(reference *rootAnchorReference) {
	s.mu.Lock()
	reference.borrowed--
	closeRoot := reference.retired && reference.borrowed == 0
	s.mu.Unlock()
	if closeRoot {
		s.closeRetiredRootAnchor(reference)
	}
}

func (s *Store) closeRetiredRootAnchor(reference *rootAnchorReference) {
	_ = reference.approved.Close()
	s.mu.Lock()
	if s.rootAnchorReferences[reference.approved] == reference {
		delete(s.rootAnchorReferences, reference.approved)
	}
	s.mu.Unlock()
	s.rootClosures.Done()
}

// The caller holds Store.mu across the successful binding commit and this
// installation. The exact approved anchor was cloned before the transaction;
// ownership transfers here, without reopening any filesystem name after commit.
// All validation must finish before commit, so installation cannot fail.
func (s *Store) installRootBindingAnchorLocked(root libraryRoot, approved *os.Root) {
	if s.rootBindingAnchors == nil {
		s.rootBindingAnchors = make(map[string]rootBindingAnchor)
	}
	previous := s.rootBindingAnchors[root.id]
	s.rootBindingAnchors[root.id] = rootBindingAnchor{root: root, approved: approved}
	if previous.approved != nil && previous.approved != approved {
		s.retireRootAnchorLocked(previous.approved)
	}
}

// The caller holds Store.mu after a successful library deletion or shutdown.
// An empty library ID retires every override during Store cleanup.
func (s *Store) retireRootBindingAnchorsLocked(libraryID string) {
	for rootID, anchor := range s.rootBindingAnchors {
		if libraryID != "" && anchor.root.libraryID != libraryID {
			continue
		}
		delete(s.rootBindingAnchors, rootID)
		s.retireRootAnchorLocked(anchor.approved)
	}
}
