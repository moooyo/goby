package backupformat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestManifestStructureLimitsAndNoNumericOverflow(t *testing.T) {
	cases := map[string]func(*Manifest){
		"unknown format":               func(m *Manifest) { m.Format = "goby.backup.v2" },
		"uppercase archive ID":         func(m *Manifest) { m.ID = strings.ToUpper(m.ID) },
		"short archive ID":             func(m *Manifest) { m.ID = "0123" },
		"missing date":                 func(m *Manifest) { m.CreatedAt = time.Time{} },
		"non UTC date":                 func(m *Manifest) { m.CreatedAt = m.CreatedAt.In(time.FixedZone("offset", 3600)) },
		"invalid version text":         func(m *Manifest) { m.GobyVersion = "version\nsecret" },
		"invalid source ID":            func(m *Manifest) { m.Source.ServerID = "server\x00id" },
		"invalid source UTF8":          func(m *Manifest) { m.Source.ServerID = string([]byte{0xff}) },
		"missing schema version":       func(m *Manifest) { m.Source.SchemaVersion = 0 },
		"negative probe":               func(m *Manifest) { m.Source.ProbeVersion = -1 },
		"version overflow":             func(m *Manifest) { m.Source.SchemaVersion = math.MaxInt64 },
		"invalid schema hash":          func(m *Manifest) { m.Source.SchemaSHA256 = strings.Repeat("X", 64) },
		"missing migration checksums":  func(m *Manifest) { m.Source.MigrationChecksums = nil },
		"missing migration":            func(m *Manifest) { m.Source.MigrationChecksums = m.Source.MigrationChecksums[1:] },
		"duplicate migration":          func(m *Manifest) { m.Source.MigrationChecksums[1] = m.Source.MigrationChecksums[0] },
		"migration version gap":        func(m *Manifest) { m.Source.MigrationChecksums[0].Version = 2 },
		"migration filename version":   func(m *Manifest) { m.Source.MigrationChecksums[0].Name = "0002_initial.sql" },
		"migration filename traversal": func(m *Manifest) { m.Source.MigrationChecksums[0].Name = "0001_../initial.sql" },
		"migration filename padding":   func(m *Manifest) { m.Source.MigrationChecksums[0].Name = "001_initial.sql" },
		"migration filename character": func(m *Manifest) { m.Source.MigrationChecksums[0].Name = "0001_Initial.sql" },
		"migration checksum":           func(m *Manifest) { m.Source.MigrationChecksums[0].SHA256 = strings.Repeat("0", 63) },
		"schema identifier":            func(m *Manifest) { m.Source.DatabaseSchema = "public; drop database" },
		"table identifier":             func(m *Manifest) { m.Source.Tables[0].Name = "public.users" },
		"duplicate table":              func(m *Manifest) { m.Source.Tables = append(m.Source.Tables, m.Source.Tables[0]) },
		"unsorted tables": func(m *Manifest) {
			m.Source.Tables = append(m.Source.Tables, TableFact{Name: "activity_entries", SHA256: strings.Repeat("0", 64)})
		},
		"negative table count":  func(m *Manifest) { m.Source.Tables[0].Rows = -1 },
		"empty table inventory": func(m *Manifest) { m.Source.Tables = nil },
		"invalid fingerprint":   func(m *Manifest) { m.Source.Tables[0].SHA256 = strings.Repeat("g", 64) },
		"missing dump":          func(m *Manifest) { m.Files = m.Files[1:] },
		"reordered members":     func(m *Manifest) { m.Files[0], m.Files[1] = m.Files[1], m.Files[0] },
		"file path":             func(m *Manifest) { m.Files[0].Name = "../database.dump" },
		"negative size":         func(m *Manifest) { m.Files[0].Size = -1 },
		"empty configuration":   func(m *Manifest) { m.Files[1].Size = 0 },
		"invalid checksum":      func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("A", 64) },
		"short master":          func(m *Manifest) { m.Files[2].Size = 31 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest, members := fixture(true)
			mutate(&manifest)
			if err := ValidateManifest(manifest, Limits{}); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid source manifest: %v", err)
			}
			// The public reader applies the same structural requirements to a
			// real authenticated archive built independently of Create.
			manifestData, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if name == "invalid source UTF8" {
				// Marshal repairs invalid string bytes. Reinsert the original
				// invalid bytes into the authenticated JSON to test the reader,
				// while preserving the separate writer-input assertion above.
				encodedID, err := json.Marshal(manifest.Source.ServerID)
				if err != nil {
					t.Fatal(err)
				}
				field := []byte(`"server_id":`)
				before := append(append([]byte{}, field...), encodedID...)
				after := append(append(append([]byte{}, field...), '"'), []byte(manifest.Source.ServerID)...)
				after = append(after, '"')
				manifestData = bytes.Replace(manifestData, before, after, 1)
				if utf8.Valid(manifestData) {
					t.Fatal("invalid UTF-8 archive fixture was repaired before import")
				}
			}
			archive := encryptFixture(t, plainWithManifest(t, manifestData, manifest, members))
			if _, err := Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{}); !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("invalid imported manifest: %v", err)
			}
		})
	}
	for _, size := range []int64{math.MaxInt64, math.MaxInt64 - 511, 1 << 40} {
		manifest, _ := fixture(false)
		manifest.Files[0].Size = size
		if err := ValidateManifest(manifest, Limits{}); !errors.Is(err, ErrLimit) {
			t.Fatalf("unbounded size %d: %v", size, err)
		}
	}
	manifest, _ := fixture(false)
	manifest.Source.Tables[0].Rows = math.MaxInt64
	if err := ValidateManifest(manifest, Limits{}); err != nil {
		t.Fatalf("exact int64 count was unnecessarily rounded or rejected: %v", err)
	}
	if err := ValidateManifest(manifest, Limits{MaxPlaintextBytes: 1024}); !errors.Is(err, ErrLimit) {
		t.Fatalf("total archive bytes were not bounded: %v", err)
	}
	manifest.Files[0].Size = 1 << 40
	if err := ValidateManifest(manifest, Limits{MaxDatabaseBytes: 1 << 40, MaxPlaintextBytes: 2 << 40, MaxEncryptedBytes: 2 << 40}); err != nil {
		t.Fatalf("large GNU-compatible descriptor rejected: %v", err)
	}
}

