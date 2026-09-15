package media

import "time"

type DiagnosticSelection struct {
	IncludeConfiguredHardware bool
	// This snapshot is server-owned; callers cannot supply devices or backends
	// through the administrator run request.
	ConfiguredProfile DiagnosticProfile
}

// DiagnosticCommandFact contains observations only. A successful command is
// necessary but never sufficient for a successful stage.
type DiagnosticCommandFact struct {
	Started            bool
	ExitCode           int
	Elapsed            time.Duration
	ToolSHA256         string
	EnvironmentSHA256  string
	InputSHA256        string
	OutputSHA256       string
	OutputBytes        int
	ProcessesClosed    bool
	OutputLimitReached bool
	MemoryLimitEvents  uint64
	MemoryOOMEvents    uint64
	MemoryOOMKills     uint64
	TaskLimitEvents    uint64
	Evidence           *DiagnosticFFmpegEvidence `json:",omitempty"`
}

type DiagnosticPreparation struct {
	ID            string
	Media         string
	Generation    int
	State         string
	Code          string
	ParentSHA256  string
	EncodedSHA256 string
	EncodedBytes  int
	DecodedSHA256 string
	DecodedBytes  int
	ADTS          *DiagnosticADTS         `json:",omitempty"`
	Encode        *DiagnosticCommandFact  `json:",omitempty"`
	Decode        *DiagnosticCommandFact  `json:",omitempty"`
	Video         *DiagnosticVideoContent `json:",omitempty"`
	Audio         *DiagnosticAudioContent `json:",omitempty"`
}

type DiagnosticStage struct {
	ID                  string
	Media               string
	Path                string
	Mode                DiagnosticMode
	Profile             DiagnosticProfile
	State               string
	Code                string
	StartedAt           time.Time
	FinishedAt          time.Time
	ReferenceGeneration int
	ReferenceSHA256     string
	Command             *DiagnosticCommandFact `json:",omitempty"`
	Verification        *DiagnosticCommandFact `json:",omitempty"`
	DecodedSHA256       string
	DecodedBytes        int
	ADTS                *DiagnosticADTS         `json:",omitempty"`
	Video               *DiagnosticVideoContent `json:",omitempty"`
	Audio               *DiagnosticAudioContent `json:",omitempty"`
}

// This report intentionally has no release/support or global hardware flag.
// Even "passed" stages remain provisional while the enclosing owner has not
// completed session closure. This internal pipeline does not itself supply
// administrator or conversion admission; those belong to its enclosing owner.
type DiagnosticReport struct {
	Version                int
	Selection              DiagnosticSelection
	StartedAt              time.Time
	FinishedAt             time.Time
	State                  string
	Code                   string
	ToolVersion            string
	ToolSHA256             string
	EnvironmentSHA256      string
	VersionCommand         *DiagnosticCommandFact `json:",omitempty"`
	CommandsAttempted      int
	CommandsStarted        int
	SessionClosureRequired bool
	Preparations           []DiagnosticPreparation
	Stages                 []DiagnosticStage
}
