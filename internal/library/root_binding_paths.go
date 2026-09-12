package library

import "os"

// A root binding changes only this registered root's approved anchor. The
// configured anchor remains shared by every root without an explicit override.
type rootBindingAnchor struct {
	root     libraryRoot
	approved *os.Root
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
	// Opens and publication leases retain independent clones while Store.mu is
	// held, so closing the retired Store handle cannot invalidate their lifetime.
	if previous.approved != nil && previous.approved != approved {
		_ = previous.approved.Close()
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
		if anchor.approved != nil {
			_ = anchor.approved.Close()
		}
	}
}
