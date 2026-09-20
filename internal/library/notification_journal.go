package library

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/moooyo/goby/internal/notificationjournal"
)

func (tx *ownedTx) recordNotificationJournal() error {
	batch := tx.catalogChanges
	if !batch.resync && len(batch.changes) == 0 {
		return nil
	}
	refs := tx.notificationReferences
	raw, _ := json.Marshal(struct {
		Refs   []notificationjournal.Reference
		Resync bool
	}{refs, batch.resync})
	stamp := sha256.Sum256(raw)
	if tx.notificationJournalRecorded && tx.notificationJournalStamp == stamp {
		return nil
	}
	if tx.notificationMutationID == "" {
		tx.notificationMutationID = notificationjournal.NewID()
	}
	err := notificationjournal.RecordCatalog(tx.ctx, tx.Tx, tx.notificationMutationID, refs, tx.catalogChanges.resync)
	if err == nil {
		tx.notificationJournalStamp = stamp
		tx.notificationJournalRecorded = true
	}
	return err
}

func (tx *ownedTx) rememberNotificationChanges(changes []CatalogChange) {
	for _, change := range changes {
		if validCatalogChange(change) {
			tx.rememberNotificationScope(change.LibraryID, change.ItemID, change.ParentID, change.PreviousParentID)
		}
	}
}
func (tx *ownedTx) rememberNotificationScope(libraryID string, ids ...string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		ref := notificationjournal.Reference{Kind: "Item", ID: id, LibraryID: libraryID, SourceID: ids[0]}
		found := false
		for _, old := range tx.notificationReferences {
			found = found || old == ref
		}
		if !found && len(tx.notificationReferences) < 4097 {
			tx.notificationReferences = append(tx.notificationReferences, ref)
		}
	}
}
