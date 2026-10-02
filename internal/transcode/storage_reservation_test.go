package transcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

type memoryStorageJournal struct {
	state               storageReservationState
	exists              bool
	writes              int
	failWrite           int
	commitBeforeFailure bool
}

func (j *memoryStorageJournal) load() (storageReservationState, bool, error) {
	return cloneStorageReservationState(j.state), j.exists, nil
}

func (j *memoryStorageJournal) store(state storageReservationState) error {
	j.writes++
	if j.writes == j.failWrite {
		if j.commitBeforeFailure {
			j.state, j.exists = cloneStorageReservationState(state), true
		}
		return errors.New("injected journal synchronization failure")
	}
	j.state, j.exists = cloneStorageReservationState(state), true
	return nil
}

func (*memoryStorageJournal) close() error { return nil }

func storageTestLimits(volumes int) storageReservationLimits {
	l := storageReservationLimits{VolumeBytes: 64 << 20, VolumeInodes: 1024,
		OwnerBytes: fixedVolumeOwnerBytes + 1<<20, OwnerInodes: fixedVolumeOwnerInodes,
		BaseBytes: minStorageJournalBaseBytes, BaseInodes: minStorageJournalBaseInodes,
		MaxLeases: 8, MaxPins: 4}
	l.Bytes = l.BaseBytes + int64(volumes)*(l.VolumeBytes+l.OwnerBytes)
	l.Inodes = l.BaseInodes + int64(volumes)*(l.VolumeInodes+l.OwnerInodes)
	return l
}

func storageTestID(index int) string { return fmt.Sprintf("%032x", index) }

