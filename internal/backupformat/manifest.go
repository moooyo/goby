package backupformat

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	hardManifestBytes      = 1 << 20
	hardDatabaseBytes      = 1 << 40
	hardConfigurationBytes = 16 << 20
	hardStreamBytes        = 2 << 40
	hardTables             = 4096
	maxHeaderBytes         = 16 << 10
	tarBlockBytes          = 512
)

func normalizeLimits(limits Limits) (Limits, error) {
	defaults := DefaultLimits()
	values := []struct {
		value    *int64
		fallback int64
		maximum  int64
	}{
		{&limits.MaxManifestBytes, defaults.MaxManifestBytes, hardManifestBytes},
		{&limits.MaxDatabaseBytes, defaults.MaxDatabaseBytes, hardDatabaseBytes},
		{&limits.MaxConfigurationBytes, defaults.MaxConfigurationBytes, hardConfigurationBytes},
		{&limits.MaxPlaintextBytes, defaults.MaxPlaintextBytes, hardStreamBytes},
		{&limits.MaxEncryptedBytes, defaults.MaxEncryptedBytes, hardStreamBytes},
	}
	for _, setting := range values {
		if *setting.value == 0 {
			*setting.value = setting.fallback
		}
		if *setting.value < 1 || *setting.value > setting.maximum {
			return Limits{}, ErrInvalidInput
		}
	}
	if limits.MaxTables == 0 {
		limits.MaxTables = defaults.MaxTables
	}
	if limits.MaxTables < 1 || limits.MaxTables > hardTables {
		return Limits{}, ErrInvalidInput
	}
	return limits, nil
}

// ValidateManifest checks the format's structural rules and size limits. It
// does not verify file contents or establish database compatibility.
func ValidateManifest(manifest Manifest, limits Limits) error {
	limits, err := normalizeLimits(limits)
	if err != nil {
		return err
	}
	_, err = encodeManifest(manifest, limits)
	return err
}

func encodeManifest(manifest Manifest, limits Limits) ([]byte, error) {
	if err := validateManifestFields(manifest, limits); err != nil {
		return nil, err
	}
	// UTC is the only wire representation, including when a caller supplies a
	// different zero-offset Location. Fractional seconds use Go's canonical
	// RFC 3339 representation with no insignificant trailing zeroes.
	manifest.CreatedAt = manifest.CreatedAt.UTC()
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if int64(len(data)) > limits.MaxManifestBytes {
		return nil, ErrLimit
	}
	if err := checkArchiveSize(int64(len(data)), manifest.Files, limits.MaxPlaintextBytes); err != nil {
		return nil, err
	}
	return data, nil
}

func decodeManifest(data []byte, limits Limits) (Manifest, error) {
	// encoding/json may replace invalid UTF-8 with U+FFFD. Reject the original
	// bytes before decoding instead of allowing that lossy normalization.
	if !utf8.Valid(data) {
		return Manifest{}, ErrInvalidArchive
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, ErrInvalidArchive
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, ErrInvalidArchive
	}
	encoded, err := encodeManifest(manifest, limits)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return Manifest{}, err
		}
		return Manifest{}, ErrInvalidArchive
	}
	// Exact canonical bytes reject duplicate members, reordered members,
	// alternate number/string encodings, nulls, unknown fields, and whitespace
	// ambiguity. Decoder.DisallowUnknownFields alone does not reject duplicates.
	if !bytes.Equal(data, encoded) {
		return Manifest{}, ErrInvalidArchive
	}
	manifest.CreatedAt = manifest.CreatedAt.UTC()
	return manifest, nil
}

