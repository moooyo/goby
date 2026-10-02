package media

// Background retirement keeps its outer unknown admission/domain charge even
// after an independently proven group and copier join. The conventional kernel
// registry is needed only until that actual join, not as another synthetic
// unknown-permit pool shared across otherwise independent governors.
func (process *mediaProcess) detachBackgroundKernelOwnerAfterJoin() bool {
	owner := process.conventional
	if owner == nil {
		return !conventionalRetirementSupported() && process.command.ProcessState != nil
	}
	conventionalMediaOwners.mu.Lock()
	defer conventionalMediaOwners.mu.Unlock()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if !owner.joined || !owner.signalsRetired || !owner.registered || owner.slot < 0 || owner.slot >= len(conventionalMediaOwners.entries) ||
		conventionalMediaOwners.entries[owner.slot] != owner {
		return false
	}
	owner.registered = false
	conventionalMediaOwners.entries[owner.slot] = nil
	closeConventionalProcessPin(owner.pin)
	owner.pin = -1
	// Deliberately do not call process.release: the outer fault contract owns
	// that uncertain admission and any additional command-domain reservation.
	return true
}
