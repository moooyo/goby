// Package analysiscache stores sealed, disposable media-analysis derivatives.
// Authorization, current media identity, and database publication fences remain
// the caller's responsibility. Only Linux provides the required disk primitives.
package analysiscache

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"sync"
)

var (
	ErrUnsafe       = errors.New("analysis cache filesystem is unsafe or changed")
	ErrOwned        = errors.New("analysis cache already has a writer")
	ErrUnsupported  = errors.New("analysis cache requires Linux")
	ErrClosed       = errors.New("analysis cache is closing or closed")
	ErrNotFound     = errors.New("analysis cache entry not found")
	ErrBusy         = errors.New("analysis cache entry is in use")
	ErrLimit        = errors.New("analysis cache budget exceeded")
	ErrExists       = errors.New("analysis cache entry already exists")
	ErrInvalidInput = errors.New("invalid analysis cache input")
	ErrSealMismatch = errors.New("analysis cache seal mismatch")
)

const (
	controlReserve        int64 = 4096
	maxManifestBytes      int64 = 3072
	maxSmallManifestBytes int64 = 64 << 10
	maxRootNames                = 131074
	hardMaxTemporaryFiles       = 65536
	rootMarkerBytes       int64 = int64(len(diskMarkerVersion) + 32 + len(`{"marker":"","owner":""}`))
)

var keyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var temporaryPattern = regexp.MustCompile(`^frame-[0-9]{6}\.jpg$`)

type Config struct {
	Root              string
	MaxBytes          int64
	MaxEntries        int
	MaxEntryBytes     int64
	MaxFileBytes      int64
	MaxTemporaryFiles int
}

func normalizeConfig(config Config) (Config, error) {
	if config.MaxBytes == 0 {
		config.MaxBytes = 2 << 30
	}
	if config.MaxEntries == 0 {
		config.MaxEntries = 512
	}
	if config.MaxEntryBytes == 0 {
		config.MaxEntryBytes = 512 << 20
	}
	if config.MaxFileBytes == 0 {
		config.MaxFileBytes = 128 << 20
	}
	if config.MaxTemporaryFiles == 0 {
		config.MaxTemporaryFiles = 8192
	}
	if config.Root == "" || config.MaxBytes < controlReserve+rootMarkerBytes || config.MaxBytes > 16<<30 || config.MaxEntries < 1 || config.MaxEntries > 65536 ||
		config.MaxEntryBytes < controlReserve || config.MaxEntryBytes > 1<<30 || config.MaxEntryBytes > config.MaxBytes || config.MaxFileBytes < 1 ||
		config.MaxFileBytes > 512<<20 || config.MaxFileBytes > config.MaxEntryBytes || config.MaxTemporaryFiles < 1 || config.MaxTemporaryFiles > hardMaxTemporaryFiles {
		return Config{}, ErrInvalidInput
	}
	return config, nil
}

type Artifact struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Identity string `json:"identity"`
}

// Entry is an immutable-by-convention public snapshot. Mutating it cannot
// change the store's private manifest, budgets, seal, or publication pins.
type Entry struct {
	Key       string
	Seal      string
	Bytes     int64
	Artifacts []Artifact
}

func cloneEntry(entry Entry) Entry {
	entry.Artifacts = append([]Artifact(nil), entry.Artifacts...)
	return entry
}

type Stats struct {
	ReadyEntries        int
	BuildingEntries     int
	PendingPublications int
	Readers             int
	BusyEntries         int
	ReadyBytes          int64
	ReservedBytes       int64
	ControlBytes        int64
	TotalBytes          int64
	Closing             bool
}

// PruneResult counts only entries removed by this invocation, independently of
// concurrent LRU eviction, reader release or builder cancellation.
type PruneResult struct {
	RemovedEntries int
	RemovedBytes   int64
	RemainingBytes int64
	BusyEntries    int
}

type entryState struct {
	entry     Entry
	directory string
	refs      int
	pending   bool
	deleting  bool
	invalid   bool
	missing   bool
	lastUse   uint64
}

type Store struct {
	mu           sync.Mutex
	config       Config
	disk         *diskRoot
	controlBytes int64
	entries      map[string]*entryState
	builders     map[string]*Builder
	clock        uint64
	active       int
	closing      bool
	changed      chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
	closeErr     error
}

// Publication protects the filesystem-to-database decision window from LRU
// eviction. After committing its database reference, the caller must Keep it.
// A definitely unreferenced failed publication must Discard it. An uncertain
// database commit must conservatively Keep until later reference reconciliation.
// Losing this object without either decision keeps its pin and makes Close wait.
type Publication struct {
	Entry     Entry
	store     *Store
	state     *entryState
	mu        sync.Mutex
	finished  bool
	discarded bool
}

// Lease holds a read-only descriptor and pins its complete entry until Close.
// Closing File directly is insufficient: Lease.Close must release the pin.
type Lease struct {
	File       *os.File
	ownedFile  *os.File
	Artifact   Artifact
	Seal       string
	store      *Store
	state      *entryState
	retirement *closeResult
}

type closeResult struct {
	once sync.Once
	err  error
}

func (lease *Lease) Close() error {
	if lease == nil {
		return nil
	}
	if lease.ownedFile == nil || lease.store == nil || lease.state == nil || lease.retirement == nil {
		return ErrInvalidInput
	}
	lease.retirement.once.Do(func() {
		lease.retirement.err = lease.ownedFile.Close()
		if errors.Is(lease.retirement.err, os.ErrClosed) {
			lease.retirement.err = nil
		}
		lease.store.mu.Lock()
		lease.state.refs--
		lease.store.active--
		lease.store.signalLocked()
		lease.store.mu.Unlock()
	})
	return lease.retirement.err
}

func allowedArtifact(name string) bool {
	switch name {
	case "240.bif", "320.bif", "400.bif", "manifest.json":
		return true
	}
	return false
}

func copyWithContext(ctx context.Context, writer io.Writer, reader io.Reader) (int64, error) {
	var total int64
	buffer := make([]byte, 32<<10)
	empty := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := reader.Read(buffer)
		if n < 0 || n > len(buffer) {
			return total, ErrUnsafe
		}
		if n > 0 {
			empty = 0
			written, err := writer.Write(buffer[:n])
			if written < 0 || written > n {
				return total, io.ErrShortWrite
			}
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		} else {
			empty++
			if empty >= 100 {
				return total, io.ErrNoProgress
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, ctx.Err()
			}
			return total, readErr
		}
	}
}
