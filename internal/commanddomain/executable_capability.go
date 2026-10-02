package commanddomain

import (
	"os"
	"sync"
)

// ExecutableCapability is a process-local root-issued held executable. It is
// not a pathname, wire approval, storage reservation or readiness certificate.
// The caller retains its source descriptor and this capability through every
// actual command, copier and caller-owned consumer join. Domain borrows are
// registered separately and prevent Close before successful retirement.
type ExecutableCapability struct {
	*executableCapabilityState
}

type executableCapabilityState struct {
	mu       sync.Mutex
	file     *os.File
	named    *os.File
	approval ApprovedExecutable
	identity Identity
	issuer   executableCapabilityIssuer
	borrows  [MaxDomainCommands]*Domain
	uses     [MaxDomainCommands]*ExecutableUse
}

type executableCapabilityIssuer struct {
	kind      uint8
	pid       int
	boot      string
	namespace LauncherObjectIdentity
}

// NewExecutableCapability borrows source; it never consumes or reopens that
// descriptor. A non-nil result with an error retains a failed cleanup owner.
// Root identity, immutable native ELF properties and explicit approval are
// checked against actual kernel objects by the supported platform constructor.
func NewExecutableCapability(source *os.File, approvedPath, expectedSHA256 string) (*ExecutableCapability, error) {
	return newExecutableCapability(source, ApprovedExecutable{Path: approvedPath, SHA256: expectedSHA256})
}

// Open creates an independently owned read-only close-on-exec descriptor from
// the held object, never from the approved pathname. This duplicate is not a
// command join or storage proof; its caller must hold it through all actual uses.
func (c *ExecutableCapability) Open() (*os.File, error) {
	if c == nil || c.executableCapabilityState == nil {
		return nil, ErrUnsafe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return nil, ErrClosed
	}
	if err := checkExecutableCapability(c); err != nil {
		return nil, err
	}
	return duplicateExecutableCapability(c)
}

// Close cannot abandon an active or unknown Domain borrow. A failed descriptor
// close retains the same object for its recovery owner instead of claiming that
// the borrowed executable or its associated domain lifetime was returned.
func (c *ExecutableCapability) Close() error {
	if c == nil || c.executableCapabilityState == nil {
		return ErrUnsafe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, owner := range c.borrows {
		if owner != nil {
			return ErrRetained
		}
	}
	for _, use := range c.uses {
		if use != nil {
			return ErrRetained
		}
	}
	if c.named != nil {
		if c.named.Close() != nil {
			return ErrRetained
		}
		c.named = nil
	}
	if c.file == nil {
		return nil
	}
	if err := c.file.Close(); err != nil {
		return ErrRetained
	}
	c.file = nil
	return nil
}

func (ExecutableCapability) MarshalJSON() ([]byte, error) { return nil, ErrUnsafe }
func (*ExecutableCapability) UnmarshalJSON([]byte) error  { return ErrUnsafe }

func (c *ExecutableCapability) releaseBorrow(owner *Domain, slot int) error {
	if c == nil || c.executableCapabilityState == nil || owner == nil || slot < 0 || slot >= len(c.borrows) {
		return ErrRetained
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.borrows[slot] != owner {
		return ErrRetained
	}
	c.borrows[slot] = nil
	return nil
}
