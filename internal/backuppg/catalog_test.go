package backuppg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPublishedRecoveryCatalogsRetainHistoricalMigrationPrefixes(t *testing.T) {
	digests := make(map[string]int64)
	for _, baseline := range []struct {
		version int64
		tables  int
	}{{23, 29}, {24, 30}, {25, 30}, {26, 33}} {
		t.Run(fmt.Sprintf("schema%d", baseline.version), func(t *testing.T) {
			catalog, migrations, err := loadCatalog(baseline.version, "published_catalog_test")
			if err != nil {
				t.Fatalf("load the authenticated published schema %d catalog: %v", baseline.version, err)
			}
			if len(catalog.Tables) != baseline.tables || len(migrations) != int(baseline.version) || migrations[len(migrations)-1].Version != baseline.version {
				t.Fatalf("schema %d lost its historical table inventory or migration prefix", baseline.version)
			}
			if previous, exists := digests[catalog.SHA256]; exists {
				t.Fatalf("schema %d reused schema %d's catalog digest", baseline.version, previous)
			}
			digests[catalog.SHA256] = baseline.version
		})
	}
}

func TestCatalogCanonicalJSONPreservesEscapesAndInt64(t *testing.T) {
	original := []byte(`[{"value":{"source":"CHECK (id > 0 AND id < 9223372036854775807)","maximum":9223372036854775807},"name":"sequence"}]`)
	canonical, err := normalizeCatalogObjects(original)
	if err != nil {
		t.Fatal("normalize PostgreSQL object representation")
	}
	transport, err := json.MarshalIndent(json.RawMessage(original), "", "  ")
	if err != nil {
		t.Fatal("serialize baseline transport")
	}
	if !bytes.Contains(transport, []byte(`\u003c`)) {
		t.Fatal("fixture did not exercise HTML escaping")
	}
	recovered, err := normalizeCatalogObjects(transport)
	if err != nil || !bytes.Equal(canonical, recovered) {
		t.Fatal("JSON transport escaping changed the trusted schema digest input")
	}
	if !bytes.Contains(canonical, []byte("9223372036854775807")) || bytes.Contains(canonical, []byte("9223372036854775808")) {
		t.Fatal("schema normalization lost bigint precision")
	}
	if _, err := normalizeCatalogObjects(append(original, []byte(` {"extra":true}`)...)); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
}
