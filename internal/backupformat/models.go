// Package backupformat reads and writes the encrypted Goby backup format.
// It does not open filesystem paths, execute database commands, or activate a
// restored database. Callers must keep all output in private staging until the
// entire operation succeeds and must discard it after any error.
package backupformat

import (
	"errors"
	"io"
	"time"
)

const (
	FormatVersion      = "goby.backup.v1"
	ManifestName       = "manifest.json"
	DatabaseName       = "database.dump"
	ConfigurationName  = "configuration.json"
	MasterKeyName      = "master.key"
	ScryptWorkFactor   = 18
	MaxPassphraseBytes = 1024
)

var (
	ErrInvalidInput   = errors.New("invalid backup format input")
	ErrInvalidArchive = errors.New("invalid or unauthenticated backup archive")
	ErrLimit          = errors.New("backup format limit exceeded")
	ErrSourceChanged  = errors.New("backup source differs from its descriptor")
	ErrSource         = errors.New("backup source could not be read")
	ErrSink           = errors.New("backup destination could not be written")
)

// Manifest is canonical JSON in the first tar member. SHA-256 binds member
// contents to this manifest; it is not an independent statement of provenance.
// A party that knows the passphrase can create a different valid archive.
type Manifest struct {
	Format      string      `json:"format"`
	ID          string      `json:"id"`
	CreatedAt   time.Time   `json:"created_at"`
	GobyVersion string      `json:"goby_version"`
	Source      SourceFacts `json:"source"`
	Files       []File      `json:"files"`
}

// SourceFacts describes the source snapshot without connection credentials.
// The restore engine must independently validate the database schema, table
// inventory, fingerprints, and compatibility. This package checks structure
// only and never treats a decrypted PostgreSQL dump as safe to execute.
type SourceFacts struct {
	SchemaVersion        int64           `json:"schema_version"`
	SchemaSHA256         string          `json:"schema_sha256"`
	MigrationChecksums   []MigrationFact `json:"migration_checksums"`
	ProbeVersion         int64           `json:"probe_version"`
	PostgreSQLVersion    string          `json:"postgresql_version"`
	PostgreSQLVersionNum int             `json:"postgresql_version_num"`
	DatabaseSchema       string          `json:"database_schema"`
	ServerID             string          `json:"server_id"`
	Tables               []TableFact     `json:"tables"`
}

// MigrationFact identifies one embedded migration source. Its checksum is
// not a claim that older database versions persisted migration checksums.
type MigrationFact struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
	SHA256  string `json:"sha256"`
}

// TableFact is one entry in a strictly name-sorted table inventory. SHA256 is
// a snapshot fingerprint whose serialization rules belong to the engine.
type TableFact struct {
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}

// File describes a member. The list must contain database.dump and then
// configuration.json, optionally followed by the exactly 32-byte master.key.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Entries are caller-owned sources positioned at their first byte. Create
// checks each descriptor again while streaming and checks EOF after its size.
// Sources and destinations are never closed by this package.
type Entries struct {
	Database      io.Reader
	Configuration io.Reader
	MasterKey     io.Reader
}

// Sinks are fixed-purpose, caller-owned staging destinations. A nil sink
// discards that member while still verifying its entire contents. No source
// name is ever interpreted as a filesystem path. Written bytes remain
// untrusted until Extract returns successfully.
type Sinks struct {
	Database      io.Writer
	Configuration io.Writer
	MasterKey     io.Writer
}

// Limits bounds both encoded input and decoded contents. Zero fields use the
// defaults. Negative values and values above the hard ceilings are invalid.
// Limits never change the production scrypt work factor or its reading cap.
type Limits struct {
	MaxManifestBytes      int64
	MaxDatabaseBytes      int64
	MaxConfigurationBytes int64
	MaxPlaintextBytes     int64
	MaxEncryptedBytes     int64
	MaxTables             int
}

// DefaultLimits is a value, not shared mutable configuration.
func DefaultLimits() Limits {
	return Limits{
		MaxManifestBytes:      256 << 10,
		MaxDatabaseBytes:      16 << 30,
		MaxConfigurationBytes: 1 << 20,
		MaxPlaintextBytes:     17 << 30,
		MaxEncryptedBytes:     18 << 30,
		MaxTables:             512,
	}
}
