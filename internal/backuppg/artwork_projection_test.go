package backuppg

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/backupformat"
)

func TestManagedArtworkOmissionRequiresAllExactEmptyFacts(t *testing.T) {
	valid := backupformat.SourceFacts{SchemaVersion: 45, Tables: []backupformat.TableFact{
		emptyManagedArtworkFact("artwork_state"), emptyManagedArtworkFact("artwork_images"),
		emptyManagedArtworkFact("item_provider_images"), emptyManagedArtworkFact("item_embedded_artwork"),
		{Name: "users", Rows: 1, SHA256: strings.Repeat("a", 64)},
	}}
	if !factsOmitManagedArtwork(valid) {
		t.Fatal("an exact empty artwork projection was not recognized")
	}
	for _, test := range []struct {
		name   string
		change func(*backupformat.SourceFacts)
	}{
		{"missing_set", func(f *backupformat.SourceFacts) { f.Tables = f.Tables[1:] }},
		{"missing_payload", func(f *backupformat.SourceFacts) { f.Tables = append(f.Tables[:1], f.Tables[2:]...) }},
		{"duplicate", func(f *backupformat.SourceFacts) { f.Tables = append(f.Tables, f.Tables[0]) }},
		{"nonempty_set", func(f *backupformat.SourceFacts) { f.Tables[0].Rows = 1 }},
		{"nonempty_payload", func(f *backupformat.SourceFacts) { f.Tables[1].Rows = 1 }},
		{"invalid_set_hash", func(f *backupformat.SourceFacts) { f.Tables[0].SHA256 = strings.Repeat("0", 64) }},
		{"invalid_payload_hash", func(f *backupformat.SourceFacts) { f.Tables[1].SHA256 = "" }},
		{"missing_provider", func(f *backupformat.SourceFacts) { f.Tables = append(f.Tables[:2], f.Tables[3:]...) }},
		{"missing_embedded", func(f *backupformat.SourceFacts) { f.Tables = append(f.Tables[:3], f.Tables[4:]...) }},
		{"nonempty_provider", func(f *backupformat.SourceFacts) { f.Tables[2].Rows = 1 }},
		{"nonempty_embedded", func(f *backupformat.SourceFacts) { f.Tables[3].Rows = 1 }},
		{"invalid_provider_hash", func(f *backupformat.SourceFacts) { f.Tables[2].SHA256 = "" }},
		{"invalid_embedded_hash", func(f *backupformat.SourceFacts) { f.Tables[3].SHA256 = strings.Repeat("0", 64) }},
		{"future_table", func(f *backupformat.SourceFacts) { f.SchemaVersion = 44 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := cloneFacts(valid)
			test.change(&facts)
			if factsOmitManagedArtwork(facts) {
				t.Fatal("incomplete or nonempty facts weakened the complete COPY grammar")
			}
		})
	}
}

func TestManagedArtworkOmissionPreservesTrustedSchemaBoundaries(t *testing.T) {
	for _, test := range []struct {
		version int64
		tables  []string
		want    bool
	}{
		{30, nil, false},
		{31, []string{"item_provider_images"}, true},
		{37, []string{"item_provider_images"}, true},
		{38, []string{"artwork_state", "artwork_images", "item_provider_images"}, true},
		{44, []string{"artwork_state", "artwork_images", "item_provider_images"}, true},
		{45, []string{"artwork_state", "artwork_images", "item_provider_images", "item_embedded_artwork"}, true},
		{45, []string{"artwork_state", "artwork_images", "item_provider_images"}, false},
		{38, []string{"artwork_state", "artwork_images"}, false},
	} {
		facts := backupformat.SourceFacts{SchemaVersion: test.version}
		for _, name := range test.tables {
			facts.Tables = append(facts.Tables, emptyManagedArtworkFact(name))
		}
		if got := factsOmitManagedArtwork(facts); got != test.want {
			t.Fatalf("schema %d omission=%v, want %v", test.version, got, test.want)
		}
	}
}

func artworkProjectionDecodeCatalog() Catalog {
	catalog := decodeTestCatalog()
	catalog.Tables = append(catalog.Tables,
		TableSpec{Name: "artwork_state", Columns: []string{"id"}, PrimaryKey: []string{"id"}},
		TableSpec{Name: "artwork_images", Columns: []string{"state_id", "content"}, PrimaryKey: []string{"state_id"}},
		TableSpec{Name: "item_provider_images", Columns: []string{"item_id", "content"}, PrimaryKey: []string{"item_id"}},
		TableSpec{Name: "item_embedded_artwork", Columns: []string{"item_id", "content"}, PrimaryKey: []string{"item_id"}},
	)
	return catalog
}

