// Package recovery coordinates native encrypted backups and staged recovery.
// It never implements or claims the upstream Emby backup archive format.
package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

var (
	ErrInvalid     = errors.New("invalid recovery request")
	ErrUnavailable = errors.New("recovery service unavailable")
	ErrBusy        = errors.New("another recovery operation is running")
	ErrArchive     = errors.New("backup archive could not be authenticated or validated")
	ErrConflict    = errors.New("recovery state changed")
)

const MinPassphraseBytes = 12

// Engine serializes memory-intensive encryption and extraction. Its owner
// supplies already authorized work, owns every dependency, and must drain
// operations before closing the database, vault, or backup store. Passphrases
// remain caller-owned and are never retained in configuration or job metadata.
type Engine struct {
	pool          *pgxpool.Pool
	vault         *identity.ApplicationKeyVault
	store         *backupstore.Store
	options       backuppg.Options
	limits        backupformat.Limits
	configuration []byte
	version       string
	gate          chan struct{}
}

// NewEngine resolves deployment-selected executables without executing them.
// The PostgreSQL layer verifies their versions for each operation. This is a
// runtime constructor; configuration loading itself does not probe the host.
func NewEngine(cfg config.Config, pool *pgxpool.Pool, vault *identity.ApplicationKeyVault, store *backupstore.Store, version string) (*Engine, error) {
	if pool == nil || vault == nil {
		return nil, ErrInvalid
	}
	return newEngine(cfg, pool, vault, store, version)
}

// NewOfflineEngine accepts the original identity only from trusted deployment
// configuration. It never connects to that database or reads the active vault.
// It cannot create backups; its purpose is recovery when the old server or
// database is unavailable. Native request bodies must not supply this config.
func NewOfflineEngine(cfg config.Config, store *backupstore.Store, version string) (*Engine, error) {
	return newEngine(cfg, nil, nil, store, version)
}

func newEngine(cfg config.Config, pool *pgxpool.Pool, vault *identity.ApplicationKeyVault, store *backupstore.Store, version string) (*Engine, error) {
	if store == nil || version == "" {
		return nil, ErrInvalid
	}
	rc := cfg.Recovery.WithDefaults()
	if err := rc.Validate(cfg.DatabaseURL); err != nil {
		return nil, ErrInvalid
	}
	configuration, err := config.EncodeBackupDefaults(cfg)
	if err != nil {
		return nil, ErrInvalid
	}
	resolve := func(name string) (string, error) {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", ErrUnavailable
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return "", ErrUnavailable
		}
		return path, nil
	}
	dump, err := resolve(rc.PGDumpPath)
	if err != nil {
		return nil, err
	}
	restore, err := resolve(rc.PGRestorePath)
	if err != nil {
		return nil, err
	}
	limits := backupformat.DefaultLimits()
	limits.MaxDatabaseBytes = min(limits.MaxDatabaseBytes, rc.Backups.MaxObjectBytes)
	limits.MaxConfigurationBytes = config.MaxBackupDefaultsBytes
	limits.MaxEncryptedBytes = min(limits.MaxEncryptedBytes, rc.Backups.MaxObjectBytes)
	return &Engine{
		pool: pool, vault: vault, store: store, configuration: configuration,
		version: version, gate: make(chan struct{}, 1), limits: limits,
		options: backuppg.Options{
			SourceURL: cfg.DatabaseURL, PGDump: dump, PGRestore: restore,
			Schema: "public", Timeout: rc.OperationTimeout,
			MaxDumpBytes: limits.MaxDatabaseBytes, ProbeVersion: media.CurrentProbeVersion,
		},
	}, nil
}

func ValidatePassphrase(passphrase []byte) error {
	if len(passphrase) < MinPassphraseBytes || len(passphrase) > backupformat.MaxPassphraseBytes || !utf8.Valid(passphrase) {
		return ErrInvalid
	}
	return nil
}

func (e *Engine) acquire(ctx context.Context) error {
	if e == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case e.gate <- struct{}{}:
		return nil
	default:
		return ErrBusy
	}
}

