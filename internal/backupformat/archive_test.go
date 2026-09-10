package backupformat

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

var syntheticPassphrase = []byte("synthetic-format-tests-only-8a48ca3d")

func TestProductionAgeRoundTrip(t *testing.T) {
	releaseKDFMemory()
	t.Cleanup(releaseKDFMemory)
	manifest, members := fixture(true)
	members[DatabaseName] = bytes.Repeat([]byte("synthetic PostgreSQL custom dump bytes\x00"), 4096)
	manifest.Files[0] = describe(DatabaseName, members[DatabaseName])
	var encrypted bytes.Buffer
	if err := Create(context.Background(), &encrypted, syntheticPassphrase, manifest, entryReaders(members)); err != nil {
		t.Fatal(err)
	}
	releaseKDFMemory()
	lines := bytes.SplitN(encrypted.Bytes(), []byte{'\n'}, 4)
	if len(lines) != 4 || !bytes.HasPrefix(lines[1], []byte("-> scrypt ")) || !bytes.HasSuffix(lines[1], []byte(" 18")) {
		t.Fatal("production creation did not use the required age scrypt work factor")
	}
	var database, configuration, master bytes.Buffer
	got, err := Extract(context.Background(), bytes.NewReader(encrypted.Bytes()), syntheticPassphrase, Sinks{
		Database: &database, Configuration: &configuration, MasterKey: &master,
	}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, manifest) || !bytes.Equal(database.Bytes(), members[DatabaseName]) ||
		!bytes.Equal(configuration.Bytes(), members[ConfigurationName]) || !bytes.Equal(master.Bytes(), members[MasterKeyName]) {
		t.Fatal("round trip changed manifest or member contents")
	}
}

func TestInspectWithoutMasterVerifiesAllMembers(t *testing.T) {
	manifest, members := fixture(false)
	archive := encryptFixture(t, plainFixture(t, manifest, members))
	got, err := Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{})
	if err != nil || !reflect.DeepEqual(got, manifest) {
		t.Fatalf("inspect = %#v, %v", got, err)
	}
	manifest.Files[0].SHA256 = strings.Repeat("0", 64)
	archive = encryptFixture(t, plainFixture(t, manifest, members))
	got, err = Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{})
	if !errors.Is(err, ErrInvalidArchive) || got.Format != "" {
		t.Fatal("Inspect returned success or a partial manifest without checking the dump hash")
	}
}

func TestEncryptedCorruptionTruncationAndSuffixes(t *testing.T) {
	manifest, members := fixture(false)
	members[DatabaseName] = bytes.Repeat([]byte("synthetic data"), 8192)
	manifest.Files[0] = describe(DatabaseName, members[DatabaseName])
	archive := encryptFixture(t, plainFixture(t, manifest, members))
	cases := map[string][]byte{
		"empty":                {},
		"partial introduction": archive[:10],
		"partial header":       archive[:90],
		"missing final byte":   archive[:len(archive)-1],
		"missing final tag":    archive[:len(archive)-16],
		"missing final chunk":  archive[:66000],
		"ciphertext suffix":    append(append([]byte{}, archive...), 1),
		"zero suffix":          append(append([]byte{}, archive...), make([]byte, 1024)...),
		"concatenated archive": append(append([]byte{}, archive...), archive...),
	}
	for _, offset := range []int{55, 300, len(archive) - 1} {
		mutated := append([]byte{}, archive...)
		mutated[offset] ^= 0x40
		cases["tamper "+string(rune('a'+offset%26))] = mutated
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			manifest, err := Inspect(context.Background(), bytes.NewReader(data), syntheticPassphrase, Limits{})
			if !errors.Is(err, ErrInvalidArchive) || manifest.Format != "" {
				t.Fatalf("corrupt archive accepted: %v", err)
			}
		})
	}
	_, err := Inspect(context.Background(), bytes.NewReader(archive), []byte("wrong synthetic password"), Limits{})
	if !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("wrong passphrase: %v", err)
	}
}

