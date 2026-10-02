package commanddomain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
)

// ExecutableUse owns a per-command held executable duplicate. The live scope
// catalog remains its authority; neither a descriptor nor its digest can mint
// this use. Close requires all registered command/consumer owners to retire.
type ExecutableUse struct{ *executableUseState }

type executableUseState struct {
	mu           sync.Mutex
	scope        *commandScopeState
	capability   *ExecutableCapability
	approval     ApprovedExecutable
	file         *os.File
	slot         int
	capSlot      int
	registered   bool
	initializing bool
	closed       bool
	unknown      bool
	commands     []*scopedCommand
}

// Metadata duplicates never authorize execution. Their fixed operation slot
// owns the descriptor until successful caller handoff or known successful
// closure; an unknown close remains an exact retained orphan in this slot.
type scopeMetadataDuplicate struct {
	mu      sync.Mutex
	file    *os.File
	slot    int
	unknown bool
}

// DuplicateApprovedExecutable returns caller-owned metadata only. Empty
// expectedSHA is allowed here because the exact catalog path still selects its
// configured digest and actual live capability. Raw duplicates cannot Start.
func (s *CommandScope) DuplicateApprovedExecutable(path, expectedSHA string) (*os.File, error) {
	if s == nil || s.commandScopeState == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	if s.gateClosed || s.closed || s.quarantined {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	slot := freeMetadataDuplicateSlot(s.metadata)
	if slot < 0 {
		s.mu.Unlock()
		return nil, ErrCapacity
	}
	capability, approval, err := s.selectExecutableLocked(path, expectedSHA, true)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	owner := &scopeMetadataDuplicate{slot: slot}
	s.metadata[slot] = owner
	s.notifyLocked()
	s.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			s.retainMetadataDuplicate(owner, ErrRetained)
		}
	}()
	file, err := duplicateScopeMetadata(s.commandScopeState, capability, approval, owner)
	result, resultErr := s.finishMetadataDuplicate(owner, file, err)
	returned = true
	return result, resultErr
}

func freeMetadataDuplicateSlot(owners []*scopeMetadataDuplicate) int {
	for index, owner := range owners {
		if owner == nil {
			return index
		}
	}
	return -1
}

func (s *CommandScope) retainMetadataDuplicate(owner *scopeMetadataDuplicate, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.unknown = true
	s.gateClosed, s.quarantined = true, true
	s.notifyLocked()
	return errors.Join(cause, ErrRetained)
}

func (s *CommandScope) finishMetadataDuplicate(owner *scopeMetadataDuplicate, file *os.File, cause error) (*os.File, error) {
	owner.mu.Lock()
	if owner.file == nil && file != nil {
		owner.file = file
	}
	held := owner.file
	owner.mu.Unlock()
	if cause != nil && held != nil {
		return nil, s.retainMetadataDuplicate(owner, cause)
	}
	if cause == nil && (file == nil || held != file) {
		return nil, s.retainMetadataDuplicate(owner, ErrUnsafe)
	}
	s.mu.Lock()
	if owner.slot < 0 || owner.slot >= len(s.metadata) || s.metadata[owner.slot] != owner {
		s.mu.Unlock()
		return nil, s.retainMetadataDuplicate(owner, ErrRetained)
	}
	closed := s.gateClosed || s.closed || s.quarantined
	if cause != nil || !closed {
		owner.mu.Lock()
		owner.file = nil
		owner.mu.Unlock()
		s.metadata[owner.slot] = nil
		s.notifyLocked()
		s.mu.Unlock()
		return file, cause
	}
	s.mu.Unlock()
	// Admission can close during metadata IO. Do not hand off that late FD.
	// A failed actual Close remains scope-owned rather than caller-ignored.
	if err := file.Close(); err != nil {
		return nil, s.retainMetadataDuplicate(owner, err)
	}
	owner.mu.Lock()
	owner.file = nil
	owner.mu.Unlock()
	s.mu.Lock()
	s.metadata[owner.slot] = nil
	s.notifyLocked()
	s.mu.Unlock()
	return nil, ErrClosed
}