func storageTestLease(t *testing.T, ledger *storageReservationLedger, index int) *storageLease {
	t.Helper()
	lease, err := ledger.reserve(storageTestID(index), storageTestID(999))
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func storageTestActivate(t *testing.T, lease *storageLease, metadataBytes ...int64) fixedVolumeIdentity {
	t.Helper()
	permit, err := lease.authorizeProvision()
	if err != nil {
		t.Fatal(err)
	}
	if err := permit.claimProvider(1, 2, storageTestID(998)); err != nil {
		t.Fatal(err)
	}
	identity := fixedVolumeIdentity{ID: fixedVolumeLeaseID(lease.id, lease.serial), ProviderToken: storageTestID(998),
		ProvisionRootDevice: 1, ProvisionRootInode: 2, NamespaceDevice: 1, NamespaceInode: 3,
		LedgerRootDevice: 1, LedgerRootInode: 20,
		BootID: "12345678-1234-1234-1234-123456789abc", MountNamespaceDevice: 11, MountNamespaceInode: 12,
		MountpointDevice: 1, MountpointInode: 4, BackingDevice: 1, BackingInode: 5,
		BackingBytes: permit.bytes, BackingAllocatedBytes: permit.bytes, LoopDevice: 7, LoopNumber: 8,
		BackingObservedBytes:  permit.bytes,
		BackingAllowanceBytes: permit.bytes + permit.ownerBytes - fixedVolumeOwnerBytes, BackingMapSHA256: fmt.Sprintf("%064x", 1),
		MountID: 9, RootDevice: 7, RootInode: 2, FilesystemUUID: "12345678-1234-1234-1234-123456789abc",
		FilesystemBytes: permit.bytes - 4096, FilesystemInodes: permit.inodes, BlockSize: fixedVolumeBlockSize}
	if len(metadataBytes) != 0 {
		identity.BackingAllocatedBytes += metadataBytes[0]
	}
	if err := lease.activate(identity); err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestStorageReservationKnownExtentMetadataKeepsFullAllowanceThroughRetirement(t *testing.T) {
	limits := storageTestLimits(1)
	ledger, err := newStorageReservationLedger(limits, &memoryStorageJournal{})
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	identity := storageTestActivate(t, lease, 4096)
	if identity.BackingAllocatedBytes <= identity.BackingBytes {
		t.Fatal("the fixture did not include backing extent metadata")
	}
	if snapshot := ledger.snapshot(); snapshot.ReservedBytes != limits.Bytes {
		t.Fatalf("current allocation shrank the full physical allowance: %+v", snapshot)
	}
	if err := lease.writersDrained(); err != nil {
		t.Fatal(err)
	}
	if err := lease.terminalHandled(true); err != nil {
		t.Fatal(err)
	}
	receipt := storageTestRetireReceipt(t, lease, identity)
	if err := lease.completeRetirement(receipt); err != nil {
		t.Fatalf("known extent metadata inside the durable allowance stranded retirement: %v", err)
	}
	if snapshot := ledger.snapshot(); snapshot.Leases != 0 || snapshot.ReservedBytes != limits.BaseBytes {
		t.Fatalf("exact retirement lost the capacity allowance: %+v", snapshot)
	}
}

func storageTestRetireReceipt(t *testing.T, lease *storageLease, identity fixedVolumeIdentity) *storageRetirementReceipt {
	t.Helper()
	permit, err := lease.beginRetirement()
	if err != nil {
		t.Fatal(err)
	}
	// This synthetic receipt tests ledger bookkeeping only. It is never
	// evidence of kernel allocation, write confinement or guarded deletion.
	receipt := &storageRetirementReceipt{permit: permit, identity: identity, cleanupIdentity: identity, providerToken: permit.providerToken, proof: [32]byte{1}}
	if err := permit.acceptRetirementReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := lease.confirmRetirement(receipt); err != nil {
		t.Fatal(err)
	}
	if !permit.completed(receipt) {
		t.Fatal("cleanup proof was not durably confirmed")
	}
	// The fake provider acknowledges only after the durable confirmation.
	receipt.acknowledged.Store(true)
	return receipt
}

func TestStorageReservationWholeVolumeSurvivesTerminalAndReaders(t *testing.T) {
	limits := storageTestLimits(1)
	ledger, err := newStorageReservationLedger(limits, &memoryStorageJournal{})
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	identity := storageTestActivate(t, lease)
	pin, err := lease.pin()
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.writersDrained(); err != nil {
		t.Fatal(err)
	}
	if err := lease.terminalHandled(true); err != nil {
		t.Fatal(err)
	}
	if next, err := ledger.reserve(storageTestID(2), storageTestID(999)); next != nil || !errors.Is(err, ErrStorageCapacity) {
		t.Fatalf("terminal handling released a retained fixed volume: %v", err)
	}
	if permit, err := lease.beginRetirement(); permit != nil || !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("a reader pin allowed storage retirement: %v", err)
	}
	before := ledger.snapshot()
	if before.ReservedBytes != limits.Bytes || before.ReservedInodes != limits.Inodes || before.Readers != 1 {
		t.Fatalf("the complete volume was not charged through retention: %+v", before)
	}
	if err := pin.closeAfterDescriptors(); err != nil {
		t.Fatal(err)
	}
	receipt := storageTestRetireReceipt(t, lease, identity)
	if _, err := lease.pin(); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("deletion admitted a new reader: %v", err)
	}
	if ledger.snapshot().ReservedBytes != before.ReservedBytes {
		t.Fatal("deletion intent released storage before guarded cleanup")
	}
	if err := lease.completeRetirement(receipt); err != nil {
		t.Fatal(err)
	}
	if err := lease.completeRetirement(receipt); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("stale cleanup receipt released storage twice: %v", err)
	}
	if after := ledger.snapshot(); after.Leases != 0 || after.ReservedBytes != limits.BaseBytes || after.ReservedInodes != limits.BaseInodes {
		t.Fatalf("guarded retirement did not release exactly one complete reservation: %+v", after)
	}
	if _, err := ledger.reserve(storageTestID(2), storageTestID(999)); err != nil {
		t.Fatal(err)
	}
}

func TestStorageReservationBoundsBytesAndInodesIndependently(t *testing.T) {
	for _, dimension := range []string{"bytes", "inodes", "leases"} {
		t.Run(dimension, func(t *testing.T) {
			limits := storageTestLimits(2)
			switch dimension {
			case "bytes":
				limits.Bytes -= limits.VolumeBytes + limits.OwnerBytes
			case "inodes":
				limits.Inodes -= limits.VolumeInodes + limits.OwnerInodes
			case "leases":
				limits.MaxLeases = 1
			}
			ledger, err := newStorageReservationLedger(limits, &memoryStorageJournal{})
			if err != nil {
				t.Fatal(err)
			}
			storageTestLease(t, ledger, 1)
			if _, err := ledger.reserve(storageTestID(2), storageTestID(999)); !errors.Is(err, ErrStorageCapacity) {
				t.Fatalf("the %s dimension did not bound full reservations: %v", dimension, err)
			}
		})
	}
}

