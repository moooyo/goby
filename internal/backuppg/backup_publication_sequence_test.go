package backuppg

import (
	"fmt"
	"testing"
)

func TestBackupPublicationReceiptSequenceConsumerIsPresentInPublishedCatalogs(t *testing.T) {
	for version := int64(71); version <= currentRecoveryVersion(t); version++ {
		t.Run(fmt.Sprintf("schema%d", version), func(t *testing.T) {
			catalog, _, err := loadCatalog(version, "backup_publication_sequence_test")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, sequence := range catalog.Sequences {
				if sequence.Name != "activity_entries_id_seq" {
					continue
				}
				found = true
				parent, receipt := 0, 0
				for _, consumer := range sequence.Consumers {
					if consumer.Table == "activity_entries" && consumer.Column == "id" {
						parent++
					}
					if consumer.Table == "backup_publication_receipts" && consumer.Column == "activity_id" {
						receipt++
					}
				}
				if parent != 1 || receipt != 1 {
					t.Fatalf("activity sequence lost its display or retained-publication consumer: %#v", sequence.Consumers)
				}
			}
			if !found {
				t.Fatal("activity identity sequence was omitted")
			}
		})
	}
	for _, version := range []int64{68, 69, 70} {
		catalog, _, err := loadCatalog(version, "historical_publication_sequence_test")
		if err != nil {
			t.Fatal(err)
		}
		for _, sequence := range catalog.Sequences {
			for _, consumer := range sequence.Consumers {
				if consumer.Table == "backup_publication_receipts" {
					t.Fatalf("historical schema %d invented a future publication receipt consumer", version)
				}
			}
		}
	}
}