func validateManifestFields(manifest Manifest, limits Limits) error {
	_, offset := manifest.CreatedAt.Zone()
	if manifest.Format != FormatVersion || !lowerHex(manifest.ID, 16) ||
		manifest.CreatedAt.IsZero() || manifest.CreatedAt.Year() < 1970 ||
		manifest.CreatedAt.Year() > 9999 || offset != 0 ||
		!boundedText(manifest.GobyVersion, 128) {
		return ErrInvalidInput
	}
	if manifest.Source.SchemaVersion < 1 || manifest.Source.SchemaVersion > 9999 ||
		!lowerHex(manifest.Source.SchemaSHA256, 32) ||
		manifest.Source.ProbeVersion < 0 || manifest.Source.ProbeVersion > math.MaxInt32 ||
		manifest.Source.PostgreSQLVersionNum < 100000 || manifest.Source.PostgreSQLVersionNum > 99999999 ||
		!boundedText(manifest.Source.PostgreSQLVersion, 256) ||
		!sqlIdentifier(manifest.Source.DatabaseSchema) || !boundedText(manifest.Source.ServerID, 256) {
		return ErrInvalidInput
	}
	if int64(len(manifest.Source.MigrationChecksums)) != manifest.Source.SchemaVersion {
		return ErrInvalidInput
	}
	for index, migration := range manifest.Source.MigrationChecksums {
		if migration.Version != int64(index+1) || !migrationName(migration.Name, migration.Version) || !lowerHex(migration.SHA256, 32) {
			return ErrInvalidInput
		}
	}
	if len(manifest.Source.Tables) > limits.MaxTables {
		return ErrLimit
	}
	if len(manifest.Source.Tables) == 0 {
		return ErrInvalidInput
	}
	previous := ""
	for _, table := range manifest.Source.Tables {
		if !sqlIdentifier(table.Name) || table.Name <= previous || table.Rows < 0 || !lowerHex(table.SHA256, 32) {
			return ErrInvalidInput
		}
		previous = table.Name
	}
	if len(manifest.Files) != 2 && len(manifest.Files) != 3 {
		return ErrInvalidInput
	}
	for index, file := range manifest.Files {
		name, maximum := DatabaseName, limits.MaxDatabaseBytes
		switch index {
		case 1:
			name, maximum = ConfigurationName, limits.MaxConfigurationBytes
		case 2:
			name, maximum = MasterKeyName, 32
		}
		if file.Name != name || file.Size < 1 || !lowerHex(file.SHA256, 32) {
			return ErrInvalidInput
		}
		if file.Size > maximum {
			return ErrLimit
		}
		if index == 2 && file.Size != 32 {
			return ErrInvalidInput
		}
	}
	return nil
}

func checkArchiveSize(manifestSize int64, files []File, maximum int64) error {
	total := int64(2 * tarBlockBytes)
	add := func(size int64) bool {
		if size < 0 || size > hardDatabaseBytes {
			return false
		}
		entry := int64(tarBlockBytes) + size + paddingSize(size)
		if total > maximum || entry > maximum-total {
			return false
		}
		total += entry
		return true
	}
	if !add(manifestSize) {
		return ErrLimit
	}
	for _, file := range files {
		if !add(file.Size) {
			return ErrLimit
		}
	}
	return nil
}

func lowerHex(value string, size int) bool {
	if len(value) != size*2 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func boundedText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return false
		}
	}
	return true
}

// Conservative SQL identifiers are structural metadata, never executable SQL.
func sqlIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for index, c := range value {
		if !(c >= 'a' && c <= 'z' || c == '_' || index > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func validPassphrase(passphrase []byte) bool {
	return len(passphrase) > 0 && len(passphrase) <= MaxPassphraseBytes && utf8.Valid(passphrase)
}

func migrationName(name string, version int64) bool {
	if version < 1 || version > 9999 || len(name) < len("0001_a.sql") || len(name) > 128 || !strings.HasSuffix(name, ".sql") {
		return false
	}
	digits := strconv.FormatInt(version, 10)
	prefix := strings.Repeat("0", 4-len(digits)) + digits + "_"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	stem := name[len(prefix) : len(name)-len(".sql")]
	for index, c := range stem {
		if !(c >= 'a' && c <= 'z' || index > 0 && (c >= '0' && c <= '9' || c == '_')) {
			return false
		}
	}
	return true
}
