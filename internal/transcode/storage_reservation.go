package transcode

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
)

var (
	ErrStorageUnavailable      = errors.New("enforced workspace storage is unavailable")
	ErrStorageCapacity         = errors.New("workspace storage reservation capacity is exhausted")
	ErrStorageUnsafe           = errors.New("unsafe workspace storage ownership")
	ErrStoragePersistence      = errors.New("workspace storage reservation could not be persisted")
	ErrStorageRecoveryRequired = errors.New("workspace storage ownership requires broker recovery")
)

const (
	storageJournalVersion       = 1
	maxStorageLeases            = 4096
	maxStoragePins              = 65536
	maxStorageJournalBytes      = 8 << 20
	minStorageJournalBaseBytes  = 2*maxStorageJournalBytes + 64*1024
	minStorageJournalBaseInodes = 8
)

// storageReservationLimits reserves a complete fixed filesystem, its backing
// image and bounded broker metadata before any private workspace is created.
// Base allowances include both atomic journal copies. The owner must separately
// enforce these limits on its metadata implementation. Logical output sizes and
// current filesystem usage never reduce an outstanding fixed-volume charge.
type storageReservationLimits struct {
	Bytes        int64
	Inodes       int64
	VolumeBytes  int64
	VolumeInodes int64
	OwnerBytes   int64
	OwnerInodes  int64
	BaseBytes    int64
	BaseInodes   int64
	MaxLeases    int
	MaxPins      int
}

type storageReservationPhase uint8

const (
	storageReserved storageReservationPhase = iota
	storageProvisioning
	storageActive
	storageDeleting
	storageRetired
)

type storageTerminalOutcome uint8

const (
	storageTerminalPending storageTerminalOutcome = iota
	storageTerminalPersisted
	storageTerminalFailed
)

type storageReservationRecord struct {
	ID                 string
	Serial             uint64
	OwnerGeneration    string
	Phase              storageReservationPhase
	Identity           fixedVolumeIdentity `json:",omitzero"`
	ProviderRootDevice uint64
	ProviderRootInode  uint64
	ProviderToken      string
	WritersDrained     bool
	Terminal           storageTerminalOutcome
	DeleteRevision     uint64
	RetirementProof    [32]byte            `json:",omitzero"`
	RetirementIdentity fixedVolumeIdentity `json:",omitzero"`
	Pins               []uint64
}

type storageReservationState struct {
	Version    int
	Revision   uint64
	NextSerial uint64
	Limits     storageReservationLimits
	Records    []storageReservationRecord
}

// storageReservationJournal is the persistence seam for ledger fault tests.
// Only the rooted, exclusively locked Linux implementation is suitable for the
// broker. An in-memory implementation provides no capacity enforcement proof.
type storageReservationJournal interface {
	load() (storageReservationState, bool, error)
	store(storageReservationState) error
	close() error
}

type singleOwnerStorageJournal interface {
	claimLedger() error
}

type storageReservationLedger struct {
	mu             sync.Mutex
	journal        storageReservationJournal
	state          storageReservationState
	failed         error
	recovering     bool
	closed         bool
	retirePermits  map[uint64]*storageRetirePermit
	retireReceipts map[uint64]*storageRetirementReceipt
}

type storageLease struct {
	ledger *storageReservationLedger
	id     string
	serial uint64
}

// A permit is issued only after the corresponding intent is durable. It does
// not attest write confinement, command-domain retirement or backend readiness.
type storageProvisionPermit struct {
	ledger      *storageReservationLedger
	id          string
	serial      uint64
	bytes       int64
	inodes      int64
	ownerBytes  int64
	ownerInodes int64
}

type storageRetirePermit struct {
	ledger             *storageReservationLedger
	id                 string
	serial             uint64
	revision           uint64
	identity           fixedVolumeIdentity
	providerRootDevice uint64
	providerRootInode  uint64
	providerToken      string
	cleanupIdentity    fixedVolumeIdentity
}

type storageReaderPin struct {
	lease  *storageLease
	serial uint64
}

type storageReservationSnapshot struct {
	ReservedBytes    int64
	ReservedInodes   int64
	AvailableBytes   int64
	AvailableInodes  int64
	Leases           int
	Readers          int
	Fenced           bool
	RecoveryRequired bool
	Closed           bool
}