func TestStorageProvisionPermitCannotMultiplyOneReservation(t *testing.T) {
	journal := &memoryStorageJournal{}
	ledger, err := newStorageReservationLedger(storageTestLimits(1), journal)
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	permit, err := lease.authorizeProvision()
	if err != nil {
		t.Fatal(err)
	}
	if err := permit.claimProvider(1, 2, storageTestID(998)); err != nil {
		t.Fatal(err)
	}
	for _, root := range []uint64{2, 4} {
		if err := permit.claimProvider(1, root, storageTestID(997)); !errors.Is(err, ErrStorageUnsafe) {
			t.Fatalf("one durable permit created another physical namespace: %v", err)
		}
	}
	if _, err := lease.authorizeProvision(); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("provisioning authority was issued twice: %v", err)
	}
	if record := journal.state.Records[0]; record.ProviderRootInode != 2 || record.ProviderToken != storageTestID(998) {
		t.Fatalf("provider identity was not durable before creation: %+v", record)
	}
}

func TestStorageReservationUnknownCommitFencesEveryNewAuthority(t *testing.T) {
	journal := &memoryStorageJournal{}
	ledger, err := newStorageReservationLedger(storageTestLimits(2), journal)
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	journal.failWrite, journal.commitBeforeFailure = journal.writes+1, true
	if _, err := lease.authorizeProvision(); !errors.Is(err, ErrStoragePersistence) {
		t.Fatalf("unknown commit was accepted: %v", err)
	}
	if !ledger.snapshot().Fenced {
		t.Fatal("an uncertain durable commit left admission open")
	}
	if _, err := ledger.reserve(storageTestID(2), storageTestID(999)); !errors.Is(err, ErrStoragePersistence) {
		t.Fatalf("capacity was allocated after unknown persistence: %v", err)
	}
	if _, err := lease.beginRetirement(); !errors.Is(err, ErrStoragePersistence) {
		t.Fatalf("cleanup authority escaped the persistence fence: %v", err)
	}
	reopened, err := newStorageReservationLedger(storageTestLimits(2), journal)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := reopened.snapshot(); !snapshot.RecoveryRequired || snapshot.Leases != 1 {
		t.Fatalf("recovery ignored a possibly committed reservation: %+v", snapshot)
	}
}

func TestStorageReservationRestartKeepsReaderPinsAndFullCapacity(t *testing.T) {
	journal := &memoryStorageJournal{}
	limits := storageTestLimits(2)
	ledger, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	storageTestActivate(t, lease)
	if _, err := lease.pin(); err != nil {
		t.Fatal(err)
	}
	if err := lease.writersDrained(); err != nil {
		t.Fatal(err)
	}
	if err := lease.terminalHandled(false); err != nil {
		t.Fatal(err)
	}
	before := ledger.snapshot()
	reopened, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	if after := reopened.snapshot(); after.Readers != 1 || after.ReservedBytes != before.ReservedBytes || after.ReservedInodes != before.ReservedInodes || !after.Fenced {
		t.Fatalf("restart discarded ownership or pins: %+v", after)
	}
	if _, err := reopened.reserve(storageTestID(2), storageTestID(999)); !errors.Is(err, ErrStorageRecoveryRequired) {
		t.Fatalf("unreconciled restart admitted work: %v", err)
	}
	recovered := reopened.recoveredLeases()[0]
	if _, err := recovered.pin(); !errors.Is(err, ErrStorageRecoveryRequired) {
		t.Fatalf("recovery granted a reader descriptor: %v", err)
	}
	if _, err := recovered.beginRetirement(); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("restart erased the old generation reader: %v", err)
	}
}