func (e *Engine) release() { <-e.gate }

// Create writes a complete encrypted archive into an unpublished store writer.
// The caller must Prepare, commit its audit event, and Publish separately, or
// Abort on any error. Neither a partially written file nor a successful return
// grants publication authority. The writer's ID becomes the archive ID.
func (e *Engine) Create(ctx context.Context, writer *backupstore.Writer, passphrase []byte) (manifest backupformat.Manifest, resultErr error) {
	if e == nil || e.pool == nil || e.vault == nil || writer == nil || ValidatePassphrase(passphrase) != nil {
		return manifest, ErrInvalid
	}
	if err := e.acquire(ctx); err != nil {
		return manifest, err
	}
	defer e.release()
	ctx, cancel := context.WithTimeout(ctx, e.options.Timeout)
	defer cancel()
	scratch, err := e.store.Scratch(ctx, e.limits.MaxDatabaseBytes)
	if err != nil {
		return manifest, err
	}
	defer func() {
		if err := scratch.Close(); err != nil && resultErr == nil {
			manifest, resultErr = backupformat.Manifest{}, ErrUnavailable
		}
	}()
	snapshot, err := backuppg.OpenSnapshot(ctx, e.pool, e.options)
	if err != nil {
		return manifest, err
	}
	defer snapshot.Close()
	master := make([]byte, 32)
	defer clear(master)
	witness, err := e.vault.WitnessBackup(snapshot.Context(), snapshot.Tx(), master)
	if err != nil {
		return manifest, err
	}
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		return manifest, err
	}
	if err := snapshot.Dump(ctx, scratch.File()); err != nil {
		return manifest, err
	}
	// Release the exported database snapshot before hashing and the expensive
	// KDF. The copied key and deployment defaults already match that snapshot.
	if err := snapshot.Close(); err != nil {
		return manifest, err
	}
	descriptor, err := describeFile(ctx, scratch.File(), backupformat.DatabaseName, e.limits.MaxDatabaseBytes)
	if err != nil {
		return manifest, err
	}
	manifest = backupformat.Manifest{
		Format: backupformat.FormatVersion, ID: writer.Metadata().ID,
		CreatedAt: time.Now().UTC(), GobyVersion: e.version, Source: facts,
		Files: []backupformat.File{descriptor, describeBytes(backupformat.ConfigurationName, e.configuration)},
	}
	entries := backupformat.Entries{Database: scratch.File(), Configuration: bytes.NewReader(e.configuration)}
	if witness.HasMasterKey {
		manifest.Files = append(manifest.Files, describeBytes(backupformat.MasterKeyName, master))
		entries.MasterKey = bytes.NewReader(master)
	}
	if err := backupformat.CreateWithLimits(ctx, writer, passphrase, manifest, entries, e.limits); err != nil {
		return backupformat.Manifest{}, err
	}
	return manifest, nil
}

// Archive contains authenticated bytes, not a database restoration approval.
// Source facts still require independent PostgreSQL validation, and the master
// must be matched against restored ciphertext before credentials are revoked.
// Close erases the caller-owned key buffer and closes the anonymous dump. The
// engine remains occupied until Close; callers must not log or serialize it.
type Archive struct {
	Manifest backupformat.Manifest
	Defaults config.BackupDefaults
	Master   []byte `json:"-"`
	scratch  *backupstore.Scratch
	engine   *Engine
	ctx      context.Context
	cancel   context.CancelFunc
	use      sync.Mutex
	closed   bool
	once     sync.Once
	err      error
}

func (a *Archive) Database() *os.File {
	if a == nil || a.scratch == nil {
		return nil
	}
	return a.scratch.File()
}

func (a *Archive) Context() context.Context { return a.ctx }

func (a *Archive) Close() error {
	if a == nil {
		return nil
	}
	a.once.Do(func() {
		a.use.Lock()
		defer a.use.Unlock()
		a.closed = true
		clear(a.Master)
		a.cancel()
		a.err = a.scratch.Close()
		a.engine.release()
	})
	return a.err
}

