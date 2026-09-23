package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// ProgressSnapshot is the last complete, accepted FFmpeg progress update. It
// excludes media paths, credentials, command arguments and publication state.
type ProgressSnapshot struct {
	OutputTicks int64
	Bytes       int64
	Ended       bool
}

// ProgressFailure is allocated only for the first rejected progress line.
// LineBytes excludes LF and is a lower bound when Truncated is true; LineSHA256
// then covers only CapturedBytes. No raw line or arbitrary field/value survives.
// Previous is nil when no complete progress update preceded the rejection.
type ProgressFailure struct {
	Phase          string
	Reason         string
	Field          string
	LineBytes      int
	CapturedBytes  int
	Truncated      bool
	LineSHA256     string
	SafeValue      string
	Previous       *ProgressSnapshot
	WaitDelay      bool
	WaitErrorClass string
	ExitCode       int
}

var progressDiagnosticNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func newProgressFailure(phase, reason string, raw []byte, observed int, previous *ProgressSnapshot) *ProgressFailure {
	sum := sha256.Sum256(raw)
	failure := &ProgressFailure{Phase: phase, Reason: reason, Field: "unknown", LineBytes: observed,
		CapturedBytes: len(raw), Truncated: observed > len(raw), LineSHA256: hex.EncodeToString(sum[:]), Previous: previous}
	key, value, separated := strings.Cut(strings.TrimSuffix(string(raw), "\r"), "=")
	switch key {
	case "frame", "fps", "stream_0_0_q", "bitrate", "total_size", "out_time_us", "out_time_ms", "out_time", "dup_frames", "drop_frames", "speed", "progress":
		failure.Field = strings.Clone(key)
		if separated && !failure.Truncated && len(value) > 0 && len(value) <= 64 &&
			(value == "continue" || value == "end" || value == "N/A" || progressDiagnosticNumber.MatchString(value)) {
			failure.SafeValue = strings.Clone(value)
		}
	}
	return failure
}

func progressWaitErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, exec.ErrWaitDelay):
		return "wait_delay"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline"
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "exit"
		}
		return "other"
	}
}
