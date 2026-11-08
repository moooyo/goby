package backuppg

import "github.com/moooyo/goby/internal/backupformat"

var managedArtworkTables = [...]string{"artwork_state", "artwork_images", "item_provider_images", "item_embedded_artwork"}

const emptyTableSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func managedArtworkTable(name string) bool {
	return managedArtworkIntroduced(name) != 0
}

func managedArtworkIntroduced(name string) int64 {
	switch name {
	case "item_provider_images":
		return 31
	case "artwork_state", "artwork_images":
		return 38
	case "item_embedded_artwork":
		return 45
	default:
		return 0
	}
}

func emptyManagedArtworkFact(name string) backupformat.TableFact {
	return backupformat.TableFact{Name: name, Rows: 0, SHA256: emptyTableSHA256}
}

// A custom dump may omit artwork COPY blocks only when every artwork table
// in its trusted schema prefix attests to empty data. Neither a source option
// nor a zero row count alone
// weakens the archive's complete-table grammar. Restoration still verifies
// every resulting table fingerprint, foreign key, and application invariant.
func factsOmitManagedArtwork(facts backupformat.SourceFacts) bool {
	seen := make(map[string]bool, len(managedArtworkTables))
	expected := 0
	for _, name := range managedArtworkTables {
		if facts.SchemaVersion >= managedArtworkIntroduced(name) {
			expected++
		}
	}
	if expected == 0 {
		return false
	}
	for _, table := range facts.Tables {
		if !managedArtworkTable(table.Name) {
			continue
		}
		if facts.SchemaVersion < managedArtworkIntroduced(table.Name) || seen[table.Name] || table.Rows != 0 || table.SHA256 != emptyTableSHA256 {
			return false
		}
		seen[table.Name] = true
	}
	return len(seen) == expected
}
