package database

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func publishedMigrationTestFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	result := make(fstest.MapFS, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := "migrations/" + entry.Name()
		data, err := migrationFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		result[name] = &fstest.MapFile{Data: data, Mode: 0600}
	}
	return result
}

func TestPublishedMigrationManifestMatchesEmbeddedRelease(t *testing.T) {
	available, err := migrations()
	if err != nil {
		t.Fatalf("embedded SQL must match the independently pinned publication: %v", err)
	}
	if len(available) == 0 || len(available) != len(publishedMigrations) {
		t.Fatal("published migration inventory is empty or incomplete")
	}
	facts, err := EmbeddedMigrations()
	if err != nil || len(facts) != len(publishedMigrations) {
		t.Fatalf("recovery migration facts must use the same validated inventory: %v", err)
	}
	for index, expected := range publishedMigrations {
		if facts[index].Version != expected.version || facts[index].Name != expected.name || facts[index].SHA256 != expected.sha256 {
			t.Fatalf("recovery facts changed the pinned migration at version %d", expected.version)
		}
	}
}

func TestPublishedMigrationManifestRejectsSameNameSQLDrift(t *testing.T) {
	for _, name := range []string{"0001_identity.sql", "0014_item_metadata.sql", "0028_storage_root_bindings.sql"} {
		t.Run(name, func(t *testing.T) {
			source := publishedMigrationTestFiles(t)
			path := "migrations/" + name
			source[path].Data = append(source[path].Data, []byte("\nSELECT 1;\n")...)
			available, err := readMigrations(source)
			if !errors.Is(err, ErrMigrationIntegrity) || available != nil {
				t.Fatalf("same-name SQL drift must fail before an inventory can be executed: %v", err)
			}
		})
	}
	for _, change := range []string{"empty SQL", "line ending drift"} {
		t.Run(change, func(t *testing.T) {
			source := publishedMigrationTestFiles(t)
			entry := source["migrations/0001_identity.sql"]
			if change == "empty SQL" {
				entry.Data = nil
			} else {
				entry.Data = bytes.ReplaceAll(entry.Data, []byte("\n"), []byte("\r\n"))
			}
			if _, err := readMigrations(source); !errors.Is(err, ErrMigrationIntegrity) {
				t.Fatalf("published migration bytes must be exact: %v", err)
			}
		})
	}
}

func TestPublishedMigrationManifestRejectsChangedInventory(t *testing.T) {
	for _, change := range []string{"missing first", "missing middle", "missing last", "renamed SQL", "unpublished addition", "duplicate version", "invalid filename"} {
		t.Run(change, func(t *testing.T) {
			source := publishedMigrationTestFiles(t)
			switch change {
			case "missing first":
				delete(source, "migrations/0001_identity.sql")
			case "missing middle":
				delete(source, "migrations/0014_item_metadata.sql")
			case "missing last":
				delete(source, "migrations/0028_storage_root_bindings.sql")
			case "renamed SQL":
				source["migrations/0001_renamed.sql"] = source["migrations/0001_identity.sql"]
				delete(source, "migrations/0001_identity.sql")
			case "unpublished addition":
				source["migrations/0029_unpublished.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
			case "duplicate version":
				source["migrations/0001_duplicate.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
			case "invalid filename":
				source["migrations/not_a_version.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
			}
			if available, err := readMigrations(source); err == nil || available != nil {
				t.Fatal("a changed SQL inventory was accepted as the published release")
			}
		})
	}
}

func TestPublishedMigrationManifestRejectsUnreadableInventory(t *testing.T) {
	if _, err := readMigrations(fstest.MapFS{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing migration directory must retain its read failure: %v", err)
	}
}
