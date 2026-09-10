// Package tasks manages stable task definitions and their durable executions.
package tasks

import (
	"errors"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

const (
	LibraryScanKey     = "library.scan"
	LibraryScanEmbyKey = "RefreshLibrary"
	DefaultPageLimit   = 50
	MaxPageLimit       = 200
	MaxStartIndex      = 2147483647
	MaxTriggers        = 32
	MaxRequestIDBytes  = 128
	MaxErrorBytes      = 2048
)

var (
	ErrNotFound         = errors.New("task resource not found")
	ErrNotRunning       = errors.New("task is not running")
	ErrInvalidInput     = errors.New("invalid task input")
	ErrUnavailable      = errors.New("task service unavailable")
	ErrDisabled         = errors.New("task is disabled")
	ErrRequestConflict  = errors.New("task request ID was already used with different input")
	ErrRevisionConflict = errors.New("task revision conflict")
	ErrInconsistent     = errors.New("task execution history is inconsistent")
)

type RunState string

const (
	RunPending     RunState = "pending"
	RunRunning     RunState = "running"
	RunStopping    RunState = "stopping"
	RunCompleted   RunState = "completed"
	RunFailed      RunState = "failed"
	RunCancelled   RunState = "cancelled"
	RunInterrupted RunState = "interrupted"
)

func (state RunState) Active() bool {
	return state == RunPending || state == RunRunning || state == RunStopping
}

type ChildState string

const (
	ChildWaiting     ChildState = "waiting"
	ChildQueued      ChildState = "queued"
	ChildRunning     ChildState = "running"
	ChildCompleted   ChildState = "completed"
	ChildFailed      ChildState = "failed"
	ChildCancelled   ChildState = "cancelled"
	ChildUnavailable ChildState = "unavailable"
	ChildInterrupted ChildState = "interrupted"
)

func (state ChildState) Active() bool {
	return state == ChildWaiting || state == ChildQueued || state == ChildRunning
}

type Actor struct {
	Principal identity.Principal
	Audience  identity.AdministratorAudience
}

type Definition struct {
	ID               string    `json:"id"`
	Key              string    `json:"key"`
	EmbyKey          string    `json:"emby_key"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Category         string    `json:"category"`
	IsHidden         bool      `json:"is_hidden"`
	Enabled          bool      `json:"enabled"`
	Revision         int64     `json:"revision"`
	ScheduleTimezone string    `json:"schedule_timezone"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Triggers         []Trigger `json:"triggers"`
	CurrentRun       *Run      `json:"current_run"`
	LastRun          *Run      `json:"last_run"`
}

// Trigger uses Goby's typed schedule model, not unobserved Emby write fields.
type Trigger struct {
	ID               string     `json:"id"`
	TaskID           string     `json:"task_id"`
	ScheduleRevision int64      `json:"schedule_revision"`
	Position         int        `json:"position"`
	Kind             string     `json:"kind"`
	IntervalTicks    *int64     `json:"interval_ticks"`
	AnchorAt         *time.Time `json:"anchor_at"`
	TimeOfDayTicks   *int64     `json:"time_of_day_ticks"`
	DayOfWeek        *int       `json:"day_of_week"`
	Timezone         *string    `json:"timezone"`
	MaxRuntimeTicks  *int64     `json:"max_runtime_ticks"`
	NextFireAt       *time.Time `json:"next_fire_at"`
	LastDueAt        *time.Time `json:"last_due_at"`
	CalculationError string     `json:"calculation_error"`
	RetiredAt        *time.Time `json:"retired_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Run struct {
	ID             string   `json:"id"`
	TaskID         string   `json:"task_id"`
	State          RunState `json:"state"`
	Source         string   `json:"source"`
	RequestID      *string  `json:"request_id"`
	ActorUserID    string   `json:"actor_user_id"`
	ActorSessionID string   `json:"actor_session_id"`
	ActorKind      string   `json:"actor_kind"`
	TaskKey        string   `json:"task_key"`
	TaskEmbyKey    string   `json:"task_emby_key"`
	TaskName       string   `json:"task_name"`
	// Trigger identity, revision, and due instant are all absent for manual
	// requests and all present for scheduled/startup runs. The referenced rule
	// remains retained, including after replacement retires its revision.
	TriggerID           *string    `json:"trigger_id"`
	TriggerRevision     *int64     `json:"trigger_revision"`
	ScheduledFor        *time.Time `json:"scheduled_for"`
	MaxRuntimeTicks     *int64     `json:"max_runtime_ticks"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at"`
	DeadlineAt          *time.Time `json:"deadline_at"`
	StopRequestedAt     *time.Time `json:"stop_requested_at"`
	StopReason          string     `json:"stop_reason"`
	FinishedAt          *time.Time `json:"finished_at"`
	ErrorCode           string     `json:"error_code"`
	ErrorMessage        string     `json:"error_message"`
	TotalChildren       int64      `json:"total_children"`
	TerminalChildren    int64      `json:"terminal_children"`
	CompletedChildren   int64      `json:"completed_children"`
	FailedChildren      int64      `json:"failed_children"`
	CancelledChildren   int64      `json:"cancelled_children"`
	InterruptedChildren int64      `json:"interrupted_children"`
	UnavailableChildren int64      `json:"unavailable_children"`
	Scanned             int64      `json:"scanned"`
	Added               int64      `json:"added"`
	Updated             int64      `json:"updated"`
}

type Child struct {
	ID           string     `json:"id"`
	RunID        string     `json:"run_id"`
	LibraryID    string     `json:"library_id"`
	LibraryName  string     `json:"library_name"`
	Ordinal      int        `json:"ordinal"`
	State        ChildState `json:"state"`
	ScanJobID    *string    `json:"scan_job_id"`
	Scanned      int64      `json:"scanned"`
	Added        int64      `json:"added"`
	Updated      int64      `json:"updated"`
	ErrorCode    string     `json:"error_code"`
	ErrorMessage string     `json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

type ListOptions struct {
	IsHidden  *bool
	IsEnabled *bool
}

type Page struct {
	StartIndex int
	Limit      int
}

type RunPage struct {
	Items            []Run
	TotalRecordCount int64
	StartIndex       int
	Limit            int
}

type ChildPage struct {
	Items            []Child
	TotalRecordCount int64
	StartIndex       int
	Limit            int
}

type StartRequest struct {
	TaskID    string
	RequestID string
}

type Admission struct {
	Run      Run
	Admitted bool
}

type ValidationError struct{ Fields map[string]string }

func (err *ValidationError) Error() string { return "invalid task input" }
func (err *ValidationError) Unwrap() error { return ErrInvalidInput }