func TestDecodeManagedArtworkProjectionPreservesOtherRowsAndSequences(t *testing.T) {
	sink := &decodeTestSink{}
	if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), artworkProjectionDecodeCatalog(), sink,
		DecodeOptions{AllowEmptyManagedArtwork: true}); err != nil {
		t.Fatal(err)
	}
	for _, table := range managedArtworkTables {
		data, exists := sink.data[table]
		if !exists || len(data) != 0 {
			t.Fatal("an omitted artwork table was not delivered to the sink as exact empty data")
		}
	}
	if len(sink.data["users"]) == 0 || len(sink.data) != 6 || sink.sequences["users_id_seq"] != (decodeTestSequenceValue{3, true}) {
		t.Fatal("artwork omission lost ordinary data or sequence state")
	}
}

func TestDecodeManagedArtworkProjectionKeepsCompleteGrammar(t *testing.T) {
	for _, test := range []struct {
		name    string
		archive string
		options DecodeOptions
	}{
		{"disabled", decodeTestArchive(), DecodeOptions{}},
		{"ordinary_empty_table", strings.Replace(decodeTestArchive(), decodeTestEmpty, "", 1), DecodeOptions{AllowEmptyManagedArtwork: true}},
		{"ordinary_populated_table", strings.Replace(decodeTestArchive(), decodeTestUsers, "", 1), DecodeOptions{AllowEmptyManagedArtwork: true}},
		{"sequence", strings.Replace(decodeTestArchive(), decodeTestSequence, "", 1), DecodeOptions{AllowEmptyManagedArtwork: true}},
		{"trailer", strings.Replace(decodeTestArchive(), decodeTestFooter, "", 1), DecodeOptions{AllowEmptyManagedArtwork: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := Decode(t.Context(), strings.NewReader(test.archive), artworkProjectionDecodeCatalog(), &decodeTestSink{}, test.options); !errors.Is(err, ErrArchive) {
				t.Fatalf("an unrelated missing archive entry was accepted: %v", err)
			}
		})
	}
}

func TestDecodeManagedArtworkProjectionChecksEmptySinkAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		rows int64
		err  error
		want error
	}{
		{"unexpected_rows", 1, nil, ErrArchive},
		{"sink_failure", 0, io.ErrClosedPipe, io.ErrClosedPipe},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &decodeTestSink{copyFn: func(ctx context.Context, table TableSpec, data io.Reader) (int64, error) {
				if managedArtworkTable(table.Name) {
					return test.rows, test.err
				}
				content, err := io.ReadAll(data)
				return int64(strings.Count(string(content), "\n")), err
			}}
			if err := Decode(t.Context(), strings.NewReader(decodeTestArchive()), artworkProjectionDecodeCatalog(), sink,
				DecodeOptions{AllowEmptyManagedArtwork: true}); !errors.Is(err, test.want) {
				t.Fatalf("omitted data bypassed sink failure: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &decodeTestSink{copyFn: func(ctx context.Context, table TableSpec, data io.Reader) (int64, error) {
		if managedArtworkTable(table.Name) {
			cancel()
			return 0, nil
		}
		content, err := io.ReadAll(data)
		return int64(strings.Count(string(content), "\n")), err
	}}
	if err := Decode(ctx, strings.NewReader(decodeTestArchive()), artworkProjectionDecodeCatalog(), sink,
		DecodeOptions{AllowEmptyManagedArtwork: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("omitted data ignored cancellation: %v", err)
	}
}

func TestExportManagedArtworkProjectionDoesNotReadOmittedPayloads(t *testing.T) {
	catalog := Catalog{Tables: []TableSpec{{Name: "artwork_state"}, {Name: "artwork_images"}, {Name: "item_provider_images"}, {Name: "item_embedded_artwork"}}}
	facts, err := exportFingerprints(t.Context(), nil, catalog, true)
	if err != nil || len(facts) != 4 {
		t.Fatalf("omitted image payloads were read or received incorrect export facts: %v", err)
	}
	for index, name := range managedArtworkTables {
		if facts[index] != emptyManagedArtworkFact(name) {
			t.Fatalf("omitted table %s retained payload fingerprints", name)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := exportFingerprints(ctx, nil, catalog, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("artwork projection ignored snapshot cancellation: %v", err)
	}
}