func (s *CommandScope) selectExecutableLocked(path, expectedSHA string, metadata bool) (*ExecutableCapability, ApprovedExecutable, error) {
	if path == "" || !metadata && expectedSHA == "" {
		return nil, ApprovedExecutable{}, ErrUnsafe
	}
	for index, approval := range s.config.Tools {
		if approval.Path != path {
			continue
		}
		if expectedSHA != "" && expectedSHA != approval.SHA256 {
			return nil, ApprovedExecutable{}, ErrUnsafe
		}
		if index >= len(s.tools) || s.tools[index] == nil {
			return nil, ApprovedExecutable{}, ErrUnsafe
		}
		return s.tools[index], approval, nil
	}
	return nil, ApprovedExecutable{}, ErrUnsafe
}

func (s *CommandScope) BorrowApprovedExecutable(path, expectedSHA string) (*ExecutableUse, error) {
	return s.borrowExecutable(path, expectedSHA, nil)
}

// BorrowApprovedExecutableFromFD matches a caller-owned metadata FD against an
// actual registered catalog inode and live issuer. It creates its own duplicate;
// it never consumes this FD or authorizes execution from a digest alone.
func (s *CommandScope) BorrowApprovedExecutableFromFD(file *os.File) (*ExecutableUse, error) {
	if file == nil {
		return nil, ErrUnsafe
	}
	return s.borrowExecutable("", "", file)
}

func (s *CommandScope) borrowExecutable(path, expectedSHA string, source *os.File) (*ExecutableUse, error) {
	if s == nil || s.commandScopeState == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	if s.gateClosed || s.closed || s.quarantined {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	slot := freeExecutableUseSlot(s.uses)
	if slot < 0 {
		s.mu.Unlock()
		return nil, ErrCapacity
	}
	var capability *ExecutableCapability
	var approval ApprovedExecutable
	var err error
	if source == nil {
		capability, approval, err = s.selectExecutableLocked(path, expectedSHA, false)
	} else {
		capability, approval, err = selectScopeExecutableFromFD(s.commandScopeState, source)
	}
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	use := &ExecutableUse{executableUseState: &executableUseState{scope: s.commandScopeState, capability: capability, approval: approval,
		slot: slot, capSlot: -1, initializing: true, commands: make([]*scopedCommand, len(s.slots))}}
	s.uses[slot] = use
	s.notifyLocked()
	s.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			use.quarantine()
		}
	}()
	err = initializeExecutableUse(use, source)
	use.mu.Lock()
	use.initializing = false
	use.mu.Unlock()
	if err == nil {
		s.mu.Lock()
		closed := s.gateClosed || s.closed || s.quarantined
		s.mu.Unlock()
		if closed {
			err = ErrClosed
		}
	}
	if err != nil {
		if closeErr := use.Close(); closeErr != nil {
			returned = true
			return use, errors.Join(err, closeErr, ErrRetained)
		}
		returned = true
		return nil, err
	}
	returned = true
	return use, nil
}

func freeExecutableUseSlot(uses []*ExecutableUse) int {
	for index, current := range uses {
		if current == nil {
			return index
		}
	}
	return -1
}

// Descriptor is borrowed only for stat/ReadAt/hash. Its caller must not Close,
// change flags or transfer ownership. Metadata callers that need to close their
// descriptor use DuplicateApprovedExecutable instead.
func (use *ExecutableUse) Descriptor() (*os.File, error) {
	if use == nil || use.executableUseState == nil {
		return nil, ErrUnsafe
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	if use.closed {
		return nil, ErrClosed
	}
	if use.unknown || use.initializing {
		return nil, ErrRetained
	}
	if err := checkExecutableUse(use); err != nil {
		return nil, err
	}
	return use.file, nil
}

func (use *ExecutableUse) Close() error {
	if use == nil || use.executableUseState == nil || use.scope == nil {
		return ErrUnsafe
	}
	s := use.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	use.mu.Lock()
	defer use.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			use.unknown = true
			s.gateClosed, s.quarantined = true, true
			s.notifyLocked()
		}
	}()
	err := use.closeLocked()
	returned = true
	return err
}

