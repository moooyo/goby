package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	scanEvidenceMaxPasses      = 4
	scanEvidenceMaxReservation = int64(4 << 30)
	// This covers evidence-owned roots and fallback identities, plus ten fixed
	// descriptors: parent, spool, change queue, enumerator, record, verification
	// root/file, named-chain root, and the private openat directory/file pair.
	// Root-binding capture has its own separate budget.
	scanEvidenceDescriptorsPerPass = 4096 + scanReconciliationMaxRoots + 10
	scanEvidenceMaxDescriptors     = scanEvidenceMaxPasses * scanEvidenceDescriptorsPerPass
)

type storeOptions struct{ scanEvidence *ScanEvidenceOptions }

// Option attaches optional deployment resources without changing existing callers.
type Option func(*storeOptions) error

// ScanEvidenceOptions describes one separately provisioned private spool root.
// ServerID is the persistent public identity; no connection secret is persisted.
type ScanEvidenceOptions struct {
	Directory          string
	ServerID           string
	ExcludedRoots      []string
	MaxBytes           int64
	MaxDirectories     int
	MaxEntries         int
	MaxFallbackHandles int
}

func WithScanEvidence(options ScanEvidenceOptions) Option {
	options.ExcludedRoots = append([]string(nil), options.ExcludedRoots...)
	return func(settings *storeOptions) error {
		if settings.scanEvidence != nil {
			return fmt.Errorf("%w: repeated scan evidence option", ErrInvalidInput)
		}
		value, err := options.normalized()
		if err != nil {
			return err
		}
		settings.scanEvidence = &value
		return nil
	}
}

func (options ScanEvidenceOptions) normalized() (ScanEvidenceOptions, error) {
	if options.MaxBytes == 0 {
		options.MaxBytes = 1 << 30
	}
	if options.MaxDirectories == 0 {
		options.MaxDirectories = 131072
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = 1048576
	}
	if options.MaxFallbackHandles == 0 {
		options.MaxFallbackHandles = 4096
	}
	if options.Directory == "" || !filepath.IsAbs(options.Directory) || filepath.Clean(options.Directory) != options.Directory ||
		options.Directory == filepath.VolumeName(options.Directory)+string(filepath.Separator) ||
		strings.ContainsRune(options.Directory, '\x00') || len(options.Directory) > 4096 ||
		strings.TrimSpace(options.ServerID) == "" || len(options.ServerID) > 128 || strings.ContainsRune(options.ServerID, '\x00') ||
		options.MaxBytes < 64<<10 || options.MaxBytes > 1<<30 || options.MaxDirectories < 1 || options.MaxDirectories > 131072 ||
		options.MaxEntries < 1 || options.MaxEntries > 1048576 || options.MaxFallbackHandles < 1 || options.MaxFallbackHandles > 4096 {
		return options, fmt.Errorf("%w: invalid scan evidence deployment inventory", ErrInvalidInput)
	}
	return options, nil
}

// ScanEvidenceStatus reports reserved capacity, including blocked retirement.
type ScanEvidenceStatus struct {
	Enabled                    bool
	ActivePasses               int
	RetiringPasses             int
	CleanupFailures            int
	ReservedBytes              int64
	ReservedFileDescriptors    int
	MaxPasses                  int
	MaxReservedBytes           int64
	MaxReservedFileDescriptors int
}

type scanEvidenceLease struct {
	id       string
	name     string
	evidence *scanReconciliationEvidence
	finished bool
	err      error
}

type scanEvidenceManager struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	options      ScanEvidenceOptions
	disk         *scanEvidenceDisk
	leases       map[string]*scanEvidenceLease
	constructors sync.WaitGroup
	retirements  sync.WaitGroup
	closed       bool
	err          error
}

