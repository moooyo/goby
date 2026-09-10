package backuppg

import (
	"bytes"
	"encoding/json"
	"testing"
)

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
