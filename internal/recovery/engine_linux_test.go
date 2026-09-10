//go:build linux

package recovery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
)

func testDeployment(t *testing.T) config.Config {
	t.Helper()
	directory := t.TempDir()
	return config.Config{
		DatabaseURL: "postgres://primary:fixture-password@127.0.0.1:15432/goby_primary?sslmode=disable",
		ServerName:  "Deployment defaults", ListenAddress: "127.0.0.1:18098",
		PublicURL: "http://127.0.0.1:18098", APIKeyMasterKeyFile: filepath.Join(directory, "original-master.key"),
		MediaRoots: []string{filepath.Join(directory, "media")},
		Transcoding: config.TranscodingConfig{
			MaxBitrate: config.DefaultMaxBitrate, MaxWidth: config.DefaultMaxWidth,
			MaxHeight: config.DefaultMaxHeight, MaxAudioChannels: config.DefaultMaxAudioChannels,
		},
		Recovery: config.RecoveryConfig{
			Directory:   filepath.Join(directory, "recovery"),
			DatabaseURL: "postgres://recovery:fixture-password@127.0.0.1:15432/goby_recovery?sslmode=disable",
			Backups: backupstore.Config{Directory: filepath.Join(directory, "backups"),
				MaxObjectBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxObjects: 8, MinFreeBytes: 1},
		}.WithDefaults(),
	}
}

func TestOpenArchiveRetainsScratchUntilCallerCloses(t *testing.T) {
	cfg := testDeployment(t)
	store, err := backupstore.Open(cfg.Recovery.Backups)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	limits := backupformat.DefaultLimits()
	limits.MaxDatabaseBytes = 1 << 20
	limits.MaxEncryptedBytes = 1 << 20
	limits.MaxConfigurationBytes = config.MaxBackupDefaultsBytes
	engine := &Engine{store: store, limits: limits, options: backuppg.Options{Timeout: time.Minute}, gate: make(chan struct{}, 1)}
	configuration, err := config.EncodeBackupDefaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("PGDMP synthetic lifecycle fixture, never execute")
	master := bytes.Repeat([]byte{0x79}, 32)
	manifest := backupformat.Manifest{
		Format: backupformat.FormatVersion, ID: strings.Repeat("a", 32), CreatedAt: time.Now().UTC(), GobyVersion: "test",
		Source: backupformat.SourceFacts{
			SchemaVersion: 1, ProbeVersion: 6, DatabaseSchema: "public", ServerID: "synthetic-server",
			PostgreSQLVersion: "17.11", PostgreSQLVersionNum: 170011, SchemaSHA256: strings.Repeat("a", 64),
			MigrationChecksums: []backupformat.MigrationFact{{Version: 1, Name: "0001_synthetic.sql", SHA256: strings.Repeat("b", 64)}},
			Tables:             []backupformat.TableFact{{Name: "users", Rows: 0, SHA256: strings.Repeat("c", 64)}},
		},
		Files: []backupformat.File{describeBytes(backupformat.DatabaseName, data),
			describeBytes(backupformat.ConfigurationName, configuration), describeBytes(backupformat.MasterKeyName, master)},
	}
	passphrase := []byte("fixture passphrase for lifecycle")
	var encrypted bytes.Buffer
	if err := backupformat.CreateWithLimits(context.Background(), &encrypted, passphrase, manifest,
		backupformat.Entries{Database: bytes.NewReader(data), Configuration: bytes.NewReader(configuration), MasterKey: bytes.NewReader(master)}, limits); err != nil {
		t.Fatal(err)
	}
	// Preserve the production KDF while keeping sequential race-test memory
	// within the remote verifier's explicit cgroup budget.
	runtime.GC()
	debug.FreeOSMemory()
	t.Cleanup(func() { runtime.GC(); debug.FreeOSMemory() })
	archive, err := engine.OpenArchive(context.Background(), bytes.NewReader(encrypted.Bytes()), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	select {
	case <-archive.Context().Done():
		t.Fatal("successful extraction cancelled the caller's archive lifetime")
	case <-time.After(20 * time.Millisecond):
	}
	actual, err := io.ReadAll(archive.Database())
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("returned dump was not available to its caller: %v", err)
	}
	if !bytes.Equal(archive.Master, master) || archive.Defaults.ServerName != cfg.ServerName {
		t.Fatal("archive did not retain its authenticated key and deployment defaults")
	}
	encoded, err := json.Marshal(archive)
	if err != nil || bytes.Contains(encoded, []byte(base64.StdEncoding.EncodeToString(master))) || bytes.Contains(encoded, []byte(`"Master"`)) {
		t.Fatal("JSON serialization exposed the recovered master")
	}
	if _, err := engine.OpenArchive(context.Background(), bytes.NewReader(encrypted.Bytes()), passphrase); !errors.Is(err, ErrBusy) {
		t.Fatalf("another KDF operation entered while the archive was retained: %v", err)
	}
	file := archive.Database()
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed archive retained its plaintext descriptor: %v", err)
	}
	if !bytes.Equal(archive.Master, make([]byte, 32)) {
		t.Fatal("Close did not clear the recovered master buffer")
	}
	if err := engine.acquire(context.Background()); err != nil {
		t.Fatalf("Close did not release the operation gate: %v", err)
	}
	engine.release()
}

func TestPassphrasePolicyPreservesExactUTF8Bytes(t *testing.T) {
	for _, invalid := range [][]byte{nil, []byte("too short"), bytes.Repeat([]byte{'x'}, backupformat.MaxPassphraseBytes+1), append(bytes.Repeat([]byte{'x'}, 12), 0xff)} {
		if !errors.Is(ValidatePassphrase(invalid), ErrInvalid) {
			t.Fatal("invalid passphrase accepted")
		}
	}
	for _, valid := range [][]byte{[]byte(" twelve bytes "), []byte("\u5bc6\u78bc\u5bc6\u78bc"), bytes.Repeat([]byte{'x'}, backupformat.MaxPassphraseBytes)} {
		before := bytes.Clone(valid)
		if err := ValidatePassphrase(valid); err != nil || !bytes.Equal(valid, before) {
			t.Fatalf("valid passphrase changed or was rejected: %v", err)
		}
	}
}
