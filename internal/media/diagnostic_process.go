package media

import (
	"errors"
	"time"
)

var (
	ErrDiagnosticResources = errors.New("diagnostic_resources_unavailable")
	ErrDiagnosticTool      = errors.New("diagnostic_tool_unavailable")
	ErrDiagnosticInput     = errors.New("diagnostic_input_invalid")
	ErrDiagnosticExecution = errors.New("diagnostic_execution_failed")
	ErrDiagnosticClosure   = errors.New("diagnostic_process_closure_failed")
)

const (
	diagnosticRunDeadline     = 2 * time.Minute
	diagnosticCloseDeadline   = 5 * time.Second
	diagnosticMaximumCommands = 24
	diagnosticMemoryBytes     = 512 << 20
	diagnosticMaximumTasks    = 64
)

// This is an internal execution configuration, never an administrator request
// payload. The parent must be an already delegated cgroup v2 domain with memory
// and pids enabled for children. The executor does not modify the parent or move
// existing processes. Deployment and conversion-slot admission remain the
// caller's responsibility before a process session is created.
type diagnosticProcessOptions struct {
	FFmpegPath          string
	CgroupParent        string
	ScratchDirectory    string
	LoaderDirectories   []string
	HardwareEnvironment map[string]string
}

// A command observation is not a stage acceptance. In particular, it does not
// assert codec selection, hardware execution, or decoded-content correctness.
// Stdout and stderr stay private to the executor; an API must expose only
// verified, bounded structured facts and safe error codes.
type diagnosticCommandObservation struct {
	Started            bool
	ExitCode           int
	Elapsed            time.Duration
	Stdout             []byte `json:"-"`
	Stderr             []byte `json:"-"`
	ToolSHA256         string
	EnvironmentSHA256  string
	InputSHA256        string
	OutputLimitReached bool
	MemoryLimitEvents  uint64
	MemoryOOMEvents    uint64
	MemoryOOMKills     uint64
	TaskLimitEvents    uint64
	ProcessesClosed    bool
}