func (use *ExecutableUse) closeLocked() error {
	s := use.scope
	if use.closed {
		return nil
	}
	if use.unknown || use.initializing {
		return ErrRetained
	}
	for _, command := range use.commands {
		if command != nil {
			return ErrRetained
		}
	}
	if use.slot < 0 || use.slot >= len(s.uses) || s.uses[use.slot] != use {
		return ErrRetained
	}
	if use.registered {
		capability := use.capability
		if capability == nil || capability.executableCapabilityState == nil {
			return ErrRetained
		}
		capability.mu.Lock()
		defer capability.mu.Unlock()
		if use.capSlot < 0 || use.capSlot >= len(capability.uses) || capability.uses[use.capSlot] != use {
			return ErrRetained
		}
	}
	if use.file != nil {
		if use.file.Close() != nil {
			use.unknown = true
			s.gateClosed, s.quarantined = true, true
			return ErrRetained
		}
		use.file = nil
	}
	if use.registered {
		capability := use.capability
		capability.uses[use.capSlot] = nil
		use.registered = false
	}
	use.closed = true
	s.uses[use.slot] = nil
	s.notifyLocked()
	return nil
}

func (use *ExecutableUse) quarantine() {
	if use == nil || use.executableUseState == nil || use.scope == nil {
		return
	}
	s := use.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	use.mu.Lock()
	defer use.mu.Unlock()
	use.unknown = true
	s.gateClosed, s.quarantined = true, true
	s.notifyLocked()
}

func (s *CommandScope) StartTemplatePipesWithExecutable(ctx context.Context, template *exec.Cmd, use *ExecutableUse, class LimitsClass, stdout, stderr bool) (*ScopedTemplatePipes, error) {
	if s == nil || s.commandScopeState == nil {
		return nil, ErrUnavailable
	}
	if ctx == nil || template == nil || !stdout && !stderr || use == nil || use.executableUseState == nil {
		return nil, ErrUnsafe
	}
	if err := validateLimitsClass(class); err != nil {
		return nil, err
	}
	if err := checkExecutableUseTemplate(s.commandScopeState, use, template); err != nil {
		return nil, err
	}
	if err := checkLimitsPlatform(class); err != nil {
		return nil, err
	}
	entry, err := s.admitExecutable(ctx, template, use, class)
	if err != nil {
		return nil, err
	}
	handle := &ScopedTemplatePipes{command: entry}
	returned := false
	defer func() {
		if !returned {
			entry.quarantine()
		}
	}()
	err = entry.start(stdout, stderr)
	returned = true
	if entry.isRetired() {
		return nil, err
	}
	return handle, err
}

func (s *CommandScope) admitExecutable(ctx context.Context, template *exec.Cmd, use *ExecutableUse, class LimitsClass) (*scopedCommand, error) {
	if template.Process != nil || template.ProcessState != nil {
		return nil, ErrUnsafe
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gateClosed || s.closed || s.quarantined {
		return nil, ErrClosed
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	if use.scope != s.commandScopeState || use.closed || use.unknown || use.initializing || use.slot < 0 || use.slot >= len(s.uses) || s.uses[use.slot] != use {
		return nil, ErrUnsafe
	}
	index := freeCommandSlot(s.slots)
	if index < 0 {
		return nil, ErrCapacity
	}
	for _, current := range use.commands {
		if current != nil {
			return nil, ErrCapacity
		}
	}
	entry := &scopedCommand{scope: s.commandScopeState, slot: index, context: ctx, template: template, executable: use, limits: class}
	s.slots[index], use.commands[index] = entry, entry
	s.notifyLocked()
	return entry, nil
}

func (ExecutableUse) MarshalJSON() ([]byte, error) { return nil, ErrUnsafe }
func (*ExecutableUse) UnmarshalJSON([]byte) error  { return ErrUnsafe }