func newStorageReservationLedger(limits storageReservationLimits, journal storageReservationJournal) (*storageReservationLedger, error) {
	if journal == nil || !validStorageReservationLimits(limits) {
		return nil, ErrStorageUnsafe
	}
	if owner, ok := journal.(singleOwnerStorageJournal); ok {
		if err := owner.claimLedger(); err != nil {
			return nil, err
		}
	}
	state, exists, err := journal.load()
	if err != nil {
		return nil, errors.Join(ErrStoragePersistence, err)
	}
	ledger := &storageReservationLedger{journal: journal, retirePermits: make(map[uint64]*storageRetirePermit),
		retireReceipts: make(map[uint64]*storageRetirementReceipt)}
	if exists {
		if err := validateStorageReservationState(state, limits); err != nil {
			return nil, err
		}
		ledger.state = cloneStorageReservationState(state)
		// A persisted pin is never discarded on restart. Neither an RPC loss
		// nor a missing PID proves that inherited filesystem references ended.
		ledger.recovering = len(state.Records) != 0
		return ledger, nil
	}
	state = storageReservationState{Version: storageJournalVersion, NextSerial: 1, Limits: limits}
	if err := ledger.commitLocked(state); err != nil {
		return nil, err
	}
	return ledger, nil
}

func validStorageReservationLimits(l storageReservationLimits) bool {
	bytes, bytesOK := addStorageCapacity(l.VolumeBytes, l.OwnerBytes)
	inodes, inodesOK := addStorageCapacity(l.VolumeInodes, l.OwnerInodes)
	return l.Bytes > 0 && l.Inodes > 0 && l.VolumeBytes > 0 && l.VolumeInodes > 0 &&
		l.OwnerBytes > 0 && l.OwnerInodes > 0 && l.BaseBytes >= minStorageJournalBaseBytes &&
		l.BaseInodes >= minStorageJournalBaseInodes && l.BaseBytes <= l.Bytes && l.BaseInodes <= l.Inodes &&
		bytesOK && inodesOK && bytes <= l.Bytes-l.BaseBytes && inodes <= l.Inodes-l.BaseInodes &&
		l.MaxLeases >= 1 && l.MaxLeases <= maxStorageLeases && l.MaxPins >= 1 && l.MaxPins <= maxStoragePins
}

