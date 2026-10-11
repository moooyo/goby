package library

import "github.com/moooyo/goby/internal/notificationjournal"

// Retain one extra reference so RecordCatalog preserves its overflow behavior.
const notificationReferenceOverflow = 4097

func (tx *ownedTx) recordNotificationJournal() error {
	batch := tx.catalogChanges
	if !batch.resync && len(batch.changes) == 0 {
		return nil
	}
	refs := tx.notificationReferences
	// References only append, so an unchanged length preserves the full prefix.
	if tx.notificationJournalRecorded && tx.notificationJournalLength == len(refs) && tx.notificationJournalResync == batch.resync {
		return nil
	}
	if tx.notificationMutationID == "" {
		tx.notificationMutationID = notificationjournal.NewID()
	}
	err := notificationjournal.RecordCatalog(tx.ctx, tx.Tx, tx.notificationMutationID, refs, batch.resync)
	if err == nil {
		tx.notificationJournalLength = len(refs)
		tx.notificationJournalResync = batch.resync
		tx.notificationJournalRecorded = true
	}
	return err
}

func (tx *ownedTx) rememberNotificationChanges(changes []CatalogChange) {
	for _, change := range changes {
		if len(tx.notificationReferences) >= notificationReferenceOverflow {
			return
		}
		if validCatalogChange(change) {
			tx.rememberNotificationScope(change.LibraryID, change.ItemID, change.ParentID, change.PreviousParentID)
		}
	}
}
func (tx *ownedTx) rememberNotificationScope(libraryID string, ids ...string) {
	for _, id := range ids {
		if len(tx.notificationReferences) >= notificationReferenceOverflow {
			return
		}
		if id == "" {
			continue
		}
		ref := notificationjournal.Reference{Kind: "Item", ID: id, LibraryID: libraryID, SourceID: ids[0]}
		tx.rememberNotificationReferences(ref)
	}
}

// All append paths share this transaction-private index. The slice retains
// first-insertion order, and source-scoped references remain distinct.
func (tx *ownedTx) rememberNotificationReferences(refs ...notificationjournal.Reference) {
	for _, ref := range refs {
		if len(tx.notificationReferences) >= notificationReferenceOverflow {
			return
		}
		if _, found := tx.notificationReferenceSet[ref]; found {
			continue
		}
		if tx.notificationReferenceSet == nil {
			tx.notificationReferenceSet = make(map[notificationjournal.Reference]struct{})
		}
		tx.notificationReferenceSet[ref] = struct{}{}
		tx.notificationReferences = append(tx.notificationReferences, ref)
	}
}