func TestStorageReservationConcurrentGrantsRetainFiniteOwnership(t *testing.T) {
	limits := storageTestLimits(2)
	ledger, err := newStorageReservationLedger(limits, &memoryStorageJournal{})
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for index := range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := ledger.reserve(storageTestID(index+1), storageTestID(999)); err == nil {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if snapshot := ledger.snapshot(); accepted.Load() != 2 || snapshot.Leases != 2 || snapshot.AvailableBytes != 0 || snapshot.AvailableInodes != 0 {
		t.Fatalf("concurrent admission exceeded full-volume capacity: accepted=%d snapshot=%+v", accepted.Load(), snapshot)
	}
}

func TestStorageReservationRejectsCorruptionAndCapacityReinterpretation(t *testing.T) {
	limits := storageTestLimits(2)
	journal := &memoryStorageJournal{}
	ledger, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	storageTestLease(t, ledger, 1)
	for _, mutate := range []func(*storageReservationState){
		func(s *storageReservationState) { s.Version++ },
		func(s *storageReservationState) { s.NextSerial = 1 },
		func(s *storageReservationState) { s.Records = append(s.Records, s.Records[0]) },
		func(s *storageReservationState) { s.Records[0].Phase = storageActive },
		func(s *storageReservationState) { s.Records[0].Pins = []uint64{1} },
		func(s *storageReservationState) { s.Limits.Bytes++ },
	} {
		corrupt := cloneStorageReservationState(journal.state)
		mutate(&corrupt)
		if _, err := newStorageReservationLedger(limits, &memoryStorageJournal{state: corrupt, exists: true}); !errors.Is(err, ErrStorageUnsafe) {
			t.Fatalf("corrupt durable ownership was accepted: %v", err)
		}
	}
	changed := limits
	changed.Bytes += changed.VolumeBytes
	if _, err := newStorageReservationLedger(changed, journal); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("changed policy reinterpreted retained ownership: %v", err)
	}
	overflow := limits
	overflow.VolumeBytes, overflow.OwnerBytes = math.MaxInt64, 1
	if _, err := newStorageReservationLedger(overflow, &memoryStorageJournal{}); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("overflowing whole-volume charge was accepted: %v", err)
	}
}

func TestStorageRetirementReceiptBindsExactIssuedAuthority(t *testing.T) {
	ledger, err := newStorageReservationLedger(storageTestLimits(1), &memoryStorageJournal{})
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	identity := storageTestActivate(t, lease)
	if err := lease.writersDrained(); err != nil {
		t.Fatal(err)
	}
	if err := lease.terminalHandled(true); err != nil {
		t.Fatal(err)
	}
	receipt := storageTestRetireReceipt(t, lease, identity)
	copyPermit := *receipt.permit
	for _, invalid := range []*storageRetirementReceipt{
		{permit: &copyPermit, identity: identity, providerToken: receipt.providerToken, proof: [32]byte{1}},
		{permit: receipt.permit, identity: identity, providerToken: receipt.providerToken},
		{permit: receipt.permit, identity: identity, providerToken: storageTestID(997), proof: [32]byte{1}},
	} {
		invalid.acknowledged.Store(true)
		if err := lease.completeRetirement(invalid); !errors.Is(err, ErrStorageUnsafe) {
			t.Fatalf("an unbound cleanup receipt released capacity: %v", err)
		}
	}
	identity.BackingInode++
	wrongIdentity := &storageRetirementReceipt{permit: receipt.permit, identity: identity, providerToken: receipt.providerToken, proof: [32]byte{1}}
	wrongIdentity.acknowledged.Store(true)
	if err := lease.completeRetirement(wrongIdentity); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("another backing inode released this lease: %v", err)
	}
	if ledger.snapshot().Leases != 1 {
		t.Fatal("a rejected cleanup receipt released ownership")
	}
	if permit, err := lease.retirementPermit(); err != nil || permit != receipt.permit {
		t.Fatalf("cleanup retry invented a different authority: %v", err)
	}
}

func TestStorageRetirementCrashBetweenReceiptAckAndLedgerReleaseKeepsFullProof(t *testing.T) {
	journal := &memoryStorageJournal{}
	limits := storageTestLimits(1)
	ledger, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	identity := storageTestActivate(t, lease)
	if err := lease.writersDrained(); err != nil {
		t.Fatal(err)
	}
	if err := lease.terminalHandled(true); err != nil {
		t.Fatal(err)
	}
	receipt := storageTestRetireReceipt(t, lease, identity)
	reopened, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	recovered := reopened.recoveredLeases()[0]
	permit, err := recovered.retirementPermit()
	if err != nil {
		t.Fatal(err)
	}
	if proof := permit.confirmedProof(); proof != receipt.proof {
		t.Fatal("restart lost the durable cleanup proof after receipt acknowledgement")
	}
	if snapshot := reopened.snapshot(); snapshot.ReservedBytes != limits.Bytes || snapshot.ReservedInodes != limits.Inodes {
		t.Fatalf("uncompleted ledger release lost full capacity: %+v", snapshot)
	}
	reminted := &storageRetirementReceipt{permit: permit, identity: identity, cleanupIdentity: identity, providerToken: permit.providerToken, proof: receipt.proof}
	if err := permit.acceptRetirementReceipt(reminted); err != nil {
		t.Fatal(err)
	}
	if !permit.completed(reminted) {
		t.Fatal("durable cleanup confirmation was not usable by the recovered exact provider")
	}
	if err := recovered.completeRetirement(reminted); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("recovery released capacity before receipt acknowledgement: %v", err)
	}
	reminted.acknowledged.Store(true)
	if err := recovered.completeRetirement(reminted); err != nil {
		t.Fatal(err)
	}
	if snapshot := reopened.snapshot(); snapshot.Leases != 0 || !snapshot.RecoveryRequired {
		t.Fatalf("cleanup discarded the current owner's storage recovery fence: %+v", snapshot)
	}
}

