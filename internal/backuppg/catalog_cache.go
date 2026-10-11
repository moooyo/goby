package backuppg

import (
	"slices"
	"sync"

	"github.com/moooyo/goby/internal/backupformat"
)

type verifiedCatalog struct {
	catalog    Catalog
	migrations []backupformat.MigrationFact
}

type catalogBaselineCache struct {
	mu      sync.Mutex
	entries map[int64]verifiedCatalog
}

var embeddedCatalogs catalogBaselineCache

func (cache *catalogBaselineCache) load(version int64, schema string) (Catalog, []backupformat.MigrationFact, error) {
	if version < 1 || version > 9999 || !identifierPattern.MatchString(schema) {
		return Catalog{}, nil, ErrConfiguration
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	baseline, ok := cache.entries[version]
	if !ok {
		catalog, migrations, err := readCatalogBaseline(version)
		if err != nil {
			return Catalog{}, nil, err
		}
		// Only successful embedded versions enter the cache. The large Objects
		// document was authenticated by the loader and is not retained here.
		baseline = verifiedCatalog{catalog: catalog, migrations: migrations}
		if cache.entries == nil {
			cache.entries = make(map[int64]verifiedCatalog)
		}
		cache.entries[version] = baseline
	}
	catalog := cloneCatalog(baseline.catalog)
	catalog.Schema = schema
	return catalog, slices.Clone(baseline.migrations), nil
}

func cloneCatalog(catalog Catalog) Catalog {
	catalog.Tables = slices.Clone(catalog.Tables)
	for index := range catalog.Tables {
		table := &catalog.Tables[index]
		table.Columns = slices.Clone(table.Columns)
		table.PrimaryKey = slices.Clone(table.PrimaryKey)
		table.SortKey = slices.Clone(table.SortKey)
	}
	catalog.Sequences = slices.Clone(catalog.Sequences)
	for index := range catalog.Sequences {
		catalog.Sequences[index].Consumers = slices.Clone(catalog.Sequences[index].Consumers)
	}
	catalog.Constraints = slices.Clone(catalog.Constraints)
	return catalog
}
