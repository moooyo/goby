package library

import (
	"fmt"
	"testing"
)

// This benchmark uses only the pre-existing collector entry point, so the same
// fixture can measure baseline and candidate source snapshots.
func BenchmarkNotificationReferenceCollection(b *testing.B) {
	for _, count := range []int{1024, 32768} {
		changes := make([]CatalogChange, count)
		for index := range changes {
			changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("item-%05d", index), LibraryID: "library", ParentID: "shared-parent"}
		}
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				tx := new(ownedTx)
				tx.rememberNotificationChanges(changes)
				if len(tx.notificationReferences) != min(2*count, 4097) {
					b.Fatal("reference collection lost its bounded scope")
				}
			}
		})
	}
}
