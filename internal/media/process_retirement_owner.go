package media

import (
	"errors"
	"sync"
)

// The registry shares the existing media admission budget. An uncertain owner
// remains reachable with its unreaped leader and strong kernel handle.
var conventionalMediaOwners struct {
	mu      sync.Mutex
	entries [mediaProcessOwnerLimit]*conventionalMediaProcessOwner
}

type conventionalMediaProcessOwner struct {
	mu             sync.Mutex
	process        *mediaProcess
	identity       conventionalProcessIdentity
	pin            int
	slot           int
	unknown        bool
	joined         bool
	registered     bool
	signalsRetired bool
}

func (process *mediaProcess) ensureConventionalOwner() error {
	return process.ensureConventionalOwnerWithCapture(captureConventionalProcess)
}

func (process *mediaProcess) ensureConventionalOwnerWithCapture(capture conventionalProcessCapture) error {
	process.ownerOnce.Do(func() {
		conventionalMediaOwners.mu.Lock()
		defer conventionalMediaOwners.mu.Unlock()
		for slot, entry := range conventionalMediaOwners.entries {
			if entry == nil {
				owner := &conventionalMediaProcessOwner{process: process, slot: slot, pin: -1, registered: true}
				process.conventional = owner
				conventionalMediaOwners.entries[slot] = owner
				if process.command == nil || process.command.Process == nil || process.command.ProcessState != nil {
					owner.unknown = true
					process.ownerErr = ErrProcessRetirementUnknown
					return
				}
				owner.identity, owner.pin, process.ownerErr = capture(process.command)
				if process.ownerErr != nil {
					owner.unknown = true
					process.ownerErr = errors.Join(ErrProcessRetirementUnknown, process.ownerErr)
				}
				return
			}
		}
		process.ownerErr = ErrProcessCapacity
	})
	return process.ownerErr
}

func (owner *conventionalMediaProcessOwner) markUnknown() {
	if owner == nil {
		return
	}
	owner.mu.Lock()
	owner.unknown = true
	owner.mu.Unlock()
	owner.process.observeProbeRetirement(false, true)
}

// joinedKnown is reached only after the independent kernel fence and exec's
// real Wait/copier join. An earlier fault remains visible to the cohort.
func (owner *conventionalMediaProcessOwner) joinedKnown() {
	owner.mu.Lock()
	if owner.joined {
		owner.mu.Unlock()
		return
	}
	owner.joined = true
	unknown := owner.unknown
	owner.mu.Unlock()
	owner.process.observeProbeRetirement(true, unknown)
	if !owner.process.retainForCleanup {
		owner.returnKnownCapacity()
	}
}

func (owner *conventionalMediaProcessOwner) returnKnownCapacity() {
	if owner == nil {
		return
	}
	completed := false
	defer func() {
		if !completed {
			owner.markUnknown()
		}
	}()
	conventionalMediaOwners.mu.Lock()
	defer conventionalMediaOwners.mu.Unlock()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if !owner.joined || !owner.registered {
		completed = true
		return
	}
	if owner.slot < 0 || owner.slot >= len(conventionalMediaOwners.entries) || conventionalMediaOwners.entries[owner.slot] != owner {
		return
	}
	// Keep the exact registry owner while a release may wake a successor.
	owner.process.release()
	owner.registered = false
	conventionalMediaOwners.entries[owner.slot] = nil
	closeConventionalProcessPin(owner.pin)
	owner.pin = -1
	completed = true
}

func (owner *conventionalMediaProcessOwner) cancel() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.joined || owner.signalsRetired {
		return nil
	}
	return cancelConventionalProcess(owner.identity, owner.pin)
}

func (owner *conventionalMediaProcessOwner) fence() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.joined {
		return nil
	}
	if owner.signalsRetired {
		return nil
	}
	if err := fenceConventionalProcess(owner.identity, owner.pin); err != nil {
		return err
	}
	owner.signalsRetired = true
	return nil
}

func (owner *conventionalMediaProcessOwner) retryCapture() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return retryConventionalProcessCapture(owner)
}