func TestStorageReservationUnclaimedRollbackCannotRetireClaimedResources(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		journal := &memoryStorageJournal{}
		ledger, err := newStorageReservationLedger(storageTestLimits(1), journal)
		if err != nil {
			t.Fatal(err)
		}
		lease := storageTestLease(t, ledger, 1)
		var permit *storageProvisionPermit
		if authorized {
			permit, err = lease.authorizeProvision()
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := lease.rollbackUnclaimed(); err != nil {
			t.Fatal(err)
		}
		if ledger.snapshot().Leases != 0 {
			t.Fatal("an unclaimed reservation could not be durably returned")
		}
		if permit != nil && permit.valid() {
			t.Fatal("rollback left provisioning authority usable")
		}
	}
	ledger, err := newStorageReservationLedger(storageTestLimits(1), &memoryStorageJournal{})
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	permit, err := lease.authorizeProvision()
	if err != nil {
		t.Fatal(err)
	}
	if err := permit.claimProvider(1, 2, storageTestID(998)); err != nil {
		t.Fatal(err)
	}
	if err := lease.rollbackUnclaimed(); !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("a claimed physical domain used no-resource rollback: %v", err)
	}
	if ledger.snapshot().Leases != 1 {
		t.Fatal("a claimed volume lost full accounting")
	}
}

func TestStorageReservationUnclaimedRollbackCrashKeepsDurableAuthorityClosed(t *testing.T) {
	journal := &memoryStorageJournal{}
	limits := storageTestLimits(1)
	ledger, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	permit, err := lease.authorizeProvision()
	if err != nil {
		t.Fatal(err)
	}
	journal.failWrite = journal.writes + 2
	if err := lease.rollbackUnclaimed(); !errors.Is(err, ErrStoragePersistence) {
		t.Fatalf("final rollback commit failure was ignored: %v", err)
	}
	if permit.valid() {
		t.Fatal("a durably retired unclaimed lease could provision after failure")
	}
	reopened, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := reopened.snapshot(); snapshot.Leases != 1 || snapshot.ReservedBytes != limits.Bytes {
		t.Fatalf("failed final release erased the full reservation: %+v", snapshot)
	}
	if err := reopened.recoveredLeases()[0].rollbackUnclaimed(); err != nil {
		t.Fatal(err)
	}
	if reopened.snapshot().Leases != 0 {
		t.Fatal("recovery could not finish the proven unclaimed rollback")
	}
}