func (s *Store) scanEvidenceScope(ctx context.Context, serverID string) (string, error) {
	// The already held catalog lock prevents concurrent same-catalog recovery.
	s.ownership.mu.Lock()
	defer s.ownership.mu.Unlock()
	if s.ownership.conn == nil || s.ownership.lost.Load() {
		return "", ErrUnavailable
	}
	var database, schema string
	var databaseOID, schemaOID uint32
	err := s.ownership.conn.QueryRow(ctx, `SELECT current_database(), current_schema(),
		(SELECT oid FROM pg_database WHERE datname=current_database()), current_schema()::regnamespace::oid`).Scan(
		&database, &schema, &databaseOID, &schemaOID)
	if err != nil {
		return "", fmt.Errorf("read scan evidence catalog scope: %w", err)
	}
	connection := s.pool.Config().ConnConfig
	data, _ := json.Marshal(struct {
		Server, Database, Schema, Host string
		Port                           uint16
		DatabaseOID, SchemaOID         uint32
	}{serverID, database, schema, strings.ToLower(connection.Host), connection.Port, databaseOID, schemaOID})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func newScanEvidenceManager(ctx context.Context, scope string, options ScanEvidenceOptions, mediaRoots []string) (*scanEvidenceManager, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	excluded := append(append([]string(nil), options.ExcludedRoots...), mediaRoots...)
	disk, err := openScanEvidenceDisk(options.Directory, scope, excluded)
	if err != nil {
		return nil, err
	}
	managerCtx, cancel := context.WithCancel(ctx)
	return &scanEvidenceManager{ctx: managerCtx, cancel: cancel, options: options, disk: disk,
		leases: make(map[string]*scanEvidenceLease)}, nil
}

// newScanReconciliationEvidence preserves bounded memory for deployments that
// did not explicitly enable the independent on-disk evidence inventory.
func (s *Store) newScanReconciliationEvidence(ctx context.Context) (*scanReconciliationEvidence, error) {
	if s == nil || ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closing.Load() {
		return nil, ErrUnavailable
	}
	if s.scanEvidence == nil {
		return newScanReconciliationEvidence(), nil
	}
	return s.scanEvidence.admit(ctx)
}

func (manager *scanEvidenceManager) admit(ctx context.Context) (*scanReconciliationEvidence, error) {
	manager.mu.Lock()
	if manager.closed || manager.ctx.Err() != nil || manager.err != nil {
		err := errors.Join(ErrUnavailable, manager.err)
		manager.mu.Unlock()
		return nil, err
	}
	if len(manager.leases) >= scanEvidenceMaxPasses {
		manager.mu.Unlock()
		return nil, fmt.Errorf("%w: scan evidence retirement capacity is full", ErrBusy)
	}
	if err := manager.disk.check(); err != nil {
		manager.err = errors.Join(manager.err, err)
		manager.mu.Unlock()
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		manager.mu.Unlock()
		return nil, err
	}
	lease := &scanEvidenceLease{id: id}
	manager.leases[id] = lease
	manager.constructors.Add(1)
	manager.retirements.Add(1)
	manager.mu.Unlock()
	defer manager.constructors.Done()
	passCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(manager.ctx, cancel)
	cleanup := func(receipt scanReconciliationSpoolCleanup) {
		stop()
		cancel()
		manager.finish(lease, receipt)
	}
	evidence := newScanReconciliationSpoolEvidence(passCtx, scanReconciliationSpoolOptions{
		Parent: manager.disk.root, MaxDirectories: manager.options.MaxDirectories, MaxEntries: manager.options.MaxEntries,
		MaxBytes: manager.options.MaxBytes, MaxRoots: scanReconciliationMaxRoots, MaxFallbackHandles: manager.options.MaxFallbackHandles,
		BeforeCreate: func(name string) error {
			manager.mu.Lock()
			defer manager.mu.Unlock()
			if manager.closed || passCtx.Err() != nil {
				return ErrUnavailable
			}
			lease.name = name
			if err := manager.disk.reserve(name, manager.options.MaxBytes); err != nil {
				lease.err = err
				manager.err = errors.Join(manager.err, err)
				return err
			}
			return nil
		}, Created: func(name string, info os.FileInfo) error {
			manager.mu.Lock()
			defer manager.mu.Unlock()
			if err := manager.disk.created(name, info); err != nil {
				lease.err = err
				manager.err = errors.Join(manager.err, err)
				return err
			}
			return nil
		}, OnCleanup: cleanup,
	})
	manager.mu.Lock()
	lease.evidence = evidence
	manager.mu.Unlock()
	// Invalid constructor input creates no spool and therefore no callback.
	// The manager still owns and retires the reservation it admitted above.
	if evidence.spool == nil {
		cleanup(scanReconciliationSpoolCleanup{})
	}
	if err := evidence.Err(); err != nil {
		_ = evidence.Close()
		return nil, err
	}
	return evidence, nil
}

func (manager *scanEvidenceManager) finish(lease *scanEvidenceLease, receipt scanReconciliationSpoolCleanup) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if lease.finished {
		return
	}
	lease.finished = true
	defer manager.retirements.Done()
	lease.err = errors.Join(lease.err, receipt.Err)
	if lease.err == nil && lease.name != "" {
		lease.err = manager.disk.release(lease.name)
	}
	if lease.err != nil {
		manager.err = errors.Join(manager.err, lease.err)
		return
	}
	delete(manager.leases, lease.id)
}

func (s *Store) ScanEvidenceStatus() ScanEvidenceStatus {
	if s == nil || s.scanEvidence == nil {
		return ScanEvidenceStatus{}
	}
	manager := s.scanEvidence
	manager.mu.Lock()
	defer manager.mu.Unlock()
	status := ScanEvidenceStatus{Enabled: true, ActivePasses: len(manager.leases), MaxPasses: scanEvidenceMaxPasses,
		MaxReservedBytes: scanEvidenceMaxReservation, MaxReservedFileDescriptors: scanEvidenceMaxDescriptors}
	for _, lease := range manager.leases {
		status.ReservedBytes += manager.options.MaxBytes
		status.ReservedFileDescriptors += manager.options.MaxFallbackHandles + scanReconciliationMaxRoots + 10
		if lease.finished && lease.err != nil {
			status.CleanupFailures++
		}
		if !lease.finished && lease.evidence != nil {
			lifetime := &lease.evidence.observation
			lifetime.mu.Lock()
			if lifetime.retired {
				status.RetiringPasses++
			}
			lifetime.mu.Unlock()
		}
	}
	return status
}

// close joins real retirement without transferring the held parent or lock on
// an error. Store.Close supplies the caller deadline while this work continues.
func (manager *scanEvidenceManager) close() error {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	manager.closed = true
	manager.cancel()
	manager.mu.Unlock()
	manager.constructors.Wait()
	manager.mu.Lock()
	var evidence []*scanReconciliationEvidence
	for _, lease := range manager.leases {
		if lease.evidence != nil {
			evidence = append(evidence, lease.evidence)
		}
	}
	manager.mu.Unlock()
	for _, pass := range evidence {
		_ = pass.Close()
	}
	manager.retirements.Wait()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.err != nil || len(manager.leases) != 0 {
		return errors.Join(fmt.Errorf("%w: scan evidence cleanup remains unfinished", ErrUnavailable), manager.err)
	}
	return manager.disk.close()
}
