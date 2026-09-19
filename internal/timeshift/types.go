// Package timeshift retains bounded, already published media presentations.
// It neither opens network sources nor authorizes users. Every operation uses
// an exact server-authenticated scope and never accepts an artifact pathname.
package timeshift

import (
	"errors"
	"os"
	"time"
)

const TicksPerSecond int64 = 10_000_000

var (
	ErrInvalid         = errors.New("invalid timeshift request")
	ErrNotFound        = errors.New("timeshift presentation or artifact not found")
	ErrWindowExpired   = errors.New("requested timeshift position has expired")
	ErrNotBuffered     = errors.New("requested position has not been buffered")
	ErrSnapshotChanged = errors.New("timeshift snapshot changed before advertisement")
	ErrBusy            = errors.New("timeshift concurrency limit reached")
	ErrQuota           = errors.New("timeshift storage limit reached")
	ErrClosed          = errors.New("timeshift store or presentation closed")
	ErrEnded           = errors.New("timeshift presentation has ended")
	ErrStorage         = errors.New("timeshift storage operation failed")
	ErrUnsafe          = errors.New("unsafe timeshift storage")
	ErrLocked          = errors.New("timeshift storage is already locked")
	ErrUnsupported     = errors.New("timeshift storage requires Linux")
)

// Scope contains ownership, not credentials. A matching scope does not replace
// the caller's current credential, catalog, source and playback policy checks.
type Scope struct {
	UserID              string
	AuthSessionID       string
	DeviceID            string
	PlaySessionID       string
	ItemID              string
	SourceID            string
	ApplicationKey      bool
	ApplicationClientID string
}

// Variant is fixed for a presentation. Kind is video, audio, or subtitle;
// Format is ts, fmp4, or vtt. Only fmp4 variants have initialization resources.
type Variant struct {
	ID     string
	Kind   string
	Format string
}

type Options struct {
	Root             string
	MaxBytes         int64
	MaxWindows       int
	MaxOwnerWindows  int
	MaxReaders       int
	MaxWindowReaders int
	MaxPublishing    int
	MaxArtifactBytes int64
	MaxSegments      int
	MaxEpochs        int
	MaxVariants      int
	MaxArtifacts     int
	// ReadTimeout is an absolute handle lifetime, never a sliding read-idle
	// timeout. Active reads do not postpone forced closure.
	ReadTimeout time.Duration
	// IdleTimeout measures authorized consumer/control activity. Publishing
	// or updating source state alone never renews this lease.
	IdleTimeout   time.Duration
	SweepInterval time.Duration
	// Now is a clock injection for deterministic retention tests. Production
	// callers should leave it nil. It must be safe for concurrent calls.
	Now func() time.Time
}

type WindowOptions struct {
	Window   time.Duration
	MaxBytes int64
	Variants []Variant
	// TargetDurationTicks fixes the HLS target for the complete presentation.
	// It is a positive whole number of seconds for advertised windows. Zero
	// permits generic storage but deliberately cannot authorize advertisement.
	TargetDurationTicks int64
}

// ArtifactInput borrows a complete regular file from offset zero. Publish does
// not seek or close it, and verifies its identity/size/change time after copying.
// The caller must not modify it until Publish returns.
type ArtifactInput struct {
	VariantID string
	File      *os.File
}

// Publication contains one complete aligned interval for every fixed variant.
// DurationTicks is observed media duration, not an estimate from wall time.
// A larger Generation starts a new epoch. It supplies one initialization file
// per fmp4 variant; same-generation calls must omit Initializations.
type Publication struct {
	Generation      uint64
	DurationTicks   int64
	Initializations []ArtifactInput
	Segments        []ArtifactInput
}

type Artifact struct {
	ID             string
	VariantID      string
	InitID         string
	Size           int64
	Initialization bool
}

type Segment struct {
	Sequence              uint64
	Generation            uint64
	StartTicks            int64
	DurationTicks         int64
	Discontinuity         bool
	DiscontinuitySequence uint64
	Artifacts             []Artifact
}

type Epoch struct {
	Generation            uint64
	FirstSequence         uint64
	DiscontinuitySequence uint64
	Initializations       []Artifact
}

type State struct {
	Ended   bool
	Stalled bool
}

type WindowSnapshot struct {
	PresentationID string
	Revision       uint64
	// NextSequence preserves the monotonic sequence after all current media
	// has expired, including an empty terminal playlist.
	NextSequence          uint64
	EarliestTicks         int64
	LiveEdgeTicks         int64
	LiveStartTicks        int64
	TargetDurationTicks   int64
	DiscontinuitySequence uint64
	// Bytes charges all retained and expired-but-open artifact allocations.
	Bytes        int64
	PendingBytes int64
	Ended        bool
	Stalled      bool
	Variants     []Variant
	Epochs       []Epoch
	Segments     []Segment
}

// Position identifies the retained interval containing the requested point.
// StartTicks is its actual decodable boundary; no media outside the window is
// implied. Live chooses a safe retained start, not an unpublished future frame.
type Position struct {
	Sequence       uint64
	Generation     uint64
	StartTicks     int64
	RequestedTicks int64
}

type Usage struct {
	Bytes        int64
	PendingBytes int64
	Windows      int
	Artifacts    int
	Readers      int
	Publishing   int
}