func TestStorageReservationPartialCleanupIdentitySurvivesReceiptAcknowledgement(t *testing.T) {
	journal := &memoryStorageJournal{}
	limits := storageTestLimits(1)
	ledger, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	lease := storageTestLease(t, ledger, 1)
	provision, err := lease.authorizeProvision()
	if err != nil {
		t.Fatal(err)
	}
	if err := provision.claimProvider(1, 2, storageTestID(998)); err != nil {
		t.Fatal(err)
	}
	retire, err := lease.beginRetirement()
	if err != nil {
		t.Fatal(err)
	}
	partial := fixedVolumeIdentity{ID: fixedVolumeLeaseID(lease.id, lease.serial), ProviderToken: storageTestID(998),
		ProvisionRootDevice: 1, ProvisionRootInode: 2, LedgerRootDevice: 1, LedgerRootInode: 20,
		BootID: "12345678-1234-1234-1234-123456789abc", MountNamespaceDevice: 11, MountNamespaceInode: 12,
		NamespaceDevice: 1, NamespaceInode: 3, BackingDevice: 1, BackingInode: 5,
		BackingBytes: limits.VolumeBytes, BackingAllowanceBytes: limits.VolumeBytes + limits.OwnerBytes - fixedVolumeOwnerBytes, BlockSize: fixedVolumeBlockSize}
	receipt := &storageRetirementReceipt{permit: retire, cleanupIdentity: partial, providerToken: retire.providerToken, proof: [32]byte{1}}
	if err := retire.acceptRetirementReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := lease.confirmRetirement(receipt); err != nil {
		t.Fatal(err)
	}
	if identity := retire.confirmedCleanupIdentity(); identity != partial {
		t.Fatal("same-process retry lost the confirmed partial cleanup identity")
	}
	reopened, err := newStorageReservationLedger(limits, journal)
	if err != nil {
		t.Fatal(err)
	}
	recovered := reopened.recoveredLeases()[0]
	permit, err := recovered.retirementPermit()
	if err != nil {
		t.Fatal(err)
	}
	if identity := permit.confirmedCleanupIdentity(); identity != partial {
		t.Fatal("restart lost the exact partial provisioning cleanup identity")
	}
	reminted := &storageRetirementReceipt{permit: permit, cleanupIdentity: partial, providerToken: permit.providerToken, proof: receipt.proof}
	if err := permit.acceptRetirementReceipt(reminted); err != nil {
		t.Fatal(err)
	}
	reminted.acknowledged.Store(true)
	if err := recovered.completeRetirement(reminted); err != nil {
		t.Fatal(err)
	}
	if reopened.snapshot().Leases != 0 {
		t.Fatal("confirmed partial cleanup could not release its full reservation")
	}
}

func TestStorageReservationMaximumPopulationFitsReservedJournal(t *testing.T) {
	limits := storageTestLimits(maxStorageLeases)
	limits.MaxLeases, limits.MaxPins = maxStorageLeases, maxStoragePins
	state := storageReservationState{Version: storageJournalVersion, Revision: 1, Limits: limits}
	serial := uint64(math.MaxUint64 - maxStorageLeases - maxStoragePins - 1)
	for index := range maxStorageLeases {
		serial++
		id := storageTestID(index + 1)
		identity := fixedVolumeIdentity{ID: fixedVolumeLeaseID(id, serial), ProviderToken: storageTestID(998),
			ProvisionRootDevice: 1, ProvisionRootInode: 2, LedgerRootDevice: 1, LedgerRootInode: 20,
			BootID: "12345678-1234-1234-1234-123456789abc", MountNamespaceDevice: 11, MountNamespaceInode: 12,
			NamespaceDevice: 1, NamespaceInode: 3, MountpointDevice: 1, MountpointInode: 4,
			BackingDevice: 1, BackingInode: 5, BackingBytes: limits.VolumeBytes, BackingAllocatedBytes: limits.VolumeBytes,
			BackingObservedBytes:  limits.VolumeBytes,
			BackingAllowanceBytes: limits.VolumeBytes + limits.OwnerBytes - fixedVolumeOwnerBytes, BackingMapSHA256: fmt.Sprintf("%064x", 1),
			LoopDevice: 7, LoopNumber: 8, MountID: 9, RootDevice: 7, RootInode: 2,
			FilesystemUUID: "12345678-1234-1234-1234-123456789abc", FilesystemBytes: limits.VolumeBytes - 4096,
			FilesystemInodes: limits.VolumeInodes, BlockSize: fixedVolumeBlockSize}
		record := storageReservationRecord{ID: id, Serial: serial, OwnerGeneration: storageTestID(999),
			Phase: storageActive, Identity: identity, ProviderRootDevice: 1, ProviderRootInode: 2, ProviderToken: storageTestID(998)}
		for range maxStoragePins / maxStorageLeases {
			serial++
			record.Pins = append(record.Pins, serial)
		}
		state.Records = append(state.Records, record)
	}
	state.NextSerial = serial + 1
	for _, retired := range []bool{false, true} {
		if retired {
			for index := range state.Records {
				record := &state.Records[index]
				record.Pins, record.WritersDrained, record.Terminal = nil, true, storageTerminalPersisted
				record.Phase, record.DeleteRevision, record.RetirementProof = storageRetired, 1, [32]byte{1}
			}
		}
		if err := validateStorageReservationState(state, limits); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(state)
		if err != nil || len(data) > maxStorageJournalBytes {
			t.Fatalf("maximum ownership population exceeded its full journal reservation: retired=%t bytes=%d error=%v", retired, len(data), err)
		}
	}
}
