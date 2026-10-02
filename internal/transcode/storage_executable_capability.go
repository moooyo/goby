package transcode

import (
	"errors"
	"os"
	"os/exec"
	"sync"
)

var errFixedExecutableRetained = errors.New("approved executable ownership must be retained")

const maxFixedExecutableUses = 64

// This preparation is created only during the real pooled factory's root
// startup. A trusted installed Go preparer supplies an explicitly approved
// digest and a live descriptor; neither JSON inventory nor a role flag is an
// executable capability. The preparation becomes inactive before publication.
type fixedPoolExecutablePreparation struct {
	issuer any
	active bool
}

// The native issuer owns this private descriptor and every use record. Its
// complete outside-static allocation is still accounted exactly once by inode.
// This object does not attest storage release, consumer drain or readiness.
type fixedPoolExecutableCapability struct {
	issuer       any
	file         *os.File
	path         string
	digest       string
	device       uint64
	inode        uint64
	allocated    int64
	creatorPID   int
	bootID       string
	namespaceDev uint64
	namespaceIno uint64
	poolDevice   uint64
	poolInode    uint64
	poolMountID  uint64
	token        string
	uses         [maxFixedExecutableUses]*fixedPoolExecutableUse
	closeTried   bool
	closed       bool
	closeErr     error
}

// A use owns an independent CLOEXEC descriptor. When it starts a command, only
// its actual Start/Wait operations may establish absence or join. Closing a
// descriptor does not prove a child or callback retired. Native gateways must
// retain a descriptor use until their real domain and owned consumers retire.
type fixedPoolExecutableUse struct {
	mu            *sync.Mutex
	capability    *fixedPoolExecutableCapability
	file          *os.File
	slot          int
	command       *exec.Cmd
	attempted     bool
	startReturned bool
	waitAttempted bool
	started       bool
	joined        bool
	unknown       bool
	closed        bool
}

func (fixedPoolExecutablePreparation) MarshalJSON() ([]byte, error) {
	return nil, errFixedBackingPoolUnsafe
}
func (*fixedPoolExecutablePreparation) UnmarshalJSON([]byte) error { return errFixedBackingPoolUnsafe }
func (fixedPoolExecutableCapability) MarshalJSON() ([]byte, error) {
	return nil, errFixedBackingPoolUnsafe
}
func (*fixedPoolExecutableCapability) UnmarshalJSON([]byte) error { return errFixedBackingPoolUnsafe }
func (fixedPoolExecutableUse) MarshalJSON() ([]byte, error)       { return nil, errFixedBackingPoolUnsafe }
func (*fixedPoolExecutableUse) UnmarshalJSON([]byte) error        { return errFixedBackingPoolUnsafe }
