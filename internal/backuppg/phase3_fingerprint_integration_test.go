//go:build linux

package backuppg

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"testing"
)

func TestPostgreSQLPhase3FingerprintQueryRetainsCanonicalRows(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal("begin the rollback-only fingerprint compatibility witness")
	}
	defer rollback(tx)
	if err := configureTransaction(ctx, tx, options.Schema); err != nil {
		t.Fatalf("apply the canonical fingerprint output settings: %v", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE phase3_fingerprint_rows (
		tenant text, item bigint, payload bytea, document jsonb, observed_at timestamptz,
		retired text, derived bigint GENERATED ALWAYS AS (item+1) STORED,
		UNIQUE NULLS NOT DISTINCT(tenant,item)
	) ON COMMIT DROP;
	INSERT INTO phase3_fingerprint_rows(tenant,item,payload,document,observed_at,retired) VALUES
		(NULL,2,decode('000a5cff80','hex'),'{"exact":9007199254740993,"nested":[null,true,1.25]}','2020-01-01T01:02:03.456789+02','old'),
		(NULL,10,decode('ff0080','hex'),'{"escaped":"quote\" and slash\\ and line\n"}','2020-01-02T00:00:00Z','old'),
		('alpha',NULL,NULL,'null','2020-01-03T00:00:00Z','old'),
		('alpha',9007199254740993,decode('00','hex'),'{"unicode":"\u00e9","value":9223372036854775807}','2020-01-04T00:00:00Z','old'),
		(chr(233)||'-accent',1,decode('7f','hex'),'{}','2020-01-05T00:00:00Z','old');
	ALTER TABLE phase3_fingerprint_rows DROP COLUMN retired`); err != nil {
		t.Fatalf("seed canonical rows with nullable ordering, generated values, and opaque bytes: %v", err)
	}
	// This is the published row representation and ordering, without the former
	// duplicated size predicate. Small fixtures keep the independent oracle bounded.
	rows, err := tx.Query(ctx, `SELECT pg_catalog.to_jsonb(t)::text FROM pg_temp.phase3_fingerprint_rows t
		ORDER BY t.tenant::text COLLATE "C" NULLS FIRST,t.item::text COLLATE "C" NULLS FIRST`)
	if err != nil {
		t.Fatalf("read the published canonical row representation: %v", err)
	}
	digest := sha256.New()
	var count int64
	var length [8]byte
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			rows.Close()
			t.Fatalf("read a canonical fingerprint witness row: %v", err)
		}
		binary.BigEndian.PutUint64(length[:], uint64(len(row)))
		_, _ = digest.Write(length[:])
		_, _ = io.WriteString(digest, row)
		count++
	}
	rows.Close()
	if err := rows.Err(); err != nil || count != 5 {
		t.Fatalf("the canonical fingerprint witness omitted rows: %v", err)
	}
	actual, err := fingerprints(ctx, tx, Catalog{Schema: "pg_temp", Tables: []TableSpec{{
		Name:    "phase3_fingerprint_rows",
		Columns: []string{"tenant", "item", "payload", "document", "observed_at"},
		SortKey: []string{"tenant", "item"},
	}}})
	if err != nil || len(actual) != 1 || actual[0].Rows != count || actual[0].SHA256 != hex.EncodeToString(digest.Sum(nil)) {
		t.Fatalf("single serialization changed the published row bytes, ordering, or generated-field coverage: %v", err)
	}
}
