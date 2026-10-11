package backuppg

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/backupformat"
)

func TestCatalogCacheReturnsIndependentNestedValues(t *testing.T) {
	var cache catalogBaselineCache
	version := currentRecoveryVersion(t)
	want, wantMigrations, err := cache.load(version, "first_schema")
	if err != nil {
		t.Fatal(err)
	}
	changed, migrations, err := cache.load(version, "changed_schema")
	if err != nil {
		t.Fatal(err)
	}
	changed.Schema, changed.SHA256 = "changed", "changed"
	for index := range changed.Tables {
		table := &changed.Tables[index]
		table.Name = "changed"
		for _, fields := range [][]string{table.Columns, table.PrimaryKey, table.SortKey} {
			for field := range fields {
				fields[field] = "changed"
			}
		}
	}
	for index := range changed.Sequences {
		changed.Sequences[index].Name = "changed"
		for consumer := range changed.Sequences[index].Consumers {
			changed.Sequences[index].Consumers[consumer] = SequenceColumn{Table: "changed", Column: "changed"}
		}
	}
	for index := range changed.Constraints {
		changed.Constraints[index] = ConstraintSpec{Table: "changed", Name: "changed", Definition: "changed"}
	}
	for index := range migrations {
		migrations[index] = backupformat.MigrationFact{Version: -1, Name: "changed", SHA256: "changed"}
	}
	actual, actualMigrations, err := cache.load(version, "second_schema")
	want.Schema = "second_schema"
	if err != nil || !reflect.DeepEqual(actual, want) || !reflect.DeepEqual(actualMigrations, wantMigrations) {
		t.Fatalf("caller mutation changed the cached catalog or migration facts: %v", err)
	}
}

func TestCatalogClonePreservesNilAndEmptySlices(t *testing.T) {
	for _, catalog := range []Catalog{
		{},
		{Tables: []TableSpec{}, Sequences: []SequenceSpec{}, Constraints: []ConstraintSpec{}},
		{Tables: []TableSpec{{}, {Columns: []string{}, PrimaryKey: []string{}, SortKey: []string{}}},
			Sequences: []SequenceSpec{{}, {Consumers: []SequenceColumn{}}}},
	} {
		if cloned := cloneCatalog(catalog); !reflect.DeepEqual(cloned, catalog) {
			t.Fatal("catalog cloning changed nil or empty slice distinctions")
		}
	}
}

func TestCatalogCacheConcurrentLoadsKeepCallerSchemas(t *testing.T) {
	var cache catalogBaselineCache
	version := currentRecoveryVersion(t)
	want, migrations, err := readCatalogBaseline(version)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for index := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			schema := fmt.Sprintf("caller_%d", index)
			catalog, facts, err := cache.load(version, schema)
			if err != nil || catalog.Schema != schema {
				t.Errorf("concurrent load lost its caller schema: %v", err)
				return
			}
			catalog.Schema = ""
			if !reflect.DeepEqual(catalog, want) || !reflect.DeepEqual(facts, migrations) {
				t.Error("concurrent load returned an incomplete baseline")
			}
		}()
	}
	workers.Wait()
	if len(cache.entries) != 1 {
		t.Fatal("concurrent loads retained duplicate baseline entries")
	}
}

func TestCatalogCacheValidatesInputsBeforeWarmHits(t *testing.T) {
	var cache catalogBaselineCache
	version := currentRecoveryVersion(t)
	if _, _, err := cache.load(version, "public"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		version int64
		schema  string
		want    error
	}{
		{version, "", ErrConfiguration},
		{version, "public; SELECT 1", ErrConfiguration},
		{0, "public", ErrConfiguration},
		{10000, "public", ErrConfiguration},
		{30, "public", ErrUnsupported},
		{9999, "public", ErrUnsupported},
	} {
		if _, _, err := cache.load(test.version, test.schema); !errors.Is(err, test.want) {
			t.Fatalf("catalog input %d/%q returned %v, want %v", test.version, test.schema, err, test.want)
		}
	}
	if len(cache.entries) != 1 {
		t.Fatal("invalid or absent embedded versions entered the cache")
	}
}

func TestCatalogBaselineDecoderRetainsFirstLoadProofs(t *testing.T) {
	version := currentRecoveryVersion(t)
	data, err := catalogFiles.ReadFile(fmt.Sprintf("catalogs/schema-%d-postgresql-17.json", version))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*catalogBaseline)
	}{
		{"version", func(value *catalogBaseline) { value.Version++ }},
		{"postgresql", func(value *catalogBaseline) { value.PostgreSQLMajor = 18 }},
		{"migrations", func(value *catalogBaseline) { value.Migrations[0].SHA256 = "changed" }},
		{"checksum", func(value *catalogBaseline) { value.Catalog.SHA256 = "changed" }},
		{"tables", func(value *catalogBaseline) { value.Catalog.Tables = nil }},
		{"objects", func(value *catalogBaseline) { value.Objects = json.RawMessage(`null`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var baseline catalogBaseline
			if err := json.Unmarshal(data, &baseline); err != nil {
				t.Fatal(err)
			}
			test.change(&baseline)
			changed, err := json.Marshal(baseline)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := decodeCatalogBaseline(version, changed); !errors.Is(err, ErrSchema) {
				t.Fatalf("invalid baseline proof was accepted: %v", err)
			}
		})
	}
	for _, malformed := range [][]byte{[]byte(`{"unknown":1}`), append(append([]byte(nil), data...), []byte(` {}`)...)} {
		if _, _, err := decodeCatalogBaseline(version, malformed); !errors.Is(err, ErrSchema) {
			t.Fatalf("noncanonical baseline envelope was accepted: %v", err)
		}
	}
}