func addStorageCapacity(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

func storageStateCharge(state storageReservationState) (bytes, inodes int64, readers int, ok bool) {
	bytes, inodes = state.Limits.BaseBytes, state.Limits.BaseInodes
	jobBytes, bytesOK := addStorageCapacity(state.Limits.VolumeBytes, state.Limits.OwnerBytes)
	jobInodes, inodesOK := addStorageCapacity(state.Limits.VolumeInodes, state.Limits.OwnerInodes)
	if !bytesOK || !inodesOK {
		return 0, 0, 0, false
	}
	for _, record := range state.Records {
		bytes, bytesOK = addStorageCapacity(bytes, jobBytes)
		inodes, inodesOK = addStorageCapacity(inodes, jobInodes)
		if !bytesOK || !inodesOK || len(record.Pins) > state.Limits.MaxPins-readers {
			return 0, 0, 0, false
		}
		readers += len(record.Pins)
	}
	return bytes, inodes, readers, true
}

func validateStorageReservationState(state storageReservationState, limits storageReservationLimits) error {
	if state.Version != storageJournalVersion || state.Limits != limits || state.NextSerial == 0 ||
		state.Revision == 0 || len(state.Records) > limits.MaxLeases {
		return ErrStorageUnsafe
	}
	seenIDs := make(map[string]bool, len(state.Records))
	seenSerials := make(map[uint64]bool, len(state.Records))
	for _, record := range state.Records {
		if !validJobID(record.ID) || !validJobID(record.OwnerGeneration) || seenIDs[record.ID] ||
			record.Serial == 0 || record.Serial >= state.NextSerial || seenSerials[record.Serial] ||
			record.Phase > storageRetired || record.Terminal > storageTerminalFailed {
			return ErrStorageUnsafe
		}
		seenIDs[record.ID], seenSerials[record.Serial] = true, true
		providerBound := record.ProviderRootDevice != 0 && record.ProviderRootInode != 0 && validJobID(record.ProviderToken)
		if !providerBound && (record.ProviderRootDevice != 0 || record.ProviderRootInode != 0 || record.ProviderToken != "") ||
			record.Phase == storageReserved && providerBound || record.Phase == storageActive && !providerBound {
			return ErrStorageUnsafe
		}
		if record.Phase >= storageDeleting && (record.DeleteRevision == 0 || record.DeleteRevision > state.Revision) ||
			record.Phase < storageDeleting && record.DeleteRevision != 0 ||
			record.Phase == storageRetired && record.RetirementProof == ([32]byte{}) ||
			record.Phase != storageRetired && record.RetirementProof != ([32]byte{}) {
			return ErrStorageUnsafe
		}
		if (record.Phase != storageRetired || !providerBound) && record.RetirementIdentity != (fixedVolumeIdentity{}) {
			return ErrStorageUnsafe
		}
		if record.Phase == storageRetired && providerBound {
			if !validStorageCleanupIdentity(storageRecordCleanupIdentity(record), record, limits) {
				return ErrStorageUnsafe
			}
		}
		if record.Phase == storageActive || record.Identity != (fixedVolumeIdentity{}) {
			if err := record.Identity.validateReservation(limits.VolumeBytes, limits.VolumeInodes, limits.OwnerBytes); err != nil {
				return errors.Join(ErrStorageUnsafe, err)
			}
			if record.Identity.ProvisionRootDevice != record.ProviderRootDevice || record.Identity.ProvisionRootInode != record.ProviderRootInode ||
				record.Identity.ProviderToken != record.ProviderToken {
				return ErrStorageUnsafe
			}
			if record.Identity.ID != fixedVolumeLeaseID(record.ID, record.Serial) || record.Phase == storageProvisioning {
				return ErrStorageUnsafe
			}
		} else if record.WritersDrained || record.Terminal != storageTerminalPending || len(record.Pins) != 0 {
			return ErrStorageUnsafe
		}
		if record.Terminal != storageTerminalPending && !record.WritersDrained || record.Phase >= storageDeleting && len(record.Pins) != 0 {
			return ErrStorageUnsafe
		}
		for _, pin := range record.Pins {
			if pin == 0 || pin >= state.NextSerial || seenSerials[pin] {
				return ErrStorageUnsafe
			}
			seenSerials[pin] = true
		}
	}
	bytes, inodes, _, ok := storageStateCharge(state)
	if !ok || bytes > limits.Bytes || inodes > limits.Inodes {
		return ErrStorageUnsafe
	}
	return nil
}

func validStorageCleanupIdentity(cleanup fixedVolumeIdentity, record storageReservationRecord, limits storageReservationLimits) bool {
	metadataBytes := limits.OwnerBytes - fixedVolumeOwnerBytes
	allowance, allowanceOK := addStorageCapacity(limits.VolumeBytes, metadataBytes)
	return cleanup.ID == fixedVolumeLeaseID(record.ID, record.Serial) && cleanup.ProviderToken == record.ProviderToken &&
		cleanup.ProvisionRootDevice == record.ProviderRootDevice && cleanup.ProvisionRootInode == record.ProviderRootInode &&
		cleanup.LedgerRootDevice != 0 && cleanup.LedgerRootInode != 0 && fixedVolumeValidUUID(cleanup.BootID) &&
		cleanup.MountNamespaceDevice != 0 && cleanup.MountNamespaceInode != 0 && cleanup.BlockSize == fixedVolumeBlockSize &&
		allowanceOK && cleanup.BackingAllowanceBytes == allowance && cleanup.BackingBytes == limits.VolumeBytes &&
		cleanup.BackingObservedBytes >= 0 && cleanup.BackingObservedBytes <= cleanup.BackingBytes &&
		cleanup.BackingAllocatedBytes >= 0 && cleanup.BackingAllocatedBytes <= allowance &&
		(cleanup.BackingMapSHA256 == "" || fixedVolumeHex(cleanup.BackingMapSHA256, 32)) &&
		cleanup.FilesystemBytes >= 0 && cleanup.FilesystemBytes <= limits.VolumeBytes &&
		cleanup.FilesystemInodes >= 0 && cleanup.FilesystemInodes <= limits.VolumeInodes &&
		(cleanup.FilesystemUUID == "" || fixedVolumeValidUUID(cleanup.FilesystemUUID)) &&
		(cleanup.NamespaceDevice == 0) == (cleanup.NamespaceInode == 0) &&
		(cleanup.BackingDevice == 0) == (cleanup.BackingInode == 0) &&
		(cleanup.MountpointDevice == 0) == (cleanup.MountpointInode == 0) &&
		(cleanup.BackingInode == 0 || cleanup.NamespaceInode != 0) &&
		(cleanup.LoopDevice == 0 || cleanup.BackingInode != 0) &&
		(record.Identity == (fixedVolumeIdentity{}) || cleanup == record.Identity)
}

func storageRecordCleanupIdentity(record storageReservationRecord) fixedVolumeIdentity {
	if record.RetirementIdentity == (fixedVolumeIdentity{}) && record.Phase == storageRetired {
		return record.Identity
	}
	return record.RetirementIdentity
}

func cloneStorageReservationState(state storageReservationState) storageReservationState {
	state.Records = slices.Clone(state.Records)
	for index := range state.Records {
		state.Records[index].Pins = slices.Clone(state.Records[index].Pins)
	}
	return state
}

func (l *storageReservationLedger) commitLocked(candidate storageReservationState) error {
	if l.state.Revision == math.MaxUint64 {
		l.failed = ErrStorageUnsafe
		return l.failed
	}
	candidate.Revision = l.state.Revision + 1
	if err := l.journal.store(candidate); err != nil {
		// Rename may have succeeded before directory synchronization failed.
		// Every subsequent action is fenced until a new owner reads the ledger
		// and reconciles the actual resources; rollback is not an authority.
		l.failed = errors.Join(ErrStoragePersistence, err)
		return l.failed
	}
	l.state = candidate
	return nil
}

func (l *storageReservationLedger) availableLocked(admission bool) error {
	if l.closed {
		return ErrStorageUnavailable
	}
	if l.failed != nil {
		return l.failed
	}
	if admission && l.recovering {
		return ErrStorageRecoveryRequired
	}
	return nil
}

func (l *storageReservationLedger) reserve(id, ownerGeneration string) (*storageLease, error) {
	if l == nil || !validJobID(id) || !validJobID(ownerGeneration) {
		return nil, ErrStorageUnsafe
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.availableLocked(true); err != nil {
		return nil, err
	}
	if l.recordIndexLocked(id, 0) >= 0 {
		return nil, ErrStorageUnsafe
	}
	bytes, inodes, _, ok := storageStateCharge(l.state)
	jobBytes, _ := addStorageCapacity(l.state.Limits.VolumeBytes, l.state.Limits.OwnerBytes)
	jobInodes, _ := addStorageCapacity(l.state.Limits.VolumeInodes, l.state.Limits.OwnerInodes)
	if !ok || len(l.state.Records) >= l.state.Limits.MaxLeases || jobBytes > l.state.Limits.Bytes-bytes || jobInodes > l.state.Limits.Inodes-inodes {
		return nil, ErrStorageCapacity
	}
	candidate := cloneStorageReservationState(l.state)
	serial, err := takeStorageSerial(&candidate)
	if err != nil {
		return nil, err
	}
	candidate.Records = append(candidate.Records, storageReservationRecord{ID: id, Serial: serial, OwnerGeneration: ownerGeneration})
	slices.SortFunc(candidate.Records, func(a, b storageReservationRecord) int { return compareStorageIDs(a.ID, b.ID) })
	if err := l.commitLocked(candidate); err != nil {
		return nil, err
	}
	return &storageLease{ledger: l, id: id, serial: serial}, nil
}

func compareStorageIDs(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func takeStorageSerial(state *storageReservationState) (uint64, error) {
	if state.NextSerial == 0 || state.NextSerial == math.MaxUint64 {
		return 0, ErrStorageUnsafe
	}
	serial := state.NextSerial
	state.NextSerial++
	return serial, nil
}

func (l *storageReservationLedger) recordIndexLocked(id string, serial uint64) int {
	for index, record := range l.state.Records {
		if record.ID == id && (serial == 0 || record.Serial == serial) {
			return index
		}
	}
	return -1
}

func (lease *storageLease) mutate(admission bool, change func(*storageReservationState, *storageReservationRecord) error) error {
	if lease == nil || lease.ledger == nil {
		return ErrStorageUnsafe
	}
	l := lease.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.availableLocked(admission); err != nil {
		return err
	}
	index := l.recordIndexLocked(lease.id, lease.serial)
	if index < 0 {
		return ErrStorageUnsafe
	}
	candidate := cloneStorageReservationState(l.state)
	if err := change(&candidate, &candidate.Records[index]); err != nil {
		return err
	}
	return l.commitLocked(candidate)
}

func (lease *storageLease) authorizeProvision() (*storageProvisionPermit, error) {
	var limits storageReservationLimits
	err := lease.mutate(true, func(state *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageReserved {
			return ErrStorageUnsafe
		}
		limits = state.Limits
		record.Phase = storageProvisioning
		return nil
	})
	if err != nil {
		return nil, err
	}
	l := lease.ledger
	return &storageProvisionPermit{ledger: l, id: lease.id, serial: lease.serial,
		bytes: limits.VolumeBytes, inodes: limits.VolumeInodes,
		ownerBytes: limits.OwnerBytes, ownerInodes: limits.OwnerInodes}, nil
}

// claimProvider consumes the provisioning authority exactly once and binds it
// durably to one exact root before mkdir, allocation, formatting or attachment.
// Reusing a permit in another root cannot multiply one physical reservation.
func (permit *storageProvisionPermit) claimProvider(device, inode uint64, token string) error {
	if permit == nil || !permit.valid() || device == 0 || inode == 0 || !validJobID(token) {
		return ErrStorageUnsafe
	}
	lease := &storageLease{ledger: permit.ledger, id: permit.id, serial: permit.serial}
	return lease.mutate(true, func(_ *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageProvisioning || record.ProviderRootDevice != 0 || record.ProviderRootInode != 0 || record.ProviderToken != "" {
			return ErrStorageUnsafe
		}
		record.ProviderRootDevice, record.ProviderRootInode, record.ProviderToken = device, inode, token
		return nil
	})
}

func (permit *storageProvisionPermit) valid() bool {
	if permit == nil || permit.ledger == nil {
		return false
	}
	l := permit.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(permit.id, permit.serial)
	return l.availableLocked(true) == nil && index >= 0 && l.state.Records[index].Phase == storageProvisioning &&
		permit.bytes == l.state.Limits.VolumeBytes && permit.inodes == l.state.Limits.VolumeInodes &&
		permit.ownerBytes == l.state.Limits.OwnerBytes && permit.ownerInodes == l.state.Limits.OwnerInodes
}

func (lease *storageLease) activate(identity fixedVolumeIdentity) error {
	return lease.mutate(false, func(state *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageProvisioning {
			return ErrStorageUnsafe
		}
		if err := identity.validateReservation(state.Limits.VolumeBytes, state.Limits.VolumeInodes, state.Limits.OwnerBytes); err != nil {
			return err
		}
		if identity.ProvisionRootDevice != record.ProviderRootDevice || identity.ProvisionRootInode != record.ProviderRootInode || identity.ProviderToken != record.ProviderToken {
			return ErrStorageUnsafe
		}
		if identity.ID != fixedVolumeLeaseID(record.ID, record.Serial) {
			return ErrStorageUnsafe
		}
		record.Identity, record.Phase = identity, storageActive
		return nil
	})
}

// writersDrained records the owner's joined-writer observation. It neither
// seals the volume nor releases execution or storage capacity. No lease flag
// can stand in for the broker's process-domain and write-confinement proof.
func (lease *storageLease) writersDrained() error {
	return lease.mutate(false, func(_ *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageActive || record.WritersDrained {
			return ErrStorageUnsafe
		}
		record.WritersDrained = true
		return nil
	})
}

func (lease *storageLease) terminalHandled(persisted bool) error {
	return lease.mutate(false, func(_ *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageActive || !record.WritersDrained || record.Terminal != storageTerminalPending {
			return ErrStorageUnsafe
		}
		record.Terminal = storageTerminalFailed
		if persisted {
			record.Terminal = storageTerminalPersisted
		}
		return nil
	})
}

// pin must commit before a reader descriptor is opened or transferred. The
// token remains charged until the owner closes every descriptor it represents.
func (lease *storageLease) pin() (*storageReaderPin, error) {
	var serial uint64
	err := lease.mutate(true, func(state *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageActive {
			return ErrStorageUnsafe
		}
		_, _, readers, ok := storageStateCharge(*state)
		if !ok || readers >= state.Limits.MaxPins {
			return ErrStorageCapacity
		}
		var err error
		serial, err = takeStorageSerial(state)
		if err != nil {
			return err
		}
		record.Pins = append(record.Pins, serial)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &storageReaderPin{lease: lease, serial: serial}, nil
}

func (pin *storageReaderPin) closeAfterDescriptors() error {
	if pin == nil || pin.lease == nil {
		return ErrStorageUnsafe
	}
	return pin.lease.mutate(false, func(_ *storageReservationState, record *storageReservationRecord) error {
		index := slices.Index(record.Pins, pin.serial)
		if index < 0 {
			return ErrStorageUnsafe
		}
		record.Pins = slices.Delete(record.Pins, index, index+1)
		return nil
	})
}

func (lease *storageLease) beginRetirement() (*storageRetirePermit, error) {
	var identity fixedVolumeIdentity
	var providerRootDevice, providerRootInode uint64
	var providerToken string
	err := lease.mutate(false, func(state *storageReservationState, record *storageReservationRecord) error {
		if record.Phase >= storageDeleting || len(record.Pins) != 0 ||
			record.Phase == storageActive && (!record.WritersDrained || record.Terminal == storageTerminalPending) {
			return ErrStorageUnsafe
		}
		identity = record.Identity
		providerRootDevice, providerRootInode, providerToken = record.ProviderRootDevice, record.ProviderRootInode, record.ProviderToken
		record.Phase = storageDeleting
		record.DeleteRevision = state.Revision + 1
		return nil
	})
	if err != nil {
		return nil, err
	}
	l := lease.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(lease.id, lease.serial)
	if index < 0 {
		return nil, ErrStorageUnsafe
	}
	if existing := l.retirePermits[lease.serial]; existing != nil {
		return existing, nil
	}
	permit := &storageRetirePermit{ledger: l, id: lease.id, serial: lease.serial, revision: l.state.Records[index].DeleteRevision, identity: identity,
		providerRootDevice: providerRootDevice, providerRootInode: providerRootInode, providerToken: providerToken}
	l.retirePermits[lease.serial] = permit
	return permit, nil
}

// retirementPermit retries an already durable deletion intent. Recovery grants
// no writer or reader authority and keeps the original full storage charge.
func (lease *storageLease) retirementPermit() (*storageRetirePermit, error) {
	if lease == nil || lease.ledger == nil {
		return nil, ErrStorageUnsafe
	}
	l := lease.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.availableLocked(false); err != nil {
		return nil, err
	}
	index := l.recordIndexLocked(lease.id, lease.serial)
	if index < 0 || l.state.Records[index].Phase < storageDeleting || len(l.state.Records[index].Pins) != 0 {
		return nil, ErrStorageUnsafe
	}
	if permit := l.retirePermits[lease.serial]; permit != nil {
		return permit, nil
	}
	record := l.state.Records[index]
	permit := &storageRetirePermit{ledger: l, id: lease.id, serial: lease.serial, revision: record.DeleteRevision, identity: record.Identity,
		providerRootDevice: record.ProviderRootDevice, providerRootInode: record.ProviderRootInode, providerToken: record.ProviderToken,
		cleanupIdentity: storageRecordCleanupIdentity(record)}
	l.retirePermits[lease.serial] = permit
	return permit, nil
}

func (permit *storageRetirePermit) valid() bool {
	if permit == nil || permit.ledger == nil {
		return false
	}
	l := permit.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(permit.id, permit.serial)
	return l.availableLocked(false) == nil && index >= 0 && l.state.Records[index].Phase >= storageDeleting &&
		len(l.state.Records[index].Pins) == 0 && l.state.Records[index].Identity == permit.identity && permit.revision == l.state.Records[index].DeleteRevision &&
		l.retirePermits[permit.serial] == permit &&
		l.state.Records[index].ProviderRootDevice == permit.providerRootDevice && l.state.Records[index].ProviderRootInode == permit.providerRootInode &&
		l.state.Records[index].ProviderToken == permit.providerToken
}

// acceptRetirementReceipt binds the provider's guarded cleanup transcript to
// the exact issued authority. A digest is not an independent kernel proof.
func (permit *storageRetirePermit) acceptRetirementReceipt(receipt *storageRetirementReceipt) error {
	if permit == nil || receipt == nil || receipt.permit != permit || !permit.valid() ||
		receipt.providerToken != permit.providerToken || receipt.proof == ([32]byte{}) ||
		permit.identity != (fixedVolumeIdentity{}) && receipt.identity != permit.identity {
		return ErrStorageUnsafe
	}
	l := permit.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(permit.id, permit.serial)
	if index < 0 || l.retirePermits[permit.serial] != permit {
		return ErrStorageUnsafe
	}
	record := l.state.Records[index]
	if record.Phase == storageRetired && record.RetirementProof != receipt.proof {
		return ErrStorageUnsafe
	}
	if record.Phase == storageRetired && storageRecordCleanupIdentity(record) != receipt.cleanupIdentity {
		return ErrStorageUnsafe
	}
	if !validStorageCleanupIdentity(receipt.cleanupIdentity, record, l.state.Limits) {
		return ErrStorageUnsafe
	}
	l.retireReceipts[permit.serial] = receipt
	return nil
}

func (permit *storageRetirePermit) confirmedProof() [32]byte {
	if permit == nil || !permit.valid() {
		return [32]byte{}
	}
	l := permit.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(permit.id, permit.serial)
	if index < 0 || l.state.Records[index].Phase != storageRetired {
		return [32]byte{}
	}
	return l.state.Records[index].RetirementProof
}

func (permit *storageRetirePermit) confirmedCleanupIdentity() fixedVolumeIdentity {
	if permit == nil || !permit.valid() {
		return fixedVolumeIdentity{}
	}
	l := permit.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(permit.id, permit.serial)
	if index < 0 || l.state.Records[index].Phase != storageRetired {
		return fixedVolumeIdentity{}
	}
	return storageRecordCleanupIdentity(l.state.Records[index])
}

func (permit *storageRetirePermit) completed(receipt *storageRetirementReceipt) bool {
	if permit == nil || receipt == nil || receipt.permit != permit || !permit.valid() {
		return false
	}
	l := permit.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.recordIndexLocked(permit.id, permit.serial)
	return index >= 0 && l.state.Records[index].Phase == storageRetired &&
		l.state.Records[index].RetirementProof == receipt.proof && l.retireReceipts[permit.serial] == receipt
}

// confirmRetirement durably retains the cleanup proof before the provider's
// final receipt may be removed. The complete byte and inode charge remains.
func (lease *storageLease) confirmRetirement(receipt *storageRetirementReceipt) error {
	if lease == nil || receipt == nil || receipt.permit == nil || receipt.permit.ledger != lease.ledger ||
		receipt.permit.id != lease.id || receipt.permit.serial != lease.serial || !receipt.permit.valid() {
		return ErrStorageUnsafe
	}
	return lease.mutate(false, func(_ *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageDeleting || lease.ledger.retireReceipts[lease.serial] != receipt {
			return ErrStorageUnsafe
		}
		record.Phase, record.RetirementProof, record.RetirementIdentity = storageRetired, receipt.proof, receipt.cleanupIdentity
		// A complete identity already persists the same cleanup authority. Store
		// the extra copy only for partial provisioning, keeping the maximum
		// record/pin population within the reserved journal allocation.
		if record.Identity == record.RetirementIdentity {
			record.RetirementIdentity = fixedVolumeIdentity{}
		}
		return nil
	})
}

// completeRetirement accepts only the provider's exact guarded cleanup receipt.
// There is intentionally no generic release method or current-usage shrink.
func (lease *storageLease) completeRetirement(receipt *storageRetirementReceipt) error {
	if lease == nil || receipt == nil || receipt.permit == nil || receipt.permit.ledger != lease.ledger ||
		receipt.permit.id != lease.id || receipt.permit.serial != lease.serial || !receipt.permit.valid() ||
		receipt.providerToken != receipt.permit.providerToken || receipt.proof == ([32]byte{}) || !receipt.acknowledged.Load() {
		return ErrStorageUnsafe
	}
	err := lease.mutate(false, func(state *storageReservationState, record *storageReservationRecord) error {
		if record.Phase != storageRetired || record.RetirementProof != receipt.proof || storageRecordCleanupIdentity(*record) != receipt.cleanupIdentity ||
			lease.ledger.retireReceipts[lease.serial] != receipt ||
			len(record.Pins) != 0 || record.Identity != receipt.permit.identity {
			return ErrStorageUnsafe
		}
		if record.Identity != (fixedVolumeIdentity{}) && receipt.identity != record.Identity {
			return ErrStorageUnsafe
		}
		index := slices.IndexFunc(state.Records, func(candidate storageReservationRecord) bool {
			return candidate.ID == lease.id && candidate.Serial == lease.serial
		})
		state.Records = slices.Delete(state.Records, index, index+1)
		return nil
	})
	if err == nil {
		lease.ledger.mu.Lock()
		delete(lease.ledger.retirePermits, lease.serial)
		delete(lease.ledger.retireReceipts, lease.serial)
		lease.ledger.mu.Unlock()
	}
	return err
}

// rollbackUnclaimed is the only no-resource rollback. The same durable lock
// proves that no provider ever consumed authority before closing this lease to
// every future claim. A claimed or partially provisioned lease must use actual
// provider cleanup and can never take this path.
func (lease *storageLease) rollbackUnclaimed() error {
	if lease == nil || lease.ledger == nil {
		return ErrStorageUnsafe
	}
	l := lease.ledger
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.availableLocked(false); err != nil {
		return err
	}
	index := l.recordIndexLocked(lease.id, lease.serial)
	if index < 0 {
		return ErrStorageUnsafe
	}
	record := l.state.Records[index]
	if record.ProviderRootDevice != 0 || record.ProviderRootInode != 0 || record.ProviderToken != "" ||
		record.Identity != (fixedVolumeIdentity{}) || len(record.Pins) != 0 || record.WritersDrained || record.Terminal != storageTerminalPending {
		return ErrStorageUnsafe
	}
	if record.Phase != storageRetired {
		candidate := cloneStorageReservationState(l.state)
		pending := &candidate.Records[index]
		if pending.DeleteRevision == 0 {
			pending.DeleteRevision = l.state.Revision + 1
		}
		pending.Phase = storageRetired
		pending.RetirementProof = sha256.Sum256([]byte(fmt.Sprintf("unclaimed:%s:%d:%d", pending.ID, pending.Serial, pending.DeleteRevision)))
		if err := l.commitLocked(candidate); err != nil {
			return err
		}
	}
	candidate := cloneStorageReservationState(l.state)
	candidate.Records = slices.Delete(candidate.Records, index, index+1)
	if err := l.commitLocked(candidate); err != nil {
		return err
	}
	delete(l.retirePermits, lease.serial)
	delete(l.retireReceipts, lease.serial)
	return nil
}

// recoveredLeases allows exact provider cleanup while admission remains fenced.
// It never reconstructs reader-release authority or resets persisted pins.
func (l *storageReservationLedger) recoveredLeases() []*storageLease {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	leases := make([]*storageLease, 0, len(l.state.Records))
	for _, record := range l.state.Records {
		leases = append(leases, &storageLease{ledger: l, id: record.ID, serial: record.Serial})
	}
	return leases
}

func (l *storageReservationLedger) snapshot() storageReservationSnapshot {
	if l == nil {
		return storageReservationSnapshot{Fenced: true}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	bytes, inodes, readers, ok := storageStateCharge(l.state)
	return storageReservationSnapshot{ReservedBytes: bytes, ReservedInodes: inodes,
		AvailableBytes: l.state.Limits.Bytes - bytes, AvailableInodes: l.state.Limits.Inodes - inodes,
		Leases: len(l.state.Records), Readers: readers, Fenced: !ok || l.failed != nil || l.recovering || l.closed,
		RecoveryRequired: l.recovering, Closed: l.closed}
}

func (l *storageReservationLedger) close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if err := l.journal.close(); err != nil {
		return fmt.Errorf("close workspace storage journal: %w", err)
	}
	return nil
}