func TestTarEndMustCoincideWithAuthenticatedEOF(t *testing.T) {
	manifest, members := fixture(false)
	plain := plainFixture(t, manifest, members)
	cases := map[string][]byte{
		"plaintext suffix":  append(append([]byte{}, plain...), []byte("unlisted contents")...),
		"extra zero block":  append(append([]byte{}, plain...), make([]byte, 512)...),
		"one end block":     plain[:len(plain)-512],
		"no end blocks":     plain[:len(plain)-1024],
		"nonzero end block": append([]byte{}, plain...),
	}
	cases["nonzero end block"][len(plain)-1] = 1
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Inspect(context.Background(), bytes.NewReader(encryptFixture(t, data)), syntheticPassphrase, Limits{})
			if !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("malformed plaintext accepted: %v", err)
			}
		})
	}
	// The final age chunk can be exactly 64 KiB. This exercises the stream
	// format's final-chunk EOF probe rather than only a short final chunk.
	for range 3 {
		plain = plainFixture(t, manifest, members)
		if len(plain)%(64<<10) == 0 {
			break
		}
		members[DatabaseName] = append(members[DatabaseName], make([]byte, (64<<10)-len(plain)%(64<<10))...)
		manifest.Files[0] = describe(DatabaseName, members[DatabaseName])
	}
	plain = plainFixture(t, manifest, members)
	if len(plain)%(64<<10) != 0 {
		t.Fatal("fixture did not end at a full age chunk")
	}
	archive := encryptFixture(t, plain)
	if _, err := Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{}); err != nil {
		t.Fatal(err)
	}
	archive = append(archive, 1)
	if _, err := Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("full final chunk concealed trailing ciphertext: %v", err)
	}
}

func TestStrictMemberHeadersNamesTypesAndPadding(t *testing.T) {
	manifest, members := fixture(true)
	plain := plainFixture(t, manifest, members)
	manifestData, _ := json.Marshal(manifest)
	databaseOffset := 512 + rounded(len(manifestData))
	configurationOffset := databaseOffset + 512 + rounded(len(members[DatabaseName]))
	masterOffset := configurationOffset + 512 + rounded(len(members[ConfigurationName]))
	cases := map[string][]byte{}
	for name, hdr := range map[string]tar.Header{
		"parent path":        {Name: "../database.dump"},
		"absolute path":      {Name: "/database.dump"},
		"dot path":           {Name: "./database.dump"},
		"backslash path":     {Name: "..\\database.dump"},
		"unknown member":     {Name: "other.dump"},
		"duplicate member":   {Name: ManifestName},
		"wrong mode":         {Name: DatabaseName, Mode: 0644},
		"owner metadata":     {Name: DatabaseName, Uname: "unexpected"},
		"group metadata":     {Name: DatabaseName, Gid: 1},
		"timestamp metadata": {Name: DatabaseName, ModTime: time.Unix(1, 0)},
		"ustar encoding":     {Name: DatabaseName, Format: tar.FormatUSTAR},
		"symbolic link":      {Name: DatabaseName, Typeflag: tar.TypeSymlink, Linkname: "target"},
		"hard link":          {Name: DatabaseName, Typeflag: tar.TypeLink, Linkname: "target"},
		"directory":          {Name: DatabaseName, Typeflag: tar.TypeDir},
		"fifo":               {Name: DatabaseName, Typeflag: tar.TypeFifo},
		"character device":   {Name: DatabaseName, Typeflag: tar.TypeChar},
		"block device":       {Name: DatabaseName, Typeflag: tar.TypeBlock},
	} {
		hdr.Size = int64(len(members[DatabaseName]))
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0600
		}
		if hdr.ModTime.IsZero() {
			hdr.ModTime = time.Unix(0, 0)
		}
		if hdr.Format == tar.FormatUnknown {
			hdr.Format = tar.FormatGNU
		}
		var header bytes.Buffer
		if err := tar.NewWriter(&header).WriteHeader(&hdr); err != nil {
			t.Fatalf("test header %s: %v", name, err)
		}
		if header.Len() != 512 {
			t.Fatalf("test header %s uses extensions", name)
		}
		mutated := append([]byte{}, plain...)
		copy(mutated[databaseOffset:databaseOffset+512], header.Bytes())
		cases[name] = mutated
	}
	for name, typeflag := range map[string]byte{
		"PAX": tar.TypeXHeader, "global PAX": tar.TypeXGlobalHeader,
		"GNU long name": tar.TypeGNULongName, "GNU long link": tar.TypeGNULongLink,
		"GNU sparse": tar.TypeGNUSparse, "legacy regular type": tar.TypeRegA,
	} {
		mutated := append([]byte{}, plain...)
		mutated[databaseOffset+156] = typeflag
		fixChecksum(mutated[databaseOffset : databaseOffset+512])
		cases[name] = mutated
	}
	mutated := append([]byte{}, plain...)
	mutated[databaseOffset+512+len(members[DatabaseName])] = 1
	cases["nonzero member padding"] = mutated
	mutated = append([]byte{}, plain...)
	copy(mutated[:512], plain[databaseOffset:databaseOffset+512])
	cases["manifest not first"] = mutated
	mutated = append([]byte{}, plain...)
	copy(mutated[masterOffset:masterOffset+512], plain[configurationOffset:configurationOffset+512])
	cases["duplicate configuration"] = mutated
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Inspect(context.Background(), bytes.NewReader(encryptFixture(t, data)), syntheticPassphrase, Limits{})
			if !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("unsafe tar accepted: %v", err)
			}
		})
	}
}