// OpenArchive decrypts into anonymous private scratch. A stored import's
// Verified flag must remain false until PostgreSQL restoration and the vault
// witness also succeed. The input snapshot remains the caller's resource.
func (e *Engine) OpenArchive(ctx context.Context, input io.Reader, passphrase []byte) (_ *Archive, resultErr error) {
	if e == nil || !validArchiveSource(input, e.limits.MaxEncryptedBytes) || ValidatePassphrase(passphrase) != nil {
		return nil, ErrInvalid
	}
	if err := e.acquire(ctx); err != nil {
		return nil, err
	}
	release := true
	defer func() {
		if release {
			e.release()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, e.options.Timeout)
	defer func() {
		if release {
			cancel()
		}
	}()
	scratch, err := e.store.Scratch(ctx, e.limits.MaxDatabaseBytes)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = scratch.Close()
		}
	}()
	var configuration, master bytes.Buffer
	defer func() { clear(master.Bytes()) }()
	manifest, err := backupformat.Extract(ctx, input, passphrase, backupformat.Sinks{
		Database: scratch.File(), Configuration: &configuration, MasterKey: &master,
	}, e.limits)
	if err != nil {
		return nil, ErrArchive
	}
	defaults, err := config.DecodeBackupDefaults(configuration.Bytes())
	if err != nil || manifest.Source.ProbeVersion > media.CurrentProbeVersion {
		return nil, ErrArchive
	}
	if _, err := scratch.File().Seek(0, io.SeekStart); err != nil {
		return nil, ErrUnavailable
	}
	result := &Archive{Manifest: manifest, Defaults: defaults, Master: bytes.Clone(master.Bytes()), scratch: scratch, engine: e, ctx: ctx, cancel: cancel}
	keep, release = true, false
	return result, nil
}

// A caller-provided network reader can block despite context cancellation.
// Extraction accepts only finite memory buffers or owned regular file handles;
// HTTP uploads must first complete their bounded encrypted-store import.
func validArchiveSource(input io.Reader, maximum int64) bool {
	var size int64
	switch source := input.(type) {
	case *backupstore.Snapshot:
		if source == nil {
			return false
		}
		size = source.Size()
	case *os.File:
		if source == nil {
			return false
		}
		info, err := source.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		size = info.Size()
	case *bytes.Reader:
		if source == nil {
			return false
		}
		size = int64(source.Len())
	case *bytes.Buffer:
		if source == nil {
			return false
		}
		size = int64(source.Len())
	default:
		return false
	}
	return size > 0 && size <= maximum
}

// Summary projects only safe archive facts. Callers may use it for generated
// archives or after a successful full restore rehearsal, never merely after
// parsing an imported encrypted manifest.
func Summary(manifest backupformat.Manifest) backupstore.SourceSummary {
	tables := make([]backupstore.TableCount, len(manifest.Source.Tables))
	for index, table := range manifest.Source.Tables {
		tables[index] = backupstore.TableCount{Name: table.Name, Rows: table.Rows}
	}
	return backupstore.SourceSummary{
		ArchiveID: manifest.ID, FormatVersion: 1, ApplicationVersion: manifest.GobyVersion,
		SchemaVersion: int(manifest.Source.SchemaVersion), ServerID: manifest.Source.ServerID,
		CreatedAt: manifest.CreatedAt, Tables: tables,
	}
}

func describeBytes(name string, data []byte) backupformat.File {
	digest := sha256.Sum256(data)
	return backupformat.File{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
}

func describeFile(ctx context.Context, file *os.File, name string, maximum int64) (backupformat.File, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return backupformat.File{}, ErrUnavailable
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return backupformat.File{}, ErrUnavailable
	}
	hash := sha256.New()
	reader := &contextReader{ctx: ctx, reader: io.LimitReader(file, maximum+1)}
	written, err := io.CopyBuffer(hash, reader, make([]byte, 64<<10))
	if err != nil || written != info.Size() {
		return backupformat.File{}, ErrUnavailable
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return backupformat.File{}, ErrUnavailable
	}
	return backupformat.File{Name: name, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