func TestManifestRequiresCanonicalUnambiguousJSON(t *testing.T) {
	manifest, members := fixture(false)
	canonical, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(canonical)
	cases := map[string]string{
		"unknown member":      strings.Replace(text, `"format":`, `"unexpected":true,"format":`, 1),
		"duplicate member":    strings.Replace(text, `"format":`, `"format":"goby.backup.v1","format":`, 1),
		"duplicate nested":    strings.Replace(text, `"schema_version":22`, `"schema_version":22,"schema_version":22`, 1),
		"trailing whitespace": text + "\n",
		"prefix whitespace":   " " + text,
		"alternate escaping":  strings.Replace(text, `goby.backup.v1`, `goby\u002ebackup.v1`, 1),
		"second JSON value":   text + `{}`,
		"exponent integer":    strings.Replace(text, `"schema_version":22`, `"schema_version":2.2e1`, 1),
		"integer overflow":    strings.Replace(text, `"rows":2`, `"rows":9223372036854775808`, 1),
		"explicit plus UTC":   strings.Replace(text, `12:00:00Z`, `12:00:00+00:00`, 1),
		"timestamp precision": strings.Replace(text, `12:00:00Z`, `12:00:00.000Z`, 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			archive := encryptFixture(t, plainWithManifest(t, []byte(data), manifest, members))
			if _, err := Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{}); !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("ambiguous manifest accepted: %v", err)
			}
		})
	}
}

func TestInvalidInputsFailBeforeReadingOrWriting(t *testing.T) {
	manifest, members := fixture(false)
	for _, password := range [][]byte{nil, {}, {0xff}, bytes.Repeat([]byte("x"), MaxPassphraseBytes+1)} {
		reader := &countingReader{source: bytes.NewReader([]byte("must remain unread"))}
		var destination bytes.Buffer
		if err := Create(context.Background(), &destination, password, manifest, entryReaders(members)); !errors.Is(err, ErrInvalidInput) || destination.Len() != 0 {
			t.Fatalf("invalid creation passphrase: %v", err)
		}
		if _, err := Inspect(context.Background(), reader, password, Limits{}); !errors.Is(err, ErrInvalidInput) || reader.total != 0 {
			t.Fatalf("invalid import passphrase: %v", err)
		}
	}
	for _, limits := range []Limits{
		{MaxManifestBytes: -1}, {MaxManifestBytes: hardManifestBytes + 1},
		{MaxDatabaseBytes: math.MaxInt64}, {MaxPlaintextBytes: math.MaxInt64},
		{MaxEncryptedBytes: -1}, {MaxTables: hardTables + 1},
	} {
		reader := &countingReader{source: bytes.NewReader([]byte("must remain unread"))}
		if _, err := Inspect(context.Background(), reader, syntheticPassphrase, limits); !errors.Is(err, ErrInvalidInput) || reader.total != 0 {
			t.Fatalf("invalid import bounds: %v", err)
		}
	}
	if err := Create(context.Background(), new(bytes.Buffer), syntheticPassphrase, manifest, Entries{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing sources accepted")
	}
	entries := entryReaders(members)
	entries.MasterKey = bytes.NewReader(make([]byte, 32))
	if err := Create(context.Background(), new(bytes.Buffer), syntheticPassphrase, manifest, entries); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unlisted master key source accepted")
	}
	manifest, members = fixture(true)
	entries = entryReaders(members)
	entries.MasterKey = nil
	if err := Create(context.Background(), new(bytes.Buffer), syntheticPassphrase, manifest, entries); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing listed master key source accepted")
	}
}