func fixture(withMaster bool) (Manifest, map[string][]byte) {
	members := map[string][]byte{
		DatabaseName:      []byte("PGDMP synthetic fixture; never execute"),
		ConfigurationName: []byte(`{"format":"goby.configuration.v1"}`),
	}
	manifest := Manifest{
		Format: FormatVersion, ID: "0123456789abcdef0123456789abcdef",
		CreatedAt: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), GobyVersion: "0.1.0-dev",
		Source: SourceFacts{
			SchemaVersion: 22, ProbeVersion: 6, PostgreSQLVersion: "17.11", PostgreSQLVersionNum: 170011,
			SchemaSHA256:   digest([]byte("synthetic database schema catalog")),
			DatabaseSchema: "public", ServerID: "opaque:legacy-server-\U0001f5c3",
			Tables: []TableFact{{Name: "users", Rows: 2, SHA256: digest([]byte("synthetic table snapshot"))}},
		},
		Files: []File{describe(DatabaseName, members[DatabaseName]), describe(ConfigurationName, members[ConfigurationName])},
	}
	for version := int64(1); version <= manifest.Source.SchemaVersion; version++ {
		name := fmt.Sprintf("%04d_synthetic_migration.sql", version)
		manifest.Source.MigrationChecksums = append(manifest.Source.MigrationChecksums, MigrationFact{
			Version: version, Name: name, SHA256: digest([]byte(name)),
		})
	}
	if withMaster {
		members[MasterKeyName] = bytes.Repeat([]byte{0x42}, 32)
		manifest.Files = append(manifest.Files, describe(MasterKeyName, members[MasterKeyName]))
	}
	return manifest, members
}

func describe(name string, data []byte) File {
	return File{Name: name, Size: int64(len(data)), SHA256: digest(data)}
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func entryReaders(members map[string][]byte) Entries {
	entries := Entries{Database: bytes.NewReader(members[DatabaseName]), Configuration: bytes.NewReader(members[ConfigurationName])}
	if data, present := members[MasterKeyName]; present {
		entries.MasterKey = bytes.NewReader(data)
	}
	return entries
}

func plainFixture(t *testing.T, manifest Manifest, members map[string][]byte) []byte {
	t.Helper()
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return plainWithManifest(t, manifestData, manifest, members)
}

func plainWithManifest(t *testing.T, manifestData []byte, manifest Manifest, members map[string][]byte) []byte {
	t.Helper()
	var plain bytes.Buffer
	writer := tar.NewWriter(&plain)
	write := func(name string, data []byte) {
		t.Helper()
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), ModTime: time.Unix(0, 0).UTC(), Typeflag: tar.TypeReg, Format: tar.FormatGNU}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write(ManifestName, manifestData)
	for _, file := range manifest.Files {
		write(file.Name, members[file.Name])
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return plain.Bytes()
}

func encryptFixture(t *testing.T, plain []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	recipient, err := age.NewScryptRecipient(string(syntheticPassphrase))
	if err != nil {
		t.Fatal(err)
	}
	// Only independently constructed test fixtures use a low work factor.
	// Production Create has no override for its fixed factor of 18.
	recipient.SetWorkFactor(1)
	writer, err := age.Encrypt(&archive, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func rounded(size int) int {
	return ((size + 511) / 512) * 512
}

func fixChecksum(header []byte) {
	for index := 148; index < 156; index++ {
		header[index] = ' '
	}
	var sum int
	for _, value := range header {
		sum += int(value)
	}
	for index := 153; index >= 148; index-- {
		header[index] = byte('0' + sum%8)
		sum /= 8
	}
	header[154], header[155] = 0, ' '
}

// Production-factor tests remain sequential and explicitly return unreachable
// KDF allocations before the next KDF. This keeps race-enabled verification
// from retaining multiple approximately 256 MiB workspaces unnecessarily.
// It is a test resource policy, not a claim that password memory is erased.
func releaseKDFMemory() {
	runtime.GC()
	debug.FreeOSMemory()
}
