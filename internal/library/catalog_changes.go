package library

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type CatalogChangeKind uint8

const (
	CatalogAdded CatalogChangeKind = iota + 1
	CatalogUpdated
	CatalogRemoved
)

// CatalogChange is a trusted fact captured inside its catalog transaction.
// Removed items retain their former library and parent rather than looking them
// up after deletion. These facts are independent of any client wire protocol.
type CatalogChange struct {
	Kind               CatalogChangeKind
	ItemID             string
	LibraryID          string
	ParentID           string
	IsFolder           bool
	IsCollectionFolder bool
}

// CatalogNotification describes one successful commit. Resync replaces an
// incomplete or excessive fact list; it never accompanies a partial Changes set.
type CatalogNotification struct {
	Changes []CatalogChange
	Resync  bool
}

const (
	maxCatalogChanges     = 1024
	maxCatalogChangeBytes = 256 * 1024
	catalogChangeOverhead = 64
)

type catalogChangeListener struct {
	notify func(CatalogNotification)
}

// SetCatalogChangeListener replaces the single process-owned listener. The
// callback must only perform bounded, nonblocking enqueue work: it must not use
// the database or network, acquire Store.mu, or wait for a consumer or Store
// method. Commits invoke it synchronously while holding the ownership mutex.
//
// Passing nil prevents future callback loads but does not wait for an already
// captured callback. A consumer must safely ignore enqueue calls after closing.
// Registrations after the store's final shutdown are ignored. This setter never
// acquires Store.mu and may be used by a callback to uninstall itself.
func (s *Store) SetCatalogChangeListener(listener func(CatalogNotification)) {
	if s == nil {
		return
	}
	if listener == nil {
		s.catalogListener.Store(nil)
		return
	}
	if s.catalogChangesClosed.Load() {
		return
	}
	s.catalogListener.Store(&catalogChangeListener{notify: listener})
	if s.catalogChangesClosed.Load() {
		s.catalogListener.Store(nil)
	}
}

// Final shutdown calls this after ownership.release has waited for every
// in-flight commit and its callback. Future owned transactions cannot begin.
func (s *Store) closeCatalogChangeListener() {
	s.catalogChangesClosed.Store(true)
	s.catalogListener.Store(nil)
}

func (s *Store) notifyCatalogChanges(notification CatalogNotification) {
	if !notification.Resync && len(notification.Changes) == 0 {
		return
	}
	listener := s.catalogListener.Load()
	if listener == nil {
		return
	}
	if !invokeCatalogChangeListener(listener.notify, notification) {
		// A listener failure cannot turn a committed write into a reported
		// database failure. Attempt one bounded invalidation, never a DB retry.
		invokeCatalogChangeListener(listener.notify, CatalogNotification{Resync: true})
	}
}

func invokeCatalogChangeListener(listener func(CatalogNotification), notification CatalogNotification) (completed bool) {
	defer func() { _ = recover() }()
	listener(notification)
	return true
}

type catalogChangeBatch struct {
	changes []CatalogChange
	bytes   int
	resync  bool
}

// recordCatalogChanges attaches explicit facts to the owned transaction without
// publishing. No SQL parsing or post-deletion lookup supplies these identities.
// Programming errors involving an unowned/finished transaction are rejected;
// invalid or excessive facts instead require resynchronization after commit.
func recordCatalogChanges(tx pgx.Tx, changes ...CatalogChange) error {
	owned, ok := tx.(*ownedTx)
	if !ok || owned == nil {
		return fmt.Errorf("%w: catalog notifications require an owned transaction", ErrInvalidInput)
	}
	if owned.finished {
		return pgx.ErrTxClosed
	}
	batch := &owned.catalogChanges
	if batch.resync || len(changes) == 0 {
		return nil
	}
	if len(changes) > maxCatalogChanges-len(batch.changes) {
		batch.requireResync()
		return nil
	}
	size := batch.bytes
	for _, change := range changes {
		if !validCatalogChange(change) {
			batch.requireResync()
			return nil
		}
		needed := catalogChangeOverhead + len(change.ItemID) + len(change.LibraryID) + len(change.ParentID)
		if needed > maxCatalogChangeBytes-size {
			batch.requireResync()
			return nil
		}
		size += needed
	}
	// Allocate only after both bounds and all identifiers are validated. Exact
	// capacity avoids retaining uncharged spare fact slots across commit.
	retained := make([]CatalogChange, len(batch.changes)+len(changes))
	copy(retained, batch.changes)
	for index, change := range changes {
		change.ItemID = strings.Clone(change.ItemID)
		change.LibraryID = strings.Clone(change.LibraryID)
		change.ParentID = strings.Clone(change.ParentID)
		retained[len(batch.changes)+index] = change
	}
	batch.changes, batch.bytes = retained, size
	return nil
}

func validCatalogChange(change CatalogChange) bool {
	return (change.Kind == CatalogAdded || change.Kind == CatalogUpdated || change.Kind == CatalogRemoved) &&
		validCatalogLibraryIdentifier(change.ItemID) && validCatalogLibraryIdentifier(change.LibraryID) &&
		(change.ParentID == "" || validCatalogLibraryIdentifier(change.ParentID)) &&
		(!change.IsCollectionFolder || change.IsFolder && change.ItemID == change.LibraryID && change.ParentID == "")
}

func (batch *catalogChangeBatch) requireResync() {
	*batch = catalogChangeBatch{resync: true}
}

// take transfers an independent snapshot to the listener and releases all
// transaction references. Subsequent transactions never reuse this slice.
func (batch *catalogChangeBatch) take() CatalogNotification {
	notification := CatalogNotification{Changes: batch.changes, Resync: batch.resync}
	*batch = catalogChangeBatch{}
	return notification
}
